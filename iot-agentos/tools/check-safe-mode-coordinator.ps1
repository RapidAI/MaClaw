[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_safe_mode_coordinator.c'
$source = Join-Path $projectRoot 'main\services\safe_mode_coordinator.c'
$header = Join-Path $projectRoot 'main\services\safe_mode_coordinator.h'
$configurationSource = Join-Path $projectRoot 'main\configuration_service.c'
$failureInjectionHeader = Join-Path $projectRoot 'main\provisioning_failure_injection.h'
$failureInjectionSource = Join-Path $projectRoot 'main\provisioning_failure_injection.c'
$rootSource = Join-Path $projectRoot 'main\main.c'
$failures = @()
foreach ($path in @($testSource, $source, $header, (Join-Path $projectRoot 'main\device_api.h'),
        $configurationSource, $failureInjectionHeader, $failureInjectionSource, $rootSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if ($failures.Count -eq 0) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    foreach ($required in @(
            'SAFE_MODE_COORDINATOR_ABI_VERSION',
            'SAFE_MODE_STAGE_QUIESCE_NONESSENTIAL',
            'SAFE_MODE_STAGE_INITIALIZE_CLOCK_FEEDBACK',
            'SAFE_MODE_STAGE_INITIALIZE_ALARM',
            'SAFE_MODE_STAGE_PUBLISH_DIAGNOSTIC_SURFACE',
            'SAFE_MODE_STAGE_FAILED',
            'safe_mode_coordinator_enter')) {
        if ($headerText -notmatch [regex]::Escape($required) -and
            $sourceText -notmatch [regex]::Escape($required)) {
            $failures += "SAFE_MODE coordinator contract is incomplete ($required)"
        }
    }
    if ($headerText -match '\b(?:esp_|freertos/|TaskHandle_t|QueueHandle_t|SemaphoreHandle_t|gpio_|i2c_|httpd_|esp_http_client)\b') {
        $failures += 'safe_mode_coordinator.h must remain value-only and SDK/RTOS/board/HTTP neutral'
    }
    if ($sourceText -match '\b(?:esp_|freertos/|TaskHandle_t|QueueHandle_t|SemaphoreHandle_t|httpd_|esp_http_client|abort_system_sleep_prepare|rollback)\b') {
        $failures += 'safe_mode_coordinator.c must not own SDK/RTOS/HTTP objects or normal-startup rollback'
    }
    if ($sourceText -notmatch 'quiesce_nonessential[\s\S]*?initialize_clock_feedback[\s\S]*?initialize_alarm[\s\S]*?publish_diagnostic_surface' -or
        $sourceText -notmatch 'SAFE_MODE_STAGE_FAILED[\s\S]*?return\s+status') {
        $failures += 'SAFE_MODE coordinator must preserve minimum-service ordering and terminal fail-closed failure'
    }
    $configurationText = Get-Content -LiteralPath $configurationSource -Raw
    $failureInjectionHeaderText = Get-Content -LiteralPath $failureInjectionHeader -Raw
    $failureInjectionSourceText = Get-Content -LiteralPath $failureInjectionSource -Raw
    if ($failureInjectionHeaderText -notmatch 'provisioning_failure_injection_safe_mode_force_setup_take_fails\s*\(' -or
        $failureInjectionSourceText -notmatch 'CONFIG_MACLAW_SAFE_MODE_TEST_FORCE_SETUP_TAKE_FAILURE' -or
        $configurationText -notmatch '(?s)load_locked\s*\(snapshot,\s*&requested\)\s*;[\s\S]*?provisioning_failure_injection_safe_mode_force_setup_take_fails\s*\(\)[\s\S]*?write_locked\s*\(snapshot,\s*false\)') {
        $failures += 'C7 force-setup fault seam must fail after durable read and before clearing the flag'
    }
    $rootText = Get-Content -LiteralPath $rootSource -Raw
    if ($rootText -notmatch '(?s)static\s+void\s+startup_enter_safe_mode_terminal_failure\s*\([^)]*\).*?lifecycle_service_degrade.*?ordinary startup rollback is intentionally skipped') {
        $failures += 'SAFE_MODE bridge failure requires its own terminal fail-closed transition'
    }
    foreach ($safeModeCallSite in @(
            'local-ready SAFE_MODE test injection',
            'configuration force-setup request')) {
        $escapedCallSite = [regex]::Escape($safeModeCallSite)
        if ($rootText -notmatch "(?s)startup_enter_safe_mode\s*\([^;]*?$escapedCallSite[^;]*?\);[\s\S]*?STARTUP_SAFE_MODE_ENTRY_TERMINAL_FAILURE\)\s*\{\s*startup_enter_safe_mode_terminal_failure") {
            $failures += "SAFE_MODE terminal bridge failure for '$safeModeCallSite' must not enter ordinary startup rollback"
        }
        if ($rootText -notmatch "(?s)STARTUP_SAFE_MODE_ENTRY_NOT_STARTED\)\s*\{\s*startup_enter_degraded") {
            $failures += 'SAFE_MODE pre-quiescence initialization failure must retain normal startup rollback'
        }
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for SAFE_MODE coordinator test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_safe_mode_coordinator.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host SAFE_MODE coordinator compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host SAFE_MODE coordinator test failed (exit $LASTEXITCODE)"
        }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("SAFE_MODE coordinator check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'SAFE_MODE coordinator check passed: minimum services share one deadline and every failure remains terminal'
