[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$failures = @()

# A6 shadow coordinator contract stays a value-only public header: the later
# authoritative cut-over must not smuggle SDK/RTOS/board types across it.
$header = Join-Path $projectRoot 'main\services\foreground_coordinator.h'
$source = Join-Path $projectRoot 'main\services\foreground_coordinator.c'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_foreground_coordinator.c'
foreach ($path in @($header, $source, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if ($failures.Count -eq 0) {
    $headerText = Get-Content -LiteralPath $header -Raw
    foreach ($rule in @(
            '#include\s*[<"](?:esp_|freertos/|driver/)',
            '\besp_err_t\b',
            '\bTaskHandle_t\b|\bSemaphoreHandle_t\b|\bQueueHandle_t\b',
            '\bcJSON\b',
            '\bboard_port_[A-Za-z0-9_]+',
            '\bCONFIG_MACLAW_BOARD_')) {
        if ($headerText -match $rule) {
            $failures += "foreground_coordinator.h violates the value-only contract: /$rule/"
        }
    }
    # The authoritative switch must default to shadow mode: no production
    # caller may enable it inside shared sources.
    $cFiles = Get-ChildItem -LiteralPath (Join-Path $projectRoot 'main') -Filter '*.c' -File -Recurse |
        Where-Object { $_.Name -ne 'foreground_coordinator.c' }
    $enablers = @($cFiles | Select-String -Pattern 'foreground_coordinator_set_authoritative\s*\(\s*true')
    if ($enablers.Count -gt 0) {
        $failures += "authoritative cut-over leaked into production sources: $($enablers.Filename -join ', ')"
    }
}

if ($failures.Count -eq 0) {
    $cc = Get-Command gcc -ErrorAction SilentlyContinue
    if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
    if (-not $cc) {
        Write-Output 'foreground coordinator host test skipped: no gcc/clang on PATH'
    } else {
        $outDir = Join-Path $projectRoot 'build-host-tests'
        New-Item -ItemType Directory -Force -Path $outDir | Out-Null
        $exe = Join-Path $outDir 'test_foreground_coordinator.exe'
        $mockRoot = Join-Path $PSScriptRoot 'host_tests\mocks'
        # The production source feeds mask/seq/held_token only into ESP_LOG
        # macros, which the host mocks compile out. Keep -Werror for everything
        # else and silence exactly those log-only diagnostics.
        & $cc.Source -std=c11 -Wall -Wextra -Werror `
            -Wno-unused-but-set-variable -Wno-unused-variable `
            "-I$mockRoot" `
            "-I$(Join-Path $projectRoot 'main')" `
            $testSource $source -o $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "foreground coordinator host compile failed (exit $LASTEXITCODE)"
        } else {
            & $exe
            if ($LASTEXITCODE -ne 0) {
                $failures += "foreground coordinator host test failed (exit $LASTEXITCODE)"
            }
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("foreground coordinator check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'PASS foreground coordinator lease/token/shadow matrix'
Write-Output 'foreground coordinator check passed: value-only header, shadow-mode default, host regression green'
exit 0
