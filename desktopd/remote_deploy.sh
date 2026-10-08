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
# Outbound forward proxy SERVED BY THIS desktopd (overseas host). DESKTOPD_PROXY=1
# enables it on DESKTOPD_PROXY_ADDR (default :18082); the proxy key defaults to
# DESKTOPD_TOKEN when DESKTOPD_PROXY_TOKEN is empty.
: "${DESKTOPD_PROXY:=}"
: "${DESKTOPD_PROXY_ADDR:=}"
: "${DESKTOPD_PROXY_TOKEN:=}"
# TLS wrap for the proxy listener: a public certificate/key pair. Consumers
# then reach the proxy with an https:// proxy URL (their CONNECT and key
# cross the internet inside TLS).
: "${DESKTOPD_PROXY_TLS_CERT:=}"
: "${DESKTOPD_PROXY_TLS_KEY:=}"
# Consumer side (mainland host): egress through another desktopd's forward
# proxy. DESKTOPD_UPSTREAM_PROXY is the proxy URL, e.g.
#   http://docker:<key>@<overseas-host>:18082
# With it set, the image build receives HTTP(S)_PROXY/NO_PROXY build args and
# the desktopd process chains its own bridge listener to it.
# DESKTOPD_UPSTREAM_PROXY_APPLY=1 additionally writes the dockerd systemd
# drop-in and restarts docker: running desktops come back through their
# restart policy, but open sessions are cut, so deploy at a quiet time.
# DESKTOPD_DESKTOP_PROXY_URL is the proxy URL desktop containers receive
# (http://<bridge-gateway>:<port>, e.g. http://172.17.0.1:18083); the
# unauthenticated bridge listener binds that host:port on this machine.
: "${DESKTOPD_UPSTREAM_PROXY:=}"
: "${DESKTOPD_UPSTREAM_PROXY_APPLY:=0}"
: "${DESKTOPD_UPSTREAM_PROXY_NO_PROXY:=}"
: "${DESKTOPD_DESKTOP_PROXY_URL:=}"

rand_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 48 | tr -d '\n'
  else
    dd if=/dev/urandom bs=48 count=1 2>/dev/null | base64 | tr -d '\n'
  fi
}

# rand_hex is URL-safe by construction. The proxy key ends up embedded in
# proxy URLs on consumer hosts, and the base64 DESKTOPD_TOKEN fallback can
# carry "/" which cuts userinfo parsing there.
rand_hex() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
  else
    dd if=/dev/urandom bs=24 count=1 2>/dev/null | od -An -tx1 | tr -d ' \n'
  fi
}

if [ -z "$DESKTOPD_ADVERTISE_HOST" ]; then
  echo "[remote] DESKTOPD_ADVERTISE_HOST is required" >&2
  exit 1
fi

# Go's proxy-URL parser needs the scheme; a bare host:port upstream would be
# misparsed on the consumer side. Warn here, at deploy time, not in a pull.
# The value embeds the proxy key and is never printed as is.
if [ -n "$DESKTOPD_UPSTREAM_PROXY" ]; then
  case "$DESKTOPD_UPSTREAM_PROXY" in
    http://*|https://*) ;;
    *) echo "[remote] warning: DESKTOPD_UPSTREAM_PROXY should start with http:// or https://" >&2 ;;
  esac
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

# Persist the proxy-server feature flags into .env when the caller provided
# them, in both the fresh and the existing .env branch above.
if [ -n "$DESKTOPD_PROXY" ]; then
  grep -q '^DESKTOPD_PROXY=' "$DESKTOPD_DEPLOY_DIR/.env" || echo "DESKTOPD_PROXY=$DESKTOPD_PROXY" >> "$DESKTOPD_DEPLOY_DIR/.env"
fi
if [ -n "$DESKTOPD_PROXY_ADDR" ]; then
  grep -q '^DESKTOPD_PROXY_ADDR=' "$DESKTOPD_DEPLOY_DIR/.env" || echo "DESKTOPD_PROXY_ADDR=$DESKTOPD_PROXY_ADDR" >> "$DESKTOPD_DEPLOY_DIR/.env"
fi
if [ -n "$DESKTOPD_PROXY_TOKEN" ]; then
  grep -q '^DESKTOPD_PROXY_TOKEN=' "$DESKTOPD_DEPLOY_DIR/.env" || echo "DESKTOPD_PROXY_TOKEN=$DESKTOPD_PROXY_TOKEN" >> "$DESKTOPD_DEPLOY_DIR/.env"
fi
if [ -n "$DESKTOPD_PROXY_TLS_CERT" ]; then
  grep -q '^DESKTOPD_PROXY_TLS_CERT=' "$DESKTOPD_DEPLOY_DIR/.env" || echo "DESKTOPD_PROXY_TLS_CERT=$DESKTOPD_PROXY_TLS_CERT" >> "$DESKTOPD_DEPLOY_DIR/.env"
fi
if [ -n "$DESKTOPD_PROXY_TLS_KEY" ]; then
  grep -q '^DESKTOPD_PROXY_TLS_KEY=' "$DESKTOPD_DEPLOY_DIR/.env" || echo "DESKTOPD_PROXY_TLS_KEY=$DESKTOPD_PROXY_TLS_KEY" >> "$DESKTOPD_DEPLOY_DIR/.env"
fi
# Proxy enabled without an explicit key: generate the URL-safe one now, so
# consumers never inherit a base64 token that may contain "/".
if [ -n "$DESKTOPD_PROXY" ] && ! grep -q '^DESKTOPD_PROXY_TOKEN=' "$DESKTOPD_DEPLOY_DIR/.env"; then
  echo "DESKTOPD_PROXY_TOKEN=$(rand_hex)" >> "$DESKTOPD_DEPLOY_DIR/.env"
fi
# Consumer side: the desktopd process reads DESKTOPD_UPSTREAM_PROXY (its
# bridge listener chains to the overseas proxy) and DESKTOPD_DESKTOP_PROXY_URL
# (injected into desktop containers), so both live in .env.
if [ -n "$DESKTOPD_UPSTREAM_PROXY" ]; then
  grep -q '^DESKTOPD_UPSTREAM_PROXY=' "$DESKTOPD_DEPLOY_DIR/.env" || echo "DESKTOPD_UPSTREAM_PROXY=$DESKTOPD_UPSTREAM_PROXY" >> "$DESKTOPD_DEPLOY_DIR/.env"
fi
if [ -n "$DESKTOPD_DESKTOP_PROXY_URL" ]; then
  grep -q '^DESKTOPD_DESKTOP_PROXY_URL=' "$DESKTOPD_DEPLOY_DIR/.env" || echo "DESKTOPD_DESKTOP_PROXY_URL=$DESKTOPD_DESKTOP_PROXY_URL" >> "$DESKTOPD_DEPLOY_DIR/.env"
fi

# upstream_no_proxy keeps domestic/LAN traffic off the proxy: Tencent's
# metadata service and internal mirrors are only reachable without one. Both
# wildcard and dot forms are listed because dockerd's parser has shifted
# between releases.
upstream_no_proxy="${DESKTOPD_UPSTREAM_PROXY_NO_PROXY:-localhost,127.0.0.1,::1,*.tencentyun.com,.tencentyun.com,mirror.ccs.tencentyun.com,mirrors.tencentyun.com,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16}"

# configure_docker_proxy points this host's dockerd at the upstream forward
# proxy (pulls), and is called before the image build so the build args and
# the daemon agree on the egress path. The drop-in is idempotent: unchanged
# content does not restart docker, so routine deploys do not bounce desktops.
configure_docker_proxy() {
  [ -n "$DESKTOPD_UPSTREAM_PROXY" ] || return 0
  if [ "$DESKTOPD_UPSTREAM_PROXY_APPLY" != "1" ]; then
    echo "[remote] DESKTOPD_UPSTREAM_PROXY set: image build gets proxy build args (APPLY=1 would also configure dockerd; desktop containers need DESKTOPD_DESKTOP_PROXY_URL)"
    return 0
  fi
  if ! command -v systemctl >/dev/null 2>&1; then
    echo "[remote] no systemd; configure dockerd proxy manually (HTTP_PROXY/HTTPS_PROXY env)" >&2
    return 0
  fi
  mkdir -p /etc/systemd/system/docker.service.d
  dropin=/etc/systemd/system/docker.service.d/maclaw-proxy.conf
  candidate="$(mktemp)"
  cat > "$candidate" << PROXYEOF
[Service]
Environment="HTTP_PROXY=$DESKTOPD_UPSTREAM_PROXY"
Environment="HTTPS_PROXY=$DESKTOPD_UPSTREAM_PROXY"
Environment="NO_PROXY=$upstream_no_proxy"
PROXYEOF
  if [ -f "$dropin" ] && cmp -s "$candidate" "$dropin"; then
    rm -f "$candidate"
    echo "[remote] dockerd proxy drop-in unchanged; docker not restarted"
    return 0
  fi
  mv "$candidate" "$dropin"
  echo "[remote] Wrote $dropin; restarting docker (desktops restart via their restart policy)"
  systemctl daemon-reload
  systemctl restart docker
}

on_tencent_cloud() {
  command -v curl >/dev/null 2>&1 &&
    curl -fsS -m 2 http://metadata.tencentyun.com/latest/meta-data/instance-id >/dev/null 2>&1
}

# build_desktop_image builds DESKTOPD_IMAGE from Dockerfile.v2. It builds
# under a temporary tag and checks the result first, so a failed or broken
# build never replaces the image running desktops use. Only Dockerfile.v2 is
# ever tagged as a v2 image; the legacy overlay Dockerfile is maclaw-gui:1 only.
build_desktop_image() {
  context="$DESKTOPD_DEPLOY_DIR/image"
  dockerfile="$context/Dockerfile.v2"
  if [ "$DESKTOPD_SKIP_IMAGE_BUILD" = "1" ]; then
    if ! docker image inspect "$DESKTOPD_IMAGE" >/dev/null 2>&1; then
      echo "[remote] DESKTOPD_SKIP_IMAGE_BUILD=1 but $DESKTOPD_IMAGE does not exist" >&2
      exit 1
    fi
    echo "[remote] Skipped building $DESKTOPD_IMAGE"
    return 0
  fi
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
  proxyargs=""
  if [ -n "$DESKTOPD_UPSTREAM_PROXY" ]; then
    proxyargs="--build-arg HTTP_PROXY=$DESKTOPD_UPSTREAM_PROXY --build-arg HTTPS_PROXY=$DESKTOPD_UPSTREAM_PROXY --build-arg NO_PROXY=$upstream_no_proxy"
  fi
  echo "[remote] Building $DESKTOPD_IMAGE from Dockerfile.v2 (base $base, apt mirror ${mirror:-deb.debian.org}${proxyargs:+, proxied}); log: $log"
  attempt=1
  while :; do
    if docker build -f "$dockerfile" \
        --build-arg BASE_IMAGE="$base" \
        --build-arg APT_MIRROR="$mirror" \
        $proxyargs \
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
  if ! docker run --rm --entrypoint sh "$candidate" -c '
      test -f /desktop_supervisor.py && test -f /usr/share/novnc/vnc.html &&
      for tool in python3 Xvfb x11vnc websockify xdotool chromium startxfce4 dbus-launch import; do
        command -v "$tool" >/dev/null || { echo "missing $tool"; exit 1; }
      done' >&2; then
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
    *) build_desktop_image ;;
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
configure_docker_proxy
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
