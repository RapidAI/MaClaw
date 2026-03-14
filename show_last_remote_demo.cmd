@echo off
setlocal

set ROOT_DIR=%~dp0
set PROGRESS_FILE=%ROOT_DIR%.last_remote_demo.json
set HAS_PWA_URL=

if not "%~1"=="" set PROGRESS_FILE=%~1

echo.
echo MaClaw Remote Demo Status
echo Progress file: %PROGRESS_FILE%
echo.

if not exist "%PROGRESS_FILE%" (
  echo [FAIL] Progress file not found.
  exit /b 1
)

powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$path = '%PROGRESS_FILE%';" ^
  "$json = Get-Content -Raw -Path $path | ConvertFrom-Json;" ^
  "Write-Host ('Success:          ' + [string][bool]$json.success);" ^
  "Write-Host ('Phase:            ' + [string]$json.phase);" ^
  "Write-Host ('Last Updated:     ' + [string]$json.last_updated);" ^
  "if ($json.activation) {" ^
  "  Write-Host ('Activation:       ' + [string]$json.activation.status);" ^
  "  Write-Host ('Activation Email: ' + [string]$json.activation.email);" ^
  "  if ($json.activation.machine_id) { Write-Host ('Machine ID:        ' + [string]$json.activation.machine_id) }" ^
  "}" ^
  "if ($json.pty_probe) { Write-Host ('ConPTY Ready:      ' + [string][bool]$json.pty_probe.ready) }" ^
  "if ($json.launch_probe) { Write-Host ('Launch Ready:      ' + [string][bool]$json.launch_probe.ready) }" ^
  "if ($json.started_session) {" ^
  "  Write-Host ('Session ID:       ' + [string]$json.started_session.id);" ^
  "  Write-Host ('Session Status:   ' + [string]$json.started_session.status);" ^
  "}" ^
  "if ($json.hub_visibility) { Write-Host ('Hub Visible:       ' + [string][bool]$json.hub_visibility.verified) }" ^
  "if ($json.recommended_next) {" ^
  "  Write-Host '';" ^
  "  Write-Host ('Recommended Next: ' + [string]$json.recommended_next);" ^
  "}" ^
  "if ($json.activation -and $json.activation.email) {" ^
  "  $encoded = [uri]::EscapeDataString([string]$json.activation.email);" ^
  "  Write-Host ('PWA Autologin:     http://127.0.0.1:9399/app?email=' + $encoded + '&entry=app&autologin=1');" ^
  "}"

if errorlevel 1 (
  echo [FAIL] Could not parse progress file.
  exit /b 1
)

echo.
echo Tip:
echo   Hub admin:    http://127.0.0.1:9399/admin
echo   Center admin: http://127.0.0.1:9388/admin
for /f "usebackq delims=" %%E in (`powershell -NoProfile -ExecutionPolicy Bypass -Command "$path = '%PROGRESS_FILE%'; try { $json = Get-Content -Raw -Path $path | ConvertFrom-Json; if ($json.activation.email) { $encoded = [uri]::EscapeDataString([string]$json.activation.email); Write-Output ('http://127.0.0.1:9399/app?email=' + $encoded + '&entry=app&autologin=1') } } catch {}"`) do (
  echo   Hub PWA:      %%E
  set HAS_PWA_URL=1
)
if not defined HAS_PWA_URL (
  echo   Hub PWA:      http://127.0.0.1:9399/app
)

endlocal
