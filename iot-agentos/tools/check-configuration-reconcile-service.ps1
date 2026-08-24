[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\configuration_reconcile_service.c'
$header = Join-Path $projectRoot 'main\configuration_reconcile_service.h'
$main = Join-Path $projectRoot 'main\main.c'
$cmake = Join-Path $projectRoot 'main\CMakeLists.txt'
$failures = @()

foreach ($path in @($source, $header, $main, $cmake)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if (Test-Path -LiteralPath $header) {
    $text = Get-Content -LiteralPath $header -Raw
    foreach ($required in @(
            'configuration_reconcile_service_init\s*\(',
            'configuration_reconcile_service_reconcile\s*\(',
            'configuration_reconcile_service_apply_runtime_override\s*\(',
            'configuration_reconcile_service_get_snapshot\s*\(',
            'CONFIGURATION_RECONCILE_REASON_BOOT_RESTORE',
            'CONFIGURATION_RECONCILE_REASON_RUNTIME_POLICY')) {
        if ($text -notmatch $required) { $failures += "reconcile service public contract missing $required" }
    }
    if ($text -cmatch '\b(?:esp_|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|heap_caps|board_)\b') {
        $failures += 'reconcile service public contract leaked platform/hardware detail'
    }
}

if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    foreach ($required in @(
            'configuration_service_load_effective_revisioned_snapshot\s*\(',
            'configuration_apply_state_begin_with_requirements\s*\(',
            'apply_output_volume',
            'apply_display_brightness',
            'requested_output_volume',
            'requested_display_brightness',
            'configuration_apply_state_begin_with_requirements',
            'configuration_apply_state_output_volume_needs_apply\s*\(',
            'configuration_apply_state_display_brightness_needs_apply\s*\(',
            'configuration_apply_state_screen_sleep_seconds_needs_apply\s*\(',
            'configuration_apply_state_t\s+apply_state\s*=\s*s_apply_state',
            's_apply_state\s*=\s*apply_state',
            'audio_arbitration_set_output_volume\s*\(',
            'scene_presenter_apply_remote_brightness\s*\(',
            'scene_presenter_apply_display_off_idle_policy\s*\(',
            'audio_service_get_output_volume_state\s*\(',
            'display_service_get_brightness_state\s*\(',
            'scene_presenter_get_display_off_idle_policy_state\s*\(',
            'schedule_required',
            'schedule_armed',
            'observation_from_volume_ack\s*\(',
            'observation_from_brightness_ack\s*\(',
            'observation_from_idle_policy_ack\s*\(',
            'status_from_observation\s*\(',
            'retain_first_failure\s*\(',
            'configuration_apply_state_record_output_volume\s*\(',
            'configuration_apply_state_record_display_brightness\s*\(',
            'configuration_apply_state_record_screen_sleep_seconds\s*\(',
            'configuration_service_next_runtime_override_expiry_ms\s*\(',
            'esp_timer_start_once\s*\(',
            'configuration_reconcile_retry_delay_ms\s*\(',
            'CONFIGURATION_RECONCILE_NOTIFY_RETRY',
            's_retry_due_us',
            'now_us\s*>=\s*s_retry_due_us',
            's_retry_generation',
            's_retry_delivered_generation',
            's_retry_delivered_generation\s*==\s*s_retry_generation',
            'esp_timer_start_once\(s_retry_timer, delay_us\)\s*==\s*ESP_OK',
            'retryable_status\(status\)',
            'rearm_expiry_timer_under_mutex\s*\(',
            'status == DEVICE_STATUS_BUSY',
            's_initialized && s_stopping',
            'CONFIGURATION_RUNTIME_OVERRIDE_VALUE_TRANSPORT_SELECTION',
            'DEVICE_STATUS_UNAVAILABLE',
            'CONFIGURATION_APPLY_OBSERVATION_UNKNOWN')) {
        if ($text -notmatch $required) { $failures += "reconcile service implementation missing $required" }
    }
    if ($text -notmatch 'return\s+observation\s*==\s*CONFIGURATION_APPLY_OBSERVATION_APPLIED\s*\?\s*DEVICE_STATUS_OK\s*:\s*DEVICE_STATUS_INTERNAL_ERROR') {
        $failures += 'receipt mismatch must not be returned as a successful reconciliation'
    }
    if ($text -notmatch 'reason\s*!=\s*CONFIGURATION_RECONCILE_REASON_BOOT_RESTORE\s*\|\|\s*effective->snapshot.output_volume_saved') {
        $failures += 'boot/default output-volume requirement must remain explicit at the reconcile owner'
    }
    if ($text -notmatch 'reason\s*==\s*CONFIGURATION_RECONCILE_REASON_BOOT_RESTORE') {
        $failures += 'boot-visible brightness omission must remain explicit at the reconcile owner'
    }
    if ($text -notmatch 'never hold the spinlock while invoking them') {
        $failures += 'consumer calls must remain outside the state publication critical section'
    }
    if ($text -notmatch 'stopped/rearmed esp_timer callback') {
        $failures += 'retry worker must discard callbacks whose timer generation was superseded'
    }
    $reconcileStart = $text.LastIndexOf('static device_status_t reconcile_internal(')
    $reconcileEnd = $text.IndexOf('device_status_t configuration_reconcile_service_apply_runtime_override(', $reconcileStart)
    if ($reconcileStart -lt 0 -or $reconcileEnd -lt $reconcileStart) {
        $failures += 'cannot locate serialized reconcile owner for timer ordering audit'
    } else {
        $reconcile = $text.Substring($reconcileStart, $reconcileEnd - $reconcileStart)
        $schedule = $reconcile.IndexOf('schedule_or_cancel_retry(status, reason, authorization)')
        $release = $reconcile.IndexOf('xSemaphoreGive(s_mutex)')
        if ($schedule -lt 0 -or $release -lt 0 -or $schedule -gt $release) {
            $failures += 'retry timer decision must occur before serialized reconcile mutex release'
        }
        $expiryRearm = $reconcile.IndexOf('rearm_expiry_timer_under_mutex()')
        if ($expiryRearm -lt 0 -or $expiryRearm -gt $release) {
            $failures += 'expiry timer rearm must occur before serialized reconcile mutex release'
        }
        if ($reconcile -notmatch '!schedule_or_cancel_retry\(status, reason, authorization\)\s*&&\s*retryable_status\(status\)') {
            $failures += 'retry timer arm failure must remain observable as a degraded reconcile result'
        }
    }
}

$sceneHeader = Join-Path $projectRoot 'main\presentation\scene_presenter.h'
$sceneSource = Join-Path $projectRoot 'main\presentation\scene_presenter.c'
$appUiHeader = Join-Path $projectRoot 'main\app_ui.h'
$appUiSource = Join-Path $projectRoot 'main\app_ui.c'
foreach ($path in @($sceneHeader, $sceneSource, $appUiHeader, $appUiSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing idle-policy acknowledgement path $path" }
}
if ((Test-Path -LiteralPath $sceneHeader) -and (Test-Path -LiteralPath $sceneSource) -and
    ((Get-Content -LiteralPath $sceneHeader -Raw) -notmatch 'scene_presenter_get_display_off_idle_policy_state\s*\(' -or
     (Get-Content -LiteralPath $sceneSource -Raw) -notmatch 'app_ui_get_display_off_idle_policy_state\s*\(')) {
    $failures += 'Scene Presenter is missing idle-policy acknowledgement translation'
}
if (Test-Path -LiteralPath $appUiSource) {
    $uiText = Get-Content -LiteralPath $appUiSource -Raw
    $applyStart = $uiText.IndexOf('device_status_t app_ui_apply_display_off_idle_policy')
    $ack = $uiText.IndexOf('s_display_off_idle_policy_known = true', $applyStart)
    $foregroundReturn = $uiText.IndexOf('model.alarm_visual_active) return DEVICE_STATUS_OK', $applyStart)
    if ($applyStart -lt 0 -or $ack -lt $applyStart -or
        ($foregroundReturn -ge 0 -and $ack -gt $foregroundReturn)) {
        $failures += 'idle-policy acknowledgement must be published before a foreground scheduling no-op returns'
    }
    foreach ($required in @(
            'device_power_get_snapshot\s*\(',
            'device_power_cancel_display_off\s*\(',
            'const\s+device_status_t\s+status\s*=\s*device_power_cancel_display_off\s*\(',
            'if\s*\(status\s*!=\s*DEVICE_STATUS_OK\)',
            's_display_off_idle_replacement_pending',
            's_display_off_idle_schedule_required',
            's_display_off_idle_schedule_armed',
            'note_idle_policy_schedule_observation\s*\(')) {
        if ($uiText -notmatch $required) {
            $failures += "idle-policy scheduling evidence missing $required"
        }
    }
}

$deviceApiHeader = Join-Path $projectRoot 'main\device_api.h'
$powerHeader = Join-Path $projectRoot 'main\power_service.h'
foreach ($path in @($deviceApiHeader, $powerHeader)) {
    if (-not (Test-Path -LiteralPath $path)) {
        $failures += "missing observable DISPLAY_OFF cancellation contract $path"
        continue
    }
    $text = Get-Content -LiteralPath $path -Raw
    $symbol = if ($path -eq $deviceApiHeader) {
        'device_power_cancel_display_off'
    } else {
        'power_service_cancel_display_off'
    }
    if ($text -notmatch ("device_status_t\s+" + $symbol + "\s*\(void\)")) {
        $failures += "DISPLAY_OFF cancellation must expose device_status_t: $symbol"
    }
}

if (Test-Path -LiteralPath $main) {
    $text = Get-Content -LiteralPath $main -Raw
    foreach ($required in @(
            'configuration_reconcile_service_init\s*\(',
            'configuration_reconcile_service_reconcile\s*\(',
            'configuration_reconcile_output_volume_applied\s*\(',
            'configuration_reconcile_display_brightness_applied\s*\(',
            'configuration_reconcile_screen_sleep_applied\s*\(')) {
        if ($text -notmatch $required) { $failures += "composition root reconciliation wiring missing $required" }
    }
    $gateway = [regex]::Match($text, 'static void gateway_host_handle_hardware_config\([\s\S]*?\n\}')
    if (-not $gateway.Success) {
        $failures += 'cannot locate gateway hardware configuration owner'
    } else {
        if ($gateway.Value -match 'audio_arbitration_set_output_volume\s*\(' -or
            $gateway.Value -match 'scene_presenter_apply_remote_brightness\s*\(') {
            $failures += 'gateway hardware config bypasses the reconciliation service'
        }
    }
    $rollback = $text.IndexOf('configuration_reconcile_service_deinit')
    $configurationDeinit = $text.IndexOf('configuration_service_deinit')
    if ($rollback -lt 0 -or $configurationDeinit -lt 0 -or $rollback -gt $configurationDeinit) {
        $failures += 'startup rollback must join reconcile timer/worker before Configuration deinit'
    }
}

if ((Test-Path -LiteralPath $cmake) -and
    ((Get-Content -LiteralPath $cmake -Raw) -notmatch '"configuration_reconcile_service\.c"')) {
    $failures += 'reconcile service source is not compiled by the main component'
}

if ($failures.Count -gt 0) {
    Write-Error ("configuration reconcile service check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'configuration reconcile service check passed: one revision-bound coordinator owns audio/display policy application'
