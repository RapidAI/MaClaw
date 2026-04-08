@echo off
setlocal enabledelayedexpansion

set "ROOT=%~dp0"
set "DIST_DIR=%ROOT%dist"
set "OUTPUT=%DIST_DIR%\DiWorker-debug.exe"
pushd "%ROOT%" || exit /b 1

where npm >nul 2>nul
if errorlevel 1 (
  echo [ERROR] npm not found in PATH.
  popd
  exit /b 1
)

where wails >nul 2>nul
if errorlevel 1 (
  echo [ERROR] wails not found in PATH.
  popd
  exit /b 1
)

if not exist "%DIST_DIR%" mkdir "%DIST_DIR%"

echo [1/4] Install frontend dependencies...
call npm install --prefix frontend --cache frontend/.npm_cache
if errorlevel 1 goto :fail

echo [2/4] Build frontend...
call npm run build --prefix frontend
if errorlevel 1 goto :fail

echo [3/4] Build DiWorker debug for Windows...
call wails build -platform windows/amd64 -debug -nopackage
if errorlevel 1 goto :fail

echo [4/4] Copy artifact to dist...
copy /y "build\bin\DiWorker.exe" "%OUTPUT%" >nul
if errorlevel 1 goto :fail

echo [OK] Build complete: %OUTPUT%
popd
exit /b 0

:fail
echo [ERROR] Build failed.
popd
exit /b 1
