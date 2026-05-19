@echo off
setlocal EnableDelayedExpansion

echo [INFO] Building TigerProxy...

set "ROOT=%~dp0"
set "APP_NAME=TigerProxy"
set "OUTPUT_DIR=%ROOT%dist"
set "ICON_PATH=%ROOT%TigerProxy\assets\maclaw.ico"
set "GOVERSIONINFO_PATH=%USERPROFILE%\go\bin\goversioninfo.exe"

if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"
if not exist "%ICON_PATH%" copy /Y "%ROOT%build\windows\icon.ico" "%ICON_PATH%" >nul

if not exist "%GOVERSIONINFO_PATH%" (
  echo [INFO] Installing goversioninfo...
  go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
  if !errorlevel! neq 0 goto :error
)

echo [Step 1/3] Preparing Windows resources...
powershell -NoProfile -Command "$version='0.1.0.1'; $manifest = Get-Content '%ROOT%build\windows\wails.exe.manifest' -Raw; $manifest = $manifest.Replace('{{.Name}}','TigerProxy').Replace('{{.Info.ProductVersion}}',$version); [System.IO.File]::WriteAllText('%ROOT%TigerProxy\wails.exe.manifest.tmp', $manifest, [System.Text.UTF8Encoding]::new($false)); $versionInfo = @{ FixedFileInfo = @{ FileVersion = @{ Major = 0; Minor = 1; Patch = 0; Build = 1 }; ProductVersion = @{ Major = 0; Minor = 1; Patch = 0; Build = 1 } }; StringFileInfo = @{ Comments = 'TigerProxy: CodeGen protocol proxy'; CompanyName = 'QianXin'; FileDescription = 'TigerProxy'; FileVersion = $version; InternalName = 'TigerProxy'; LegalCopyright = 'Copyright (C) 2026 QianXin'; OriginalFilename = 'TigerProxy.exe'; ProductName = 'TigerProxy'; ProductVersion = $version }; VarFileInfo = @{ Translation = @{ LangID = '0409'; CharsetID = '04B0' } } } | ConvertTo-Json -Depth 6; [System.IO.File]::WriteAllText('%ROOT%TigerProxy\versioninfo.json.tmp', $versionInfo, [System.Text.UTF8Encoding]::new($false))"
if !errorlevel! neq 0 goto :error

echo [Step 2/3] Generating resource syso...
del /q "%ROOT%TigerProxy\resource_windows_*.syso" 2>nul
"%GOVERSIONINFO_PATH%" -64 -icon "%ICON_PATH%" -manifest "%ROOT%TigerProxy\wails.exe.manifest.tmp" -o "%ROOT%TigerProxy\resource_windows_amd64.syso" "%ROOT%TigerProxy\versioninfo.json.tmp"
if !errorlevel! neq 0 goto :error

echo [Step 3/3] Compiling TigerProxy...
set "GOOS=windows"
set "GOARCH=amd64"
set "CGO_ENABLED=0"
go build -tags desktop,production,oem_qianxin -ldflags "-s -w -H windowsgui" -o "%OUTPUT_DIR%\%APP_NAME%.exe" ./TigerProxy
if !errorlevel! neq 0 goto :error

del /q "%ROOT%TigerProxy\resource_windows_amd64.syso" "%ROOT%TigerProxy\wails.exe.manifest.tmp" "%ROOT%TigerProxy\versioninfo.json.tmp" 2>nul

echo [SUCCESS] TigerProxy built: %OUTPUT_DIR%\%APP_NAME%.exe
endlocal
goto :eof

:error
echo [FAILED] TigerProxy build failed.
endlocal
exit /b 1
