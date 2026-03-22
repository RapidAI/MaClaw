@echo off
setlocal

cd /d "%~dp0"

if exist signing.env.cmd (
    call signing.env.cmd
)

if "%RELEASE_STORE_FILE%"=="" (
    echo [ERROR] RELEASE_STORE_FILE is not set.
    echo Create signing.env.cmd from signing.example.cmd and fill in your keystore values.
    goto :fail
)
if "%RELEASE_STORE_PASSWORD%"=="" (
    echo [ERROR] RELEASE_STORE_PASSWORD is not set.
    goto :fail
)
if "%RELEASE_KEY_ALIAS%"=="" (
    echo [ERROR] RELEASE_KEY_ALIAS is not set.
    goto :fail
)
if "%RELEASE_KEY_PASSWORD%"=="" (
    echo [ERROR] RELEASE_KEY_PASSWORD is not set.
    goto :fail
)

call build_android.cmd assembleRelease
if errorlevel 1 goto :fail

echo.
echo ========================================
echo   BUILD SUCCEEDED
echo ========================================
echo.
if not defined NOPAUSE pause
exit /b 0

:fail
echo.
echo ========================================
echo   BUILD FAILED
echo ========================================
echo.
if not defined NOPAUSE pause
exit /b 1
