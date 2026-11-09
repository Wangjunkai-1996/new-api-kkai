import collections,datetime,hashlib,json,pathlib,statistics
r=pathlib.Path(__file__).resolve().parent;b=json.loads((r/'baseline-noise-frozen.json').read_text());protocol=json.loads((r/'B-followup-protocol.json').read_text());assert hashlib.sha256((r/'baseline-noise-frozen.json').read_bytes()).hexdigest()==protocol['frozen_noise_sha256']
metrics=['content_p50_ms','content_p95_ms','complete_p50_ms','complete_p95_ms','requests_per_second'];allrows=[];groups={};tailbase={}
for f in r.glob('baseline-*-?.jsonl'):
 for row in map(json.loads,f.read_text().splitlines()):
  key='|'.join(str(row[x]) for x in ['funding','users','conflict_rows','protocol','unlimited_token']);tailbase.setdefault(key,[]).append(row)
for f in sorted(r.glob('Bfinal-*.jsonl')):
 row=json.loads(f.read_text());row['file']=f.name;allrows.append(row)
 phase='stress' if '-stress-' in f.name else 'production';groups.setdefault((phase,row['conflict_rows']),[]).append(row)
report={'revision':(r/'candidate-B-final-ref.txt').read_text().strip(),'prior_full_matrix_revision':(r/'candidate-B-ref.txt').read_text().strip(),'cells':len(allrows),'requested':sum(x['requested'] for x in allrows),'succeeded':sum(x['succeeded'] for x in allrows),'valid':all(x['valid_functional_measurement'] for x in allrows),'sql_errors':sum(x['sql_errors_including_worker'] for x in allrows),'unsettled':sum(x['unsettled_http_requests'] for x in allrows),'pool_waits':sum(x['pool_wait_count'] for x in allrows),'poll_runs':sum(x.get('poll_runs',0) for x in allrows),'worker_runs':sum(x['worker_runs'] for x in allrows),'frozen_sha256':protocol['frozen_noise_sha256'],'comparisons':{},'created_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
for (phase,conflicts),rows in groups.items():
 key=f'membership|4|{conflicts}|sse|True';candidate=[x for x in rows if x['source_revision']==report['revision']];baseline=[x for x in rows if x['source_revision']!=report['revision']];assert len(candidate)==2
 out={'metrics':{},'primary_flags':[],'secondary_p99_flags':[],'candidate_files':[x['file'] for x in candidate],'baseline_files':[x['file'] for x in baseline]}
 for m in metrics+['content_p99_ms','complete_p99_ms']:
  if m in metrics:bound=b[key][m]['min' if m=='requests_per_second' else 'max']
  else:bound=max(x[m] for x in tailbase[key])
  vals=[x[m] for x in candidate];bv=[x[m] for x in baseline];outside=all(v<bound if m=='requests_per_second' else v>bound for v in vals)
  out['metrics'][m]={'candidate':vals,'original_frozen_boundary':bound,'both_outside':outside,'contemporaneous_baseline':bv}
  if bv:out['metrics'][m]['candidate_median_change_pct']=100*(statistics.median(vals)/statistics.median(bv)-1)
  if outside:out['primary_flags' if m in metrics else 'secondary_p99_flags'].append(m)
 out['candidate_resources']=[{k:x[k] for k in ['worker_runs','worker_active_ms','poll_runs','poll_ms','sql_count_including_worker','process_cpu_ms','gc_cycles','heap_allocated_bytes']} for x in candidate]
 report['comparisons'][phase+'-'+str(conflicts)]=out
(r/'candidate-B-final-followup-summary.json').write_text(json.dumps(report,indent=2)+'\n')
print({k:v for k,v in report.items() if k!='comparisons'})
for name,out in report['comparisons'].items():
 print(name,'primary',out['primary_flags'],'p99',out['secondary_p99_flags'])
 for m,v in out['metrics'].items():print(m,[round(x,3) for x in v['candidate']],v.get('candidate_median_change_pct'))
