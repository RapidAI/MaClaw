@echo off
setlocal EnableDelayedExpansion

REM ==============================================================================
REM == Batch Script to Build and Package MaClaw (GUI + TUI/CLI + maclaw-cli + DataSrv) for Windows ==
REM ==============================================================================

echo [INFO] Starting the build process...

REM -- Set Environment Variables --
set "APP_NAME=MaClaw"
set "OUTPUT_DIR=%~dp0dist"
set "NSIS_PATH=C:\Program Files (x86)\NSIS\makensis.exe"
set "GOVERSIONINFO_PATH=%USERPROFILE%\go\bin\goversioninfo.exe"

REM -- Ensure Go tools are in PATH --
set "GOPATH=%USERPROFILE%\go"
set "PATH=%GOPATH%\bin;%PATH%"
set "GOMAXPROCS=1"

REM -- Clean previous MaClaw build artifacts (preserve other brands' files) --
REM    Note: maclawsrv and maclaw-data-srv binary names are shared across brands.
REM    Building MaClaw will overwrite MetaStaff's service binaries (same names, different build tags).
echo [Step 1/14] Cleaning previous MaClaw build...
if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"
del /q "%OUTPUT_DIR%\%APP_NAME%.exe" 2>nul
del /q "%OUTPUT_DIR%\%APP_NAME%_amd64.exe" 2>nul
del /q "%OUTPUT_DIR%\%APP_NAME%_arm64.exe" 2>nul
del /q "%OUTPUT_DIR%\%APP_NAME%-Setup.exe" 2>nul
del /q "%OUTPUT_DIR%\%APP_NAME%-Windows-Portable.zip" 2>nul
del /q "%OUTPUT_DIR%\maclaw-tui*.exe" 2>nul
del /q "%OUTPUT_DIR%\maclaw-cli*.exe" 2>nul
del /q "%OUTPUT_DIR%\maclaw-tool*.exe" 2>nul
del /q "%OUTPUT_DIR%\maclawsrv*.exe" 2>nul
del /q "%OUTPUT_DIR%\maclawsrv-Setup.exe" 2>nul
del /q "%OUTPUT_DIR%\maclaw-data-srv*.exe" 2>nul
del /q "%OUTPUT_DIR%\maclaw-data-srv-Setup.exe" 2>nul

REM -- Increment build number and set version (single PowerShell call) --
echo [Step 2/14] Updating version number...
%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -Command ^
  "$root = '%~dp0'; if (Test-Path ($root + 'build_number')) { $n = [int](Get-Content ($root + 'build_number')) + 1 } else { $n = 1 }; Set-Content -Path ($root + 'build_number') -Value $n -NoNewline; $cfg = Get-Content ($root + 'wails.json') -Raw | ConvertFrom-Json; $parts = $cfg.info.productVersion.Split('.'); $parts[3] = [string]$n; $ver = $parts -join '.'; Set-Content -Path ($root + 'temp_VERSION.txt') -Value $ver -NoNewline; Set-Content -Path ($root + 'temp_BUILD_NUM.txt') -Value ([string]$n) -NoNewline; Set-Content -Path ($root + 'temp_PRODUCT_NAME.txt') -Value '%APP_NAME%' -NoNewline; Set-Content -Path ($root + 'temp_COMPANY_NAME.txt') -Value $cfg.info.companyName -NoNewline; Set-Content -Path ($root + 'temp_COPYRIGHT.txt') -Value $cfg.info.copyright -NoNewline"
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
setlocal DisableDelayedExpansion
powershell -NoProfile -Command "$utf8NoBom = [System.Text.UTF8Encoding]::new($false); $content = @('!define INFO_PROJECTNAME ''%APP_NAME%''','!define PRODUCT_EXECUTABLE ''%APP_NAME%.exe''','!define INFO_PRODUCTNAME ''%PRODUCT_NAME%''','!define INFO_COMPANYNAME ''%COMPANY_NAME%''','!define INFO_COPYRIGHT ''%COPYRIGHT_TEXT%''','!define INFO_PRODUCTVERSION ''%VERSION%''','!define ARG_WAILS_AMD64_BINARY ''%OUTPUT_DIR%\%APP_NAME%_amd64.exe''','!define ARG_WAILS_ARM64_BINARY ''%OUTPUT_DIR%\%APP_NAME%_arm64.exe''','!define ARG_MACLAWCLI_AMD64_BINARY ''%OUTPUT_DIR%\maclaw-cli_amd64.exe''','!define ARG_MACLAWCLI_ARM64_BINARY ''%OUTPUT_DIR%\maclaw-cli_arm64.exe''') -join [Environment]::NewLine; [System.IO.File]::WriteAllText('%~dp0build\windows\installer\build_params.nsh.tmp', $content, $utf8NoBom)"
endlocal
echo [INFO] Building Version: %VERSION%

REM -- Sync version with frontend --
echo [Step 3/14] Syncing version with frontend...
powershell -NoProfile -Command "@('export const buildNumber = ''%BUILD_NUM%'';','export const appVersion = ''%VERSION%'';') | Set-Content -Path '%~dp0gui\frontend\src\version.ts' -Encoding Utf8"

REM -- Build Frontend --
echo [Step 4/14] Building frontend...
cd /d "%~dp0gui\frontend"
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
echo [Step 5/14] Generating Windows resources...
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

powershell -NoProfile -Command "$cfg = Get-Content '%~dp0wails.json' -Raw | ConvertFrom-Json; $parts = '%VERSION%'.Split('.'); if ($parts.Length -ne 4) { throw 'Version must contain 4 numeric parts for Windows resources.' }; $safeName = ($cfg.name -replace '[^a-zA-Z0-9._-]',''); if (-not $safeName) { $safeName = 'MaClaw' }; $clampedBuild = [Math]::Min([int]$parts[3], 65534); $manifestVer = $parts[0]+'.'+$parts[1]+'.'+$parts[2]+'.'+$clampedBuild; $manifest = Get-Content '%~dp0build\windows\wails.exe.manifest' -Raw; $manifest = $manifest.Replace('{{.Name}}', $safeName).Replace('{{.Info.ProductVersion}}', $manifestVer); [System.IO.File]::WriteAllText('%~dp0build\windows\wails.exe.manifest.tmp', $manifest, [System.Text.UTF8Encoding]::new($false)); $versionInfo = @{ FixedFileInfo = @{ FileVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = $clampedBuild }; ProductVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = $clampedBuild } }; StringFileInfo = @{ Comments = $cfg.info.comments; CompanyName = $cfg.info.companyName; FileDescription = $cfg.info.productName; FileVersion = '%VERSION%'; InternalName = $cfg.info.productName; LegalCopyright = $cfg.info.copyright; OriginalFilename = '%APP_NAME%.exe'; ProductName = $cfg.info.productName; ProductVersion = '%VERSION%' }; VarFileInfo = @{ Translation = @{ LangID = '0409'; CharsetID = '04B0' } } } | ConvertTo-Json -Depth 6; [System.IO.File]::WriteAllText('%~dp0build\windows\versioninfo.json.tmp', $versionInfo, [System.Text.UTF8Encoding]::new($false))"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to prepare Windows version resource inputs.
    goto :error
)

REM -- Build Go Binaries --
echo [Step 6/14] Compiling GUI binaries...
REM -- Kill stale processes and clean locked Go temp dirs to prevent "Access is denied" errors --
taskkill /F /IM %APP_NAME%.exe 2>nul
taskkill /F /IM maclaw-tui.exe 2>nul
taskkill /F /IM maclaw-cli.exe 2>nul
taskkill /F /IM maclaw-tool.exe 2>nul
taskkill /F /IM maclawsrv.exe 2>nul
taskkill /F /IM maclaw-data-srv.exe 2>nul
%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -Command "Get-ChildItem $env:TEMP -Filter 'go-build*' -Directory -ErrorAction SilentlyContinue | Where-Object { $_.LastWriteTime -lt (Get-Date).AddMinutes(-2) } | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue"
set "GOOS=windows"
set "GOARCH=amd64"
set "CGO_ENABLED=0"
set "CC="
"%GOVERSIONINFO_PATH%" -64 -icon "%~dp0build\windows\icon.ico" -manifest "%~dp0build\windows\wails.exe.manifest.tmp" -o "%~dp0gui\resource_windows_amd64.syso" "%~dp0build\windows\versioninfo.json.tmp"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to generate amd64 resources.
    goto :error
)
call :go_build -p 1 -tags desktop,production -ldflags "-s -w -H windowsgui -X main.version=%VERSION%" -o "%OUTPUT_DIR%\%APP_NAME%_amd64.exe" ./gui/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for GUI amd64 failed.
    goto :error
)
del "%~dp0gui\resource_windows_amd64.syso"
set "GOARCH=arm64"
set "CGO_ENABLED=0"
set "CC="
if not exist "%~dp0build\windows\wails.exe.manifest.tmp" powershell -NoProfile -Command "$cfg = Get-Content '%~dp0wails.json' -Raw | ConvertFrom-Json; $parts = '%VERSION%'.Split('.'); if ($parts.Length -ne 4) { throw 'Version must contain 4 numeric parts for Windows resources.' }; $safeName = ($cfg.name -replace '[^a-zA-Z0-9._-]',''); if (-not $safeName) { $safeName = 'MaClaw' }; $clampedBuild = [Math]::Min([int]$parts[3], 65534); $manifestVer = $parts[0]+'.'+$parts[1]+'.'+$parts[2]+'.'+$clampedBuild; $manifest = Get-Content '%~dp0build\windows\wails.exe.manifest' -Raw; $manifest = $manifest.Replace('{{.Name}}', $safeName).Replace('{{.Info.ProductVersion}}', $manifestVer); [System.IO.File]::WriteAllText('%~dp0build\windows\wails.exe.manifest.tmp', $manifest, [System.Text.UTF8Encoding]::new($false))"
if not exist "%~dp0build\windows\versioninfo.json.tmp" powershell -NoProfile -Command "$cfg = Get-Content '%~dp0wails.json' -Raw | ConvertFrom-Json; $parts = '%VERSION%'.Split('.'); if ($parts.Length -ne 4) { throw 'Version must contain 4 numeric parts for Windows resources.' }; $clampedBuild = [Math]::Min([int]$parts[3], 65534); $versionInfo = @{ FixedFileInfo = @{ FileVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = $clampedBuild }; ProductVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = $clampedBuild } }; StringFileInfo = @{ Comments = $cfg.info.comments; CompanyName = $cfg.info.companyName; FileDescription = $cfg.info.productName; FileVersion = '%VERSION%'; InternalName = $cfg.info.productName; LegalCopyright = $cfg.info.copyright; OriginalFilename = '%APP_NAME%.exe'; ProductName = $cfg.info.productName; ProductVersion = '%VERSION%' }; VarFileInfo = @{ Translation = @{ LangID = '0409'; CharsetID = '04B0' } } } | ConvertTo-Json -Depth 6; [System.IO.File]::WriteAllText('%~dp0build\windows\versioninfo.json.tmp', $versionInfo, [System.Text.UTF8Encoding]::new($false))"
"%GOVERSIONINFO_PATH%" -64 -arm -icon "%~dp0build\windows\icon.ico" -manifest "%~dp0build\windows\wails.exe.manifest.tmp" -o "%~dp0gui\resource_windows_arm64.syso" "%~dp0build\windows\versioninfo.json.tmp"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to generate arm64 resources.
    goto :error
)
call :go_build -p 1 -tags desktop,production -ldflags "-s -w -H windowsgui -X main.version=%VERSION%" -o "%OUTPUT_DIR%\%APP_NAME%_arm64.exe" ./gui/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for GUI arm64 failed.
    goto :error
)
del "%~dp0gui\resource_windows_arm64.syso"
del "%~dp0build\windows\wails.exe.manifest.tmp"
del "%~dp0build\windows\versioninfo.json.tmp"

REM -- Build TUI/CLI Binaries --
echo [Step 7/14] Compiling TUI/CLI binaries...
set "CGO_ENABLED=0"
set "GOARCH=amd64"
call :go_build -p 1 -ldflags "-s -w -X main.version=%VERSION%" -o "%OUTPUT_DIR%\maclaw-tui_amd64.exe" ./tui/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for TUI amd64 failed.
    goto :error
)
set "GOARCH=arm64"
call :go_build -p 1 -ldflags "-s -w -X main.version=%VERSION%" -o "%OUTPUT_DIR%\maclaw-tui_arm64.exe" ./tui/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for TUI arm64 failed.
    goto :error
)

REM -- Build maclaw-tool Binary --
echo [Step 8/14] Compiling maclaw-tool binaries...
set "GOARCH=amd64"
call :go_build -p 1 -ldflags "-s -w -X main.version=%VERSION%" -o "%OUTPUT_DIR%\maclaw-tool_amd64.exe" ./cmd/maclaw-tool/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for maclaw-tool amd64 failed.
    goto :error
)
set "GOARCH=arm64"
call :go_build -p 1 -ldflags "-s -w -X main.version=%VERSION%" -o "%OUTPUT_DIR%\maclaw-tool_arm64.exe" ./cmd/maclaw-tool/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for maclaw-tool arm64 failed.
    goto :error
)

REM -- Build MaClaw Service Binary --
echo [Step 9/14] Compiling maclawsrv binaries...
set "GOARCH=amd64"
call :go_build -p 1 -ldflags "-s -w -X main.serviceVersion=%VERSION%" -o "%OUTPUT_DIR%\maclawsrv_amd64.exe" ./MaClawSrv/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for maclawsrv amd64 failed.
    goto :error
)
set "GOARCH=arm64"
call :go_build -p 1 -ldflags "-s -w -X main.serviceVersion=%VERSION%" -o "%OUTPUT_DIR%\maclawsrv_arm64.exe" ./MaClawSrv/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for maclawsrv arm64 failed.
    goto :error
)

REM -- Build MaClaw Data Service Binary --
echo [Step 10/14] Compiling maclaw-data-srv binaries...
set "GOARCH=amd64"
call :go_build_datasrv -p 1 -ldflags "-s -w -X main.serviceVersion=%VERSION%" -o "%OUTPUT_DIR%\maclaw-data-srv_amd64.exe" ./cmd/maclaw-data-srv/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for maclaw-data-srv amd64 failed.
    goto :error
)
set "GOARCH=arm64"
call :go_build_datasrv -p 1 -ldflags "-s -w -X main.serviceVersion=%VERSION%" -o "%OUTPUT_DIR%\maclaw-data-srv_arm64.exe" ./cmd/maclaw-data-srv/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for maclaw-data-srv arm64 failed.
    goto :error
)

REM -- Build maclaw-cli Binary --
echo [Step 11/14] Compiling maclaw-cli binaries...
set "GOARCH=amd64"
call :go_build -p 1 -ldflags "-s -w -X main.cliVersion=%VERSION%" -o "%OUTPUT_DIR%\maclaw-cli_amd64.exe" ./maclaw-cli/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for maclaw-cli amd64 failed.
    goto :error
)
set "GOARCH=arm64"
call :go_build -p 1 -ldflags "-s -w -X main.cliVersion=%VERSION%" -o "%OUTPUT_DIR%\maclaw-cli_arm64.exe" ./maclaw-cli/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for maclaw-cli arm64 failed.
    goto :error
)

REM Reset Env for NSIS
set "GOOS="
set "GOARCH="
set "CGO_ENABLED="
set "CC="
set "CXX="

REM -- Create NSIS Installer --
echo [Step 12/14] Creating NSIS installer...
if not exist "%NSIS_PATH%" goto nsis_missing

"%NSIS_PATH%" "%~dp0build\windows\installer\multiarch.nsi"
if !errorlevel! neq 0 (
    echo [ERROR] NSIS installer creation failed.
    goto :error
)
del /q "%~dp0build\windows\installer\build_params.nsh.tmp" 2>nul

if exist "%OUTPUT_DIR%\%APP_NAME%-Setup.exe" (
    echo [SUCCESS] Windows installer created at: %OUTPUT_DIR%\%APP_NAME%-Setup.exe
)

REM -- Create standalone DataSrv NSIS installer --
echo [Step 13/14] Creating standalone maclawsrv NSIS installer...
setlocal DisableDelayedExpansion
powershell -NoProfile -Command "$utf8NoBom = [System.Text.UTF8Encoding]::new($false); $content = @('!define INFO_PRODUCTNAME ''MaClaw Service''','!define INFO_COMPANYNAME ''%COMPANY_NAME%''','!define INFO_COPYRIGHT ''%COPYRIGHT_TEXT%''','!define INFO_PRODUCTVERSION ''%VERSION%''','!define PRODUCT_EXECUTABLE ''maclawsrv.exe''','!define ARG_MACLAWSRV_AMD64_BINARY ''%OUTPUT_DIR%\maclawsrv_amd64.exe''','!define ARG_MACLAWSRV_ARM64_BINARY ''%OUTPUT_DIR%\maclawsrv_arm64.exe''') -join [Environment]::NewLine; [System.IO.File]::WriteAllText('%~dp0build\windows\installer\maclawsrv_build_params.nsh.tmp', $content, $utf8NoBom)"
endlocal
if !errorlevel! neq 0 (
    echo [ERROR] Failed to prepare maclawsrv installer parameters.
    goto :error
)
"%NSIS_PATH%" "%~dp0build\windows\installer\maclawsrv.nsi"
if !errorlevel! neq 0 (
    echo [ERROR] maclawsrv NSIS installer creation failed.
    goto :error
)
del /q "%~dp0build\windows\installer\maclawsrv_build_params.nsh.tmp" 2>nul
if exist "%OUTPUT_DIR%\maclawsrv-Setup.exe" (
    echo [SUCCESS] maclawsrv Windows installer created at: %OUTPUT_DIR%\maclawsrv-Setup.exe
)

REM -- Create standalone DataSrv NSIS installer --
echo [Step 14/14] Creating standalone maclaw-data-srv NSIS installer...
setlocal DisableDelayedExpansion
powershell -NoProfile -Command "$utf8NoBom = [System.Text.UTF8Encoding]::new($false); $content = @('!define INFO_PRODUCTNAME ''MaClaw Data Service''','!define INFO_COMPANYNAME ''%COMPANY_NAME%''','!define INFO_COPYRIGHT ''%COPYRIGHT_TEXT%''','!define INFO_PRODUCTVERSION ''%VERSION%''','!define PRODUCT_EXECUTABLE ''maclaw-data-srv.exe''','!define ARG_DATASRV_AMD64_BINARY ''%OUTPUT_DIR%\maclaw-data-srv_amd64.exe''','!define ARG_DATASRV_ARM64_BINARY ''%OUTPUT_DIR%\maclaw-data-srv_arm64.exe''') -join [Environment]::NewLine; [System.IO.File]::WriteAllText('%~dp0build\windows\installer\datasrv_build_params.nsh.tmp', $content, $utf8NoBom)"
endlocal
if !errorlevel! neq 0 (
    echo [ERROR] Failed to prepare DataSrv installer parameters.
    goto :error
)
"%NSIS_PATH%" "%~dp0build\windows\installer\datasrv.nsi"
if !errorlevel! neq 0 (
    echo [ERROR] DataSrv NSIS installer creation failed.
    goto :error
)
del /q "%~dp0build\windows\installer\datasrv_build_params.nsh.tmp" 2>nul
if exist "%OUTPUT_DIR%\maclaw-data-srv-Setup.exe" (
    echo [SUCCESS] DataSrv Windows installer created at: %OUTPUT_DIR%\maclaw-data-srv-Setup.exe
)

REM -- Copy/Rename Main Binaries for convenience --
echo   - Creating main executable copies (amd64)...
copy /Y "%OUTPUT_DIR%\%APP_NAME%_amd64.exe" "%OUTPUT_DIR%\%APP_NAME%.exe" >nul
copy /Y "%OUTPUT_DIR%\maclaw-tui_amd64.exe" "%OUTPUT_DIR%\maclaw-tui.exe" >nul
copy /Y "%OUTPUT_DIR%\maclaw-cli_amd64.exe" "%OUTPUT_DIR%\maclaw-cli.exe" >nul
copy /Y "%OUTPUT_DIR%\maclaw-tool_amd64.exe" "%OUTPUT_DIR%\maclaw-tool.exe" >nul
copy /Y "%OUTPUT_DIR%\maclawsrv_amd64.exe" "%OUTPUT_DIR%\maclawsrv.exe" >nul
copy /Y "%OUTPUT_DIR%\maclaw-data-srv_amd64.exe" "%OUTPUT_DIR%\maclaw-data-srv.exe" >nul

if exist "%OUTPUT_DIR%\%APP_NAME%.exe" (
    echo [SUCCESS] GUI binary: %OUTPUT_DIR%\%APP_NAME%.exe
)
if exist "%OUTPUT_DIR%\maclaw-tui.exe" (
    echo [SUCCESS] TUI/CLI binary: %OUTPUT_DIR%\maclaw-tui.exe
)
if exist "%OUTPUT_DIR%\maclaw-cli.exe" (
    echo [SUCCESS] maclaw-cli binary: %OUTPUT_DIR%\maclaw-cli.exe
)
if exist "%OUTPUT_DIR%\maclaw-tool.exe" (
    echo [SUCCESS] maclaw-tool binary: %OUTPUT_DIR%\maclaw-tool.exe
)
if exist "%OUTPUT_DIR%\maclawsrv.exe" (
    echo [SUCCESS] maclawsrv binary: %OUTPUT_DIR%\maclawsrv.exe
)
if exist "%OUTPUT_DIR%\maclaw-data-srv.exe" (
    echo [SUCCESS] maclaw-data-srv binary: %OUTPUT_DIR%\maclaw-data-srv.exe
)

goto :success

:go_build
set "GO_BUILD_ATTEMPT=1"
:go_build_retry
go build %*
if !errorlevel! equ 0 exit /b 0
set "GO_BUILD_ERROR=!errorlevel!"
if !GO_BUILD_ATTEMPT! geq 8 exit /b !GO_BUILD_ERROR!
echo [WARN] go build failed with exit code !GO_BUILD_ERROR!, retrying...
%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -Command "Get-Process go,compile,link,gcc -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue; Start-Sleep -Milliseconds 500; Get-ChildItem $env:TEMP -Filter 'go-build*' -Directory -ErrorAction SilentlyContinue | Where-Object { $_.LastWriteTime -lt (Get-Date).AddMinutes(-1) } | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue"
set /a GO_BUILD_ATTEMPT+=1
goto :go_build_retry

:go_build_datasrv
set "GO_BUILD_ATTEMPT=1"
:go_build_datasrv_retry
go -C "%~dp0datasrv" build %*
if !errorlevel! equ 0 exit /b 0
set "GO_BUILD_ERROR=!errorlevel!"
if !GO_BUILD_ATTEMPT! geq 8 exit /b !GO_BUILD_ERROR!
echo [WARN] datasrv go build failed with exit code !GO_BUILD_ERROR!, retrying...
%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -Command "Get-Process go,compile,link,gcc -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue; Start-Sleep -Milliseconds 500; Get-ChildItem $env:TEMP -Filter 'go-build*' -Directory -ErrorAction SilentlyContinue | Where-Object { $_.LastWriteTime -lt (Get-Date).AddMinutes(-1) } | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue"
set /a GO_BUILD_ATTEMPT+=1
goto :go_build_datasrv_retry

:nsis_missing
echo [ERROR] NSIS not found at "%NSIS_PATH%". Please install NSIS.
goto :error

:success
echo.
echo [SUCCESS] Build and packaging complete!
echo Artifacts are in: %OUTPUT_DIR%
endlocal & exit /b 0

:error
echo.
echo [FAILED] The build process failed. Please check the output above for errors.
endlocal & exit /b 1

