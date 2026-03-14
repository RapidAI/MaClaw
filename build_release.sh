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
    VERSION="3.5.0.${BUILD_NUM}"
else
    BUILD_NUM="1"
    VERSION="3.5.0.1"
fi

# Sync version to frontend
echo "Syncing version $VERSION to frontend..."
sed -i '' "s/const APP_VERSION = \".*\";/const APP_VERSION = \"$VERSION\";/" frontend/src/App.tsx
echo "export const buildNumber = \"$BUILD_NUM\";" > frontend/src/version.ts

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
cd frontend
npm install --cache ./.npm_cache
npm run build
cd ..

# Build Binaries
echo "[2/4] Compiling Go Binaries..."

# Build AMD64
echo "  - Building for amd64..."
CGO_ENABLED=1 CGO_LDFLAGS="-weak_framework UniformTypeIdentifiers" GOOS=darwin GOARCH=amd64 go build -tags desktop,production -o "${BIN_DIR}/${APP_NAME}_amd64"

# Build ARM64
echo "  - Building for arm64..."
CGO_ENABLED=1 CGO_LDFLAGS="-weak_framework UniformTypeIdentifiers" GOOS=darwin GOARCH=arm64 go build -tags desktop,production -o "${BIN_DIR}/${APP_NAME}_arm64"

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

    "$RSRC_TOOL" -manifest build/windows/wails.exe.manifest.tmp -ico build/windows/icon.ico -arch amd64 -o resource_windows_amd64.syso
    "$RSRC_TOOL" -manifest build/windows/wails.exe.manifest.tmp -ico build/windows/icon.ico -arch arm64 -o resource_windows_arm64.syso
    
    rm build/windows/wails.exe.manifest.tmp
else
    echo "Warning: rsrc tool not found. Windows executables will not have icons."
fi

# Build Windows AMD64
echo "  - Building for Windows amd64..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags desktop,production -ldflags "-s -w -H windowsgui" -o "${BIN_DIR}/${APP_NAME}_amd64.exe"

# Build Windows ARM64
echo "  - Building for Windows arm64..."
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -tags desktop,production -ldflags "-s -w -H windowsgui" -o "${BIN_DIR}/${APP_NAME}_arm64.exe"

# Cleanup Windows Resources
rm -f resource_windows_amd64.syso resource_windows_arm64.syso

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
        eval $CC_CMD CGO_ENABLED=1 GOOS=linux GOARCH=$ARCH go build -tags desktop,production -o "${BIN_DIR}/${APP_NAME}_${ARCH}_linux"
    elif [ "$(uname)" == "Linux" ]; then
        CGO_ENABLED=1 GOOS=linux GOARCH=$ARCH go build -tags desktop,production -o "${BIN_DIR}/${APP_NAME}_${ARCH}_linux"
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
    <string>Copyright 2025</string>
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
mkdir -p "${UNIVERSAL_BUNDLE}/Contents/MacOS"
mkdir -p "${UNIVERSAL_BUNDLE}/Contents/Resources"
lipo -create "${BIN_DIR}/${APP_NAME}_amd64" "${BIN_DIR}/${APP_NAME}_arm64" -output "${UNIVERSAL_BUNDLE}/Contents/MacOS/${APP_NAME}"
cp "${OUTPUT_DIR}/${APP_NAME}_arm64.app/Contents/Info.plist" "${UNIVERSAL_BUNDLE}/Contents/Info.plist"
cp -R "${OUTPUT_DIR}/${APP_NAME}_arm64.app/Contents/Resources/" "${UNIVERSAL_BUNDLE}/Contents/Resources/"
touch "${UNIVERSAL_BUNDLE}"

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
    
    # Temporary root for pkgbuild
    TEMP_ROOT="build/pkg_root_${ARCH}"
    rm -rf "$TEMP_ROOT"
    mkdir -p "$TEMP_ROOT/Applications"
    cp -R "$BUNDLE_PATH" "$TEMP_ROOT/Applications/"
    
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

echo "[4/4] Creating Packages..."
cp "${BIN_DIR}/${APP_NAME}_amd64.exe" "${OUTPUT_DIR}/"
cp "${BIN_DIR}/${APP_NAME}_arm64.exe" "${OUTPUT_DIR}/"
create_pkg amd64
create_pkg arm64
create_pkg universal

echo "Build Complete!"
echo "App Bundles and Packages are in $OUTPUT_DIR"

