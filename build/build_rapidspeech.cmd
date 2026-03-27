@echo off
REM Build RapidSpeech static library for CGO embedding integration.
REM Output: RapidSpeech.cpp/build/librapidspeech_static.a (or .lib on MSVC)
REM
REM Prerequisites: CMake 3.14+, C/C++ compiler (gcc/clang/MSVC)

setlocal
set SCRIPT_DIR=%~dp0
set RS_DIR=%SCRIPT_DIR%..\RapidSpeech.cpp
set BUILD_DIR=%RS_DIR%\build

echo [build_rapidspeech] Building static library...
echo [build_rapidspeech] Source: %RS_DIR%
echo [build_rapidspeech] Build:  %BUILD_DIR%

if not exist "%BUILD_DIR%" mkdir "%BUILD_DIR%"

cmake -G "MinGW Makefiles" -S "%RS_DIR%" -B "%BUILD_DIR%" ^
    -DCMAKE_BUILD_TYPE=Release ^
    -DBUILD_SHARED_LIBS=OFF ^
    -DRS_BUILD_TESTS=OFF ^
    -DRS_BUILD_SERVER=OFF ^
    -DRS_ENABLE_PYTHON=OFF

if %ERRORLEVEL% neq 0 (
    echo [build_rapidspeech] CMake configure failed.
    exit /b 1
)

cmake --build "%BUILD_DIR%" --target rapidspeech_static --config Release -j %NUMBER_OF_PROCESSORS%

if %ERRORLEVEL% neq 0 (
    echo [build_rapidspeech] Build failed.
    exit /b 1
)

echo [build_rapidspeech] Done. Static library built successfully.
endlocal
