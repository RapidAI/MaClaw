[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\configuration_source_priority.c'
$header = Join-Path $projectRoot 'main\configuration_source_priority.h'
$effective = Join-Path $projectRoot 'main\configuration_effective_policy.c'
$test = Join-Path $PSScriptRoot 'host_tests\test_configuration_source_priority.c'
$cmake = Join-Path $projectRoot 'main\CMakeLists.txt'
$failures = @()

foreach ($path in @($source, $header, $effective, $test, $cmake)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
foreach ($path in @($source, $header)) {
    if (Test-Path -LiteralPath $path) {
        $text = Get-Content -LiteralPath $path -Raw
        if ($text -cmatch '\b(?:esp_|freertos/|driver/|nvs_|http|wifi|TaskHandle_t|SemaphoreHandle_t|heap_caps|board_)\b') {
            $failures += "source-priority value model leaked platform/network/RTOS detail ($path)"
        }
    }
}
if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    foreach ($required in @(
            'CONFIGURATION_SOURCE_COMPILED_DEFAULT',
            'CONFIGURATION_SOURCE_MANUFACTURING_MANIFEST',
            'CONFIGURATION_SOURCE_USER_LOCAL',
            'CONFIGURATION_SOURCE_HUB_AUTHENTICATED',
            'CONFIGURATION_SOURCE_RUNTIME_OVERRIDE',
            'configuration_policy_authorize\s*\(',
            'seen_present_source',
            'CONFIGURATION_SOURCE_RESOLVE_NO_CANDIDATE')) {
        if ($text -notmatch $required) { $failures += "source priority implementation missing $required" }
    }
}
if ((Test-Path -LiteralPath $effective) -and
    ((Get-Content -LiteralPath $effective -Raw) -notmatch 'configuration_source_priority_resolve\s*\(')) {
    $failures += 'runtime override validation bypasses the common source priority resolver'
}
if ((Test-Path -LiteralPath $cmake) -and
    ((Get-Content -LiteralPath $cmake -Raw) -notmatch '"configuration_source_priority\.c"')) {
    $failures += 'source priority source is not compiled by main component'
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for source priority test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_configuration_source_priority.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $test $source `
        (Join-Path $projectRoot 'main\configuration_policy.c') -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host source priority compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host source priority test failed (exit $LASTEXITCODE)"
        }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("configuration source priority check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'configuration source priority check passed: all configuration sources use one authorized per-key precedence resolver'
