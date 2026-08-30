[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$configuration = Join-Path $projectRoot 'main\configuration_service.c'
$header = Join-Path $projectRoot 'main\configuration_service.h'
$mainSource = Join-Path $projectRoot 'main\main.c'
$failures = @()

foreach ($path in @($configuration, $header, $mainSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if (Test-Path -LiteralPath $header) {
    $text = Get-Content -LiteralPath $header -Raw
    foreach ($required in @(
            'configuration_display_policy_update_t',
            'CONFIGURATION_DISPLAY_POLICY_UPDATE_ABI_VERSION',
            'has_brightness',
            'has_screen_sleep_seconds',
            'configuration_service_apply_display_policy_with_policy\s*\(')) {
        if ($text -notmatch $required) { $failures += "display policy public contract missing $required" }
    }
    if ($text -cmatch '\b(?:esp_|freertos/|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|heap_caps|board_)\b') {
        $failures += 'display policy public contract leaked platform/network/RTOS detail'
    }
}

if (Test-Path -LiteralPath $configuration) {
    $text = Get-Content -LiteralPath $configuration -Raw
    foreach ($required in @(
            'configuration_service_apply_display_policy_with_policy_legacy\s*\(',
            'CONFIGURATION_KEY_DISPLAY_BRIGHTNESS',
            'CONFIGURATION_KEY_SCREEN_SLEEP_SECONDS',
            'configuration_transaction_apply_confirmed_policy\s*\(',
            'write_locked\s*\(snapshot,\s*force_setup\)',
            '\*out_revision\s*=\s*s_scratch_store->revision')) {
        if ($text -notmatch $required) { $failures += "display policy Configuration transaction missing $required" }
    }
}

if (Test-Path -LiteralPath $mainSource) {
    $text = Get-Content -LiteralPath $mainSource -Raw
    foreach ($required in @(
            'persist_hub_display_policy\s*\(',
            'request->display_policy',
            'configuration_service_apply_display_policy_with_policy\s*\(',
            'display policy persistence failed; no Display/Power apply',
            'configuration_reconcile_service_reconcile\s*\(',
            'configuration_reconcile_screen_sleep_applied\s*\(')) {
        if ($text -notmatch $required) { $failures += "display policy composition ordering missing $required" }
    }
    $handler = [regex]::Match($text,
        'static void gateway_host_handle_hardware_config\([\s\S]*?\n\}',
        [System.Text.RegularExpressions.RegexOptions]::Singleline)
    if (-not $handler.Success) {
        $failures += 'cannot inspect gateway hardware-config handler'
    } else {
        $body = $handler.Value
        $persist = $body.IndexOf('persist_hub_display_policy')
        $reconcile = $body.IndexOf('configuration_reconcile_service_reconcile')
        $legacyBrightness = $body.IndexOf('scene_presenter_apply_remote_brightness')
        $legacySleep = $body.IndexOf('scene_presenter_set_display_off_idle_ms')
        if ($persist -lt 0 -or $reconcile -lt 0 -or
            $legacyBrightness -ge 0 -or $legacySleep -ge 0) {
            $failures += 'gateway must publish display policy before its single reconciliation owner applies it'
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("configuration display policy check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'configuration display policy check passed: Hub display values publish together before Display/Power apply'
