[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_platform_power_fail_closed.c'
$mockRoot = Join-Path $PSScriptRoot 'host_tests\mocks'
$failures = @()
$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) { $failures += 'host C compiler (gcc or clang) is required for Power profile test' }
foreach ($path in @($testSource, (Join-Path $mockRoot 'esp_err.h'),
                        (Join-Path $mockRoot 'freertos\FreeRTOS.h'))) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

$profiles = @(
    @{ Name = 'compact'; Source = 'main\platform_power_compact.c'; Compact = 1 },
    @{ Name = 'round'; Source = 'main\platform_power_round.c'; Compact = 0 }
)
# Round profile adapters must reject impossible PMIC capacity values before
# publishing normalized telemetry; clamping a corrupt register to 100% would
# bypass the shared Battery Policy protection path.
$waveshareAdapter = Join-Path $projectRoot 'main\boards\waveshare_amoled_1_75c\waveshare_peripheral_adapter.h'
if (-not (Test-Path -LiteralPath $waveshareAdapter)) {
    $failures += "missing $waveshareAdapter"
} else {
    $waveshareSource = Get-Content -LiteralPath $waveshareAdapter -Raw
    if ($waveshareSource -notmatch 'if \(capacity > 100u\) return false') {
        $failures += 'Waveshare PMIC capacity does not fail closed above 100%'
    }
}
if ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    foreach ($profile in $profiles) {
        $source = Join-Path $projectRoot $profile.Source
        if (-not (Test-Path -LiteralPath $source)) {
            $failures += "$($profile.Name): missing $source"
            continue
        }
        $exe = Join-Path $outDir ("test_platform_power_{0}.exe" -f $profile.Name)
        & $cc.Source -std=c11 -Wall -Wextra -Werror `
            "-I$mockRoot" "-I$(Join-Path $projectRoot 'main')" `
            "-DTEST_COMPACT_PROFILE=$($profile.Compact)" `
            $testSource $source -o $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "$($profile.Name): Power profile compile failed (exit $LASTEXITCODE)"
            continue
        }
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "$($profile.Name): Power profile test failed (exit $LASTEXITCODE)"
        }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("platform Power fail-closed check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'platform Power fail-closed check passed: compact and round retain validated rollback semantics'
