#!/bin/sh
# Call the desktopd HTTP API on the Docker host without printing the token.
#
#   dapi.sh METHOD PATH [JSON_BODY]
#   dapi.sh GET  /v1/health
#   dapi.sh POST /v1/desktops/session '{"tenant_id":"tenant_default","user_id":"rollout-test-gui2","image":"maclaw-gui:2","memory":"3g","cpus":"1.5","shm_size":"1g"}'
#   dapi.sh GET  '/v1/desktops/screenshot?tenant_id=tenant_default&user_id=rollout-test-gui2' > shot.png
#
# Environment:
#   DESKTOPD_ENV_FILE  .env holding DESKTOPD_TOKEN (default /data/soft/maclaw_desktopd/.env)
#   DESKTOPD_URL       API base (default http://127.0.0.1:18081; use the
#                      public base_url, e.g. https://dockerd.example.com, to
#                      test the same route Hub uses)
#   DESKTOPD_TOKEN     used as is when already set (the .env is then not read)
#   DAPI_TIMEOUT       curl --max-time seconds (default 600; opening a desktop
#                      may recreate the container)
# Session responses carry a per-desktop gate token in cdp_url/novnc_url. Redact
# it before logging or pasting: sed -E 's/desktop:[0-9a-f]{16,}@/desktop:<redacted>@/g'
set -eu

if [ "$#" -lt 2 ]; then
  sed -n '2,20p' "$0" >&2
  exit 2
fi
if [ -z "${DESKTOPD_TOKEN:-}" ]; then
  env_file="${DESKTOPD_ENV_FILE:-/data/soft/maclaw_desktopd/.env}"
  DESKTOPD_TOKEN="$(sed -n 's/^DESKTOPD_TOKEN=//p' "$env_file" | tail -n 1)"
fi
if [ -z "$DESKTOPD_TOKEN" ]; then
  echo "DESKTOPD_TOKEN is empty" >&2
  exit 1
fi
url="${DESKTOPD_URL:-http://127.0.0.1:18081}$2"
timeout="${DAPI_TIMEOUT:-600}"
# The header goes through a here-doc on stdin so the token is not in argv.
if [ "$#" -ge 3 ]; then
  printf 'Authorization: Bearer %s\n' "$DESKTOPD_TOKEN" | curl -sS -m "$timeout" -X "$1" \
    -H @- -H 'Content-Type: application/json' --data "$3" "$url"
else
  printf 'Authorization: Bearer %s\n' "$DESKTOPD_TOKEN" | curl -sS -m "$timeout" -X "$1" -H @- "$url"
fi
