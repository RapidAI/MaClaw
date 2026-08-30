[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\services\connectivity_network_lifecycle_service.c'
$header = Join-Path $projectRoot 'main\services\connectivity_network_lifecycle_service.h'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_connectivity_network_lifecycle_service.c'
$failures = @()
foreach ($path in @($source, $header, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ($failures.Count -eq 0) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    foreach ($seam in @('connectivity_network_lifecycle_service_init',
                         'connectivity_network_lifecycle_service_ensure_core',
                         'connectivity_network_lifecycle_service_ensure_wifi',
                         'connectivity_network_lifecycle_service_stop',
                         'configure_physical_lifecycle', 'stop_physical',
                         'deinitialize_logical')) {
        if ($headerText -notmatch ("\b$seam\b")) { $failures += "network lifecycle contract missing $seam" }
    }
    if ($headerText -match '\b(?:esp_|freertos/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|gateway_)\b') {
        $failures += 'network lifecycle public contract leaked SDK/RTOS/allocator/crypto/transport detail'
    }
    if ($sourceText -match '\b(?:esp_wifi|esp_netif|esp_http_client|xTask|SemaphoreHandle_t|gateway_transport|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $failures += 'network lifecycle service absorbed SDK/HTTP/RTOS/transport/board ownership'
    }
    if ($sourceText -notmatch 'stop_physical' -or $sourceText -notmatch 'deinitialize_logical' -or
        $sourceText -notmatch 'rollback_failed_start') {
        $failures += 'network lifecycle service must preserve physical-before-logical stop and failed-init rollback'
    }
    if ($sourceText -notmatch 'physical_has_resources\(s_host\.context\)' -or
        $sourceText -notmatch 'physical_core_ready\(s_host\.context\)' -or
        $sourceText -notmatch 'wifi_ready\(s_host\.context\)') {
        $failures += 'network lifecycle service must revalidate physical/core/Wi-Fi readiness facts'
    }
    if ($sourceText -notmatch 'return DEVICE_STATUS_BUSY;' -or
        $sourceText -notmatch 'open_wifi_callback_admission') {
        $failures += 'ambiguous readiness/stop outcomes must fail closed before callback admission'
    }
}
$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for network lifecycle test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_connectivity_network_lifecycle_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) { $failures += "host network lifecycle compile failed (exit $LASTEXITCODE)" }
    else { & $exe; if ($LASTEXITCODE -ne 0) { $failures += "host network lifecycle test failed (exit $LASTEXITCODE)" } }
}
if ($failures.Count -gt 0) { Write-Error ("network lifecycle check failed:`n" + ($failures -join "`n")); exit 1 }
Write-Output 'network lifecycle check passed: physical/logical ordering remains value-only and host-tested'
