[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\configuration_reconcile.c'
$header = Join-Path $projectRoot 'main\configuration_reconcile.h'
$test = Join-Path $PSScriptRoot 'host_tests\test_configuration_reconcile.c'
$cmake = Join-Path $projectRoot 'main\CMakeLists.txt'
$failures = @()

foreach ($path in @($source, $header, $test, $cmake)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

foreach ($path in @($source, $header)) {
    if (Test-Path -LiteralPath $path) {
        $text = Get-Content -LiteralPath $path -Raw
        if ($text -cmatch '\b(?:esp_|freertos/|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|heap_caps|board_)\b') {
            $failures += "configuration reconcile value contract leaked platform/network/RTOS detail ($path)"
        }
    }
}

if (Test-Path -LiteralPath $header) {
    $text = Get-Content -LiteralPath $header -Raw
    foreach ($required in @(
            'CONFIGURATION_RECONCILE_UNKNOWN_OUTCOME',
            'configuration_reconcile_validate_t',
            'configuration_reconcile_prepare_t',
            'configuration_reconcile_publish_t',
            'configuration_reconcile_apply_t',
            'configuration_reconcile_rollback_t',
            'configuration_reconcile_execute\s*\(')) {
        if ($text -notmatch $required) { $failures += "reconcile public contract missing $required" }
    }
}

if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    foreach ($required in @(
            'transaction->validate',
            'observers\[i\]\.prepare',
            'transaction->publish',
            'observers\[i\]\.apply',
            'observers\[i\]\.rollback',
            'rollback_observers\s*\(',
            'CONFIGURATION_RECONCILE_UNKNOWN_OUTCOME')) {
        if ($text -notmatch $required) { $failures += "reconcile transaction ordering missing $required" }
    }
}

if (Test-Path -LiteralPath $cmake) {
    $text = Get-Content -LiteralPath $cmake -Raw
    if ($text -notmatch '"configuration_reconcile\.c"') {
        $failures += 'configuration reconcile source is not compiled by the main component'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for configuration reconcile test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_configuration_reconcile.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $test $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host configuration reconcile compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host configuration reconcile test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("configuration reconcile check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'configuration reconcile check passed: validate/prepare/publish/apply/rollback is ordered, reverses prepared work and fails closed'
