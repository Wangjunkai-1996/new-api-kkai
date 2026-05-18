#!/usr/bin/env python3
"""Validate daemon evidence never persists raw client tokens."""

from __future__ import annotations

import json
import runpy
import tempfile
from pathlib import Path


SCRIPT_DIR = Path(__file__).resolve().parent
GUARD_DIR = SCRIPT_DIR.parent
DAEMON_PATH = GUARD_DIR / "bin" / "ai-risk-guardd"
LUA_PATH = GUARD_DIR / "nginx" / "ai_risk_guard_access.lua"


def assert_not_contains(path: Path, secret: str) -> None:
    text = path.read_text(encoding="utf-8")
    if secret in text:
        raise AssertionError(f"{path} persisted raw token material")


def main() -> None:
    lua_text = LUA_PATH.read_text(encoding="utf-8")
    if "token = token" in lua_text:
        raise AssertionError("Lua event payload still writes raw token")

    daemon = runpy.run_path(str(DAEMON_PATH))
    secret = "sk-test-redaction-token-12345678"
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        events = root / "events.jsonl"
        black_ip = root / "black_ip"
        lock = root / "guard.lock"
        state = root / "state"
        args = daemon["parse_args"](
            [
                "--events",
                str(events),
                "--state-dir",
                str(state),
                "--black-ip-file",
                str(black_ip),
                "--bridge-black-log-file",
                "",
                "--lock-file",
                str(lock),
                "--once",
                "--dry-run",
            ]
        )
        guard = daemon["Guard"](args)
        guard.setup()
        event = {
            "risk_case_id": "risk-redaction-test",
            "enforce": True,
            "remote_addr": "203.0.113.10",
            "token": secret,
            "rule_id": "test_rule",
        }
        guard.process_line(json.dumps(event, sort_keys=True) + "\n")

        case_dir = state / "cases" / "risk-redaction-test"
        for path in (
            case_dir / "raw_event.redacted.jsonl",
            case_dir / "event.redacted.json",
            state / "cases.jsonl",
            state / "actions.jsonl",
        ):
            assert_not_contains(path, secret)

        raw_event = json.loads((case_dir / "raw_event.redacted.jsonl").read_text(encoding="utf-8").splitlines()[0])
        redacted_payload = json.loads(raw_event["raw"])
        token = redacted_payload.get("token")
        if not isinstance(token, dict) or token.get("redacted") is not True or not token.get("sha256"):
            raise AssertionError("redacted raw event did not replace token with digest metadata")

    print("PASS: daemon evidence redacts raw token material")


if __name__ == "__main__":
    main()
