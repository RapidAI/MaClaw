[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$sourcePath = Join-Path $projectRoot 'main\services\gateway_transport.c'
$headerPath = Join-Path $projectRoot 'main\services\gateway_transport.h'
$failures = @()
foreach ($path in @($sourcePath, $headerPath)) { if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" } }
if ($failures.Count -eq 0) {
    $source = Get-Content -LiteralPath $sourcePath -Raw
    $header = Get-Content -LiteralPath $headerPath -Raw
    foreach ($requirement in @(
        'gateway_transport_download_frame\s*\(',
        's_asset_download_guard\s*=\s*xSemaphoreCreateMutex',
        'while\s*\(xSemaphoreTake\(s_asset_download_guard,\s*pdMS_TO_TICKS\(100\)\)',
        'const\s+uint32_t\s+requested_epoch\s*=\s*asset_cancel_epoch_snapshot',
        'if\s*\(\s*!asset_cancel_epoch_current\(requested_epoch\)\)\s*return\s+ESP_ERR_INVALID_STATE',
        's_asset_download_task\s*=\s*xTaskGetCurrentTaskHandle\(\)',
        's_asset_download_epoch\s*=\s*requested_epoch')) {
        if ($source -notmatch $requirement) { $failures += "asset guard requirement missing: $requirement" }
    }
    if ($source -notmatch '(?s)if\s*\(asset_request\s*&&\s*!asset_cancel_epoch_current\(asset_epoch\)\)\s*\{.*?err\s*=\s*ESP_ERR_INVALID_STATE;') { $failures += 'late asset response is not converted to cancellation error' }
    if ($source -notmatch '(?s)if\s*\(asset_cancelled\)\s*\{.*?out->len\s*=\s*0;') { $failures += 'cellular late-200 path does not clear response length' }
    if ($source -notmatch '(?s)if\s*\(!asset_cancelled\)\s*out->len\s*=\s*cellular_response_len') { $failures += 'cellular response publication is not epoch gated' }
    if ($source -notmatch '(?s)if\s*\(!cancelled\s*&&\s*cellular_asset_owner_still_active\(cellular_asset_owner\).*?status\s*=\s*DEVICE_STATUS_BUSY') { $failures += 'cellular owner distinction missing' }
    if ($header -notmatch 'gateway_transport_cancel_active_requests\s*\(' -or $header -notmatch 'GATEWAY_TRANSPORT_CANCEL_ASSET') { $failures += 'asset cancellation mask contract missing' }
}
if ($failures.Count -gt 0) { Write-Error ("Gateway Transport asset cancellation check failed:`n" + ($failures -join "`n")); exit 1 }
Write-Output 'Gateway Transport asset cancellation check passed: single-owner guard, epoch race, and cellular owner outcomes are fail-closed'
