@echo off
setlocal EnableExtensions
REM Deploy MaClawSrv to maclawsrv.maclaw.top.

if not defined REMOTE_HOST set "REMOTE_HOST=maclawsrv.maclaw.top"
if not defined REMOTE_HOSTKEY set "REMOTE_HOSTKEY=ssh-ed25519 255 SHA256:yoyEXbuT2kezyG9Y8cJDZplBMZgaPAN7+sureAkVRVE"

call "%~dp0deploy_maclawsrv.cmd" %*
exit /b %ERRORLEVEL%
