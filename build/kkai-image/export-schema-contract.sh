#!/usr/bin/env bash
set -Eeuo pipefail

readonly dialect="${1:?usage: $0 DIALECT}"
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly root
cd "${root}"

contract="$(
  go run \
    -ldflags '-X github.com/QuantumNous/new-api/common.SchemaManagementMode=external' \
    ./cmd/kkai-migrate --describe-contract --dialect "${dialect}" --json
)"
readonly contract

jq --exit-status '
  (.runtime_min_version | tostring) as $minimum_prefix |
  (.runtime_max_version | tostring) as $maximum_prefix |
  keys == [
    "compatible_prefixes",
    "migration_kind",
    "migration_set_digest",
    "migration_target_version",
    "runtime_max_version",
    "runtime_min_version",
    "schema_management"
  ] and
  ([.runtime_min_version, .runtime_max_version, .migration_target_version] |
    all(type == "number" and . > 0 and floor == .)) and
  .runtime_min_version <= .migration_target_version and
  .migration_target_version <= .runtime_max_version and
  .schema_management == "external" and
  (.migration_kind == "none" or .migration_kind == "expand") and
  (.migration_set_digest | test("^sha256:[0-9a-f]{64}$")) and
  (.compatible_prefixes |
    type == "object" and has($minimum_prefix) and has($maximum_prefix) and
    all(.[]; test("^sha256:[0-9a-f]{64}$")))
' <<<"${contract}" >/dev/null

jq --raw-output '
  "compatible_prefixes=\(.compatible_prefixes|tojson)",
  "runtime_min_version=\(.runtime_min_version)",
  "runtime_max_version=\(.runtime_max_version)",
  "migration_target=\(.migration_target_version)",
  "migration_kind=\(.migration_kind)",
  "migration_set_digest=\(.migration_set_digest)",
  "schema_management=\(.schema_management)"
' <<<"${contract}"
