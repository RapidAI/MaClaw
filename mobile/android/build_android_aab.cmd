@echo off
setlocal

cd /d "%~dp0"

if exist gradlew.bat (
    call gradlew.bat bundleRelease
    goto :after_build
)

where gradle >nul 2>nul
if %ERRORLEVEL%==0 (
    call gradle bundleRelease
    goto :after_build
)

echo [ERROR] Neither gradlew.bat nor gradle was found.
echo Install Gradle or open this project in Android Studio first.
exit /b 1

:after_build
if not %ERRORLEVEL%==0 (
    echo [ERROR] Android AAB build failed.
    exit /b %ERRORLEVEL%
)

set "AAB_DIR=%CD%\app\build\outputs\bundle\release"
set "DIST_DIR=%CD%\..\dist"
if exist "%AAB_DIR%" (
    if not exist "%DIST_DIR%" mkdir "%DIST_DIR%"
    if exist "%AAB_DIR%\*.aab" (
        for %%F in ("%AAB_DIR%\*.aab") do (
            copy /y "%%~fF" "%DIST_DIR%\maclaw-release.aab" >nul
        )
    )
    echo.
    echo Build finished.
    echo AAB output directory:
    echo   %AAB_DIR%
    echo Copied AAB to:
    echo   %DIST_DIR%\maclaw-release.aab
    dir /b "%AAB_DIR%\*.aab" 2>nul
) else (
    echo.
    echo Build finished, but AAB directory was not found yet:
    echo   %AAB_DIR%
)

exit /b 0
