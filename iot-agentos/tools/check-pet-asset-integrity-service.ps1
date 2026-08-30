[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$h = Join-Path $root 'main/services/pet_asset_integrity_service.h'
$c = Join-Path $root 'main/services/pet_asset_integrity_service.c'
$t = Join-Path $PSScriptRoot 'host_tests/test_pet_asset_integrity_service.c'
foreach ($p in @($h,$c,$t)) { if (-not (Test-Path -LiteralPath $p)) { throw "missing $p" } }
$header = Get-Content -LiteralPath $h -Raw
$source = Get-Content -LiteralPath $c -Raw
foreach ($name in @('pet_asset_integrity_service_verify_frame','compute_sha256')) {
    if ($header -notmatch "\b$name\b") { throw "integrity contract missing $name" }
}
if ($header -match '\b(?:psa_|esp_|freertos/|heap_caps|TaskHandle_t|SemaphoreHandle_t|cJSON|gateway_)\b') {
    throw 'integrity public contract leaked platform/crypto/allocator/RTOS detail'
}
if ($source -match '\b(?:psa_hash|esp_http_client|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport)\b') {
    throw 'integrity service absorbed physical crypto/HTTP/allocator/RTOS ownership'
}
if ($source -notmatch 'pet_asset_service_sha256_matches_hex') { throw 'integrity service must use canonical digest comparison' }
$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) { throw 'host C compiler (gcc or clang) is required for integrity regression' }
$cjsonDir = Join-Path $root 'managed_components/espressif__cjson/cJSON'
$outDir = Join-Path $root 'build-host-tests'
New-Item -ItemType Directory -Force -Path $outDir | Out-Null
$exe = Join-Path $outDir 'test_pet_asset_integrity_service.exe'
& $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $root 'main')" "-I$cjsonDir" `
    $c $t (Join-Path $root 'main/services/pet_asset_service.c') `
    (Join-Path $cjsonDir 'cJSON.c') -o $exe
if ($LASTEXITCODE -ne 0) { throw "integrity host regression compile failed (exit $LASTEXITCODE)" }
& $exe
if ($LASTEXITCODE -ne 0) { throw "integrity host regression failed (exit $LASTEXITCODE)" }
Write-Host 'pet asset integrity service contract: PASS'
