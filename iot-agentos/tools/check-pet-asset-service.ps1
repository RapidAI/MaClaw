[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$serviceC = Join-Path $projectRoot 'main\services\pet_asset_service.c'
$serviceH = Join-Path $projectRoot 'main\services\pet_asset_service.h'
$cacheC = Join-Path $projectRoot 'main\pet_asset_cache_storage.c'
$cacheH = Join-Path $projectRoot 'main\pet_asset_cache_storage.h'
$testC = Join-Path $PSScriptRoot 'host_tests\test_pet_asset_service.c'
$cacheTestC = Join-Path $PSScriptRoot 'host_tests\test_pet_asset_cache_storage.c'
$cjsonDir = Join-Path $projectRoot 'managed_components\espressif__cjson\cJSON'
$cjsonC = Join-Path $cjsonDir 'cJSON.c'
$failures = @()

foreach ($path in @($serviceC, $serviceH, $cacheC, $cacheH, $testC, $cacheTestC, (Join-Path $cjsonDir 'cJSON.h'), $cjsonC)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if (Test-Path -LiteralPath $serviceH) {
    $header = Get-Content -LiteralPath $serviceH -Raw
    foreach ($api in @(
            'pet_asset_service_frame_bytes',
            'pet_asset_service_calculate_memory_requirements',
            'pet_asset_service_format_cache_metadata',
            'pet_asset_service_parse_cache_metadata',
            'pet_asset_service_sha256_matches_hex',
            'pet_asset_service_limit_frame_count',
            'pet_asset_service_next_memory_fallback')) {
        if ($header -notmatch ("\b$api\s*\(")) { $failures += "header missing $api" }
    }
    if ($header -match '\b(?:esp_|freertos/|driver/|TaskHandle_t|SemaphoreHandle_t|device_display|heap_caps)\b') {
        $failures += 'pet asset public contract leaked platform/RTOS/allocator detail'
    }
}

if (Test-Path -LiteralPath $serviceC) {
    $source = Get-Content -LiteralPath $serviceC -Raw
    if ($source -match '\b(?:device_display_get_pet_asset_install_budget|device_resource_pressure|heap_caps|scene_presenter_set_pet_asset|xTask|SemaphoreHandle_t)\b') {
        $failures += 'pet asset value service absorbed Display/pressure/allocator/renderer/RTOS ownership'
    }
}

if (Test-Path -LiteralPath $cacheH) {
    $cacheHeader = Get-Content -LiteralPath $cacheH -Raw
    foreach ($api in @(
            'pet_asset_cache_storage_write',
            'pet_asset_cache_storage_read_descriptor',
            'pet_asset_cache_storage_read_frame',
            'pet_asset_cache_storage_clear',
            'pet_asset_cache_storage_drop_if_stale')) {
        if ($cacheHeader -notmatch ("\b$api\s*\(")) { $failures += "cache header missing $api" }
    }
    if ($cacheHeader -match '\b(?:esp_|freertos/|driver/|TaskHandle_t|SemaphoreHandle_t|device_display|heap_caps)\b') {
        $failures += 'pet cache storage public contract leaked platform/RTOS/allocator/VFS handle detail'
    }
}

if (Test-Path -LiteralPath $cacheC) {
    $cacheSource = Get-Content -LiteralPath $cacheC -Raw
    if ($cacheSource -match '\b(?:heap_caps|scene_presenter_set_pet_asset|xTask|SemaphoreHandle_t|board_port|esp_)\b') {
        $failures += 'pet cache storage adapter absorbed allocator/renderer/RTOS/board ownership'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for pet asset value contract test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $cacheDir = Join-Path $outDir 'pet-cache'
    New-Item -ItemType Directory -Force -Path $cacheDir | Out-Null
    $exe = Join-Path $outDir 'test_pet_asset_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror -ffunction-sections -fdata-sections `
        "-I$cjsonDir" "-I$(Join-Path $projectRoot 'main')" $testC $serviceC `
        $cjsonC `
        '-Wl,--gc-sections' -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host pet asset service compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host pet asset service test failed (exit $LASTEXITCODE)"
        }
    }
    $cacheExe = Join-Path $outDir 'test_pet_asset_cache_storage.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror -ffunction-sections -fdata-sections `
        "-I$cjsonDir" "-I$(Join-Path $projectRoot 'main')" `
        '-DPET_CACHE_STORAGE_HOST_TEST' `
        $cacheTestC $cacheC $serviceC $cjsonC '-Wl,--gc-sections' -o $cacheExe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host pet cache storage compile failed (exit $LASTEXITCODE)"
    } else {
        Push-Location $projectRoot
        try {
            & $cacheExe
            if ($LASTEXITCODE -ne 0) {
                $failures += "host pet cache storage test failed (exit $LASTEXITCODE)"
            }
        } finally {
            Pop-Location
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("pet asset service check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'pet asset service check passed: value contract and cache storage adapter have no Display/pressure/allocator/RTOS ownership and host tests pass'
