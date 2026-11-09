#!/usr/bin/env python3
"""Local PostgreSQL 18 / Linux CLI proof rehearsal; synthetic runtime identities.

Uses an existing disposable database container and an external-schema Linux ARM64
CLI binary. Builds no image, pulls nothing, and never accesses production.
"""

import argparse
import copy
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import time
from urllib.parse import quote

IMAGE = "postgres:18.4-bookworm"
PROOF_PATH = "/run/membership-billing-recovery-migration/proof.json"
OPERATION = "--membership-billing-recovery-migration"
PROOF_FLAG = "--membership-billing-recovery-controller-proof"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--container", required=True)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--password-file", type=Path, required=True)
    parser.add_argument("--port", type=int, default=62729)
    args = parser.parse_args()
    if not re.fullmatch(r"kkai-membership-v21-pg18-[a-z0-9]+", args.container):
        raise ValueError("requires an explicitly named disposable membership-v21 PG18 container")
    if os.environ.get("DOCKER_HOST") or not 1024 <= args.port <= 65535:
        raise ValueError("requires local Docker and a nonprivileged loopback port")
    binary, output = args.binary.resolve(), args.output.resolve()
    if not binary.is_file() or not output.is_dir():
        raise ValueError("existing CLI binary and output directory are required")
    password = args.password_file.read_text().strip()
    if not password:
        raise ValueError("empty local database password")
    started, deadline = time.monotonic(), time.monotonic() + 235
    docker = ["docker", "--context", "desktop-linux"]
    token = secrets.token_hex(4)
    database = "kkai_test_shop_http_recovery_" + token
    migrator, standby = "recovery_owner_" + token, "recovery_reader_" + token
    volume = "kkai-membership-v21-proof-" + token
    report = {"schema": "kkai_membership_recovery_cli_root_proof_rehearsal_v1", "result": "failed",
              "production_access": False, "production_image_built": False,
              "runtime_evidence": "synthetic", "database": database,
              "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(), "tests": [],
              "limits": ["Exercises real Linux CLI proof validation and PostgreSQL DDL, not production host process observations or installed-controller acceptance.",
                         "The local driver restores the disposable migration role in finally blocks; production signal recovery remains a separate controller gate."]}
    cleanup_errors, created_database, created_volume = [], False, False

    def run(command, *, payload=None, env=None, timeout=20, check=True, cleanup=False):
        remaining = deadline - time.monotonic() - (0 if cleanup else 20)
        if remaining <= 0:
            raise TimeoutError("local rehearsal exhausted its bounded time budget")
        result = subprocess.run(command, input=payload, text=True, capture_output=True,
                                cwd=Path(__file__).resolve().parents[2], env=env, timeout=min(timeout, remaining))
        if check and result.returncode:
            detail = result.stderr[-2000:].replace(password, "[redacted]").replace(quote(password, safe=""), "[redacted]")
            raise RuntimeError(f"local command failed ({result.returncode}): {detail}")
        return result

    def sql(statement, *, target=database, cleanup=False):
        env = {**os.environ, "PGPASSWORD": password, "KKAI_REHEARSAL_PASSWORD": password}
        return run([*docker, "exec", "-i", "--env", "PGPASSWORD", "--env", "KKAI_REHEARSAL_PASSWORD",
                    args.container, "psql", "-h", "127.0.0.1", "-U", "postgres", "-d", target,
                    "-XqAt", "-v", "ON_ERROR_STOP=1"], payload=statement, env=env, cleanup=cleanup).stdout.strip()

    def close_owner(*, cleanup=False):
        sql(f"ALTER ROLE {migrator} NOLOGIN; SELECT pg_terminate_backend(pid) FROM pg_stat_activity "
            f"WHERE usename='{migrator}' AND pid<>pg_backend_pid();", target="postgres", cleanup=cleanup)

    def owner_closed():
        return sql(f"SELECT NOT rolcanlogin AND NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename='{migrator}') "
                   f"FROM pg_roles WHERE rolname='{migrator}';", target="postgres") == "t"

    def cli(arguments, *, writable=False, proof=None, readonly=True, mode="0400", owner="0:0", check=True):
        command = [*docker, "run", "--rm", "-i", "--pull", "never", "--user", "0:0", "--read-only",
                   "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
                   "--network", "container:" + args.container,
                   "--mount", f"type=bind,source={binary},target=/kkai-migrate,readonly"]
        if proof is not None:
            # Docker Desktop bind mounts retain Mac ownership. Create the proof
            # on the daemon's volume, then bind its individual file read-only.
            run([*docker, "run", "--rm", "-i", "--pull", "never", "--network", "none", "--user", "0:0",
                 "--mount", f"type=volume,source={volume},target=/proof", "--entrypoint", "/bin/sh", IMAGE,
                 "-ceu", 'umask 077; chmod 0700 /proof; cat > /proof/proof.json; chown "$1" /proof/proof.json; chmod "$2" /proof/proof.json',
                 "proof-writer", owner, mode], payload=json.dumps(proof))
            command += ["--mount", f"type=bind,source={proof_mount}/proof.json,target={PROOF_PATH}" + (",readonly" if readonly else "")]
        command += ["--entrypoint", "/kkai-migrate", IMAGE, *arguments, "--dsn-stdin", "--timeout", "15s"]
        role = migrator if writable else standby
        dsn = f"postgres://{role}:{quote(password, safe='')}@127.0.0.1:5432/{database}?sslmode=disable&connect_timeout=5&lock_timeout=100ms&statement_timeout=5s"
        if writable:
            dsn += "&transaction_timeout=5s"
        try:
            if writable:
                sql(f"ALTER ROLE {migrator} LOGIN;", target="postgres")
            return run(command, payload=dsn + "\n", timeout=20, check=check)
        finally:
            if writable:
                close_owner(cleanup=True)

    def proof_for(state):
        now = int(time.time())
        proof = {"schema": "kkai_membership_billing_recovery_migration_proof_v1",
                 "protocol": "membership-billing-recovery-migration-v1", "operation": "apply",
                 "installed_infra_sha": "a" * 40, "image_id": report["cli_container_image_id"],
                 "source_version": 20, "target_version": 21,
                 **{key: state[key] for key in ("source_digest", "target_digest", "plan_checksum", "database_name", "database_schema")},
                 "issued_at": now, "expires_at": now + 120, "nonce": secrets.token_hex(16), "runtimes": []}
        for index, role in enumerate(("active", "rollback", "writer")):
            proof["runtimes"].append({"name": "synthetic-" + role, "role": role,
                "container_id": chr(97 + index) * 64, "image_id": proof["image_id"], "version": "synthetic-local",
                "tenant_routing_enabled": role != "rollback", "tenant_aware": True, "separate_log_database": False,
                "container_started_at": datetime.datetime.fromtimestamp(now - 60, datetime.timezone.utc).isoformat(),
                "container_init_start_ticks": 100, "application_pid": index + 10, "application_start_ticks": 101,
                "application_mount_namespace": "mnt:[123]", "application_executable_device": 1,
                "application_executable_inode": 2, "application_cgroup_sha256": "d" * 64,
                "schema_contract": copy.deepcopy(contract)})
        return proof

    def record(name):
        assert owner_closed(), "migration role must be NOLOGIN with zero sessions"
        report["tests"].append({"name": name, "result": "pass", "migration_role": "NOLOGIN/0 sessions"})

    def receipts():
        return sql("SELECT coalesce(json_agg(row_to_json(r) ORDER BY version)::text,'[]') FROM kkai_schema_migrations r;")

    try:
        endpoint = run([*docker, "context", "inspect", "--format", "{{.Endpoints.docker.Host}}"]).stdout.strip()
        assert endpoint.startswith("unix://"), "only local Docker Unix sockets are allowed"
        container = json.loads(run([*docker, "inspect", args.container]).stdout)[0]
        assert container["State"]["Running"] is True
        ports = container["NetworkSettings"]["Ports"].get("5432/tcp", [])
        assert {"HostIp": "127.0.0.1", "HostPort": str(args.port)} in ports, "database must use the explicit loopback port"
        image = json.loads(run([*docker, "image", "inspect", IMAGE]).stdout)[0]
        assert container["Image"] == image["Id"], "database image must be the local reviewed PG18 image"
        report["database_image_id"] = report["cli_container_image_id"] = image["Id"]
        assert sql("SHOW server_version_num;", target="postgres").startswith("18")
        contract = json.loads(run([*docker, "run", "--rm", "--pull", "never", "--network", "none",
            "--mount", f"type=bind,source={binary},target=/kkai-migrate,readonly", "--entrypoint", "/kkai-migrate",
            IMAGE, "--describe-contract", "--dialect", "postgres", "--json"]).stdout)
        assert (contract["runtime_min_version"], contract["runtime_max_version"], contract["migration_target_version"], contract["schema_management"]) == (14, 21, 14, "external")
        report["contract"] = contract
        sql(f"CREATE DATABASE {database};", target="postgres")
        created_database = True
        fixture_dsn = f"postgres://postgres:{quote(password, safe='')}@127.0.0.1:{args.port}/{database}?sslmode=disable"
        env = {**os.environ, "KKAI_SHOP_HTTP_TEST_DSN": fixture_dsn, "KKAI_SHOP_HTTP_FIXTURE_SCHEMA": "20", "GOPROXY": "off", "GOTOOLCHAIN": "local"}
        run(["go", "test", "./pkg/kkaimigrate", "-run", "^TestShopHTTPFixtureSchema$", "-count=1", "-timeout=90s"], env=env, timeout=100)
        sql(f"CREATE ROLE {migrator} NOLOGIN; CREATE ROLE {standby} LOGIN;\n"
            "\\getenv local_password KKAI_REHEARSAL_PASSWORD\n"
            f"SELECT format('ALTER ROLE %I PASSWORD %L', role, :'local_password') FROM unnest(ARRAY['{migrator}','{standby}']) AS role \\gexec\n"
            f"ALTER DATABASE {database} OWNER TO {migrator}; ALTER SCHEMA public OWNER TO {migrator}; "
            f"SELECT format('ALTER TABLE public.%I OWNER TO {migrator}', tablename) FROM pg_tables WHERE schemaname='public' \\gexec\n"
            f"GRANT CONNECT ON DATABASE {database} TO {standby}; GRANT USAGE ON SCHEMA public TO {standby}; "
            f"GRANT SELECT ON ALL TABLES IN SCHEMA public TO {standby}; ALTER ROLE {standby} SET default_transaction_read_only=on; "
            f"ALTER DEFAULT PRIVILEGES FOR ROLE {migrator} IN SCHEMA public GRANT SELECT ON TABLES TO {standby};")
        run([*docker, "volume", "create", volume])
        created_volume = True
        proof_mount = run([*docker, "volume", "inspect", "--format", "{{.Mountpoint}}", volume]).stdout.strip()
        assert proof_mount.startswith("/var/lib/docker/volumes/"), "unexpected daemon proof volume"
        before = receipts()
        state = json.loads(cli([OPERATION, "plan", "--json"]).stdout)
        assert state["current_version"] == 20 and not state["membership_billing_recovery_schema_present"]
        assert receipts() == before
        record("readonly_plan_on_canonical20")
        apply_args = [OPERATION, "apply", PROOF_FLAG, PROOF_PATH, "--json"]
        for case in ("expired", "writable_mount", "wrong_owner", "wrong_mode", "old_rollback"):
            proof, options = proof_for(state), {}
            expected = {"expired": "expired", "writable_mount": "independent read-only bind mount",
                        "wrong_owner": "root-owned 0400", "wrong_mode": "root-owned 0400", "old_rollback": "exact v14-v21-compatible"}[case]
            if case == "expired":
                proof["issued_at"], proof["expires_at"] = int(time.time()) - 120, int(time.time()) - 1
            elif case == "writable_mount":
                options["readonly"] = False
            elif case == "wrong_owner":
                options["owner"] = "10007:10007"
            elif case == "wrong_mode":
                options["mode"] = "0600"
            else:
                proof["runtimes"][1]["schema_contract"]["runtime_max_version"] = 20
                del proof["runtimes"][1]["schema_contract"]["compatible_prefixes"]["21"]
            result = cli(apply_args, writable=True, proof=proof, check=False, **options)
            assert result.returncode != 0 and expected in result.stderr, "proof rejection did not match " + case
            assert receipts() == before
            record(case + "_rejected")
        sql("CREATE INDEX ux_membership_recovery_receipt ON users(id);")
        result = cli(apply_args, writable=True, proof=proof_for(state), check=False)
        assert result.returncode != 0 and "ux_membership_recovery_receipt" in result.stderr and "already exists" in result.stderr, "forced DDL conflict must reach the index creation"
        assert sql("SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('kkai_membership_billing_recovery','kkai_membership_billing_scan_progress');") == "0"
        assert receipts() == before
        record("ddl_failure_rolls_back_tables_and_receipt")
        sql("DROP INDEX ux_membership_recovery_receipt;")
        after = json.loads(cli(apply_args, writable=True, proof=proof_for(state)).stdout)
        assert after["current_version"] == 21 and after["membership_billing_recovery_schema_present"]
        assert after["target_digest"] == contract["compatible_prefixes"]["21"]
        assert sql("SELECT count(*) FROM kkai_schema_migrations WHERE version=21;") == "1"
        saved = receipts()
        assert json.loads(saved)[:-1] == json.loads(before)
        record("root_readonly_proof_apply_succeeds")
        replay = json.loads(cli(apply_args, proof=proof_for(after)).stdout)
        assert replay == after and receipts() == saved
        record("completed_apply_replay_uses_readonly_identity")
        report["result"] = "pass"
    except Exception as error:
        report["result"] = "failed"
        report["error"] = str(error).replace(password, "[redacted]").replace(quote(password, safe=""), "[redacted]")
    finally:
        for name, cleanup in (
            ("roles_closed", lambda: sql(f"DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='{migrator}') THEN "
                f"ALTER ROLE {migrator} NOLOGIN; END IF; END $$; SELECT pg_terminate_backend(pid) FROM pg_stat_activity "
                f"WHERE datname='{database}' AND pid<>pg_backend_pid();", target="postgres", cleanup=True)),
            ("database_removed", lambda: sql(f"DROP DATABASE IF EXISTS {database};", target="postgres", cleanup=True)),
            ("roles_removed", lambda: sql(f"DROP ROLE IF EXISTS {standby}; DROP ROLE IF EXISTS {migrator};", target="postgres", cleanup=True)),
            ("proof_volume_removed", lambda: run([*docker, "volume", "rm", volume], cleanup=True))):
            if name == "proof_volume_removed" and not created_volume or name != "proof_volume_removed" and not created_database:
                continue
            try:
                cleanup()
                report.setdefault("cleanup", {})[name] = True
            except Exception:
                cleanup_errors.append(name)
        report["cleanup_errors"] = cleanup_errors
        report["elapsed_seconds"] = round(time.monotonic() - started, 3)
        if cleanup_errors:
            report["result"] = "failed"
        evidence = output / ("membership-recovery-root-proof-" + token + ".json")
        evidence.write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps({"result": report["result"], "tests": report["tests"], "evidence": str(evidence), "cleanup_errors": cleanup_errors}))
    return 0 if report["result"] == "pass" else 1


if __name__ == "__main__":
    raise SystemExit(main())
