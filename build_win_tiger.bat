@echo off
setlocal EnableDelayedExpansion

REM ==============================================================================
REM == Batch Script to Build and Package TigerClaw (OEM QianXin) for Windows   ==
REM == Based on build_win.bat, with oem_qianxin build tag and TigerClaw naming ==
REM ==============================================================================

echo [INFO] Starting TigerClaw build process...

REM -- Set Environment Variables --
set "APP_NAME=TigerClaw"
set "BUILD_TAGS=desktop,production,oem_qianxin"
set "TUI_NAME=tigerclaw-tui"
set "TOOL_NAME=tigerclaw-tool"
set "OUTPUT_DIR=%~dp0dist"
set "NSIS_PATH=C:\Program Files (x86)\NSIS\makensis.exe"
set "GOVERSIONINFO_PATH=%USERPROFILE%\go\bin\goversioninfo.exe"

REM -- Icon: use tigerclaw.ico from assets --
set "ICON_PATH=%~dp0assets\tigerclaw.ico"
if not exist "%ICON_PATH%" (
    echo [WARN] assets\tigerclaw.ico not found, falling back to default icon.ico
    set "ICON_PATH=%~dp0build\windows\icon.ico"
)

REM -- Ensure Go tools are in PATH --
set "GOPATH=%USERPROFILE%\go"
set "PATH=%GOPATH%\bin;%PATH%"

REM -- Clean previous TigerClaw build artifacts (preserve MaClaw files) --
echo [Step 1/9] Cleaning previous TigerClaw build...
if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"
del /q "%OUTPUT_DIR%\%APP_NAME%*.exe" 2>nul
del /q "%OUTPUT_DIR%\%TUI_NAME%*.exe" 2>nul
del /q "%OUTPUT_DIR%\%TOOL_NAME%*.exe" 2>nul
del /q "%OUTPUT_DIR%\%APP_NAME%-Windows-Portable.zip" 2>nul

REM -- Increment build number and set version (single PowerShell call) --
echo [Step 2/9] Updating version number...
%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -Command ^
  "if (Test-Path 'build_number') { $n = [int](Get-Content 'build_number') + 1 } else { $n = 1 };" ^
  "Set-Content -Path 'build_number' -Value $n -NoNewline;" ^
  "$cfg = Get-Content '%~dp0wails.json' -Raw | ConvertFrom-Json;" ^
  "$parts = $cfg.info.productVersion.Split('.');" ^
  "$parts[3] = [string]$n;" ^
  "$ver = $parts -join '.';" ^
  "@{" ^
  "  VERSION = $ver;" ^
  "  BUILD_NUM = [string]$n;" ^
  "  PRODUCT_NAME = 'TigerClaw';" ^
  "  COMPANY_NAME = 'QianXin';" ^
  "  COPYRIGHT = 'Copyright (C) 2026 QianXin'" ^
  "}.GetEnumerator() | ForEach-Object { Set-Content -Path ('%~dp0temp_' + $_.Key + '.txt') -Value $_.Value -NoNewline }"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to update version info.
    goto :error
)
set /p BUILD_NUM=<"%~dp0temp_BUILD_NUM.txt"
set /p VERSION=<"%~dp0temp_VERSION.txt"
set /p PRODUCT_NAME=<"%~dp0temp_PRODUCT_NAME.txt"
set /p COMPANY_NAME=<"%~dp0temp_COMPANY_NAME.txt"
set /p COPYRIGHT_TEXT=<"%~dp0temp_COPYRIGHT.txt"
del /q "%~dp0temp_BUILD_NUM.txt" "%~dp0temp_VERSION.txt" "%~dp0temp_PRODUCT_NAME.txt" "%~dp0temp_COMPANY_NAME.txt" "%~dp0temp_COPYRIGHT.txt" 2>nul
echo [INFO] Building TigerClaw Version: %VERSION%

REM -- Sync version with frontend --
echo [Step 3/9] Syncing version with frontend...
powershell -NoProfile -Command "@('export const buildNumber = ''%BUILD_NUM%'';','export const appVersion = ''%VERSION%'';') | Set-Content -Path '%~dp0gui\frontend\src\version.ts' -Encoding Utf8"

REM -- Build Frontend --
echo [Step 4/9] Building frontend...
cd "%~dp0gui\frontend"
if not exist "node_modules" (
    call npm.cmd install --cache ./.npm_cache
    if !errorlevel! neq 0 (
        echo [ERROR] npm install failed.
        goto :error
    )
)
if exist "dist" ( rmdir /s /q "dist" )
call npm.cmd run build
if !errorlevel! neq 0 (
    echo [ERROR] Frontend build failed.
    goto :error
)
cd "%~dp0"

REM -- Generate Windows Resources (icon + version info) --
echo [Step 5/9] Generating Windows resources...
del /q "%~dp0gui\resource_windows_*.syso" 2>nul
del /q "%~dp0resource_windows_*.syso" 2>nul
del /q "%~dp0tmp*.syso" 2>nul
del /q "%~dp0tmp*.json" 2>nul
del /q "%~dp0build\windows\wails.exe.manifest.tmp" 2>nul
del /q "%~dp0build\windows\versioninfo.json.tmp" 2>nul
if not exist "%GOVERSIONINFO_PATH%" (
    echo [INFO] goversioninfo not found. Installing...
    go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
    if !errorlevel! neq 0 (
        echo [ERROR] Failed to install goversioninfo.
        goto :error
    )
)

powershell -NoProfile -Command "$cfg = Get-Content '%~dp0wails.json' -Raw | ConvertFrom-Json; $parts = '%VERSION%'.Split('.'); if ($parts.Length -ne 4) { throw 'Version must contain 4 numeric parts for Windows resources.' }; $manifest = Get-Content '%~dp0build\windows\wails.exe.manifest' -Raw; $manifest = $manifest.Replace('{{.Name}}', 'TigerClaw').Replace('{{.Info.ProductVersion}}', '%VERSION%'); [System.IO.File]::WriteAllText('%~dp0build\windows\wails.exe.manifest.tmp', $manifest, [System.Text.UTF8Encoding]::new($false)); $versionInfo = @{ FixedFileInfo = @{ FileVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = [int]$parts[3] }; ProductVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = [int]$parts[3] } }; StringFileInfo = @{ Comments = 'TigerClaw: Secure coding assistant by QianXin'; CompanyName = 'QianXin'; FileDescription = 'TigerClaw'; FileVersion = '%VERSION%'; InternalName = 'TigerClaw'; LegalCopyright = 'Copyright (C) 2026 QianXin'; OriginalFilename = '%APP_NAME%.exe'; ProductName = 'TigerClaw'; ProductVersion = '%VERSION%' }; VarFileInfo = @{ Translation = @{ LangID = '0409'; CharsetID = '04B0' } } } | ConvertTo-Json -Depth 6; [System.IO.File]::WriteAllText('%~dp0build\windows\versioninfo.json.tmp', $versionInfo, [System.Text.UTF8Encoding]::new($false))"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to prepare Windows version resource inputs.
    goto :error
)

REM -- (Optional) Build RapidSpeech static library for CGO embedding --
REM   If build\build_rapidspeech.cmd succeeds, the cgo_embedding tag is added
REM   so that GemmaEmbedder (vector embedding via RapidSpeech) is compiled in.
REM   If it fails (missing CMake/compiler), the build continues without it.
echo [Step 5.5/9] Building RapidSpeech static library (optional)...
set "RS_LIB=%~dp0RapidSpeech.cpp\build\librapidspeech_static.a"
if exist "%~dp0build\build_rapidspeech.cmd" (
    call "%~dp0build\build_rapidspeech.cmd"
    if !errorlevel! equ 0 (
        if exist "%RS_LIB%" (
            echo [INFO] RapidSpeech static library built. Enabling cgo_embedding tag.
            set "BUILD_TAGS=%BUILD_TAGS%,cgo_embedding"
            set "CGO_ENABLED=1"
        ) else (
            echo [WARN] build_rapidspeech.cmd succeeded but library not found. Skipping cgo_embedding.
        )
    ) else (
        echo [WARN] RapidSpeech static library build failed. Continuing without cgo_embedding.
    )
) else (
    echo [WARN] build\build_rapidspeech.cmd not found. Skipping RapidSpeech build.
)

REM -- Build Go Binaries (with oem_qianxin tag) --
echo [Step 6/9] Compiling TigerClaw GUI binaries...
set "GOOS=windows"
if not "%CGO_ENABLED%"=="1" set "CGO_ENABLED=0"
set "GOARCH=amd64"
"%GOVERSIONINFO_PATH%" -64 -icon "%ICON_PATH%" -manifest "%~dp0build\windows\wails.exe.manifest.tmp" -o "%~dp0gui\resource_windows_amd64.syso" "%~dp0build\windows\versioninfo.json.tmp"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to generate amd64 resources.
    goto :error
)
go build -tags %BUILD_TAGS% -ldflags "-s -w -H windowsgui" -o "%OUTPUT_DIR%\%APP_NAME%_amd64.exe" ./gui/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for TigerClaw GUI amd64 failed.
    goto :error
)
del "%~dp0gui\resource_windows_amd64.syso"
set "GOARCH=arm64"
"%GOVERSIONINFO_PATH%" -64 -arm -icon "%ICON_PATH%" -manifest "%~dp0build\windows\wails.exe.manifest.tmp" -o "%~dp0gui\resource_windows_arm64.syso" "%~dp0build\windows\versioninfo.json.tmp"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to generate arm64 resources.
    goto :error
)
go build -tags %BUILD_TAGS% -ldflags "-s -w -H windowsgui" -o "%OUTPUT_DIR%\%APP_NAME%_arm64.exe" ./gui/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for TigerClaw GUI arm64 failed.
    goto :error
)
del "%~dp0gui\resource_windows_arm64.syso"
del "%~dp0build\windows\wails.exe.manifest.tmp"
del "%~dp0build\windows\versioninfo.json.tmp"

REM -- Build TUI/CLI Binaries --
echo [Step 7/9] Compiling TigerClaw TUI/CLI binaries...
set "CGO_ENABLED=0"
set "GOARCH=amd64"
go build -tags oem_qianxin -ldflags "-s -w -X main.version=%VERSION%" -o "%OUTPUT_DIR%\%TUI_NAME%_amd64.exe" ./tui/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for TigerClaw TUI amd64 failed.
    goto :error
)
set "GOARCH=arm64"
go build -tags oem_qianxin -ldflags "-s -w -X main.version=%VERSION%" -o "%OUTPUT_DIR%\%TUI_NAME%_arm64.exe" ./tui/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for TigerClaw TUI arm64 failed.
    goto :error
)

REM -- Build tigerclaw-tool Binary --
echo [Step 8/9] Compiling tigerclaw-tool binaries...
set "GOARCH=amd64"
go build -tags oem_qianxin -ldflags "-s -w -X main.version=%VERSION%" -o "%OUTPUT_DIR%\%TOOL_NAME%_amd64.exe" ./cmd/maclaw-tool/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for tigerclaw-tool amd64 failed.
    goto :error
)
set "GOARCH=arm64"
go build -tags oem_qianxin -ldflags "-s -w -X main.version=%VERSION%" -o "%OUTPUT_DIR%\%TOOL_NAME%_arm64.exe" ./cmd/maclaw-tool/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for tigerclaw-tool arm64 failed.
    goto :error
)

REM Reset Env for NSIS
set "GOOS="
set "GOARCH="
set "CGO_ENABLED="
set "CC="
set "CXX="

REM -- Create NSIS Installer --
echo [Step 9/9] Creating NSIS installer...
if not exist "%NSIS_PATH%" goto nsis_missing

"%NSIS_PATH%" /DINFO_PROJECTNAME="%APP_NAME%" /DPRODUCT_EXECUTABLE="%APP_NAME%.exe" /DINFO_PRODUCTNAME="%PRODUCT_NAME%" /DINFO_COMPANYNAME="%COMPANY_NAME%" /DINFO_COPYRIGHT="%COPYRIGHT_TEXT%" /DINFO_PRODUCTVERSION="%VERSION%" /DARG_WAILS_AMD64_BINARY="%OUTPUT_DIR%\%APP_NAME%_amd64.exe" /DARG_WAILS_ARM64_BINARY="%OUTPUT_DIR%\%APP_NAME%_arm64.exe" /DMUI_ICON_PATH="%ICON_PATH%" "%~dp0build\windows\installer\multiarch.nsi"
if !errorlevel! neq 0 (
    echo [ERROR] NSIS installer creation failed.
    goto :error
)

if exist "%OUTPUT_DIR%\%APP_NAME%-Setup.exe" (
    echo [SUCCESS] Windows installer created at: %OUTPUT_DIR%\%APP_NAME%-Setup.exe
)

REM -- Copy/Rename Main Binaries for convenience --
echo   - Creating main executable copies (amd64)...
copy /Y "%OUTPUT_DIR%\%APP_NAME%_amd64.exe" "%OUTPUT_DIR%\%APP_NAME%.exe" >nul
copy /Y "%OUTPUT_DIR%\%TUI_NAME%_amd64.exe" "%OUTPUT_DIR%\%TUI_NAME%.exe" >nul
copy /Y "%OUTPUT_DIR%\%TOOL_NAME%_amd64.exe" "%OUTPUT_DIR%\%TOOL_NAME%.exe" >nul

if exist "%OUTPUT_DIR%\%APP_NAME%.exe" (
    echo [SUCCESS] GUI binary: %OUTPUT_DIR%\%APP_NAME%.exe
)
if exist "%OUTPUT_DIR%\%TUI_NAME%.exe" (
    echo [SUCCESS] TUI/CLI binary: %OUTPUT_DIR%\%TUI_NAME%.exe
)
if exist "%OUTPUT_DIR%\%TOOL_NAME%.exe" (
    echo [SUCCESS] tigerclaw-tool binary: %OUTPUT_DIR%\%TOOL_NAME%.exe
)

echo   - Creating Windows portable zip...
powershell -NoProfile -Command "Add-Type -AssemblyName System.IO.Compression.FileSystem; $zip = '%OUTPUT_DIR%\%APP_NAME%-Windows-Portable.zip'; if (Test-Path $zip) { Remove-Item $zip -Force }; $tmp = Join-Path $env:TEMP ('tigerclaw_zip_' + [guid]::NewGuid().ToString('N')); New-Item -ItemType Directory -Path $tmp | Out-Null; Copy-Item '%OUTPUT_DIR%\%APP_NAME%.exe','%OUTPUT_DIR%\%TUI_NAME%.exe','%OUTPUT_DIR%\%TOOL_NAME%.exe' -Destination $tmp; [System.IO.Compression.ZipFile]::CreateFromDirectory($tmp, $zip); Remove-Item $tmp -Recurse -Force; Write-Host '[INFO] Portable zip created.'"

goto :success

:nsis_missing
echo [ERROR] NSIS not found at "%NSIS_PATH%". Please install NSIS.
goto :error

:success
echo.
echo [SUCCESS] TigerClaw build and packaging complete!
echo Artifacts are in: %OUTPUT_DIR%
endlocal
goto :eof

:error
echo.
echo [FAILED] The TigerClaw build process failed. Please check the output above for errors.
endlocal
pause
exit /b 1
