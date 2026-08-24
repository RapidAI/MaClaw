[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\configuration_policy.c'
$header = Join-Path $projectRoot 'main\configuration_policy.h'
$test = Join-Path $PSScriptRoot 'host_tests\test_configuration_policy.c'
$configuration = Join-Path $projectRoot 'main\configuration_service.c'
$configurationHeader = Join-Path $projectRoot 'main\configuration_service.h'
$mainSource = Join-Path $projectRoot 'main\main.c'
$failures = @()

foreach ($path in @($source, $header, $test, $configuration, $configurationHeader, $mainSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

foreach ($path in @($source, $header)) {
    if (Test-Path -LiteralPath $path) {
        $text = Get-Content -LiteralPath $path -Raw
        if ($text -cmatch '\b(?:esp_|freertos/|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|heap_caps)\b') {
            $failures += "configuration policy leaked platform/network/RTOS detail ($path)"
        }
    }
}

if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    foreach ($required in @(
            'CONFIGURATION_SOURCE_HUB_AUTHENTICATED',
            'CONFIGURATION_POLICY_MAX_RUNTIME_OVERRIDE_TTL_MS',
            'CONFIGURATION_KEY_PROVISIONING_CREDENTIALS',
            'CONFIGURATION_KEY_DISPLAY_BRIGHTNESS',
            'CONFIGURATION_KEY_SCREEN_SLEEP_SECONDS',
            'CONFIGURATION_KEY_GATEWAY_PAIRING_TOKEN',
            'CONFIGURATION_POLICY_DENIED')) {
        if ($text -notmatch $required) { $failures += "configuration policy missing $required" }
    }
}

if (Test-Path -LiteralPath $configuration) {
    $text = Get-Content -LiteralPath $configuration -Raw
    foreach ($required in @(
            'configuration_service_set_output_volume_with_policy_legacy\s*\(',
            'configuration_service_set_output_volume_with_policy_revision_legacy\s*\(',
            'configuration_policy_authorize\s*\(',
            'CONFIGURATION_KEY_OUTPUT_VOLUME',
            'configuration_service_set_display_brightness_with_policy_legacy\s*\(',
            'configuration_service_set_screen_sleep_seconds_with_policy_legacy\s*\(')) {
        if ($text -notmatch $required) { $failures += "configuration service policy boundary missing $required" }
    }
}

if (Test-Path -LiteralPath $configurationHeader) {
    $text = Get-Content -LiteralPath $configurationHeader -Raw
    if ($text -notmatch 'configuration_service_set_output_volume_with_policy\s*\(' -or
        $text -notmatch 'configuration_service_set_output_volume_with_policy_revision\s*\(') {
        $failures += 'configuration service public policy mutation missing'
    }
}

if (Test-Path -LiteralPath $mainSource) {
    $text = Get-Content -LiteralPath $mainSource -Raw
    foreach ($required in @(
            'persist_hub_output_volume\s*\(',
            'persist_hub_display_policy\s*\(',
            'hub_authenticated',
            'configuration_service_set_output_volume_with_policy\s*\(',
            'configuration_service_set_output_volume_with_policy_revision\s*\(',
            'configuration_service_set_display_brightness_with_policy\s*\(',
            'configuration_service_set_screen_sleep_seconds_with_policy\s*\(',
            'configuration_service_apply_display_policy_with_policy\s*\(',
            'CONFIGURATION_SOURCE_HUB_AUTHENTICATED')) {
        if ($text -notmatch $required) { $failures += "Hub volume provenance wiring missing $required" }
    }
    $handler = [regex]::Match($text,
        'static void gateway_host_handle_hardware_config\([\s\S]*?\n\}',
        [System.Text.RegularExpressions.RegexOptions]::Singleline)
    if (-not $handler.Success) {
        $failures += 'cannot inspect gateway hardware-config policy ordering'
    } else {
        $body = $handler.Value
        $persist = $body.IndexOf('persist_hub_output_volume')
        $apply = $body.IndexOf('audio_arbitration_set_output_volume')
        if ($persist -lt 0 -or ($apply -ge 0 -and $apply -lt $persist)) {
            $failures += 'gateway applies Audio output volume before durable Configuration publication'
        }
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for configuration policy test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_configuration_policy.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $test $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host configuration policy compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host configuration policy test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("configuration policy check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'configuration policy check passed: source, authentication, key authority and TTL are fail-closed'
