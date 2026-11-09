#!/usr/bin/env python3
"""Fixed diagnostic subset; immutable performance binaries and frozen thresholds."""
import argparse,datetime,hashlib,json,os,pathlib,signal,subprocess,sys,time
p=argparse.ArgumentParser();p.add_argument('scenario');a=p.parse_args()
r=pathlib.Path(__file__).resolve().parent
plan=json.loads((r/'diagnostic-plan.json').read_text()); spec=next(s for s in plan['scenarios'] if s['id']==a.scenario)
container=json.loads((r/'resource.json').read_text())['name']
signal.alarm(270)
query="""SELECT json_build_object('at',clock_timestamp(),'clients',coalesce((SELECT json_agg(json_build_object('pid',pid,'state',state,'wait_type',wait_event_type,'wait',wait_event,'verb',split_part(ltrim(query),' ',1),'table',substring(query FROM '(?i)(?:FROM|UPDATE|INTO)\\s+([^\\s\\(]+)'),'query_age_ms',extract(epoch FROM clock_timestamp()-query_start)*1000,'transaction_age_ms',extract(epoch FROM clock_timestamp()-xact_start)*1000,'blockers',pg_blocking_pids(pid))) FROM pg_stat_activity WHERE datname=current_database() AND backend_type='client backend' AND application_name<>'membership-perf-diagnostic' AND state<>'idle'),'[]'::json));
\\watch 0.05
"""
def counters():
    p=subprocess.run(['docker','exec',container,'sh','-c','cat /sys/fs/cgroup/cpu.stat /sys/fs/cgroup/io.stat'],text=True,capture_output=True,timeout=5)
    return {'host_load':os.getloadavg(),'cpu_io':p.stdout,'exit':p.returncode,'at':datetime.datetime.now(datetime.timezone.utc).isoformat()}
for index,entry in enumerate(plan['cell_order'],1):
    version,policy=entry.split('/');label='baseline' if version=='baseline' else 'candidate'
    mode='apply' if label=='baseline' else 'paused'
    name=f"diagnostic-{spec['id']}-{index}-{label}-{policy}"
    path=r/(name+'.waits.log');assert not path.exists(), 'diagnostics are one-shot'
    scope=f"^TestMembershipRecoveryHTTPMeasurement/postgres/{spec['funding']}/users-{spec['users']}/conflicts-{spec['conflicts']}/{mode}/{spec['protocol']}/{spec['token']}$"
    with path.open('w') as output:
        sampler=subprocess.Popen(['docker','exec','-i','-e','PGAPPNAME=membership-perf-diagnostic',container,'psql','-X','-qAt','-U','postgres','-d','kkai_test_performance'],stdin=subprocess.PIPE,stdout=output,stderr=subprocess.STDOUT,text=True)
        sampler.stdin.write(query);sampler.stdin.flush()
        before=counters();started=time.monotonic()
        try:
            result=subprocess.run([sys.executable,str(r/'run-batch.py'),label,name,'--scope',scope,'--samples',str(plan['samples']),'--worker',policy],cwd=r,capture_output=True,text=True,timeout=40)
            print(result.stdout,flush=True)
            if result.stderr:print(result.stderr,flush=True)
        finally:
            after=counters()
            subprocess.run(['docker','exec',container,'psql','-X','-qAt','-U','postgres','-d','kkai_test_performance','-c',"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name='membership-perf-diagnostic' AND pid<>pg_backend_pid()"],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=5)
            try:sampler.wait(timeout=5)
            except subprocess.TimeoutExpired:sampler.kill();sampler.wait()
    wait_samples=[]
    for line in path.read_text().splitlines():
        if line.startswith('{'):
            try:wait_samples.append(json.loads(line))
            except ValueError:pass
    (r/(name+'.diagnostic.json')).write_text(json.dumps({'scenario':spec,'before':before,'after':after,'elapsed_seconds':time.monotonic()-started,'sampling_interval_seconds':0.05,'sample_count':len(wait_samples),'wait_samples':wait_samples},indent=2)+'\n')
    if result.returncode or not wait_samples:raise SystemExit(f'diagnostic failed: {name},exit={result.returncode},waits={len(wait_samples)}')
assert hashlib.sha256((r/'baseline-noise-frozen.json').read_bytes()).hexdigest()==plan['frozen_noise_sha256']
print('diagnostic scenario complete; frozen baseline unchanged',flush=True)
