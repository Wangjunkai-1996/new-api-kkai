import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "maintenance", Path(__file__).with_name("prepare-maintenance-release.py")
)
maintenance = importlib.util.module_from_spec(spec)
spec.loader.exec_module(maintenance)


class MaintenanceReleaseTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.version = "kkai-prod-20261005.1-111111111"
        self.metadata = self.root / f"{self.version}.json"
        self.console = {
            "format_version": 1, "api_contracts": [2],
            "capabilities": sorted(maintenance.REQUIRED_CAPABILITIES),
        }
        config = {
            "architecture": "amd64", "os": "linux",
            "config": {
                "User": "10007:10007", "Entrypoint": ["/new-api-entrypoint"],
                "Env": ["FRONTEND_MODE=external"],
                "Labels": {
                    "org.opencontainers.image.revision": "1" * 40,
                    "org.opencontainers.image.version": self.version,
                    "io.kkrich.schema-contract": "feature",
                    "io.kkrich.frontend-mode": "external",
                    "io.kkrich.console-contract": json.dumps(self.console),
                },
            },
        }
        tag = f"kkai-newapi-manual:{self.version}"
        archive_path = self.root / f"{self.version}.tar"
        with tarfile.open(archive_path, "w") as archive:
            for name, document in {
                "manifest.json": [{"Config": "config.json", "RepoTags": [tag]}],
                "config.json": config,
            }.items():
                data = json.dumps(document).encode()
                member = tarfile.TarInfo(name)
                member.size = len(data)
                archive.addfile(member, io.BytesIO(data))
        self.image_id = "sha256:" + hashlib.sha256(json.dumps(config).encode()).hexdigest()
        self.metadata.write_text(json.dumps({
            "source_sha": "1" * 40, "version": self.version, "image_tag": tag,
            "release_purpose": "maintenance-preparation", "schema_contract": "feature",
            "frontend_mode": "external", "platform": "linux/amd64",
            "archive": archive_path.name,
            "archive_sha256": maintenance.digest_file(archive_path),
            "console_contract": self.console,
        }))
        self.calls = []
        self.schema = maintenance.SCHEMA_CONTRACT
        self.endpoint = "unix:///tmp/docker.sock"
        self.addCleanup(patch.stopall)
        patch.dict(maintenance.os.environ, {"DOCKER_HOST": "", "DOCKER_CONTEXT": ""}).start()
        patch.object(maintenance, "run", side_effect=self.run_command).start()

    def run_command(self, *arguments):
        self.calls.append(arguments)
        if arguments[0] == "git":
            return "2" * 40
        if arguments[1] == "context":
            return self.endpoint
        if arguments[1] == "image":
            return "loaded"
        self.assertEqual(arguments[:8], (
            "docker", "run", "--rm", "--pull", "never", "--network", "none", "--entrypoint",
        ))
        self.assertEqual(arguments[9], self.image_id)
        if arguments[8] == "/kkai-migrate":
            self.assertEqual(arguments[10:], ("--describe-contract", "--dialect", "postgres", "--json"))
            return json.dumps(self.schema)
        self.assertEqual(arguments[8], "/new-api")
        self.assertEqual(arguments[10:], ("--describe-console-contract",))
        return json.dumps(self.console)

    def plan(self):
        return maintenance.release_plan(self.metadata, "3" * 40, "rc41-maintenance-v1")

    def test_binds_one_image_to_archive_source_and_both_binary_contracts(self):
        plan = self.plan()
        self.assertEqual(plan["image_id"], self.image_id)
        self.assertEqual(plan["source_tree"], "2" * 40)
        self.assertEqual(plan["schema_contract"], maintenance.SCHEMA_CONTRACT)
        self.assertEqual(plan["console_contract"], self.console)
        self.assertEqual(plan["planned_infra_sha"], "3" * 40)
        self.assertEqual(plan["metadata_sha256"], maintenance.digest_file(self.metadata))

    def test_tampered_archive_is_rejected_before_any_command(self):
        with (self.root / f"{self.version}.tar").open("ab") as archive:
            archive.write(b"changed")
        with self.assertRaisesRegex(ValueError, "checksum"):
            self.plan()
        self.assertEqual(self.calls, [])

    def test_rejects_schema_drift_and_remote_docker(self):
        self.schema = {**maintenance.SCHEMA_CONTRACT, "runtime_min_version": 8}
        with self.assertRaisesRegex(ValueError, "PostgreSQL v9"):
            self.plan()
        self.calls.clear()
        self.endpoint = "tcp://production:2376"
        with self.assertRaisesRegex(ValueError, "local Docker"):
            self.plan()
        self.assertFalse(any(call[:3] == ("docker", "image", "load") for call in self.calls))

    def test_plan_is_not_overwritten_and_verification_rejects_planned_infra_drift(self):
        arguments = ["prepare-maintenance-release.py", "create", "--metadata", str(self.metadata),
                      "--planned-infra-sha", "3" * 40,
                      "--planned-deployment-protocol", "rc41-maintenance-v1"]
        with patch.object(maintenance.sys, "argv", arguments), patch("builtins.print"):
            maintenance.main()
            with self.assertRaisesRegex(ValueError, "already exists"):
                maintenance.main()
        arguments[1] = "verify"
        with patch.object(maintenance.sys, "argv", arguments), patch("builtins.print"):
            maintenance.main()
            arguments[5] = "4" * 40
            with self.assertRaisesRegex(ValueError, "planned infrastructure"):
                maintenance.main()


if __name__ == "__main__":
    unittest.main()
