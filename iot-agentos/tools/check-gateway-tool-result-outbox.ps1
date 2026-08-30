[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$src = Join-Path $root 'main\services\gateway_tool_result_outbox_policy.c'
$hdr = Join-Path $root 'main\services\gateway_tool_result_outbox_policy.h'
$main = Join-Path $root 'main\main.c'
$disp = Join-Path $root 'main\services\gateway_dispatcher.c'
$test = Join-Path $PSScriptRoot 'host_tests\test_gateway_tool_result_outbox_policy.c'
$fail = @()
foreach ($p in @($src,$hdr,$main,$disp,$test)) { if (-not (Test-Path -LiteralPath $p)) { $fail += "missing $p" } }
if (Test-Path -LiteralPath $main) {
  $t = Get-Content -LiteralPath $main -Raw
  foreach ($needle in @('tool_result_outbox','gateway_tool_result_outbox_validate_record','persistence_service_write_blob','gateway_host_flush_tool_result_outbox')) {
    if ($t -notmatch [regex]::Escape($needle)) { $fail += "main missing $needle" }
  }
  if ($t -match 'heap_caps_malloc\(GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY,\s*MALLOC_CAP_INTERNAL') {
    $fail += 'tool-result outbox buffers must not require internal heap'
  }
}
if (Test-Path -LiteralPath $disp) {
  $t = Get-Content -LiteralPath $disp -Raw
  if ($t -notmatch 'flush_tool_result_outbox' -or $t -notmatch 'pending tool-result outbox') { $fail += 'dispatcher does not flush tool-result outbox before polling' }
}
$cc = Get-Command gcc -ErrorAction SilentlyContinue; if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) { $fail += 'host C compiler required' }
if ($fail.Count -eq 0) {
  $out = Join-Path $root 'build-host-tests'; New-Item -ItemType Directory -Force -Path $out | Out-Null
  $exe = Join-Path $out 'test_gateway_tool_result_outbox_policy.exe'
  & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $PSScriptRoot 'host_tests\mocks')" "-I$(Join-Path $root 'main')" $test $src -o $exe
  if ($LASTEXITCODE -ne 0) { $fail += 'compile failed' } else { & $exe; if ($LASTEXITCODE -ne 0) { $fail += 'test failed' } }
}
if ($fail.Count) { Write-Error ($fail -join "`n"); exit 1 }
Write-Output 'Gateway Tool-result outbox check passed'
