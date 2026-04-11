@echo off
setlocal EnableExtensions EnableDelayedExpansion
REM =========================================================================
REM  deploy_iworkercenter.cmd — Build and deploy iWorkerCenter service
REM
REM  Similar to deploy_all.cmd but for the iWorkerCenter service.
REM  Uploads source to remote Linux host, builds there, deploys binary + web.
REM =========================================================================

set "ROOT_DIR=%~dp0"
set "ROOT_DIR_TRIM=%ROOT_DIR:~0,-1%"
set "POWERSHELL=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
set "PROMPT_SCRIPT=%ROOT_DIR%prompt_password.ps1"
set "REMOTE_HOST=hubs.mypapers.top"
set "REMOTE_PORT=22"
set "REMOTE_USER=root"
set "REMOTE_HOSTKEY=ssh-ed25519 255 SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog"
set "REMOTE_DEPLOY_DIR=/data/soft/iworkercenter"
set "REMOTE_TMP_DIR=/tmp/iwc_deploy"
if not defined REMOTE_PASS (
  set "PAUSE_ON_EXIT=1"
)
set "CGO_ENABLED=0"

set "BUILD_ROOT=%ROOT_DIR%build\iwc_deploy"
set "STAGE_ROOT=%BUILD_ROOT%\stage"
set "ARCHIVE_PATH=%BUILD_ROOT%\iwc-src.tar.gz"
set "REMOTE_SCRIPT=%BUILD_ROOT%\remote_deploy_iwc.sh"
set "PASSWORD_FILE=%TEMP%\deploy_iwc_password_%RANDOM%_%RANDOM%.txt"

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

call :resolve_tool PLINK_EXE plink.exe
if errorlevel 1 goto :fail
call :resolve_tool PSCP_EXE pscp.exe
if errorlevel 1 goto :fail
call :resolve_tool TAR_EXE tar.exe
if errorlevel 1 goto :fail

call :prompt_password
if errorlevel 1 goto :fail

echo.
echo [1/5] Connection info
echo        Host: %REMOTE_USER%@%REMOTE_HOST%:%REMOTE_PORT%
echo        Deploy -^> %REMOTE_DEPLOY_DIR%
echo.

echo [2/5] Staging source...
if exist "%BUILD_ROOT%" rmdir /s /q "%BUILD_ROOT%"
mkdir "%STAGE_ROOT%" >nul 2>nul

"%POWERSHELL%" -NoProfile -ExecutionPolicy Bypass -Command ^
  "$src='%ROOT_DIR_TRIM%';$dst='%STAGE_ROOT%';" ^
  "Copy-Item (Join-Path $src 'go.mod') $dst -Force;" ^
  "Copy-Item (Join-Path $src 'go.sum') $dst -Force;" ^
  "Copy-Item (Join-Path $src 'corelib') (Join-Path $dst 'corelib') -Recurse -Force;" ^
  "Copy-Item (Join-Path $src 'iWorkerCenter') (Join-Path $dst 'iWorkerCenter') -Recurse -Force;"
if errorlevel 1 (
  echo [ERROR] Failed to stage source.
  goto :fail
)

echo [2/5] Creating archive...
"%TAR_EXE%" -czf "%ARCHIVE_PATH%" -C "%STAGE_ROOT%" .
if errorlevel 1 (
  echo [ERROR] Failed to create archive.
  goto :fail
)

echo [3/5] Writing remote script...
call :write_remote_script
if errorlevel 1 goto :fail

echo [4/5] Uploading...
"%PLINK_EXE%" -batch -hostkey "%REMOTE_HOSTKEY%" -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_USER%@%REMOTE_HOST%" "mkdir -p %REMOTE_TMP_DIR%"
"%PSCP_EXE%" -batch -hostkey "%REMOTE_HOSTKEY%" -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%ARCHIVE_PATH%" "%REMOTE_USER%@%REMOTE_HOST%:%REMOTE_TMP_DIR%/iwc-src.tar.gz"
"%PSCP_EXE%" -batch -hostkey "%REMOTE_HOSTKEY%" -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_SCRIPT%" "%REMOTE_USER%@%REMOTE_HOST%:%REMOTE_TMP_DIR%/remote_deploy_iwc.sh"
if errorlevel 1 (
  echo [ERROR] Upload failed.
  goto :fail
)

echo [5/5] Remote build and deploy...
"%PLINK_EXE%" -batch -hostkey "%REMOTE_HOSTKEY%" -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_USER%@%REMOTE_HOST%" "sed -i 's/\r$//' %REMOTE_TMP_DIR%/remote_deploy_iwc.sh && chmod +x %REMOTE_TMP_DIR%/remote_deploy_iwc.sh && REMOTE_DEPLOY_DIR=%REMOTE_DEPLOY_DIR% REMOTE_TMP_DIR=%REMOTE_TMP_DIR% %REMOTE_TMP_DIR%/remote_deploy_iwc.sh"
if errorlevel 1 (
  echo [ERROR] Remote deployment failed.
  goto :fail
)

echo.
echo Deployment completed: %REMOTE_HOST%:%REMOTE_DEPLOY_DIR%
call :exit_with 0
goto :eof

:fail
call :exit_with 1
goto :eof

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
  echo : "${REMOTE_DEPLOY_DIR:=/data/soft/iworkercenter}"
  echo : "${REMOTE_TMP_DIR:=/tmp/iwc_deploy}"
  echo : "${GOPROXY:=https://goproxy.cn,direct}"
  echo.
  echo if ! command -v go ^>/dev/null 2^>^&1; then
  echo   echo "[ERROR] go not installed" ^>^&2; exit 1
  echo fi
  echo.
  echo SRC="$REMOTE_TMP_DIR/src"
  echo rm -rf "$SRC"
  echo mkdir -p "$SRC"
  echo tar -xzf "$REMOTE_TMP_DIR/iwc-src.tar.gz" -C "$SRC"
  echo cd "$SRC"
  echo.
  echo echo "[remote] Downloading deps..."
  echo GOPROXY="$GOPROXY" go mod download
  echo.
  echo echo "[remote] Building iworkercenter..."
  echo CGO_ENABLED=0 GOPROXY="$GOPROXY" go build -o "$REMOTE_TMP_DIR/iworkercenter" ./iWorkerCenter/cmd/iworkercenter
  echo.
  echo echo "[remote] Deploying..."
  echo mkdir -p "$REMOTE_DEPLOY_DIR" "$REMOTE_DEPLOY_DIR/data"
  echo cp -f "$REMOTE_TMP_DIR/iworkercenter" "$REMOTE_DEPLOY_DIR/iworkercenter"
  echo chmod +x "$REMOTE_DEPLOY_DIR/iworkercenter"
  echo.
  echo # Deploy web assets
  echo if [ -d "$SRC/iWorkerCenter/cmd/iworkercenter/web" ]; then
  echo   rm -rf "$REMOTE_DEPLOY_DIR/web"
  echo   cp -R "$SRC/iWorkerCenter/cmd/iworkercenter/web" "$REMOTE_DEPLOY_DIR/web"
  echo fi
  echo.
  echo # Write start script
  echo cat ^> "$REMOTE_DEPLOY_DIR/start.sh" ^<^< 'STARTEOF'
  echo #!/bin/sh
  echo cd "$(dirname "$0")"
  echo pkill -f "iworkercenter" 2^>/dev/null ^|^| true
  echo sleep 1
  echo nohup ./iworkercenter -addr :9377 ^> data/iworkercenter.log 2^>^&1 ^&
  echo echo "iWorkerCenter started on :9377 (PID: $!)"
  echo STARTEOF
  echo chmod +x "$REMOTE_DEPLOY_DIR/start.sh"
  echo.
  echo echo "[remote] Restarting service..."
  echo cd "$REMOTE_DEPLOY_DIR" ^&^& ./start.sh
  echo.
  echo rm -rf "$SRC" "$REMOTE_TMP_DIR/iworkercenter" "$REMOTE_TMP_DIR/iwc-src.tar.gz" "$REMOTE_TMP_DIR/remote_deploy_iwc.sh"
  echo echo "iWorkerCenter deployed to $REMOTE_DEPLOY_DIR"
) > "%REMOTE_SCRIPT%"
if errorlevel 1 (
  endlocal
  echo [ERROR] Failed to write remote script.
  exit /b 1
)
endlocal
exit /b 0
