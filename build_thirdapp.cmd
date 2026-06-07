@echo off
setlocal EnableExtensions EnableDelayedExpansion

REM =========================================================================
REM  build_thirdapp.cmd - Build ThirdAPIDemo Wails desktop app
REM =========================================================================

set "ROOT=%~dp0"
set "APP_DIR=%ROOT%ThirdAPIDemo"
set "APP_NAME=ThirdAPIDemo"
set "APP_EXE=%APP_DIR%\build\bin\%APP_NAME%.exe"
set "OUTPUT_DIR=%ROOT%dist"
set "OUTPUT_EXE=%OUTPUT_DIR%\%APP_NAME%.exe"

echo [INFO] Building %APP_NAME%...

if not exist "%APP_DIR%\wails.json" (
  echo [ERROR] Missing %APP_DIR%\wails.json
  goto :error
)

where go >nul 2>nul
if !errorlevel! neq 0 (
  echo [ERROR] Go was not found in PATH.
  goto :error
)

where wails >nul 2>nul
if !errorlevel! neq 0 (
  echo [ERROR] Wails CLI was not found in PATH.
  echo [HINT] Install with: go install github.com/wailsapp/wails/v2/cmd/wails@latest
  goto :error
)

if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"

echo [Step 1/3] Cleaning previous build...
if exist "%APP_DIR%\build\bin" rmdir /s /q "%APP_DIR%\build\bin"
if exist "%OUTPUT_EXE%" del /q "%OUTPUT_EXE%"

echo [Step 2/3] Running Wails build...
pushd "%APP_DIR%"
wails build -clean
if !errorlevel! neq 0 (
  popd
  echo [ERROR] Wails build failed.
  goto :error
)
popd

if not exist "%APP_EXE%" (
  echo [ERROR] Expected build output was not found: %APP_EXE%
  goto :error
)

echo [Step 3/3] Copying artifact...
copy /Y "%APP_EXE%" "%OUTPUT_EXE%" >nul
if !errorlevel! neq 0 (
  echo [ERROR] Failed to copy %APP_EXE% to %OUTPUT_EXE%
  goto :error
)

echo [SUCCESS] Built: %OUTPUT_EXE%
endlocal
exit /b 0

:error
echo [FAILED] %APP_NAME% build failed.
endlocal
exit /b 1
