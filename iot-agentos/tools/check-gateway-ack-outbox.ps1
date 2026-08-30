[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\services\gateway_ack_outbox_policy.c'
$header = Join-Path $projectRoot 'main\services\gateway_ack_outbox_policy.h'
$dispatcher = Join-Path $projectRoot 'main\services\gateway_dispatcher.c'
$test = Join-Path $PSScriptRoot 'host_tests\test_gateway_ack_outbox_policy.c'
$mockRoot = Join-Path $PSScriptRoot 'host_tests\mocks'
$failures = @()
foreach ($path in @($source,$header,$dispatcher,$test)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if (Test-Path -LiteralPath $dispatcher) {
    $text = Get-Content -LiteralPath $dispatcher -Raw
    if ($text -notmatch 'gateway_ack_outbox_validate_record') { $failures += 'Dispatcher does not use the ACK outbox value policy' }
    if ($text -notmatch 'gateway_dispatcher_flush_ack_outbox') { $failures += 'Dispatcher ACK outbox flush path missing' }
    if ($text -notmatch 'return outbox_err') { $failures += 'ACK outbox failure does not block new poll pages' }
    if ($text -notmatch 'existing_status' -or $text -notmatch 'DEVICE_STATUS_BUSY' -or
        $text -notmatch 'Never overwrite an older envelope') { $failures += 'ACK outbox overwrite guard missing' }
}
$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) { $failures += 'host C compiler (gcc or clang) is required for ACK outbox policy test' }
if ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_gateway_ack_outbox_policy.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$mockRoot" "-I$(Join-Path $projectRoot 'main')" $test $source -o $exe
    if ($LASTEXITCODE -ne 0) { $failures += "host ACK outbox policy compile failed (exit $LASTEXITCODE)" }
    else { & $exe; if ($LASTEXITCODE -ne 0) { $failures += "host ACK outbox policy test failed (exit $LASTEXITCODE)" } }
}
if ($failures.Count -gt 0) { Write-Error ("Gateway ACK outbox check failed:`n" + ($failures -join "`n")); exit 1 }
Write-Output 'Gateway ACK outbox check passed: bounded records validate and poll fail-closes on pending replay'
