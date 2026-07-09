@echo off
setlocal EnableDelayedExpansion

echo [INFO] Building TigerProxy...

set "ROOT=%~dp0"

rem --- Ensure Go is in PATH ---
where go >nul 2>nul
if !errorlevel! neq 0 (
  if exist "C:\Program Files\Go\bin\go.exe" (
    set "PATH=C:\Program Files\Go\bin;%USERPROFILE%\go\bin;%PATH%"
  ) else if exist "C:\Go\bin\go.exe" (
    set "PATH=C:\Go\bin;%USERPROFILE%\go\bin;%PATH%"
  ) else (
    echo [FAILED] Go not found. Please install Go from https://go.dev/dl/
    pause
    exit /b 1
  )
)
set "APP_NAME=TigerProxy"
set "OUTPUT_DIR=%ROOT%dist"
set "TIGER_DIR=%ROOT%TigerProxy"
set "ICON_PATH=%TIGER_DIR%\assets\maclaw.ico"
set "GOVERSIONINFO_PATH=%USERPROFILE%\go\bin\goversioninfo.exe"

if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"
if not exist "%ICON_PATH%" copy /Y "%ROOT%build\windows\icon.ico" "%ICON_PATH%" >nul

if not exist "%GOVERSIONINFO_PATH%" (
  echo [INFO] Installing goversioninfo...
  go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
  if !errorlevel! neq 0 goto :error
)

echo [Step 1/3] Preparing Windows resources...
"%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -Command "$version='0.1.0.1'; $manifest = Get-Content '%ROOT%build\windows\wails.exe.manifest' -Raw; $manifest = $manifest.Replace('{{.Name}}','TigerProxy').Replace('{{.Info.ProductVersion}}',$version); [System.IO.File]::WriteAllText('%TIGER_DIR%\wails.exe.manifest.tmp', $manifest, [System.Text.UTF8Encoding]::new($false)); $versionInfo = @{ FixedFileInfo = @{ FileVersion = @{ Major = 0; Minor = 1; Patch = 0; Build = 1 }; ProductVersion = @{ Major = 0; Minor = 1; Patch = 0; Build = 1 } }; StringFileInfo = @{ Comments = 'TigerProxy: CodeGen protocol proxy'; CompanyName = 'QianXin'; FileDescription = 'TigerProxy'; FileVersion = $version; InternalName = 'TigerProxy'; LegalCopyright = 'Copyright (C) 2026 QianXin'; OriginalFilename = 'TigerProxy.exe'; ProductName = 'TigerProxy'; ProductVersion = $version }; VarFileInfo = @{ Translation = @{ LangID = '0409'; CharsetID = '04B0' } } } | ConvertTo-Json -Depth 6; [System.IO.File]::WriteAllText('%TIGER_DIR%\versioninfo.json.tmp', $versionInfo, [System.Text.UTF8Encoding]::new($false))"
if !errorlevel! neq 0 goto :error

echo [Step 2/3] Generating resource syso...
del /q "%TIGER_DIR%\resource_windows_*.syso" 2>nul
"%GOVERSIONINFO_PATH%" -64 -icon "%ICON_PATH%" -manifest "%TIGER_DIR%\wails.exe.manifest.tmp" -o "%TIGER_DIR%\resource_windows_amd64.syso" "%TIGER_DIR%\versioninfo.json.tmp"
if !errorlevel! neq 0 goto :error

echo [Step 3/3] Compiling TigerProxy...
pushd "%TIGER_DIR%"
set "GOOS=windows"
set "GOARCH=amd64"
set "CGO_ENABLED=0"
go build -tags desktop,production,oem_qianxin -ldflags "-s -w -H windowsgui" -o "%OUTPUT_DIR%\%APP_NAME%.exe" .
if !errorlevel! neq 0 (
  popd
  goto :error
)
popd

del /q "%TIGER_DIR%\resource_windows_amd64.syso" "%TIGER_DIR%\wails.exe.manifest.tmp" "%TIGER_DIR%\versioninfo.json.tmp" 2>nul

echo [SUCCESS] TigerProxy built: %OUTPUT_DIR%\%APP_NAME%.exe
echo.
pause
endlocal
goto :eof

:error
echo [FAILED] TigerProxy build failed.
echo.
pause
endlocal
exit /b 1
