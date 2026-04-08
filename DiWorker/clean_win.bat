@echo off
setlocal

set "ROOT=%~dp0"
pushd "%ROOT%" || exit /b 1

echo [1/4] Remove frontend dist...
if exist "frontend\dist" rmdir /s /q "frontend\dist"

echo [2/4] Remove frontend cache...
if exist "frontend\.npm_cache" rmdir /s /q "frontend\.npm_cache"

echo [3/4] Remove build bin...
if exist "build\bin" rmdir /s /q "build\bin"

echo [4/4] Remove project dist...
if exist "dist" rmdir /s /q "dist"

echo [OK] Clean complete.
popd
exit /b 0
