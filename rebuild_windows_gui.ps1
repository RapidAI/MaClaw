$ErrorActionPreference = 'Stop'
$root = 'D:\workprj\aicoder'
$version = '5.6.0.10084'
$cfg = Get-Content (Join-Path $root 'wails.json') -Raw | ConvertFrom-Json
$parts = $version.Split('.')

Remove-Item (Join-Path $root 'gui\resource_windows_amd64.syso') -ErrorAction SilentlyContinue
Remove-Item (Join-Path $root 'gui\resource_windows_arm64.syso') -ErrorAction SilentlyContinue
Remove-Item (Join-Path $root 'build\windows\wails.exe.manifest.tmp') -ErrorAction SilentlyContinue
Remove-Item (Join-Path $root 'build\windows\versioninfo.json.tmp') -ErrorAction SilentlyContinue

$manifest = Get-Content (Join-Path $root 'build\windows\wails.exe.manifest') -Raw
$manifest = $manifest.Replace('{{.Name}}', $cfg.name).Replace('{{.Info.ProductVersion}}', $version)
[System.IO.File]::WriteAllText((Join-Path $root 'build\windows\wails.exe.manifest.tmp'), $manifest, [System.Text.UTF8Encoding]::new($false))

$versionInfo = @{
  FixedFileInfo = @{
    FileVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = [int]$parts[3] }
    ProductVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = [int]$parts[3] }
  }
  StringFileInfo = @{
    Comments = $cfg.info.comments
    CompanyName = $cfg.info.companyName
    FileDescription = $cfg.info.productName
    FileVersion = $version
    InternalName = $cfg.info.productName
    LegalCopyright = $cfg.info.copyright
    OriginalFilename = 'MaClaw.exe'
    ProductName = $cfg.info.productName
    ProductVersion = $version
  }
  VarFileInfo = @{
    Translation = @{ LangID = '0409'; CharsetID = '04B0' }
  }
} | ConvertTo-Json -Depth 6
[System.IO.File]::WriteAllText((Join-Path $root 'build\windows\versioninfo.json.tmp'), $versionInfo, [System.Text.UTF8Encoding]::new($false))

& 'C:\Users\ma139\go\bin\goversioninfo.exe' -64 -icon (Join-Path $root 'build\windows\icon.ico') -manifest (Join-Path $root 'build\windows\wails.exe.manifest.tmp') -o (Join-Path $root 'gui\resource_windows_amd64.syso') (Join-Path $root 'build\windows\versioninfo.json.tmp')
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Push-Location $root
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '1'
$env:CC = 'gcc'
& go build -tags desktop,production -ldflags '-s -w -H windowsgui' -o (Join-Path $root 'dist\MaClaw_amd64.exe') ./gui/
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }

& 'C:\Users\ma139\go\bin\goversioninfo.exe' -64 -arm -icon (Join-Path $root 'build\windows\icon.ico') -manifest (Join-Path $root 'build\windows\wails.exe.manifest.tmp') -o (Join-Path $root 'gui\resource_windows_arm64.syso') (Join-Path $root 'build\windows\versioninfo.json.tmp')
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }

$env:GOARCH = 'arm64'
$env:CGO_ENABLED = '0'
Remove-Item Env:CC -ErrorAction SilentlyContinue
& go build -tags desktop,production -ldflags '-s -w -H windowsgui' -o (Join-Path $root 'dist\MaClaw_arm64.exe') ./gui/
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }

Copy-Item (Join-Path $root 'dist\MaClaw_amd64.exe') (Join-Path $root 'dist\MaClaw.exe') -Force
Pop-Location
Write-Output 'REBUILT_OK'
