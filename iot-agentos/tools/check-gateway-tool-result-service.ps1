[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$src = Join-Path $root 'main\services\gateway_tool_result_service.c'
$hdr = Join-Path $root 'main\services\gateway_tool_result_service.h'
$main = Join-Path $root 'main\main.c'
$cmake = Join-Path $root 'main\CMakeLists.txt'
$test = Join-Path $PSScriptRoot 'host_tests\test_gateway_tool_result_service.c'
$outbox = Join-Path $root 'main\services\gateway_tool_result_outbox_policy.c'
$cjsonDir = Join-Path $root 'managed_components\espressif__cjson\cJSON'
$cjsonC = Join-Path $cjsonDir 'cJSON.c'
$fail = @()
foreach ($p in @($src,$hdr,$main,$cmake,$test,$outbox,(Join-Path $cjsonDir 'cJSON.h'),$cjsonC)) { if (-not (Test-Path -LiteralPath $p)) { $fail += "missing $p" } }
if (Test-Path -LiteralPath $main) {
  $t = Get-Content -LiteralPath $main -Raw
  # The orchestration must be owned by the service, not the composition root.
  foreach ($needle in @('s_delivered_tool_result_id','handle_client_tool_call','/api/im-gateway/v1/tool-result')) {
    if ($t -match [regex]::Escape($needle)) { $fail += "main still owns $needle" }
  }
  foreach ($needle in @('gateway_tool_result_service_init','gateway_tool_result_service_handle_tool_call','gateway_tool_result_service_outbox_already_delivered','gateway_tool_result_service_flush_outbox')) {
    if ($t -notmatch [regex]::Escape($needle)) { $fail += "main missing service wiring $needle" }
  }
}
if (Test-Path -LiteralPath $hdr) {
  $t = Get-Content -LiteralPath $hdr -Raw
  if ($t -match '(?m)^\s*#include\s*[<"][^>"]*(cJSON|esp_|freertos|heap_caps|stdio)') {
    $fail += 'service header is not value-only (banned include)'
  }
  foreach ($api in @('gateway_tool_result_service_init','gateway_tool_result_service_handle_tool_call','gateway_tool_result_service_outbox_already_delivered','gateway_tool_result_service_flush_outbox')) {
    if ($t -notmatch ("\b" + [regex]::Escape($api) + "\b")) { $fail += "service header missing $api" }
  }
}
if (Test-Path -LiteralPath $src) {
  $t = Get-Content -LiteralPath $src -Raw
  foreach ($needle in @('/api/im-gateway/v1/tool-result','s_delivered_tool_result_id','gateway_tool_result_outbox_validate_record','gateway_tool_result_outbox_upgrade_legacy','gateway_tool_result_outbox_peek','gateway_tool_result_outbox_pop','gateway_transport_post_json','persistence_service_read_blob','persistence_service_write_blob','persistence_service_erase_key','factory_reset_service_reboot_if_pending','device_tool_registry_execute','capacity_exhausted','retryable')) {
    if ($t -notmatch [regex]::Escape($needle)) { $fail += "service missing $needle" }
  }
  if ($t -match 'MALLOC_CAP_INTERNAL') { $fail += 'tool-result outbox buffers must not require internal heap' }
}
if (Test-Path -LiteralPath $cmake) {
  $t = Get-Content -LiteralPath $cmake -Raw
  if ($t -notmatch [regex]::Escape('services/gateway_tool_result_service.c')) { $fail += 'CMakeLists missing services/gateway_tool_result_service.c' }
}
$cc = Get-Command gcc -ErrorAction SilentlyContinue; if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) { $fail += 'host C compiler required' }
if ($fail.Count -eq 0) {
  $out = Join-Path $root 'build-host-tests'; New-Item -ItemType Directory -Force -Path $out | Out-Null
  $exe = Join-Path $out 'test_gateway_tool_result_service.exe'
  & $cc.Source -std=c11 -Wall -Wextra -Werror `
      "-I$(Join-Path $PSScriptRoot 'host_tests\mocks')" "-I$cjsonDir" "-I$(Join-Path $root 'main')" `
      $test $src $outbox $cjsonC -o $exe
  if ($LASTEXITCODE -ne 0) { $fail += 'compile failed' } else { & $exe; if ($LASTEXITCODE -ne 0) { $fail += 'test failed' } }
}
if ($fail.Count) { Write-Error ($fail -join "`n"); exit 1 }
Write-Output 'Gateway Tool-result service check passed'
