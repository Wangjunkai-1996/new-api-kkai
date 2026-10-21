#!/usr/bin/env bash
# Shared, local-only evidence. Each invocation owns a separate private sidecar.
# shellcheck source-path=SCRIPTDIR

KKAI_RECORD_TOOL="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/release-record.py"
KKAI_RECORD_PATH=''
KKAI_RECORD_UNAVAILABLE=0

kkai_record_init() {
  command -v python3 >/dev/null 2>&1 || { echo 'release-record: missing python3' >&2; return 1; }
  KKAI_RECORD_PATH="$(python3 "${KKAI_RECORD_TOOL}" init --kind "$1" --release-id "$2" --source-sha "$3" --directory "$4")" || return 1
  printf 'RELEASE_RECORD=%s\n' "${KKAI_RECORD_PATH}" >&2
}

kkai_record_update() {
  [[ ${KKAI_RECORD_UNAVAILABLE} == 0 ]] || return 0
  if ! python3 "${KKAI_RECORD_TOOL}" "$1" "${KKAI_RECORD_PATH}" "${@:2}"; then
    KKAI_RECORD_UNAVAILABLE=1
    printf '%s\n' 'release-record: timing evidence incomplete; preserving the original command result; inspect official state before recovery' >&2
  fi
}

kkai_record_identity() {
  kkai_record_update identity "$1" "$2"
}

kkai_record_phase() {
  kkai_record_update phase "$1"
}

kkai_record_phase_end() {
  kkai_record_update phase-end
}

kkai_record_finish() {
  [[ -n ${KKAI_RECORD_PATH} ]] || return 0
  kkai_record_update finish "$1"
}
