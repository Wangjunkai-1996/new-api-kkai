#!/usr/bin/env python3
"""Validate daemon evidence never persists raw client tokens."""

from __future__ import annotations

import json
import runpy
import tempfile
from pathlib import Path
from typing import Any


SCRIPT_DIR = Path(__file__).resolve().parent
GUARD_DIR = SCRIPT_DIR.parent
DAEMON_PATH = GUARD_DIR / "bin" / "ai-risk-guardd"
LUA_PATH = GUARD_DIR / "nginx" / "ai_risk_guard_access.lua"


def assert_not_contains(path: Path, secret: str) -> None:
    text = path.read_text(encoding="utf-8")
    if secret in text:
        raise AssertionError(f"{path} persisted raw token material")


class FakePg:
    def __init__(self, token_owner_user_id: int, user_role: int = 1) -> None:
        self.token_owner_user_id = token_owner_user_id
        self.user_role = user_role
        self.token_key = "db-owner-token-secret"
        self.queries: list[str] = []

    def query(self, sql: str) -> str:
        self.queries.append(sql)
        if sql.startswith("SELECT id,user_id,status,key FROM tokens WHERE id=42"):
            return f"42|{self.token_owner_user_id}|1|{self.token_key}"
        if sql.startswith(f"SELECT id,status,role FROM users WHERE id={self.token_owner_user_id}"):
            return f"{self.token_owner_user_id}|1|{self.user_role}"
        if sql.startswith("UPDATE tokens SET status=2 WHERE id=42"):
            return ""
        if sql.startswith(f"UPDATE users SET status=2 WHERE id={self.token_owner_user_id}"):
            return ""
        raise AssertionError(f"unexpected query: {sql}")


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]


def main() -> None:
    lua_text = LUA_PATH.read_text(encoding="utf-8")
    if "token = token" in lua_text:
        raise AssertionError("Lua event payload still writes raw token")

    daemon = runpy.run_path(str(DAEMON_PATH))
    secret = "sk-test-redaction-token-12345678"
    nested_secret = "Bearer sk-nested-redaction-token-abcdef1234567890"
    query_secret = "sk-query-redaction-token-abcdef1234567890"
    opaque_secret = "abc1234567890abcdefABCDEF1234567890abcdefABCDEF"
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
            "headers": {
                "Authorization": nested_secret,
                "x-trace": "request api_key=" + query_secret,
            },
            "nested": [
                {
                    "api_key_full": query_secret,
                    "message": "opaque secret " + opaque_secret,
                }
            ],
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
            assert_not_contains(path, nested_secret)
            assert_not_contains(path, query_secret)
            assert_not_contains(path, opaque_secret)

        raw_event = json.loads((case_dir / "raw_event.redacted.jsonl").read_text(encoding="utf-8").splitlines()[0])
        redacted_payload = json.loads(raw_event["raw"])
        token = redacted_payload.get("token")
        if not isinstance(token, dict) or token.get("redacted") is not True or not token.get("sha256"):
            raise AssertionError("redacted raw event did not replace token with digest metadata")
        authorization = redacted_payload.get("headers", {}).get("Authorization")
        if not isinstance(authorization, dict) or authorization.get("redacted") is not True or not authorization.get("sha256"):
            raise AssertionError("nested authorization header was not redacted")
        nested = redacted_payload.get("nested", [{}])[0]
        if not isinstance(nested.get("api_key_full"), dict) or nested["api_key_full"].get("redacted") is not True:
            raise AssertionError("nested api_key_full was not redacted")
        if "[REDACTED]" not in nested.get("message", ""):
            raise AssertionError("free-form long secret was not sanitized")

    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        state = root / "state"
        args = daemon["parse_args"](
            [
                "--events",
                str(root / "events.jsonl"),
                "--state-dir",
                str(state),
                "--black-ip-file",
                str(root / "black_ip"),
                "--bridge-black-log-file",
                "",
                "--lock-file",
                str(root / "guard.lock"),
                "--once",
            ]
        )
        guard = daemon["Guard"](args)
        guard.pg = FakePg(token_owner_user_id=9001)
        guard.setup()
        guard.process_line(
            json.dumps(
                {
                    "risk_case_id": "risk-attribution-test",
                    "enforce": True,
                    "token_id": 42,
                    "api_key_hash": daemon["md5_token_key"](guard.pg.token_key),
                    "api_key_suffix": guard.pg.token_key[-8:],
                    "user_id": 1001,
                    "rule_id": "test_rule",
                },
                sort_keys=True,
            )
            + "\n"
        )
        actions = read_jsonl(state / "actions.jsonl")
        if not any(
            action.get("action") == "attribution_mismatch"
            and action.get("result") == "event_user_id_mismatch"
            and action.get("event_user_id") == 1001
            for action in actions
        ):
            raise AssertionError("daemon did not log attribution_mismatch for event/token owner disagreement")
        if not any(
            action.get("action") == "disable_user"
            and action.get("result") == "user_disable_skipped_attribution_mismatch"
            and action.get("target", {}).get("user_id") == 9001
            for action in actions
        ):
            raise AssertionError("daemon did not log user disable skip for attribution mismatch")
        disabled_users = [
            action.get("target", {}).get("user_id")
            for action in actions
            if action.get("action") == "disable_user" and action.get("result") == "disabled"
        ]
        if disabled_users:
            raise AssertionError(f"daemon disabled user(s) despite attribution mismatch: {disabled_users}")
        if any(sql.startswith("UPDATE users SET status=2") for sql in guard.pg.queries):
            raise AssertionError("daemon updated users table despite attribution mismatch")

    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        state = root / "state"
        args = daemon["parse_args"](
            [
                "--events",
                str(root / "events.jsonl"),
                "--state-dir",
                str(state),
                "--black-ip-file",
                str(root / "black_ip"),
                "--bridge-black-log-file",
                "",
                "--lock-file",
                str(root / "guard.lock"),
                "--once",
            ]
        )
        guard = daemon["Guard"](args)
        guard.pg = FakePg(token_owner_user_id=9001)
        guard.setup()
        guard.process_line(
            json.dumps(
                {
                    "risk_case_id": "risk-token-lookup-mismatch-test",
                    "enforce": True,
                    "token_id": 42,
                    "api_key_hash": "0" * 32,
                    "api_key_suffix": "wrong999",
                    "rule_id": "test_rule",
                },
                sort_keys=True,
            )
            + "\n"
        )
        actions = read_jsonl(state / "actions.jsonl")
        if not any(
            action.get("action") == "attribution_mismatch"
            and action.get("result") == "token_lookup_mismatch"
            for action in actions
        ):
            raise AssertionError("daemon did not log token lookup attribution mismatch")
        if any(action.get("action") == "disable_user" for action in actions):
            raise AssertionError("daemon disabled a user after token lookup mismatch")
        if any(
            action.get("action") == "disable_token" and action.get("result") == "disabled"
            for action in actions
        ):
            raise AssertionError("daemon disabled a token after token lookup mismatch")
        if any(
            sql.startswith("UPDATE tokens SET status=2") or sql.startswith("UPDATE users SET status=2")
            for sql in guard.pg.queries
        ):
            raise AssertionError("daemon mutated DB after token lookup attribution mismatch")

    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        state = root / "state"
        args = daemon["parse_args"](
            [
                "--events",
                str(root / "events.jsonl"),
                "--state-dir",
                str(state),
                "--black-ip-file",
                str(root / "black_ip"),
                "--bridge-black-log-file",
                "",
                "--lock-file",
                str(root / "guard.lock"),
                "--once",
            ]
        )
        guard = daemon["Guard"](args)
        guard.pg = FakePg(token_owner_user_id=9001, user_role=10)
        guard.setup()
        guard.process_line(
            json.dumps(
                {
                    "risk_case_id": "risk-privileged-user-test",
                    "enforce": True,
                    "token_id": 42,
                    "api_key_hash": daemon["md5_token_key"](guard.pg.token_key),
                    "api_key_suffix": guard.pg.token_key[-8:],
                    "user_id": 9001,
                    "rule_id": "test_rule",
                },
                sort_keys=True,
            )
            + "\n"
        )
        actions = read_jsonl(state / "actions.jsonl")
        if not any(
            action.get("action") == "disable_user"
            and action.get("result") == "user_disable_skipped_privileged"
            and action.get("previous_status") == 1
            and action.get("role") == 10
            for action in actions
        ):
            raise AssertionError("daemon did not skip privileged user disable with status/role evidence")
        if any(sql.startswith("UPDATE users SET status=2") for sql in guard.pg.queries):
            raise AssertionError("daemon updated users table for privileged user")

    print("PASS: daemon evidence redacts raw token material and enforces safe token/user attribution")


if __name__ == "__main__":
    main()
