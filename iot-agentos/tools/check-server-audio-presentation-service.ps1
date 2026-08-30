[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$header = Join-Path $projectRoot 'main\services\server_audio_presentation_service.h'
$source = Join-Path $projectRoot 'main\services\server_audio_presentation_service.c'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_server_audio_presentation_service.c'
$main = Join-Path $projectRoot 'main\main.c'
$failures = @()

foreach ($path in @($header, $source, $testSource, $main)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ($failures.Count -eq 0) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    $mainText = Get-Content -LiteralPath $main -Raw
    foreach ($api in @('server_audio_presentation_service_host_t',
                        'server_audio_presentation_service_init',
                        'server_audio_presentation_service_mime_supported',
                        'server_audio_presentation_service_url_allowed',
                        'server_audio_presentation_service_play',
                        'server_audio_presentation_service_error_is_permanent')) {
        if ($headerText -notmatch ("\b$api\b")) {
            $failures += "server audio presentation contract missing $api"
        }
    }
    if ($headerText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|QueueHandle_t|cJSON|http|mp3_player|audio_arbitration|CONFIG_MACLAW_BOARD_)\b') {
        $failures += 'server audio presentation public contract leaked SDK/RTOS/JSON/HTTP/codec/audio/board detail'
    }
    foreach ($implementation in @('payload_is_mp3', 'play_mp3', 'play_wav',
                                  'server_audio_presentation_service_url_allowed',
                                  'DEVICE_STATUS_INVALID_ARGUMENT',
                                  'DEVICE_STATUS_BUSY', 'DEVICE_STATUS_TIMEOUT')) {
        if ($sourceText -notmatch $implementation) {
            $failures += "server audio presentation implementation missing $implementation"
        }
    }
    if ($sourceText -match '\b(?:cJSON|esp_http|freertos/|mp3_player|audio_arbitration|scene_presenter|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $failures += 'server audio presentation implementation absorbed JSON/HTTP/RTOS/codec/audio-renderer/board ownership'
    }
    if ($mainText -match '\b(?:audio_payload_is_mp3|audio_error_is_permanent)\b' -or
        $mainText -notmatch 'server_audio_presentation_service_init\s*\(' -or
        $mainText -notmatch 'server_audio_presentation_service_play\s*\(') {
        $failures += 'main.c still owns server-audio format/presentation policy or does not wire the service'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for server audio presentation policy test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_server_audio_presentation_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $source $testSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host server audio presentation contract compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host server audio presentation policy test failed (exit $LASTEXITCODE)"
        }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("server audio presentation check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'server audio presentation check passed: format policy is private and root exposes only audio renderer callbacks'
