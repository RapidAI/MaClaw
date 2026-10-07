#!/bin/sh
# Host side of the maclaw-gui:2 rollout (desktopd + Hub + MaClawSrv on one
# Docker host). rollout_desktop_gui2.sh uploads this file with the build
# archives and runs one step at a time:
#
#   rollout_remote.sh backup|deploy-desktopd|deploy-hub|deploy-maclawsrv|verify|switch-image|rollback
#
# The steps mirror the Windows deploy scripts:
#   deploy-desktopd   deploy_desktopd.cmd -> desktopd/remote_deploy.sh (unchanged)
#   deploy-hub        deploy_all.cmd / deploy/deploy_all_ha.ps1 deploy_hub(), except
#                     that configs/config.yaml is kept as it is (no rendered config)
#   deploy-maclawsrv  deploy_maclawsrv.cmd :write_remote_script
# No step prints DESKTOPD_TOKEN, Hub tokens, or .env contents.
set -eu

: "${REMOTE_TMP:=/tmp/maclaw_gui2_rollout}"
: "${DESKTOPD_DEPLOY_DIR:=/data/soft/maclaw_desktopd}"
: "${HUB_DIR:=/data/soft/hub}"
: "${MACLAWSRV_DEPLOY_DIR:=/data/soft/maclaw_srv}"
: "${BACKUP_ROOT:=/data/soft/backups}"
: "${BACKUP_TS:=}"
: "${DESKTOPD_ADVERTISE_HOST:=}"
: "${DESKTOPD_IMAGE:=maclaw-gui:2}"
: "${DESKTOPD_BASE_IMAGE:=}"
: "${DESKTOPD_APT_MIRROR:=}"
: "${DESKTOPD_SKIP_IMAGE_BUILD:=0}"
# build | pull | auto, see desktopd/remote_deploy.sh. DESKTOPD_IMAGE_SOURCE and
# DESKTOPD_IMAGE_PULL_TIMEOUT pass through the environment only when set.
: "${DESKTOPD_IMAGE_FROM:=build}"
: "${HUB_PUBLIC_URL:=}"
: "${DESKTOP_IMAGE:=maclaw-gui:2}"
: "${DESKTOP_MEMORY:=3g}"
: "${DESKTOP_CPUS:=1.5}"
: "${DESKTOP_SHM:=1g}"
: "${DESKTOP_SERVER_ID:=}"
SRC="$REMOTE_TMP/src"
HUB_DB="$HUB_DIR/data/codeclaw-hub.db"
export HUB_DB

log() { echo "[remote] $*"; }

unpack() {
  # $1 = archive basename under $REMOTE_TMP; .tar.xz or .tar.gz
  if [ -f "$REMOTE_TMP/$1.sha256" ]; then
    (cd "$REMOTE_TMP" && sha256sum -c --quiet "$1.sha256") || { echo "checksum mismatch: $1" >&2; exit 1; }
  fi
  mkdir -p "$SRC"
  case "$1" in
    *.xz) xz -dc "$REMOTE_TMP/$1" | tar -x -C "$SRC" ;;
    *) tar -xzf "$REMOTE_TMP/$1" -C "$SRC" ;;
  esac
}

backup() {
  ts="${BACKUP_TS:-$(date +%Y%m%d-%H%M%S)}"
  b="$BACKUP_ROOT/gui2-rollout-$ts"
  mkdir -p "$b"; chmod 700 "$b"
  d="$DESKTOPD_DEPLOY_DIR.bak.$ts"; mkdir -p "$d"; chmod 700 "$d"
  for f in bin image .env state; do if [ -e "$DESKTOPD_DEPLOY_DIR/$f" ]; then cp -a "$DESKTOPD_DEPLOY_DIR/$f" "$d/"; fi; done
  cp -a /etc/systemd/system/maclaw-desktopd.service "$d/" 2>/dev/null || true
  for c in $(docker ps -aq --filter name=maclaw-desktop-) $(docker ps -aq --filter name='^maclaw-gui$'); do
    docker cp "$c":/desktop_supervisor.py "$d/supervisor.$(docker inspect --format '{{.Name}}' "$c" | tr -d /).py" 2>/dev/null || true
  done
  h="$HUB_DIR.bak.$ts"; mkdir -p "$h"; chmod 700 "$h"
  for f in maclaw-hub meeting_asr_worker start.sh configs web; do if [ -e "$HUB_DIR/$f" ]; then cp -a "$HUB_DIR/$f" "$h/"; fi; done
  python3 "$REMOTE_TMP/scripts/hub_desktop_settings.py" backup "$h/desktop_service.settings.json" || log "no desktop_service settings to back up"
  s="$MACLAWSRV_DEPLOY_DIR.bak.$ts"; mkdir -p "$s/bin"; chmod 700 "$s"
  cp -a "$MACLAWSRV_DEPLOY_DIR/bin/maclawsrv" "$s/bin/" 2>/dev/null || true
  for f in .env start.sh; do if [ -e "$MACLAWSRV_DEPLOY_DIR/$f" ]; then cp -a "$MACLAWSRV_DEPLOY_DIR/$f" "$s/"; fi; done
  cp -a /etc/systemd/system/maclawsrv.service "$s/" 2>/dev/null || true
  {
    date
    for u in maclaw-desktopd maclawsrv; do systemctl --no-pager status "$u" 2>&1 | head -5; done
    echo "hub pid $(cat "$HUB_DIR/data/maclaw-hub.pid" 2>/dev/null)"
    ps -eo pid,lstart,args | grep -E "[m]aclaw-hub|[m]aclawsrv|[d]esktopd"
  } > "$b/service-states.txt" 2>&1
  docker ps -a --no-trunc --format '{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Label "maclaw.image"}}\t{{.Ports}}' > "$b/containers.txt"
  docker images --no-trunc --digests > "$b/images.txt"
  docker volume ls > "$b/volumes.txt"
  for c in $(docker ps -aq); do docker inspect "$c"; done > "$b/containers.inspect.json"
  md5sum "$HUB_DIR/maclaw-hub" "$MACLAWSRV_DEPLOY_DIR/bin/maclawsrv" "$DESKTOPD_DEPLOY_DIR/bin/desktopd" > "$b/binaries.md5" 2>/dev/null || true
  chmod 600 "$b"/*
  echo "$ts" > "$REMOTE_TMP/BACKUP_TS"
  log "backup $ts: $d $h $s $b"
}

deploy_desktopd() {
  [ -n "$DESKTOPD_ADVERTISE_HOST" ] || { echo "DESKTOPD_ADVERTISE_HOST is required" >&2; exit 1; }
  mkdir -p /tmp/maclaw_desktopd_deploy
  cp -f "$REMOTE_TMP/desktopd-deploy.tar.gz" /tmp/maclaw_desktopd_deploy/desktopd-deploy.tar.gz
  cp -f "$REMOTE_TMP/remote_deploy.sh" /tmp/maclaw_desktopd_deploy/remote_deploy.sh
  sed -i 's/\r$//' /tmp/maclaw_desktopd_deploy/remote_deploy.sh
  chmod +x /tmp/maclaw_desktopd_deploy/remote_deploy.sh
  REMOTE_TMP_DIR=/tmp/maclaw_desktopd_deploy DESKTOPD_DEPLOY_DIR="$DESKTOPD_DEPLOY_DIR" \
  DESKTOPD_BIND_ADDR=127.0.0.1:18081 DESKTOPD_PORT=18081 DESKTOPD_ADVERTISE_HOST="$DESKTOPD_ADVERTISE_HOST" \
  DESKTOPD_IMAGE="$DESKTOPD_IMAGE" DESKTOPD_BASE_IMAGE="$DESKTOPD_BASE_IMAGE" DESKTOPD_APT_MIRROR="$DESKTOPD_APT_MIRROR" \
  DESKTOPD_SKIP_IMAGE_BUILD="$DESKTOPD_SKIP_IMAGE_BUILD" DESKTOPD_IMAGE_FROM="$DESKTOPD_IMAGE_FROM" \
    sh /tmp/maclaw_desktopd_deploy/remote_deploy.sh
}

deploy_hub() {
  rm -rf "$SRC"; unpack hub.tar.xz
  mkdir -p "$HUB_DIR" "$HUB_DIR/configs" "$HUB_DIR/data" "$HUB_DIR/data/logs"
  cp -f "$SRC/bin/maclaw-hub" "$HUB_DIR/maclaw-hub"; chmod +x "$HUB_DIR/maclaw-hub"
  cp -f "$SRC/bin/meeting_asr_worker" "$HUB_DIR/meeting_asr_worker"; chmod +x "$HUB_DIR/meeting_asr_worker"
  cp -f "$SRC/hub/start.sh" "$HUB_DIR/start.sh"; sed -i 's/\r$//' "$HUB_DIR/start.sh"; chmod +x "$HUB_DIR/start.sh"
  cp -f "$SRC/hub/configs/config.example.yaml" "$HUB_DIR/configs/config.example.yaml"
  # replace_web_tree: stage beside the live tree, verify the manifest, swap.
  staging="$HUB_DIR/web.deploying.$(date +%s).$$"
  rm -rf "$staging"; mkdir -p "$staging"; cp -R "$SRC/hub/web/." "$staging/"
  (cd "$staging" && sha256sum -c --quiet "$SRC/hub-web.sha256") || { rm -rf "$staging"; echo "hub web manifest mismatch" >&2; exit 1; }
  rm -rf "$HUB_DIR/web"; mv "$staging" "$HUB_DIR/web"
  # configs/config.yaml is production state: keep it.
  (cd "$HUB_DIR" && ./start.sh < /dev/null)
  ok=""
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    if [ "$(curl -s -o /dev/null -w '%{http_code}' -m 10 http://127.0.0.1:9399/healthz)" = 200 ]; then ok=1; break; fi
    sleep 3
  done
  [ -n "$ok" ] || { echo "hub healthz did not return 200" >&2; exit 1; }
  log "hub healthy (pid $(cat "$HUB_DIR/data/maclaw-hub.pid"))"
  rm -rf "$SRC"
}

deploy_maclawsrv() {
  rm -rf "$SRC"; unpack maclawsrv.tar.xz
  D="$MACLAWSRV_DEPLOY_DIR"
  mkdir -p "$D/bin" "$D/data" "$D/logs"
  cp -f "$SRC/bin/maclawsrv" "$D/bin/maclawsrv"; chmod +x "$D/bin/maclawsrv"
  if [ ! -f "$D/.env" ]; then
    echo "$D/.env is missing; run deploy_maclawsrv.cmd once to create it with generated secrets" >&2
    exit 1
  fi
  for kv in "MACLAW_DATA_ROOT=$D/data" "MACLAW_HTTP_ADDR=:18080" "MACLAW_ALLOW_INSECURE_HTTP=true" \
            "MACLAW_ADMIN_WEB_DEFAULT_LOCALE=zh-CN" "MACLAW_ENABLE_SCHEDULER=true"; do
    grep -q "^${kv%%=*}=" "$D/.env" || echo "$kv" >> "$D/.env"
  done
  cat > "$D/start.sh" <<'STARTEOF'
#!/bin/sh
set -eu
cd "$(dirname "$0")"
set -a
. ./.env
set +a
pkill -f "bin/maclawsrv" 2>/dev/null || true
sleep 1
nohup ./bin/maclawsrv > ./logs/maclawsrv.log 2>&1 &
echo "MaClawSrv started (PID: $!)"
STARTEOF
  chmod +x "$D/start.sh"
  cat > /etc/systemd/system/maclawsrv.service <<SERVICEEOF
[Unit]
Description=MaClawSrv REST service
After=network.target

[Service]
Type=simple
WorkingDirectory=$D
EnvironmentFile=$D/.env
ExecStart=$D/bin/maclawsrv
Restart=always
RestartSec=3
StandardOutput=append:$D/logs/maclawsrv.log
StandardError=append:$D/logs/maclawsrv.err.log

[Install]
WantedBy=multi-user.target
SERVICEEOF
  systemctl daemon-reload
  systemctl enable maclawsrv.service >/dev/null 2>&1 || true
  systemctl restart maclawsrv.service
  sleep 3
  curl -fsS -m 10 http://127.0.0.1:18080/health; echo
  curl -fsS -m 10 http://127.0.0.1:18080/version; echo
  rm -rf "$SRC"
}

verify() {
  fails=0
  check() { if [ "$2" = "$3" ]; then echo "PASS  $1 -> $2"; else echo "FAIL  $1 -> $2 (want $3)"; fails=$((fails + 1)); fi; }
  for u in maclaw-desktopd maclawsrv; do check "systemctl $u" "$(systemctl is-active "$u")" active; done
  check "desktopd /v1/health" "$(DESKTOPD_ENV_FILE="$DESKTOPD_DEPLOY_DIR/.env" sh "$REMOTE_TMP/scripts/dapi.sh" GET /v1/health 2>&1)" '{"ok":true}'
  check "hub /healthz" "$(curl -s -o /dev/null -w '%{http_code}' -m 10 http://127.0.0.1:9399/healthz)" 200
  check "maclawsrv /health" "$(curl -s -m 10 http://127.0.0.1:18080/health)" '{"status":"ok"}'
  if [ -n "$HUB_PUBLIC_URL" ]; then
    for item in "/healthz 200" "/admin 200" "/api/admin/card-store/config 401" "/api/admin/desktop-services 401"; do
      set -- $item
      check "$HUB_PUBLIC_URL$1" "$(curl -k -s -o /dev/null -w '%{http_code}' -m 15 "$HUB_PUBLIC_URL$1")" "$2"
    done
  fi
  echo "-- desktop containers (an empty maclaw.image label means maclaw-gui:1)"
  docker ps -a --filter name=maclaw-desktop- --format '{{.Names}}\t{{.Status}}\t{{.Label "maclaw.image"}}'
  echo "-- Hub desktop_service"
  python3 "$REMOTE_TMP/scripts/hub_desktop_settings.py" show || true
  [ "$fails" -eq 0 ]
}

switch_image() {
  set -- --image "$DESKTOP_IMAGE" --memory "$DESKTOP_MEMORY" --cpus "$DESKTOP_CPUS" --shm "$DESKTOP_SHM"
  if [ -n "$DESKTOP_SERVER_ID" ]; then set -- --server-id "$DESKTOP_SERVER_ID" "$@"; fi
  python3 "$REMOTE_TMP/scripts/hub_desktop_settings.py" set "$@"
  log "users move to $DESKTOP_IMAGE the next time their desktop opens; running containers are not touched"
}

rollback() {
  ts="${BACKUP_TS:-$(cat "$REMOTE_TMP/BACKUP_TS" 2>/dev/null || true)}"
  [ -n "$ts" ] || { echo "BACKUP_TS is required" >&2; exit 1; }
  h="$HUB_DIR.bak.$ts"; d="$DESKTOPD_DEPLOY_DIR.bak.$ts"; s="$MACLAWSRV_DEPLOY_DIR.bak.$ts"
  for x in "$h" "$d" "$s"; do [ -d "$x" ] || { echo "missing backup $x" >&2; exit 1; }; done
  # 1. Hub desktop settings first, so nobody else is moved while binaries change.
  if [ -f "$h/desktop_service.settings.json" ]; then python3 "$REMOTE_TMP/scripts/hub_desktop_settings.py" restore "$h/desktop_service.settings.json"; fi
  # 2. desktopd
  cp -f "$d/bin/desktopd" "$DESKTOPD_DEPLOY_DIR/bin/desktopd"
  if [ -d "$d/image" ]; then rm -rf "$DESKTOPD_DEPLOY_DIR/image"; cp -a "$d/image" "$DESKTOPD_DEPLOY_DIR/image"; fi
  systemctl restart maclaw-desktopd
  # 3. Hub
  for f in maclaw-hub meeting_asr_worker start.sh; do if [ -e "$h/$f" ]; then cp -af "$h/$f" "$HUB_DIR/$f"; fi; done
  if [ -d "$h/web" ]; then rm -rf "$HUB_DIR/web"; cp -a "$h/web" "$HUB_DIR/web"; fi
  (cd "$HUB_DIR" && ./start.sh < /dev/null)
  # 4. MaClawSrv
  cp -f "$s/bin/maclawsrv" "$MACLAWSRV_DEPLOY_DIR/bin/maclawsrv"
  systemctl restart maclawsrv
  log "rolled back to $ts. Users already moved to maclaw-gui:2 stay there; see docs/desktop-gui-v2.md (回滚) to move one back from its :prev image."
}

case "${1:-}" in
  backup) backup ;;
  deploy-desktopd) deploy_desktopd ;;
  deploy-hub) deploy_hub ;;
  deploy-maclawsrv) deploy_maclawsrv ;;
  verify) verify ;;
  switch-image) switch_image ;;
  rollback) rollback ;;
  *) sed -n '2,15p' "$0" >&2; exit 2 ;;
esac
