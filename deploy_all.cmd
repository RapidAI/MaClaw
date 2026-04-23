@echo off
setlocal EnableExtensions

set "ROOT_DIR=%~dp0"
set "POWERSHELL=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
set "SCRIPT_PATH=%ROOT_DIR%deploy\deploy_all_ha.ps1"

if not exist "%SCRIPT_PATH%" (
  echo [ERROR] Missing deployment script: %SCRIPT_PATH%
  exit /b 1
)

set "PAUSE_ON_EXIT="
if not defined REMOTE_PASS (
  set "PAUSE_ON_EXIT=1"
)

"%POWERSHELL%" -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT_PATH%"
set "EXIT_CODE=%ERRORLEVEL%"

if defined PAUSE_ON_EXIT (
  echo.
  pause
)

exit /b %EXIT_CODE%
