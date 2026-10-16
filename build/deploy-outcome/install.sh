#!/bin/sh
# Replace maclawsrv only. The outcome check lives in this binary.
# Do not rewrite units, start.sh, .env, nginx, hub, desktopd, or the desktop image.
set -eu
NEW=/tmp/maclaw-outcome
SRV=/data/soft/maclaw_srv/bin/maclawsrv
BAK="${SRV}.bak-outcome-20261010"
test -f "$NEW/maclawsrv"
test -f "$SRV"
test -f /data/soft/hub/start.sh
test -f /data/soft/hub/desktop-api.env
test -f /data/soft/maclaw_srv/.env

envstamp() {
  sha256sum /data/soft/hub/start.sh /data/soft/hub/desktop-api.env /data/soft/maclaw_srv/.env | sha256sum | awk '{print $1}'
}
BEFORE_ENV=$(envstamp)
echo "HUB_PID_BEFORE=$(ps -eo pid=,args= | awk '/\/data\/soft\/hub\/maclaw-hub/ && !/awk/ && !/hubcenter/ {print $1; exit}')"
echo "HUBCENTER_BEFORE=$(ps -eo pid=,args= | awk '/maclaw-hubcenter/ && !/awk/ {print $1; exit}')"
echo "DESKTOPD_PID_BEFORE=$(systemctl show -p MainPID --value maclaw-desktopd.service)"
echo "DESKTOPS_BEFORE"
docker ps -a --filter name=maclaw-desktop- --format '{{.ID}} {{.Status}} {{.Names}}' || true

if [ ! -f "$BAK" ]; then
  cp -f "$SRV" "$BAK"
fi
cp -f "$NEW/maclawsrv" "$SRV"
chmod 755 "$SRV"

AFTER_ENV=$(envstamp)
if [ "$BEFORE_ENV" != "$AFTER_ENV" ]; then
  echo ENV_CHANGED
  exit 1
fi
echo ENV_UNCHANGED

rollback() {
  echo ROLLBACK
  cp -f "$BAK" "$SRV"
  chmod 755 "$SRV"
  systemctl restart maclawsrv.service || true
  echo ROLLBACK_DONE
}

systemctl restart maclawsrv.service
ok=0
i=0
while [ "$i" -lt 20 ]; do
  if curl -fsS --max-time 2 http://127.0.0.1:18080/version; then
    echo
    ok=1
    break
  fi
  i=$((i + 1))
  sleep 2
done
if [ "$ok" != 1 ]; then
  echo VERSION_FAIL
  rollback
  exit 1
fi

ver=$(curl -fsS --max-time 3 http://127.0.0.1:18080/version)
printf '%s\n' "$ver"
printf '%s\n' "$ver" | grep -q '20261010.outcome' || {
  echo VERSION_MISMATCH
  rollback
  exit 1
}

if ! python3 - "$SRV" <<'PY'
import sys
path = sys.argv[1]
needle = "核对时略去中间".encode("utf-8")
data = open(path, "rb").read()
if needle not in data:
    sys.exit(2)
print("OUTCOME_MARKER_OK")
PY
then
  echo OUTCOME_MARKER_MISSING
  rollback
  exit 1
fi

echo "MACLAWSRV_PID=$(systemctl show -p MainPID --value maclawsrv.service)"
echo "HUB_PID_AFTER=$(ps -eo pid=,args= | awk '/\/data\/soft\/hub\/maclaw-hub/ && !/awk/ && !/hubcenter/ {print $1; exit}')"
echo "HUBCENTER_AFTER=$(ps -eo pid=,args= | awk '/maclaw-hubcenter/ && !/awk/ {print $1; exit}')"
echo "DESKTOPD_PID_AFTER=$(systemctl show -p MainPID --value maclaw-desktopd.service)"
echo "DESKTOPS_AFTER"
docker ps -a --filter name=maclaw-desktop- --format '{{.ID}} {{.Status}} {{.Names}}' || true
sha256sum "$SRV" "$BAK"
code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:9399/api/mobile/bootstrap || echo fail)
echo "bootstrap_9399=$code"
echo DEPLOY_OK
