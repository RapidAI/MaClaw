@echo off
rem Runs idf.py with the ESP-IDF 6.0.2 environment preloaded.
rem
rem export.bat refuses to run when MSYSTEM is set (Git Bash inherits it into
rem cmd.exe), so clear it for this child shell only.
setlocal
set MSYSTEM=
set MSYSCON=
call "C:\esp\v6.0.2\esp-idf\export.bat" >nul
if errorlevel 1 (
  echo [buddy] ESP-IDF export.bat failed
  exit /b 1
)
idf.py %*
