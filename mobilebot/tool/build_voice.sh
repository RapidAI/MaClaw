#!/usr/bin/env bash
# Build libmaclaw_voice.so (Kokoro TTS + SenseVoice ASR — the desktop's own
# Go inference) for Android jniLibs. Mirrors tool/build_voice.cmd for Linux
# CI runners (ubuntu-latest has the Android NDK preinstalled).
set -euo pipefail
cd "$(dirname "$0")/../.."   # repo root — voiceffi imports corelib via the root go.mod

NDK="${ANDROID_NDK_HOME:-${ANDROID_NDK_LATEST_HOME:-}}"
if [ -z "$NDK" ]; then
  NDK=$(ls -d "${ANDROID_HOME:-/usr/local/lib/android/sdk}"/ndk/* 2>/dev/null | sort -V | tail -1)
fi
if [ -z "$NDK" ] || [ ! -d "$NDK" ]; then
  echo "Android NDK not found" >&2
  exit 1
fi
BIN="$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin"
OUT="mobilebot/android/app/src/main/jniLibs"
mkdir -p "$OUT/arm64-v8a" "$OUT/x86_64"
export CGO_ENABLED=1

GOOS=android GOARCH=arm64 \
  CC="$BIN/aarch64-linux-android21-clang" CXX="$BIN/aarch64-linux-android21-clang++" \
  go build -buildmode=c-shared -o "$OUT/arm64-v8a/libmaclaw_voice.so" ./mobilebot/native/voiceffi/
echo "  arm64-v8a ok"

# armv7 skipped: kokoro simd kernels only exist for arm64/amd64.
GOOS=android GOARCH=amd64 \
  CC="$BIN/x86_64-linux-android21-clang" CXX="$BIN/x86_64-linux-android21-clang++" \
  go build -buildmode=c-shared -o "$OUT/x86_64/libmaclaw_voice.so" ./mobilebot/native/voiceffi/
echo "  x86_64 ok"
echo "Built libmaclaw_voice.so for arm64-v8a x86_64"
