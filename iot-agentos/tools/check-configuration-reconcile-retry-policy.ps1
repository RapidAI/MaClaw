[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\configuration_reconcile_retry_policy.c'
$header = Join-Path $projectRoot 'main\configuration_reconcile_retry_policy.h'
$test = Join-Path $PSScriptRoot 'host_tests\test_configuration_reconcile_retry_policy.c'
$cmake = Join-Path $projectRoot 'main\CMakeLists.txt'
$failures = @()

foreach ($path in @($source, $header, $test, $cmake)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if (Test-Path -LiteralPath $header) {
    $text = Get-Content -LiteralPath $header -Raw
    foreach ($required in @(
            'configuration_reconcile_retry_delay_ms\s*\(',
            'CONFIGURATION_RECONCILE_RETRY_INITIAL_DELAY_MS',
            'CONFIGURATION_RECONCILE_RETRY_MAX_DELAY_MS')) {
        if ($text -notmatch $required) { $failures += "retry policy public contract missing $required" }
    }
    if ($text -cmatch '\b(?:esp_|freertos/|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|board_)\b') {
        $failures += 'retry policy public contract leaked platform/hardware detail'
    }
}

if ((Test-Path -LiteralPath $cmake) -and
    ((Get-Content -LiteralPath $cmake -Raw) -notmatch '"configuration_reconcile_retry_policy\.c"')) {
    $failures += 'retry policy source is not compiled by the main component'
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for retry policy test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_configuration_reconcile_retry_policy.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $test $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host retry policy compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) { $failures += "host retry policy test failed (exit $LASTEXITCODE)" }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("configuration reconcile retry policy check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'configuration reconcile retry policy check passed: one bounded value-only retry cadence'
