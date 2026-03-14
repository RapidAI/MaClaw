@echo off
setlocal

cd /d "%~dp0"

if exist signing.env.cmd (
    call signing.env.cmd
)

if "%RELEASE_STORE_FILE%"=="" (
    echo [ERROR] RELEASE_STORE_FILE is not set.
    echo Create signing.env.cmd from signing.example.cmd and fill in your keystore values.
    exit /b 1
)
if "%RELEASE_STORE_PASSWORD%"=="" (
    echo [ERROR] RELEASE_STORE_PASSWORD is not set.
    exit /b 1
)
if "%RELEASE_KEY_ALIAS%"=="" (
    echo [ERROR] RELEASE_KEY_ALIAS is not set.
    exit /b 1
)
if "%RELEASE_KEY_PASSWORD%"=="" (
    echo [ERROR] RELEASE_KEY_PASSWORD is not set.
    exit /b 1
)

call build_android.cmd assembleRelease
exit /b %ERRORLEVEL%
