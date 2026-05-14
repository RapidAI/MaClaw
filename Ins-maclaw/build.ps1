$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$out = Join-Path $root 'dist\Ins-maclaw'
$oldGoEnv = @{
  GOOS = $env:GOOS
  GOARCH = $env:GOARCH
  CGO_ENABLED = $env:CGO_ENABLED
}

function Restore-GoEnvironment {
  foreach ($name in $oldGoEnv.Keys) {
    if ($null -eq $oldGoEnv[$name]) {
      Remove-Item "Env:$name" -ErrorAction SilentlyContinue
    } else {
      Set-Item "Env:$name" $oldGoEnv[$name]
    }
  }
}

function Clear-GeneratedWindowsResources {
  Remove-Item (Join-Path $root 'Ins-maclaw\resource_windows_*.syso') -Force -ErrorAction SilentlyContinue
  Remove-Item (Join-Path $root 'Ins-maclaw\windows\generated-resource.rc') -Force -ErrorAction SilentlyContinue
  Remove-Item (Join-Path $root 'Ins-maclaw\windows\generated-versioninfo.json') -Force -ErrorAction SilentlyContinue
}

trap {
  Clear-GeneratedWindowsResources
  Restore-GoEnvironment
  throw $_
}

$outFull = [System.IO.Path]::GetFullPath($out)
$expectedOut = [System.IO.Path]::GetFullPath((Join-Path $root 'dist\Ins-maclaw'))
if (-not $outFull.Equals($expectedOut, [System.StringComparison]::OrdinalIgnoreCase)) {
  throw "Refusing to clean unexpected output directory: $outFull"
}
New-Item -ItemType Directory -Force -Path $out | Out-Null
& (Join-Path $PSScriptRoot 'sync-assets.ps1')
Get-ChildItem -Path $out -Force -ErrorAction SilentlyContinue | Remove-Item -Force -Recurse -ErrorAction Stop

$version = 'dev'
if (Test-Path (Join-Path $root 'wails.json')) {
  $cfg = Get-Content (Join-Path $root 'wails.json') -Raw | ConvertFrom-Json
  $version = $cfg.info.productVersion
}


function Convert-VersionToCommas([string]$value) {
  $parts = @($value -split '[^0-9]+' | Where-Object { $_ -ne '' } | Select-Object -First 4)
  while ($parts.Count -lt 4) { $parts += '0' }
  return ($parts -join ',')
}

function New-WindowsResource([string]$version, [string]$arch) {
  $icon = Join-Path $root 'build\windows\icon.ico'
  if (-not (Test-Path $icon)) {
    Write-Warning "Icon not found: $icon"
    return $null
  }
  $resDir = Join-Path $root 'Ins-maclaw\windows'
  New-Item -ItemType Directory -Force -Path $resDir | Out-Null
  $syso = Join-Path $root "Ins-maclaw\resource_windows_${arch}.syso"
  $manifest = Join-Path $resDir 'ins-maclaw.manifest'

  $goversioninfo = (Get-Command goversioninfo -ErrorAction SilentlyContinue).Source
  if ($goversioninfo) {
    $json = Join-Path $resDir 'generated-versioninfo.json'
    $versionParts = @($version -split '[^0-9]+' | Where-Object { $_ -ne '' } | Select-Object -First 4)
    while ($versionParts.Count -lt 4) { $versionParts += '0' }
    $versionJson = [ordered]@{
      FixedFileInfo = [ordered]@{
        FileVersion = [ordered]@{ Major=[int]$versionParts[0]; Minor=[int]$versionParts[1]; Patch=[int]$versionParts[2]; Build=[int]$versionParts[3] }
        ProductVersion = [ordered]@{ Major=[int]$versionParts[0]; Minor=[int]$versionParts[1]; Patch=[int]$versionParts[2]; Build=[int]$versionParts[3] }
        FileFlagsMask = '3f'; FileFlags = '00'; FileOS = '040004'; FileType = '01'; FileSubType = '00'
      }
      StringFileInfo = [ordered]@{
        Comments = 'Small native online installer for MaClaw'
        CompanyName = 'RapidAI'
        FileDescription = 'Ins-maclaw Setup Bootstrapper'
        FileVersion = $version
        InternalName = 'Ins-maclaw'
        LegalCopyright = 'Copyright (C) 2026 RapidAI'
        LegalTrademarks = ''
        OriginalFilename = 'Ins-maclaw.exe'
        PrivateBuild = ''
        ProductName = 'Ins-maclaw'
        ProductVersion = $version
        SpecialBuild = ''
      }
      VarFileInfo = [ordered]@{ Translation = [ordered]@{ LangID='0409'; CharsetID='04B0' } }
    }
    $jsonText = $versionJson | ConvertTo-Json -Depth 8
    [System.IO.File]::WriteAllText($json, $jsonText, [System.Text.UTF8Encoding]::new($false))
    $args = @('-64', '-icon', $icon, '-manifest', $manifest, '-o', $syso, $json)
    if ($arch -eq 'arm64') { $args = @('-64', '-arm') + $args[1..($args.Count-1)] }
    & $goversioninfo @args
    if (-not (Test-Path $syso)) { throw "goversioninfo did not create $syso" }
    Write-Host "Embedded Windows $arch icon/version/manifest resource: $syso"
    return $syso
  }

  if ($arch -ne 'amd64') {
    Write-Warning "goversioninfo not found; Windows $arch binary will be built without embedded icon/version metadata."
    return $null
  }
  $windres = (Get-Command windres -ErrorAction SilentlyContinue).Source
  if (-not $windres) {
    Write-Warning 'windres not found; Windows amd64 binary will be built without embedded icon/version metadata.'
    return $null
  }
  $rc = Join-Path $resDir 'generated-resource.rc'
  $fileVersion = Convert-VersionToCommas $version
  $iconRc = ($icon -replace '\', '/')
  $manifestRc = ($manifest -replace '\', '/')
  @"
1 ICON "$iconRc"
1 RT_MANIFEST "$manifestRc"
1 VERSIONINFO
FILEVERSION $fileVersion
PRODUCTVERSION $fileVersion
FILEFLAGSMASK 0x3fL
FILEFLAGS 0x0L
FILEOS 0x40004L
FILETYPE 0x1L
FILESUBTYPE 0x0L
BEGIN
  BLOCK "StringFileInfo"
  BEGIN
    BLOCK "040904b0"
    BEGIN
      VALUE "CompanyName", "RapidAI\0"
      VALUE "FileDescription", "Ins-maclaw Setup Bootstrapper\0"
      VALUE "FileVersion", "$version\0"
      VALUE "InternalName", "Ins-maclaw\0"
      VALUE "OriginalFilename", "Ins-maclaw.exe\0"
      VALUE "ProductName", "Ins-maclaw\0"
      VALUE "ProductVersion", "$version\0"
    END
  END
  BLOCK "VarFileInfo"
  BEGIN
    VALUE "Translation", 0x409, 1200
  END
END
"@ | Set-Content -Path $rc -Encoding ASCII
  & $windres -O coff -F pe-x86-64 $rc -o $syso
  if (-not (Test-Path $syso)) { throw "windres did not create $syso" }
  Write-Host "Embedded Windows amd64 icon/version/manifest resource: $syso"
  return $syso
}

$targets = @(
  @{ GOOS='windows'; GOARCH='amd64'; Name='Ins-maclaw-windows-amd64.exe'; Gui=$true },
  @{ GOOS='windows'; GOARCH='arm64'; Name='Ins-maclaw-windows-arm64.exe'; Gui=$true },
  @{ GOOS='windows'; GOARCH='amd64'; Name='Ins-maclaw-windows-amd64-tui.exe'; Gui=$false },
  @{ GOOS='windows'; GOARCH='arm64'; Name='Ins-maclaw-windows-arm64-tui.exe'; Gui=$false },
  @{ GOOS='darwin'; GOARCH='amd64'; Name='Ins-maclaw-darwin-amd64' },
  @{ GOOS='darwin'; GOARCH='arm64'; Name='Ins-maclaw-darwin-arm64' },
  @{ GOOS='linux'; GOARCH='amd64'; Name='Ins-maclaw-linux-amd64' },
  @{ GOOS='linux'; GOARCH='arm64'; Name='Ins-maclaw-linux-arm64' }
)

Remove-Item (Join-Path $root 'Ins-maclaw\resource_windows_*.syso') -Force -ErrorAction SilentlyContinue

foreach ($target in $targets) {
  $env:CGO_ENABLED = '0'
  $env:GOOS = $target.GOOS
  $env:GOARCH = $target.GOARCH
  $path = Join-Path $out $target.Name
  Write-Host "Building $($target.GOOS)/$($target.GOARCH) -> $path"
  $ldflags = "-s -w -X main.version=$version"
  $resourcePath = Join-Path $root "Ins-maclaw\resource_windows_$($target.GOARCH).syso"
  Remove-Item $resourcePath -Force -ErrorAction SilentlyContinue
  if ($target.GOOS -eq 'windows') {
    $resourcePath = New-WindowsResource $version $target.GOARCH
  }
  if ($target.GOOS -eq 'windows' -and $target.Gui) { $ldflags = "$ldflags -H windowsgui -X main.windowsGUI=true" }
  go build -trimpath -ldflags $ldflags -o $path ./Ins-maclaw
  if ($resourcePath) { Remove-Item $resourcePath -Force -ErrorAction SilentlyContinue }
}

Clear-GeneratedWindowsResources
Restore-GoEnvironment
$built = Get-ChildItem -Path $out -File
if ($built.Count -ne $targets.Count) {
  throw "Expected $($targets.Count) Ins-maclaw artifacts, got $($built.Count)."
}
foreach ($file in $built) {
  if ($file.Length -lt 1024KB) {
    throw "Artifact too small: $($file.FullName) size=$($file.Length)"
  }
}
Write-Host "Done: $out"
$built | Sort-Object Name | ForEach-Object { Write-Host ("  {0} ({1:n0} bytes)" -f $_.Name, $_.Length) }
