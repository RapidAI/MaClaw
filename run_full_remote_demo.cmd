@echo off
setlocal

set ROOT_DIR=%~dp0
set PROJECT_DIR=%ROOT_DIR:~0,-1%
set HUBCENTER_URL=http://127.0.0.1:9388
set HUB_URL=http://127.0.0.1:9399
set EMAIL=admin@local.maclaw
set HOLD_SECONDS=60
set PROGRESS_FILE=%ROOT_DIR%.last_remote_demo.json
set EXTRA_ARGS=
set AUTO_OPEN=0
set VERIFY_INPUT=
set VERIFY_MODE=

if not "%~1"=="" set EMAIL=%~1
if not "%~2"=="" set PROJECT_DIR=%~2
if not "%~3"=="" set HOLD_SECONDS=%~3
if not "%~4"=="" set PROGRESS_FILE=%~4
if not "%~5"=="" set EXTRA_ARGS=%~5
if /I "%~6"=="auto-open" set AUTO_OPEN=1
if not "%~7"=="" set VERIFY_INPUT=%~7
if /I "%~8"=="interrupt" set VERIFY_MODE=interrupt
if /I "%~8"=="kill" set VERIFY_MODE=kill

echo.
echo Running full MaClaw remote demo...
echo Email:        %EMAIL%
echo Project:      %PROJECT_DIR%
echo Hub Center:   %HUBCENTER_URL%
echo Hub:          %HUB_URL%
echo Hold Seconds: %HOLD_SECONDS%
echo Progress:     %PROGRESS_FILE%
if "%AUTO_OPEN%"=="1" (
echo Auto Open:    enabled
) else (
echo Auto Open:    disabled
)
if not "%VERIFY_INPUT%"=="" echo Verify Input:  %VERIFY_INPUT%
if not "%VERIFY_MODE%"=="" echo Verify Mode:   %VERIFY_MODE%
echo.

call "%ROOT_DIR%setup_remote_stack.cmd" %HUBCENTER_URL% %HUB_URL%
if errorlevel 1 (
  echo.
  echo [FAIL] setup_remote_stack.cmd failed.
  exit /b 1
)

call "%ROOT_DIR%run_remote_smoke.cmd" -- -email %EMAIL% -hub-url %HUB_URL% -center-url %HUBCENTER_URL% -activate -project "%PROJECT_DIR%" -pty-probe -launch-probe -start -verify-hub -hold-seconds %HOLD_SECONDS% -progress-file "%PROGRESS_FILE%" %EXTRA_ARGS%
if errorlevel 1 (
  echo.
  echo [FAIL] run_remote_smoke.cmd failed.
  call :print_progress_summary "%PROGRESS_FILE%"
  exit /b 1
)

echo.
echo Verifying viewer login and control path...
call "%ROOT_DIR%verify_remote_controls.cmd" %HUB_URL% "%PROGRESS_FILE%" "%VERIFY_INPUT%" %VERIFY_MODE%
if errorlevel 1 (
  echo.
  echo [FAIL] verify_remote_controls.cmd failed.
  call :print_progress_summary "%PROGRESS_FILE%"
  exit /b 1
)

echo.
echo [OK] Full MaClaw remote demo completed.
echo.
echo Inspect:
echo   Hub admin:    %HUB_URL%/admin
for /f "usebackq delims=" %%U in (`powershell -NoProfile -ExecutionPolicy Bypass -Command "$encoded = [uri]::EscapeDataString('%EMAIL%'); Write-Output ('%HUB_URL%/app?email=' + $encoded + '&entry=app&autologin=1')"` ) do (
  echo   Hub PWA:      %%U
)
echo   Center admin: %HUBCENTER_URL%/admin
echo   Progress JSON: %PROGRESS_FILE%
echo.
call :print_progress_summary "%PROGRESS_FILE%"
echo.
echo Quick Checks:
echo   [1] Open Hub admin and confirm the machine is online while the held session is active.
echo   [2] Open Hub PWA and confirm the Claude session is visible before the hold window expires.
echo   [3] Confirm the session status and summary look reasonable.
echo   [4] Viewer login and control APIs have already been verified.
echo   [5] Optionally try Send / Interrupt / Kill again from the PWA control area.
echo.
echo Optional:
echo   Run open_remote_demo_pages.cmd to open the main local pages.
if "%AUTO_OPEN%"=="1" (
  echo.
  echo Opening local demo pages...
  call "%ROOT_DIR%open_remote_demo_pages.cmd" %HUBCENTER_URL% %HUB_URL% %EMAIL%
)

endlocal
exit /b 0

:print_progress_summary
set "PROGRESS_PATH=%~1"
echo Summary:
if not exist "%PROGRESS_PATH%" (
  echo   Progress file not found: %PROGRESS_PATH%
  goto :eof
)

powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$path = '%PROGRESS_PATH%';" ^
  "$json = Get-Content -Raw -Path $path | ConvertFrom-Json;" ^
  "$success = [bool]$json.success;" ^
  "$phase = [string]$json.phase;" ^
  "$updated = [string]$json.last_updated;" ^
  "$next = [string]$json.recommended_next;" ^
  "$sessionId = '';" ^
  "if ($json.started_session) { $sessionId = [string]$json.started_session.id }" ^
  "$hubVerified = $false;" ^
  "if ($json.hub_visibility) { $hubVerified = [bool]$json.hub_visibility.verified }" ^
  "Write-Host ('  Success:           ' + $success);" ^
  "Write-Host ('  Phase:             ' + $phase);" ^
  "Write-Host ('  Last Updated:      ' + $updated);" ^
  "if ($sessionId -ne '') { Write-Host ('  Started Session:   ' + $sessionId) }" ^
  "Write-Host ('  Hub Visible:       ' + $hubVerified);" ^
  "if ($next -ne '') { Write-Host ('  Recommended Next:  ' + $next) }"
if errorlevel 1 (
  echo   Failed to parse progress file: %PROGRESS_PATH%
)
goto :eof
