[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\services\cellular_recovery_policy.c'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_cellular_recovery_policy.c'
$headerRoot = Join-Path $projectRoot 'main'
$failures = @()
foreach ($path in @($source, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Cellular Recovery policy test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_cellular_recovery_policy.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$headerRoot" $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Cellular Recovery policy compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Cellular Recovery policy test failed (exit $LASTEXITCODE)"
        }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("Cellular Recovery policy check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Cellular Recovery policy check passed: bounded exponential backoff is saturating and host-testable'
