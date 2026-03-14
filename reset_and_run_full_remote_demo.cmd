@echo off
setlocal

set ROOT_DIR=%~dp0

echo.
echo Resetting local MaClaw remote stack and running the full demo...
echo.

call "%ROOT_DIR%clean_remote_stack.cmd"
if errorlevel 1 (
  echo [FAIL] clean_remote_stack.cmd failed.
  exit /b 1
)

call "%ROOT_DIR%run_full_remote_demo_auto_open.cmd" %*
exit /b %errorlevel%
