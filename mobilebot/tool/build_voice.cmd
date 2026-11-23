@echo off
REM Build libmaclaw_voice.so (Kokoro TTS + SenseVoice ASR, the desktop's own
REM Go inference) for Android jniLibs. Requires Go 1.26+ and an Android NDK,
REM found via ANDROID_NDK_HOME, then ANDROID_HOME / ANDROID_SDK_ROOT / the
REM default SDK path below (highest ndk/<version> wins).
setlocal enabledelayedexpansion
set "NDK=%ANDROID_NDK_HOME%"
if "%NDK%"=="" if not "%ANDROID_NDK_LATEST_HOME%"=="" set "NDK=%ANDROID_NDK_LATEST_HOME%"
if "%NDK%"=="" call :findndk "%ANDROID_HOME%"
if "%NDK%"=="" call :findndk "%ANDROID_SDK_ROOT%"
if "%NDK%"=="" call :findndk "D:\Android\Sdk"
if "%NDK%"=="" call :findndk "%LOCALAPPDATA%\Android\Sdk"
if "%NDK%"=="" (
  echo Android NDK not found: set ANDROID_NDK_HOME or install one under the SDK's ndk\ folder.
  exit /b 1
)
echo Using NDK %NDK%
set "BIN=%NDK%\toolchains\llvm\prebuilt\windows-x86_64\bin"
REM The ndk clang wrappers are shipped as *.cmd (r23+) or bare executables; both work as CC.
if not exist "%BIN%\aarch64-linux-android21-clang.cmd" if not exist "%BIN%\aarch64-linux-android21-clang" (
  echo NDK at %NDK% has no windows-x86_64 clang toolchain.
  exit /b 1
)
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

:findndk
REM %~1 = SDK root. Picks the highest ndk/<version> that ships the clang wrappers.
REM Sets NDK for the caller via `endlocal & set`: a plain set inside this
REM subroutine (and inside its for block) would be discarded on return.
if "%~1"=="" goto :eof
if not exist "%~1\ndk" goto :eof
set "FOUND="
for /f "delims=" %%d in ('dir /b /ad /o-n "%~1\ndk" 2^>nul') do (
  if not defined FOUND (
    if exist "%~1\ndk\%%d\toolchains\llvm\prebuilt\windows-x86_64\bin\aarch64-linux-android21-clang.cmd" set "FOUND=%%d"
    if not defined FOUND if exist "%~1\ndk\%%d\toolchains\llvm\prebuilt\windows-x86_64\bin\aarch64-linux-android21-clang" set "FOUND=%%d"
  )
)
if not defined FOUND goto :eof
endlocal & set "NDK=%~1\ndk\%FOUND%"
goto :eof

:one
set GOOS=android
if "%1"=="arm" (set GOARCH=arm& set GOARM=7) else (set GOARCH=%1& set GOARM=)
set "CC=%BIN%\%2" & set "CXX=%BIN%\%2++"
go build -buildmode=c-shared -o "%OUT%\%3\libmaclaw_voice.so" ./mobilebot/native/voiceffi/ || exit /b 1
echo   %3 ok
REM The c-shared build links libc++_shared.so. A plain APK does not ship it,
REM so dlopen fails and the app reports no local engine. Copy it beside the lib.
set "CXXSRC="
for /f "delims=" %%f in ('dir /b /s "%NDK%\toolchains\llvm\prebuilt\*\sysroot\usr\lib\%3\libc++_shared.so" 2^>nul') do if not defined CXXSRC set "CXXSRC=%%f"
if not defined CXXSRC (
  echo libc++_shared.so for %3 not found in %NDK%
  exit /b 1
)
copy /y "%CXXSRC%" "%OUT%\%3\libc++_shared.so" >nul || exit /b 1
echo   %3 libc++_shared.so
goto :eof

:fail
echo build failed
exit /b 1
