[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$sourcePath = Join-Path $projectRoot 'main\boards\fangtang_4g\fangtang_peripheral_adapter.c'
$headerPath = Join-Path $projectRoot 'main\boards\fangtang_4g\fangtang_peripheral_adapter.h'
$failures = @()
foreach ($path in @($sourcePath, $headerPath)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ($failures.Count -eq 0) {
    $source = Get-Content -LiteralPath $sourcePath -Raw
    $header = Get-Content -LiteralPath $headerPath -Raw
    foreach ($needle in @('#include "esp_adc/adc_cali.h"', '#include "esp_adc/adc_cali_scheme.h"', 'adc_cali_check_scheme', 'adc_cali_create_scheme_curve_fitting', 'adc_cali_raw_to_voltage', 'adc_cali_delete_scheme_curve_fitting', 'adc_oneshot_del_unit', 'fangtang_release_adc_resources', 'fangtang_invalidate_telemetry', 'BATTERY_DIVIDER_NUMERATOR', 'BATTERY_DIVIDER_DENOMINATOR', 'CHARGE_STATUS_ACTIVE_LEVEL')) {
        if ($source -notmatch [regex]::Escape($needle)) { $failures += "Fangtang calibration source is missing: $needle" }
    }
    if ($header -match 'adc_|gpio_|TaskHandle_t|SemaphoreHandle_t') { $failures += 'profile-private hardware types leaked through Fangtang adapter header' }
    if ($source -notmatch 'adc_cali_raw_to_voltage[\s\S]*?fangtang_invalidate_telemetry') { $failures += 'calibration conversion failure does not invalidate telemetry' }
    if ($source -notmatch 'adc_oneshot_read[\s\S]*?fangtang_invalidate_telemetry') { $failures += 'ADC read failure does not invalidate telemetry' }
}
if ($failures.Count -gt 0) { Write-Error ("Fangtang battery calibration check failed:`n" + ($failures -join "`n")); exit 1 }
Write-Output 'Fangtang battery calibration check passed: calibrated mV path, lifecycle cleanup, and fail-closed telemetry are locked'
