[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $root 'main\trusted_time_policy.c'
$header = Join-Path $root 'main\trusted_time_policy.h'
$main = Join-Path $root 'main\main.c'
$clock = Join-Path $root 'main\services\clock_sync_service.c'
$test = Join-Path $PSScriptRoot 'host_tests\test_trusted_time_policy.c'
$failures = @()
foreach ($p in @($source,$header,$main,$clock,$test)) { if (-not (Test-Path -LiteralPath $p)) { $failures += "missing $p" } }
$sourceText = if (Test-Path $source) { Get-Content $source -Raw } else { '' }
$headerText = if (Test-Path $header) { Get-Content $header -Raw } else { '' }
$mainText = if (Test-Path $main) { Get-Content $main -Raw } else { '' }
$clockText = if (Test-Path $clock) { Get-Content $clock -Raw } else { '' }
if ($headerText -match '\b(?:esp_|freertos/|cJSON|TaskHandle_t|SemaphoreHandle_t|wifi_|http|board_)\b') { $failures += 'trusted-time public contract leaked platform detail' }
if ($mainText -match '\bsettimeofday\s*\(' -or
    $mainText -match 'trusted_time_policy_state_observe\s*\(') {
    $failures += 'main.c must not apply wall-clock or own trusted-time state'
}
foreach ($needle in @('isfinite','floor','TRUSTED_TIME_MIN_MS','TRUSTED_TIME_MAX_MS','TRUSTED_TIME_SOURCE_HUB_AUTHENTICATED','TRUSTED_TIME_STATE_ANOMALY','trusted_time_policy_state_observe','TRUSTED_TIME_MAX_ROLLBACK_SEC')) { if ($sourceText -notmatch $needle) { $failures += "trusted-time policy missing $needle" } }
if ($mainText -notmatch 'clock_sync_service_apply_authenticated_millis\s*\(' -or
    $clockText -notmatch 'trusted_time_policy_from_millis\s*\(' -or
    $clockText -notmatch 'TRUSTED_TIME_SOURCE_HUB_AUTHENTICATED' -or
    $clockText -notmatch 's_time_apply_inflight' -or
    $clockText -notmatch 'settimeofday\s*\(') {
    $failures += 'Hub serverTime is not routed through trusted-time policy owner'
}
$cc = Get-Command gcc -ErrorAction SilentlyContinue; if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) { $failures += 'host compiler required' }
if ($failures.Count -eq 0) {
    $out = Join-Path $root 'build-host-tests'; New-Item -ItemType Directory -Force -Path $out | Out-Null
    $exe = Join-Path $out 'test_trusted_time_policy.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $root 'main')" $test $source -lm -o $exe
    if ($LASTEXITCODE -ne 0) { $failures += 'trusted-time host compile failed' } else { & $exe; if ($LASTEXITCODE -ne 0) { $failures += 'trusted-time host test failed' } }
}
if ($failures.Count) { Write-Error ($failures -join "`n"); exit 1 }
Write-Output 'trusted time policy check passed: bounded integral epochs and authenticated-source admission are enforced'
