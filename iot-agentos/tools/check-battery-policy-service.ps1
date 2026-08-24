[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_battery_policy_service.c'
$source = Join-Path $projectRoot 'main\battery_policy_service.c'
$mockRoot = Join-Path $PSScriptRoot 'host_tests\mocks'
$failures = @()
foreach ($path in @($testSource, $source, (Join-Path $mockRoot 'platform_power.h'),
                        (Join-Path $mockRoot 'esp_timer.h'),
                        (Join-Path $mockRoot 'freertos\FreeRTOS.h'))) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
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
