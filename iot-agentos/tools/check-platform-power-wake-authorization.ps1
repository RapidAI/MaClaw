[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_platform_power_wake_authorization.c'
$sources = @(
    (Join-Path $projectRoot 'main\platform_power.c'),
    (Join-Path $projectRoot 'main\platform_wake.c')
)
$failures = @()
foreach ($path in @($testSource) + $sources) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Power/Wake authorization test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_platform_power_wake_authorization.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $testSource $sources -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Power/Wake authorization compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Power/Wake authorization test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("platform Power/Wake authorization check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'platform Power/Wake authorization check passed: candidates cannot reach a profile transaction'
