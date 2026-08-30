[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$src = Join-Path $root 'main\services\credential_service.c'
$hdr = Join-Path $root 'main\services\credential_service.h'
$test = Join-Path $PSScriptRoot 'host_tests\test_credential_service.c'
$transport = Join-Path $root 'main\services\gateway_transport.c'
$transportHdr = Join-Path $root 'main\services\gateway_transport.h'
$main = Join-Path $root 'main\main.c'
$failures = @()
foreach ($p in @($src,$hdr,$test,$transport,$transportHdr,$main)) { if (-not (Test-Path -LiteralPath $p)) { $failures += "missing $p" } }
$h = if (Test-Path $hdr) { Get-Content $hdr -Raw } else { '' }
$c = if (Test-Path $src) { Get-Content $src -Raw } else { '' }
$transportText = if (Test-Path $transport) { Get-Content $transport -Raw } else { '' }
$transportHeaderText = if (Test-Path $transportHdr) { Get-Content $transportHdr -Raw } else { '' }
$mainText = if (Test-Path $main) { Get-Content $main -Raw } else { '' }
if ($h -match '\b(?:esp_|freertos/|cJSON|TaskHandle_t|SemaphoreHandle_t|wifi_|http|board_)\b') { $failures += 'credential public contract leaked platform detail' }
foreach ($needle in @('generation','revoke_gateway_token','copy_gateway_token','bind_identity','restore_gateway_token','CREDENTIAL_SERVICE_MAX_TOKEN','wipe','bounded_length','out_length) *out_length = 0u','wipe(out, capacity)','set_generation_persistence','s_persistence_fault','s_floor_write_lock','persist_generation_floor','current_generation > requested_generation')) { if ($c -notmatch [regex]::Escape($needle)) { $failures += "credential lifecycle missing $needle" } }
if ($transportText -match 'snprintf\s*\(\s*authorization[\s\S]*s_gateway_token' -or
    $transportText -match 's_gateway_token\[0\]\s*&&\s*bearer_request') {
    $failures += 'Gateway bearer authorization bypasses Credential Service generation fence'
}
if ($transportText -notmatch 'gateway_transport_revoke_credentials' -or
    $transportHeaderText -notmatch 'gateway_transport_revoke_credentials' -or
    $mainText -notmatch 'factory_reset_reboot[\s\S]*gateway_transport_revoke_credentials') {
    $failures += 'factory-reset reboot is not wired to credential revocation'
}
if ($mainText -notmatch 'credential_service_set_generation_persistence' -or
    $mainText -notmatch 'credential_generation_floor_read' -or
    $mainText -notmatch 'credential_generation_floor_write') {
    $failures += 'credential generation floor is not wired before configuration load'
}
$cc = Get-Command gcc -ErrorAction SilentlyContinue; if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) { $failures += 'host compiler required' }
if ($failures.Count -eq 0) {
  $out = Join-Path $root 'build-host-tests'; New-Item -ItemType Directory -Force -Path $out | Out-Null
  $exe = Join-Path $out 'test_credential_service.exe'
  & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $root 'main')" $test $src -o $exe
  if ($LASTEXITCODE -ne 0) { $failures += 'credential host compile failed' } else { & $exe; if ($LASTEXITCODE -ne 0) { $failures += 'credential host test failed' } }
}
if ($failures.Count) { Write-Error ($failures -join "`n"); exit 1 }
Write-Output 'credential service check passed: generation-fenced storage, copy-out and revocation are enforced'
