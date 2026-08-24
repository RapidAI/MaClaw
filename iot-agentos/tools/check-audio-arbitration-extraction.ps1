[CmdletBinding()]
param(
    [switch]$SkipHostTest
)

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$failures = @()
$hostTestStatus = 'skipped'

function Assert-FileLacks([string]$Path, [string]$Pattern, [string]$Why) {
    $hits = Select-String -Path $Path -Pattern $Pattern
    if ($hits) {
        $script:failures += "${Why}: found /$Pattern/ in $Path ($($hits.Count) hit(s))"
    }
}

$svc = Join-Path $projectRoot 'main\services\audio_arbitration_service.c'
$hdr = Join-Path $projectRoot 'main\services\audio_arbitration_service.h'
if (-not (Test-Path -LiteralPath $svc)) { $failures += 'missing services/audio_arbitration_service.c' }
if (-not (Test-Path -LiteralPath $hdr)) { $failures += 'missing services/audio_arbitration_service.h' }

Assert-FileLacks (Join-Path $projectRoot 'main\services\alarm_service.c') `
    'device_audio_play_alarm_burst' 'alarm must obtain burst via arbitration'
Assert-FileLacks (Join-Path $projectRoot 'main\services\meeting_service.c') `
    'device_audio_stream_' 'meeting must obtain stream via arbitration'
Assert-FileLacks (Join-Path $projectRoot 'main\services\interaction_service.c') `
    'device_audio_capture_wav' 'command capture must obtain wav via arbitration'
Assert-FileLacks (Join-Path $projectRoot 'main\services\interaction_service.c') `
    'device_audio_release_captured_wav' 'command capture release must go via arbitration'
Assert-FileLacks (Join-Path $projectRoot 'main\main.c') `
    'device_audio_play_wav' 'WAV playback must obtain audio via arbitration'
Assert-FileLacks (Join-Path $projectRoot 'main\mp3_player.c') `
    'device_audio_playback_begin' 'PCM playback begin must go via arbitration'
Assert-FileLacks (Join-Path $projectRoot 'main\mp3_player.c') `
    'device_audio_playback_write' 'PCM playback write must go via arbitration'
Assert-FileLacks (Join-Path $projectRoot 'main\mp3_player.c') `
    'device_audio_playback_end' 'PCM playback end must go via arbitration'
Assert-FileLacks (Join-Path $projectRoot 'main\main.c') `
    'device_audio_set_output_volume' 'volume set must go via arbitration'
Assert-FileLacks (Join-Path $projectRoot 'main\main.c') `
    'device_audio_request_playback_stop' 'playback stop token must go via arbitration'
Assert-FileLacks (Join-Path $projectRoot 'main\main.c') `
    'device_wake_word_start\s*\(' 'wake start must go via arbitration'
Assert-FileLacks (Join-Path $projectRoot 'main\presentation\input_binding.c') `
    'device_audio_' 'input binding audio must go via arbitration'
Assert-FileLacks (Join-Path $projectRoot 'main\services\interaction_service.c') `
    'device_audio_' 'interaction audio stop/reset must go via arbitration'
Assert-FileLacks (Join-Path $projectRoot 'main\services\provisioning_service.c') `
    'device_wake_word_pause' 'provisioning wake pause must go via arbitration'

$arbC = Get-Content -LiteralPath $svc -Raw -ErrorAction SilentlyContinue
if ($arbC) {
    foreach ($api in @(
            'audio_service_play_wav',
            'audio_service_playback_begin',
            'audio_service_playback_write',
            'audio_service_playback_end',
            'audio_service_set_output_volume',
            'audio_service_adjust_output_volume',
            'audio_service_request_playback_stop',
            'audio_service_request_capture_stop',
            'audio_service_reset_capture_stop',
            'audio_service_wake_word_start',
            'audio_service_wake_word_stop',
            'audio_service_wake_word_stop_with_timeout',
            'audio_service_wake_word_pause'
        )) {
        if ($arbC -notlike "*$api*") {
            $failures += "audio_arbitration_service.c must call $api"
        }
    }
}

foreach ($path in @($svc, $hdr)) {
    if (Test-Path -LiteralPath $path) {
        Assert-FileLacks $path 'board_port_' 'new increment must not call board_port'
        Assert-FileLacks $path 'CONFIG_MACLAW_BOARD_' 'new increment must not branch on board type'
    }
}

if (Test-Path -LiteralPath $hdr) {
    Assert-FileLacks $hdr '#include\s*[<"](?:esp_|freertos/|httpd)' 'public header must not include ESP-IDF'
    Assert-FileLacks $hdr '\besp_err_t\b' 'public header must not expose esp_err_t'
    Assert-FileLacks $hdr 'foreground_coordinator_set_authoritative\s*\(\s*true' `
        'must not enable coordinator authoritative'
    Assert-FileLacks $hdr 'audio_arbitration_set_authoritative\s*\(\s*true' `
        'must not enable arbitration authoritative'
}

$cmake = Get-Content -LiteralPath (Join-Path $projectRoot 'main\CMakeLists.txt') -Raw
if ($cmake -notlike '*services/audio_arbitration_service.c*') {
    $failures += 'CMakeLists.txt missing services/audio_arbitration_service.c'
}

if (-not $SkipHostTest) {
    $cc = Get-Command gcc -ErrorAction SilentlyContinue
    if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
    if (-not $cc) {
        $hostTestStatus = 'skipped (no gcc/clang)'
        Write-Output 'host audio_arbitration test skipped: no gcc/clang on PATH'
    } else {
        $outDir = Join-Path $projectRoot 'build-host-tests'
        New-Item -ItemType Directory -Force -Path $outDir | Out-Null
        $exe = Join-Path $outDir 'test_audio_arbitration.exe'
        $testC = Join-Path $PSScriptRoot 'host_tests\test_audio_arbitration.c'
        $inc = Join-Path $projectRoot 'main'
        & $cc.Source -std=c11 -Wall -Wextra -Werror -I $inc $testC -o $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host audio_arbitration compile failed (exit $LASTEXITCODE)"
            $hostTestStatus = 'compile-failed'
        } else {
            & $exe
            if ($LASTEXITCODE -ne 0) {
                $failures += "host audio_arbitration test failed (exit $LASTEXITCODE)"
                $hostTestStatus = 'failed'
            } else {
                $hostTestStatus = 'PASS'
            }
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("audio arbitration extraction check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output "audio arbitration extraction check passed: Command/Meeting/Alarm/WAV/PCM/volume/stop/wake use lease wrappers, host test $hostTestStatus"
exit 0
