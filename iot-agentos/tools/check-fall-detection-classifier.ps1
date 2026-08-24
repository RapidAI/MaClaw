[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\fall_detection_classifier.c'
$header = Join-Path $projectRoot 'main\fall_detection_classifier.h'
$service = Join-Path $projectRoot 'main\fall_detection_service.c'
$test = Join-Path $PSScriptRoot 'host_tests\test_fall_detection_classifier.c'
$cmake = Join-Path $projectRoot 'main\CMakeLists.txt'
$failures = @()

foreach ($path in @($source, $header, $service, $test, $cmake)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

foreach ($path in @($source, $header)) {
    if (Test-Path -LiteralPath $path) {
        $text = Get-Content -LiteralPath $path -Raw
        if ($text -cmatch '\b(?:esp_|freertos/|driver/|i2c|gpio|nvs_|cJSON|TaskHandle_t|SemaphoreHandle_t|board_|platform_)\b') {
            $failures += "fall classifier value core leaked platform/RTOS detail ($path)"
        }
    }
}

if (Test-Path -LiteralPath $header) {
    $text = Get-Content -LiteralPath $header -Raw
    foreach ($required in @(
            'FALL_DETECTION_CLASSIFIER_MAX_INTER_SAMPLE_GAP_US',
            'fall_detection_classifier_t',
            'fall_detection_classifier_reset\s*\(',
            'fall_detection_classifier_observe\s*\(')) {
        if ($text -notmatch $required) { $failures += "fall classifier public contract missing $required" }
    }
}

if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    foreach ($required in @(
            'sample->timestamp_us\s*<=\s*classifier->last_timestamp_us',
            'FALL_DETECTION_CLASSIFIER_MAX_INTER_SAMPLE_GAP_US',
            'classifier->have_baseline\s*=\s*false',
            'FALL_DETECTION_CLASSIFIER_FREEFALL',
            'FALL_DETECTION_CLASSIFIER_POST_IMPACT',
            'FALL_STILL_MIN_US')) {
        if ($text -notmatch $required) { $failures += "fall classifier continuity/evidence rule missing $required" }
    }
}

if ((Test-Path -LiteralPath $service) -and
    ((Get-Content -LiteralPath $service -Raw) -notmatch 'fall_detection_classifier_observe\s*\(')) {
    $failures += 'fall detection service does not consume the shared classifier value core'
}

if ((Test-Path -LiteralPath $cmake) -and
    ((Get-Content -LiteralPath $cmake -Raw) -notmatch '"fall_detection_classifier\.c"')) {
    $failures += 'fall detection classifier source is not compiled by the main component'
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for fall classifier regression'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_fall_detection_classifier.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $test $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host fall classifier regression compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host fall classifier regression failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Fall detection classifier check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Fall detection classifier check passed: evidence is value-only and cannot span invalid, stale, reversed or stalled Motion samples'
