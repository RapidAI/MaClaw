[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$src = Join-Path $root 'main\wifi_enterprise_trust_policy.c'
$hdr = Join-Path $root 'main\wifi_enterprise_trust_policy.h'
$owner = Join-Path $root 'main\services\connectivity_wifi_driver_owner.c'
$tx = Join-Path $root 'main\configuration_transaction.c'
$test = Join-Path $PSScriptRoot 'host_tests\test_wifi_enterprise_trust_policy.c'
$failures = @()
foreach ($p in @($src,$hdr,$owner,$tx,$test)) { if (-not (Test-Path -LiteralPath $p)) { $failures += "missing $p" } }
$h = if (Test-Path $hdr) { Get-Content $hdr -Raw } else { '' }
$c = if (Test-Path $src) { Get-Content $src -Raw } else { '' }
$o = if (Test-Path $owner) { Get-Content $owner -Raw } else { '' }
$t = if (Test-Path $tx) { Get-Content $tx -Raw } else { '' }
if ($h -match '\b(?:esp_|freertos/|cJSON|TaskHandle_t|wifi_config_t|http)\b') { $failures += 'enterprise trust public header leaked platform detail' }
foreach ($needle in @('wifi_enterprise_trust_policy_valid_domain','has_dot','label_length')) { if ($c -notmatch $needle) { $failures += "domain policy missing $needle" } }
if ($o -notmatch 'wifi_enterprise_trust_policy_valid_domain' -or $o -notmatch '!config->use_system_ca') { $failures += 'EAP owner does not enforce CA and domain binding' }
if ($t -notmatch 'wifi_enterprise_trust_policy_valid_domain') { $failures += 'configuration transaction does not enforce domain binding' }
$cc = Get-Command gcc -ErrorAction SilentlyContinue; if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) { $failures += 'host compiler required' }
if ($failures.Count -eq 0) {
  $out = Join-Path $root 'build-host-tests'; New-Item -ItemType Directory -Force -Path $out | Out-Null
  $exe = Join-Path $out 'test_wifi_enterprise_trust_policy.exe'
  & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $root 'main')" $test $src -o $exe
  if ($LASTEXITCODE -ne 0) { $failures += 'enterprise trust host compile failed' } else { & $exe; if ($LASTEXITCODE -ne 0) { $failures += 'enterprise trust host test failed' } }
}
if ($failures.Count) { Write-Error ($failures -join "`n"); exit 1 }
Write-Output 'enterprise trust policy check passed: EAP CA and DNS domain binding are fail-closed'
