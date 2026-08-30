#!/bin/bash
set -e

# Set minimum macOS version to 10.15 (Catalina)
# We use -weak_framework for UniformTypeIdentifiers to allow running on 10.15
export MACOSX_DEPLOYMENT_TARGET=10.15
# Ensure Go build cache is writable in this workspace (sandbox-safe)
export GOCACHE="${PWD}/.gocache"
mkdir -p "$GOCACHE"

APP_NAME="MaClaw"
# Read version from build_number if exists, else default
if [ -f "build_number" ]; then
    BUILD_NUM=$(cat build_number)
    VERSION="7.5.0.${BUILD_NUM}"
else
    BUILD_NUM="1"
    VERSION="7.5.0.1"
fi

# Sync version to frontend
echo "Syncing version $VERSION to frontend..."
sed -i '' "s/const APP_VERSION = \".*\";/const APP_VERSION = \"$VERSION\";/" gui/frontend/src/App.tsx
cat > gui/frontend/src/version.ts <<VEOF
export const buildNumber = "$BUILD_NUM";
export const appVersion = "$VERSION";
VEOF

IDENTIFIER="com.wails.MaClaw"
OUTPUT_DIR="dist"
BIN_DIR="build/bin"

# Check for Go
if ! command -v go &> /dev/null; then
    echo "Error: 'go' command not found in PATH."
    echo "Please ensure Go is installed and available."
    exit 1
fi

echo "Starting build process for version $VERSION..."

# Clean previous build
rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"
mkdir -p "$BIN_DIR"

# Build Frontend
echo "[1/4] Building Frontend..."
cd gui/frontend
npm install --cache ./.npm_cache
npm run build
cd ../..

# Build Binaries
echo "[2/4] Compiling Go Binaries..."

# Build AMD64
echo "  - Building for amd64..."
CGO_ENABLED=1 CGO_LDFLAGS="-weak_framework UniformTypeIdentifiers" GOOS=darwin GOARCH=amd64 go build -tags desktop,production -ldflags "-X main.version=${VERSION}" -o "${BIN_DIR}/${APP_NAME}_amd64" ./gui/

# Build ARM64
echo "  - Building for arm64..."
CGO_ENABLED=1 CGO_LDFLAGS="-weak_framework UniformTypeIdentifiers" GOOS=darwin GOARCH=arm64 go build -tags desktop,production -ldflags "-X main.version=${VERSION}" -o "${BIN_DIR}/${APP_NAME}_arm64" ./gui/

# Generate Windows Resources
echo "  - Generating Windows Resources..."
RSRC_TOOL=$(go env GOPATH)/bin/rsrc
if [ ! -x "$RSRC_TOOL" ]; then
    RSRC_TOOL=rsrc
fi

if command -v "$RSRC_TOOL" &> /dev/null; then
    # Create temporary manifest with substituted values
    # Replace template variables with actual values to prevent "Side-by-side configuration is incorrect" error
    sed -e "s/{{.Name}}/$APP_NAME/g" \
        -e "s/{{.Info.ProductVersion}}.0/$VERSION/g" \
        build/windows/wails.exe.manifest > build/windows/wails.exe.manifest.tmp

    "$RSRC_TOOL" -manifest build/windows/wails.exe.manifest.tmp -ico build/windows/icon.ico -arch amd64 -o gui/resource_windows_amd64.syso
    "$RSRC_TOOL" -manifest build/windows/wails.exe.manifest.tmp -ico build/windows/icon.ico -arch arm64 -o gui/resource_windows_arm64.syso
    
    rm build/windows/wails.exe.manifest.tmp
else
    echo "Warning: rsrc tool not found. Windows executables will not have icons."
fi

# Build Windows AMD64
echo "  - Building for Windows amd64..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags desktop,production -ldflags "-s -w -H windowsgui -X main.version=${VERSION}" -o "${BIN_DIR}/${APP_NAME}_amd64.exe" ./gui/

# Build Windows ARM64
echo "  - Building for Windows arm64..."
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -tags desktop,production -ldflags "-s -w -H windowsgui -X main.version=${VERSION}" -o "${BIN_DIR}/${APP_NAME}_arm64.exe" ./gui/

# Cleanup Windows Resources
rm -f gui/resource_windows_amd64.syso gui/resource_windows_arm64.syso

# Build TUI/CLI Binaries
echo "  - Building TUI/CLI, MaClawSrv and DataSrv binaries..."
TUI_LDFLAGS="-s -w -X main.version=${VERSION}"
MACLAWSRV_LDFLAGS="-s -w -X main.serviceVersion=${VERSION}"
DATASRV_LDFLAGS="-s -w -X main.serviceVersion=${VERSION}"
for TUI_ARCH in amd64 arm64; do
    # macOS TUI
    CGO_ENABLED=0 GOOS=darwin GOARCH=$TUI_ARCH go build -ldflags "$TUI_LDFLAGS" -o "${BIN_DIR}/maclaw-tui_${TUI_ARCH}_darwin" ./tui/
    # Windows TUI
    CGO_ENABLED=0 GOOS=windows GOARCH=$TUI_ARCH go build -ldflags "$TUI_LDFLAGS" -o "${BIN_DIR}/maclaw-tui_${TUI_ARCH}.exe" ./tui/
    # Linux TUI
    CGO_ENABLED=0 GOOS=linux GOARCH=$TUI_ARCH go build -ldflags "$TUI_LDFLAGS" -o "${BIN_DIR}/maclaw-tui_${TUI_ARCH}_linux" ./tui/
    # maclaw-tool
    CGO_ENABLED=0 GOOS=darwin GOARCH=$TUI_ARCH go build -ldflags "$TUI_LDFLAGS" -o "${BIN_DIR}/maclaw-tool_${TUI_ARCH}_darwin" ./cmd/maclaw-tool/
    CGO_ENABLED=0 GOOS=windows GOARCH=$TUI_ARCH go build -ldflags "$TUI_LDFLAGS" -o "${BIN_DIR}/maclaw-tool_${TUI_ARCH}.exe" ./cmd/maclaw-tool/
    CGO_ENABLED=0 GOOS=linux GOARCH=$TUI_ARCH go build -ldflags "$TUI_LDFLAGS" -o "${BIN_DIR}/maclaw-tool_${TUI_ARCH}_linux" ./cmd/maclaw-tool/
    # maclawsrv
    CGO_ENABLED=0 GOOS=darwin GOARCH=$TUI_ARCH go build -ldflags "$MACLAWSRV_LDFLAGS" -o "${BIN_DIR}/maclawsrv_${TUI_ARCH}_darwin" ./MaClawSrv/
    CGO_ENABLED=0 GOOS=windows GOARCH=$TUI_ARCH go build -ldflags "$MACLAWSRV_LDFLAGS" -o "${BIN_DIR}/maclawsrv_${TUI_ARCH}.exe" ./MaClawSrv/
    CGO_ENABLED=0 GOOS=linux GOARCH=$TUI_ARCH go build -ldflags "$MACLAWSRV_LDFLAGS" -o "${BIN_DIR}/maclawsrv_${TUI_ARCH}_linux" ./MaClawSrv/
    # maclaw-data-srv
    CGO_ENABLED=0 GOOS=darwin GOARCH=$TUI_ARCH go -C datasrv build -ldflags "$DATASRV_LDFLAGS" -o "../${BIN_DIR}/maclaw-data-srv_${TUI_ARCH}_darwin" ./cmd/maclaw-data-srv/
    CGO_ENABLED=0 GOOS=windows GOARCH=$TUI_ARCH go -C datasrv build -ldflags "$DATASRV_LDFLAGS" -o "../${BIN_DIR}/maclaw-data-srv_${TUI_ARCH}.exe" ./cmd/maclaw-data-srv/
    CGO_ENABLED=0 GOOS=linux GOARCH=$TUI_ARCH go -C datasrv build -ldflags "$DATASRV_LDFLAGS" -o "../${BIN_DIR}/maclaw-data-srv_${TUI_ARCH}_linux" ./cmd/maclaw-data-srv/
done
echo "  TUI/CLI, MaClawSrv and DataSrv binaries built for all platforms."

# Export PATH for nfpm
export PATH=$PATH:$(go env GOPATH)/bin

# Build Linux
build_linux() {
    ARCH=$1
    echo "  - Building for Linux $ARCH..."
    
    # Check for cross-compiler if on macOS
    CC_CMD=""
    if [ "$(uname)" == "Darwin" ]; then
        if [ "$ARCH" == "amd64" ]; then
            if command -v x86_64-linux-gnu-gcc &> /dev/null; then
                CC_CMD="CC=x86_64-linux-gnu-gcc"
            else
                echo "    Skipping Linux $ARCH build: x86_64-linux-gnu-gcc not found."
                return
            fi
        elif [ "$ARCH" == "arm64" ]; then
             if command -v aarch64-linux-gnu-gcc &> /dev/null; then
                CC_CMD="CC=aarch64-linux-gnu-gcc"
             else
                echo "    Skipping Linux $ARCH build: aarch64-linux-gnu-gcc not found."
                return
            fi
        fi
    fi

    # Build binary
    # Note: On Linux/macOS cross-compile, CGO is required for Wails.
    if [ -n "$CC_CMD" ]; then
        eval $CC_CMD CGO_ENABLED=1 GOOS=linux GOARCH=$ARCH go build -tags desktop,production -ldflags "-X main.version=${VERSION}" -o "${BIN_DIR}/${APP_NAME}_${ARCH}_linux" ./gui/
    elif [ "$(uname)" == "Linux" ]; then
        CGO_ENABLED=1 GOOS=linux GOARCH=$ARCH go build -tags desktop,production -ldflags "-X main.version=${VERSION}" -o "${BIN_DIR}/${APP_NAME}_${ARCH}_linux" ./gui/
    fi
    
    # Package
    if [ -f "${BIN_DIR}/${APP_NAME}_${ARCH}_linux" ]; then
        APP_DIR="build/linux/AppDir_${ARCH}"
        rm -rf "$APP_DIR"
        mkdir -p "$APP_DIR/usr/bin"
        mkdir -p "$APP_DIR/usr/share/icons/hicolor/512x512/apps"
        
        # Copy binary
        cp "${BIN_DIR}/${APP_NAME}_${ARCH}_linux" "$APP_DIR/usr/bin/maclaw"
        
        # Copy desktop file
        cp "build/linux/maclaw.desktop" "$APP_DIR/"
        
        # Copy icon
        cp "build/appicon.png" "$APP_DIR/maclaw.png"
        cp "build/appicon.png" "$APP_DIR/.DirIcon"
        
        # Create AppRun
        cat > "$APP_DIR/AppRun" <<EOF
#!/bin/bash
HERE="\$(dirname "\$(readlink -f "\${0}")")"
export PATH="\${HERE}/usr/bin:\${PATH}"
exec maclaw "\$@"
EOF
        chmod +x "$APP_DIR/AppRun"
        
        # Check and setup appimagetool
        if ! command -v appimagetool &> /dev/null; then
            if [ "$(uname)" == "Linux" ]; then
                echo "    appimagetool not found. Downloading..."
                wget -q -O appimagetool "https://github.com/AppImage/AppImageKit/releases/download/13/appimagetool-x86_64.AppImage"
                chmod +x appimagetool
                export PATH="$(pwd):$PATH"
            fi
        fi

        if command -v appimagetool &> /dev/null; then
            echo "    Generating AppImage for Linux $ARCH..."
            # appimagetool requires ARCH variable to be set correctly
            AI_ARCH=$ARCH
            if [ "$ARCH" == "amd64" ]; then AI_ARCH="x86_64"; fi
            if [ "$ARCH" == "arm64" ]; then AI_ARCH="aarch64"; fi
            
            # Try running normally, if fails (no FUSE), try extract-and-run
if ! ARCH=$AI_ARCH appimagetool "$APP_DIR" "${OUTPUT_DIR}/MaClaw-${VERSION}-${AI_ARCH}.AppImage"; then
                echo "    Standard run failed (likely no FUSE), trying --appimage-extract-and-run..."
  ARCH=$AI_ARCH appimagetool --appimage-extract-and-run "$APP_DIR" "${OUTPUT_DIR}/MaClaw-${VERSION}-${AI_ARCH}.AppImage"
            fi
        else
            echo "    Skipping AppImage generation: appimagetool not found (and could not be downloaded/run on this OS)."
            echo "    AppDir prepared at $APP_DIR"
        fi
    fi
}

build_linux amd64
build_linux arm64

# Generate ICNS
echo "  - Generating .icns file..."
if [ -f "build/appicon.png" ]; then
    ICONSET_DIR="build/appicon.iconset"
    rm -rf "$ICONSET_DIR"
    mkdir -p "$ICONSET_DIR"
    
    # Generate standard sizes
    sips -z 16 16     "build/appicon.png" --out "${ICONSET_DIR}/icon_16x16.png" > /dev/null
    sips -z 32 32     "build/appicon.png" --out "${ICONSET_DIR}/icon_16x16@2x.png" > /dev/null
    sips -z 32 32     "build/appicon.png" --out "${ICONSET_DIR}/icon_32x32.png" > /dev/null
    sips -z 64 64     "build/appicon.png" --out "${ICONSET_DIR}/icon_32x32@2x.png" > /dev/null
    sips -z 128 128   "build/appicon.png" --out "${ICONSET_DIR}/icon_128x128.png" > /dev/null
    sips -z 256 256   "build/appicon.png" --out "${ICONSET_DIR}/icon_128x128@2x.png" > /dev/null
    sips -z 256 256   "build/appicon.png" --out "${ICONSET_DIR}/icon_256x256.png" > /dev/null
    sips -z 512 512   "build/appicon.png" --out "${ICONSET_DIR}/icon_256x256@2x.png" > /dev/null
    sips -z 512 512   "build/appicon.png" --out "${ICONSET_DIR}/icon_512x512.png" > /dev/null
    sips -z 1024 1024 "build/appicon.png" --out "${ICONSET_DIR}/icon_512x512@2x.png" > /dev/null
    
    if ! iconutil -c icns "$ICONSET_DIR" -o "build/AppIcon.icns"; then
        echo "    Warning: iconutil failed. Falling back to existing .icns if available."
        if [ -f "build/iconfile.icns" ]; then
            cp "build/iconfile.icns" "build/AppIcon.icns"
            echo "    Fallback icon copied from build/iconfile.icns"
        elif [ -f "build/AppIcon.icns" ]; then
            echo "    Keeping existing build/AppIcon.icns"
        else
            echo "    No .icns fallback found; app bundle will use PNG icon."
        fi
    fi
    if [ -f "build/AppIcon.icns" ]; then
        cp "build/AppIcon.icns" "build/iconfile.icns"
        echo "    Synced build/iconfile.icns"
    fi
    rm -rf "$ICONSET_DIR"
    echo "    Generated build/AppIcon.icns"
fi

# Function to create App Bundle
create_app_bundle() {
    ARCH=$1
    BINARY_NAME="${APP_NAME}_${ARCH}"
    BUNDLE_PATH="${OUTPUT_DIR}/${APP_NAME}_${ARCH}.app"
    
    echo "  - Creating App Bundle for $ARCH..."
    mkdir -p "${BUNDLE_PATH}/Contents/MacOS"
    mkdir -p "${BUNDLE_PATH}/Contents/Resources"
    
    # Copy Binary
    cp "${BIN_DIR}/${BINARY_NAME}" "${BUNDLE_PATH}/Contents/MacOS/${APP_NAME}"
    chmod +x "${BUNDLE_PATH}/Contents/MacOS/${APP_NAME}"
    
    # Create Info.plist (Clean generation)
    cat > "${BUNDLE_PATH}/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleName</key>
    <string>${APP_NAME}</string>
    <key>CFBundleExecutable</key>
    <string>${APP_NAME}</string>
    <key>CFBundleIdentifier</key>
    <string>${IDENTIFIER}</string>
    <key>CFBundleVersion</key>
    <string>${VERSION}</string>
    <key>CFBundleGetInfoString</key>
<string>MaClaw</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION}</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>LSMinimumSystemVersion</key>
    <string>10.15.0</string>
    <key>NSHighResolutionCapable</key>
    <string>true</string>
    <key>NSHumanReadableCopyright</key>
    <string>Copyright 2026 RapidAI</string>
    <key>NSDesktopFolderUsageDescription</key>
    <string>MaClaw needs access to your Desktop folder to manage project files.</string>
    <key>NSDocumentsFolderUsageDescription</key>
    <string>MaClaw needs access to your Documents folder to manage project files.</string>
    <key>NSDownloadsFolderUsageDescription</key>
    <string>MaClaw needs access to your Downloads folder to install tools and updates.</string>
    <key>NSLocalNetworkUsageDescription</key>
    <string>MaClaw uses local network to discover and connect to Hub services.</string>
    <key>NSPhotoLibraryUsageDescription</key>
    <string>MaClaw needs photo library access to process screenshots and images.</string>
</dict>
</plist>
EOF
        
    # Copy Icon
    if [ -f "build/AppIcon.icns" ]; then
        cp "build/AppIcon.icns" "${BUNDLE_PATH}/Contents/Resources/AppIcon.icns"
    elif [ -f "build/appicon.png" ]; then
        cp "build/appicon.png" "${BUNDLE_PATH}/Contents/Resources/AppIcon.png"
    fi
    
    touch "${BUNDLE_PATH}"
}

echo "[3/4] Creating App Bundles..."
create_app_bundle amd64
create_app_bundle arm64

# Create Universal Binary
echo "  - Creating Universal Binary..."
UNIVERSAL_BUNDLE="${OUTPUT_DIR}/${APP_NAME}.app"
rm -rf "${UNIVERSAL_BUNDLE}"
mkdir -p "${UNIVERSAL_BUNDLE}/Contents/MacOS"
mkdir -p "${UNIVERSAL_BUNDLE}/Contents/Resources"
lipo -create "${BIN_DIR}/${APP_NAME}_amd64" "${BIN_DIR}/${APP_NAME}_arm64" -output "${UNIVERSAL_BUNDLE}/Contents/MacOS/${APP_NAME}"
cp "${OUTPUT_DIR}/${APP_NAME}_arm64.app/Contents/Info.plist" "${UNIVERSAL_BUNDLE}/Contents/Info.plist"
ditto --noextattr --norsrc "${OUTPUT_DIR}/${APP_NAME}_arm64.app/Contents/Resources/" "${UNIVERSAL_BUNDLE}/Contents/Resources/"
touch "${UNIVERSAL_BUNDLE}"

# Code Sign macOS App Bundles
# Uses "MaClaw Dev" self-signed cert for stable TCC permissions across rebuilds.
# If cert not found, falls back to ad-hoc signing with stable identifier.
echo "  - Signing App Bundles..."
SIGN_IDENTITY=""
if security find-identity -v -p codesigning 2>/dev/null | grep -q "MaClaw Dev"; then
    SIGN_IDENTITY="MaClaw Dev"
    echo "    Using developer certificate: $SIGN_IDENTITY"
else
    SIGN_IDENTITY="-"
    echo "    ⚠️  'MaClaw Dev' cert not found, using ad-hoc signing."
    echo "    Run scripts/create_dev_cert.sh to create a stable signing cert."
    echo "    Without it, TCC permissions (screen recording) reset on every build."
fi

SIGNED_BUNDLE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/maclaw-signed-bundles.XXXXXX")"

codesign_bundle() {
    local BUNDLE="$1"
    if [ -d "$BUNDLE" ]; then
        local SIGN_TMP_DIR=""
        SIGN_TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/maclaw-sign.XXXXXX")"
        local SIGN_BUNDLE="${SIGN_TMP_DIR}/$(basename "$BUNDLE")"
        # codesign rejects bundles with FinderInfo/resource-fork metadata.
        # xattr -cr can leave FinderInfo behind on some file-provider backed
        # volumes, so sign from a local temp copy and then copy the clean signed
        # bundle back into the workspace.
        ditto --noextattr --norsrc "$BUNDLE" "$SIGN_BUNDLE"
        if command -v dot_clean >/dev/null 2>&1; then
            dot_clean -m "$SIGN_BUNDLE" 2>/dev/null || true
        fi
        xattr -cr "$SIGN_BUNDLE" 2>/dev/null || true
        while IFS= read -r -d '' item; do
            xattr -d com.apple.FinderInfo "$item" 2>/dev/null || true
            xattr -d com.apple.ResourceFork "$item" 2>/dev/null || true
            xattr -d com.apple.provenance "$item" 2>/dev/null || true
            xattr -d 'com.apple.fileprovider.fpfs#P' "$item" 2>/dev/null || true
            xattr -d 'com.apple.fileprovider.dir#N' "$item" 2>/dev/null || true
        done < <(find "$SIGN_BUNDLE" -print0)
        codesign --force --sign "$SIGN_IDENTITY" \
            --identifier "$IDENTIFIER" \
            --options runtime \
            --entitlements build/darwin/entitlements.plist \
            --deep \
            "$SIGN_BUNDLE" 2>/dev/null || \
        codesign --force --sign "$SIGN_IDENTITY" \
            --identifier "$IDENTIFIER" \
            --deep \
            "$SIGN_BUNDLE"
        rm -rf "${SIGNED_BUNDLE_DIR}/$(basename "$BUNDLE")"
        ditto --noextattr --norsrc "$SIGN_BUNDLE" "${SIGNED_BUNDLE_DIR}/$(basename "$BUNDLE")"
        rm -rf "$BUNDLE"
        ditto --noextattr --norsrc "$SIGN_BUNDLE" "$BUNDLE"
        rm -rf "$SIGN_TMP_DIR"
        echo "    Signed: $(basename "$BUNDLE")"
    fi
}

codesign_bundle "${OUTPUT_DIR}/${APP_NAME}_amd64.app"
codesign_bundle "${OUTPUT_DIR}/${APP_NAME}_arm64.app"
codesign_bundle "${UNIVERSAL_BUNDLE}"

# Function to create PKG
create_pkg() {
    ARCH=$1
    if [ "$ARCH" == "universal" ]; then
        BUNDLE_PATH="${OUTPUT_DIR}/${APP_NAME}.app"
        PKG_NAME="${APP_NAME}-Universal.pkg"
    else
        BUNDLE_PATH="${OUTPUT_DIR}/${APP_NAME}_${ARCH}.app"
        PKG_NAME="${APP_NAME}-${ARCH}.pkg"
    fi
    SIGNED_BUNDLE_PATH="${SIGNED_BUNDLE_DIR}/$(basename "$BUNDLE_PATH")"
    if [ -d "$SIGNED_BUNDLE_PATH" ]; then
        BUNDLE_PATH="$SIGNED_BUNDLE_PATH"
    fi
    
    # Temporary root for pkgbuild. Keep app bundles off file-provider backed
    # workspace paths so FinderInfo metadata does not poison the signature.
    TEMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/maclaw-pkg-root-${ARCH}.XXXXXX")"
    rm -rf "$TEMP_ROOT"
    mkdir -p "$TEMP_ROOT/Applications"
    # Always install as MaClaw.app regardless of arch-specific bundle name
    ditto --noextattr --norsrc "$BUNDLE_PATH" "$TEMP_ROOT/Applications/${APP_NAME}.app"
    
    SCRIPTS_DIR="build/scripts_x64"
    if [ "$ARCH" == "arm64" ] || [ "$ARCH" == "universal" ]; then
        SCRIPTS_DIR="build/scripts_arm64"
    fi
    
    echo "  - Creating PKG for $ARCH using scripts from $SCRIPTS_DIR..."
    
    # Ensure scripts are executable
    chmod +x "$SCRIPTS_DIR/preinstall"
    chmod +x "$SCRIPTS_DIR/postinstall"
    
    pkgbuild --root "$TEMP_ROOT" \
             --identifier "$IDENTIFIER" \
             --version "$VERSION" \
             --install-location "/" \
             --scripts "$SCRIPTS_DIR" \
             "${OUTPUT_DIR}/${PKG_NAME}"
    
    rm -rf "$TEMP_ROOT"
}

# Function to create standalone MaClawSrv PKG
create_maclawsrv_pkg() {
    ARCH=$1
    if [ "$ARCH" == "universal" ]; then
        SRC_BINARY="${BIN_DIR}/maclawsrv_universal_darwin"
        PKG_NAME="maclawsrv-Universal.pkg"
        if [ ! -f "$SRC_BINARY" ]; then
            lipo -create "${BIN_DIR}/maclawsrv_amd64_darwin" "${BIN_DIR}/maclawsrv_arm64_darwin" -output "$SRC_BINARY"
        fi
    else
        SRC_BINARY="${BIN_DIR}/maclawsrv_${ARCH}_darwin"
        PKG_NAME="maclawsrv-${ARCH}.pkg"
    fi

    if [ ! -f "$SRC_BINARY" ]; then
        echo "  - Skipping MaClawSrv PKG for $ARCH: $SRC_BINARY not found."
        return
    fi

    TEMP_ROOT="build/maclawsrv_pkg_root_${ARCH}"
    SCRIPTS_DIR="build/maclawsrv_pkg_scripts_${ARCH}"
    rm -rf "$TEMP_ROOT" "$SCRIPTS_DIR"
    mkdir -p "$TEMP_ROOT/usr/local/bin"
    cp "$SRC_BINARY" "$TEMP_ROOT/usr/local/bin/maclawsrv"
    chmod 755 "$TEMP_ROOT/usr/local/bin/maclawsrv"
    mkdir -p "$SCRIPTS_DIR"
    cat > "$SCRIPTS_DIR/postinstall" <<'EOF'
#!/bin/sh
set -e

LABEL="com.rapidai.maclaw.srv"
PLIST="/Library/LaunchDaemons/${LABEL}.plist"
DATA_ROOT="/Library/Application Support/MaClawSrv"
LOG_DIR="/Library/Logs/MaClawSrv"

make_secret() {
    BYTES="$1"
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex "$BYTES"
        return
    fi
    uuidgen | tr -d '-' | tr '[:upper:]' '[:lower:]'
}

ADMIN_SECRET="$(make_secret 24)"
TOKEN_SECRET="$(make_secret 32)"
if [ -f "$PLIST" ]; then
    OLD_ADMIN_SECRET="$(/usr/libexec/PlistBuddy -c 'Print :EnvironmentVariables:MACLAW_ADMIN_SECRET' "$PLIST" 2>/dev/null || true)"
    OLD_TOKEN_SECRET="$(/usr/libexec/PlistBuddy -c 'Print :EnvironmentVariables:MACLAW_TOKEN_SECRET' "$PLIST" 2>/dev/null || true)"
    [ -n "$OLD_ADMIN_SECRET" ] && ADMIN_SECRET="$OLD_ADMIN_SECRET"
    [ -n "$OLD_TOKEN_SECRET" ] && TOKEN_SECRET="$OLD_TOKEN_SECRET"
fi

mkdir -p "$DATA_ROOT" "$LOG_DIR"
chown root:wheel "$DATA_ROOT" "$LOG_DIR"
chmod 700 "$DATA_ROOT"
chmod 755 "$LOG_DIR"

if launchctl print "system/${LABEL}" >/dev/null 2>&1; then
    launchctl bootout system "$PLIST" >/dev/null 2>&1 || true
fi

cat > "$PLIST" <<PLIST_EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>${LABEL}</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/maclawsrv</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>EnvironmentVariables</key>
    <dict>
        <key>MACLAW_ADMIN_SECRET</key>
        <string>${ADMIN_SECRET}</string>
        <key>MACLAW_TOKEN_SECRET</key>
        <string>${TOKEN_SECRET}</string>
        <key>MACLAW_DATA_ROOT</key>
        <string>${DATA_ROOT}</string>
    </dict>
    <key>StandardOutPath</key>
    <string>/Library/Logs/MaClawSrv/maclawsrv.log</string>
    <key>StandardErrorPath</key>
    <string>/Library/Logs/MaClawSrv/maclawsrv.err.log</string>
</dict>
</plist>
PLIST_EOF

chown root:wheel "$PLIST"
chmod 600 "$PLIST"
launchctl bootstrap system "$PLIST"
launchctl enable "system/${LABEL}"
launchctl kickstart -k "system/${LABEL}"
exit 0
EOF
    chmod 755 "$SCRIPTS_DIR/postinstall"

    echo "  - Creating MaClawSrv PKG for $ARCH..."
    pkgbuild --root "$TEMP_ROOT" \
             --scripts "$SCRIPTS_DIR" \
             --identifier "com.rapidai.maclaw.srv" \
             --version "$VERSION" \
             --install-location "/" \
             "${OUTPUT_DIR}/${PKG_NAME}"

    rm -rf "$TEMP_ROOT" "$SCRIPTS_DIR"
}

# Function to create standalone DataSrv PKG
create_datasrv_pkg() {
    ARCH=$1
    if [ "$ARCH" == "universal" ]; then
        SRC_BINARY="${BIN_DIR}/maclaw-data-srv_universal_darwin"
        PKG_NAME="maclaw-data-srv-Universal.pkg"
        if [ ! -f "$SRC_BINARY" ]; then
            lipo -create "${BIN_DIR}/maclaw-data-srv_amd64_darwin" "${BIN_DIR}/maclaw-data-srv_arm64_darwin" -output "$SRC_BINARY"
        fi
    else
        SRC_BINARY="${BIN_DIR}/maclaw-data-srv_${ARCH}_darwin"
        PKG_NAME="maclaw-data-srv-${ARCH}.pkg"
    fi

    if [ ! -f "$SRC_BINARY" ]; then
        echo "  - Skipping DataSrv PKG for $ARCH: $SRC_BINARY not found."
        return
    fi

    TEMP_ROOT="build/datasrv_pkg_root_${ARCH}"
    rm -rf "$TEMP_ROOT"
    mkdir -p "$TEMP_ROOT/usr/local/bin"
    cp "$SRC_BINARY" "$TEMP_ROOT/usr/local/bin/maclaw-data-srv"
    chmod 755 "$TEMP_ROOT/usr/local/bin/maclaw-data-srv"

    echo "  - Creating DataSrv PKG for $ARCH..."
    pkgbuild --root "$TEMP_ROOT" \
             --identifier "com.rapidai.maclaw.datasrv" \
             --version "$VERSION" \
             --install-location "/" \
             "${OUTPUT_DIR}/${PKG_NAME}"

    rm -rf "$TEMP_ROOT"
}

echo "[4/4] Creating Packages..."
cp "${BIN_DIR}/${APP_NAME}_amd64.exe" "${OUTPUT_DIR}/"
cp "${BIN_DIR}/${APP_NAME}_arm64.exe" "${OUTPUT_DIR}/"

# Copy TUI/CLI binaries to output
for TUI_ARCH in amd64 arm64; do
    cp "${BIN_DIR}/maclaw-tui_${TUI_ARCH}_darwin" "${OUTPUT_DIR}/" 2>/dev/null || true
    cp "${BIN_DIR}/maclaw-tui_${TUI_ARCH}.exe" "${OUTPUT_DIR}/" 2>/dev/null || true
    cp "${BIN_DIR}/maclaw-tui_${TUI_ARCH}_linux" "${OUTPUT_DIR}/" 2>/dev/null || true
    cp "${BIN_DIR}/maclaw-tool_${TUI_ARCH}_darwin" "${OUTPUT_DIR}/" 2>/dev/null || true
    cp "${BIN_DIR}/maclaw-tool_${TUI_ARCH}.exe" "${OUTPUT_DIR}/" 2>/dev/null || true
    cp "${BIN_DIR}/maclaw-tool_${TUI_ARCH}_linux" "${OUTPUT_DIR}/" 2>/dev/null || true
    cp "${BIN_DIR}/maclawsrv_${TUI_ARCH}_darwin" "${OUTPUT_DIR}/" 2>/dev/null || true
    cp "${BIN_DIR}/maclawsrv_${TUI_ARCH}.exe" "${OUTPUT_DIR}/" 2>/dev/null || true
    cp "${BIN_DIR}/maclawsrv_${TUI_ARCH}_linux" "${OUTPUT_DIR}/" 2>/dev/null || true
    cp "${BIN_DIR}/maclaw-data-srv_${TUI_ARCH}_darwin" "${OUTPUT_DIR}/" 2>/dev/null || true
    cp "${BIN_DIR}/maclaw-data-srv_${TUI_ARCH}.exe" "${OUTPUT_DIR}/" 2>/dev/null || true
    cp "${BIN_DIR}/maclaw-data-srv_${TUI_ARCH}_linux" "${OUTPUT_DIR}/" 2>/dev/null || true
done

create_pkg amd64
create_pkg arm64
create_pkg universal
rm -rf "$SIGNED_BUNDLE_DIR"
create_maclawsrv_pkg amd64
create_maclawsrv_pkg arm64
create_maclawsrv_pkg universal
create_datasrv_pkg amd64
create_datasrv_pkg arm64
create_datasrv_pkg universal

echo "Build Complete!"
echo "App Bundles and Packages are in $OUTPUT_DIR"
