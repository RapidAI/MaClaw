#!/bin/sh
# Run the offline computer-use regression set on a throwaway TEST desktop.
#
#   desktopd/regress/run.sh            create regress-test-<time>, run, remove it
#   KEEP=1 desktopd/regress/run.sh     leave the test container for a look
#
# Needs root on a desktopd host (reads /data/soft/maclaw_desktopd/.env for the
# local API). Only containers whose maclaw.user label starts with
# regress-test- are ever touched; a person's desktop is never used.
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
ENV_FILE=${DESKTOPD_ENV:-/data/soft/maclaw_desktopd/.env}
set -a; . "$ENV_FILE"; set +a
ADDR=$DESKTOPD_ADDR; case $ADDR in :*) ADDR=127.0.0.1$ADDR;; esac
USER_ID=regress-test-$(date +%Y%m%d%H%M%S)
IMAGE=${IMAGE:-maclaw-gui:2}
api() { curl -sS --max-time 300 -H "Authorization: Bearer $DESKTOPD_TOKEN" -H 'Content-Type: application/json' -d "$2" "http://$ADDR$1"; }
api /v1/desktops "{\"tenant_id\":\"tenant_default\",\"user_id\":\"$USER_ID\",\"image\":\"$IMAGE\"}" >/dev/null
C=$(docker ps --filter "label=maclaw.user=$USER_ID" --format '{{.Names}}' | head -1)
[ -n "$C" ] || { echo "no test container for $USER_ID" >&2; exit 2; }
case "$(docker inspect -f '{{index .Config.Labels "maclaw.user"}}' "$C")" in regress-test-*) ;; *) echo "refusing: $C is not a regress-test container" >&2; exit 2;; esac
cleanup() {
  [ "${KEEP:-0}" = 1 ] && { echo "kept $C" >&2; return; }
  vols=$(docker inspect -f '{{range .Mounts}}{{.Name}} {{end}}' "$C" 2>/dev/null || true)
  docker rm -f "$C" >/dev/null 2>&1 || true
  for v in $vols; do docker volume rm "$v" >/dev/null 2>&1 || true; done
}
trap cleanup EXIT
docker exec "$C" sh -c 'python3 /desktop_supervisor.py ensure "$MACLAW_DESKTOP_KEY" >/dev/null'
for i in $(seq 1 30); do docker exec "$C" curl -fsS http://127.0.0.1:18020/json/version >/dev/null 2>&1 && break; sleep 1; done
docker cp "$HERE/regress.py" "$C:/tmp/regress.py"
docker exec -e REGRESS_CDP_PORT=18020 -e REGRESS_DISPLAY=:20 "$C" python3 /tmp/regress.py "$@"
