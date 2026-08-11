@echo off
setlocal EnableExtensions

rem Build one MaClaw board profile with only its profile-private Component
rem Manager dependencies.  Keep this wrapper as the supported developer/CI
rem entry point: manifests are resolved before Kconfig is available, so the
rem selected board cannot be inferred safely from CONFIG_* at that phase.
if "%~1"=="" goto :usage

set "MACLAW_PROFILE=%~1"
rem Keep the selected identity in the child environment as well as the CMake
rem cache. ESP-IDF's preliminary component-requirements pass launches a
rem separate `cmake -P` process which cannot see top-level -D cache entries.
rem main/CMakeLists.txt reads this value in that pass to select only the
rem profile's private driver dependencies.
set "MACLAW_EXTRA_COMPONENT_DIRS="
set "MACLAW_BUILD_DIR="
set "MACLAW_SDKCONFIG="
set "MACLAW_DEFAULTS="

if /I "%MACLAW_PROFILE%"=="echoear-2st" (
  set "MACLAW_EXTRA_COMPONENT_DIRS=%~dp0..\profile_components\echoear_deps"
  set "MACLAW_BUILD_DIR=build-unified-echoear"
  set "MACLAW_SDKCONFIG=sdkconfig.echoear-2st"
  goto :build
)
if /I "%MACLAW_PROFILE%"=="bread-compact" (
  set "MACLAW_BUILD_DIR=build-unified-bread"
  set "MACLAW_SDKCONFIG=sdkconfig.bread-compact"
  set "MACLAW_DEFAULTS=sdkconfig.defaults;sdkconfig.defaults.bread-compact"
  goto :build
)
if /I "%MACLAW_PROFILE%"=="bread-compact-renderer-fi" (
  set "MACLAW_PROFILE=bread-compact"
  set "MACLAW_BUILD_DIR=build-test-bread-compact-renderer-fi"
  set "MACLAW_SDKCONFIG=build-test-bread-compact-renderer-fi\sdkconfig"
  set "MACLAW_DEFAULTS=sdkconfig.defaults;sdkconfig.defaults.bread-compact;sdkconfig.defaults.bread-compact-renderer-fi"
  goto :build
)
if /I "%MACLAW_PROFILE%"=="fangtang-4g" (
  set "MACLAW_EXTRA_COMPONENT_DIRS=%~dp0..\profile_components\fangtang_deps"
  set "MACLAW_BUILD_DIR=build-unified-fangtang"
  set "MACLAW_SDKCONFIG=sdkconfig.fangtang-4g"
  set "MACLAW_DEFAULTS=sdkconfig.defaults;sdkconfig.defaults.fangtang-4g"
  goto :build
)
if /I "%MACLAW_PROFILE%"=="fangtang-4g-renderer-fi" (
  set "MACLAW_PROFILE=fangtang-4g"
  set "MACLAW_EXTRA_COMPONENT_DIRS=%~dp0..\profile_components\fangtang_deps"
  set "MACLAW_BUILD_DIR=build-test-fangtang-4g-renderer-fi"
  set "MACLAW_SDKCONFIG=build-test-fangtang-4g-renderer-fi\sdkconfig"
  set "MACLAW_DEFAULTS=sdkconfig.defaults;sdkconfig.defaults.fangtang-4g;sdkconfig.defaults.fangtang-4g-renderer-fi"
  goto :build
)
if /I "%MACLAW_PROFILE%"=="waveshare-amoled-1.75c" (
  set "MACLAW_EXTRA_COMPONENT_DIRS=%~dp0..\profile_components\waveshare_deps"
  set "MACLAW_BUILD_DIR=build-unified-waveshare"
  set "MACLAW_SDKCONFIG=sdkconfig.waveshare-amoled-1.75c"
  set "MACLAW_DEFAULTS=sdkconfig.defaults;sdkconfig.defaults.waveshare-amoled-1.75c"
  goto :build
)
goto :usage

:build
pushd "%~dp0.." || exit /b 2
if not "%IDF_PATH%"=="" goto :idf_ready
call C:\esp\v6.0.2\esp-idf\export.bat || (popd & exit /b 2)
:idf_ready
rem Each board has a distinct managed-dependency closure.  The root CMake
rem file maps MACLAW_PROFILE to a profile-qualified Component Manager lock;
rem pass only the identity here so the property remains authoritative in both
rem ESP-IDF configure passes.
set "MACLAW_IDF_ARGS=-B %MACLAW_BUILD_DIR% -D MACLAW_PROFILE=%MACLAW_PROFILE% -D SDKCONFIG=%MACLAW_SDKCONFIG% -D EXTRA_COMPONENT_DIRS=%MACLAW_EXTRA_COMPONENT_DIRS%"
if not "%MACLAW_DEFAULTS%"=="" set "MACLAW_IDF_ARGS=%MACLAW_IDF_ARGS% -D SDKCONFIG_DEFAULTS=%MACLAW_DEFAULTS%"

rem Keep the shared business/HAL headers independent of ESP-IDF, FreeRTOS and
rem board-driver objects on every supported profile build.  Profile-private
rem adapters remain the deliberate translation boundary below this check.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0check-hal-boundaries.ps1"
if errorlevel 1 (
  set "MACLAW_RESULT=%ERRORLEVEL%"
  goto :done
)

idf.py %MACLAW_IDF_ARGS% %~2 %~3 %~4 %~5 %~6 %~7 %~8 %~9
set "MACLAW_RESULT=%ERRORLEVEL%"

:done
popd
exit /b %MACLAW_RESULT%

:usage
echo Usage: %~nx0 ^<echoear-2st^|bread-compact^|bread-compact-renderer-fi^|fangtang-4g^|fangtang-4g-renderer-fi^|waveshare-amoled-1.75c^> [idf.py action...]
echo Example: %~nx0 bread-compact build
exit /b 64
