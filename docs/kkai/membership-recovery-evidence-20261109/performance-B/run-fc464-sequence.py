import argparse,pathlib,signal,subprocess,sys
p=argparse.ArgumentParser();p.add_argument('sequence',choices=['stress','production-501','production-0']);a=p.parse_args();r=pathlib.Path(__file__).resolve().parent;signal.alarm(200)
if a.sequence=='stress':
 cells=[('candidate-B-optimized',501),('candidate-B-optimized',0),('candidate-B-optimized',0),('candidate-B-optimized',501)]
else:
 conflicts=int(a.sequence.split('-')[1]);cells=[('baseline-production',conflicts),('candidate-B-optimized-production',conflicts),('candidate-B-optimized-production',conflicts),('baseline-production',conflicts)]
for index,(label,conflicts) in enumerate(cells,1):
 name=f'Boptimized-{a.sequence}-{index}-{label}'
 command=[sys.executable,str(r/'run-fc464.py'),label,name,'--conflicts',str(conflicts)]
 subprocess.run(command,cwd=r,check=True,timeout=40)
print('fixed sequence complete:'+a.sequence,flush=True)
