"""Read only fixed recovery summary fields from writer logs; never print raw logs."""
import datetime
import hashlib
import json
import re
import subprocess
import sys

since = sys.argv[1]
assert re.fullmatch(r'[0-9]{10}|[0-9TZ:.-]+', since)
r = subprocess.run(['docker', 'logs', '--timestamps', '--since', since,
                    'kkai-newapi-writer'], text=True, capture_output=True, timeout=15, check=True)
pattern = re.compile(r'membership recovery mode=(\w+) scanned=(\d+) queued=(\d+) settled=(\d+) released=(\d+) conflicts=(\d+) cache_refreshed=(\d+) adjusted_quota=(\d+) failures=(\d+)')
fields = ('mode', 'scanned', 'queued', 'settled', 'released', 'conflicts', 'cache_refreshed', 'adjusted_quota', 'failures')
summaries, failures = [], []
for line in (r.stdout + '\n' + r.stderr).splitlines():
    match = pattern.search(line)
    if match:
        row = dict(zip(fields, match.groups()))
        row.update({k: int(row[k]) for k in fields[1:]})
        row['timestamp'] = line.split(' ', 1)[0]
        summaries.append(row)
    elif ('membership' in line.lower() and ('recovery' in line.lower() or 'reconcile' in line.lower())
          and any(x in line.lower() for x in ('error', 'failed', 'failure'))):
        failures.append({'timestamp': line.split(' ', 1)[0], 'sha256': hashlib.sha256(line.encode()).hexdigest()})
print(json.dumps({'observed_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                  'since': since, 'summaries': summaries, 'other_failure_lines': failures}, sort_keys=True))
