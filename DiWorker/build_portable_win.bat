@echo off
setlocal enabledelayedexpansion

set "ROOT=%~dp0"
set "DIST_DIR=%ROOT%dist"
set "PORTABLE_DIR=%DIST_DIR%\portable-win"
pushd "%ROOT%" || exit /b 1

call "%ROOT%build_release_win.bat"
if errorlevel 1 goto :fail

if not exist "%PORTABLE_DIR%" mkdir "%PORTABLE_DIR%"
copy /y "%DIST_DIR%\DiWorker.exe" "%PORTABLE_DIR%\DiWorker.exe" >nul
if errorlevel 1 goto :fail

echo [OK] Portable package ready: %PORTABLE_DIR%
popd
exit /b 0

:fail
echo [ERROR] Portable export failed.
popd
exit /b 1
