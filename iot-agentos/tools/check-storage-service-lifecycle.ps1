[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_storage_service_lifecycle.c'
$source = Join-Path $projectRoot 'main\storage_service.c'
$faultDomainSource = Join-Path $projectRoot 'main\fault_domain.c'
$failures = @()
foreach ($path in @($testSource, $source, $faultDomainSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Storage lifecycle test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_storage_service_lifecycle.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $testSource $source $faultDomainSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Storage lifecycle compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Storage lifecycle test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Storage Service lifecycle check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Storage Service lifecycle check passed: fault-domain generation, observed self-test and failed cleanup remain fail-closed/retryable'
