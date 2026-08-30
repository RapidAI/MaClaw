[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$testSource = Join-Path $PSScriptRoot 'host_tests\test_official_device_profiles.c'
$validationSource = Join-Path $projectRoot 'main\device_profile_validation.c'
$failures = @()
foreach ($path in @($testSource, $validationSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for official Device profile matrix'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $profiles = @(
        @{ Name = 'bread-compact'; Source = 'main\boards\bread_compact\board_profile.c'; Id = 'bread-compact-wifi-lcd-v1';
           Defines = @('EXPECTED_WIDTH=240', 'EXPECTED_HEIGHT=320',
                       'EXPECTED_CAPABILITIES=DEVICE_CAPABILITY_REQUIRED_BASELINE',
                       'EXPECTED_PRIMARY_SOURCE=DEVICE_INPUT_SOURCE_PRIMARY_CONTROL',
                       'EXPECTED_WAKE_SOURCES=DEVICE_INPUT_SOURCE_FLAG(DEVICE_INPUT_SOURCE_PRIMARY_CONTROL)') },
        @{ Name = 'echoear-2st'; Source = 'main\boards\echoear_2st\board_profile.c'; Id = 'echoear-2st-r8';
           Defines = @('EXPECTED_WIDTH=360', 'EXPECTED_HEIGHT=360',
                       'EXPECTED_CAPABILITIES=(DEVICE_CAPABILITY_REQUIRED_BASELINE|DEVICE_CAPABILITY_TOUCH_INPUT|DEVICE_CAPABILITY_ROUND_DISPLAY)',
                       'EXPECTED_PRIMARY_SOURCE=DEVICE_INPUT_SOURCE_TOUCH',
                       'EXPECTED_WAKE_SOURCES=(DEVICE_INPUT_SOURCE_FLAG(DEVICE_INPUT_SOURCE_TOUCH)|DEVICE_INPUT_SOURCE_FLAG(DEVICE_INPUT_SOURCE_AUXILIARY_CONTROL))') },
        @{ Name = 'fangtang-4g'; Source = 'main\boards\fangtang_4g\board_profile.c'; Id = 'fangtang-4g-v1';
           Defines = @('EXPECTED_WIDTH=240', 'EXPECTED_HEIGHT=240',
                       'EXPECTED_CAPABILITIES=(DEVICE_CAPABILITY_REQUIRED_BASELINE|DEVICE_CAPABILITY_BATTERY_TELEMETRY|DEVICE_CAPABILITY_CELLULAR_TRANSPORT)',
                       'EXPECTED_PRIMARY_SOURCE=DEVICE_INPUT_SOURCE_PRIMARY_CONTROL',
                       'EXPECTED_WAKE_SOURCES=DEVICE_INPUT_SOURCE_FLAG(DEVICE_INPUT_SOURCE_PRIMARY_CONTROL)') },
        @{ Name = 'waveshare-amoled-1.75c'; Source = 'main\boards\waveshare_amoled_1_75c\board_profile.c'; Id = 'waveshare-s3-touch-amoled-1.75c-v1';
           Defines = @('EXPECTED_WIDTH=466', 'EXPECTED_HEIGHT=466',
                       'EXPECTED_CAPABILITIES=(DEVICE_CAPABILITY_REQUIRED_BASELINE|DEVICE_CAPABILITY_TOUCH_INPUT|DEVICE_CAPABILITY_BATTERY_TELEMETRY|DEVICE_CAPABILITY_ROUND_DISPLAY|DEVICE_CAPABILITY_MOTION_SENSOR)',
                       'EXPECTED_PRIMARY_SOURCE=DEVICE_INPUT_SOURCE_TOUCH',
                       'EXPECTED_WAKE_SOURCES=(DEVICE_INPUT_SOURCE_FLAG(DEVICE_INPUT_SOURCE_TOUCH)|DEVICE_INPUT_SOURCE_FLAG(DEVICE_INPUT_SOURCE_AUXILIARY_CONTROL))') }
    )
    foreach ($profile in $profiles) {
        $source = Join-Path $projectRoot $profile.Source
        if (-not (Test-Path -LiteralPath $source)) {
            $failures += "missing official profile $source"
            continue
        }
        $exe = Join-Path $outDir ("test_official_device_profile_" + $profile.Name + '.exe')
        $defines = @($profile.Defines | ForEach-Object { "-D$_" })
        # Keep literal quotes in the compiler argument; backslash-escaped quotes
        # are interpreted as stray characters by MinGW's preprocessor on Windows.
        $idDefinition = ('-DEXPECTED_PROFILE_ID_TEXT="{0}"' -f $profile.Id)
        & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
            $idDefinition $defines $testSource $validationSource $source -o $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "official Device profile matrix compile failed for $($profile.Name) (exit $LASTEXITCODE)"
            continue
        }
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "official Device profile matrix test failed for $($profile.Name) (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Official Device profile matrix check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Official Device profile matrix check passed: Bread, EchoEar, Fangtang, and Waveshare match their HAL declarations'
