#!/bin/sh
set -eu
: "${REMOTE_TMP_DIR:=/tmp/maclawsrv_deploy}"
: "${MACLAWSRV_DEPLOY_DIR:=/data/soft/maclaw_srv}"
: "${MACLAWSRV_BIND_ADDR:=:18080}"
: "${MACLAW_ADMIN_WEB_DEFAULT_LOCALE:=zh-CN}"
: "${MACLAW_ENABLE_SCHEDULER:=true}"
: "${MACLAW_ALLOW_INSECURE_HTTP:=true}"

rand_secret() {
  if command -v openssl > /dev/null 2>&1; then
    openssl rand -base64 48 | tr -d '\n'
  else
    dd if=/dev/urandom bs=48 count=1 2>/dev/null | base64 | tr -d '\n'
  fi
}

SRC="$REMOTE_TMP_DIR/src"
ARCHIVE_PATH="$REMOTE_TMP_DIR/maclawsrv-deploy.tar.gz"
rm -rf "$SRC"
mkdir -p "$SRC" "$MACLAWSRV_DEPLOY_DIR/bin" "$MACLAWSRV_DEPLOY_DIR/data" "$MACLAWSRV_DEPLOY_DIR/logs"
tar -xzf "$ARCHIVE_PATH" -C "$SRC"
cp -f "$SRC/bin/maclawsrv" "$MACLAWSRV_DEPLOY_DIR/bin/maclawsrv"
chmod +x "$MACLAWSRV_DEPLOY_DIR/bin/maclawsrv"

if [ ! -f "$MACLAWSRV_DEPLOY_DIR/.env" ]; then
  ADMIN_SECRET="$(rand_secret)"
  TOKEN_SECRET="$(rand_secret)"
  cat > "$MACLAWSRV_DEPLOY_DIR/.env" << ENVEOF
MACLAW_DATA_ROOT=$MACLAWSRV_DEPLOY_DIR/data
MACLAW_HTTP_ADDR=$MACLAWSRV_BIND_ADDR
MACLAW_ALLOW_INSECURE_HTTP=$MACLAW_ALLOW_INSECURE_HTTP
MACLAW_ADMIN_SECRET=$ADMIN_SECRET
MACLAW_TOKEN_SECRET=$TOKEN_SECRET
MACLAW_ADMIN_WEB_DEFAULT_LOCALE=$MACLAW_ADMIN_WEB_DEFAULT_LOCALE
MACLAW_ENABLE_SCHEDULER=$MACLAW_ENABLE_SCHEDULER
ENVEOF
  chmod 600 "$MACLAWSRV_DEPLOY_DIR/.env"
  echo "[remote] Created $MACLAWSRV_DEPLOY_DIR/.env with generated secrets."
else
  grep -q 'MACLAW_DATA_ROOT=' "$MACLAWSRV_DEPLOY_DIR/.env" || echo "MACLAW_DATA_ROOT=$MACLAWSRV_DEPLOY_DIR/data" >> "$MACLAWSRV_DEPLOY_DIR/.env"
  grep -q 'MACLAW_HTTP_ADDR=' "$MACLAWSRV_DEPLOY_DIR/.env" || echo "MACLAW_HTTP_ADDR=$MACLAWSRV_BIND_ADDR" >> "$MACLAWSRV_DEPLOY_DIR/.env"
  grep -q 'MACLAW_ALLOW_INSECURE_HTTP=' "$MACLAWSRV_DEPLOY_DIR/.env" || echo "MACLAW_ALLOW_INSECURE_HTTP=$MACLAW_ALLOW_INSECURE_HTTP" >> "$MACLAWSRV_DEPLOY_DIR/.env"
  grep -q 'MACLAW_ADMIN_WEB_DEFAULT_LOCALE=' "$MACLAWSRV_DEPLOY_DIR/.env" || echo "MACLAW_ADMIN_WEB_DEFAULT_LOCALE=$MACLAW_ADMIN_WEB_DEFAULT_LOCALE" >> "$MACLAWSRV_DEPLOY_DIR/.env"
  grep -q 'MACLAW_ENABLE_SCHEDULER=' "$MACLAWSRV_DEPLOY_DIR/.env" || echo "MACLAW_ENABLE_SCHEDULER=$MACLAW_ENABLE_SCHEDULER" >> "$MACLAWSRV_DEPLOY_DIR/.env"
fi

cat > "$MACLAWSRV_DEPLOY_DIR/start.sh" << 'STARTEOF'
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
chmod +x "$MACLAWSRV_DEPLOY_DIR/start.sh"

if command -v systemctl > /dev/null 2>&1; then
  cat > /etc/systemd/system/maclawsrv.service << SERVICEEOF
[Unit]
Description=MaClawSrv REST service
After=network.target

[Service]
Type=simple
WorkingDirectory=$MACLAWSRV_DEPLOY_DIR
EnvironmentFile=$MACLAWSRV_DEPLOY_DIR/.env
ExecStart=$MACLAWSRV_DEPLOY_DIR/bin/maclawsrv
Restart=always
RestartSec=3
StandardOutput=append:$MACLAWSRV_DEPLOY_DIR/logs/maclawsrv.log
StandardError=append:$MACLAWSRV_DEPLOY_DIR/logs/maclawsrv.err.log

[Install]
WantedBy=multi-user.target
SERVICEEOF
  systemctl daemon-reload
  systemctl enable maclawsrv.service > /dev/null 2>&1 || true
  systemctl restart maclawsrv.service
  systemctl --no-pager --full status maclawsrv.service | head -n 20 || true
else
  cd "$MACLAWSRV_DEPLOY_DIR" && ./start.sh
fi

rm -rf "$SRC" "$ARCHIVE_PATH" "$REMOTE_TMP_DIR/remote_deploy_maclawsrv.sh"
echo "MaClawSrv deployed to $MACLAWSRV_DEPLOY_DIR"
