#!/bin/bash
# create_dev_cert.sh — Creates a self-signed code signing certificate for local
# development. Only needs to run once. The certificate is stored in the login
# keychain and persists across reboots.
#
# After creation, builds signed with this cert will have stable TCC permissions
# (screen recording, accessibility, etc.) that survive recompilation.
#
# To export for CI:
#   security export -k ~/Library/Keychains/login.keychain-db -t identities \
#     -f pkcs12 -P "<your-ci-password>" -o maclaw_dev.p12
#   base64 -i maclaw_dev.p12 | pbcopy
#   # Paste into GitHub Secret: MACOS_CERTIFICATE_BASE64
#   # Set MACOS_CERTIFICATE_PWD to <your-ci-password>

set -e

CERT_NAME="MaClaw Dev"

# Check if cert already exists
if security find-certificate -c "$CERT_NAME" ~/Library/Keychains/login.keychain-db >/dev/null 2>&1; then
    echo "✅ Certificate '$CERT_NAME' already exists in login keychain."
    echo "   To recreate, first delete it in Keychain Access, then re-run this script."
    exit 0
fi

echo "Creating self-signed code signing certificate '$CERT_NAME'..."
echo ""

# Generate cert + key via openssl
openssl req -x509 -newkey rsa:2048 -keyout /tmp/maclaw_dev.key -out /tmp/maclaw_dev.crt \
    -days 3650 -nodes -subj "/CN=$CERT_NAME" \
    -addext "keyUsage=digitalSignature" \
    -addext "extendedKeyUsage=codeSigning" 2>/dev/null

# Create p12 for keychain import (temporary password, deleted after import)
P12_PWD="maclaw_tmp_$$"
openssl pkcs12 -export -out /tmp/maclaw_dev.p12 \
    -inkey /tmp/maclaw_dev.key -in /tmp/maclaw_dev.crt \
    -passout "pass:$P12_PWD" 2>/dev/null

# Import to login keychain
security import /tmp/maclaw_dev.p12 -k ~/Library/Keychains/login.keychain-db \
    -T /usr/bin/codesign -P "$P12_PWD"

# Set trust to "always trust" for code signing
security add-trusted-cert -d -r trustRoot -k ~/Library/Keychains/login.keychain-db /tmp/maclaw_dev.crt

# Cleanup temp files
rm -f /tmp/maclaw_dev.key /tmp/maclaw_dev.crt /tmp/maclaw_dev.p12

echo ""
echo "✅ Certificate '$CERT_NAME' created and trusted."
echo "   Valid for 10 years."
echo ""
echo "   To verify: security find-identity -v -p codesigning"
echo ""
echo "   Now run your build script — it will automatically sign with this cert."
