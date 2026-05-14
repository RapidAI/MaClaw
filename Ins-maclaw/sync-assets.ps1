$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$assetDir = Join-Path $root 'Ins-maclaw\assets'
$sourceLogo = Join-Path $root 'gui\build\appicon.png'
$targetLogo = Join-Path $assetDir 'appicon.png'

if (-not (Test-Path $sourceLogo)) {
  throw "MaClaw GUI logo not found: $sourceLogo"
}

New-Item -ItemType Directory -Force -Path $assetDir | Out-Null
Copy-Item -Path $sourceLogo -Destination $targetLogo -Force
Write-Host "Synced Ins-maclaw logo: $targetLogo"
