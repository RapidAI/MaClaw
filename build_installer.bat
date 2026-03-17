@echo off
setlocal EnableDelayedExpansion

REM ==============================================================================
REM == Batch Script to Build and Package the CodeClaw Application for Windows    ==
REM ==============================================================================

echo [INFO] Starting the build process...

REM -- Set Environment Variables --
set "APP_NAME=CodeClaw"
set "OUTPUT_DIR=%~dp0dist"
set "NSIS_PATH=C:\Program Files (x86)\NSIS\makensis.exe"
set "GOVERSIONINFO_PATH=%USERPROFILE%\go\bin\goversioninfo.exe"

REM -- Ensure Go tools are in PATH --
set "GOPATH=%USERPROFILE%\go"
set "PATH=%GOPATH%\bin;%PATH%"

REM -- Clean previous build artifacts --
echo [Step 1/7] Cleaning previous build...
if exist "%OUTPUT_DIR%" (
    rmdir /s /q "%OUTPUT_DIR%"
)
mkdir "%OUTPUT_DIR%"

REM -- Increment build number and set version --
echo [Step 2/7] Updating version number...
powershell -NoProfile -Command "if (Test-Path 'build_number') { $n = [int](Get-Content 'build_number') + 1 } else { $n = 1 }; Set-Content -Path 'build_number' -Value $n -NoNewline; Set-Content -Path 'temp_build_num.txt' -Value $n -NoNewline"
set /p BUILD_NUM=<temp_build_num.txt
del temp_build_num.txt
%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -Command "$ver = (Get-Content '%~dp0wails.json' -Raw | ConvertFrom-Json).info.productVersion; $parts = $ver.Split('.'); $bn = Get-Content '%~dp0build_number'; $parts[3] = $bn; $newVer = $parts -join '.'; Set-Content -Path '%~dp0temp_version.txt' -Value $newVer -NoNewline"
set /p VERSION=<temp_version.txt
del temp_version.txt
%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -Command "(Get-Content '%~dp0wails.json' -Raw | ConvertFrom-Json).info.productName | Set-Content -Path '%~dp0temp_product_name.txt' -NoNewline"
set /p PRODUCT_NAME=<temp_product_name.txt
del temp_product_name.txt
%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -Command "(Get-Content '%~dp0wails.json' -Raw | ConvertFrom-Json).info.companyName | Set-Content -Path '%~dp0temp_company_name.txt' -NoNewline"
set /p COMPANY_NAME=<temp_company_name.txt
del temp_company_name.txt
%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -Command "(Get-Content '%~dp0wails.json' -Raw | ConvertFrom-Json).info.copyright | Set-Content -Path '%~dp0temp_copyright.txt' -NoNewline"
set /p COPYRIGHT_TEXT=<temp_copyright.txt
del temp_copyright.txt
echo [INFO] Building Version: %VERSION% 

REM -- Sync version with frontend --
echo [Step 3/7] Syncing version with frontend...
powershell -NoProfile -Command "@('export const buildNumber = ''%BUILD_NUM%'';','export const appVersion = ''%VERSION%'';') | Set-Content -Path '%~dp0frontend\src\version.ts' -Encoding Utf8"

REM -- Build Frontend --
echo [Step 4/7] Building frontend...
cd "%~dp0frontend"
if not exist "node_modules" (
    call npm.cmd install --cache ./.npm_cache
    if !errorlevel! neq 0 (
        echo [ERROR] npm install failed.
        goto :error
    )
)
if exist "dist" ( rmdir /s /q "dist" )
%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -Command "npm run build"
if !errorlevel! neq 0 (
    echo [ERROR] Frontend build failed.
    goto :error
)
cd "%~dp0"

REM -- Generate Windows Resources (icon + version info) --
echo [Step 5/7] Generating Windows resources...
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

powershell -NoProfile -Command "$cfg = Get-Content '%~dp0wails.json' -Raw | ConvertFrom-Json; $parts = '%VERSION%'.Split('.'); if ($parts.Length -ne 4) { throw 'Version must contain 4 numeric parts for Windows resources.' }; $manifest = Get-Content '%~dp0build\windows\wails.exe.manifest' -Raw; $manifest = $manifest.Replace('{{.Name}}', $cfg.name).Replace('{{.Info.ProductVersion}}', '%VERSION%'); [System.IO.File]::WriteAllText('%~dp0build\windows\wails.exe.manifest.tmp', $manifest, [System.Text.UTF8Encoding]::new($false)); $versionInfo = @{ FixedFileInfo = @{ FileVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = [int]$parts[3] }; ProductVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = [int]$parts[3] } }; StringFileInfo = @{ Comments = $cfg.info.comments; CompanyName = $cfg.info.companyName; FileDescription = $cfg.info.productName; FileVersion = '%VERSION%'; InternalName = $cfg.info.productName; LegalCopyright = $cfg.info.copyright; OriginalFilename = '%APP_NAME%.exe'; ProductName = $cfg.info.productName; ProductVersion = '%VERSION%' }; VarFileInfo = @{ Translation = @{ LangID = '0409'; CharsetID = '04B0' } } } | ConvertTo-Json -Depth 6; [System.IO.File]::WriteAllText('%~dp0build\windows\versioninfo.json.tmp', $versionInfo, [System.Text.UTF8Encoding]::new($false))"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to prepare Windows version resource inputs.
    goto :error
)

REM -- Build Go Binaries --
echo [Step 6/7] Compiling Go binaries...
set "GOOS=windows"
set "CGO_ENABLED=0"
set "GOARCH=amd64"
"%GOVERSIONINFO_PATH%" -64 -icon "%~dp0build\windows\icon.ico" -manifest "%~dp0build\windows\wails.exe.manifest.tmp" -o "%~dp0resource_windows_amd64.syso" "%~dp0build\windows\versioninfo.json.tmp"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to generate amd64 resources.
    goto :error
)
go build -tags desktop,production -ldflags "-s -w -H windowsgui" -o "%OUTPUT_DIR%\%APP_NAME%_amd64.exe"
if !errorlevel! neq 0 (
    echo [ERROR] Go build for amd64 failed.
    goto :error
)
del "%~dp0resource_windows_amd64.syso"
set "GOARCH=arm64"
"%GOVERSIONINFO_PATH%" -64 -arm -icon "%~dp0build\windows\icon.ico" -manifest "%~dp0build\windows\wails.exe.manifest.tmp" -o "%~dp0resource_windows_arm64.syso" "%~dp0build\windows\versioninfo.json.tmp"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to generate arm64 resources.
    goto :error
)
go build -tags desktop,production -ldflags "-s -w -H windowsgui" -o "%OUTPUT_DIR%\%APP_NAME%_arm64.exe"
if !errorlevel! neq 0 (
    echo [ERROR] Go build for arm64 failed.
    goto :error
)
del "%~dp0resource_windows_arm64.syso"
del "%~dp0build\windows\wails.exe.manifest.tmp"
del "%~dp0build\windows\versioninfo.json.tmp"

REM Reset Env for NSIS
set "GOOS="
set "GOARCH="
set "CGO_ENABLED="
set "CC="
set "CXX="

REM -- Create NSIS Installer --
echo [Step 7/7] Creating NSIS installer...
if not exist "%NSIS_PATH%" goto nsis_missing

"%NSIS_PATH%" /DINFO_PRODUCTNAME="%PRODUCT_NAME%" /DINFO_COMPANYNAME="%COMPANY_NAME%" /DINFO_COPYRIGHT="%COPYRIGHT_TEXT%" /DINFO_PRODUCTVERSION="%VERSION%" /DARG_WAILS_AMD64_BINARY="%OUTPUT_DIR%\%APP_NAME%_amd64.exe" /DARG_WAILS_ARM64_BINARY="%OUTPUT_DIR%\%APP_NAME%_arm64.exe" "%~dp0build\windows\installer\multiarch.nsi"
if !errorlevel! neq 0 (
    echo [ERROR] NSIS installer creation failed.
    goto :error
)

if exist "%OUTPUT_DIR%\%APP_NAME%-Setup.exe" (
    echo [SUCCESS] Windows installer created at: %OUTPUT_DIR%\%APP_NAME%-Setup.exe
)

REM -- Copy/Rename Main Binary for convenience --
echo   - Creating main executable copy (amd64)...
copy /Y "%OUTPUT_DIR%\%APP_NAME%_amd64.exe" "%OUTPUT_DIR%\%APP_NAME%.exe" >nul
if exist "%OUTPUT_DIR%\%APP_NAME%.exe" (
    echo [SUCCESS] Windows main binary created: %OUTPUT_DIR%\%APP_NAME%.exe
    
    echo   - Creating Windows portable zip...
    powershell -Command "Compress-Archive -Path '%OUTPUT_DIR%\%APP_NAME%.exe' -DestinationPath '%OUTPUT_DIR%\%APP_NAME%-Windows-Portable.zip' -Force"
)

goto :success

:nsis_missing
echo [ERROR] NSIS not found at "%NSIS_PATH%". Please install NSIS.
goto :error

:success
echo.
echo [SUCCESS] Build and packaging complete!
echo Artifacts are in: %OUTPUT_DIR%
goto :eof

:error
echo.
echo [FAILED] The build process failed. Please check the output above for errors.
pause
exit /b 1

endlocal
