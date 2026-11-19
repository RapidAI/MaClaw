#!/bin/sh
# check_image_context.sh CONTEXT [DOCKERFILE...] — verify that every COPY/ADD
# source the Dockerfiles expect from the build context exists under CONTEXT.
# Prints the missing sources (one per line) and exits 1 when any are absent;
# exits 0 when the context is complete. With no DOCKERFILE arguments it checks
# every Dockerfile* file under CONTEXT.
#
# The rollout uploads this directory to $REMOTE_TMP/scripts/, so on a host it
# can be run by hand before docker build:
#   sh $REMOTE_TMP/scripts/check_image_context.sh /data/soft/maclaw_desktopd/image
# remote_deploy.sh inlines the same image_context_missing function — it is
# uploaded standalone by deploy_desktopd.cmd, so the two copies are kept
# byte-identical (checked by TestImageContextCheckMatchesDeployInlineCopy).
set -eu

image_context_missing() {
  ctx="$1"; shift
  for df; do
    [ -f "$df" ] || continue
    # Comment-only and blank lines are removed before joining backslash
    # continuations: docker drops them inside a continuation too.
    grep -vE '^[[:space:]]*(#|$)' "$df" | sed -e :a -e '/\\$/{N;s/\\\n/ /;ba}' | sed -nE 's/^[[:space:]]*([Cc][Oo][Pp][Yy]|[Aa][Dd][Dd])[[:space:]]+//p' | while read -r args || [ -n "$args" ]; do
      set -f; set -- $args; set +f
      from=0
      while [ $# -gt 0 ]; do
        case "$1" in
          --from=*) from=1; shift ;;
          --*) shift ;;
          *) break ;;
        esac
      done
      if [ "$from" = 1 ]; then continue; fi
      # The last word is the destination; every word before it is a context
      # source (URLs fetched by ADD are not).
      while [ $# -gt 1 ]; do
        case "$1" in
          http://*|https://*) ;;
          *) [ -e "$ctx/$1" ] || echo "$1" ;;
        esac
        shift
      done
    done
  done
}

case $# in 0) echo "usage: $0 CONTEXT [DOCKERFILE...]" >&2; exit 2 ;; esac
ctx="$1"; shift
if [ $# -eq 0 ]; then
  set -- "$ctx"/Dockerfile*
fi
# A context with no readable Dockerfile is almost certainly the wrong
# directory; reporting it "complete" would be a silent pass.
found=0
for df; do
  if [ -f "$df" ]; then found=1; break; fi
done
if [ "$found" = 0 ]; then
  echo "$0: none of the Dockerfile paths is readable (context: $ctx)" >&2
  exit 2
fi
missing="$(image_context_missing "$ctx" "$@" | sort -u)"
if [ -n "$missing" ]; then
  printf '%s\n' "$missing"
  exit 1
fi
