[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\input_service.c'
$gate = Join-Path $projectRoot 'main\input_scanner_lifecycle_gate.h'
$compactRenderer = Join-Path $projectRoot 'main\compact_renderer.c'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_input_scanner_lifecycle_gate.c'
$failures = @()

foreach ($path in @($source, $gate, $compactRenderer, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if ($failures.Count -eq 0) {
    $sourceText = Get-Content -LiteralPath $source -Raw
    $gateText = Get-Content -LiteralPath $gate -Raw
    $compactRendererText = Get-Content -LiteralPath $compactRenderer -Raw
    foreach ($required in @(
            '#include "input_scanner_lifecycle_gate\.h"',
            'input_scanner_lifecycle_gate_allows_start\s*\(',
            'input_scanner_lifecycle_gate_note_stop_failed\s*\(',
            'input_scanner_lifecycle_gate_note_stop_succeeded\s*\(')) {
        if ($sourceText -notmatch $required) {
            $failures += "main/input_service.c missing scanner restart lifecycle gate: $required"
        }
    }
    if ($sourceText -match 's_board_scanner_initialized') {
        $failures += 'main/input_service.c must not permanently reject a scanner that has completed stop/join'
    }
    if ($gateText -match '\b(?:esp_|freertos/|driver/|TaskHandle_t|SemaphoreHandle_t|gpio_|i2c_)') {
        $failures += 'main/input_scanner_lifecycle_gate.h must remain a value-only lifecycle gate'
    }
    $stopMatch = [regex]::Match(
        $compactRendererText,
        'esp_err_t\s+legacy_bootstrap_input_stop_scanner\s*\([^)]*\)\s*\{([\s\S]*?)\n\}')
    if (-not $stopMatch.Success -or
        $stopMatch.Groups[1].Value -notmatch 'compact_input_service_stop_scanner\s*\(' -or
        $stopMatch.Groups[1].Value -notmatch 's_button_cb\s*=\s*NULL' -or
        $stopMatch.Groups[1].Value -notmatch 's_button_arg\s*=\s*NULL') {
        $failures += 'main/compact_renderer.c must clear its old publisher only after successful scanner stop'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Input scanner lifecycle test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_input_scanner_lifecycle_gate.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Input scanner lifecycle compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Input scanner lifecycle test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Input scanner lifecycle check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Input scanner lifecycle check passed: successful stop/join permits only a fresh publisher generation'
