#!/usr/bin/env python3
"""Offline validation for ai-risk-guard role-aware fixture expectations."""

from __future__ import annotations

import json
import os
import re
from pathlib import Path
from typing import Any


SCRIPT_DIR = Path(__file__).resolve().parent
GUARD_DIR = SCRIPT_DIR.parent
RULES_PATH = Path(os.environ.get("RULES_PATH", GUARD_DIR / "rules" / "pre-risk-rules.json"))
FIXTURE_DIR = Path(os.environ.get("FIXTURE_DIR", SCRIPT_DIR / "fixtures"))


def endpoint_kind(path: str) -> str | None:
    if path in {"/v1/responses", "/v1beta/responses"}:
        return "responses"
    if path in {"/v1/chat/completions", "/v1beta/chat/completions"}:
        return "chat_completions"
    if path in {"/v1/completions", "/v1beta/completions"}:
        return "completions"
    return None


def append_text(parts: list[str], value: Any) -> None:
    if isinstance(value, str) and value:
        parts.append(value)


def content_part_has_user_text(part: Any) -> bool:
    if not isinstance(part, dict):
        return False
    part_type = str(part.get("type") or "").lower()
    return part_type in {"", "text", "input_text"}


def append_content_text(parts: list[str], content: Any) -> None:
    if isinstance(content, str):
        append_text(parts, content)
        return
    if isinstance(content, list):
        for part in content:
            if isinstance(part, str):
                append_text(parts, part)
            elif content_part_has_user_text(part):
                append_text(parts, part.get("text"))
                append_text(parts, part.get("input_text"))
        return
    if content_part_has_user_text(content):
        append_text(parts, content.get("text"))
        append_text(parts, content.get("input_text"))


def append_responses_input_item(parts: list[str], item: Any) -> None:
    if isinstance(item, str):
        append_text(parts, item)
        return
    if not isinstance(item, dict):
        return

    item_type = str(item.get("type") or "").lower()
    if item_type == "input_text":
        append_text(parts, item.get("text"))
        append_text(parts, item.get("input_text"))
        return

    if str(item.get("role") or "").lower() != "user":
        return

    append_content_text(parts, item.get("content"))
    if item_type in {"", "text", "message"}:
        append_text(parts, item.get("text"))
        append_text(parts, item.get("input_text"))


def responses_scan_text(payload: dict[str, Any]) -> str:
    parts: list[str] = []
    input_value = payload.get("input")
    if isinstance(input_value, list):
        for item in input_value:
            append_responses_input_item(parts, item)
    else:
        append_responses_input_item(parts, input_value)
    return "\n".join(parts)


def chat_scan_text(payload: dict[str, Any]) -> str:
    parts: list[str] = []
    for message in payload.get("messages") or []:
        if isinstance(message, dict) and str(message.get("role") or "").lower() == "user":
            append_content_text(parts, message.get("content"))
    return "\n".join(parts)


def completion_scan_text(payload: dict[str, Any]) -> str:
    parts: list[str] = []

    def append_prompt(prompt: Any) -> None:
        if isinstance(prompt, str):
            append_text(parts, prompt)
            return
        if isinstance(prompt, list):
            for item in prompt:
                append_prompt(item)

    append_prompt(payload.get("prompt"))
    return "\n".join(parts)


def json_scan_text(path: str, payload: dict[str, Any]) -> str:
    kind = endpoint_kind(path)
    if kind == "responses":
        return responses_scan_text(payload)
    if kind == "chat_completions":
        return chat_scan_text(payload)
    if kind == "completions":
        return completion_scan_text(payload)
    return json.dumps(payload, ensure_ascii=False)


def multipart_scan_text(boundary: str, body: str) -> str:
    parts: list[str] = []
    for raw_part in body.split(f"--{boundary}"):
        if not raw_part or raw_part.startswith("--"):
            continue
        raw_part = raw_part.lstrip("\r\n")
        if "\r\n\r\n" in raw_part:
            header_text, value = raw_part.split("\r\n\r\n", 1)
        elif "\n\n" in raw_part:
            header_text, value = raw_part.split("\n\n", 1)
        else:
            continue
        headers = header_text.lower()
        if "content-disposition: form-data" not in headers:
            continue
        if re.search(r"filename\s*=", headers):
            continue
        content_type = None
        match = re.search(r"(?:^|\n)content-type:\s*([^;\r\n]+)", headers)
        if match:
            content_type = match.group(1)
        if content_type and not (
            content_type.startswith("text/")
            or "json" in content_type
            or "x-www-form-urlencoded" in content_type
        ):
            continue
        append_text(parts, value.rstrip("\r\n"))
    return "\n".join(parts)


def pattern_matches(text: str, pattern: str) -> tuple[bool, re.Match[str] | None]:
    try:
        match = re.search(pattern, text)
    except re.error as exc:
        raise AssertionError(f"invalid Python-compatible rule pattern {pattern!r}: {exc}") from exc
    return match is not None, match


def score_rule(text: str, rule: dict[str, Any]) -> tuple[bool, list[dict[str, Any]], int]:
    total = 0
    matched_signals: list[dict[str, Any]] = []
    for signal in rule.get("signals") or []:
        matched, _ = pattern_matches(text, signal["pattern"])
        if matched:
            score = int(signal.get("score") or 1)
            total += score
            matched_signals.append({"id": signal.get("id"), "score": score})
    return total >= int(rule["score_threshold"]), matched_signals, total


def rule_matches(text: str, rule: dict[str, Any]) -> bool:
    if rule.get("pattern"):
        matched, _ = pattern_matches(text, rule["pattern"])
        return matched
    if rule.get("score_threshold"):
        matched, _, _ = score_rule(text, rule)
        return matched
    for pattern in rule.get("all") or []:
        matched, _ = pattern_matches(text, pattern)
        if not matched:
            return False
    any_patterns = rule.get("any") or []
    if not any_patterns:
        return True
    return any(pattern_matches(text, pattern)[0] for pattern in any_patterns)


def find_rule_hit(text: str, rules_doc: dict[str, Any]) -> dict[str, Any] | None:
    for rule in rules_doc.get("rules") or []:
        if rule.get("enabled") is False:
            continue
        if rule_matches(text, rule):
            return rule
    return None


def enforcement(rule: dict[str, Any] | None) -> str | None:
    if not rule:
        return None
    return str(rule.get("enforcement") or "block")


def load_fixture(name: str) -> dict[str, Any]:
    with (FIXTURE_DIR / name).open(encoding="utf-8") as fixture:
        return json.load(fixture)


def assert_case(
    rules_doc: dict[str, Any],
    label: str,
    path: str,
    fixture: str,
    expect_block: bool,
    expect_rule: str | None = None,
    expect_observe: bool = False,
) -> None:
    scan_text = json_scan_text(path, load_fixture(fixture))
    hit = find_rule_hit(scan_text, rules_doc)
    action = enforcement(hit)
    blocked = action == "block"

    if expect_block and not blocked:
        raise AssertionError(f"{label}: expected block, got rule={hit and hit.get('case_id')} enforcement={action}")
    if not expect_block and blocked:
        raise AssertionError(f"{label}: expected no block, got blocking rule={hit and hit.get('case_id')}")
    if expect_rule and (not hit or hit.get("case_id") != expect_rule):
        raise AssertionError(f"{label}: expected rule {expect_rule}, got {hit and hit.get('case_id')}")
    if expect_observe and action != "observe":
        raise AssertionError(f"{label}: expected observe-only hit, got rule={hit and hit.get('case_id')} enforcement={action}")

    print(f"PASS: {label} -> rule={hit and hit.get('case_id')} enforcement={action}")


def assert_multipart_file_is_ignored(rules_doc: dict[str, Any]) -> None:
    boundary = "----ai-risk-guard-test-boundary"
    file_text = (FIXTURE_DIR / "false-positive-multipart-file-pwn.txt").read_text(encoding="utf-8")
    body = (
        f"--{boundary}\r\n"
        'Content-Disposition: form-data; name="prompt"\r\n\r\n'
        "Please summarize this harmless uploaded fixture.\r\n"
        f"--{boundary}\r\n"
        'Content-Disposition: form-data; name="file"; filename="pwn.txt"\r\n'
        "Content-Type: application/octet-stream\r\n\r\n"
        f"{file_text}\r\n"
        f"--{boundary}--\r\n"
    )
    scan_text = multipart_scan_text(boundary, body)
    hit = find_rule_hit(scan_text, rules_doc)
    if enforcement(hit) == "block":
        raise AssertionError(f"multipart file content should be ignored, got blocking rule={hit and hit.get('case_id')}")
    if "tcache poisoning" in scan_text or "AnantaCracker" in scan_text:
        raise AssertionError("multipart scan text included uploaded file content")
    print(f"PASS: multipart file content ignored -> rule={hit and hit.get('case_id')} enforcement={enforcement(hit)}")


def main() -> None:
    rules_doc = json.loads(RULES_PATH.read_text(encoding="utf-8"))
    if not str(rules_doc.get("version", "")).startswith("2026-05-18.role-aware.v3"):
        raise AssertionError(f"unexpected rules version: {rules_doc.get('version')}")

    assert_case(
        rules_doc,
        "responses ignores instructions/system/developer/assistant/tool/patch output",
        "/v1/responses",
        "false-positive-responses-system-tool-output.json",
        expect_block=False,
    )
    assert_case(
        rules_doc,
        "chat completions scans user messages only",
        "/v1/chat/completions",
        "false-positive-chat-non-user-history.json",
        expect_block=False,
    )
    assert_case(
        rules_doc,
        "single generic term is observe-only",
        "/v1/responses",
        "false-positive-generic-single-term.json",
        expect_block=False,
        expect_rule="generic_single_security_term_observe",
        expect_observe=True,
    )
    assert_multipart_file_is_ignored(rules_doc)
    assert_case(
        rules_doc,
        "AnantaCracker/DumpedLua/tolua action chain blocks",
        "/v1/responses",
        "high-risk-ananta-cracker.json",
        expect_block=True,
        expect_rule="game_reverse_named_tooling_action_chain",
    )
    assert_case(
        rules_doc,
        "frida/xposed/zygisk hook dump login/role chain blocks",
        "/v1/responses",
        "high-risk-game-frida-zygisk.json",
        expect_block=True,
        expect_rule="game_reverse_named_tooling_action_chain",
    )
    assert_case(
        rules_doc,
        "tcache poisoning __free_hook chain blocks",
        "/v1/responses",
        "high-risk-pwn-tcache-free-hook.json",
        expect_block=True,
        expect_rule="pwn_tcache_free_hook_chain",
    )
    assert_case(
        rules_doc,
        "chat ROP open read write chain blocks",
        "/v1/chat/completions",
        "high-risk-pwn-rop-flag-chat.json",
        expect_block=True,
        expect_rule="pwn_rop_flag_open_read_write_chain",
    )
    assert_case(
        rules_doc,
        "completions prompt ROP open read write chain blocks",
        "/v1/completions",
        "high-risk-completions-rop-flag.json",
        expect_block=True,
        expect_rule="pwn_rop_flag_open_read_write_chain",
    )


if __name__ == "__main__":
    main()
