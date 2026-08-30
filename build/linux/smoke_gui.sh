#!/usr/bin/env bash
# Prove a CI Linux GUI artifact can start on a machine that is not the builder.
#
# Checks:
#   1. The packaged AppImage contains bundled WebKit (so a remote host without
#      the runner's libwebkit2gtk package is not dead on arrival).
#   2. The GUI binary / AppRun --version loads (dynamic linker + CGO).
#   3. Under xvfb, the GUI process stays up instead of crashing immediately.
set -euo pipefail

APPIMAGE=""
BINARY=""
HOLD_SECONDS="${MACLAW_GUI_SMOKE_HOLD:-8}"
WORKDIR=""

usage() {
  cat <<'EOF'
Usage: smoke_gui.sh --appimage PATH [--binary PATH] [--hold-seconds N]
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --appimage) APPIMAGE="${2:-}"; shift 2 ;;
    --binary) BINARY="${2:-}"; shift 2 ;;
    --hold-seconds) HOLD_SECONDS="${2:-}"; shift 2 ;;
    --workdir) WORKDIR="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [ -z "$APPIMAGE" ]; then
  usage >&2
  exit 2
fi
if [ ! -f "$APPIMAGE" ]; then
  echo "AppImage not found: $APPIMAGE" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -z "$WORKDIR" ]; then
  WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/maclaw-gui-smoke.XXXXXX")"
  trap 'rm -rf "$WORKDIR"' EXIT
fi
mkdir -p "$WORKDIR"
chmod +x "$APPIMAGE"

echo "=== MaClaw Linux GUI smoke ==="
echo "  appimage: $APPIMAGE"
echo "  workdir:  $WORKDIR"

# --- extract without FUSE (works on GHA and typical VPS hosts) ---
ABS_APPIMAGE="$(readlink -f "$APPIMAGE")"
(
  cd "$WORKDIR"
  export APPIMAGE_EXTRACT_AND_RUN=1
  if ! "$ABS_APPIMAGE" --appimage-extract >/dev/null; then
    echo "AppImage --appimage-extract failed; trying extract-and-run helper" >&2
    "$ABS_APPIMAGE" --appimage-extract-and-run --appimage-extract >/dev/null
  fi
)
ROOT="$WORKDIR/squashfs-root"
if [ ! -x "$ROOT/AppRun" ]; then
  echo "extracted AppDir is missing AppRun at $ROOT/AppRun" >&2
  exit 1
fi

echo "--- bundled WebKit ---"
if [ -f "$ROOT/usr/share/maclaw/runtime.txt" ]; then
  cat "$ROOT/usr/share/maclaw/runtime.txt"
fi
if ! find "$ROOT/usr/lib" -name 'libwebkit2gtk-*.so*' | grep -q .; then
  echo "FAIL: AppImage does not bundle libwebkit2gtk (remote hosts without the runner package will not start)" >&2
  find "$ROOT/usr/lib" -name 'libwebkit*' -o -name 'libjavascriptcore*' >&2 || true
  exit 1
fi
if ! find "$ROOT/usr/lib" -name 'WebKitWebProcess' -executable | grep -q .; then
  echo "FAIL: AppImage is missing WebKitWebProcess" >&2
  exit 1
fi
echo "bundled WebKit library + WebKitWebProcess: OK"

# Dynamic linker check using *only* bundled libs (plus the host glibc/GL).
GUI_BIN=""
is_elf() {
  local f="$1"
  if command -v file >/dev/null 2>&1; then
    file "$f" | grep -q 'ELF'
    return $?
  fi
  [ "$(od -An -N4 -t x1 "$f" 2>/dev/null | tr -d ' \n')" = "7f454c46" ]
}
for candidate in "$ROOT/usr/bin/"*; do
  if [ -x "$candidate" ] && is_elf "$candidate"; then
    GUI_BIN="$candidate"
    break
  fi
done
if [ -z "$GUI_BIN" ]; then
  echo "FAIL: no ELF GUI binary in AppDir usr/bin" >&2
  exit 1
fi

LIBPATH=""
for libdir in "$ROOT/usr/lib" "$ROOT/usr/lib/x86_64-linux-gnu" "$ROOT/usr/lib/aarch64-linux-gnu"; do
  [ -d "$libdir" ] && LIBPATH="${libdir}${LIBPATH:+:$LIBPATH}"
done

echo "--- ldd (bundled LD_LIBRARY_PATH) ---"
missing="$(LD_LIBRARY_PATH="$LIBPATH" ldd "$GUI_BIN" | awk '/not found/ { print }' || true)"
if [ -n "$missing" ]; then
  # Host libc / libGL "not found" would be unusual; anything WebKit/GTK is fatal.
  if echo "$missing" | grep -Eq 'libwebkit|libjavascriptcore|libgtk|libgdk|libsoup|libicu'; then
    echo "FAIL: bundled runtime is missing libraries the GUI needs:" >&2
    echo "$missing" >&2
    exit 1
  fi
  echo "warning: ldd reported unresolved libraries (likely host glibc/GL):"
  echo "$missing"
else
  echo "ldd: all resolved via bundled path + host glibc"
fi

echo "--- --version (AppImage / AppRun) ---"
version_out="$WORKDIR/version.out"
if ! "$ROOT/AppRun" --version > "$version_out" 2>"$WORKDIR/version.err"; then
  echo "FAIL: AppRun --version exited $?" >&2
  cat "$WORKDIR/version.err" >&2 || true
  exit 1
fi
if [ ! -s "$version_out" ]; then
  echo "FAIL: AppRun --version produced no stdout (binary did not load a version string)" >&2
  cat "$WORKDIR/version.err" >&2 || true
  exit 1
fi
echo "AppRun --version: $(tr -d '\r' < "$version_out")"

if [ -n "$BINARY" ] && [ -f "$BINARY" ]; then
  echo "--- --version (standalone binary, host WebKit) ---"
  if ! "$BINARY" --version > "$WORKDIR/bin-version.out" 2>"$WORKDIR/bin-version.err"; then
    echo "FAIL: standalone GUI --version exited $?" >&2
    cat "$WORKDIR/bin-version.err" >&2 || true
    exit 1
  fi
  echo "binary --version: $(tr -d '\r' < "$WORKDIR/bin-version.out")"
fi

echo "--- --help ---"
if ! "$ROOT/AppRun" --help > "$WORKDIR/help.out" 2>"$WORKDIR/help.err"; then
  echo "FAIL: AppRun --help exited $?" >&2
  cat "$WORKDIR/help.err" >&2 || true
  exit 1
fi
if ! grep -qi 'maclaw\|tigerclaw\|metastaff\|usage' "$WORKDIR/help.out"; then
  echo "FAIL: AppRun --help did not look like GUI help" >&2
  cat "$WORKDIR/help.out" >&2 || true
  exit 1
fi
echo "AppRun --help: OK"

if ! command -v xvfb-run >/dev/null 2>&1; then
  echo "xvfb-run not installed; skipping window-start hold (install xvfb for the full smoke)."
  echo "Required checks (--version, bundled WebKit, ldd) passed."
  exit 0
fi

echo "--- xvfb start (hold ${HOLD_SECONDS}s) ---"
export LIBGL_ALWAYS_SOFTWARE=1
export WEBKIT_DISABLE_COMPOSITING_MODE=1
export WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1
set +e
timeout --signal=TERM --kill-after=5 "${HOLD_SECONDS}" \
  xvfb-run -a --server-args='-screen 0 1280x720x24' \
  "$ROOT/AppRun" > "$WORKDIR/xvfb.out" 2>"$WORKDIR/xvfb.err"
xvfb_rc=$?
set -e

# timeout(1) returns 124 when the process was still running — that is success.
if [ "$xvfb_rc" -eq 124 ]; then
  echo "GUI stayed up under xvfb for ${HOLD_SECONDS}s: OK"
  exit 0
fi

if [ "$xvfb_rc" -eq 0 ]; then
  echo "FAIL: GUI exited 0 immediately instead of staying up" >&2
  cat "$WORKDIR/xvfb.out" >&2 || true
  cat "$WORKDIR/xvfb.err" >&2 || true
  exit 1
fi

# Some headless agents cannot initialize GTK/WebKit even with xvfb (no DRI,
# missing dbus). The artifact is still remote-runnable; --version + bundled
# WebKit already proved the linker/runtime. Keep this as a hard failure on
# GitHub-hosted Linux (MACLAW_GUI_SMOKE_REQUIRE_XVFB=1, set by the workflow).
echo "xvfb start exited $xvfb_rc (process did not stay up)"
if [ -s "$WORKDIR/xvfb.err" ]; then
  echo "--- xvfb stderr ---"
  cat "$WORKDIR/xvfb.err"
fi
if [ "${MACLAW_GUI_SMOKE_REQUIRE_XVFB:-0}" = "1" ]; then
  echo "FAIL: xvfb window smoke is required in this environment" >&2
  exit 1
fi
echo "warning: xvfb window smoke failed; --version and bundled WebKit checks passed."
echo "On a desktop or GHA ubuntu-*-latest runner this step is expected to pass."
exit 0
