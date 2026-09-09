[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$header = Join-Path $projectRoot 'main\services\media_transfer_service.h'
$source = Join-Path $projectRoot 'main\services\media_transfer_service.c'
$main = Join-Path $projectRoot 'main\main.c'
$dispatcher = Join-Path $projectRoot 'main\services\gateway_dispatcher.c'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_media_transfer_service.c'
$failures = @()

foreach ($path in @($header, $source, $main, $dispatcher, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ((Test-Path -LiteralPath $header) -and (Test-Path -LiteralPath $source) -and
    (Test-Path -LiteralPath $main) -and (Test-Path -LiteralPath $dispatcher)) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    $mainText = Get-Content -LiteralPath $main -Raw
    $dispatcherText = Get-Content -LiteralPath $dispatcher -Raw
    foreach ($api in @('media_transfer_service_host_t',
                         'media_transfer_service_init',
                         'media_transfer_service_begin_server_audio_wake_lease',
                         'media_transfer_service_finish_server_audio_wake_lease',
                         'media_transfer_service_begin_optional_wake_lease',
                         'media_transfer_service_finish_optional_wake_lease',
                         'media_transfer_service_prepare_system_sleep',
                         'media_transfer_service_abort_system_sleep_prepare',
                         'media_transfer_service_take_lane',
                         'media_transfer_service_release_lane')) {
        if ($headerText -notmatch ("\b$api\b")) {
            $failures += "media transfer public contract missing $api"
        }
    }
    if ($headerText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|QueueHandle_t|heap_caps|wifi_|netif|http|cJSON|CONFIG_MACLAW_BOARD_)\b') {
        $failures += 'media transfer public contract leaked SDK/RTOS/network/HTTP/JSON/board detail'
    }
    foreach ($fence in @('s_lane_mutex', 's_wake_memory_lease_count',
                          'MEDIA_TRANSFER_MAX_OPTIONAL_WAKE_LEASES',
                          's_optional_wake_lease_owner',
                          's_lane_owner',
                          's_server_audio_wake_lease_owner',
                          'xTaskGetCurrentTaskHandle',
                          's_server_audio_wake_lease_active',
                          's_audio_download_active', 'xSemaphoreTake',
                          'cancel_startup_pet_for_server_audio',
                          'take_startup_pet_audio_preemption',
                          'rearm_preempted_startup_pet',
                          'schedule_wake_restart')) {
        if ($sourceText -notmatch $fence) {
            $failures += "media transfer lifecycle/priority fence missing $fence"
        }
    }
    if ($sourceText -notmatch 's_system_sleep_preparing\s*=\s*true' -or
        $sourceText -notmatch '!s_system_sleep_preparing' -or
        $sourceText -notmatch 's_lane_owner\s*==\s*NULL' -or
        $sourceText -notmatch '!s_server_audio_wake_lease_active') {
        $failures += 'media System Sleep PREPARE must close admission and wait for lane/wake-lease drain'
    }
    if ($sourceText -notmatch 'media_transfer_service_abort_system_sleep_prepare\s*\(' -or
        $sourceText -notmatch 's_system_sleep_preparing\s*=\s*false') {
        $failures += 'media System Sleep ABORT must reopen the retained admission fence'
    }
    if ($sourceText -notmatch 's_lane_owner\s*=\s*current' -or
        $sourceText -notmatch 'owner\s*==\s*xTaskGetCurrentTaskHandle\(\)' -or
        $sourceText -notmatch 's_server_audio_wake_lease_owner\s*=\s*xTaskGetCurrentTaskHandle\(\)' -or
        $sourceText -notmatch 's_optional_wake_lease_owner\[.*\]\s*=\s*current' -or
        $sourceText -notmatch 's_optional_wake_lease_owner\[.*\]\s*==\s*current') {
        $failures += 'media lane ownership must be task-scoped and fail closed on non-owner release'
    }
    if ($sourceText -notmatch 'if\s*\(\s*!completed\s*\)\s*return false') {
        $failures += 'optional wake lease finish must fail closed when caller owns no lease'
    }
    foreach ($rootRequirement in @(
        'media_transfer_service_init\s*\(',
        'media_transfer_service_begin_server_audio_wake_lease\s*\(',
        'media_transfer_service_finish_server_audio_wake_lease\s*\(',
        'media_transfer_service_begin_optional_wake_lease\s*\(',
        'media_transfer_service_finish_optional_wake_lease\s*\(',
        'media_transfer_service_take_lane\s*\(')) {
        if ($mainText -notmatch $rootRequirement) {
            $failures += "main composition wiring missing $rootRequirement"
        }
    }
    if ($mainText -match '\bs_(?:media_transfer_mutex|audio_media_download_active|media_wake_memory_lease_count|server_audio_wake_memory_lease_active)\b') {
        $failures += 'main.c still owns media lane, foreground priority, or wake-memory lease state'
    }
    if ($mainText -notmatch '(?s)media_transfer_service_take_lane\(35000\).*?\{.*?media_transfer_service_finish_server_audio_wake_lease\(\).*?return ESP_ERR_TIMEOUT') {
        $failures += 'server-audio download must release its wake lease when media lane admission times out'
    }
    if ($dispatcherText -notmatch '(?s)inline_wake_lease_acquired\s*=\s*s_host\.begin_server_audio_wake_lease\(.*?\).*?if\s*\(\s*!inline_wake_lease_acquired\s*\).*?audio_permanently_invalid' -or
        $dispatcherText -notmatch 'server audio wake lease busy; deferring inline payload') {
        $failures += 'inline server audio must fail closed when its singleton wake lease is busy'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for media transfer contract test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_media_transfer_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host media transfer contract compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host media transfer contract test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("media transfer service check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'media transfer service check passed: value contract, foreground priority, wake-memory lease, and composition wiring are intact'
