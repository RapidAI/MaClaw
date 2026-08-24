[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\wake_deadline_service.c'
$header = Join-Path $projectRoot 'main\wake_deadline_sleep_gate.h'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_wake_deadline_sleep_gate.c'
$powerSource = Join-Path $projectRoot 'main\power_service.c'
$failures = @()
foreach ($path in @($source, $header, $testSource, $powerSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if ($failures.Count -eq 0) {
    $sourceText = Get-Content -LiteralPath $source -Raw
    $powerText = Get-Content -LiteralPath $powerSource -Raw
    foreach ($required in @(
            '#include "wake_deadline_sleep_gate.h"',
            'wake_deadline_sleep_gate_begin\s*\(\s*&s_system_sleep_preparing\s*\)',
            'wake_deadline_sleep_gate_callbacks_drained\s*\(\s*&s_system_sleep_callbacks_inflight\s*\)',
            'wake_deadline_sleep_gate_abort\s*\(\s*&s_system_sleep_preparing\s*\)')) {
        if ($sourceText -notmatch $required) {
            $failures += "main/wake_deadline_service.c missing System Sleep gate use: $required"
        }
    }
    $prepareMatch = [regex]::Match(
        $sourceText,
        'device_status_t\s+wake_deadline_service_prepare_system_sleep\s*\([\s\S]*?\n\}\s*\n\s*void\s+wake_deadline_service_abort_system_sleep_prepare')
    if (-not $prepareMatch.Success) {
        $failures += 'main/wake_deadline_service.c must retain an inspectable System Sleep PREPARE body'
    } elseif ($prepareMatch.Value -match 'wake_deadline_service_abort_system_sleep_prepare\s*\(') {
        $failures += 'main/wake_deadline_service.c PREPARE timeout must retain admission closed until Power ABORT'
    }
    if ($powerText -notmatch 'wake_deadline_service_prepare_system_sleep[\s\S]{0,1200}wake_deadline_service_abort_system_sleep_prepare') {
        $failures += 'main/power_service.c must roll back Wake Deadline after its PREPARE failure'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Wake Deadline System Sleep test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_wake_deadline_sleep_gate.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Wake Deadline System Sleep compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Wake Deadline System Sleep test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Wake Deadline System Sleep check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Wake Deadline System Sleep check passed: timeout remains closed until Power ABORT'
