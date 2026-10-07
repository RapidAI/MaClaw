#!/usr/bin/env bash
# Linux/macOS equivalent of the Windows deploy scripts for the maclaw-gui:2
# rollout on a single Docker host that runs desktopd, Hub, and MaClawSrv
# (production: hubs.mypapers.top = maclawsrv/dockerd.mypapers.top).
#
#   REMOTE_HOST=hubs.example.com deploy/linux/rollout_desktop_gui2.sh <step>...
#
# Steps (run in this order; each is safe to re-run):
#   build             cross-compile desktopd, maclaw-hub, meeting_asr_worker,
#                     maclawsrv (linux/amd64, CGO_ENABLED=0, same flags as the
#                     .cmd/.ps1 scripts) and pack xz archives + sha256
#   upload            rsync --partial (resumes after a dropped link) to REMOTE_TMP
#   backup            timestamped *.bak.<ts> copies + Hub desktop settings + docker lists
#   deploy-desktopd   desktopd/remote_deploy.sh: binary, image dir, maclaw-gui:2
#                     build on the host, supervisor into running desktops, restart
#   deploy-hub        Hub binary + meeting_asr_worker + web + start.sh; keeps config.yaml
#   deploy-maclawsrv  MaClawSrv binary, .env keys, start.sh, systemd unit, restart
#   verify            health checks, desktop containers, Hub desktop settings
#   smoke             desktopd/scripts/smoke_gui2.sh on the host (test user only)
#   switch-image      point the Hub desktop service at DESKTOP_IMAGE (needs CONFIRM=1)
#   rollback          restore binaries, web, and Hub settings from BACKUP_TS
#   all               build upload backup deploy-desktopd deploy-hub deploy-maclawsrv verify
#
# Environment:
#   REMOTE_HOST (required)  REMOTE_USER=root  REMOTE_PORT=22
#   SSHPASS        optional; when set, sshpass -e is used. Never put it in a file.
#   REMOTE_TMP=/tmp/maclaw_gui2_rollout  WORK_DIR=build/linux_rollout
#   GO=go  GOTOOLCHAIN (e.g. go1.26.5 to match production)  GOPROXY=https://goproxy.cn,direct
#   BUILD_NUMBER (default: ./build_number)  ONLY="desktopd hub maclawsrv" (subset to build/upload)
#   DESKTOPD_ADVERTISE_HOST (default REMOTE_HOST; .env keeps an existing value)
#   DESKTOPD_BASE_IMAGE / DESKTOPD_APT_MIRROR (empty = auto Tencent mirrors on Tencent Cloud)
#   DESKTOPD_SKIP_IMAGE_BUILD=0  HUB_PUBLIC_URL (e.g. https://hub.example.com, for verify)
#   DESKTOPD_IMAGE_FROM=build|pull|auto (default build; pull/auto use the public
#   ghcr.io/rapidai/maclaw-gui:2)  DESKTOPD_IMAGE_SOURCE  DESKTOPD_IMAGE_PULL_TIMEOUT=30m
#   DESKTOP_IMAGE=maclaw-gui:2 DESKTOP_MEMORY=3g DESKTOP_CPUS=1.5 DESKTOP_SHM=1g DESKTOP_SERVER_ID
#   TEST_USER=rollout-test-gui2 MIGRATE=1 STOP_AFTER=1 (smoke)   BACKUP_TS (rollback)
# Usage notes: docs/desktop-gui-v2.md
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
: "${REMOTE_HOST:?REMOTE_HOST is required}"
REMOTE_USER="${REMOTE_USER:-root}"
REMOTE_PORT="${REMOTE_PORT:-22}"
REMOTE_TMP="${REMOTE_TMP:-/tmp/maclaw_gui2_rollout}"
WORK_DIR="${WORK_DIR:-$ROOT/build/linux_rollout}"
GO="${GO:-go}"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
ONLY="${ONLY:-desktopd hub maclawsrv}"
CONTROL="${SSH_CONTROL_PATH:-/tmp/maclaw-rollout-%C}"
SSH_OPTS=(-p "$REMOTE_PORT" -o ControlMaster=auto -o "ControlPath=$CONTROL" -o ControlPersist=2h
          -o ConnectTimeout=20 -o ServerAliveInterval=20 -o ServerAliveCountMax=6)
TARGET="$REMOTE_USER@$REMOTE_HOST"

wants() { [[ " $ONLY " == *" $1 "* ]]; }

ssh_base() {
  if [[ -n "${SSHPASS:-}" ]]; then sshpass -e ssh "${SSH_OPTS[@]}" "$@"; else ssh "${SSH_OPTS[@]}" "$@"; fi
}

# The production sshd often resets connections during the banner (scanner
# load). Retry only connection failures (exit 255), then reuse one master.
remote() {
  local rc
  for _ in $(seq 1 30); do
    rc=0; ssh_base "$TARGET" "$@" || rc=$?
    [[ $rc -ne 255 ]] && return $rc
    sleep 5
  done
  return 255
}

remote_env() {
  printf 'REMOTE_TMP=%q DESKTOPD_ADVERTISE_HOST=%q DESKTOPD_IMAGE=%q DESKTOPD_BASE_IMAGE=%q DESKTOPD_APT_MIRROR=%q DESKTOPD_SKIP_IMAGE_BUILD=%q HUB_PUBLIC_URL=%q DESKTOP_IMAGE=%q DESKTOP_MEMORY=%q DESKTOP_CPUS=%q DESKTOP_SHM=%q DESKTOP_SERVER_ID=%q BACKUP_TS=%q' \
    "$REMOTE_TMP" "${DESKTOPD_ADVERTISE_HOST:-$REMOTE_HOST}" "${DESKTOPD_IMAGE:-maclaw-gui:2}" \
    "${DESKTOPD_BASE_IMAGE:-}" "${DESKTOPD_APT_MIRROR:-}" "${DESKTOPD_SKIP_IMAGE_BUILD:-0}" "${HUB_PUBLIC_URL:-}" \
    "${DESKTOP_IMAGE:-maclaw-gui:2}" "${DESKTOP_MEMORY:-3g}" "${DESKTOP_CPUS:-1.5}" "${DESKTOP_SHM:-1g}" \
    "${DESKTOP_SERVER_ID:-}" "${BACKUP_TS:-}"
  printf ' DESKTOPD_IMAGE_FROM=%q' "${DESKTOPD_IMAGE_FROM:-build}"
  # Only explicit values: remote_deploy.sh writes them to the host .env.
  if [[ -n "${DESKTOPD_IMAGE_SOURCE+x}" ]]; then printf ' DESKTOPD_IMAGE_SOURCE=%q' "$DESKTOPD_IMAGE_SOURCE"; fi
  if [[ -n "${DESKTOPD_IMAGE_PULL_TIMEOUT+x}" ]]; then printf ' DESKTOPD_IMAGE_PULL_TIMEOUT=%q' "$DESKTOPD_IMAGE_PULL_TIMEOUT"; fi
}

run_remote_step() {
  remote "$(remote_env) sh $REMOTE_TMP/rollout_remote.sh $1"
}

# The image build takes minutes; run it detached on the host so a dropped SSH
# link does not kill it, and poll its log.
run_remote_detached() {
  local log="$REMOTE_TMP/$1.log" out
  # Assignments before nohup reach the detached shell's environment.
  remote "$(remote_env) nohup sh -c 'sh \"\$0\" \"\$1\"; echo ROLLOUT_EXIT=\$?' $REMOTE_TMP/rollout_remote.sh $1 > $log 2>&1 < /dev/null & echo started $1, log $log"
  while :; do
    sleep 15
    out="$(remote "tail -n 5 $log" || true)"
    echo "$out" | tail -n 2
    if [[ "$out" == *ROLLOUT_EXIT=* ]]; then
      remote "cat $log" | grep -v ROLLOUT_EXIT || true
      if [[ "$out" == *ROLLOUT_EXIT=0* ]]; then return 0; fi
      return 1
    fi
  done
}

step_build() {
  local stage="$WORK_DIR/stage" out="$WORK_DIR/out"
  local build_number commit built
  build_number="${BUILD_NUMBER:-$(tr -dc 0-9 < "$ROOT/build_number" 2>/dev/null || echo dev)}"
  commit="$(git -C "$ROOT" rev-parse --short HEAD)"
  built="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  rm -rf "$WORK_DIR"; mkdir -p "$out"
  cd "$ROOT"
  if wants desktopd; then
    mkdir -p "$stage/desktopd/bin" "$stage/desktopd/image"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 "$GO" build -ldflags "-s -w" -o "$stage/desktopd/bin/desktopd" ./desktopd/cmd/desktopd
    cp desktopd/image/{desktop_supervisor.py,Dockerfile,Dockerfile.v2,close_range_shim.c,fcitx5-profile} "$stage/desktopd/image/"
    tar -czf "$out/desktopd-deploy.tar.gz" -C "$stage/desktopd" bin image
  fi
  if wants hub; then
    mkdir -p "$stage/hub/bin" "$stage/hub/hub/configs"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 "$GO" build -p 1 -o "$stage/hub/bin/maclaw-hub" ./hub/cmd/hub
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 "$GO" build -p 1 -o "$stage/hub/bin/meeting_asr_worker" ./hub/cmd/meeting_asr_worker
    cp hub/start.sh "$stage/hub/hub/start.sh"
    cp hub/configs/config.example.yaml "$stage/hub/hub/configs/"
    git archive HEAD hub/web | tar -x -C "$stage/hub"
    (cd "$stage/hub/hub/web" && find . -type f | LC_ALL=C sort | xargs -d '\n' sha256sum) > "$stage/hub/hub-web.sha256"
    tar -cf - -C "$stage/hub" . | xz -9 -T0 > "$out/hub.tar.xz"
  fi
  if wants maclawsrv; then
    mkdir -p "$stage/maclawsrv/bin"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 "$GO" build \
      -ldflags "-s -w -X main.serviceVersion=$build_number -X main.serviceCommit=$commit -X main.serviceBuiltAt=$built" \
      -o "$stage/maclawsrv/bin/maclawsrv" ./MaClawSrv
    tar -cf - -C "$stage/maclawsrv" bin | xz -9 -T0 > "$out/maclawsrv.tar.xz"
  fi
  (cd "$out" && for f in *.tar.*; do sha256sum "$f" > "$f.sha256"; done)
  ls -la "$out"
  echo "built from $(git rev-parse HEAD) with $("$GO" version)"
}

step_upload() {
  local out="$WORK_DIR/out" ssh_cmd
  remote "mkdir -p $REMOTE_TMP/scripts"
  ssh_cmd="ssh ${SSH_OPTS[*]}"
  [[ -n "${SSHPASS:-}" ]] && ssh_cmd="sshpass -e $ssh_cmd"
  local files=("$ROOT/deploy/linux/rollout_remote.sh" "$ROOT/desktopd/remote_deploy.sh")
  for f in "$out"/*.tar.* ; do files+=("$f"); done
  for _ in $(seq 1 20); do
    if rsync -av --partial --inplace --timeout=120 -e "$ssh_cmd" "${files[@]}" "$TARGET:$REMOTE_TMP/" &&
       rsync -av --timeout=120 -e "$ssh_cmd" "$ROOT/desktopd/scripts/" "$TARGET:$REMOTE_TMP/scripts/"; then
      remote "cd $REMOTE_TMP && sha256sum -c *.sha256"
      return 0
    fi
    echo "upload interrupted; resuming in 10s"; sleep 10
  done
  return 1
}

step_smoke() {
  remote "TEST_USER=${TEST_USER:-rollout-test-gui2} MIGRATE=${MIGRATE:-1} STOP_AFTER=${STOP_AFTER:-1} DESKTOPD_URL=${DESKTOPD_URL:-http://127.0.0.1:18081} sh $REMOTE_TMP/scripts/smoke_gui2.sh"
}

for step in "$@"; do
  case "$step" in
    build) step_build ;;
    upload) step_upload ;;
    deploy-desktopd) run_remote_detached deploy-desktopd ;;
    backup|deploy-hub|deploy-maclawsrv|verify|rollback) run_remote_step "$step" ;;
    smoke) step_smoke ;;
    switch-image)
      [[ "${CONFIRM:-}" == 1 ]] || { echo "switch-image changes which image every user moves to on their next desktop open; set CONFIRM=1" >&2; exit 2; }
      run_remote_step switch-image ;;
    all) "$0" build upload backup deploy-desktopd deploy-hub deploy-maclawsrv verify ;;
    *) sed -n '2,40p' "$0" >&2; exit 2 ;;
  esac
done
[[ $# -gt 0 ]] || { sed -n '2,40p' "$0" >&2; exit 2; }
