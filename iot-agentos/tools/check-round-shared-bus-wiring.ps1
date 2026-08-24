[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$audio = Join-Path $projectRoot 'main\round_audio_service.c'
$adapter = Join-Path $projectRoot 'main\boards\round_audio_codec_adapter.h'
$peripheral = Join-Path $projectRoot 'main\round_peripheral_service.c'
$lifecycle = Join-Path $projectRoot 'main\round_audio_lifecycle.h'
$failures = @()

foreach ($path in @($audio, $adapter, $peripheral, $lifecycle)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if ($failures.Count -eq 0) {
    $audioText = Get-Content -LiteralPath $audio -Raw
    $adapterText = Get-Content -LiteralPath $adapter -Raw
    $peripheralText = Get-Content -LiteralPath $peripheral -Raw
    $lifecycleText = Get-Content -LiteralPath $lifecycle -Raw

    foreach ($required in @(
            'shared_bus_lifecycle_reset\(',
            'FAULT_DOMAIN_ID_SHARED_BUS',
            'shared_bus_lifecycle_acquire\(',
            'FAULT_DOMAIN_SELF_TEST',
            'shared_bus_lifecycle_begin_recovery\(',
            'shared_bus_lifecycle_mark_detached\(')) {
        if ($audioText -notmatch $required) { $failures += "round Audio owner missing lifecycle action $required" }
    }
    foreach ($required in @(
            'round_audio_lifecycle_shared_bus_begin_bootstrap\(',
            'round_audio_lifecycle_shared_bus_mark_attached\(',
            'round_audio_lifecycle_shared_bus_begin_self_test\(',
            'round_audio_lifecycle_shared_bus_mark_ready\(',
            'round_audio_lifecycle_shared_bus_finish_teardown\(')) {
        if ($adapterText -notmatch $required) { $failures += "codec adapter lifecycle wiring missing $required" }
    }
    foreach ($required in @(
            'const esp_err_t peripheral_cleanup_err\s*=\s*round_peripheral_lifecycle_detach\(',
            'const esp_err_t codec_cleanup_err\s*=\s*round_audio_adapter_release_codecs\(',
            'const esp_err_t bus_cleanup_err\s*=\s*round_audio_adapter_delete_codec_bus\(',
            'peripheral_cleanup_err\s*!=\s*ESP_OK',
            'codec_cleanup_err\s*!=\s*ESP_OK')) {
        if ($adapterText -notmatch $required) { $failures += "codec teardown does not propagate peripheral cleanup evidence: $required" }
    }
    foreach ($required in @(
            'round_audio_lifecycle_shared_bus_codec_control_begin\(',
            'round_audio_lifecycle_shared_bus_codec_control_end\(')) {
        if ($adapterText -notmatch $required) { $failures += "codec I2C control is not lifecycle-fenced: $required" }
    }
    foreach ($required in @(
            'round_audio_lifecycle_shared_bus_borrow_begin\(',
            'round_audio_lifecycle_shared_bus_borrow_end\(')) {
        if ($peripheralText -notmatch $required) { $failures += "peripheral observation is not lifecycle-fenced: $required" }
    }
    if ($peripheralText -notmatch 'esp_err_t\s+round_peripheral_lifecycle_detach\s*\(' -or
        $peripheralText -notmatch 'return\s+round_peripheral_adapter_release\s*\(') {
        $failures += 'peripheral lifecycle detach must return its adapter cleanup outcome'
    }
    if ($lifecycleText -match 'i2c_master_bus_handle_t') {
        $failures += 'round lifecycle header leaked an I2C driver handle'
    }
    foreach ($required in @(
            'round_audio_lifecycle_recover_shared_bus\s*\(',
            'shared_bus_recovery_transaction_execute\s*\(',
            'round_audio_lifecycle_recovery_quiesce',
            'round_audio_lifecycle_recovery_detach_peripherals',
            'round_audio_lifecycle_recovery_detach_codecs',
            'round_audio_lifecycle_recovery_delete_bus',
            'round_audio_lifecycle_recovery_create_bus',
            'round_audio_lifecycle_recovery_self_test',
            'round_audio_lifecycle_recovery_status_to_esp_err\s*\(',
            'round_audio_lifecycle_recovery_status_to_esp_err\(result\.status\)',
            'remaining_after_audio_lock',
            'round_audio_lifecycle_recovery_remaining_timeout\(&context\)',
            'round_audio_lifecycle_shared_bus_lock_for\(remaining_after_audio_lock\)')) {
        if ($audioText -notmatch $required) { $failures += "round Audio recovery wiring missing $required" }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("round shared-bus wiring check failed:`n" + ($failures -join "`n"))
    exit 1
}

Write-Output 'round shared-bus wiring check passed: circular bring-up and explicit recovery preserve owner ordering, and peripheral plus runtime codec control are admission-fenced'
