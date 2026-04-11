#!/bin/sh
set -eu

: "${REMOTE_TMP_DIR:=/tmp/aicoder_deploy}"
: "${REMOTE_HUB_DIR:=/data/soft/hub}"
: "${REMOTE_HUBCENTER_DIR:=/data/soft/hubcenter}"
: "${REMOTE_IWC_DIR:=/data/soft/iworkercenter}"
: "${REMOTE_IWCLOUD_DIR:=/data/soft/iworkercloud}"
: "${CGO_ENABLED:=0}"
: "${GOPROXY:=https://goproxy.cn,direct}"

if ! command -v go >/dev/null 2>&1; then
  echo "[ERROR] go is not installed on remote host" >&2
  exit 1
fi

SRC_ROOT="$REMOTE_TMP_DIR/src"
BUILD_ROOT="$REMOTE_TMP_DIR/build"
ARCHIVE_PATH="$REMOTE_TMP_DIR/maclaw-src.tar.gz"

rm -rf "$SRC_ROOT" "$BUILD_ROOT"
mkdir -p "$SRC_ROOT" "$BUILD_ROOT"
tar -xzf "$ARCHIVE_PATH" -C "$SRC_ROOT"
cd "$SRC_ROOT"

echo "[remote] Downloading dependencies..."
GOPROXY="$GOPROXY" go mod download

# Optional: Build RapidSpeech static library for cgo_embedding support.
# If CMake/compiler is available and build succeeds, CGO_ENABLED is set to 1
# and the cgo_embedding tag is added. Otherwise the Go build proceeds without it.
RS_BUILD_SCRIPT="$SRC_ROOT/build/build_rapidspeech.sh"
RS_LIB="$SRC_ROOT/RapidSpeech.cpp/build/librapidspeech_static.a"
EXTRA_TAGS=""
if [ -f "$RS_BUILD_SCRIPT" ]; then
  echo "[remote] Building RapidSpeech static library (optional)..."
  chmod +x "$RS_BUILD_SCRIPT"
  if "$RS_BUILD_SCRIPT" && [ -f "$RS_LIB" ]; then
    echo "[remote] RapidSpeech built. Enabling cgo_embedding."
    CGO_ENABLED=1
    EXTRA_TAGS="cgo_embedding"
  else
    echo "[remote] RapidSpeech build skipped or failed. Continuing without cgo_embedding."
  fi
fi

echo "[remote] Building hub..."
GOPROXY="$GOPROXY" CGO_ENABLED="$CGO_ENABLED" go build -tags "$EXTRA_TAGS" -o "$BUILD_ROOT/maclaw-hub" ./hub/cmd/hub
echo "[remote] Building hubcenter..."
GOPROXY="$GOPROXY" CGO_ENABLED="$CGO_ENABLED" go build -tags "$EXTRA_TAGS" -o "$BUILD_ROOT/maclaw-hubcenter" ./hubcenter/cmd/hubcenter
echo "[remote] Building iworkercenter..."
GOPROXY="$GOPROXY" CGO_ENABLED=0 go build -o "$BUILD_ROOT/iworkercenter" ./iWorkerCenter/cmd/iworkercenter
echo "[remote] Building iworkercloud..."
GOPROXY="$GOPROXY" CGO_ENABLED=0 go build -o "$BUILD_ROOT/iworkercloud" ./iWorkerCloud

deploy_one() {
  source_dir="$1"
  target_dir="$2"
  binary_path="$3"
  binary_name="$4"

  mkdir -p "$target_dir" "$target_dir/configs" "$target_dir/data" "$target_dir/data/logs"
  cp -f "$binary_path" "$target_dir/$binary_name"
  chmod +x "$target_dir/$binary_name"

  if [ -f "$source_dir/start.sh" ]; then
    cp -f "$source_dir/start.sh" "$target_dir/start.sh"
    sed -i 's/\r$//' "$target_dir/start.sh"
    chmod +x "$target_dir/start.sh"
  fi

  if [ -f "$source_dir/configs/config.example.yaml" ]; then
    cp -f "$source_dir/configs/config.example.yaml" "$target_dir/configs/config.example.yaml"
  fi

  if [ ! -f "$target_dir/configs/config.yaml" ] && [ -f "$target_dir/configs/config.example.yaml" ]; then
    cp -f "$target_dir/configs/config.example.yaml" "$target_dir/configs/config.yaml"
  fi

  if [ -d "$source_dir/web" ]; then
    rm -rf "$target_dir/web"
    cp -R "$source_dir/web" "$target_dir/web"
  fi
}

echo "[remote] Deploying hub files..."
deploy_one "$SRC_ROOT/hub" "$REMOTE_HUB_DIR" "$BUILD_ROOT/maclaw-hub" "maclaw-hub"
echo "[remote] Deploying hubcenter files..."
deploy_one "$SRC_ROOT/hubcenter" "$REMOTE_HUBCENTER_DIR" "$BUILD_ROOT/maclaw-hubcenter" "maclaw-hubcenter"

echo "[remote] Deploying iworkercenter files..."
mkdir -p "$REMOTE_IWC_DIR" "$REMOTE_IWC_DIR/data"
cp -f "$BUILD_ROOT/iworkercenter" "$REMOTE_IWC_DIR/iworkercenter"
chmod +x "$REMOTE_IWC_DIR/iworkercenter"
if [ -d "$SRC_ROOT/iWorkerCenter/cmd/iworkercenter/web" ]; then
  rm -rf "$REMOTE_IWC_DIR/web"
  cp -R "$SRC_ROOT/iWorkerCenter/cmd/iworkercenter/web" "$REMOTE_IWC_DIR/web"
fi
# Write iworkercenter start script if missing
if [ ! -f "$REMOTE_IWC_DIR/start.sh" ]; then
  cat > "$REMOTE_IWC_DIR/start.sh" << 'IWCEOF'
#!/bin/sh
cd "$(dirname "$0")"
pkill -f "iworkercenter" 2>/dev/null || true
sleep 1
nohup ./iworkercenter -addr :9377 > data/iworkercenter.log 2>&1 &
echo "iWorkerCenter started on :9377 (PID: $!)"
IWCEOF
  chmod +x "$REMOTE_IWC_DIR/start.sh"
fi

echo "[remote] Deploying iworkercloud files..."
mkdir -p "$REMOTE_IWCLOUD_DIR" "$REMOTE_IWCLOUD_DIR/data"
cp -f "$BUILD_ROOT/iworkercloud" "$REMOTE_IWCLOUD_DIR/iworkercloud"
chmod +x "$REMOTE_IWCLOUD_DIR/iworkercloud"
if [ -d "$SRC_ROOT/iWorkerCloud/web" ]; then
  rm -rf "$REMOTE_IWCLOUD_DIR/web"
  cp -R "$SRC_ROOT/iWorkerCloud/web" "$REMOTE_IWCLOUD_DIR/web"
fi
# Write iworkercloud start script if missing
if [ ! -f "$REMOTE_IWCLOUD_DIR/start.sh" ]; then
  cat > "$REMOTE_IWCLOUD_DIR/start.sh" << 'IWCLOUDEOF'
#!/bin/sh
cd "$(dirname "$0")"
pkill -f "iworkercloud" 2>/dev/null || true
sleep 1
nohup ./iworkercloud > data/iworkercloud.log 2>&1 &
echo "iWorkerCloud started on :9366 (PID: $!)"
IWCLOUDEOF
  chmod +x "$REMOTE_IWCLOUD_DIR/start.sh"
fi

# Deploy openclaw-bridge (Node.js project)
BRIDGE_SRC="$SRC_ROOT/openclaw-bridge"
BRIDGE_DST="$REMOTE_HUB_DIR/openclaw-bridge"
if [ -d "$BRIDGE_SRC" ] && [ -f "$BRIDGE_SRC/package.json" ]; then
  echo "[remote] Deploying openclaw-bridge..."
  mkdir -p "$BRIDGE_DST"
  cp -f "$BRIDGE_SRC/package.json" "$BRIDGE_DST/package.json"
  cp -f "$BRIDGE_SRC/tsconfig.json" "$BRIDGE_DST/tsconfig.json" 2>/dev/null || true
  rm -rf "$BRIDGE_DST/src" "$BRIDGE_DST/dist"
  cp -Rf "$BRIDGE_SRC/src" "$BRIDGE_DST/src"
  if [ -f "$BRIDGE_SRC/config.example.json" ]; then
    cp -f "$BRIDGE_SRC/config.example.json" "$BRIDGE_DST/config.example.json"
  fi
  if command -v npm >/dev/null 2>&1; then
    echo "[remote] Running npm install in openclaw-bridge..."
    cd "$BRIDGE_DST" && npm install 2>&1 || echo "[WARN] npm install failed for openclaw-bridge"
    echo "[remote] Building openclaw-bridge..."
    npx tsc 2>&1 || echo "[WARN] tsc build failed for openclaw-bridge"
    echo "[remote] Pruning dev dependencies..."
    npm prune --production 2>&1 || true
    cd "$SRC_ROOT"
  else
    echo "[WARN] npm not found on remote host, skipping openclaw-bridge dependencies"
  fi
else
  echo "[remote] openclaw-bridge source not found, skipping"
fi

echo "[remote] Restarting hub..."
if [ -x "$REMOTE_HUB_DIR/start.sh" ]; then
  cd "$REMOTE_HUB_DIR"
  ./start.sh
fi
echo "[remote] Restarting hubcenter..."
if [ -x "$REMOTE_HUBCENTER_DIR/start.sh" ]; then
  cd "$REMOTE_HUBCENTER_DIR"
  ./start.sh
fi
echo "[remote] Restarting iworkercenter..."
if [ -x "$REMOTE_IWC_DIR/start.sh" ]; then
  cd "$REMOTE_IWC_DIR"
  ./start.sh
fi
echo "[remote] Restarting iworkercloud..."
if [ -x "$REMOTE_IWCLOUD_DIR/start.sh" ]; then
  cd "$REMOTE_IWCLOUD_DIR"
  ./start.sh
fi

rm -rf "$SRC_ROOT" "$BUILD_ROOT"
rm -f "$ARCHIVE_PATH" "$REMOTE_TMP_DIR/remote_deploy.sh"
echo "Remote build and deploy finished."
