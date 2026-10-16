#!/bin/sh
# Replace maclawsrv, hub, and desktopd binaries. Do not rewrite units, start.sh, .env, nginx, or the desktop image.
set -eu
NEW=/tmp/maclaw-botssh
SRV=/data/soft/maclaw_srv/bin/maclawsrv
HUB=/data/soft/hub/maclaw-hub
DSK=/data/soft/maclaw_desktopd/bin/desktopd
test -f "$NEW/maclawsrv"
test -f "$NEW/maclaw-hub"
test -f "$NEW/desktopd"
test -f "$SRV"
test -f "$HUB"
test -f "$DSK"
test -f /data/soft/hub/start.sh
test -f /data/soft/hub/desktop-api.env
test -f /data/soft/maclaw_srv/.env

envstamp() {
  sha256sum /data/soft/hub/start.sh /data/soft/hub/desktop-api.env /data/soft/maclaw_srv/.env | sha256sum | awk '{print $1}'
}
BEFORE_ENV=$(envstamp)

echo "HUBCENTER_BEFORE=$(ps -eo pid=,args= | awk '/maclaw-hubcenter/ && !/awk/ {print $1; exit}')"
echo "DESKTOPS_BEFORE"
docker ps -a --filter name=maclaw-desktop- --format '{{.ID}} {{.Status}} {{.Names}}' || true

if [ ! -f "${SRV}.bak-botssh-20261010" ]; then
  cp -f "$SRV" "${SRV}.bak-botssh-20261010"
fi
if [ ! -f "${HUB}.bak-botssh-20261010" ]; then
  cp -f "$HUB" "${HUB}.bak-botssh-20261010"
fi
if [ ! -f "${DSK}.bak-botssh-20261010" ]; then
  cp -f "$DSK" "${DSK}.bak-botssh-20261010"
fi

cp -f "$NEW/maclawsrv" "$SRV"
cp -f "$NEW/maclaw-hub" "$HUB"
cp -f "$NEW/desktopd" "$DSK"
chmod 755 "$SRV" "$HUB" "$DSK"

AFTER_ENV=$(envstamp)
if [ "$BEFORE_ENV" != "$AFTER_ENV" ]; then
  echo ENV_CHANGED
  exit 1
fi
echo ENV_UNCHANGED

rollback() {
  echo ROLLBACK
  cp -f "${SRV}.bak-botssh-20261010" "$SRV"
  cp -f "${HUB}.bak-botssh-20261010" "$HUB"
  cp -f "${DSK}.bak-botssh-20261010" "$DSK"
  chmod 755 "$SRV" "$HUB" "$DSK"
  systemctl restart maclawsrv.service || true
  ( cd /data/soft/hub && ./start.sh ) >/tmp/hub-start-rb.out 2>&1 || true
  rm -f /tmp/hub-start-rb.out
  systemctl restart maclaw-desktopd.service || true
  echo ROLLBACK_DONE
}

systemctl restart maclawsrv.service
( cd /data/soft/hub && ./start.sh ) >/tmp/hub-start.out 2>&1 || {
  echo HUB_START_FAIL
  awk 'BEGIN{IGNORECASE=1} /TOKEN|PASS|SECRET/ {print "REDACTED_LINE"; next} {print}' /tmp/hub-start.out || true
  rm -f /tmp/hub-start.out
  rollback
  exit 1
}
awk 'BEGIN{IGNORECASE=1} /TOKEN|PASS|SECRET/ {print "REDACTED_LINE"; next} {print}' /tmp/hub-start.out
rm -f /tmp/hub-start.out
systemctl restart maclaw-desktopd.service

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

code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:9399/api/mobile/bootstrap || echo fail)
echo "bootstrap_9399=$code"
echo "MACLAWSRV_PID=$(systemctl show -p MainPID --value maclawsrv.service)"
echo "DESKTOPD_PID=$(systemctl show -p MainPID --value maclaw-desktopd.service)"
echo "HUBCENTER_AFTER=$(ps -eo pid=,args= | awk '/maclaw-hubcenter/ && !/awk/ {print $1; exit}')"
echo "DESKTOPS_AFTER"
docker ps -a --filter name=maclaw-desktop- --format '{{.ID}} {{.Status}} {{.Names}}' || true
sha256sum "$SRV" "$HUB" "$DSK"
if grep -a -q "plan phase blocks ssh" "$SRV"; then
  echo SSH_MARKER_OK
else
  echo SSH_MARKER_MISSING
fi
if grep -a -q "person's cloud desktop" "$SRV"; then
  echo DESKTOP_MARKER_OK
else
  echo DESKTOP_MARKER_MISSING
fi

SPID=$(systemctl show -p MainPID --value maclawsrv.service)
if [ -r "/proc/$SPID/environ" ]; then
  tr '\0' '\n' < "/proc/$SPID/environ" | awk -F= '
    $1=="MACLAW_DESKTOP_API_TOKEN" { print "SRV_TOKEN_LEN=" length($2) }
    $1=="MACLAW_HUB_URL" { print "SRV_HUB_URL_LEN=" length($2) }
  '
fi
HPID=$(ps -eo pid=,args= | awk '/\/data\/soft\/hub\/maclaw-hub/ && !/awk/ && !/hubcenter/ {print $1; exit}')
echo "HUB_PID=$HPID"
if [ -n "$HPID" ] && [ -r "/proc/$HPID/environ" ]; then
  tr '\0' '\n' < "/proc/$HPID/environ" | awk -F= '
    $1=="MACLAW_DESKTOP_API_TOKEN" { print "HUB_TOKEN_LEN=" length($2) }
  '
fi

python3 - <<'PY'
import urllib.request
token = ""
with open("/data/soft/hub/desktop-api.env") as fh:
    for line in fh:
        line = line.strip()
        if line.startswith("MACLAW_DESKTOP_API_TOKEN="):
            token = line.split("=", 1)[1].strip().strip('"').strip("'")
            break
if not token:
    print("SESSION_PROBE=no-token")
    raise SystemExit(0)
req = urllib.request.Request(
    "http://127.0.0.1:9399/api/v1/desktop-services/session",
    data=b"{",
    headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"},
    method="POST",
)
try:
    urllib.request.urlopen(req, timeout=3)
    print("SESSION_PROBE=unexpected-200")
except Exception as exc:
    status = getattr(exc, "code", None)
    print("SESSION_PROBE=%s" % (status if status else "err"))
PY
echo DEPLOY_OK
