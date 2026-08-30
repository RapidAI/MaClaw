[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\services\provisioning_qr_service.c'
$header = Join-Path $projectRoot 'main\services\provisioning_qr_service.h'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_provisioning_qr_service.c'
$main = Join-Path $projectRoot 'main\main.c'
$failures = @()
foreach ($path in @($source, $header, $testSource, $main)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ($failures.Count -eq 0) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    $mainText = Get-Content -LiteralPath $main -Raw
    foreach ($seam in @('provisioning_qr_service_init', 'provisioning_qr_service_show',
                         'publish_modules', 'publish_fallback_message')) {
        if ($headerText -notmatch ("\b$seam\b")) { $failures += "provisioning QR contract missing $seam" }
    }
    if ($headerText -match '\b(?:esp_|freertos/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|qrcode)\b') {
        $failures += 'provisioning QR public contract leaked SDK/RTOS/allocator/crypto detail'
    }
    if ($sourceText -notmatch 'esp_qrcode_generate' -or $sourceText -notmatch 'free\(modules\)' -or
        $sourceText -notmatch 'length\s*>=\s*\(int\)sizeof\(payload\)' -or
        $sourceText -notmatch 'esp_log_level_set\s*\(\s*"QRCODE"\s*,\s*ESP_LOG_NONE\s*\)') {
        $failures += 'provisioning QR service must own bounded encoding, module release, oversized-payload closure, and QR payload-log suppression'
    }
    if ($mainText -match '\b(?:esp_qrcode_|esp_qrcode_handle_t|ESP_QRCODE_)\b' -or
        $mainText -match '\bshow_setup_qrcode\b') {
        $failures += 'main.c still owns QR SDK callback or encoder transaction'
    }
    if ($mainText -notmatch 'provisioning_qr_service_init\s*\(' -or
        $mainText -notmatch 'provisioning_qr_service_show\s*\(') {
        $failures += 'main.c provisioning composition does not wire QR service'
    }
}
$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for provisioning QR value-contract test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_provisioning_qr_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource -o $exe
    if ($LASTEXITCODE -ne 0) { $failures += "host provisioning QR compile failed (exit $LASTEXITCODE)" }
    else { & $exe; if ($LASTEXITCODE -ne 0) { $failures += "host provisioning QR test failed (exit $LASTEXITCODE)" } }
}
if ($failures.Count -gt 0) { Write-Error ("provisioning QR check failed:`n" + ($failures -join "`n")); exit 1 }
Write-Output 'provisioning QR check passed: encoder/temporary matrix stay private and root exposes only semantic presentation callbacks'
