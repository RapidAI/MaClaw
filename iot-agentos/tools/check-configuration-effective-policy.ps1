[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\configuration_effective_policy.c'
$header = Join-Path $projectRoot 'main\configuration_effective_policy.h'
$storeSource = Join-Path $projectRoot 'main\configuration_runtime_override_store.c'
$storeHeader = Join-Path $projectRoot 'main\configuration_runtime_override_store.h'
$sourcePriority = Join-Path $projectRoot 'main\configuration_source_priority.c'
$sourcePriorityHeader = Join-Path $projectRoot 'main\configuration_source_priority.h'
$serviceHeader = Join-Path $projectRoot 'main\configuration_runtime_override_service.h'
$serviceSource = Join-Path $projectRoot 'main\configuration_service.c'
$test = Join-Path $PSScriptRoot 'host_tests\test_configuration_effective_policy.c'
$cmake = Join-Path $projectRoot 'main\CMakeLists.txt'
$failures = @()

foreach ($path in @($source, $header, $storeSource, $storeHeader, $sourcePriority, $sourcePriorityHeader, $serviceHeader, $serviceSource, $test, $cmake)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
foreach ($path in @($source, $header, $storeSource, $storeHeader)) {
    if (Test-Path -LiteralPath $path) {
        $text = Get-Content -LiteralPath $path -Raw
        if ($text -cmatch '\b(?:esp_|freertos/|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|heap_caps|board_)\b') {
            $failures += "effective policy value model leaked platform/network/RTOS detail ($path)"
        }
    }
}
if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    foreach ($required in @(
            'CONFIGURATION_SOURCE_RUNTIME_OVERRIDE',
            'expires_at_monotonic_ms',
            'configuration_source_priority_resolve\s*\(',
            'CONFIGURATION_RUNTIME_OVERRIDE_VALUE_TRANSPORT_SELECTION')) {
        if ($text -notmatch $required) { $failures += "effective policy implementation missing $required" }
    }
}
if (Test-Path -LiteralPath $storeSource) {
    $text = Get-Content -LiteralPath $storeSource -Raw
    foreach ($required in @(
            'configuration_runtime_override_store_put\s*\(',
            'configuration_runtime_override_store_discard_expired\s*\(',
            'configuration_runtime_override_store_clear_all\s*\(',
            'configuration_runtime_override_store_next_expiry\s*\(',
            'configuration_effective_policy_resolve\s*\(',
            'effective_revision')) {
        if ($text -notmatch $required) { $failures += "runtime override store missing $required" }
    }
}
if ((Test-Path -LiteralPath $serviceHeader) -and (Test-Path -LiteralPath $serviceSource)) {
    $headerText = Get-Content -LiteralPath $serviceHeader -Raw
    $serviceText = Get-Content -LiteralPath $serviceSource -Raw
    foreach ($required in @(
            'configuration_service_apply_runtime_override\s*\(',
            'configuration_service_clear_runtime_overrides\s*\(',
            'configuration_service_next_runtime_override_expiry_ms\s*\(',
            'configuration_service_load_effective_revisioned_snapshot\s*\(')) {
        if ($headerText -notmatch $required -or $serviceText -notmatch $required) {
            $failures += "runtime override facade missing $required"
        }
    }
    foreach ($required in @(
            's_runtime_override_store',
            'configuration_runtime_override_store_init\s*\(',
            'configuration_runtime_override_store_resolve\s*\(',
            'heap_caps_free\(s_runtime_override_store\)',
            'configuration_runtime_override_store_clear_all\(s_runtime_override_store\)')) {
        if ($serviceText -notmatch $required) {
            $failures += "Configuration runtime override lifecycle missing $required"
        }
    }
}
if (Test-Path -LiteralPath $cmake) {
    if ((Get-Content -LiteralPath $cmake -Raw) -notmatch '"configuration_effective_policy\.c"') {
        $failures += 'effective policy source is not compiled by main component'
    }
    if ((Get-Content -LiteralPath $cmake -Raw) -notmatch '"configuration_runtime_override_store\.c"') {
        $failures += 'runtime override store source is not compiled by main component'
    }
    if ((Get-Content -LiteralPath $cmake -Raw) -notmatch '"configuration_source_priority\.c"') {
        $failures += 'source priority source is not compiled by main component'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for effective policy test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_configuration_effective_policy.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $test $source $storeSource $sourcePriority `
        (Join-Path $projectRoot 'main\configuration_policy.c') -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host effective policy compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host effective policy test failed (exit $LASTEXITCODE)"
        }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("configuration effective policy check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'configuration effective policy check passed: runtime overrides are owned, bounded, authenticated and value-only'
