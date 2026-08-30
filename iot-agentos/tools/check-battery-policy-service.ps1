[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_battery_policy_service.c'
$source = Join-Path $projectRoot 'main\battery_policy_service.c'
$header = Join-Path $projectRoot 'main\battery_policy_service.h'
$displaySource = Join-Path $projectRoot 'main\display_service.c'
$audioSource = Join-Path $projectRoot 'main\audio_service.c'
$mockRoot = Join-Path $PSScriptRoot 'host_tests\mocks'
$failures = @()
foreach ($path in @($testSource, $source, $header, $displaySource, $audioSource, (Join-Path $mockRoot 'platform_power.h'),
                        (Join-Path $mockRoot 'esp_timer.h'),
                        (Join-Path $mockRoot 'freertos\FreeRTOS.h'))) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    foreach ($needle in @('s_emergency_checkpoint_in_flight','s_emergency_checkpoint_done',
                          's_emergency_checkpoint_failed',
                          'battery_policy_service_try_begin_emergency_checkpoint',
                          'battery_policy_service_complete_emergency_checkpoint',
                          'battery_policy_service_limit_backlight_percent',
                          'if (!device_power_get_telemetry',
                          'telemetry->available',
                          'DEVICE_BATTERY_POLICY_PROTECT')) {
        if ($text -notmatch [regex]::Escape($needle)) { $failures += "missing emergency checkpoint contract: $needle" }
    }
    if ($text -notmatch 's_emergency_checkpoint_failed' -or
        $text -notmatch 'else s_emergency_checkpoint_failed = true') {
        $failures += 'checkpoint failure must latch terminally for the current PROTECT generation'
    }
}
if (Test-Path -LiteralPath $displaySource) {
    $displayText = Get-Content -LiteralPath $displaySource -Raw
    if ($displayText -notmatch 'battery_policy_service_limit_backlight_percent') {
        $failures += 'Display Service does not apply the Battery Policy backlight limit'
    }
}
if (Test-Path -LiteralPath $audioSource) {
    $audioText = Get-Content -LiteralPath $audioSource -Raw
    if ($audioText -notmatch 'platform_audio_play_alarm_burst\(peak_percent\)') {
        $failures += 'Audio Service does not pass a policy-derived alarm peak limit'
    }
}
$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Battery Policy test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_battery_policy_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$mockRoot" "-I$(Join-Path $projectRoot 'main')" `
        $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Battery Policy compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Battery Policy test failed (exit $LASTEXITCODE)"
        }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("Battery Policy check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Battery Policy check passed: System Sleep telemetry admission remains fail-closed'
