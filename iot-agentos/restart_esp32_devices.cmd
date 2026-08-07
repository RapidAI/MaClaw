@echo off
setlocal EnableExtensions
chcp 65001 >nul

rem Restart the three connected ESP32 devices by using their serial reset lines.
rem Usage:
rem   restart_esp32_devices.cmd          (restart all devices)
rem   restart_esp32_devices.cmd echoear  (COM3 only)
rem   restart_esp32_devices.cmd bread    (COM4 only)
rem   restart_esp32_devices.cmd fangtang (COM5 only)
rem   restart_esp32_devices.cmd COM3     (a port name also works)

set "TARGET=%~1"
if "%TARGET%"=="" set "TARGET=all"
set "FAILED=0"

if /I "%TARGET%"=="all" (
    call :restart COM3 "Echo Ear 2st"
    if errorlevel 1 set "FAILED=1"
    call :restart COM4 "Bread Compact"
    if errorlevel 1 set "FAILED=1"
    call :restart COM5 "Fangtang 4G"
    if errorlevel 1 set "FAILED=1"
    goto :done
)
if /I "%TARGET%"=="echoear" set "TARGET=COM3"
if /I "%TARGET%"=="echo" set "TARGET=COM3"
if /I "%TARGET%"=="bread" set "TARGET=COM4"
if /I "%TARGET%"=="fangtang" set "TARGET=COM5"

if /I "%TARGET%"=="COM3" (
    call :restart COM3 "Echo Ear 2st"
    goto :done
)
if /I "%TARGET%"=="COM4" (
    call :restart COM4 "Bread Compact"
    goto :done
)
if /I "%TARGET%"=="COM5" (
    call :restart COM5 "Fangtang 4G"
    goto :done
)

echo Unknown device: %~1
echo Usage: %~nx0 [all^|echoear^|bread^|fangtang^|COM3^|COM4^|COM5]
exit /b 2

:restart
set "PORT=%~1"
set "DEVICE=%~2"
echo Restarting %DEVICE% on %PORT%...
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0restart_esp32_device.ps1" -Port "%PORT%"
if errorlevel 1 (
    echo FAILED: %DEVICE% on %PORT% could not be restarted. Close serial monitors and try again.
    exit /b 1
)
echo OK: %DEVICE% on %PORT% restart signal sent.
exit /b 0

:done
echo.
echo Complete.
if "%FAILED%"=="1" exit /b 1
endlocal
