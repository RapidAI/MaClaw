#!/bin/sh
# Replace the Hub (maclaw-hub) or MaClawSrv binary on one host, with backups,
# a health check, and automatic rollback. Configs/.env are not rewritten.
#
#   deploy_hub_maclawsrv.sh hub       /path/to/new/maclaw-hub
#   deploy_hub_maclawsrv.sh maclawsrv /path/to/new/maclawsrv
#
# Backups land in /data/soft/backup/<component>-<CST timestamp>/: the old
# binary, configs, and an online SQLite backup (python3 sqlite3 API) of the
# component's databases. No secrets are printed.
set -eu
COMP=$1
NEW=$2
TS=$(TZ=Asia/Shanghai date +%Y%m%d%H%M%S)
B=/data/soft/backup/$COMP-$TS
mkdir -p "$B"
chmod 700 "$B"

dbbackup() { # src dst: online backup; old python3 without Connection.backup
  # falls back to copying the db with its -wal/-shm files.
  python3 - "$1" "$2" <<'PY' 2>/dev/null || { cp -p "$1" "$2"; for x in -wal -shm; do [ -f "$1$x" ] && cp -p "$1$x" "$2$x"; done; true; }
import sqlite3, sys
src = sqlite3.connect("file:%s?mode=ro" % sys.argv[1], uri=True)
dst = sqlite3.connect(sys.argv[2])
src.backup(dst)
dst.close(); src.close()
PY
}

case "$COMP" in
hub)
  DIR=/data/soft/hub
  BIN=$DIR/maclaw-hub
  cp -p "$BIN" "$B/"
  cp -p "$DIR/configs/config.yaml" "$DIR/start.sh" "$B/"
  [ -f "$DIR/desktop-api.env" ] && cp -p "$DIR/desktop-api.env" "$B/"
  for db in "$DIR"/data/*.db; do dbbackup "$db" "$B/$(basename "$db")"; done
  health() { curl -fsS -m 3 http://127.0.0.1:9399/healthz >/dev/null; }
  restart() { (cd "$DIR" && ./start.sh) >/dev/null 2>&1; }
  ;;
maclawsrv)
  DIR=/data/soft/maclaw_srv
  BIN=$DIR/bin/maclawsrv
  cp -p "$BIN" "$B/"
  cp -p "$DIR/.env" "$B/env"
  [ -d "$DIR/data/config" ] && cp -rp "$DIR/data/config" "$B/config"
  find "$DIR/data" -maxdepth 2 -name '*.db' | while read -r db; do dbbackup "$db" "$B/$(echo "${db#$DIR/data/}" | tr / _)"; done
  health() { curl -fsS -m 3 http://127.0.0.1:18080/version >/dev/null; }
  restart() { systemctl restart maclawsrv.service; }
  ;;
*) echo "usage: $0 hub|maclawsrv NEW_BINARY" >&2; exit 2 ;;
esac

install -m 755 "$NEW" "$BIN.new"
mv -f "$BIN.new" "$BIN"
restart
ok=0
for i in $(seq 1 30); do
  if health; then ok=1; break; fi
  sleep 2
done
if [ "$ok" != 1 ]; then
  install -m 755 "$B/$(basename "$BIN")" "$BIN.new" && mv -f "$BIN.new" "$BIN"
  restart
  sleep 5
  if health; then
    echo "Health check failed. Rolled back $COMP using the binary in $B. Read the log (Hub: data/logs/maclaw-hub.out.log; MaClawSrv: journalctl -u maclawsrv). Do not deploy this binary again until that log says what to change."
    exit 1
  fi
  echo "Rollback is in place and the health check still failed. Do not run this script again. Restore the binary from $B by hand after reading the log."
  exit 1
fi
echo "OK $COMP restarted $(TZ=Asia/Shanghai date '+%F %T') CST md5=$(md5sum "$BIN" | cut -c1-32) backup=$B"
