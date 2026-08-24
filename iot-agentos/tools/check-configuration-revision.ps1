[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\configuration_revision.c'
$header = Join-Path $projectRoot 'main\configuration_revision.h'
$test = Join-Path $PSScriptRoot 'host_tests\test_configuration_revision.c'
$configuration = Join-Path $projectRoot 'main\configuration_service.c'
$configurationHeader = Join-Path $projectRoot 'main\configuration_service.h'
$failures = @()

foreach ($path in @($source, $header, $test, $configuration, $configurationHeader)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if (Test-Path -LiteralPath $header) {
    $text = Get-Content -LiteralPath $header -Raw
    if ($text -notmatch '\bconfiguration_revision_next\s*\(') {
        $failures += 'revision value contract missing next transition'
    }
    if ($text -cmatch '\b(?:esp_|freertos/|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|heap_caps)\b') {
        $failures += 'revision value contract leaked platform/network/RTOS detail'
    }
}

if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    if ($text -notmatch 'UINT64_MAX') { $failures += 'revision overflow does not fail closed' }
    if ($text -cmatch '\b(?:esp_|freertos/|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|heap_caps)\b') {
        $failures += 'revision value implementation leaked platform/network/RTOS detail'
    }
}

if (Test-Path -LiteralPath $configuration) {
    $text = Get-Content -LiteralPath $configuration -Raw
    foreach ($required in @(
            'CONFIGURATION_STORE_VERSION 7u',
            'configuration_store_v6_t',
            'migrate_v6_locked\s*\(',
            'configuration_store_v5_t',
            'migrate_v5_locked\s*\(',
            'commit_store_locked\s*\(',
            'configuration_revision_next\s*\(',
            'configuration_service_load_revisioned_snapshot_legacy\s*\(')) {
        if ($text -notmatch $required) {
            $failures += "configuration revision integration missing $required"
        }
    }
}

if (Test-Path -LiteralPath $configurationHeader) {
    $text = Get-Content -LiteralPath $configurationHeader -Raw
    foreach ($required in @(
            'configuration_revisioned_snapshot_t',
            'CONFIGURATION_REVISIONED_SNAPSHOT_ABI_VERSION',
            'configuration_service_load_revisioned_snapshot\s*\(')) {
        if ($text -notmatch $required) {
            $failures += "configuration revision public contract missing $required"
        }
    }
    if ($text -cmatch '\b(?:esp_|freertos/|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|heap_caps)\b') {
        $failures += 'revisioned configuration public contract leaked platform/network/RTOS detail'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for configuration revision test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_configuration_revision.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $test $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host configuration revision compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host configuration revision test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("configuration revision check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'configuration revision check passed: immutable copied revision contract is platform-neutral and monotonic'
