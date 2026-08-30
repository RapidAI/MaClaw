[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\services\wifi_startup_service.c'
$header = Join-Path $projectRoot 'main\services\wifi_startup_service.h'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_wifi_startup_service.c'
$failures = @()

foreach ($path in @($source, $header, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if ((Test-Path -LiteralPath $header) -and (Test-Path -LiteralPath $source)) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    foreach ($seam in @('wifi_startup_service_host_t', 'wifi_startup_service_request_t',
                         'scan_visible', 'select_saved_network', 'begin_attempt',
                         'wait_attempt', 'configure_enterprise',
                         'wifi_startup_service_connect')) {
        if ($headerText -notmatch ("\b$seam\b")) {
            $failures += "Wi-Fi startup public contract missing $seam"
        }
    }
    if ($headerText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|gateway_)\b') {
        $failures += 'Wi-Fi startup public contract leaked SDK/RTOS/allocator/crypto/transport detail'
    }
    if ($sourceText -match '\b(?:esp_wifi|esp_netif|esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|gateway_transport|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $failures += 'Wi-Fi startup policy absorbed physical SDK/HTTP/crypto/allocator/RTOS/transport/board ownership'
    }
    if ($sourceText -notmatch 'try_saved_networks' -or
        $sourceText -notmatch 'collect_saved_candidate' -or
        $sourceText -notmatch 'DEVICE_STATUS_TIMEOUT') {
        $failures += 'Wi-Fi startup policy must preserve RSSI candidate ordering, fallback, and timeout result'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Wi-Fi startup policy test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_wifi_startup_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Wi-Fi startup policy compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Wi-Fi startup policy test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Wi-Fi startup policy check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Wi-Fi startup policy check passed: saved-network selection and enterprise/fallback ordering remain value-only and host-tested'
