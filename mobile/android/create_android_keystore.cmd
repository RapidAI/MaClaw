@echo off
setlocal

cd /d "%~dp0"

set "KEYSTORE_PATH=%CD%\codeclaw-release.jks"
if not "%~1"=="" set "KEYSTORE_PATH=%~1"
set "KEY_ALIAS=codeclaw"
if not "%~2"=="" set "KEY_ALIAS=%~2"

set "KEYTOOL="
if exist "%JAVA_HOME%\bin\keytool.exe" set "KEYTOOL=%JAVA_HOME%\bin\keytool.exe"
if "%KEYTOOL%"=="" if exist "C:\Program Files\Android\Android Studio\jbr\bin\keytool.exe" set "KEYTOOL=C:\Program Files\Android\Android Studio\jbr\bin\keytool.exe"
if "%KEYTOOL%"=="" for %%I in (keytool.exe) do set "KEYTOOL=%%~$PATH:I"

if "%KEYTOOL%"=="" (
    echo [ERROR] keytool.exe was not found.
    echo Set JAVA_HOME or install a JDK / Android Studio JBR first.
    exit /b 1
)

echo Creating keystore:
echo   %KEYSTORE_PATH%
echo Alias:
echo   %KEY_ALIAS%
echo.
echo keytool will prompt you for the keystore password and certificate details.

"%KEYTOOL%" -genkeypair -v -storetype PKCS12 -keystore "%KEYSTORE_PATH%" -alias "%KEY_ALIAS%" -keyalg RSA -keysize 2048 -validity 3650
if not %ERRORLEVEL%==0 exit /b %ERRORLEVEL%

echo.
echo Keystore created successfully.
echo Next step:
echo   1. Copy signing.example.cmd to signing.env.cmd
echo   2. Set RELEASE_STORE_FILE=%KEYSTORE_PATH%
echo   3. Set RELEASE_KEY_ALIAS=%KEY_ALIAS%
echo   4. Fill in the passwords you entered above
exit /b 0
