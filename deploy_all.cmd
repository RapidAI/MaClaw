@echo off
setlocal EnableExtensions EnableDelayedExpansion

set "ROOT_DIR=%~dp0"
set "ROOT_DIR_TRIM=%ROOT_DIR:~0,-1%"
set "POWERSHELL=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
set "REMOTE_HOST=hubs.rapidai.tech"
set "REMOTE_PORT=22"
set "REMOTE_USER=root"
set "REMOTE_HOSTKEY=ssh-ed25519 255 SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog"
set "REMOTE_HUB_DIR=/data/soft/hub"
set "REMOTE_HUBCENTER_DIR=/data/soft/hubcenter"
set "REMOTE_TMP_DIR=/tmp/aicoder_deploy"
if "%CGO_ENABLED%"=="" (
  set "CGO_ENABLED=0"
)

set "BUILD_ROOT=%ROOT_DIR%build\deploy"
set "ARCHIVE_PATH=%BUILD_ROOT%\codeclaw-src.tar.gz"
set "REMOTE_SCRIPT=%BUILD_ROOT%\remote_deploy.sh"

if not exist "%ROOT_DIR%go.mod" (
  echo [ERROR] Missing go.mod
  exit /b 1
)

if not exist "%ROOT_DIR%go.sum" (
  echo [ERROR] Missing go.sum
  exit /b 1
)

if not exist "%ROOT_DIR%hub\cmd\hub" (
  echo [ERROR] Missing hub source: %ROOT_DIR%hub\cmd\hub
  exit /b 1
)

if not exist "%ROOT_DIR%hubcenter\cmd\hubcenter" (
  echo [ERROR] Missing hubcenter source: %ROOT_DIR%hubcenter\cmd\hubcenter
  exit /b 1
)

call :resolve_tool PLINK_EXE plink.exe
if errorlevel 1 exit /b 1

call :resolve_tool PSCP_EXE pscp.exe
if errorlevel 1 exit /b 1

call :resolve_tool TAR_EXE tar.exe
if errorlevel 1 exit /b 1

call :prompt_password
if errorlevel 1 exit /b 1

echo.
echo [1/5] Preparing connection info
echo        Host: %REMOTE_USER%@%REMOTE_HOST%:%REMOTE_PORT%
echo        HostKey: %REMOTE_HOSTKEY%
echo        Deploy hub       -> %REMOTE_HUB_DIR%
echo        Deploy hubcenter -> %REMOTE_HUBCENTER_DIR%
echo.

echo [2/5] Preparing source package...
if exist "%BUILD_ROOT%" rmdir /s /q "%BUILD_ROOT%"
mkdir "%BUILD_ROOT%" >nul 2>nul

echo [2/5] Creating source archive...
"%TAR_EXE%" -czf "%ARCHIVE_PATH%" ^
  --exclude ".git" ^
  --exclude ".gocache" ^
  --exclude ".gomodcache" ^
  --exclude ".kode" ^
  --exclude ".vscode" ^
  --exclude "./build" ^
  --exclude "./hub/bin" ^
  --exclude "./hub/package" ^
  --exclude "./hub/data" ^
  --exclude "./hub/.gocache" ^
  --exclude "./hub/.gomodcache" ^
  --exclude "./hubcenter/bin" ^
  --exclude "./hubcenter/package" ^
  --exclude "./hubcenter/data" ^
  --exclude "./hubcenter/.gocache" ^
  --exclude "./hubcenter/.gomodcache" ^
  --exclude "*.exe" ^
  --exclude "*.exe~" ^
  -C "%ROOT_DIR_TRIM%" .
if errorlevel 1 (
  echo [ERROR] Failed to create source archive.
  exit /b 1
)

echo [3/5] Writing remote deploy script...
call :write_remote_script
if errorlevel 1 exit /b 1

echo [4/5] Creating remote temp directory...
"%PLINK_EXE%" -batch -hostkey "%REMOTE_HOSTKEY%" -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_USER%@%REMOTE_HOST%" "mkdir -p %REMOTE_TMP_DIR%"
if errorlevel 1 (
  echo [ERROR] Failed to create remote temp directory.
  exit /b 1
)

echo [4/5] Uploading source archive...
"%PSCP_EXE%" -batch -hostkey "%REMOTE_HOSTKEY%" -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%ARCHIVE_PATH%" "%REMOTE_USER%@%REMOTE_HOST%:%REMOTE_TMP_DIR%/codeclaw-src.tar.gz"
if errorlevel 1 (
  echo [ERROR] Upload failed for source archive.
  exit /b 1
)

echo [4/5] Uploading remote deploy script...
"%PSCP_EXE%" -batch -hostkey "%REMOTE_HOSTKEY%" -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_SCRIPT%" "%REMOTE_USER%@%REMOTE_HOST%:%REMOTE_TMP_DIR%/remote_deploy.sh"
if errorlevel 1 (
  echo [ERROR] Upload failed for remote deploy script.
  exit /b 1
)

echo [5/5] Running remote build and deployment...
"%PLINK_EXE%" -batch -hostkey "%REMOTE_HOSTKEY%" -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_USER%@%REMOTE_HOST%" "sed -i 's/\r$//' %REMOTE_TMP_DIR%/remote_deploy.sh && chmod +x %REMOTE_TMP_DIR%/remote_deploy.sh && CGO_ENABLED=%CGO_ENABLED% REMOTE_HUB_DIR=%REMOTE_HUB_DIR% REMOTE_HUBCENTER_DIR=%REMOTE_HUBCENTER_DIR% REMOTE_TMP_DIR=%REMOTE_TMP_DIR% %REMOTE_TMP_DIR%/remote_deploy.sh"
if errorlevel 1 (
  echo [ERROR] Remote deployment failed.
  exit /b 1
)

echo.
echo Deployment completed successfully.
echo   Hub       -^> %REMOTE_HOST%:%REMOTE_HUB_DIR%
echo   HubCenter -^> %REMOTE_HOST%:%REMOTE_HUBCENTER_DIR%
echo   Mode      -^> upload source, build on remote host, keep config/data
exit /b 0

:prompt_password
if defined REMOTE_PASS exit /b 0
echo Please enter SSH password for %REMOTE_USER%@%REMOTE_HOST%.
for /f "usebackq delims=" %%I in (`"%POWERSHELL%" -NoProfile -Command "$p = Read-Host 'Password' -AsSecureString; $b = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($p); try { [Runtime.InteropServices.Marshal]::PtrToStringAuto($b) } finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($b) }"`) do (
  set "REMOTE_PASS=%%I"
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
  echo ARCHIVE_PATH="$REMOTE_TMP_DIR/codeclaw-src.tar.gz"
  echo.
  echo rm -rf "$SRC_ROOT" "$BUILD_ROOT"
  echo mkdir -p "$SRC_ROOT" "$BUILD_ROOT"
  echo tar -xzf "$ARCHIVE_PATH" -C "$SRC_ROOT"
  echo cd "$SRC_ROOT"
  echo.
  echo echo "[remote] Building hub..."
  echo GOPROXY="$GOPROXY" CGO_ENABLED="$CGO_ENABLED" go build -o "$BUILD_ROOT/codeclaw-hub" ./hub/cmd/hub
  echo echo "[remote] Building hubcenter..."
  echo GOPROXY="$GOPROXY" CGO_ENABLED="$CGO_ENABLED" go build -o "$BUILD_ROOT/codeclaw-hubcenter" ./hubcenter/cmd/hubcenter
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
  echo deploy_one "$SRC_ROOT/hub" "$REMOTE_HUB_DIR" "$BUILD_ROOT/codeclaw-hub" "codeclaw-hub"
  echo echo "[remote] Deploying hubcenter files..."
  echo deploy_one "$SRC_ROOT/hubcenter" "$REMOTE_HUBCENTER_DIR" "$BUILD_ROOT/codeclaw-hubcenter" "codeclaw-hubcenter"
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


