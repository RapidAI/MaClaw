#!/usr/bin/env bash
set -euo pipefail

echo "============================================"
echo "  MaClaw Chat - Release APK Builder"
echo "============================================"
echo

# ── Locate Flutter SDK ──
FLUTTER_BIN=""
if command -v flutter &>/dev/null; then
    FLUTTER_BIN="flutter"
elif [ -x "$HOME/flutter/bin/flutter" ]; then
    FLUTTER_BIN="$HOME/flutter/bin/flutter"
elif [ -x "/opt/flutter/bin/flutter" ]; then
    FLUTTER_BIN="/opt/flutter/bin/flutter"
fi

if [ -z "$FLUTTER_BIN" ]; then
    echo "[ERROR] Flutter SDK not found in PATH"
    exit 1
fi
echo "[OK] Flutter: $FLUTTER_BIN"

# ── Navigate to project ──
cd "$(dirname "$0")"
echo "[OK] Project dir: $(pwd)"
echo

# ── Clean ──
echo "[1/4] Cleaning previous build..."
$FLUTTER_BIN clean >/dev/null 2>&1 || true
echo "      Done."

# ── Dependencies ──
echo "[2/4] Resolving dependencies..."
$FLUTTER_BIN pub get
echo "      Done."
echo

# ── Build fat release APK ──
echo "[3/4] Building release APK (fat)..."
$FLUTTER_BIN build apk --release
echo "      Done."
echo

# ── Build split-per-abi ──
echo "[4/4] Building split-per-abi release APKs..."
$FLUTTER_BIN build apk --release --split-per-abi
echo "      Done."
echo

# ── Summary ──
echo "============================================"
echo "  Build Complete!"
echo "============================================"
echo

FAT_APK="build/app/outputs/flutter-apk/app-release.apk"
if [ -f "$FAT_APK" ]; then
    echo "Fat APK:"
    echo "  $FAT_APK  ($(stat -f%z "$FAT_APK" 2>/dev/null || stat -c%s "$FAT_APK") bytes)"
fi
echo

echo "Split APKs:"
for arch in arm64-v8a armeabi-v7a x86_64; do
    SPLIT="build/app/outputs/flutter-apk/app-${arch}-release.apk"
    if [ -f "$SPLIT" ]; then
        echo "  $SPLIT  ($(stat -f%z "$SPLIT" 2>/dev/null || stat -c%s "$SPLIT") bytes)"
    fi
done
echo

# ── Copy to dist ──
mkdir -p dist
cp -f "$FAT_APK" dist/maclaw-chat-release.apk 2>/dev/null && \
    echo "Copied fat APK to dist/maclaw-chat-release.apk"
echo
echo "Install on device:  $FLUTTER_BIN install --release"
