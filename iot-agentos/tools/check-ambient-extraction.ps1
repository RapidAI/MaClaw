[CmdletBinding()]
param(
    [switch]$SkipHostTest
)

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$mainC = Join-Path $projectRoot 'main\main.c'
$failures = @()
$hostTestStatus = 'skipped'

function Assert-FileLacks([string]$Path, [string]$Pattern, [string]$Why) {
    $hits = Select-String -Path $Path -Pattern $Pattern
    if ($hits) {
        $script:failures += "${Why}: found /$Pattern/ in $Path ($($hits.Count) hit(s))"
    }
}

foreach ($ident in @(
        's_weather_summary',
        's_weather\b',
        's_display_clock_epoch',
        's_display_clock_valid',
        's_display_clock_anchor',
        'refresh_ambient_display',
        'start_ambient_clock_task',
        'apply_ambient_json',
        'apply_glyphs_json',
        'glyph_codepoint_from_key'
    )) {
    Assert-FileLacks $mainC $ident "composition-root dual-write leftover"
}

$cFiles = Get-ChildItem -LiteralPath (Join-Path $projectRoot 'main') -Filter '*.c' -File -Recurse
$ambientCalls = $cFiles | Select-String -Pattern 'app_ui_set_ambient\s*\('
$badCalls = @($ambientCalls | Where-Object {
        $_.Path -notmatch '[\\/]app_ui\.c$' -and
        $_.Path -notmatch '[\\/]scene_presenter\.c$'
    })
if ($badCalls.Count -gt 0) {
    $failures += "app_ui_set_ambient leaked outside presenter: $($badCalls.Filename -join ', ')"
}
$presenterHit = @($ambientCalls | Where-Object { $_.Path -match '[\\/]scene_presenter\.c$' })
if ($presenterHit.Count -lt 1) {
    $failures += 'scene_presenter.c must call app_ui_set_ambient'
}

foreach ($api in @(
        @{ Name = 'app_ui_set_wifi_status'; MustCall = $true },
        @{ Name = 'app_ui_set_alarm_scheduled'; MustCall = $true },
        @{ Name = 'app_ui_set_pet_state'; MustCall = $true },
        @{ Name = 'app_ui_set_pet_profile'; MustCall = $true },
        @{ Name = 'app_ui_set_pet_asset'; MustCall = $true },
        @{ Name = 'app_ui_set_pet_asset_consuming'; MustCall = $true },
        @{ Name = 'app_ui_cache_glyph'; MustCall = $true },
        @{ Name = 'app_ui_set_command_stage'; MustCall = $true },
        @{ Name = 'app_ui_set_recording_mode'; MustCall = $true },
        @{ Name = 'app_ui_set_recording_visual'; MustCall = $true },
        @{ Name = 'app_ui_set_audio_level'; MustCall = $true },
        @{ Name = 'app_ui_push_recording_pcm'; MustCall = $true },
        @{ Name = 'app_ui_set_alarm_visual'; MustCall = $true },
        @{ Name = 'app_ui_show_text'; MustCall = $true },
        @{ Name = 'app_ui_show_response'; MustCall = $true },
        @{ Name = 'app_ui_show_response_image'; MustCall = $true },
        @{ Name = 'app_ui_show_upload_progress'; MustCall = $true },
        @{ Name = 'app_ui_show_ready_prompt'; MustCall = $true },
        @{ Name = 'app_ui_cancel_ready_prompt'; MustCall = $true },
        @{ Name = 'app_ui_show_qrcode_modules'; MustCall = $true },
        @{ Name = 'app_ui_show_startup_screen'; MustCall = $true },
        @{ Name = 'app_ui_set_command_display_lock'; MustCall = $true },
        @{ Name = 'app_ui_set_command_cancel_enabled'; MustCall = $true },
        @{ Name = 'app_ui_navigate_response'; MustCall = $true },
        @{ Name = 'app_ui_dismiss_response'; MustCall = $true },
        @{ Name = 'app_ui_restore_standby'; MustCall = $true },
        @{ Name = 'app_ui_wake_from_idle'; MustCall = $true },
        @{ Name = 'app_ui_set_service_ready'; MustCall = $true },
        @{ Name = 'app_ui_apply_display_off_idle_policy'; MustCall = $true },
        @{ Name = 'app_ui_apply_remote_brightness'; MustCall = $true },
        @{ Name = 'app_ui_note_schedule_display_wake'; MustCall = $true }
    )) {
    $hits = $cFiles | Select-String -Pattern ($api.Name + '\s*\(')
    $bad = @($hits | Where-Object {
            $_.Path -notmatch '[\\/]app_ui\.c$' -and
            $_.Path -notmatch '[\\/]scene_presenter\.c$'
        })
    if ($bad.Count -gt 0) {
        $failures += "$($api.Name) leaked outside presenter: $($bad.Filename -join ', ')"
    }
    $ok = @($hits | Where-Object { $_.Path -match '[\\/]scene_presenter\.c$' })
    if ($api.MustCall -and $ok.Count -lt 1) {
        $failures += "scene_presenter.c must call $($api.Name)"
    }
}

$cmake = Get-Content -LiteralPath (Join-Path $projectRoot 'main\CMakeLists.txt') -Raw
foreach ($src in @(
        'services/ambient_service.c',
        'services/clock_sync_service.c',
        'presentation/scene_presenter.c'
    )) {
    if ($cmake -notlike "*$src*") {
        $failures += "CMakeLists.txt missing $src"
    }
}

foreach ($rel in @(
        'main\services\ambient_service.c',
        'main\services\ambient_service.h',
        'main\services\clock_sync_service.c',
        'main\services\clock_sync_service.h',
        'main\presentation\scene_model.h',
        'main\presentation\scene_presenter.c',
        'main\presentation\scene_presenter.h'
    )) {
    $path = Join-Path $projectRoot $rel
    if (-not (Test-Path -LiteralPath $path)) {
        $failures += "missing $rel"
        continue
    }
    Assert-FileLacks $path 'board_port_' "new increment must not call board_port"
    Assert-FileLacks $path 'CONFIG_MACLAW_BOARD_' "new increment must not branch on board type"
}

Assert-FileLacks (Join-Path $projectRoot 'main\services\ambient_service.h') `
    '#include\s*[<"]cJSON' 'ambient public header must not include the JSON parser'
$clockSyncHeader = Join-Path $projectRoot 'main\services\clock_sync_service.h'
Assert-FileLacks $clockSyncHeader `
    '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|cJSON|board_|platform_)\b' `
    'clock-sync public header must remain value-only'
$clockSyncC = Get-Content -LiteralPath (Join-Path $projectRoot 'main\services\clock_sync_service.c') -Raw
foreach ($clockRequirement in @(
        'clock_sync_service_note_authenticated_epoch',
        'clock_sync_service_prepare_system_sleep',
        'clock_sync_service_abort_system_sleep_prepare',
        'esp_netif_sntp_sync_wait',
        's_system_sleep_restart_pending'
    )) {
    if ($clockSyncC -notlike "*$clockRequirement*") {
        $failures += "clock_sync_service.c missing lifecycle/time contract: $clockRequirement"
    }
}
$mainText = Get-Content -LiteralPath $mainC -Raw
foreach ($rootClockRequirement in @(
        'clock_sync_service_init',
        'clock_sync_service_start',
        'clock_sync_service_note_authenticated_epoch',
        'clock_sync_service_stop',
        'clock_sync_service_prepare_system_sleep',
        'clock_sync_service_abort_system_sleep_prepare'
    )) {
    if ($mainText -notlike "*$rootClockRequirement*") {
        $failures += "main.c missing Clock Sync Service composition-root seam: $rootClockRequirement"
    }
}
$ambientC = Get-Content -LiteralPath (Join-Path $projectRoot 'main\services\ambient_service.c') -Raw
if ($ambientC -notlike '*ambient_service_apply_hub_ambient*') {
    $failures += 'ambient_service.c must implement apply_hub_ambient'
}
if ($ambientC -notlike '*mbedtls_base64_decode*') {
    $failures += 'ambient_service.c must decode glyph base64'
}

if (-not $SkipHostTest) {
    $cc = Get-Command gcc -ErrorAction SilentlyContinue
    if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
    if (-not $cc) {
        $hostTestStatus = 'skipped (no gcc/clang)'
        Write-Output 'host scene_presenter test skipped: no gcc/clang on PATH'
    } else {
        $outDir = Join-Path $projectRoot 'build-host-tests'
        New-Item -ItemType Directory -Force -Path $outDir | Out-Null
        $exe = Join-Path $outDir 'test_scene_presenter.exe'
        $testC = Join-Path $PSScriptRoot 'host_tests\test_scene_presenter.c'
        $presenterC = Join-Path $projectRoot 'main\presentation\scene_presenter.c'
        $inc = Join-Path $projectRoot 'main'
        & $cc.Source -std=c11 -Wall -Wextra -Werror -I $inc $testC $presenterC -o $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host scene_presenter compile failed (exit $LASTEXITCODE)"
            $hostTestStatus = 'compile-failed'
        } else {
            & $exe
            if ($LASTEXITCODE -ne 0) {
                $failures += "host scene_presenter test failed (exit $LASTEXITCODE)"
                $hostTestStatus = 'failed'
            } else {
                $hostTestStatus = 'PASS'
            }
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("ambient extraction check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output "ambient extraction check passed: no main.c dual-write, presenter is sole scene App UI caller, hub weather/glyph JSON in ambient_service, host test $hostTestStatus"
exit 0
