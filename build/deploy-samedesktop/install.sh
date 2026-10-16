#!/bin/sh
# Replace maclawsrv, hub, and desktopd binaries. Do not rewrite units, start.sh, .env, nginx, or the desktop image.
set -eu
NEW=/tmp/maclaw-samedesktop
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

echo "HUBCENTER_BEFORE=$(ps -eo pid=,args= | awk '/maclaw-hubcenter/ && !/awk/ {print $1; exit}')"
echo "DESKTOPS_BEFORE"
docker ps -a --filter name=maclaw-desktop- --format '{{.ID}} {{.Status}} {{.Names}}' || true

cp -f "$SRV" "${SRV}.bak-samedesktop-20261010"
cp -f "$HUB" "${HUB}.bak-samedesktop-20261010"
cp -f "$DSK" "${DSK}.bak-samedesktop-20261010"
cp -f "$NEW/maclawsrv" "$SRV"
cp -f "$NEW/maclaw-hub" "$HUB"
cp -f "$NEW/desktopd" "$DSK"
chmod 755 "$SRV" "$HUB" "$DSK"

systemctl restart maclawsrv.service
( cd /data/soft/hub && ./start.sh )
systemctl restart maclaw-desktopd.service

ok=0
i=0
while [ "$i" -lt 15 ]; do
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
if grep -a -q "person's cloud desktop" "$SRV"; then
  echo MARKER_OK
else
  echo MARKER_MISSING
fi
if grep -a -q '/home/desktop/Desktop' "$SRV"; then
  echo HOME_MARKER_OK
else
  echo HOME_MARKER_MISSING
fi
echo DEPLOY_OK
