#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

usage() {
  cat <<'EOF'
Usage:
  deploy_all_maclinux.sh [full|hubcenter-only|hub-only] [--no-check] [--clean-hubcenter-db] [--brand rapidai|tigerclaw] [--skip-targets hc-3,hubs2.maclaw.top,hub2.maclaw.top]

  --clean-hubcenter-db  Backup and rebuild remote HubCenter SQLite DB without ha_sync_ops/ha_applied_ops.

Environment:
  REMOTE_USER=root
  REMOTE_PORT=22
  REMOTE_PASS=...              Optional. Requires sshpass when set.
  CLUSTER_SECRET=...           Optional. Existing/generated secret used when empty.
  DEPLOY_SKIP_TARGETS=...
  DEPLOY_GOOS=linux
  DEPLOY_GOARCH=amd64
  DEPLOY_GO_BUILD_P=1
  DEPLOY_GO_BUILD_RETRIES=3
  DEPLOY_HOST_HC_1=...
  DEPLOY_HOST_HC_2=...
  DEPLOY_HOST_HC_3=...
  REMOTE_TMP_DIR_HC_1=...
  REMOTE_HUB_DIR_HC_1=...
  REMOTE_HUBCENTER_DIR_HC_1=...
EOF
}

die() {
  echo "[ERROR] $*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || die "Required tool not found: $1"
}

yaml_quote() {
  local value="${1-}"
  value="${value//\'/\'\'}"
  printf "'%s'" "$value"
}

normalize_url() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  while [[ "$value" == */ ]]; do value="${value%/}"; done
  printf '%s' "$value"
}

host_from_url() {
  local value="$1"
  value="$(normalize_url "$value")"
  if [[ "$value" =~ ^[A-Za-z][A-Za-z0-9+.-]*://([^/:]+) ]]; then
    printf '%s' "${BASH_REMATCH[1],,}"
  else
    value="${value#/}"
    value="${value%%/*}"
    printf '%s' "${value,,}"
  fi
}

shell_quote() {
  printf "%q" "$1"
}

join_by() {
  local sep="$1"
  shift || true
  local out=""
  local item
  for item in "$@"; do
    if [[ -z "$out" ]]; then
      out="$item"
    else
      out+="$sep$item"
    fi
  done
  printf '%s' "$out"
}

rand_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 48 | tr -d '='
  else
    LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 64
    echo
  fi
}

target_suffix() {
  local name="$1"
  name="${name//[^A-Za-z0-9]/_}"
  printf '%s' "$name" | tr '[:lower:]' '[:upper:]'
}

target_setting() {
  local base="$1" node="$2" default="$3" suffix scoped
  suffix="$(target_suffix "$node")"
  scoped="${base}_${suffix}"
  printf '%s' "${!scoped:-${!base:-$default}}"
}

scope="full"
brand="rapidai"
skip_targets_arg=""
no_check=0
clean_hubcenter_db=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help|/h|"/?")
      usage
      exit 0
      ;;
    full|hubcenter-only|hub-only)
      scope="$1"
      shift
      ;;
    --no-check|-NoCheck)
      no_check=1
      shift
      ;;
    --clean-hubcenter-db)
      clean_hubcenter_db=1
      shift
      ;;
    --brand)
      [[ $# -ge 2 && -n "$2" ]] || die "Missing value for --brand"
      brand="$2"
      shift 2
      ;;
    --skip-targets)
      [[ $# -ge 2 && -n "$2" ]] || die "Missing value for --skip-targets"
      skip_targets_arg="$2"
      shift 2
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
done

[[ "$brand" == "rapidai" || "$brand" == "tigerclaw" ]] || die "--brand must be rapidai or tigerclaw"
[[ "$scope" != "hub-only" || "$clean_hubcenter_db" != "1" ]] || die "--clean-hubcenter-db cannot be used with hub-only."

need go
need tar
need ssh
need scp
need curl
need rsync

if [[ -n "${REMOTE_PASS:-}" ]]; then
  need sshpass
fi

[[ -f "$ROOT_DIR/go.mod" ]] || die "Missing go.mod"
[[ -f "$ROOT_DIR/go.sum" ]] || die "Missing go.sum"
[[ -d "$ROOT_DIR/hub/cmd/hub" ]] || die "Missing hub source."
[[ -d "$ROOT_DIR/hubcenter/cmd/hubcenter" ]] || die "Missing hubcenter source."

REMOTE_USER="${REMOTE_USER:-root}"
REMOTE_PORT="${REMOTE_PORT:-22}"
HUB_MODEL_BASE_URL="${HUB_MODEL_BASE_URL:-https://github.com/RapidAI/MaClaw/releases/download/Model_Release}"
HUB_MODEL_FILES="${HUB_MODEL_FILES:-embeddinggemma-300M-Q8_0.gguf moonshine-base-zh.gguf omniparser-v2.yolow kokoro-v1_0.koro kokoro_82m_selected_voices_koro.zip}"

brand_build_tag=""
hub_binary_name="maclaw-hub"
hubcenter_binary_name="maclaw-hubcenter"
if [[ "$brand" == "tigerclaw" ]]; then
  brand_build_tag="oem_qianxin"
  hub_binary_name="tigerclaw-hub"
  hubcenter_binary_name="tigerclaw-hubcenter"
fi

TARGET_NAMES=("hc-1" "hc-2" "hc-3")
TARGET_HOSTS=(
  "$(target_setting DEPLOY_HOST hc-1 hubs.mypapers.top)"
  "$(target_setting DEPLOY_HOST hc-2 hubs.maclaw.top)"
  "$(target_setting DEPLOY_HOST hc-3 hubs2.maclaw.top)"
)
TARGET_TMP_DIRS=(
  "$(target_setting REMOTE_TMP_DIR hc-1 /tmp/aicoder_deploy)"
  "$(target_setting REMOTE_TMP_DIR hc-2 /tmp/aicoder_deploy)"
  "$(target_setting REMOTE_TMP_DIR hc-3 /tmp/aicoder_deploy)"
)
TARGET_HUB_DIRS=(
  "$(target_setting REMOTE_HUB_DIR hc-1 /data/soft/hub)"
  "$(target_setting REMOTE_HUB_DIR hc-2 /data/soft/hub)"
  "$(target_setting REMOTE_HUB_DIR hc-3 /data/soft/hub)"
)
TARGET_HUBCENTER_DIRS=(
  "$(target_setting REMOTE_HUBCENTER_DIR hc-1 /data/soft/hubcenter)"
  "$(target_setting REMOTE_HUBCENTER_DIR hc-2 /data/soft/hubcenter)"
  "$(target_setting REMOTE_HUBCENTER_DIR hc-3 /data/soft/hubcenter)"
)
TARGET_HUBCENTER_CONFIGS=("hubcenter-hc-1.yaml" "hubcenter-hc-2.yaml" "hubcenter-hc-3.yaml")
TARGET_HUB_CONFIGS=("hub-mypapers.yaml" "hub-maclaw.yaml" "hub2-maclaw.yaml")
TARGET_HUB_PUBLIC_URLS=("https://hub.mypapers.top" "https://hub.maclaw.top" "https://hub2.maclaw.top")
TARGET_NGINX_CONFIGS=("" "/etc/nginx/conf.d/maclaw.top.conf" "")
TARGET_NGINX_SERVER_NAMES=("" "hub.maclaw.top" "")
TARGET_NGINX_PROXY_PORTS=("" "9399" "")
TARGET_DEPLOY_HUBCENTER=(1 1 1)
TARGET_DEPLOY_HUB=(1 1 1)

if [[ "$scope" == "hubcenter-only" ]]; then
  TARGET_DEPLOY_HUB=(0 0 0)
  TARGET_HUB_CONFIGS=("" "" "")
  TARGET_HUB_PUBLIC_URLS=("" "" "")
elif [[ "$scope" == "hub-only" ]]; then
  TARGET_DEPLOY_HUBCENTER=(0 0 0)
  TARGET_HUBCENTER_CONFIGS=("" "" "")
fi

skip_targets="${skip_targets_arg:-${DEPLOY_SKIP_TARGETS:-}}"
SELECTED=()
if [[ -n "$skip_targets" ]]; then
  normalized_skip="$(printf '%s' "$skip_targets" | tr ',;' '  ' | tr '[:upper:]' '[:lower:]')"
  for i in "${!TARGET_NAMES[@]}"; do
    name="${TARGET_NAMES[$i],,}"
    host="${TARGET_HOSTS[$i],,}"
    hub_host=""
    [[ -n "${TARGET_HUB_PUBLIC_URLS[$i]}" ]] && hub_host="$(host_from_url "${TARGET_HUB_PUBLIC_URLS[$i]}")"
    skip=0
    for token in $normalized_skip; do
      if [[ "$token" == "$name" || "$token" == "$host" || "$token" == "$hub_host" ]]; then
        skip=1
      fi
    done
    [[ "$skip" == "0" ]] && SELECTED+=("$i")
  done
else
  SELECTED=(0 1 2)
fi
[[ "${#SELECTED[@]}" -gt 0 ]] || die "DEPLOY_SKIP_TARGETS excluded all deployment targets."

should_build_hub=0
should_build_hubcenter=0
for i in "${SELECTED[@]}"; do
  [[ "${TARGET_DEPLOY_HUB[$i]}" == "1" ]] && should_build_hub=1
  [[ "${TARGET_DEPLOY_HUBCENTER[$i]}" == "1" ]] && should_build_hubcenter=1
done

ssh_cmd() {
  local host="$1"; shift
  if [[ -n "${REMOTE_PASS:-}" ]]; then
    sshpass -p "$REMOTE_PASS" ssh -p "$REMOTE_PORT" -o StrictHostKeyChecking=accept-new -o PreferredAuthentications=password -o PubkeyAuthentication=no "$REMOTE_USER@$host" "$@"
  else
    ssh -p "$REMOTE_PORT" -o StrictHostKeyChecking=accept-new "$REMOTE_USER@$host" "$@"
  fi
}

scp_upload() {
  local host="$1" local_path="$2" remote_path="$3"
  if [[ -n "${REMOTE_PASS:-}" ]]; then
    sshpass -p "$REMOTE_PASS" scp -P "$REMOTE_PORT" -o StrictHostKeyChecking=accept-new -o PreferredAuthentications=password -o PubkeyAuthentication=no "$local_path" "$REMOTE_USER@$host:$remote_path"
  else
    scp -P "$REMOTE_PORT" -o StrictHostKeyChecking=accept-new "$local_path" "$REMOTE_USER@$host:$remote_path"
  fi
}

remote_precheck() {
  local i="$1" host="${TARGET_HOSTS[$i]}" tmp="${TARGET_TMP_DIRS[$i]}" hub="${TARGET_HUB_DIRS[$i]}" hc="${TARGET_HUBCENTER_DIRS[$i]}"
  local deploy_hub="${TARGET_DEPLOY_HUB[$i]}" deploy_hc="${TARGET_DEPLOY_HUBCENTER[$i]}"
  local cmd="set -eu; PATH=\"\$PATH:/usr/local/go/bin:/root/go/bin\"; export PATH; command -v sh >/dev/null; command -v tar >/dev/null; mkdir -p $(shell_quote "$tmp")"
  [[ "$deploy_hub" == "1" ]] && cmd+=" $(shell_quote "$hub")"
  [[ "$deploy_hc" == "1" ]] && cmd+=" $(shell_quote "$hc")"
  cmd+="; [ -w $(shell_quote "$tmp") ]"
  [[ "$deploy_hub" == "1" ]] && cmd+="; [ -w $(shell_quote "$hub") ]; command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1"
  [[ "$deploy_hc" == "1" ]] && cmd+="; [ -w $(shell_quote "$hc") ]"
  cmd+="; echo precheck:ok"
  ssh_cmd "$host" "$cmd" | grep -q 'precheck:ok' || die "Remote precheck failed on $host"
}

render_configs() {
  local out="$1" secret="$2"
  mkdir -p "$out"

  local centers_public=("https://hubs.mypapers.top" "https://hubs.maclaw.top" "https://hubs2.maclaw.top")
  local center_names=("hubcenter-1" "hubcenter-2" "hubcenter-3")
  local hub_public=("https://hub.mypapers.top" "https://hub.maclaw.top" "https://hub2.maclaw.top")
  local hub_primary=("https://hubs.mypapers.top" "https://hubs.maclaw.top" "https://hubs2.maclaw.top")
  local hub_files=("hub-mypapers.yaml" "hub-maclaw.yaml" "hub2-maclaw.yaml")
  local corp_domains=("rapidai.tech" "" "")
  local corp_domain_lists=("rapidai.tech qianxin.com" "" "")
  local public_signup=("false" "true" "true")

  for i in 0 1 2; do
    cat >"$out/hubcenter-${TARGET_NAMES[$i]}.yaml" <<EOF
server:
  listen_host: 0.0.0.0
  listen_port: 9388
  public_base_url: $(normalize_url "${centers_public[$i]}")

ha:
  enabled: true
  self_fqdn: $(host_from_url "${centers_public[$i]}")
  private_key_path: ./data/ha_node_key.pem
  cluster_secret: $secret
  sync_interval_seconds: 180
  push_debounce_seconds: 180
  pull_batch_size: 200
  heartbeat_sync_min_interval_seconds: 600
  history_retention_days: 0.5
  history_max_retained_ops: 50000
  history_prune_interval_minutes: 10
  history_prune_batch_size: 20000
  nodes:
EOF
    for n in 0 1 2; do
      cat >>"$out/hubcenter-${TARGET_NAMES[$i]}.yaml" <<EOF
    - fqdn: $(host_from_url "${centers_public[$n]}")
      node_id: ${TARGET_NAMES[$n]}
      node_name: ${center_names[$n]}
      advertise_url: $(normalize_url "${centers_public[$n]}")
      enabled: true
EOF
    done
    cat >>"$out/hubcenter-${TARGET_NAMES[$i]}.yaml" <<EOF

database:
  driver: sqlite
  dsn: $(yaml_quote './data/codeclaw-hubcenter.db')
  wal: true
  busy_timeout_ms: 10000
  max_read_open_conns: 4
  max_read_idle_conns: 4
  max_write_open_conns: 1
  max_write_idle_conns: 1

mail:
  enabled: false
  provider: smtp
  smtp_host: smtp.example.com
  smtp_port: 587
  smtp_username: no-reply@example.com
  smtp_password: change-me
  from_name: MaClaw Hub Center
  from_email: no-reply@example.com

logging:
  level: info
  dir: ./data/logs
EOF
  done

  for i in 0 1 2; do
    cat >"$out/${hub_files[$i]}" <<EOF
server:
  listen_host: 0.0.0.0
  listen_port: 9399
  public_base_url: $(normalize_url "${hub_public[$i]}")

database:
  driver: sqlite
  dsn: $(yaml_quote './data/codeclaw-hub.db')
  wal: true
  busy_timeout_ms: 5000
  max_read_open_conns: 8
  max_read_idle_conns: 4
  max_write_open_conns: 1
  max_write_idle_conns: 1

identity:
  enrollment_mode: open
  allow_self_enroll: true

pwa:
  static_dir: ./web/dist
  route_prefix: /app

center:
  enabled: true
  base_url: $(normalize_url "${hub_primary[$i]}")
  base_urls:
    - https://hubs.mypapers.top
    - https://hubs.maclaw.top
    - https://hubs2.maclaw.top
  register_on_startup: true
  heartbeat_interval_sec: 30

hub:
  name: $(yaml_quote 'MaClaw Hub')
  description: $(yaml_quote 'Self-hosted MaClaw remote hub')
  visibility: $(yaml_quote 'shared')
  corporate_email_domain: $(yaml_quote "${corp_domains[$i]}")
EOF
    if [[ -n "${corp_domain_lists[$i]}" ]]; then
      echo "  corporate_email_domains:" >>"$out/${hub_files[$i]}"
      for domain in ${corp_domain_lists[$i]}; do
        echo "    - $(yaml_quote "$domain")" >>"$out/${hub_files[$i]}"
      done
    fi
    cat >>"$out/${hub_files[$i]}" <<EOF
  accept_public_signup: ${public_signup[$i]}

mail:
  enabled: false
  provider: smtp
  smtp_host: smtp.example.com
  smtp_port: 587
  smtp_username: no-reply@example.com
  smtp_password: change-me
  from_name: MaClaw Hub
  from_email: no-reply@example.com

logging:
  level: info
  dir: ./data/logs
EOF
  done
}

stage_assets() {
  local stage="$1"
  mkdir -p "$stage"
  cp -f "$ROOT_DIR/go.mod" "$ROOT_DIR/go.sum" "$stage/"
  rsync -a --exclude bin --exclude package --exclude data --exclude .gocache --exclude .gomodcache --exclude cmd --exclude internal --exclude '*.exe' --exclude '*.exe~' "$ROOT_DIR/hubcenter/" "$stage/hubcenter/"
  rsync -a --exclude bin --exclude package --exclude data --exclude .gocache --exclude .gomodcache --exclude cmd --exclude internal --exclude '*.exe' --exclude '*.exe~' "$ROOT_DIR/hub/" "$stage/hub/"
  rsync -a --exclude node_modules --exclude dist "$ROOT_DIR/openclaw-bridge/" "$stage/openclaw-bridge/" 2>/dev/null || true

  [[ -d "$stage/hubcenter/web/admin" ]] || die "Missing deploy directory: hubcenter admin web assets"
  [[ -f "$stage/hubcenter/web/admin/assets/js/admin-core.js" ]] || die "Missing deploy payload: hubcenter admin core script"
  [[ -d "$stage/hub/web/admin" ]] || die "Missing deploy directory: hub admin web assets"
  [[ -d "$stage/hub/web/dist" ]] || die "Missing deploy directory: hub pwa web dist"
  [[ -d "$stage/hub/web/card_store" ]] || die "Missing deploy directory: hub card store web assets"
  [[ -f "$stage/hub/web/card_store/index.html" ]] || die "Missing deploy payload: hub card store index"
  [[ -f "$stage/hub/web/card_store/professional.css" ]] || die "Missing deploy payload: hub card store stylesheet"
}

build_binaries() {
  local stage="$1" goos="${DEPLOY_GOOS:-linux}" goarch="${DEPLOY_GOARCH:-amd64}" cgo="${CGO_ENABLED:-0}"
  local goproxy="${GOPROXY:-https://goproxy.cn,direct}" parallel="${DEPLOY_GO_BUILD_P:-1}" retries="${DEPLOY_GO_BUILD_RETRIES:-3}"
  local gocache="$BUILD_ROOT/.gocache"
  mkdir -p "$stage/bin" "$gocache"
  echo "  - target: $goos/$goarch, CGO_ENABLED=$cgo"

  run_go_build() {
    local label="$1" output="$2" pkg="$3" attempt
    local args=(build -p "$parallel")
    [[ -n "$brand_build_tag" ]] && args+=(-tags "$brand_build_tag")
    args+=(-o "$output" "$pkg")
    for ((attempt=1; attempt<=retries; attempt++)); do
      [[ "$attempt" -gt 1 ]] && { echo "  - retrying $label build ($attempt/$retries)..."; sleep 3; }
      if (cd "$ROOT_DIR" && GOOS="$goos" GOARCH="$goarch" CGO_ENABLED="$cgo" GOPROXY="$goproxy" GOCACHE="$gocache" go "${args[@]}"); then
        return 0
      fi
    done
    die "Local $label build failed."
  }

  if [[ "$should_build_hubcenter" == "1" ]]; then
    echo "  - building hubcenter locally..."
    run_go_build hubcenter "$stage/bin/$hubcenter_binary_name" ./hubcenter/cmd/hubcenter
  fi
  if [[ "$should_build_hub" == "1" ]]; then
    echo "  - building hub locally..."
    run_go_build hub "$stage/bin/$hub_binary_name" ./hub/cmd/hub
  fi
}

write_remote_script() {
  local path="$1"
  cat >"$path" <<'REMOTE_EOF'
#!/bin/sh
set -eu

: "${REMOTE_TMP_DIR:=/tmp/aicoder_deploy}"
: "${REMOTE_HUB_DIR:=/data/soft/hub}"
: "${REMOTE_HUBCENTER_DIR:=/data/soft/hubcenter}"
: "${DEPLOY_HUBCENTER:=1}"
: "${DEPLOY_HUB:=0}"
: "${ENSURE_HUB_MODELS:=0}"
: "${HUB_MODEL_BASE_URL:=https://github.com/RapidAI/MaClaw/releases/download/Model_Release}"
: "${HUB_MODEL_FILES:=embeddinggemma-300M-Q8_0.gguf moonshine-base-zh.gguf omniparser-v2.yolow kokoro-v1_0.koro kokoro_82m_selected_voices_koro.zip}"
: "${HUB_CONFIG_BASENAME:=}"
: "${HUBCENTER_CONFIG_BASENAME:=hubcenter-config.yaml}"
: "${HUB_BINARY_NAME:=maclaw-hub}"
: "${HUBCENTER_BINARY_NAME:=maclaw-hubcenter}"
: "${CLEAN_HUBCENTER_DB:=0}"
: "${HUBCENTER_DB_PATH:=./data/codeclaw-hubcenter.db}"

PATH="$PATH:/usr/local/go/bin:/root/go/bin"
export PATH

SRC_ROOT="$REMOTE_TMP_DIR/src"
ARCHIVE_PATH="$REMOTE_TMP_DIR/maclaw-deploy.tar.gz"
HUB_DATA_DIR="$REMOTE_HUB_DIR/data"
HUB_MODELS_DIR="$HUB_DATA_DIR/models"
HOME_MODELS_DIR="$HOME/.maclaw/models"
MODEL_SENTINEL="$HUB_MODELS_DIR/.models-initialized"
MODEL_LOCK="$HUB_MODELS_DIR/.models-downloading"
MODEL_SCRIPT="$HUB_DATA_DIR/download-models.sh"
MODEL_LOG="$HUB_DATA_DIR/logs/model-download.log"

rm -rf "$SRC_ROOT"
mkdir -p "$SRC_ROOT"
tar -xzf "$ARCHIVE_PATH" -C "$SRC_ROOT"
cd "$SRC_ROOT"

backup_and_write_config() {
  target_path="$1"
  source_path="$2"
  [ -f "$source_path" ] || { echo "[ERROR] Missing config payload: $source_path" >&2; exit 1; }
  mkdir -p "$(dirname "$target_path")"
  [ -f "$target_path" ] && cp -f "$target_path" "$target_path.bak"
  cp -f "$source_path" "$target_path"
}

stop_hubcenter_process() {
  for file in "$REMOTE_HUBCENTER_DIR/data/$HUBCENTER_BINARY_NAME.pid" "$REMOTE_HUBCENTER_DIR/data/maclaw-hubcenter.pid"; do
    if [ -f "$file" ]; then
      old_pid=$(cat "$file" 2>/dev/null || true)
      if [ -n "${old_pid:-}" ] && kill -0 "$old_pid" 2>/dev/null; then
        echo "[remote] Stopping hubcenter process: $old_pid"
        kill "$old_pid" 2>/dev/null || true
        sleep 2
        kill -0 "$old_pid" 2>/dev/null && kill -9 "$old_pid" 2>/dev/null || true
      fi
      rm -f "$file"
    fi
  done
  ps -eo pid=,args= | awk -v dir="$REMOTE_HUBCENTER_DIR/" 'index($0, dir) && ($0 ~ /maclaw-hubcenter/ || $0 ~ /tigerclaw-hubcenter/) { print $1 }' | while read -r pid; do
    [ -n "${pid:-}" ] && [ "$pid" != "$$" ] || continue
    kill "$pid" 2>/dev/null || true
    sleep 1
    kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null || true
  done
}

clean_hubcenter_db_if_requested() {
  [ "$CLEAN_HUBCENTER_DB" = "1" ] || return 0
  command -v sqlite3 >/dev/null 2>&1 || { echo "[ERROR] sqlite3 is required for --clean-hubcenter-db" >&2; exit 1; }
  case "$HUBCENTER_DB_PATH" in
    /*) db_path="$HUBCENTER_DB_PATH" ;;
    *) db_path="$REMOTE_HUBCENTER_DIR/$HUBCENTER_DB_PATH" ;;
  esac
  [ -f "$db_path" ] || { echo "[remote] HubCenter DB not found, skip rebuild: $db_path"; return 0; }
  db_dir=$(dirname "$db_path")
  ts=$(date +%Y%m%d_%H%M%S)
  backup_path="$db_path.bak.$ts"
  dump_path="$db_dir/hubcenter_clean.$ts.sql"
  new_path="$db_path.clean.$ts"
  echo "[remote] Rebuilding HubCenter DB without HA history: $db_path"
  stop_hubcenter_process
  cp -f "$db_path" "$backup_path"
  sqlite3 "$db_path" '.dump' | grep -v -E 'ha_sync_ops|ha_applied_ops' > "$dump_path"
  sqlite3 "$new_path" < "$dump_path"
  mv -f "$db_path" "$db_path.old.$ts"
  mv -f "$new_path" "$db_path"
  rm -f "$dump_path" "$db_path-shm" "$db_path-wal"
  echo "[remote] HubCenter DB rebuilt. Backup: $backup_path"
}

is_allowed_model_file() {
  case "$1" in ""|*/*|*\\*|*..*) return 1 ;; *.gguf|*.yolow|*.koro|*.zip) return 0 ;; *) return 1 ;; esac
}

seed_home_models() {
  mkdir -p "$HOME_MODELS_DIR"
  for name in $HUB_MODEL_FILES; do
    is_allowed_model_file "$name" || continue
    [ -f "$HUB_MODELS_DIR/$name" ] && [ ! -f "$HOME_MODELS_DIR/$name" ] && cp -f "$HUB_MODELS_DIR/$name" "$HOME_MODELS_DIR/$name"
  done
}

write_model_download_script() {
  mkdir -p "$HUB_MODELS_DIR" "$HUB_DATA_DIR/logs"
  cat > "$MODEL_SCRIPT" <<'MODELEOF'
#!/bin/sh
set -eu
BASE_URL="$1"; TARGET_DIR="$2"; HOME_DIR="$3"; SENTINEL="$4"; LOCK_FILE="$5"; shift 5
mkdir -p "$TARGET_DIR" "$HOME_DIR"
cleanup() { rm -f "$LOCK_FILE"; }
trap cleanup EXIT INT TERM
download_one() {
  name="$1"; url="$BASE_URL/$name"; target="$TARGET_DIR/$name"; tmp="$target.part"
  [ -f "$target" ] && { cp -f "$target" "$HOME_DIR/$name"; return 0; }
  rm -f "$tmp"
  if command -v curl >/dev/null 2>&1; then
    curl -L --fail --retry 3 --retry-delay 2 --connect-timeout 15 -o "$tmp" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget --tries=3 --timeout=30 -O "$tmp" "$url"
  else
    echo "[ERROR] Neither curl nor wget is available" >&2; exit 1
  fi
  mv -f "$tmp" "$target"; cp -f "$target" "$HOME_DIR/$name"
}
for file in "$@"; do
  case "$file" in ""|*/*|*\\*|*..*) echo "[WARN] Skip unsafe model filename: $file" >&2; continue ;; esac
  download_one "$file"
done
touch "$SENTINEL"
MODELEOF
  chmod +x "$MODEL_SCRIPT"
}

hub_models_missing() {
  valid=0
  for name in $HUB_MODEL_FILES; do
    is_allowed_model_file "$name" || continue
    valid=$((valid + 1))
    [ ! -f "$HUB_MODELS_DIR/$name" ] && return 0
  done
  [ "$valid" -eq 0 ] && return 0
  return 1
}

ensure_hub_models() {
  mkdir -p "$HUB_MODELS_DIR" "$HUB_DATA_DIR/logs"
  if ! hub_models_missing; then
    touch "$MODEL_SENTINEL"; seed_home_models; return 0
  fi
  rm -f "$MODEL_SENTINEL"
  filtered=""
  for name in $HUB_MODEL_FILES; do
    is_allowed_model_file "$name" && filtered="$filtered $name"
  done
  filtered=$(printf "%s" "$filtered" | xargs)
  [ -n "$filtered" ] || { echo "[remote] No valid hub model files configured." >&2; return 1; }
  [ -f "$MODEL_LOCK" ] && { echo "[remote] Hub model download already in progress."; return 0; }
  write_model_download_script
  touch "$MODEL_LOCK"
  nohup "$MODEL_SCRIPT" "$HUB_MODEL_BASE_URL" "$HUB_MODELS_DIR" "$HOME_MODELS_DIR" "$MODEL_SENTINEL" "$MODEL_LOCK" $filtered >> "$MODEL_LOG" 2>&1 &
  echo "[remote] Hub model download started in background: $MODEL_LOG"
}

deploy_hubcenter() {
  mkdir -p "$REMOTE_HUBCENTER_DIR" "$REMOTE_HUBCENTER_DIR/configs" "$REMOTE_HUBCENTER_DIR/data" "$REMOTE_HUBCENTER_DIR/data/logs"
  [ -f "$SRC_ROOT/bin/$HUBCENTER_BINARY_NAME" ] || { echo "[ERROR] Missing hubcenter binary: $SRC_ROOT/bin/$HUBCENTER_BINARY_NAME" >&2; exit 1; }
  stop_hubcenter_process
  cp -f "$SRC_ROOT/bin/$HUBCENTER_BINARY_NAME" "$REMOTE_HUBCENTER_DIR/$HUBCENTER_BINARY_NAME"
  chmod +x "$REMOTE_HUBCENTER_DIR/$HUBCENTER_BINARY_NAME"
  if [ -f "$SRC_ROOT/hubcenter/start.sh" ]; then
    cp -f "$SRC_ROOT/hubcenter/start.sh" "$REMOTE_HUBCENTER_DIR/start.sh"
    sed -i 's/\r$//' "$REMOTE_HUBCENTER_DIR/start.sh"
    sed -i "s/maclaw-hubcenter/$HUBCENTER_BINARY_NAME/g" "$REMOTE_HUBCENTER_DIR/start.sh"
    chmod +x "$REMOTE_HUBCENTER_DIR/start.sh"
  fi
  [ -f "$SRC_ROOT/hubcenter/configs/config.example.yaml" ] && cp -f "$SRC_ROOT/hubcenter/configs/config.example.yaml" "$REMOTE_HUBCENTER_DIR/configs/config.example.yaml"
  if [ -d "$SRC_ROOT/hubcenter/web" ]; then rm -rf "$REMOTE_HUBCENTER_DIR/web"; cp -R "$SRC_ROOT/hubcenter/web" "$REMOTE_HUBCENTER_DIR/web"; fi
  backup_and_write_config "$REMOTE_HUBCENTER_DIR/configs/config.yaml" "$REMOTE_TMP_DIR/$HUBCENTER_CONFIG_BASENAME"
}

deploy_hub() {
  mkdir -p "$REMOTE_HUB_DIR" "$REMOTE_HUB_DIR/configs" "$REMOTE_HUB_DIR/data" "$REMOTE_HUB_DIR/data/logs"
  [ -f "$SRC_ROOT/bin/$HUB_BINARY_NAME" ] || { echo "[ERROR] Missing hub binary: $SRC_ROOT/bin/$HUB_BINARY_NAME" >&2; exit 1; }
  cp -f "$SRC_ROOT/bin/$HUB_BINARY_NAME" "$REMOTE_HUB_DIR/$HUB_BINARY_NAME"
  chmod +x "$REMOTE_HUB_DIR/$HUB_BINARY_NAME"
  if [ -f "$SRC_ROOT/hub/start.sh" ]; then
    cp -f "$SRC_ROOT/hub/start.sh" "$REMOTE_HUB_DIR/start.sh"
    sed -i 's/\r$//' "$REMOTE_HUB_DIR/start.sh"
    sed -i "s/maclaw-hub/$HUB_BINARY_NAME/g" "$REMOTE_HUB_DIR/start.sh"
    chmod +x "$REMOTE_HUB_DIR/start.sh"
  fi
  [ -f "$SRC_ROOT/hub/configs/config.example.yaml" ] && cp -f "$SRC_ROOT/hub/configs/config.example.yaml" "$REMOTE_HUB_DIR/configs/config.example.yaml"
  if [ -d "$SRC_ROOT/hub/web" ]; then rm -rf "$REMOTE_HUB_DIR/web"; cp -R "$SRC_ROOT/hub/web" "$REMOTE_HUB_DIR/web"; fi
  backup_and_write_config "$REMOTE_HUB_DIR/configs/config.yaml" "$REMOTE_TMP_DIR/$HUB_CONFIG_BASENAME"
}

[ "$DEPLOY_HUBCENTER" = "1" ] && { echo "[remote] Deploying hubcenter files..."; deploy_hubcenter; clean_hubcenter_db_if_requested; }
[ "$DEPLOY_HUB" = "1" ] && { echo "[remote] Deploying hub files..."; deploy_hub; [ "$ENSURE_HUB_MODELS" = "1" ] && ensure_hub_models; }
[ "$DEPLOY_HUBCENTER" = "1" ] && [ -x "$REMOTE_HUBCENTER_DIR/start.sh" ] && { echo "[remote] Restarting hubcenter..."; cd "$REMOTE_HUBCENTER_DIR"; ./start.sh; }
[ "$DEPLOY_HUB" = "1" ] && [ -x "$REMOTE_HUB_DIR/start.sh" ] && { echo "[remote] Restarting hub..."; cd "$REMOTE_HUB_DIR"; ./start.sh; }

rm -rf "$SRC_ROOT"
rm -f "$ARCHIVE_PATH" "$REMOTE_TMP_DIR/remote_deploy.sh"
[ -n "$HUBCENTER_CONFIG_BASENAME" ] && rm -f "$REMOTE_TMP_DIR/$HUBCENTER_CONFIG_BASENAME"
[ -n "$HUB_CONFIG_BASENAME" ] && rm -f "$REMOTE_TMP_DIR/$HUB_CONFIG_BASENAME"
echo "Remote build and deploy finished."
REMOTE_EOF
  chmod +x "$path"
}

post_deploy_check() {
  local failures=0
  local method url expected status
  for i in "${SELECTED[@]}"; do
    local host="${TARGET_HOSTS[$i]}"
    if [[ "${TARGET_DEPLOY_HUBCENTER[$i]}" == "1" ]]; then
      method=GET; url="https://$host/healthz"; expected=200
      status="$(curl -k -sS -o /dev/null -w '%{http_code}' -X "$method" --connect-timeout 10 --max-time 10 "$url" || true)"
      echo "  - $host: $url -> $status"
      [[ "$status" == "$expected" ]] || failures=$((failures + 1))
      method=GET; url="https://$host/admin"; expected=200
      status="$(curl -k -sS -o /dev/null -w '%{http_code}' -X "$method" --connect-timeout 10 --max-time 10 "$url" || true)"
      echo "  - $host: $url -> $status"
      [[ "$status" == "$expected" ]] || failures=$((failures + 1))
      method=POST; url="https://$host/api/admin/hubs/registration-policy"; expected=401
      status="$(curl -k -sS -o /dev/null -w '%{http_code}' -X "$method" --connect-timeout 10 --max-time 10 -H 'Content-Type: application/json' --data '{"hub_id":"smoke"}' "$url" || true)"
      echo "  - $host: $url -> $status"
      [[ "$status" == "$expected" ]] || failures=$((failures + 1))
    fi
    if [[ "${TARGET_DEPLOY_HUB[$i]}" == "1" && -n "${TARGET_HUB_PUBLIC_URLS[$i]}" ]]; then
      base="${TARGET_HUB_PUBLIC_URLS[$i]%/}"
      for item in "GET $base/healthz 200" "GET $base/admin 200" "GET $base/card_store?tenant_id=tenant_default 200" "GET $base/api/card-store/products?tenant_id=tenant_default 200" "GET $base/api/admin/card-store/config 401"; do
        set -- $item
        status="$(curl -k -sS -o /dev/null -w '%{http_code}' -X "$1" --connect-timeout 10 --max-time 10 "$2" || true)"
        echo "  - $host: $2 -> $status"
        [[ "$status" == "$3" ]] || failures=$((failures + 1))
      done
    fi
  done
  [[ "$failures" == "0" ]] || die "Post-deploy smoke check failed: $failures failure(s)."
}

inventory_path="$ROOT_DIR/deploy/hubcenter-ha.inventory.generated.psd1"
cluster_secret="${CLUSTER_SECRET:-}"
secret_source="environment"
if [[ -z "$cluster_secret" ]]; then
  if [[ -f "$inventory_path" ]]; then
    cluster_secret="$(awk -F"'" '/ClusterSecret/ {print $2; exit}' "$inventory_path" || true)"
    secret_source="existing-inventory"
  fi
  if [[ -z "$cluster_secret" ]]; then
    cluster_secret="$(rand_secret)"
    secret_source="generated"
  fi
fi

run_stamp="$(date +%Y%m%d-%H%M%S)"
BUILD_ROOT="$ROOT_DIR/build/deploy-ha/run-$run_stamp-$$"
stage_root="$BUILD_ROOT/stage"
rendered_dir="$ROOT_DIR/deploy/rendered-configs-temp"
archive_path="$BUILD_ROOT/maclaw-deploy.tar.gz"
remote_script_path="$BUILD_ROOT/remote_deploy.sh"
mkdir -p "$BUILD_ROOT"
rm -rf "$stage_root" "$rendered_dir"
mkdir -p "$stage_root" "$rendered_dir"

echo
echo "[1/9] Deployment topology"
for i in "${SELECTED[@]}"; do
  if [[ "${TARGET_DEPLOY_HUBCENTER[$i]}" == "1" && "${TARGET_DEPLOY_HUB[$i]}" == "1" ]]; then
    echo "  - ${TARGET_HOSTS[$i]}: hubcenter[${TARGET_HUBCENTER_DIRS[$i]}] + hub[${TARGET_HUB_DIRS[$i]}]"
  elif [[ "${TARGET_DEPLOY_HUBCENTER[$i]}" == "1" ]]; then
    echo "  - ${TARGET_HOSTS[$i]}: hubcenter[${TARGET_HUBCENTER_DIRS[$i]}] only"
  else
    echo "  - ${TARGET_HOSTS[$i]}: hub[${TARGET_HUB_DIRS[$i]}] only"
  fi
done
echo "  Shared cluster secret: $secret_source"
echo

echo "[2/9] Running remote prechecks..."
for i in "${SELECTED[@]}"; do
  echo "  - checking ${TARGET_HOSTS[$i]}"
  remote_precheck "$i"
done

echo "[3/9] Preparing build workspace..."

echo "[4/9] Rendering hubcenter/hub configs..."
cat >"$inventory_path" <<EOF
@{
    ClusterSecret = '$(printf "%s" "$cluster_secret" | sed "s/'/''/g")'
}
EOF
render_configs "$rendered_dir" "$cluster_secret"

echo "[5/9] Building local Linux binaries and staging deploy assets..."
build_binaries "$stage_root"
stage_assets "$stage_root"

echo "[6/9] Creating deploy archive..."
tar -czf "$archive_path" -C "$stage_root" .

echo "[7/9] Writing remote deployment script..."
write_remote_script "$remote_script_path"

model_statuses=()
target_pos=0
for i in "${SELECTED[@]}"; do
  target_pos=$((target_pos + 1))
  host="${TARGET_HOSTS[$i]}"
  tmp="${TARGET_TMP_DIRS[$i]}"
  hc_config="${TARGET_HUBCENTER_CONFIGS[$i]}"
  hub_config="${TARGET_HUB_CONFIGS[$i]}"
  ensure_hub_models=0

  [[ "${TARGET_DEPLOY_HUBCENTER[$i]}" == "0" || -f "$rendered_dir/$hc_config" ]] || die "Missing hubcenter config for ${TARGET_NAMES[$i]}"
  [[ "${TARGET_DEPLOY_HUB[$i]}" == "0" || -f "$rendered_dir/$hub_config" ]] || die "Missing hub config for ${TARGET_NAMES[$i]}"

  echo
  echo "[8/9][$target_pos/${#SELECTED[@]}] Uploading artifacts to $host..."
  ssh_cmd "$host" "mkdir -p $(shell_quote "$tmp")"
  scp_upload "$host" "$archive_path" "$tmp/maclaw-deploy.tar.gz"
  scp_upload "$host" "$remote_script_path" "$tmp/remote_deploy.sh"
  [[ "${TARGET_DEPLOY_HUBCENTER[$i]}" == "1" ]] && scp_upload "$host" "$rendered_dir/$hc_config" "$tmp/$hc_config"
  if [[ "${TARGET_DEPLOY_HUB[$i]}" == "1" ]]; then
    scp_upload "$host" "$rendered_dir/$hub_config" "$tmp/$hub_config"
    model_dir="${TARGET_HUB_DIRS[$i]}/data/models"
    model_state_cmd="missing=0; for name in $HUB_MODEL_FILES; do [ -f '$model_dir/'\"\$name\" ] || missing=1; done; if [ -f '$model_dir/.models-downloading' ]; then echo downloading; elif [ \$missing -eq 0 ]; then echo ready; else echo missing; fi"
    model_state="$(ssh_cmd "$host" "$model_state_cmd" | tail -n 1 || true)"
    if [[ "$model_state" == "ready" ]]; then
      model_statuses+=("$host: existing models kept in ${TARGET_HUB_DIRS[$i]}/data/models")
    elif [[ "$model_state" == "downloading" ]]; then
      model_statuses+=("$host: model download already running in background")
    else
      ensure_hub_models=1
      model_statuses+=("$host: model download will be started in background to ${TARGET_HUB_DIRS[$i]}/data/models")
    fi
  fi

  env_cmd=(
    "export REMOTE_TMP_DIR=$(shell_quote "$tmp")"
    "export REMOTE_HUB_DIR=$(shell_quote "${TARGET_HUB_DIRS[$i]}")"
    "export REMOTE_HUBCENTER_DIR=$(shell_quote "${TARGET_HUBCENTER_DIRS[$i]}")"
    "export DEPLOY_HUBCENTER=${TARGET_DEPLOY_HUBCENTER[$i]}"
    "export DEPLOY_HUB=${TARGET_DEPLOY_HUB[$i]}"
    "export CLEAN_HUBCENTER_DB=$clean_hubcenter_db"
    "export HUBCENTER_DB_PATH=$(shell_quote './data/codeclaw-hubcenter.db')"
    "export ENSURE_HUB_MODELS=$ensure_hub_models"
    "export HUB_MODEL_BASE_URL=$(shell_quote "$HUB_MODEL_BASE_URL")"
    "export HUB_MODEL_FILES=$(shell_quote "$HUB_MODEL_FILES")"
    "export HUB_BINARY_NAME=$(shell_quote "$hub_binary_name")"
    "export HUBCENTER_BINARY_NAME=$(shell_quote "$hubcenter_binary_name")"
  )
  [[ "${TARGET_DEPLOY_HUBCENTER[$i]}" == "1" ]] && env_cmd+=("export HUBCENTER_CONFIG_BASENAME=$(shell_quote "$hc_config")")
  [[ "${TARGET_DEPLOY_HUB[$i]}" == "1" ]] && env_cmd+=("export HUB_CONFIG_BASENAME=$(shell_quote "$hub_config")")

  remote_command="sed -i 's/\r$//' $(shell_quote "$tmp")/remote_deploy.sh && chmod +x $(shell_quote "$tmp")/remote_deploy.sh && $(join_by ' && ' "${env_cmd[@]}") && $(shell_quote "$tmp")/remote_deploy.sh"
  echo "[9/9][$target_pos/${#SELECTED[@]}] Deploying uploaded binaries on $host..."
  ssh_cmd "$host" "$remote_command"

  if [[ "${TARGET_DEPLOY_HUB[$i]}" == "1" && -n "${TARGET_NGINX_CONFIGS[$i]}" ]]; then
    nginx_cmd="set -e; config=$(shell_quote "${TARGET_NGINX_CONFIGS[$i]}"); server_pattern='${TARGET_NGINX_SERVER_NAMES[$i]//./\\.}'; proxy_port=$(shell_quote "${TARGET_NGINX_PROXY_PORTS[$i]}"); if [ -f \"\$config\" ]; then cp -a \"\$config\" \"\$config.bak.codex-nginx-proxy\"; sed -i \"/server_name[[:space:]]\\+\$server_pattern;/,/^}/s#proxy_pass http://127\\.0\\.0\\.1:[0-9]\\+;#proxy_pass http://127.0.0.1:\$proxy_port;#\" \"\$config\"; nginx -t; if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet nginx; then systemctl reload nginx; else nginx -s reload; fi; fi"
    echo "Ensuring nginx proxy for ${TARGET_NGINX_SERVER_NAMES[$i]} -> 127.0.0.1:${TARGET_NGINX_PROXY_PORTS[$i]} on $host..."
    ssh_cmd "$host" "$nginx_cmd"
  fi
done

echo
if [[ "$no_check" == "1" ]]; then
  echo "Post-deploy smoke check skipped (--no-check)."
else
  echo "Running post-deploy smoke checks..."
  sleep 3
  post_deploy_check
fi

echo "Deployment completed successfully."
echo "Rendered configs: $rendered_dir"
echo "Services deployed:"
for i in "${SELECTED[@]}"; do
  if [[ "${TARGET_DEPLOY_HUBCENTER[$i]}" == "1" && "${TARGET_DEPLOY_HUB[$i]}" == "1" ]]; then
    echo "  - ${TARGET_HOSTS[$i]}: hubcenter + ${TARGET_HUB_PUBLIC_URLS[$i]}"
  elif [[ "${TARGET_DEPLOY_HUBCENTER[$i]}" == "1" ]]; then
    echo "  - ${TARGET_HOSTS[$i]}: hubcenter"
  else
    echo "  - ${TARGET_HOSTS[$i]}: ${TARGET_HUB_PUBLIC_URLS[$i]}"
  fi
done
if [[ "${#model_statuses[@]}" -gt 0 ]]; then
  echo "Hub model status:"
  printf '  - %s\n' "${model_statuses[@]}"
  echo "Model download log path:"
  for i in "${SELECTED[@]}"; do
    [[ "${TARGET_DEPLOY_HUB[$i]}" == "1" ]] && echo "  - ${TARGET_HOSTS[$i]}: ${TARGET_HUB_DIRS[$i]}/data/logs/model-download.log"
  done
fi
if [[ "$secret_source" == "generated" ]]; then
  echo
  echo "Generated cluster secret for this rollout:"
  echo "$cluster_secret"
fi
