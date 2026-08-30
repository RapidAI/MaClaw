[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$header = Join-Path $projectRoot 'main\services\entropy_service.h'
$source = Join-Path $projectRoot 'main\services\entropy_service.c'
$main = Join-Path $projectRoot 'main\main.c'
$provisioning = Join-Path $projectRoot 'main\services\provisioning_service.c'
$qr = Join-Path $projectRoot 'main\services\provisioning_qr_service.c'
$transport = Join-Path $projectRoot 'main\services\gateway_transport.c'
$transportHeader = Join-Path $projectRoot 'main\services\gateway_transport.h'
$cmake = Join-Path $projectRoot 'main\CMakeLists.txt'
$trustedTime = Join-Path $PSScriptRoot 'check-trusted-time-policy.ps1'
$failures = @()

foreach ($path in @($header, $source, $main, $provisioning, $qr, $transport, $transportHeader, $cmake)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

function Text([string]$path) {
    if (Test-Path -LiteralPath $path) { return Get-Content -LiteralPath $path -Raw }
    return ''
}

$headerText = Text $header
$sourceText = Text $source
$mainText = Text $main
$provisioningText = Text $provisioning
$qrText = Text $qr
$transportText = Text $transport
$transportHeaderText = Text $transportHeader
$cmakeText = Text $cmake

foreach ($api in @('entropy_service_init\s*\(',
                   'entropy_service_fill\s*\(',
                   'entropy_service_ready\s*\(')) {
    if ($headerText -notmatch $api) { $failures += "Entropy public contract missing $api" }
}
if ($headerText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|QueueHandle_t|wifi_|netif|http|cJSON|CONFIG_MACLAW_BOARD_)\b') {
    $failures += 'Entropy public header leaked SDK/RTOS/network/HTTP/JSON/board detail'
}
foreach ($required in @('ENTROPY_STATE_COLD', 'ENTROPY_STATE_PROBING',
                        'ENTROPY_STATE_READY', 'atomic_compare_exchange',
                        'esp_fill_random\s*\(', 'mbedtls_platform_zeroize\(probe')) {
    if ($sourceText -notmatch $required) { $failures += "Entropy source missing $required" }
}
if ($sourceText -notmatch 'any\s*=\s*any\s*\|\|') {
    $failures += 'Entropy readiness probe does not reject an all-zero sample'
}
if ($sourceText -notmatch 'equal_halves') {
    $failures += 'Entropy readiness probe lacks repeated-sample check'
}
if ($mainText -match '\besp_fill_random\s*\(') {
    $failures += 'main.c must not call esp_fill_random directly'
}
if ($provisioningText -match '\besp_fill_random\s*\(') {
    $failures += 'Provisioning Service must not call esp_fill_random directly'
}
if ($provisioningText -notmatch 'entropy_service_fill\s*\(') {
    $failures += 'Provisioning Service is not wired to Entropy Service'
}
foreach ($required in @('mbedtls_platform_zeroize\(entropy, sizeof\(entropy\)\)',
                        'mbedtls_platform_zeroize\(provided, sizeof\(provided\)\)')) {
    if ($provisioningText -notmatch $required) {
        $failures += "Provisioning random input lifetime lacks cleanup ($required)"
    }
}
if ($mainText -notmatch 'entropy_service_init\s*\(' -or
    $mainText -notmatch 'entropy_service_fill\s*\(') {
    $failures += 'composition root lacks entropy initialization/readiness barrier'
}
if ($qrText -notmatch 'mbedtls_platform_zeroize\(payload, sizeof\(payload\)\)') {
    $failures += 'QR credential payload is not wiped on all encoding outcomes'
}
if ($qrText -notmatch 'mbedtls_platform_zeroize\(modules, module_count\)') {
    $failures += 'QR module matrix is not wiped before release'
}
if ($cmakeText -notmatch 'services/entropy_service\.c') {
    $failures += 'CMakeLists.txt does not compile Entropy Service'
}
if ($transportText -notmatch 'gateway_https_origin_valid\s*\(' -or
    $transportText -notmatch 'strncmp\(url, "https://", 8u\)') {
    $failures += 'Gateway Transport lacks final HTTPS-origin fail-closed validation'
}
if ($transportText -notmatch 'gateway_https_absolute_url_valid\s*\(' -or
    $transportText -notmatch 'rejecting non-HTTPS or malformed absolute media URL') {
    $failures += 'Gateway Transport lacks HTTPS validation for Hub-provided absolute media URLs'
}
if ($transportText -notmatch 'gateway_https_origin_valid\(s_gateway_url') {
    $failures += 'Gateway Transport does not fail closed when relative requests lack a valid origin'
}
if ($transportHeaderText -match 'gateway_transport_set_gateway_credentials\s*\(' -and
    $transportText -notmatch 'gateway_https_origin_valid\(gateway_url') {
    $failures += 'Gateway credential setter does not enforce HTTPS origin policy'
}

if ($failures.Count -gt 0) {
    Write-Error ("entropy service check failed:`n" + ($failures -join "`n"))
    exit 1
}
if (Test-Path -LiteralPath $trustedTime) {
    & powershell -ExecutionPolicy Bypass -File $trustedTime
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
Write-Output 'entropy service check passed: readiness barrier, value-only boundary, and credential payload cleanup are enforced'
