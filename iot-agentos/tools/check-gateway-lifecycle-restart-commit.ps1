[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_gateway_lifecycle_restart_commit.c'
$source = Join-Path $projectRoot 'main\services\gateway_lifecycle_service.c'
$mockRoot = Join-Path $PSScriptRoot 'host_tests\mocks'
$failures = @()
foreach ($path in @($testSource, $source, (Join-Path $projectRoot 'main\services\gateway_lifecycle_service.h'),
                        (Join-Path $mockRoot 'esp_timer.h'), (Join-Path $mockRoot 'freertos\FreeRTOS.h'))) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Gateway Lifecycle restart-commit test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_gateway_lifecycle_restart_commit.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$mockRoot" "-I$(Join-Path $projectRoot 'main')" `
        $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Gateway Lifecycle restart-commit compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Gateway Lifecycle restart-commit test failed (exit $LASTEXITCODE)"
        }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("Gateway Lifecycle restart-commit check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Gateway Lifecycle restart-commit check passed: prepared worker generations retire without System Sleep rollback'
