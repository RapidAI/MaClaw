@echo off
rem Thin shim so `scripts/goheavy.cmd go test ./guiapp` works from cmd/git-bash.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0goheavy.ps1" %*
exit /b %ERRORLEVEL%
