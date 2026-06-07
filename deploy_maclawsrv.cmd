@echo off
setlocal EnableExtensions EnableDelayedExpansion
REM =========================================================================
REM  deploy_maclawsrv.cmd - Deploy MaClawSrv service
REM
REM  Builds a Linux/amd64 maclawsrv binary locally, uploads it to
REM  the selected remote host, installs it under /data/soft/maclaw_srv, and
REM  restarts the remote service. The SSH password is requested at runtime
REM  unless REMOTE_PASS is already set.
REM =========================================================================

set "ROOT_DIR=%~dp0"
set "ROOT_DIR_TRIM=%ROOT_DIR:~0,-1%"
set "POWERSHELL=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
set "PROMPT_SCRIPT=%ROOT_DIR%prompt_password.ps1"

set "DEPLOY_TARGET=driverdevelop"
if /I "%~1"=="hub" (
  set "DEPLOY_TARGET=hub"
  shift
)
if /I "%~1"=="hub.maclaw.top" (
  set "DEPLOY_TARGET=hub"
  shift
)
if /I "%~1"=="mypapers" (
  set "DEPLOY_TARGET=mypapers"
  shift
)
if /I "%~1"=="maclawsrv.mypapers.top" (
  set "DEPLOY_TARGET=mypapers"
  shift
)
if /I "%~1"=="maclaw" (
  set "DEPLOY_TARGET=maclaw"
  shift
)
if /I "%~1"=="maclawsrv.maclaw.top" (
  set "DEPLOY_TARGET=maclaw"
  shift
)
if not defined REMOTE_HOST (
  if /I "%DEPLOY_TARGET%"=="mypapers" (
    set "REMOTE_HOST=maclawsrv.mypapers.top"
  ) else if /I "%DEPLOY_TARGET%"=="maclaw" (
    set "REMOTE_HOST=maclawsrv.maclaw.top"
  ) else if /I "%DEPLOY_TARGET%"=="hub" (
    set "REMOTE_HOST=hub.maclaw.top"
  ) else (
    set "REMOTE_HOST=www.driverdevelop.com"
  )
)
if not defined REMOTE_PORT set "REMOTE_PORT=22"
if not defined REMOTE_USER set "REMOTE_USER=root"
if not defined REMOTE_HOSTKEY (
  if /I "%REMOTE_HOST%"=="maclawsrv.maclaw.top" (
    set "REMOTE_HOSTKEY=ssh-ed25519 255 SHA256:yoyEXbuT2kezyG9Y8cJDZplBMZgaPAN7+sureAkVRVE"
  ) else if /I "%REMOTE_HOST%"=="hub.maclaw.top" (
    set "REMOTE_HOSTKEY=ssh-ed25519 255 SHA256:yoyEXbuT2kezyG9Y8cJDZplBMZgaPAN7+sureAkVRVE"
  ) else (
    set "REMOTE_HOSTKEY=ssh-ed25519 255 SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog"
  )
)
if not defined REMOTE_TMP_DIR set "REMOTE_TMP_DIR=/tmp/maclawsrv_deploy"
if not defined MACLAWSRV_DEPLOY_DIR set "MACLAWSRV_DEPLOY_DIR=/data/soft/maclaw_srv"
if not defined MACLAWSRV_PORT set "MACLAWSRV_PORT=18080"
if not defined MACLAWSRV_BIND_ADDR set "MACLAWSRV_BIND_ADDR=:18080"
if not defined MACLAW_ADMIN_WEB_DEFAULT_LOCALE set "MACLAW_ADMIN_WEB_DEFAULT_LOCALE=zh-CN"
if not defined MACLAW_ENABLE_SCHEDULER set "MACLAW_ENABLE_SCHEDULER=true"
if not defined MACLAW_ALLOW_INSECURE_HTTP set "MACLAW_ALLOW_INSECURE_HTTP=true"
if not defined GOPROXY set "GOPROXY=https://goproxy.cn,direct"
if not defined REMOTE_PASS set "PAUSE_ON_EXIT=1"

set "BUILD_ROOT=%ROOT_DIR%build\maclawsrv_deploy"
set "STAGE_ROOT=%BUILD_ROOT%\stage"
set "ARCHIVE_PATH=%BUILD_ROOT%\maclawsrv-deploy.tar.gz"
set "REMOTE_SCRIPT=%BUILD_ROOT%\remote_deploy_maclawsrv.sh"
set "PASSWORD_FILE=%TEMP%\deploy_maclawsrv_password_%RANDOM%_%RANDOM%.txt"

if /I "%~1"=="-h" goto :usage
if /I "%~1"=="--help" goto :usage
if /I "%~1"=="/h" goto :usage
if /I "%~1"=="/?" goto :usage
if not "%~1"=="" (
  echo [ERROR] Unknown argument: %~1
  goto :usage_error
)

goto :main

:usage
echo Usage:
echo   deploy_maclawsrv.cmd
echo   deploy_maclawsrv.cmd hub
echo   deploy_maclawsrv.cmd mypapers
echo   deploy_maclawsrv.cmd maclaw
echo.
echo Optional environment overrides:
echo   REMOTE_HOST=www.driverdevelop.com
echo   REMOTE_HOST=hub.maclaw.top
echo   REMOTE_HOST=maclawsrv.mypapers.top
echo   REMOTE_HOST=maclawsrv.maclaw.top
echo   REMOTE_USER=root
echo   REMOTE_PORT=22
echo   REMOTE_PASS=...
echo   MACLAWSRV_DEPLOY_DIR=/data/soft/maclaw_srv
echo   MACLAWSRV_BIND_ADDR=:18080
echo   MACLAW_ALLOW_INSECURE_HTTP=true
exit /b 0

:usage_error
exit /b 1

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
if not exist "%ROOT_DIR%MaClawSrv\main.go" (
  echo [ERROR] Missing MaClawSrv source
  goto :fail
)
if not exist "%ROOT_DIR%MaClawSrv\admin_web\index.html" (
  echo [ERROR] Missing MaClawSrv admin web assets
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

call :prompt_password
if errorlevel 1 goto :fail

set "HOSTKEY_ARG="
if defined REMOTE_HOSTKEY set HOSTKEY_ARG=-hostkey "%REMOTE_HOSTKEY%"

echo.
echo [1/6] Connection info
echo        Host: %REMOTE_USER%@%REMOTE_HOST%:%REMOTE_PORT%
echo        MaClawSrv -^> %MACLAWSRV_DEPLOY_DIR%
echo        Listen    -^> %MACLAWSRV_BIND_ADDR%
echo.

echo [2/6] Building Linux binary locally...
if exist "%BUILD_ROOT%" rmdir /s /q "%BUILD_ROOT%"
mkdir "%STAGE_ROOT%\bin" >nul 2>nul

set "VERSION=dev"
if exist "%ROOT_DIR%build_number" set /p VERSION=<"%ROOT_DIR%build_number"
set "COMMIT="
for /f "delims=" %%I in ('git -C "%ROOT_DIR_TRIM%" rev-parse --short HEAD 2^>nul') do set "COMMIT=%%I"
for /f "delims=" %%I in ('%POWERSHELL% -NoProfile -Command "Get-Date -Format yyyy-MM-ddTHH:mm:ssK"') do set "BUILT_AT=%%I"

set "OLD_GOOS=%GOOS%"
set "OLD_GOARCH=%GOARCH%"
set "OLD_CGO_ENABLED=%CGO_ENABLED%"
set "GOOS=linux"
set "GOARCH=amd64"
set "CGO_ENABLED=0"
"%GO_EXE%" build -ldflags "-s -w -X main.serviceVersion=%VERSION% -X main.serviceCommit=%COMMIT% -X main.serviceBuiltAt=%BUILT_AT%" -o "%STAGE_ROOT%\bin\maclawsrv" ./MaClawSrv
if errorlevel 1 goto :build_fail
set "GOOS=%OLD_GOOS%"
set "GOARCH=%OLD_GOARCH%"
set "CGO_ENABLED=%OLD_CGO_ENABLED%"

echo [3/6] Creating archive...
"%TAR_EXE%" -czf "%ARCHIVE_PATH%" -C "%STAGE_ROOT%" .
if errorlevel 1 (
  echo [ERROR] Failed to create archive.
  goto :fail
)

echo [4/6] Writing remote script...
call :write_remote_script
if errorlevel 1 goto :fail

echo [5/6] Uploading...
"%PLINK_EXE%" -batch %HOSTKEY_ARG% -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_USER%@%REMOTE_HOST%" "mkdir -p %REMOTE_TMP_DIR%"
if errorlevel 1 goto :upload_fail
"%PSCP_EXE%" -batch %HOSTKEY_ARG% -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%ARCHIVE_PATH%" "%REMOTE_USER%@%REMOTE_HOST%:%REMOTE_TMP_DIR%/maclawsrv-deploy.tar.gz"
if errorlevel 1 goto :upload_fail
"%PSCP_EXE%" -batch %HOSTKEY_ARG% -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_SCRIPT%" "%REMOTE_USER%@%REMOTE_HOST%:%REMOTE_TMP_DIR%/remote_deploy_maclawsrv.sh"
if errorlevel 1 goto :upload_fail

echo [6/6] Remote deploy and restart...
"%PLINK_EXE%" -batch %HOSTKEY_ARG% -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_USER%@%REMOTE_HOST%" "sed -i 's/\r$//' %REMOTE_TMP_DIR%/remote_deploy_maclawsrv.sh && chmod +x %REMOTE_TMP_DIR%/remote_deploy_maclawsrv.sh && REMOTE_TMP_DIR=%REMOTE_TMP_DIR% MACLAWSRV_DEPLOY_DIR=%MACLAWSRV_DEPLOY_DIR% MACLAWSRV_BIND_ADDR=%MACLAWSRV_BIND_ADDR% MACLAW_ADMIN_WEB_DEFAULT_LOCALE=%MACLAW_ADMIN_WEB_DEFAULT_LOCALE% MACLAW_ENABLE_SCHEDULER=%MACLAW_ENABLE_SCHEDULER% MACLAW_ALLOW_INSECURE_HTTP=%MACLAW_ALLOW_INSECURE_HTTP% %REMOTE_TMP_DIR%/remote_deploy_maclawsrv.sh"
if errorlevel 1 (
  echo [ERROR] Remote deployment failed.
  goto :fail
)

echo.
echo Deployment completed on %REMOTE_HOST%
echo   MaClawSrv: %MACLAWSRV_DEPLOY_DIR%
echo   URL      : http://%REMOTE_HOST%:%MACLAWSRV_PORT%/admin/
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
  echo : "${REMOTE_TMP_DIR:=/tmp/maclawsrv_deploy}"
  echo : "${MACLAWSRV_DEPLOY_DIR:=/data/soft/maclaw_srv}"
  echo : "${MACLAWSRV_BIND_ADDR:=:18080}"
  echo : "${MACLAW_ADMIN_WEB_DEFAULT_LOCALE:=zh-CN}"
  echo : "${MACLAW_ENABLE_SCHEDULER:=true}"
  echo : "${MACLAW_ALLOW_INSECURE_HTTP:=true}"
  echo.
  echo rand_secret^(^) {
  echo   if command -v openssl ^> /dev/null 2^>^&1; then
  echo     openssl rand -base64 48 ^| tr -d '\n'
  echo   else
  echo     dd if=/dev/urandom bs=48 count=1 2^>/dev/null ^| base64 ^| tr -d '\n'
  echo   fi
  echo }
  echo.
  echo SRC="$REMOTE_TMP_DIR/src"
  echo ARCHIVE_PATH="$REMOTE_TMP_DIR/maclawsrv-deploy.tar.gz"
  echo rm -rf "$SRC"
  echo mkdir -p "$SRC" "$MACLAWSRV_DEPLOY_DIR/bin" "$MACLAWSRV_DEPLOY_DIR/data" "$MACLAWSRV_DEPLOY_DIR/logs"
  echo tar -xzf "$ARCHIVE_PATH" -C "$SRC"
  echo cp -f "$SRC/bin/maclawsrv" "$MACLAWSRV_DEPLOY_DIR/bin/maclawsrv"
  echo chmod +x "$MACLAWSRV_DEPLOY_DIR/bin/maclawsrv"
  echo.
  echo if [ ! -f "$MACLAWSRV_DEPLOY_DIR/.env" ]; then
  echo   ADMIN_SECRET="$(rand_secret)"
  echo   TOKEN_SECRET="$(rand_secret)"
  echo   cat ^> "$MACLAWSRV_DEPLOY_DIR/.env" ^<^< ENVEOF
  echo MACLAW_DATA_ROOT=$MACLAWSRV_DEPLOY_DIR/data
  echo MACLAW_HTTP_ADDR=$MACLAWSRV_BIND_ADDR
  echo MACLAW_ALLOW_INSECURE_HTTP=$MACLAW_ALLOW_INSECURE_HTTP
  echo MACLAW_ADMIN_SECRET=$ADMIN_SECRET
  echo MACLAW_TOKEN_SECRET=$TOKEN_SECRET
  echo MACLAW_ADMIN_WEB_DEFAULT_LOCALE=$MACLAW_ADMIN_WEB_DEFAULT_LOCALE
  echo MACLAW_ENABLE_SCHEDULER=$MACLAW_ENABLE_SCHEDULER
  echo ENVEOF
  echo   chmod 600 "$MACLAWSRV_DEPLOY_DIR/.env"
  echo   echo "[remote] Created $MACLAWSRV_DEPLOY_DIR/.env with generated secrets."
  echo else
  echo   grep -q '^MACLAW_DATA_ROOT=' "$MACLAWSRV_DEPLOY_DIR/.env" ^|^| echo "MACLAW_DATA_ROOT=$MACLAWSRV_DEPLOY_DIR/data" ^>^> "$MACLAWSRV_DEPLOY_DIR/.env"
  echo   grep -q '^MACLAW_HTTP_ADDR=' "$MACLAWSRV_DEPLOY_DIR/.env" ^|^| echo "MACLAW_HTTP_ADDR=$MACLAWSRV_BIND_ADDR" ^>^> "$MACLAWSRV_DEPLOY_DIR/.env"
  echo   grep -q '^MACLAW_ALLOW_INSECURE_HTTP=' "$MACLAWSRV_DEPLOY_DIR/.env" ^|^| echo "MACLAW_ALLOW_INSECURE_HTTP=$MACLAW_ALLOW_INSECURE_HTTP" ^>^> "$MACLAWSRV_DEPLOY_DIR/.env"
  echo   grep -q '^MACLAW_ADMIN_WEB_DEFAULT_LOCALE=' "$MACLAWSRV_DEPLOY_DIR/.env" ^|^| echo "MACLAW_ADMIN_WEB_DEFAULT_LOCALE=$MACLAW_ADMIN_WEB_DEFAULT_LOCALE" ^>^> "$MACLAWSRV_DEPLOY_DIR/.env"
  echo   grep -q '^MACLAW_ENABLE_SCHEDULER=' "$MACLAWSRV_DEPLOY_DIR/.env" ^|^| echo "MACLAW_ENABLE_SCHEDULER=$MACLAW_ENABLE_SCHEDULER" ^>^> "$MACLAWSRV_DEPLOY_DIR/.env"
  echo fi
  echo.
  echo cat ^> "$MACLAWSRV_DEPLOY_DIR/start.sh" ^<^< 'STARTEOF'
  echo #!/bin/sh
  echo set -eu
  echo cd "$(dirname "$0")"
  echo set -a
  echo . ./.env
  echo set +a
  echo pkill -f "bin/maclawsrv" 2^>/dev/null ^|^| true
  echo sleep 1
  echo nohup ./bin/maclawsrv ^> ./logs/maclawsrv.log 2^>^&1 ^&
  echo echo "MaClawSrv started (PID: $!)"
  echo STARTEOF
  echo chmod +x "$MACLAWSRV_DEPLOY_DIR/start.sh"
  echo.
  echo if command -v systemctl ^> /dev/null 2^>^&1; then
  echo   cat ^> /etc/systemd/system/maclawsrv.service ^<^< SERVICEEOF
  echo [Unit]
  echo Description=MaClawSrv REST service
  echo After=network.target
  echo.
  echo [Service]
  echo Type=simple
  echo WorkingDirectory=$MACLAWSRV_DEPLOY_DIR
  echo EnvironmentFile=$MACLAWSRV_DEPLOY_DIR/.env
  echo ExecStart=$MACLAWSRV_DEPLOY_DIR/bin/maclawsrv
  echo Restart=always
  echo RestartSec=3
  echo StandardOutput=append:$MACLAWSRV_DEPLOY_DIR/logs/maclawsrv.log
  echo StandardError=append:$MACLAWSRV_DEPLOY_DIR/logs/maclawsrv.err.log
  echo.
  echo [Install]
  echo WantedBy=multi-user.target
  echo SERVICEEOF
  echo   systemctl daemon-reload
  echo   systemctl enable maclawsrv.service ^> /dev/null 2^>^&1 ^|^| true
  echo   systemctl restart maclawsrv.service
  echo   systemctl --no-pager --full status maclawsrv.service ^| head -n 20 ^|^| true
  echo else
  echo   cd "$MACLAWSRV_DEPLOY_DIR" ^&^& ./start.sh
  echo fi
  echo.
  echo rm -rf "$SRC" "$ARCHIVE_PATH" "$REMOTE_TMP_DIR/remote_deploy_maclawsrv.sh"
  echo echo "MaClawSrv deployed to $MACLAWSRV_DEPLOY_DIR"
) > "%REMOTE_SCRIPT%"
if errorlevel 1 (
  endlocal
  echo [ERROR] Failed to write remote script.
  exit /b 1
)
endlocal
exit /b 0
