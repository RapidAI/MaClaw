[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_connectivity_restart_coordinator.c'
$source = Join-Path $projectRoot 'main\services\connectivity_restart_coordinator.c'
$header = Join-Path $projectRoot 'main\services\connectivity_restart_coordinator.h'
$failures = @()
foreach ($path in @($testSource, $source, $header, (Join-Path $projectRoot 'main\device_api.h'))) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Connectivity Restart Coordinator test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_connectivity_restart_coordinator.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Connectivity Restart Coordinator compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Connectivity Restart Coordinator test failed (exit $LASTEXITCODE)"
        }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("Connectivity Restart Coordinator check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Connectivity Restart Coordinator check passed: all stages use one deadline and failures stay terminal/fail-closed'
