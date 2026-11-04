"""Capture actual Sub2 recovery writer output without modifying its checkout."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--sub2", type=Path, required=True, help="Sub2 checkout root")
    args = parser.parse_args()
    fixtures = Path(__file__).resolve().parent
    backend = args.sub2.resolve() / "backend"
    sources = (
        "internal/service/openai_recovery_stream_writer.go",
        "internal/service/openai_recovery_structured.go",
        "internal/service/openai_retry_budget.go",
    )
    before = {name: hashlib.sha256((backend / name).read_bytes()).hexdigest() for name in sources}
    with tempfile.TemporaryDirectory(prefix="newapi-recovery-capture-") as tmp:
        output = Path(tmp) / "captures"
        output.mkdir()
        overlay = Path(tmp) / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {
            str(backend / "internal/service/zz_newapi_capture_test.go"): str(fixtures / "capture_test.go.txt")
        }}))
        subprocess.run(
            ["go", "test", "-overlay", str(overlay), "./internal/service",
             "-run", "^TestCaptureNewAPIRecoveryFixtures$", "-count=1", "-timeout=120s"],
            cwd=backend, env={**os.environ, "KKAI_CAPTURE_DIR": str(output)}, check=True,
        )
        captured = {p.name: p.read_bytes() for p in sorted(output.glob("*.sse"))}
    after = {name: hashlib.sha256((backend / name).read_bytes()).hexdigest() for name in sources}
    if before != after:
        raise RuntimeError("Sub2 sources changed during capture; rerun on stable sources")
    manifest = {"producer": "Sub2 OpenAIRecoveryStreamWriter", "source_sha256": after,
                "fixture_sha256": {name: hashlib.sha256(data).hexdigest()
                                   for name, data in captured.items()}}
    for name, data in captured.items():
        (fixtures / name).write_bytes(data)
    (fixtures / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")


if __name__ == "__main__":
    main()
