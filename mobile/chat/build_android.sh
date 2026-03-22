#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# MaClaw Chat — Android Build Script (Linux/macOS/CI)
# ============================================================
# Usage:
#   ./build_android.sh [debug|release|appbundle]
#
# Prerequisites:
#   - Flutter SDK (3.2+) in PATH
#   - Android SDK (ANDROID_HOME set)
#   - Java 17+
#   - Firebase config: android/app/google-services.json
# ============================================================

BUILD_TYPE="${1:-release}"

echo ""
echo "========================================"
echo " MaClaw Chat Android Build"
echo " Mode: $BUILD_TYPE"
echo "========================================"
echo ""

# ── Step 1: Check Flutter ──────────────────────────────────
if ! command -v flutter &>/dev/null; then
    echo "[ERROR] Flutter not found in PATH."
    echo "Install: https://docs.flutter.dev/get-started/install"
    exit 1
fi
echo "[OK] Flutter: $(flutter --version | head -1)"

# ── Step 2: Check Android SDK ──────────────────────────────
ANDROID_HOME="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
if [ -z "$ANDROID_HOME" ]; then
    # Try common default locations
    for d in "$HOME/Android/Sdk" "$HOME/Library/Android/sdk"; do
        if [ -d "$d" ]; then
            ANDROID_HOME="$d"
            break
        fi
    done
fi
if [ ! -d "${ANDROID_HOME}/platform-tools" ]; then
    echo "[ERROR] Android SDK not found. Set ANDROID_HOME."
    exit 1
fi
echo "[OK] Android SDK: $ANDROID_HOME"
export ANDROID_HOME

# ── Step 3: Ensure Android platform exists ─────────────────
if [ ! -d "android" ]; then
    echo "[INFO] Creating Android platform files..."
    flutter create --platforms=android .
fi

# ── Step 4: Check google-services.json ─────────────────────
if [ ! -f "android/app/google-services.json" ]; then
    echo "[WARN] android/app/google-services.json not found."
    echo "Firebase push won't work. Download from Firebase Console."
    echo "Creating placeholder to allow build..."
    mkdir -p android/app
    cat > android/app/google-services.json << 'EOF'
{"project_info":{"project_number":"000","project_id":"placeholder"},"client":[{"client_info":{"mobilesdk_app_id":"1:000:android:placeholder","android_client_info":{"package_name":"com.maclaw.chat"}},"api_key":[{"current_key":"placeholder"}]}]}
EOF
fi

# ── Step 5: Get dependencies ───────────────────────────────
echo ""
echo "[INFO] Getting dependencies..."
flutter pub get

# ── Step 6: Build ──────────────────────────────────────────
echo ""
case "$BUILD_TYPE" in
    debug)
        echo "[INFO] Building debug APK..."
        flutter build apk --debug
        ;;
    appbundle)
        echo "[INFO] Building release App Bundle (for Play Store)..."
        flutter build appbundle --release
        ;;
    *)
        echo "[INFO] Building release APK (split per ABI)..."
        flutter build apk --release --split-per-abi
        ;;
esac

# ── Step 7: Output ─────────────────────────────────────────
echo ""
echo "========================================"
echo " Build successful!"
echo "========================================"

case "$BUILD_TYPE" in
    debug)
        echo "Output: build/app/outputs/flutter-apk/app-debug.apk"
        ;;
    appbundle)
        echo "Output: build/app/outputs/bundle/release/app-release.aab"
        ;;
    *)
        echo "Output: build/app/outputs/flutter-apk/"
        echo "  app-arm64-v8a-release.apk"
        echo "  app-armeabi-v7a-release.apk"
        echo "  app-x86_64-release.apk"
        ;;
esac

echo ""
echo "To install on connected device:"
echo "  flutter install"
echo ""
