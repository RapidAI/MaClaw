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
set "POWERSHELL_EXE=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
set "NPM_CMD=C:\Program Files\nodejs\npm.cmd"
set "NPM_NODE=C:\Program Files\nodejs\node.exe"
set "NPM_CLI=C:\Program Files\nodejs\node_modules\npm\bin\npm-cli.js"
set "GO_EXE=C:\Program Files\Go\bin\go.exe"

REM -- Ensure Go tools are in PATH --
set "GOPATH=%USERPROFILE%\go"
set "PATH=%SystemRoot%\System32;%SystemRoot%;C:\Program Files\nodejs;C:\Program Files\Go\bin;%GOPATH%\bin"
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
del /q "%OUTPUT_DIR%\maclaw-acp-bridge*.exe" 2>nul
del /q "%OUTPUT_DIR%\maclaw-tool*.exe" 2>nul
del /q "%OUTPUT_DIR%\maclawsrv*.exe" 2>nul
del /q "%OUTPUT_DIR%\maclawsrv-Setup.exe" 2>nul
del /q "%OUTPUT_DIR%\maclaw-data-srv*.exe" 2>nul
del /q "%OUTPUT_DIR%\maclaw-data-srv-Setup.exe" 2>nul

REM -- Increment build number and set version (single PowerShell call) --
echo [Step 2/14] Updating version number...
"%POWERSHELL_EXE%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\update_build_version.ps1" -Root "%~dp0." -AppName "%APP_NAME%"
if errorlevel 1 goto :error
set /p BUILD_NUM=<"%~dp0temp_BUILD_NUM.txt"
set /p VERSION=<"%~dp0temp_VERSION.txt"
set /p PRODUCT_NAME=<"%~dp0temp_PRODUCT_NAME.txt"
set /p COMPANY_NAME=<"%~dp0temp_COMPANY_NAME.txt"
set /p COPYRIGHT_TEXT=<"%~dp0temp_COPYRIGHT.txt"
del /q "%~dp0temp_BUILD_NUM.txt" "%~dp0temp_VERSION.txt" "%~dp0temp_PRODUCT_NAME.txt" "%~dp0temp_COMPANY_NAME.txt" "%~dp0temp_COPYRIGHT.txt" 2>nul
echo [INFO] Building Version: %VERSION%

REM -- Sync version with frontend --
echo [Step 3/14] Syncing version with frontend...
"%POWERSHELL_EXE%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\sync_frontend_version.ps1" -Root "%~dp0." -BuildNumber "%BUILD_NUM%" -Version "%VERSION%"

REM -- Build Frontend --
echo [Step 4/14] Building frontend...
cd /d "%~dp0guiapp\frontend"
if not exist "node_modules" call "%NPM_CMD%" install --cache ./.npm_cache
if errorlevel 1 goto :error
if exist "dist" rmdir /s /q "dist"
call "%NPM_CMD%" run build
if errorlevel 1 goto :error
echo [INFO] Frontend build completed.
cd "%~dp0"
REM Keep local packaging aligned with GitHub workflow: reject stale/old AI welcome page.
echo [Step 4b/14] Verifying AI assistant welcome page in frontend dist...
node "%~dp0scripts\verify-frontend-welcome.mjs" --dist "%~dp0guiapp\frontend\dist"
if errorlevel 1 (
    echo [ERROR] Frontend welcome verification failed.
    goto :error
)

REM -- Generate Windows Resources (icon + version info) --
echo [Step 5/14] Generating Windows resources...
del /q "%~dp0guiapp\resource_windows_*.syso" 2>nul
del /q "%~dp0guiapp\wails_window_icon_*.syso" 2>nul
REM Only remove the legacy Wails resource object for this product. A wildcard
REM here could delete a separately maintained resource object in guiapp\.
del /q "%~dp0guiapp\MaClaw-res.syso" 2>nul
del /q "%~dp0resource_windows_*.syso" 2>nul
del /q "%~dp0tmp*.syso" 2>nul
del /q "%~dp0tmp*.json" 2>nul
del /q "%~dp0build\windows\wails.exe.manifest.tmp" 2>nul
del /q "%~dp0build\windows\versioninfo.json.tmp" 2>nul

"%POWERSHELL_EXE%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\prepare_windows_resources.ps1" -Root "%~dp0." -Version "%VERSION%" -AppName "%APP_NAME%"
if errorlevel 1 (
    echo [ERROR] Failed to prepare Windows version resource inputs.
    goto :error
)

REM -- Refresh first-party VS Code extension VSIX BEFORE the GUI build embeds it (best-effort; committed asset is fallback) --
echo [Step 5b/14] Refreshing VS Code extension VSIX (best-effort)...
"%POWERSHELL_EXE%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0vscode-ext\build-vsix.ps1" || echo [WARN] VSIX refresh skipped; using committed guiapp/vscode_ext_asset/maclaw-acp.vsix

REM -- Build Go Binaries --
echo [Step 6/14] Compiling GUI binaries...
REM -- Kill stale processes and clean locked Go temp dirs to prevent "Access is denied" errors --
taskkill /F /IM %APP_NAME%.exe 2>nul
taskkill /F /IM maclaw-tui.exe 2>nul
taskkill /F /IM maclaw-cli.exe 2>nul
taskkill /F /IM maclaw-tool.exe 2>nul
taskkill /F /IM maclawsrv.exe 2>nul
taskkill /F /IM maclaw-data-srv.exe 2>nul
"%POWERSHELL_EXE%" -NoProfile -Command "Get-ChildItem $env:TEMP -Filter 'go-build*' -Directory -ErrorAction SilentlyContinue | Where-Object { $_.LastWriteTime -lt (Get-Date).AddMinutes(-2) } | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue"
set "GOOS=windows"
set "GOARCH=amd64"
set "CGO_ENABLED=0"
set "CC="
call :generate_windows_resources "amd64" "%~dp0guiapp\resource_windows_amd64.syso"
if errorlevel 1 goto :error
call :go_build -p 1 -tags desktop,production -ldflags "-s -w -H windowsgui -X main.version=%VERSION%" -o "%OUTPUT_DIR%\%APP_NAME%_amd64.exe" ./cmd/maclaw-gui/
if errorlevel 1 (
    echo [ERROR] Go build for GUI amd64 failed.
    goto :error
)
node "%~dp0scripts\verify-frontend-welcome.mjs" --binary "%OUTPUT_DIR%\%APP_NAME%_amd64.exe"
if errorlevel 1 (
    echo [ERROR] GUI amd64 welcome embed verification failed.
    goto :error
)
call :verify_windows_resources "%OUTPUT_DIR%\%APP_NAME%_amd64.exe"
if errorlevel 1 goto :error
del "%~dp0guiapp\resource_windows_amd64.syso"
set "GOARCH=arm64"
set "CGO_ENABLED=0"
set "CC="
if not exist "%~dp0build\windows\wails.exe.manifest.tmp" (
    echo [ERROR] Windows manifest resource input is missing.
    goto :error
)
if not exist "%~dp0build\windows\versioninfo.json.tmp" (
    echo [ERROR] Windows version resource input is missing.
    goto :error
)
call :generate_windows_resources "arm64" "%~dp0guiapp\resource_windows_arm64.syso"
if errorlevel 1 goto :error
call :go_build -p 1 -tags desktop,production -ldflags "-s -w -H windowsgui -X main.version=%VERSION%" -o "%OUTPUT_DIR%\%APP_NAME%_arm64.exe" ./cmd/maclaw-gui/
if errorlevel 1 (
    echo [ERROR] Go build for GUI arm64 failed.
    goto :error
)
node "%~dp0scripts\verify-frontend-welcome.mjs" --binary "%OUTPUT_DIR%\%APP_NAME%_arm64.exe"
if errorlevel 1 (
    echo [ERROR] GUI arm64 welcome embed verification failed.
    goto :error
)
call :verify_windows_resources "%OUTPUT_DIR%\%APP_NAME%_arm64.exe"
if errorlevel 1 goto :error
del "%~dp0guiapp\resource_windows_arm64.syso"
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

REM -- Build maclaw-acp-bridge (VS Code ACP attach to GUI) --
echo [Step 11a/14] Compiling maclaw-acp-bridge binaries...
set "GOARCH=amd64"
call :go_build -p 1 -ldflags "-s -w -X main.version=%VERSION%" -o "%OUTPUT_DIR%\maclaw-acp-bridge_amd64.exe" ./cmd/maclaw-acp-bridge/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for maclaw-acp-bridge amd64 failed.
    goto :error
)
set "GOARCH=arm64"
call :go_build -p 1 -ldflags "-s -w -X main.version=%VERSION%" -o "%OUTPUT_DIR%\maclaw-acp-bridge_arm64.exe" ./cmd/maclaw-acp-bridge/
if !errorlevel! neq 0 (
    echo [ERROR] Go build for maclaw-acp-bridge arm64 failed.
    goto :error
)

REM Reset Env for NSIS
set "GOOS="
set "GOARCH="
set "CGO_ENABLED="
set "CC="
set "CXX="

REM -- Build Computer Use UIA sidecar (precompiled for installers / portable) --
echo [Step 11b/14] Building maclaw-uia-sidecar.exe...
"%POWERSHELL_EXE%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\build_uia_sidecar.ps1" -OutDir "%OUTPUT_DIR%"
if !errorlevel! neq 0 (
    echo [WARN] UIA sidecar build failed — runtime will fall back to PowerShell / on-demand csc.
) else (
    echo [SUCCESS] UIA sidecar: %OUTPUT_DIR%\maclaw-uia-sidecar.exe
)

REM -- Create NSIS Installer --
if /i "%~1"=="compile-only" goto copy_binaries
echo [Step 12/14] Creating NSIS installer...
if not exist "%NSIS_PATH%" goto nsis_missing

"%POWERSHELL_EXE%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\write_installer_params.ps1" -Root "%~dp0."
if errorlevel 1 goto :error
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
"%POWERSHELL_EXE%" -NoProfile -Command "$utf8NoBom = [System.Text.UTF8Encoding]::new($false); $content = @('!define INFO_PRODUCTNAME `MaClaw Service`','!define INFO_COMPANYNAME `%COMPANY_NAME%`','!define INFO_COPYRIGHT `%COPYRIGHT_TEXT%`','!define INFO_PRODUCTVERSION `%VERSION%`','!define PRODUCT_EXECUTABLE `maclawsrv.exe`','!define ARG_MACLAWSRV_AMD64_BINARY `%OUTPUT_DIR%\maclawsrv_amd64.exe`','!define ARG_MACLAWSRV_ARM64_BINARY `%OUTPUT_DIR%\maclawsrv_arm64.exe`') -join [Environment]::NewLine; [System.IO.File]::WriteAllText('%~dp0build\windows\installer\maclawsrv_build_params.nsh.tmp', $content, $utf8NoBom)"
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
"%POWERSHELL_EXE%" -NoProfile -Command "$utf8NoBom = [System.Text.UTF8Encoding]::new($false); $path = '%~dp0build\windows\installer\datasrv_build_params.nsh.tmp'; $content = @('!define INFO_PRODUCTNAME `MaClaw Data Service`','!define INFO_COMPANYNAME `%COMPANY_NAME%`','!define INFO_COPYRIGHT `%COPYRIGHT_TEXT%`','!define INFO_PRODUCTVERSION `%VERSION%`','!define PRODUCT_EXECUTABLE `maclaw-data-srv.exe`','!define ARG_DATASRV_AMD64_BINARY `%OUTPUT_DIR%\maclaw-data-srv_amd64.exe`','!define ARG_DATASRV_ARM64_BINARY `%OUTPUT_DIR%\maclaw-data-srv_arm64.exe`') -join [Environment]::NewLine; for ($i = 0; $i -lt 8; $i++) { try { [System.IO.File]::WriteAllText($path, $content, $utf8NoBom); exit 0 } catch { Start-Sleep -Milliseconds 300 } }; throw 'Failed to write datasrv_build_params.nsh.tmp after retries.'"
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
:copy_binaries
if /i "%~1"=="compile-only" echo [INFO] Compile-only mode: skipping NSIS installers.
echo   - Creating main executable copies (amd64)...
REM The running GUI keeps its executable locked. Do not report a stale binary as
REM successful: retry briefly, then stop with a clear actionable error so the
REM caller launches the newly-built MaClaw_amd64.exe or closes MaClaw and retries.
call :copy_main_gui "%OUTPUT_DIR%\%APP_NAME%_amd64.exe" "%OUTPUT_DIR%\%APP_NAME%.exe"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to update %OUTPUT_DIR%\%APP_NAME%.exe because it is in use.
    echo [ERROR] Close MaClaw and rerun this script, or launch %OUTPUT_DIR%\%APP_NAME%_amd64.exe.
    goto :error
)
copy /Y "%OUTPUT_DIR%\maclaw-tui_amd64.exe" "%OUTPUT_DIR%\maclaw-tui.exe" >nul
copy /Y "%OUTPUT_DIR%\maclaw-cli_amd64.exe" "%OUTPUT_DIR%\maclaw-cli.exe" >nul
copy /Y "%OUTPUT_DIR%\maclaw-acp-bridge_amd64.exe" "%OUTPUT_DIR%\maclaw-acp-bridge.exe" >nul
copy /Y "%OUTPUT_DIR%\maclaw-tool_amd64.exe" "%OUTPUT_DIR%\maclaw-tool.exe" >nul
copy /Y "%OUTPUT_DIR%\maclawsrv_amd64.exe" "%OUTPUT_DIR%\maclawsrv.exe" >nul
copy /Y "%OUTPUT_DIR%\maclaw-data-srv_amd64.exe" "%OUTPUT_DIR%\maclaw-data-srv.exe" >nul

if exist "%OUTPUT_DIR%\%APP_NAME%.exe" (
    echo [SUCCESS] GUI binary: %OUTPUT_DIR%\%APP_NAME%.exe
)

REM Keep the launch locations used by older shortcuts in sync with the freshly
REM built GUI.  Leaving build\bin or guiapp\gui.exe stale makes a successful
REM build appear to have no frontend changes when the user starts the legacy
REM shortcut instead of dist\MaClaw.exe.
echo [INFO] Syncing legacy GUI launch copies...
copy /Y "%OUTPUT_DIR%\%APP_NAME%.exe" "%~dp0build\bin\%APP_NAME%.exe" >nul
if errorlevel 1 (
    echo [ERROR] Failed to sync build\bin\%APP_NAME%.exe.
    goto :error
)
copy /Y "%OUTPUT_DIR%\%APP_NAME%.exe" "%~dp0guiapp\gui.exe" >nul
if errorlevel 1 (
    echo [ERROR] Failed to sync guiapp\gui.exe.
    goto :error
)
copy /Y "%OUTPUT_DIR%\%APP_NAME%.exe" "%~dp0guiapp\frontend\gui.exe" >nul
if errorlevel 1 (
    echo [ERROR] Failed to sync guiapp\frontend\gui.exe.
    goto :error
)
if exist "%OUTPUT_DIR%\%APP_NAME%_arm64.exe" (
    copy /Y "%OUTPUT_DIR%\%APP_NAME%_arm64.exe" "%~dp0build\bin\%APP_NAME%_arm64.exe" >nul
    if errorlevel 1 (
        echo [ERROR] Failed to sync build\bin\%APP_NAME%_arm64.exe.
        goto :error
    )
)
if exist "%OUTPUT_DIR%\maclaw-tui.exe" (
    echo [SUCCESS] TUI/CLI binary: %OUTPUT_DIR%\maclaw-tui.exe
)
if exist "%OUTPUT_DIR%\maclaw-cli.exe" (
    echo [SUCCESS] maclaw-cli binary: %OUTPUT_DIR%\maclaw-cli.exe
)
if exist "%OUTPUT_DIR%\maclaw-acp-bridge.exe" (
    echo [SUCCESS] maclaw-acp-bridge binary: %OUTPUT_DIR%\maclaw-acp-bridge.exe
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

:copy_main_gui
set "GUI_SOURCE=%~1"
set "GUI_TARGET=%~2"
if not exist "%GUI_SOURCE%" exit /b 1
for /L %%I in (1,1,10) do (
    copy /Y "%GUI_SOURCE%" "%GUI_TARGET%" >nul 2>nul
    if !errorlevel! equ 0 exit /b 0
    timeout /t 1 /nobreak >nul
)
exit /b 1

:generate_windows_resources
set "RESOURCE_ARCH=%~1"
set "RESOURCE_OUTPUT=%~2"
del /q "%RESOURCE_OUTPUT%" 2>nul
setlocal
set "GOARCH=amd64"
"%GO_EXE%" -C "%~dp0tools\windowsresgen" run . -arch "%RESOURCE_ARCH%" -ico "%~dp0build\windows\icon.ico" -manifest "%~dp0build\windows\wails.exe.manifest.tmp" -versioninfo "%~dp0build\windows\versioninfo.json.tmp" -out "%RESOURCE_OUTPUT%"
set "RESOURCE_RESULT=!errorlevel!"
endlocal & if %RESOURCE_RESULT% equ 0 exit /b 0
echo [ERROR] Failed to generate the unified %RESOURCE_ARCH% Windows resource object.
exit /b 1

:verify_windows_resources
setlocal
set "GOARCH=amd64"
"%GO_EXE%" -C "%~dp0tools\windowsresgen" run . -verify "%~1" -ico "%~dp0build\windows\icon.ico"
set "RESOURCE_RESULT=!errorlevel!"
endlocal & if %RESOURCE_RESULT% equ 0 exit /b 0
echo [ERROR] Final executable does not contain the expected Explorer, Wails, and IDI_APPLICATION icon groups.
exit /b 1

:go_build
set "GO_BUILD_ATTEMPT=1"
:go_build_retry
"%GO_EXE%" build %*
if !errorlevel! equ 0 exit /b 0
set "GO_BUILD_ERROR=!errorlevel!"
if !GO_BUILD_ATTEMPT! geq 8 exit /b !GO_BUILD_ERROR!
echo [WARN] go build failed with exit code !GO_BUILD_ERROR!, retrying...
"%POWERSHELL_EXE%" -NoProfile -Command "Get-Process go,compile,link,gcc -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue; Start-Sleep -Milliseconds 500; Get-ChildItem $env:TEMP -Filter 'go-build*' -Directory -ErrorAction SilentlyContinue | Where-Object { $_.LastWriteTime -lt (Get-Date).AddMinutes(-1) } | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue"
set /a GO_BUILD_ATTEMPT+=1
goto :go_build_retry

:go_build_datasrv
set "GO_BUILD_ATTEMPT=1"
:go_build_datasrv_retry
"%GO_EXE%" -C "%~dp0datasrv" build -mod=mod %*
if !errorlevel! equ 0 exit /b 0
set "GO_BUILD_ERROR=!errorlevel!"
if !GO_BUILD_ATTEMPT! geq 8 exit /b !GO_BUILD_ERROR!
echo [WARN] datasrv go build failed with exit code !GO_BUILD_ERROR!, retrying...
"%POWERSHELL_EXE%" -NoProfile -Command "Get-Process go,compile,link,gcc -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue; Start-Sleep -Milliseconds 500; Get-ChildItem $env:TEMP -Filter 'go-build*' -Directory -ErrorAction SilentlyContinue | Where-Object { $_.LastWriteTime -lt (Get-Date).AddMinutes(-1) } | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue"
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
