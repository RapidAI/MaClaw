[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_resource_pressure_service.c'
$source = Join-Path $projectRoot 'main\resource_pressure_service.c'
$mockRoot = Join-Path $PSScriptRoot 'host_tests\mocks'
$failures = @()
foreach ($path in @($testSource, $source, (Join-Path $mockRoot 'host_compat.h'),
                     (Join-Path $mockRoot 'freertos\FreeRTOS.h'),
                     (Join-Path $mockRoot 'freertos\semphr.h'))) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Resource Pressure contract test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_resource_pressure_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$mockRoot" `
        "-I$(Join-Path $projectRoot 'main')" "-include$(Join-Path $mockRoot 'host_compat.h')" `
        $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Resource Pressure contract compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Resource Pressure contract test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Resource Pressure contract check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Resource Pressure contract check passed: malformed platform samples and stopped storage observation remain fail-closed'
