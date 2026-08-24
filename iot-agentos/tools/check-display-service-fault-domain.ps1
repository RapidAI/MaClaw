[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\display_service.c'
$header = Join-Path $projectRoot 'main\display_service.h'
$failures = @()

foreach ($path in @($source, $header)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ($failures.Count -eq 0) {
    $sourceText = Get-Content -LiteralPath $source -Raw
    $headerText = Get-Content -LiteralPath $header -Raw
    foreach ($required in @(
            'FAULT_DOMAIN_ID_DISPLAY',
            'fault_domain_begin_start',
            'fault_domain_begin_self_test',
            'fault_domain_mark_ready',
            'fault_domain_begin_quiesce',
            'fault_domain_mark_unknown_outcome',
            'display_service_mark_logical_domain_stopped',
            'display_service_fault_domain_accepting')) {
        if ($sourceText -notmatch [regex]::Escape($required)) {
            $failures += "Display Service fault-domain lifecycle missing $required"
        }
    }
    if ($headerText -notmatch 'display_service_get_fault_domain_snapshot' -or
        $headerText -notmatch 'fault_domain\.h') {
        $failures += 'Display Service must expose only a value-only fault-domain diagnostic snapshot'
    }
    $stopIndex = $sourceText.IndexOf('DISPLAY_REQUEST_STOP')
    $stoppedIndex = $sourceText.IndexOf('display_service_mark_logical_domain_stopped', $stopIndex)
    $clearTaskIndex = $sourceText.IndexOf('s_display_service_task = NULL', $stopIndex)
    if ($stopIndex -lt 0 -or $stoppedIndex -lt 0 -or $clearTaskIndex -lt 0 -or
        $stoppedIndex -gt $clearTaskIndex) {
        $failures += 'Display STOP must record authoritative logical-domain stop before clearing the task handle'
    }
    if ($sourceText -match 'fault_domain_mark_ready[\s\S]{0,300}\b(?:platform_display_|board_port_|esp_lcd_|esp_lcd_panel_)') {
        $failures += 'Display logical-domain READY must not claim profile renderer/panel ownership'
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Display Service fault-domain check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Display Service fault-domain check passed: logical admission lifecycle is fenced without claiming panel/DMA restart'
