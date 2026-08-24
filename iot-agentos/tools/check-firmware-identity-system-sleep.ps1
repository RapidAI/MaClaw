[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_firmware_identity_sleep_gate.c'
$gateHeader = Join-Path $projectRoot 'main\firmware_identity_sleep_gate.h'
$identitySource = Join-Path $projectRoot 'main\firmware_identity.c'
$failures = @()
foreach ($path in @($testSource, $gateHeader, $identitySource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if ($failures.Count -eq 0) {
    $identityText = Get-Content -LiteralPath $identitySource -Raw
    foreach ($required in @(
            '#include "firmware_identity_sleep_gate.h"',
            's_system_sleep_observers',
            'firmware_identity_sleep_gate_begin\s*\(',
            'firmware_identity_sleep_gate_end\s*\(',
            's_system_sleep_observers[^\r\n]*!=\s*0',
            'firmware_identity_sleep_gate_begin\s*\(\s*&s_system_sleep_preparing\s*,\s*&s_system_sleep_observers\s*\)',
            'TaskHandle_t\s+task\s*=\s*query_task_handle\s*\(\s*\)\s*;',
            'if\s*\(\s*task\s*&&\s*!s_system_sleep_quiesced\s*\)\s*return\s+DEVICE_STATUS_UNAVAILABLE\s*;')) {
        if ($identityText -notmatch $required) {
            $failures += "main/firmware_identity.c missing System Sleep observer fence: $required"
        }
    }
    if ($identityText -match 'if\s*\(\s*!s_system_sleep_quiesced\s*\)\s*return\s+DEVICE_STATUS_UNAVAILABLE\s*;') {
        $failures += 'main/firmware_identity.c must allow a valid no-worker System Sleep PREPARE generation'
    }
    $prepareMatch = [regex]::Match(
        $identityText,
        'device_status_t\s+firmware_identity_prepare_system_sleep\s*\([\s\S]*?\n\}\s*\n\s*void\s+firmware_identity_abort_system_sleep_prepare')
    if (-not $prepareMatch.Success) {
        $failures += 'main/firmware_identity.c must retain an inspectable System Sleep PREPARE body'
    } elseif ($prepareMatch.Value -match
              '__atomic_store_n\s*\(\s*&s_system_sleep_preparing\s*,\s*false') {
        $failures += 'main/firmware_identity.c must retain System Sleep admission closed on PREPARE timeout until ABORT'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Firmware Identity System Sleep test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_firmware_identity_sleep_gate.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Firmware Identity System Sleep compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Firmware Identity System Sleep test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Firmware Identity System Sleep check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Firmware Identity System Sleep check passed: synchronous observers drain fail-closed'
