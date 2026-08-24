[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$modelC = Join-Path $projectRoot 'main\configuration_transaction.c'
$modelH = Join-Path $projectRoot 'main\configuration_transaction.h'
$testC = Join-Path $PSScriptRoot 'host_tests\test_configuration_transaction.c'
$configurationC = Join-Path $projectRoot 'main\configuration_service.c'
$configurationH = Join-Path $projectRoot 'main\configuration_service.h'
$revisionC = Join-Path $projectRoot 'main\configuration_revision.c'
$failures = @()

foreach ($path in @($modelC, $modelH, $testC, $configurationC, $configurationH, $revisionC)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if (Test-Path -LiteralPath $modelH) {
    $header = Get-Content -LiteralPath $modelH -Raw
    foreach ($api in @(
            'configuration_transaction_valid_pairing_code',
            'configuration_transaction_stage_provisioning_request',
            'configuration_transaction_apply_confirmed_policy',
            'configuration_transaction_boot_snapshot',
            'configuration_transaction_begin_staged_boot',
            'configuration_transaction_commit_gateway_pairing_token',
            'configuration_transaction_rollback')) {
        if ($header -notmatch ("\b$api\s*\(")) {
            $failures += "transaction header missing $api"
        }
    }
    if ($header -cmatch '\b(?:esp_|freertos/|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|heap_caps)\b') {
        $failures += 'configuration transaction public contract leaked platform/network/RTOS detail'
    }
}

if (Test-Path -LiteralPath $modelC) {
    $source = Get-Content -LiteralPath $modelC -Raw
    if ($source -cmatch '\b(?:esp_|freertos/|driver/|nvs_|TaskHandle_t|SemaphoreHandle_t|heap_caps)\b') {
        $failures += 'configuration transaction value model absorbed platform/network/RTOS detail'
    }
    foreach ($retired in @(
            'configuration_transaction_stage\s*\(',
            'configuration_transaction_confirm\s*\(')) {
        if ($source -match $retired) {
            $failures += "configuration transaction retained bypassable mutation API ($retired)"
        }
    }
}

if (Test-Path -LiteralPath $configurationC) {
    $source = Get-Content -LiteralPath $configurationC -Raw
    foreach ($required in @(
            'configuration_provisioning_transaction_t',
            'configuration_transaction_valid_pairing_code\s*\(',
            'configuration_transaction_stage_provisioning_request\s*\(',
            'configuration_transaction_apply_confirmed_policy\s*\(',
            'configuration_transaction_boot_snapshot\s*\(',
            'configuration_transaction_begin_staged_boot\s*\(',
            'configuration_transaction_commit_gateway_pairing_token\s*\(',
            'configuration_transaction_rollback\s*\(')) {
        if ($source -notmatch $required) {
            $failures += "configuration service is not routed through transaction value model ($required)"
        }
    }
}

if (Test-Path -LiteralPath $configurationH) {
    $header = Get-Content -LiteralPath $configurationH -Raw
    foreach ($required in @(
            'configuration_provisioning_request_t',
            'configuration_service_stage_provisioning\s*\(',
            'configuration_service_valid_pairing_code\s*\(',
            'configuration_service_begin_staged_provisioning_boot\s*\(')) {
        if ($header -notmatch $required) {
            $failures += "configuration service public contract lacks $required"
        }
    }
    if ($header -cmatch '\b(?:esp_|freertos/|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|heap_caps)\b') {
        $failures += 'configuration provisioning request contract leaked platform/network/RTOS detail'
    }
}

if (Test-Path -LiteralPath $configurationC) {
    $source = Get-Content -LiteralPath $configurationC -Raw
    foreach ($required in @(
            'configuration_service_stage_provisioning_legacy\s*\(',
            'configuration_service_stage_provisioning\s*\(',
            'configuration_transaction_stage_provisioning_request\s*\(',
            'configuration_transaction_apply_confirmed_policy\s*\(',
            'configuration_transaction_commit_gateway_pairing_token\s*\(')) {
        if ($source -notmatch $required) {
            $failures += "configuration service provisioning request derivation missing $required"
        }
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for configuration transaction test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_configuration_transaction.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $testC $modelC $revisionC -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host configuration transaction compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host configuration transaction test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("configuration transaction check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'configuration transaction check passed: confirmed/staged value model is platform-neutral and host regression passes'
