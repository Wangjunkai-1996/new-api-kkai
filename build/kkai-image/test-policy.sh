#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly BUILD_ROOT="${ROOT}/build/kkai-image"
readonly WORKFLOW="${ROOT}/.github/workflows/kkai-production-image.yml"
readonly CANDIDATE_WORKFLOW="${ROOT}/.github/workflows/kkai-image-candidate.yml"
readonly QUALITY_WORKFLOW="${ROOT}/.github/workflows/kkai-fork-quality.yml"
readonly RISK_INTEGRATION_TEST="${ROOT}/service/kkai_risk_stream_redis_integration_test.go"
readonly SCHEMA_CONTRACT_EXPORTER="${BUILD_ROOT}/export-schema-contract.sh"
readonly AGENT_RULES="${ROOT}/AGENTS.md"
readonly REDIS_CI_IMAGE='redis:8.6.3@sha256:48e78eb9d1e1adcfb10184b2cc3c7fc5ed21e5a3be08875f239257d194bab8c9'

fail() {
  echo "KKAI image policy: $*" >&2
  exit 1
}

contains_fixed() {
  grep -Fq -- "$1" "$2"
}

contains_regex() {
  grep -Eq -- "$1" "$2"
}

for script in export-release.sh export-schema-contract.sh smoke-compose.sh verify-image.sh; do
  [[ -x "${BUILD_ROOT}/${script}" ]] || fail "${script} is not executable"
done
contains_fixed '/build' "${ROOT}/.dockerignore" ||
  fail "fork-owned image tools leak into the application build context"

for image_arg in BUN_IMAGE GO_IMAGE BUSYBOX_IMAGE DISTROLESS_IMAGE; do
  contains_regex "^ARG ${image_arg}=[^[:space:]]+@sha256:[0-9a-f]{64}$" \
    "${BUILD_ROOT}/Dockerfile" ||
    fail "${image_arg} is not pinned to an immutable manifest"
done
[[ "$(grep -Ec '^ARG BUN_IMAGE=' "${BUILD_ROOT}/Dockerfile")" -eq 1 ]] ||
  fail "Bun image definition is duplicated"
[[ "$(grep -Ec '^ARG GO_IMAGE=' "${BUILD_ROOT}/Dockerfile")" -eq 1 ]] ||
  fail "Go image definition is duplicated"

contains_fixed 'COPY --from=kkai_image' "${BUILD_ROOT}/Dockerfile" ||
  fail "runtime tools are not sourced from the repository build context"
contains_fixed '-o /out/new-api .' "${BUILD_ROOT}/Dockerfile" ||
  fail "image build does not compile the complete root package"
contains_fixed '-o /out/kkai-migrate ./cmd/kkai-migrate' "${BUILD_ROOT}/Dockerfile" ||
  fail "image build does not compile the unified schema command"
contains_fixed '-o /out/kkai-topup-recovery ./cmd/kkai-topup-recovery' "${BUILD_ROOT}/Dockerfile" ||
  fail "image build omits top-up recovery"
contains_fixed "common.SchemaManagementMode=\${SCHEMA_MANAGEMENT}" "${BUILD_ROOT}/Dockerfile" ||
  fail "formal binaries do not compile the immutable schema management mode"
contains_fixed "com.kkai.runtime.schema-management=\"\${SCHEMA_MANAGEMENT}\"" "${BUILD_ROOT}/Dockerfile" ||
  fail "Dockerfile does not publish external schema management"
for delivery_file in \
  "${BUILD_ROOT}/Dockerfile" \
  "${BUILD_ROOT}/smoke-compose.sh" \
  "${BUILD_ROOT}/verify-image.sh" \
  "${BUILD_ROOT}/export-release.sh" \
  "${WORKFLOW}" \
  "${CANDIDATE_WORKFLOW}"; do
  if contains_regex 'kkai-schema-observe|describe-upstream-schema|upstream-schema-compatibility|check-upstream-baseline|bootstrap-empty-upstream-baseline' \
    "${delivery_file}"; then
    fail "delivery path still depends on the retired upstream adoption stack: ${delivery_file}"
  fi
done
if contains_regex 'go get|go mod tidy|./main\.go' "${BUILD_ROOT}/Dockerfile"; then
  fail "image build mutates dependencies or compiles a single source file"
fi

for role in leader serving; do
  contains_fixed "NEWAPI_NODE_ROLE=${role}" "${BUILD_ROOT}/smoke-compose.sh" ||
    fail "smoke test does not exercise ${role} startup"
done
contains_fixed '--dsn-stdin' "${BUILD_ROOT}/smoke-compose.sh" ||
  fail "smoke test does not exercise migration stdin"
if ! ruby -ryaml -e 'YAML.safe_load_file(ARGV.fetch(0), aliases: true)' "${BUILD_ROOT}/smoke-compose.yml" >/dev/null; then
  fail "smoke-compose.yml is not valid YAML"
fi
contains_fixed 'compose config --quiet' "${BUILD_ROOT}/smoke-compose.sh" ||
  fail "smoke test does not validate the resolved Compose configuration"
contains_fixed '--observe --current --json --dsn-stdin' "${BUILD_ROOT}/smoke-compose.sh" ||
  fail "smoke test does not use the unified migrator observer"
contains_fixed 'EXPECTED_SCHEMA_MIGRATION_KIND' "${BUILD_ROOT}/smoke-compose.sh" ||
  fail "smoke test does not validate the exported migration kind"
for fingerprint in schema_fingerprint ledger_fingerprint; do
  contains_fixed "${fingerprint}_before" "${BUILD_ROOT}/smoke-compose.sh" ||
    fail "smoke test does not capture the initial ${fingerprint}"
  contains_fixed "${fingerprint}_after" "${BUILD_ROOT}/smoke-compose.sh" ||
    fail "smoke test does not capture the final ${fingerprint}"
done
if contains_regex 'ALTER TABLE kkai_outbox|outbox_event_key_mysql57_compat|current_version == 4|== 191' \
  "${BUILD_ROOT}/smoke-compose.sh"; then
  fail "PostgreSQL v3 smoke must not execute or emulate the MySQL-only v4 migration"
fi
if contains_fixed 'KKAI_UPSTREAM_SCHEMA_MIGRATION_MODE' "${BUILD_ROOT}/smoke-compose.yml" ||
  contains_fixed 'KKAI_UPSTREAM_SCHEMA_MIGRATION_MODE' "${ROOT}/common/node_role.go"; then
  fail "production application runtime still exposes the legacy one-shot upstream migration mode"
fi
if contains_fixed "image: ${REDIS_CI_IMAGE}" "${WORKFLOW}" ||
  contains_fixed 'KKAI_TEST_REDIS_ADDRESS' "${WORKFLOW}" ||
  contains_fixed 'KKAI_TEST_REDIS_REQUIRED' "${WORKFLOW}"; then
  fail "ordinary production image builds have a fixed Redis integration dependency"
fi
contains_fixed 'risk-stream-redis-integration:' "${QUALITY_WORKFLOW}" ||
  fail "fork quality has no independent Risk Redis integration check"
risk_integration_job="$(
  sed -n '/^  risk-stream-redis-integration:$/,$p' "${QUALITY_WORKFLOW}"
)"
readonly risk_integration_job
[[ -n "${risk_integration_job}" ]] ||
  fail "Risk Redis integration job is empty"
[[ "$(grep -Fc "image: ${REDIS_CI_IMAGE}" "${QUALITY_WORKFLOW}")" -eq 1 ]] ||
  fail "reviewed Redis CI image must appear exactly once"
contains_fixed "image: ${REDIS_CI_IMAGE}" <(printf '%s\n' "${risk_integration_job}") ||
  fail "Risk Redis integration is not pinned to the reviewed linux/amd64 digest"
ordinary_quality_jobs="$(
  sed '/^  risk-stream-redis-integration:$/,$d' "${QUALITY_WORKFLOW}"
)"
readonly ordinary_quality_jobs
if grep -Eq 'image: redis:|KKAI_TEST_REDIS_(ADDRESS|REQUIRED)' <<<"${ordinary_quality_jobs}"; then
  fail "ordinary fork quality jobs have a fixed Redis integration dependency"
fi
contains_fixed "if: needs.risk-integration-scope.outputs.required == 'true'" "${QUALITY_WORKFLOW}" ||
  fail "Risk Redis integration is not path-gated"
contains_fixed "KKAI_TEST_REDIS_REQUIRED: 'true'" "${QUALITY_WORKFLOW}" ||
  fail "Risk Redis integration can silently skip when its service is unavailable"
contains_fixed "go test ./service -run '^TestRedis86RiskStreamConsumerLifecycle$' -count=1" \
  "${QUALITY_WORKFLOW}" ||
  fail "Risk Redis integration does not run the reviewed lifecycle test"
for risk_scope_path in \
  'build/kkai-image/internal/riskguard/*' \
  'service/kkai_risk_*.go' \
  'service/kkai_runtime_jobs.go'; do
  contains_fixed "${risk_scope_path}" "${QUALITY_WORKFLOW}" ||
    fail "Risk integration scope omits ${risk_scope_path}"
done
for lifecycle_operation in \
  'client.XAutoClaim' \
  'store.ReadNew' \
  'store.Ack' \
  'store.Reject' \
  'client.XRangeN'; do
  contains_fixed "${lifecycle_operation}" "${RISK_INTEGRATION_TEST}" ||
    fail "Risk Redis lifecycle test omits ${lifecycle_operation}"
done
for schema_label in \
  'com.kkai.schema.compatible-prefixes' \
  'com.kkai.schema.min-compatible' \
  'com.kkai.schema.max-compatible' \
  'com.kkai.schema.migration-target' \
  'com.kkai.schema.migration-kind' \
  'com.kkai.schema.migration-set-digest'; do
  contains_fixed "${schema_label}" "${BUILD_ROOT}/Dockerfile" ||
    fail "production image is missing ${schema_label}"
done
[[ "$(grep -c 'com\.kkai\.schema\.' "${BUILD_ROOT}/Dockerfile")" == 6 ]] ||
  fail "production image must publish exactly six com.kkai.schema labels"
contains_fixed 'com.kkai.runtime.schema-management' "${BUILD_ROOT}/Dockerfile" ||
  fail "production image is missing the external schema management label"
for workflow in "${WORKFLOW}" "${CANDIDATE_WORKFLOW}"; do
  contains_fixed 'build/kkai-image/export-schema-contract.sh postgres' "${workflow}" ||
    fail "workflow does not consume the App-owned contract exporter: ${workflow}"
  for schema_arg in \
    'SCHEMA_MANAGEMENT' \
    'SCHEMA_COMPATIBLE_PREFIXES' \
    'SCHEMA_RUNTIME_MIN_VERSION' \
    'SCHEMA_RUNTIME_MAX_VERSION' \
    'SCHEMA_MIGRATION_TARGET' \
    'SCHEMA_MIGRATION_KIND' \
    'SCHEMA_MIGRATION_SET_DIGEST'; do
    contains_fixed "${schema_arg}=" "${workflow}" ||
      fail "workflow does not pass ${schema_arg}: ${workflow}"
  done
done
schema_contract_outputs="$("${SCHEMA_CONTRACT_EXPORTER}" postgres)"
readonly schema_contract_outputs
[[ "$(wc -l <<<"${schema_contract_outputs}" | tr -d ' ')" == 7 ]] ||
  fail "schema contract exporter must emit exactly seven outputs"
for exact_output in \
  'runtime_min_version=3' \
  'runtime_max_version=4' \
  'migration_target=3' \
  'migration_kind=none' \
  'schema_management=external'; do
  grep -Fx "${exact_output}" <<<"${schema_contract_outputs}" >/dev/null ||
    fail "schema contract exporter is missing reviewed output ${exact_output}"
done
compatible_prefixes="$(sed -n 's/^compatible_prefixes=//p' <<<"${schema_contract_outputs}")"
readonly compatible_prefixes
migration_set_digest="$(sed -n 's/^migration_set_digest=//p' <<<"${schema_contract_outputs}")"
readonly migration_set_digest
[[ "${migration_set_digest}" =~ ^sha256:[0-9a-f]{64}$ ]] ||
  fail "schema contract exporter emitted an invalid migration-set digest"
jq --exit-status --arg digest "${migration_set_digest}" \
  'keys == ["3", "4"] and .["3"] == $digest and
   all(.[]; test("^sha256:[0-9a-f]{64}$"))' \
  <<<"${compatible_prefixes}" >/dev/null ||
  fail "schema contract exporter emitted an invalid PostgreSQL compatible-prefix map"
contains_fixed 'NEWAPI_REBATE_EVENT_INGEST_SECRET_FILE: /run/secrets/rebate_event_ingest_secret' \
  "${BUILD_ROOT}/smoke-compose.yml" ||
  fail "smoke runtime does not mount the rebate event ingest credential"
contains_fixed 'REBATE_EVENT_INGEST_URL: http://127.0.0.1:9/api/internal/rebate-source-events' \
  "${BUILD_ROOT}/smoke-compose.yml" ||
  fail "smoke runtime does not exercise delivery-enabled startup"
contains_fixed 'rebate-event-ingest-secret' "${BUILD_ROOT}/smoke-compose.sh" ||
  fail "smoke test does not provision the rebate event ingest credential"
contains_fixed 'delivery_disabled_stage_version' "${BUILD_ROOT}/smoke-compose.sh" ||
  fail "smoke test does not exercise delivery-disabled startup"
contains_fixed "stage_version=\"\$(delivery_disabled_stage_version)\"" "${BUILD_ROOT}/smoke-compose.sh" ||
  fail "smoke test does not assert delivery-disabled stage startup"
delivery_disabled_stage_smoke="$(
  sed -n '/^delivery_disabled_stage_version() {$/,/^}$/p' "${BUILD_ROOT}/smoke-compose.sh"
)"
[[ -n "${delivery_disabled_stage_smoke}" ]] ||
  fail "delivery-disabled stage smoke function is empty"
if grep -Eq 'REBATE_EVENT_INGEST_(URL|SECRET)' <<<"${delivery_disabled_stage_smoke}"; then
  fail "delivery-disabled stage smoke mounts or configures rebate delivery"
fi

contains_fixed 'refs/heads/production/kkrich' "${WORKFLOW}" ||
  fail "workflow is not restricted to production/kkrich"
contains_fixed '    paths-ignore:' "${WORKFLOW}" ||
  fail "workflow does not separate documentation from production releases"
contains_fixed "      - '**/*.md'" "${WORKFLOW}" ||
  fail "Markdown-only policy changes can trigger a production release"
contains_fixed 'packages: write' "${WORKFLOW}" ||
  fail "workflow cannot publish to GHCR"
contains_fixed 'sbom: true' "${WORKFLOW}" ||
  fail "workflow does not publish BuildKit SBOM provenance"
contains_fixed 'cosign sign --yes' "${WORKFLOW}" ||
  fail "workflow does not sign the immutable digest"
contains_fixed 'cosign verify --output json' "${WORKFLOW}" ||
  fail "workflow does not verify the signed immutable digest"
contains_fixed 'dist/image-signature-verification.json' "${WORKFLOW}" ||
  fail "workflow does not retain Cosign identity evidence"
contains_fixed "candidate-\${{ steps.release.outputs.version }}" "${WORKFLOW}" ||
  fail "workflow does not isolate the unscanned candidate tag"
contains_fixed 'imagetools create' "${WORKFLOW}" ||
  fail "workflow does not promote the scanned digest"
contains_fixed "go-version: '1.26.5'" "${QUALITY_WORKFLOW}" ||
  fail "fork quality does not use the production Go toolchain"
if contains_regex 'calciumion/new-api|docker\.io|ssh|ansible|workflow_run' "${WORKFLOW}"; then
  fail "production image workflow targets upstream or contains deployment behavior"
fi
if contains_regex 'uses: [^ ]+@v[0-9]' "${WORKFLOW}"; then
  fail "workflow contains an unpinned action reference"
fi

contains_fixed '## KKAI Production Delivery (Mandatory)' "${AGENT_RULES}" ||
  fail "repository agent rules omit the production delivery entrypoint"
contains_fixed 'make -C ../kkai-infra newapi-status' "${AGENT_RULES}" ||
  fail "repository agent rules omit the live production status gate"
contains_fixed 'docs/runbooks/15-newapi-automated-deployment.md' "${AGENT_RULES}" ||
  fail "repository agent rules omit the automated deployment runbook"
contains_fixed 'Application workflows must not SSH to production' "${AGENT_RULES}" ||
  fail "repository agent rules do not preserve deployment ownership"

for branch_pattern in "'rebuild/**'" "'integration/**'"; do
  contains_fixed "- ${branch_pattern}" "${CANDIDATE_WORKFLOW}" ||
    fail "candidate workflow does not build ${branch_pattern}"
done
contains_fixed 'load: true' "${CANDIDATE_WORKFLOW}" ||
  fail "candidate workflow does not load the image for smoke tests"
if contains_regex 'packages: write|push: true|cosign sign|ssh|ansible' "${CANDIDATE_WORKFLOW}"; then
  fail "candidate workflow can publish or deploy"
fi
if contains_regex 'uses: [^ ]+@v[0-9]' "${CANDIDATE_WORKFLOW}"; then
  fail "candidate workflow contains an unpinned action reference"
fi

echo "KKAI production image policy passed"
