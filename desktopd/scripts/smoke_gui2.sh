#!/bin/sh
# End-to-end check of maclaw-gui:2 and the v1 -> v2 migration on a Docker host,
# using a dedicated test user. Run as root on the desktopd host; it only
# touches the test user's container, volumes, and images.
#
#   sudo TEST_USER=rollout-test-gui2 desktopd/scripts/smoke_gui2.sh
#   sudo TEST_USER=rollout-test-gui2b MIGRATE=0 DESKTOPD_URL=https://dockerd.example.com desktopd/scripts/smoke_gui2.sh
#
# MIGRATE=1 (default): open the desktop with OLD_IMAGE first, write a marker
# into each of the 4 private volumes, then open it with NEW_IMAGE and check the
# recreate (label, :prev image, same volumes, markers kept). MIGRATE=0 only
# checks NEW_IMAGE. A user that already exists on NEW_IMAGE is not recreated.
#
# Checks: maclaw.image label, resources, :prev, volumes, screenshot PNG via
# desktopd, XFCE processes, CDP over the token gate, CJK fonts, fcitx5 pinyin
# typing into Chromium ("nihao shijie" -> 你好世界), and desktopd log errors.
# Leaves the desktop running unless STOP_AFTER=1. Never prints tokens.
#
# Environment (defaults):
#   TENANT_ID=tenant_default TEST_USER=rollout-test-gui2 MIGRATE=1 STOP_AFTER=0
#   OLD_IMAGE=maclaw-gui:1 OLD_MEMORY=2500m OLD_CPUS=1.5 OLD_SHM=512m
#   NEW_IMAGE=maclaw-gui:2 NEW_MEMORY=3g NEW_CPUS=1.5 NEW_SHM=1g
#   OUT_DIR=/tmp/maclaw-gui2-smoke   DESKTOPD_URL / DESKTOPD_ENV_FILE: see dapi.sh
set -u

HERE=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
DAPI="$HERE/dapi.sh"
CDP="$HERE/cdp.py"
TENANT_ID="${TENANT_ID:-tenant_default}"
TEST_USER="${TEST_USER:-rollout-test-gui2}"
MIGRATE="${MIGRATE:-1}"
STOP_AFTER="${STOP_AFTER:-0}"
OLD_IMAGE="${OLD_IMAGE:-maclaw-gui:1}"; OLD_MEMORY="${OLD_MEMORY:-2500m}"; OLD_CPUS="${OLD_CPUS:-1.5}"; OLD_SHM="${OLD_SHM:-512m}"
NEW_IMAGE="${NEW_IMAGE:-maclaw-gui:2}"; NEW_MEMORY="${NEW_MEMORY:-3g}"; NEW_CPUS="${NEW_CPUS:-1.5}"; NEW_SHM="${NEW_SHM:-1g}"
OUT_DIR="${OUT_DIR:-/tmp/maclaw-gui2-smoke}"
mkdir -p "$OUT_DIR"; chmod 700 "$OUT_DIR"
FAILS=0

pass() { echo "PASS  $*"; }
fail() { echo "FAIL  $*"; FAILS=$((FAILS + 1)); }
redact() { sed -E 's/desktop:[0-9a-f]{16,}@/desktop:<redacted>@/g'; }
body() { printf '{"tenant_id":"%s","user_id":"%s","image":"%s","memory":"%s","cpus":"%s","shm_size":"%s"}' "$TENANT_ID" "$TEST_USER" "$1" "$2" "$3" "$4"; }
field() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get(sys.argv[2], ""))' "$1" "$2"; }
open_desktop() {
  "$DAPI" POST /v1/desktops/session "$(body "$@")" > "$OUT_DIR/session.json"
  if ! grep -q '"cdp_url"' "$OUT_DIR/session.json"; then
    fail "open desktop ($1): $(redact < "$OUT_DIR/session.json")"
    return 1
  fi
  chmod 600 "$OUT_DIR/session.json"
  CONTAINER=$(field "$OUT_DIR/session.json" container)
  KEY=${CONTAINER#maclaw-desktop-}
}
vols() { docker inspect "$CONTAINER" --format '{{range .Mounts}}{{.Name}}:{{.Destination}}{{"\n"}}{{end}}' | sort; }
markers() { docker exec "$CONTAINER" sh -c 'cat /desktops/.maclaw_smoke /home/desktop/.maclaw_smoke /opt/.maclaw_smoke /usr/local/.maclaw_smoke 2>/dev/null | md5sum'; }

echo "== desktopd health: $("$DAPI" GET /v1/health)"
START=$(date '+%Y-%m-%d %H:%M:%S')

if [ "$MIGRATE" = "1" ]; then
  open_desktop "$OLD_IMAGE" "$OLD_MEMORY" "$OLD_CPUS" "$OLD_SHM" || exit 1
  if [ "$(docker inspect "$CONTAINER" --format '{{index .Config.Labels "maclaw.image"}}')" = "$NEW_IMAGE" ]; then
    echo "NOTE  $CONTAINER is already on $NEW_IMAGE; desktopd does not move users back. Use a new TEST_USER for the migration check."
    MIGRATE=0
  else
    sleep 5
    docker exec "$CONTAINER" sh -c 'v=smoke-$(date +%s); for d in /desktops /home/desktop /opt /usr/local; do echo "$v" > "$d/.maclaw_smoke"; done'
    BEFORE_MARKS=$(markers); BEFORE_VOLS=$(vols)
  fi
fi

open_desktop "$NEW_IMAGE" "$NEW_MEMORY" "$NEW_CPUS" "$NEW_SHM" || exit 1
echo "      container $CONTAINER"
LABEL=$(docker inspect "$CONTAINER" --format '{{index .Config.Labels "maclaw.image"}}')
[ "$LABEL" = "$NEW_IMAGE" ] && pass "label maclaw.image=$LABEL" || fail "label maclaw.image=$LABEL, want $NEW_IMAGE"
RES=$(docker inspect "$CONTAINER" --format '{{.HostConfig.Memory}} {{.HostConfig.ShmSize}} {{.HostConfig.NanoCpus}}')
echo "      memory/shm/nanocpus: $RES"

if [ "$MIGRATE" = "1" ]; then
  PREV="maclaw-desktop-user-$KEY:prev"
  PL=$(docker image inspect "$PREV" --format '{{index .Config.Labels "maclaw.image"}}' 2>/dev/null)
  [ -n "$PL" ] && pass "$PREV exists (committed from $PL)" || fail "$PREV missing"
  [ "$(vols)" = "$BEFORE_VOLS" ] && pass "4 private volumes unchanged" || fail "volumes changed: $(vols | tr '\n' ' ')"
  [ "$(markers)" = "$BEFORE_MARKS" ] && pass "volume markers kept" || fail "volume markers differ"
fi

sleep "${SETTLE_SECONDS:-12}"
"$DAPI" GET "/v1/desktops/screenshot?tenant_id=$TENANT_ID&user_id=$TEST_USER" > "$OUT_DIR/screenshot.png"
if head -c 8 "$OUT_DIR/screenshot.png" | od -An -c | grep -q 'P   N   G'; then pass "screenshot $OUT_DIR/screenshot.png ($(wc -c < "$OUT_DIR/screenshot.png") bytes)"; else fail "screenshot is not a PNG"; fi

for proc in xfce4-session xfwm4 xfce4-panel xfdesktop fcitx5 Xvfb x11vnc; do
  docker exec "$CONTAINER" pgrep -x "$proc" >/dev/null && pass "process $proc" || fail "process $proc missing"
done

HOSTPORT=$(field "$OUT_DIR/session.json" cdp_url | sed -E 's/.*:([0-9]+)$/\1/')
V=$(python3 "$CDP" --session "$OUT_DIR/session.json" --host 127.0.0.1 version 2>&1)
case "$V" in *Chrome*) pass "CDP via gate :$HOSTPORT $V" ;; *) fail "CDP: $V" ;; esac
NOAUTH=$(curl -s -o /dev/null -w '%{http_code}' -m 10 "http://127.0.0.1:$HOSTPORT/json/version")
[ "$NOAUTH" != "200" ] && pass "CDP gate refuses requests without token ($NOAUTH)" || fail "CDP gate answered without token"

FONT=$(docker exec "$CONTAINER" fc-match 'sans-serif:lang=zh-cn' 2>/dev/null)
case "$FONT" in *CJK*) pass "CJK font: $FONT" ;; *) fail "CJK font: $FONT" ;; esac

PAGE='data:text/html;charset=utf-8,%3Cmeta%20charset%3Dutf-8%3E%3Ctextarea%20id%3Dt%20autofocus%20style%3D%22width%3A600px%3Bheight%3A160px%3Bfont-size%3A32px%22%3E%3C%2Ftextarea%3E%3Cp%20style%3Dfont-size%3A32px%3E%E4%B8%AD%E6%96%87%E5%AD%97%E4%BD%93%E6%B5%8B%E8%AF%95%3C%2Fp%3E'
python3 "$CDP" --session "$OUT_DIR/session.json" navigate "$PAGE" >/dev/null
sleep 3
python3 "$CDP" --session "$OUT_DIR/session.json" eval 'document.getElementById("t").focus(); 1' >/dev/null
BUS="unix:path=/tmp/.maclaw-dbus-20"
docker exec -e DISPLAY=:20 "$CONTAINER" sh -c 'xdotool mousemove 300 300 click 1; sleep 1'
docker exec -e DISPLAY=:20 -e DBUS_SESSION_BUS_ADDRESS="$BUS" "$CONTAINER" sh -c 'fcitx5-remote -o; sleep 1'
IM=$(docker exec -e DISPLAY=:20 -e DBUS_SESSION_BUS_ADDRESS="$BUS" "$CONTAINER" fcitx5-remote -n 2>/dev/null)
docker exec -e DISPLAY=:20 "$CONTAINER" sh -c 'xdotool type --delay 150 nihao; sleep 1; xdotool key space; sleep 1; xdotool type --delay 150 shijie; sleep 1; xdotool key space; sleep 1'
TYPED=$(python3 "$CDP" --session "$OUT_DIR/session.json" eval 'document.getElementById("t").value')
[ "$TYPED" = '"你好世界"' ] && pass "fcitx5 $IM typed $TYPED in Chromium" || fail "pinyin typing gave $TYPED (im=$IM)"
"$DAPI" GET "/v1/desktops/screenshot?tenant_id=$TENANT_ID&user_id=$TEST_USER" > "$OUT_DIR/screenshot-pinyin.png"

ERRS=$(journalctl -u maclaw-desktopd --since "$START" --no-pager 2>/dev/null | grep -v 'Failed to parse output specifier' | grep -iE 'error|panic|fatal')
[ -z "$ERRS" ] && pass "no desktopd errors since $START" || fail "desktopd errors: $ERRS"
journalctl -u maclaw-desktopd --since "$START" --no-pager 2>/dev/null | grep -E 'recreating|:prev' | sed 's/^/      /'

if [ "$STOP_AFTER" = "1" ]; then
  "$DAPI" POST /v1/desktops/stop "{\"tenant_id\":\"$TENANT_ID\",\"user_id\":\"$TEST_USER\"}" >/dev/null && echo "      stopped $CONTAINER"
fi
rm -f "$OUT_DIR/session.json"
echo "== $FAILS failure(s); screenshots in $OUT_DIR"
[ "$FAILS" -eq 0 ]
