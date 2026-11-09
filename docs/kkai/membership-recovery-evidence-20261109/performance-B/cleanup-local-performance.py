import datetime,gzip,hashlib,json,pathlib,shutil,subprocess
repo=pathlib.Path('/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai');root=pathlib.Path('/var/folders/tm/gclh_4_j3530jvnqmxrhjv7c0000gn/T/membership-performance-20261109-abk7b829').resolve();evidence=repo/'docs/kkai/membership-recovery-evidence-20261109';commit='c45e634dd7d1a630b0db84ea462cd385228c5560'
subprocess.run(['git','merge-base','--is-ancestor',commit,'HEAD'],cwd=repo,check=True)
for label in ['performance-A','performance-B']:
 base=evidence/label;m=json.loads((base/'manifest.json').read_text())
 for row in m['files']:
  data=(base/row['file']).read_bytes();assert hashlib.sha256(data).hexdigest()==row['stored_sha256'];raw=gzip.decompress(data) if row['compressed'] else data;assert hashlib.sha256(raw).hexdigest()==row['source_sha256']
for ref in ['922db66baa46c07b5f36cdce8191a52d9af519db','2648161aba4755a4cc7f75a10098deb3cbbd1d4a','cc99ff13494538bacfad06d215415d761013988d','c6fc071f87b81c886935cf9e3ac806aca645cfe0','fc46400f3fed2647fafadd974ea1fc8d005d6015']:
 subprocess.run(['git','cat-file','-e',ref+'^{tree}'],cwd=repo,check=True)
folders=['baseline','baseline-production','candidate','candidate-B','candidate-B-production','candidate-B-final','candidate-B-final-production','candidate-B-optimized','candidate-B-optimized-production']
files=['baseline.test','baseline-production.test','candidate.test','candidate-B.test','candidate-B-final.test','candidate-B-final-production.test','candidate-B-optimized.test','candidate-B-optimized-production.test','workspace-B-pilot.test','candidate-B-source.tar','candidate-B-final-source.tar','candidate-B-optimized-source.tar']
credentials=['password','postgres.env']
removals=[]
for name in folders+files+credentials:
 p=root/name;assert p.parent==root and not p.is_symlink() and p.exists()
 if p.is_dir():
  children=list(p.rglob('*'));assert not any(x.name=='.git' for x in children);size=sum(x.stat().st_size for x in children if x.is_file());item={'path':str(p),'kind':'exported_source_tree','bytes':size,'reconstructible':'exact local Git refs and archived test overlays'}
 else:
  item={'path':str(p),'kind':'credential' if name in credentials else 'test_binary' if name.endswith('.test') else 'source_archive','bytes':p.stat().st_size}
  if name not in credentials:item['sha256']=hashlib.sha256(p.read_bytes()).hexdigest()
 removals.append(item)
resource=json.loads((root/'resource.json').read_text());raw=json.loads(subprocess.check_output(['docker','inspect',resource['name']],text=True))[0];assert raw['Id']==resource['id'];volumes=[m['Name'] for m in raw['Mounts'] if m['Type']=='volume'];assert len(volumes)==len(raw['Mounts'])
for volume in volumes:
 ids=subprocess.check_output(['docker','ps','-aq','--no-trunc','--filter','volume='+volume],text=True).splitlines();assert ids==[resource['id']]
report={'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'authorization':'Parent requested docs commit then exact local performance cleanup; no further local performance tests needed. This releases prior temporary source/binary preservation requirement.','evidence_commit':commit,'scope':'owned local performance container, anonymous volume, two credential files, exact source exports and test binaries; no .local-releases or production access','container':{'id':resource['id'],'name':resource['name'],'anonymous_volumes':volumes},'removed_paths':removals,'preserved':['all raw redacted JSONL/logs and protocol/source/binary manifests','performance-B-evidence directory and tar.gz','tool-overlay frozen A acceptance sources','committed performance-A and performance-B evidence','all local Git source commits','Docker PostgreSQL image cache','production release assets and .local-releases'],'errors':[]}
receipt=root/'performance-cleanup-receipt.json';receipt.write_text(json.dumps(report,indent=2)+'\n')
try:
 subprocess.run(['docker','stop','--time','10',resource['id']],check=True,stdout=subprocess.DEVNULL,timeout=25)
 subprocess.run(['docker','rm','-v',resource['id']],check=True,stdout=subprocess.DEVNULL,timeout=15)
 assert subprocess.run(['docker','inspect',resource['id']],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0
 for volume in volumes:assert subprocess.run(['docker','volume','inspect',volume],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0
 for item in removals:
  p=pathlib.Path(item['path']);shutil.rmtree(p) if p.is_dir() else p.unlink();assert not p.exists();item['removed']=True
 report.update(completed_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),result='completed',removed_local_bytes=sum(x['bytes'] for x in removals),container_absent=True,anonymous_volumes_absent=True,credentials_absent=all(not(root/x).exists() for x in credentials))
except Exception as err:
 report['result']='incomplete';report['errors'].append(type(err).__name__+': '+str(err));raise
finally:receipt.write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps({'result':report['result'],'removed_items':len(removals),'local_bytes':report.get('removed_local_bytes'),'container_absent':report.get('container_absent'),'volume_absent':report.get('anonymous_volumes_absent'),'evidence_commit':commit}))
