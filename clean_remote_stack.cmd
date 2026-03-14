@echo off
setlocal

set ROOT_DIR=%~dp0
set HUB_DIR=%ROOT_DIR%hub
set HUBCENTER_DIR=%ROOT_DIR%hubcenter

echo.
echo Cleaning local MaClaw remote stack state...
echo.

call "%ROOT_DIR%stop_remote_stack.cmd" >nul
call :wait_ports 9388 9399

echo Removing Hub data files...
if exist "%HUB_DIR%\data\maclaw-hub.db" del /f /q "%HUB_DIR%\data\maclaw-hub.db" >nul 2>nul
if exist "%HUB_DIR%\data\maclaw-hub.db-shm" del /f /q "%HUB_DIR%\data\maclaw-hub.db-shm" >nul 2>nul
if exist "%HUB_DIR%\data\maclaw-hub.db-wal" del /f /q "%HUB_DIR%\data\maclaw-hub.db-wal" >nul 2>nul

echo Removing Hub Center data files...
if exist "%HUBCENTER_DIR%\data\maclaw-hubcenter.db" del /f /q "%HUBCENTER_DIR%\data\maclaw-hubcenter.db" >nul 2>nul
if exist "%HUBCENTER_DIR%\data\maclaw-hubcenter.db-shm" del /f /q "%HUBCENTER_DIR%\data\maclaw-hubcenter.db-shm" >nul 2>nul
if exist "%HUBCENTER_DIR%\data\maclaw-hubcenter.db-wal" del /f /q "%HUBCENTER_DIR%\data\maclaw-hubcenter.db-wal" >nul 2>nul

echo Removing local log files...
if exist "%HUB_DIR%\data\logs" rmdir /s /q "%HUB_DIR%\data\logs" >nul 2>nul
if exist "%HUBCENTER_DIR%\data\logs" rmdir /s /q "%HUBCENTER_DIR%\data\logs" >nul 2>nul

echo Removing local demo and smoke progress files...
if exist "%ROOT_DIR%.last_remote_demo.json" del /f /q "%ROOT_DIR%.last_remote_demo.json" >nul 2>nul
if exist "%ROOT_DIR%.last_remote_smoke_live.json" del /f /q "%ROOT_DIR%.last_remote_smoke_live.json" >nul 2>nul

echo.
echo [OK] Local MaClaw remote stack state cleaned.
echo.
echo Next:
echo   1. run_remote_stack.cmd
echo   2. setup_remote_stack.cmd
echo   3. run_full_remote_demo_auto_open.cmd
echo.

endlocal
exit /b 0

:wait_ports
set "PORTS=%*"
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$ports = '%PORTS%'.Split(' ', [System.StringSplitOptions]::RemoveEmptyEntries) | ForEach-Object { [int]$_ };" ^
  "$deadline = (Get-Date).AddSeconds(15);" ^
  "while ((Get-Date) -lt $deadline) {" ^
  "  $busy = @();" ^
  "  foreach ($port in $ports) {" ^
  "    if (Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue) { $busy += $port }" ^
  "  }" ^
  "  if ($busy.Count -eq 0) { exit 0 }" ^
  "  Start-Sleep -Milliseconds 500;" ^
  "}" ^
  "Write-Host ('[WARN] Ports still listening after wait: ' + (($ports | Where-Object { Get-NetTCPConnection -State Listen -LocalPort $_ -ErrorAction SilentlyContinue }) -join ', '));"
goto :eof
