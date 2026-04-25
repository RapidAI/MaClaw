@echo off
setlocal enabledelayedexpansion

set "ROOT=%~dp0"
set "DIST_DIR=%ROOT%dist"
set "OUTPUT=%DIST_DIR%\iWorker.exe"
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

echo [1/6] Install frontend dependencies...
call npm install --prefix frontend --cache frontend/.npm_cache
if errorlevel 1 goto :fail

echo [2/6] Regenerate Wails bindings...
call wails build -platform windows/amd64 -debug -nopackage
if errorlevel 1 goto :fail

echo [3/6] Run frontend tests...
call npm test --prefix frontend
if errorlevel 1 goto :fail

echo [4/6] Build frontend...
call npm run build --prefix frontend
if errorlevel 1 goto :fail

echo [5/6] Build iWorker release for Windows...
call wails build -platform windows/amd64
if errorlevel 1 goto :fail

echo [6/6] Copy artifact to dist...
copy /y "build\bin\iWorker.exe" "%OUTPUT%" >nul
if errorlevel 1 goto :fail

echo [OK] All done.
echo [OK] Output: %OUTPUT%
popd
exit /b 0

:fail
echo [ERROR] Build pipeline failed.
popd
exit /b 1

