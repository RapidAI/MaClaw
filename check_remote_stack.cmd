@echo off
setlocal

powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$urls = @('http://127.0.0.1:9388/healthz','http://127.0.0.1:9399/healthz');" ^
  "$failed = $false;" ^
  "foreach ($url in $urls) {" ^
  "  try {" ^
  "    $resp = Invoke-WebRequest -UseBasicParsing -Uri $url -TimeoutSec 5;" ^
  "    Write-Host ('[OK] ' + $url + ' -> ' + $resp.StatusCode);" ^
  "  } catch {" ^
  "    Write-Host ('[FAIL] ' + $url + ' -> ' + $_.Exception.Message);" ^
  "    $failed = $true;" ^
  "  }" ^
  "}" ^
  "if ($failed) { exit 1 }"

if errorlevel 1 (
  echo.
  echo One or more MaClaw remote services are not healthy yet.
  exit /b 1
)

echo.
echo All MaClaw remote services are healthy.
echo Hub Center admin: http://127.0.0.1:9388/admin
echo Hub admin:        http://127.0.0.1:9399/admin
echo Hub PWA:          http://127.0.0.1:9399/app
