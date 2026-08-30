[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$header = Join-Path $projectRoot 'main\services\deferred_setup_worker_service.h'
$source = Join-Path $projectRoot 'main\services\deferred_setup_worker_service.c'
$main = Join-Path $projectRoot 'main\main.c'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_deferred_setup_worker_service.c'
$failures = @()

foreach ($path in @($header, $source, $main, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ((Test-Path -LiteralPath $header) -and (Test-Path -LiteralPath $source) -and
    (Test-Path -LiteralPath $main)) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    $mainText = Get-Content -LiteralPath $main -Raw
    foreach ($api in @('deferred_setup_worker_service_host_t',
                         'deferred_setup_worker_service_init',
                         'deferred_setup_worker_service_start',
                         'deferred_setup_worker_service_stop',
                         'deferred_setup_worker_service_prepare_system_sleep',
                         'deferred_setup_worker_service_abort_system_sleep_prepare',
                         'deferred_setup_worker_service_prepare_network_restart',
                         'deferred_setup_worker_service_commit_prepared_network_restart')) {
        if ($headerText -notmatch ("\b$api\b")) {
            $failures += "deferred setup worker public contract missing $api"
        }
    }
    if ($headerText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|QueueHandle_t|heap_caps|wifi_|netif|http|cJSON|CONFIG_MACLAW_BOARD_)\b') {
        $failures += 'deferred setup worker public contract leaked SDK/RTOS/network/HTTP/JSON/board detail'
    }
    foreach ($fence in @('s_start_gate', 'task_registry_register\s*\(',
                         'task_registry_unregister_with_timeout\s*\(',
                         's_registry_retirement_failed',
                         's_system_sleep_preparing', 's_system_sleep_restart_pending',
                         's_network_restart_preparing',
                         'DEFERRED_SETUP_WAIT_US')) {
        if ($sourceText -notmatch $fence) {
            $failures += "deferred setup worker lifecycle fence missing $fence"
        }
    }
    foreach ($rootRequirement in @(
        'deferred_setup_worker_service_init\s*\(\s*&s_deferred_setup_worker_service_host\s*\)',
        'deferred_setup_worker_service_start\s*\(\s*\)',
        'deferred_setup_worker_service_stop\s*\(\s*remaining_ms\s*\)',
        'deferred_setup_worker_service_prepare_system_sleep\s*\(\s*timeout_ms\s*\)',
        'deferred_setup_worker_service_abort_system_sleep_prepare\s*\(\s*\)',
        'deferred_setup_worker_service_prepare_network_restart\s*\(\s*timeout_ms\s*\)',
        'deferred_setup_worker_service_commit_prepared_network_restart\s*\(\s*\)')) {
        if ($mainText -notmatch $rootRequirement) {
            $failures += "main composition wiring missing $rootRequirement"
        }
    }
    if ($mainText -match '\bs_deferred_setup_(?:task|start_gate|stopped|stop_requested|admission_open|system_sleep_preparing|retiring|exit_status|registry_retirement_failed)\b') {
        $failures += 'main.c still owns deferred setup task/Registry/admission state'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for deferred setup worker contract test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_deferred_setup_worker_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host deferred setup worker contract compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host deferred setup worker contract test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("deferred setup worker check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'deferred setup worker check passed: value contract, lifecycle fence, composition wiring, and host compilation are intact'
