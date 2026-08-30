[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_official_device_profiles.c'
$validationSource = Join-Path $projectRoot 'main\device_profile_validation.c'
$profileSource = Join-Path $projectRoot 'main\boards\reference_fake\board_profile.c'
$kconfigSource = Join-Path $projectRoot 'main\Kconfig.projbuild'
$cmakeSource = Join-Path $projectRoot 'main\CMakeLists.txt'
$defaultsSource = Join-Path $projectRoot 'sdkconfig.defaults.reference-fake'
$wrapperSource = Join-Path $PSScriptRoot 'build-profile.cmd'
$failures = @()

foreach ($path in @($testSource, $validationSource, $profileSource,
                    $kconfigSource, $cmakeSource, $defaultsSource, $wrapperSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if ($failures.Count -eq 0) {
    $kconfigText = Get-Content -LiteralPath $kconfigSource -Raw
    $cmakeText = Get-Content -LiteralPath $cmakeSource -Raw
    $defaultsText = Get-Content -LiteralPath $defaultsSource -Raw
    $wrapperText = Get-Content -LiteralPath $wrapperSource -Raw
    foreach ($required in @(
        'config MACLAW_BOARD_REFERENCE_FAKE',
        'default "reference-fake-v1" if MACLAW_BOARD_REFERENCE_FAKE',
        'default "maclaw-clawmate:reference-fake-v1:maclaw-s3-16m-factory-v2" if MACLAW_BOARD_REFERENCE_FAKE',
        'default "ClawMate / Reference Fake \(CI only\)" if MACLAW_BOARD_REFERENCE_FAKE'
    )) {
        if ($kconfigText -notmatch $required) {
            $failures += "main/Kconfig.projbuild is missing Reference/Fake profile contract: $required"
        }
    }
    foreach ($required in @(
        'CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD OR CONFIG_MACLAW_BOARD_REFERENCE_FAKE',
        'boards/reference_fake/board_profile\.c',
        'set\(MACLAW_PROFILE "reference-fake"\)',
        'bread_compact_splash\.rgb565'
    )) {
        if ($cmakeText -notmatch $required) {
            $failures += "main/CMakeLists.txt is missing Reference/Fake profile build wiring: $required"
        }
    }
    if ($defaultsText -notmatch '(?m)^CONFIG_MACLAW_BOARD_REFERENCE_FAKE=y\s*$') {
        $failures += 'sdkconfig.defaults.reference-fake must select only MACLAW_BOARD_REFERENCE_FAKE'
    }
    foreach ($required in @(
        'if /I "%MACLAW_PROFILE%"=="reference-fake"',
        'set "MACLAW_BUILD_DIR=build-reference-fake"',
        'sdkconfig\.defaults\.reference-fake',
        'Reference fake profile is CI-only and cannot flash or monitor'
    )) {
        if ($wrapperText -notmatch $required) {
            $failures += "tools/build-profile.cmd is missing Reference/Fake profile safety wiring: $required"
        }
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Reference/Fake Device profile validation'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_reference_fake_device_profile.exe'
    $idDefinition = if ($PSVersionTable.PSEdition -eq 'Desktop') {
        '-DEXPECTED_PROFILE_ID_TEXT=\"reference-fake-v1\"'
    } else {
        '-DEXPECTED_PROFILE_ID_TEXT="reference-fake-v1"'
    }
    $defines = @(
        $idDefinition,
        '-DEXPECTED_WIDTH=240',
        '-DEXPECTED_HEIGHT=320',
        '-DEXPECTED_CAPABILITIES=DEVICE_CAPABILITY_REQUIRED_BASELINE',
        '-DEXPECTED_PRIMARY_SOURCE=DEVICE_INPUT_SOURCE_PRIMARY_CONTROL',
        '-DEXPECTED_WAKE_SOURCES=DEVICE_INPUT_SOURCE_FLAG(DEVICE_INPUT_SOURCE_PRIMARY_CONTROL)'
    )
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $defines $testSource $validationSource $profileSource -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "Reference/Fake Device profile compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "Reference/Fake Device profile test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Reference/Fake profile check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Reference/Fake profile check passed: CI-only fourth profile has a coherent HAL declaration and isolated build wiring'
