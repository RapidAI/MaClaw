param(
  [string]$InstallDir = "$env:USERPROFILE\.local\bin"
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$outDir = [System.IO.Path]::GetFullPath($InstallDir)
$outPath = Join-Path $outDir "maclaw-cli.exe"

New-Item -ItemType Directory -Force -Path $outDir | Out-Null
Push-Location $repoRoot
try {
  go build -o $outPath ./maclaw-cli
} finally {
  Pop-Location
}

Write-Output "Installed maclaw-cli to $outPath"
Write-Output "Run: $outPath agent-help"
Write-Output "Add $outDir to PATH if needed."
