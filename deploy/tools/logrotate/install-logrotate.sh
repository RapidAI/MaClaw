#!/bin/sh
# Install the MaClaw hub/hubcenter logrotate config on a deploy node.
# Usage: ssh root@<node> 'sh -s' < deploy/tools/logrotate/install-logrotate.sh
#    or: sh install-logrotate.sh   (run as root on the node)
set -eu

if [ "$(id -u)" != "0" ]; then
  echo "This script must run as root. Run sudo sh install-logrotate.sh." >&2
  exit 1
fi
if ! command -v logrotate >/dev/null 2>&1; then
  echo "ERROR: logrotate not installed (apt-get install logrotate)" >&2
  exit 1
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
CONF_SRC="$SCRIPT_DIR/maclaw-hub.conf"

cat > /etc/logrotate.d/maclaw-hub <"$CONF_SRC"
chmod 644 /etc/logrotate.d/maclaw-hub

if ! logrotate -d /etc/logrotate.d/maclaw-hub >/dev/null 2>&1; then
  logrotate -d /etc/logrotate.d/maclaw-hub >&2 || true
  echo "logrotate rejected /etc/logrotate.d/maclaw-hub. Fix the paths in deploy/tools/logrotate/maclaw-hub.conf and rerun this script. The dry-run output is above." >&2
  exit 1
fi

echo "Installed /etc/logrotate.d/maclaw-hub"
echo "Dry-run check:"
logrotate -d /etc/logrotate.d/maclaw-hub 2>&1 | grep -E "considering|log needs|does not need" | sed 's/^/  /'
echo
echo "Logrotate runs daily via /etc/cron.daily/logrotate (ensure cron is active)."
