@echo off
setlocal enabledelayedexpansion
title MaClaw Chat - Release APK Builder

echo ============================================
echo   MaClaw Chat - Release APK Builder
echo ============================================
echo.

:: ── Locate Flutter SDK ──
set "FLUTTER_BIN="
where flutter >nul 2>&1 && set "FLUTTER_BIN=flutter"
if not defined FLUTTER_BIN (
    if exist "D:\flutter\bin\flutter.bat" set "FLUTTER_BIN=D:\flutter\bin\flutter"
)
if not defined FLUTTER_BIN (
    echo [ERROR] Flutter SDK not found. Set PATH or install to D:\flutter
    exit /b 1
)
echo [OK] Flutter: %FLUTTER_BIN%

:: ── Locate Android SDK ──
if not defined ANDROID_HOME (
    if exist "%LOCALAPPDATA%\Android\Sdk" set "ANDROID_HOME=%LOCALAPPDATA%\Android\Sdk"
)
if defined ANDROID_HOME (
    echo [OK] Android SDK: %ANDROID_HOME%
) else (
    echo [WARN] ANDROID_HOME not set, relying on Flutter defaults
)

:: ── Navigate to project ──
cd /d "%~dp0"
echo [OK] Project dir: %CD%
echo.

:: ── Clean previous build ──
echo [1/4] Cleaning previous build...
%FLUTTER_BIN% clean >nul 2>&1
echo       Done.

:: ── Get dependencies ──
echo [2/4] Resolving dependencies...
%FLUTTER_BIN% pub get
if errorlevel 1 (
    echo [ERROR] pub get failed
    exit /b 1
)
echo       Done.
echo.

:: ── Build fat release APK ──
echo [3/4] Building release APK (fat)...
%FLUTTER_BIN% build apk --release
if errorlevel 1 (
    echo [ERROR] Release APK build failed
    exit /b 1
)
echo       Done.
echo.

:: ── Build split-per-abi release APKs ──
echo [4/4] Building split-per-abi release APKs...
%FLUTTER_BIN% build apk --release --split-per-abi
if errorlevel 1 (
    echo [ERROR] Split-per-abi build failed
    exit /b 1
)
echo       Done.
echo.

:: ── Summary ──
echo ============================================
echo   Build Complete!
echo ============================================
echo.
echo Fat APK:
set "FAT_APK=build\app\outputs\flutter-apk\app-release.apk"
if exist "%FAT_APK%" (
    for %%F in ("%FAT_APK%") do echo   %FAT_APK%  (%%~zF bytes)
) else (
    echo   [not found]
)
echo.
echo Split APKs:
for %%A in (arm64-v8a armeabi-v7a x86_64) do (
    set "SPLIT_APK=build\app\outputs\flutter-apk\app-%%A-release.apk"
    if exist "!SPLIT_APK!" (
        for %%F in ("!SPLIT_APK!") do echo   !SPLIT_APK!  (%%~zF bytes)
    )
)
echo.

:: ── Copy to dist folder ──
if not exist "dist" mkdir dist
copy /y "%FAT_APK%" "dist\maclaw-chat-release.apk" >nul 2>&1
echo Copied fat APK to dist\maclaw-chat-release.apk
echo.
echo Install on device:  %FLUTTER_BIN% install --release
echo.

endlocal
