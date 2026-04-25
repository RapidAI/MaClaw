@echo off
setlocal EnableExtensions

set "ROOT_DIR=%~dp0"
call "%ROOT_DIR%deploy_all.cmd" hubcenter-only %*
exit /b %ERRORLEVEL%
