#!/usr/bin/env python3
"""Bind one local release archive for maintenance; never stage or migrate it."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile

ROOT = Path(__file__).resolve().parents[2]
PG_V9_DIGEST = "sha256:cc1fd8e06a943a01531257e66929183734bdb1377f2584f0f57ab66f7c991034"
SCHEMA_CONTRACT = {
    "runtime_min_version": 9,
    "runtime_max_version": 9,
    "migration_target_version": 9,
    "migration_kind": "none",
    "migration_set_digest": PG_V9_DIGEST,
    "compatible_prefixes": {"9": PG_V9_DIGEST},
    "schema_management": "external",
}
REQUIRED_CAPABILITIES = {
    "dashboard_jwt", "scoped_access_tokens", "task_plugins", "studio_media_urls",
}


def digest_file(path):
    with path.open("rb") as stream:
        digest = hashlib.sha256()
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def run(*arguments):
    return subprocess.check_output(
        arguments, text=True, env={**os.environ, "GIT_NO_LAZY_FETCH": "1"}
    ).strip()


def archive_config(path, tag):
    with tarfile.open(path, "r:*") as archive:
        def read(name):
            members = [member for member in archive.getmembers() if member.name == name]
            if len(members) != 1 or not members[0].isfile() or members[0].size > 4 * 1024 * 1024:
                raise ValueError("archive configuration is missing, duplicated or invalid")
            with archive.extractfile(members[0]) as stream:
                return stream.read()

        entries = json.loads(read("manifest.json"))
        if len(entries) != 1 or entries[0].get("RepoTags") != [tag]:
            raise ValueError("archive must contain only the exact release image")
        raw = read(entries[0]["Config"])
        return "sha256:" + hashlib.sha256(raw).hexdigest(), json.loads(raw)


def release_plan(metadata_path, planned_infra_sha, planned_protocol):
    if not re.fullmatch(r"[0-9a-f]{40}", planned_infra_sha):
        raise ValueError("invalid planned infrastructure SHA")
    if not re.fullmatch(r"[a-z][a-z0-9-]{2,63}", planned_protocol):
        raise ValueError("invalid planned deployment protocol")
    if metadata_path.is_symlink() or not metadata_path.is_file():
        raise ValueError("metadata must be a regular local file")
    metadata = json.loads(metadata_path.read_text())
    source = metadata["source_sha"]
    version = metadata["version"]
    if not re.fullmatch(r"[0-9a-f]{40}", source) or not re.fullmatch(
        rf"kkai-prod-[0-9]{{8}}\.[1-9][0-9]*-{source[:9]}", version
    ):
        raise ValueError("release version and source do not match")
    tag = f"kkai-newapi-manual:{version}"
    if (metadata.get("release_purpose") != "maintenance-preparation"
            or metadata.get("schema_contract") != "feature"
            or metadata.get("frontend_mode") != "external"
            or metadata.get("platform") != "linux/amd64"
            or metadata.get("image_tag") != tag):
        raise ValueError("maintenance preparation requires an external feature release for linux/amd64")
    archive_name = metadata["archive"]
    if archive_name != f"{version}.tar":
        raise ValueError("archive name does not match the release")
    archive_path = metadata_path.parent / archive_name
    if archive_path.is_symlink() or not archive_path.is_file():
        raise ValueError("archive must be a regular local file")
    archive_digest = digest_file(archive_path)
    if archive_digest != metadata["archive_sha256"]:
        raise ValueError("archive checksum mismatch")
    image_id, config = archive_config(archive_path, tag)
    image = config["config"]
    labels = image["Labels"]
    if (config.get("architecture") != "amd64" or config.get("os") != "linux"
            or image.get("User") != "10007:10007"
            or image.get("Entrypoint") != ["/new-api-entrypoint"]
            or [value for value in image.get("Env", []) if value.startswith("FRONTEND_MODE=")] != ["FRONTEND_MODE=external"]
            or labels.get("org.opencontainers.image.revision") != source
            or labels.get("org.opencontainers.image.version") != version
            or labels.get("io.kkrich.schema-contract") != "feature"
            or labels.get("io.kkrich.frontend-mode") != "external"):
        raise ValueError("archive image identity does not match release metadata")
    console = json.loads(labels["io.kkrich.console-contract"])
    if (console != metadata.get("console_contract") or console.get("format_version") != 1
            or console.get("api_contracts") != [2]
            or not REQUIRED_CAPABILITIES.issubset(console.get("capabilities", []))):
        raise ValueError("maintenance release requires the exact console 2 declaration")
    source_tree = run("git", "-C", str(ROOT), "rev-parse", f"{source}^{{tree}}")
    if not re.fullmatch(r"[0-9a-f]{40}", source_tree):
        raise ValueError("source tree is unavailable")
    endpoint = os.environ.get("DOCKER_HOST", "") if not os.environ.get("DOCKER_CONTEXT") else ""
    if not endpoint:
        endpoint = run("docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
    if not endpoint.startswith(("unix:///", "npipe:///")):
        raise ValueError("maintenance preparation requires a local Docker endpoint")
    # Load the already built archive, then probe only its immutable ID. These
    # commands have no network, credentials, production mounts or application DB.
    run("docker", "image", "load", "--input", str(archive_path))
    schema = json.loads(run(
        "docker", "run", "--rm", "--pull", "never", "--network", "none",
        "--entrypoint", "/kkai-migrate", image_id,
        "--describe-contract", "--dialect", "postgres", "--json",
    ))
    if schema != SCHEMA_CONTRACT:
        raise ValueError("binary schema contract does not match the reviewed PostgreSQL v9 contract")
    binary_console = json.loads(run(
        "docker", "run", "--rm", "--pull", "never", "--network", "none",
        "--entrypoint", "/new-api", image_id, "--describe-console-contract",
    ))
    if binary_console != console:
        raise ValueError("binary console contract differs from the archive label")
    return {
        "format_version": 1,
        "purpose": "rc41-maintenance-preparation",
        "metadata": metadata_path.name,
        "metadata_sha256": digest_file(metadata_path),
        "archive": archive_name,
        "archive_sha256": archive_digest,
        "version": version,
        "source_sha": source,
        "source_tree": source_tree,
        "image_tag": tag,
        "image_id": image_id,
        "platform": "linux/amd64",
        "frontend_mode": "external",
        "schema_contract": schema,
        "console_contract": console,
        "planned_infra_sha": planned_infra_sha,
        "planned_deployment_protocol": planned_protocol,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("create", "verify"))
    parser.add_argument("--metadata", type=Path, required=True)
    parser.add_argument("--planned-infra-sha", required=True)
    parser.add_argument("--planned-deployment-protocol", required=True)
    args = parser.parse_args()
    metadata_path = args.metadata.absolute()
    plan_path = metadata_path.with_suffix(".maintenance.json")
    if args.action == "create" and (plan_path.exists() or plan_path.is_symlink()):
        raise ValueError("maintenance plan already exists; verify or reuse it, never overwrite")
    expected = release_plan(metadata_path, args.planned_infra_sha, args.planned_deployment_protocol)
    if args.action == "create":
        with plan_path.open("x") as output:
            output.write(json.dumps(expected, sort_keys=True, indent=2) + "\n")
        plan_path.chmod(0o444)
    elif plan_path.is_symlink() or json.loads(plan_path.read_text()) != expected:
        raise ValueError("maintenance plan no longer matches the release or planned infrastructure")
    print(f"MAINTENANCE_RELEASE_PLAN={plan_path}")
    print(f"MAINTENANCE_RELEASE_PLAN_SHA256={digest_file(plan_path)}")
    print("MAINTENANCE_RELEASE_RESULT=prepared-only")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, tarfile.TarError, subprocess.CalledProcessError) as exc:
        sys.exit(f"prepare-maintenance-release: {exc}")
