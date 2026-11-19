@echo off
REM Build libmaclaw_voice.so (Kokoro TTS + SenseVoice ASR, the desktop's own
REM Go inference) for Android jniLibs. Requires Go 1.26+ and ANDROID_NDK_HOME
REM or the default SDK NDK path below.
setlocal
set "NDK=%ANDROID_NDK_HOME%"
if "%NDK%"=="" set "NDK=D:\Android\Sdk\ndk\28.2.13676358"
set "BIN=%NDK%\toolchains\llvm\prebuilt\windows-x86_64\bin"
set "GOTMP=C:\g"
if not exist "%GOTMP%" mkdir "%GOTMP%"
set "OUT=%~dp0..\android\app\src\main\jniLibs"
set "TMP=%GOTMP%" & set "TEMP=%GOTMP%" & set "GOTMPDIR=%GOTMP%"
set CGO_ENABLED=1

call :one arm64   aarch64-linux-android21-clang arm64-v8a    || goto :fail
REM armv7 skipped: kokoro simd kernels only exist for arm64/amd64.
call :one amd64   x86_64-linux-android21-clang x86_64        || goto :fail
echo Built libmaclaw_voice.so for arm64-v8a x86_64
goto :eof

:one
set GOOS=android
if "%1"=="arm" (set GOARCH=arm& set GOARM=7) else (set GOARCH=%1& set GOARM=)
set "CC=%BIN%\%2" & set "CXX=%BIN%\%2++"
go build -buildmode=c-shared -o "%OUT%\%3\libmaclaw_voice.so" ./mobilebot/native/voiceffi/ || exit /b 1
echo   %3 ok
goto :eof

:fail
echo build failed
exit /b 1
