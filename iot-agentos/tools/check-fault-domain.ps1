[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\fault_domain.c'
$header = Join-Path $projectRoot 'main\fault_domain.h'
$test = Join-Path $PSScriptRoot 'host_tests\test_fault_domain.c'
$failures = @()

foreach ($path in @($source, $header, $test)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ($failures.Count -eq 0) {
    $headerText = Get-Content -LiteralPath $header -Raw
    foreach ($required in @(
            'FAULT_DOMAIN_ABI_VERSION',
            'FAULT_DOMAIN_ID_STORAGE',
            'FAULT_DOMAIN_UNKNOWN_OUTCOME',
            'FAULT_DOMAIN_DEGRADED',
            'fault_domain_begin_start',
            'fault_domain_begin_quiesce',
            'fault_domain_begin_self_test',
            'fault_domain_mark_ready',
            'fault_domain_get_snapshot')) {
        if ($headerText -notmatch [regex]::Escape($required)) {
            $failures += "fault-domain contract missing $required"
        }
    }
    if ($headerText -match '\b(?:esp_|freertos/|driver/|TaskHandle_t|QueueHandle_t|SemaphoreHandle_t|gpio_|i2c_|FILE)\b') {
        $failures += 'fault-domain public contract leaked SDK/RTOS/driver/VFS detail'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for fault-domain test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_fault_domain.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" $test $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host fault-domain compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) { $failures += "host fault-domain test failed (exit $LASTEXITCODE)" }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("fault-domain check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'fault-domain check passed: value-only phases/generations close admission until explicit self-test'
