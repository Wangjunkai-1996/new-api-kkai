#!/usr/bin/env bash
set -Eeuo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly root

fail() {
  echo "FRT header patch check failed: $1" >&2
  exit 1
}

require_match() {
  local pattern="$1"
  local path="$2"
  local description="$3"
  grep -Eq -- "${pattern}" "${root}/${path}" || fail "${description} (${path})"
}

require_match 'UpstreamHeaderTime[[:space:]]+time\.Time' \
  relay/common/relay_info.go 'RelayInfo.UpstreamHeaderTime is missing'
require_match 'func \(info \*RelayInfo\) SetUpstreamHeaderTime\(\)' \
  relay/common/relay_info.go 'RelayInfo.SetUpstreamHeaderTime is missing'
require_match 'info\.SetUpstreamHeaderTime\(\)' \
  relay/channel/api_request.go 'upstream header timestamp is not recorded'
require_match 'UpstreamHeaderTime\.Sub\(relayInfo\.StartTime\)\.Milliseconds\(\)' \
  service/log_info_generate.go 'displayed FRT does not use upstream header time'
require_match 'other\["first_sse_ms"\]' \
  service/log_info_generate.go 'first SSE latency is not retained'
require_match 'TestGenerateTextOtherInfoUsesUpstreamHeaderTimeForDisplayedFRT' \
  service/log_info_generate_test.go 'header-time regression coverage is missing'
require_match 'TestGenerateTextOtherInfoFallsBackToFirstSSEWhenHeaderTimeMissing' \
  service/log_info_generate_test.go 'fallback regression coverage is missing'
require_match 'TestDoRequestRecordsUpstreamHeaderTime' \
  relay/channel/api_request_test.go 'HTTP response timing regression coverage is missing'
require_match 'TestGenerateTextOtherInfoOmitsInvalidResponseTimings' \
  service/log_info_generate_test.go 'invalid-timing regression coverage is missing'

echo 'FRT header patch check passed'
