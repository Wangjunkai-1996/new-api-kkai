import argparse,datetime,hashlib,json,os,pathlib,subprocess,time
p=argparse.ArgumentParser();p.add_argument('label',choices=['baseline-production','candidate-B-optimized','candidate-B-optimized-production']);p.add_argument('name');p.add_argument('--conflicts',type=int,choices=[0,501],required=True);a=p.parse_args()
r=pathlib.Path(__file__).resolve().parent;resource=json.loads((r/'resource.json').read_text());secret=(r/'password').read_text().strip()
address=subprocess.check_output(['docker','port',resource['name'],'5432/tcp'],text=True).strip();assert address.startswith('127.0.0.1:') and '\n' not in address;host,port=address.split(':')
ref_label='baseline' if a.label=='baseline-production' else 'candidate-B-optimized';ref=(r/(ref_label+'-ref.txt')).read_text().strip()
policy='production' if a.label.endswith('-production') else 'continuous';output=r/(a.name+'.jsonl');assert not output.exists()
env=dict(os.environ,KKAI_MEMBERSHIP_ENABLED='true',KKAI_MEMBERSHIP_RECOVERY_MEASURE='1',KKAI_MEMBERSHIP_RECOVERY_SAMPLES='200',KKAI_MEMBERSHIP_RECOVERY_CONCURRENCY='4',KKAI_MEMBERSHIP_RECOVERY_WORKER=policy,GOMAXPROCS='4',KKAI_MEMBERSHIP_RECOVERY_SOURCE=ref,KKAI_MEMBERSHIP_RECOVERY_POSTGRES_DSN=f'host={host} port={port} user=postgres password={secret} dbname=kkai_test_performance sslmode=disable',KKAI_MEMBERSHIP_RECOVERY_OUTPUT=str(output))
binary=r/(a.label+'.test');scope=f'^TestMembershipRecoveryHTTPMeasurement/postgres/membership/users-4/conflicts-{a.conflicts}/apply/sse/unlimited$';command=[str(binary),'-test.run',scope,'-test.count=1','-test.timeout=90s','-test.v']
def state():
 raw=json.loads(subprocess.check_output(['docker','inspect',resource['name']],text=True))[0]
 return {'state':raw['State'],'memory_limit_bytes':raw['HostConfig']['Memory'],'stats':subprocess.check_output(['docker','stats','--no-stream','--format','{{json .}}',resource['name']],text=True).strip()}
receipt={'source':ref,'label':a.label,'policy':policy,'command':command,'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'tool_sha256':hashlib.sha256((r/a.label/'service/membership_billing_recovery_http_acceptance_test.go').read_bytes()).hexdigest(),'resource_before':state(),'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()};start=time.monotonic()
p=subprocess.run(command,cwd=r/a.label,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=110);receipt.update(exit_code=p.returncode,wall_seconds=time.monotonic()-start,resource_after=state())
(r/(a.name+'.log')).write_text(p.stdout.replace(secret,'[REDACTED]'));(r/(a.name+'.receipt.json')).write_text(json.dumps(receipt,indent=2)+'\n')
rows=[json.loads(line) for line in output.read_text().splitlines()] if output.exists() else [];assert len(rows)==1
row=rows[0];print(json.dumps({'run':a.name,'exit':p.returncode,'wall_seconds':receipt['wall_seconds'],'succeeded':row['succeeded'],'valid':row['valid_functional_measurement'],'poll_runs':row.get('poll_runs',0),'worker_runs':row['worker_runs'],'worker_active_ms':row['worker_active_ms'],'content_p50_ms':row['content_p50_ms'],'complete_p95_ms':row['complete_p95_ms'],'rps':row['requests_per_second'],'sql_count':row['sql_count_including_worker']}),flush=True)
if p.returncode:print('\n'.join(s for s in p.stdout.splitlines() if 'FAIL' in s or 'Error:' in s)[-6000:])
raise SystemExit(p.returncode)
