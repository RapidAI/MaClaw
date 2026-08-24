[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_platform_storage_contract.c'
$source = Join-Path $projectRoot 'main\platform_storage.c'
$mockRoot = Join-Path $PSScriptRoot 'host_tests\mocks'
$failures = @()
foreach ($path in @($testSource, $source, (Join-Path $mockRoot 'esp_spiffs.h'))) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Platform Storage contract test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_platform_storage_contract.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror -Wno-format-truncation "-I$mockRoot" `
        "-I$(Join-Path $projectRoot 'main')" $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Platform Storage contract compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Platform Storage contract test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Platform Storage contract check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Platform Storage contract check passed: unknown VFS ownership remains fail-closed'
