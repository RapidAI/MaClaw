[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_device_profile_validation.c'
$source = Join-Path $projectRoot 'main\device_profile_validation.c'
$failures = @()
foreach ($path in @($testSource, $source)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Device profile validation test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_device_profile_validation.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Device profile validation compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Device profile validation test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Device profile validation check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Device profile validation check passed: input, wake, and round-display facts are coherent'
