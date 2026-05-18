# ai-risk-guard validation tests

These scripts validate Nginx Lua risk-guard behavior and daemon follow-up state against a local or staging endpoint. They default to `http://127.0.0.1` and do not target production unless the caller explicitly sets `BASE_URL`.

## Nginx Lua guard checks

Offline fixture and rule-shape checks:

```bash
./ops/ai-risk-guard/tests/validate-role-aware-fixtures.py
```

Live endpoint checks:

```bash
BASE_URL=http://127.0.0.1 ./ops/ai-risk-guard/tests/validate-risk-guard.sh
```

Validated behavior:

- `GET /v1/models` with a normal key/IP is not locally blocked.
- `POST /v1/responses` with a normal body is not locally blocked.
- A harmless `_RPC5...` random-string regression sample is not locally blocked.
- `/v1/responses` ignores high-risk text in `instructions`, system/developer
  messages, assistant history, tool output, and patch output when the current
  user input is harmless.
- `/v1/chat/completions` scans user messages only and ignores high-risk
  non-user history.
- `multipart/form-data` scans text fields only and ignores high-risk uploaded
  file content.
- Single generic technical terms are observe-only and do not locally block.
- A harmless request body larger than the scan limit is logged as evidence only and is not locally blocked.
- The high-risk sample containing `AnantaCracker.dll`, `DumpedLua`, `tolua.dll`, `去水印`, and `反截图` returns HTTP 403.
- High-risk game runtime samples combining `frida`/`xposed`/`zygisk` with
  `hook`/`dump`/`绕过`/`登录`/`创角` return HTTP 403.
- High-risk pwn samples for `tcache` poisoning plus `__free_hook`, and ROP
  `open("./flag")` -> `read` -> `write` chains return HTTP 403 on the
  role-aware endpoint that carries the user text.
- Reusing the same IP/key after the high-risk request returns HTTP 403.

Useful environment variables:

- `BASE_URL`: target base URL, default `http://127.0.0.1`.
- `NORMAL_API_KEY`: bearer token for normal allow checks.
- `RISK_API_KEY`: bearer token for the high-risk and repeat-block checks.
- `CLIENT_IP`: `X-Forwarded-For` value for the high-risk/repeat checks.
- `LOCAL_403_MARKER`: optional body marker required on local 403 responses.
- `CASE_ID_FILE`: file where a detected risk case id is stored for daemon validation.

## Daemon follow-up checks

```bash
CASE_ID=risk-case-id \
TOKEN_ID=token-id \
USER_ID=user-id \
./ops/ai-risk-guard/tests/validate-daemon.sh
```

Validated behavior:

- `riskctl case show` succeeds for the generated case.
- The affected token shows `status=2` after daemon processing.
- The affected user shows `status=2` after daemon processing.

`validate-daemon.sh` passes values as argv and validates inputs before use:
`TOKEN_ID` and `USER_ID` must be positive integers, and `CASE_ID` must use only
safe case-id characters. It no longer accepts shell command templates.

Useful environment variables:

- `RISKCTL`: path to `riskctl`, default `ops/ai-risk-guard/bin/riskctl`.
- `STATE_DIR`: daemon state directory, default `/var/lib/ai-risk-guard`.
- `DB_CONTAINER`, `DB_USER`, `DB_NAME`: read-only status query targets.
