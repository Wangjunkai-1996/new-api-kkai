#!/usr/bin/env python3
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts/kkai/release-resume-status.sh"
VERSION = "kkai-prod-20261021.1-111111111"


class ResumeStatusTest(unittest.TestCase):
    def test_only_official_reads_and_exact_coordinates(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            metadata = root / "release.json"
            metadata.write_text(json.dumps({"version": VERSION, "source_sha": "1" * 40}))
            log = root / "calls"
            ssh = root / "ssh"
            ssh.write_text('''#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
with open(os.environ['CALL_LOG'],'a') as f: f.write(' '.join(sys.argv[1:])+'\\n')
if sys.argv[-1]=='candidate-status':
 if os.environ.get('CANDIDATE_NONE')=='1': print('KKAI_CANDIDATE_STATUS=none')
 else:
  print('KKAI_CANDIDATE_STATUS=ready')
  print('KKAI_CANDIDATE_VERSION='+os.environ['CANDIDATE_VERSION'])
  print('KKAI_CANDIDATE_SLOT=green')
elif sys.argv[-2:]==['status','--json']:
 if os.environ.get('ROLLOUT_NONE')=='1': print(json.dumps({'result':'status','status':'inactive','drift':[], 'state':None}))
 else: print(json.dumps({'result':'status','status':os.environ.get('ROLLOUT_STATUS','active'),'healthy':True,'drift':[], 'state':{'rollout_id':'test-rollout','target_slot':'green','target_version':os.environ.get('ROLLOUT_VERSION',os.environ['CANDIDATE_VERSION'])}}))
else: sys.exit(99)
''')
            ssh.chmod(0o755)
            environment = dict(os.environ, PATH=f"{root}:{os.environ['PATH']}", CALL_LOG=str(log), CANDIDATE_VERSION=VERSION)
            command = ["bash", str(SCRIPT), "--metadata", str(metadata), "--slot", "green", "--rollout-id", "test-rollout"]
            result = subprocess.run(command, env=environment, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(len(log.read_text().splitlines()), 2)
            self.assertIn("RESUME_STATUS=read-only", result.stdout)
            environment["CANDIDATE_VERSION"] = "kkai-prod-20261021.2-222222222"
            result = subprocess.run(command, env=environment, capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(len(log.read_text().splitlines()), 3)
            environment["CANDIDATE_VERSION"] = VERSION
            result = subprocess.run(command[:-1] + ["wrong-id"], env=environment, capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            result = subprocess.run(command[:-1] + ["bad;command"], env=environment, capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(len(log.read_text().splitlines()), 5)
            # A healthy historical rollout is not evidence for this metadata.
            environment.update(CANDIDATE_NONE="1", ROLLOUT_STATUS="rolled_back", ROLLOUT_VERSION="kkai-prod-20261020.1-222222222")
            result = subprocess.run(command[:4], env=environment, capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn("RESUME_STATUS=", result.stdout)
            result = subprocess.run(command, env=environment, capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn("RESUME_STATUS=", result.stdout)
            environment["ROLLOUT_NONE"] = "1"
            result = subprocess.run(command[:4], env=environment, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("RESUME_STATUS=read-only", result.stdout)


if __name__ == "__main__":
    unittest.main()
