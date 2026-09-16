import io
import json
from pathlib import Path
import runpy
import tarfile
import tempfile
import unittest

verify = runpy.run_path(str(Path(__file__).with_name("verify-release-console-contract.py")))["verify"]


class ConsoleArchiveTests(unittest.TestCase):
    def test_metadata_cannot_override_immutable_image_contract(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            contract = {"format_version": 1, "api_contracts": [1], "capabilities": []}
            with tarfile.open(root / "release.tar", "w") as archive:
                for name, document in {
                    "manifest.json": [{"Config": "config.json", "RepoTags": ["new-api:test"]}],
                    "config.json": {"config": {"Labels": {"io.kkrich.console-contract": json.dumps(contract)}}},
                }.items():
                    content = json.dumps(document).encode()
                    member = tarfile.TarInfo(name)
                    member.size = len(content)
                    archive.addfile(member, io.BytesIO(content))
            metadata = root / "release.json"
            document = {"archive": "release.tar", "image_tag": "new-api:test", "console_contract": contract}
            metadata.write_text(json.dumps(document))
            verify(metadata)
            document["console_contract"] = {**contract, "api_contracts": [2]}
            metadata.write_text(json.dumps(document))
            with self.assertRaisesRegex(ValueError, "contracts differ"):
                verify(metadata)


if __name__ == "__main__":
    unittest.main()
