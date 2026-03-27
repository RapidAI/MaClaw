@echo off
setlocal EnableDelayedExpansion

REM ==============================================================================
REM == Build MaClaw GUI with CGO Embedding (GemmaEmbedder) for Windows         ==
REM == Requires: CMake, C/C++ compiler (MinGW-w64 gcc), Go                     ==
REM ==============================================================================

echo [INFO] Starting CGO embedding build...

REM -- Set Environment Variables --
set "APP_NAME=MaClaw"
set "OUTPUT_DIR=%~dp0dist"
set "GOVERSIONINFO_PATH=%USERPROFILE%\go\bin\goversioninfo.exe"
set "GOPATH=%USERPROFILE%\go"
set "PATH=%GOPATH%\bin;%PATH%"

REM -- Ensure output dir exists --
if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"

REM -- Read version from wails.json --
echo [Step 1/5] Reading version...
%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -Command ^
  "$cfg = Get-Content '%~dp0wails.json' -Raw | ConvertFrom-Json;" ^
  "Set-Content -Path '%~dp0temp_CGO_VERSION.txt' -Value $cfg.info.productVersion -NoNewline"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to read version.
    goto :error
)
set /p VERSION=<"%~dp0temp_CGO_VERSION.txt"
del /q "%~dp0temp_CGO_VERSION.txt" 2>nul
echo [INFO] Version: %VERSION%

REM -- Build RapidSpeech static library --
echo [Step 2/5] Building RapidSpeech static library...
REM Clean any DLL/import-lib leftovers that confuse the static linker
if exist "%~dp0RapidSpeech.cpp\build" (
    del /q "%~dp0RapidSpeech.cpp\build\*.dll" 2>nul
    del /q "%~dp0RapidSpeech.cpp\build\*.dll.a" 2>nul
    for /r "%~dp0RapidSpeech.cpp\build\ggml" %%f in (*.dll *.dll.a) do del /q "%%f" 2>nul
)
call "%~dp0build\build_rapidspeech.cmd"
if !errorlevel! neq 0 (
    echo [ERROR] RapidSpeech build failed.
    goto :error
)

REM -- Build Frontend (skip if dist already exists) --
echo [Step 3/5] Building frontend...
if not exist "%~dp0gui\frontend\dist" (
    cd "%~dp0gui\frontend"
    if not exist "node_modules" (
        call npm.cmd install --cache ./.npm_cache
        if !errorlevel! neq 0 (
            echo [ERROR] npm install failed.
            goto :error
        )
    )
    %SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -Command "npm run build"
    if !errorlevel! neq 0 (
        echo [ERROR] Frontend build failed.
        goto :error
    )
    cd "%~dp0"
) else (
    echo [INFO] Frontend dist already exists, skipping build.
)

REM -- Generate Windows Resources --
echo [Step 4/5] Generating Windows resources...
del /q "%~dp0gui\resource_windows_*.syso" 2>nul
if not exist "%GOVERSIONINFO_PATH%" (
    echo [INFO] Installing goversioninfo...
    go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
    if !errorlevel! neq 0 (
        echo [ERROR] Failed to install goversioninfo.
        goto :error
    )
)

powershell -NoProfile -Command "$cfg = Get-Content '%~dp0wails.json' -Raw | ConvertFrom-Json; $parts = '%VERSION%'.Split('.'); $manifest = Get-Content '%~dp0build\windows\wails.exe.manifest' -Raw; $manifest = $manifest.Replace('{{.Name}}', $cfg.name).Replace('{{.Info.ProductVersion}}', '%VERSION%'); [System.IO.File]::WriteAllText('%~dp0build\windows\wails.exe.manifest.tmp', $manifest, [System.Text.UTF8Encoding]::new($false)); $versionInfo = @{ FixedFileInfo = @{ FileVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = [int]$parts[3] }; ProductVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = [int]$parts[3] } }; StringFileInfo = @{ CompanyName = $cfg.info.companyName; FileDescription = $cfg.info.productName; FileVersion = '%VERSION%'; ProductName = $cfg.info.productName; ProductVersion = '%VERSION%'; LegalCopyright = $cfg.info.copyright }; VarFileInfo = @{ Translation = @{ LangID = '0409'; CharsetID = '04B0' } } } | ConvertTo-Json -Depth 6; [System.IO.File]::WriteAllText('%~dp0build\windows\versioninfo.json.tmp', $versionInfo, [System.Text.UTF8Encoding]::new($false))"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to prepare Windows version resources.
    goto :error
)

REM -- Build GUI with CGO Embedding (amd64 only) --
echo [Step 5/5] Compiling GUI with cgo_embedding tag (amd64)...
set "GOOS=windows"
set "GOARCH=amd64"
set "CGO_ENABLED=1"

REM Clear Go build cache to ensure updated CGO flags take effect
go clean -cache >nul 2>&1

"%GOVERSIONINFO_PATH%" -64 -icon "%~dp0build\windows\icon.ico" -manifest "%~dp0build\windows\wails.exe.manifest.tmp" -o "%~dp0gui\resource_windows_amd64.syso" "%~dp0build\windows\versioninfo.json.tmp"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to generate amd64 resources.
    goto :error
)

go build -tags desktop,production,cgo_embedding -ldflags "-s -w -H windowsgui" -o "%OUTPUT_DIR%\%APP_NAME%_cgo_amd64.exe" ./gui/
if !errorlevel! neq 0 (
    echo [ERROR] CGO GUI build failed.
    echo [HINT] Make sure you have MinGW-w64 gcc in PATH and RapidSpeech static lib built.
    goto :error
)

del "%~dp0gui\resource_windows_amd64.syso" 2>nul
del "%~dp0build\windows\wails.exe.manifest.tmp" 2>nul
del "%~dp0build\windows\versioninfo.json.tmp" 2>nul

echo.
echo [SUCCESS] CGO embedding build complete!
echo Output: %OUTPUT_DIR%\%APP_NAME%_cgo_amd64.exe
echo.
echo NOTE: This build includes GemmaEmbedder via CGO.
echo       The standard build_win.bat uses NoopEmbedder (no CGO).
endlocal
goto :eof

:error
echo.
echo [FAILED] Build failed. Check output above.
endlocal
pause
exit /b 1
