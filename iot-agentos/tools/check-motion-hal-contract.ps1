[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_motion_hal_contract.c'
$sources = @(
    (Join-Path $projectRoot 'main\motion_service.c'),
    (Join-Path $projectRoot 'main\platform_sensor.c')
)
$failures = @()
foreach ($path in @($testSource) + $sources) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Motion HAL contract test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_motion_hal_contract.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $testSource $sources -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Motion HAL contract compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Motion HAL contract test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Motion HAL contract check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Motion HAL contract check passed: unsupported profiles bypass adapters and successful samples are ABI-complete and chronological'
