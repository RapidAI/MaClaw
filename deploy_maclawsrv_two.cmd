@echo off
setlocal EnableExtensions
REM Deploy MaClawSrv to both public MaClawSrv servers.

set "ROOT_DIR=%~dp0"
set "POWERSHELL=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
set "PROMPT_SCRIPT=%ROOT_DIR%prompt_password.ps1"
set "PASSWORD_FILE=%TEMP%\deploy_maclawsrv_two_password_%RANDOM%_%RANDOM%.txt"

if /I "%~1"=="-h" goto :usage
if /I "%~1"=="--help" goto :usage
if /I "%~1"=="/h" goto :usage
if /I "%~1"=="/?" goto :usage

if not defined REMOTE_PASS call :prompt_password
if errorlevel 1 exit /b 1

call "%~dp0deploy_maclawsrv_mypapers.cmd" %*
if errorlevel 1 exit /b %ERRORLEVEL%

call "%~dp0deploy_maclawsrv_maclaw.cmd" %*
exit /b %ERRORLEVEL%

:usage
call "%~dp0deploy_maclawsrv.cmd" --help
exit /b %ERRORLEVEL%

:prompt_password
if not exist "%PROMPT_SCRIPT%" (
  echo [ERROR] Missing %PROMPT_SCRIPT%
  exit /b 1
)
echo Enter SSH password for root@maclawsrv.mypapers.top and root@maclawsrv.maclaw.top:
del /q "%PASSWORD_FILE%" >nul 2>nul
"%POWERSHELL%" -NoProfile -ExecutionPolicy Bypass -File "%PROMPT_SCRIPT%" -Prompt "Password" -OutputPath "%PASSWORD_FILE%"
if exist "%PASSWORD_FILE%" (
  set /p REMOTE_PASS=<"%PASSWORD_FILE%"
  del /q "%PASSWORD_FILE%" >nul 2>nul
)
if not defined REMOTE_PASS (
  echo [ERROR] Empty password.
  exit /b 1
)
exit /b 0
