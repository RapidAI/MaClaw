[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\shared_bus_lifecycle.c'
$header = Join-Path $projectRoot 'main\shared_bus_lifecycle.h'
$faultSource = Join-Path $projectRoot 'main\fault_domain.c'
$test = Join-Path $PSScriptRoot 'host_tests\test_shared_bus_lifecycle.c'
$cmake = Join-Path $projectRoot 'main\CMakeLists.txt'
$failures = @()

foreach ($path in @($source, $header, $faultSource, $test, $cmake)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

foreach ($path in @($source, $header)) {
    if (Test-Path -LiteralPath $path) {
        $text = Get-Content -LiteralPath $path -Raw
        if ($text -cmatch '\b(?:esp_|freertos/|driver/|i2c_|spi_|gpio|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|board_)\b') {
            $failures += "shared bus lifecycle contract leaked platform/hardware detail ($path)"
        }
    }
}

if (Test-Path -LiteralPath $header) {
    $text = Get-Content -LiteralPath $header -Raw
    foreach ($required in @(
            'SHARED_BUS_LIFECYCLE_STALE_LEASE',
            'SHARED_BUS_LIFECYCLE_UNKNOWN_OUTCOME',
            'shared_bus_lifecycle_begin_recovery\s*\(',
            'shared_bus_lifecycle_can_detach\s*\(',
            'shared_bus_lifecycle_mark_detached\s*\(',
            'shared_bus_lifecycle_cancel_unattached_start\s*\(',
            'shared_bus_lifecycle_acquire\s*\(')) {
        if ($text -notmatch $required) { $failures += "shared bus lifecycle public contract missing $required" }
    }
}

if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    foreach ($required in @(
            'fault_domain_begin_quiesce',
            'active_lease_count\(lifecycle\) != 0u',
            'FAULT_DOMAIN_QUIESCING',
            'fault_domain_mark_stopped',
            'FAULT_DOMAIN_UNKNOWN_OUTCOME')) {
        if ($text -notmatch $required) { $failures += "shared bus lifecycle recovery ordering missing $required" }
    }
}

if ((Test-Path -LiteralPath $cmake) -and
    ((Get-Content -LiteralPath $cmake -Raw) -notmatch '"shared_bus_lifecycle\.c"')) {
    $failures += 'shared bus lifecycle source is not compiled by the main component'
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for shared bus lifecycle test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_shared_bus_lifecycle.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $test $source $faultSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host shared bus lifecycle compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host shared bus lifecycle test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("shared bus lifecycle check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'shared bus lifecycle check passed: admission closes, stale borrower leases drain, and detach/reprobe can only follow a closed generation'
