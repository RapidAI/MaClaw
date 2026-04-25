@echo off
setlocal EnableExtensions

set "ROOT_DIR=%~dp0"
set "POWERSHELL=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
set "SCRIPT_PATH=%ROOT_DIR%deploy\deploy_all_ha.ps1"
set "PS_ARGS="

if not exist "%SCRIPT_PATH%" (
  echo [ERROR] Missing deployment script: %SCRIPT_PATH%
  exit /b 1
)

:parse_args
if "%~1"=="" goto run
if /I "%~1"=="-h" goto usage
if /I "%~1"=="--help" goto usage
if /I "%~1"=="/h" goto usage
if /I "%~1"=="/?" goto usage
if /I "%~1"=="hubcenter-only" (
  set "PS_ARGS=%PS_ARGS% -Scope hubcenter-only"
  shift
  goto parse_args
)
if /I "%~1"=="full" (
  set "PS_ARGS=%PS_ARGS% -Scope full"
  shift
  goto parse_args
)
if /I "%~1"=="--no-check" (
  set "PS_ARGS=%PS_ARGS% -NoCheck"
  shift
  goto parse_args
)
if /I "%~1"=="-NoCheck" (
  set "PS_ARGS=%PS_ARGS% -NoCheck"
  shift
  goto parse_args
)
if /I "%~1"=="--brand" (
  if "%~2"=="" (
    echo [ERROR] Missing value for --brand
    exit /b 1
  )
  set "PS_ARGS=%PS_ARGS% -Brand %~2"
  shift
  shift
  goto parse_args
)
echo [ERROR] Unknown argument: %~1
exit /b 1

:run
set "PAUSE_ON_EXIT="
if not defined REMOTE_PASS (
  set "PAUSE_ON_EXIT=1"
)

"%POWERSHELL%" -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT_PATH%" %PS_ARGS%
set "EXIT_CODE=%ERRORLEVEL%"

if defined PAUSE_ON_EXIT (
  echo.
  pause
)

exit /b %EXIT_CODE%

:usage
echo Usage:
echo   deploy_all.cmd [full^|hubcenter-only] [--no-check] [--brand rapidai^|tigerclaw]
exit /b 0
