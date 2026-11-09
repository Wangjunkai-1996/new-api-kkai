#!/usr/bin/env python3
"""Run on sys1 after a committed membership reconciliation operation."""

import hashlib
import hmac
import json
import re
import socket
import subprocess
import sys
from pathlib import Path


def docker_inspect(name):
    return json.loads(subprocess.check_output(["docker", "inspect", name], text=True))[0]


def redis_command(reader, sock, *values):
    parts = [str(value).encode() for value in values]
    payload = f"*{len(parts)}\r\n".encode()
    for part in parts:
        payload += f"${len(part)}\r\n".encode() + part + b"\r\n"
    sock.sendall(payload)
    reply = reader.readline().rstrip(b"\r\n")
    if not reply or reply[:1] not in (b"+", b":"):
        raise RuntimeError(f"Redis command failed: {reply[:80]!r}")
    return reply[1:]


def main(operation_id, expected_rows):
    if not re.fullmatch(r"[a-zA-Z0-9_-]{1,128}", operation_id):
        raise ValueError("invalid operation ID")
    sql = (
        "SELECT b.user_id, coalesce(t.key, '') "
        "FROM kkai_membership_billing b "
        "LEFT JOIN tokens t ON t.id=b.token_id AND t.user_id=b.user_id "
        "WHERE b.state IN ('released','settled') "
        "AND b.review_resolution LIKE "
        f"'%{operation_id}%' ORDER BY b.user_id, b.request_id"
    )
    output = subprocess.check_output(
        ["docker", "exec", "--user", "999:999", "kkai-postgres", "psql",
         "--username=postgres", "--dbname=newapi", "--no-psqlrc", "--tuples-only",
         "--no-align", "--field-separator=\t", "--set=ON_ERROR_STOP=1", "--command", sql],
        text=True,
    )
    rows = [line.split("\t", 1) for line in output.splitlines()]
    if len(rows) != expected_rows or any(len(row) != 2 for row in rows):
        raise RuntimeError(f"expected {expected_rows} resolved rows, found {len(rows)}")

    app = docker_inspect("kkai-newapi-writer")
    redis = docker_inspect("kkai-newapi-redis")
    env = dict(entry.split("=", 1) for entry in app["Config"]["Env"] if "=" in entry)
    mounts = {item["Destination"]: item["Source"] for item in app["Mounts"]}

    def secret(name):
        return Path(mounts[env[name]]).read_text().removesuffix("\n").removesuffix("\r")

    crypto_secret = secret("NEWAPI_CRYPTO_SECRET_FILE").encode()
    redis_password = secret("NEWAPI_REDIS_PASSWORD_FILE")
    common_networks = set(app["NetworkSettings"]["Networks"]) & set(redis["NetworkSettings"]["Networks"])
    if len(common_networks) != 1:
        raise RuntimeError("NewAPI and Redis network identity is ambiguous")
    redis_ip = redis["NetworkSettings"]["Networks"][common_networks.pop()]["IPAddress"]
    users = {row[0] for row in rows}
    tokens = {row[1] for row in rows if row[1]}

    with socket.create_connection((redis_ip, int(env["NEWAPI_REDIS_PORT"])), timeout=8) as sock:
        reader = sock.makefile("rb")
        redis_command(reader, sock, "AUTH", env["NEWAPI_REDIS_USER"], redis_password)
        redis_command(reader, sock, "SELECT", env["NEWAPI_REDIS_DATABASE"])
        for token in sorted(tokens):
            digest = hmac.new(crypto_secret, token.encode(), hashlib.sha256).hexdigest()
            redis_command(reader, sock, "SET", f"token:fence:{digest}", "1", "EX", "10")
            redis_command(reader, sock, "DEL", f"token:{digest}", f"token:generation:{digest}")
        for user_id in sorted(users):
            redis_command(reader, sock, "DEL", f"user:{user_id}")
    print(f"cache invalidated: operation={operation_id} rows={len(rows)} users={len(users)} tokens={len(tokens)}")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit("usage: invalidate...py OPERATION_ID EXPECTED_ROWS")
    main(sys.argv[1], int(sys.argv[2]))
