#!/usr/bin/env bash
set -euo pipefail

NGINX_BIN="${NGINX_BIN:-nginx}"
NGINX_RELOAD_CMD="${NGINX_RELOAD_CMD:-systemctl reload nginx}"
SYSTEMCTL_BIN="${SYSTEMCTL_BIN:-systemctl}"
RESTART_AI_RISK_GUARD="${RESTART_AI_RISK_GUARD:-0}"
BACKUP_ROOT="${BACKUP_ROOT:-/var/backups/ai-risk-guard}"

if [[ -z "${BACKUP_DIR:-}" ]]; then
  if [[ -n "${TIMESTAMP:-}" ]]; then
    BACKUP_DIR="$BACKUP_ROOT/$TIMESTAMP"
  else
    echo "FAIL: BACKUP_DIR or TIMESTAMP is required" >&2
    exit 1
  fi
fi

declare -a SUDO=()
if [[ "${USE_SUDO:-1}" != "0" && "${EUID:-$(id -u)}" -ne 0 ]]; then
  SUDO=(sudo)
fi

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

run() {
  echo "+ $*"
  "$@"
}

as_root() {
  if ((${#SUDO[@]})); then
    "${SUDO[@]}" "$@"
  else
    "$@"
  fi
}

run_as_root() {
  local prefix=""
  if ((${#SUDO[@]})); then
    prefix="${SUDO[*]} "
  fi
  echo "+ ${prefix}$*"
  as_root "$@"
}

require_confirm() {
  [[ "${CONFIRM_ROLLBACK:-}" == "rollback-ai-risk-guard" ]] || {
    cat >&2 <<'MSG'
Refusing to rollback without confirmation.
Set CONFIRM_ROLLBACK=rollback-ai-risk-guard when running on the intended target host.
MSG
    exit 2
  }
}

restore_path() {
  local dest="$1"
  local state="$2"
  local backup_file="$BACKUP_DIR/files$dest"

  if [[ "$state" == "present" ]]; then
    [[ -e "$backup_file" || -L "$backup_file" ]] || fail "backup file missing for $dest"
    run_as_root mkdir -p "$(dirname "$dest")"
    run_as_root cp -a "$backup_file" "$dest"
  elif [[ "$state" == "missing" ]]; then
    run_as_root rm -f "$dest"
  else
    fail "unknown backup state '$state' for $dest"
  fi
}

main() {
  require_confirm

  local manifest="$BACKUP_DIR/manifest.tsv"
  [[ -f "$manifest" ]] || fail "missing backup manifest: $manifest"

  while IFS=$'\t' read -r dest state; do
    [[ -n "$dest" ]] || continue
    restore_path "$dest" "$state"
  done <"$manifest"

  run_as_root "$SYSTEMCTL_BIN" daemon-reload

  run_as_root "$NGINX_BIN" -t
  run_as_root bash -c "$NGINX_RELOAD_CMD"

  if [[ "$RESTART_AI_RISK_GUARD" == "1" ]]; then
    run_as_root "$SYSTEMCTL_BIN" restart ai-risk-guard
  else
    echo "Skipping ai-risk-guard restart; set RESTART_AI_RISK_GUARD=1 to restart it explicitly."
  fi

  echo "Rollback completed from $BACKUP_DIR"
}

main "$@"
