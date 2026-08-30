[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$header = Join-Path $projectRoot 'main\services\media_transfer_service.h'
$source = Join-Path $projectRoot 'main\services\media_transfer_service.c'
$main = Join-Path $projectRoot 'main\main.c'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_media_transfer_service.c'
$failures = @()

foreach ($path in @($header, $source, $main, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ((Test-Path -LiteralPath $header) -and (Test-Path -LiteralPath $source) -and
    (Test-Path -LiteralPath $main)) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    $mainText = Get-Content -LiteralPath $main -Raw
    foreach ($api in @('media_transfer_service_host_t',
                         'media_transfer_service_init',
                         'media_transfer_service_begin_server_audio_wake_lease',
                         'media_transfer_service_finish_server_audio_wake_lease',
                         'media_transfer_service_begin_optional_wake_lease',
                         'media_transfer_service_finish_optional_wake_lease',
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
