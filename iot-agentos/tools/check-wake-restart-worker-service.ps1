[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$header = Join-Path $projectRoot 'main\services\wake_restart_worker_service.h'
$source = Join-Path $projectRoot 'main\services\wake_restart_worker_service.c'
$main = Join-Path $projectRoot 'main\main.c'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_wake_restart_worker_service.c'
$failures = @()

foreach ($path in @($header, $source, $main, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ((Test-Path -LiteralPath $header) -and (Test-Path -LiteralPath $source) -and
    (Test-Path -LiteralPath $main)) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    $mainText = Get-Content -LiteralPath $main -Raw
    foreach ($api in @('wake_restart_worker_service_host_t',
                         'wake_restart_worker_service_init',
                         'wake_restart_worker_service_start',
                         'wake_restart_worker_service_stop',
                         'wake_restart_worker_service_close_admission',
                         'wake_restart_worker_service_note_startup_teardown',
                         'wake_restart_worker_service_consume_startup_teardown',
                         'wake_restart_worker_service_prepare_system_sleep',
                         'wake_restart_worker_service_abort_system_sleep_prepare',
                         'wake_restart_worker_service_prepare_network_restart',
                         'wake_restart_worker_service_commit_prepared_network_restart')) {
        if ($headerText -notmatch ("\b$api\b")) {
            $failures += "wake restart worker public contract missing $api"
        }
    }
    if ($headerText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|QueueHandle_t|heap_caps|wifi_|netif|http|cJSON|CONFIG_MACLAW_BOARD_)\b') {
        $failures += 'wake restart worker public contract leaked SDK/RTOS/network/HTTP/JSON/board detail'
    }
    foreach ($fence in @('MALLOC_CAP_SPIRAM', 's_start_gate',
                         'task_registry_register\s*\(',
                         'task_registry_unregister_with_timeout\s*\(',
                         's_registry_retirement_failed',
                         's_system_sleep_preparing',
                         's_system_sleep_restart_pending',
                         's_network_restart_preparing',
                         'WAKE_RESTART_WORKER_STACK_SIZE')) {
        if ($sourceText -notmatch $fence) {
            $failures += "wake restart worker lifecycle fence missing $fence"
        }
    }
    foreach ($rootRequirement in @(
        'wake_restart_worker_service_init\s*\(\s*&s_wake_restart_worker_service_host\s*\)',
        'wake_restart_worker_service_start\s*\(\s*\)',
        'wake_restart_worker_service_stop\s*\(\s*timeout_ms\s*\)',
        'wake_restart_worker_service_close_admission\s*\(\s*\)',
        'wake_restart_worker_service_note_startup_teardown\s*\(\s*\)',
        'wake_restart_worker_service_consume_startup_teardown\s*\(\s*\)',
        'wake_restart_worker_service_prepare_system_sleep\s*\(\s*timeout_ms\s*\)',
        'wake_restart_worker_service_abort_system_sleep_prepare\s*\(\s*\)',
        'wake_restart_worker_service_prepare_network_restart\s*\(\s*timeout_ms\s*\)',
        'wake_restart_worker_service_commit_prepared_network_restart\s*\(\s*\)')) {
        if ($mainText -notmatch $rootRequirement) {
            $failures += "main composition wiring missing $rootRequirement"
        }
    }
    if ($mainText -match '\bs_wake_restart_(?:scheduled|after_startup|task|start_gate|stopped|stop_requested|admission_open|system_sleep_preparing|retiring|exit_status|registry_retirement_failed)\b') {
        $failures += 'main.c still owns wake-restart task/Registry/admission state'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for wake restart worker contract test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_wake_restart_worker_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host wake restart worker contract compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host wake restart worker contract test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("wake restart worker check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'wake restart worker check passed: value contract, lifecycle fence, composition wiring, and host compilation are intact'
