@echo off
setlocal EnableExtensions
chcp 65001 >nul
cd /d "%~dp0"

echo ========================================
echo MaClaw Release Publisher
echo ========================================
echo.

where git >nul 2>nul
if errorlevel 1 (
  echo ERROR: git not found in PATH.
  pause
  exit /b 1
)

if not exist "gui\frontend\src\version.ts" (
  echo ERROR: gui\frontend\src\version.ts not found.
  pause
  exit /b 1
)

for /f "tokens=2 delims='" %%v in ('findstr /C:"appVersion" "gui\frontend\src\version.ts"') do set "VERSION=%%v"
if not defined VERSION (
  echo ERROR: Could not read appVersion from gui\frontend\src\version.ts.
  pause
  exit /b 1
)

set "TAG=V%VERSION%"

echo Version: %VERSION%
echo Tag:     %TAG%
echo.
set /p "DESC=Release description: "
if not defined DESC (
  echo ERROR: Release description is required.
  pause
  exit /b 1
)

for /f "delims=" %%b in ('git branch --show-current') do set "BRANCH=%%b"
if /i not "%BRANCH%"=="main" (
  echo ERROR: Current branch is "%BRANCH%". Please release from main.
  pause
  exit /b 1
)

for /f "delims=" %%s in ('git status --porcelain') do set "DIRTY=1"
if defined DIRTY (
  echo ERROR: Working tree is not clean. Commit or stash changes before release.
  echo.
  git status --short
  pause
  exit /b 1
)

echo.
echo Fetching latest origin/main and tags...
git fetch origin main --tags
if errorlevel 1 goto :fail

for /f "delims=" %%h in ('git rev-parse HEAD') do set "LOCAL_HEAD=%%h"
for /f "delims=" %%h in ('git rev-parse origin/main') do set "REMOTE_HEAD=%%h"
if /i not "%LOCAL_HEAD%"=="%REMOTE_HEAD%" (
  echo.
  echo Local main is not the same as origin/main. Pushing main first...
  git push origin main
  if errorlevel 1 goto :fail
)

git rev-parse -q --verify "refs/tags/%TAG%" >nul 2>nul
if not errorlevel 1 (
  echo ERROR: Local tag %TAG% already exists.
  pause
  exit /b 1
)

git ls-remote --exit-code --tags origin "refs/tags/%TAG%" >nul 2>nul
if not errorlevel 1 (
  echo ERROR: Remote tag %TAG% already exists on origin.
  pause
  exit /b 1
)

echo.
echo Creating annotated tag %TAG%...
git tag -a "%TAG%" -m "%DESC%"
if errorlevel 1 goto :fail

echo Pushing tag to GitHub to trigger Release Build...
git push origin "%TAG%"
if errorlevel 1 goto :fail

echo.
echo Release started successfully.
echo GitHub Actions: https://github.com/RapidAI/MaClaw/actions/workflows/main.yml
echo GitHub Release will be created for: %TAG%
echo Gitee Release will be synced automatically after GitHub Release succeeds.
echo.
pause
exit /b 0

:fail
echo.
echo ERROR: Release command failed.
echo If the tag was created locally but push failed, inspect it with:
echo   git tag -n99 %TAG%
echo Remove it only if needed with:
echo   git tag -d %TAG%
echo.
pause
exit /b 1