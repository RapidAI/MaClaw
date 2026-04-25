@echo off
setlocal enabledelayedexpansion

set "ROOT=%~dp0"
set "PARENT=%ROOT%.."
set "DIST_DIR=%ROOT%dist"
set "OUTPUT=%DIST_DIR%\iWorker.exe"
set "BIN_DIR=%ROOT%build\bin"

where npm >nul 2>nul
if errorlevel 1 (
  echo [ERROR] npm not found in PATH.
  exit /b 1
)

where go >nul 2>nul
if errorlevel 1 (
  echo [ERROR] go not found in PATH.
  exit /b 1
)

if not exist "%DIST_DIR%" mkdir "%DIST_DIR%"
if not exist "%BIN_DIR%" mkdir "%BIN_DIR%"

echo [1/4] Install frontend dependencies...
pushd "%ROOT%frontend" || exit /b 1
call npm install --cache .npm_cache
if errorlevel 1 ( popd & goto :fail )
popd

echo [2/4] Build frontend...
pushd "%ROOT%frontend" || exit /b 1
call npm run build
if errorlevel 1 ( popd & goto :fail )
popd

echo [3/4] Build iWorker release for Windows...
pushd "%PARENT%" || exit /b 1
go build -ldflags="-s -w -H windowsgui" -o "%BIN_DIR%\iWorker.exe" ./iWorker/
if errorlevel 1 ( popd & goto :fail )
popd

echo [4/4] Copy artifact to dist...
copy /y "%BIN_DIR%\iWorker.exe" "%OUTPUT%" >nul
if errorlevel 1 goto :fail

echo [OK] Release build complete.
echo [OK] Output: %OUTPUT%
exit /b 0

:fail
echo [ERROR] Release build failed.
exit /b 1

