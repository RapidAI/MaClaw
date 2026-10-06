#!/bin/sh
# Install the desktopd binary on the Docker host and restart it.
# Called by deploy_desktopd.cmd. Does not print DESKTOPD_TOKEN.
set -eu

: "${REMOTE_TMP_DIR:=/tmp/maclaw_desktopd_deploy}"
: "${DESKTOPD_DEPLOY_DIR:=/data/soft/maclaw_desktopd}"
: "${DESKTOPD_BIND_ADDR:=127.0.0.1:18081}"
: "${DESKTOPD_PORT:=18081}"
: "${DESKTOPD_ADVERTISE_HOST:=}"
: "${DESKTOPD_IMAGE:=maclaw-gui:1}"

rand_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 48 | tr -d '\n'
  else
    dd if=/dev/urandom bs=48 count=1 2>/dev/null | base64 | tr -d '\n'
  fi
}

if [ -z "$DESKTOPD_ADVERTISE_HOST" ]; then
  echo "[remote] DESKTOPD_ADVERTISE_HOST is required" >&2
  exit 1
fi

SRC="$REMOTE_TMP_DIR/src"
ARCHIVE_PATH="$REMOTE_TMP_DIR/desktopd-deploy.tar.gz"
rm -rf "$SRC"
mkdir -p "$SRC" "$DESKTOPD_DEPLOY_DIR/bin" "$DESKTOPD_DEPLOY_DIR/image" "$DESKTOPD_DEPLOY_DIR/logs"
tar -xzf "$ARCHIVE_PATH" -C "$SRC"
cp -f "$SRC/bin/desktopd" "$DESKTOPD_DEPLOY_DIR/bin/desktopd"
cp -f "$SRC/image/desktop_supervisor.py" "$DESKTOPD_DEPLOY_DIR/image/desktop_supervisor.py"
chmod 755 "$DESKTOPD_DEPLOY_DIR/bin/desktopd" "$DESKTOPD_DEPLOY_DIR/image/desktop_supervisor.py"

if [ ! -f "$DESKTOPD_DEPLOY_DIR/.env" ]; then
  TOKEN="$(rand_secret)"
  cat > "$DESKTOPD_DEPLOY_DIR/.env" << ENVEOF
DESKTOPD_ADDR=$DESKTOPD_BIND_ADDR
DESKTOPD_TOKEN=$TOKEN
DESKTOPD_ADVERTISE_HOST=$DESKTOPD_ADVERTISE_HOST
DESKTOPD_STATE_DIR=$DESKTOPD_DEPLOY_DIR/state
ENVEOF
  chmod 600 "$DESKTOPD_DEPLOY_DIR/.env"
  echo "[remote] Created $DESKTOPD_DEPLOY_DIR/.env with a generated token."
else
  grep -q '^DESKTOPD_ADDR=' "$DESKTOPD_DEPLOY_DIR/.env" || echo "DESKTOPD_ADDR=$DESKTOPD_BIND_ADDR" >> "$DESKTOPD_DEPLOY_DIR/.env"
  # The public name only proxies this API. Do not leave the process on every interface.
  if grep -q '^DESKTOPD_ADDR=:' "$DESKTOPD_DEPLOY_DIR/.env" || grep -q '^DESKTOPD_ADDR=0.0.0.0:' "$DESKTOPD_DEPLOY_DIR/.env"; then
    sed -i "s/^DESKTOPD_ADDR=.*/DESKTOPD_ADDR=127.0.0.1:${DESKTOPD_PORT}/" "$DESKTOPD_DEPLOY_DIR/.env"
  fi
  grep -q '^DESKTOPD_TOKEN=' "$DESKTOPD_DEPLOY_DIR/.env" || echo "DESKTOPD_TOKEN=$(rand_secret)" >> "$DESKTOPD_DEPLOY_DIR/.env"
  grep -q '^DESKTOPD_ADVERTISE_HOST=' "$DESKTOPD_DEPLOY_DIR/.env" || echo "DESKTOPD_ADVERTISE_HOST=$DESKTOPD_ADVERTISE_HOST" >> "$DESKTOPD_DEPLOY_DIR/.env"
  grep -q '^DESKTOPD_STATE_DIR=' "$DESKTOPD_DEPLOY_DIR/.env" || echo "DESKTOPD_STATE_DIR=$DESKTOPD_DEPLOY_DIR/state" >> "$DESKTOPD_DEPLOY_DIR/.env"
fi
mkdir -p "$DESKTOPD_DEPLOY_DIR/state"

install_supervisor() {
  script="$DESKTOPD_DEPLOY_DIR/image/desktop_supervisor.py"
  if ! command -v docker >/dev/null 2>&1; then
    echo "[remote] docker is not installed; skipped supervisor image update."
    return 0
  fi
  dockerfile="$SRC/image/Dockerfile"
  if [ -f "$dockerfile" ] && docker image inspect "$DESKTOPD_IMAGE" >/dev/null 2>&1; then
    docker build -f "$dockerfile" -t "$DESKTOPD_IMAGE" "$SRC/image"
    echo "[remote] Built $DESKTOPD_IMAGE with the desktop supervisor, noVNC, and /desktops"
  elif docker image inspect "$DESKTOPD_IMAGE" >/dev/null 2>&1; then
    cid="$(docker create "$DESKTOPD_IMAGE")"
    docker cp "$script" "$cid":/desktop_supervisor.py
    docker commit "$cid" "$DESKTOPD_IMAGE" >/dev/null
    docker rm "$cid" >/dev/null
    echo "[remote] Updated $DESKTOPD_IMAGE with desktop_supervisor.py"
  else
    echo "[remote] Image $DESKTOPD_IMAGE is not present yet."
  fi
  for cid in $(docker ps -aq --filter name=maclaw-desktop-); do
    docker cp "$script" "$cid":/desktop_supervisor.py || true
  done
  if docker ps -aq --filter name='^maclaw-gui$' | grep -q .; then
    docker cp "$script" maclaw-gui:/desktop_supervisor.py || true
  fi
}
install_supervisor

cat > /etc/systemd/system/maclaw-desktopd.service << SERVICEEOF
[Unit]
Description=MaClaw Docker desktop service
After=network.target docker.service
Wants=docker.service

[Service]
Type=simple
WorkingDirectory=$DESKTOPD_DEPLOY_DIR
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
EnvironmentFile=$DESKTOPD_DEPLOY_DIR/.env
ExecStart=$DESKTOPD_DEPLOY_DIR/bin/desktopd
Restart=always
RestartSec=3
StandardOutput=append:$DESKTOPD_DEPLOY_DIR/logs/desktopd.log
StandardError=append:$DESKTOPD_DEPLOY_DIR/logs/desktopd.err.log

[Install]
WantedBy=multi-user.target
SERVICEEOF

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
  systemctl enable maclaw-desktopd.service >/dev/null 2>&1 || true
  systemctl restart maclaw-desktopd.service
  systemctl --no-pager --full status maclaw-desktopd.service | head -n 20 || true
else
  cd "$DESKTOPD_DEPLOY_DIR"
  set -a
  . ./.env
  set +a
  pkill -f "bin/desktopd" 2>/dev/null || true
  sleep 1
  nohup ./bin/desktopd > ./logs/desktopd.log 2>&1 &
  echo "desktopd started (PID: $!)"
fi

set -a
. "$DESKTOPD_DEPLOY_DIR/.env"
set +a
sleep 1
if command -v curl >/dev/null 2>&1; then
  curl -fsS -H "Authorization: Bearer $DESKTOPD_TOKEN" "http://127.0.0.1:${DESKTOPD_PORT}/v1/health"
  echo
else
  echo "[remote] curl is missing; skipped health check."
fi

rm -rf "$SRC" "$ARCHIVE_PATH" "$REMOTE_TMP_DIR/remote_deploy.sh"
echo "desktopd deployed to $DESKTOPD_DEPLOY_DIR"
echo "advertise host: $DESKTOPD_ADVERTISE_HOST"
echo "token file: $DESKTOPD_DEPLOY_DIR/.env"
