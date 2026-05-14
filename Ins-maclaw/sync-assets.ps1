$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$assetDir = Join-Path $root 'Ins-maclaw\assets'
$sourceLogo = Join-Path $root 'gui\build\appicon.png'
$targetLogo = Join-Path $assetDir 'appicon.png'
$sourceIcon = Join-Path $root 'build\windows\icon.ico'
$targetIcon = Join-Path $assetDir 'icon.ico'

if (-not (Test-Path $sourceLogo)) {
  throw "MaClaw GUI logo not found: $sourceLogo"
}
if (-not (Test-Path $sourceIcon)) {
  throw "MaClaw Windows icon not found: $sourceIcon"
}

New-Item -ItemType Directory -Force -Path $assetDir | Out-Null
Copy-Item -Path $sourceLogo -Destination $targetLogo -Force
Copy-Item -Path $sourceIcon -Destination $targetIcon -Force
Write-Host "Synced Ins-maclaw logo: $targetLogo"
Write-Host "Synced Ins-maclaw icon: $targetIcon"
