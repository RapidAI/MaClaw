@echo off
setlocal EnableExtensions EnableDelayedExpansion

set "ROOT_DIR=%~dp0"
set "ROOT_DIR_TRIM=%ROOT_DIR:~0,-1%"
set "POWERSHELL=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
set "PROMPT_SCRIPT=%ROOT_DIR%prompt_password.ps1"
set "REMOTE_HOST=hubs.mypapers.top"
set "REMOTE_PORT=22"
set "REMOTE_USER=root"
set "REMOTE_HOSTKEY=ssh-ed25519 255 SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog"
set "REMOTE_HUB_DIR=/data/soft/hub"
set "REMOTE_HUBCENTER_DIR=/data/soft/hubcenter"
set "REMOTE_TMP_DIR=/tmp/aicoder_deploy"
if not defined REMOTE_PASS (
  set "PAUSE_ON_EXIT=1"
)
if "%CGO_ENABLED%"=="" (
  set "CGO_ENABLED=0"
)

set "BUILD_ROOT=%ROOT_DIR%build\deploy"
set "STAGE_ROOT=%BUILD_ROOT%\stage"
set "ARCHIVE_PATH=%BUILD_ROOT%\maclaw-src.tar.gz"
set "REMOTE_SCRIPT=%BUILD_ROOT%\remote_deploy.sh"
set "PASSWORD_FILE=%TEMP%\deploy_all_password_%RANDOM%_%RANDOM%.txt"

goto :main

:exit_with
set "EXIT_CODE=%~1"
REM -- Ensure password temp file is always cleaned up --
if exist "%PASSWORD_FILE%" del /q "%PASSWORD_FILE%" >nul 2>nul
if defined PAUSE_ON_EXIT (
  echo.
  pause
)
exit /b %EXIT_CODE%

:main

if not exist "%ROOT_DIR%go.mod" (
  echo [ERROR] Missing go.mod
  goto :fail
)

if not exist "%ROOT_DIR%go.sum" (
  echo [ERROR] Missing go.sum
  goto :fail
)

if not exist "%ROOT_DIR%hub\cmd\hub" (
  echo [ERROR] Missing hub source: %ROOT_DIR%hub\cmd\hub
  goto :fail
)

if not exist "%ROOT_DIR%hubcenter\cmd\hubcenter" (
  echo [ERROR] Missing hubcenter source: %ROOT_DIR%hubcenter\cmd\hubcenter
  goto :fail
)

call :resolve_tool PLINK_EXE plink.exe
if errorlevel 1 goto :fail

call :resolve_tool PSCP_EXE pscp.exe
if errorlevel 1 goto :fail

call :resolve_tool TAR_EXE tar.exe
if errorlevel 1 goto :fail

call :prompt_password
if errorlevel 1 goto :fail

echo.
echo [1/5] Preparing connection info
echo        Host: %REMOTE_USER%@%REMOTE_HOST%:%REMOTE_PORT%
echo        HostKey: %REMOTE_HOSTKEY%
echo        Deploy hub       -^> %REMOTE_HUB_DIR%
echo        Deploy hubcenter -^> %REMOTE_HUBCENTER_DIR%
echo.

echo [2/5] Preparing source package...
if exist "%BUILD_ROOT%" rmdir /s /q "%BUILD_ROOT%"
mkdir "%BUILD_ROOT%" >nul 2>nul
mkdir "%STAGE_ROOT%" >nul 2>nul

echo [2/5] Staging source tree...
call :stage_source_tree
if errorlevel 1 goto :fail

echo [2/5] Creating source archive...
"%TAR_EXE%" -czf "%ARCHIVE_PATH%" -C "%STAGE_ROOT%" .
if errorlevel 1 (
  echo [ERROR] Failed to create source archive.
  goto :fail
)

echo [3/5] Writing remote deploy script...
call :write_remote_script
if errorlevel 1 goto :fail

echo [4/5] Creating remote temp directory...
"%PLINK_EXE%" -batch -hostkey "%REMOTE_HOSTKEY%" -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_USER%@%REMOTE_HOST%" "mkdir -p %REMOTE_TMP_DIR%"
if errorlevel 1 (
  echo [ERROR] Failed to create remote temp directory.
  goto :fail
)

echo [4/5] Uploading source archive...
"%PSCP_EXE%" -batch -hostkey "%REMOTE_HOSTKEY%" -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%ARCHIVE_PATH%" "%REMOTE_USER%@%REMOTE_HOST%:%REMOTE_TMP_DIR%/maclaw-src.tar.gz"
if errorlevel 1 (
  echo [ERROR] Upload failed for source archive.
  goto :fail
)

echo [4/5] Uploading remote deploy script...
"%PSCP_EXE%" -batch -hostkey "%REMOTE_HOSTKEY%" -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_SCRIPT%" "%REMOTE_USER%@%REMOTE_HOST%:%REMOTE_TMP_DIR%/remote_deploy.sh"
if errorlevel 1 (
  echo [ERROR] Upload failed for remote deploy script.
  goto :fail
)

echo [5/5] Running remote build and deployment...
"%PLINK_EXE%" -batch -hostkey "%REMOTE_HOSTKEY%" -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_USER%@%REMOTE_HOST%" "sed -i 's/\r$//' %REMOTE_TMP_DIR%/remote_deploy.sh && chmod +x %REMOTE_TMP_DIR%/remote_deploy.sh && CGO_ENABLED=%CGO_ENABLED% REMOTE_HUB_DIR=%REMOTE_HUB_DIR% REMOTE_HUBCENTER_DIR=%REMOTE_HUBCENTER_DIR% REMOTE_TMP_DIR=%REMOTE_TMP_DIR% %REMOTE_TMP_DIR%/remote_deploy.sh"
if errorlevel 1 (
  echo [ERROR] Remote deployment failed.
  goto :fail
)

echo.
echo Deployment completed successfully.
echo   Hub       -^> %REMOTE_HOST%:%REMOTE_HUB_DIR%
echo   HubCenter -^> %REMOTE_HOST%:%REMOTE_HUBCENTER_DIR%
echo   Mode      -^> upload source, build on remote host, keep config/data
call :exit_with 0
goto :eof

:fail
call :exit_with 1
goto :eof

:prompt_password
if defined REMOTE_PASS exit /b 0
if not exist "%PROMPT_SCRIPT%" (
  echo [ERROR] Missing password prompt helper: %PROMPT_SCRIPT%
  exit /b 1
)
echo Please enter SSH password for %REMOTE_USER%@%REMOTE_HOST%.
del /q "%PASSWORD_FILE%" >nul 2>nul
"%POWERSHELL%" -NoProfile -ExecutionPolicy Bypass -File "%PROMPT_SCRIPT%" -Prompt "Password" -OutputPath "%PASSWORD_FILE%"
if exist "%PASSWORD_FILE%" (
  set /p REMOTE_PASS=<"%PASSWORD_FILE%"
  del /q "%PASSWORD_FILE%" >nul 2>nul
)
if not defined REMOTE_PASS (
  echo [ERROR] Password input was empty.
  exit /b 1
)
exit /b 0

:resolve_tool
set "%~1="
for /f "delims=" %%I in ('where.exe %~2 2^>nul') do (
  set "%~1=%%I"
  goto :resolve_tool_done
)
:resolve_tool_done
if not defined %~1 (
  echo [ERROR] Required tool not found: %~2
  exit /b 1
)
exit /b 0

:stage_source_tree
if not exist "%POWERSHELL%" (
  echo [ERROR] PowerShell not found: %POWERSHELL%
  exit /b 1
)
"%POWERSHELL%" -NoProfile -ExecutionPolicy Bypass -Command ^
  "$ErrorActionPreference = 'Stop';" ^
  "$src = '%ROOT_DIR_TRIM%';" ^
  "$dst = '%STAGE_ROOT%';" ^
  "$skipNames = @('.git','.gocache','.gomodcache','.kiro','.kode','.vscode','build','dist');" ^
  "Get-ChildItem -Path $src -Force | Where-Object { $skipNames -notcontains $_.Name } | ForEach-Object { Copy-Item -Path $_.FullName -Destination $dst -Recurse -Force };" ^
  "$removePaths = @('frontend\node_modules','hub\bin','hub\package','hub\data','hub\.gocache','hub\.gomodcache','hubcenter\bin','hubcenter\package','hubcenter\data','hubcenter\.gocache','hubcenter\.gomodcache','openclaw-bridge\node_modules','openclaw-bridge\dist');" ^
  "foreach ($rel in $removePaths) { $path = Join-Path $dst $rel; if (Test-Path $path) { Remove-Item -Recurse -Force $path -ErrorAction SilentlyContinue } };" ^
  "Get-ChildItem -Path $dst -Recurse -File -Include *.exe,*.exe~ -Force | Remove-Item -Force -ErrorAction SilentlyContinue;"
if errorlevel 1 (
  echo [ERROR] Failed to stage source tree.
  exit /b 1
)
exit /b 0

:write_remote_script
setlocal DisableDelayedExpansion
(
  echo #!/bin/sh
  echo set -eu
  echo.
  echo : "${REMOTE_TMP_DIR:=/tmp/aicoder_deploy}"
  echo : "${REMOTE_HUB_DIR:=/data/soft/hub}"
  echo : "${REMOTE_HUBCENTER_DIR:=/data/soft/hubcenter}"
  echo : "${CGO_ENABLED:=0}"
  echo : "${GOPROXY:=https://goproxy.cn,direct}"
  echo.
  echo if ! command -v go ^>/dev/null 2^>^&1; then
  echo   echo "[ERROR] go is not installed on remote host" ^>^&2
  echo   exit 1
  echo fi
  echo.
  echo SRC_ROOT="$REMOTE_TMP_DIR/src"
  echo BUILD_ROOT="$REMOTE_TMP_DIR/build"
  echo ARCHIVE_PATH="$REMOTE_TMP_DIR/maclaw-src.tar.gz"
  echo.
  echo rm -rf "$SRC_ROOT" "$BUILD_ROOT"
  echo mkdir -p "$SRC_ROOT" "$BUILD_ROOT"
  echo tar -xzf "$ARCHIVE_PATH" -C "$SRC_ROOT"
  echo cd "$SRC_ROOT"
  echo.
  echo echo "[remote] Downloading dependencies..."
  echo GOPROXY="$GOPROXY" go mod download
  echo.
  echo echo "[remote] Building hub..."
  echo GOPROXY="$GOPROXY" CGO_ENABLED="$CGO_ENABLED" go build -o "$BUILD_ROOT/maclaw-hub" ./hub/cmd/hub
  echo echo "[remote] Building hubcenter..."
  echo GOPROXY="$GOPROXY" CGO_ENABLED="$CGO_ENABLED" go build -o "$BUILD_ROOT/maclaw-hubcenter" ./hubcenter/cmd/hubcenter
  echo.
  echo deploy_one^(^) {
  echo   source_dir="$1"
  echo   target_dir="$2"
  echo   binary_path="$3"
  echo   binary_name="$4"
  echo.
  echo   mkdir -p "$target_dir" "$target_dir/configs" "$target_dir/data" "$target_dir/data/logs"
  echo   cp -f "$binary_path" "$target_dir/$binary_name"
  echo   chmod +x "$target_dir/$binary_name"
  echo.
  echo   if [ -f "$source_dir/start.sh" ]; then
  echo     cp -f "$source_dir/start.sh" "$target_dir/start.sh"
  echo     sed -i 's/\r$//' "$target_dir/start.sh"
  echo     chmod +x "$target_dir/start.sh"
  echo   fi
  echo.
  echo   if [ -f "$source_dir/configs/config.example.yaml" ]; then
  echo     cp -f "$source_dir/configs/config.example.yaml" "$target_dir/configs/config.example.yaml"
  echo   fi
  echo.
  echo   if [ ! -f "$target_dir/configs/config.yaml" ] ^&^& [ -f "$target_dir/configs/config.example.yaml" ]; then
  echo     cp -f "$target_dir/configs/config.example.yaml" "$target_dir/configs/config.yaml"
  echo   fi
  echo.
  echo   if [ -d "$source_dir/web" ]; then
  echo     rm -rf "$target_dir/web"
  echo     cp -R "$source_dir/web" "$target_dir/web"
  echo   fi
  echo }
  echo.
  echo echo "[remote] Deploying hub files..."
  echo deploy_one "$SRC_ROOT/hub" "$REMOTE_HUB_DIR" "$BUILD_ROOT/maclaw-hub" "maclaw-hub"
  echo echo "[remote] Deploying hubcenter files..."
  echo deploy_one "$SRC_ROOT/hubcenter" "$REMOTE_HUBCENTER_DIR" "$BUILD_ROOT/maclaw-hubcenter" "maclaw-hubcenter"
  echo.
  echo # Deploy openclaw-bridge ^(Node.js project^)
  echo BRIDGE_SRC="$SRC_ROOT/openclaw-bridge"
  echo BRIDGE_DST="$REMOTE_HUB_DIR/openclaw-bridge"
  echo if [ -d "$BRIDGE_SRC" ] ^&^& [ -f "$BRIDGE_SRC/package.json" ]; then
  echo   echo "[remote] Deploying openclaw-bridge..."
  echo   mkdir -p "$BRIDGE_DST"
  echo   cp -f "$BRIDGE_SRC/package.json" "$BRIDGE_DST/package.json"
  echo   cp -f "$BRIDGE_SRC/tsconfig.json" "$BRIDGE_DST/tsconfig.json" 2^>/dev/null ^|^| true
  echo   rm -rf "$BRIDGE_DST/src" "$BRIDGE_DST/dist"
  echo   cp -Rf "$BRIDGE_SRC/src" "$BRIDGE_DST/src"
  echo   if [ -f "$BRIDGE_SRC/config.example.json" ]; then
  echo     cp -f "$BRIDGE_SRC/config.example.json" "$BRIDGE_DST/config.example.json"
  echo   fi
  echo   if command -v npm ^>/dev/null 2^>^&1; then
  echo     echo "[remote] Running npm install in openclaw-bridge..."
  echo     cd "$BRIDGE_DST" ^&^& npm install 2^>^&1 ^|^| echo "[WARN] npm install failed for openclaw-bridge"
  echo     echo "[remote] Building openclaw-bridge..."
  echo     npx tsc 2^>^&1 ^|^| echo "[WARN] tsc build failed for openclaw-bridge"
  echo     echo "[remote] Pruning dev dependencies..."
  echo     npm prune --production 2^>^&1 ^|^| true
  echo     cd "$SRC_ROOT"
  echo   else
  echo     echo "[WARN] npm not found on remote host, skipping openclaw-bridge dependencies"
  echo   fi
  echo else
  echo   echo "[remote] openclaw-bridge source not found, skipping"
  echo fi
  echo.
  echo echo "[remote] Restarting hub..."
  echo if [ -x "$REMOTE_HUB_DIR/start.sh" ]; then
  echo   cd "$REMOTE_HUB_DIR"
  echo   ./start.sh
  echo fi
  echo echo "[remote] Restarting hubcenter..."
  echo if [ -x "$REMOTE_HUBCENTER_DIR/start.sh" ]; then
  echo   cd "$REMOTE_HUBCENTER_DIR"
  echo   ./start.sh
  echo fi
  echo.
  echo rm -rf "$SRC_ROOT" "$BUILD_ROOT"
  echo rm -f "$ARCHIVE_PATH" "$REMOTE_TMP_DIR/remote_deploy.sh"
  echo echo "Remote build and deploy finished."
) > "%REMOTE_SCRIPT%"

if errorlevel 1 (
  endlocal
  echo [ERROR] Failed to write remote deploy script.
  exit /b 1
)
endlocal
exit /b 0
