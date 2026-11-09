#!/usr/bin/env python3
"""Run a bounded, recorded subset of an already compiled isolated test binary."""
import argparse, hashlib, json, os, pathlib, subprocess, time
p=argparse.ArgumentParser()
p.add_argument("label",choices=["candidate-B"])
p.add_argument("run_name")
p.add_argument("--scope",required=True)
p.add_argument("--samples",type=int,default=200)
p.add_argument("--worker",choices=["continuous","none"],default="continuous")
a=p.parse_args()
root=pathlib.Path(__file__).resolve().parent
resource=json.loads((root/"resource.json").read_text())
address=subprocess.check_output(["docker","port",resource["name"],"5432/tcp"],text=True).strip()
assert address.startswith("127.0.0.1:") and "\n" not in address
host,port=address.split(":")
secret=(root/"password").read_text().strip()
output=root/(a.run_name+".jsonl")
assert not output.exists(), "use a unique run name; never append unrelated runs"
env=os.environ.copy()
env.update(KKAI_MEMBERSHIP_ENABLED="true",KKAI_MEMBERSHIP_RECOVERY_MEASURE="1",KKAI_MEMBERSHIP_RECOVERY_SAMPLES=str(a.samples),KKAI_MEMBERSHIP_RECOVERY_CONCURRENCY="4",GOMAXPROCS="4",KKAI_MEMBERSHIP_RECOVERY_WORKER=a.worker)
env["KKAI_MEMBERSHIP_RECOVERY_SOURCE"]=(root/(a.label+"-ref.txt")).read_text().strip()
env["KKAI_MEMBERSHIP_RECOVERY_POSTGRES_DSN"]=f"host={host} port={port} user=postgres password={secret} dbname=kkai_test_performance sslmode=disable"
env["KKAI_MEMBERSHIP_RECOVERY_OUTPUT"]=str(output)
binary=root/(a.label+".test")
command=[str(binary),"-test.run",a.scope,"-test.count=1","-test.timeout=180s","-test.v"]
def container_state():
 raw=json.loads(subprocess.check_output(["docker","inspect",resource["name"]],text=True))[0]
 return {"state":raw["State"],"memory_limit_bytes":raw["HostConfig"]["Memory"],"stats":subprocess.check_output(["docker","stats","--no-stream","--format","{{json .}}",resource["name"]],text=True).strip()}
receipt={"label":a.label,"revision":env["KKAI_MEMBERSHIP_RECOVERY_SOURCE"],"command":command,"samples_per_cell":a.samples,"worker_policy":a.worker,"gomaxprocs":4,"binary_sha256":hashlib.sha256(binary.read_bytes()).hexdigest(),"resource_before":container_state()}
started=time.monotonic()
result=subprocess.run(command,cwd=root/a.label,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=200)
receipt.update(exit_code=result.returncode,wall_seconds=time.monotonic()-started,resource_after=container_state())
(root/(a.run_name+".log")).write_text(result.stdout.replace(secret,"[REDACTED]"))
(root/(a.run_name+".receipt.json")).write_text(json.dumps(receipt,indent=2)+"\n")
rows=[json.loads(line) for line in output.read_text().splitlines()] if output.exists() else []
print(json.dumps({"run":a.run_name,"exit_code":result.returncode,"wall_seconds":receipt["wall_seconds"],"cells":len(rows),"succeeded":sum(r["succeeded"] for r in rows),"all_valid":bool(rows) and all(r["valid_functional_measurement"] for r in rows)}),flush=True)
if result.returncode:
 print("\n".join(line for line in result.stdout.splitlines() if "Error:" in line or "FAIL" in line or "stage=" in line)[-8000:])
raise SystemExit(result.returncode)
