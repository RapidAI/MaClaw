#!/bin/sh
set -eu

APP_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
BIN_NAME="codeclaw-hub"
CONFIG_PATH="$APP_DIR/configs/config.yaml"
EXAMPLE_CONFIG_PATH="$APP_DIR/configs/config.example.yaml"
PID_FILE="$APP_DIR/data/codeclaw-hub.pid"
LOG_DIR="$APP_DIR/data/logs"
LOG_FILE="$LOG_DIR/codeclaw-hub.out.log"

mkdir -p "$APP_DIR/data" "$LOG_DIR"

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

echo "Starting $BIN_NAME..."
nohup "$APP_DIR/$BIN_NAME" --config "$CONFIG_PATH" >>"$LOG_FILE" 2>&1 &
NEW_PID=$!
echo "$NEW_PID" > "$PID_FILE"
echo "$BIN_NAME started with PID $NEW_PID"
