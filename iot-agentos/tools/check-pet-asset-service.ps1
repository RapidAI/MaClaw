[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$serviceC = Join-Path $projectRoot 'main\services\pet_asset_service.c'
$serviceH = Join-Path $projectRoot 'main\services\pet_asset_service.h'
$downloadC = Join-Path $projectRoot 'main\services\pet_asset_download_service.c'
$downloadH = Join-Path $projectRoot 'main\services\pet_asset_download_service.h'
$runtimeC = Join-Path $projectRoot 'main\services\pet_asset_runtime_service.c'
$runtimeH = Join-Path $projectRoot 'main\services\pet_asset_runtime_service.h'
$applyC = Join-Path $projectRoot 'main\services\pet_asset_apply_service.c'
$applyH = Join-Path $projectRoot 'main\services\pet_asset_apply_service.h'
$admissionC = Join-Path $projectRoot 'main\services\startup_pet_asset_admission_service.c'
$admissionH = Join-Path $projectRoot 'main\services\startup_pet_asset_admission_service.h'
$profileC = Join-Path $projectRoot 'main\services\pet_asset_profile_service.c'
$profileH = Join-Path $projectRoot 'main\services\pet_asset_profile_service.h'
$sleepC = Join-Path $projectRoot 'main\services\startup_pet_asset_sleep_service.c'
$sleepH = Join-Path $projectRoot 'main\services\startup_pet_asset_sleep_service.h'
$startupC = Join-Path $projectRoot 'main\services\pet_asset_startup_service.c'
$startupH = Join-Path $projectRoot 'main\services\pet_asset_startup_service.h'
$restoreC = Join-Path $projectRoot 'main\services\pet_asset_restore_service.c'
$restoreH = Join-Path $projectRoot 'main\services\pet_asset_restore_service.h'
$restoreWorkerC = Join-Path $projectRoot 'main\services\pet_asset_restore_worker_service.c'
$restoreWorkerH = Join-Path $projectRoot 'main\services\pet_asset_restore_worker_service.h'
$retryC = Join-Path $projectRoot 'main\services\pet_asset_retry_service.c'
$retryH = Join-Path $projectRoot 'main\services\pet_asset_retry_service.h'
$cacheCoordinatorC = Join-Path $projectRoot 'main\services\pet_cache_service.c'
$cacheCoordinatorH = Join-Path $projectRoot 'main\services\pet_cache_service.h'
$cacheC = Join-Path $projectRoot 'main\pet_asset_cache_storage.c'
$cacheH = Join-Path $projectRoot 'main\pet_asset_cache_storage.h'
$testC = Join-Path $PSScriptRoot 'host_tests\test_pet_asset_service.c'
$downloadTestC = Join-Path $PSScriptRoot 'host_tests\test_pet_asset_download_service.c'
$runtimeTestC = Join-Path $PSScriptRoot 'host_tests\test_pet_asset_runtime_service.c'
$admissionTestC = Join-Path $PSScriptRoot 'host_tests\test_startup_pet_asset_admission_service.c'
$profileTestC = Join-Path $PSScriptRoot 'host_tests\test_pet_asset_profile_service.c'
$sleepTestC = Join-Path $PSScriptRoot 'host_tests\test_startup_pet_asset_sleep_service.c'
$startupTestC = Join-Path $PSScriptRoot 'host_tests\test_pet_asset_startup_service.c'
$restoreTestC = Join-Path $PSScriptRoot 'host_tests\test_pet_asset_restore_service.c'
$restoreWorkerTestC = Join-Path $PSScriptRoot 'host_tests\test_pet_asset_restore_worker_service.c'
$retryTestC = Join-Path $PSScriptRoot 'host_tests\test_pet_asset_retry_service.c'
$cacheTestC = Join-Path $PSScriptRoot 'host_tests\test_pet_asset_cache_storage.c'
$cjsonDir = Join-Path $projectRoot 'managed_components\espressif__cjson\cJSON'
$cjsonC = Join-Path $cjsonDir 'cJSON.c'
$failures = @()

foreach ($path in @($serviceC, $serviceH, $downloadC, $downloadH, $runtimeC, $runtimeH, $applyC, $applyH, $admissionC, $admissionH, $profileC, $profileH, $sleepC, $sleepH, $startupC, $startupH, $restoreC, $restoreH, $restoreWorkerC, $restoreWorkerH, $retryC, $retryH, $cacheCoordinatorC, $cacheCoordinatorH, $cacheC, $cacheH, $testC, $downloadTestC, $runtimeTestC, $admissionTestC, $profileTestC, $sleepTestC, $startupTestC, $restoreTestC, $restoreWorkerTestC, $retryTestC, $cacheTestC, (Join-Path $cjsonDir 'cJSON.h'), $cjsonC)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if ((Test-Path -LiteralPath $retryH) -and (Test-Path -LiteralPath $retryC)) {
    $retryHeader = Get-Content -LiteralPath $retryH -Raw
    $retrySource = Get-Content -LiteralPath $retryC -Raw
    foreach ($api in @('pet_asset_retry_service_init', 'pet_asset_retry_service_reset',
                         'pet_asset_retry_service_note_failure',
                         'pet_asset_retry_service_exhausted')) {
        if ($retryHeader -notmatch ("\b$api\b")) {
            $failures += "pet asset retry header missing $api"
        }
    }
    if ($retryHeader -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|gateway_)\b') {
        $failures += 'pet asset retry public contract leaked platform/RTOS/allocator/crypto/gateway detail'
    }
    if ($retrySource -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|gateway_)\b') {
        $failures += 'pet asset retry service absorbed platform/RTOS/allocator/crypto/gateway ownership'
    }
}

if ((Test-Path -LiteralPath $downloadH) -and (Test-Path -LiteralPath $downloadC)) {
    $downloadHeader = Get-Content -LiteralPath $downloadH -Raw
    $downloadSource = Get-Content -LiteralPath $downloadC -Raw
    foreach ($api in @('pet_asset_download_service_fetch', 'request_frame',
                         'verify_frame_sha256', 'wait_before_retry',
                         'wait_before_pack_retry',
                         'pet_asset_download_service_fetch_startup_pack',
                         'install_first_frame_preview')) {
        if ($downloadHeader -notmatch ("\b$api\b")) {
            $failures += "pet asset download header missing $api"
        }
    }
    if ($downloadHeader -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_)\b') {
        $failures += 'pet asset download public contract leaked platform/RTOS/allocator/crypto detail'
    }
    if ($downloadSource -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport)\b') {
        $failures += 'pet asset download service absorbed HTTP/crypto/allocator/RTOS/renderer/transport ownership'
    }
}

if ((Test-Path -LiteralPath $runtimeH) -and (Test-Path -LiteralPath $runtimeC)) {
    $runtimeHeader = Get-Content -LiteralPath $runtimeH -Raw
    $runtimeSource = Get-Content -LiteralPath $runtimeC -Raw
    foreach ($api in @('pet_asset_runtime_service_apply', 'capture_gateway_lease',
                         'capacity_available', 'drop_stale_cache', 'download',
                         'install_full', 'cache_in_background', 'release_frames')) {
        if ($runtimeHeader -notmatch ("\b$api\b")) {
            $failures += "pet asset runtime header missing $api"
        }
    }
    if ($runtimeHeader -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_)\b') {
        $failures += 'pet asset runtime public contract leaked platform/RTOS/allocator/crypto detail'
    }
    if ($runtimeSource -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport)\b') {
        $failures += 'pet asset runtime service absorbed HTTP/crypto/allocator/RTOS/renderer/transport ownership'
    }
}

if ((Test-Path -LiteralPath $admissionH) -and (Test-Path -LiteralPath $admissionC)) {
    $admissionHeader = Get-Content -LiteralPath $admissionH -Raw
    $admissionSource = Get-Content -LiteralPath $admissionC -Raw
    foreach ($api in @('startup_pet_asset_admission_service_admit_pending',
                         'startup_pet_asset_admission_service_rearm_preempted',
                         'capacity_available', 'drop_stale_cache',
                         'take_capacity_retry', 'schedule_retry',
                         'finish_generation', 'start_worker')) {
        if ($admissionHeader -notmatch ("\b$api\b")) {
            $failures += "startup pet admission header missing $api"
        }
    }
    if ($admissionHeader -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_)\b') {
        $failures += 'startup pet admission public contract leaked platform/RTOS/allocator/crypto detail'
    }
    if ($admissionSource -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport)\b') {
        $failures += 'startup pet admission service absorbed HTTP/crypto/allocator/RTOS/renderer/transport ownership'
    }
}

if ((Test-Path -LiteralPath $profileH) -and (Test-Path -LiteralPath $profileC)) {
    $profileHeader = Get-Content -LiteralPath $profileH -Raw
    $profileSource = Get-Content -LiteralPath $profileC -Raw
    foreach ($api in @('pet_asset_profile_service_apply', 'startup_profile_matches',
                         'set_startup_pending', 'apply_asset', 'clear_asset',
                         'note_transient_failure', 'retry_exhausted', 'reset_retries')) {
        if ($profileHeader -notmatch ("\b$api\b")) {
            $failures += "pet asset profile header missing $api"
        }
    }
    if ($profileHeader -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_)\b') {
        $failures += 'pet asset profile public contract leaked platform/RTOS/allocator/crypto detail'
    }
    if ($profileSource -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport)\b') {
        $failures += 'pet asset profile service absorbed HTTP/crypto/allocator/RTOS/renderer/transport ownership'
    }
}

if ((Test-Path -LiteralPath $sleepH) -and (Test-Path -LiteralPath $sleepC)) {
    $sleepHeader = Get-Content -LiteralPath $sleepH -Raw
    $sleepSource = Get-Content -LiteralPath $sleepC -Raw
    foreach ($api in @('startup_pet_asset_sleep_service_prepare',
                         'startup_pet_asset_sleep_service_abort',
                         'prepare_state', 'prepare_worker', 'prepare_retry',
                         'prepare_cache', 'abort_state', 'rearm_preempted')) {
        if ($sleepHeader -notmatch ("\b$api\b")) {
            $failures += "startup pet sleep header missing $api"
        }
    }
    if ($sleepHeader -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_)\b') {
        $failures += 'startup pet sleep public contract leaked platform/RTOS/allocator/crypto detail'
    }
    if ($sleepSource -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport)\b') {
        $failures += 'startup pet sleep service absorbed HTTP/crypto/allocator/RTOS/renderer/transport ownership'
    }
}

if ((Test-Path -LiteralPath $startupH) -and (Test-Path -LiteralPath $startupC)) {
    $startupHeader = Get-Content -LiteralPath $startupH -Raw
    $startupSource = Get-Content -LiteralPath $startupC -Raw
    foreach ($api in @('pet_asset_startup_service_apply', 'snapshot',
                         'capture_gateway_lease', 'generation_admitted',
                         'download', 'install_full', 'cache_in_background',
                         'clear_applied', 'finish_generation')) {
        if ($startupHeader -notmatch ("\b$api\b")) {
            $failures += "pet asset startup header missing $api"
        }
    }
    if ($startupHeader -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_)\b') {
        $failures += 'pet asset startup public contract leaked platform/RTOS/allocator/crypto detail'
    }
    if ($startupSource -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport)\b') {
        $failures += 'pet asset startup service absorbed HTTP/crypto/allocator/RTOS/renderer/transport ownership'
    }
}

if ((Test-Path -LiteralPath $applyH) -and (Test-Path -LiteralPath $applyC)) {
    $applyHeader = Get-Content -LiteralPath $applyH -Raw
    $applySource = Get-Content -LiteralPath $applyC -Raw
    if ($applyHeader -notmatch '(?s)pet_asset_apply_service_clear\s*\(\s*pet_asset_apply_service_admitted_fn\s+admitted') {
        $failures += 'pet asset apply clear must accept a late-admission probe'
    }
    if ($applySource -notmatch '(?s)pet_asset_apply_service_clear\s*\([^)]*\).*?xSemaphoreTake.*?if\s*\(\s*admitted\s*&&\s*!admitted\s*\(\s*admission_context\s*\)\s*\)') {
        $failures += 'pet asset apply clear must evaluate late admission after acquiring renderer mutex'
    }
}

if ((Test-Path -LiteralPath $cacheCoordinatorC) -and (Test-Path -LiteralPath $cacheCoordinatorH)) {
    $cacheCoordinatorSource = Get-Content -LiteralPath $cacheCoordinatorC -Raw
    if ($cacheCoordinatorSource -notmatch '(?s)if\s*\(\s*job->operation\s*==\s*PET_CACHE_CLEAR\s*\).*?job_cancelled\s*\(\s*job\s*\).*?pet_asset_cache_storage_clear') {
        $failures += 'pet cache clear must revalidate cancellation immediately before retained files are removed'
    }
}

if ((Test-Path -LiteralPath $restoreH) -and (Test-Path -LiteralPath $restoreC)) {
    $restoreHeader = Get-Content -LiteralPath $restoreH -Raw
    $restoreSource = Get-Content -LiteralPath $restoreC -Raw
    foreach ($api in @('pet_asset_restore_service_restore', 'read_descriptor',
                         'load_verified_frame', 'install_full', 'release_frames',
                         'clear_cache', 'apply_cached_profile')) {
        if ($restoreHeader -notmatch ("\b$api\b")) {
            $failures += "pet asset restore header missing $api"
        }
    }
    if ($restoreHeader -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_)\b') {
        $failures += 'pet asset restore public contract leaked platform/RTOS/allocator/crypto detail'
    }
    if ($restoreSource -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport)\b') {
        $failures += 'pet asset restore service absorbed HTTP/crypto/allocator/RTOS/renderer/transport ownership'
    }
}

if ((Test-Path -LiteralPath $restoreWorkerH) -and (Test-Path -LiteralPath $restoreWorkerC)) {
    $restoreWorkerHeader = Get-Content -LiteralPath $restoreWorkerH -Raw
    $restoreWorkerSource = Get-Content -LiteralPath $restoreWorkerC -Raw
    foreach ($api in @('pet_asset_restore_worker_service_run', 'run_restore')) {
        if ($restoreWorkerHeader -notmatch ("\b$api\b")) {
            $failures += "pet asset restore worker header missing $api"
        }
    }
    if ($restoreWorkerHeader -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|gateway_)\b') {
        $failures += 'pet asset restore worker public contract leaked platform/RTOS/allocator/crypto/gateway detail'
    }
    if ($restoreWorkerSource -notmatch 'xTaskCreatePinnedToCoreWithCaps' -or
        $restoreWorkerSource -notmatch 'xSemaphoreTake') {
        $failures += 'pet asset restore worker must own bounded task creation and join'
    }
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
    $downloadExe = Join-Path $outDir 'test_pet_asset_download_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror -ffunction-sections -fdata-sections `
        "-I$cjsonDir" "-I$(Join-Path $projectRoot 'main')" `
        $downloadTestC $downloadC $serviceC $cjsonC `
        '-Wl,--gc-sections' -o $downloadExe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host pet asset download service compile failed (exit $LASTEXITCODE)"
    } else {
        & $downloadExe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host pet asset download service test failed (exit $LASTEXITCODE)"
        }
    }
    $runtimeExe = Join-Path $outDir 'test_pet_asset_runtime_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $runtimeTestC $runtimeC -o $runtimeExe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host pet asset runtime service compile failed (exit $LASTEXITCODE)"
    } else {
        & $runtimeExe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host pet asset runtime service test failed (exit $LASTEXITCODE)"
        }
    }
    $admissionExe = Join-Path $outDir 'test_startup_pet_asset_admission_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $admissionTestC $admissionC -o $admissionExe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host startup pet admission service compile failed (exit $LASTEXITCODE)"
    } else {
        & $admissionExe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host startup pet admission service test failed (exit $LASTEXITCODE)"
        }
    }
    $profileExe = Join-Path $outDir 'test_pet_asset_profile_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $profileTestC $profileC -o $profileExe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host pet asset profile service compile failed (exit $LASTEXITCODE)"
    } else {
        & $profileExe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host pet asset profile service test failed (exit $LASTEXITCODE)"
        }
    }
    $sleepExe = Join-Path $outDir 'test_startup_pet_asset_sleep_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $sleepTestC $sleepC -o $sleepExe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host startup pet sleep service compile failed (exit $LASTEXITCODE)"
    } else {
        & $sleepExe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host startup pet sleep service test failed (exit $LASTEXITCODE)"
        }
    }
    $startupExe = Join-Path $outDir 'test_pet_asset_startup_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $startupTestC $startupC -o $startupExe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host pet asset startup service compile failed (exit $LASTEXITCODE)"
    } else {
        & $startupExe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host pet asset startup service test failed (exit $LASTEXITCODE)"
        }
    }
    $restoreExe = Join-Path $outDir 'test_pet_asset_restore_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $restoreTestC $restoreC -o $restoreExe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host pet asset restore service compile failed (exit $LASTEXITCODE)"
    } else {
        & $restoreExe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host pet asset restore service test failed (exit $LASTEXITCODE)"
        }
    }
    $restoreWorkerExe = Join-Path $outDir 'test_pet_asset_restore_worker_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $restoreWorkerTestC -o $restoreWorkerExe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host pet asset restore worker value-contract compile failed (exit $LASTEXITCODE)"
    } else {
        & $restoreWorkerExe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host pet asset restore worker value-contract test failed (exit $LASTEXITCODE)"
        }
    }
    $retryExe = Join-Path $outDir 'test_pet_asset_retry_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror `
        "-I$(Join-Path $projectRoot 'main')" $retryTestC $retryC -o $retryExe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host pet asset retry service compile failed (exit $LASTEXITCODE)"
    } else {
        & $retryExe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host pet asset retry service test failed (exit $LASTEXITCODE)"
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
