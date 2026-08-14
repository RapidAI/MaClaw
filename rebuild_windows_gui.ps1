$ErrorActionPreference = 'Stop'
$root = 'D:\workprj\aicoder'
$gcc = Get-Command gcc -ErrorAction SilentlyContinue
if ($null -eq $gcc) {
  throw 'Windows GUI rebuild requires gcc (for CGO). Install a MinGW-w64 toolchain and add its bin directory to PATH.'
}
$goversioninfo = 'C:\Users\ma139\go\bin\goversioninfo.exe'
if (-not (Test-Path $goversioninfo)) {
  throw "Windows GUI rebuild requires goversioninfo at $goversioninfo. Install it with: go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest"
}
$cfg = Get-Content (Join-Path $root 'wails.json') -Raw | ConvertFrom-Json
$productVersion = [string]$cfg.info.productVersion
$buildNumber = (Get-Content (Join-Path $root 'build_number') -Raw).Trim()
if ([string]::IsNullOrWhiteSpace($productVersion) -or [string]::IsNullOrWhiteSpace($buildNumber)) {
  throw 'wails.json productVersion and build_number must both be set.'
}
$versionParts = $productVersion.Split('.')
if ($versionParts.Count -lt 3 -or $versionParts[0..2] | Where-Object { $_ -notmatch '^\d+$' }) {
  throw "wails.json productVersion must start with major.minor.patch, got: $productVersion"
}
if ($buildNumber -notmatch '^\d+$') {
  throw "build_number must be numeric, got: $buildNumber"
}
# Keep the linked runtime version and Windows file metadata aligned with the
# installer/release convention (major.minor.patch.build).  Previously this
# script read build_number but silently discarded it, producing local binaries
# reported as 7.1.0 while the installed build was 7.1.0.11864.
$version = "$($versionParts[0]).$($versionParts[1]).$($versionParts[2]).$buildNumber"
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

& $goversioninfo -64 -icon (Join-Path $root 'build\windows\icon.ico') -manifest (Join-Path $root 'build\windows\wails.exe.manifest.tmp') -o (Join-Path $root 'gui\resource_windows_amd64.syso') (Join-Path $root 'build\windows\versioninfo.json.tmp')
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Push-Location $root
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '1'
$env:CC = 'gcc'
& go build -tags desktop,production -ldflags "-s -w -H windowsgui -X main.version=$version" -o (Join-Path $root 'dist\MaClaw_amd64.exe') ./gui/
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }

& $goversioninfo -64 -arm -icon (Join-Path $root 'build\windows\icon.ico') -manifest (Join-Path $root 'build\windows\wails.exe.manifest.tmp') -o (Join-Path $root 'gui\resource_windows_arm64.syso') (Join-Path $root 'build\windows\versioninfo.json.tmp')
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }

$env:GOARCH = 'arm64'
$env:CGO_ENABLED = '0'
Remove-Item Env:CC -ErrorAction SilentlyContinue
& go build -tags desktop,production -ldflags "-s -w -H windowsgui -X main.version=$version" -o (Join-Path $root 'dist\MaClaw_arm64.exe') ./gui/
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }

Copy-Item (Join-Path $root 'dist\MaClaw_amd64.exe') (Join-Path $root 'dist\MaClaw.exe') -Force

# ACP bridge ships next to MaClaw for VS Code integration (production closed loop).
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
Remove-Item Env:CC -ErrorAction SilentlyContinue
& go build -ldflags "-s -w -X main.version=$version" -o (Join-Path $root 'dist\maclaw-acp-bridge_amd64.exe') ./cmd/maclaw-acp-bridge/
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }
Copy-Item (Join-Path $root 'dist\maclaw-acp-bridge_amd64.exe') (Join-Path $root 'dist\maclaw-acp-bridge.exe') -Force
# bridge.exe already sits beside MaClaw.exe in dist\ — nothing further to copy.

Pop-Location
Write-Output 'REBUILT_OK'
