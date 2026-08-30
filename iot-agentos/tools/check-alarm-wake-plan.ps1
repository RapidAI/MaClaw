[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\alarm_wake_plan.c'
$header = Join-Path $projectRoot 'main\alarm_wake_plan.h'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_alarm_wake_plan.c'
$powerSource = Join-Path $projectRoot 'main\power_service.c'
$alarmSource = Join-Path $projectRoot 'main\services\alarm_service.c'
$deadlineSource = Join-Path $projectRoot 'main\wake_deadline_service.c'
$deadlineHeader = Join-Path $projectRoot 'main\wake_deadline_service.h'
$mainSource = Join-Path $projectRoot 'main\main.c'
$failures = @()

foreach ($path in @($source, $header, $testSource, $powerSource, $alarmSource, $deadlineSource, $deadlineHeader, $mainSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if (Test-Path -LiteralPath $header) {
    $text = Get-Content -LiteralPath $header -Raw
    if ($text -match '\b(?:esp_|gpio_|RTC_|freertos/|TaskHandle_t|SemaphoreHandle_t)\b') {
        $failures += 'alarm wake plan public contract leaked platform or RTOS detail'
    }
}
if (Test-Path -LiteralPath $source) {
    $text = Get-Content -LiteralPath $source -Raw
    if ($text -match '\b(?:esp_sleep|gpio_|esp_timer|xTask|vTask)\b') {
        $failures += 'alarm wake plan acquired a hardware or RTOS side effect'
    }
    if ($text -notmatch 'DENY_UNTRUSTED_TIME' -or
        $text -notmatch 'DENY_ALARM_DUE' -or
        $text -notmatch 'wake_deadline_epoch_ms' -or
        $text -notmatch 'wake_arm_epoch_ms' -or
        $text -notmatch 'drift_guard_ms' -or
        $text -notmatch 'alarm_wake_plan_revalidate_deadline') {
        $failures += 'alarm wake plan missing trusted-time or earliest-deadline fence'
    }
}
if (Test-Path -LiteralPath $powerSource) {
    $powerText = Get-Content -LiteralPath $powerSource -Raw
    foreach ($needle in @(
            '#include "alarm_wake_plan.h"',
            'alarm_service_earliest_queued_alarm',
            'system_sleep_prepare_remaining_ms',
            'wake_deadline_service_get_clock_status',
            'alarm_wake_plan_compute',
            'alarm_manager_abort_system_sleep_prepare',
            'wake_deadline_service_abort_system_sleep_prepare')) {
        if ($powerText -notmatch [regex]::Escape($needle)) {
            $failures += "Power PREPARE is missing alarm wake-plan wiring: $needle"
        }
    }
    $alarmIndex = $powerText.IndexOf('alarm_service_earliest_queued_alarm', [StringComparison]::Ordinal)
    $computeIndex = $powerText.IndexOf('alarm_wake_plan_compute', [StringComparison]::Ordinal)
    if ($alarmIndex -lt 0 -or $computeIndex -lt 0 -or $alarmIndex -gt $computeIndex) {
        $failures += 'Power must snapshot the earliest alarm before computing the wake plan'
    }
    if ($powerText -notmatch 'remaining_ms\s*=\s*system_sleep_prepare_remaining_ms\(prepare_deadline_us\)[\s\S]{0,300}alarm_service_earliest_queued_alarm') {
        $failures += 'earliest alarm query must consume the shared PREPARE remaining budget'
    }
}
if (Test-Path -LiteralPath $alarmSource) {
    $alarmText = Get-Content -LiteralPath $alarmSource -Raw
    if ($alarmText -notmatch 's_store\.count\s*>\s*0u') {
        $failures += 'Alarm earliest-queued query does not use the durable queue count'
    }
}
if (Test-Path -LiteralPath $deadlineSource) {
    $deadlineText = Get-Content -LiteralPath $deadlineSource -Raw
    if ($deadlineText -notmatch 'wake_deadline_service_get_clock_status') {
        $failures += 'Wake Deadline clock observation implementation is missing'
    }
    if ($deadlineText -notmatch 'xSemaphoreTake\(s_lock' -or
        $deadlineText -notmatch 'xSemaphoreGive\(s_lock') {
        $failures += 'Wake Deadline clock observation must serialize lifecycle state'
    }
    foreach ($needle in @(
            's_wall_clock_trusted',
            'wake_deadline_service_on_trusted_wall_clock_updated',
            's_wall_clock_trusted && clock_is_plausible')) {
        if ($deadlineText -notmatch [regex]::Escape($needle)) {
            $failures += "Wake Deadline is missing explicit trusted-time admission: $needle"
        }
    }
    if ($deadlineText -match '\bwake_deadline_service_on_wall_clock_updated\b') {
        $failures += 'Wake Deadline retained legacy non-authoritative wall-clock admission'
    }
}
if (Test-Path -LiteralPath $deadlineHeader) {
    $deadlineHeaderText = Get-Content -LiteralPath $deadlineHeader -Raw
    if ($deadlineHeaderText -notmatch 'wake_deadline_service_on_trusted_wall_clock_updated') {
        $failures += 'Wake Deadline public contract is missing trusted-time update entrypoint'
    }
}
if (Test-Path -LiteralPath $mainSource) {
    $mainText = Get-Content -LiteralPath $mainSource -Raw
    if ($mainText -notmatch 'wake_deadline_service_on_trusted_wall_clock_updated') {
        $failures += 'Clock Sync composition callback does not publish explicit trusted-time fact'
    }
}
$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for alarm wake plan test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_alarm_wake_plan.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host alarm wake plan compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) { $failures += "host alarm wake plan test failed (exit $LASTEXITCODE)" }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("alarm wake plan check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'alarm wake plan check passed: value-only trusted-time/deadline contract'
