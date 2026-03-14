@echo off
setlocal

cd /d "%~dp0"
call build_android.cmd assembleRelease
exit /b %ERRORLEVEL%
