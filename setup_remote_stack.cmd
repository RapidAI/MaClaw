@echo off
setlocal

set "HUBCENTER_URL=http://127.0.0.1:9388"
set "HUB_URL=http://127.0.0.1:9399"

if not "%~1"=="" set "HUBCENTER_URL=%~1"
if not "%~2"=="" set "HUB_URL=%~2"

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0setup_remote_stack.ps1" ^
  -HubCenterUrl "%HUBCENTER_URL%" ^
  -HubUrl "%HUB_URL%" ^
  -HubCenterAdminUser "%~3" ^
  -HubCenterAdminPass "%~4" ^
  -HubCenterAdminEmail "%~5" ^
  -HubAdminUser "%~6" ^
  -HubAdminPass "%~7" ^
  -HubAdminEmail "%~8"

if errorlevel 1 (
  echo.
  echo [FAIL] Remote stack initialization failed.
  echo.
  echo Tips:
  echo   0. Services will be auto-started if needed, then health-checked on:
  echo      %HUBCENTER_URL%/healthz
  echo      %HUB_URL%/healthz
  echo   1. If either admin has already been initialized with a different password, rerun with overrides.
  echo   2. Example:
  echo      set HUBCENTER_ADMIN_PASS=YourExistingPassword
  echo      set HUB_ADMIN_PASS=YourExistingPassword
  echo      setup_remote_stack.cmd
  echo   3. Or pass explicit arguments:
  echo      setup_remote_stack.cmd %HUBCENTER_URL% %HUB_URL% admin YourPass admin@local.maclaw admin YourPass admin@local.maclaw
  exit /b 1
)

echo.
echo MaClaw remote stack initialization completed.
endlocal
exit /b 0
