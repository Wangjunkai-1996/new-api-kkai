#!/bin/sh
set -eu

fail() {
  echo "FFmpeg static verification: $*" >&2
  exit 1
}

[ "$#" -gt 0 ] || fail "no media binaries were provided"

scanelf_path="$(command -v scanelf 2>/dev/null || true)"
[ -n "${scanelf_path}" ] || fail "scanelf is unavailable"

needed_status=0
needed_output="$("${scanelf_path}" -q -n "$@")" || needed_status=$?
[ "${needed_status}" -eq 0 ] || fail "scanelf NEEDED inspection failed with status ${needed_status}"
[ -z "${needed_output}" ] || fail "media tools have dynamic dependencies"

interp_status=0
interp_output="$("${scanelf_path}" -q -i "$@")" || interp_status=$?
[ "${interp_status}" -eq 0 ] || fail "scanelf INTERP inspection failed with status ${interp_status}"
[ -z "${interp_output}" ] || fail "media tools have a dynamic interpreter"
