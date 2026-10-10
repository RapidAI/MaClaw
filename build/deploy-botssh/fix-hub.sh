#!/bin/sh
# Replace only the hub binary with the already uploaded build. Leave maclawsrv, desktopd, HubCenter, docker, containers, start.sh, and env files alone.
set -eu
NEW=/tmp/maclaw-botssh/maclaw-hub
HUB=/data/soft/hub/maclaw-hub
EXPECT=5b4121510b737dc47d9158b70157ecf29497e28ab1a2d0492da0be8cdeb27033
EXPECT_SIZE=65822882
SRV_EXPECT=aaf36016a46e6f03e731727d99904521b190435f63b8dbb52b133525c1cc478e
DSK_EXPECT=7a0da1d961b6a3357198e7952a4273b9660b9c59b018dc5020ad54d54266fb69

redact() {
  awk 'BEGIN{IGNORECASE=1} /TOKEN|PASS|SECRET|AUTHORIZATION|BEARER/ {print "REDACTED_LINE"; next} {print}'
}

envstamp() {
  sha256sum /data/soft/hub/start.sh /data/soft/hub/desktop-api.env /data/soft/maclaw_srv/.env | sha256sum | awk '{print $1}'
}

echo FIX_BEGIN
test -f "$NEW"
test -f "$HUB"
test -f /data/soft/hub/start.sh
test -f /data/soft/hub/desktop-api.env
test -f /data/soft/maclaw_srv/.env
test -f "${HUB}.bak-botssh-20261010"

echo TMP
sha256sum "$NEW"
stat -c '%s %y %n' "$NEW" "$HUB" "${HUB}.bak-botssh-20261010"
echo FILETYPE
file "$NEW" "$HUB" || true
echo HUB_PROCS
ps -eo pid=,lstart=,args= | awk '/maclaw-hub/ && !/awk/ {print}'
echo SOURCE_LINES
grep -n 'desktop-api\|\. /\|source ' /data/soft/hub/start.sh | redact || true
echo SAME_SIZE
find /data/soft /tmp /opt /root -maxdepth 5 -name 'maclaw-hub*' -printf '%s %TY-%Tm-%Td %TH:%TM %p\n' 2>/dev/null || true
echo INSTALLED_BEFORE
sha256sum "$HUB"

BEFORE=$(envstamp)
got=$(sha256sum "$NEW" | awk '{print $1}')
sz=$(stat -c '%s' "$NEW")
if [ "$got" != "$EXPECT" ] || [ "$sz" != "$EXPECT_SIZE" ]; then
  echo TMP_MISMATCH
  exit 1
fi

cp -f "$NEW" "$HUB"
chmod 755 "$HUB"
after=$(sha256sum "$HUB" | awk '{print $1}')
asz=$(stat -c '%s' "$HUB")
echo "AFTER_COPY hash=$after size=$asz"
if [ "$after" != "$EXPECT" ] || [ "$asz" != "$EXPECT_SIZE" ]; then
  echo COPY_MISMATCH
  exit 1
fi

( cd /data/soft/hub && ./start.sh ) >/tmp/hub-fix-start.out 2>&1 || {
  echo HUB_START_FAIL
  redact < /tmp/hub-fix-start.out || true
  rm -f /tmp/hub-fix-start.out
  exit 1
}
redact < /tmp/hub-fix-start.out || true
rm -f /tmp/hub-fix-start.out

sleep 2
final=$(sha256sum "$HUB" | awk '{print $1}')
fsz=$(stat -c '%s' "$HUB")
echo "AFTER_START hash=$final size=$fsz"
stat -c '%s %y %n' "$HUB"

HPID=$(ps -eo pid=,args= | awk '/\/data\/soft\/hub\/maclaw-hub/ && !/awk/ && !/hubcenter/ {print $1; exit}')
echo "HUB_PID=$HPID"
if [ -n "$HPID" ]; then
  ps -o pid=,lstart=,args= -p "$HPID" || true
  if [ -e "/proc/$HPID/exe" ]; then
    echo "EXE_SIZE=$(stat -c '%s' "/proc/$HPID/exe")"
    echo "EXE_HASH=$(sha256sum "/proc/$HPID/exe" | awk '{print $1}')"
    echo "EXE_LINK_LEN=$(readlink "/proc/$HPID/exe" | awk '{print length}')"
  fi
  if [ -r "/proc/$HPID/environ" ]; then
    tr '\0' '\n' < "/proc/$HPID/environ" | awk -F= '$1=="MACLAW_DESKTOP_API_TOKEN" { print "HUB_TOKEN_LEN=" length($2) }'
  fi
fi

if [ "$(envstamp)" = "$BEFORE" ]; then
  echo ENV_UNCHANGED
else
  echo ENV_CHANGED
fi

echo "SRV_HASH=$(sha256sum /data/soft/maclaw_srv/bin/maclawsrv | awk '{print $1}')"
echo "DSK_HASH=$(sha256sum /data/soft/maclaw_desktopd/bin/desktopd | awk '{print $1}')"
echo "SRV_PID=$(systemctl show -p MainPID --value maclawsrv.service)"
echo "DSK_PID=$(systemctl show -p MainPID --value maclaw-desktopd.service)"
echo "SRV_ACTIVE=$(systemctl is-active maclawsrv.service)"
echo "DSK_ACTIVE=$(systemctl is-active maclaw-desktopd.service)"
if [ "$(sha256sum /data/soft/maclaw_srv/bin/maclawsrv | awk '{print $1}')" = "$SRV_EXPECT" ]; then
  echo SRV_HASH_OK
else
  echo SRV_HASH_DIFF
fi
if [ "$(sha256sum /data/soft/maclaw_desktopd/bin/desktopd | awk '{print $1}')" = "$DSK_EXPECT" ]; then
  echo DSK_HASH_OK
else
  echo DSK_HASH_DIFF
fi

curl -fsS --max-time 3 http://127.0.0.1:18080/version || echo VERSION_FAIL
echo
code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:9399/api/mobile/bootstrap || echo fail)
echo "bootstrap_9399=$code"
echo "HUBCENTER=$(ps -eo pid=,args= | awk '/maclaw-hubcenter/ && !/awk/ {print $1; exit}')"
echo DESKTOPS
docker ps -a --filter name=maclaw-desktop- --format '{{.ID}} {{.Status}} {{.Names}}' || true

SPID=$(systemctl show -p MainPID --value maclawsrv.service)
if [ -r "/proc/$SPID/environ" ]; then
  tr '\0' '\n' < "/proc/$SPID/environ" | awk -F= '
    $1=="MACLAW_DESKTOP_API_TOKEN" { print "SRV_TOKEN_LEN=" length($2) }
    $1=="MACLAW_HUB_URL" { print "SRV_HUB_URL_LEN=" length($2) }
  '
fi

python3 - <<'PY'
def lens(path, label):
    tok = url = 0
    fh = open(path)
    for line in fh:
        line = line.strip()
        if line.startswith("MACLAW_DESKTOP_API_TOKEN="):
            tok = len(line.split("=", 1)[1].strip().strip('"').strip("'"))
        if line.startswith("MACLAW_HUB_URL="):
            url = len(line.split("=", 1)[1].strip().strip('"').strip("'"))
    print("%s_TOKEN_LEN=%s %s_URL_LEN=%s" % (label, tok, label, url))
lens("/data/soft/maclaw_srv/.env", "SRVFILE")
lens("/data/soft/hub/desktop-api.env", "HUBFILE")
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
import urllib.request
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

if [ "$final" != "$EXPECT" ]; then
  echo DRIFT
  find /data/soft/hub -maxdepth 2 -type f -mmin -20 -printf '%s %TY-%Tm-%Td %TH:%TM:%TS %p\n' | redact || true
  exit 1
fi
echo FIX_OK
