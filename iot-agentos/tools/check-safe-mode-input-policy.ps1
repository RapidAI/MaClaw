param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\presentation\safe_mode_input_policy.c'
$header = Join-Path $projectRoot 'main\presentation\safe_mode_input_policy.h'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_safe_mode_input_policy.c'
$binding = Join-Path $projectRoot 'main\presentation\input_binding.c'
$failures = @()

foreach ($path in @($source, $header, $testSource, $binding)) {
    if (-not (Test-Path -LiteralPath $path)) {
        $failures += "missing required SAFE_MODE input policy file: $path"
    }
}

if ($failures.Count -eq 0) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    $bindingText = Get-Content -LiteralPath $binding -Raw
    if ($headerText -match '\b(?:esp_|freertos/|TaskHandle_t|QueueHandle_t|SemaphoreHandle_t|gpio_|i2c_|httpd_|esp_http_client)\b' -or
        $sourceText -match '\b(?:esp_|freertos/|TaskHandle_t|QueueHandle_t|SemaphoreHandle_t|gpio_|i2c_|httpd_|esp_http_client)\b') {
        $failures += 'SAFE_MODE input policy must remain SDK/RTOS/board neutral'
    }
    if ($bindingText -notmatch 'safe_mode_input_policy_route\s*\(\s*alarm_manager_is_initialized\s*\(\s*\)\s*,\s*alarm_manager_is_ringing\s*\(\s*\)\s*,\s*primary_interaction_source\s*,\s*s_host_installed\s*&&\s*s_host\.safe_mode_active\s*&&\s*s_host\.safe_mode_active\s*\(\s*\)\s*\)' -or
        $bindingText -notmatch 'SAFE_MODE_INPUT_ROUTE_DISMISS_ALARM' -or
        $bindingText -notmatch 'SAFE_MODE_INPUT_ROUTE_IGNORE_SAFE_MODE') {
        $failures += 'Input Binding must delegate alarm-first SAFE_MODE admission to the shared value policy'
    }
    $safeModeGate = [regex]::Match(
        $bindingText,
        '(?s)SAFE_MODE_INPUT_ROUTE_IGNORE_SAFE_MODE.*?return\s*;')
    if (-not $safeModeGate.Success) {
        $failures += 'Input Binding must close SAFE_MODE ordinary admission with a terminal return'
    } else {
        $prefix = $bindingText.Substring(0, $safeModeGate.Index)
        foreach ($forbiddenBeforeGate in @(
                'fall_detection_service_cancel_from_user\s*\(',
                'command_service_consume_capture_stop_gesture\s*\(')) {
            if ($prefix -match $forbiddenBeforeGate) {
                $failures += "SAFE_MODE gate must precede nonessential foreground policy ($forbiddenBeforeGate)"
            }
        }
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for SAFE_MODE input policy test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_safe_mode_input_policy.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host SAFE_MODE input policy compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host SAFE_MODE input policy test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("SAFE_MODE input policy check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'SAFE_MODE input policy check passed: alarm dismiss remains available before ordinary SAFE_MODE rejection'
