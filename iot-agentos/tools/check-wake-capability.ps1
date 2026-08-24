[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\platform_wake.c'
$header = Join-Path $projectRoot 'main\platform_wake.h'
$profileHeader = Join-Path $projectRoot 'main\platform_wake_profile.h'
$deviceHeader = Join-Path $projectRoot 'main\device_api.h'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_wake_capability.c'
$failures = @()

foreach ($path in @($source, $header, $profileHeader, $deviceHeader, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if (Test-Path -LiteralPath $header) {
    $text = Get-Content -LiteralPath $header -Raw
    if ($text -match '\b(?:esp_|gpio_|RTC_|freertos/|TaskHandle_t|SemaphoreHandle_t)\b') {
        $failures += 'platform wake public contract leaked electrical or RTOS detail'
    }
}
if (Test-Path -LiteralPath $profileHeader) {
    $text = Get-Content -LiteralPath $profileHeader -Raw
    foreach ($field in @('verified_display_off_sources', 'light_sleep_candidate_sources', 'deep_sleep_candidate_sources')) {
        if ($text -notmatch $field) { $failures += "wake profile matrix missing $field" }
    }
}
if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    foreach ($required in @(
            'verified_sources\s*=\s*matrix\.verified_display_off_sources',
            'candidate_sources\s*=\s*matrix\.light_sleep_candidate_sources',
            'candidate_sources\s*=\s*matrix\.deep_sleep_candidate_sources')) {
        if ($text -notmatch $required) { $failures += "platform wake mapping missing $required" }
    }
    if ($text -match 'verified_sources\s*=\s*matrix\.(?:light_sleep|deep_sleep)_candidate_sources') {
        $failures += 'platform wake promoted an unverified Light/Deep candidate'
    }
    if ($text -match '\b(?:esp_sleep|gpio_|CONFIG_MACLAW_BOARD_)') {
        $failures += 'platform wake selected board electrical implementation'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Wake capability test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_wake_capability.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Wake capability compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Wake capability test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("wake capability check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'wake capability check passed: DISPLAY_OFF is verified while Light/Deep stay candidate-only'
