[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$header = Join-Path $projectRoot 'main\services\startup_welcome_service.h'
$source = Join-Path $projectRoot 'main\services\startup_welcome_service.c'
$main = Join-Path $projectRoot 'main\main.c'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_startup_welcome_service.c'
$failures = @()

foreach ($path in @($header, $source, $main, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ((Test-Path -LiteralPath $header) -and (Test-Path -LiteralPath $source) -and
    (Test-Path -LiteralPath $main)) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    $mainText = Get-Content -LiteralPath $main -Raw
    foreach ($api in @('startup_welcome_service_host_t',
                         'startup_welcome_service_init',
                         'startup_welcome_service_note_handshake_queued',
                         'startup_welcome_service_begin_sequence',
                         'startup_welcome_service_mark_startup_failed',
                         'startup_welcome_service_wait_for_completion',
                         'startup_welcome_service_gate_active',
                         'startup_welcome_service_should_discard_current',
                         'startup_welcome_service_complete_current')) {
        if ($headerText -notmatch ("\b$api\b")) {
            $failures += "startup Welcome public contract missing $api"
        }
    }
    if ($headerText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|QueueHandle_t|heap_caps|wifi_|netif|http|cJSON|CONFIG_MACLAW_BOARD_)\b') {
        $failures += 'startup Welcome public contract leaked SDK/RTOS/network/HTTP/JSON/board detail'
    }
    foreach ($fence in @('s_completion', 's_handshake_queued', 's_gate_active',
                          's_timed_out', 's_consumed', 'xSemaphoreTake',
                          'startup_welcome_service_mark_startup_failed')) {
        if ($sourceText -notmatch $fence) {
            $failures += "startup Welcome lifecycle fence missing $fence"
        }
    }
    foreach ($rootRequirement in @('startup_welcome_service_init\s*\(',
                                    'startup_welcome_service_note_handshake_queued\s*\(',
                                    'startup_welcome_service_begin_sequence\s*\(',
                                    'startup_welcome_service_mark_startup_failed\s*\(',
                                    'startup_welcome_service_wait_for_completion\s*\(',
                                    'startup_welcome_service_gate_active\s*\(',
                                    'startup_welcome_service_should_discard_current\s*\(',
                                    'startup_welcome_service_complete_current\s*\(')) {
        if ($mainText -notmatch $rootRequirement) {
            $failures += "main composition wiring missing $rootRequirement"
        }
    }
    if ($mainText -match '\bs_startup_welcome_(?:done|gate_active|timed_out|consumed)\b|\bs_handshake_startup_welcome_queued\b') {
        $failures += 'main.c still owns startup Welcome completion/gate/consumption state'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for startup Welcome contract test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_startup_welcome_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host startup Welcome contract compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host startup Welcome contract test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("startup Welcome service check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'startup Welcome service check passed: value contract, one-shot gate, timeout closure, and composition wiring are intact'
