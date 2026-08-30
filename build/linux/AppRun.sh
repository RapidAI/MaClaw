#!/bin/bash
# AppImage entrypoint for MaClaw / TigerClaw / MetaStaff.
# Placeholders (__BIN_NAME__, __WEBKIT_*) are substituted by package_appimage.sh.
#
# CI bundles WebKitGTK + GTK into usr/lib so this AppImage can start on a
# remote host that does not have the GitHub runner's libwebkit2gtk package.
# Pick the build that matches the host glibc/Ubuntu:
#   Ubuntu 22.04  -> *-u2204.AppImage (WebKit 4.0)
#   Ubuntu 24.04+ -> *-u2404.AppImage (WebKit 4.1)
set -u

HERE="$(dirname "$(readlink -f "${0}")")"
export PATH="${HERE}/usr/bin:${PATH}"

# Prefer bundled shared libraries over the host. Host libc, libm, libGL stay
# unresolved here on purpose so glibc and the GPU driver come from the system.
for libdir in \
  "${HERE}/usr/lib" \
  "${HERE}/usr/lib64" \
  "${HERE}/usr/lib/x86_64-linux-gnu" \
  "${HERE}/usr/lib/aarch64-linux-gnu"
do
  if [ -d "$libdir" ]; then
    LD_LIBRARY_PATH="${libdir}${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
  fi
done
export LD_LIBRARY_PATH

# WebKitGTK spawns WebKitWebProcess / WebKitNetworkProcess from this directory.
WEBKIT_EXEC_PATH=""
for wdir in \
  "${HERE}/usr/lib/x86_64-linux-gnu/webkit2gtk-"* \
  "${HERE}/usr/lib/aarch64-linux-gnu/webkit2gtk-"* \
  "${HERE}/usr/lib/webkit2gtk-"*
do
  if [ -x "${wdir}/WebKitWebProcess" ]; then
    WEBKIT_EXEC_PATH="$wdir"
    break
  fi
done
if [ -n "$WEBKIT_EXEC_PATH" ]; then
  export WEBKIT_EXEC_PATH
fi

# bubblewrap cannot see helpers inside the AppImage squashfs.
export WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1

if [ -d "${HERE}/usr/share/glib-2.0/schemas" ]; then
  export GSETTINGS_SCHEMA_DIR="${HERE}/usr/share/glib-2.0/schemas"
fi

if [ -f "${HERE}/usr/share/maclaw/gdk-pixbuf-loaders.cache.in" ]; then
  cache_dir="${XDG_CACHE_HOME:-${HOME:-/tmp}/.cache}"
  mkdir -p "$cache_dir" 2>/dev/null || cache_dir="/tmp"
  cache_file="${cache_dir}/maclaw-gdk-pixbuf-loaders.cache"
  sed "s|@APPDIR@|${HERE}|g" \
    "${HERE}/usr/share/maclaw/gdk-pixbuf-loaders.cache.in" \
    > "$cache_file" 2>/dev/null || true
  if [ -s "$cache_file" ]; then
    export GDK_PIXBUF_MODULE_FILE="$cache_file"
  fi
fi
for pixdir in \
  "${HERE}/usr/lib/x86_64-linux-gnu/gdk-pixbuf-2.0/"*"/loaders" \
  "${HERE}/usr/lib/aarch64-linux-gnu/gdk-pixbuf-2.0/"*"/loaders" \
  "${HERE}/usr/lib/gdk-pixbuf-2.0/"*"/loaders"
do
  if [ -d "$pixdir" ]; then
    export GDK_PIXBUF_MODULEDIR="$pixdir"
    break
  fi
done

bundled_webkit=""
for candidate in \
  "${HERE}/usr/lib/__WEBKIT_SONAME__" \
  "${HERE}/usr/lib/x86_64-linux-gnu/__WEBKIT_SONAME__" \
  "${HERE}/usr/lib/aarch64-linux-gnu/__WEBKIT_SONAME__"
do
  if [ -e "$candidate" ]; then
    bundled_webkit="$candidate"
    break
  fi
done

host_webkit=""
if ldconfig -p 2>/dev/null | grep -q "__WEBKIT_SONAME__"; then
  host_webkit="yes"
fi

if [ -z "$bundled_webkit" ] && [ -z "$host_webkit" ]; then
  echo "=========================================="
  echo " Missing WebKit2GTK runtime (__WEBKIT_SONAME__)"
  echo "=========================================="
  echo ""
  echo "This AppImage was built for Ubuntu __UBUNTU_VER__ (WebKit2GTK __WEBKIT_API__)."
  echo "It normally bundles WebKit; this copy does not contain it."
  echo "Install the matching runtime, or download the CI AppImage:"
  echo ""
  echo "  sudo apt install __WEBKIT_PKG__"
  echo ""
  echo "  Ubuntu 22.04           -> *-u2204.AppImage (webkit2gtk-4.0)"
  echo "  Ubuntu 24.04 / 26.04   -> *-u2404.AppImage (webkit2gtk-4.1)"
  echo ""
  if command -v zenity >/dev/null 2>&1; then
    zenity --error --title="Missing Dependency" \
      --text="WebKit2GTK __WEBKIT_API__ is required.\n\nsudo apt install __WEBKIT_PKG__\n\nThis AppImage is for Ubuntu __UBUNTU_VER__." \
      --width=400 2>/dev/null || true
  elif command -v kdialog >/dev/null 2>&1; then
    kdialog --error "WebKit2GTK __WEBKIT_API__ is required. sudo apt install __WEBKIT_PKG__" 2>/dev/null || true
  fi
  exit 1
fi

exec "${HERE}/usr/bin/__BIN_NAME__" "$@"
