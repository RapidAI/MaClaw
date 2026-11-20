@echo off
rem Thin shim so `scripts/goheavy.cmd go test ./guiapp` works from cmd/git-bash.
rem Routed through powershell -Command with SINGLE-quoted tokens: a quoted "-o"
rem under -File would still be bound to the script's common parameters
rem (-o prefix-matches -OutVariable/-OutBuffer -> AmbiguousParameter), while a
rem single-quoted string inside -Command is a plain literal. Single quotes also
rem keep space-containing tokens (e.g. -ldflags "-s -w") as ONE argument.
rem NOTE: capture %~dp0 BEFORE the arg loop — cmd's `shift` rewrites %0 too,
rem so after the last shift %~dp0 would point at the last argument's directory.
setlocal EnableExtensions
set "GOHEAVY_DIR=%~dp0"
set "GOHEAVY_ARGS="
:next_arg
if "%~1"=="" goto run
set "GOHEAVY_ARGS=%GOHEAVY_ARGS% '%~1'"
shift
goto next_arg
:run
powershell -NoProfile -ExecutionPolicy Bypass -Command "& '%GOHEAVY_DIR%goheavy.ps1' %GOHEAVY_ARGS%"
endlocal & exit /b %ERRORLEVEL%
