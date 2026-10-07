#!/bin/sh
# Install the desktopd binary on the Docker host and restart it.
# Called by deploy_desktopd.cmd. Does not print DESKTOPD_TOKEN.
set -eu

: "${REMOTE_TMP_DIR:=/tmp/maclaw_desktopd_deploy}"
: "${DESKTOPD_DEPLOY_DIR:=/data/soft/maclaw_desktopd}"
: "${DESKTOPD_BIND_ADDR:=127.0.0.1:18081}"
: "${DESKTOPD_PORT:=18081}"
: "${DESKTOPD_ADVERTISE_HOST:=}"
: "${DESKTOPD_IMAGE:=maclaw-gui:2}"
# Build inputs for maclaw-gui:2 (desktopd/image/Dockerfile.v2). Empty means
# auto: Tencent Cloud mirrors when the Tencent metadata service answers (no
# Docker Hub there), Docker Hub and deb.debian.org otherwise. APT_MIRROR=none
# forces Debian's own apt sources.
: "${DESKTOPD_BASE_IMAGE:=}"
: "${DESKTOPD_APT_MIRROR:=}"
# 1 skips the image build and requires DESKTOPD_IMAGE to exist already.
: "${DESKTOPD_SKIP_IMAGE_BUILD:=0}"
# Where the deploy gets maclaw-gui:2 from:
#   build (default)  docker build Dockerfile.v2 on this host (production: Tencent mirrors)
#   pull             docker pull DESKTOPD_IMAGE_SOURCE, check it, tag it DESKTOPD_IMAGE
#   auto             pull with DESKTOPD_IMAGE_PULL_TIMEOUT, build when that fails
# Either way the image is checked before it is tagged, and a failure leaves
# the existing image alone.
: "${DESKTOPD_IMAGE_FROM:=build}"
# Remember whether the caller chose these, so only explicit values reach .env
# (desktopd itself defaults to ghcr.io/rapidai/maclaw-gui:2 for a missing image).
IMAGE_SOURCE_GIVEN="${DESKTOPD_IMAGE_SOURCE+x}"
PULL_TIMEOUT_GIVEN="${DESKTOPD_IMAGE_PULL_TIMEOUT+x}"
: "${DESKTOPD_IMAGE_SOURCE:=ghcr.io/rapidai/maclaw-gui:2}"
# coreutils timeout syntax: 30m, 1800s, 1h.
: "${DESKTOPD_IMAGE_PULL_TIMEOUT:=30m}"

rand_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 48 | tr -d '\n'
  else
    dd if=/dev/urandom bs=48 count=1 2>/dev/null | base64 | tr -d '\n'
  fi
}

case "$DESKTOPD_IMAGE_FROM" in
  build|pull|auto) ;;
  *) echo "[remote] DESKTOPD_IMAGE_FROM must be build, pull, or auto (got $DESKTOPD_IMAGE_FROM)" >&2; exit 1 ;;
esac

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
# The whole image directory is kept so the image can be rebuilt by hand:
#   docker build -f $DESKTOPD_DEPLOY_DIR/image/Dockerfile.v2 ... $DESKTOPD_DEPLOY_DIR/image
for file in "$SRC"/image/*; do
  cp -f "$file" "$DESKTOPD_DEPLOY_DIR/image/"
done
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

# set_env_key KEY VALUE replaces or appends one .env line (values are image
# references or durations; no secrets).
set_env_key() {
  if grep -q "^$1=" "$DESKTOPD_DEPLOY_DIR/.env"; then
    sed -i "s|^$1=.*|$1=$2|" "$DESKTOPD_DEPLOY_DIR/.env"
  else
    echo "$1=$2" >> "$DESKTOPD_DEPLOY_DIR/.env"
  fi
}
if [ -n "$IMAGE_SOURCE_GIVEN" ]; then set_env_key DESKTOPD_IMAGE_SOURCE "$DESKTOPD_IMAGE_SOURCE"; fi
if [ -n "$PULL_TIMEOUT_GIVEN" ]; then set_env_key DESKTOPD_IMAGE_PULL_TIMEOUT "$DESKTOPD_IMAGE_PULL_TIMEOUT"; fi

on_tencent_cloud() {
  command -v curl >/dev/null 2>&1 &&
    curl -fsS -m 2 http://metadata.tencentyun.com/latest/meta-data/instance-id >/dev/null 2>&1
}

# check_image_contract IMAGE: the files and tools desktopd and
# desktop_supervisor.py rely on. desktopd runs the same check after its own
# source pulls (imageContractScript in desktopd/image_source.go).
check_image_contract() {
  docker run --rm --entrypoint sh "$1" -c '
      test -f /desktop_supervisor.py && test -f /usr/share/novnc/vnc.html &&
      for tool in python3 Xvfb x11vnc websockify xdotool chromium startxfce4 dbus-launch import; do
        command -v "$tool" >/dev/null || { echo "missing $tool"; exit 1; }
      done' >&2
}

# pull_desktop_image pulls DESKTOPD_IMAGE_SOURCE (a tag or an @sha256 digest),
# checks it, and tags it DESKTOPD_IMAGE. Returns 1 without touching
# DESKTOPD_IMAGE when the pull or the check fails.
pull_desktop_image() {
  src="$DESKTOPD_IMAGE_SOURCE"
  case "$src" in
    ""|off|none) echo "[remote] DESKTOPD_IMAGE_SOURCE is off; nothing to pull" >&2; return 1 ;;
  esac
  log="$DESKTOPD_DEPLOY_DIR/logs/image-pull.log"
  echo "[remote] Pulling $src for $DESKTOPD_IMAGE (timeout $DESKTOPD_IMAGE_PULL_TIMEOUT); log: $log"
  if command -v timeout >/dev/null 2>&1; then
    timeout "$DESKTOPD_IMAGE_PULL_TIMEOUT" docker pull "$src" > "$log" 2>&1 || {
      tail -n 5 "$log" >&2 || true
      echo "[remote] Pulling $src failed or timed out" >&2
      return 1
    }
  else
    docker pull "$src" > "$log" 2>&1 || { tail -n 5 "$log" >&2 || true; echo "[remote] Pulling $src failed" >&2; return 1; }
  fi
  id="$(docker image inspect --format '{{.Id}}' "$src" 2>/dev/null)" || { echo "[remote] $src is missing after the pull" >&2; return 1; }
  if ! check_image_contract "$id"; then
    echo "[remote] $src does not provide the desktop contract; $DESKTOPD_IMAGE left unchanged" >&2
    return 1
  fi
  docker tag "$id" "$DESKTOPD_IMAGE" || return 1
  echo "[remote] Pulled $DESKTOPD_IMAGE from $src ($id)"
}

# provide_desktop_image applies DESKTOPD_SKIP_IMAGE_BUILD / DESKTOPD_IMAGE_FROM.
provide_desktop_image() {
  if [ "$DESKTOPD_SKIP_IMAGE_BUILD" = "1" ]; then
    if ! docker image inspect "$DESKTOPD_IMAGE" >/dev/null 2>&1; then
      echo "[remote] DESKTOPD_SKIP_IMAGE_BUILD=1 but $DESKTOPD_IMAGE does not exist" >&2
      exit 1
    fi
    echo "[remote] Skipped building $DESKTOPD_IMAGE"
    return 0
  fi
  case "$DESKTOPD_IMAGE_FROM" in
    build) build_desktop_image ;;
    pull)
      if ! pull_desktop_image; then
        echo "[remote] DESKTOPD_IMAGE_FROM=pull failed; $DESKTOPD_IMAGE left unchanged" >&2
        exit 1
      fi ;;
    auto)
      if ! pull_desktop_image; then
        echo "[remote] Falling back to building $DESKTOPD_IMAGE on this host"
        build_desktop_image
      fi ;;
    *) echo "[remote] DESKTOPD_IMAGE_FROM must be build, pull, or auto (got $DESKTOPD_IMAGE_FROM)" >&2; exit 1 ;;
  esac
}

# build_desktop_image builds DESKTOPD_IMAGE from Dockerfile.v2. It builds
# under a temporary tag and checks the result first, so a failed or broken
# build never replaces the image running desktops use. Only Dockerfile.v2 is
# ever tagged as a v2 image; the legacy overlay Dockerfile is maclaw-gui:1 only.
build_desktop_image() {
  context="$DESKTOPD_DEPLOY_DIR/image"
  dockerfile="$context/Dockerfile.v2"
  if [ ! -f "$dockerfile" ]; then
    echo "[remote] $dockerfile is missing; refusing to tag another build as $DESKTOPD_IMAGE" >&2
    exit 1
  fi
  base="$DESKTOPD_BASE_IMAGE"
  mirror="$DESKTOPD_APT_MIRROR"
  if [ -z "$base" ] || [ -z "$mirror" ]; then
    if on_tencent_cloud; then
      [ -n "$base" ] || base="mirror.ccs.tencentyun.com/library/debian:bookworm"
      [ -n "$mirror" ] || mirror="mirrors.tencentyun.com"
    fi
  fi
  [ -n "$base" ] || base="debian:bookworm"
  [ "$mirror" != "none" ] || mirror=""
  candidate="maclaw-gui-build:$(date +%Y%m%d%H%M%S)"
  log="$DESKTOPD_DEPLOY_DIR/logs/image-build.log"
  echo "[remote] Building $DESKTOPD_IMAGE from Dockerfile.v2 (base $base, apt mirror ${mirror:-deb.debian.org}); log: $log"
  attempt=1
  while :; do
    if docker build -f "$dockerfile" \
        --build-arg BASE_IMAGE="$base" \
        --build-arg APT_MIRROR="$mirror" \
        -t "$candidate" "$context" > "$log" 2>&1; then
      break
    fi
    if [ "$attempt" -ge 2 ]; then
      tail -n 40 "$log" >&2 || true
      echo "[remote] Building $DESKTOPD_IMAGE failed; the existing image was left unchanged" >&2
      exit 1
    fi
    echo "[remote] Image build failed (attempt $attempt); retrying once for flaky mirrors"
    attempt=$((attempt + 1))
  done
  # The contract desktopd and desktop_supervisor.py depend on.
  if ! check_image_contract "$candidate"; then
    docker rmi "$candidate" >/dev/null 2>&1 || true
    echo "[remote] $candidate does not provide the desktop contract; $DESKTOPD_IMAGE left unchanged" >&2
    exit 1
  fi
  docker tag "$candidate" "$DESKTOPD_IMAGE"
  docker rmi "$candidate" >/dev/null 2>&1 || true
  echo "[remote] Built $DESKTOPD_IMAGE ($(docker image inspect --format '{{.Id}}' "$DESKTOPD_IMAGE"))"
}

# update_legacy_image keeps the old behaviour for DESKTOPD_IMAGE=maclaw-gui:1:
# overlay the supervisor onto the existing image.
update_legacy_image() {
  script="$DESKTOPD_DEPLOY_DIR/image/desktop_supervisor.py"
  dockerfile="$DESKTOPD_DEPLOY_DIR/image/Dockerfile"
  if [ -f "$dockerfile" ] && docker image inspect "$DESKTOPD_IMAGE" >/dev/null 2>&1; then
    docker build -f "$dockerfile" -t "$DESKTOPD_IMAGE" "$DESKTOPD_DEPLOY_DIR/image"
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
}

install_supervisor() {
  script="$DESKTOPD_DEPLOY_DIR/image/desktop_supervisor.py"
  if ! command -v docker >/dev/null 2>&1; then
    echo "[remote] docker is not installed; skipped supervisor image update."
    return 0
  fi
  case "$DESKTOPD_IMAGE" in
    maclaw-gui:1) update_legacy_image ;;
    *) provide_desktop_image ;;
  esac
  # Running desktops (v1 or v2) pick up the new supervisor on their next
  # start; it falls back to fluxbox where XFCE is not installed. desktopd
  # moves each user onto DESKTOPD_IMAGE the next time that user's desktop is
  # opened with it (see recreateReason in desktopd/service.go).
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
