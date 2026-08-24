[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\transport_selection_transaction.c'
$header = Join-Path $projectRoot 'main\transport_selection_transaction.h'
$test = Join-Path $PSScriptRoot 'host_tests\test_transport_selection_transaction.c'
$cmake = Join-Path $projectRoot 'main\CMakeLists.txt'
$failures = @()

foreach ($path in @($source, $header, $test, $cmake)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

foreach ($path in @($source, $header)) {
    if (Test-Path -LiteralPath $path) {
        $text = Get-Content -LiteralPath $path -Raw
        if ($text -cmatch '\b(?:esp_|freertos/|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|heap_caps|board_|platform_connectivity)\b') {
            $failures += "transport selection contract leaked platform/network/RTOS detail ($path)"
        }
    }
}

if (Test-Path -LiteralPath $header) {
    $text = Get-Content -LiteralPath $header -Raw
    foreach ($required in @(
            'TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME',
            'TRANSPORT_SELECTION_TRANSACTION_STALE_GENERATION',
            'TRANSPORT_SELECTION_TRANSACTION_DEADLINE_EXPIRED',
            'transport_selection_generation_is_current_t',
            'transport_selection_remaining_timeout_t',
            'transport_selection_support_t',
            'transport_selection_step_t',
            'transport_selection_commit_t',
            'transport_selection_transaction_execute\s*\(')) {
        if ($text -notmatch $required) { $failures += "transport selection public contract missing $required" }
    }
}

if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    foreach ($required in @(
            'check_supported',
            'drain_current',
            'quiesce_current',
            'activate_target',
            'wait_target_ready',
            'restore_previous',
            'generation_is_current',
            'remaining_timeout_ms',
            'commit[\s\S]*?remaining_timeout_ms',
            'result\.status\s*!=\s*DEVICE_STATUS_OK',
            'commit[\s\S]*?generation_is_current',
            'TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME')) {
        if ($text -notmatch $required) { $failures += "transport selection transaction ordering missing $required" }
    }
}

if ((Test-Path -LiteralPath $cmake) -and
    ((Get-Content -LiteralPath $cmake -Raw) -notmatch '"transport_selection_transaction\.c"')) {
    $failures += 'transport selection transaction source is not compiled by the main component'
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for transport selection transaction test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_transport_selection_transaction.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $test $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host transport selection transaction compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host transport selection transaction test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("transport selection transaction check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'transport selection transaction check passed: drain/quiesce/activate/readiness/rollback preserves one fail-closed logical selection boundary'
