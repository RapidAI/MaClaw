#!/bin/bash
# ci_codesign.sh — Non-interactive code signing for CI environments.
#
# This script is a standalone version of the signing logic that's also inlined
# in .github/workflows/main.yml. Use it for local testing of the CI signing flow
# or for non-GitHub CI systems (Jenkins, GitLab, etc.).
#
# Required environment variables (set via GitHub Secrets):
#   MACOS_CERTIFICATE_BASE64  — Base64-encoded .p12 file containing the signing cert
#   MACOS_CERTIFICATE_PWD     — Password for the .p12 file
#
# Optional:
#   MACOS_KEYCHAIN_PWD        — Temporary keychain password (defaults to random)
#   CODESIGN_IDENTIFIER       — Bundle identifier (defaults to com.wails.MaClaw)
#
# Usage:
#   export MACOS_CERTIFICATE_BASE64="$(cat cert.p12 | base64)"
#   export MACOS_CERTIFICATE_PWD="ci_maclaw_2026"
#   ./scripts/ci_codesign.sh dist/MaClaw.app

set -e

APP_BUNDLE="${1:?Usage: $0 <path-to-app-bundle>}"
IDENTIFIER="${CODESIGN_IDENTIFIER:-com.wails.MaClaw}"
KEYCHAIN_PWD="${MACOS_KEYCHAIN_PWD:-$(openssl rand -hex 16)}"
KEYCHAIN_PATH="$RUNNER_TEMP/maclaw-signing.keychain-db"

if [ -z "$MACOS_CERTIFICATE_BASE64" ]; then
    echo "Error: MACOS_CERTIFICATE_BASE64 not set"
    exit 1
fi
if [ -z "$MACOS_CERTIFICATE_PWD" ]; then
    echo "Error: MACOS_CERTIFICATE_PWD not set"
    exit 1
fi

# Use RUNNER_TEMP if available (GitHub Actions), otherwise /tmp
if [ -z "$RUNNER_TEMP" ]; then
    RUNNER_TEMP="/tmp"
    KEYCHAIN_PATH="/tmp/maclaw-signing.keychain-db"
fi

echo "[ci-codesign] Setting up temporary keychain..."

# Decode certificate
echo "$MACOS_CERTIFICATE_BASE64" | base64 --decode > "$RUNNER_TEMP/cert.p12"

# Create temporary keychain
security create-keychain -p "$KEYCHAIN_PWD" "$KEYCHAIN_PATH"
security set-keychain-settings -lut 21600 "$KEYCHAIN_PATH"
security unlock-keychain -p "$KEYCHAIN_PWD" "$KEYCHAIN_PATH"

# Import certificate
security import "$RUNNER_TEMP/cert.p12" \
    -k "$KEYCHAIN_PATH" \
    -P "$MACOS_CERTIFICATE_PWD" \
    -T /usr/bin/codesign \
    -T /usr/bin/security

# Allow codesign to use the keychain without UI prompt
security set-key-partition-list -S apple-tool:,apple: -k "$KEYCHAIN_PWD" "$KEYCHAIN_PATH"

# Add temporary keychain to search list (prepend so it's found first)
security list-keychains -d user -s "$KEYCHAIN_PATH" $(security list-keychains -d user | tr -d '"')

echo "[ci-codesign] Signing $APP_BUNDLE with identifier=$IDENTIFIER..."

# Clean extended attributes
xattr -cr "$APP_BUNDLE" 2>/dev/null || true

# Sign with entitlements if available, otherwise without
ENTITLEMENTS="build/darwin/entitlements.plist"
if [ -f "$ENTITLEMENTS" ]; then
    codesign --force --sign "MaClaw Dev" \
        --identifier "$IDENTIFIER" \
        --options runtime \
        --entitlements "$ENTITLEMENTS" \
        --deep \
        "$APP_BUNDLE"
else
    codesign --force --sign "MaClaw Dev" \
        --identifier "$IDENTIFIER" \
        --deep \
        "$APP_BUNDLE"
fi

echo "[ci-codesign] ✅ Signed successfully."
codesign -dv "$APP_BUNDLE" 2>&1 | grep "Identifier\|Signature\|Authority"

# Cleanup
rm -f "$RUNNER_TEMP/cert.p12"
# Note: Don't delete keychain here — other steps in the workflow may need it.
# GitHub Actions cleans RUNNER_TEMP after the job.
