@echo off
setlocal EnableDelayedExpansion

set ROOT_DIR=%~dp0
set PROGRESS_FILE=%ROOT_DIR%.last_remote_demo.json
set DEMO_EMAIL=

if not "%~1"=="" set PROGRESS_FILE=%~1

echo.
echo MaClaw Remote Stack Status
echo ============================
echo Root:          %ROOT_DIR%
echo Progress File: %PROGRESS_FILE%
echo.

call "%ROOT_DIR%check_remote_stack.cmd"
set HEALTH_EXIT=%ERRORLEVEL%

echo.
if exist "%PROGRESS_FILE%" (
  call "%ROOT_DIR%show_last_remote_demo.cmd" "%PROGRESS_FILE%"
  set DEMO_EXIT=%ERRORLEVEL%
  for /f "usebackq delims=" %%E in (`powershell -NoProfile -ExecutionPolicy Bypass -Command "$path = '%PROGRESS_FILE%'; if (Test-Path $path) { try { $json = Get-Content -Raw -Path $path | ConvertFrom-Json; if ($json.activation.email) { Write-Output $json.activation.email } } catch {} }"`) do set "DEMO_EMAIL=%%E"
) else (
  echo Remote demo progress file not found yet.
  echo Tip: run run_full_remote_demo.cmd or reset_and_run_full_remote_demo.cmd first.
  set DEMO_EXIT=1
)

echo.
echo Quick Links
echo   Hub Center admin: http://127.0.0.1:9388/admin
echo   Hub admin:        http://127.0.0.1:9399/admin
if defined DEMO_EMAIL (
  powershell -NoProfile -ExecutionPolicy Bypass -Command ^
    "$encoded = [uri]::EscapeDataString('%DEMO_EMAIL%');" ^
    "Write-Host ('  Hub PWA:          http://127.0.0.1:9399/app?email=' + $encoded + '&entry=app&autologin=1')"
) else (
  echo   Hub PWA:          http://127.0.0.1:9399/app
)
echo.

if %HEALTH_EXIT% NEQ 0 (
  echo Recommended:
  echo   1. Run run_remote_stack.cmd
  echo   2. Wait a few seconds
  echo   3. Run setup_remote_stack.cmd
  echo   4. Run run_full_remote_demo.cmd
  exit /b 1
)

if %DEMO_EXIT% NEQ 0 (
  echo Recommended:
  echo   1. Run setup_remote_stack.cmd
  echo   2. Run run_full_remote_demo.cmd
  exit /b 1
)

echo Remote stack is healthy and the latest demo result is available.
exit /b 0
