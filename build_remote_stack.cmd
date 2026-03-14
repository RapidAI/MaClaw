@echo off
setlocal

set ROOT_DIR=%~dp0

if not exist "%ROOT_DIR%hub\build.cmd" (
  echo [ERROR] Missing hub\build.cmd
  exit /b 1
)

if not exist "%ROOT_DIR%hubcenter\build.cmd" (
  echo [ERROR] Missing hubcenter\build.cmd
  exit /b 1
)

echo Building MaClaw Hub...
call "%ROOT_DIR%hub\build.cmd"
if errorlevel 1 exit /b %errorlevel%

echo Building MaClaw Hub Center...
call "%ROOT_DIR%hubcenter\build.cmd"
if errorlevel 1 exit /b %errorlevel%

echo.
echo Remote stack packaging finished.
