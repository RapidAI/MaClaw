[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$header = Join-Path $projectRoot 'main\services\configuration_persistence_worker_service.h'
$source = Join-Path $projectRoot 'main\services\configuration_persistence_worker_service.c'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_configuration_persistence_worker_service.c'
$failures = @()

foreach ($path in @($header, $source, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ((Test-Path -LiteralPath $header) -and (Test-Path -LiteralPath $source)) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    foreach ($api in @('configuration_persistence_request_t',
                         'configuration_persistence_reply_t',
                         'configuration_persistence_worker_service_host_t',
                         'configuration_persistence_worker_service_init',
                         'configuration_persistence_worker_service_submit',
                         'configuration_persistence_worker_service_submit_until',
                         'configuration_persistence_worker_service_prepare_system_sleep',
                         'configuration_persistence_worker_service_abort_system_sleep_prepare')) {
        if ($headerText -notmatch ("\b$api\b")) {
            $failures += "configuration persistence worker public contract missing $api"
        }
    }
    if ($headerText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|QueueHandle_t|heap_caps|nvs_|cJSON|CONFIG_MACLAW_BOARD_)\b') {
        $failures += 'configuration persistence worker public contract leaked SDK/RTOS/allocator/NVS/JSON/board detail'
    }
    foreach ($fence in @('s_start_gate', 'task_registry_register\s*\(',
                         'task_registry_unregister_with_timeout\s*\(',
                         's_retiring',
                         's_registry_retirement_failed',
                         's_system_sleep_preparing',
                         'configuration_persistence_worker_service_stop\s*\(')) {
        if ($sourceText -notmatch $fence) {
            $failures += "configuration persistence worker lifecycle fence missing $fence"
        }
    }
    if ($sourceText -notmatch 'remaining_ms\s*\(\s*deadline_us\s*\)') {
        $failures += 'single-deadline submit path missing monotonic remaining-budget calculation'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for persistence worker contract test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_configuration_persistence_worker_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host configuration persistence worker contract compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host configuration persistence worker contract test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("configuration persistence worker check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'configuration persistence worker check passed: value contract, lifecycle fence, and host compilation are intact'
