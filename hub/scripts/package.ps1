param(
    [string]$OutputDir = ".\dist",
    [string]$PackageDir = ".\package"
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$distDir = Join-Path $root $OutputDir
$pkgDir = Join-Path $root $PackageDir
$pkgRoot = Join-Path $pkgDir "maclaw-hub"

if (!(Test-Path (Join-Path $distDir "maclaw-hub.exe"))) {
    & (Join-Path $PSScriptRoot "build.ps1") -OutputDir $OutputDir
}

Remove-Item -Recurse -Force $pkgRoot -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $pkgRoot | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $pkgRoot "configs") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $pkgRoot "data") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $pkgRoot "data\logs") | Out-Null

Copy-Item (Join-Path $distDir "maclaw-hub.exe") (Join-Path $pkgRoot "maclaw-hub.exe") -Force
Copy-Item (Join-Path $root "configs\config.example.yaml") (Join-Path $pkgRoot "configs\config.yaml") -Force

if (Test-Path (Join-Path $root "web\dist")) {
    Copy-Item (Join-Path $root "web\dist") (Join-Path $pkgRoot "web") -Recurse -Force
}
if (Test-Path (Join-Path $root "web\admin")) {
    New-Item -ItemType Directory -Force -Path (Join-Path $pkgRoot "web") | Out-Null
    Copy-Item (Join-Path $root "web\admin") (Join-Path $pkgRoot "web\admin") -Recurse -Force
}

Write-Host "Packaged MaClaw Hub to $pkgRoot"
