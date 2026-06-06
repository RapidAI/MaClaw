@echo off
setlocal EnableExtensions
REM Deploy MaClawSrv to hub.maclaw.top. All normal deploy_maclawsrv.cmd
REM environment overrides still work, including REMOTE_PASS and MACLAWSRV_*.

if not defined REMOTE_HOST set "REMOTE_HOST=hub.maclaw.top"
if not defined REMOTE_HOSTKEY set "REMOTE_HOSTKEY=ssh-ed25519 255 SHA256:yoyEXbuT2kezyG9Y8cJDZplBMZgaPAN7+sureAkVRVE"

call "%~dp0deploy_maclawsrv.cmd" %*
exit /b %ERRORLEVEL%
