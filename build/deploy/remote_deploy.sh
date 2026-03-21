#!/bin/sh
set -eu

: "${REMOTE_TMP_DIR:=/tmp/aicoder_deploy}"
: "${REMOTE_HUB_DIR:=/data/soft/hub}"
: "${REMOTE_HUBCENTER_DIR:=/data/soft/hubcenter}"
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

echo "[remote] Building hub..."
GOPROXY="$GOPROXY" CGO_ENABLED="$CGO_ENABLED" go build -o "$BUILD_ROOT/maclaw-hub" ./hub/cmd/hub
echo "[remote] Building hubcenter..."
GOPROXY="$GOPROXY" CGO_ENABLED="$CGO_ENABLED" go build -o "$BUILD_ROOT/maclaw-hubcenter" ./hubcenter/cmd/hubcenter

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

rm -rf "$SRC_ROOT" "$BUILD_ROOT"
rm -f "$ARCHIVE_PATH" "$REMOTE_TMP_DIR/remote_deploy.sh"
echo "Remote build and deploy finished."
