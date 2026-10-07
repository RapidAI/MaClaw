@echo off
setlocal EnableExtensions EnableDelayedExpansion
REM =========================================================================
REM  deploy_desktopd.cmd - Deploy the Docker desktop service
REM
REM  Builds a Linux/amd64 desktopd binary locally, uploads it to the Docker
REM  host, installs it under /data/soft/maclaw_desktopd, and restarts the
REM  remote service. The SSH password is requested at runtime unless
REM  REMOTE_PASS is already set.
REM =========================================================================

set "ROOT_DIR=%~dp0"
set "POWERSHELL=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
set "PROMPT_SCRIPT=%ROOT_DIR%prompt_password.ps1"

set "DEPLOY_TARGET=mypapers"
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
if /I "%~1"=="-h" goto :usage
if /I "%~1"=="--help" goto :usage
if /I "%~1"=="/h" goto :usage
if /I "%~1"=="/?" goto :usage
if not "%~1"=="" (
  if not defined REMOTE_HOST set "REMOTE_HOST=%~1"
  shift
)
if not "%~1"=="" (
  echo [ERROR] Unknown argument: %~1
  goto :usage_error
)

if not defined REMOTE_HOST (
  if /I "%DEPLOY_TARGET%"=="maclaw" (
    set "REMOTE_HOST=maclawsrv.maclaw.top"
  ) else (
    set "REMOTE_HOST=maclawsrv.mypapers.top"
  )
)
if not defined REMOTE_PORT set "REMOTE_PORT=22"
if not defined REMOTE_USER set "REMOTE_USER=root"
if not defined REMOTE_HOSTKEY (
  if /I "%REMOTE_HOST%"=="maclawsrv.maclaw.top" (
    set "REMOTE_HOSTKEY=ssh-ed25519 255 SHA256:yoyEXbuT2kezyG9Y8cJDZplBMZgaPAN7+sureAkVRVE"
  ) else if /I "%REMOTE_HOST%"=="maclawsrv.mypapers.top" (
    set "REMOTE_HOSTKEY=ssh-ed25519 255 SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog"
  )
)
if not defined REMOTE_TMP_DIR set "REMOTE_TMP_DIR=/tmp/maclaw_desktopd_deploy"
if not defined DESKTOPD_DEPLOY_DIR set "DESKTOPD_DEPLOY_DIR=/data/soft/maclaw_desktopd"
if not defined DESKTOPD_PORT set "DESKTOPD_PORT=18081"
if not defined DESKTOPD_BIND_ADDR set "DESKTOPD_BIND_ADDR=127.0.0.1:%DESKTOPD_PORT%"
if not defined DESKTOPD_ADVERTISE_HOST set "DESKTOPD_ADVERTISE_HOST=%REMOTE_HOST%"
if not defined DESKTOPD_IMAGE set "DESKTOPD_IMAGE=maclaw-gui:2"
REM maclaw-gui:2 is built on the Docker host from desktopd\image\Dockerfile.v2.
REM Empty base image / apt mirror lets remote_deploy.sh pick the Tencent Cloud
REM mirrors when it runs on Tencent Cloud, and Docker Hub otherwise.
if not defined DESKTOPD_BASE_IMAGE set "DESKTOPD_BASE_IMAGE="
if not defined DESKTOPD_APT_MIRROR set "DESKTOPD_APT_MIRROR="
if not defined DESKTOPD_SKIP_IMAGE_BUILD set "DESKTOPD_SKIP_IMAGE_BUILD=0"
if not defined GOPROXY set "GOPROXY=https://goproxy.cn,direct"
if not defined REMOTE_PASS set "PAUSE_ON_EXIT=1"

set "BUILD_ROOT=%ROOT_DIR%build\desktopd_deploy"
set "STAGE_ROOT=%BUILD_ROOT%\stage"
set "ARCHIVE_PATH=%BUILD_ROOT%\desktopd-deploy.tar.gz"
set "REMOTE_SCRIPT=%ROOT_DIR%desktopd\remote_deploy.sh"
set "PASSWORD_FILE=%TEMP%\deploy_desktopd_password_%RANDOM%_%RANDOM%.txt"

goto :main

:usage
echo Usage:
echo   deploy_desktopd.cmd
echo   deploy_desktopd.cmd mypapers
echo   deploy_desktopd.cmd maclaw
echo   deploy_desktopd.cmd docker-host.example
echo.
echo Optional environment overrides:
echo   REMOTE_HOST=maclawsrv.mypapers.top
echo   REMOTE_USER=root
echo   REMOTE_PORT=22
echo   REMOTE_PASS=...
echo   REMOTE_HOSTKEY=ssh-ed25519 255 SHA256:...
echo   DESKTOPD_DEPLOY_DIR=/data/soft/maclaw_desktopd
echo   DESKTOPD_BIND_ADDR=127.0.0.1:18081
echo   DESKTOPD_ADVERTISE_HOST=maclawsrv.mypapers.top
echo   DESKTOPD_IMAGE=maclaw-gui:2
echo   DESKTOPD_BASE_IMAGE=mirror.ccs.tencentyun.com/library/debian:bookworm
echo   DESKTOPD_APT_MIRROR=mirrors.tencentyun.com   (none = deb.debian.org)
echo   DESKTOPD_SKIP_IMAGE_BUILD=1                  (reuse the existing image)
echo.
echo The access token is generated on the Docker host the first time and kept
echo in DESKTOPD_DEPLOY_DIR\.env. It is not printed.
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
if not exist "%ROOT_DIR%desktopd\cmd\desktopd\main.go" (
  echo [ERROR] Missing desktopd source
  goto :fail
)
if not exist "%ROOT_DIR%desktopd\image\desktop_supervisor.py" (
  echo [ERROR] Missing desktop supervisor script
  goto :fail
)
for %%F in (Dockerfile Dockerfile.v2 close_range_shim.c fcitx5-profile) do (
  if not exist "%ROOT_DIR%desktopd\image\%%F" (
    echo [ERROR] Missing desktopd\image\%%F
    goto :fail
  )
)
if not exist "%REMOTE_SCRIPT%" (
  echo [ERROR] Missing %REMOTE_SCRIPT%
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
echo [1/5] Connection info
echo        Host: %REMOTE_USER%@%REMOTE_HOST%:%REMOTE_PORT%
echo        desktopd -^> %DESKTOPD_DEPLOY_DIR%
echo        Listen   -^> %DESKTOPD_BIND_ADDR%
echo        Advertise-^> %DESKTOPD_ADVERTISE_HOST%
echo        Image    -^> %DESKTOPD_IMAGE% ^(built on the host from Dockerfile.v2^)
echo.

echo [2/5] Building Linux binary locally...
cd /d "%ROOT_DIR%"
if exist "%BUILD_ROOT%" rmdir /s /q "%BUILD_ROOT%"
mkdir "%STAGE_ROOT%\bin" >nul 2>nul
mkdir "%STAGE_ROOT%\image" >nul 2>nul
copy /y "%ROOT_DIR%desktopd\image\desktop_supervisor.py" "%STAGE_ROOT%\image\desktop_supervisor.py" >nul
if errorlevel 1 (
  echo [ERROR] Failed to stage desktop_supervisor.py
  goto :fail
)
for %%F in (Dockerfile Dockerfile.v2 close_range_shim.c fcitx5-profile) do (
  copy /y "%ROOT_DIR%desktopd\image\%%F" "%STAGE_ROOT%\image\%%F" >nul
  if errorlevel 1 (
    echo [ERROR] Failed to stage desktopd\image\%%F
    goto :fail
  )
)

set "OLD_GOOS=%GOOS%"
set "OLD_GOARCH=%GOARCH%"
set "OLD_CGO_ENABLED=%CGO_ENABLED%"
set "GOOS=linux"
set "GOARCH=amd64"
set "CGO_ENABLED=0"
"%GO_EXE%" build -ldflags "-s -w" -o "%STAGE_ROOT%\bin\desktopd" ./desktopd/cmd/desktopd
if errorlevel 1 goto :build_fail
set "GOOS=%OLD_GOOS%"
set "GOARCH=%OLD_GOARCH%"
set "CGO_ENABLED=%OLD_CGO_ENABLED%"

echo [3/5] Creating archive...
"%TAR_EXE%" -czf "%ARCHIVE_PATH%" -C "%STAGE_ROOT%" .
if errorlevel 1 (
  echo [ERROR] Failed to create archive.
  goto :fail
)

echo [4/5] Uploading...
"%PLINK_EXE%" -batch %HOSTKEY_ARG% -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_USER%@%REMOTE_HOST%" "mkdir -p %REMOTE_TMP_DIR%"
if errorlevel 1 goto :upload_fail
"%PSCP_EXE%" -batch %HOSTKEY_ARG% -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%ARCHIVE_PATH%" "%REMOTE_USER%@%REMOTE_HOST%:%REMOTE_TMP_DIR%/desktopd-deploy.tar.gz"
if errorlevel 1 goto :upload_fail
"%PSCP_EXE%" -batch %HOSTKEY_ARG% -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_SCRIPT%" "%REMOTE_USER%@%REMOTE_HOST%:%REMOTE_TMP_DIR%/remote_deploy.sh"
if errorlevel 1 goto :upload_fail

echo [5/5] Remote deploy and restart...
"%PLINK_EXE%" -batch %HOSTKEY_ARG% -P %REMOTE_PORT% -pw "%REMOTE_PASS%" "%REMOTE_USER%@%REMOTE_HOST%" "sed -i 's/\r$//' %REMOTE_TMP_DIR%/remote_deploy.sh && chmod +x %REMOTE_TMP_DIR%/remote_deploy.sh && REMOTE_TMP_DIR=%REMOTE_TMP_DIR% DESKTOPD_DEPLOY_DIR=%DESKTOPD_DEPLOY_DIR% DESKTOPD_BIND_ADDR=%DESKTOPD_BIND_ADDR% DESKTOPD_PORT=%DESKTOPD_PORT% DESKTOPD_ADVERTISE_HOST=%DESKTOPD_ADVERTISE_HOST% DESKTOPD_IMAGE=%DESKTOPD_IMAGE% DESKTOPD_BASE_IMAGE=%DESKTOPD_BASE_IMAGE% DESKTOPD_APT_MIRROR=%DESKTOPD_APT_MIRROR% DESKTOPD_SKIP_IMAGE_BUILD=%DESKTOPD_SKIP_IMAGE_BUILD% %REMOTE_TMP_DIR%/remote_deploy.sh"
if errorlevel 1 (
  echo [ERROR] Remote deployment failed.
  goto :fail
)

echo.
echo Deployment completed on %REMOTE_HOST%
echo   desktopd : %DESKTOPD_DEPLOY_DIR%
echo   Health   : http://%REMOTE_HOST%:%DESKTOPD_PORT%/v1/health
echo   Token    : %DESKTOPD_DEPLOY_DIR%/.env on the Docker host
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
