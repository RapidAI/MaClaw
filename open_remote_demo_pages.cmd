@echo off
setlocal

set ROOT_DIR=%~dp0
set HUBCENTER_URL=http://127.0.0.1:9388
set HUB_URL=http://127.0.0.1:9399
set DEMO_EMAIL=admin@local.maclaw
set ENCODED_EMAIL=admin%%40local.maclaw
set PROGRESS_FILE=%ROOT_DIR%.last_remote_demo.json

if not "%~1"=="" set HUBCENTER_URL=%~1
if not "%~2"=="" set HUB_URL=%~2
if not "%~3"=="" set DEMO_EMAIL=%~3

if "%~3"=="" (
  for /f "usebackq delims=" %%E in (`powershell -NoProfile -ExecutionPolicy Bypass -Command "$path = '%PROGRESS_FILE%'; try { if (Test-Path $path) { $json = Get-Content -Raw -Path $path | ConvertFrom-Json; if ($json.activation.email) { Write-Output $json.activation.email } } } catch {}"`) do (
    set "DEMO_EMAIL=%%E"
  )
)

for /f "usebackq delims=" %%U in (`powershell -NoProfile -ExecutionPolicy Bypass -Command "[uri]::EscapeDataString('%DEMO_EMAIL%')"`) do (
  set "ENCODED_EMAIL=%%U"
)
set "PWA_URL=%HUB_URL%/app?email=%ENCODED_EMAIL%^&entry=app^&autologin=1"

echo Opening MaClaw remote demo pages...
echo   Hub admin:    %HUB_URL%/admin
echo   Hub PWA:      %PWA_URL%
echo   Center admin: %HUBCENTER_URL%/admin

start "" "%HUB_URL%/admin"
start "" "%PWA_URL%"
start "" "%HUBCENTER_URL%/admin"

endlocal
