#!/usr/bin/env bash
# Fails when a site domain is hardcoded in Go code. Site-specific data lives
# in corelib/browser/siteprofiles/*.json; tests, testdata and comments may
# name sites. desktopd (network proxy routing) is out of scope.
set -euo pipefail
cd "$(dirname "$0")/.."
re='(zhihu|xiaohongshu|xhscdn|csdn|juejin|weibo|bilibili|medium)\.(com|cn|net|im)'
hits=$(find corelib MaClawSrv -name '*.go' ! -name '*_test.go' ! -path '*/testdata/*' -print0 |
  xargs -0 grep -nEi "$re" | grep -vE '^[^:]+:[0-9]+:[[:space:]]*//' || true)
if [ -n "$hits" ]; then
  echo "site domains hardcoded in code (move them to a site profile):"
  echo "$hits"
  exit 1
fi
echo "ok: no site domains in code"
