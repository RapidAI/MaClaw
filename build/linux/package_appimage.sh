#!/usr/bin/env bash
# Package a Wails/CGO Linux GUI into an AppImage that can run on a remote host.
#
# The GitHub runner's WebKit is dynamically linked. A bare AppDir that only
# ships the binary will fail on a typical Ubuntu 24.04 VPS that does not have
# libwebkit2gtk installed, and the old AppRun *exited* on that check.
# This script copies WebKitGTK/GTK (and WebKit helper processes) into the
# AppImage and points AppRun at them.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: package_appimage.sh --binary PATH --app-name NAME [options]

Required:
  --binary PATH           CGO GUI binary (e.g. build/bin/MaClaw_amd64_linux)
  --app-name NAME         File-name brand (MaClaw | TigerClaw | MetaStaff)

Optional:
  --display-name TEXT     Desktop Name= (defaults to --app-name)
  --icon PATH             PNG icon
  --arch ARCH             x86_64 | aarch64 (auto)
  --ubuntu-ver VER        22.04 | 24.04
  --ubuntu-label LABEL    u2204 | u2404
  --webkit-api VER        4.0 | 4.1 (auto from linked binary)
  --webkit-soname NAME    libwebkit2gtk-4.1.so.0
  --webkit-pkg DEB        libwebkit2gtk-4.1-0
  --output PATH           AppImage output path
  --appdir PATH           AppDir working directory
EOF
}

BINARY=""
APP_NAME=""
DISPLAY_NAME=""
ICON=""
ARCH=""
UBUNTU_VER=""
UBUNTU_LABEL=""
WEBKIT_API=""
WEBKIT_SONAME=""
WEBKIT_PKG=""
OUTPUT=""
APP_DIR=""

while [ $# -gt 0 ]; do
  case "$1" in
    --binary) BINARY="${2:-}"; shift 2 ;;
    --app-name) APP_NAME="${2:-}"; shift 2 ;;
    --display-name) DISPLAY_NAME="${2:-}"; shift 2 ;;
    --icon) ICON="${2:-}"; shift 2 ;;
    --arch) ARCH="${2:-}"; shift 2 ;;
    --ubuntu-ver) UBUNTU_VER="${2:-}"; shift 2 ;;
    --ubuntu-label) UBUNTU_LABEL="${2:-}"; shift 2 ;;
    --webkit-api) WEBKIT_API="${2:-}"; shift 2 ;;
    --webkit-soname) WEBKIT_SONAME="${2:-}"; shift 2 ;;
    --webkit-pkg) WEBKIT_PKG="${2:-}"; shift 2 ;;
    --output) OUTPUT="${2:-}"; shift 2 ;;
    --appdir) APP_DIR="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [ -z "$BINARY" ] || [ -z "$APP_NAME" ]; then
  usage >&2
  exit 2
fi
if [ ! -f "$BINARY" ]; then
  echo "binary not found: $BINARY" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DISPLAY_NAME="${DISPLAY_NAME:-$APP_NAME}"
ICON="${ICON:-$REPO_ROOT/build/appicon.png}"
OUT_LOWER="$(echo "$APP_NAME" | tr '[:upper:]' '[:lower:]')"

if [ -z "$ARCH" ]; then
  case "$(uname -m)" in
    x86_64|amd64) ARCH=x86_64 ;;
    aarch64|arm64) ARCH=aarch64 ;;
    *) echo "unsupported uname -m: $(uname -m)" >&2; exit 1 ;;
  esac
fi
if [ "$ARCH" = "amd64" ]; then ARCH=x86_64; fi
if [ "$ARCH" = "arm64" ]; then ARCH=aarch64; fi

MULTIARCH="x86_64-linux-gnu"
if [ "$ARCH" = "aarch64" ]; then
  MULTIARCH="aarch64-linux-gnu"
fi

if [ -z "$WEBKIT_API" ]; then
  if ldd "$BINARY" 2>/dev/null | grep -q 'libwebkit2gtk-4.1'; then
    WEBKIT_API=4.1
  else
    WEBKIT_API=4.0
  fi
fi
if [ -z "$WEBKIT_SONAME" ]; then
  if [ "$WEBKIT_API" = "4.1" ]; then
    WEBKIT_SONAME="libwebkit2gtk-4.1.so.0"
  else
    WEBKIT_SONAME="libwebkit2gtk-4.0.so.37"
  fi
fi
if [ -z "$WEBKIT_PKG" ]; then
  if [ "$WEBKIT_API" = "4.1" ]; then
    WEBKIT_PKG="libwebkit2gtk-4.1-0"
  else
    WEBKIT_PKG="libwebkit2gtk-4.0-37"
  fi
fi
if [ -z "$UBUNTU_VER" ]; then
  if [ "$WEBKIT_API" = "4.1" ]; then
    UBUNTU_VER=24.04
  else
    UBUNTU_VER=22.04
  fi
fi
if [ -z "$UBUNTU_LABEL" ]; then
  UBUNTU_LABEL="u${UBUNTU_VER/./}"
fi
if [ -z "$APP_DIR" ]; then
  APP_DIR="$REPO_ROOT/build/linux/AppDir_${ARCH}"
fi
if [ -z "$OUTPUT" ]; then
  mkdir -p "$REPO_ROOT/dist"
  OUTPUT="$REPO_ROOT/dist/${APP_NAME}-${ARCH}-${UBUNTU_LABEL}.AppImage"
fi

echo "Packaging $APP_NAME AppImage"
echo "  binary:        $BINARY"
echo "  arch:          $ARCH ($MULTIARCH)"
echo "  webkit:        $WEBKIT_API ($WEBKIT_SONAME)"
echo "  ubuntu label:  $UBUNTU_LABEL"
echo "  appdir:        $APP_DIR"
echo "  output:        $OUTPUT"

rm -rf "$APP_DIR"
mkdir -p \
  "$APP_DIR/usr/bin" \
  "$APP_DIR/usr/lib/$MULTIARCH" \
  "$APP_DIR/usr/share/applications" \
  "$APP_DIR/usr/share/icons/hicolor/512x512/apps" \
  "$APP_DIR/usr/share/maclaw" \
  "$APP_DIR/usr/share/glib-2.0/schemas"

cp "$BINARY" "$APP_DIR/usr/bin/$OUT_LOWER"
chmod +x "$APP_DIR/usr/bin/$OUT_LOWER"

if [ -f "$ICON" ]; then
  cp "$ICON" "$APP_DIR/${OUT_LOWER}.png"
  cp "$ICON" "$APP_DIR/.DirIcon"
  cp "$ICON" "$APP_DIR/usr/share/icons/hicolor/512x512/apps/${OUT_LOWER}.png"
else
  echo "warning: icon not found at $ICON" >&2
fi

cat > "$APP_DIR/${OUT_LOWER}.desktop" <<EOF
[Desktop Entry]
Name=${DISPLAY_NAME}
Exec=${OUT_LOWER}
Icon=${OUT_LOWER}
Type=Application
Categories=Development;
MimeType=x-scheme-handler/maclaw;
Terminal=false
X-AppImage-Arch=${ARCH}
X-AppImage-Ubuntu=${UBUNTU_LABEL}
X-AppImage-WebKit=${WEBKIT_API}
EOF
cp "$APP_DIR/${OUT_LOWER}.desktop" "$APP_DIR/usr/share/applications/${OUT_LOWER}.desktop"

should_skip_lib() {
  local base
  base="$(basename "$1")"
  case "$base" in
    linux-vdso.so*|ld-linux*.so*|ld-linux-*) return 0 ;;
    libc.so.*|libm.so.*|libpthread.so.*|libdl.so.*|librt.so.*|libutil.so.*) return 0 ;;
    libc_malloc_debug.so.*|libanl.so.*|libnsl.so.*|libresolv.so.*|libcidn.so.*) return 0 ;;
    libcrypt.so.*) return 0 ;;
    libGL.so.*|libOpenGL.so.*|libGLdispatch.so.*|libGLX.so.*|libEGL.so.*) return 0 ;;
    libGLX_*.so.*|libEGL_*.so.*|libGLESv2.so.*) return 0 ;;
    libdrm.so.*|libdrm_*.so.*|libgbm.so.*) return 0 ;;
    libnvidia*|libcuda.so*|libwayland-egl.so.*) return 0 ;;
  esac
  return 1
}

copy_lib() {
  local src="$1"
  local dest_dir="$2"
  [ -e "$src" ] || return 0
  mkdir -p "$dest_dir"
  local base real realbase
  base="$(basename "$src")"
  if [ -L "$src" ]; then
    real="$(readlink -f "$src" || true)"
    if [ -n "$real" ] && [ -f "$real" ]; then
      realbase="$(basename "$real")"
      if [ ! -e "$dest_dir/$realbase" ]; then
        cp -L "$real" "$dest_dir/$realbase"
      fi
      if [ ! -e "$dest_dir/$base" ]; then
        ln -s "$realbase" "$dest_dir/$base"
      fi
      return 0
    fi
  fi
  if [ ! -e "$dest_dir/$base" ]; then
    cp -L "$src" "$dest_dir/$base"
  fi
}

collect_needed() {
  local file="$1"
  ldd "$file" 2>/dev/null | awk '
    $1 ~ /^\// { print $1 }
    $2 == "=>" && $3 ~ /^\// { print $3 }
  '
}

declare -A SEEN_FILES=()
QUEUE=("$APP_DIR/usr/bin/$OUT_LOWER")

# WebKit helper processes + injected bundle (dlopened, not in the GUI NEEDED).
WEBKIT_HELPER_SRC=""
dest_helper=""
for candidate in \
  "/usr/lib/${MULTIARCH}/webkit2gtk-${WEBKIT_API}" \
  "/usr/lib/webkit2gtk-${WEBKIT_API}"
do
  if [ -d "$candidate" ]; then
    WEBKIT_HELPER_SRC="$candidate"
    break
  fi
done
if [ -z "$WEBKIT_HELPER_SRC" ]; then
  WEBKIT_HELPER_SRC="$(find /usr/lib -type d -name "webkit2gtk-${WEBKIT_API}" 2>/dev/null | head -n 1 || true)"
fi
if [ -n "$WEBKIT_HELPER_SRC" ]; then
  dest_helper="$APP_DIR/usr/lib/${MULTIARCH}/webkit2gtk-${WEBKIT_API}"
  mkdir -p "$dest_helper"
  cp -a "$WEBKIT_HELPER_SRC/." "$dest_helper/"
  echo "  bundled WebKit helpers from $WEBKIT_HELPER_SRC"
  while IFS= read -r -d '' helper; do
    QUEUE+=("$helper")
  done < <(find "$dest_helper" -type f -executable -print0 2>/dev/null)
fi

idx=0
while [ "$idx" -lt "${#QUEUE[@]}" ]; do
  current="${QUEUE[$idx]}"
  idx=$((idx + 1))
  [ -f "$current" ] || continue
  [ -z "${SEEN_FILES[$current]:-}" ] || continue
  SEEN_FILES[$current]=1
  while IFS= read -r lib; do
    [ -n "$lib" ] || continue
    if should_skip_lib "$lib"; then
      continue
    fi
    if [ -z "${SEEN_FILES[$lib]:-}" ]; then
      QUEUE+=("$lib")
    fi
    copy_lib "$lib" "$APP_DIR/usr/lib"
    copy_lib "$lib" "$APP_DIR/usr/lib/$MULTIARCH"
  done < <(collect_needed "$current")
done

if [ ! -e "$APP_DIR/usr/lib/$WEBKIT_SONAME" ] && [ ! -e "$APP_DIR/usr/lib/$MULTIARCH/$WEBKIT_SONAME" ]; then
  echo "error: failed to bundle $WEBKIT_SONAME — is libwebkit2gtk installed on the builder?" >&2
  ldd "$BINARY" >&2 || true
  exit 1
fi

# gdk-pixbuf loaders so GTK can decode the desktop icon / any PNG chrome.
PIXBUF_LOADERS=""
for candidate in \
  "/usr/lib/${MULTIARCH}/gdk-pixbuf-2.0" \
  "/usr/lib/gdk-pixbuf-2.0"
do
  if [ -d "$candidate" ]; then
    PIXBUF_LOADERS="$candidate"
    break
  fi
done
if [ -n "$PIXBUF_LOADERS" ]; then
  dest_pix="$APP_DIR/usr/lib/${MULTIARCH}/gdk-pixbuf-2.0"
  mkdir -p "$dest_pix"
  cp -a "$PIXBUF_LOADERS/." "$dest_pix/"
  loader_dir="$(find "$dest_pix" -type d -name loaders | head -n 1 || true)"
  if [ -n "$loader_dir" ] && command -v gdk-pixbuf-query-loaders >/dev/null 2>&1; then
    host_loader_dir="$(find "$PIXBUF_LOADERS" -type d -name loaders | head -n 1 || true)"
    if [ -n "$host_loader_dir" ]; then
      gdk-pixbuf-query-loaders "$host_loader_dir"/* > "$APP_DIR/usr/share/maclaw/gdk-pixbuf-loaders.cache.in" || true
      if [ -s "$APP_DIR/usr/share/maclaw/gdk-pixbuf-loaders.cache.in" ]; then
        sed -i "s|$host_loader_dir|@APPDIR@/usr/lib/${MULTIARCH}/gdk-pixbuf-2.0/$(basename "$(dirname "$host_loader_dir")")/loaders|g" \
          "$APP_DIR/usr/share/maclaw/gdk-pixbuf-loaders.cache.in"
      fi
    fi
    while IFS= read -r -d '' loader; do
      QUEUE+=("$loader")
    done < <(find "$loader_dir" -type f \( -name '*.so' -o -name '*.so.*' \) -print0 2>/dev/null)
    # Second pass for loader deps.
    for loader in "$loader_dir"/*.so; do
      [ -f "$loader" ] || continue
      while IFS= read -r lib; do
        [ -n "$lib" ] || continue
        should_skip_lib "$lib" && continue
        copy_lib "$lib" "$APP_DIR/usr/lib"
        copy_lib "$lib" "$APP_DIR/usr/lib/$MULTIARCH"
      done < <(collect_needed "$loader")
    done
  fi
fi

if [ -d /usr/share/glib-2.0/schemas ]; then
  cp -a /usr/share/glib-2.0/schemas/*.xml "$APP_DIR/usr/share/glib-2.0/schemas/" 2>/dev/null || true
  if command -v glib-compile-schemas >/dev/null 2>&1; then
    glib-compile-schemas "$APP_DIR/usr/share/glib-2.0/schemas" 2>/dev/null || true
  fi
fi

if command -v patchelf >/dev/null 2>&1; then
  rpath_gui="\$ORIGIN/../lib:\$ORIGIN/../lib/${MULTIARCH}"
  patchelf --force-rpath --set-rpath "$rpath_gui" "$APP_DIR/usr/bin/$OUT_LOWER" || true
  if [ -n "${dest_helper:-}" ]; then
    while IFS= read -r -d '' helper; do
      patchelf --force-rpath --set-rpath "\$ORIGIN:\$ORIGIN/..:\$ORIGIN/../.." "$helper" || true
    done < <(find "$dest_helper" -type f -executable -print0 2>/dev/null)
  fi
else
  echo "  patchelf not found; AppRun LD_LIBRARY_PATH will locate bundled libs"
fi

APPRUN_SRC="$SCRIPT_DIR/AppRun.sh"
if [ ! -f "$APPRUN_SRC" ]; then
  echo "missing $APPRUN_SRC" >&2
  exit 1
fi
sed \
  -e "s|__BIN_NAME__|${OUT_LOWER}|g" \
  -e "s|__WEBKIT_API__|${WEBKIT_API}|g" \
  -e "s|__WEBKIT_SONAME__|${WEBKIT_SONAME}|g" \
  -e "s|__WEBKIT_PKG__|${WEBKIT_PKG}|g" \
  -e "s|__UBUNTU_VER__|${UBUNTU_VER}|g" \
  "$APPRUN_SRC" > "$APP_DIR/AppRun"
chmod +x "$APP_DIR/AppRun"

# Record what was bundled so smoke tests / humans can inspect the artifact.
{
  echo "app=${APP_NAME}"
  echo "arch=${ARCH}"
  echo "ubuntu_label=${UBUNTU_LABEL}"
  echo "webkit_api=${WEBKIT_API}"
  echo "webkit_soname=${WEBKIT_SONAME}"
  echo "bundled_webkit=yes"
} > "$APP_DIR/usr/share/maclaw/runtime.txt"
if [ -e "$APP_DIR/usr/lib/$WEBKIT_SONAME" ] || [ -e "$APP_DIR/usr/lib/$MULTIARCH/$WEBKIT_SONAME" ]; then
  echo "webkit_lib=present" >> "$APP_DIR/usr/share/maclaw/runtime.txt"
fi
if [ -x "${dest_helper:-/nonexistent}/WebKitWebProcess" ]; then
  echo "webkit_webprocess=present" >> "$APP_DIR/usr/share/maclaw/runtime.txt"
fi

mkdir -p "$(dirname "$OUTPUT")"

download_appimagetool() {
  local url dest
  dest="${1:-}"
  url="https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-${ARCH}.AppImage"
  echo "  downloading appimagetool from $url"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$dest" "$url"
  else
    wget -q -O "$dest" "$url"
  fi
  chmod +x "$dest"
}

if ! command -v appimagetool >/dev/null 2>&1; then
  TOOL="$REPO_ROOT/appimagetool"
  if [ ! -x "$TOOL" ]; then
    download_appimagetool "$TOOL"
  fi
  APPIMAGETOOL="$TOOL"
else
  APPIMAGETOOL="$(command -v appimagetool)"
fi

export APPIMAGE_EXTRACT_AND_RUN=1
ARCH="$ARCH" "$APPIMAGETOOL" --appimage-extract-and-run --no-appstream \
  "$APP_DIR" "$OUTPUT"

chmod +x "$OUTPUT"
echo "Created $OUTPUT ($(wc -c < "$OUTPUT") bytes)"
