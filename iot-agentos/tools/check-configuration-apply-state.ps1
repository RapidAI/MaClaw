[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\configuration_apply_state.c'
$header = Join-Path $projectRoot 'main\configuration_apply_state.h'
$test = Join-Path $PSScriptRoot 'host_tests\test_configuration_apply_state.c'
$cmake = Join-Path $projectRoot 'main\CMakeLists.txt'
$failures = @()

foreach ($path in @($source, $header, $test, $cmake)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
foreach ($path in @($source, $header)) {
    if (Test-Path -LiteralPath $path) {
        $text = Get-Content -LiteralPath $path -Raw
        if ($text -cmatch '\b(?:esp_|freertos/|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|heap_caps|board_)\b') {
            $failures += "configuration apply state leaked platform/network/RTOS detail ($path)"
        }
    }
}
if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    foreach ($required in @(
            'configuration_apply_state_begin\s*\(',
            'configuration_apply_state_begin_with_requirements\s*\(',
            'configuration_apply_state_record_output_volume\s*\(',
            'configuration_apply_state_record_display_brightness\s*\(',
            'configuration_apply_state_output_volume_needs_apply\s*\(',
            'configuration_apply_state_display_brightness_needs_apply\s*\(',
            'configuration_apply_state_screen_sleep_seconds_needs_apply\s*\(',
            'value_needs_apply\s*\(',
            'output_volume_policy_required',
            'display_brightness_policy_required',
            'one revision pair',
            'ignore later caller metadata',
            'CONFIGURATION_APPLY_OBSERVATION_UNKNOWN',
            'configuration_apply_state_is_converged\s*\(')) {
        if ($text -notmatch $required) { $failures += "configuration apply state missing $required" }
    }
}
if ((Test-Path -LiteralPath $cmake) -and
    ((Get-Content -LiteralPath $cmake -Raw) -notmatch '"configuration_apply_state\.c"')) {
    $failures += 'configuration apply state source is not compiled by the main component'
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for configuration apply-state test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_configuration_apply_state.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $test $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host configuration apply-state compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host configuration apply-state test failed (exit $LASTEXITCODE)"
        }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("configuration apply-state check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'configuration apply-state check passed: desired and observed policy state remains revision-bound and fails closed'
