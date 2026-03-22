@echo off
setlocal enabledelayedexpansion

cd /d "%~dp0"

set "TASK=assembleDebug"
if not "%~1"=="" set "TASK=%~1"
set "OUTPUT_KIND=debug"
echo %TASK% | findstr /I "Release" >nul
if not errorlevel 1 set "OUTPUT_KIND=release"

if exist gradlew.bat (
    call gradlew.bat %TASK%
    if !ERRORLEVEL! neq 0 goto :build_failed
    goto :after_build
)

where gradle >nul 2>nul
if %ERRORLEVEL%==0 (
    call gradle %TASK%
    if !ERRORLEVEL! neq 0 goto :build_failed
    goto :after_build
)

echo [ERROR] Neither gradlew.bat nor gradle was found.
echo Install Gradle or open this project in Android Studio first.
exit /b 1

:build_failed
echo [ERROR] Android build failed.
exit /b 1

:after_build
set "APK_DIR=%CD%\app\build\outputs\apk\%OUTPUT_KIND%"
set "DIST_DIR=%CD%\..\dist"
if exist "%APK_DIR%" (
    if not exist "%DIST_DIR%" mkdir "%DIST_DIR%"
    for %%F in ("%APK_DIR%\*.apk") do (
        copy /y "%%~fF" "%DIST_DIR%\%%~nxF" >nul
    )
    echo.
    echo Build finished.
    echo APK output directory:
    echo   %APK_DIR%
    echo Copied APKs to:
    echo   %DIST_DIR%
    dir /b "%APK_DIR%\*.apk" 2>nul
) else (
    echo.
    echo Build finished, but APK directory was not found yet:
    echo   %APK_DIR%
)

exit /b 0
