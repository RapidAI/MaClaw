[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\audio_service.c'
$header = Join-Path $projectRoot 'main\audio_service.h'
$failures = @()
foreach ($path in @($source, $header)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ($failures.Count -eq 0) {
    $sourceText = Get-Content -LiteralPath $source -Raw
    $headerText = Get-Content -LiteralPath $header -Raw
    foreach ($required in @(
            'FAULT_DOMAIN_ID_AUDIO',
            'fault_domain_begin_start',
            'fault_domain_begin_self_test',
            'fault_domain_mark_ready',
            'fault_domain_begin_quiesce',
            'fault_domain_mark_unknown_outcome',
            'audio_service_wake_runtime_mark_stopped',
            's_wake_word_starting')) {
        if ($sourceText -notmatch [regex]::Escape($required)) {
            $failures += "Audio wake fault-domain lifecycle missing $required"
        }
    }
    if ($headerText -notmatch 'audio_service_get_wake_runtime_fault_domain_snapshot' -or
        $headerText -notmatch 'fault_domain\.h') {
        $failures += 'Audio Service must expose only a value-only wake-runtime fault-domain snapshot'
    }
    $startIndex = $sourceText.IndexOf('device_status_t audio_service_wake_word_start')
    $beginIndex = $sourceText.IndexOf('fault_domain_begin_start', $startIndex)
    $platformIndex = $sourceText.IndexOf('platform_audio_wake_word_start', $startIndex)
    if ($startIndex -lt 0 -or $beginIndex -lt 0 -or $platformIndex -lt 0 -or
        $beginIndex -gt $platformIndex) {
        $failures += 'Audio wake start must close the new generation before profile runtime creation'
    }
    $sleepPrepareIndex = $sourceText.IndexOf('device_status_t audio_service_prepare_system_sleep')
    $sleepStartingIndex = if ($sleepPrepareIndex -ge 0) {
        $sourceText.IndexOf('s_wake_word_starting', $sleepPrepareIndex)
    } else { -1 }
    if ($sleepPrepareIndex -lt 0 -or $sleepStartingIndex -lt $sleepPrepareIndex) {
        $failures += 'System Sleep must reject an in-flight wake-runtime start before reporting audio safe'
    }
    if ($sourceText -match '\bfault_domain_[A-Za-z0-9_]+[\s\S]{0,180}\b(?:i2s_|i2c_|codec|round_peripheral|compact_audio)') {
        $failures += 'Audio wake fault domain must not claim codec/I2S/shared-I2C ownership'
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("Audio wake fault-domain check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Audio wake fault-domain check passed: recognizer runtime admission is generation-fenced without claiming codec/I2S/shared-I2C restart'
