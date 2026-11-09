import argparse, json, pathlib, statistics
root=pathlib.Path(__file__).resolve().parent
p=argparse.ArgumentParser();p.add_argument("mode",choices=["freeze","compare"]);p.add_argument("--prefix",default="candidate-A");a=p.parse_args()
fields=["funding","users","conflict_rows","protocol","unlimited_token"]
metrics=["content_p50_ms","content_p95_ms","complete_p50_ms","complete_p95_ms","requests_per_second"]
def key(row):return "|".join(str(row[x]) for x in fields)
def read(path):return [json.loads(line) for line in path.read_text().splitlines()]
noise_path=root/"baseline-noise-frozen.json"
if a.mode=="freeze":
 assert not noise_path.exists(),"noise bounds already frozen; never silently overwrite"
 rows={}
 for funding in ["membership","wallet"]:
  for repeat in [1,2,3]:
   data=read(root/f"baseline-{funding}-{repeat}.jsonl")
   assert len(data)==16 and all(row["valid_functional_measurement"] for row in data)
   for row in data:rows.setdefault(key(row),[]).append(row)
 bounds={k:{metric:{"min":min(row[metric] for row in values),"max":max(row[metric] for row in values),"median":statistics.median(row[metric] for row in values),"runs":[row[metric] for row in values]} for metric in metrics} for k,values in rows.items()}
 noise_path.write_text(json.dumps(bounds,indent=2)+"\n")
 print("frozen",len(bounds),"cell bounds from exactly three pre-candidate baseline repetitions")
else:
 bounds=json.loads(noise_path.read_text());rows={};files=[]
 for funding in ["membership","wallet"]:
  for repeat in [1,2]:
   path=root/f"{a.prefix}-{funding}-{repeat}.jsonl";files.append(path.name)
   for row in read(path):
    assert row["valid_functional_measurement"]
    rows.setdefault(key(row),[]).append(row)
 flags=[];comparison={}
 for k,values in rows.items():
  assert len(values)==2
  comparison[k]={}
  for metric in metrics:
   higher_worse=metric!="requests_per_second"
   boundary=bounds[k][metric]["max" if higher_worse else "min"]
   observed=[row[metric] for row in values]
   worse=all(x>boundary for x in observed) if higher_worse else all(x<boundary for x in observed)
   comparison[k][metric]={"baseline":bounds[k][metric],"candidate":observed,"repeated_outside_frozen_range":worse}
   if worse:flags.append({"cell":k,"metric":metric,"baseline_boundary":boundary,"candidate":observed})
 report={"candidate_prefix":a.prefix,"files":files,"cells":len(rows),"flags":flags,"comparison":comparison,"interpretation":"Flags require diagnosis and prevent an unqualified performance pass. Zero flags only means no repeated regression exceeded this fixed local baseline range, not zero overhead."}
 (root/(a.prefix+"-comparison.json")).write_text(json.dumps(report,indent=2)+"\n")
 print(json.dumps({"cells":len(rows),"flags":flags},indent=2))
