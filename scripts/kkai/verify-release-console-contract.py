#!/usr/bin/env python3
"""Cross-check release metadata against its immutable Docker archive label."""

import json
from pathlib import Path
import sys
import tarfile


def verify(metadata_path: Path) -> None:
    metadata = json.loads(metadata_path.read_text())
    expected = metadata.get("console_contract")
    if expected is None:
        return  # Historical metadata retains its legacy deployment gates.
    if not isinstance(expected, dict) or expected.get("format_version") != 1:
        raise ValueError("invalid console contract in release metadata")
    archive_path = metadata_path.parent / metadata["archive"]
    with tarfile.open(archive_path, "r:*") as archive:
        manifest = archive.extractfile("manifest.json")
        if manifest is None:
            raise ValueError("Docker manifest is missing")
        with manifest:
            entries = json.load(manifest)
        selected = [entry for entry in entries if metadata["image_tag"] in entry.get("RepoTags", [])]
        if len(selected) != 1:
            raise ValueError("Docker archive does not contain one exact release tag")
        config = archive.extractfile(selected[0]["Config"])
        if config is None:
            raise ValueError("Docker image configuration is missing")
        with config:
            labels = json.load(config)["config"]["Labels"]
        if json.loads(labels["io.kkrich.console-contract"]) != expected:
            raise ValueError("release metadata and image console contracts differ")


if __name__ == "__main__":
    try:
        verify(Path(sys.argv[1]))
    except (OSError, ValueError, KeyError, TypeError, tarfile.TarError) as exc:
        sys.exit(f"verify-release-console-contract: {exc}")
