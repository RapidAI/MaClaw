@echo off
setlocal enabledelayedexpansion
REM ============================================================================
REM build_win_wails3.bat — wails3 dev build for MaClaw (branch wails3-migration)
REM
REM Mirrors the essential steps of build_win.bat with -tags wails3:
REM   frontend dist -> Windows resources (syso) -> GUI binary (amd64)
REM Differences from the v2 release pipeline:
REM   - wails3 desktop targets require CGO_ENABLED=1 (internal/buildguard)
REM   - no `wails3 generate bindings` step: the frontend shim routes calls
REM     through window.go.main.App Proxy -> Call.ByName (full-import-path FQN)
REM   - dev build only: no NSIS installer / multi-binary matrix
REM The v2 release pipeline (build_win.bat) is untouched.
REM ============================================================================
setlocal
cd /d "%~dp0"

set "APP_NAME=MaClaw"
set "POWERSHELL_EXE=powershell"
set "OUTPUT_DIR=dist"

REM Derive the 4-part version exactly like build_win.bat Step 2 does
REM (scripts/update_build_version.ps1 writes temp_BUILD_NUM/temp_VERSION).
"%POWERSHELL_EXE%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\update_build_version.ps1" -Root "%~dp0." -AppName "%APP_NAME%"
if errorlevel 1 (
    echo [ERROR] Failed to derive version.
    goto :error
)
set /p BUILD_NUM=<"%~dp0temp_BUILD_NUM.txt"
set /p VERSION=<"%~dp0temp_VERSION.txt"
del /q "%~dp0temp_BUILD_NUM.txt" "%~dp0temp_VERSION.txt" "%~dp0temp_PRODUCT_NAME.txt" "%~dp0temp_COMPANY_NAME.txt" "%~dp0temp_COPYRIGHT.txt" 2>nul


echo [Step 1/4] Cleaning previous build...
if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"
del /q "%OUTPUT_DIR%\%APP_NAME%_wails3_amd64.exe" 2>nul

echo [Step 2/4] Building frontend (npx vite build)...
pushd guiapp\frontend
call npx vite build
if errorlevel 1 (
    echo [ERROR] Frontend build failed.
    popd
    goto :error
)
popd

echo [Step 3/4] Generating Windows resources (manifest + versioninfo syso)...
del /q "%~dp0guiapp\resource_windows_amd64.syso" 2>nul
del /q "%~dp0build\windows\wails.exe.manifest.tmp" 2>nul
del /q "%~dp0build\windows\versioninfo.json.tmp" 2>nul
"%POWERSHELL_EXE%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\prepare_windows_resources.ps1" -Root "%~dp0." -Version "%VERSION%" -AppName "%APP_NAME%"
if errorlevel 1 (
    echo [ERROR] Failed to prepare Windows version resource inputs.
    goto :error
)
go -C tools\windowsresgen run . -arch amd64 -ico "%~dp0build\windows\icon.ico" -manifest "%~dp0build\windows\wails.exe.manifest.tmp" -versioninfo "%~dp0build\windows\versioninfo.json.tmp" -out "%~dp0guiapp\resource_windows_amd64.syso"
if errorlevel 1 (
    echo [ERROR] windowsresgen failed.
    goto :error
)

echo [Step 4/4] Compiling wails3 GUI binary (amd64)...
set "GOOS=windows"
set "GOARCH=amd64"
REM wails3 desktop targets require CGO (internal/buildguard refuses without
REM it); the machine needs gcc (mingw) on PATH for this step.
set "CGO_ENABLED=1"
scripts\goheavy.cmd go build -tags "wails3,production" -ldflags "-s -w -H windowsgui -X main.version=%VERSION%" -o "%OUTPUT_DIR%\%APP_NAME%_wails3_amd64.exe" ./cmd/maclaw-gui/
if errorlevel 1 (
    echo [ERROR] wails3 GUI build failed.
    goto :error
)

echo.
echo [OK] Build finished: %OUTPUT_DIR%\%APP_NAME%_wails3_amd64.exe (version %VERSION%)
goto :eof

:error
echo [ERROR] Build failed.
exit /b 1
