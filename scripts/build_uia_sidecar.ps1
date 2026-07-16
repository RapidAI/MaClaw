# Build maclaw-uia-sidecar.exe using .NET Framework csc.exe (no Visual Studio required).
# Usage: powershell -NoProfile -File scripts/build_uia_sidecar.ps1 [-OutDir dist]
param(
    [string]$OutDir = ""
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
if (-not $OutDir) { $OutDir = Join-Path $root "dist" }
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$src = Join-Path $root "corelib\accessibility\tools\MaclawUIASidecar\Program.cs"
if (-not (Test-Path $src)) { throw "Source not found: $src" }

$windir = $env:WINDIR
if (-not $windir) { $windir = "C:\Windows" }
$cscCandidates = @(
    (Join-Path $windir "Microsoft.NET\Framework64\v4.0.30319\csc.exe"),
    (Join-Path $windir "Microsoft.NET\Framework\v4.0.30319\csc.exe")
)
$csc = $cscCandidates | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $csc) { throw "csc.exe not found under $windir\Microsoft.NET\Framework*" }

function Find-GacDll([string]$name) {
    $gac = Join-Path $windir "Microsoft.NET\assembly\GAC_MSIL\$name"
    if (-not (Test-Path $gac)) { return $null }
    $hit = Get-ChildItem -Path $gac -Recurse -Filter "$name.dll" -ErrorAction SilentlyContinue |
        Where-Object { $_.FullName -match 'v4\.0_' } |
        Select-Object -First 1
    if ($hit) { return $hit.FullName }
    return $null
}

$refs = @()
foreach ($n in @("UIAutomationClient", "UIAutomationTypes", "WindowsBase")) {
    $p = Find-GacDll $n
    if (-not $p) { throw "Reference DLL not found in GAC: $n" }
    $refs += "/r:$p"
}

$outExe = Join-Path $OutDir "maclaw-uia-sidecar.exe"
$args = @("/nologo", "/t:exe", "/out:$outExe", "/platform:anycpu") + $refs + @($src)
Write-Host "[UIA] csc: $csc"
Write-Host "[UIA] out: $outExe"
& $csc @args
if ($LASTEXITCODE -ne 0) { throw "csc failed with exit $LASTEXITCODE" }
if (-not (Test-Path $outExe)) { throw "output missing: $outExe" }
Write-Host "[UIA] built $outExe ($((Get-Item $outExe).Length) bytes)"
