@echo off
setlocal EnableExtensions EnableDelayedExpansion
REM =========================================================================
REM  deploy_iworker.cmd - Deploy iWorkerCenter and iWorkerCloud services
REM
REM  Builds Linux/amd64 binaries locally, uploads binaries + web assets to
REM  www.driverdevelop.com, and restarts both services. The SSH password is
REM  requested at runtime unless REMOTE_PASS is already set.
REM =========================================================================

set "ROOT_DIR=%~dp0"
set "ROOT_DIR_TRIM=%ROOT_DIR:~0,-1%"
set "POWERSHELL=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
set "PROMPT_SCRIPT=%ROOT_DIR%prompt_password.ps1"

if not defined REMOTE_HOST set "REMOTE_HOST=www.driverdevelop.com"
if not defined REMOTE_PORT set "REMOTE_PORT=22"
if not defined REMOTE_USER set "REMOTE_USER=root"
if not defined REMOTE_HOSTKEY set "REMOTE_HOSTKEY=ssh-ed25519 255 SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog"
if not defined REMOTE_TMP_DIR set "REMOTE_TMP_DIR=/tmp/iworker_services_deploy"
if not defined IWORKERCENTER_DEPLOY_DIR set "IWORKERCENTER_DEPLOY_DIR=/data/soft/iworkercenter"
if not defined IWORKERCLOUD_DEPLOY_DIR set "IWORKERCLOUD_DEPLOY_DIR=/data/soft/iworkercloud"
if not defined IWORKERCENTER_PORT set "IWORKERCENTER_PORT=9377"
if not defined IWORKERCLOUD_PORT set "IWORKERCLOUD_PORT=9366"
if not defined IWORKERCLOUD_PUBLIC_BASE_URL set "IWORKERCLOUD_PUBLIC_BASE_URL=https://www.driverdevelop.com"
if not defined GOPROXY set "GOPROXY=https://goproxy.cn,direct"
if not defined REMOTE_PASS set "PAUSE_ON_EXIT=1"

set "BUILD_ROOT=%ROOT_DIR%build\iworker_services_deploy"
set "STAGE_ROOT=%BUILD_ROOT%\stage"
set "ARCHIVE_PATH=%BUILD_ROOT%\iworker-services-deploy.tar.gz"
set "REMOTE_SCRIPT=%BUILD_ROOT%\remote_deploy_iworker_services.sh"
set "PASSWORD_FILE=%TEMP%\deploy_iworker_password_%RANDOM%_%RANDOM%.txt"

goto :main

:exit_with
set "EXIT_CODE=%~1"
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
if not exist "%ROOT_DIR%iWorkerCenter\cmd\iworkercenter" (
  echo [ERROR] Missing iWorkerCenter source
  goto :fail
)
if not exist "%ROOT_DIR%iWorkerCloud\main.go" (
  echo [ERROR] Missing iWorkerCloud source
  goto :fail
)
if not exist "%ROOT_DIR%iWorkerCenter\cmd\iworkercenter\web" (
  echo [ERROR] Missing iWorkerCenter web assets
  goto :fail
)

call :resolve_tool GO_EXE go.exe
if errorlevel 1 goto :fail
call :resolve_tool PLINK_EXE plink.exe
if errorlevel 1 goto :fail
call :resolve_tool PSCP_EXE pscp.exe
if errorlevel 1 goto :fail
call :resolve_tool TAR_EXE tar.exe
if errorlevel 1 goto :fail
call :resolve_tool NPM_EXE npm.cmd
if errorlevel 1 goto :fail

call :prompt_password
if errorlevel 1 goto :fail

set "HOSTKEY_ARG="
if defined REMOTE_HOSTKEY set HOSTKEY_ARG=-hostkey "%REMOTE_HOSTKEY%"

echo.
echo [1/8] Connection info
echo        Host: %REMOTE_USER%@%REMOTE_HOST%:%REMOTE_PORT%
echo        iWorkerCenter -^> %IWORKERCENTER_DEPLOY_DIR%  port %IWORKERCENTER_PORT%
echo        iWorkerCloud  -^> %IWORKERCLOUD_DEPLOY_DIR%  port %IWORKERCLOUD_PORT%
echo.

echo [2/8] Building and syncing web assets...
pushd "%ROOT_DIR%iWorkerCenter\frontend"
call "%NPM_EXE%" run build
if errorlevel 1 (
  popd
  echo [ERROR] Failed to build iWorkerCenter frontend.
  goto :fail
)
popd
if not exist "%ROOT_DIR%iWorkerCenter\frontend\dist\index.html" (
  echo [ERROR] Missing iWorkerCenter frontend dist/index.html
  goto :fail
)
if exist "%ROOT_DIR%iWorkerCenter\cmd\iworkercenter\web\admin" rmdir /s /q "%ROOT_DIR%iWorkerCenter\cmd\iworkercenter\web\admin"
mkdir "%ROOT_DIR%iWorkerCenter\cmd\iworkercenter\web\admin" >nul 2>nul
robocopy "%ROOT_DIR%iWorkerCenter\frontend\dist" "%ROOT_DIR%iWorkerCenter\cmd\iworkercenter\web\admin" /E /NFL /NDL /NJH /NJS /NP >nul
if errorlevel 8 (
  echo [ERROR] Failed to sync iWorkerCenter web assets.
  goto :fail
)
rmdir /s /q "%ROOT_DIR%iWorkerCenter\frontend\dist"
pushd "%ROOT_DIR%iWorkerCloud\web\admin"
call "%NPM_EXE%" run build
if errorlevel 1 (
  popd
  echo [ERROR] Failed to build iWorkerCloud admin frontend.
  goto :fail
)
popd

if not exist "%ROOT_DIR%iWorkerCloud\web\admin\dist\index.html" (
  echo [ERROR] Missing iWorkerCloud web/admin/dist after build.
  goto :fail
)

echo [3/8] Building Linux binaries locally...
if exist "%BUILD_ROOT%" rmdir /s /q "%BUILD_ROOT%"
mkdir "%STAGE_ROOT%\bin" >nul 2>nul

set "OLD_GOOS=%GOOS%"
set "OLD_GOARCH=%GOARCH%"
set "OLD_CGO_ENABLED=%CGO_ENABLED%"
set "GOOS=linux"
set "GOARCH=amd64"
set "CGO_ENABLED=0"
"%GO_EXE%" build -o "%STAGE_ROOT%\bin\iworkercenter" ./iWorkerCenter/cmd/iworkercenter
if errorlevel 1 goto :build_fail
"%GO_EXE%" build -o "%STAGE_ROOT%\bin\iworkercloud" ./iWorkerCloud
if errorlevel 1 goto :build_fail
set "GOOS=%OLD_GOOS%"
set "GOARCH=%OLD_GOARCH%"
set "CGO_ENABLED=%OLD_CGO_ENABLED%"

echo [4/8] Staging web assets...
"%POWERSHELL%" -NoProfile -ExecutionPolicy Bypass -Command ^
  "$src='%ROOT_DIR_TRIM%';$dst='%STAGE_ROOT%';" ^
  "New-Item -ItemType Directory -Force (Join-Path $dst 'iWorkerCenter') | Out-Null;" ^
  "New-Item -ItemType Directory -Force (Join-Path $dst 'iWorkerCloud/web/admin') | Out-Null;" ^
  "Copy-Item (Join-Path $src 'iWorkerCenter/cmd/iworkercenter/web') (Join-Path $dst 'iWorkerCenter/web') -Recurse -Force;" ^
  "Copy-Item (Join-Path $src 'iWorkerCloud/web/admin/dist') (Join-Path $dst 'iWorkerCloud/web/admin/dist') -Recurse -Force;"
if errorlevel 1 (
  echo [ERROR] Failed to stage web assets.
  goto :fail
)

echo [5/8] Creating archive...
"%TAR_EXE%" -czf "%ARCHIVE_PATH%" -C "%STAGE_ROOT%" .
if errorlevel 1 (
  echo [ERROR] Failed to create archive.
  goto :fail
)

echo [6/8] Writing remote script...
call :write_remote_script
if errorlevel 1 goto :fail

echo [7/8] Uploading...
"%PLINK_EXE%" -batch %HOSTKEY_ARG% -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_USER%@%REMOTE_HOST%" "mkdir -p %REMOTE_TMP_DIR%"
if errorlevel 1 goto :upload_fail
"%PSCP_EXE%" -batch %HOSTKEY_ARG% -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%ARCHIVE_PATH%" "%REMOTE_USER%@%REMOTE_HOST%:%REMOTE_TMP_DIR%/iworker-services-deploy.tar.gz"
if errorlevel 1 goto :upload_fail
"%PSCP_EXE%" -batch %HOSTKEY_ARG% -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_SCRIPT%" "%REMOTE_USER%@%REMOTE_HOST%:%REMOTE_TMP_DIR%/remote_deploy_iworker_services.sh"
if errorlevel 1 goto :upload_fail

echo [8/8] Remote deploy and restart...
"%PLINK_EXE%" -batch %HOSTKEY_ARG% -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_USER%@%REMOTE_HOST%" "sed -i 's/\r$//' %REMOTE_TMP_DIR%/remote_deploy_iworker_services.sh && chmod +x %REMOTE_TMP_DIR%/remote_deploy_iworker_services.sh && REMOTE_TMP_DIR=%REMOTE_TMP_DIR% IWORKERCENTER_DEPLOY_DIR=%IWORKERCENTER_DEPLOY_DIR% IWORKERCLOUD_DEPLOY_DIR=%IWORKERCLOUD_DEPLOY_DIR% IWORKERCENTER_PORT=%IWORKERCENTER_PORT% IWORKERCLOUD_PORT=%IWORKERCLOUD_PORT% IWORKERCLOUD_PUBLIC_BASE_URL=%IWORKERCLOUD_PUBLIC_BASE_URL% %REMOTE_TMP_DIR%/remote_deploy_iworker_services.sh"
if errorlevel 1 (
  echo [ERROR] Remote deployment failed.
  goto :fail
)

echo.
echo Deployment completed on %REMOTE_HOST%
echo   iWorkerCenter: %IWORKERCENTER_DEPLOY_DIR%
echo   iWorkerCloud : %IWORKERCLOUD_DEPLOY_DIR%
call :exit_with 0
exit /b 0

:build_fail
set "GOOS=%OLD_GOOS%"
set "GOARCH=%OLD_GOARCH%"
set "CGO_ENABLED=%OLD_CGO_ENABLED%"
echo [ERROR] Local Linux build failed.
goto :fail

:upload_fail
echo [ERROR] Upload failed.
goto :fail

:fail
call :exit_with 1
exit /b 1

:prompt_password
if defined REMOTE_PASS exit /b 0
if not exist "%PROMPT_SCRIPT%" (
  echo [ERROR] Missing %PROMPT_SCRIPT%
  exit /b 1
)
echo Enter SSH password for %REMOTE_USER%@%REMOTE_HOST%:
del /q "%PASSWORD_FILE%" >nul 2>nul
"%POWERSHELL%" -NoProfile -ExecutionPolicy Bypass -File "%PROMPT_SCRIPT%" -Prompt "Password" -OutputPath "%PASSWORD_FILE%"
if exist "%PASSWORD_FILE%" (
  set /p REMOTE_PASS=<"%PASSWORD_FILE%"
  del /q "%PASSWORD_FILE%" >nul 2>nul
)
if not defined REMOTE_PASS (
  echo [ERROR] Empty password.
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
  echo [ERROR] Tool not found: %~2
  exit /b 1
)
exit /b 0

:write_remote_script
setlocal DisableDelayedExpansion
(
  echo #!/bin/sh
  echo set -eu
  echo : "${REMOTE_TMP_DIR:=/tmp/iworker_services_deploy}"
  echo : "${IWORKERCENTER_DEPLOY_DIR:=/data/soft/iworkercenter}"
  echo : "${IWORKERCLOUD_DEPLOY_DIR:=/data/soft/iworkercloud}"
  echo : "${IWORKERCENTER_PORT:=9377}"
  echo : "${IWORKERCLOUD_PORT:=9366}"
  echo : "${IWORKERCLOUD_PUBLIC_BASE_URL:=https://www.driverdevelop.com}"
  echo.
  echo SRC="$REMOTE_TMP_DIR/src"
  echo rm -rf "$SRC"
  echo mkdir -p "$SRC"
  echo tar -xzf "$REMOTE_TMP_DIR/iworker-services-deploy.tar.gz" -C "$SRC"
  echo.
  echo echo "[remote] Deploying iWorkerCenter..."
  echo mkdir -p "$IWORKERCENTER_DEPLOY_DIR" "$IWORKERCENTER_DEPLOY_DIR/data"
  echo cp -f "$SRC/bin/iworkercenter" "$IWORKERCENTER_DEPLOY_DIR/iworkercenter"
  echo chmod +x "$IWORKERCENTER_DEPLOY_DIR/iworkercenter"
  echo rm -rf "$IWORKERCENTER_DEPLOY_DIR/web"
  echo cp -R "$SRC/iWorkerCenter/web" "$IWORKERCENTER_DEPLOY_DIR/web"
  echo cat ^> "$IWORKERCENTER_DEPLOY_DIR/start.sh" ^<^< 'CENTEREOF'
  echo #!/bin/sh
  echo cd "$(dirname "$0")"
  echo pkill -f "iworkercenter" 2^>/dev/null ^|^| true
  echo sleep 1
  echo nohup ./iworkercenter -addr :${IWORKERCENTER_PORT:-9377} ^> data/iworkercenter.log 2^>^&1 ^&
  echo echo "iWorkerCenter started on :${IWORKERCENTER_PORT:-9377} (PID: $!)"
  echo CENTEREOF
  echo chmod +x "$IWORKERCENTER_DEPLOY_DIR/start.sh"
  echo.
  echo echo "[remote] Deploying iWorkerCloud..."
  echo mkdir -p "$IWORKERCLOUD_DEPLOY_DIR/bin" "$IWORKERCLOUD_DEPLOY_DIR/data" "$IWORKERCLOUD_DEPLOY_DIR/logs"
  echo cp -f "$SRC/bin/iworkercloud" "$IWORKERCLOUD_DEPLOY_DIR/bin/iworkercloud"
  echo chmod +x "$IWORKERCLOUD_DEPLOY_DIR/bin/iworkercloud"
  echo rm -rf "$IWORKERCLOUD_DEPLOY_DIR/web"
  echo mkdir -p "$IWORKERCLOUD_DEPLOY_DIR/web/admin"
  echo cp -R "$SRC/iWorkerCloud/web/admin/dist" "$IWORKERCLOUD_DEPLOY_DIR/web/admin/dist"
  echo if [ ! -f "$IWORKERCLOUD_DEPLOY_DIR/config.yaml" ]; then
  echo   cat ^> "$IWORKERCLOUD_DEPLOY_DIR/config.yaml" ^<^< CONFIGEOF
  echo server:
  echo   host: 0.0.0.0
  echo   port: $IWORKERCLOUD_PORT
  echo   public_base_url: $IWORKERCLOUD_PUBLIC_BASE_URL
  echo database:
  echo   dsn: $IWORKERCLOUD_DEPLOY_DIR/data/iworkercloud.db
  echo mail:
  echo   smtp_host: ""
  echo   smtp_port: 0
  echo   username: ""
  echo   password: ""
  echo   from: ""
  echo CONFIGEOF
  echo fi
  echo cat ^> "$IWORKERCLOUD_DEPLOY_DIR/start.sh" ^<^< 'CLOUDEOF'
  echo #!/bin/sh
  echo cd "$(dirname "$0")"
  echo pkill -f "iworkercloud" 2^>/dev/null ^|^| true
  echo sleep 1
  echo nohup ./bin/iworkercloud -config ./config.yaml ^> ./logs/iworkercloud.log 2^>^&1 ^&
  echo echo "iWorkerCloud started (PID: $!)"
  echo CLOUDEOF
  echo chmod +x "$IWORKERCLOUD_DEPLOY_DIR/start.sh"
  echo.
  echo echo "[remote] Restarting services..."
  echo cd "$IWORKERCENTER_DEPLOY_DIR" ^&^& IWORKERCENTER_PORT="$IWORKERCENTER_PORT" ./start.sh
  echo cd "$IWORKERCLOUD_DEPLOY_DIR" ^&^& ./start.sh
  echo.
  echo rm -rf "$SRC" "$REMOTE_TMP_DIR/iworker-services-deploy.tar.gz" "$REMOTE_TMP_DIR/remote_deploy_iworker_services.sh"
  echo echo "iWorkerCenter deployed to $IWORKERCENTER_DEPLOY_DIR"
  echo echo "iWorkerCloud deployed to $IWORKERCLOUD_DEPLOY_DIR"
) > "%REMOTE_SCRIPT%"
if errorlevel 1 (
  endlocal
  echo [ERROR] Failed to write remote script.
  exit /b 1
)
endlocal
exit /b 0

