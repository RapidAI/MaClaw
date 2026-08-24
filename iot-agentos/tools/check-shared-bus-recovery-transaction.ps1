[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\shared_bus_recovery_transaction.c'
$header = Join-Path $projectRoot 'main\shared_bus_recovery_transaction.h'
$lifecycleSource = Join-Path $projectRoot 'main\shared_bus_lifecycle.c'
$faultSource = Join-Path $projectRoot 'main\fault_domain.c'
$test = Join-Path $PSScriptRoot 'host_tests\test_shared_bus_recovery_transaction.c'
$cmake = Join-Path $projectRoot 'main\CMakeLists.txt'
$failures = @()

foreach ($path in @($source, $header, $lifecycleSource, $faultSource, $test, $cmake)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

foreach ($path in @($source, $header)) {
    if (Test-Path -LiteralPath $path) {
        $text = Get-Content -LiteralPath $path -Raw
        if ($text -cmatch '\b(?:esp_|freertos/|driver/|i2c_|spi_|gpio|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|board_|platform_)\b') {
            $failures += "shared bus recovery transaction leaked platform/hardware detail ($path)"
        }
    }
}

if (Test-Path -LiteralPath $header) {
    $text = Get-Content -LiteralPath $header -Raw
    foreach ($required in @(
            'SHARED_BUS_RECOVERY_TRANSACTION_UNKNOWN_OUTCOME',
            'shared_bus_recovery_remaining_timeout_t',
            'quiesce_consumers',
            'wait_for_borrowers',
            'detach_peripherals',
            'detach_codec',
            'delete_bus',
            'create_bus',
            'attach_peripherals',
            'attach_codec',
            'self_test',
            'shared_bus_recovery_transaction_execute\s*\(')) {
        if ($text -notmatch $required) { $failures += "shared bus recovery contract missing $required" }
    }
}

if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    foreach ($required in @(
            'shared_bus_lifecycle_begin_recovery',
            'shared_bus_lifecycle_can_detach',
            'shared_bus_lifecycle_mark_detached',
            'shared_bus_lifecycle_begin_reinitialize',
            'shared_bus_lifecycle_mark_attached',
            'shared_bus_lifecycle_begin_self_test',
            'shared_bus_lifecycle_mark_ready',
            'shared_bus_lifecycle_mark_unknown',
            'callbacks->remaining_timeout_ms\(callbacks->context\)\s*==\s*0u',
            'result\.status\s*==\s*SHARED_BUS_RECOVERY_STATUS_OK')) {
        if ($text -notmatch $required) { $failures += "shared bus recovery transaction ordering missing $required" }
    }
}

if ((Test-Path -LiteralPath $cmake) -and
    ((Get-Content -LiteralPath $cmake -Raw) -notmatch '"shared_bus_recovery_transaction\.c"')) {
    $failures += 'shared bus recovery transaction source is not compiled by the main component'
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for shared bus recovery transaction test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_shared_bus_recovery_transaction.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $test $source $lifecycleSource $faultSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host shared bus recovery transaction compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host shared bus recovery transaction test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("shared bus recovery transaction check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'shared bus recovery transaction check passed: recovery fences borrowers, preserves teardown/rebuild order, enforces a deadline, and fails closed on every post-fence ambiguity'
