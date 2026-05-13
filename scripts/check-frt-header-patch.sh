#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'FRT header patch check failed: %s\n' "$1" >&2
  exit 1
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

require_file() {
  local path="$1"
  [[ -f "$path" ]] || fail "missing required file: $path"
}

require_grep() {
  local pattern="$1"
  local path="$2"
  local description="$3"

  if ! grep -Eq "$pattern" "$path"; then
    fail "$description ($path)"
  fi
}

require_file relay/common/relay_info.go
require_file relay/channel/api_request.go
require_file service/log_info_generate.go
require_file service/log_info_generate_test.go

require_grep 'UpstreamHeaderTime[[:space:]]+time\.Time' relay/common/relay_info.go 'RelayInfo.UpstreamHeaderTime is missing'
require_grep 'func \(info \*RelayInfo\) SetUpstreamHeaderTime\(\)' relay/common/relay_info.go 'RelayInfo.SetUpstreamHeaderTime is missing'
require_grep 'info\.SetUpstreamHeaderTime\(\)' relay/channel/api_request.go 'upstream header timestamp is not recorded after client.Do'
require_grep 'firstResponseDisplayMs' service/log_info_generate.go 'display FRT helper is missing'
require_grep 'UpstreamHeaderTime\.Sub\(relayInfo\.StartTime\)\.Milliseconds\(\)' service/log_info_generate.go 'display FRT does not use upstream header time'
require_grep 'other\["first_sse_ms"\]' service/log_info_generate.go 'first_sse_ms is not written to log other JSON'
require_grep 'TestGenerateTextOtherInfoUsesUpstreamHeaderTimeForDisplayedFRT' service/log_info_generate_test.go 'header-time FRT regression test is missing'
require_grep 'TestGenerateTextOtherInfoFallsBackToFirstSSEWhenHeaderTimeMissing' service/log_info_generate_test.go 'fallback regression test is missing'
require_grep 'TestGenerateTextOtherInfoOmitsFirstSSEWhenNoResponseWasReceived' service/log_info_generate_test.go 'first_sse_ms omission regression test is missing'

printf 'FRT header patch check passed.\n'
