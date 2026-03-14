#!/bin/sh
set -eu

SCHEME=RapidAIHubShell
CONFIGURATION=Debug
SDK=iphonesimulator
DIST_DIR="$(CDPATH= cd -- "$(dirname "$0")/../dist" && pwd)"
DERIVED_DATA_DIR="$DIST_DIR/ios-deriveddata"
APP_BUNDLE="$DERIVED_DATA_DIR/Build/Products/${CONFIGURATION}-iphonesimulator/RapidAIHubShell.app"
COPIED_APP="$DIST_DIR/maclaw-ios-simulator.app"

if ! command -v xcodebuild >/dev/null 2>&1; then
  echo "[ERROR] xcodebuild was not found."
  echo "Run this on macOS with Xcode command line tools installed."
  exit 1
fi

mkdir -p "$DERIVED_DATA_DIR"
xcodebuild -project "RapidAIHubShell.xcodeproj" -scheme "$SCHEME" -sdk "$SDK" -configuration "$CONFIGURATION" -derivedDataPath "$DERIVED_DATA_DIR" CODE_SIGNING_ALLOWED=NO build

if [ -d "$APP_BUNDLE" ]; then
  rm -rf "$COPIED_APP"
  cp -R "$APP_BUNDLE" "$COPIED_APP"
fi

echo
echo "Build finished."
echo "DerivedData path:"
echo "  $DERIVED_DATA_DIR"
if [ -d "$COPIED_APP" ]; then
  echo "Copied simulator app:"
  echo "  $COPIED_APP"
fi
