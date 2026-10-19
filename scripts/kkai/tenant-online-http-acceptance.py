#!/usr/bin/env python3
"""Disposable native HTTP rehearsal; never reads production credentials or hosts.

Requires a synthetic schema-11 dump, locally cached postgres:18.4-bookworm and
redis:8.6-alpine images, Go and Docker. No image build or network pull occurs.
Old source is read with git archive, never checked out. The compiler overlay
changes only the HTTP listener to 127.0.0.1, for both source versions.
"""

import argparse
import datetime
from fractions import Fraction
import hashlib
import http.client
import http.server
import json
import os
from pathlib import Path
import re
import socket
import socketserver
import subprocess
import threading
import time
import urllib.parse
import uuid


REPO = Path(__file__).resolve().parents[2]
PASSWORD = "LocalSyntheticAcceptance123"
PLATFORM = "platform.local.invalid"
FIXTURE_HASH = "9ddf138b294168c62bf8e6a2099eef0b9e1539d4b6a3d0cdc00a5d77c41b6986"
EPAY_KEY = "local-synthetic-signing-key"


def run(args, *, payload=None, cwd=None, env=None, timeout=90, check=True):
    result = subprocess.run(args, input=payload, cwd=cwd, env=env,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    if check and result.returncode:
        raise RuntimeError(f"{args[0]} failed ({result.returncode}): {result.stderr.decode()[-2000:]}")
    return result


def port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


class ModelStub(http.server.BaseHTTPRequestHandler):
    calls = []

    def do_POST(self):
        data = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        assert self.headers.get("Authorization") == "Bearer local-synthetic-model-key"
        type(self).calls.append({"path": self.path, "model": data["model"]})
        content = data["messages"][0]["content"]
        failed = content == "fail-locally"
        usage = 1 if content == "tiny-locally" else 100000
        body = {"error": {"message": "synthetic upstream failure", "type": "server_error"}} if failed else {
            "id": "chatcmpl-local", "object": "chat.completion", "created": 1,
            "model": "gpt-4o-mini", "choices": [{"index": 0, "message": {
                "role": "assistant", "content": "local synthetic response"}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": usage, "completion_tokens": 0, "total_tokens": usage}}
        raw = json.dumps(body).encode()
        self.send_response(500 if failed else 200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def log_message(self, *_):
        pass


class SMTPStub(socketserver.StreamRequestHandler):
    codes = []

    def handle(self):
        self.connection.settimeout(10)
        self.wfile.write(b"220 local synthetic SMTP\r\n")
        while line := self.rfile.readline():
            command = line.decode().strip()
            if command == "DATA":
                self.wfile.write(b"354 end with dot\r\n")
                message = b""
                while (line := self.rfile.readline()) != b".\r\n":
                    if not line:
                        return
                    message += line
                match = re.search(rb"<strong>([a-zA-Z0-9]{6})</strong>", message)
                assert match, "verification code missing from synthetic email"
                type(self).codes.append(match[1].decode())
                self.wfile.write(b"250 captured locally\r\n")
            elif command == "QUIT":
                self.wfile.write(b"221 bye\r\n")
                return
            else:
                self.wfile.write(b"250 local OK\r\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixture", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--old-ref", default="55744a4ed")
    parser.add_argument("--reuse-binaries", type=Path, help="reuse exactly hashed binaries only when all business source inputs match")
    args = parser.parse_args()
    out = args.output.resolve()
    out.mkdir(mode=0o700, parents=True, exist_ok=False)
    evidence = {"scope": "local synthetic PostgreSQL/Redis and loopback HTTP only", "passed": False,
                "acceptance_script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(), "checks": []}
    containers, processes, servers = [], {}, []
    phase_started = time.monotonic()

    def phase(name):
        nonlocal phase_started
        now = time.monotonic()
        evidence.setdefault("phase_seconds", {})[name] = round(now - phase_started, 3)
        phase_started = now
        print(json.dumps({"phase": name, "seconds": evidence["phase_seconds"][name]}), flush=True)

    def check(name, condition):
        evidence["checks"].append({"name": name, "passed": bool(condition)})
        if not condition:
            raise AssertionError(name)

    def sql(statement):
        return run(["docker", "exec", "-i", pg_name, "psql", "-U", "postgres", "-d",
                    database, "-X", "-qAt", "-v", "ON_ERROR_STOP=1"], payload=statement.encode()).stdout.decode().strip()

    def request(process, method, host, path, body=None, token="", cookie="", record=True):
        headers = {"Host": host, "Origin": "https://" + host,
                   "User-Agent": "KKAI-local-synthetic-transition/1"}
        payload = None if body is None else json.dumps(body).encode()
        if payload is not None:
            headers["Content-Type"] = "application/json"
        if token:
            headers["Authorization"] = "Bearer " + token
        if cookie:
            headers["Cookie"] = cookie
        connection = http.client.HTTPConnection("127.0.0.1", ports[process], timeout=10)
        try:
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            raw = response.read()
            result = json.loads(raw) if response.getheader("Content-Type", "").startswith("application/json") else raw.decode()
            cookies = response.getheaders()
            if record:
                evidence.setdefault("http", []).append({"process": process, "method": method,
                    "host": host, "path": path.split("?")[0], "status": response.status})
            return response.status, result, cookies
        finally:
            connection.close()

    def ok(process, method, host, path, body=None, token=""):
        status, result, _ = request(process, method, host, path, body, token)
        check(f"{process} {method} {host}{path}", status == 200 and isinstance(result, dict)
              and (result.get("success") is True or result.get("code") is True or result.get("message") == "success"))
        return result.get("data")

    def login(process, username, host=PLATFORM, owner=False):
        status, result, headers = request(process, "POST", host, "/api/user/login",
                                         {"username": username, "password": PASSWORD, "owner_login": owner})
        check(f"{process} login {username}@{host}", status == 200 and result.get("success") is True)
        data = result["data"]
        cookie = next(value for key, value in headers if key.lower() == "set-cookie" and value.startswith("new_api_refresh="))
        check("host-only secure refresh cookie", "Domain=" not in cookie and "Secure" in cookie and "HttpOnly" in cookie)
        return {"token": data["access_token"], "id": data["user"]["id"], "cookie": cookie.split(";", 1)[0]}

    def api_key(process, session, host=PLATFORM):
        ok(process, "POST", host, "/api/token/", {"name": "local-transition", "expired_time": -1,
           "unlimited_quota": True, "group": "default"}, session["token"])
        listing = ok(process, "GET", host, "/api/token/", token=session["token"])
        token_id = next(item["id"] for item in listing["items"] if item["name"] == "local-transition")
        return ok(process, "POST", host, f"/api/token/{token_id}/key", token=session["token"])["key"]

    def pay(process, session, host=PLATFORM, callback_process=None):
        order = ok(process, "POST", host, "/api/user/pay", {"amount": 1000, "payment_method": "alipay"}, session["token"])
        params = {"pid": "local-merchant", "out_trade_no": order["out_trade_no"], "trade_no": "local-provider-proof",
                  "type": "alipay", "name": "TUC1000", "money": order["money"], "trade_status": "TRADE_SUCCESS"}
        text = "&".join(key + "=" + params[key] for key in sorted(params)) + EPAY_KEY
        params.update(sign=hashlib.md5(text.encode()).hexdigest(), sign_type="MD5")
        invalid = {**params, "sign": "0" * 32}
        status, result, _ = request(callback_process or process, "GET", PLATFORM,
                                   "/api/user/epay/notify?" + urllib.parse.urlencode(invalid))
        check("invalid payment signature rejected", status == 200 and result == "fail")
        before = int(sql(f"SELECT quota FROM users WHERE id={session['id']};"))
        for _ in range(2):
            status, result, _ = request(callback_process or process, "GET", PLATFORM,
                                       "/api/user/epay/notify?" + urllib.parse.urlencode(params))
            check("signed payment callback accepted", status == 200 and result == "success")
        after = int(sql(f"SELECT quota FROM users WHERE id={session['id']};"))
        check("callback replay credits quota once", after - before == 500000000)
        return int(sql("SELECT id FROM top_ups WHERE trade_no='" + order["out_trade_no"] + "';"))

    def chat(process, key, host=PLATFORM, failed=False, tiny=False):
        status, result, _ = request(process, "POST", host, "/v1/chat/completions", {
            "model": "gpt-4o-mini", "max_tokens": 1, "stream": False,
            "messages": [{"role": "user", "content": "fail-locally" if failed else "tiny-locally" if tiny else "hello"}]}, key)
        check(f"{process} text relay {host} failure={failed} tiny={tiny}", status >= 400 if failed else
              status == 200 and result.get("choices", [{}])[0].get("message", {}).get("content") == "local synthetic response")

    def helper(mode):
        result = run([str(out / "helper"), mode], env={**os.environ, "KKAI_LOCAL_HTTP_DSN": dsn,
            "KKAI_LOCAL_HTTP_REDIS": f"redis://127.0.0.1:{redis_port}/0"})
        return result.stdout.decode().strip()

    def start(name, tenant=False):
        directory = out / name
        directory.mkdir(exist_ok=True)
        log = (out / (name + ".log")).open("ab")
        environment = {**base_env, "PORT": str(ports[name]), "TENANT_ROUTING_ENABLED": str(tenant).lower()}
        process = subprocess.Popen([str(out / binaries[name])], cwd=directory, env=environment, stdout=log, stderr=log)
        processes[name] = process
        for _ in range(100):
            if process.poll() is not None:
                raise RuntimeError(f"{name} exited at startup: {(out / (name + '.log')).read_text()[-3000:]}")
            try:
                if request(name, "GET", PLATFORM, "/api/status", record=False)[0] == 200:
                    return
            except (OSError, http.client.HTTPException):
                pass
            time.sleep(0.1)
        raise RuntimeError(name + " failed to become ready")

    def stop(name):
        process = processes.pop(name)
        process.terminate()
        try:
            process.wait(15)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(5)

    def availability_during(process, session, operation):
        failures, observed = [], []
        done = threading.Event()

        def poll():
            while not done.is_set():
                try:
                    status, data, _ = request(process, "GET", PLATFORM, "/api/user/self", token=session["token"], record=False)
                    observed.append(status)
                    if status != 200 or not data.get("success"):
                        failures.append(status)
                except Exception as error:
                    failures.append(type(error).__name__)
                done.wait(0.02)

        worker = threading.Thread(target=poll)
        worker.start()
        try:
            result = operation()
        finally:
            done.set()
            worker.join()
        evidence.setdefault("availability", []).append({"process": process, "requests": len(observed), "failures": failures})
        check(process + " authenticated HTTP remains available across migration", bool(observed) and not failures)
        return result

    try:
        fixture = args.fixture.resolve()
        check("known synthetic source11 fixture", hashlib.sha256(fixture.read_bytes()).hexdigest() == FIXTURE_HASH)
        endpoint = run(["docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}"] ).stdout.decode().strip()
        check("local Unix Docker context", not os.environ.get("DOCKER_HOST") and endpoint.startswith("unix:///"))
        source = out / "old-source"
        source.mkdir()
        archive = run(["git", "archive", args.old_ref], cwd=REPO).stdout
        run(["tar", "-xf", "-", "-C", str(source)], payload=archive)
        evidence["old_source_commit"] = run(["git", "rev-parse", args.old_ref], cwd=REPO).stdout.decode().strip()
        evidence["current_source_commit"] = run(["git", "rev-parse", "HEAD"], cwd=REPO).stdout.decode().strip()
        evidence["current_dirty_diff_sha256"] = hashlib.sha256(run(["git", "diff", "HEAD"], cwd=REPO).stdout).hexdigest()
        source_hash = hashlib.sha256()
        paths = run(["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=REPO).stdout.decode().split("\0")
        for path in sorted(set(paths)):
            if path.endswith(".go") or path in ["go.mod", "go.sum", "common/console_contract.json"] or path.startswith("plugins/"):
                source_hash.update(path.encode() + b"\0" + (REPO / path).read_bytes())
        evidence["business_source_sha256"] = source_hash.hexdigest()
        evidence["listener_overlay"] = "only Addr :port -> 127.0.0.1:port; business code unchanged"
        build_env = {**os.environ, "GOPROXY": "off", "GOSUMDB": "off"}
        builds = [("old-api", source, "external_frontend"), ("bridge-api", REPO, "external_frontend kkai_bridge"),
                  ("feature-api", REPO, "external_frontend")]
        prior = json.loads((args.reuse_binaries / "result.json").read_text()) if args.reuse_binaries else None
        reuse_current = prior and prior["business_source_sha256"] == evidence["business_source_sha256"]
        if prior:
            check("reused old source commit matches", prior["old_source_commit"] == evidence["old_source_commit"])
        for name, checkout, tags in builds:
            if prior and (name == "old-api" or reuse_current):
                original = args.reuse_binaries / name
                check(name + " exact cached binary hash", hashlib.sha256(original.read_bytes()).hexdigest() == prior["binaries"][name])
                (out / name).hardlink_to(original)
                evidence.setdefault("binaries", {})[name] = prior["binaries"][name]
                continue
            main_source = checkout / "main.go"
            content = main_source.read_text()
            check(name + " listener overlay matches one site", content.count('Addr:    ":" + port,') == 1)
            replacement = out / (name + "-main.go")
            replacement.write_text(content.replace('Addr:    ":" + port,', 'Addr:    "127.0.0.1:" + port,'))
            overlay = out / (name + "-overlay.json")
            overlay.write_text(json.dumps({"Replace": {str(main_source): str(replacement)}}))
            command = ["go", "build", "-overlay", str(overlay), "-tags", tags, "-ldflags",
                "-X github.com/QuantumNous/new-api/common.SchemaManagementMode=external "
                "-X github.com/QuantumNous/new-api/common.Version=local-" + name,
                "-o", str(out / name), "."]
            built = run(command, cwd=checkout, env=build_env, timeout=240)
            (out / (name + "-build.log")).write_bytes(built.stdout + built.stderr)
            evidence.setdefault("binaries", {})[name] = hashlib.sha256((out / name).read_bytes()).hexdigest()
        if reuse_current:
            original = args.reuse_binaries / "helper"
            check("helper exact cached binary hash", hashlib.sha256(original.read_bytes()).hexdigest() == prior["binaries"]["helper"])
            (out / "helper").hardlink_to(original)
        else:
            run(["go", "build", "-tags", "kkai_bridge", "-o", str(out / "helper"),
                 str(REPO / "scripts/kkai/testdata/tenant-online-http-helper.go")], cwd=REPO, env=build_env, timeout=240)
        evidence["binaries"]["helper"] = hashlib.sha256((out / "helper").read_bytes()).hexdigest()
        check("bridge contract is 11..14 target11", run([str(out / "helper"), "describe"]).stdout.strip() == b"min=11 max=14 target=11")
        phase("source_and_test_binaries")
        uid = uuid.uuid4().hex[:10]
        database = "kkai_test_http_" + uid
        pg_name, redis_name = "kkai-online-http-pg-" + uid, "kkai-online-http-redis-" + uid
        pg_port, redis_port = port(), port()
        for name, image, mapping, extra in [
            (pg_name, "postgres:18.4-bookworm", f"127.0.0.1:{pg_port}:5432", ["--tmpfs", "/var/lib/postgresql:rw", "-e", "POSTGRES_HOST_AUTH_METHOD=trust", "-e", "POSTGRES_DB=" + database]),
            (redis_name, "redis:8.6-alpine", f"127.0.0.1:{redis_port}:6379", ["--tmpfs", "/data:rw"])]:
            container = run(["docker", "run", "-d", "--pull", "never", "--name", name, "-p", mapping, *extra, image]).stdout.decode().strip()
            containers.append((container, name))
        evidence["containers"] = [{"id": container, "name": name} for container, name in containers]
        for _ in range(80):
            final_process = run(["docker", "exec", pg_name, "cat", "/proc/1/comm"], check=False).stdout.strip() == b"postgres"
            ready = run(["docker", "exec", pg_name, "psql", "-U", "postgres", "-d", database, "-X", "-qAt", "-c", "SELECT 1"], check=False)
            if final_process and ready.returncode == 0:
                break
            time.sleep(0.1)
        else:
            raise RuntimeError("local PostgreSQL failed to initialize")
        run(["docker", "exec", "-i", pg_name, "pg_restore", "-U", "postgres", "-d", database, "--exit-on-error"], payload=fixture.read_bytes())
        check("source ledger exactly 1..11", sql("SELECT string_agg(version::text,',' ORDER BY version) FROM kkai_schema_migrations;") == "1,2,3,4,5,6,7,8,9,10,11")
        dsn = f"postgresql://postgres@127.0.0.1:{pg_port}/{database}?sslmode=disable"
        model_server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), ModelStub)
        smtp_server = socketserver.ThreadingTCPServer(("127.0.0.1", 0), SMTPStub)
        for server in [model_server, smtp_server]:
            threading.Thread(target=server.serve_forever, daemon=True).start()
            servers.append(server)
        password_hash = run([str(out / "helper"), "hash"]).stdout.decode().strip()
        sql(f"UPDATE users SET password='{password_hash}', status=1, auth_version=1, display_name=username, quota=600000000, used_quota=0, \"group\"='default', role=CASE WHEN id=1 THEN 100 ELSE 1 END;")
        options = {"RegisterEnabled": "true", "PasswordRegisterEnabled": "true", "PasswordLoginEnabled": "true",
            "EmailVerificationEnabled": "true", "TurnstileCheckEnabled": "false", "SMTPServer": "127.0.0.1",
            "SMTPPort": str(smtp_server.server_address[1]), "SMTPFrom": "local@example.invalid", "SMTPAccount": "", "SMTPToken": "",
            "SMTPSSLEnabled": "false", "SMTPStartTLSEnabled": "false", "EmailDomainRestrictionEnabled": "false",
            "EmailAliasRestrictionEnabled": "false", "PayAddress": "http://127.0.0.1:1", "EpayId": "local-merchant", "EpayKey": EPAY_KEY,
            "Price": "1", "MinTopUp": "1", "ServerAddress": "https://" + PLATFORM,
            "ModelRatio": '{"gpt-4o-mini":1000}', "CompletionRatio": '{"gpt-4o-mini":1}', "GroupRatio": '{"default":1}',
            "CacheRatio": '{"gpt-4o-mini":0}', "CreateCacheRatio": '{"gpt-4o-mini":0}',
            "ImageRatio": '{"gpt-4o-mini":0}', "AudioRatio": '{"gpt-4o-mini":0}',
            "AudioCompletionRatio": '{"gpt-4o-mini":0}',
            "RetryTimes": "0", "LogConsumeEnabled": "true", "QuotaForNewUser": "0", "QuotaForInviter": "0", "QuotaForInvitee": "0",
            "payment_setting.compliance_confirmed": "true", "payment_setting.compliance_terms_version": "v1"}
        for key, value in options.items():
            sql("INSERT INTO options(\"key\",value) VALUES('" + key + "','" + value.replace("'", "''") + "') ON CONFLICT(\"key\") DO UPDATE SET value=excluded.value;")
        # The synthetic upstream has a bounded, verified aggregate usage contract.
        # Keep membership sales disabled; this config supplies the tenant budget guard.
        catalog = json.dumps({"model_limits": {"gpt-4o-mini": {"max_input_tokens": 100000,
            "max_output_tokens": 1, "max_tool_calls": 0,
            "evidence": "local stub guarantees aggregate input <=100000 and output <=1"}},
            "sales_enabled": False, "topup_discounts_disabled": True, "tiers": [], "groups": []})
        release_id = sql("INSERT INTO kkai_membership_releases(payload,actor_id,created_at) VALUES('" +
            catalog.replace("'", "''") + "',1,1) RETURNING id;")
        sql("INSERT INTO kkai_membership_catalog(id,release_id,sales_enabled,topup_discounts_disabled) VALUES(1," +
            release_id + ",false,true) ON CONFLICT(id) DO UPDATE SET release_id=excluded.release_id,sales_enabled=false,topup_discounts_disabled=true;")
        sql(f"INSERT INTO channels(id,type,key,status,name,base_url,models,\"group\",priority,weight,channel_info) VALUES(901,1,'local-synthetic-model-key',1,'local stub','http://127.0.0.1:{model_server.server_port}','gpt-4o-mini','default',0,1,'{{}}'); INSERT INTO abilities(\"group\",model,channel_id,enabled,priority,weight) VALUES('default','gpt-4o-mini',901,true,0,1);")
        # Shared Redis deliberately keeps the original namespaces for real old/new cache interoperability.
        base_env = {"PATH": os.environ["PATH"], "SQL_DSN": dsn, "REDIS_CONN_STRING": f"redis://127.0.0.1:{redis_port}/0",
            "FRONTEND_MODE": "external", "KKAI_NODE_ROLE": "serving", "NODE_TYPE": "master", "DISABLE_BACKGROUND_TASKS": "true",
            "BATCH_UPDATE_ENABLED": "false", "UPDATE_TASK": "false", "SESSION_SECRET": "local-synthetic-transition-secret-not-production",
            "SESSION_COOKIE_SECURE": "true", "SESSION_COOKIE_TRUSTED_URL": "https://" + PLATFORM,
            "TENANT_PLATFORM_HOSTS": PLATFORM, "TENANT_ROOT_DOMAIN": "tenant.invalid", "PASSWORD_LOGIN_ENCRYPTION_ENABLED": "false",
            "GLOBAL_API_RATE_LIMIT_ENABLE": "false", "GLOBAL_WEB_RATE_LIMIT_ENABLE": "false", "CRITICAL_RATE_LIMIT_ENABLE": "false",
            "GIN_MODE": "release", "MEMORY_CACHE_ENABLED": "true", "SHUTDOWN_TIMEOUT_SECONDS": "3", "RESELLER_WITHDRAWAL_FEE_MINOR": "100",
            "NO_PROXY": "*"}
        ports = {name: port() for name in ["old", "bridge", "feature"]}
        binaries = {"old": "old-api", "bridge": "bridge-api", "feature": "feature-api"}
        start("old")
        phase("isolated_fixture_and_legacy_startup")
        legacy = login("old", "FixtureCase")
        old_key = api_key("old", legacy)
        ok("old", "GET", PLATFORM, "/api/user/self", token=legacy["token"])
        expanded = json.loads(availability_during("old", legacy, lambda: helper("expand")))
        evidence["expanded"] = expanded
        check("real additive expand retains ledger11", expanded["current_version"] == 11 and expanded["state"] == "expanded")
        start("bridge")
        bridge_runtime = ok("bridge", "GET", PLATFORM, "/api/internal/tenant-runtime")["tenant_runtime"]
        evidence["bridge_runtime_before_contract"] = bridge_runtime
        check("expanded startup snapshot is not reseller-ready", bridge_runtime["schema_at_startup"] == "expanded"
              and bridge_runtime["reseller_ready_at_startup"] is False
              and bridge_runtime["plan_checksum"] == "dbf0ffa60103902d51e41482d98b106643570b3060625251ade3473cd8a95fc4")
        ok("bridge", "GET", PLATFORM, "/api/user/self", token=legacy["token"])
        current = login("bridge", "FixtureCase")
        ok("old", "GET", PLATFORM, "/api/user/self", token=current["token"])
        status, refreshed, _ = request("bridge", "POST", PLATFORM, "/api/user/auth/refresh", cookie=legacy["cookie"])
        check("legacy refresh cookie accepted by bridge", status == 200 and refreshed.get("success") is True)
        ok("bridge", "GET", PLATFORM, "/api/usage/token/", token=old_key)
        pay("old", legacy, callback_process="bridge")
        pay("bridge", current, callback_process="old")
        for process in ["old", "bridge"]:
            before = int(sql(f"SELECT quota FROM users WHERE id={legacy['id']};"))
            chat(process, old_key)
            after = int(sql(f"SELECT quota FROM users WHERE id={legacy['id']};"))
            check(process + " platform text quota charged exactly", before - after == 100000000)
        cache = run(["docker", "exec", redis_name, "redis-cli", "HGETALL", f"user:{legacy['id']}"]).stdout.decode()
        evidence["shared_redis_user_hash"] = cache
        check("shared Redis was actually used", bool(run(["docker", "exec", redis_name, "redis-cli", "DBSIZE"]).stdout.strip())
              and int(run(["docker", "exec", redis_name, "redis-cli", "DBSIZE"]).stdout) > 0)
        status, _, _ = request("bridge", "GET", PLATFORM, "/api/reseller/applications", token=current["token"])
        check("expanded bridge keeps reseller business closed", status == 503)
        phase("expanded11_dual_http_and_redis")
        stop("old")
        finalized = json.loads(availability_during("bridge", current, lambda: helper("contract")))
        evidence["finalized"] = finalized
        check("real contract finalizes directly to14", finalized["current_version"] == 14 and finalized["state"] == "finalized")
        check("history never contains12/13", sql("SELECT string_agg(version::text,',' ORDER BY version) FROM kkai_schema_migrations;") == "1,2,3,4,5,6,7,8,9,10,11,14")
        start("feature", tenant=True)
        evidence["bridge_runtime_after_contract"] = ok("bridge", "GET", PLATFORM, "/api/internal/tenant-runtime")["tenant_runtime"]
        check("live bridge startup capability remains frozen", evidence["bridge_runtime_after_contract"] == bridge_runtime)
        feature_runtime = ok("feature", "GET", PLATFORM, "/api/internal/tenant-runtime")["tenant_runtime"]
        evidence["feature_runtime"] = feature_runtime
        check("final14 startup is reseller-ready", feature_runtime["schema_at_startup"] == "finalized"
              and feature_runtime["reseller_ready_at_startup"] is True)
        ok("feature", "GET", PLATFORM, "/api/user/self", token=current["token"])
        ok("bridge", "GET", PLATFORM, "/api/usage/token/", token=old_key)
        rejected = run([str(out / "old-api")], cwd=out / "old", env={**base_env, "PORT": str(ports['old'])}, timeout=20, check=False)
        (out / "old-after-contract.log").write_bytes(rejected.stdout + rejected.stderr)
        check("schema11 binary refuses final14", rejected.returncode != 0 and b"schema" in rejected.stdout + rejected.stderr)
        phase("contract14_and_feature_startup")
        root_name = sql("SELECT username FROM users WHERE id=1;")
        admin = login("feature", root_name)
        tenants, customers, owners, keys = [], [], [], []
        for index, prefix in enumerate(["alpha", "beta"]):
            host = prefix + ".tenant.invalid"
            owner_name = ["FixtureCase", "fixturecase"][index]
            owner_main = login("feature", owner_name)
            application = ok("feature", "POST", PLATFORM, "/api/reseller/applications", {"business_name": prefix + " brand",
                "contact_name": prefix, "contact_email": prefix + "@example.invalid", "notes": "synthetic"}, owner_main["token"])
            reviewed = ok("feature", "PATCH", PLATFORM, f"/api/admin/resellers/applications/{application['id']}", {"status": "approved"}, admin["token"])
            tenant = reviewed["tenant_id"]
            tenants.append(tenant)
            ok("feature", "POST", PLATFORM, f"/api/admin/resellers/{tenant}/domain", {"hostname": host}, admin["token"])
            owner = login("feature", owner_name, host, owner=True)
            owners.append(owner)
            ok("feature", "PATCH", host, "/api/reseller/profile", {"site_name": prefix + " unique", "contact": prefix + "@example.invalid",
                "announcement": prefix + " announcement", "homepage_content": prefix + " introduction"}, owner["token"])
            ok("feature", "PATCH", host, "/api/reseller/catalog", {"entries": [{"sales_group": "default", "model": "gpt-4o-mini", "status": "enabled", "is_default": True}]}, owner["token"])
            ok("feature", "PATCH", host, "/api/reseller/prices", {"prices": [{"sales_group": "default", "model": "gpt-4o-mini", "sale_multiplier": str(index + 2), "expected_version": 0}]}, owner["token"])
            ok("feature", "PATCH", PLATFORM, f"/api/admin/resellers/{tenant}/status", {"status": "active"}, admin["token"])
            brand = ok("feature", "GET", host, "/api/site-config")
            check(prefix + " brand isolation", brand["site_name"] == prefix + " unique")
            ok("feature", "GET", host, "/api/verification?email=shared%40example.invalid")
            check("local email verification captured", bool(SMTPStub.codes))
            ok("feature", "POST", host, "/api/user/register", {"username": "SharedCustomer", "email": "shared@example.invalid", "password": PASSWORD,
                "verification_code": SMTPStub.codes.pop(0), "tenant_id": 999})
            customer = login("feature", "SHAREDCUSTOMER", host)
            customers.append(customer)
            email_login = login("feature", "SHARED@EXAMPLE.INVALID", host)
            check(prefix + " email resolves own customer", email_login["id"] == customer["id"])
            key = api_key("feature", customer, host)
            keys.append(key)
            topup_id = pay("feature", customer, host)
            check(prefix + " paid lot created once", sql(f"SELECT count(*) FROM customer_payment_lots WHERE top_up_id={topup_id};") == "1")
            chat("feature", key, host)
            ledger = json.loads(sql(f"SELECT row_to_json(l) FROM reseller_ledger_entries l WHERE tenant_id={tenant} AND kind='consumption_settlement';"))
            evidence.setdefault("settlements", {})[prefix] = ledger
            sale = [40000, 60000][index]
            profit = [19360, 39040][index]
            check(prefix + " exact cash settlement", ledger["sale_minor"] == sale and ledger["payment_fee_minor"] == [640, 960][index]
                  and ledger["supplier_minor_exact"] == "20000" and ledger["amount_minor"] == profit)
            before = sql(f"SELECT count(*) FROM reseller_ledger_entries WHERE tenant_id={tenant} AND kind='consumption_settlement';")
            chat("feature", key, host, failed=True)
            check(prefix + " failed request earns nothing", sql(f"SELECT count(*) FROM reseller_ledger_entries WHERE tenant_id={tenant} AND kind='consumption_settlement';") == before)
            unresolved = json.loads(sql(f"SELECT COALESCE(json_agg(request_id),'[]') FROM tenant_consumptions WHERE tenant_id={tenant} AND status='reserved';"))
            for request_id in unresolved:
                status, _, _ = request("feature", "POST", PLATFORM, f"/api/admin/resellers/{tenant}/payments/{topup_id}/reverse",
                    {"idempotency_key": prefix + "-inflight", "kind": "chargeback", "reason": "synthetic check",
                     "payment_reference": "local-proof"}, admin["token"])
                check(prefix + " uncertain usage blocks payment reversal", status == 409)
                reconciliation = {"idempotency_key": prefix + "-zero-usage", "actual_quota": 0,
                    "confirmed_zero": True, "confirmed_execution_stopped": True,
                    "reason": "local stub confirmed rejected request consumed zero", "evidence": "synthetic upstream call completed with zero billable usage"}
                corrected = ok("feature", "POST", PLATFORM, f"/api/admin/resellers/{tenant}/consumptions/{request_id}/reconcile", reconciliation, admin["token"])
                duplicate = ok("feature", "POST", PLATFORM, f"/api/admin/resellers/{tenant}/consumptions/{request_id}/reconcile", reconciliation, admin["token"])
                check(prefix + " usage reconciliation is idempotent", corrected["id"] == duplicate["id"])
            helper("settle")
            check(prefix + " zero-usage reconciliation earns nothing", sql(f"SELECT count(*) FROM reseller_ledger_entries WHERE tenant_id={tenant} AND kind='consumption_settlement';") == before)
            fractions = []
            for version, multiplier in enumerate(["1.030000000000000003", "1.070000000000000011", "1.090000000000000013", "1.130000000000000017"], 1):
                ok("feature", "PATCH", host, "/api/reseller/prices", {"prices": [{"sales_group": "default", "model": "gpt-4o-mini",
                    "sale_multiplier": multiplier, "expected_version": version}]}, owner["token"])
                quota_before = int(sql(f"SELECT quota FROM users WHERE id={customer['id']};"))
                chat("feature", key, host, tiny=True)
                tiny_ledger = json.loads(sql(f"SELECT row_to_json(l) FROM reseller_ledger_entries l WHERE tenant_id={tenant} AND kind='consumption_settlement' ORDER BY id DESC LIMIT 1;"))
                consumed_quota = quota_before - int(sql(f"SELECT quota FROM users WHERE id={customer['id']};"))
                expected_quota = [1030, 1070, 1090, 1130][version - 1]
                check(prefix + f" price version {version + 1} exact tiny supply", consumed_quota == expected_quota
                      and Fraction(tiny_ledger["supplier_minor_exact"]) == Fraction(1, 5000) * consumed_quota / Fraction(multiplier)
                      and tiny_ledger["sale_minor"] == 0 and tiny_ledger["payment_fee_minor"] == 0)
                fractions.append(tiny_ledger)
            fractional_wallet = ok("feature", "GET", host, "/api/reseller/earnings", token=owner["token"])["wallet"]
            expected_carry = sum((Fraction(item["exact_profit_minor"]) for item in fractions), Fraction())
            check(prefix + " multi-price exact carry exceeds128 without truncation", len(fractional_wallet["exact_profit_carry"]) > 128
                  and Fraction(fractional_wallet["exact_profit_carry"]) == expected_carry
                  and fractional_wallet["pending_minor"] == profit)
            evidence.setdefault("multi_price_aggregates", {})[prefix] = {"settlements": fractions, "wallet": fractional_wallet}
            helper("release")
            status, _, _ = request("feature", "POST", host, "/api/reseller/withdrawals", {"amount_minor": profit,
                "idempotency_key": prefix + "-negative-carry-guard", "alipay_account": prefix + "@example.invalid", "alipay_name": prefix}, owner["token"])
            check(prefix + " large negative fractional carry reserves cash before withdrawal", status == 400)
            amount = 15000 if index == 0 else 20000
            withdrawal = ok("feature", "POST", host, "/api/reseller/withdrawals", {"amount_minor": amount, "idempotency_key": prefix + "-withdrawal",
                "alipay_account": prefix + "@example.invalid", "alipay_name": prefix}, owner["token"])
            duplicate = ok("feature", "POST", host, "/api/reseller/withdrawals", {"amount_minor": amount, "idempotency_key": prefix + "-withdrawal",
                "alipay_account": prefix + "@example.invalid", "alipay_name": prefix}, owner["token"])
            check(prefix + " withdrawal replay same ID", withdrawal["id"] == duplicate["id"] and withdrawal["fee_minor"] == 100)
            for status in ["approved", "paid"]:
                ok("feature", "PATCH", PLATFORM, f"/api/admin/resellers/withdrawals/{withdrawal['id']}", {"status": status,
                    "payment_ref": "local-confirmed-alipay" if status == "paid" else "", "note": "synthetic manual review"}, admin["token"])
            reversal_ids = []
            for _ in range(2):
                reversal = ok("feature", "POST", PLATFORM, f"/api/admin/resellers/{tenant}/payments/{topup_id}/reverse", {"idempotency_key": prefix + "-chargeback",
                    "kind": "chargeback", "reason": "local synthetic provider rejection", "payment_reference": "local-proof"}, admin["token"])
                reversal_ids.append(reversal["id"])
            check(prefix + " full payment reversal preserves large exact aggregate", reversal_ids[0] == reversal_ids[1]
                  and len(reversal["reversed_profit_minor_exact"]) > 128
                  and Fraction(reversal["reversed_profit_minor_exact"]) == profit + expected_carry)
            evidence["multi_price_aggregates"][prefix]["payment_reversal"] = reversal
            wallet = ok("feature", "GET", host, "/api/reseller/earnings", token=owner["token"])["wallet"]
            check(prefix + " paid chargeback creates exact debt", wallet["debt_minor"] == amount and wallet["available_minor"] == 0
                  and wallet["pending_minor"] == 0 and wallet["frozen_minor"] == 0 and wallet["exact_profit_carry"] == "0")
            status, _, _ = request("feature", "POST", host, "/api/reseller/withdrawals", {"amount_minor": 10000, "idempotency_key": prefix + "-blocked",
                "alipay_account": prefix + "@example.invalid", "alipay_name": prefix}, owner["token"])
            check(prefix + " debt blocks withdrawal", status == 400)
            check(prefix + " append-only reversal count", sql(f"SELECT count(*) FROM customer_payment_reversals WHERE tenant_id={tenant};") == "1")
        check("same email yields separate tenant users", customers[0]["id"] != customers[1]["id"])
        for index, prefix in enumerate(["alpha", "beta"]):
            host, other = prefix + ".tenant.invalid", ["beta", "alpha"][index] + ".tenant.invalid"
            for path, token in [("/api/user/self", customers[index]["token"]), ("/api/usage/token/", keys[index]), ("/api/reseller/earnings", owners[index]["token"])]:
                status, _, _ = request("feature", "GET", other, path, token=token)
                check(prefix + " cross-tenant " + path + " rejected", status in [401, 403])
            for path in ["/api/admin/resellers", "/api/channel/"]:
                status, _, _ = request("feature", "GET", host, path, token=owners[index]["token"])
                check(prefix + " owner platform privilege denied", status == 403)
            status, _, _ = request("feature", "GET", host, "/api/reseller/me", token=customers[index]["token"])
            check(prefix + " customer owner privilege denied", status == 403)
        check("cash ledger reconciles every wallet", sql("SELECT count(*) FROM reseller_wallets w WHERE (pending_minor,available_minor,frozen_minor,debt_minor) <> (SELECT COALESCE(SUM(pending_delta),0),COALESCE(SUM(available_delta),0),COALESCE(SUM(frozen_delta),0),COALESCE(SUM(debt_delta),0) FROM reseller_ledger_entries l WHERE l.tenant_id=w.tenant_id);") == "0")
        check("main identity keys remain untouched", sql("SELECT count(*) FROM users WHERE tenant_id=0 AND (normalized_username IS NOT NULL OR normalized_email IS NOT NULL);") == "0")
        evidence["model_stub_calls"] = ModelStub.calls
        evidence["wallets"] = json.loads(sql("SELECT json_agg(row_to_json(w)) FROM reseller_wallets w;"))
        phase("dual_tenant_http_cash_and_withdrawals")
        evidence["passed"] = True
    except BaseException as error:
        evidence["failure"] = str(error)
        raise
    finally:
        for name in list(processes):
            stop(name)
        for server in servers:
            server.shutdown()
            server.server_close()
        for container, name in reversed(containers):
            actual = run(["docker", "inspect", "--format", "{{.Id}} {{.Name}}", container], check=False).stdout.decode().strip()
            if actual != container + " /" + name:
                raise RuntimeError("refusing cleanup of changed container identity")
            run(["docker", "rm", "--force", "--volumes", container])
        evidence["cleanup_complete"] = all(run(["docker", "inspect", container], check=False).returncode != 0 for container, _ in containers)
        evidence["completed_at"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        report = out / "result.json"
        report.write_text(json.dumps(evidence, indent=2) + "\n")
        report.chmod(0o600)
    print(json.dumps({"passed": evidence["passed"], "checks": len(evidence["checks"]), "evidence": str(out / "result.json"), "cleanup": evidence["cleanup_complete"]}))


if __name__ == "__main__":
    main()
