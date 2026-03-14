@echo off
setlocal

set "SCRIPT_DIR=%~dp0"
set "POWERSHELL=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"

if /I "%~1"=="build" (
    "%POWERSHELL%" -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT_DIR%scripts\build.ps1"
) else (
    "%POWERSHELL%" -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT_DIR%scripts\package.ps1"
)

if errorlevel 1 (
    echo.
    echo MaClaw Hub Center build failed.
    exit /b %errorlevel%
)

echo.
if /I "%~1"=="build" (
    echo MaClaw Hub Center build completed.
) else (
    echo MaClaw Hub Center package completed.
)

endlocal
