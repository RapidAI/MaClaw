[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_connectivity_service.c'
$source = Join-Path $projectRoot 'main\connectivity_service.c'
$faultDomainSource = Join-Path $projectRoot 'main\fault_domain.c'
$mockRoot = Join-Path $PSScriptRoot 'host_tests\mocks'
$failures = @()
foreach ($path in @($testSource, $source, $faultDomainSource, (Join-Path $mockRoot 'esp_err.h'),
                        (Join-Path $mockRoot 'freertos\event_groups.h'),
                        (Join-Path $mockRoot 'platform_connectivity.h'))) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Connectivity Service test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_connectivity_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$mockRoot" "-I$(Join-Path $projectRoot 'main')" `
        $testSource $source $faultDomainSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Connectivity Service compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Connectivity Service test failed (exit $LASTEXITCODE)"
        }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("Connectivity Service check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Connectivity Service check passed: fault-domain generation and system-sleep admission close stale Wi-Fi work'
