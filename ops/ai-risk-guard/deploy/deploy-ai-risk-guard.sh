#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ARTIFACT_DIR="${ARTIFACT_DIR:-$(cd "$SCRIPT_DIR/.." && pwd)}"
TIMESTAMP="${TIMESTAMP:-$(date +%Y%m%d-%H%M%S)}"
BACKUP_ROOT="${BACKUP_ROOT:-/var/backups/ai-risk-guard}"
BACKUP_DIR="${BACKUP_DIR:-$BACKUP_ROOT/$TIMESTAMP}"

NGINX_BIN="${NGINX_BIN:-nginx}"
SYSTEMCTL_BIN="${SYSTEMCTL_BIN:-systemctl}"
RESTART_AI_RISK_GUARD="${RESTART_AI_RISK_GUARD:-0}"

TARGET_DIR="${TARGET_DIR:-/opt/ai-risk-guard}"
BIN_DIR="${BIN_DIR:-$TARGET_DIR/bin}"
RULES_DIR="${RULES_DIR:-$TARGET_DIR/rules}"
NGINX_SNIPPET_DIR="${NGINX_SNIPPET_DIR:-$TARGET_DIR/nginx}"

DAEMON_PATH="${DAEMON_PATH:-$BIN_DIR/ai-risk-guardd}"
RISKCTL_PATH="${RISKCTL_PATH:-$BIN_DIR/riskctl}"
RULES_PATH="${RULES_PATH:-$RULES_DIR/pre-risk-rules.json}"
SYSTEMD_UNIT_PATH="${SYSTEMD_UNIT_PATH:-/etc/systemd/system/ai-risk-guard.service}"
HTTP_CONF_PATH="${HTTP_CONF_PATH:-$NGINX_SNIPPET_DIR/ai-risk-guard.http.conf}"
LOCATION_CONF_PATH="${LOCATION_CONF_PATH:-$NGINX_SNIPPET_DIR/ai-risk-guard.location.conf}"
LUA_PATH="${LUA_PATH:-$NGINX_SNIPPET_DIR/ai_risk_guard_access.lua}"
README_PATH="${README_PATH:-$TARGET_DIR/README.md}"
NGINX_CONF_PATH="${NGINX_CONF_PATH:-/www/server/nginx/conf/nginx.conf}"
AI_LOCATIONS_CONF_PATH="${AI_LOCATIONS_CONF_PATH:-/opt/ai-bridge/nginx/ai-bridge.locations.conf}"
STATE_DIR="${STATE_DIR:-/var/lib/ai-risk-guard}"
EVENTS_FILE="${EVENTS_FILE:-$STATE_DIR/events.jsonl}"

DAEMON_SRC="$ARTIFACT_DIR/bin/ai-risk-guardd"
RISKCTL_SRC="$ARTIFACT_DIR/bin/riskctl"
RULES_SRC="$ARTIFACT_DIR/rules/pre-risk-rules.json"
SYSTEMD_UNIT_SRC="$ARTIFACT_DIR/systemd/ai-risk-guard.service"
HTTP_CONF_SRC="$ARTIFACT_DIR/nginx/ai-risk-guard.http.conf"
LOCATION_CONF_SRC="$ARTIFACT_DIR/nginx/ai-risk-guard.location.conf"
LUA_SRC="$ARTIFACT_DIR/nginx/ai_risk_guard_access.lua"
README_SRC="$ARTIFACT_DIR/README.md"

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

reload_nginx() {
  run_as_root "$SYSTEMCTL_BIN" reload nginx
}

require_confirm() {
  [[ "${CONFIRM_DEPLOY:-}" == "install-ai-risk-guard" ]] || {
    cat >&2 <<'MSG'
Refusing to deploy without confirmation.
Set CONFIRM_DEPLOY=install-ai-risk-guard when running on the intended target host.
MSG
    exit 2
  }
}

require_file() {
  local path="$1"
  [[ -f "$path" ]] || fail "missing artifact: $path"
}

backup_path() {
  local dest="$1"
  local backup_file="$BACKUP_DIR/files$dest"

  run_as_root mkdir -p "$(dirname "$backup_file")"
  if [[ -e "$dest" || -L "$dest" ]]; then
    run_as_root cp -a "$dest" "$backup_file"
    printf '%s\tpresent\n' "$dest" | as_root tee -a "$BACKUP_DIR/manifest.tsv" >/dev/null
  else
    printf '%s\tmissing\n' "$dest" | as_root tee -a "$BACKUP_DIR/manifest.tsv" >/dev/null
  fi
}

restore_backed_up_path() {
  local dest="$1"
  local state
  local backup_file="$BACKUP_DIR/files$dest"

  state="$(awk -F '\t' -v dest="$dest" '$1 == dest { state = $2 } END { print state }' "$BACKUP_DIR/manifest.tsv")"
  [[ -n "$state" ]] || fail "backup manifest missing entry for $dest"

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

restore_nginx_configs() {
  restore_backed_up_path "$NGINX_CONF_PATH"
  restore_backed_up_path "$AI_LOCATIONS_CONF_PATH"
}

install_file() {
  local src="$1"
  local dest="$2"
  local mode="$3"
  local owner="${4:-root}"
  local group="${5:-root}"

  run_as_root mkdir -p "$(dirname "$dest")"
  if [[ "${USE_SUDO:-1}" == "0" && "${EUID:-$(id -u)}" -ne 0 ]]; then
    run_as_root install -m "$mode" "$src" "$dest"
  else
    run_as_root install -o "$owner" -g "$group" -m "$mode" "$src" "$dest"
  fi
}

patch_nginx_confs() {
  echo "+ patch nginx includes"
  if ((${#SUDO[@]})); then
    "${SUDO[@]}" env \
      NGINX_CONF_PATH="$NGINX_CONF_PATH" \
      AI_LOCATIONS_CONF_PATH="$AI_LOCATIONS_CONF_PATH" \
      HTTP_CONF_PATH="$HTTP_CONF_PATH" \
      LOCATION_CONF_PATH="$LOCATION_CONF_PATH" \
      python3 - <<'PY'
import os
from pathlib import Path

nginx_conf = Path(os.environ["NGINX_CONF_PATH"])
ai_locations = Path(os.environ["AI_LOCATIONS_CONF_PATH"])
http_conf = os.environ["HTTP_CONF_PATH"]
location_conf = os.environ["LOCATION_CONF_PATH"]

text = nginx_conf.read_text()
http_include = f"        include {http_conf};\n"
if http_conf not in text:
    marker = '        lua_package_path "/www/server/nginx/lib/lua/?.lua;;";\n'
    if marker in text:
        text = text.replace(marker, marker + http_include, 1)
    else:
        marker = "http\n    {\n"
        if marker not in text:
            raise SystemExit("could not find nginx http block")
        text = text.replace(marker, marker + http_include, 1)
    nginx_conf.write_text(text)

lines = ai_locations.read_text().splitlines(keepends=True)
new_lines = []
for line in lines:
    if "proxy_pass http://127.0.0.1:8080;" in line:
        if not any(location_conf in previous for previous in new_lines[-8:]):
            indent = line[: len(line) - len(line.lstrip())]
            new_lines.append(f"{indent}include {location_conf};\n")
    new_lines.append(line)
new_text = "".join(new_lines)
if new_text != "".join(lines):
    ai_locations.write_text(new_text)
PY
  else
    env \
      NGINX_CONF_PATH="$NGINX_CONF_PATH" \
      AI_LOCATIONS_CONF_PATH="$AI_LOCATIONS_CONF_PATH" \
      HTTP_CONF_PATH="$HTTP_CONF_PATH" \
      LOCATION_CONF_PATH="$LOCATION_CONF_PATH" \
      python3 - <<'PY'
import os
from pathlib import Path

nginx_conf = Path(os.environ["NGINX_CONF_PATH"])
ai_locations = Path(os.environ["AI_LOCATIONS_CONF_PATH"])
http_conf = os.environ["HTTP_CONF_PATH"]
location_conf = os.environ["LOCATION_CONF_PATH"]

text = nginx_conf.read_text()
http_include = f"        include {http_conf};\n"
if http_conf not in text:
    marker = '        lua_package_path "/www/server/nginx/lib/lua/?.lua;;";\n'
    if marker in text:
        text = text.replace(marker, marker + http_include, 1)
    else:
        marker = "http\n    {\n"
        if marker not in text:
            raise SystemExit("could not find nginx http block")
        text = text.replace(marker, marker + http_include, 1)
    nginx_conf.write_text(text)

lines = ai_locations.read_text().splitlines(keepends=True)
new_lines = []
for line in lines:
    if "proxy_pass http://127.0.0.1:8080;" in line:
        if not any(location_conf in previous for previous in new_lines[-8:]):
            indent = line[: len(line) - len(line.lstrip())]
            new_lines.append(f"{indent}include {location_conf};\n")
    new_lines.append(line)
new_text = "".join(new_lines)
if new_text != "".join(lines):
    ai_locations.write_text(new_text)
PY
  fi
}

prepare_state_files() {
  run_as_root mkdir -p "$STATE_DIR"
  run_as_root chown root:root "$STATE_DIR"
  run_as_root chmod 0755 "$STATE_DIR"
  if [[ ! -e "$EVENTS_FILE" ]]; then
    run_as_root touch "$EVENTS_FILE"
  fi
  run_as_root chown www:root "$EVENTS_FILE"
  run_as_root chmod 0600 "$EVENTS_FILE"
}

main() {
  require_confirm

  require_file "$DAEMON_SRC"
  require_file "$RISKCTL_SRC"
  require_file "$RULES_SRC"
  require_file "$SYSTEMD_UNIT_SRC"
  require_file "$HTTP_CONF_SRC"
  require_file "$LOCATION_CONF_SRC"
  require_file "$LUA_SRC"
  require_file "$README_SRC"

  run_as_root mkdir -p "$BACKUP_DIR" "$BIN_DIR" "$RULES_DIR" "$NGINX_SNIPPET_DIR"
  : | as_root tee "$BACKUP_DIR/manifest.tsv" >/dev/null

  backup_path "$DAEMON_PATH"
  backup_path "$RISKCTL_PATH"
  backup_path "$RULES_PATH"
  backup_path "$SYSTEMD_UNIT_PATH"
  backup_path "$HTTP_CONF_PATH"
  backup_path "$LOCATION_CONF_PATH"
  backup_path "$LUA_PATH"
  backup_path "$README_PATH"
  backup_path "$NGINX_CONF_PATH"
  backup_path "$AI_LOCATIONS_CONF_PATH"

  install_file "$DAEMON_SRC" "$DAEMON_PATH" 0755
  install_file "$RISKCTL_SRC" "$RISKCTL_PATH" 0755
  install_file "$RULES_SRC" "$RULES_PATH" 0644
  install_file "$SYSTEMD_UNIT_SRC" "$SYSTEMD_UNIT_PATH" 0644
  install_file "$HTTP_CONF_SRC" "$HTTP_CONF_PATH" 0644
  install_file "$LOCATION_CONF_SRC" "$LOCATION_CONF_PATH" 0644
  install_file "$LUA_SRC" "$LUA_PATH" 0644
  install_file "$README_SRC" "$README_PATH" 0644
  prepare_state_files
  patch_nginx_confs

  run_as_root "$SYSTEMCTL_BIN" daemon-reload

  if ! run_as_root "$NGINX_BIN" -t; then
    echo "Nginx config test failed; restoring nginx config files from backup." >&2
    restore_nginx_configs
    run_as_root "$NGINX_BIN" -t
    fail "nginx config test failed after ai-risk-guard deploy changes"
  fi
  reload_nginx

  if [[ "$RESTART_AI_RISK_GUARD" == "1" ]]; then
    run_as_root "$SYSTEMCTL_BIN" enable --now ai-risk-guard
    run_as_root "$SYSTEMCTL_BIN" restart ai-risk-guard
  else
    echo "Skipping ai-risk-guard restart; set RESTART_AI_RISK_GUARD=1 to restart it explicitly."
  fi

  cat <<MSG

Deployment completed.
Backup dir: $BACKUP_DIR
ai-risk-guard restart: $([[ "$RESTART_AI_RISK_GUARD" == "1" ]] && printf 'performed' || printf 'skipped (set RESTART_AI_RISK_GUARD=1 to restart explicitly)')
Rollback:
  CONFIRM_ROLLBACK=rollback-ai-risk-guard BACKUP_DIR='$BACKUP_DIR' $SCRIPT_DIR/rollback-ai-risk-guard.sh
MSG
}

main "$@"
