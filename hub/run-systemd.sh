#!/bin/sh
# systemd ExecStart for maclaw-hub. Do not point the unit at start.sh:
# start.sh restarts the unit when it is installed, which would loop.
set -eu

APP_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
cd "$APP_DIR"

# Desktop API token shared with MaClawSrv. Mode 600. A binary deploy does not rewrite this file.
if [ -f "$APP_DIR/desktop-api.env" ]; then
  set -a
  . "$APP_DIR/desktop-api.env"
  set +a
fi

# The mobile meeting-recording API discovers its ASR capability from this
# command. Keep the worker and its model beside the Hub. Explicit
# service-manager values still take precedence.
MEETING_ASR_WORKER="$APP_DIR/meeting_asr_worker"
MEETING_ASR_MODEL_DEFAULT="$APP_DIR/data/models/sensevoice-small-q8.gguf"
if [ -z "${MACLAW_MEETING_TRANSCRIBE_COMMAND:-}" ] && [ -x "$MEETING_ASR_WORKER" ]; then
  export MACLAW_MEETING_TRANSCRIBE_COMMAND="$MEETING_ASR_WORKER"
fi
if [ -z "${MACLAW_MEETING_ASR_MODEL:-}" ] && [ -f "$MEETING_ASR_MODEL_DEFAULT" ]; then
  export MACLAW_MEETING_ASR_MODEL="$MEETING_ASR_MODEL_DEFAULT"
fi

mkdir -p "$APP_DIR/data/logs"
# systemd 237 (Ubuntu 18.04) cannot parse StandardOutput=append:. Redirect
# here, then replace this shell so the hub process keeps the log fds.
exec >>"$APP_DIR/data/logs/maclaw-hub.out.log" 2>&1
exec ./maclaw-hub --config configs/config.yaml
