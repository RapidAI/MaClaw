param(
    [string]$OutputDir = ".\dist"
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$projectRoot = Split-Path -Parent $root
$targetDir = Join-Path $root $OutputDir
$goCacheDir = Join-Path $projectRoot ".gocache"
$goModCacheDir = Join-Path $projectRoot ".gomodcache"

New-Item -ItemType Directory -Force -Path $targetDir | Out-Null
New-Item -ItemType Directory -Force -Path $goCacheDir | Out-Null
New-Item -ItemType Directory -Force -Path $goModCacheDir | Out-Null

Push-Location $projectRoot
try {
    $env:GOCACHE = $goCacheDir
    $env:GOMODCACHE = $goModCacheDir
    go build -o (Join-Path $targetDir "MaClaw-hubcenter.exe") .\hubcenter\cmd\hubcenter
}
finally {
    Pop-Location
}

Write-Host "Built MaClaw Hub Center to $targetDir"
