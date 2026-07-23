#!/bin/sh
set -eu

APP_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
BIN_NAME="maclaw-hub"
CONFIG_PATH="$APP_DIR/configs/config.yaml"
EXAMPLE_CONFIG_PATH="$APP_DIR/configs/config.example.yaml"
PID_FILE="$APP_DIR/data/maclaw-hub.pid"
LOG_DIR="$APP_DIR/data/logs"
LOG_FILE="$LOG_DIR/maclaw-hub.out.log"
MEETING_ASR_WORKER="$APP_DIR/meeting_asr_worker"
MEETING_ASR_MODEL_DEFAULT="$APP_DIR/data/models/sensevoice-small-q8.gguf"

mkdir -p "$APP_DIR/data" "$LOG_DIR"

cd "$APP_DIR"

# The mobile meeting-recording API discovers its ASR capability from this
# command.  Keep the worker and its model beside the Hub so deploy_all.cmd can
# update them atomically with the Hub binary.  Explicit service-manager values
# still take precedence for installations using a managed external worker.
if [ -z "${MACLAW_MEETING_TRANSCRIBE_COMMAND:-}" ] && [ -x "$MEETING_ASR_WORKER" ]; then
  export MACLAW_MEETING_TRANSCRIBE_COMMAND="$MEETING_ASR_WORKER"
fi
if [ -z "${MACLAW_MEETING_ASR_MODEL:-}" ] && [ -f "$MEETING_ASR_MODEL_DEFAULT" ]; then
  export MACLAW_MEETING_ASR_MODEL="$MEETING_ASR_MODEL_DEFAULT"
fi

if [ ! -f "$CONFIG_PATH" ] && [ -f "$EXAMPLE_CONFIG_PATH" ]; then
  cp -f "$EXAMPLE_CONFIG_PATH" "$CONFIG_PATH"
fi

if [ -f "$PID_FILE" ]; then
  OLD_PID=$(cat "$PID_FILE" 2>/dev/null || true)
  if [ -n "${OLD_PID:-}" ] && kill -0 "$OLD_PID" 2>/dev/null; then
    echo "Stopping existing $BIN_NAME process: $OLD_PID"
    kill "$OLD_PID" 2>/dev/null || true
    sleep 2
    if kill -0 "$OLD_PID" 2>/dev/null; then
      kill -9 "$OLD_PID" 2>/dev/null || true
    fi
  fi
  rm -f "$PID_FILE"
fi

# Also kill any legacy "codeclaw-hub" process that may hold the port.
ps -eo pid=,args= | awk -v cmd="$APP_DIR/codeclaw-hub" '$2 == cmd { print $1 }' | while read -r pid; do
  if [ -n "${pid:-}" ]; then
    echo "Stopping legacy codeclaw-hub process: $pid"
    kill "$pid" 2>/dev/null || true
    sleep 1
    if kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" 2>/dev/null || true
    fi
  fi
done

ps -eo pid=,args= | awk -v cmd="$APP_DIR/$BIN_NAME" '$2 == cmd { print $1 }' | while read -r pid; do
  if [ -n "${pid:-}" ]; then
    echo "Stopping stale $BIN_NAME process: $pid"
    kill "$pid" 2>/dev/null || true
    sleep 1
    if kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" 2>/dev/null || true
    fi
  fi
done

# Older deploys started the binary as "./maclaw-hub" from APP_DIR. Those
# processes survive path-based matching after the binary is replaced.
ps -eo pid=,args= | awk -v bin="$BIN_NAME" '$2 == "./" bin || $2 == bin { print $1 }' | while read -r pid; do
  if [ -n "${pid:-}" ] && [ "$(readlink "/proc/$pid/cwd" 2>/dev/null || true)" = "$APP_DIR" ]; then
    echo "Stopping relative $BIN_NAME process: $pid"
    kill "$pid" 2>/dev/null || true
    sleep 1
    if kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" 2>/dev/null || true
    fi
  fi
done

echo "Starting $BIN_NAME..."
nohup "$APP_DIR/$BIN_NAME" --config "$CONFIG_PATH" >>"$LOG_FILE" 2>&1 &
NEW_PID=$!
echo "$NEW_PID" > "$PID_FILE"
echo "$BIN_NAME started with PID $NEW_PID"
