@echo off
setlocal EnableExtensions
REM Deploy MaClawSrv to maclawsrv.mypapers.top.

if not defined REMOTE_HOST set "REMOTE_HOST=maclawsrv.mypapers.top"
if not defined REMOTE_HOSTKEY set "REMOTE_HOSTKEY=ssh-ed25519 255 SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog"

call "%~dp0deploy_maclawsrv.cmd" %*
exit /b %ERRORLEVEL%
