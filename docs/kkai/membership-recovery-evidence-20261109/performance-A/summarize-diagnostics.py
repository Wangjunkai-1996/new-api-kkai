import collections,hashlib,json,pathlib,statistics
r=pathlib.Path('.')
plan=json.loads((r/'diagnostic-plan.json').read_text());frozen=json.loads((r/'baseline-noise-frozen.json').read_text());original=json.loads((r/'candidate-A-comparison.json').read_text())
assert hashlib.sha256((r/'baseline-noise-frozen.json').read_bytes()).hexdigest()==plan['frozen_noise_sha256']
report={'purpose':plan['purpose'],'original_flags':original['flags'],'frozen_noise_sha256':plan['frozen_noise_sha256'],'scenarios':[],'cells':0,'requested':0,'succeeded':0,'sql_errors':0,'unsettled':0,'valid':True,'sampling_limitations':'Sampler covers whole test process including setup and teardown. 50ms snapshots do not measure per-query durations or original formal outliers. Same probe added to all diagnostic cells; original binary remains unchanged.'}
def cpu_metrics(raw):
 out={}
 for line in raw.splitlines():
  parts=line.split()
  if len(parts)==2 and parts[1].isdigit():out[parts[0]]=int(parts[1])
 return out
for spec in plan['scenarios']:
 entry={'scenario':spec,'cells':[]}
 key='|'.join(map(str,[spec['funding'],spec['users'],spec['conflicts'],spec['protocol'],spec['token']=='unlimited']))
 for f in sorted(r.glob(f"diagnostic-{spec['id']}-*.jsonl")):
  rows=[json.loads(l) for l in f.read_text().splitlines()];assert len(rows)==1
  row=rows[0]; d=json.loads(f.with_suffix('.diagnostic.json').read_text())
  report['cells']+=1;report['requested']+=row['requested'];report['succeeded']+=row['succeeded'];report['sql_errors']+=row['sql_errors_including_worker'];report['unsettled']+=row['unsettled_http_requests'];report['valid']=report['valid'] and row['valid_functional_measurement']
  waits=collections.Counter();tables=collections.Counter();blocking=0;max_query=0
  for w in d['wait_samples']:
   for c in w['clients']:
    waits['|'.join(str(c[k]) for k in ['state','wait_type','wait'])]+=1
    if c['wait_type']=='Lock':tables[str(c['table'])]+=1
    blocking+=bool(c['blockers']);max_query=max(max_query,c['query_age_ms'] or 0)
  before=cpu_metrics(d['before']['cpu_io']);after=cpu_metrics(d['after']['cpu_io'])
  metrics={k:row[k] for k in row if k not in ['samples','notes','scope']}
  entry['cells'].append({'file':f.name,'metrics':metrics,'wait_observations':dict(waits),'lock_tables':dict(tables),'blocked_observations':blocking,'largest_observed_query_age_ms':max_query,'container_cpu_delta':{k:after.get(k,0)-v for k,v in before.items()},'host_load_before':d['before']['host_load'],'host_load_after':d['after']['host_load']})
 metrics=plan.get('primary_metrics',['content_p50_ms','content_p95_ms','complete_p50_ms','complete_p95_ms','requests_per_second'])
 entry['cross_source']={}
 for metric in metrics:
  baseline=[c['metrics'][metric] for c in entry['cells'] if 'baseline-continuous' in c['file']]
  candidate=[c['metrics'][metric] for c in entry['cells'] if 'candidate-continuous' in c['file']]
  bound=frozen[key][metric]['min' if metric=='requests_per_second' else 'max']
  entry['cross_source'][metric]={'baseline_diagnostic':baseline,'candidate_diagnostic':candidate,'candidate_median_change_pct':100*(statistics.median(candidate)/statistics.median(baseline)-1),'original_frozen_boundary':bound,'diagnostic_both_outside_original':all(v<bound if metric=='requests_per_second' else v>bound for v in candidate)}
 report['scenarios'].append(entry)
(r/'A-diagnostic-summary.json').write_text(json.dumps(report,indent=2)+'\n')
print({k:report[k] for k in ['cells','requested','succeeded','sql_errors','unsettled','valid']})
for e in report['scenarios']:
 print('\n'+e['scenario']['id'])
 for k,v in e['cross_source'].items():print(k,dict(v,baseline_diagnostic=[round(x,3) for x in v['baseline_diagnostic']],candidate_diagnostic=[round(x,3) for x in v['candidate_diagnostic']],candidate_median_change_pct=round(v['candidate_median_change_pct'],2)))
 for c in e['cells']:
  if 'none' in c['file']:print(c['file'],{k:c['metrics'][k] for k in ['sql_count_including_worker','process_cpu_ms','gc_cycles','heap_allocated_bytes']})
 print('cpu throttle_usec deltas',[c['container_cpu_delta'].get('throttled_usec') for c in e['cells']]);print('locks',sum(c['blocked_observations'] for c in e['cells']))
