[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$platformWake = Join-Path $projectRoot 'main\platform_wake.c'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_wake_profile_matrix.c'
$failures = @()

$profiles = @(
    @{ Name = 'bread-compact'; Source = 'main/platform_wake_bread_compact.c'; Display = 2; Light = 3; Deep = 3 },
    @{ Name = 'echoear-2st'; Source = 'main/platform_wake_echoear_2st.c'; Display = 12; Light = 9; Deep = 9 },
    @{ Name = 'fangtang-4g'; Source = 'main/platform_wake_fangtang_4g.c'; Display = 2; Light = 3; Deep = 3 },
    @{ Name = 'waveshare-amoled-1.75c'; Source = 'main/platform_wake_waveshare_amoled_1_75c.c'; Display = 12; Light = 13; Deep = 9 }
)

foreach ($path in @($platformWake, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Wake profile matrix test'
}

$outDir = Join-Path $projectRoot 'build-host-tests'
if ($failures.Count -eq 0) {
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    foreach ($profile in $profiles) {
        $profileSource = Join-Path $projectRoot $profile.Source
        if (-not (Test-Path -LiteralPath $profileSource)) {
            $failures += "$($profile.Name): missing $profileSource"
            continue
        }
        $exe = Join-Path $outDir ("test_wake_profile_matrix_{0}.exe" -f $profile.Name)
        & $cc.Source -std=c11 -Wall -Wextra -Werror `
            "-I$(Join-Path $projectRoot 'main')" `
            "-DEXPECTED_DISPLAY_OFF_SOURCES=$($profile.Display)" `
            "-DEXPECTED_LIGHT_SLEEP_CANDIDATES=$($profile.Light)" `
            "-DEXPECTED_DEEP_SLEEP_CANDIDATES=$($profile.Deep)" `
            $testSource $platformWake $profileSource -o $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "$($profile.Name): Wake profile matrix compile failed (exit $LASTEXITCODE)"
            continue
        }
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "$($profile.Name): Wake profile matrix test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("wake profile matrix check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'wake profile matrix check passed: all four profiles retain candidate-only Light/Deep wake sources'
