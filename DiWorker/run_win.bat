@echo off
setlocal enabledelayedexpansion

set "ROOT=%~dp0"
set "DIST_DIR=%ROOT%dist"
set "EXE=%DIST_DIR%\DiWorker-debug.exe"
pushd "%ROOT%" || exit /b 1

call "%ROOT%build_win.bat"
if errorlevel 1 goto :fail

if not exist "%EXE%" (
  echo [ERROR] Built executable not found: %EXE%
  goto :fail
)

echo [OK] Launching %EXE%
start "DiWorker" "%EXE%"
popd
exit /b 0

:fail
echo [ERROR] Run failed.
popd
exit /b 1
