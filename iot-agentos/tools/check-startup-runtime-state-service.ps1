[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$header = Join-Path $projectRoot 'main\services\startup_runtime_state_service.h'
$source = Join-Path $projectRoot 'main\services\startup_runtime_state_service.c'
$main = Join-Path $projectRoot 'main\main.c'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_startup_runtime_state_service.c'
$failures = @()

foreach ($path in @($header, $source, $main, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ((Test-Path -LiteralPath $header) -and (Test-Path -LiteralPath $source) -and
    (Test-Path -LiteralPath $main)) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    $mainText = Get-Content -LiteralPath $main -Raw
    foreach ($api in @('startup_runtime_state_service_init',
                         'startup_runtime_state_service_capture_boot_session_id',
                         'startup_runtime_state_service_boot_session_id',
                         'startup_runtime_state_service_matches_boot_session_id',
                         'startup_runtime_state_service_capture_staged_provisioning',
                         'startup_runtime_state_service_staged_provisioning_pending',
                         'startup_runtime_state_service_begin_sequence',
                         'startup_runtime_state_service_complete_sequence',
                         'startup_runtime_state_service_sequence_complete',
                         'startup_runtime_state_service_permit_gateway_startup',
                         'startup_runtime_state_service_gateway_startup_recovery_allowed',
                         'startup_runtime_state_service_enter_safe_mode',
                         'startup_runtime_state_service_safe_mode_active')) {
        if ($headerText -notmatch ("\b$api\b")) {
            $failures += "startup runtime state public contract missing $api"
        }
    }
    if ($headerText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|QueueHandle_t|heap_caps|wifi_|netif|http|cJSON|CONFIG_MACLAW_BOARD_)\b') {
        $failures += 'startup runtime state public contract leaked SDK/RTOS/network/HTTP/JSON/board detail'
    }
    foreach ($fence in @('atomic_compare_exchange',
                          'STARTUP_RUNTIME_STATE_SAFE_MODE_ACTIVE',
                          'STARTUP_RUNTIME_STATE_SEQUENCE_COMPLETE',
                          'STARTUP_RUNTIME_STATE_GATEWAY_STARTUP_ALLOWED',
                          'STARTUP_RUNTIME_STATE_PROVISIONING_CAPTURED',
                          'STARTUP_RUNTIME_STATE_PROVISIONING_STAGED',
                          'STARTUP_RUNTIME_STATE_BOOT_SESSION_CAPTURING',
                          'STARTUP_RUNTIME_STATE_BOOT_SESSION_CAPTURED')) {
        if ($sourceText -notmatch $fence) {
            $failures += "startup runtime state atomic fence missing $fence"
        }
    }
    foreach ($rootRequirement in @('startup_runtime_state_service_init\s*\(',
                                    'startup_runtime_state_service_capture_boot_session_id\s*\(',
                                    'startup_runtime_state_service_boot_session_id\s*\(',
                                    'startup_runtime_state_service_matches_boot_session_id\s*\(',
                                    'startup_runtime_state_service_capture_staged_provisioning\s*\(',
                                    'startup_runtime_state_service_staged_provisioning_pending\s*\(',
                                    'startup_runtime_state_service_begin_sequence\s*\(',
                                    'startup_runtime_state_service_complete_sequence\s*\(',
                                    'startup_runtime_state_service_permit_gateway_startup\s*\(',
                                    'startup_runtime_state_service_gateway_startup_recovery_allowed\s*\(',
                                    'startup_runtime_state_service_enter_safe_mode\s*\(',
                                    'startup_runtime_state_service_safe_mode_active\s*\(')) {
        if ($mainText -notmatch $rootRequirement) {
            $failures += "main composition wiring missing $rootRequirement"
        }
    }
    if ($mainText -match '\bs_(?:safe_mode_active|gateway_startup_allowed|startup_sequence_complete|task_state_lock|boot_provisioning_staged|boot_session_id)\b') {
        $failures += 'main.c still owns startup admission flags or their dedicated root lock'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for startup runtime state test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_startup_runtime_state_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host startup runtime state compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host startup runtime state test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("startup runtime state service check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'startup runtime state service check passed: atomic SAFE_MODE/startup/Gateway admission facts and composition wiring are intact'
