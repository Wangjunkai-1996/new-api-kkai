import argparse,datetime,hashlib,json,os,pathlib,subprocess,time
p=argparse.ArgumentParser();p.add_argument('label',choices=['baseline-production','candidate-B-optimized','candidate-B-optimized-production']);a=p.parse_args();r=pathlib.Path(__file__).resolve().parent
ref_label='baseline' if a.label=='baseline-production' else 'candidate-B-optimized';ref=(r/(ref_label+'-ref.txt')).read_text().strip();binary=r/(a.label+'.test');assert not binary.exists()
tags='membership_acceptance,membership_legacy_baseline' if a.label=='baseline-production' else 'membership_acceptance';command=['go','test','-c','-mod=readonly','-tags',tags,'-o',str(binary),'./service'];start=time.monotonic()
p=subprocess.run(command,cwd=r/a.label,env=dict(os.environ,GOMAXPROCS='4',GOPROXY='off',GOTOOLCHAIN='local'),stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=180)
(r/(a.label+'-build.log')).write_text(p.stdout);report={'label':a.label,'revision':ref,'tool_sha256':hashlib.sha256((r/a.label/'service/membership_billing_recovery_http_acceptance_test.go').read_bytes()).hexdigest(),'command':command,'exit':p.returncode,'seconds':time.monotonic()-start,'built_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'production_code_overlay':False,'test_only_schedule_overlay':a.label.endswith('-production')}
if p.returncode==0:report['binary_sha256']=hashlib.sha256(binary.read_bytes()).hexdigest()
(r/(a.label+'-build.json')).write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report));print(p.stdout[-5000:]);raise SystemExit(p.returncode)
