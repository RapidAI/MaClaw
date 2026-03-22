@echo off
setlocal enabledelayedexpansion

:: ============================================================
:: MaClaw Chat — Android Build Script
:: ============================================================
:: Usage:
::   build_android.cmd [debug|release|appbundle]
::
:: Prerequisites:
::   - Flutter SDK (3.2+) in PATH
::   - Android SDK with build-tools, platform-tools
::   - Java 17+ (for Gradle)
::   - Firebase config: android/app/google-services.json
:: ============================================================

set "BUILD_TYPE=%~1"
if "%BUILD_TYPE%"=="" set "BUILD_TYPE=release"

echo.
echo ========================================
echo  MaClaw Chat Android Build
echo  Mode: %BUILD_TYPE%
echo ========================================
echo.

:: ── Step 1: Check Flutter ──────────────────────────────────
where flutter >nul 2>&1
if errorlevel 1 (
    echo [ERROR] Flutter not found in PATH.
    echo Please install Flutter SDK and add it to PATH:
    echo   https://docs.flutter.dev/get-started/install/windows
    exit /b 1
)

:: ── Step 2: Check Android SDK ──────────────────────────────
if "%ANDROID_HOME%"=="" (
    if "%ANDROID_SDK_ROOT%"=="" (
        echo [WARN] ANDROID_HOME / ANDROID_SDK_ROOT not set.
        echo Trying default location...
        set "ANDROID_HOME=%LOCALAPPDATA%\Android\Sdk"
    ) else (
        set "ANDROID_HOME=%ANDROID_SDK_ROOT%"
    )
)
if not exist "%ANDROID_HOME%\platform-tools" (
    echo [ERROR] Android SDK not found at %ANDROID_HOME%
    echo Please install Android SDK or set ANDROID_HOME.
    exit /b 1
)
echo [OK] Android SDK: %ANDROID_HOME%

:: ── Step 3: Ensure Android platform is created ─────────────
if not exist "android" (
    echo [INFO] Creating Android platform files...
    flutter create --platforms=android .
    if errorlevel 1 (
        echo [ERROR] Failed to create Android platform.
        exit /b 1
    )
)

:: ── Step 4: Check google-services.json ─────────────────────
if not exist "android\app\google-services.json" (
    echo [WARN] android\app\google-services.json not found.
    echo Firebase push notifications will not work without it.
    echo Download from Firebase Console ^> Project Settings ^> Android app.
    echo.
    echo Creating placeholder to allow build...
    mkdir "android\app" 2>nul
    echo {"project_info":{"project_number":"000","project_id":"placeholder"},"client":[{"client_info":{"mobilesdk_app_id":"1:000:android:placeholder","android_client_info":{"package_name":"com.maclaw.chat"}},"api_key":[{"current_key":"placeholder"}]}]} > "android\app\google-services.json"
)

:: ── Step 5: Get dependencies ───────────────────────────────
echo.
echo [INFO] Getting dependencies...
flutter pub get
if errorlevel 1 (
    echo [ERROR] flutter pub get failed.
    exit /b 1
)

:: ── Step 6: Build ──────────────────────────────────────────
echo.
if /i "%BUILD_TYPE%"=="debug" (
    echo [INFO] Building debug APK...
    flutter build apk --debug
) else if /i "%BUILD_TYPE%"=="appbundle" (
    echo [INFO] Building release App Bundle ^(for Play Store^)...
    flutter build appbundle --release
) else (
    echo [INFO] Building release APK...
    flutter build apk --release --split-per-abi
)

if errorlevel 1 (
    echo.
    echo [ERROR] Build failed. Check errors above.
    exit /b 1
)

:: ── Step 7: Output ─────────────────────────────────────────
echo.
echo ========================================
echo  Build successful!
echo ========================================

if /i "%BUILD_TYPE%"=="debug" (
    echo Output: build\app\outputs\flutter-apk\app-debug.apk
) else if /i "%BUILD_TYPE%"=="appbundle" (
    echo Output: build\app\outputs\bundle\release\app-release.aab
) else (
    echo Output: build\app\outputs\flutter-apk\
    echo   app-arm64-v8a-release.apk
    echo   app-armeabi-v7a-release.apk
    echo   app-x86_64-release.apk
)

echo.
echo To install on connected device:
echo   flutter install
echo.

endlocal
