#!/usr/bin/env python3
"""Bounded, local-only Linux proof and synthetic scale rehearsal; no image build.

Uses an existing disposable kkai-online-core-* PostgreSQL container and a
cross-compiled kkai-migrate binary. Each invocation is one resumable phase.
"""

import argparse
import datetime
from functools import partial
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import threading
import time
import urllib.parse


IMAGE = "postgres:18.4-bookworm"
FIXTURE_SHA = "9ddf138b294168c62bf8e6a2099eef0b9e1539d4b6a3d0cdc00a5d77c41b6986"
PROOF_PATH = "/run/tenant-online/proof.json"


def invoke(command, *, deadline, payload=None, timeout=120, check=True):
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise TimeoutError("the bounded local phase exhausted its 235-second budget")
    result = subprocess.run(command, input=payload, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=min(timeout, remaining))
    if check and result.returncode:
        raise RuntimeError(f"{command[0]} exited {result.returncode}: {result.stderr.decode()[-2000:]}")
    return result


def main():
    run = partial(invoke, deadline=time.monotonic() + 235)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("phase", choices=["proof", "initialize", "populate", "expand", "finalize", "sizes"])
    parser.add_argument("--container", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--fixture", type=Path)
    parser.add_argument("--table", choices=["logs", "quota_data"])
    parser.add_argument("--start", type=int, default=1)
    parser.add_argument("--count", type=int, default=100000)
    args = parser.parse_args()
    if not re.fullmatch(r"kkai-online-core-[a-z0-9-]+", args.container):
        raise ValueError("requires the dedicated local online-core test container")
    endpoint = run(["docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}"])
    if not endpoint.stdout.decode().strip().startswith("unix://") or os.environ.get("DOCKER_HOST"):
        raise ValueError("requires a local Docker Unix socket without DOCKER_HOST override")
    output = args.output.resolve()
    if not (output / "kkai-migrate").is_file():
        raise ValueError("missing locally cross-compiled kkai-migrate")
    database = "kkai_test_tenant_proof" if args.phase == "proof" else "kkai_test_online_scale"
    report = {"phase": args.phase, "scope": "disposable local synthetic data only",
              "database": database, "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "binary_sha256": hashlib.sha256((output / "kkai-migrate").read_bytes()).hexdigest()}
    started = time.monotonic()

    def sql(statement, timeout=120):
        result = run(["docker", "exec", "-i", args.container, "psql", "-U", "postgres", "-d", database,
                      "-XqAt", "-v", "ON_ERROR_STOP=1"], payload=statement.encode(), timeout=timeout)
        return result.stdout.decode().strip()

    def cli(operation, proof=None, *, readonly=True, user="0", check=True):
        options = "-c lock_timeout=100ms -c statement_timeout=30s -c transaction_timeout=65s -c log_min_duration_statement=0"
        dsn = "postgres://postgres@127.0.0.1:5432/" + database + "?" + urllib.parse.urlencode({
            "sslmode": "disable", "application_name": "kkai_tenant_local_" + args.phase, "options": options})
        command = ["docker", "run", "--rm", "-i", "--pull", "never", "--user", user, "--read-only", "--cap-drop", "ALL",
                   "--security-opt", "no-new-privileges", "--network", "container:" + args.container,
                   "--mount", f"type=bind,src={output / 'kkai-migrate'},dst=/kkai-migrate,readonly"]
        if proof:
            command += ["--mount", f"type=bind,src={proof},dst={PROOF_PATH}" + (",readonly" if readonly else "")]
        command += ["--entrypoint", "/kkai-migrate", IMAGE, "--dsn-stdin", "--tenant-expand", operation, "--json", "--timeout", "110s"]
        if proof:
            command += ["--tenant-controller-proof", PROOF_PATH]
        return run(command, payload=(dsn + "\n").encode(), timeout=125, check=check)

    def proof_file(operation, name, *, expired=False, owner=0, mode="0400"):
        status = json.loads(cli("plan").stdout)
        now = int(time.time())
        proof = {"schema": "kkai_tenant_online_proof_v1", "protocol": "tenant-online-v1", "operation": operation,
                 "installed_infra_sha": "b" * 40, "image_id": "sha256:" + "c" * 64,
                 "plan_checksum": status["plan_checksum"], "source_digest": status["source_digest"],
                 "source_version": 11, "target_version": 14, "database_name": status["database_name"],
                 "database_schema": status["database_schema"], "issued_at": now - 120 if expired else now,
                 "expires_at": now - 1 if expired else now + 120, "nonce": "d" * 32, "runtimes": []}
        for index, role in enumerate(["active", "rollback", "writer"]):
            proof["runtimes"].append({"name": "synthetic-" + role, "role": role, "container_id": chr(97 + index) * 64,
                "image_id": "sha256:" + "c" * 64, "version": "synthetic-local", "tenant_routing_enabled": False,
                "tenant_aware": True, "separate_log_database": False})
        source = output / (name + ".source.json")
        source.write_text(json.dumps(proof))
        source.chmod(0o600)
        target = output / (name + ".proof.json")
        run(["docker", "run", "--rm", "--pull", "never", "--network", "none", "--user", "0",
             "--mount", f"type=bind,src={output},dst=/evidence", "--entrypoint", "install", IMAGE,
             "-m", mode, "-o", str(owner), "-g", str(owner), "/evidence/" + source.name, "/evidence/" + target.name])
        return target

    def sizes():
        return json.loads(sql("""SELECT json_build_object('database_bytes',pg_database_size(current_database()),
 'tables',(SELECT json_agg(json_build_object('table',relname,'heap_bytes',pg_relation_size(oid),
 'table_bytes',pg_table_size(oid),'index_bytes',pg_indexes_size(oid),'estimated_rows',reltuples::bigint))
 FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind='r'),
 'logs_rows',(SELECT count(*) FROM logs),'quota_data_rows',(SELECT count(*) FROM quota_data));"""))

    try:
        if args.phase == "proof":
            report["cases"] = []
            for name, owner, mode, readonly, expired, user, expected in [
                ("rw-mount", 0, "0400", False, False, "0", "not an independent read-only bind mount"),
                ("wrong-owner", 10007, "0400", True, False, "0", "root-owned 0400"),
                ("wrong-mode", 0, "0600", True, False, "0", "root-owned 0400"),
                ("expired", 0, "0400", True, True, "0", "expired or has an invalid validity window"),
                ("non-root", 0, "0400", True, False, "10007:10007", "requires UID 0"),
            ]:
                proof = proof_file("apply", name, owner=owner, mode=mode, expired=expired)
                result = cli("apply", proof, readonly=readonly, user=user, check=False)
                passed = result.returncode != 0 and expected in result.stderr.decode()
                report["cases"].append({"case": name, "passed": passed, "exit_code": result.returncode,
                    "error": result.stderr.decode().strip()})
                if not passed:
                    raise AssertionError(name)
                assert json.loads(cli("plan").stdout)["state"] == "pristine"
            for operation, expected in [("apply", "expanded"), ("finalize", "finalized")]:
                result = cli(operation, proof_file(operation, "valid-" + operation))
                status = json.loads(result.stdout)
                assert status["state"] == expected
                report["cases"].append({"case": "valid-" + operation, "passed": True, "status": status})
        elif args.phase == "initialize":
            if not args.fixture or hashlib.sha256(args.fixture.read_bytes()).hexdigest() != FIXTURE_SHA:
                raise ValueError("requires the independently verified synthetic source11 identity-collision fixture")
            run(["docker", "exec", args.container, "createdb", "-U", "postgres", database])
            run(["docker", "exec", "-i", args.container, "pg_restore", "-U", "postgres", "-d", database,
                 "--exit-on-error"], payload=args.fixture.read_bytes())
            report["baseline"] = json.loads(cli("plan").stdout)
            # Keep WAL bounded inside the disposable tmpfs while retaining logged
            # tables, fsync and full_page_writes. Never apply these GUCs remotely.
            for setting, value in [("max_wal_size", "128MB"), ("min_wal_size", "80MB"),
                                   ("log_line_prefix", "%m [%p] %a ")]:
                sql("ALTER SYSTEM SET " + setting + "='" + value + "';")
            sql("SELECT pg_reload_conf();")
            report["sizes"] = sizes()
        elif args.phase == "populate":
            if not args.table or args.start < 1 or not 1 <= args.count <= 100000:
                raise ValueError("populate requires a fixed table and 1..100000 synthetic rows per bounded chunk")
            capacity = run(["docker", "exec", args.container, "df", "-B1", "--output=avail", "/var/lib/postgresql"])
            report["tmpfs_available_bytes_before"] = int(capacity.stdout.decode().splitlines()[-1])
            if report["tmpfs_available_bytes_before"] < 400000000:
                raise RuntimeError("insufficient disposable tmpfs headroom; stop rather than fill the database volume")
            last = args.start + args.count - 1
            if args.table == "logs":
                statement = f"""INSERT INTO logs(id,user_id,created_at,type,content,username,token_name,model_name,
 quota,prompt_tokens,completion_tokens,use_time,is_stream,channel_id,token_id,\"group\",ip,request_id,other)
 SELECT 1000000+g,1,1800000000+g,2,repeat(md5(g::text),36),'local-synthetic','scale-token','gpt-4o-mini',
 100,10,10,1,false,1,1,'default','127.0.0.1','synthetic-'||g,'{{}}'
 FROM generate_series({args.start},{last}) g;"""
            else:
                statement = f"""INSERT INTO quota_data(id,user_id,username,model_name,created_at,token_used,count,quota,
 use_group,token_id,channel_id,node_name) SELECT 1000000+g,1,'local-synthetic','gpt-4o-mini',
 1800000000+g,20,1,100,'default',1,1,'synthetic-node' FROM generate_series({args.start},{last}) g;"""
            sql("SET statement_timeout='210s';" + statement + "CHECKPOINT;", timeout=225)
            report.update(table=args.table, first=args.start, last=last, inserted=args.count, sizes=sizes())
        elif args.phase in ("expand", "finalize"):
            operation = "apply" if args.phase == "expand" else "finalize"
            proof = proof_file(operation, "scale-" + args.phase)
            report["before"] = sizes()
            stop = threading.Event()
            writes, failures, waits, progress = [], [], [], []

            def legacy_writer():
                count = 0
                while not stop.is_set():
                    count += 1
                    begin = time.monotonic()
                    try:
                        sql("SET statement_timeout='2s';SET lock_timeout='500ms';BEGIN;"
                            "UPDATE users SET quota=quota+1,used_quota=used_quota+1 WHERE id=1;"
                            "INSERT INTO logs(user_id,created_at,type,content,username,model_name,quota)"
                            " VALUES(1,1800000000,2,'old SQL projection','local-synthetic','gpt-4o-mini',1);"
                            "INSERT INTO quota_data(user_id,username,model_name,quota)"
                            " VALUES(1,'local-synthetic','gpt-4o-mini',1);COMMIT;", timeout=5)
                        writes.append(round(time.monotonic() - begin, 6))
                    except Exception as error:
                        failures.append(str(error))

            def observe_locks():
                while not stop.is_set():
                    try:
                        snapshot = json.loads(sql("""SELECT json_build_object('waits',(SELECT json_agg(json_build_object(
 'application',application_name,'event',wait_event,'query',left(query,180))) FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event_type='Lock'),'progress',(SELECT json_agg(json_build_object(
 'phase',phase,'blocks_total',blocks_total,'blocks_done',blocks_done,'tuples_total',tuples_total,
 'tuples_done',tuples_done)) FROM pg_stat_progress_create_index WHERE datname=current_database()));""", timeout=5))
                        if snapshot["waits"]:
                            waits.append(snapshot["waits"])
                        if snapshot["progress"]:
                            progress.append(snapshot["progress"])
                    except Exception as error:
                        failures.append("observer: " + str(error))
                    stop.wait(0.05)

            threads = [threading.Thread(target=legacy_writer), threading.Thread(target=observe_locks)]
            for thread in threads:
                thread.start()
            try:
                migration_start = time.monotonic()
                result = cli(operation, proof, check=False)
                report["migration_seconds"] = round(time.monotonic() - migration_start, 6)
                report["exit_code"] = result.returncode
                report["stdout"] = result.stdout.decode()
                report["stderr"] = result.stderr.decode()
                if result.returncode == 0:
                    report["status"] = json.loads(result.stdout)
                    assert report["status"]["state"] == ("expanded" if operation == "apply" else "finalized")
            finally:
                stop.set()
                for thread in threads:
                    thread.join(10)
            report.update(legacy_write_successes=len(writes), legacy_write_failures=failures,
                          legacy_max_transaction_seconds=max(writes, default=0),
                          observed_lock_waits=waits, observed_index_progress=progress, after=sizes())
            logs = run(["docker", "logs", "--since", report["started_at"], args.container]).stderr.decode()
            report["ddl_server_durations"] = [line for line in logs.splitlines()
                if "kkai_tenant_local_" + args.phase in line and re.search(r"(CREATE TABLE|CREATE .*INDEX.*CONCURRENTLY|ALTER TABLE|LOCK TABLE)", line)]
            if report["exit_code"] or failures:
                raise AssertionError("migration or legacy writer failed; inspect recorded evidence")
        else:
            report["sizes"] = sizes()
        report["passed"] = True
    except Exception as error:
        report.update(passed=False, error=str(error))
        raise
    finally:
        report["wall_seconds"] = round(time.monotonic() - started, 6)
        name = args.phase + (f"-{args.table}-{args.start}" if args.phase == "populate" else "")
        (output / (name + "-" + str(time.time_ns()) + ".evidence.json")).write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps({key: value for key, value in report.items() if key not in
                         ("before", "after", "sizes", "ddl_server_durations", "observed_index_progress")}))


if __name__ == "__main__":
    main()
