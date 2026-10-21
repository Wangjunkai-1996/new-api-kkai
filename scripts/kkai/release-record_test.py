#!/usr/bin/env python3
"""Bounded local checks for release evidence and command exit semantics."""

import json
import signal
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


TOOL = Path(__file__).with_name("release-record.py")
SOURCE = "1" * 40


class ReleaseRecordTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def cli(self, *args):
        return subprocess.run([sys.executable, str(TOOL), *map(str, args)], capture_output=True, text=True, timeout=10)

    def create(self):
        result = self.cli("init", "--directory", self.root, "--kind", "stage", "--release-id", "test-release", "--source-sha", SOURCE)
        self.assertEqual(result.returncode, 0, result.stderr)
        return Path(result.stdout.strip())

    def test_partial_failure_keeps_completed_phase_and_private_identity(self):
        path = self.create()
        self.assertEqual(self.cli("identity", path, "archive_sha256", "a" * 64).returncode, 0)
        self.assertEqual(self.cli("phase", path, "preflight").returncode, 0)
        self.assertEqual(self.cli("phase", path, "upload").returncode, 0)
        self.assertEqual(self.cli("finish", path, 42).returncode, 0)
        record = json.loads(path.read_text())
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        self.assertEqual(record["source_sha"], SOURCE)
        self.assertEqual(record["identity"]["archive_sha256"], "a" * 64)
        self.assertEqual(record["exit_code"], 42)
        self.assertEqual([(phase["name"], phase["result"]) for phase in record["phases"]], [("preflight", "passed"), ("upload", "failed")])
        self.assertTrue(all(phase["duration_seconds"] >= 0 for phase in record["phases"]))
        self.assertNotEqual(self.cli("phase", path, "retry").returncode, 0)

    def test_record_rejects_public_file_and_symlink_without_changing_target(self):
        path = self.create()
        original = path.read_bytes()
        alias = self.root / "alias.json"
        alias.symlink_to(path)
        self.assertNotEqual(self.cli("phase", alias, "upload").returncode, 0)
        path.chmod(0o644)
        self.assertNotEqual(self.cli("phase", path, "upload").returncode, 0)
        self.assertEqual(path.read_bytes(), original)

    def test_run_preserves_command_status_and_does_not_store_arguments_or_output(self):
        metadata = self.root / "release.json"
        metadata.write_text(json.dumps({"version": "test-release", "source_sha": SOURCE, "archive_sha256": "a" * 64}))
        marker = "secret-fixture-token"
        result = self.cli("run", "--metadata", metadata, "--phase", "candidate-acceptance", "--slot", "green", "--rollout-id", "test-rollout", "--", sys.executable, "-c", f"print('{marker}'); raise SystemExit(7)")
        self.assertEqual(result.returncode, 7)
        self.assertIn(marker, result.stdout)
        records = list(self.root.glob("test-release.manual-check-*.json"))
        self.assertEqual(len(records), 1)
        raw = records[0].read_text()
        self.assertNotIn(marker, raw)
        record = json.loads(raw)
        self.assertEqual(record["result"], "failed")
        self.assertEqual(record["phases"][0]["exit_code"], 7)
        self.assertEqual(record["identity"]["archive_sha256"], "a" * 64)
        self.assertEqual(record["identity"]["target_slot"], "green")
        self.assertEqual(record["identity"]["rollout_id"], "test-rollout")

    def test_untrusted_identity_and_filename_are_rejected(self):
        path = self.create()
        self.assertNotEqual(self.cli("identity", path, "credential", "fixture-secret").returncode, 0)
        self.assertNotEqual(self.cli("identity", path, "builder", "invalid value\nfixture-secret").returncode, 0)
        self.assertNotEqual(self.cli("init", "--directory", self.root, "--kind", "stage", "--release-id", "../escaped", "--source-sha", SOURCE).returncode, 0)
        self.assertEqual(json.loads(path.read_text())["identity"], {})

    def test_terminated_wrapper_forwards_signal_and_records_unknown_command_outcome(self):
        metadata = self.root / "release.json"
        metadata.write_text(json.dumps({"version": "test-release", "source_sha": SOURCE}))
        command = [sys.executable, str(TOOL), "run", "--metadata", str(metadata), "--phase", "promote", "--",
                   sys.executable, "-c", "import signal; print('ready', flush=True); signal.pause()"]
        process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            self.assertEqual(process.stdout.readline().strip(), "ready")
            process.send_signal(signal.SIGTERM)
            _, diagnostics = process.communicate(timeout=10)
            self.assertEqual(process.returncode, 143, diagnostics)
        finally:
            if process.poll() is None:
                process.kill()
                process.wait()
        record = json.loads(next(self.root.glob("test-release.manual-check-*.json")).read_text())
        self.assertEqual(record["exit_code"], 143)
        self.assertEqual(record["result"], "failed")

    def test_lost_timing_file_does_not_change_successful_operation(self):
        helper = TOOL.with_suffix(".sh")
        script = '''set -Eeuo pipefail
source "$1"
kkai_record_init stage test-release "$2" "$3"
rm "$KKAI_RECORD_PATH"
kkai_record_phase load-stage
kkai_record_finish 0
'''
        result = subprocess.run(["bash", "-c", script, "test", str(helper), SOURCE, str(self.root)],
                                capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0)
        self.assertIn("timing evidence incomplete", result.stderr)


if __name__ == "__main__":
    unittest.main()
