#!/usr/bin/env python3
"""Local timing evidence, never an authority for advancing a release."""

import argparse
import datetime as dt
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import tempfile
import time


IDENTITY_KEYS = {
    "profile", "builder", "cpu_quota", "memory_limit", "go_parallelism",
    "media_parallelism", "frontend_mode", "schema_contract", "archive_sha256",
    "archive_bytes", "bun_version", "lockfile_sha256", "theme", "frontend_format",
    "infra_sha", "deployment_protocol", "target_slot", "rollout_id", "compression",
    "ssh_multiplex",
}
LABEL = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}\Z")
VALUE = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9.:_+/-]{0,191}\Z")


def utc_now():
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def create_record(directory, kind, release_id, source_sha):
    if not LABEL.fullmatch(kind) or not LABEL.fullmatch(release_id):
        raise ValueError("invalid record kind or release ID")
    if not re.fullmatch(r"[0-9a-f]{40}", source_sha):
        raise ValueError("invalid source SHA")
    fd, name = tempfile.mkstemp(prefix=f"{release_id}.{kind}-", suffix=".json", dir=directory)
    record = {
        "format_version": 1, "kind": kind, "release_id": release_id,
        "source_sha": source_sha, "identity": {}, "started_at": utc_now(),
        "finished_at": None, "result": "running", "exit_code": None, "phases": [],
    }
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        json.dump(record, stream, indent=2)
        stream.write("\n")
    return Path(name), record


def read_record(path):
    # Each invocation owns a separate record; no shared journal or remote state machine.
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, encoding="utf-8") as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077:
            raise ValueError("record must be a private regular file (0600)")
        record = json.load(stream)
    if record.get("format_version") != 1 or record.get("result") != "running":
        raise ValueError("record is unsupported or already finished")
    return record


def save_record(path, record):
    if path.is_symlink():
        raise ValueError("refusing a symlink record")
    fd, temporary = tempfile.mkstemp(prefix=".release-record-", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(record, stream, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def close_phase(record, exit_code):
    if not record["phases"] or record["phases"][-1]["result"] != "running":
        return
    phase = record["phases"][-1]
    elapsed = time.monotonic_ns() - phase.pop("started_monotonic_ns")
    phase.update(
        finished_at=utc_now(), duration_seconds=round(elapsed / 1e9, 3) if elapsed >= 0 else None,
        result="passed" if exit_code == 0 else "failed", exit_code=exit_code,
    )


def start_phase(record, name):
    if not LABEL.fullmatch(name):
        raise ValueError("invalid phase name")
    close_phase(record, 0)
    record["phases"].append({
        "name": name, "started_at": utc_now(), "started_monotonic_ns": time.monotonic_ns(),
        "finished_at": None, "duration_seconds": None, "result": "running", "exit_code": None,
    })


def finish_record(record, exit_code):
    close_phase(record, exit_code)
    record.update(finished_at=utc_now(), result="passed" if exit_code == 0 else "failed", exit_code=exit_code)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="action", required=True)
    init = commands.add_parser("init")
    for name in ("directory", "kind", "release-id", "source-sha"):
        init.add_argument(f"--{name}", required=True)
    for action in ("identity", "phase", "phase-end", "finish"):
        command = commands.add_parser(action)
        command.add_argument("record", type=Path)
        if action == "identity":
            command.add_argument("key", choices=sorted(IDENTITY_KEYS))
            command.add_argument("value")
        elif action == "phase":
            command.add_argument("name")
        elif action == "finish":
            command.add_argument("exit_code", type=int, choices=range(256), metavar="EXIT_CODE")
    run = commands.add_parser("run", help="time one existing manual command; no command arguments are stored")
    run.add_argument("--metadata", required=True, type=Path)
    run.add_argument("--slot", choices=("blue", "green"))
    run.add_argument("--rollout-id")
    run.add_argument("--phase", required=True, choices=(
        "candidate-acceptance", "canary-10", "canary-50", "canary-100", "handoff-ready",
        "promote", "status-immediate", "status-delayed", "error-window", "frontend-prepare",
        "frontend-activate", "public-acceptance",
    ))
    run.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if args.action == "init":
        path, _ = create_record(args.directory, args.kind, args.release_id, args.source_sha)
        print(path)
        return 0
    if args.action == "run":
        command = args.command[1:] if args.command[:1] == ["--"] else args.command
        if not command:
            raise ValueError("run requires an existing command after --")
        if bool(args.slot) != bool(args.rollout_id) or (args.rollout_id and not LABEL.fullmatch(args.rollout_id)):
            raise ValueError("supply both valid slot and rollout ID")
        metadata = json.loads(args.metadata.read_text(encoding="utf-8"))
        release_id = metadata.get("version") or metadata.get("release_id")
        source_sha = metadata.get("source_sha")
        if not isinstance(release_id, str) or not isinstance(source_sha, str):
            raise ValueError("metadata is missing release/source identity")
        path, record = create_record(args.metadata.resolve().parent, "manual-check", release_id, source_sha)
        # Store only fixed identity fields; credentials/arguments/output never enter the record.
        digest = metadata.get("archive_sha256")
        if isinstance(digest, str) and re.fullmatch(r"[0-9a-f]{64}", digest):
            record["identity"]["archive_sha256"] = digest
        if args.slot:
            record["identity"].update(target_slot=args.slot, rollout_id=args.rollout_id)
        print(f"RELEASE_RECORD={path}", file=sys.stderr)
        start_phase(record, args.phase)
        save_record(path, record)
        process = None
        interrupted = 0

        def forward_signal(number, _frame):
            nonlocal interrupted
            interrupted = number
            if process is not None and process.poll() is None:
                try:
                    process.send_signal(number)
                except ProcessLookupError:
                    pass

        previous_handlers = {number: signal.signal(number, forward_signal)
                             for number in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)}
        try:
            process = subprocess.Popen(command)
            if interrupted:
                forward_signal(interrupted, None)
            status = process.wait()
            status = 128 + interrupted if interrupted else (status if status >= 0 else 128 - status)
        except OSError:
            status = 127
        finally:
            for number, handler in previous_handlers.items():
                signal.signal(number, handler)
        finish_record(record, status)
        try:
            save_record(path, record)
        except (OSError, ValueError):
            print("release-record: final record unavailable; inspect official controller state before recovery", file=sys.stderr)
        return status
    record = read_record(args.record)
    if args.action == "identity":
        if not VALUE.fullmatch(args.value):
            raise ValueError("invalid identity value")
        record["identity"][args.key] = args.value
    elif args.action == "phase":
        start_phase(record, args.name)
    elif args.action == "phase-end":
        close_phase(record, 0)
    else:
        finish_record(record, args.exit_code)
    save_record(args.record, record)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, TypeError):
        # Never echo malformed content, filesystem paths or command arguments.
        print("release-record: cannot write/read valid local evidence", file=sys.stderr)
        sys.exit(1)
