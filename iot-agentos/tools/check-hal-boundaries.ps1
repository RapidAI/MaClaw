[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

# These headers are the shared, hardware-neutral contracts.  Board ports and
# profile-private adapters intentionally sit below this list, so they may use
# ESP-IDF while translating the contract to real hardware.
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$headers = @(
    'main/device_api.h',
    'main/board_profile.h',
    'main/app_ui.h',
    'main/display_service.h',
    'main/audio_service.h',
    'main/platform_audio.h',
    'main/platform_bootstrap.h',
    'main/platform_connectivity.h',
    'main/platform_display.h',
    'main/platform_input.h',
    'main/platform_lifecycle.h',
    'main/platform_power.h',
    'main/platform_sensor.h',
    'main/platform_storage.h',
    'main/fault_domain.h',
    'main/services/pet_asset_service.h',
    'main/services/pet_asset_integrity_service.h',
    'main/services/pet_asset_download_service.h',
    'main/services/pet_asset_apply_service.h',
    'main/services/pet_asset_runtime_service.h',
    'main/services/pet_asset_profile_service.h',
    'main/services/startup_welcome_service.h',
    'main/services/startup_runtime_state_service.h',
    'main/services/pet_asset_startup_service.h',
    'main/services/pet_asset_restore_service.h',
    'main/services/pet_asset_restore_worker_service.h',
    'main/services/startup_pet_asset_admission_service.h',
    'main/services/startup_pet_asset_sleep_service.h',
    'main/services/pet_asset_retry_service.h',
    'main/services/startup_pet_asset_state_service.h',
    'main/services/pet_cache_service.h',
    'main/services/startup_pet_retry_service.h',
    'main/services/wifi_startup_service.h',
    'main/services/provisioning_qr_service.h',
    'main/services/server_audio_presentation_service.h',
    'main/services/connectivity_network_lifecycle_service.h',
    'main/services/configuration_persistence_worker_service.h',
    'main/services/gateway_capability_projection.h',
    'main/alarm_wake_plan.h'
)

# Keep the check deliberately structural: it guards SDK/RTOS/driver object
# leakage, not ordinary words in comments.  Add a new hardware-neutral value
# type to device_api.h instead of allowing a profile implementation type here.
$forbidden = @(
    @{ Name = 'ESP-IDF/FreeRTOS/driver include'; Pattern = '#include\s*[<\"](?:esp_|freertos/|driver/|lwip/|soc/|hal/)' },
    @{ Name = 'ESP-IDF error type'; Pattern = '\besp_err_t\b' },
    @{ Name = 'FreeRTOS handle'; Pattern = '\b(?:Task|Queue|Semaphore|EventGroup)Handle_t\b' },
    @{ Name = 'ESP timer handle'; Pattern = '\besp_timer_handle_t\b' },
    @{ Name = 'NVS handle'; Pattern = '\bnvs_handle_t\b' },
    @{ Name = 'JSON object pointer'; Pattern = '\bcJSON\s*\*' },
    @{ Name = 'driver port/pin type'; Pattern = '\b(?:gpio_num_t|i2c_port_t|i2s_port_t|uart_port_t)\b' }
)

$violations = @()
foreach ($relativePath in $headers) {
    $path = Join-Path $projectRoot $relativePath
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "${relativePath}: required HAL boundary header is missing"
        continue
    }
    $lineNumber = 0
    foreach ($line in Get-Content -LiteralPath $path) {
        $lineNumber++
        foreach ($rule in $forbidden) {
            if ($line -match $rule.Pattern) {
                $violations += "${relativePath}:${lineNumber}: $($rule.Name): $($line.Trim())"
            }
        }
    }
}

# Circular boards share one physical I2C bus, but that must not pull touch,
# PMIC or IMU ownership back into the Audio HAL.  Keep this narrow structural
# assertion next to the public-header gate: the selected peripheral adapter is
# a one-TU implementation detail, and Input must consume its normalized facts
# directly.  This makes a future board addition fail in CI if it reintroduces
# a convenient-but-wrong Audio facade for a non-audio controller.
$roundPeripheralSelector = 'main/boards/round_peripheral_adapter.h'
$roundDisplaySelector = 'main/boards/round_display_adapter.h'
$roundPeripheralOwner = 'main/round_peripheral_service.c'
$roundDisplayOwner = 'main/round_display_service.c'
$roundPeripheralLifecycle = 'main/round_peripheral_lifecycle.h'
$roundAudioLifecycle = 'main/round_audio_lifecycle.h'
$roundInputService = 'main/round_input_service.c'
$roundAudioHeader = 'main/round_audio_service.h'
$roundWakeService = 'main/round_wake_service.c'
$platformAudioFacade = 'main/platform_audio.c'
$platformAudioProfileSources = @(
    'main/platform_audio_compact.c',
    'main/platform_audio_round.c'
)
$platformPowerFacade = 'main/platform_power.c'
$platformPowerProfileSources = @(
    'main/platform_power_compact.c',
    'main/platform_power_round.c'
)
$platformSensorFacade = 'main/platform_sensor.c'
$platformSensorProfileSources = @(
    'main/platform_sensor_compact.c',
    'main/platform_sensor_round.c'
)
$platformDisplayFacade = 'main/platform_display.c'
$platformDisplayProfileSources = @(
    'main/platform_display_compact.c',
    'main/platform_display_round.c'
)
$platformInputFacade = 'main/platform_input.c'
$platformInputProfileSources = @(
    'main/platform_input_compact.c',
    'main/platform_input_round.c'
)
$platformBootstrapFacade = 'main/platform_bootstrap.c'
$platformBootstrapProfileSources = @(
    'main/platform_bootstrap_compact.c',
    'main/platform_bootstrap_round.c'
)
$platformConnectivityFacade = 'main/platform_connectivity.c'
$platformConnectivityProfileSources = @(
    'main/platform_connectivity_compact.c',
    'main/platform_connectivity_round.c'
)
$platformLifecycleFacade = 'main/platform_lifecycle.c'
$platformLifecycleProfileSources = @(
    'main/platform_lifecycle_compact.c',
    'main/platform_lifecycle_round.c'
)
$platformStorageFacade = 'main/platform_storage.c'
$platformStorageProfileSources = @(
    'main/platform_storage_compact.c',
    'main/platform_storage_round.c'
)
$allSourceFiles = Get-ChildItem -Path (Join-Path $projectRoot 'main') -Recurse -File -Include '*.c','*.cpp','*.h'
$allCFiles = $allSourceFiles | Where-Object { $_.Extension -in @('.c','.h') }

# A9 begins by moving the authenticated pet descriptor contract out of the
# composition root.  It must remain a value-only service seam: JSON parsing is
# private to the .c implementation, while callers only receive descriptor
# values.  Hardware/media transport, renderer allocation and FreeRTOS worker
# lifetime still belong to their existing lower layers until later increments.
$petAssetHeader = Join-Path $projectRoot 'main/services/pet_asset_service.h'
$petAssetSource = Join-Path $projectRoot 'main/services/pet_asset_service.c'
$petAssetApplyHeader = Join-Path $projectRoot 'main/services/pet_asset_apply_service.h'
$petAssetApplySource = Join-Path $projectRoot 'main/services/pet_asset_apply_service.c'
$petAssetDownloadHeader = Join-Path $projectRoot 'main/services/pet_asset_download_service.h'
$petAssetIntegrityHeader = Join-Path $projectRoot 'main/services/pet_asset_integrity_service.h'
$petAssetIntegritySource = Join-Path $projectRoot 'main/services/pet_asset_integrity_service.c'
$petAssetDownloadSource = Join-Path $projectRoot 'main/services/pet_asset_download_service.c'
$petAssetRuntimeHeader = Join-Path $projectRoot 'main/services/pet_asset_runtime_service.h'
$petAssetRuntimeSource = Join-Path $projectRoot 'main/services/pet_asset_runtime_service.c'
$petAssetProfileHeader = Join-Path $projectRoot 'main/services/pet_asset_profile_service.h'
$petAssetProfileSource = Join-Path $projectRoot 'main/services/pet_asset_profile_service.c'
$petAssetStartupHeader = Join-Path $projectRoot 'main/services/pet_asset_startup_service.h'
$petAssetStartupSource = Join-Path $projectRoot 'main/services/pet_asset_startup_service.c'
$startupPetAdmissionHeader = Join-Path $projectRoot 'main/services/startup_pet_asset_admission_service.h'
$startupPetAdmissionSource = Join-Path $projectRoot 'main/services/startup_pet_asset_admission_service.c'
$startupPetSleepHeader = Join-Path $projectRoot 'main/services/startup_pet_asset_sleep_service.h'
$startupPetSleepSource = Join-Path $projectRoot 'main/services/startup_pet_asset_sleep_service.c'
$petAssetRestoreHeader = Join-Path $projectRoot 'main/services/pet_asset_restore_service.h'
$petAssetRestoreSource = Join-Path $projectRoot 'main/services/pet_asset_restore_service.c'
$petAssetRestoreWorkerHeader = Join-Path $projectRoot 'main/services/pet_asset_restore_worker_service.h'
$petAssetRestoreWorkerSource = Join-Path $projectRoot 'main/services/pet_asset_restore_worker_service.c'
$petAssetRetryHeader = Join-Path $projectRoot 'main/services/pet_asset_retry_service.h'
$petAssetRetrySource = Join-Path $projectRoot 'main/services/pet_asset_retry_service.c'
$startupPetAssetStateHeader = Join-Path $projectRoot 'main/services/startup_pet_asset_state_service.h'
$startupPetAssetStateSource = Join-Path $projectRoot 'main/services/startup_pet_asset_state_service.c'
$petAssetCacheHeader = Join-Path $projectRoot 'main/pet_asset_cache_storage.h'
$petAssetCacheSource = Join-Path $projectRoot 'main/pet_asset_cache_storage.c'
$meetingRecordingStorageHeader = Join-Path $projectRoot 'main/meeting_recording_storage.h'
$meetingRecordingStorageSource = Join-Path $projectRoot 'main/meeting_recording_storage.c'
$meetingServiceHeader = Join-Path $projectRoot 'main/services/meeting_service.h'
$meetingServiceSource = Join-Path $projectRoot 'main/services/meeting_service.c'
if (-not (Test-Path -LiteralPath $petAssetHeader) -or
    -not (Test-Path -LiteralPath $petAssetSource)) {
    $violations += 'main/services/pet_asset_service.[ch]: A9 pet descriptor service is missing'
} else {
    $petAssetHeaderText = Get-Content -LiteralPath $petAssetHeader -Raw
    $petAssetSourceText = Get-Content -LiteralPath $petAssetSource -Raw
    foreach ($petAssetRequirement in @(
            'typedef\s+struct\s*\{[\s\S]*?\}\s*pet_asset_descriptor_t\s*;',
            'typedef\s+struct\s*\{[\s\S]*?\}\s*pet_asset_memory_requirements_t\s*;',
            'bool\s+pet_asset_service_parse_hub_descriptor\s*\(\s*const\s+void\s*\*',
            'bool\s+pet_asset_service_frame_bytes\s*\(',
            'bool\s+pet_asset_service_calculate_memory_requirements\s*\(',
            'bool\s+pet_asset_service_format_cache_metadata\s*\(',
            'bool\s+pet_asset_service_parse_cache_metadata\s*\(',
            'bool\s+pet_asset_service_sha256_matches_hex\s*\(',
            'bool\s+pet_asset_service_limit_frame_count\s*\(',
            'bool\s+pet_asset_service_next_memory_fallback\s*\(')) {
        if ($petAssetHeaderText -notmatch $petAssetRequirement) {
            $violations += "main/services/pet_asset_service.h: A9 descriptor contract is incomplete (${petAssetRequirement})"
        }
    }
    if ($petAssetHeaderText -match '\bcJSON\b|\b(?:esp_|freertos/|driver/|board_port|CONFIG_MACLAW_BOARD_|TaskHandle_t|SemaphoreHandle_t)\b') {
        $violations += 'main/services/pet_asset_service.h: descriptor contract must remain value-only and hardware/JSON/RTOS-neutral'
    }
    if ($petAssetSourceText -notmatch '#include\s*[<"]cJSON\.h[>"]' -or
        $petAssetSourceText -notmatch '\bcJSON_IsObject\s*\(') {
        $violations += 'main/services/pet_asset_service.c: descriptor parsing must keep cJSON private to the implementation'
    }
    if ($petAssetSourceText -notmatch 'MACLAW_PET_V2' -or
        $petAssetSourceText -notmatch 'pet_asset_service_format_cache_metadata' -or
        $petAssetSourceText -notmatch 'pet_asset_service_parse_cache_metadata') {
        $violations += 'main/services/pet_asset_service.c: cache metadata format/validation must remain co-located with the descriptor contract'
    }
    if ($petAssetSourceText -notmatch 'pet_asset_service_sha256_matches_hex' -or
        $petAssetSourceText -match '\b(?:psa_hash_compute|mbedtls_|esp_crypto_)\b') {
        $violations += 'main/services/pet_asset_service.c: pet digest representation may be shared, but cryptographic-provider ownership must remain outside the service'
    }
    if ($petAssetSourceText -notmatch 'pet_asset_service_limit_frame_count' -or
        $petAssetSourceText -match '\b(?:device_display_get_pet_asset_install_budget|scene_presenter_set_pet_asset|board_port)\b') {
        $violations += 'main/services/pet_asset_service.c: frame-count adaptation may be shared, but Display HAL query/install ownership must remain outside the service'
    }
    if ($petAssetSourceText -notmatch 'pet_asset_service_next_memory_fallback' -or
        $petAssetSourceText -match '\b(?:heap_caps|scene_presenter_set_pet_asset|ESP_ERR_NO_MEM)\b') {
        $violations += 'main/services/pet_asset_service.c: memory-pressure fallback selection must remain value-only and outside allocator/renderer/error ownership'
    }
    if ($petAssetSourceText -notmatch 'pet_asset_service_calculate_memory_requirements' -or
        $petAssetSourceText -match '\b(?:device_resource_pressure|device_display_get_pet_asset_install_budget|heap_caps)\b') {
        $violations += 'main/services/pet_asset_service.c: asset-memory arithmetic must remain value-only and outside pressure/Display HAL/allocator ownership'
    }
    if ($petAssetSourceText -match '\b(?:board_port|device_display|heap_caps|xTask|SemaphoreHandle_t|esp_http_client|esp_sleep|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_service.c: descriptor service must not absorb hardware, HTTP, allocator, RTOS, or board policy'
    }
}

# Cache restore has the same boundary rule as runtime installation: the
# coordinator owns validation/terminal cleanup, while physical storage, SHA,
# allocation, renderer and boot-task mechanics remain host-owned.
if (-not (Test-Path -LiteralPath $petAssetRestoreHeader) -or
    -not (Test-Path -LiteralPath $petAssetRestoreSource)) {
    $violations += 'main/services/pet_asset_restore_service.[ch]: cache restore transaction service is missing'
} else {
    $petAssetRestoreHeaderText = Get-Content -LiteralPath $petAssetRestoreHeader -Raw
    $petAssetRestoreSourceText = Get-Content -LiteralPath $petAssetRestoreSource -Raw
    foreach ($petRestoreRequirement in @(
            'pet_asset_restore_service_host_t',
            'read_descriptor',
            'load_verified_frame',
            'install_full',
            'release_frames',
            'clear_cache',
            'apply_cached_profile',
            'pet_asset_restore_service_restore\s*\(')) {
        if ($petAssetRestoreHeaderText -notmatch $petRestoreRequirement -or
            $petAssetRestoreSourceText -notmatch $petRestoreRequirement) {
            $violations += "main/services/pet_asset_restore_service.[ch]: cache restore transaction is incomplete (${petRestoreRequirement})"
        }
    }
    if ($petAssetRestoreHeaderText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_restore_service.h: public restore contract must remain value-only and hardware/RTOS/JSON/crypto-neutral'
    }
    if ($petAssetRestoreSourceText -notmatch 'load_verified_frame\s*\(' -or
        $petAssetRestoreSourceText -notmatch 'release_frames\s*\(' -or
        $petAssetRestoreSourceText -notmatch 'clear_cache\s*\(') {
        $violations += 'main/services/pet_asset_restore_service.c: must own ordered verification, terminal release, and invalid-cache cleanup'
    }
    if ($petAssetRestoreSourceText -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_restore_service.c: restore transaction must not absorb HTTP, crypto, allocator, RTOS, Display, transport, or board ownership'
    }
}

# The bounded boot worker owns only task/join mechanics. Its public host seam
# remains value-only, while the private source is the sole FreeRTOS owner.
if (-not (Test-Path -LiteralPath $petAssetRestoreWorkerHeader) -or
    -not (Test-Path -LiteralPath $petAssetRestoreWorkerSource)) {
    $violations += 'main/services/pet_asset_restore_worker_service.[ch]: bounded restore worker is missing'
} else {
    $petAssetRestoreWorkerHeaderText = Get-Content -LiteralPath $petAssetRestoreWorkerHeader -Raw
    $petAssetRestoreWorkerSourceText = Get-Content -LiteralPath $petAssetRestoreWorkerSource -Raw
    foreach ($petRestoreWorkerRequirement in @(
            'pet_asset_restore_worker_service_host_t',
            'run_restore',
            'pet_asset_restore_worker_service_run\s*\(',
            'xTaskCreatePinnedToCoreWithCaps',
            'xSemaphoreTake')) {
        if ($petAssetRestoreWorkerHeaderText -notmatch $petRestoreWorkerRequirement -and
            $petAssetRestoreWorkerSourceText -notmatch $petRestoreWorkerRequirement) {
            $violations += "main/services/pet_asset_restore_worker_service.[ch]: bounded restore worker is incomplete (${petRestoreWorkerRequirement})"
        }
    }
    if ($petAssetRestoreWorkerHeaderText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_restore_worker_service.h: public restore-worker contract must remain value-only and hardware/RTOS-neutral'
    }
    $restoreWorkerRootText = Get-Content -LiteralPath (Join-Path $projectRoot 'main\main.c') -Raw
    if ($restoreWorkerRootText -match '\b(?:cached_pet_restore_task|start_cached_pet_restore_task)\b' -or
        $restoreWorkerRootText -notmatch 'pet_asset_restore_worker_service_run\s*\(') {
        $violations += 'main/main.c: cached pet restore FreeRTOS lifecycle must be owned by pet_asset_restore_worker_service'
    }
}

# Runtime pet-profile orchestration is policy, not a composition-root state
# machine. Physical Gateway/HTTP/PSA/media/Display/Storage owners remain host
# callbacks, and the shared coordinator remains value-only.
if (-not (Test-Path -LiteralPath $petAssetRuntimeHeader) -or
    -not (Test-Path -LiteralPath $petAssetRuntimeSource)) {
    $violations += 'main/services/pet_asset_runtime_service.[ch]: runtime pet transaction service is missing'
} else {
    $petAssetRuntimeHeaderText = Get-Content -LiteralPath $petAssetRuntimeHeader -Raw
    $petAssetRuntimeSourceText = Get-Content -LiteralPath $petAssetRuntimeSource -Raw
    foreach ($petRuntimeRequirement in @(
            'pet_asset_runtime_service_host_t',
            'capture_gateway_lease',
            'capacity_available',
            'drop_stale_cache',
            'download',
            'install_full',
            'cache_in_background',
            'release_frames',
            'pet_asset_runtime_service_apply\s*\(')) {
        if ($petAssetRuntimeHeaderText -notmatch $petRuntimeRequirement -or
            $petAssetRuntimeSourceText -notmatch $petRuntimeRequirement) {
            $violations += "main/services/pet_asset_runtime_service.[ch]: runtime transaction is incomplete (${petRuntimeRequirement})"
        }
    }
    if ($petAssetRuntimeHeaderText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_runtime_service.h: public runtime contract must remain value-only and hardware/RTOS/JSON/crypto-neutral'
    }
    if ($petAssetRuntimeSourceText -notmatch 'gateway_lease_current\s*\(' -or
        $petAssetRuntimeSourceText -notmatch 'drop_stale_cache\s*\(' -or
        $petAssetRuntimeSourceText -notmatch 'release_frames\s*\(') {
        $violations += 'main/services/pet_asset_runtime_service.c: must own lease fence, stale-cache retry, and terminal frame release'
    }
    if ($petAssetRuntimeSourceText -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_runtime_service.c: runtime transaction must not absorb HTTP, crypto, allocator, RTOS, Display, transport, or board ownership'
    }
}

# Gateway pet-profile update order is business policy. Its coordinator decides
# latest-wins startup supersession and failed-ACK retry classification while the
# root retains JSON, Display, HTTP, Gateway and Storage physical ownership.
if (-not (Test-Path -LiteralPath $petAssetProfileHeader) -or
    -not (Test-Path -LiteralPath $petAssetProfileSource)) {
    $violations += 'main/services/pet_asset_profile_service.[ch]: runtime pet profile service is missing'
} else {
    $petAssetProfileHeaderText = Get-Content -LiteralPath $petAssetProfileHeader -Raw
    $petAssetProfileSourceText = Get-Content -LiteralPath $petAssetProfileSource -Raw
    foreach ($petProfileRequirement in @(
            'pet_asset_profile_service_host_t',
            'startup_profile_matches',
            'set_startup_pending',
            'apply_asset',
            'clear_asset',
            'note_transient_failure',
            'retry_exhausted',
            'pet_asset_profile_service_apply\s*\(')) {
        if ($petAssetProfileHeaderText -notmatch $petProfileRequirement -or
            $petAssetProfileSourceText -notmatch $petProfileRequirement) {
            $violations += "main/services/pet_asset_profile_service.[ch]: profile transaction is incomplete (${petProfileRequirement})"
        }
    }
    if ($petAssetProfileHeaderText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_profile_service.h: public profile contract must remain value-only and hardware/RTOS/JSON/crypto-neutral'
    }
    if ($petAssetProfileSourceText -notmatch 'set_startup_pending\s*\(' -or
        $petAssetProfileSourceText -notmatch 'retry_exhausted\s*\(' -or
        $petAssetProfileSourceText -notmatch 'reset_retries\s*\(') {
        $violations += 'main/services/pet_asset_profile_service.c: must own latest-wins, retry exhaustion, and retry reset ordering'
    }
    if ($petAssetProfileSourceText -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_profile_service.c: profile transaction must not absorb HTTP, crypto, allocator, RTOS, Display, transport, or board ownership'
    }
}

# Cold-start pet orchestration is likewise business ordering. The service must
# complete only its captured generation, while the root retains descriptor
# state, HTTP/PSA/media, Display, Storage and worker ownership behind callbacks.
if (-not (Test-Path -LiteralPath $petAssetStartupHeader) -or
    -not (Test-Path -LiteralPath $petAssetStartupSource)) {
    $violations += 'main/services/pet_asset_startup_service.[ch]: startup pet transaction service is missing'
} else {
    $petAssetStartupHeaderText = Get-Content -LiteralPath $petAssetStartupHeader -Raw
    $petAssetStartupSourceText = Get-Content -LiteralPath $petAssetStartupSource -Raw
    foreach ($petStartupRequirement in @(
            'pet_asset_startup_service_host_t',
            'snapshot',
            'generation_admitted',
            'capture_gateway_lease',
            'download',
            'install_full',
            'cache_in_background',
            'release_frames',
            'finish_generation',
            'pet_asset_startup_service_apply\s*\(')) {
        if ($petAssetStartupHeaderText -notmatch $petStartupRequirement -or
            $petAssetStartupSourceText -notmatch $petStartupRequirement) {
            $violations += "main/services/pet_asset_startup_service.[ch]: startup transaction is incomplete (${petStartupRequirement})"
        }
    }
    if ($petAssetStartupHeaderText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_startup_service.h: public startup contract must remain value-only and hardware/RTOS/JSON/crypto-neutral'
    }
    if ($petAssetStartupSourceText -notmatch 'generation_admitted\s*\(' -or
        $petAssetStartupSourceText -notmatch 'finish_generation\s*\(' -or
        $petAssetStartupSourceText -notmatch 'release_frames\s*\(') {
        $violations += 'main/services/pet_asset_startup_service.c: must own generation fence, terminal completion, and frame release'
    }
    if ($petAssetStartupSourceText -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_startup_service.c: startup transaction must not absorb HTTP, crypto, allocator, RTOS, Display, transport, or board ownership'
    }
}

# Startup descriptor admission is business policy. It may decide capacity
# backoff, worker admission and audio re-arm, but physical timer/worker,
# Gateway, Display, Storage and media ownership remain host callbacks.
if (-not (Test-Path -LiteralPath $startupPetAdmissionHeader) -or
    -not (Test-Path -LiteralPath $startupPetAdmissionSource)) {
    $violations += 'main/services/startup_pet_asset_admission_service.[ch]: startup pet admission service is missing'
} else {
    $startupPetAdmissionHeaderText = Get-Content -LiteralPath $startupPetAdmissionHeader -Raw
    $startupPetAdmissionSourceText = Get-Content -LiteralPath $startupPetAdmissionSource -Raw
    foreach ($startupPetAdmissionRequirement in @(
            'startup_pet_asset_admission_service_host_t',
            'capacity_available',
            'drop_stale_cache',
            'take_capacity_retry',
            'schedule_retry',
            'start_worker',
            'startup_pet_asset_admission_service_admit_pending\s*\(',
            'startup_pet_asset_admission_service_rearm_preempted\s*\(')) {
        if ($startupPetAdmissionHeaderText -notmatch $startupPetAdmissionRequirement -or
            $startupPetAdmissionSourceText -notmatch $startupPetAdmissionRequirement) {
            $violations += "main/services/startup_pet_asset_admission_service.[ch]: admission transaction is incomplete (${startupPetAdmissionRequirement})"
        }
    }
    if ($startupPetAdmissionHeaderText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/startup_pet_asset_admission_service.h: public admission contract must remain value-only and hardware/RTOS/JSON/crypto-neutral'
    }
    if ($startupPetAdmissionSourceText -notmatch 'return_capacity_retry\s*\(' -or
        $startupPetAdmissionSourceText -notmatch 'finish_generation\s*\(' -or
        $startupPetAdmissionSourceText -notmatch 'set_pending\s*\(') {
        $violations += 'main/services/startup_pet_asset_admission_service.c: must own retry reservation cleanup, generation finish, and re-arm reversal'
    }
    if ($startupPetAdmissionSourceText -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/startup_pet_asset_admission_service.c: admission service must not absorb HTTP, crypto, allocator, RTOS, Display, transport, or board ownership'
    }
}

# The startup-pet System Sleep participant owns only reversible service order
# and its shared deadline. State, retry timer, cache worker and startup worker
# retain their own physical lifetimes behind the value-only host callbacks.
if (-not (Test-Path -LiteralPath $startupPetSleepHeader) -or
    -not (Test-Path -LiteralPath $startupPetSleepSource)) {
    $violations += 'main/services/startup_pet_asset_sleep_service.[ch]: startup pet System Sleep coordinator is missing'
} else {
    $startupPetSleepHeaderText = Get-Content -LiteralPath $startupPetSleepHeader -Raw
    $startupPetSleepSourceText = Get-Content -LiteralPath $startupPetSleepSource -Raw
    foreach ($startupPetSleepRequirement in @(
            'startup_pet_asset_sleep_service_host_t',
            'prepare_state',
            'prepare_worker',
            'prepare_retry',
            'prepare_cache',
            'abort_state',
            'abort_worker',
            'abort_retry',
            'abort_cache',
            'startup_pet_asset_sleep_service_prepare\s*\(',
            'startup_pet_asset_sleep_service_abort\s*\(')) {
        if ($startupPetSleepHeaderText -notmatch $startupPetSleepRequirement -or
            $startupPetSleepSourceText -notmatch $startupPetSleepRequirement) {
            $violations += "main/services/startup_pet_asset_sleep_service.[ch]: System Sleep transaction is incomplete (${startupPetSleepRequirement})"
        }
    }
    if ($startupPetSleepHeaderText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/startup_pet_asset_sleep_service.h: public sleep contract must remain value-only and hardware/RTOS/JSON/crypto-neutral'
    }
    if ($startupPetSleepSourceText -notmatch 'remaining_timeout_ms\s*\(' -or
        $startupPetSleepSourceText -notmatch 'abort_cache\s*\(' -or
        $startupPetSleepSourceText -notmatch 'abort_retry\s*\(' -or
        $startupPetSleepSourceText -notmatch 'abort_worker\s*\(') {
        $violations += 'main/services/startup_pet_asset_sleep_service.c: must own shared deadline and reverse participant rollback ordering'
    }
    if ($startupPetSleepSourceText -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/startup_pet_asset_sleep_service.c: sleep coordinator must not absorb HTTP, crypto, allocator, RTOS, Display, transport, or board ownership'
    }
}

# The ordered downlink retry counter is message/cursor policy, not a root
# global. Keep its state value-only; Gateway Dispatcher owns the actual page
# cursor and ACK, while HTTP, JSON and task ownership remain outside it.
if (-not (Test-Path -LiteralPath $petAssetRetryHeader) -or
    -not (Test-Path -LiteralPath $petAssetRetrySource)) {
    $violations += 'main/services/pet_asset_retry_service.[ch]: pet ordered-retry value service is missing'
} else {
    $petAssetRetryHeaderText = Get-Content -LiteralPath $petAssetRetryHeader -Raw
    $petAssetRetrySourceText = Get-Content -LiteralPath $petAssetRetrySource -Raw
    foreach ($petRetryRequirement in @(
            'pet_asset_retry_service_init\s*\(',
            'pet_asset_retry_service_reset\s*\(',
            'pet_asset_retry_service_note_failure\s*\(',
            'pet_asset_retry_service_exhausted\s*\(')) {
        if ($petAssetRetryHeaderText -notmatch $petRetryRequirement -or
            $petAssetRetrySourceText -notmatch $petRetryRequirement) {
            $violations += "main/services/pet_asset_retry_service.[ch]: ordered retry contract is incomplete (${petRetryRequirement})"
        }
    }
    if ($petAssetRetryHeaderText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|gateway_)\b' -or
        $petAssetRetrySourceText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|gateway_)\b') {
        $violations += 'main/services/pet_asset_retry_service.[ch]: ordered retry state must remain value-only and free of platform/RTOS/JSON/crypto/Gateway ownership'
    }
}

# Startup pet latest-wins state is a separate synchronized value boundary.
# It may use a private FreeRTOS mutex, but must not pull HTTP, JSON, crypto,
# renderer, gateway or board mechanics out of the composition root.
if (-not (Test-Path -LiteralPath $startupPetAssetStateHeader) -or
    -not (Test-Path -LiteralPath $startupPetAssetStateSource)) {
    $violations += 'main/services/startup_pet_asset_state_service.[ch]: startup pet state service is missing'
} else {
    $startupPetAssetStateHeaderText = Get-Content -LiteralPath $startupPetAssetStateHeader -Raw
    $startupPetAssetStateSourceText = Get-Content -LiteralPath $startupPetAssetStateSource -Raw
    foreach ($startupPetStateRequirement in @(
            'startup_pet_asset_state_service_init\s*\(',
            'startup_pet_asset_state_service_record\s*\(',
            'startup_pet_asset_state_service_snapshot\s*\(',
            'startup_pet_asset_state_service_pending_generation\s*\(',
            'startup_pet_asset_state_service_take_capacity_retry\s*\(',
            'startup_pet_asset_state_service_return_capacity_retry\s*\(',
            'startup_pet_asset_state_service_prepare_system_sleep\s*\(',
            'startup_pet_asset_state_service_abort_system_sleep_prepare\s*\(',
            'startup_pet_asset_state_service_system_sleep_preparing\s*\(',
            'startup_pet_asset_state_service_preempt_for_audio\s*\(',
            'startup_pet_asset_state_service_finish_generation\s*\(',
            'startup_pet_asset_state_service_matches_profile\s*\(')) {
        if ($startupPetAssetStateHeaderText -notmatch $startupPetStateRequirement -or
            $startupPetAssetStateSourceText -notmatch $startupPetStateRequirement) {
            $violations += "main/services/startup_pet_asset_state_service.[ch]: state transition contract is incomplete (${startupPetStateRequirement})"
        }
    }
    if ($startupPetAssetStateHeaderText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|gateway_|scene_presenter|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/startup_pet_asset_state_service.h: public state contract must remain value-only and platform-neutral'
    }
    if ($startupPetAssetStateSourceText -notmatch 'next_generation\s*\(' -or
        $startupPetAssetStateSourceText -notmatch 's_state\.generation\s*==' -or
        $startupPetAssetStateSourceText -notmatch 's_capacity_retry_count' -or
        $startupPetAssetStateSourceText -notmatch 's_system_sleep_preparing' -or
        $startupPetAssetStateSourceText -notmatch 'xSemaphoreTake') {
        $violations += 'main/services/startup_pet_asset_state_service.c: generation-fenced, synchronized latest-wins state is incomplete'
    }
    if ($startupPetAssetStateSourceText -match '\b(?:esp_http_client|psa_hash|heap_caps|cJSON|scene_presenter|gateway_transport|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/startup_pet_asset_state_service.c: state service must not absorb HTTP, crypto, allocator, JSON, renderer, gateway, or board ownership'
    }
    if ($startupPetAssetStateSourceText -notmatch 'startup_pet_asset_state_service_prepare_system_sleep[\s\S]*?s_system_sleep_preparing\s*=\s*true' -or
        $startupPetAssetStateSourceText -notmatch 'startup_pet_asset_state_service_abort_system_sleep_prepare[\s\S]*?s_system_sleep_preparing\s*=\s*false') {
        $violations += 'main/services/startup_pet_asset_state_service.c: System Sleep descriptor-state rollback fence is incomplete'
    }
}

# A12's next increment moves descriptor traversal and bounded retry out of
# the composition root. HTTP, PSA, media lease, FreeRTOS wait and Display
# preview remain host callbacks, so the shared service contract cannot leak
# those physical owners back into main-independent business code.
if (-not (Test-Path -LiteralPath $petAssetDownloadHeader) -or
    -not (Test-Path -LiteralPath $petAssetDownloadSource)) {
    $violations += 'main/services/pet_asset_download_service.[ch]: pet download transaction service is missing'
} else {
    $petAssetDownloadHeaderText = Get-Content -LiteralPath $petAssetDownloadHeader -Raw
    $petAssetDownloadSourceText = Get-Content -LiteralPath $petAssetDownloadSource -Raw
    foreach ($petDownloadRequirement in @(
            'pet_asset_download_service_host_t',
            'transaction_admitted',
            'gateway_lease_current',
            'request_frame',
            'verify_frame_sha256',
            'release_frame',
            'wait_before_retry',
            'wait_before_pack_retry',
            'install_first_frame_preview',
            'pet_asset_download_service_fetch\s*\(',
            'pet_asset_download_service_fetch_startup_pack\s*\(')) {
        if ($petAssetDownloadHeaderText -notmatch $petDownloadRequirement -or
            $petAssetDownloadSourceText -notmatch $petDownloadRequirement) {
            $violations += "main/services/pet_asset_download_service.[ch]: download transaction is incomplete (${petDownloadRequirement})"
        }
    }
    if ($petAssetDownloadHeaderText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_download_service.h: public download contract must remain value-only and hardware/RTOS/JSON/crypto-neutral'
    }
    if ($petAssetDownloadSourceText -notmatch 'PET_ASSET_DOWNLOAD_STARTUP_ATTEMPTS' -or
        $petAssetDownloadSourceText -notmatch 'PET_ASSET_DOWNLOAD_STARTUP_PACK_ATTEMPTS' -or
        $petAssetDownloadSourceText -notmatch 'http_status\s*>=\s*400' -or
        $petAssetDownloadSourceText -notmatch 'startup_pack_retryable\s*\(' -or
        $petAssetDownloadSourceText -notmatch 'transaction_current\s*\(') {
        $violations += 'main/services/pet_asset_download_service.c: must own frame/pack bounded retries, permanent HTTP classification, and boundary admission checks'
    }
    if ($petAssetDownloadSourceText -match '\b(?:esp_http_client|psa_hash|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_download_service.c: download transaction must not absorb HTTP, crypto, allocator, RTOS, Display, transport, or board ownership'
    }
}

# A12's pet-application coordinator owns the applied-revision state and the
# Display serialization mutex.  The root may still own authenticated HTTP,
# SHA verification, capability admission and cache policy, but it must not
# grow a second renderer ownership state machine around this service.
if (-not (Test-Path -LiteralPath $petAssetApplyHeader) -or
    -not (Test-Path -LiteralPath $petAssetApplySource)) {
    $violations += 'main/services/pet_asset_apply_service.[ch]: pet display-application coordinator is missing'
} else {
    $petAssetApplyHeaderText = Get-Content -LiteralPath $petAssetApplyHeader -Raw
    $petAssetApplySourceText = Get-Content -LiteralPath $petAssetApplySource -Raw
    foreach ($petApplyRequirement in @(
            'pet_asset_apply_service_init\s*\(',
            'pet_asset_apply_service_free_frames\s*\(',
            'pet_asset_apply_service_revision_installed\s*\(',
            'pet_asset_apply_service_install_preview\s*\(',
            'pet_asset_apply_service_clear\s*\(',
            'pet_asset_apply_service_admitted_fn',
            'pet_asset_apply_service_install_full\s*\(')) {
        if ($petAssetApplyHeaderText -notmatch $petApplyRequirement -or
            $petAssetApplySourceText -notmatch $petApplyRequirement) {
            $violations += "main/services/pet_asset_apply_service.[ch]: application coordinator is incomplete (${petApplyRequirement})"
        }
    }
    if ($petAssetApplyHeaderText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_apply_service.h: public application contract must remain value-only and hardware/RTOS/JSON-neutral'
    }
    if ($petAssetApplySourceText -notmatch 'scene_presenter_set_pet_asset_consuming' -or
        $petAssetApplySourceText -notmatch 'pet_asset_service_next_memory_fallback' -or
        $petAssetApplySourceText -notmatch 's_loaded_revision' -or
        $petAssetApplySourceText -notmatch 'admitted\s*&&\s*!admitted') {
        $violations += 'main/services/pet_asset_apply_service.c: coordinator must own consuming install, fallback, applied revision and late-admission state'
    }
    if ($petAssetApplySourceText -match '\b(?:esp_http_client|psa_hash|cJSON|gateway_transport|pet_asset_cache_storage|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_apply_service.c: application coordinator must not absorb HTTP, crypto, JSON, cache, or board ownership'
    }
}

# A10 Storage: retained meeting WAV ownership is separate from recovery
# metadata, recorder business policy and transport.  The public seam must not
# expose VFS or profile/RTOS objects; the service gets bytes only through the
# opaque range reader carried in its transport request.
if (-not (Test-Path -LiteralPath $meetingRecordingStorageHeader) -or
    -not (Test-Path -LiteralPath $meetingRecordingStorageSource) -or
    -not (Test-Path -LiteralPath $meetingServiceHeader) -or
    -not (Test-Path -LiteralPath $meetingServiceSource)) {
    $violations += 'meeting recording Storage adapter or Meeting Service contract is missing'
} else {
    $meetingRecordingStorageHeaderText = Get-Content -LiteralPath $meetingRecordingStorageHeader -Raw
    $meetingRecordingStorageSourceText = Get-Content -LiteralPath $meetingRecordingStorageSource -Raw
    $meetingServiceHeaderText = Get-Content -LiteralPath $meetingServiceHeader -Raw
    $meetingServiceStorageText = Get-Content -LiteralPath $meetingServiceSource -Raw
    foreach ($meetingStorageRequirement in @(
            'meeting_recording_storage_create\s*\(',
            'meeting_recording_storage_append_pcm\s*\(',
            'meeting_recording_storage_finalize\s*\(',
            'meeting_recording_storage_open_for_upload\s*\(',
            'meeting_recording_storage_read_range\s*\(',
            'meeting_recording_storage_has_pending_audio\s*\(',
            'meeting_recording_storage_clear\s*\(')) {
        if ($meetingRecordingStorageHeaderText -notmatch $meetingStorageRequirement) {
            $violations += "main/meeting_recording_storage.h: recording storage contract is incomplete (${meetingStorageRequirement})"
        }
    }
    if ($meetingRecordingStorageHeaderText -cmatch '\b(?:esp_|freertos/|driver/|TaskHandle_t|SemaphoreHandle_t|heap_caps|FILE)\b') {
        $violations += 'main/meeting_recording_storage.h: recording storage contract must remain hardware/RTOS/allocator/VFS-handle neutral'
    }
    if ($meetingRecordingStorageSourceText -notmatch 'MEETING_RECORDING_PATH' -or
        $meetingRecordingStorageSourceText -notmatch 'fsync\(fileno\(file\)\)' -or
        $meetingRecordingStorageSourceText -notmatch 'validate_or_repair_header') {
        $violations += 'main/meeting_recording_storage.c: adapter must own retained object name, durability and WAV repair validation'
    }
    if ($meetingRecordingStorageSourceText -match '\b(?:heap_caps|scene_presenter|xTask|SemaphoreHandle_t|board_port|CONFIG_MACLAW_BOARD_|esp_)\b') {
        $violations += 'main/meeting_recording_storage.c: adapter must not absorb renderer/allocator/RTOS/board ownership'
    }
    $meetingRecoverySource = Join-Path $projectRoot 'main\meeting_recovery_service.c'
    if (-not (Test-Path -LiteralPath $meetingRecoverySource)) {
        $violations += 'main/meeting_recovery_service.c: recovery metadata owner is missing'
    } else {
        $meetingRecoveryText = Get-Content -LiteralPath $meetingRecoverySource -Raw
        foreach ($recoveryInvariant in @(
                'static\s+bool\s+valid_recovery_state\s*\(',
                'valid_recovery_state\(store->pending\s*!=\s*0',
                'valid_recovery_state\(snapshot->pending',
                '\*out_found\s*&&\s*!valid_snapshot\(snapshot\)')) {
            if ($meetingRecoveryText -notmatch $recoveryInvariant) {
                $violations += "main/meeting_recovery_service.c: recovery metadata invariant is incomplete (${recoveryInvariant})"
            }
        }
    }
    if ($meetingServiceHeaderText -cmatch '\b(?:FILE|fopen|fread|fwrite|fseek|esp_|freertos/|driver/)\b') {
        $violations += 'main/services/meeting_service.h: meeting transport contract must remain VFS/platform-handle neutral'
    }
    if ($meetingServiceStorageText -cmatch '\b(?:FILE|fopen|fclose|fread|fwrite|fseek|stat|unlink|MEETING_WAV_PATH)\b') {
        $violations += 'main/services/meeting_service.c: business service must route retained WAV access through the Storage adapter'
    }
    if ($meetingServiceStorageText -notmatch 'meeting_recording_storage_open_for_upload' -or
        $meetingServiceStorageText -notmatch 'meeting_recording_storage_append_pcm' -or
        $meetingServiceStorageText -notmatch 'meeting_recording_storage_finalize') {
        $violations += 'main/services/meeting_service.c: Storage adapter routing is incomplete'
    }
    $meetingAudioClear = $meetingServiceStorageText.IndexOf('meeting_recording_storage_clear()')
    $meetingMetadataClear = $meetingServiceStorageText.IndexOf('return save_meeting_recovery(false, "", 0, 0);', $meetingAudioClear)
    if ($meetingAudioClear -lt 0 -or $meetingMetadataClear -lt $meetingAudioClear) {
        $violations += 'main/services/meeting_service.c: terminal cleanup must retain recovery metadata until the retained WAV delete has succeeded'
    }
}

# Pet cache named-object/VFS transaction belongs to a Storage adapter rather
# than the A9 value descriptor service or main's business orchestration.  The
# public adapter boundary is callback/bytes only: it must not expose ESP-IDF,
# RTOS, allocator, renderer or board/display types.
if (-not (Test-Path -LiteralPath $petAssetCacheHeader) -or
    -not (Test-Path -LiteralPath $petAssetCacheSource)) {
    $violations += 'main/pet_asset_cache_storage.[ch]: pet cache Storage adapter is missing'
} else {
    $petAssetCacheHeaderText = Get-Content -LiteralPath $petAssetCacheHeader -Raw
    $petAssetCacheSourceText = Get-Content -LiteralPath $petAssetCacheSource -Raw
    foreach ($petAssetCacheRequirement in @(
            'bool\s+pet_asset_cache_storage_write\s*\(',
            'bool\s+pet_asset_cache_storage_read_descriptor\s*\(',
            'bool\s+pet_asset_cache_storage_read_frame\s*\(',
            'void\s+pet_asset_cache_storage_clear\s*\(',
            'bool\s+pet_asset_cache_storage_drop_if_stale\s*\(')) {
        if ($petAssetCacheHeaderText -notmatch $petAssetCacheRequirement) {
            $violations += "main/pet_asset_cache_storage.h: cache storage contract is incomplete (${petAssetCacheRequirement})"
        }
    }
    if ($petAssetCacheHeaderText -match '\b(?:esp_|freertos/|driver/|board_port|CONFIG_MACLAW_BOARD_|TaskHandle_t|SemaphoreHandle_t|heap_caps|FILE)\b') {
        $violations += 'main/pet_asset_cache_storage.h: cache storage contract must remain hardware/RTOS/allocator/VFS-handle neutral'
    }
    if ($petAssetCacheSourceText -notmatch 'PET_CACHE_META_PATH' -or
        $petAssetCacheSourceText -notmatch 'pet_asset_service_format_cache_metadata' -or
        $petAssetCacheSourceText -notmatch 'pet_asset_service_parse_cache_metadata' -or
        $petAssetCacheSourceText -notmatch 'fsync\(fileno\(file\)\)') {
        $violations += 'main/pet_asset_cache_storage.c: cache adapter must own exact named-object commit/read and durable metadata sequencing'
    }
    if ($petAssetCacheSourceText -match '\b(?:heap_caps|scene_presenter_set_pet_asset|xTask|SemaphoreHandle_t|board_port|CONFIG_MACLAW_BOARD_|esp_)\b') {
        $violations += 'main/pet_asset_cache_storage.c: cache adapter must not absorb renderer/allocator/RTOS/board ownership'
    }
}

# Platform Audio is the Device-to-physical-audio seam.  It may call only the
# private family Audio/Wake services; reintroducing a broad renderer facade
# here would turn a future profile into a second giant facade and defeat the split.
foreach ($relativePath in @($platformAudioFacade) + $platformAudioProfileSources) {
    $path = Join-Path $projectRoot $relativePath
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "${relativePath}: Platform Audio source is missing"
        continue
    }
    $audioText = Get-Content -LiteralPath $path -Raw
    if ($audioText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${relativePath}: Platform Audio must use private family Audio/Wake services, never board_port"
    }
}
# Platform Power mirrors the Audio split: the shared facade may bridge Power
# policy to Display Service, while telemetry must come from exactly one
# selected family peripheral service.  Reintroducing a broad renderer facade
# here would make battery/charger details leak back above the profile seam.
foreach ($relativePath in @($platformPowerFacade) + $platformPowerProfileSources) {
    $path = Join-Path $projectRoot $relativePath
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "${relativePath}: Platform Power source is missing"
        continue
    }
    $powerText = Get-Content -LiteralPath $path -Raw
    if ($powerText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${relativePath}: Platform Power must use Display Service and private family peripheral services, never board_port"
    }
}
# Platform Sensor follows the same private-family rule.  Motion Service sees
# a normalized sample/status only; the shared platform facade cannot climb
# back through a renderer facade merely because older renderers still publish it.
foreach ($relativePath in @($platformSensorFacade) + $platformSensorProfileSources) {
    $path = Join-Path $projectRoot $relativePath
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "${relativePath}: Platform Sensor source is missing"
        continue
    }
    $sensorText = Get-Content -LiteralPath $path -Raw
    if ($sensorText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${relativePath}: Platform Sensor must use private family peripheral services, never board_port"
    }
}
# Platform Display is now also a profile-family split.  Display Service and
# its selected bridges must not need the giant renderer facade: only the
# renderer source owner may reach legacy implementation, while a narrow scene seam carries
# the remaining synchronous scene calls during decomposition.
$platformDisplayFacadePath = Join-Path $projectRoot $platformDisplayFacade
if (-not (Test-Path -LiteralPath $platformDisplayFacadePath)) {
    $violations += "${platformDisplayFacade}: Platform Display facade is missing"
} else {
    $displayText = Get-Content -LiteralPath $platformDisplayFacadePath -Raw
    if ($displayText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${platformDisplayFacade}: Platform Display must use the selected profile bridge, never board_port"
    }
}
foreach ($relativePath in $platformDisplayProfileSources) {
    $path = Join-Path $projectRoot $relativePath
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "${relativePath}: Platform Display profile bridge is missing"
        continue
    }
    $displayProfileText = Get-Content -LiteralPath $path -Raw
    if ($displayProfileText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${relativePath}: selected Display profile bridge must use the legacy display scene seam, never board_port"
    }
    if ($displayProfileText -notmatch '#include\s*[<"]legacy_display_scene\.h[>"]' -or
        $displayProfileText -notmatch '\blegacy_display_scene_[A-Za-z0-9_]+\s*\(') {
        $violations += "${relativePath}: selected Display profile bridge must use the private legacy display scene seam"
    }
}
$legacyDisplaySceneHeader = 'main/legacy_display_scene.h'
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot $legacyDisplaySceneHeader))) {
    $violations += "${legacyDisplaySceneHeader}: private legacy display scene seam is missing"
} else {
    $legacyDisplaySceneReferences = @($allCFiles | Where-Object {
        Select-String -Path $_.FullName -Pattern 'legacy_display_scene\.h' -Quiet
    })
    $legacyDisplaySceneActual = @($legacyDisplaySceneReferences | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    } | Sort-Object -Unique)
    $legacyDisplaySceneExpected = @(
        'main/board_port.c',
        'main/compact_renderer.c',
        'main/platform_display_compact.c',
        'main/platform_display_round.c'
    )
    if (($legacyDisplaySceneActual -join '|') -ne ($legacyDisplaySceneExpected -join '|')) {
        $violations += "${legacyDisplaySceneHeader}: may be included only by $($legacyDisplaySceneExpected -join ', '); found: $($legacyDisplaySceneActual -join ', ')"
    }
}
# Platform Bootstrap owns the one-time renderer/peripheral transaction.  Its
# public facade is hardware neutral; the selected profile bridge reaches the
# remaining renderer construction through a narrow private seam, never a
# broad renderer facade.
$platformBootstrapFacadePath = Join-Path $projectRoot $platformBootstrapFacade
if (-not (Test-Path -LiteralPath $platformBootstrapFacadePath)) {
    $violations += "${platformBootstrapFacade}: Platform Bootstrap facade is missing"
} else {
    $bootstrapText = Get-Content -LiteralPath $platformBootstrapFacadePath -Raw
    if ($bootstrapText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${platformBootstrapFacade}: Platform Bootstrap must use selected profile bridge, never board_port"
    }
}
foreach ($relativePath in $platformBootstrapProfileSources) {
    $path = Join-Path $projectRoot $relativePath
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "${relativePath}: Platform Bootstrap profile bridge is missing"
        continue
    }
    $bootstrapProfileText = Get-Content -LiteralPath $path -Raw
    if ($bootstrapProfileText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${relativePath}: selected Bootstrap bridge must use the private legacy bootstrap/input seam, never board_port"
    }
    if ($bootstrapProfileText -notmatch '#include\s*[<"]legacy_bootstrap_input\.h[>"]' -or
        $bootstrapProfileText -notmatch '\blegacy_bootstrap_input_initialize\s*\(') {
        $violations += "${relativePath}: selected Bootstrap bridge must use the private legacy bootstrap/input seam"
    }
}

# Platform Input follows the same migration shape, but after Bootstrap it owns
# only normalized scanner publication/stop; starting a scanner must not also
# repeat the renderer/panel/audio/peripheral boot transaction.
$platformInputFacadePath = Join-Path $projectRoot $platformInputFacade
if (-not (Test-Path -LiteralPath $platformInputFacadePath)) {
    $violations += "${platformInputFacade}: Platform Input facade is missing"
} else {
    $inputText = Get-Content -LiteralPath $platformInputFacadePath -Raw
    if ($inputText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${platformInputFacade}: Platform Input must use selected profile bridge, never board_port"
    }
}
foreach ($relativePath in $platformInputProfileSources) {
    $path = Join-Path $projectRoot $relativePath
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "${relativePath}: Platform Input profile bridge is missing"
        continue
    }
    $inputProfileText = Get-Content -LiteralPath $path -Raw
    if ($inputProfileText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${relativePath}: selected Input profile bridge must use the private legacy bootstrap/input seam, never board_port"
    }
    if ($inputProfileText -notmatch '#include\s*[<"]legacy_bootstrap_input\.h[>"]' -or
        $inputProfileText -notmatch '\blegacy_bootstrap_input_start_scanner\s*\(' -or
        $inputProfileText -notmatch '\blegacy_bootstrap_input_stop_scanner\s*\(') {
        $violations += "${relativePath}: Input profile bridge must call only private scanner start/stop seam"
    }
    if ($inputProfileText -match '\bboard_port_set_command_cancel_enabled\s*\(') {
        $violations += "${relativePath}: command-cancel policy must use the selected private Input service, never board_port"
    }
}
$legacyBootstrapInputHeader = 'main/legacy_bootstrap_input.h'
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot $legacyBootstrapInputHeader))) {
    $violations += "${legacyBootstrapInputHeader}: private legacy Bootstrap/Input seam is missing"
} else {
    $legacyBootstrapInputReferences = @($allCFiles | Where-Object {
        Select-String -Path $_.FullName -Pattern 'legacy_bootstrap_input\.h' -Quiet
    })
    $legacyBootstrapInputActual = @($legacyBootstrapInputReferences | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    } | Sort-Object -Unique)
    $legacyBootstrapInputExpected = @(
        'main/board_port.c',
        'main/compact_renderer.c',
        'main/platform_bootstrap_compact.c',
        'main/platform_bootstrap_round.c',
        'main/platform_input_compact.c',
        'main/platform_input_round.c'
    )
    if (($legacyBootstrapInputActual -join '|') -ne ($legacyBootstrapInputExpected -join '|')) {
        $violations += "${legacyBootstrapInputHeader}: may be included only by $($legacyBootstrapInputExpected -join ', '); found: $($legacyBootstrapInputActual -join ', ')"
    }
}
$obsoleteBootstrapInputFacade = @($allCFiles | Where-Object {
    Select-String -Path $_.FullName -Pattern '\bboard_port_(?:bootstrap|start_input|stop_input)\s*\(' -Quiet
})
foreach ($reference in $obsoleteBootstrapInputFacade) {
    $relative = $reference.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    if ($relative -notin @('main/board_port.c', 'main/compact_renderer.c', 'main/legacy_bootstrap_input.h')) {
        $violations += "${relative}: obsolete board_port Bootstrap/Input facade remains; route through the selected private Bootstrap/Input seam"
    }
}

# Command cancellation is normalized business policy, but recognition belongs
# to the selected Input implementation.  Keep the rectangular no-op and round
# touch gesture revision below their respective private services; a renderer
# facade must not become the policy carrier again.
foreach ($inputCancelContract in @(
    @{ Path = 'main/compact_input_service.h'; Pattern = '\bvoid\s+compact_input_service_set_command_cancel_enabled\s*\(\s*bool\s+enabled\s*\)' },
    @{ Path = 'main/round_input_service.h'; Pattern = '\bvoid\s+round_input_service_set_command_cancel_enabled\s*\(\s*bool\s+enabled\s*\)' }
)) {
    $path = Join-Path $projectRoot $inputCancelContract.Path
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "$($inputCancelContract.Path): private Input command-cancel contract is missing"
        continue
    }
    if ((Get-Content -LiteralPath $path -Raw) -notmatch $inputCancelContract.Pattern) {
        $violations += "$($inputCancelContract.Path): private Input command-cancel contract is malformed"
    }
}
$compactInputBridge = Join-Path $projectRoot 'main/platform_input_compact.c'
$roundInputBridge = Join-Path $projectRoot 'main/platform_input_round.c'
 $compactInputBridgeText = Get-Content -LiteralPath $compactInputBridge -Raw
if ($compactInputBridgeText -notmatch '#include\s*[<"]compact_input_service\.h[>"]' -or
    $compactInputBridgeText -notmatch '\bcompact_input_service_set_command_cancel_enabled\s*\(') {
    $violations += 'main/platform_input_compact.c: compact command-cancel policy must stay in compact Input service'
}
$roundInputBridgeText = Get-Content -LiteralPath $roundInputBridge -Raw
if ($roundInputBridgeText -notmatch '#include\s*[<"]round_input_service\.h[>"]' -or
    $roundInputBridgeText -notmatch '\bround_input_service_set_command_cancel_enabled\s*\(') {
    $violations += 'main/platform_input_round.c: round command-cancel policy must stay in round Input service'
}
$obsoleteCommandCancelFacade = @($allCFiles | Where-Object {
    Select-String -Path $_.FullName -Pattern '\bboard_port_set_command_cancel_enabled\s*\(' -Quiet
})
foreach ($reference in $obsoleteCommandCancelFacade) {
    $relative = $reference.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    $violations += "${relative}: obsolete board_port command-cancel facade remains; route policy through selected private Input service"
}

$obsoleteBoardInit = @($allCFiles | Where-Object {
    Select-String -Path $_.FullName -Pattern '\bboard_port_init\s*\(' -Quiet
})
foreach ($reference in $obsoleteBoardInit) {
    $relative = $reference.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    $violations += "${relative}: obsolete board_port_init remains; split it into Bootstrap and Input scanner lifecycle"
}
# Platform Connectivity uses the same family bridge pattern.  Connectivity
# Service retains policy and transaction ownership; the shared facade must not
# gain modem, persistence or legacy renderer knowledge simply because Fangtang
# has ML307.  The selected compact/round bridge owns that migration seam
# through a private legacy transport contract; it must never depend on the
# broad renderer compatibility facade.
$platformConnectivityFacadePath = Join-Path $projectRoot $platformConnectivityFacade
if (-not (Test-Path -LiteralPath $platformConnectivityFacadePath)) {
    $violations += "${platformConnectivityFacade}: Platform Connectivity facade is missing"
} else {
    $connectivityText = Get-Content -LiteralPath $platformConnectivityFacadePath -Raw
    if ($connectivityText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${platformConnectivityFacade}: Platform Connectivity must use selected profile bridge, never board_port"
    }
}
foreach ($relativePath in $platformConnectivityProfileSources) {
    $path = Join-Path $projectRoot $relativePath
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "${relativePath}: Platform Connectivity profile bridge is missing"
        continue
    }
    $connectivityProfileText = Get-Content -LiteralPath $path -Raw
    if ($connectivityProfileText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${relativePath}: selected Connectivity profile bridge must use the private legacy transport seam, never board_port"
    }
    if ($connectivityProfileText -notmatch '#include\s*[<"]legacy_connectivity_transport\.h[>"]' -or
        $connectivityProfileText -notmatch '\blegacy_connectivity_transport_[A-Za-z0-9_]+\s*\(') {
        $violations += "${relativePath}: selected Connectivity profile bridge must use the private legacy transport seam"
    }
}
$legacyConnectivityTransportHeader = 'main/legacy_connectivity_transport.h'
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot $legacyConnectivityTransportHeader))) {
    $violations += "${legacyConnectivityTransportHeader}: private legacy Connectivity transport seam is missing"
} else {
    $legacyConnectivityTransportReferences = @($allCFiles | Where-Object {
        Select-String -Path $_.FullName -Pattern 'legacy_connectivity_transport\.h' -Quiet
    })
    $legacyConnectivityTransportActual = @($legacyConnectivityTransportReferences | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    } | Sort-Object -Unique)
    $legacyConnectivityTransportExpected = @(
        'main/board_port.c',
        'main/compact_renderer.c',
        'main/platform_connectivity_compact.c',
        'main/platform_connectivity_round.c'
    )
    if (($legacyConnectivityTransportActual -join '|') -ne ($legacyConnectivityTransportExpected -join '|')) {
        $violations += "${legacyConnectivityTransportHeader}: may be included only by $($legacyConnectivityTransportExpected -join ', '); found: $($legacyConnectivityTransportActual -join ', ')"
    }
}
# Lifecycle Service owns rollback ordering and time budgets.  Public Platform
# Lifecycle must therefore not see the renderer facade; only the selected
# bridge can translate its bounded background-task shutdown during migration.
$platformLifecycleFacadePath = Join-Path $projectRoot $platformLifecycleFacade
if (-not (Test-Path -LiteralPath $platformLifecycleFacadePath)) {
    $violations += "${platformLifecycleFacade}: Platform Lifecycle facade is missing"
} else {
    $lifecycleText = Get-Content -LiteralPath $platformLifecycleFacadePath -Raw
    if ($lifecycleText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${platformLifecycleFacade}: Platform Lifecycle must use selected profile bridge, never board_port"
    }
}
foreach ($relativePath in $platformLifecycleProfileSources) {
    $path = Join-Path $projectRoot $relativePath
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "${relativePath}: Platform Lifecycle profile bridge is missing"
        continue
    }
    $lifecycleProfileText = Get-Content -LiteralPath $path -Raw
    if ($lifecycleProfileText -notmatch '#include\s*[<"]board_background_lifecycle\.h[>"]' -or
        $lifecycleProfileText -notmatch '\bboard_background_lifecycle_stop\s*\(') {
        $violations += "${relativePath}: selected Lifecycle profile bridge must own only the bounded renderer-background lifecycle seam"
    }
}
# The lifecycle seam is private to legacy renderers and the selected bridge.
# New business or Platform code must not use it to reconstruct a board-wide
# deinit/restart contract.
$backgroundLifecycleHeader = 'main/board_background_lifecycle.h'
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot $backgroundLifecycleHeader))) {
    $violations += "${backgroundLifecycleHeader}: private renderer-background lifecycle seam is missing"
} else {
    $backgroundLifecycleReferences = @($allCFiles | Where-Object {
        Select-String -Path $_.FullName -Pattern 'board_background_lifecycle\.h' -Quiet
    })
    $backgroundLifecycleActual = @($backgroundLifecycleReferences | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    } | Sort-Object -Unique)
    $backgroundLifecycleExpected = @(
        'main/board_port.c',
        'main/compact_renderer.c',
        'main/platform_lifecycle_compact.c',
        'main/platform_lifecycle_round.c'
    )
    if (($backgroundLifecycleActual -join '|') -ne ($backgroundLifecycleExpected -join '|')) {
        $violations += "${backgroundLifecycleHeader}: may be included only by $($backgroundLifecycleExpected -join ', '); found: $($backgroundLifecycleActual -join ', ')"
    }
}
# Platform Storage owns physical VFS/partition operations but must not learn
# the renderer facade just to ask whether rebuildable flash work is safe.  The
# selected profile bridge reaches its board-specific cache-fabric admission
# only through a narrow private legacy seam.
$platformStorageFacadePath = Join-Path $projectRoot $platformStorageFacade
if (-not (Test-Path -LiteralPath $platformStorageFacadePath)) {
    $violations += "${platformStorageFacade}: Platform Storage facade is missing"
} else {
    $storageText = Get-Content -LiteralPath $platformStorageFacadePath -Raw
    if ($storageText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${platformStorageFacade}: Platform Storage must use selected profile bridge, never board_port"
    }
}
foreach ($relativePath in $platformStorageProfileSources) {
    $path = Join-Path $projectRoot $relativePath
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "${relativePath}: Platform Storage profile bridge is missing"
        continue
    }
    $storageProfileText = Get-Content -LiteralPath $path -Raw
    if ($storageProfileText -match '#include\s*[<"]board_port\.h[>"]|\bboard_port_[A-Za-z0-9_]+') {
        $violations += "${relativePath}: selected Storage profile bridge must use the private legacy storage admission seam, never board_port"
    }
    if ($storageProfileText -notmatch '#include\s*[<"]legacy_storage_admission\.h[>"]' -or
        $storageProfileText -notmatch '\blegacy_storage_admission_allows_optional_flash_work\s*\(') {
        $violations += "${relativePath}: selected Storage profile bridge must use the private legacy storage admission seam"
    }
}
$legacyStorageAdmissionHeader = 'main/legacy_storage_admission.h'
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot $legacyStorageAdmissionHeader))) {
    $violations += "${legacyStorageAdmissionHeader}: private legacy Storage admission seam is missing"
} else {
    $legacyStorageAdmissionReferences = @($allCFiles | Where-Object {
        Select-String -Path $_.FullName -Pattern 'legacy_storage_admission\.h' -Quiet
    })
    $legacyStorageAdmissionActual = @($legacyStorageAdmissionReferences | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    } | Sort-Object -Unique)
    $legacyStorageAdmissionExpected = @(
        'main/board_port.c',
        'main/compact_renderer.c',
        'main/platform_storage_compact.c',
        'main/platform_storage_round.c'
    )
    if (($legacyStorageAdmissionActual -join '|') -ne ($legacyStorageAdmissionExpected -join '|')) {
        $violations += "${legacyStorageAdmissionHeader}: may be included only by $($legacyStorageAdmissionExpected -join ', '); found: $($legacyStorageAdmissionActual -join ', ')"
    }
}
$obsoleteStorageAdmissionFacade = @($allCFiles | Where-Object {
    Select-String -Path $_.FullName -Pattern '\bboard_port_allows_optional_flash_work\s*\(' -Quiet
})
foreach ($reference in $obsoleteStorageAdmissionFacade) {
    $relative = $reference.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    if ($relative -notin @('main/board_port.c', 'main/compact_renderer.c', 'main/legacy_storage_admission.h')) {
        $violations += "${relative}: obsolete board_port Storage admission facade remains; route through the selected private storage admission seam"
    }
}
$selectorReferences = @($allCFiles | Where-Object {
    $_.FullName -ne (Join-Path $projectRoot $roundPeripheralSelector) -and
    (Select-String -Path $_.FullName -Pattern 'boards/round_peripheral_adapter\.h' -Quiet)
})
if ($selectorReferences.Count -ne 1 -or
    $selectorReferences[0].FullName -ne (Join-Path $projectRoot $roundPeripheralOwner)) {
    $names = ($selectorReferences | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    }) -join ', '
    $violations += "${roundPeripheralSelector}: must be included only by ${roundPeripheralOwner}; found: ${names}"
}
$roundDisplaySelectorReferences = @($allCFiles | Where-Object {
    $_.FullName -ne (Join-Path $projectRoot $roundDisplaySelector) -and
    (Select-String -Path $_.FullName -Pattern 'boards/round_display_adapter\.h' -Quiet)
})
if ($roundDisplaySelectorReferences.Count -ne 1 -or
    $roundDisplaySelectorReferences[0].FullName -ne (Join-Path $projectRoot $roundDisplayOwner)) {
    $names = ($roundDisplaySelectorReferences | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    }) -join ', '
    $violations += "${roundDisplaySelector}: must be included only by ${roundDisplayOwner}; found: ${names}"
}
if (-not (Select-String -Path (Join-Path $projectRoot $roundInputService) -Pattern '"round_peripheral_service\.h"' -Quiet)) {
    $violations += "${roundInputService}: must consume normalized touch facts from round_peripheral_service"
}
if (Select-String -Path (Join-Path $projectRoot $roundAudioHeader) -Pattern 'round_audio_adapter_(touch|get_power_status|get_motion_sample)' -Quiet) {
    $violations += "${roundAudioHeader}: peripheral observations must not be exposed through the Audio HAL"
}
foreach ($source in $allCFiles) {
    if (Select-String -Path $source.FullName -Pattern 'round_audio_adapter_(touch|get_power_status|get_motion_sample)' -Quiet) {
        $relative = $source.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
        $violations += "${relative}: peripheral observation leaked through Audio HAL"
    }
}
if (Select-String -Path (Join-Path $projectRoot 'main/round_peripheral_service.h') -Pattern 'i2c_master|round_peripheral_service_(initialize|release)' -Quiet) {
    $violations += 'main/round_peripheral_service.h: public Peripheral observation contract must not expose shared I2C lifecycle'
}

# Waveshare's IMU enhances the shared Motion HAL, but it is not a prerequisite
# for the common touch/display/audio product baseline.  Keep that downgrade
# decision profile-private: a future missing optional sensor must not recreate
# a board-specific exception in the shared bootstrap or business services.
$wavesharePeripheralAdapter = Join-Path $projectRoot 'main/boards/waveshare_amoled_1_75c/waveshare_peripheral_adapter.h'
if (-not (Test-Path -LiteralPath $wavesharePeripheralAdapter)) {
    $violations += 'main/boards/waveshare_amoled_1_75c/waveshare_peripheral_adapter.h: Waveshare peripheral adapter is missing'
} else {
    $wavesharePeripheralText = Get-Content -LiteralPath $wavesharePeripheralAdapter -Raw
    foreach ($waveshareMotionRequirement in @(
            'esp_err_t\s+imu_err\s*=\s*waveshare_qmi8658_init\s*\(\s*bus\s*\)',
            'optional QMI8658 unavailable',
            'Motion HAL disabled',
            's_waveshare_qmi8658\s*=\s*NULL')) {
        if ($wavesharePeripheralText -notmatch $waveshareMotionRequirement) {
            $violations += "main/boards/waveshare_amoled_1_75c/waveshare_peripheral_adapter.h: optional Motion downgrade is incomplete (${waveshareMotionRequirement})"
        }
    }
    if ($wavesharePeripheralText -match 'ESP_RETURN_ON_ERROR\s*\(\s*waveshare_qmi8658_init') {
        $violations += 'main/boards/waveshare_amoled_1_75c/waveshare_peripheral_adapter.h: optional QMI8658 may not fail the common peripheral bootstrap'
    }
    foreach ($waveshareMotionTestRequirement in @(
            'provisioning_failure_injection_waveshare_qmi8658_init_fails\s*\(\s*\)',
            'test injection:\s*forcing optional QMI8658 initialization failure',
            'provisioning_failure_injection_waveshare_qmi8658_motion_read_fails_once\s*\(\s*\)',
            'test injection:\s*forcing one optional QMI8658 motion read failure')) {
        if ($wavesharePeripheralText -notmatch $waveshareMotionTestRequirement) {
            $violations += "main/boards/waveshare_amoled_1_75c/waveshare_peripheral_adapter.h: optional Motion HIL test seam is incomplete (${waveshareMotionTestRequirement})"
        }
    }
    $failureInjectionHeader = Join-Path $projectRoot 'main/provisioning_failure_injection.h'
    $failureInjectionSource = Join-Path $projectRoot 'main/provisioning_failure_injection.c'
    $waveshareMotionTestDefaults = Join-Path $projectRoot 'sdkconfig.defaults.waveshare-amoled-1.75c-qmi8658-init-fi'
    $waveshareMotionReadTestDefaults = Join-Path $projectRoot 'sdkconfig.defaults.waveshare-amoled-1.75c-qmi8658-read-fi'
    $profileBuildWrapper = Join-Path $projectRoot 'tools/build-profile.cmd'
    if (-not (Test-Path -LiteralPath $failureInjectionHeader) -or
        -not (Test-Path -LiteralPath $failureInjectionSource) -or
        -not (Test-Path -LiteralPath $waveshareMotionTestDefaults) -or
        -not (Test-Path -LiteralPath $waveshareMotionReadTestDefaults) -or
        -not (Test-Path -LiteralPath $profileBuildWrapper)) {
        $violations += 'Waveshare optional Motion HIL artifact: failure-injection contract, defaults, or build wrapper is missing'
    } else {
        $failureInjectionHeaderText = Get-Content -LiteralPath $failureInjectionHeader -Raw
        $failureInjectionSourceText = Get-Content -LiteralPath $failureInjectionSource -Raw
        $waveshareMotionTestDefaultsText = Get-Content -LiteralPath $waveshareMotionTestDefaults -Raw
        $waveshareMotionReadTestDefaultsText = Get-Content -LiteralPath $waveshareMotionReadTestDefaults -Raw
        $profileBuildWrapperText = Get-Content -LiteralPath $profileBuildWrapper -Raw
        foreach ($waveshareMotionArtifactRequirement in @(
                'bool\s+provisioning_failure_injection_waveshare_qmi8658_init_fails\s*\(\s*void\s*\)',
                'CONFIG_MACLAW_TEST_BUILD\s*&&\s*CONFIG_MACLAW_WAVESHARE_QMI8658_INIT_FAILURE',
                'bool\s+provisioning_failure_injection_waveshare_qmi8658_motion_read_fails_once\s*\(\s*void\s*\)',
                's_waveshare_qmi8658_motion_read_probe_seen',
                'CONFIG_MACLAW_TEST_BUILD\s*&&\s*CONFIG_MACLAW_WAVESHARE_QMI8658_MOTION_READ_FAILURE')) {
            if ($failureInjectionHeaderText -notmatch $waveshareMotionArtifactRequirement -and
                $failureInjectionSourceText -notmatch $waveshareMotionArtifactRequirement) {
                $violations += "main/provisioning_failure_injection.[ch]: Waveshare optional Motion HIL seam is incomplete (${waveshareMotionArtifactRequirement})"
            }
        }
        if ($waveshareMotionTestDefaultsText -notmatch 'CONFIG_MACLAW_TEST_BUILD=y' -or
            $waveshareMotionTestDefaultsText -notmatch 'CONFIG_MACLAW_WAVESHARE_QMI8658_INIT_FAILURE=y') {
            $violations += 'sdkconfig.defaults.waveshare-amoled-1.75c-qmi8658-init-fi: must enable only the compile-time Waveshare optional Motion failure artifact'
        }
        if ($waveshareMotionReadTestDefaultsText -notmatch 'CONFIG_MACLAW_TEST_BUILD=y' -or
            $waveshareMotionReadTestDefaultsText -notmatch 'CONFIG_MACLAW_WAVESHARE_QMI8658_MOTION_READ_FAILURE=y' -or
            $waveshareMotionReadTestDefaultsText -match 'QMI8658_INIT_FAILURE=y') {
            $violations += 'sdkconfig.defaults.waveshare-amoled-1.75c-qmi8658-read-fi: must enable only the one-shot runtime Motion read artifact'
        }
        if ($profileBuildWrapperText -notmatch 'waveshare-amoled-1\.75c-qmi8658-init-fi' -or
            $profileBuildWrapperText -notmatch 'build-test-waveshare-amoled-1\.75c-qmi8658-init-fi' -or
            $profileBuildWrapperText -notmatch 'waveshare-amoled-1\.75c-qmi8658-read-fi' -or
            $profileBuildWrapperText -notmatch 'build-test-waveshare-amoled-1\.75c-qmi8658-read-fi') {
            $violations += 'tools/build-profile.cmd: Waveshare optional Motion HIL profiles are not selectable'
        }
    }
}
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot $roundPeripheralLifecycle))) {
    $violations += "${roundPeripheralLifecycle}: private shared-I2C lifecycle seam is missing"
} else {
    $lifecycleReferences = @($allCFiles | Where-Object {
        Select-String -Path $_.FullName -Pattern 'round_peripheral_lifecycle\.h' -Quiet
    })
    $lifecycleActual = @($lifecycleReferences | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    } | Sort-Object -Unique)
    $lifecycleExpected = @('main/round_audio_service.c', 'main/round_peripheral_service.c')
    if (($lifecycleActual -join '|') -ne ($lifecycleExpected -join '|')) {
        $violations += "${roundPeripheralLifecycle}: may be included only by $($lifecycleExpected -join ', '); found: $($lifecycleActual -join ', ')"
    }
}
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot $roundAudioLifecycle))) {
    $violations += "${roundAudioLifecycle}: private shared-I2C source-owner seam is missing"
} else {
    # The semantic Audio lifecycle bridge exists only so Peripheral can request
    # a codec-owner bus preflight.  A renderer, input scanner, power policy or
    # new board adapter including it would silently recreate the old
    # cross-domain dependency while still compiling.
    $audioLifecycleReferences = @($allCFiles | Where-Object {
        Select-String -Path $_.FullName -Pattern 'round_audio_lifecycle\.h' -Quiet
    })
    $audioLifecycleActual = @($audioLifecycleReferences | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    } | Sort-Object -Unique)
    # The codec adapter is textually included into round_audio_service.c and
    # remains that source owner's private implementation detail.  It may use
    # the semantic lifecycle hooks while no other board adapter/renderer may.
    $audioLifecycleExpected = @('main/boards/round_audio_codec_adapter.h', 'main/round_audio_service.c', 'main/round_peripheral_service.c')
    if (($audioLifecycleActual -join '|') -ne ($audioLifecycleExpected -join '|')) {
        $violations += "${roundAudioLifecycle}: may be included only by $($audioLifecycleExpected -join ', '); found: $($audioLifecycleActual -join ', ')"
    }
}

# Shared renderer source owners implement scenes and session policy only.  A private
# service is the sole bridge to a selected hardware adapter.  Keep this gate
# independent from the public-header checks above: otherwise a new adapter
# call can compile successfully while quietly putting a board-specific branch
# back into business code.
$sharedRenderers = @('main/board_port.c', 'main/compact_renderer.c')
$adapterCallPattern = '\b(?:round|compact)_(?:audio|display|input|connectivity|peripheral|visual)_adapter_[A-Za-z0-9_]+'
foreach ($relativeRenderer in $sharedRenderers) {
    $renderer = Join-Path $projectRoot $relativeRenderer
    if (-not (Test-Path -LiteralPath $renderer)) {
        $violations += "${relativeRenderer}: shared renderer is missing"
        continue
    }
    $adapterCalls = Select-String -Path $renderer -Pattern $adapterCallPattern
    foreach ($match in $adapterCalls) {
        $violations += "${relativeRenderer}:$($match.LineNumber): shared renderer must call a private HAL service, not adapter: $($match.Line.Trim())"
    }
}

# Device API is a public facade over domain services, never a second caller of
# a physical Platform port.  Connectivity Service is the sole shared owner of
# the profile-private cellular transport contract; otherwise a future Wi-Fi or
# modem lifecycle change has to be copied into Device API and bypasses the
# service's admission/validation boundary.
# Connectivity Service may keep EventGroup/ESP-IDF details privately, but its
# lifecycle result is part of the common Device contract. This prevents a new
# uplink profile from needing a second error translation above the service.
$connectivityServiceHeader = Join-Path $projectRoot 'main/connectivity_service.h'
if (-not (Test-Path -LiteralPath $connectivityServiceHeader)) {
    $violations += 'main/connectivity_service.h: Connectivity Service public contract is missing'
} else {
    $connectivityServiceText = Get-Content -LiteralPath $connectivityServiceHeader -Raw
    foreach ($lifecycleName in @('initialize', 'deinit')) {
        if ($connectivityServiceText -notmatch "\bdevice_status_t\s+connectivity_service_${lifecycleName}\s*\(") {
            $violations += "main/connectivity_service.h: ${lifecycleName} must return device_status_t"
        }
        if ($connectivityServiceText -match "\besp_err_t\s+connectivity_service_${lifecycleName}\s*\(") {
            $violations += "main/connectivity_service.h: ${lifecycleName} must not expose esp_err_t"
        }
    }
}$deviceApiSource = Join-Path $projectRoot 'main/device_api.c'
$connectivityServiceSource = Join-Path $projectRoot 'main/connectivity_service.c'
if (Select-String -Path $deviceApiSource -Pattern 'platform_(?:connectivity|power|sensor|storage)_' -Quiet) {
    $violations += 'main/device_api.c: Device API must reach physical Connectivity, Power, Sensor and Storage ports only through their domain services'
}
$platformConnectivityReferences = @($allCFiles | Where-Object {
    Select-String -Path $_.FullName -Pattern 'platform_connectivity\.h' -Quiet
})
$platformConnectivityActual = @($platformConnectivityReferences | ForEach-Object {
    $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
} | Sort-Object -Unique)
$platformConnectivityExpected = @('main/connectivity_service.c', 'main/platform_connectivity.c')
if (($platformConnectivityActual -join '|') -ne ($platformConnectivityExpected -join '|')) {
    $violations += "main/platform_connectivity.h: may be included only by connectivity_service; found: $($platformConnectivityActual -join ', ')"
}
# Cellular lifecycle is one Connectivity-domain transaction.  The composition
# root may install value-only presentation/Gateway seams, but it must not
# reconstruct recovery from readiness plus separate prepare/start calls.  That
# would make a new 4G profile reproduce stale-session and rollback checks.
$mainConnectivitySource = Join-Path $projectRoot 'main/main.c'
if (Select-String -Path $mainConnectivitySource -Pattern 'device_connectivity_(?:prepare_cellular_transport|start_cellular_transport|set_cellular_ready)\s*\(' -Quiet) {
    $violations += 'main/main.c: cellular readiness and prepare/start sequencing must use device_connectivity_establish_cellular_transport()'
}
if (Select-String -Path $mainConnectivitySource -Pattern 'device_connectivity_establish_cellular_transport\s*\(' -Quiet) {
    $violations += 'main/main.c: cellular establish/recovery transaction must live in Cellular Recovery Service'
}
$mainConnectivityText = Get-Content -LiteralPath $mainConnectivitySource -Raw
$wifiEventMatch = [regex]::Match(
    $mainConnectivityText,
    'static\s+void\s+wifi_event[\s\S]*?wifi_event_callback_leave\s*\(\s*\)\s*;\s*\n\}')
if (-not $wifiEventMatch.Success) {
    $violations += 'main/main.c: cannot inspect Wi-Fi callback recovery boundary'
} elseif ($wifiEventMatch.Value -match 'gateway_transport_(?:startup_running|start_startup_task)\s*\(' -or
          $wifiEventMatch.Value -notmatch 'cellular_recovery_service_note_wifi_ready\s*\(') {
    $violations += 'main/main.c: Wi-Fi GOT_IP Gateway recovery must notify Cellular Recovery Service, not reconstruct Gateway policy'
}
$cellularRecoveryServiceSource = Join-Path $projectRoot 'main/services/cellular_recovery_service.c'
$cellularRecoveryPolicySource = Join-Path $projectRoot 'main/services/cellular_recovery_policy.c'
$cellularRecoveryHeader = Join-Path $projectRoot 'main/services/cellular_recovery_service.h'
if (-not (Test-Path -LiteralPath $cellularRecoveryServiceSource) -or
    -not (Test-Path -LiteralPath $cellularRecoveryPolicySource) -or
    -not (Test-Path -LiteralPath $cellularRecoveryHeader)) {
    $violations += 'main/services/cellular_recovery_service.[ch] and policy are required Connectivity-domain owners'
} else {
    $cellularRecoveryText = Get-Content -LiteralPath $cellularRecoveryServiceSource -Raw
    $cellularRecoveryHeaderText = Get-Content -LiteralPath $cellularRecoveryHeader -Raw
    foreach ($requiredRecoverySeam in @(
            'cellular_recovery_service_establish_initial',
            'cellular_recovery_service_prepare_system_sleep',
            'cellular_recovery_service_abort_system_sleep_prepare',
            'cellular_recovery_service_note_wifi_ready',
            'device_connectivity_establish_cellular_transport',
            'device_connectivity_is_cellular_transport_ready',
            'TASK_REGISTRY_OWNER_CONNECTIVITY',
            'cellular_recovery_policy_next_retry_ms')) {
        if ($cellularRecoveryText -notmatch $requiredRecoverySeam -and
            $cellularRecoveryHeaderText -notmatch $requiredRecoverySeam) {
            $violations += "main/services/cellular_recovery_service.[ch]: missing Connectivity recovery seam ${requiredRecoverySeam}"
        }
    }
    if ($cellularRecoveryText -match '\b(?:ml307_|platform_connectivity_|CONFIG_MACLAW_BOARD_|gpio_|esp_sleep)') {
        $violations += 'main/services/cellular_recovery_service.c: must not select modem, board, GPIO or sleep implementation'
    }
    if ($cellularRecoveryText -notmatch 'restart_gateway_after_wifi_ready[\s\S]*?device_connectivity_is_provisioning_active' -or
        $cellularRecoveryText -notmatch 'restart_gateway_after_wifi_ready[\s\S]*?wifi_gateway_startup_recovery_allowed' -or
        $cellularRecoveryText -notmatch 'restart_gateway_after_wifi_ready[\s\S]*?!s_system_sleep_preparing') {
        $violations += 'main/services/cellular_recovery_service.c: Wi-Fi recovery must retain provisioning, startup-admission and System Sleep guards'
    }
    if ($cellularRecoveryText -notmatch 's_initial_establishing' -or
        $cellularRecoveryText -notmatch 'cellular_recovery_service_establish_initial[\s\S]*?s_initial_establishing\s*=\s*true' -or
        $cellularRecoveryText -notmatch 'cellular_recovery_service_establish_initial[\s\S]*?s_initial_establishing\s*=\s*false' -or
        $cellularRecoveryText -notmatch 'cellular_recovery_service_prepare_system_sleep[\s\S]*?s_initial_establishing' -or
        $cellularRecoveryText -notmatch 'ensure_running_internal[\s\S]*?!s_initial_establishing') {
        $violations += 'main/services/cellular_recovery_service.c: initial cellular establish must close recovery/Sleep admission until the synchronous modem operation returns'
    }
    if ($cellularRecoveryText -notmatch 'static\s+bool\s+recovery_admitted\s*\(' -or
        $cellularRecoveryText -notmatch 'while\s*\(\s*recovery_admitted\s*\(' -or
        $cellularRecoveryText -notmatch 'device_connectivity_establish_cellular_transport[\s\S]*?if\s*\(\s*!recovery_admitted\s*\(\s*\)\s*\)\s*continue\s*;[\s\S]*?publish_network_ready\s*\(\s*true\s*\)' -or
        $cellularRecoveryText -notmatch 'restart_gateway_after_wifi_ready[\s\S]*?s_admission_open' -or
        $cellularRecoveryText -notmatch 'static\s+bool\s+begin_gateway_rearm\s*\([\s\S]*?s_gateway_rearm_inflight' -or
        $cellularRecoveryText -notmatch 'cellular_recovery_service_prepare_system_sleep[\s\S]*?s_gateway_rearm_inflight') {
        $violations += 'main/services/cellular_recovery_service.c: recovery completion and Wi-Fi rearm must re-check logical admission after a concurrent lifecycle fence'
    }
}
$connectivityServiceSourceText = Get-Content -LiteralPath $connectivityServiceSource -Raw
if ($connectivityServiceSourceText -notmatch 'static\s+bool\s+begin_cellular_transport_quiesce\s*\(' -or
    $connectivityServiceSourceText -notmatch 'const\s+bool\s+physically_ready\s*=\s*platform_connectivity_is_cellular_transport_ready\s*\(' -or
    $connectivityServiceSourceText -notmatch 'session_exists\s*=\s*s_active_uplink\s*==\s*DEVICE_UPLINK_CELLULAR\s*\|\|[\s\S]*?s_cellular_ready[\s\S]*?physically_ready' -or
    $connectivityServiceSourceText -notmatch 'connectivity_service_quiesce_cellular_transport[\s\S]*?begin_cellular_transport_quiesce\s*\(') {
    $violations += 'main/connectivity_service.c: terminal cellular quiesce must drain a still-live prior session after selection changes without admitting a fresh Wi-Fi-only generation'
}
if ($connectivityServiceSourceText -notmatch 'bool\s+connectivity_service_has_cellular_transport_session\s*\(' -or
    $mainConnectivityText -notmatch 'device_connectivity_has_cellular_transport_session\s*\(\s*\)[\s\S]*?device_connectivity_quiesce_cellular_transport\s*\(') {
    $violations += 'main/main.c: terminal root rollback must use cellular-session evidence rather than the mutable selected-uplink hint before quiescing ML307'
}
$wifiStartupHeader = Join-Path $projectRoot 'main/services/wifi_startup_service.h'
$wifiStartupSource = Join-Path $projectRoot 'main/services/wifi_startup_service.c'
if (-not (Test-Path -LiteralPath $wifiStartupHeader) -or
    -not (Test-Path -LiteralPath $wifiStartupSource)) {
    $violations += 'main/services/wifi_startup_service.[ch]: Wi-Fi startup policy service is missing'
} else {
    $wifiStartupHeaderText = Get-Content -LiteralPath $wifiStartupHeader -Raw
    $wifiStartupSourceText = Get-Content -LiteralPath $wifiStartupSource -Raw
    foreach ($wifiStartupRequirement in @(
            'wifi_startup_service_host_t',
            'wifi_startup_service_request_t',
            'scan_visible',
            'begin_attempt',
            'wait_attempt',
            'configure_enterprise',
            'wifi_startup_service_connect\s*\(')) {
        if ($wifiStartupHeaderText -notmatch $wifiStartupRequirement -or
            $wifiStartupSourceText -notmatch $wifiStartupRequirement) {
            $violations += "main/services/wifi_startup_service.[ch]: Wi-Fi startup policy is incomplete (${wifiStartupRequirement})"
        }
    }
    if ($wifiStartupHeaderText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/wifi_startup_service.h: public Wi-Fi startup policy must remain value-only and hardware/RTOS/JSON-neutral'
    }
    if ($wifiStartupSourceText -match '\b(?:esp_http_client|esp_wifi|esp_netif|psa_hash|heap_caps|xTask|SemaphoreHandle_t|gateway_transport|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/wifi_startup_service.c: policy service must not absorb HTTP, Wi-Fi SDK, netif, crypto, allocator, RTOS, transport, or board ownership'
    }
    $wifiStartupRootText = Get-Content -LiteralPath $mainConnectivitySource -Raw
    if ($wifiStartupRootText -match '\b(?:start_wifi_saved_list|collect_saved_wifi_scan_candidate|saved_wifi_scan_candidates_t)\b' -or
        $wifiStartupRootText -notmatch 'wifi_startup_service_connect\s*\(') {
        $violations += 'main/main.c: saved Wi-Fi selection/attempt/enterprise startup policy must use wifi_startup_service'
    }
}
$platformSensorReferences = @($allCFiles | Where-Object {
    Select-String -Path $_.FullName -Pattern 'platform_sensor\.h' -Quiet
})
$platformSensorActual = @($platformSensorReferences | ForEach-Object {
    $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
} | Sort-Object -Unique)
$platformSensorExpected = @('main/motion_service.c', 'main/platform_sensor.c')
if (($platformSensorActual -join '|') -ne ($platformSensorExpected -join '|')) {
    $violations += "main/platform_sensor.h: may be included only by motion_service; found: $($platformSensorActual -join ', ')"
}
$platformPowerReferences = @($allCFiles | Where-Object {
    Select-String -Path $_.FullName -Pattern 'platform_power\.h' -Quiet
})
$platformPowerActual = @($platformPowerReferences | ForEach-Object {
    $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
} | Sort-Object -Unique)
$platformPowerExpected = @('main/platform_power.c', 'main/power_service.c')
if (($platformPowerActual -join '|') -ne ($platformPowerExpected -join '|')) {
    $violations += "main/platform_power.h: may be included only by power_service; found: $($platformPowerActual -join ', ')"
}
# DISPLAY_OFF is a normal hardware transaction with meaningful failure modes
# (scene ineligible, lifecycle closure, queue contention and adapter I/O).
# Keep those modes in the normalized Device status path instead of collapsing
# them to a bool at a Power/Display seam; otherwise a new profile has no way
# to preserve a transient panel error without growing board branches above HAL.
foreach ($statusContract in @(
    @{ Path = 'main/device_api.h'; Names = @('device_power_wake_display_from_user', 'device_power_wake_display_from_schedule', 'device_power_wake_display_from_remote_control') },
    @{ Path = 'main/power_service.h'; Names = @('power_service_wake_display_from_user', 'power_service_wake_display_from_schedule', 'power_service_wake_display_from_remote_control') },
    @{ Path = 'main/platform_power.h'; Names = @('platform_power_enter_display_off', 'platform_power_wake_display') },
    @{ Path = 'main/platform_display.h'; Names = @('platform_display_enter_display_off', 'platform_display_wake_display') },
    @{ Path = 'main/display_service.h'; Names = @('display_service_enter_display_off', 'display_service_wake_display') }
)) {
    $path = Join-Path $projectRoot $statusContract.Path
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "$($statusContract.Path): DISPLAY_OFF status contract header is missing"
        continue
    }
    $text = Get-Content -LiteralPath $path -Raw
    foreach ($name in $statusContract.Names) {
        if ($text -notmatch "\bdevice_status_t\s+${name}\s*\(") {
            $violations += "$($statusContract.Path): ${name} must return device_status_t"
        }
        if ($text -match "\bbool\s+${name}\s*\(") {
            $violations += "$($statusContract.Path): ${name} must not collapse DISPLAY_OFF result to bool"
        }
    }
}
# A DISPLAY_OFF deadline must not make its COMMIT decision from an unlocked
# lease snapshot. The short PREPARE -> COMMIT generation fence is the shared
# proof that no foreground audio/meeting/alarm/provisioning owner can arrive
# between eligibility and the physical panel transaction. Keep it private to
# Power Service: it is deliberately not a Device API for light/deep sleep.
$powerLeaseHeader = Join-Path $projectRoot 'main/power_lease_service.h'
$powerLeaseSource = Join-Path $projectRoot 'main/power_lease_service.c'
$powerServiceSource = Join-Path $projectRoot 'main/power_service.c'
foreach ($commitContract in @(
    @{ Path = $powerLeaseHeader; Label = 'main/power_lease_service.h'; Pattern = '\bdevice_status_t\s+power_lease_service_begin_display_off_commit\s*\(\s*uint32_t\s*\*\s*out_generation\s*\)' },
    @{ Path = $powerLeaseHeader; Label = 'main/power_lease_service.h'; Pattern = '\bbool\s+power_lease_service_display_off_commit_is_current\s*\(\s*uint32_t\s+generation\s*\)' },
    @{ Path = $powerLeaseHeader; Label = 'main/power_lease_service.h'; Pattern = '\bvoid\s+power_lease_service_end_display_off_commit\s*\(\s*uint32_t\s+generation\s*\)' },
    @{ Path = $powerLeaseSource; Label = 'main/power_lease_service.c'; Pattern = '\bs_display_off_commit_in_progress\b' },
    @{ Path = $powerServiceSource; Label = 'main/power_service.c'; Pattern = '\bpower_lease_service_begin_display_off_commit\s*\(' },
    @{ Path = $powerServiceSource; Label = 'main/power_service.c'; Pattern = '\bpower_lease_service_display_off_commit_is_current\s*\(' },
    @{ Path = $powerServiceSource; Label = 'main/power_service.c'; Pattern = '\bpower_lease_service_end_display_off_commit\s*\(' }
)) {
    if (-not (Test-Path -LiteralPath $commitContract.Path) -or
        -not (Select-String -Path $commitContract.Path -Pattern $commitContract.Pattern -Quiet)) {
        $violations += "$($commitContract.Label): DISPLAY_OFF PREPARE -> COMMIT lease fence is missing ($($commitContract.Pattern))"
    }
}
$powerLeaseDeinitText = if (Test-Path -LiteralPath $powerLeaseSource) {
    Get-Content -LiteralPath $powerLeaseSource -Raw
} else {
    ''
}
if ($powerLeaseDeinitText -notmatch 'const\s+bool\s+commit_in_progress\s*=\s*s_display_off_commit_in_progress\s*;' -or
    $powerLeaseDeinitText -notmatch 'active_count\s*==\s*0\s*&&\s*!commit_in_progress') {
    $violations += 'main/power_lease_service.c: deinit must drain an in-flight DISPLAY_OFF commit fence before reopening a lease generation'
}
if ($powerLeaseSource -and
    $powerLeaseDeinitText -notmatch '\bpower_lease_service_run_display_off_commit_lifecycle_test\s*\(') {
    $violations += 'main/power_lease_service.c: private DISPLAY_OFF commit lifecycle proof is missing'
}
$powerLeaseTestApiReferences = @($allCFiles | Where-Object {
    $_.Extension -eq '.c' -and
    $_.FullName -notmatch 'main[\\/]power_lease_service\.c$' -and
    (Select-String -Path $_.FullName -Pattern '\bpower_lease_service_run_display_off_commit_lifecycle_test\s*\(' -Quiet)
})
foreach ($reference in $powerLeaseTestApiReferences) {
    $relative = $reference.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    if ($relative -ne 'main/device_api.c') {
        $violations += "${relative}: private DISPLAY_OFF lease lifecycle proof may be called only by Device Power initialization"
    }
}
$powerRetryTestApiReferences = @($allCFiles | Where-Object {
    $_.Extension -eq '.c' -and
    $_.FullName -notmatch 'main[\\/]power_service\.c$' -and
    (Select-String -Path $_.FullName -Pattern '\bpower_service_run_display_off_retry_hil_test\s*\(' -Quiet)
})
foreach ($reference in $powerRetryTestApiReferences) {
    $relative = $reference.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    if ($relative -ne 'main/device_api.c') {
        $violations += "${relative}: private DISPLAY_OFF retry HIL may be called only by Device Power initialization"
    }
}
$obsoleteDisplayOffLeaseSnapshot = @($allCFiles | Where-Object {
    Select-String -Path $_.FullName -Pattern '\bpower_lease_service_allows_display_off\s*\(' -Quiet
})
foreach ($reference in $obsoleteDisplayOffLeaseSnapshot) {
    $relative = $reference.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    $violations += "${relative}: unlocked DISPLAY_OFF lease snapshot is forbidden; use the PREPARE -> COMMIT fence"
}
$platformStorageReferences = @($allCFiles | Where-Object {
    Select-String -Path $_.FullName -Pattern 'platform_storage\.h' -Quiet
})
$platformStorageActual = @($platformStorageReferences | ForEach-Object {
    $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
} | Sort-Object -Unique)
$platformStorageExpected = @('main/platform_storage.c', 'main/storage_service.c')
if (($platformStorageActual -join '|') -ne ($platformStorageExpected -join '|')) {
    $violations += "main/platform_storage.h: may be included only by storage_service; found: $($platformStorageActual -join ', ')"
}

# Audio Service is the common, profile-neutral owner of foreground audio
# session diagnostics. Identity/Hub observers may consume its by-value
# snapshot, but may not reach a selected codec/I2S adapter (or board renderer)
# merely to obtain diagnostics. Without this gate, a later board could add a
# useful-looking hardware counter directly to firmware_identity and silently
# undo the cross-profile observability contract.
$audioServiceHeader = Join-Path $projectRoot 'main/audio_service.h'
$firmwareIdentitySource = Join-Path $projectRoot 'main/firmware_identity.c'
if (-not (Test-Path -LiteralPath $audioServiceHeader)) {
    $violations += 'main/audio_service.h: shared Audio Service contract is missing'
} else {
    $audioServiceText = Get-Content -LiteralPath $audioServiceHeader -Raw
    if ($audioServiceText -notmatch '#define\s+AUDIO_SERVICE_SNAPSHOT_ABI_VERSION\s+2u' -or
        $audioServiceText -notmatch '#define\s+AUDIO_SERVICE_NOMINAL_SAMPLE_RATE_HZ\s+16000u' -or
        $audioServiceText -notmatch '\bbool\s+audio_service_get_snapshot\s*\(\s*audio_service_snapshot_t\s*\*\s*out_snapshot\s*\)') {
        $violations += 'main/audio_service.h: stable by-value audio diagnostics snapshot contract is missing'
    }
}
if (-not (Test-Path -LiteralPath $firmwareIdentitySource)) {
    $violations += 'main/firmware_identity.c: shared diagnostic publisher is missing'
} else {
    $firmwareIdentityText = Get-Content -LiteralPath $firmwareIdentitySource -Raw
    foreach ($requiredAudioPublication in @(
        '#include "audio_service.h"',
        'audio_service_get_snapshot\s*\(\s*&audio\s*\)',
        'cJSON_AddObjectToObject\s*\(\s*root\s*,\s*"audio"\s*\)'
    )) {
        if ($firmwareIdentityText -notmatch $requiredAudioPublication) {
            $violations += "main/firmware_identity.c: audio diagnostics must be published through audio_service snapshot (${requiredAudioPublication})"
        }
    }
    foreach ($forbiddenAudioImplementation in @(
        '#include\s+"(?:board_port|platform_audio|round_audio_service|compact_audio_service|round_audio_codec_adapter)\.h"',
        '\b(?:round|compact)_audio_adapter_[A-Za-z0-9_]+\s*\('
    )) {
        if ($firmwareIdentityText -match $forbiddenAudioImplementation) {
            $violations += 'main/firmware_identity.c: audio diagnostics must not bypass Audio Service into board/profile implementation'
        }
    }
}

# Round PCM cleanup is a private Audio-HAL transaction.  The shared renderer
# may decide capture/VAD policy, but it must not grow another codec-scale AGC,
# DC blocker, or damaged-sample filter beside a future board adapter.
$roundRenderer = Join-Path $projectRoot 'main/board_port.c'
# The private round Input stack is already the source owner for touch/key
# sampling and gesture lifecycle.  It must use normalized Device Input values
# directly: reintroducing a broad board-port header here would make a future
# circular profile inherit a legacy facade merely to publish a gesture.
foreach ($roundInputBoundaryFile in @(
    'main/round_input_service.h',
    'main/round_input_service.c',
    'main/round_input_profile_service.h',
    'main/round_input_profile_service.c',
    'main/boards/round_input_profile_adapter.h',
    'main/boards/echoear_2st/echoear_input_adapter.h',
    'main/boards/waveshare_amoled_1_75c/waveshare_input_adapter.h'
)) {
    $roundInputBoundaryPath = Join-Path $projectRoot $roundInputBoundaryFile
    if (Select-String -Path $roundInputBoundaryPath -Pattern 'board_port\.h|\bboard_(?:input|port)_' -Quiet) {
        $violations += "${roundInputBoundaryFile}: round Input HAL must not depend on board_port facade types"
    }
}
if (Select-String -Path $roundRenderer -Pattern '^[^/]*\brecording_pcm_(?:reset|process)\b' -Quiet) {
    $violations += 'main/board_port.c: capture PCM conditioning must be owned by round_audio_service'
}
if (Select-String -Path $roundRenderer -Pattern '^[^/]*\bWAKE_WORD_(?:TARGET_RMS|MIN_SOFTWARE_GAIN_Q8|MAX_SOFTWARE_GAIN_Q8|GAIN_ATTACK_SHIFT|GAIN_RELEASE_SHIFT|GAIN_UPDATE_FLOOR)\b' -Quiet) {
    $violations += 'main/board_port.c: wake PCM conditioning must be owned by round_audio_service'
}
if (Select-String -Path $roundRenderer -Pattern '^[^/]*\bs_audio_mutex\b' -Quiet) {
    $violations += 'main/board_port.c: physical audio ownership mutex must be owned by round_audio_service'
}

# Wake recognizer task handles, ESP-SR model runtime and their
# cancellation/pause generation are private Wake-service resources.  The
# round renderer supplies only its post-recognition business callback, so a
# future circular board does not need a renderer edit to change model or
# microphone recognition mechanics.
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot $roundWakeService))) {
    $violations += "${roundWakeService}: private wake lifecycle service is missing"
} else {
    $wakeLifecycleLeak = Select-String -Path $roundRenderer -Pattern '\bs_wake_(?:word_task|word_task_starting|word_ready|word_paused|word_pause_acknowledged|word_stop_requested|callback_pending|callback_task|callback_task_starting|callback_cancel_requested|word_lock)\b'
    foreach ($match in $wakeLifecycleLeak) {
        $violations += "main/board_port.c:$($match.LineNumber): wake task lifecycle must be owned by round_wake_service: $($match.Line.Trim())"
    }
    foreach ($wakeRuntimeLeak in @(
        'esp_mn_',
        'esp_srmodel_',
        'model_path\.h',
        '\bwake_word_task\b',
        '\bwake_callback_dispatch_task\b',
        '\bround_audio_service_wake_capture_'
    )) {
        $matches = Select-String -Path $roundRenderer -Pattern $wakeRuntimeLeak
        foreach ($match in $matches) {
            $violations += "main/board_port.c:$($match.LineNumber): ESP-SR wake runtime must be owned by round_wake_service: $($match.Line.Trim())"
        }
    }
    $wakeHeaderLeak = Select-String -Path (Join-Path $projectRoot 'main/round_wake_service.h') -Pattern 'freertos/|\b(?:TaskHandle_t|TaskFunction_t|srmodel_list_t|esp_mn_)\b'
    foreach ($match in $wakeHeaderLeak) {
        $violations += "main/round_wake_service.h:$($match.LineNumber): renderer-facing Wake contract must not expose RTOS or ESP-SR implementation types: $($match.Line.Trim())"
    }
    $roundAudioWakeTaskLeak = Select-String -Path (Join-Path $projectRoot 'main/round_audio_service.h') -Pattern '\bround_audio_service_start_wake_(?:recognizer|dispatch)_task\b|freertos/|\b(?:TaskHandle_t|TaskFunction_t)\b'
    foreach ($match in $roundAudioWakeTaskLeak) {
        $violations += "main/round_audio_service.h:$($match.LineNumber): Wake task placement must be owned by round_wake_service, and Audio's renderer-facing contract must not expose RTOS task types: $($match.Line.Trim())"
    }
    $rendererPeripheralPreparationLeak = Select-String -Path $roundRenderer -Pattern '\bround_audio_service_prepare_peripherals\b'
    foreach ($match in $rendererPeripheralPreparationLeak) {
        $violations += "main/board_port.c:$($match.LineNumber): touch/PMIC/IMU readiness must be requested through round_peripheral_service, not Audio HAL: $($match.Line.Trim())"
    }
    $audioPeripheralPreparationLeak = Select-String -Path (Join-Path $projectRoot 'main/round_audio_service.h') -Pattern '\bround_audio_service_prepare_peripherals\b'
    foreach ($match in $audioPeripheralPreparationLeak) {
        $violations += "main/round_audio_service.h:$($match.LineNumber): peripheral readiness is owned by round_peripheral_service; Audio exposes only audio-session contracts: $($match.Line.Trim())"
    }
}

# A foreground stream/playback session owns the physical Audio HAL lease and
# its task identity.  Scene code may choose when to begin/end a session, but
# cannot retain those handles or stop flags alongside UI state.
$roundAudioSessionLeak = Select-String -Path $roundRenderer -Pattern '\bs_audio_(?:stream_owned|playback_owner|playback_stop_requested)\b'
foreach ($match in $roundAudioSessionLeak) {
    $violations += "main/board_port.c:$($match.LineNumber): foreground audio session lifecycle must be owned by round_audio_service: $($match.Line.Trim())"
}
if (Select-String -Path $roundRenderer -Pattern '\b(?:wire|capture_mono)\s*\[\s*(?:512|256)\s*\]' -Quiet) {
    $violations += 'main/board_port.c: foreground capture wire buffers must be owned by round_audio_service'
}
foreach ($leakedWakeCaptureApi in @(
    'round_audio_service_allocate_wake_capture_buffer',
    'round_audio_service_free_wake_capture_buffer',
    'round_audio_service_capture_wire_bytes',
    'round_audio_service_extract_capture_mono',
    'round_audio_service_wake_pcm_(?:reset|process)'
)) {
    $wakeCaptureLeak = Select-String -Path $roundRenderer -Pattern "\b${leakedWakeCaptureApi}\b"
    foreach ($match in $wakeCaptureLeak) {
        $violations += "main/board_port.c:$($match.LineNumber): wake PCM buffer, wire format and conditioning must be owned by round_audio_service: $($match.Line.Trim())"
    }
}

# Only semantic Audio-HAL operations are visible to the shared renderer.
# Codec initialization, I2S locking, PCM wire transactions and analogue PA
# sequencing must remain private implementation details; otherwise a later
# board profile forces a business-code edit merely to change hardware.
foreach ($leakedAudioTransactionApi in @(
    'round_audio_service_initialize',
    'round_audio_service_(?:acquire|release_ownership)',
    'round_audio_service_(?:read|write)_pcm',
    'round_audio_service_playback_(?:prepare|reveal|abort|finish)',
    'round_audio_service_(?:set_power_amplifier|restore_input_gain)'
)) {
    $audioTransactionLeak = Select-String -Path $roundRenderer -Pattern "\b${leakedAudioTransactionApi}\b"
    foreach ($match in $audioTransactionLeak) {
        $violations += "main/board_port.c:$($match.LineNumber): low-level codec/I2S transaction must be owned by round_audio_service: $($match.Line.Trim())"
    }
}

# Audio session offsets are normalized source-frame counts.  Do not let a
# future edit reintroduce byte-count advancement after an adapter writes its
# physical stereo slots; that corrupts multi-block playback on either round
# board while still looking superficially plausible in a short acknowledgement.
if (Select-String -Path (Join-Path $projectRoot 'main/round_audio_service.c') -Pattern '\boffset\s*\+=\s*written\s*;' -Quiet) {
    $violations += 'main/round_audio_service.c: playback source-frame offset must not advance by physical byte count'
}

# The round standby animator is a Display-HAL lifecycle resource.  The scene
# renderer may provide its composition callback and decide admission, but task
# identity, wake notification and completion ownership live in the service so
# a new circular board only changes its profile adapter.
$roundDisplayLifecycleLeak = Select-String -Path $roundRenderer -Pattern '\bs_pet_animation_(?:task|stopped|stop_requested|started)\b'
foreach ($match in $roundDisplayLifecycleLeak) {
    $violations += "main/board_port.c:$($match.LineNumber): standby animation task lifecycle must be owned by round_display_service: $($match.Line.Trim())"
}
if (Select-String -Path $roundRenderer -Pattern '\bround_display_service_start_pet_animation_task\b' -Quiet) {
    $violations += 'main/board_port.c: renderer must use semantic Display animation lifecycle API, not profile task creation'
}
if (Select-String -Path $roundRenderer -Pattern '\bround_display_service_animation_wait_ticks\b|\bTickType_t\s+frame_delay\b|\bpdMS_TO_TICKS\s*\(\s*(?:RECORDING_RENDER_FRAME_MS|REMOTE_PET_RENDER_FRAME_MS|PET_ANIMATION_FRAME_MS)' -Quiet) {
    $violations += 'main/board_port.c: round animation cadence must use milliseconds through round_display_service, not FreeRTOS tick conversion'
}
if (Select-String -Path (Join-Path $projectRoot 'main/round_display_service.h') -Pattern 'freertos/|\bTickType_t\b|animation_wait_ticks' -Quiet) {
    $violations += 'main/round_display_service.h: private renderer-facing Display contract must not expose FreeRTOS tick types'
}
if (Select-String -Path $roundRenderer -Pattern '\bvTaskDelete(?:WithCaps)?\s*\(' -Quiet) {
    $violations += 'main/board_port.c: round task deletion must be owned by its private lifecycle service'
}

# Compact profiles share the same rule.  Bread/Fangtang scene composition may
# decide whether a decorative animation is admitted, but its FreeRTOS task,
# stop notification and completion semaphore are Display-HAL resources.
$compactRenderer = Join-Path $projectRoot 'main/compact_renderer.c'
# The shared UI / Power Service owns the DISPLAY_OFF deadline.  A renderer
# can accept or reject the physical transition for its current scene, but a
# second renderer-local deadline would diverge after a GUI timeout change and
# force a new board to duplicate power policy.
$rendererPowerDeadlineLeak = 's_idle_pet_sleep_expires_us|IDLE_PET_SLEEP_TIMEOUT_US'
foreach ($rendererPowerOwner in @(
    @{ Path = $compactRenderer; Name = 'main/compact_renderer.c' },
    @{ Path = $roundRenderer; Name = 'main/board_port.c' }
)) {
    if (Select-String -Path $rendererPowerOwner.Path -Pattern $rendererPowerDeadlineLeak -Quiet) {
        $violations += "$($rendererPowerOwner.Name): ambient DISPLAY_OFF deadline must be owned by app_ui/power_service, not renderer state"
    }
}
$compactDisplayLifecycleLeak = Select-String -Path $compactRenderer -Pattern '\bs_(?:remote_pet_animation|thinking_mouth)_(?:task|stopped|stop_requested)\b'
foreach ($match in $compactDisplayLifecycleLeak) {
    $violations += "main/compact_renderer.c:$($match.LineNumber): compact animation task lifecycle must be owned by compact_display_service: $($match.Line.Trim())"
}
if (Select-String -Path $compactRenderer -Pattern '\bcompact_display_service_start_(?:thinking|pet)_animation_task\b' -Quiet) {
    $violations += 'main/compact_renderer.c: renderer must use semantic compact Display animation lifecycle API, not profile task creation'
}
# Visual layout describes safe areas and typography only. DMA staging/stripe
# height is a physical Display-HAL transport fact, otherwise a new compact
# panel has to edit its standby scene just to qualify a different transfer
# bound. Keep the renderer on the semantic Display service scalar and reject
# reintroducing the old layout leak.
if (Select-String -Path $compactRenderer -Pattern 'STANDBY_LAYOUT->transfer_stripe_rows|compact_standby_layout_t[^\r\n]*transfer_stripe_rows' -Quiet) {
    $violations += 'main/compact_renderer.c: display transfer stripe height must come from compact_display_service, not standby layout'
}
if (Select-String -Path (Join-Path $projectRoot 'main/boards/compact_standby_layout.h') -Pattern '\btransfer_stripe_rows\b' -Quiet) {
    $violations += 'main/boards/compact_standby_layout.h: visual layout must not expose display DMA/transfer stripe height'
}
foreach ($compactLayoutAdapter in @(
    'main/boards/bread_compact/bread_standby_layout_adapter.h',
    'main/boards/fangtang_4g/fangtang_standby_layout_adapter.h'
)) {
    if (Select-String -Path (Join-Path $projectRoot $compactLayoutAdapter) -Pattern '\b(?:TRANSFER_STRIPE_ROWS|transfer_stripe_rows)\b' -Quiet) {
        $violations += "${compactLayoutAdapter}: visual adapter must not select display transfer stripe height"
    }
}
if (-not (Select-String -Path (Join-Path $projectRoot 'main/compact_renderer.c') -Pattern 'compact_display_service_transfer_stripe_rows\s*\(' -Quiet)) {
    $violations += 'main/compact_renderer.c: must obtain transport stripe height through compact_display_service'
}
# Round panels follow the same split.  The visual profile owns aperture-safe
# scene layout and font raster choices; controller geometry, DMA stripe budget
# and qualified animation cadence are physical Display-HAL facts.  Keep this
# enforcement symmetric with compact profiles so a future circular board
# cannot smuggle a bus limitation back through a layout/profile struct.
if (Select-String -Path $roundRenderer -Pattern 'round_visual_profile_(?:display_width|display_height|transfer_stripe_rows|pet_animation_frame_ms)\s*\(' -Quiet) {
    $violations += 'main/board_port.c: round physical display facts must come from round_display_service, not visual profile'
}
if (-not (Select-String -Path $roundRenderer -Pattern 'round_display_service_(?:width|height|transfer_stripe_rows|pet_animation_frame_ms)\s*\(' -Quiet)) {
    $violations += 'main/board_port.c: must obtain round physical display facts through round_display_service'
}
$roundVisualService = Join-Path $projectRoot 'main/round_visual_profile_service.c'
$roundVisualHeader = Join-Path $projectRoot 'main/round_visual_profile_service.h'
$roundVisualAdapter = Join-Path $projectRoot 'main/boards/round_visual_profile_adapter.h'
if (Select-String -Path @($roundVisualService, $roundVisualHeader, $roundVisualAdapter) -Pattern '\b(?:round_display_profile_t|round_visual_adapter_display|transfer_stripe_rows|pet_animation_frame_ms)\b' -Quiet) {
    $violations += 'round visual profile: must not expose physical panel transport/cadence facts'
}
if (-not (Select-String -Path (Join-Path $projectRoot 'main/round_display_service.c') -Pattern 'round_display_adapter_(?:width|height|transfer_stripe_rows|pet_animation_frame_ms)\s*\(' -Quiet)) {
    $violations += 'main/round_display_service.c: must own normalized physical display facts from selected adapter'
}
# A scene framebuffer is a renderer resource. It must never leak into the
# panel-initialization transaction merely because today's round boards use
# a related DMA stripe size. A future controller may qualify a different
# transfer bound without changing shared scene composition.
if (Select-String -Path $roundRenderer -Pattern 'round_display_service_initialize\s*\(\s*LCD_FRAMEBUFFER_BYTES' -Quiet) {
    $violations += 'main/board_port.c: renderer framebuffer bytes must not configure round Display HAL initialization'
}
if (Select-String -Path (Join-Path $projectRoot 'main/round_display_service.h'), (Join-Path $projectRoot 'main/round_display_service.c') -Pattern 'round_display_service_initialize\s*\(\s*size_t' -Quiet) {
    $violations += 'round_display_service: display initialization must not accept renderer transfer/framebuffer byte capacity'
}
foreach ($roundDisplayAdapter in @(
    'main/boards/echoear_2st/echoear_hardware_adapter.h',
    'main/boards/waveshare_amoled_1_75c/waveshare_display_adapter.h'
)) {
    $roundDisplayAdapterPath = Join-Path $projectRoot $roundDisplayAdapter
    if (Select-String -Path $roundDisplayAdapterPath -Pattern 'round_display_adapter_init_hardware\s*\(\s*size_t|round_display_adapter_bus_config\s*\(\s*size_t' -Quiet) {
        $violations += "${roundDisplayAdapter}: adapter must derive its own physical transfer capacity"
    }
}
# The compact wake recognizer predates the compact wake service and remains a
# separate HAL migration slice.  Restrict this guard to the two decorative
# worker functions above so it does not mistake that recognizer's cleanup for
# display animation ownership.
$compactAnimationDeleteLeak = Select-String -Path $compactRenderer -Pattern '\b(?:remote_pet_animation_task|thinking_mouth_task)\b[\s\S]{0,900}?\bvTaskDelete(?:WithCaps)?\s*\('
foreach ($match in $compactAnimationDeleteLeak) {
    $violations += "main/compact_renderer.c:$($match.LineNumber): compact Display animation task deletion must be owned by compact_display_service"
}

# Decorative callbacks express their wait in milliseconds through the private
# Display HAL.  Letting one callback consume a task notification itself would
# duplicate the task-stop protocol and leak tick conversion back into shared
# scene code whenever a new compact panel is added.
$compactAnimationWaitLeak = Select-String -Path $compactRenderer -Pattern '\b(?:remote_pet_animation_task|thinking_mouth_task)\b[\s\S]{0,1200}?\b(?:ulTaskNotifyTake|pdMS_TO_TICKS)\s*\('
foreach ($match in $compactAnimationWaitLeak) {
    $violations += "main/compact_renderer.c:$($match.LineNumber): compact Display animation waits must be owned by compact_display_service"
}

# Startup rollback is allowed to arrive before an optional decorative worker
# exists.  Both renderers must make that state an idempotent success: reporting
# a timeout would incorrectly prevent an independent partially initialized
# Input/Audio/Peripheral HAL owner from completing its own rollback.
foreach ($rendererStopContract in @(
    @{ Path = $compactRenderer; Name = 'main/compact_renderer.c' },
    @{ Path = $roundRenderer; Name = 'main/board_port.c' }
)) {
    $stopContract = [regex]::Match(
        (Get-Content -LiteralPath $rendererStopContract.Path -Raw),
        '(?s)esp_err_t\s+board_background_lifecycle_stop\s*\(\s*uint32_t\s+timeout_ms\s*\)\s*\{.*?\n\}')
    if (-not $stopContract.Success -or
        $stopContract.Value -notmatch 'if\s*\(\s*!s_background_tasks_lock\s*\)\s*return\s+ESP_OK\s*;') {
        $violations += "$($rendererStopContract.Name): background-task stop must be an idempotent no-op before optional worker admission"
    }
}

# Compact wake recognition has the same lifecycle boundary as the circular
# profiles: task publication, ESP-SR/MultiNet runtime, pause acknowledgements
# and stop generations live in a private Wake Service. The renderer may only
# start/pause/stop semantic wake detection.
$compactWakeService = 'main/compact_wake_service.c'
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot $compactWakeService))) {
    $violations += "${compactWakeService}: private compact wake lifecycle service is missing"
} else {
    $compactWakeLifecycleLeak = Select-String -Path $compactRenderer -Pattern '\bs_wake_(?:task|task_starting|ready|paused|pause_acknowledged|stop_requested|lock|cb|arg)\b'
    foreach ($match in $compactWakeLifecycleLeak) {
        $violations += "main/compact_renderer.c:$($match.LineNumber): compact wake lifecycle must be owned by compact_wake_service: $($match.Line.Trim())"
    }
    if (Select-String -Path $compactRenderer -Pattern '\bcompact_audio_service_start_wake_recognizer_task\b|\besp_mn_[A-Za-z0-9_]+\b|\besp_srmodel_[A-Za-z0-9_]+\b|\besp_srmodel\b|\bmodel_iface_data_t\b|\bsrmodel_list_t\b|\bwake_word_task\b|\bcompact_wake_service_recognizer_[A-Za-z0-9_]+\b|\bvTaskDelete(?:WithCaps)?\s*\(' -Quiet) {
        $violations += 'main/compact_renderer.c: compact wake task/model lifecycle must be owned by compact_wake_service'
    }
    $compactWakeHeaderLeak = Select-String -Path (Join-Path $projectRoot 'main/compact_wake_service.h') -Pattern 'freertos/|\b(?:TaskHandle_t|TaskFunction_t|srmodel_list_t|esp_mn_)\b'
    foreach ($match in $compactWakeHeaderLeak) {
        $violations += "main/compact_wake_service.h:$($match.LineNumber): renderer-facing compact Wake contract must not expose RTOS or ESP-SR implementation types: $($match.Line.Trim())"
    }
    $compactAudioWakeTaskLeak = Select-String -Path (Join-Path $projectRoot 'main/compact_audio_service.h') -Pattern '\bcompact_audio_service_start_wake_recognizer_task\b|freertos/task\.h|\b(?:TaskHandle_t|TaskFunction_t)\b'
    foreach ($match in $compactAudioWakeTaskLeak) {
        $violations += "main/compact_audio_service.h:$($match.LineNumber): Wake task placement must be owned by compact_wake_service, and Audio's renderer-facing contract must not expose RTOS task types: $($match.Line.Trim())"
    }
    foreach ($compactAudioAdapter in @('main/boards/bread_compact/bread_audio_adapter.h', 'main/boards/fangtang_4g/fangtang_audio_adapter.h')) {
        $compactAudioAdapterWakeTaskLeak = Select-String -Path (Join-Path $projectRoot $compactAudioAdapter) -Pattern '\bcompact_audio_adapter_start_wake_recognizer_task\b'
        foreach ($match in $compactAudioAdapterWakeTaskLeak) {
            $violations += "${compactAudioAdapter}:$($match.LineNumber): recognizer task placement must be owned by compact_wake_service: $($match.Line.Trim())"
        }
    }
}

# The Compact renderer must now follow the same audio-session rule as the
# circular renderer. Direct-I2S session admission, mutex ownership, playback
# task identity, wire-slot conversion and capture conditioning belong
# exclusively to compact_audio_service; renderer code owns only VAD and wake
# pause policy.
$compactAudioSessionLeak = Select-String -Path $compactRenderer -Pattern '\bs_(?:audio_(?:mutex|ready|stream_owned|playback_owner|playback_stop_requested)|command_capture_(?:active|stop_requested))\b'
foreach ($match in $compactAudioSessionLeak) {
    $violations += "main/compact_renderer.c:$($match.LineNumber): compact audio session lifecycle must be owned by compact_audio_service: $($match.Line.Trim())"
}
# Capture cancellation crosses the application/reader hand-off and can execute
# on a different core. It must remain an Audio-HAL atomic/session concern, not
# a renderer-owned volatile flag that a new microphone implementation has to
# remember to synchronize.
$roundCaptureSessionLeak = Select-String -Path $roundRenderer -Pattern '\bs_command_capture_(?:active|stop_requested)\b'
foreach ($match in $roundCaptureSessionLeak) {
    $violations += "main/board_port.c:$($match.LineNumber): round command-capture lifecycle must be owned by round_audio_service: $($match.Line.Trim())"
}
foreach ($leakedCompactAudioTransactionApi in @(
    'compact_audio_service_(?:initialize|read|write)',
    'compact_audio_service_(?:acquire|release|initialize_locked)',
    'compact_audio_adapter_[A-Za-z0-9_]+'
)) {
    $audioTransactionLeak = Select-String -Path $compactRenderer -Pattern "\b${leakedCompactAudioTransactionApi}\b"
    foreach ($match in $audioTransactionLeak) {
        $violations += "main/compact_renderer.c:$($match.LineNumber): low-level direct-I2S transaction must be owned by compact_audio_service: $($match.Line.Trim())"
    }
}
# Direct-I2S is deliberately hidden behind the service even inside the compact
# renderer. A renderer-side raw-slot shift, wake-buffer allocation, or private
# mean-level helper looks harmless but makes the next microphone topology a
# business-code change. The normalized capture statistics are the sole VAD
# input exposed above this boundary.
foreach ($compactPcmLeak in @(
    '\bint32_t\s+raw\s*\[',
    '\braw\s*\[[^\]]+\]\s*>>\s*14',
    '\bWAKE_WORD_INPUT_GAIN_(?:NUM|DEN)\b',
    '\bcompact_audio_(?:adapter_)?(?:allocate|free)_wake_capture_buffer\b',
    '\bcommand_capture_mean_level\s*\('
)) {
    $audioPcmLeak = Select-String -Path $compactRenderer -Pattern $compactPcmLeak
    foreach ($match in $audioPcmLeak) {
        $violations += "main/compact_renderer.c:$($match.LineNumber): raw PCM conversion, gain and capture buffers must be owned by compact_audio_service: $($match.Line.Trim())"
    }
}
# Round capture gets the same protection: mean-level and peak-to-level
# conversion are Audio-HAL facts, not business-renderer PCM processing.
foreach ($roundPcmLeak in @(
    '\bcommand_capture_mean_level\s*\(',
    '\bchunk_peak\s*<=\s*180',
    '\bmean_deviation\s*\*\s*1000'
)) {
    $audioPcmLeak = Select-String -Path $roundRenderer -Pattern $roundPcmLeak
    foreach ($match in $audioPcmLeak) {
        $violations += "main/board_port.c:$($match.LineNumber): round PCM statistics must be owned by round_audio_service: $($match.Line.Trim())"
    }
}

# Adapter include ownership is intentionally one-way.  Profile headers may
# include SDK drivers, but only their designated private service source may
# select them.  The two display alternatives naturally share one owner.
$adapterOwners = @{
    'boards/round_audio_codec_adapter.h' = @('main/round_audio_service.c')
    'boards/round_peripheral_adapter.h' = @('main/round_peripheral_service.c')
    'boards/round_display_adapter.h' = @('main/round_display_service.c')
    'boards/round_input_profile_adapter.h' = @('main/round_input_profile_service.c')
    'boards/compact_display_adapter.h' = @('main/compact_display_service.c')
    'boards/compact_audio_adapter.h' = @('main/compact_audio_service.c')
    'boards/compact_input_adapter.h' = @('main/compact_input_service.c')
    'boards/compact_connectivity_adapter.h' = @('main/compact_connectivity_service.c')
    'boards/compact_peripheral_adapter_selector.h' = @('main/compact_peripheral_service.c')
    'boards/compact_visual_profile_adapter.h' = @('main/compact_visual_profile_service.c')
    'boards/echoear_2st/echoear_hardware_adapter.h' = @('main/boards/round_display_adapter.h')
    'boards/waveshare_amoled_1_75c/waveshare_display_adapter.h' = @('main/boards/round_display_adapter.h')
    'boards/bread_compact/bread_audio_adapter.h' = @('main/boards/compact_audio_adapter.h')
    'boards/fangtang_4g/fangtang_audio_adapter.h' = @('main/boards/compact_audio_adapter.h')
    'boards/bread_compact/bread_display_adapter.h' = @('main/boards/compact_display_adapter.h')
    'boards/fangtang_4g/fangtang_display_adapter.h' = @('main/boards/compact_display_adapter.h')
    'boards/bread_compact/bread_input_adapter.h' = @('main/boards/compact_input_adapter.h')
    'boards/fangtang_4g/fangtang_input_adapter.h' = @('main/boards/compact_input_adapter.h')
    'boards/bread_compact/bread_connectivity_adapter.h' = @('main/boards/compact_connectivity_adapter.h')
    'boards/fangtang_4g/fangtang_cellular_adapter.h' = @('main/boards/compact_connectivity_adapter.h')
    'boards/fangtang_4g/fangtang_ml307_transport.h' = @('main/boards/fangtang_4g/fangtang_cellular_adapter.h')
    'boards/bread_compact/bread_peripheral_adapter.h' = @('main/boards/compact_peripheral_adapter_selector.h')
    'boards/fangtang_4g/fangtang_peripheral_adapter.h' = @('main/boards/compact_peripheral_adapter_selector.h')
    'boards/round_visual_profile_adapter.h' = @('main/round_visual_profile_service.c')
    'boards/bread_compact/bread_recording_layout_adapter.h' = @('main/boards/compact_visual_profile_adapter.h')
    'boards/bread_compact/bread_response_layout_adapter.h' = @('main/boards/compact_visual_profile_adapter.h')
    'boards/bread_compact/bread_standby_layout_adapter.h' = @('main/boards/compact_visual_profile_adapter.h')
    'boards/bread_compact/bread_upload_layout_adapter.h' = @('main/boards/compact_visual_profile_adapter.h')
    'boards/bread_compact/bread_alarm_layout_adapter.h' = @('main/boards/compact_visual_profile_adapter.h')
    'boards/fangtang_4g/fangtang_recording_layout_adapter.h' = @('main/boards/compact_visual_profile_adapter.h')
    'boards/fangtang_4g/fangtang_response_layout_adapter.h' = @('main/boards/compact_visual_profile_adapter.h')
    'boards/fangtang_4g/fangtang_standby_layout_adapter.h' = @('main/boards/compact_visual_profile_adapter.h')
    'boards/fangtang_4g/fangtang_upload_layout_adapter.h' = @('main/boards/compact_visual_profile_adapter.h')
    'boards/fangtang_4g/fangtang_alarm_layout_adapter.h' = @('main/boards/compact_visual_profile_adapter.h')
}
foreach ($adapter in $adapterOwners.Keys) {
    $includePattern = [regex]::Escape($adapter)
    $references = @($allCFiles | Where-Object {
        Select-String -Path $_.FullName -Pattern $includePattern -Quiet
    })
    $actual = @($references | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    } | Sort-Object -Unique)
    $expected = @($adapterOwners[$adapter] | Sort-Object -Unique)
    if (($actual -join '|') -ne ($expected -join '|')) {
        $violations += "main/${adapter}: must be selected only by $($expected -join ', '); found: $($actual -join ', ')"
    }
}

# Durable-volume mounting and unmounting are physical Storage port concerns.
# The composition
# root may consume only Storage Service availability; it must not grow a second
# SPIFFS mount/format path simply because one current product calls it meeting
# storage.  The service itself likewise stays policy-only and delegates VFS,
# partition and blank-media facts to Platform Storage.
$storagePhysicalTokens = @(
    '\besp_vfs_spiffs_', '\besp_spiffs_', '\besp_partition_(find_first|read)',
    '\besp_partition_t\b', '\besp_vfs_spiffs_conf_t\b'
)
foreach ($token in $storagePhysicalTokens) {
    foreach ($source in @($allSourceFiles | Where-Object {
        $_.FullName -notmatch 'main[\\/]platform_storage\.c$' -and
        $_.FullName -notmatch 'main[\\/]platform_resource\.c$' -and
        (Select-String -Path $_.FullName -Pattern $token -Quiet)
    })) {
        $relative = $source.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
        $violations += "${relative}: SPIFFS/partition mount facts must be owned only by main/platform_storage.c"
    }
}
foreach ($forbiddenMainStorage in @(
    '\bmount_meeting_storage\s*\(', '\bmeeting_storage_partition_is_blank\s*\(',
    '\blog_storage_inventory\s*\('
)) {
    foreach ($match in Select-String -Path $mainConnectivitySource -Pattern $forbiddenMainStorage) {
        $violations += "main/main.c:$($match.LineNumber): storage lifecycle belongs to Storage Service: $($match.Line.Trim())"
    }
}
# NVS is a physical persistence port.  The composition root may consume only
# its normalized status, while Persistence Service owns request lifecycle and
# Platform NVS is the unique SDK owner.  This prevents an otherwise convenient
# direct NVS call from bypassing cache-safe stack routing or destructive-data
# recovery policy.
$platformNvsSource = 'main/platform_nvs.c'
$platformNvsHeader = 'main/platform_nvs.h'
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot $platformNvsSource)) -or
    -not (Test-Path -LiteralPath (Join-Path $projectRoot $platformNvsHeader))) {
    $violations += 'main/platform_nvs.[c|h]: Platform NVS source owner is missing'
} else {
    $nvsPhysicalTokens = @('\bnvs_flash_', '\bnvs_(open|close|get_|set_|commit|erase)',
                           '\bnvs_handle_t\b')
    foreach ($token in $nvsPhysicalTokens) {
        foreach ($source in @($allSourceFiles | Where-Object {
            $_.FullName -notmatch 'main[\\/]platform_nvs\.c$' -and
            (Select-String -Path $_.FullName -Pattern $token -Quiet)
        })) {
            $relative = $source.FullName.Substring($projectRoot.Length + 1).Replace('\\','/')
            $violations += "${relative}: NVS SDK access must be owned only by main/platform_nvs.c"
        }
    }
    foreach ($forbiddenMainNvs in @('\bplatform_nvs_(read|write)_[A-Za-z0-9_]+\s*\(',
                                    '\bnvs_flash_', '\bnvs_(open|close|get_|set_|commit|erase)')) {
        foreach ($match in Select-String -Path $mainConnectivitySource -Pattern $forbiddenMainNvs) {
            $violations += "main/main.c:$($match.LineNumber): main.c may only initialize/deinitialize Platform NVS: $($match.Line.Trim())"
        }
    }
    $nvsHeaderText = Get-Content -LiteralPath (Join-Path $projectRoot $platformNvsHeader) -Raw
    if ($nvsHeaderText -match 'esp_|freertos/|nvs_handle_t|SemaphoreHandle_t') {
        $violations += 'main/platform_nvs.h: Platform NVS public contract must not leak SDK/RTOS types'
    }
    $persistenceHeader = Join-Path $projectRoot 'main/persistence_service.h'
    if (-not (Test-Path -LiteralPath $persistenceHeader)) {
        $violations += 'main/persistence_service.h: Persistence Service public contract is missing'
    } else {
        $persistenceHeaderText = Get-Content -LiteralPath $persistenceHeader -Raw
        if ($persistenceHeaderText -match '\besp_err_t\b|\bESP_ERR_|\bnvs_|SemaphoreHandle_t|#include\s*[<"](?:esp_|freertos/|nvs\.h)') {
            $violations += 'main/persistence_service.h: Persistence Service public contract must expose only Device value types'
        }
    }
    # Persistence-backed domain services may keep FreeRTOS implementation
    # details in their .c files, but their public boundary must never force
    # a caller to understand ESP-IDF/NVS errors. Extend this allowlist only
    # after a service has completed its Device-status migration.
    foreach ($serviceHeader in @(
        'main/weather_cache_service.h',
        'main/wake_deadline_service.h',
        'main/meeting_recovery_service.h',
        'main/configuration_service.h'
    )) {
        $servicePath = Join-Path $projectRoot $serviceHeader
        if (-not (Test-Path -LiteralPath $servicePath)) {
            $violations += "${serviceHeader}: migrated Persistence domain contract is missing"
            continue
        }
        $serviceText = Get-Content -LiteralPath $servicePath -Raw
        if ($serviceText -match '\besp_err_t\b|\bESP_ERR_|\bnvs_|SemaphoreHandle_t|#include\s*[<"](?:esp_|freertos/|nvs\.h)') {
            $violations += "${serviceHeader}: public persistence-domain contract must expose only Device value types"
        }
    }

    # Sleep Schedule keeps the Tool Registry JSON/ESP-IDF compatibility API,
    # but lifecycle callers must consume only the shared Device status. Guard
    # those two declarations explicitly rather than incorrectly treating the
    # entire tool-protocol header as a pure value-type header.
    # Fall Detection is a hardware-independent Motion consumer. Its lifecycle
# must remain Device-status based; the one JSON Tool Registry method remains
# an explicit protocol compatibility boundary, like update and sleep schedule.
# Alarm lifecycle is shared schedule policy. Keep Wake Deadline/RTOS/NVS
# implementation errors private, while retaining its JSON tool protocol seam.
$alarmManagerHeader = Join-Path $projectRoot 'main/alarm_manager.h'
if (-not (Test-Path -LiteralPath $alarmManagerHeader)) {
    $violations += 'main/alarm_manager.h: Alarm Manager public contract is missing'
} else {
    $alarmManagerText = Get-Content -LiteralPath $alarmManagerHeader -Raw
    foreach ($lifecycleName in @('init', 'deinit')) {
        if ($alarmManagerText -notmatch "\bdevice_status_t\s+alarm_manager_${lifecycleName}\s*\(") {
            $violations += "main/alarm_manager.h: ${lifecycleName} must return device_status_t"
        }
        if ($alarmManagerText -match "\besp_err_t\s+alarm_manager_${lifecycleName}\s*\(") {
            $violations += "main/alarm_manager.h: ${lifecycleName} must not expose esp_err_t"
        }
    }
    if ($alarmManagerText -notmatch "\besp_err_t\s+alarm_manager_execute_tool\s*\(") {
        $violations += 'main/alarm_manager.h: Device Tool Registry compatibility method is missing'
    }
}
$fallDetectionHeader = Join-Path $projectRoot 'main/fall_detection_service.h'
if (-not (Test-Path -LiteralPath $fallDetectionHeader)) {
    $violations += 'main/fall_detection_service.h: Fall Detection public contract is missing'
} else {
    $fallDetectionText = Get-Content -LiteralPath $fallDetectionHeader -Raw
    foreach ($lifecycleName in @('init', 'deinit')) {
        if ($fallDetectionText -notmatch "\bdevice_status_t\s+fall_detection_service_${lifecycleName}\s*\(") {
            $violations += "main/fall_detection_service.h: ${lifecycleName} must return device_status_t"
        }
        if ($fallDetectionText -match "\besp_err_t\s+fall_detection_service_${lifecycleName}\s*\(") {
            $violations += "main/fall_detection_service.h: ${lifecycleName} must not expose esp_err_t"
        }
    }
    if ($fallDetectionText -notmatch "\besp_err_t\s+fall_detection_service_execute_tool\s*\(") {
        $violations += 'main/fall_detection_service.h: Device Tool Registry compatibility method is missing'
    }
    if ($fallDetectionText -notmatch 'Hardware-independent suspected-fall classifier') {
        $violations += 'main/fall_detection_service.h: Fall Detection must remain a shared Motion consumer'
    }
}
# MP3 playback is a shared Audio-domain decoder.  The public result must use
# Device status; ESP Audio decoder codes may not escape into Gateway policy.
$mp3PlayerHeader = Join-Path $projectRoot 'main/mp3_player.h'
if (-not (Test-Path -LiteralPath $mp3PlayerHeader)) {
    $violations += 'main/mp3_player.h: MP3 Player public contract is missing'
} else {
    $mp3PlayerText = Get-Content -LiteralPath $mp3PlayerHeader -Raw
    if ($mp3PlayerText -notmatch "\bdevice_status_t\s+mp3_player_play\s*\(") {
        $violations += 'main/mp3_player.h: mp3_player_play must return device_status_t'
    }
    if ($mp3PlayerText -match "\besp_err_t\s+mp3_player_play\s*\(") {
        $violations += 'main/mp3_player.h: mp3_player_play must not expose esp_err_t'
    }
}
# Firmware Identity is a shared diagnostic/business snapshot. Its public API
# must expose only normalized Device status; USB Serial/JTAG, FreeRTOS tasks and
# Task Registry callbacks remain private implementation details.
$firmwareIdentityHeader = Join-Path $projectRoot 'main/firmware_identity.h'
if (-not (Test-Path -LiteralPath $firmwareIdentityHeader)) {
    $violations += 'main/firmware_identity.h: Firmware Identity public contract is missing'
} else {
    $firmwareIdentityText = Get-Content -LiteralPath $firmwareIdentityHeader -Raw
    foreach ($operation in @('start', 'stop', 'get')) {
        if ($firmwareIdentityText -notmatch "\bdevice_status_t\s+firmware_identity_${operation}\s*\(") {
            $violations += "main/firmware_identity.h: ${operation} must return device_status_t"
        }
        if ($firmwareIdentityText -match "\besp_err_t\s+firmware_identity_${operation}\s*\(") {
            $violations += "main/firmware_identity.h: ${operation} must not expose esp_err_t"
        }
    }
}
$sleepScheduleHeader = Join-Path $projectRoot 'main/sleep_schedule_service.h'
    if (-not (Test-Path -LiteralPath $sleepScheduleHeader)) {
        $violations += 'main/sleep_schedule_service.h: Sleep Schedule public contract is missing'
    } else {
        $sleepScheduleText = Get-Content -LiteralPath $sleepScheduleHeader -Raw
        foreach ($lifecycleName in @('init', 'deinit')) {
            if ($sleepScheduleText -notmatch "\bdevice_status_t\s+sleep_schedule_service_${lifecycleName}\s*\(") {
                $violations += "main/sleep_schedule_service.h: sleep schedule ${lifecycleName} must return device_status_t"
            }
    if ($sleepScheduleText -match "\besp_err_t\s+sleep_schedule_service_${lifecycleName}\s*\(") {
        $violations += "main/sleep_schedule_service.h: sleep schedule ${lifecycleName} must not expose esp_err_t"
    }
        }
    }

    # A once-only rest window must be durable policy, not an eternal stale
    # record after its end boundary. Keep expiry in the shared Sleep Schedule
    # service; a profile must never compensate through panel/timer branches.
    $sleepScheduleSource = Join-Path $projectRoot 'main/sleep_schedule_service.c'
    if (-not (Test-Path -LiteralPath $sleepScheduleSource)) {
        $violations += 'main/sleep_schedule_service.c: Sleep Schedule implementation is missing'
    } else {
        $sleepScheduleSourceText = Get-Content -LiteralPath $sleepScheduleSource -Raw
    foreach ($onceRetirementRequirement in @(
            'static\s+bool\s+retire_expired_once_schedule_locked\s*\(\s*int64_t\s+now_epoch\s*\)',
            'schedule->mode\s*!=\s*SLEEP_SCHEDULE_MODE_ONCE',
            'now_epoch\s*\*\s*1000LL\s*<\s*schedule->once_end_epoch_ms',
            'schedule->enabled\s*=\s*false',
            'schedule->revision\+\+',
            'persist_locked\s*\(\s*\)',
            'arm_retirement_retry_locked\s*\('
        )) {
            if ($sleepScheduleSourceText -notmatch $onceRetirementRequirement) {
                $violations += "main/sleep_schedule_service.c: durable one-shot schedule retirement is incomplete (${onceRetirementRequirement})"
            }
        }
    }

    # A schedule-end panel wake consumes the old Power deadline. The shared UI
    # must be notified through composition, not by letting Sleep Schedule
    # reach a renderer/profile; otherwise ambient repaint bookkeeping can stay
    # stale and prevent the next normal idle timeout on every board.
    foreach ($scheduleWakeRequirement in @(
            'sleep_schedule_display_wake_observer_t',
            'sleep_schedule_service_set_display_wake_observer\s*\(',
            's_schedule_display_off_requested',
            's_wall_clock_update_pending',
            's_policy_change_pending',
            'static\s+bool\s+schedule_display_off_marker_reconciled\s*\(',
            'schedule_display_off_marker_reconciled\s*\(wall_clock_updated,\s*policy_changed\)',
            'run_end_handoff_marker_lifecycle_test\s*\(',
            'device_power_wake_display_from_schedule\s*\(',
            'if\s*\(observer\)\s*observer\(wake_status,\s*observer_context\)'
        )) {
        if ($sleepScheduleSourceText -notmatch $scheduleWakeRequirement) {
            $violations += "main/sleep_schedule_service.c: schedule-end UI handoff is incomplete (${scheduleWakeRequirement})"
        }
    }
    if ($sleepScheduleSourceText -match '#include\s*[<"]app_ui\.h[>"]|#include\s*[<"](?:boards|compact_renderer|echoear_renderer|waveshare_renderer)') {
        $violations += 'main/sleep_schedule_service.c: Schedule policy must notify composition, not include UI or profile renderers'
    }
    $appUiSource = Join-Path $projectRoot 'main/app_ui.c'
    if (-not (Test-Path -LiteralPath $appUiSource) -or
        -not ((Get-Content -LiteralPath $appUiSource -Raw) -match 'void\s+app_ui_note_schedule_display_wake\s*\(')) {
        $violations += 'main/app_ui.c: schedule-end wake must reconcile the ambient idle owner'
    }
    if (-not (Select-String -Path $mainConnectivitySource -Pattern 'sleep_schedule_service_set_display_wake_observer\s*\(\s*on_schedule_display_wake' -Quiet)) {
        $violations += 'main/main.c: composition root must install schedule-end UI observer'
    }
    $powerServiceForScheduleWake = Join-Path $projectRoot 'main/power_service.c'
    if (-not (Test-Path -LiteralPath $powerServiceForScheduleWake) -or
        -not ((Get-Content -LiteralPath $powerServiceForScheduleWake -Raw) -match
              'platform_power_display_is_off\s*\(')) {
        $violations += 'main/power_service.c: schedule wake must treat an already-active panel as a successful deadline reconciliation'
    }

    # DISPLAY_OFF is still a running-MCU panel transition, yet its one-shot
    # ESP timer may fire while a synchronized display owner temporarily holds
    # the transition mutex. The shared Power Service must retain that pending
    # request with a monotonic generation/due-time check; a later cancel, wake
    # or rearm must make every stale callback/retry harmless on all profiles.
    $powerServiceSource = Join-Path $projectRoot 'main/power_service.c'
    if (-not (Test-Path -LiteralPath $powerServiceSource)) {
        $violations += 'main/power_service.c: shared Power Service implementation is missing'
    } else {
        $powerServiceText = Get-Content -LiteralPath $powerServiceSource -Raw
        foreach ($displayOffRetryRequirement in @(
            'static\s+uint32_t\s+s_display_off_generation\s*;',
            'static\s+int64_t\s+s_display_off_due_us\s*;',
            'static\s+bool\s+s_display_off_retry_pending\s*;',
            's_display_off_generation\s*==\s*generation',
            's_display_off_due_us\s*==\s*due_us',
            's_display_off_due_us\s*=\s*0\s*;',
            's_display_off_retry_pending\s*=\s*true',
            'esp_timer_get_time\s*\(\s*\)\s*>=\s*due_us',
            '\bpower_service_run_display_off_retry_hil_test\s*\('
        )) {
            if ($powerServiceText -notmatch $displayOffRetryRequirement) {
                $violations += "main/power_service.c: stale-safe DISPLAY_OFF retry is incomplete (${displayOffRetryRequirement})"
            }
        }
    }

    foreach ($source in @($allSourceFiles | Where-Object {
        $_.FullName -notmatch 'main[\\/]platform_nvs\.c$' -and
        (Select-String -Path $_.FullName -Pattern '#include\s*[<"]nvs\.h[>"]|\bESP_ERR_NVS_' -Quiet)
    })) {
        $relative = $source.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
        $violations += "${relative}: NVS-specific SDK errors/includes must remain only in main/platform_nvs.c"
    }
}

# ML307 is a Fangtang-specific physical Connectivity port.  Keeping both its
# implementation and contract beneath the Fangtang adapter prevents a future
# compact renderer or shared Connectivity facade from acquiring a direct modem
# / UART dependency just because this board happens to provide cellular.
$fangtangMl307Header = Join-Path $projectRoot 'main/boards/fangtang_4g/fangtang_ml307_transport.h'
$fangtangMl307Source = Join-Path $projectRoot 'main/boards/fangtang_4g/fangtang_ml307_transport.cpp'
$legacyMl307Paths = @(
    (Join-Path $projectRoot 'main/ml307_transport.h'),
    (Join-Path $projectRoot 'main/ml307_transport.cpp')
)
foreach ($legacyMl307Path in $legacyMl307Paths) {
    if (Test-Path -LiteralPath $legacyMl307Path) {
        $violations += "$($legacyMl307Path.Substring($projectRoot.Length + 1)): Fangtang-only ML307 transport must live below boards/fangtang_4g"
    }
}
if (-not (Test-Path -LiteralPath $fangtangMl307Header) -or
    -not (Test-Path -LiteralPath $fangtangMl307Source)) {
    $violations += 'boards/fangtang_4g: Fangtang ML307 Connectivity port source/header are missing'
} else {
    $fangtangMl307Text = Get-Content -LiteralPath $fangtangMl307Source -Raw
    $ml307HeaderReferences = @($allSourceFiles | Where-Object {
        Select-String -Path $_.FullName -Pattern 'fangtang_ml307_transport\.h' -Quiet
    })
    $ml307HeaderActual = @($ml307HeaderReferences | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    } | Sort-Object -Unique)
    # The C++ transport and the adapter are the only legal consumers of this
    # private profile-port contract.
    $ml307HeaderExpected = @('main/boards/fangtang_4g/fangtang_cellular_adapter.h',
                             'main/boards/fangtang_4g/fangtang_ml307_transport.cpp')
    if (($ml307HeaderActual -join '|') -ne ($ml307HeaderExpected -join '|')) {
        $violations += "main/boards/fangtang_4g/fangtang_ml307_transport.h: may be included only by Fangtang cellular adapter/port; found: $($ml307HeaderActual -join ', ')"
    }
    $ml307DirectCalls = @($allSourceFiles | Where-Object {
        $_.FullName -ne $fangtangMl307Source -and
        $_.FullName -notmatch 'fangtang_cellular_adapter\.h$' -and
        $_.FullName -notmatch 'fangtang_ml307_transport\.h$' -and
        (Select-String -Path $_.FullName -Pattern '\bml307_transport_[A-Za-z0-9_]+' -Quiet)
    })
    foreach ($reference in $ml307DirectCalls) {
        $relative = $reference.FullName.Substring($projectRoot.Length + 1).Replace('\\','/')
        $violations += "${relative}: ML307 operations must be reached only through the Fangtang cellular adapter"
    }
    # System Sleep must close this concrete UART/probe/HTTP port below the
    # neutral Connectivity bridge.  The private PREPARE may fail after closing
    # admission, but it must never reopen it itself: Power's reverse rollback
    # reaches the adapter's paired ABORT through Connectivity Service.
    foreach ($ml307SleepRequirement in @(
            'extern\s+"C"\s+esp_err_t\s+ml307_transport_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
            'extern\s+"C"\s+void\s+ml307_transport_abort_system_sleep_prepare\s*\(\s*void\s*\)',
            's_system_sleep_preparing\s*=\s*true',
            'close_transport_and_drain\s*\(\s*timeout_ms\s*\)',
            'ML307 System Sleep PREPARE did not reach a safe point',
            's_admission_open\.store\(true\)',
            'ML307 System Sleep ABORT restored transport admission')) {
        if ($fangtangMl307Text -notmatch $ml307SleepRequirement) {
            $violations += "main/boards/fangtang_4g/fangtang_ml307_transport.cpp: ML307 System Sleep transaction is incomplete (${ml307SleepRequirement})"
        }
    }
    $ml307SleepPreparePattern = 'extern\s+"C"\s+esp_err_t\s+ml307_transport_prepare_system_sleep' +
                                '\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*extern\s+"C"\s+void\s+' +
                                'ml307_transport_abort_system_sleep_prepare'
    $ml307SleepPrepareMatch = [regex]::Match($fangtangMl307Text, $ml307SleepPreparePattern)
    if (-not $ml307SleepPrepareMatch.Success) {
        $violations += 'main/boards/fangtang_4g/fangtang_ml307_transport.cpp: ML307 System Sleep PREPARE/ABORT boundary cannot be inspected'
    } else {
        $ml307SleepPrepareBody = $ml307SleepPrepareMatch.Groups[1].Value
        if ($ml307SleepPrepareBody -match '\bml307_transport_abort_system_sleep_prepare\s*\(' -or
            $ml307SleepPrepareBody -match 's_system_sleep_preparing\s*=\s*false' -or
            $ml307SleepPrepareBody -match 's_admission_open\.store\(true\)') {
            $violations += 'main/boards/fangtang_4g/fangtang_ml307_transport.cpp: ML307 System Sleep PREPARE failure must retain transport admission closed until Connectivity/Power ABORT'
        }
    }
}
# Every production image must expose Bread's business baseline.  Profiles may
# append only adaptation facts (touch, round aperture, modem, battery, IMU),
# never restate or accidentally omit an individual business feature.  Keep
# this source-level gate alongside the ABI validator so a new board fails both
# CI and runtime profile publication if it drifts from AgentOS parity.
$productionProfiles = @(
    'main/boards/bread_compact/board_profile.c',
    'main/boards/fangtang_4g/board_profile.c',
    'main/boards/echoear_2st/board_profile.c',
    'main/boards/waveshare_amoled_1_75c/board_profile.c'
)

# Phase 7B begins with a strict separation between normalized wake policy and
# electrical wake configuration.  Shared Device/Wake headers must remain ISO-C
# value-type contracts; only the selected profile translation unit may name a
# GPIO, RTC capability, touch controller, ESP sleep API or FreeRTOS object.
$wakeHeaders = @('main/platform_wake.h', 'main/platform_wake_profile.h',
                 'main/wake_service.h')
foreach ($wakeHeader in $wakeHeaders) {
    $wakePath = Join-Path $projectRoot $wakeHeader
    if (-not (Test-Path -LiteralPath $wakePath)) {
        $violations += "${wakeHeader}: Wake HAL/Service public contract is missing"
        continue
    }
    $wakeText = Get-Content -LiteralPath $wakePath -Raw
    if ($wakeText -match '\besp_|\bgpio_|\bRTC_|SemaphoreHandle_t|TaskHandle_t|#include\s*[<"](?:esp_|freertos/)') {
        $violations += "${wakeHeader}: Wake HAL/Service public contract must not leak SDK/RTOS/GPIO details"
    }
}
$platformWakeSource = Join-Path $projectRoot 'main/platform_wake.c'
$wakeCapabilityHostCheck = Join-Path $projectRoot 'tools/check-wake-capability.ps1'
$wakeCapabilityHostTest = Join-Path $projectRoot 'tools/host_tests/test_wake_capability.c'
$wakeProfileMatrixHostCheck = Join-Path $projectRoot 'tools/check-wake-profile-matrix.ps1'
$wakeProfileMatrixHostTest = Join-Path $projectRoot 'tools/host_tests/test_wake_profile_matrix.c'
$alarmWakePlanHostCheck = Join-Path $projectRoot 'tools/check-alarm-wake-plan.ps1'
$alarmWakePlanHostTest = Join-Path $projectRoot 'tools/host_tests/test_alarm_wake_plan.c'
$powerFailClosedHostCheck = Join-Path $projectRoot 'tools/check-platform-power-fail-closed.ps1'
$powerFailClosedHostTest = Join-Path $projectRoot 'tools/host_tests/test_platform_power_fail_closed.c'
$powerWakeAuthorizationHostCheck = Join-Path $projectRoot 'tools/check-platform-power-wake-authorization.ps1'
$powerWakeAuthorizationHostTest = Join-Path $projectRoot 'tools/host_tests/test_platform_power_wake_authorization.c'
$motionHalContractHostCheck = Join-Path $projectRoot 'tools/check-motion-hal-contract.ps1'
$motionHalContractHostTest = Join-Path $projectRoot 'tools/host_tests/test_motion_hal_contract.c'
$deviceProfileValidationHostCheck = Join-Path $projectRoot 'tools/check-device-profile-validation.ps1'
$deviceProfileValidationHostTest = Join-Path $projectRoot 'tools/host_tests/test_device_profile_validation.c'
$officialDeviceProfilesHostCheck = Join-Path $projectRoot 'tools/check-official-device-profiles.ps1'
$officialDeviceProfilesHostTest = Join-Path $projectRoot 'tools/host_tests/test_official_device_profiles.c'
$referenceFakeProfileHostCheck = Join-Path $projectRoot 'tools/check-reference-fake-profile.ps1'
$platformStorageContractHostCheck = Join-Path $projectRoot 'tools/check-platform-storage-contract.ps1'
$platformStorageContractHostTest = Join-Path $projectRoot 'tools/host_tests/test_platform_storage_contract.c'
$storageServiceLifecycleHostCheck = Join-Path $projectRoot 'tools/check-storage-service-lifecycle.ps1'
$storageServiceLifecycleHostTest = Join-Path $projectRoot 'tools/host_tests/test_storage_service_lifecycle.c'
$resourcePressureServiceHostCheck = Join-Path $projectRoot 'tools/check-resource-pressure-service.ps1'
$resourcePressureServiceHostTest = Join-Path $projectRoot 'tools/host_tests/test_resource_pressure_service.c'
$inputHalRoutingCheck = Join-Path $projectRoot 'tools/check-input-hal-routing.ps1'
$inputScannerRestartCheck = Join-Path $projectRoot 'tools/check-input-scanner-restart.ps1'
if (-not (Test-Path -LiteralPath $wakeCapabilityHostCheck) -or
    -not (Test-Path -LiteralPath $wakeCapabilityHostTest)) {
    $violations += 'tools/check-wake-capability.ps1 / tools/host_tests/test_wake_capability.c: Wake candidate/verified host regression is missing'
}
if (-not (Test-Path -LiteralPath $wakeProfileMatrixHostCheck) -or
    -not (Test-Path -LiteralPath $wakeProfileMatrixHostTest)) {
    $violations += 'tools/check-wake-profile-matrix.ps1 / tools/host_tests/test_wake_profile_matrix.c: four-profile Wake matrix host regression is missing'
}
if (-not (Test-Path -LiteralPath $powerFailClosedHostCheck) -or
    -not (Test-Path -LiteralPath $powerFailClosedHostTest)) {
    $violations += 'tools/check-platform-power-fail-closed.ps1 / tools/host_tests/test_platform_power_fail_closed.c: Power profile fail-closed host regression is missing'
}
if (-not (Test-Path -LiteralPath $powerWakeAuthorizationHostCheck) -or
    -not (Test-Path -LiteralPath $powerWakeAuthorizationHostTest)) {
    $violations += 'tools/check-platform-power-wake-authorization.ps1 / tools/host_tests/test_platform_power_wake_authorization.c: Power/Wake authorization host regression is missing'
}
if (-not (Test-Path -LiteralPath $motionHalContractHostCheck) -or
    -not (Test-Path -LiteralPath $motionHalContractHostTest)) {
    $violations += 'tools/check-motion-hal-contract.ps1 / tools/host_tests/test_motion_hal_contract.c: Motion HAL contract host regression is missing'
}
$deviceProfileValidationSource = Join-Path $projectRoot 'main/device_profile_validation.c'
if (-not (Test-Path -LiteralPath $deviceProfileValidationHostCheck) -or
    -not (Test-Path -LiteralPath $deviceProfileValidationHostTest) -or
    -not (Test-Path -LiteralPath $deviceProfileValidationSource)) {
    $violations += 'Device profile value validation regression/source is missing'
} else {
    $deviceProfileValidationText = Get-Content -LiteralPath $deviceProfileValidationSource -Raw
    foreach ($profileRequirement in @(
        'DEVICE_CAPABILITY_REQUIRED_BASELINE',
        'DEVICE_CAPABILITY_TOUCH_INPUT',
        'DEVICE_CAPABILITY_ROUND_DISPLAY',
        'DEVICE_INPUT_SOURCE_WAKE_MASK',
        'device_profile_input_source_is_wake_eligible',
        'device_input_action_is_valid',
        'device_input_source_is_valid',
        'device_input_action_source_is_valid',
        'DEVICE_INPUT_CONTACT_DOWN'
    )) {
        if ($deviceProfileValidationText -notmatch $profileRequirement) {
            $violations += "main/device_profile_validation.c: profile value contract is incomplete (${profileRequirement})"
        }
    }
    if ($deviceProfileValidationText -match 'CONFIG_MACLAW_BOARD_|\bi2c_|\bgpio_|#include\s*[<"](?:esp_|freertos/)') {
        $violations += 'main/device_profile_validation.c: shared profile validation must not select board/SDK wiring'
    }
}
$inputServiceSource = Join-Path $projectRoot 'main/input_service.c'
if (-not (Test-Path -LiteralPath $inputServiceSource)) {
    $violations += 'main/input_service.c: shared Input Service implementation is missing'
} else {
    $inputServiceText = Get-Content -LiteralPath $inputServiceSource -Raw
    if ($inputServiceText -notmatch 'device_input_action_source_is_valid\s*\(\s*action\s*,\s*source\s*\)') {
        $violations += 'main/input_service.c: adapter-published input action/source must be validated before queue admission'
    }
    if ($inputServiceText -match 'CONFIG_MACLAW_BOARD_|\bi2c_|\bgpio_|#include\s*[<"](?:driver/)') {
        $violations += 'main/input_service.c: shared Input Service must not select board/driver wiring'
    }
}
if (-not (Test-Path -LiteralPath $officialDeviceProfilesHostCheck) -or
    -not (Test-Path -LiteralPath $officialDeviceProfilesHostTest)) {
    $violations += 'tools/check-official-device-profiles.ps1 / tools/host_tests/test_official_device_profiles.c: official profile value matrix regression is missing'
}
if (-not (Test-Path -LiteralPath $referenceFakeProfileHostCheck)) {
    $violations += 'tools/check-reference-fake-profile.ps1: CI-only fourth Reference/Fake profile regression is missing'
}
$platformStorageSource = Join-Path $projectRoot 'main/platform_storage.c'
if (-not (Test-Path -LiteralPath $platformStorageContractHostCheck) -or
    -not (Test-Path -LiteralPath $platformStorageContractHostTest) -or
    -not (Test-Path -LiteralPath $platformStorageSource)) {
    $violations += 'Platform Storage physical-ownership contract regression/source is missing'
} else {
    $platformStorageText = Get-Content -LiteralPath $platformStorageSource -Raw
    foreach ($storageRequirement in @(
        'platform_storage_map_error',
        'err\s*==\s*ESP_OK',
        's_mount_owned\s*=\s*true',
        'storage ownership lost; refusing to unmount an unknown VFS'
    )) {
        if ($platformStorageText -notmatch $storageRequirement) {
            $violations += "main/platform_storage.c: physical Storage ownership contract is incomplete (${storageRequirement})"
        }
    }
    if ($platformStorageText -match 'format_if_mount_failed\s*=\s*true[\s\S]{0,500}ESP_ERR_INVALID_STATE') {
        $violations += 'main/platform_storage.c: factory blank formatting must not promote unknown VFS ownership'
    }
}
$motionServiceSource = Join-Path $projectRoot 'main/motion_service.c'
if (-not (Test-Path -LiteralPath $motionServiceSource)) {
    $violations += 'main/motion_service.c: shared Motion Service implementation is missing'
} else {
    $motionServiceText = Get-Content -LiteralPath $motionServiceSource -Raw
    foreach ($motionRequirement in @(
        'motion_sample_is_valid',
        's_last_accepted_timestamp_us',
        'motion_sample_timestamp_is_new',
        'timestamp_us\s*<=\s*last',
        'DEVICE_MOTION_ACCELERATION_COMPONENT_ABS_MAX_MG',
        'DEVICE_MOTION_ANGULAR_RATE_COMPONENT_ABS_MAX_MDPS',
        'timestamp_us\s*<=\s*\(uint64_t\)INT64_MAX',
        'DEVICE_STATUS_IO_ERROR'
    )) {
        if ($motionServiceText -notmatch $motionRequirement) {
            $violations += "main/motion_service.c: Motion sample ABI/chronology contract is incomplete (${motionRequirement})"
        }
    }
    if ($motionServiceText -match 'CONFIG_MACLAW_BOARD_|\bi2c_|\bgpio_|#include\s*[<"](?:esp_|freertos/)') {
        $violations += 'main/motion_service.c: shared Motion Service must not select board I2C/GPIO/SDK wiring'
    }
}
if (-not (Test-Path -LiteralPath $storageServiceLifecycleHostCheck) -or
    -not (Test-Path -LiteralPath $storageServiceLifecycleHostTest)) {
    $violations += 'tools/check-storage-service-lifecycle.ps1 / tools/host_tests/test_storage_service_lifecycle.c: Storage Service lifecycle host regression is missing'
}
$resourcePressureServiceSource = Join-Path $projectRoot 'main/resource_pressure_service.c'
if (-not (Test-Path -LiteralPath $resourcePressureServiceHostCheck) -or
    -not (Test-Path -LiteralPath $resourcePressureServiceHostTest) -or
    -not (Test-Path -LiteralPath $resourcePressureServiceSource)) {
    $violations += 'Resource Pressure sample/lifecycle contract regression/source is missing'
} else {
    $resourcePressureServiceText = Get-Content -LiteralPath $resourcePressureServiceSource -Raw
    foreach ($resourceRequirement in @(
        'resource_pressure_snapshot_is_valid',
        'DEVICE_RESOURCE_PRESSURE_ABI_VERSION',
        'storage_free_bytes\s*<=\s*snapshot->storage_total_bytes',
        '!sampled\s*\|\|\s*!resource_pressure_snapshot_is_valid'
    )) {
        if ($resourcePressureServiceText -notmatch $resourceRequirement) {
            $violations += "main/resource_pressure_service.c: Resource Pressure sample contract is incomplete (${resourceRequirement})"
        }
    }
    if ($resourcePressureServiceText -match 'CONFIG_MACLAW_BOARD_|\bi2c_|\bgpio_|#include\s*[<"](?:esp_|driver/)') {
        $violations += 'main/resource_pressure_service.c: shared Resource Pressure must not select board/driver wiring'
    }
}
if (-not (Test-Path -LiteralPath $inputHalRoutingCheck)) {
    $violations += 'tools/check-input-hal-routing.ps1: Input HAL-to-business routing regression is missing'
}
if (-not (Test-Path -LiteralPath $inputScannerRestartCheck)) {
    $violations += 'tools/check-input-scanner-restart.ps1: Input scanner restart lifecycle regression is missing'
}
if (-not (Test-Path -LiteralPath $platformWakeSource)) {
    $violations += 'main/platform_wake.c: shared Wake HAL implementation is missing'
} else {
    $platformWakeText = Get-Content -LiteralPath $platformWakeSource -Raw
    if ($platformWakeText -match '\besp_sleep|\bgpio_|CONFIG_MACLAW_BOARD_|#include\s*[<"](?:esp_|freertos/)') {
        $violations += 'main/platform_wake.c: shared Wake HAL must not select board GPIO/ESP sleep wiring'
    }
    foreach ($wakeRequirement in @(
        'DEVICE_POWER_STATE_DISPLAY_OFF',
        'DEVICE_POWER_STATE_LIGHT_SLEEP',
        'DEVICE_POWER_STATE_DEEP_SLEEP',
        'platform_wake_authorize_verified_sleep_sources',
        'verified_sources\s*=\s*matrix\.verified_display_off_sources',
        'candidate_sources\s*=\s*matrix\.light_sleep_candidate_sources',
        'candidate_sources\s*=\s*matrix\.deep_sleep_candidate_sources'
    )) {
        if ($platformWakeText -notmatch $wakeRequirement) {
            $violations += "main/platform_wake.c: Wake depth matrix is incomplete (${wakeRequirement})"
        }
    }
}
$wakeServiceSource = Join-Path $projectRoot 'main/wake_service.c'
if (-not (Test-Path -LiteralPath $wakeServiceSource)) {
    $violations += 'main/wake_service.c: shared Wake Service implementation is missing'
} else {
    $wakeServiceText = Get-Content -LiteralPath $wakeServiceSource -Raw
    if ($wakeServiceText -match 'CONFIG_MACLAW_BOARD_|\besp_sleep|\bgpio_|#include\s*[<"](?:esp_|freertos/)') {
        $violations += 'main/wake_service.c: shared Wake Service must not select board electrical details'
    }
}
$wakeProfileSources = @(
    'main/platform_wake_bread_compact.c',
    'main/platform_wake_fangtang_4g.c',
    'main/platform_wake_echoear_2st.c',
    'main/platform_wake_waveshare_amoled_1_75c.c'
)
foreach ($wakeProfileSource in $wakeProfileSources) {
    $wakeProfilePath = Join-Path $projectRoot $wakeProfileSource
    if (-not (Test-Path -LiteralPath $wakeProfilePath)) {
        $violations += "${wakeProfileSource}: selected profile Wake matrix is missing"
        continue
    }
    $wakeProfileText = Get-Content -LiteralPath $wakeProfilePath -Raw
    if ($wakeProfileText -notmatch 'verified_display_off_sources' -or
        $wakeProfileText -notmatch 'light_sleep_candidate_sources' -or
        $wakeProfileText -notmatch 'deep_sleep_candidate_sources') {
        $violations += "${wakeProfileSource}: profile must declare verified DISPLAY_OFF plus unverified Light/Deep candidates"
    }
    if ($wakeProfileText -match 'verified_(light|deep)_sleep|light_sleep_verified|deep_sleep_verified') {
        $violations += "${wakeProfileSource}: Light/Deep Sleep may not be published verified before electrical HIL"
    }
    if ($wakeProfileText -match '\b(?:esp_sleep|gpio_|CONFIG_MACLAW_BOARD_)') {
        $violations += "${wakeProfileSource}: profile Wake matrix must remain normalized; electrical wiring belongs below a future profile power adapter"
    }
}

# System-sleep preparation is deliberately split from Wake policy: shared
# Power code may sequence value-only calls, while the selected profile owns
# future GPIO/RTC/rail/ESP-sleep electrical work. Keep the profile contract
# narrow now, before a board can grow another direct business-side path.
$powerPrepareHeaders = @('main/platform_power.h', 'main/platform_power_profile.h')
foreach ($powerPrepareHeader in $powerPrepareHeaders) {
    $powerPreparePath = Join-Path $projectRoot $powerPrepareHeader
    if (-not (Test-Path -LiteralPath $powerPreparePath)) {
        $violations += "${powerPrepareHeader}: System Sleep Power contract is missing"
        continue
    }
    $powerPrepareText = Get-Content -LiteralPath $powerPreparePath -Raw
    if ($powerPrepareText -match '\besp_sleep|\bgpio_|\bRTC_|SemaphoreHandle_t|TaskHandle_t|#include\s*[<"](?:esp_|freertos/)') {
        $violations += "${powerPrepareHeader}: System Sleep Power contract must not leak SDK/RTOS/GPIO details"
    }
    foreach ($powerPrepareRequirement in @(
        'prepare_verified_sleep',
        'abort_verified_sleep',
        'device_wake_source_flags_t'
    )) {
        if ($powerPrepareText -notmatch $powerPrepareRequirement) {
            $violations += "${powerPrepareHeader}: System Sleep Power contract is incomplete (${powerPrepareRequirement})"
        }
    }
}
$platformPowerSource = Join-Path $projectRoot 'main/platform_power.c'
if (-not (Test-Path -LiteralPath $platformPowerSource)) {
    $violations += 'main/platform_power.c: shared System Sleep Power facade is missing'
} else {
    $platformPowerText = Get-Content -LiteralPath $platformPowerSource -Raw
    if ($platformPowerText -match 'CONFIG_MACLAW_BOARD_|\besp_sleep|\bgpio_|#include\s*[<"](?:esp_|freertos/)') {
        $violations += 'main/platform_power.c: shared System Sleep Power facade must not select board electrical wiring'
    }
    foreach ($powerFacadeRequirement in @(
        'platform_power_profile_prepare_verified_sleep',
        'platform_power_profile_abort_verified_sleep',
        'platform_power_profile_commit_verified_sleep',
        'platform_power_profile_resume_verified_sleep',
        'platform_wake_authorize_verified_sleep_sources'
    )) {
        if ($platformPowerText -notmatch $powerFacadeRequirement) {
            $violations += "main/platform_power.c: System Sleep Power facade misses ${powerFacadeRequirement}"
        }
    }
}
$powerServiceSource = Join-Path $projectRoot 'main/power_service.c'
if (-not (Test-Path -LiteralPath $powerServiceSource)) {
    $violations += 'main/power_service.c: shared System Sleep transaction owner is missing'
} else {
    $powerServiceText = Get-Content -LiteralPath $powerServiceSource -Raw
    if ($powerServiceText -match 'CONFIG_MACLAW_BOARD_|\besp_sleep|\bgpio_') {
        $violations += 'main/power_service.c: shared System Sleep transaction must not select board electrical wiring or enter ESP sleep'
    }
    foreach ($transactionRequirement in @(
        'power_lease_service_begin_system_sleep_prepare',
        'power_lease_service_system_sleep_prepare_is_current',
        'power_lease_service_end_system_sleep_prepare',
        'prepare_display_off_scheduler_system_sleep',
        'abort_display_off_scheduler_system_sleep_prepare',
        's_system_sleep_display_off_scheduler_preparing',
        's_system_sleep_display_off_scheduler_quiesced',
        'platform_power_prepare_verified_sleep',
        'platform_power_abort_verified_sleep',
        'platform_power_commit_verified_sleep',
        'platform_power_resume_verified_sleep',
        'POWER_SYSTEM_SLEEP_COMMIT_ENTRY_TIMEOUT_MS',
        'POWER_SYSTEM_SLEEP_RESUME_TIMEOUT_MS',
        'POWER_SYSTEM_SLEEP_ROLLBACK_TIMEOUT_MS',
        'audio_service_prepare_system_sleep',
        'audio_service_abort_system_sleep_prepare',
        'command_service_prepare_system_sleep',
        'command_service_abort_system_sleep_prepare',
        'app_intent_service_prepare_system_sleep',
        'app_intent_service_abort_system_sleep_prepare',
        'firmware_identity_prepare_system_sleep',
        'firmware_identity_abort_system_sleep_prepare',
        'update_service_prepare_system_sleep',
        'update_service_abort_system_sleep_prepare',
        'fall_detection_service_prepare_system_sleep',
        'fall_detection_service_abort_system_sleep_prepare',
        'provisioning_service_prepare_system_sleep',
        'provisioning_service_abort_system_sleep_prepare',
        'meeting_recovery_service_prepare_system_sleep',
        'meeting_recovery_service_abort_system_sleep_prepare',
        'weather_cache_service_prepare_system_sleep',
        'weather_cache_service_abort_system_sleep_prepare',
        'configuration_service_prepare_system_sleep',
        'configuration_service_abort_system_sleep_prepare',
        'power_service_set_system_sleep_storage_bridge',
        'abort_system_sleep_storage_bridge',
        'persistence_service_prepare_system_sleep',
        'persistence_service_abort_system_sleep_prepare',
        'display_service_prepare_system_sleep',
        'display_service_abort_system_sleep_prepare',
        'ambient_service_prepare_system_sleep',
        'ambient_service_abort_system_sleep_prepare',
        'alarm_manager_prepare_system_sleep',
        'alarm_manager_abort_system_sleep_prepare',
        'sleep_schedule_service_prepare_system_sleep',
        'sleep_schedule_service_abort_system_sleep_prepare',
        'wake_deadline_service_prepare_system_sleep',
        'wake_deadline_service_abort_system_sleep_prepare',
        'connectivity_service_prepare_system_sleep',
        'connectivity_service_abort_system_sleep_prepare',
        'battery_policy_service_prepare_system_sleep',
        'battery_policy_service_abort_system_sleep_prepare',
        'DEVICE_POWER_TRANSITION_COMMITTING',
        'DEVICE_POWER_TRANSITION_RESUMING',
        'DEVICE_POWER_TRANSITION_ROLLING_BACK'
    )) {
        if ($powerServiceText -notmatch $transactionRequirement) {
            $violations += "main/power_service.c: System Sleep transaction is incomplete (${transactionRequirement})"
        }
    }
    # Audio/Wake is the first profile-backed participant.  Its quiesce must
    # happen only after the common lease admission fence and before a profile
    # can arm any electrical wake/rail state.  Every path after its prepare
    # must roll the transaction-owned pause owner back, including the final
    # intentional fail-closed path used while other participants are absent.
    $leaseFenceIndex = $powerServiceText.IndexOf('power_lease_service_begin_system_sleep_prepare')
    $displayOffSchedulerPrepareIndex = $powerServiceText.IndexOf('prepare_display_off_scheduler_system_sleep(remaining_ms)')
    $audioPrepareIndex = $powerServiceText.IndexOf('audio_service_prepare_system_sleep')
    $commandPrepareIndex = $powerServiceText.IndexOf('command_service_prepare_system_sleep')
    $appIntentPrepareIndex = $powerServiceText.IndexOf('app_intent_service_prepare_system_sleep')
    $identityPrepareIndex = $powerServiceText.IndexOf('firmware_identity_prepare_system_sleep')
    $updatePrepareIndex = $powerServiceText.IndexOf('update_service_prepare_system_sleep')
    $fallPrepareIndex = $powerServiceText.IndexOf('fall_detection_service_prepare_system_sleep')
    $provisioningPrepareIndex = $powerServiceText.IndexOf('provisioning_service_prepare_system_sleep')
    $meetingRecoveryPrepareIndex = $powerServiceText.IndexOf('meeting_recovery_service_prepare_system_sleep')
    $weatherCachePrepareIndex = $powerServiceText.IndexOf('weather_cache_service_prepare_system_sleep')
    $configurationPrepareIndex = $powerServiceText.IndexOf('configuration_service_prepare_system_sleep')
    $storagePrepareIndex = $powerServiceText.IndexOf('storage_prepare(remaining_ms, storage_context)')
    $persistencePrepareIndex = $powerServiceText.IndexOf('persistence_service_prepare_system_sleep')
    $ambientPrepareIndex = $powerServiceText.IndexOf('ambient_service_prepare_system_sleep')
    $displayPrepareIndex = $powerServiceText.IndexOf('display_service_prepare_system_sleep')
    $alarmPrepareIndex = $powerServiceText.IndexOf('alarm_manager_prepare_system_sleep')
    $schedulePrepareIndex = $powerServiceText.IndexOf('sleep_schedule_service_prepare_system_sleep')
    $deadlinePrepareIndex = $powerServiceText.IndexOf('wake_deadline_service_prepare_system_sleep')
    $connectivityPrepareIndex = $powerServiceText.IndexOf('connectivity_service_prepare_system_sleep')
    $batteryPolicyPrepareIndex = $powerServiceText.IndexOf('battery_policy_service_prepare_system_sleep')
    $powerPrepareIndex = $powerServiceText.IndexOf('platform_power_prepare_verified_sleep')
    $powerCommitIndex = $powerServiceText.IndexOf('platform_power_commit_verified_sleep')
    $powerResumeIndex = $powerServiceText.IndexOf('platform_power_resume_verified_sleep')
    if ($leaseFenceIndex -lt 0 -or $displayOffSchedulerPrepareIndex -lt 0 -or $audioPrepareIndex -lt 0 -or $commandPrepareIndex -lt 0 -or $appIntentPrepareIndex -lt 0 -or $identityPrepareIndex -lt 0 -or $updatePrepareIndex -lt 0 -or $fallPrepareIndex -lt 0 -or $provisioningPrepareIndex -lt 0 -or $meetingRecoveryPrepareIndex -lt 0 -or $weatherCachePrepareIndex -lt 0 -or $configurationPrepareIndex -lt 0 -or $storagePrepareIndex -lt 0 -or
        $persistencePrepareIndex -lt 0 -or $ambientPrepareIndex -lt 0 -or $displayPrepareIndex -lt 0 -or
        $alarmPrepareIndex -lt 0 -or $schedulePrepareIndex -lt 0 -or $deadlinePrepareIndex -lt 0 -or $connectivityPrepareIndex -lt 0 -or $batteryPolicyPrepareIndex -lt 0 -or
        $powerPrepareIndex -lt 0 -or $powerCommitIndex -lt 0 -or $powerResumeIndex -lt 0 -or
        -not ($leaseFenceIndex -lt $displayOffSchedulerPrepareIndex -and
               $displayOffSchedulerPrepareIndex -lt $audioPrepareIndex -and
               $audioPrepareIndex -lt $commandPrepareIndex -and
               $commandPrepareIndex -lt $appIntentPrepareIndex -and
               $appIntentPrepareIndex -lt $identityPrepareIndex -and
               $identityPrepareIndex -lt $updatePrepareIndex -and
               $updatePrepareIndex -lt $fallPrepareIndex -and
               $fallPrepareIndex -lt $provisioningPrepareIndex -and
               $provisioningPrepareIndex -lt $meetingRecoveryPrepareIndex -and
               $fallPrepareIndex -lt $meetingRecoveryPrepareIndex -and
               $meetingRecoveryPrepareIndex -lt $weatherCachePrepareIndex -and
               $weatherCachePrepareIndex -lt $configurationPrepareIndex -and
               $fallPrepareIndex -lt $configurationPrepareIndex -and
               $configurationPrepareIndex -lt $storagePrepareIndex -and
               $fallPrepareIndex -lt $storagePrepareIndex -and
               $storagePrepareIndex -lt $persistencePrepareIndex -and
               $fallPrepareIndex -lt $persistencePrepareIndex -and
               $persistencePrepareIndex -lt $ambientPrepareIndex -and
               $ambientPrepareIndex -lt $displayPrepareIndex -and
               $displayPrepareIndex -lt $alarmPrepareIndex -and
               $alarmPrepareIndex -lt $schedulePrepareIndex -and
               $schedulePrepareIndex -lt $deadlinePrepareIndex -and
                $deadlinePrepareIndex -lt $connectivityPrepareIndex -and
                $connectivityPrepareIndex -lt $batteryPolicyPrepareIndex -and
                $batteryPolicyPrepareIndex -lt $powerPrepareIndex -and
                $powerPrepareIndex -lt $powerCommitIndex -and
                $powerCommitIndex -lt $powerResumeIndex)) {
        $violations += 'main/power_service.c: System Sleep must fence leases, then park the Power DISPLAY_OFF scheduler, quiesce Audio/Wake, Command cancellation, App Intent, Firmware Identity, Update metadata, Fall Detection, Provisioning, Meeting Recovery, Weather Cache, Configuration, composition-root Storage, Persistence, Ambient, Display, Alarm, Sleep Schedule, Wake Deadline, Connectivity and Battery Policy, then execute profile Power PREPARE -> COMMIT -> RESUME'
    }
    if ($powerServiceText -notmatch 'platform_power_abort_verified_sleep\s*\(\s*target_state,\s*POWER_SYSTEM_SLEEP_ROLLBACK_TIMEOUT_MS\s*\)') {
        $violations += 'main/power_service.c: System Sleep profile rollback must use a non-zero bounded cleanup budget rather than an exhausted PREPARE remainder'
    }
    if ($powerServiceText -notmatch 'platform_power_commit_verified_sleep\s*\(\s*target_state,\s*wake\.verified_sources,\s*POWER_SYSTEM_SLEEP_COMMIT_ENTRY_TIMEOUT_MS\s*\)' -or
        $powerServiceText -notmatch 'platform_power_resume_verified_sleep\s*\(\s*target_state,\s*POWER_SYSTEM_SLEEP_RESUME_TIMEOUT_MS\s*\)') {
        $violations += 'main/power_service.c: System Sleep COMMIT entry and post-wake RESUME must use fresh bounded budgets, not the pre-sleep PREPARE remainder'
    }
    $displayOffSchedulerAbortCount = [regex]::Matches(
        $powerServiceText, '\babort_display_off_scheduler_system_sleep_prepare\s*\(').Count
    if ($displayOffSchedulerAbortCount -lt 22) {
        $violations += 'main/power_service.c: every post-DISPLAY_OFF-scheduler system-sleep failure/fail-closed path must unpark Power scheduler admission'
    }
    $audioAbortCount = [regex]::Matches(
        $powerServiceText, '\baudio_service_abort_system_sleep_prepare\s*\(').Count
    if ($audioAbortCount -lt 3) {
        $violations += 'main/power_service.c: every post-Audio system-sleep failure/fail-closed path must abort Audio/Wake preparation'
    }
    $commandAbortCount = [regex]::Matches(
        $powerServiceText, '\bcommand_service_abort_system_sleep_prepare\s*\(').Count
    if ($commandAbortCount -lt 18) {
        $violations += 'main/power_service.c: every post-Command system-sleep failure/fail-closed path must restore cancellation admission'
    }
    $appIntentAbortCount = [regex]::Matches(
        $powerServiceText, '\bapp_intent_service_abort_system_sleep_prepare\s*\(').Count
    if ($appIntentAbortCount -lt 10) {
        $violations += 'main/power_service.c: every post-App-Intent system-sleep failure/fail-closed path must restore input-to-business dispatch admission'
    }
    $identityAbortCount = [regex]::Matches(
        $powerServiceText, '\bfirmware_identity_abort_system_sleep_prepare\s*\(').Count
    if ($identityAbortCount -lt 9) {
        $violations += 'main/power_service.c: every post-Firmware-Identity system-sleep failure/fail-closed path must restore USB diagnostic admission'
    }
    $updateAbortCount = [regex]::Matches(
        $powerServiceText, '\bupdate_service_abort_system_sleep_prepare\s*\(').Count
    if ($updateAbortCount -lt 8) {
        $violations += 'main/power_service.c: every post-Update system-sleep failure/fail-closed path must restore Update metadata admission'
    }
    $fallAbortCount = [regex]::Matches(
        $powerServiceText, '\bfall_detection_service_abort_system_sleep_prepare\s*\(').Count
    if ($fallAbortCount -lt 7) {
        $violations += 'main/power_service.c: every post-Fall system-sleep failure/fail-closed path must restore Fall Detection admission'
    }
    $batteryPolicyAbortCount = [regex]::Matches(
        $powerServiceText, '\bbattery_policy_service_abort_system_sleep_prepare\s*\(').Count
    if ($batteryPolicyAbortCount -lt 3) {
        $violations += 'main/power_service.c: profile-Power failure/fail-closed paths must restore Battery Policy admission'
    }
    $batteryPolicyHeader = Join-Path $projectRoot 'main/battery_policy_service.h'
    $batteryPolicySource = Join-Path $projectRoot 'main/battery_policy_service.c'
    if (-not (Test-Path -LiteralPath $batteryPolicyHeader) -or
        -not (Test-Path -LiteralPath $batteryPolicySource)) {
        $violations += 'main/battery_policy_service.[ch]: System Sleep Battery Policy participant is missing'
    } else {
        $batteryPolicyHeaderText = Get-Content -LiteralPath $batteryPolicyHeader -Raw
        $batteryPolicySourceText = Get-Content -LiteralPath $batteryPolicySource -Raw
        foreach ($batteryPolicySleepRequirement in @(
                'device_status_t\s+battery_policy_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
                'void\s+battery_policy_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
                's_system_sleep_preparing',
                's_active_queries',
                'publish\s*=\s*s_initialized\s*&&\s*!s_stopping\s*&&\s*!s_system_sleep_preparing')) {
            if ($batteryPolicyHeaderText -notmatch $batteryPolicySleepRequirement -and
                $batteryPolicySourceText -notmatch $batteryPolicySleepRequirement) {
                $violations += "main/battery_policy_service.[ch]: System Sleep Battery Policy participant is incomplete (${batteryPolicySleepRequirement})"
            }
        }
    }
    $provisioningAbortCount = [regex]::Matches(
        $powerServiceText, '\bprovisioning_service_abort_system_sleep_prepare\s*\(').Count
    if ($provisioningAbortCount -lt 13) {
        $violations += 'main/power_service.c: every post-Provisioning system-sleep failure/fail-closed path must restore portal admission'
    }
    $meetingRecoveryAbortCount = [regex]::Matches(
        $powerServiceText, '\bmeeting_recovery_service_abort_system_sleep_prepare\s*\(').Count
    if ($meetingRecoveryAbortCount -lt 11) {
        $violations += 'main/power_service.c: every post-Meeting-Recovery system-sleep failure/fail-closed path must restore recovery metadata admission'
    }
    $weatherCacheAbortCount = [regex]::Matches(
        $powerServiceText, '\bweather_cache_service_abort_system_sleep_prepare\s*\(').Count
    if ($weatherCacheAbortCount -lt 10) {
        $violations += 'main/power_service.c: every post-Weather-Cache system-sleep failure/fail-closed path must restore cache admission'
    }
    $persistenceAbortCount = [regex]::Matches(
        $powerServiceText, '\bpersistence_service_abort_system_sleep_prepare\s*\(').Count
    if ($persistenceAbortCount -lt 3) {
        $violations += 'main/power_service.c: every post-Persistence system-sleep failure/fail-closed path must restore Persistence admission'
    }
    $ambientAbortCount = [regex]::Matches(
        $powerServiceText, '\bambient_service_abort_system_sleep_prepare\s*\(').Count
    if ($ambientAbortCount -lt 3) {
        $violations += 'main/power_service.c: every post-Ambient system-sleep failure/fail-closed path must restore the standby cadence'
    }
    $displayAbortCount = [regex]::Matches(
        $powerServiceText, '\bdisplay_service_abort_system_sleep_prepare\s*\(').Count
    if ($displayAbortCount -lt 3) {
        $violations += 'main/power_service.c: every post-Display system-sleep failure/fail-closed path must restore Display admission'
    }
    $alarmAbortCount = [regex]::Matches(
        $powerServiceText, '\balarm_manager_abort_system_sleep_prepare\s*\(').Count
    if ($alarmAbortCount -lt 3) {
        $violations += 'main/power_service.c: every post-Alarm system-sleep failure/fail-closed path must restore Alarm admission'
    }
    $scheduleAbortCount = [regex]::Matches(
        $powerServiceText, '\bsleep_schedule_service_abort_system_sleep_prepare\s*\(').Count
    if ($scheduleAbortCount -lt 3) {
        $violations += 'main/power_service.c: every post-Sleep-Schedule system-sleep failure/fail-closed path must restore schedule admission'
    }
    $deadlineAbortCount = [regex]::Matches(
        $powerServiceText, '\bwake_deadline_service_abort_system_sleep_prepare\s*\(').Count
    if ($deadlineAbortCount -lt 4) {
        $violations += 'main/power_service.c: every post-Wake-Deadline system-sleep failure/fail-closed path must restore timer callback admission'
    }
    $connectivityAbortCount = [regex]::Matches(
        $powerServiceText, '\bconnectivity_service_abort_system_sleep_prepare\s*\(').Count
    if ($connectivityAbortCount -lt 2) {
        $violations += 'main/power_service.c: every post-Connectivity system-sleep failure/fail-closed path must restore Connectivity admission'
    }
}

# Meeting Recovery and Weather Cache are separate synchronous Persistence
# clients. They must close their own public admission before shared Persistence
# seals NVS, otherwise a pre-existing producer can be mistaken for a durable
# sleep checkpoint. Their headers remain value-only and hardware-neutral.
foreach ($durableMetadataParticipant in @(
        @{ Name = 'Meeting Recovery'; Header = 'main/meeting_recovery_service.h'; Source = 'main/meeting_recovery_service.c'; Prefix = 'meeting_recovery_service' },
        @{ Name = 'Weather Cache'; Header = 'main/weather_cache_service.h'; Source = 'main/weather_cache_service.c'; Prefix = 'weather_cache_service' }
    )) {
    $participantHeaderPath = Join-Path $projectRoot $durableMetadataParticipant.Header
    $participantSourcePath = Join-Path $projectRoot $durableMetadataParticipant.Source
    if (-not (Test-Path -LiteralPath $participantHeaderPath) -or
        -not (Test-Path -LiteralPath $participantSourcePath)) {
        $violations += "$($durableMetadataParticipant.Source): System Sleep $($durableMetadataParticipant.Name) participant is missing"
        continue
    }
    $participantHeaderText = Get-Content -LiteralPath $participantHeaderPath -Raw
    $participantSourceText = Get-Content -LiteralPath $participantSourcePath -Raw
    foreach ($participantRequirement in @(
            "device_status_t\s+$($durableMetadataParticipant.Prefix)_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)",
            "void\s+$($durableMetadataParticipant.Prefix)_abort_system_sleep_prepare\s*\(\s*void\s*\)",
            's_system_sleep_preparing',
            's_active_calls')) {
        if ($participantHeaderText -notmatch $participantRequirement -and
            $participantSourceText -notmatch $participantRequirement) {
            $violations += "$($durableMetadataParticipant.Source): System Sleep $($durableMetadataParticipant.Name) participant is incomplete ($participantRequirement)"
        }
    }
    if ($participantHeaderText -match '\b(?:nvs_handle_t|esp_err_t|SemaphoreHandle_t|TaskHandle_t|esp_sleep|gpio_)\b|#include\s*[<"](?:freertos/|driver/)') {
        $violations += "$($durableMetadataParticipant.Header): System Sleep $($durableMetadataParticipant.Name) contract must remain value-only"
    }
    if ($participantSourceText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += "$($durableMetadataParticipant.Source): System Sleep $($durableMetadataParticipant.Name) participant must not select board electrical wiring or enter MCU sleep"
    }
}

# Wake Deadline is the shared logical deadline dispatcher for Alarm and Sleep
# Schedule. Its System Sleep fence must retain client slots yet stop timer
# callback selection, and the public boundary must remain free of ESP timer,
# FreeRTOS, GPIO and board choice.
$wakeDeadlineHeaderForSleep = Join-Path $projectRoot 'main/wake_deadline_service.h'
$wakeDeadlineSourceForSleep = Join-Path $projectRoot 'main/wake_deadline_service.c'
$wakeDeadlineGateForSleep = Join-Path $projectRoot 'main/wake_deadline_sleep_gate.h'
$systemSleepFailureClosureGate = Join-Path $projectRoot 'tools/check-system-sleep-failure-closure.ps1'
if (-not (Test-Path -LiteralPath $systemSleepFailureClosureGate)) {
    $violations += 'tools/check-system-sleep-failure-closure.ps1: shared PREPARE failure-closure regression is missing'
}
if (-not (Test-Path -LiteralPath $wakeDeadlineHeaderForSleep) -or
    -not (Test-Path -LiteralPath $wakeDeadlineSourceForSleep) -or
    -not (Test-Path -LiteralPath $wakeDeadlineGateForSleep)) {
    $violations += 'main/wake_deadline_service.[ch]: System Sleep deadline participant is missing'
} else {
    $wakeDeadlineHeaderForSleepText = Get-Content -LiteralPath $wakeDeadlineHeaderForSleep -Raw
    $wakeDeadlineSourceForSleepText = Get-Content -LiteralPath $wakeDeadlineSourceForSleep -Raw
    $wakeDeadlineGateForSleepText = Get-Content -LiteralPath $wakeDeadlineGateForSleep -Raw
    foreach ($wakeDeadlineSleepRequirement in @(
            'device_status_t\s+wake_deadline_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
            'void\s+wake_deadline_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
            's_system_sleep_preparing',
            's_system_sleep_callbacks_inflight',
            'esp_timer_stop\s*\(',
            '!s_system_sleep_preparing\s*&&\s*clock_is_trusted',
            'wake_deadline_sleep_gate_begin\s*\(',
            'wake_deadline_sleep_gate_callbacks_drained\s*\(',
            'wake_deadline_sleep_gate_abort\s*\('
        )) {
        if ($wakeDeadlineHeaderForSleepText -notmatch $wakeDeadlineSleepRequirement -and
            $wakeDeadlineSourceForSleepText -notmatch $wakeDeadlineSleepRequirement -and
            $wakeDeadlineGateForSleepText -notmatch $wakeDeadlineSleepRequirement) {
            $violations += "main/wake_deadline_service.[ch]: System Sleep deadline participant is incomplete (${wakeDeadlineSleepRequirement})"
        }
    }
    if ($wakeDeadlineHeaderForSleepText -match '\b(?:SemaphoreHandle_t|TaskHandle_t|esp_timer|esp_sleep|gpio_)\b|#include\s*[<"](?:freertos/|driver/)') {
        $violations += 'main/wake_deadline_service.h: System Sleep deadline contract must remain value-only'
    }
    if ($wakeDeadlineSourceForSleepText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += 'main/wake_deadline_service.c: System Sleep deadline participant must not select board electrical wiring or enter MCU sleep'
    }
    if ($wakeDeadlineGateForSleepText -match '\b(?:SemaphoreHandle_t|TaskHandle_t|esp_timer|esp_sleep|gpio_)\b|#include\s*[<"](?:freertos/|driver/)') {
        $violations += 'main/wake_deadline_sleep_gate.h: System Sleep gate must remain value-only'
    }
    $storageAbortCount = [regex]::Matches(
        $powerServiceText, '\babort_system_sleep_storage_bridge\s*\(').Count
    if ($storageAbortCount -lt 10) {
        $violations += 'main/power_service.c: every post-composition-root Storage system-sleep failure/fail-closed path must restore legacy storage admission'
    }
    $configurationAbortCount = [regex]::Matches(
        $powerServiceText, '\bconfiguration_service_abort_system_sleep_prepare\s*\(').Count
    if ($configurationAbortCount -lt 10) {
        $violations += 'main/power_service.c: every post-Configuration system-sleep failure/fail-closed path must restore direct configuration admission'
    }
}

# The internal-stack persistence worker owns its queue/task/Registry lifecycle
# outside the composition root. It still participates in the same reversible
# Power transaction before shared Persistence closes new NVS admission, while
# the public worker and Power contracts remain value-only.
$powerServiceHeaderForStorage = Join-Path $projectRoot 'main/power_service.h'
$mainCompositionForStorage = Join-Path $projectRoot 'main/main.c'
$configurationPersistenceWorkerHeader = Join-Path $projectRoot 'main/services/configuration_persistence_worker_service.h'
$configurationPersistenceWorkerSource = Join-Path $projectRoot 'main/services/configuration_persistence_worker_service.c'
if (-not (Test-Path -LiteralPath $powerServiceHeaderForStorage) -or
    -not (Test-Path -LiteralPath $mainCompositionForStorage) -or
    -not (Test-Path -LiteralPath $configurationPersistenceWorkerHeader) -or
    -not (Test-Path -LiteralPath $configurationPersistenceWorkerSource)) {
    $violations += 'main/power_service.h / main/main.c / main/services/configuration_persistence_worker_service.[ch]: Storage System Sleep bridge is missing'
} else {
    $powerStorageHeaderText = Get-Content -LiteralPath $powerServiceHeaderForStorage -Raw
    $mainStorageText = Get-Content -LiteralPath $mainCompositionForStorage -Raw
    $configurationPersistenceWorkerHeaderText = Get-Content -LiteralPath $configurationPersistenceWorkerHeader -Raw
    $configurationPersistenceWorkerSourceText = Get-Content -LiteralPath $configurationPersistenceWorkerSource -Raw
    foreach ($storageBridgeRequirement in @(
            'power_service_system_sleep_storage_prepare_t',
            'power_service_system_sleep_storage_abort_t',
            'power_service_set_system_sleep_storage_bridge',
            'configuration_persistence_prepare_system_sleep',
            'configuration_persistence_abort_system_sleep_prepare',
            'configuration_persistence_worker_service_prepare_system_sleep',
            'configuration_persistence_worker_service_abort_system_sleep_prepare',
            's_system_sleep_preparing',
            's_system_sleep_quiesced',
            's_exit_status',
            's_registry_retirement_failed',
            'system_sleep_prepare\s*=\s*true',
            'power_service_set_system_sleep_storage_bridge\s*\(')) {
        if ($powerStorageHeaderText -notmatch $storageBridgeRequirement -and
            $mainStorageText -notmatch $storageBridgeRequirement -and
            $configurationPersistenceWorkerHeaderText -notmatch $storageBridgeRequirement -and
            $configurationPersistenceWorkerSourceText -notmatch $storageBridgeRequirement) {
            $violations += "main/power_service.h / main/main.c / main/services/configuration_persistence_worker_service.[ch]: Storage System Sleep bridge is incomplete (${storageBridgeRequirement})"
        }
    }
    if ($powerStorageHeaderText -match '\b(?:QueueHandle_t|TaskHandle_t|SemaphoreHandle_t|esp_err_t|nvs_|gpio_|esp_sleep)\b|#include\s*[<"](?:freertos/|driver/)') {
        $violations += 'main/power_service.h: composition-root Storage bridge contract must remain value-only'
    }
    if ($configurationPersistenceWorkerHeaderText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|QueueHandle_t|nvs_|heap_caps|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/configuration_persistence_worker_service.h: persistence worker public contract must remain value-only and SDK/RTOS/board-neutral'
    }
    foreach ($workerStorageRetirementRequirement in @(
            's_exit_status',
            's_retiring',
            's_registry_retirement_failed',
            'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
            's_exit_status\s*=\s*registry_err',
            '!s_registry_retirement_failed',
            's_start_gate')) {
        if ($configurationPersistenceWorkerSourceText -notmatch $workerStorageRetirementRequirement) {
            $violations += "main/services/configuration_persistence_worker_service.c: Storage worker must retain failed Registry retirement closed (${workerStorageRetirementRequirement})"
        }
    }
    if ($mainStorageText -match '\bs_output_volume_persist(?:_|\b)|\boutput_volume_persist_task\b') {
        $violations += 'main/main.c: legacy output-volume persistence worker state/task must be owned by configuration_persistence_worker_service'
    }
}
if (-not (Test-Path -LiteralPath $alarmWakePlanHostCheck) -or
    -not (Test-Path -LiteralPath $alarmWakePlanHostTest) -or
    -not (Test-Path -LiteralPath (Join-Path $projectRoot 'main/alarm_wake_plan.c')) -or
    -not (Test-Path -LiteralPath (Join-Path $projectRoot 'main/alarm_wake_plan.h'))) {
    $violations += 'alarm wake plan value contract/gate is missing'
}

# Configuration owns durable product credentials and its own serialized
# scratch-state mutation boundary. It must close direct mutations before the
# root legacy worker and generic Persistence fence, with a value-only public
# contract that does not disclose NVS/RTOS implementation details.
$provisioningHeaderForSleep = Join-Path $projectRoot 'main/services/provisioning_service.h'
$provisioningSourceForSleep = Join-Path $projectRoot 'main/services/provisioning_service.c'
if (-not (Test-Path -LiteralPath $provisioningHeaderForSleep) -or
    -not (Test-Path -LiteralPath $provisioningSourceForSleep)) {
    $violations += 'main/services/provisioning_service.[ch]: System Sleep provisioning participant is missing'
} else {
    $provisioningHeaderText = Get-Content -LiteralPath $provisioningHeaderForSleep -Raw
    $provisioningSourceText = Get-Content -LiteralPath $provisioningSourceForSleep -Raw
    foreach ($provisioningSleepRequirement in @(
            'device_status_t\s+provisioning_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
            'void\s+provisioning_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
            's_system_sleep_preparing',
            's_setup_portal_mutex',
            's_setup_restart_task',
            's_setup_server')) {
        if ($provisioningHeaderText -notmatch $provisioningSleepRequirement -and
            $provisioningSourceText -notmatch $provisioningSleepRequirement) {
            $violations += "main/services/provisioning_service.[ch]: System Sleep provisioning participant is incomplete (${provisioningSleepRequirement})"
        }
    }
    if ($provisioningHeaderText -match '\b(?:esp_err_t|SemaphoreHandle_t|TaskHandle_t|httpd_handle_t|esp_sleep|gpio_)\b|#include\s*[<"](?:freertos/|driver/|esp_http)') {
        $violations += 'main/services/provisioning_service.h: System Sleep provisioning contract must remain value-only'
    }
    if ($provisioningSourceText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += 'main/services/provisioning_service.c: System Sleep provisioning participant must not select board electrical wiring or enter MCU sleep'
    }
}

# The permanent command-cancel worker is not stopped during System Sleep, but
# normal startup rollback still uses the Task Registry. Its exit must publish
# completion only after its immutable Interaction identity has retired; a
# failed retirement must not allow a second permanent cancel worker.
$commandServiceSourceForRetirement = Join-Path $projectRoot 'main/services/command_service.c'
if (-not (Test-Path -LiteralPath $commandServiceSourceForRetirement)) {
    $violations += 'main/services/command_service.c: command cancel lifecycle service is missing'
} else {
    $commandServiceRetirementText = Get-Content -LiteralPath $commandServiceSourceForRetirement -Raw
    foreach ($commandRetirementRequirement in @(
            's_command_cancel_retiring',
            's_command_cancel_exit_status',
            's_command_cancel_registry_retirement_failed',
            'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
            's_command_cancel_exit_status\s*=\s*registry_err',
            's_command_cancel_retiring\s*=\s*false',
            'retirement_failed\s*\|\|\s*task_active')) {
        if ($commandServiceRetirementText -notmatch $commandRetirementRequirement) {
            $violations += "main/services/command_service.c: command cancel Registry retirement fence is incomplete (${commandRetirementRequirement})"
        }
    }
}

# Ambient's cadence worker is a POWER Registry worker but remains a shared
# presentation service. Its cadence admission and System Sleep ABORT must stay
# closed if natural exit cannot retire the immutable Registry identity.
$ambientServiceSourceForRetirement = Join-Path $projectRoot 'main/services/ambient_service.c'
if (-not (Test-Path -LiteralPath $ambientServiceSourceForRetirement)) {
    $violations += 'main/services/ambient_service.c: ambient lifecycle service is missing'
} else {
    $ambientServiceRetirementText = Get-Content -LiteralPath $ambientServiceSourceForRetirement -Raw
    foreach ($ambientRetirementRequirement in @(
            's_ambient_task_exit_status',
            's_ambient_task_registry_retirement_failed',
            'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
            's_ambient_task_exit_status\s*=\s*registry_err',
            '!s_ambient_task_registry_retirement_failed',
            'restart_clock\s*=\s*s_system_sleep_restart_clock\s*&&\s*!s_ambient_task_registry_retirement_failed')) {
        if ($ambientServiceRetirementText -notmatch $ambientRetirementRequirement) {
            $violations += "main/services/ambient_service.c: ambient Registry retirement fence is incomplete (${ambientRetirementRequirement})"
        }
    }
}

# Clock Sync owns a cancellable SNTP monitor and participates directly in
# System Sleep. Its ABORT restart path must likewise reject a failed immutable
# Registry retirement rather than using a completion token as proof.
$clockSyncServiceSourceForRetirement = Join-Path $projectRoot 'main/services/clock_sync_service.c'
if (-not (Test-Path -LiteralPath $clockSyncServiceSourceForRetirement)) {
    $violations += 'main/services/clock_sync_service.c: clock-sync lifecycle service is missing'
} else {
    $clockSyncRetirementText = Get-Content -LiteralPath $clockSyncServiceSourceForRetirement -Raw
    foreach ($clockSyncRetirementRequirement in @(
            's_exit_status',
            's_registry_retirement_failed',
            'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
            's_exit_status\s*=\s*registry_err',
            's_system_sleep_was_initialized\s*&&\s*!s_registry_retirement_failed',
            'pending\s*\|\|\s*s_registry_retirement_failed')) {
        if ($clockSyncRetirementText -notmatch $clockSyncRetirementRequirement) {
            $violations += "main/services/clock_sync_service.c: clock-sync Registry retirement fence is incomplete (${clockSyncRetirementRequirement})"
        }
    }
}

# Provisioning has three independently registered Connectivity workers.  A
# portal stop may observe their completion while the worker is still removing
# its immutable Registry identity; all three must retain admission closed on
# that failure so a new credential-bearing generation cannot reuse it.
$provisioningServiceSourceForRetirement = Join-Path $projectRoot 'main/services/provisioning_service.c'
if (-not (Test-Path -LiteralPath $provisioningServiceSourceForRetirement)) {
    $violations += 'main/services/provisioning_service.c: provisioning lifecycle service is missing'
} else {
    $provisioningRetirementText = Get-Content -LiteralPath $provisioningServiceSourceForRetirement -Raw
    foreach ($provisioningRetirementRequirement in @(
            's_dns_retiring',
            's_dns_exit_status',
            's_dns_registry_retirement_failed',
            's_setup_ttl_retiring',
            's_setup_ttl_exit_status',
            's_setup_ttl_registry_retirement_failed',
            's_setup_restart_retiring',
            's_setup_restart_exit_status',
            's_setup_restart_registry_retirement_failed',
            's_dns_exit_status\s*=\s*registry_err',
            's_setup_ttl_exit_status\s*=\s*registry_err',
            's_setup_restart_exit_status\s*=\s*registry_err',
            's_dns_admission_open\s*&&\s*!s_dns_registry_retirement_failed',
            's_setup_ttl_admission_open\s*&&\s*!s_setup_ttl_registry_retirement_failed',
            's_setup_restart_admission_open\s*&&\s*!s_setup_restart_registry_retirement_failed')) {
        if ($provisioningRetirementText -notmatch $provisioningRetirementRequirement) {
            $violations += "main/services/provisioning_service.c: provisioning Registry retirement fence is incomplete ($provisioningRetirementRequirement)"
        }
    }
}

# Foreground voice capture is shared by every profile.  It has the same
# create→publish→registered lifecycle as other Interaction workers: a terminal
# Registry timeout must retain command admission rather than letting the next
# button/touch action create a replacement against the old identity.
$interactionServiceSourceForRetirement = Join-Path $projectRoot 'main/services/interaction_service.c'
if (-not (Test-Path -LiteralPath $interactionServiceSourceForRetirement)) {
    $violations += 'main/services/interaction_service.c: interaction lifecycle service is missing'
} else {
    $interactionRetirementText = Get-Content -LiteralPath $interactionServiceSourceForRetirement -Raw
    foreach ($interactionRetirementRequirement in @(
            's_interaction_retiring',
            's_interaction_exit_status',
            's_interaction_registry_retirement_failed',
            's_interaction_exit_status\s*=\s*registry_err',
            's_interaction_stop_requested\s*=\s*true',
            'retirement_failed\s*\|\|\s*task_active')) {
        if ($interactionRetirementText -notmatch $interactionRetirementRequirement) {
            $violations += "main/services/interaction_service.c: foreground interaction Registry retirement fence is incomplete ($interactionRetirementRequirement)"
        }
    }
}

# Meeting uses one AUDIO worker plus two independent CONNECTIVITY workers.
# Completion semaphores are shared across generations, so each worker must
# prove Registry retirement before it clears its handle or lets System Sleep
# ABORT create a supervisor/capability-refresh replacement.
$meetingServiceSourceForRetirement = Join-Path $projectRoot 'main/services/meeting_service.c'
if (-not (Test-Path -LiteralPath $meetingServiceSourceForRetirement)) {
    $violations += 'main/services/meeting_service.c: meeting lifecycle service is missing'
} else {
    $meetingRetirementText = Get-Content -LiteralPath $meetingServiceSourceForRetirement -Raw
    foreach ($meetingRetirementRequirement in @(
            's_meeting_task_retiring',
            's_meeting_task_exit_status',
            's_meeting_task_registry_retirement_failed',
            's_resume_supervisor_exit_status',
            's_resume_supervisor_registry_retirement_failed',
            's_capability_refresh_exit_status',
            's_capability_refresh_registry_retirement_failed',
            's_meeting_task_exit_status\s*=\s*registry_err',
            's_resume_supervisor_exit_status\s*=\s*registry_err',
            's_capability_refresh_exit_status\s*=\s*registry_err',
            's_meeting_task_running\s*\|\|\s*s_meeting_task_retiring\s*\|\|\s*s_meeting_task_registry_retirement_failed',
            's_resume_supervisor_registry_retirement_failed',
            's_capability_refresh_registry_retirement_failed')) {
        if ($meetingRetirementText -notmatch $meetingRetirementRequirement) {
            $violations += "main/services/meeting_service.c: meeting Registry retirement fence is incomplete ($meetingRetirementRequirement)"
        }
    }
}

# Gateway startup, outgoing polling and cellular retry all own immutable
# Connectivity Registry entries.  Their completion notifications may be shared
# by successive generations, so a failed unregister must close restart
# admission rather than letting a new task reuse the same identity.
$gatewayTransportSourceForRetirement = Join-Path $projectRoot 'main/services/gateway_transport.c'
if (-not (Test-Path -LiteralPath $gatewayTransportSourceForRetirement)) {
    $violations += 'main/services/gateway_transport.c: gateway startup lifecycle owner is missing'
} else {
    $gatewayTransportRetirementText = Get-Content -LiteralPath $gatewayTransportSourceForRetirement -Raw
    foreach ($gatewayTransportRetirementRequirement in @(
            's_gateway_startup_retiring',
            's_gateway_startup_exit_status',
            's_gateway_startup_registry_retirement_failed',
            'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
            's_gateway_startup_exit_status\s*=\s*registry_err',
            's_gateway_startup_running\s*=\s*false',
            '!s_gateway_startup_registry_retirement_failed')) {
        if ($gatewayTransportRetirementText -notmatch $gatewayTransportRetirementRequirement) {
            $violations += "main/services/gateway_transport.c: gateway startup Registry retirement fence is incomplete ($gatewayTransportRetirementRequirement)"
        }
    }
}

$gatewayDispatcherSourceForRetirement = Join-Path $projectRoot 'main/services/gateway_dispatcher.c'
if (-not (Test-Path -LiteralPath $gatewayDispatcherSourceForRetirement)) {
    $violations += 'main/services/gateway_dispatcher.c: gateway poll lifecycle owner is missing'
} else {
    $gatewayDispatcherRetirementText = Get-Content -LiteralPath $gatewayDispatcherSourceForRetirement -Raw
    foreach ($gatewayDispatcherRetirementRequirement in @(
            's_gateway_poll_start_gate',
            's_gateway_poll_starting',
            's_gateway_poll_retiring',
            's_gateway_poll_exit_status',
            's_gateway_poll_registry_retirement_failed',
            'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
            's_gateway_poll_exit_status\s*=\s*registry_err',
            's_gateway_poll_starting\s*=\s*false',
            '!s_gateway_poll_registry_retirement_failed')) {
        if ($gatewayDispatcherRetirementText -notmatch $gatewayDispatcherRetirementRequirement) {
            $violations += "main/services/gateway_dispatcher.c: gateway poll Registry retirement fence is incomplete ($gatewayDispatcherRetirementRequirement)"
        }
    }
}

$cellularRecoverySourceForRetirement = Join-Path $projectRoot 'main/services/cellular_recovery_service.c'
if (-not (Test-Path -LiteralPath $cellularRecoverySourceForRetirement)) {
    $violations += 'main/services/cellular_recovery_service.c: cellular recovery lifecycle owner is missing'
} else {
    $cellularRecoveryRetirementText = Get-Content -LiteralPath $cellularRecoverySourceForRetirement -Raw
    foreach ($cellularRecoveryRetirementRequirement in @(
            's_retiring',
            's_exit_status',
            's_registry_retirement_failed',
            'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
            's_exit_status\s*=\s*registry_err',
            's_admission_open\s*=\s*false',
            '!s_registry_retirement_failed')) {
        if ($cellularRecoveryRetirementText -notmatch $cellularRecoveryRetirementRequirement) {
            $violations += "main/services/cellular_recovery_service.c: cellular recovery Registry retirement fence is incomplete ($cellularRecoveryRetirementRequirement)"
        }
    }
}

$configurationHeaderForSleep = Join-Path $projectRoot 'main/configuration_service.h'
$configurationSourceForSleep = Join-Path $projectRoot 'main/configuration_service.c'
if (-not (Test-Path -LiteralPath $configurationHeaderForSleep) -or
    -not (Test-Path -LiteralPath $configurationSourceForSleep)) {
    $violations += 'main/configuration_service.[ch]: System Sleep Configuration participant is missing'
} else {
    $configurationHeaderText = Get-Content -LiteralPath $configurationHeaderForSleep -Raw
    $configurationSourceText = Get-Content -LiteralPath $configurationSourceForSleep -Raw
    foreach ($configurationSleepRequirement in @(
            'device_status_t\s+configuration_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
            'void\s+configuration_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
            's_system_sleep_preparing',
            '!s_system_sleep_preparing')) {
        if ($configurationHeaderText -notmatch $configurationSleepRequirement -and
            $configurationSourceText -notmatch $configurationSleepRequirement) {
            $violations += "main/configuration_service.[ch]: System Sleep Configuration participant is incomplete (${configurationSleepRequirement})"
        }
    }
    if ($configurationHeaderText -match '\b(?:nvs_handle_t|esp_err_t|SemaphoreHandle_t|TaskHandle_t|esp_sleep|gpio_)\b|#include\s*[<"](?:freertos/|driver/)') {
        $violations += 'main/configuration_service.h: System Sleep Configuration contract must remain value-only'
    }
    if ($configurationSourceText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += 'main/configuration_service.c: System Sleep Configuration participant must not select board electrical wiring or enter MCU sleep'
    }
}

# App Intent is the single profile-neutral bridge from Device Input into
# business policy. It may fence semantic dispatch for future System Sleep, but
# its public header must remain free of board scanner, RTOS and electrical
# detail; those stay below Input HAL.
$commandHeaderForSleep = Join-Path $projectRoot 'main/services/command_service.h'
$commandSourceForSleep = Join-Path $projectRoot 'main/services/command_service.c'
if (-not (Test-Path -LiteralPath $commandHeaderForSleep) -or
    -not (Test-Path -LiteralPath $commandSourceForSleep)) {
    $violations += 'main/services/command_service.[ch]: System Sleep cancellation participant is missing'
} else {
    $commandHeaderForSleepText = Get-Content -LiteralPath $commandHeaderForSleep -Raw
    $commandSourceForSleepText = Get-Content -LiteralPath $commandSourceForSleep -Raw
    foreach ($commandSleepRequirement in @(
            'device_status_t\s+command_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
            'void\s+command_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
            's_system_sleep_preparing',
            's_command_cancel_worker_active',
            '!s_system_sleep_preparing',
            's_command_cancel_requested')) {
        if ($commandHeaderForSleepText -notmatch $commandSleepRequirement -and
            $commandSourceForSleepText -notmatch $commandSleepRequirement) {
            $violations += "main/services/command_service.[ch]: System Sleep cancellation participant is incomplete (${commandSleepRequirement})"
        }
    }
    if ($commandHeaderForSleepText -match '\b(?:SemaphoreHandle_t|TaskHandle_t|esp_sleep|gpio_)\b|#include\s*[<"](?:freertos/|driver/)') {
        $violations += 'main/services/command_service.h: System Sleep cancellation contract must remain value-only'
    }
    if ($commandSourceForSleepText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += 'main/services/command_service.c: System Sleep cancellation participant must not select board electrical wiring or enter MCU sleep'
    }
}

$appIntentHeaderForSleep = Join-Path $projectRoot 'main/app_intent_service.h'
$appIntentSourceForSleep = Join-Path $projectRoot 'main/app_intent_service.c'
if (-not (Test-Path -LiteralPath $appIntentHeaderForSleep) -or
    -not (Test-Path -LiteralPath $appIntentSourceForSleep)) {
    $violations += 'main/app_intent_service.[ch]: System Sleep input participant is missing'
} else {
    $appIntentHeaderForSleepText = Get-Content -LiteralPath $appIntentHeaderForSleep -Raw
    $appIntentSourceForSleepText = Get-Content -LiteralPath $appIntentSourceForSleep -Raw
    foreach ($appIntentSleepRequirement in @(
            'device_status_t\s+app_intent_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
            'void\s+app_intent_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
            'system_sleep_preparing',
            'system_sleep_epoch',
            'dispatches_in_flight',
            'begin_intent_dispatch\s*\(',
            'publishers_in_flight'
        )) {
        if ($appIntentHeaderForSleepText -notmatch $appIntentSleepRequirement -and
            $appIntentSourceForSleepText -notmatch $appIntentSleepRequirement) {
            $violations += "main/app_intent_service.[ch]: System Sleep input participant is incomplete (${appIntentSleepRequirement})"
        }
    }
    if ($appIntentHeaderForSleepText -match '\b(?:SemaphoreHandle_t|TaskHandle_t|esp_sleep|gpio_)\b|#include\s*[<"](?:freertos/|driver/)') {
        $violations += 'main/app_intent_service.h: System Sleep input contract must remain value-only'
    }
    if ($appIntentSourceForSleepText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += 'main/app_intent_service.c: System Sleep input participant must not select board electrical wiring or enter MCU sleep'
    }
}

# Firmware Identity has a retained USB Serial/JTAG query worker. System Sleep
# must park that observer rather than permanently stop USB diagnostics, while
# the public contract remains value-only and knows neither pin/USB controller
# wiring nor an ESP sleep API.
$firmwareIdentityHeaderForSleep = Join-Path $projectRoot 'main/firmware_identity.h'
$firmwareIdentitySourceForSleep = Join-Path $projectRoot 'main/firmware_identity.c'
if (-not (Test-Path -LiteralPath $firmwareIdentityHeaderForSleep) -or
    -not (Test-Path -LiteralPath $firmwareIdentitySourceForSleep)) {
    $violations += 'main/firmware_identity.[ch]: System Sleep diagnostic participant is missing'
} else {
    $firmwareIdentityHeaderForSleepText = Get-Content -LiteralPath $firmwareIdentityHeaderForSleep -Raw
    $firmwareIdentitySourceForSleepText = Get-Content -LiteralPath $firmwareIdentitySourceForSleep -Raw
    foreach ($identitySleepRequirement in @(
            'device_status_t\s+firmware_identity_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
            'void\s+firmware_identity_abort_system_sleep_prepare\s*\(\s*void\s*\)',
            's_system_sleep_preparing',
            's_system_sleep_quiesced',
            's_system_sleep_emissions',
            's_system_sleep_observers',
            's_query_task_retiring',
            's_query_task_exit_status',
            's_query_task_registry_retirement_failed',
            'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
            '__atomic_store_n\s*\(\s*&s_query_task_exit_status\s*,\s*registry_err',
            '__atomic_load_n\s*\(\s*&s_query_task_registry_retirement_failed',
            'firmware_identity_sleep_gate_begin\s*\(',
            'firmware_identity_sleep_gate_end\s*\(',
            'ulTaskNotifyTake\s*\(\s*pdTRUE\s*,\s*portMAX_DELAY\s*\)',
            'query_task_is_starting\s*\(\s*\)'
        )) {
        if ($firmwareIdentityHeaderForSleepText -notmatch $identitySleepRequirement -and
            $firmwareIdentitySourceForSleepText -notmatch $identitySleepRequirement) {
            $violations += "main/firmware_identity.[ch]: System Sleep diagnostic participant is incomplete (${identitySleepRequirement})"
        }
    }
    if ($firmwareIdentityHeaderForSleepText -match '\b(?:SemaphoreHandle_t|TaskHandle_t|esp_sleep|gpio_)\b|#include\s*[<"](?:freertos/|driver/)') {
        $violations += 'main/firmware_identity.h: System Sleep diagnostic contract must remain value-only'
    }
    if ($firmwareIdentitySourceForSleepText -match '\besp_sleep|\bgpio_') {
        $violations += 'main/firmware_identity.c: System Sleep diagnostics must not select board electrical wiring or enter MCU sleep'
    }
    if ($firmwareIdentitySourceForSleepText -notmatch 's_system_sleep_emissions[^\r\n]*!=\s*0\s*\|\|[\s\S]{0,160}s_system_sleep_observers[^\r\n]*!=\s*0') {
        $violations += 'main/firmware_identity.c: System Sleep PREPARE must drain pre-fence synchronous diagnostic observers'
    }
    if ($firmwareIdentitySourceForSleepText -match '__atomic_store_n\s*\(\s*&s_system_sleep_preparing\s*,\s*false[^\r\n]*\).*DEVICE_STATUS_TIMEOUT') {
        $violations += 'main/firmware_identity.c: System Sleep PREPARE timeout must retain diagnostic admission closed until ABORT'
    }
}

# Update Service is a shared metadata/reminder policy client of Persistence.
# It has no firmware bytes, installer or board resource, but its concurrent
# Hub metadata/tool calls can write NVS or consume a UI notice. Keep its
# reversible fence above Persistence and value-only at the public boundary.
$updateServiceHeaderForSleep = Join-Path $projectRoot 'main/update_service.h'
$updateServiceSourceForSleep = Join-Path $projectRoot 'main/update_service.c'
if (-not (Test-Path -LiteralPath $updateServiceHeaderForSleep) -or
    -not (Test-Path -LiteralPath $updateServiceSourceForSleep)) {
    $violations += 'main/update_service.[ch]: System Sleep Update participant is missing'
} else {
    $updateServiceHeaderForSleepText = Get-Content -LiteralPath $updateServiceHeaderForSleep -Raw
    $updateServiceSourceForSleepText = Get-Content -LiteralPath $updateServiceSourceForSleep -Raw
    foreach ($updateSleepRequirement in @(
            'device_status_t\s+update_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
            'void\s+update_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
            's_system_sleep_preparing',
            's_active_calls',
            '!s_system_sleep_preparing',
            'persistence_service_(?:read|write)_'
        )) {
        if ($updateServiceHeaderForSleepText -notmatch $updateSleepRequirement -and
            $updateServiceSourceForSleepText -notmatch $updateSleepRequirement) {
            $violations += "main/update_service.[ch]: System Sleep Update participant is incomplete (${updateSleepRequirement})"
        }
    }
    if ($updateServiceSourceForSleepText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += 'main/update_service.c: System Sleep Update participant must not select board electrical wiring or enter MCU sleep'
    }
}

# Fall Detection is a Motion business consumer, not a board driver.  Its
# future System Sleep participant must park the retained sampler after closing
# every side-effect admission; profiles without Motion HAL are already
# quiescent.  This prevents the Waveshare-only worker from leaking IMU/GPIO or
# RTOS ownership into Power, while keeping the same shared transaction on all
# profiles.
$fallDetectionHeaderForSleep = Join-Path $projectRoot 'main/fall_detection_service.h'
$fallDetectionSourceForSleep = Join-Path $projectRoot 'main/fall_detection_service.c'
if (-not (Test-Path -LiteralPath $fallDetectionHeaderForSleep) -or
    -not (Test-Path -LiteralPath $fallDetectionSourceForSleep)) {
    $violations += 'main/fall_detection_service.[ch]: System Sleep Fall Detection participant is missing'
} else {
    $fallDetectionHeaderForSleepText = Get-Content -LiteralPath $fallDetectionHeaderForSleep -Raw
    $fallDetectionSourceForSleepText = Get-Content -LiteralPath $fallDetectionSourceForSleep -Raw
    foreach ($fallSleepRequirement in @(
            'device_status_t\s+fall_detection_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
            'void\s+fall_detection_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
            's_system_sleep_preparing',
            's_system_sleep_quiesced',
            's_system_sleep_evaluations',
            'begin_system_sleep_evaluation\s*\(',
            'end_system_sleep_evaluation\s*\(',
            'ulTaskNotifyTake\s*\(\s*pdTRUE\s*,\s*portMAX_DELAY\s*\)',
            'device_profile_has_capability\s*\(\s*DEVICE_CAPABILITY_MOTION_SENSOR\s*\)'
        )) {
        if ($fallDetectionHeaderForSleepText -notmatch $fallSleepRequirement -and
            $fallDetectionSourceForSleepText -notmatch $fallSleepRequirement) {
            $violations += "main/fall_detection_service.[ch]: System Sleep Fall Detection participant is incomplete (${fallSleepRequirement})"
        }
    }
    if ($fallDetectionHeaderForSleepText -match '\b(?:SemaphoreHandle_t|TaskHandle_t|esp_sleep|gpio_)\b|#include\s*[<"](?:freertos/|driver/)') {
        $violations += 'main/fall_detection_service.h: System Sleep Fall Detection contract must remain value-only'
    }
    if ($fallDetectionSourceForSleepText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += 'main/fall_detection_service.c: System Sleep Fall Detection participant must not select board electrical wiring or enter MCU sleep'
    }
    foreach ($fallPolicyRequirement in @(
            'device_motion_get_sample\s*\(',
            'device_profile_has_capability\s*\(\s*DEVICE_CAPABILITY_MOTION_SENSOR\s*\)',
            'motion_hal_is_definitively_unavailable_at_boot\s*\(',
            'DEVICE_STATUS_UNAVAILABLE\s*\|\|\s*status\s*==\s*DEVICE_STATUS_NOT_FOUND',
            'DEVICE_POWER_LEASE_OWNER_FALL_DETECTION',
            'FALL_DETECTION_EVENT_SUSPECTED',
            'FALL_DETECTION_EVENT_CONFIRMED')) {
        if ($fallDetectionSourceForSleepText -notmatch $fallPolicyRequirement) {
            $violations += "main/fall_detection_service.c: Motion consumer contract is incomplete (${fallPolicyRequirement})"
        }
    }
    if ($platformWakeText -match 'verified_sources\s*=\s*matrix\.(?:light_sleep|deep_sleep)_candidate_sources') {
        $violations += 'main/platform_wake.c: Light/Deep candidate sources must never become verified before electrical HIL'
    }
}

# Sleep Schedule is a shared business-policy worker, not an Alarm alias. Its
# reversible System Sleep participant must fence every mutation/notification
# route while preserving the shared Wake Deadline ownership for future RTC
# hand-off work.
$sleepScheduleHeaderForSleep = Join-Path $projectRoot 'main/sleep_schedule_service.h'
$sleepScheduleSourceForSleep = Join-Path $projectRoot 'main/sleep_schedule_service.c'
if (-not (Test-Path -LiteralPath $sleepScheduleHeaderForSleep) -or
    -not (Test-Path -LiteralPath $sleepScheduleSourceForSleep)) {
    $violations += 'main/sleep_schedule_service.[ch]: System Sleep Schedule participant is missing'
} else {
    $sleepScheduleHeaderForSleepText = Get-Content -LiteralPath $sleepScheduleHeaderForSleep -Raw
    $sleepScheduleSourceForSleepText = Get-Content -LiteralPath $sleepScheduleSourceForSleep -Raw
    foreach ($scheduleSleepRequirement in @(
            'device_status_t\s+sleep_schedule_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
            'void\s+sleep_schedule_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
            's_system_sleep_preparing',
            's_system_sleep_evaluations',
            'begin_system_sleep_evaluation\s*\(',
            'end_system_sleep_evaluation\s*\(',
            '!s_system_sleep_preparing\s*\?\s*s_task\s*:\s*NULL'
        )) {
        if ($sleepScheduleHeaderForSleepText -notmatch $scheduleSleepRequirement -and
            $sleepScheduleSourceForSleepText -notmatch $scheduleSleepRequirement) {
            $violations += "main/sleep_schedule_service.[ch]: System Sleep Schedule participant is incomplete (${scheduleSleepRequirement})"
        }
    }
    if ($sleepScheduleSourceForSleepText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += 'main/sleep_schedule_service.c: System Sleep Schedule participant must not select board electrical wiring or enter MCU sleep'
    }
}

# Ambient's once-per-second cadence is a normal scene producer, not a panel
# adapter.  Its future sleep participant must remain a value-only service seam
# and Power must use it before the Display semantic drain.
$ambientHeader = Join-Path $projectRoot 'main/services/ambient_service.h'
$ambientSource = Join-Path $projectRoot 'main/services/ambient_service.c'
if (-not (Test-Path -LiteralPath $ambientHeader) -or
    -not (Test-Path -LiteralPath $ambientSource)) {
    $violations += 'main/services/ambient_service.[ch]: System Sleep cadence participant is missing'
} else {
    $ambientHeaderText = Get-Content -LiteralPath $ambientHeader -Raw
    $ambientSourceText = Get-Content -LiteralPath $ambientSource -Raw
    foreach ($ambientRequirement in @(
        'device_status_t\s+ambient_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
        'void\s+ambient_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
        's_ambient_task_starting',
        's_system_sleep_preparing',
        's_system_sleep_restart_clock'
    )) {
        if ($ambientHeaderText -notmatch $ambientRequirement -and
            $ambientSourceText -notmatch $ambientRequirement) {
            $violations += "main/services/ambient_service.[ch]: System Sleep cadence participant is incomplete (${ambientRequirement})"
        }
    }
    if ($ambientHeaderText -match '\b(?:esp_err_t|SemaphoreHandle_t|TaskHandle_t|esp_sleep|gpio_)\b') {
        $violations += 'main/services/ambient_service.h: System Sleep cadence contract must remain value-only'
    }
    if ($ambientSourceText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += 'main/services/ambient_service.c: System Sleep cadence participant must not select board electrical wiring or enter MCU sleep'
    }
    foreach ($ambientStartGateRequirement in @(
            'xSemaphoreCreateBinary\s*\(\s*\)',
            'xTaskCreate\s*\(\s*ambient_task',
            'xSemaphoreGive\s*\(\s*start_gate\s*\)',
            'xSemaphoreTake\s*\(\s*start_gate\s*,\s*portMAX_DELAY\s*\)',
            's_system_sleep_preparing\s*\|\|\s*s_ambient_task_starting'
        )) {
        if ($ambientSourceText -notmatch $ambientStartGateRequirement) {
            $violations += "main/services/ambient_service.c: Ambient cadence create/publish sleep fence is incomplete (${ambientStartGateRequirement})"
        }
    }
}

# Connectivity owns a common request-admission counter and may ask the legacy
# composition root to cancel concrete HTTP work through a value-only callback.
# It must not acquire Wi-Fi/ML307/ESP HTTP handles or run the profile-specific
# cancellation itself; Power Service only calls its normalized participant.
$connectivityServiceHeader = Join-Path $projectRoot 'main/connectivity_service.h'
$connectivityServiceSource = Join-Path $projectRoot 'main/connectivity_service.c'
$mainCompositionSource = Join-Path $projectRoot 'main/main.c'
if (-not (Test-Path -LiteralPath $connectivityServiceHeader) -or
    -not (Test-Path -LiteralPath $connectivityServiceSource) -or
    -not (Test-Path -LiteralPath $mainCompositionSource)) {
    $violations += 'main/connectivity_service.[ch] / main/main.c: System Sleep Connectivity participant is missing'
} else {
    $connectivityHeaderText = Get-Content -LiteralPath $connectivityServiceHeader -Raw
    $connectivitySourceText = Get-Content -LiteralPath $connectivityServiceSource -Raw
    $mainCompositionText = Get-Content -LiteralPath $mainCompositionSource -Raw
    if ($mainCompositionText -match '\bs_pet_asset_apply_mutex\b|\bs_loaded_pet_asset_(?:revision|frame_count)\b') {
        $violations += 'main/main.c: pet display application mutex/revision state must remain inside pet_asset_apply_service'
    }
    foreach ($petRootRequirement in @(
            'pet_asset_apply_service_init\s*\(',
            'pet_asset_apply_service_revision_installed\s*\(',
            'pet_asset_apply_service_install_preview\s*\(',
            'pet_asset_apply_service_install_full\s*\(')) {
        if ($mainCompositionText -notmatch $petRootRequirement) {
            $violations += "main/main.c: pet application coordinator routing is incomplete (${petRootRequirement})"
        }
    }
    if ($mainCompositionText -match '\bPET_ASSET_STARTUP_(?:TRANSACTION_ATTEMPTS|RETRY_DELAY_MS)\b' -or
        $mainCompositionText -notmatch 'pet_asset_download_service_fetch_startup_pack\s*\(') {
        $violations += 'main/main.c: startup complete-pack retry must be owned by pet_asset_download_service, not root retry state'
    }
    if ($mainCompositionText -notmatch 'pet_asset_startup_service_apply\s*\(') {
        $violations += 'main/main.c: cold-start pet download/install/cache transaction must be owned by pet_asset_startup_service'
    }
    if ($mainCompositionText -notmatch 'startup_pet_asset_admission_service_admit_pending\s*\(' -or
        $mainCompositionText -notmatch 'startup_pet_asset_admission_service_rearm_preempted\s*\(') {
        $violations += 'main/main.c: cold-start pet admission, retry and audio re-arm policy must be owned by startup_pet_asset_admission_service'
    }
    if ($mainCompositionText -notmatch 'pet_asset_profile_service_apply\s*\(') {
        $violations += 'main/main.c: runtime pet-profile latest-wins/retry policy must be owned by pet_asset_profile_service'
    }
    if ($mainCompositionText -notmatch 'startup_pet_asset_sleep_service_prepare\s*\(' -or
        $mainCompositionText -notmatch 'startup_pet_asset_sleep_service_abort\s*\(') {
        $violations += 'main/main.c: startup pet System Sleep participant order must be owned by startup_pet_asset_sleep_service'
    }
    if ($mainCompositionText -match '\bload_cached_pet_asset\b' -or
        $mainCompositionText -notmatch 'pet_asset_restore_service_restore\s*\(') {
        $violations += 'main/main.c: cached pet validation/install transaction must be owned by pet_asset_restore_service, not root orchestration'
    }
    foreach ($connectivityParticipantRequirement in @(
        'device_status_t\s+connectivity_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
        'void\s+connectivity_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
        'connectivity_service_set_system_sleep_request_canceller',
        's_network_request_users',
        's_cellular_transport_operation_users',
        's_transport_selection_users',
        's_wifi_attempt_users',
        'acquire_transport_selection_admission',
        's_system_sleep_preparing',
        'acquire_wifi_attempt_events',
        'connectivity_service_observe_wifi_got_ip'
    )) {
        if ($connectivityHeaderText -notmatch $connectivityParticipantRequirement -and
            $connectivitySourceText -notmatch $connectivityParticipantRequirement) {
            $violations += "main/connectivity_service.[ch]: System Sleep Connectivity participant is incomplete (${connectivityParticipantRequirement})"
        }
    }
    if ($connectivityServiceSource -and
        $connectivitySourceText -match '\b(?:esp_http_client|ml307_|CONFIG_MACLAW_BOARD_|esp_sleep|gpio_)') {
        $violations += 'main/connectivity_service.c: System Sleep Connectivity participant must not select a concrete HTTP/modem/board/sleep implementation'
    }
    if ($connectivitySourceText -notmatch 'acquire_wifi_attempt_events[\s\S]*?!s_system_sleep_preparing' -or
        $connectivitySourceText -notmatch 'connectivity_service_observe_wifi_got_ip[\s\S]*?!s_system_sleep_preparing' -or
        $connectivitySourceText -notmatch 'connectivity_service_observe_wifi_disconnected[\s\S]*?!s_system_sleep_preparing') {
        $violations += 'main/connectivity_service.c: System Sleep fence must close Wi-Fi attempt admission and reject late IP/disconnect callbacks before they publish readiness'
    }
    if ($connectivitySourceText -notmatch 'acquire_transport_selection_admission[\s\S]*?!s_system_sleep_preparing' -or
        $connectivitySourceText -notmatch 'acquire_transport_selection_admission[\s\S]*?s_network_request_users\s*==\s*0' -or
        $connectivitySourceText -notmatch 'acquire_transport_selection_admission[\s\S]*?s_cellular_network_request_users\s*==\s*0' -or
        $connectivitySourceText -notmatch 'acquire_transport_selection_admission[\s\S]*?s_cellular_transport_operation_users\s*==\s*0' -or
        $connectivitySourceText -notmatch 'connectivity_service_prepare_system_sleep[\s\S]*?s_transport_selection_users\s*==\s*0' -or
        $connectivitySourceText -notmatch 'connectivity_service_prepare_system_sleep[\s\S]*?s_cellular_transport_operation_users\s*==\s*0' -or
        $connectivitySourceText -notmatch 'connectivity_service_deinit_legacy[\s\S]*?s_cellular_transport_operation_users\s*==\s*0' -or
        $connectivitySourceText -notmatch 'connectivity_service_set_active_uplink[\s\S]*?acquire_transport_selection_admission') {
        $violations += 'main/connectivity_service.c: uplink selection must wait for network borrowers/physical cellular operations and System Sleep/deinit must drain them before transport park or release'
    }
    if ($connectivitySourceText -notmatch 'wake_wifi_attempt_waiters_for_system_sleep' -or
        $connectivitySourceText -notmatch 'connectivity_service_prepare_system_sleep[\s\S]*?wake_wifi_attempt_waiters_for_system_sleep' -or
        $connectivitySourceText -notmatch 'connectivity_service_prepare_system_sleep[\s\S]*?s_wifi_attempt_users\s*==\s*0' -or
        $connectivitySourceText -notmatch 'connectivity_service_wait_wifi_attempt[\s\S]*?s_system_sleep_preparing') {
        $violations += 'main/connectivity_service.c: System Sleep fence must wake and drain pre-fence Wi-Fi waiters before profile transport park'
    }
    foreach ($cellularSleepEntry in @(
            'connectivity_service_prepare_cellular_transport',
            'connectivity_service_start_cellular_transport',
            'connectivity_service_establish_cellular_transport',
            'connectivity_service_quiesce_cellular_transport',
            'connectivity_service_is_cellular_transport_ready')) {
        $cellularSleepProtected =
            $connectivitySourceText -match "$cellularSleepEntry[\s\S]*?s_system_sleep_preparing"
        if (-not $cellularSleepProtected -and
            $cellularSleepEntry -ne 'connectivity_service_is_cellular_transport_ready') {
            $cellularSleepProtected =
                $connectivitySourceText -match "$cellularSleepEntry[\s\S]*?begin_cellular_transport_(?:operation|quiesce)"
        }
        if (-not $cellularSleepProtected) {
            $violations += "main/connectivity_service.c: System Sleep fence must reject $cellularSleepEntry before it can touch or publish profile cellular state"
        }
    }
    if ($connectivitySourceText -notmatch 'begin_cellular_transport_operation[\s\S]*?s_connectivity_initialized' -or
        $connectivitySourceText -notmatch 'begin_cellular_transport_operation[\s\S]*?!s_system_sleep_preparing' -or
        $connectivitySourceText -notmatch 'begin_cellular_transport_operation[\s\S]*?s_active_uplink\s*==\s*DEVICE_UPLINK_CELLULAR' -or
        $connectivitySourceText -notmatch 'begin_cellular_transport_operation[\s\S]*?s_cellular_transport_operation_users\s*==\s*0' -or
        $connectivitySourceText -notmatch 'begin_cellular_transport_operation[\s\S]*?s_network_request_users\s*==\s*0' -or
        $connectivitySourceText -notmatch 'begin_cellular_transport_operation[\s\S]*?s_cellular_network_request_users\s*==\s*0' -or
        $connectivitySourceText -notmatch 'begin_cellular_network_request[\s\S]*?s_cellular_transport_operation_users\s*==\s*0' -or
        $connectivitySourceText -notmatch 'begin_cellular_transport_operation[\s\S]*?s_cellular_transport_operation_users') {
        $violations += 'main/connectivity_service.c: cellular physical lifecycle and request admissions must be mutually exclusive within the selected awake generation'
    }
    foreach ($cellularAdmissionEntry in @(
            'connectivity_service_prepare_cellular_transport',
            'connectivity_service_start_cellular_transport',
            'connectivity_service_establish_cellular_transport')) {
        if ($connectivitySourceText -notmatch "$cellularAdmissionEntry[\s\S]*?begin_cellular_transport_operation" -or
            $connectivitySourceText -notmatch "$cellularAdmissionEntry[\s\S]*?end_cellular_transport_operation") {
            $violations += "main/connectivity_service.c: cellular split lifecycle must retain/release physical-operation admission around $cellularAdmissionEntry before its profile adapter call"
        }
    }
    if ($connectivitySourceText -notmatch 'connectivity_service_quiesce_cellular_transport[\s\S]*?begin_cellular_transport_quiesce' -or
        $connectivitySourceText -notmatch 'connectivity_service_quiesce_cellular_transport[\s\S]*?end_cellular_transport_operation') {
        $violations += 'main/connectivity_service.c: terminal cellular quiesce must retain/release its physical-operation admission before the profile adapter call'
    }
    if ($connectivitySourceText -notmatch 'begin_cellular_network_request[\s\S]*?s_connectivity_initialized' -or
        $connectivitySourceText -notmatch 'begin_cellular_network_request[\s\S]*?s_active_uplink\s*==\s*DEVICE_UPLINK_CELLULAR' -or
        $connectivitySourceText -notmatch 's_cellular_network_request_users' -or
        $connectivitySourceText -notmatch 'connectivity_service_cellular_http_request[\s\S]*?begin_cellular_network_request' -or
        $connectivitySourceText -notmatch 'connectivity_service_cellular_http_stream_request[\s\S]*?begin_cellular_network_request' -or
        $connectivitySourceText -notmatch 'connectivity_service_cancel_cellular_foreground_request[\s\S]*?s_cellular_network_request_users' -or
        $connectivitySourceText -notmatch 'connectivity_service_cancel_cellular_requests_for_owner[\s\S]*?s_cellular_network_request_users') {
        $violations += 'main/connectivity_service.c: cellular HTTP/stream and cancellation must be bounded by selected-cellular request admission before reaching the profile adapter'
    }
    foreach ($cellularCancelCaller in @(
            (Join-Path $projectRoot 'main/services/gateway_lifecycle_service.c'),
            (Join-Path $projectRoot 'main/services/gateway_transport.c'),
            (Join-Path $projectRoot 'main/services/command_service.c'),
            (Join-Path $projectRoot 'main/services/interaction_service.c'))) {
        if (-not (Test-Path -LiteralPath $cellularCancelCaller)) {
            $violations += "missing cellular request cancellation caller $cellularCancelCaller"
            continue
        }
        $cellularCancelCallerText = Get-Content -LiteralPath $cellularCancelCaller -Raw
        if ($cellularCancelCallerText -match 'device_connectivity_is_active_cellular\s*\(\s*\)[\s\S]{0,160}?device_connectivity_cancel_cellular') {
            $violations += "${cellularCancelCaller}: in-flight cellular request cancellation must not be gated by current active uplink"
        }
    }
    # A generic request drain cannot see retained profile transport workers.
    # Require the normalized Platform Connectivity bridge, with a matching
    # ABORT before the service reopens generic request admission. The public
    # contract stays value-only; ML307 is still reachable solely in Fangtang's
    # private adapter.
    $platformConnectivityHeader = Join-Path $projectRoot 'main/platform_connectivity.h'
    $platformConnectivitySource = Join-Path $projectRoot 'main/platform_connectivity.c'
    $platformConnectivityProfileHeader = Join-Path $projectRoot 'main/platform_connectivity_profile.h'
    $compactConnectivityProfileSource = Join-Path $projectRoot 'main/platform_connectivity_compact.c'
    $roundConnectivityProfileSource = Join-Path $projectRoot 'main/platform_connectivity_round.c'
    $fangtangTransportSleepSource = Join-Path $projectRoot 'main/boards/fangtang_4g/fangtang_ml307_transport.cpp'
    $fangtangTransportSleepHeader = Join-Path $projectRoot 'main/boards/fangtang_4g/fangtang_ml307_transport.h'
    if (-not (Test-Path -LiteralPath $platformConnectivityHeader) -or
        -not (Test-Path -LiteralPath $platformConnectivitySource) -or
        -not (Test-Path -LiteralPath $platformConnectivityProfileHeader) -or
        -not (Test-Path -LiteralPath $compactConnectivityProfileSource) -or
        -not (Test-Path -LiteralPath $roundConnectivityProfileSource) -or
        -not (Test-Path -LiteralPath $fangtangTransportSleepSource) -or
        -not (Test-Path -LiteralPath $fangtangTransportSleepHeader)) {
        $violations += 'Platform/Fangtang Connectivity System Sleep transport fence sources are missing'
    } else {
        $platformConnectivityHeaderText = Get-Content -LiteralPath $platformConnectivityHeader -Raw
        $platformConnectivitySourceText = Get-Content -LiteralPath $platformConnectivitySource -Raw
        $platformConnectivityProfileHeaderText = Get-Content -LiteralPath $platformConnectivityProfileHeader -Raw
        $compactConnectivityProfileText = Get-Content -LiteralPath $compactConnectivityProfileSource -Raw
        $roundConnectivityProfileText = Get-Content -LiteralPath $roundConnectivityProfileSource -Raw
        $fangtangTransportSleepText = Get-Content -LiteralPath $fangtangTransportSleepSource -Raw
        $fangtangTransportSleepHeaderText = Get-Content -LiteralPath $fangtangTransportSleepHeader -Raw
        foreach ($transportSleepRequirement in @(
                'platform_connectivity_prepare_system_sleep\s*\(',
                'platform_connectivity_abort_system_sleep_prepare\s*\(',
                'platform_connectivity_profile_prepare_system_sleep\s*\(',
                'platform_connectivity_profile_abort_system_sleep_prepare\s*\(')) {
            if ($platformConnectivityHeaderText -notmatch $transportSleepRequirement -and
                $platformConnectivitySourceText -notmatch $transportSleepRequirement -and
                $platformConnectivityProfileHeaderText -notmatch $transportSleepRequirement) {
                $violations += "Platform Connectivity System Sleep transport contract is incomplete (${transportSleepRequirement})"
            }
        }
        if ($platformConnectivityHeaderText -match '\b(?:esp_err_t|SemaphoreHandle_t|TaskHandle_t|ml307_|gpio_|esp_sleep)\b') {
            $violations += 'main/platform_connectivity.h: System Sleep transport contract must remain value-only'
        }
        if ($platformConnectivitySourceText -match '\b(?:ml307_|gpio_|esp_sleep|CONFIG_MACLAW_BOARD_)') {
            $violations += 'main/platform_connectivity.c: shared System Sleep transport facade must not select a board/modem/sleep implementation'
        }
        if ($compactConnectivityProfileText -notmatch 'compact_connectivity_service_prepare_system_sleep' -or
            $compactConnectivityProfileText -notmatch 'compact_connectivity_service_abort_system_sleep_prepare') {
            $violations += 'main/platform_connectivity_compact.c: compact profile must route System Sleep transport fence through private Connectivity service'
        }
        if ($roundConnectivityProfileText -notmatch 'platform_connectivity_profile_prepare_system_sleep' -or
            $roundConnectivityProfileText -notmatch 'DEVICE_STATUS_OK') {
            $violations += 'main/platform_connectivity_round.c: Wi-Fi-only round profile must explicitly provide the no-op System Sleep transport fence'
        }
        foreach ($fangtangTransportSleepRequirement in @(
                'ml307_transport_prepare_system_sleep\s*\(',
                'ml307_transport_abort_system_sleep_prepare\s*\(',
                's_system_sleep_preparing',
                's_system_sleep_was_admitted',
                's_system_sleep_probe_was_running',
                's_network_probe_restart_after_stop',
                'close_transport_and_drain\s*\(')) {
            if ($fangtangTransportSleepText -notmatch $fangtangTransportSleepRequirement -and
                $fangtangTransportSleepHeaderText -notmatch $fangtangTransportSleepRequirement) {
                $violations += "main/boards/fangtang_4g/fangtang_ml307_transport.[ch]: reversible System Sleep transport fence is incomplete (${fangtangTransportSleepRequirement})"
            }
        }
        if ($connectivitySourceText -notmatch 'platform_connectivity_prepare_system_sleep' -or
            $connectivitySourceText -notmatch 'platform_connectivity_abort_system_sleep_prepare') {
            $violations += 'main/connectivity_service.c: System Sleep request fence must compose the normalized profile transport fence and its ABORT'
        }
    }
    if ($mainCompositionText -notmatch 'connectivity_service_set_system_sleep_request_canceller' -or
        $mainCompositionText -match 's_gateway_(?:poll|asset|startup)_active_client|s_foreground_http_client|cancel_active_gateway_http_requests_for_system_sleep') {
        $violations += 'main/main.c: composition root must not retain Gateway HTTP-client registry or cancellation plumbing'
    }
    $gatewayLifecycleSource = Join-Path $projectRoot 'main/services/gateway_lifecycle_service.c'
    $gatewayLifecycleHeader = Join-Path $projectRoot 'main/services/gateway_lifecycle_service.h'
    if (-not (Test-Path -LiteralPath $gatewayLifecycleSource) -or
        -not (Test-Path -LiteralPath $gatewayLifecycleHeader)) {
        $violations += 'main/services/gateway_lifecycle_service.[ch]: Connectivity-domain Gateway lifecycle coordinator is missing'
    } else {
        $gatewayLifecycleText = Get-Content -LiteralPath $gatewayLifecycleSource -Raw
        $gatewayLifecycleHeaderText = Get-Content -LiteralPath $gatewayLifecycleHeader -Raw
        foreach ($gatewayLifecycleRequirement in @(
                'gateway_lifecycle_service_prepare_system_sleep\s*\(',
                'gateway_lifecycle_service_abort_system_sleep_prepare\s*\(',
                'gateway_transport_cancel_active_requests\s*\(',
                'gateway_transport_prepare_system_sleep\s*\(',
                'gateway_dispatcher_prepare_system_sleep\s*\(',
                'meeting_service_prepare_capability_refresh_system_sleep\s*\(')) {
            if ($gatewayLifecycleText -notmatch $gatewayLifecycleRequirement -and
                $gatewayLifecycleHeaderText -notmatch $gatewayLifecycleRequirement) {
                $violations += "main/services/gateway_lifecycle_service.[ch]: Gateway cancel/join/resume ownership is incomplete ($gatewayLifecycleRequirement)"
            }
        }
        if ($gatewayLifecycleHeaderText -match 'esp_http_client|TaskHandle_t|esp_err_t|cJSON') {
            $violations += 'main/services/gateway_lifecycle_service.h: public lifecycle contract must remain value-only'
        }
        if ($mainCompositionText -notmatch 'gateway_lifecycle_service_prepare_system_sleep\s*\(' -or
            $mainCompositionText -notmatch 'gateway_lifecycle_service_abort_system_sleep_prepare\s*\(') {
            $violations += 'main/main.c: composition root must delegate Gateway worker lifecycle to the Connectivity-domain coordinator'
        }
    }
    # SNTP remains an ESP-NETIF resource, but its retry monitor/singleton has
    # been extracted from main.c into the Connectivity-domain Clock Sync
    # Service. The root may only call its value API; the service retains the
    # generation-bound PREPARE/ABORT fence and queued-callback rejection.
    $clockSyncServiceSource = Join-Path $projectRoot 'main/services/clock_sync_service.c'
    $clockSyncServiceHeader = Join-Path $projectRoot 'main/services/clock_sync_service.h'
    if (-not (Test-Path -LiteralPath $clockSyncServiceSource) -or
        -not (Test-Path -LiteralPath $clockSyncServiceHeader)) {
        $violations += 'main/services/clock_sync_service.[ch]: SNTP lifecycle service is missing'
    } else {
        $clockSyncServiceText = Get-Content -LiteralPath $clockSyncServiceSource -Raw
        $clockSyncHeaderText = Get-Content -LiteralPath $clockSyncServiceHeader -Raw
        foreach ($clockSleepRequirement in @(
                'device_status_t\s+clock_sync_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
                'void\s+clock_sync_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
                's_system_sleep_preparing',
                's_system_sleep_was_initialized',
                's_system_sleep_callback_users',
                's_system_sleep_restart_pending',
                'clock_sync_service_stop\s*\(\s*remaining\s*\)',
                'start_internal\s*\(\s*true\s*\)'
            )) {
            if ($clockSyncServiceText -notmatch $clockSleepRequirement -and
                $clockSyncHeaderText -notmatch $clockSleepRequirement) {
                $violations += "main/services/clock_sync_service.[ch]: SNTP System Sleep fence is incomplete (${clockSleepRequirement})"
            }
        }
        if ($clockSyncServiceText -notmatch 'if\s*\(!admitted\)\s*return\s*;' -or
            $clockSyncServiceText -notmatch 's_system_sleep_restart_pending\s*=\s*true' -or
            $clockSyncServiceText -notmatch 's_retiring') {
            $violations += 'main/services/clock_sync_service.c: queued callback rejection or deferred Registry-safe rollback is incomplete'
        }
        if ($clockSyncHeaderText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|cJSON|board_|platform_)\b') {
            $violations += 'main/services/clock_sync_service.h: public clock-sync contract leaked SDK, RTOS, parser, or board detail'
        }
    }
    foreach ($clockRootRequirement in @(
            'clock_sync_service_prepare_system_sleep\s*\(\s*remaining_ms\s*\)',
            'clock_sync_service_abort_system_sleep_prepare\s*\(\s*\)',
            'network_root_stop_clock_sync\s*\([\s\S]*?clock_sync_service_stop\s*\(\s*timeout_ms\s*\)',
            'clock_sync_service_start\s*\(\s*false\s*\)',
            'cellular_recovery_service_prepare_system_sleep\s*\(\s*remaining_ms\s*\)',
            'cellular_recovery_service_abort_system_sleep_prepare\s*\(\s*\)',
            'device_status_t\s+prepare_wake_restart_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
            'void\s+abort_wake_restart_system_sleep_prepare\s*\(\s*void\s*\)',
            'wake_restart_worker_service_prepare_system_sleep\s*\(\s*timeout_ms\s*\)',
            'wake_restart_worker_service_abort_system_sleep_prepare\s*\(\s*\)',
            'prepare_wake_restart_system_sleep\s*\(\s*remaining_ms\s*\)',
            'abort_wake_restart_system_sleep_prepare\s*\(\s*\)',
            'device_status_t\s+prepare_deferred_setup_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
            'void\s+abort_deferred_setup_system_sleep_prepare\s*\(\s*void\s*\)',
            'deferred_setup_worker_service_prepare_system_sleep\s*\(\s*timeout_ms\s*\)',
            'deferred_setup_worker_service_abort_system_sleep_prepare\s*\(\s*\)',
            'prepare_deferred_setup_system_sleep\s*\(\s*remaining_ms\s*\)',
            'abort_deferred_setup_system_sleep_prepare\s*\(\s*\)'
        )) {
        if ($mainCompositionText -notmatch $clockRootRequirement) {
            $violations += "main/main.c: Clock Sync Service composition root wiring is incomplete (${clockRootRequirement})"
        }
    }
    $cellularRecoveryServiceSource = Join-Path $projectRoot 'main/services/cellular_recovery_service.c'
    if (-not (Test-Path -LiteralPath $cellularRecoveryServiceSource)) {
        $violations += 'main/services/cellular_recovery_service.c: ML307 recovery System Sleep participant is missing'
    } else {
        $cellularRecoveryServiceText = Get-Content -LiteralPath $cellularRecoveryServiceSource -Raw
        if ($cellularRecoveryServiceText -notmatch 's_admission_open\s*&&\s*!s_system_sleep_preparing') {
            $violations += 'main/services/cellular_recovery_service.c: ML307 recovery must not admit a new coordinator during System Sleep PREPARE'
        }
        if ($cellularRecoveryServiceText -notmatch 's_system_sleep_restart_pending\s*=\s*true' -or
            $cellularRecoveryServiceText -notmatch 'cannot unregister recovery before deferred sleep rollback' -or
            $cellularRecoveryServiceText -notmatch 'cannot restore cellular recovery after sleep rollback') {
            $violations += 'main/services/cellular_recovery_service.c: ML307 recovery System Sleep ABORT must defer a timed-out generation restart until its old Registry identity exits'
        }
    }
    if ($mainCompositionText -notmatch 'wake_restart_worker_service_start\s*\(\s*\)') {
        $violations += 'main/main.c: offline wake restart must delegate coordinator admission to its System Sleep fenced worker service'
    }
    if ($mainCompositionText -notmatch 'wake_restart_worker_service_prepare_system_sleep\s*\(\s*timeout_ms\s*\)' -or
        $mainCompositionText -notmatch 'wake_restart_worker_service_abort_system_sleep_prepare\s*\(\s*\)') {
        $violations += 'main/main.c: offline wake restart System Sleep fence must remain delegated to its lifecycle owner'
    }
    if ($mainCompositionText -notmatch 'deferred_setup_worker_service_start\s*\(\s*\)\s*==\s*DEVICE_STATUS_OK') {
        $violations += 'main/main.c: deferred setup must delegate portal-coordinator admission to its System Sleep fenced worker service'
    }
    # Startup-pet task/Registry retirement is now private to the lifecycle
    # service. Root retains only the reversible domain state and invokes the
    # public ABORT after releasing its own lock.
    $startupPetWorkerHeader = Join-Path $projectRoot 'main/services/startup_pet_worker_service.h'
    $startupPetWorkerSource = Join-Path $projectRoot 'main/services/startup_pet_worker_service.c'
    if (-not (Test-Path -LiteralPath $startupPetWorkerHeader) -or
        -not (Test-Path -LiteralPath $startupPetWorkerSource)) {
        $violations += 'main/services/startup_pet_worker_service.[ch]: startup pet lifecycle coordinator is missing'
    } else {
        $startupPetWorkerHeaderText = Get-Content -LiteralPath $startupPetWorkerHeader -Raw
        $startupPetWorkerText = Get-Content -LiteralPath $startupPetWorkerSource -Raw
        foreach ($startupPetWorkerRequirement in @(
                's_starting',
                's_retiring',
                's_system_sleep_restart_pending',
                'TASK_REGISTRY_OWNER_CONNECTIVITY,\s*\(void \*\)self,\s*10',
                'restart_after_system_sleep_abort',
                'xTaskCreatePinnedToCoreWithCaps',
                'MALLOC_CAP_SPIRAM\s*\|\s*MALLOC_CAP_8BIT',
                'startup_pet_worker_service_prepare_system_sleep',
                'startup_pet_worker_service_abort_system_sleep_prepare'
            )) {
            if ($startupPetWorkerText -notmatch $startupPetWorkerRequirement) {
                $violations += "main/services/startup_pet_worker_service.c: startup pet worker lifecycle is incomplete (${startupPetWorkerRequirement})"
            }
        }
        if ($startupPetWorkerHeaderText -match '\b(?:esp_|freertos/|driver/|board_port|CONFIG_MACLAW_BOARD_|TaskHandle_t|SemaphoreHandle_t|esp_timer_handle_t|cJSON)\b') {
            $violations += 'main/services/startup_pet_worker_service.h: worker lifecycle contract must remain value-only and hardware/RTOS/JSON-neutral'
        }
        if ($mainCompositionText -match '\bs_startup_pet_asset_(?:task|stopped|start_gate|starting|stop_requested|retiring)\b|\bs_startup_pet_system_sleep_restart_pending\b' -or
            $mainCompositionText -notmatch 'startup_pet_worker_service_(?:init|start|stop|prepare_system_sleep|abort_system_sleep_prepare|active|stop_requested|is_current_worker)') {
            $violations += 'main/main.c: startup pet worker task/Registry state must be owned by its lifecycle service and root must use the public seam'
        }
    }
    $startupPetRetryHeader = Join-Path $projectRoot 'main/services/startup_pet_retry_service.h'
    $startupPetRetrySource = Join-Path $projectRoot 'main/services/startup_pet_retry_service.c'
    if (-not (Test-Path -LiteralPath $startupPetRetryHeader) -or
        -not (Test-Path -LiteralPath $startupPetRetrySource)) {
        $violations += 'main/services/startup_pet_retry_service.[ch]: startup pet retry coordinator is missing'
    } else {
        $startupPetRetryHeaderText = Get-Content -LiteralPath $startupPetRetryHeader -Raw
        $startupPetRetryText = Get-Content -LiteralPath $startupPetRetrySource -Raw
        foreach ($startupPetRetryRequirement in @(
                'startup_pet_retry_service_init\s*\(',
                'startup_pet_retry_service_schedule\s*\(',
                'startup_pet_retry_service_take_due\s*\(',
                'startup_pet_retry_service_stop\s*\(',
                'startup_pet_retry_service_prepare_system_sleep\s*\(',
                'startup_pet_retry_service_abort_system_sleep_prepare\s*\(',
                's_callback_admission_open',
                's_system_sleep_preparing',
                's_callbacks_inflight',
                'esp_timer_start_once',
                'retry_timer_cb')) {
            if ($startupPetRetryText -notmatch $startupPetRetryRequirement) {
                $violations += "main/services/startup_pet_retry_service.c: retry coordinator is incomplete (${startupPetRetryRequirement})"
            }
        }
        if ($startupPetRetryHeaderText -match '\b(?:esp_|freertos/|driver/|board_port|CONFIG_MACLAW_BOARD_|TaskHandle_t|SemaphoreHandle_t|esp_timer_handle_t|cJSON)\b') {
            $violations += 'main/services/startup_pet_retry_service.h: retry contract must remain value-only and hardware/RTOS/JSON-neutral'
        }
        if ($mainCompositionText -match '\bs_startup_pet_retry_(?:timer|callback|due|count)' -or
            $mainCompositionText -notmatch 'startup_pet_retry_service_(?:init|schedule|take_due|stop|prepare_system_sleep|abort_system_sleep_prepare)') {
            $violations += 'main/main.c: startup pet retry timer/callback state must be owned by its service and wired only through its public API'
        }
        if ($mainCompositionText -match '\bs_startup_pet_system_sleep_(?:preparing|was_pending|was_preempted_by_audio)\b' -or
            $mainCompositionText -notmatch 'startup_pet_asset_state_service_(?:prepare_system_sleep|abort_system_sleep_prepare|system_sleep_preparing)') {
            $violations += 'main/main.c: startup pet System Sleep descriptor facts must remain inside startup_pet_asset_state_service'
        }
    }
    $petCacheServiceSource = Join-Path $projectRoot 'main/services/pet_cache_service.c'
    if (-not (Test-Path -LiteralPath $petCacheServiceSource)) {
        $violations += 'main/services/pet_cache_service.c: pet cache coordinator is missing'
    } else {
    $petCacheServiceText = Get-Content -LiteralPath $petCacheServiceSource -Raw
    foreach ($petCacheLifecycleRequirement in @(
                's_starting',
                's_retiring',
                's_exit_status',
                's_registry_retirement_failed',
                'service_admission_open\s*\(',
                's_initialized\s*&&\s*!s_stop_requested\s*&&\s*!s_system_sleep_preparing\s*&&\s*!s_registry_retirement_failed',
                'stop_registry_entry\s*\(',
                'name\s*=\s*"pet_cache"',
                'TASK_REGISTRY_OWNER_STORAGE,\s*\(void \*\)self,\s*10',
                'xTaskCreatePinnedToCoreWithCaps',
                'MALLOC_CAP_INTERNAL\s*\|\s*MALLOC_CAP_8BIT'
            )) {
            if ($petCacheServiceText -notmatch $petCacheLifecycleRequirement) {
                $violations += "main/services/pet_cache_service.c: pet cache Flash/VFS worker must retain an immutable Storage Registry lifecycle identity (${petCacheLifecycleRequirement})"
            }
        }
        if ($petCacheServiceText -notmatch 'const esp_err_t registry_err = task_registry_register[\s\S]*?s_task = task;\s*s_starting = false;' -or
            $petCacheServiceText -notmatch 'descriptor->frame_count < 1[\s\S]*?descriptor->frame_count > \(int\)PET_ASSET_SERVICE_MAX_FRAMES[\s\S]*?for \(int i = 0; i < descriptor->frame_count; \+\+i\)\s*\{\s*if \(!frames\[i\]\)') {
            $violations += 'main/services/pet_cache_service.c: pet cache must publish a worker only after Registry registration and fail closed for incomplete background frame descriptors'
        }
        if ($mainCompositionText -notmatch 'pet_cache_service_(?:init|stop|prepare_system_sleep|abort_system_sleep_prepare)') {
            $violations += 'main/main.c: pet cache coordinator composition-root wiring is incomplete'
        }
    }
    $meetingServiceSource = Join-Path $projectRoot 'main/services/meeting_service.c'
    if (-not (Test-Path $meetingServiceSource)) {
        $violations += 'main/services/meeting_service.c: missing Meeting Service System Sleep lifecycle implementation'
    } else {
        $meetingServiceText = Get-Content -Raw $meetingServiceSource
        foreach ($meetingSleepRequirement in @(
                's_resume_supervisor_system_sleep_restart_pending',
                's_resume_supervisor_retiring',
                's_capability_refresh_system_sleep_restart_pending',
                's_capability_refresh_retiring',
                'cannot defer-restart meeting resume supervisor after system-sleep abort',
                'cannot defer-restart meeting capability refresh after system-sleep abort'
            )) {
            if ($meetingServiceText -notmatch $meetingSleepRequirement) {
                $violations += "main/services/meeting_service.c: Meeting System Sleep cross-generation rollback is incomplete (${meetingSleepRequirement})"
            }
        }
    }
    foreach ($gatewaySystemSleepSource in @(
            (Join-Path $projectRoot 'main/services/gateway_transport.c'),
            (Join-Path $projectRoot 'main/services/gateway_dispatcher.c')
        )) {
        if (-not (Test-Path $gatewaySystemSleepSource)) {
            $violations += "${gatewaySystemSleepSource}: missing Gateway System Sleep lifecycle implementation"
            continue
        }
        $gatewaySystemSleepText = Get-Content -Raw $gatewaySystemSleepSource
        if ($gatewaySystemSleepText -notmatch 's_system_sleep_restart_pending' -or
            $gatewaySystemSleepText -notmatch 'retiring' -or
            $gatewaySystemSleepText -notmatch 'cannot defer-restart gateway') {
            $violations += "${gatewaySystemSleepSource}: Gateway System Sleep ABORT must defer a timed-out generation restart until its old Registry identity exits"
        }
    }
    $ambientServiceSource = Join-Path $projectRoot 'main/services/ambient_service.c'
    if (-not (Test-Path $ambientServiceSource)) {
        $violations += 'main/services/ambient_service.c: missing Ambient System Sleep lifecycle implementation'
    } else {
        $ambientServiceText = Get-Content -Raw $ambientServiceSource
        foreach ($ambientSleepRequirement in @(
                's_system_sleep_restart_pending',
                's_ambient_task_retiring',
                'restart_after_system_sleep_abort',
                'ambient_service_ensure_clock_task\s*\(\s*\)'
            )) {
            if ($ambientServiceText -notmatch $ambientSleepRequirement) {
                $violations += "main/services/ambient_service.c: Ambient System Sleep ABORT cross-generation rollback is incomplete (${ambientSleepRequirement})"
            }
        }
    }
    $gatewayTransportSource = Join-Path $projectRoot 'main/services/gateway_transport.c'
    $gatewayTransportHeader = Join-Path $projectRoot 'main/services/gateway_transport.h'
    $gatewayDispatcherSource = Join-Path $projectRoot 'main/services/gateway_dispatcher.c'
    $gatewayDispatcherHeader = Join-Path $projectRoot 'main/services/gateway_dispatcher.h'
    if (-not (Test-Path -LiteralPath $gatewayTransportSource)) {
        $violations += 'main/services/gateway_transport.c: Gateway Transport source is missing for System Sleep request-admission audit'
    } else {
        $gatewayTransportText = Get-Content -LiteralPath $gatewayTransportSource -Raw
        $gatewayTransportHeaderText = if (Test-Path -LiteralPath $gatewayTransportHeader) {
            Get-Content -LiteralPath $gatewayTransportHeader -Raw
        } else { '' }
        $gatewayDispatcherText = if (Test-Path -LiteralPath $gatewayDispatcherSource) {
            Get-Content -LiteralPath $gatewayDispatcherSource -Raw
        } else { '' }
        $gatewayDispatcherHeaderText = if (Test-Path -LiteralPath $gatewayDispatcherHeader) {
            Get-Content -LiteralPath $gatewayDispatcherHeader -Raw
        } else { '' }
        $voicePairStart = $gatewayTransportText.IndexOf('gateway_transport_pair_by_voice')
        $voicePairEnd = $gatewayTransportText.IndexOf('static esp_err_t pair_by_code', $voicePairStart)
        $voicePairText = if ($voicePairStart -ge 0 -and $voicePairEnd -gt $voicePairStart) {
            $gatewayTransportText.Substring($voicePairStart, $voicePairEnd - $voicePairStart)
        } else { '' }
        if ($voicePairText -notmatch 'device_connectivity_begin_network_request' -or
            $voicePairText -notmatch 'device_connectivity_end_network_request' -or
            $voicePairText -notmatch 'publish_active_client\s*\(\s*GATEWAY_ACTIVE_LANE_FOREGROUND' -or
            $voicePairText -notmatch 'clear_active_client\s*\(\s*GATEWAY_ACTIVE_LANE_FOREGROUND' -or
            $gatewayTransportText -notmatch 'gateway_transport_cancel_active_requests\s*\(' -or
            $gatewayTransportText -notmatch 's_active_clients_mutex') {
            $violations += 'main/services/gateway_transport.c: Wi-Fi voice pairing must use both the Connectivity request fence and the cancellable foreground-client registry'
        }
    }
    $meetingServiceSource = Join-Path $projectRoot 'main/services/meeting_service.c'
    if (-not (Test-Path -LiteralPath $meetingServiceSource)) {
        $violations += 'main/services/meeting_service.c: Meeting Service source is missing for streaming-transport ownership audit'
    } else {
        $meetingServiceText = Get-Content -LiteralPath $meetingServiceSource -Raw
        if ($gatewayTransportText -notmatch 'gateway_transport_stream_meeting_chunk\s*\(' -or
            $gatewayTransportText -notmatch 'gateway_transport_cancel_meeting_stream\s*\(' -or
            $gatewayTransportText -notmatch 's_active_meeting_stream_client' -or
            $meetingServiceText -notmatch 'gateway_transport_stream_meeting_chunk\s*\(' -or
            $meetingServiceText -notmatch 'gateway_transport_cancel_meeting_stream\s*\(') {
            $violations += 'Gateway Transport: meeting stream must own the active client/cancel lane while Meeting Service uses only value contracts'
        }
    }
    if ($mainCompositionText -match 's_meeting_task_active_client|s_meeting_upload_reusable_client|s_meeting_task_client_mutex') {
        $violations += 'main/main.c: meeting streaming HTTP client ownership must remain in Gateway Transport, not composition root'
    }

    # Text/control events use the same Gateway Transport envelope and request
    # lane as every other incoming message.  Keep cJSON/HTTP construction out
    # of the composition root so command cancellation cannot bypass transport
    # admission, bearer handling, or active-client cancellation.
    if ($gatewayTransportHeaderText -notmatch 'gateway_transport_send_text_event\s*\(' -or
        $gatewayTransportText -notmatch 'int32_t\s+gateway_transport_send_text_event\s*\(' -or
        $gatewayTransportText -notmatch '"/api/im-gateway/v1/incoming"' -or
        $gatewayTransportText -notmatch '"replyToMessageId"' -or
        $gatewayTransportText -notmatch '"accepted"') {
        $violations += 'Gateway Transport: text-event envelope/ACK contract is missing'
    }
    if ($gatewayTransportHeaderText -notmatch 'gateway_transport_upload_voice\s*\(' -or
        $gatewayTransportHeaderText -notmatch 'gateway_transport_send_voice_event\s*\(' -or
        $gatewayTransportText -notmatch 'int32_t\s+gateway_transport_upload_voice\s*\(' -or
        $gatewayTransportText -notmatch 'int32_t\s+gateway_transport_send_voice_event\s*\(' -or
        $mainCompositionText -match 'static\s+esp_err_t\s+(?:upload_voice|send_voice_event)\s*\(') {
        $violations += 'Gateway Transport: voice upload/submit ownership must not remain in main.c'
    }
    if ($gatewayDispatcherHeaderText -notmatch 'release_audio' -or
        $gatewayDispatcherText -notmatch 'release_audio\s*\(' -or
        $mainCompositionText -match '\bfree\s*\(\s*audio\s*\)') {
        $violations += 'Gateway Dispatcher: downloaded audio buffers require an explicit transport-owned release seam'
    }
    if ($gatewayTransportHeaderText -notmatch 'gateway_transport_post_json\s*\(' -or
        $gatewayTransportText -notmatch 'int32_t\s+gateway_transport_post_json\s*\(' -or
        $mainCompositionText -match 'request\s*\(\s*"POST"\s*,\s*"/api/im-gateway/v1/tool-result"') {
        $violations += 'Gateway Transport: tool-result POST must use the shared transport JSON lane'
    }
    if ($gatewayTransportHeaderText -notmatch 'gateway_transport_ack_messages\s*\(' -or
        $gatewayTransportText -notmatch 'int32_t\s+gateway_transport_ack_messages\s*\(' -or
        $gatewayDispatcherText -match 'gateway_transport_request\s*\(\s*"POST"\s*,\s*"/api/im-gateway/v1/ack"') {
        $violations += 'Gateway Dispatcher: ACK POST must use the dedicated Gateway Transport ACK lane'
    }
    foreach ($meetingTransportContract in @(
            'gateway_transport_create_meeting\s*\(',
            'gateway_transport_get_meeting_status\s*\(',
            'gateway_transport_post_meeting_action\s*\(')) {
        if ($gatewayTransportHeaderText -notmatch $meetingTransportContract -or
            $gatewayTransportText -notmatch $meetingTransportContract) {
            $violations += "Gateway Transport: meeting endpoint contract is missing ($meetingTransportContract)"
        }
    }
    if ($mainCompositionText -match 'static\s+esp_err_t\s+(?:create_meeting_recording|get_meeting_status|post_meeting_action)\s*\(') {
        $violations += 'main/main.c: meeting endpoint HTTP/JSON implementation must remain in Gateway Transport'
    }
    if ($gatewayTransportHeaderText -notmatch 'gateway_transport_download_frame\s*\(' -or
        $gatewayTransportText -notmatch 'int32_t\s+gateway_transport_download_frame\s*\(' -or
        $mainCompositionText -match 'request_with_capacity\s*\(') {
        $violations += 'Gateway Transport: bounded frame download must own response sizing and HTTP request'
    }
    # Runtime and startup asset traversals share one transport lane, but their
    # cancellation identity must also be single-owner.  Require the public
    # frame bridge to serialize registration and to preserve the epoch decision
    # across both Wi-Fi and cellular completion races.
    foreach ($assetCancellationRequirement in @(
            's_asset_download_guard\s*=\s*xSemaphoreCreateMutex',
            'xSemaphoreTake\(s_asset_download_guard,\s*pdMS_TO_TICKS\(100\)\)',
            'const\s+uint32_t\s+requested_epoch\s*=\s*asset_cancel_epoch_snapshot',
            'asset_cancel_epoch_current\(asset_epoch\)',
            'cellular_err\s*=\s*ESP_ERR_INVALID_STATE')) {
        if ($gatewayTransportText -notmatch $assetCancellationRequirement) {
            $violations += "Gateway Transport: asset cancellation single-owner/epoch fence is incomplete (${assetCancellationRequirement})"
        }
    }
    if ($mainCompositionText -match 'static\s+esp_err_t\s+send_text_event\s*\(' -or
        $mainCompositionText -notmatch 'gateway_transport_send_text_event\s*\([\s\S]{0,120}?"/cancel"') {
        $violations += 'main/main.c: command cancellation must delegate text-event transport ownership'
    }
}

# Audio Service owns the transaction-scoped Wake Word admission marker.  The
# Platform Audio acknowledgement stays below the family adapter seam, so Power
# Service cannot learn a codec/I2S/task primitive simply to wait for an Audio
# safe point.  Reject a future implementation that starts real MCU sleep from
# either side of this normalized participant contract.
$audioServiceSource = Join-Path $projectRoot 'main/audio_service.c'
$platformAudioHeader = Join-Path $projectRoot 'main/platform_audio.h'
if (-not (Test-Path -LiteralPath $audioServiceSource) -or
    -not (Test-Path -LiteralPath $audioServiceHeader) -or
    -not (Test-Path -LiteralPath $platformAudioHeader)) {
    $violations += 'main/audio_service.[ch] / main/platform_audio.h: System Sleep Audio participant contract is missing'
} else {
    $audioServiceText = Get-Content -LiteralPath $audioServiceSource -Raw
    $audioHeaderText = Get-Content -LiteralPath $audioServiceHeader -Raw
    $platformAudioHeaderText = Get-Content -LiteralPath $platformAudioHeader -Raw
    foreach ($audioParticipantRequirement in @(
        'device_status_t\s+audio_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
        'void\s+audio_service_abort_system_sleep_prepare\s*\(\s*void\s*\)'
    )) {
        if ($audioHeaderText -notmatch $audioParticipantRequirement -or
            $audioServiceText -notmatch $audioParticipantRequirement) {
            $violations += "main/audio_service.[ch]: System Sleep Audio participant is incomplete (${audioParticipantRequirement})"
        }
    }
    if ($audioServiceText -notmatch 's_system_sleep_wake_pause_requested' -or
        $audioServiceText -notmatch 'platform_audio_wake_word_pause_with_ack') {
        $violations += 'main/audio_service.c: System Sleep Audio prepare must hold admission and wait for the profile-owned pause acknowledgement'
    }
    if ($audioServiceText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += 'main/audio_service.c: System Sleep Audio participant must not select board electrical wiring or enter MCU sleep'
    }
    if ($platformAudioHeaderText -notmatch 'device_status_t\s+platform_audio_wake_word_pause_with_ack\s*\(\s*bool\s+paused\s*,\s*uint32_t\s+timeout_ms\s*\)') {
        $violations += 'main/platform_audio.h: bounded profile Wake Word pause acknowledgement contract is missing'
    }
}
foreach ($platformAudioProfileSource in $platformAudioProfileSources) {
    $platformAudioProfilePath = Join-Path $projectRoot $platformAudioProfileSource
    if (-not (Test-Path -LiteralPath $platformAudioProfilePath)) {
        $violations += "${platformAudioProfileSource}: System Sleep Audio profile is missing"
        continue
    }
    $platformAudioProfileText = Get-Content -LiteralPath $platformAudioProfilePath -Raw
    if ($platformAudioProfileText -notmatch 'platform_audio_wake_word_pause_with_ack' -or
        $platformAudioProfileText -notmatch 'DEVICE_STATUS_TIMEOUT') {
        $violations += "${platformAudioProfileSource}: must provide bounded Wake Word pause acknowledgement for System Sleep prepare"
    }
}

# Persistence is the next common participant: it can prove that no accepted
# NVS transaction remains, but it must not leak a handle, cache-disable API or
# worker object into the System Sleep transaction owner.  PREPARE only closes
# request admission; abort must reopen the same worker generation.
$persistenceServiceHeader = Join-Path $projectRoot 'main/persistence_service.h'
$persistenceServiceSource = Join-Path $projectRoot 'main/persistence_service.c'
if (-not (Test-Path -LiteralPath $persistenceServiceHeader) -or
    -not (Test-Path -LiteralPath $persistenceServiceSource)) {
    $violations += 'main/persistence_service.[ch]: System Sleep Persistence participant contract is missing'
} else {
    $persistenceHeaderText = Get-Content -LiteralPath $persistenceServiceHeader -Raw
    $persistenceSourceText = Get-Content -LiteralPath $persistenceServiceSource -Raw
    foreach ($persistenceParticipantRequirement in @(
        'device_status_t\s+persistence_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
        'void\s+persistence_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
        's_system_sleep_preparing',
        '__atomic_store_n\s*\(\s*&s_accepting\s*,\s*false',
        '__atomic_store_n\s*\(\s*&s_accepting\s*,\s*true',
        's_worker_start_gate',
        's_worker_retiring',
        's_worker_exit_status',
        's_worker_registry_retirement_failed',
        'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
        's_worker_exit_status\s*=\s*registry_err',
        '!s_worker_registry_retirement_failed'
    )) {
        if ($persistenceHeaderText -notmatch $persistenceParticipantRequirement -and
            $persistenceSourceText -notmatch $persistenceParticipantRequirement) {
            $violations += "main/persistence_service.[ch]: System Sleep Persistence participant is incomplete (${persistenceParticipantRequirement})"
        }
    }
    if ($persistenceHeaderText -match '\b(?:nvs_handle_t|esp_err_t|SemaphoreHandle_t|TaskHandle_t)\b') {
        $violations += 'main/persistence_service.h: System Sleep Persistence contract must remain value-only'
    }
    if ($persistenceSourceText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += 'main/persistence_service.c: System Sleep Persistence participant must not select board electrical wiring or enter MCU sleep'
    }
}

# Display Service is a semantic transaction participant only. It may close
# Display Task submission and drain accepted requests, but must neither stop
# its boot-lifetime worker nor learn panel/DMA/ESP-sleep implementation facts.
$displayServiceHeader = Join-Path $projectRoot 'main/display_service.h'
$displayServiceSource = Join-Path $projectRoot 'main/display_service.c'
if (-not (Test-Path -LiteralPath $displayServiceHeader) -or
    -not (Test-Path -LiteralPath $displayServiceSource)) {
    $violations += 'main/display_service.[ch]: System Sleep Display participant contract is missing'
} else {
    $displayHeaderText = Get-Content -LiteralPath $displayServiceHeader -Raw
    $displaySourceText = Get-Content -LiteralPath $displayServiceSource -Raw
    foreach ($displayParticipantRequirement in @(
        'device_status_t\s+display_service_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
        'void\s+display_service_abort_system_sleep_prepare\s*\(\s*void\s*\)',
        's_system_sleep_preparing',
        's_system_sleep_admitted_requests',
        'display_service_system_sleep_request_complete',
        's_display_service_start_gate',
        's_display_service_retiring',
        's_display_service_exit_status',
        's_display_service_registry_retirement_failed',
        'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
        's_display_service_exit_status\s*=\s*registry_err',
        's_display_service_registry_retirement_failed\s*\|\|'
    )) {
        if ($displayHeaderText -notmatch $displayParticipantRequirement -and
            $displaySourceText -notmatch $displayParticipantRequirement) {
            $violations += "main/display_service.[ch]: System Sleep Display participant is incomplete (${displayParticipantRequirement})"
        }
    }
    if ($displayHeaderText -match '\b(?:SemaphoreHandle_t|TaskHandle_t|esp_err_t|esp_lcd|i2c_)\b') {
        $violations += 'main/display_service.h: System Sleep Display contract must remain value-only'
    }
    if ($displaySourceText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += 'main/display_service.c: System Sleep Display participant must not select board electrical wiring or enter MCU sleep'
    }
    if ($displaySourceText -notmatch 'platform_display_prepare_system_sleep') {
        $violations += 'main/display_service.c: System Sleep Display prepare must request the profile-private scan-out/DMA idle fence after semantic drain'
    }
    $platformDisplayHeader = Join-Path $projectRoot 'main/platform_display.h'
    $platformDisplaySource = Join-Path $projectRoot 'main/platform_display.c'
    $platformDisplayProfileHeader = Join-Path $projectRoot 'main/platform_display_profile.h'
    if (-not (Test-Path -LiteralPath $platformDisplayHeader) -or
        -not (Test-Path -LiteralPath $platformDisplaySource) -or
        -not (Test-Path -LiteralPath $platformDisplayProfileHeader)) {
        $violations += 'main/platform_display.[ch] / main/platform_display_profile.h: System Sleep scan-out fence contract is missing'
    } else {
        $platformDisplayHeaderText = Get-Content -LiteralPath $platformDisplayHeader -Raw
        $platformDisplaySourceText = Get-Content -LiteralPath $platformDisplaySource -Raw
        $platformDisplayProfileHeaderText = Get-Content -LiteralPath $platformDisplayProfileHeader -Raw
        foreach ($displayFenceRequirement in @(
            'device_status_t\s+platform_display_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
            'device_status_t\s+platform_display_profile_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)'
        )) {
            if ($platformDisplayHeaderText -notmatch $displayFenceRequirement -and
                $platformDisplayProfileHeaderText -notmatch $displayFenceRequirement -and
                $platformDisplaySourceText -notmatch $displayFenceRequirement) {
                $violations += "main/platform_display.[ch]: System Sleep scan-out fence contract is incomplete (${displayFenceRequirement})"
            }
        }
        if ($platformDisplayHeaderText -match '\b(?:esp_err_t|SemaphoreHandle_t|TaskHandle_t|esp_lcd|gpio_)\b') {
            $violations += 'main/platform_display.h: System Sleep scan-out fence contract must remain value-only'
        }
    }
    foreach ($displayProfileSource in @('main/platform_display_compact.c', 'main/platform_display_round.c')) {
        $displayProfilePath = Join-Path $projectRoot $displayProfileSource
        if (-not (Test-Path -LiteralPath $displayProfilePath)) {
            $violations += "${displayProfileSource}: System Sleep Display profile bridge is missing"
            continue
        }
        $displayProfileText = Get-Content -LiteralPath $displayProfilePath -Raw
        if ($displayProfileText -notmatch 'platform_display_profile_prepare_system_sleep' -or
            $displayProfileText -notmatch '(?:compact|round)_display_service_wait_for_scanout_idle') {
            $violations += "${displayProfileSource}: must route System Sleep scan-out fence through the private Display service"
        }
        if ($displayProfileText -notmatch '(?:compact|round)_display_service_prepare_system_sleep' -or
            $displayProfileText -notmatch '(?:compact|round)_display_service_abort_system_sleep_prepare') {
            $violations += "${displayProfileSource}: must park/resume retained decorative animation through the private Display service"
        }
    }
    $compactDisplayServiceForSleep = Join-Path $projectRoot 'main/compact_display_service.c'
    $roundDisplayServiceForSleep = Join-Path $projectRoot 'main/round_display_service.c'
    if (-not (Test-Path -LiteralPath $compactDisplayServiceForSleep) -or
        -not (Test-Path -LiteralPath $roundDisplayServiceForSleep)) {
        $violations += 'compact/round Display private services are missing for System Sleep animation audit'
    } else {
        $compactDisplaySleepText = Get-Content -LiteralPath $compactDisplayServiceForSleep -Raw
        $roundDisplaySleepText = Get-Content -LiteralPath $roundDisplayServiceForSleep -Raw
        foreach ($compactAnimationRequirement in @(
                'compact_display_service_prepare_system_sleep\s*\(',
                'compact_display_service_abort_system_sleep_prepare\s*\(',
                's_animation_system_sleep_preparing',
                's_animation_system_sleep_quiesced',
                'compact_display_service_park_for_system_sleep')) {
            if ($compactDisplaySleepText -notmatch $compactAnimationRequirement) {
                $violations += "compact Display System Sleep animation fence is incomplete (${compactAnimationRequirement})"
            }
        }
        foreach ($roundAnimationRequirement in @(
                'round_display_service_prepare_system_sleep\s*\(',
                'round_display_service_abort_system_sleep_prepare\s*\(',
                's_round_display_animation_system_sleep_preparing',
                's_round_display_animation_system_sleep_quiesced',
                'round_display_service_park_for_system_sleep')) {
            if ($roundDisplaySleepText -notmatch $roundAnimationRequirement) {
                $violations += "round Display System Sleep animation fence is incomplete (${roundAnimationRequirement})"
            }
        }
        foreach ($privateDisplay in @(
                @{ Text = $compactDisplaySleepText; Prepare = 'compact_display_service_prepare_system_sleep'; Abort = 'compact_display_service_abort_system_sleep_prepare'; Marker = 's_animation_system_sleep_preparing'; Name = 'compact' },
                @{ Text = $roundDisplaySleepText; Prepare = 'round_display_service_prepare_system_sleep'; Abort = 'round_display_service_abort_system_sleep_prepare'; Marker = 's_round_display_animation_system_sleep_preparing'; Name = 'round' })) {
            $displayPreparePattern = '(?:esp_err_t|device_status_t)\s+' + $privateDisplay.Prepare +
                                     '\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*void\s+' +
                                     $privateDisplay.Abort
            $displayPrepareMatch = [regex]::Match($privateDisplay.Text, $displayPreparePattern)
            if (-not $displayPrepareMatch.Success) {
                $violations += "$($privateDisplay.Name) Display System Sleep PREPARE/ABORT boundary cannot be inspected"
                continue
            }
            $displayPrepareBody = $displayPrepareMatch.Groups[1].Value
            if ($displayPrepareBody -match ('\b' + $privateDisplay.Abort + '\s*\(') -or
                $displayPrepareBody -match ($privateDisplay.Marker + '\s*=\s*false')) {
                $violations += "$($privateDisplay.Name) Display animation must remain parked after PREPARE failure until Power rollback"
            }
        }
    }
    if ($displaySourceText -notmatch 'platform_display_abort_system_sleep_prepare') {
        $violations += 'main/display_service.c: System Sleep Display rollback must release the retained-animation fence'
    }
}

# ESP-IDF default-loop Wi-Fi/IP callbacks are neither a normal Gateway request
# nor a registered worker. The composition root owns only registration of the
# physical event instances; Connectivity Service owns the independent
# admission/drain/ABORT fence so a queued callback cannot reconnect STA or
# start Gateway after the generic request drain returns.
$connectivitySourceForWifiCallbackSleep = Join-Path $projectRoot 'main/connectivity_service.c'
$compositionRootForWifiCallbackSleep = Join-Path $projectRoot 'main/main.c'
if (-not (Test-Path -LiteralPath $connectivitySourceForWifiCallbackSleep) -or
    -not (Test-Path -LiteralPath $compositionRootForWifiCallbackSleep)) {
    $violations += 'main/connectivity_service.c / main/main.c: Wi-Fi/IP callback System Sleep ownership is missing'
} else {
    $connectivityWifiCallbackSleepText =
        Get-Content -LiteralPath $connectivitySourceForWifiCallbackSleep -Raw
    $compositionRootWifiSleepText = Get-Content -LiteralPath $compositionRootForWifiCallbackSleep -Raw
    foreach ($wifiCallbackSleepRequirement in @(
            'connectivity_service_prepare_system_sleep\s*\(',
            'connectivity_service_abort_system_sleep_prepare\s*\(',
            's_wifi_event_system_sleep_preparing',
            's_wifi_event_system_sleep_was_admitted',
            's_wifi_event_callbacks_inflight',
            'connectivity_service_wifi_event_callback_enter\s*\(')) {
        if ($connectivityWifiCallbackSleepText -notmatch $wifiCallbackSleepRequirement) {
            $violations += "main/connectivity_service.c: Wi-Fi/IP callback System Sleep fence is incomplete (${wifiCallbackSleepRequirement})"
        }
    }
    if ($connectivityWifiCallbackSleepText -notmatch 'connectivity_service_prepare_system_sleep[\s\S]*?s_wifi_event_callback_admission_open\s*=\s*false' -or
        $connectivityWifiCallbackSleepText -notmatch 'connectivity_service_abort_system_sleep_prepare[\s\S]*?s_wifi_event_callback_admission_open\s*=\s*s_wifi_event_system_sleep_was_admitted' -or
        $compositionRootWifiSleepText -notmatch 'connectivity_service_wifi_event_callback_enter\s*\(' -or
        $compositionRootWifiSleepText -notmatch 'connectivity_service_wifi_event_callback_leave\s*\(') {
        $violations += 'Connectivity Service must own Wi-Fi/IP callback fence while main.c uses only value admission enter/leave'
    }
}

# Provisioning Service owns the portal's user-space lifecycle, while the
# physical composition root still owns Wi-Fi driver mode, STA/default-loop and
# event-instance registration.  The AP-side ESP-NETIF handle, DHCP/DNS
# advertisement and NAPT isolation form a smaller physical lifetime that is
# now deliberately private to one owner.  Keep the header out of Device API /
# Platform contracts and reject a future convenience regression that puts
# those ESP-NETIF calls or the old root globals back in main.c.
$provisioningNetworkOwnerHeader = Join-Path $projectRoot 'main/services/provisioning_network_owner.h'
$provisioningNetworkOwnerSource = Join-Path $projectRoot 'main/services/provisioning_network_owner.c'
if (-not (Test-Path -LiteralPath $provisioningNetworkOwnerHeader) -or
    -not (Test-Path -LiteralPath $provisioningNetworkOwnerSource)) {
    $violations += 'main/services/provisioning_network_owner.[ch]: private SoftAP ESP-NETIF owner is missing'
} else {
    $provisioningNetworkOwnerHeaderText = Get-Content -LiteralPath $provisioningNetworkOwnerHeader -Raw
    $provisioningNetworkOwnerText = Get-Content -LiteralPath $provisioningNetworkOwnerSource -Raw
    $mainProvisioningNetworkText = Get-Content -LiteralPath $compositionRootForWifiCallbackSleep -Raw
    foreach ($provisioningNetworkRequirement in @(
            'provisioning_network_owner_ensure_setup_ap\s*\(',
            'provisioning_network_owner_setup_ap_ready\s*\(',
            'provisioning_network_owner_configure_setup_ap_dhcp\s*\(',
            'provisioning_network_owner_verify_setup_ap_isolation\s*\(',
            'provisioning_network_owner_release_setup_ap\s*\(')) {
        if ($provisioningNetworkOwnerHeaderText -notmatch $provisioningNetworkRequirement) {
            $violations += "main/services/provisioning_network_owner.h: SoftAP owner contract is incomplete (${provisioningNetworkRequirement})"
        }
    }
    foreach ($stationNetifRequirement in @(
            'provisioning_network_owner_ensure_station\s*\(',
            'provisioning_network_owner_station_ready\s*\(',
            'provisioning_network_owner_release_station\s*\(')) {
        if ($provisioningNetworkOwnerHeaderText -notmatch $stationNetifRequirement) {
            $violations += "main/services/provisioning_network_owner.h: STA ESP-NETIF owner contract is incomplete (${stationNetifRequirement})"
        }
    }
    if ($provisioningNetworkOwnerHeaderText -match '\b(?:esp_netif_t|esp_err_t|esp_ip|lwip/|freertos/|TaskHandle_t|SemaphoreHandle_t)\b') {
        $violations += 'main/services/provisioning_network_owner.h: private SoftAP contract must not expose ESP-IDF, LWIP or RTOS handles'
    }
    foreach ($physicalSoftApApi in @(
            'esp_netif_create_default_wifi_ap\s*\(',
            'esp_netif_dhcps_(?:stop|start|option)\s*\(',
            'esp_netif_set_ip_info\s*\(',
            'esp_netif_set_dns_info\s*\(',
            'esp_netif_napt_disable\s*\(')) {
        if ($provisioningNetworkOwnerText -notmatch $physicalSoftApApi) {
            $violations += "main/services/provisioning_network_owner.c: SoftAP physical owner is incomplete (${physicalSoftApApi})"
        }
        if ($mainProvisioningNetworkText -match $physicalSoftApApi) {
            $violations += "main/main.c: SoftAP ESP-NETIF/DHCP/NAPT ownership must remain in provisioning_network_owner.c (${physicalSoftApApi})"
        }
    }
    if ($provisioningNetworkOwnerText -notmatch
        'esp_netif_destroy_default_wifi\s*\(\s*s_setup_ap_netif\s*\)') {
        $violations += 'main/services/provisioning_network_owner.c: SoftAP owner must destroy its own AP ESP-NETIF handle'
    }
    foreach ($stationNetifApi in @(
            'esp_netif_create_default_wifi_sta\s*\(',
            'esp_netif_destroy_default_wifi\s*\(\s*s_station_netif\s*\)')) {
        if ($provisioningNetworkOwnerText -notmatch $stationNetifApi) {
            $violations += "main/services/provisioning_network_owner.c: STA ESP-NETIF owner is incomplete (${stationNetifApi})"
        }
    }
    if ($mainProvisioningNetworkText -match 'esp_netif_create_default_wifi_sta\s*\(') {
        $violations += 'main/main.c: STA ESP-NETIF creation must remain in provisioning_network_owner.c'
    }
    foreach ($retiredRootSoftApSymbol in @(
            '\bs_setup_ap_netif\b',
            '\bs_ap_netif_created\b',
            '\bensure_setup_ap_netif\s*\(',
            '\bconfigure_setup_ap_ip\s*\(',
            '\bs_sta_netif\b',
            '\bs_sta_netif_created\b',
            '\bensure_station_netif\s*\(')) {
        if ($mainProvisioningNetworkText -match $retiredRootSoftApSymbol) {
            $violations += "main/main.c: retired SoftAP root owner leaked back (${retiredRootSoftApSymbol})"
        }
    }
    $physicalSoftApOwners = @($allCFiles | Where-Object {
        $candidateText = Get-Content -LiteralPath $_.FullName -Raw
        $candidateText -match 'esp_netif_create_default_wifi_ap\s*\(' -or
        $candidateText -match 'esp_netif_create_default_wifi_sta\s*\(' -or
        $candidateText -match 'esp_netif_dhcps_(?:stop|start|option)\s*\(' -or
        $candidateText -match 'esp_netif_napt_disable\s*\('
    } | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    } | Sort-Object -Unique)
    if (($physicalSoftApOwners -join '|') -ne 'main/services/provisioning_network_owner.c') {
        $violations += "SoftAP ESP-NETIF/DHCP/NAPT physical calls must have one owner; found: $($physicalSoftApOwners -join ', ')"
    }
}

# ESP-NETIF global initialization and the default ESP event loop are another
# physical singleton pair.  They are intentionally not promoted to a Device
# or Platform API: Wi-Fi driver/radio policy and event registrations still
# compose them in main.c.  The private owner preserves the partial-generation
# fail-closed state so a later start cannot create a colliding singleton.
$networkCoreOwnerHeader = Join-Path $projectRoot 'main/services/connectivity_network_core_owner.h'
$networkCoreOwnerSource = Join-Path $projectRoot 'main/services/connectivity_network_core_owner.c'
if (-not (Test-Path -LiteralPath $networkCoreOwnerHeader) -or
    -not (Test-Path -LiteralPath $networkCoreOwnerSource)) {
    $violations += 'main/services/connectivity_network_core_owner.[ch]: private ESP-NETIF/default-loop owner is missing'
} else {
    $networkCoreOwnerHeaderText = Get-Content -LiteralPath $networkCoreOwnerHeader -Raw
    $networkCoreOwnerText = Get-Content -LiteralPath $networkCoreOwnerSource -Raw
    $mainNetworkCoreText = Get-Content -LiteralPath $compositionRootForWifiCallbackSleep -Raw
    foreach ($networkCoreRequirement in @(
            'device_status_t\s+connectivity_network_core_owner_ensure\s*\(',
            'bool\s+connectivity_network_core_owner_ready\s*\(',
            'bool\s+connectivity_network_core_owner_has_resources\s*\(',
            'device_status_t\s+connectivity_network_core_owner_release\s*\(')) {
        if ($networkCoreOwnerHeaderText -notmatch $networkCoreRequirement) {
            $violations += "main/services/connectivity_network_core_owner.h: singleton owner contract is incomplete (${networkCoreRequirement})"
        }
    }
    if ($networkCoreOwnerHeaderText -match '\b(?:esp_err_t|esp_event|esp_netif|freertos/|TaskHandle_t|SemaphoreHandle_t)\b') {
        $violations += 'main/services/connectivity_network_core_owner.h: private singleton contract must not expose ESP-IDF or RTOS handles'
    }
    foreach ($networkCoreApi in @(
            'esp_netif_init\s*\(',
            'esp_event_loop_create_default\s*\(',
            'esp_event_loop_delete_default\s*\(',
            'esp_netif_deinit\s*\(')) {
        if ($networkCoreOwnerText -notmatch $networkCoreApi) {
            $violations += "main/services/connectivity_network_core_owner.c: singleton owner is incomplete (${networkCoreApi})"
        }
        if ($mainNetworkCoreText -match $networkCoreApi) {
            $violations += "main/main.c: ESP-NETIF/default-loop singleton ownership must remain in connectivity_network_core_owner.c (${networkCoreApi})"
        }
    }
    foreach ($retiredNetworkCoreSymbol in @(
            '\bs_netif_initialized\b',
            '\bs_default_event_loop_created\b',
            '\bs_network_initialized\b')) {
        if ($mainNetworkCoreText -match $retiredNetworkCoreSymbol) {
            $violations += "main/main.c: retired ESP-NETIF/default-loop root state leaked back (${retiredNetworkCoreSymbol})"
        }
    }
    if ($networkCoreOwnerText -notmatch
        'if\s*\(\s*s_netif_initialized\s*\|\|\s*s_default_event_loop_created\s*\)' -or
        $networkCoreOwnerText -notmatch
        'if\s*\(\s*s_default_event_loop_created\s*\)' -or
        $networkCoreOwnerText -notmatch
        'if\s*\(\s*s_netif_initialized\s*\)') {
        $violations += 'main/services/connectivity_network_core_owner.c: singleton partial-generation/release fence is incomplete'
    }
    $networkCoreOwners = @($allCFiles | Where-Object {
        $candidateText = Get-Content -LiteralPath $_.FullName -Raw
        $candidateText -match 'esp_netif_init\s*\(' -or
        $candidateText -match 'esp_event_loop_create_default\s*\(' -or
        $candidateText -match 'esp_event_loop_delete_default\s*\(' -or
        $candidateText -match 'esp_netif_deinit\s*\('
    } | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    } | Sort-Object -Unique)
    if (($networkCoreOwners -join '|') -ne 'main/services/connectivity_network_core_owner.c') {
        $violations += "ESP-NETIF/default-loop singleton APIs must have one owner; found: $($networkCoreOwners -join ', ')"
    }
}

# Wi-Fi driver initialization/deinitialization and the concrete application
# event-instance handles form a third ESP-IDF physical lifetime.  They stay
# below Connectivity Service: the root still decides radio mode, credentials,
# EAP and stop ordering, but must not retain driver/event handle ownership.
$wifiDriverOwnerHeader = Join-Path $projectRoot 'main/services/connectivity_wifi_driver_owner.h'
$wifiDriverOwnerSource = Join-Path $projectRoot 'main/services/connectivity_wifi_driver_owner.c'
if (-not (Test-Path -LiteralPath $wifiDriverOwnerHeader) -or
    -not (Test-Path -LiteralPath $wifiDriverOwnerSource)) {
    $violations += 'main/services/connectivity_wifi_driver_owner.[ch]: private Wi-Fi driver/event owner is missing'
} else {
    $wifiDriverOwnerHeaderText = Get-Content -LiteralPath $wifiDriverOwnerHeader -Raw
    $wifiDriverOwnerText = Get-Content -LiteralPath $wifiDriverOwnerSource -Raw
    $mainWifiDriverText = Get-Content -LiteralPath $compositionRootForWifiCallbackSleep -Raw
    # The root may explain ESP-IDF events in architecture comments, but it
    # must not compile against their SDK payloads.  Strip comments once so
    # the following structural checks inspect implementation text only.
    $mainWifiDriverLiveText = [regex]::Replace(
        $mainWifiDriverText, '(?s)/\*.*?\*/|//[^\r\n]*', '')
    foreach ($wifiDriverRequirement in @(
            'device_status_t\s+connectivity_wifi_driver_owner_initialize\s*\(',
            'bool\s+connectivity_wifi_driver_owner_initialized\s*\(',
            'bool\s+connectivity_wifi_driver_owner_ready\s*\(',
            'bool\s+connectivity_wifi_driver_owner_enterprise_enabled\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_configure_enterprise\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_disable_enterprise\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_configure_station\s*\(',
            'bool\s+connectivity_wifi_driver_owner_started\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_start\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_stop\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_connect\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_disconnect\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_disable_power_save\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_configure_protected_ap\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_capture_portal_radio\s*\(',
            'void\s+connectivity_wifi_driver_owner_note_portal_radio_changed\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_restore_portal_radio\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_scan_visible\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_current_station_ssid\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_read_station_mac\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_read_softap_mac\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_unregister_application_handlers\s*\(',
            'device_status_t\s+connectivity_wifi_driver_owner_deinitialize\s*\(')) {
        if ($wifiDriverOwnerHeaderText -notmatch $wifiDriverRequirement) {
            $violations += "main/services/connectivity_wifi_driver_owner.h: driver owner contract is incomplete (${wifiDriverRequirement})"
        }
    }
    if ($wifiDriverOwnerHeaderText -match '\b(?:esp_|esp_event_handler_instance_t|TaskHandle_t|SemaphoreHandle_t|freertos/)\b') {
        $violations += 'main/services/connectivity_wifi_driver_owner.h: Wi-Fi driver contract must remain ESP-IDF/RTOS-handle neutral'
    }
    if ($wifiDriverOwnerHeaderText -match 'radio_snapshot_t|wifi_mode_t|words\s*\[|\bvalid\s*;') {
        $violations += 'main/services/connectivity_wifi_driver_owner.h: portal rollback state must remain private; expose only an opaque numeric token'
    }
    foreach ($wifiDriverApi in @(
            'WIFI_INIT_CONFIG_DEFAULT\s*\(',
            'init\.ampdu_rx_enable\s*=\s*0',
            'init\.ampdu_tx_enable\s*=\s*0',
            'esp_wifi_init\s*\(',
            'esp_wifi_deinit\s*\(',
            'esp_wifi_start\s*\(',
            'esp_wifi_stop\s*\(',
            'esp_wifi_connect\s*\(',
            'esp_wifi_disconnect\s*\(',
            'esp_wifi_set_mode\s*\(',
            'esp_wifi_set_config\s*\(',
            'esp_wifi_set_ps\s*\(',
            'esp_wifi_set_protocol\s*\(',
            'esp_wifi_scan_start\s*\(',
            'esp_wifi_scan_get_ap_records\s*\(',
            'esp_wifi_sta_get_ap_info\s*\(',
            'esp_read_mac\s*\(',
            'esp_eap_client_set_identity\s*\(',
            'esp_eap_client_set_eap_methods\s*\(',
            'esp_wifi_sta_enterprise_enable\s*\(',
            'esp_wifi_sta_enterprise_disable\s*\(')) {
        if ($wifiDriverOwnerText -notmatch $wifiDriverApi) {
            $violations += "main/services/connectivity_wifi_driver_owner.c: Wi-Fi driver physical owner is incomplete (${wifiDriverApi})"
        }
    }
    foreach ($wifiEventRegistration in @(
            'WIFI_EVENT\s*,\s*ESP_EVENT_ANY_ID',
            'IP_EVENT\s*,\s*IP_EVENT_STA_GOT_IP',
            'IP_EVENT\s*,\s*IP_EVENT_ASSIGNED_IP_TO_CLIENT')) {
        if ($wifiDriverOwnerText -notmatch ('esp_event_handler_instance_register\s*\([\s\S]*?' + $wifiEventRegistration)) {
            $violations += "main/services/connectivity_wifi_driver_owner.c: application event registration is incomplete (${wifiEventRegistration})"
        }
        if ($wifiDriverOwnerText -notmatch ('esp_event_handler_instance_unregister\s*\([\s\S]*?' + $wifiEventRegistration)) {
            $violations += "main/services/connectivity_wifi_driver_owner.c: application event teardown is incomplete (${wifiEventRegistration})"
        }
    }
    if ($wifiDriverOwnerText -notmatch 'static\s+void\s+wifi_event_adapter\s*\(' -or
        $wifiDriverOwnerText -notmatch 'application_handlers_registered\s*\(' -or
        $wifiDriverOwnerText -notmatch 'Wi-Fi driver deinit rejected while event handlers remain registered') {
        $violations += 'main/services/connectivity_wifi_driver_owner.c: callback adaptation or retained-partial-generation fence is incomplete'
    }
    if ($wifiDriverOwnerHeaderText -notmatch 'connectivity_wifi_driver_event_kind_t' -or
        $wifiDriverOwnerHeaderText -notmatch 'connectivity_wifi_driver_event_t' -or
        $wifiDriverOwnerHeaderText -match 'esp_event_base_t|wifi_event_[A-Za-z0-9_]+_t|ip_event_[A-Za-z0-9_]+_t' -or
        $wifiDriverOwnerText -notmatch 'CONNECTIVITY_WIFI_DRIVER_EVENT_STATION_DISCONNECTED' -or
        $wifiDriverOwnerText -notmatch 'CONNECTIVITY_WIFI_DRIVER_EVENT_STATION_GOT_IP') {
        $violations += 'main/services/connectivity_wifi_driver_owner.[ch]: ESP Wi-Fi/IP callbacks must be translated to normalized event values'
    }
    if ($mainWifiDriverText -notmatch 'connectivity_network_root_owner_wifi_has_resources\s*\(\)[\s\S]*?connectivity_network_root_owner_wifi_ready\s*\(\)') {
        $violations += 'main/main.c: retained partial Wi-Fi driver generation must not be treated as ready'
    }
    foreach ($retiredWifiRootSymbol in @(
            '\bs_wifi_driver_initialized\b',
            '\bs_wifi_event_instance\b',
            '\bs_wifi_got_ip_event_instance\b',
            '\bs_wifi_assigned_ip_event_instance\b',
            '\bs_provisioning_radio_snapshot\b')) {
        if ($mainWifiDriverText -match $retiredWifiRootSymbol) {
            $violations += "main/main.c: retired Wi-Fi driver/event-instance owner leaked back (${retiredWifiRootSymbol})"
        }
    }
    foreach ($retiredWifiRootType in @(
            '\besp_event_base_t\b',
            '\bwifi_event_[A-Za-z0-9_]+_t\b',
            '\bip_event_[A-Za-z0-9_]+_t\b',
            '\bWIFI_EVENT\b',
            '\bIP_EVENT\b',
            '\bMACSTR\b',
            '\bMAC2STR\b')) {
        # Architecture comments can name ESP-IDF event macros.  Reject only a
        # live source line: SDK callback types and payload macros must not
        # leak back into the composition-root implementation.
        if ([regex]::IsMatch($mainWifiDriverLiveText, $retiredWifiRootType)) {
            $violations += "main/main.c: Wi-Fi/IP event SDK type or macro must remain in connectivity_wifi_driver_owner.c (${retiredWifiRootType})"
        }
    }
    if ($wifiDriverOwnerText -notmatch 's_portal_radio_snapshot' -or
        $wifiDriverOwnerText -notmatch 's_portal_radio_generation' -or
        $wifiDriverOwnerText -notmatch 'token\s*!=\s*s_portal_radio_generation') {
        $violations += 'main/services/connectivity_wifi_driver_owner.c: portal rollback must retain a private generation-fenced snapshot'
    }
    $provisioningHeader = Join-Path $projectRoot 'main/services/provisioning_service.h'
    $provisioningSource = Join-Path $projectRoot 'main/services/provisioning_service.c'
    if (-not (Test-Path -LiteralPath $provisioningHeader) -or
        -not (Test-Path -LiteralPath $provisioningSource)) {
        $violations += 'main/services/provisioning_service.[ch]: Provisioning portal token contract is missing'
    } else {
        $provisioningHeaderText = Get-Content -LiteralPath $provisioningHeader -Raw
        $provisioningSourceText = Get-Content -LiteralPath $provisioningSource -Raw
        if ($provisioningHeaderText -notmatch 'typedef\s+uint32_t\s+provisioning_radio_token_t\s*;' -or
            $provisioningHeaderText -match 'provisioning_radio_token_t\s*\{' -or
            $provisioningHeaderText -match 'bytes\s*\[' -or
            $provisioningHeaderText -notmatch 'note_radio_changed\)\(provisioning_radio_token_t\s+token\)' -or
            $provisioningHeaderText -notmatch 'restore_radio\)\(provisioning_radio_token_t\s+token\)') {
            $violations += 'main/services/provisioning_service.h: portal radio token must remain an opaque value passed by value'
        }
        if ($provisioningSourceText -match 'recover_after_setup_portal_start_failure\s*\([^;]*&radio_token' -or
            $provisioningSourceText -match 'restore_radio\s*\(\s*&') {
            $violations += 'main/services/provisioning_service.c: Provisioning must not expose or retain portal radio snapshot storage'
        }
        if ($provisioningHeaderText -match '\b(?:wifi_auth_mode_t|wifi_ap_record_t|esp_wifi|esp_err_t|SemaphoreHandle_t|TaskHandle_t)\b' -or
            $provisioningSourceText -match '\b(?:wifi_auth_mode_t|wifi_ap_record_t|wifi_scan_config_t|esp_wifi_scan_|esp_mac)\b') {
            $violations += 'main/services/provisioning_service.[ch]: portal scan must consume normalized values, never Wi-Fi SDK records or auth enums'
        }
        if ($provisioningHeaderText -notmatch 'provisioning_scan_observer_t' -or
            $provisioningHeaderText -notmatch 'scan_visible_wifi' -or
            $provisioningSourceText -notmatch 's_host\.scan_visible_wifi\s*\(') {
            $violations += 'main/services/provisioning_service.[ch]: portal scan must route through the normalized host scan observer'
        }
    }
    foreach ($retiredWifiRootApi in @(
            'esp_wifi_init\s*\(',
            'esp_wifi_deinit\s*\(',
            'esp_wifi_start\s*\(',
            'esp_wifi_stop\s*\(',
            'esp_wifi_connect\s*\(',
            'esp_wifi_disconnect\s*\(',
            'esp_wifi_set_mode\s*\(',
            'esp_wifi_set_config\s*\(',
            'esp_wifi_set_ps\s*\(',
            'esp_wifi_set_protocol\s*\(',
            'esp_wifi_scan_start\s*\(',
            'esp_wifi_scan_get_ap_records\s*\(',
            'esp_wifi_sta_get_ap_info\s*\(',
            'esp_read_mac\s*\(',
            'esp_eap_client_[A-Za-z0-9_]+\s*\(',
            'esp_wifi_sta_enterprise_(?:enable|disable)\s*\(',
            'esp_event_handler_instance_register\s*\(',
            'esp_event_handler_instance_unregister\s*\(')) {
        # main.c may document ESP-IDF API names in lifecycle comments; reject
        # only a non-comment source line that invokes one of these owners.
        if ($mainWifiDriverText -match ('(?m)^(?!\s*//)\s*.*' + $retiredWifiRootApi)) {
            $violations += "main/main.c: Wi-Fi driver/event-instance physical API must remain in connectivity_wifi_driver_owner.c (${retiredWifiRootApi})"
        }
    }
    $wifiDriverApiOwners = @($allCFiles | Where-Object {
        $candidateText = Get-Content -LiteralPath $_.FullName -Raw
        $candidateText -match 'esp_wifi_init\s*\(' -or
        $candidateText -match 'esp_wifi_deinit\s*\('
    } | ForEach-Object {
        $_.FullName.Substring($projectRoot.Length + 1).Replace('\','/')
    } | Sort-Object -Unique)
    if (($wifiDriverApiOwners -join '|') -ne 'main/services/connectivity_wifi_driver_owner.c') {
        $violations += "Wi-Fi driver init/deinit APIs must have one owner; found: $($wifiDriverApiOwners -join ', ')"
    }
}

# B3 physical root: the dependent ESP-IDF lifetimes may be represented by
# individual private owners, but their cross-owner stop order must not remain
# duplicated in main.c.  The physical transaction is private and value-only at
# its composition boundary; the root retains business policy and contributes
# only callback-admission/SNTP stop bridges.
$networkRootOwnerHeader = Join-Path $projectRoot 'main/services/connectivity_network_root_owner.h'
$networkRootOwnerSource = Join-Path $projectRoot 'main/services/connectivity_network_root_owner.c'
$networkLifecycleHeader = Join-Path $projectRoot 'main/services/connectivity_network_lifecycle_service.h'
$networkLifecycleSource = Join-Path $projectRoot 'main/services/connectivity_network_lifecycle_service.c'
$connectivityRestartCoordinatorHeader = Join-Path $projectRoot 'main/services/connectivity_restart_coordinator.h'
$connectivityRestartCoordinatorSource = Join-Path $projectRoot 'main/services/connectivity_restart_coordinator.c'
if (-not (Test-Path -LiteralPath $networkRootOwnerHeader) -or
    -not (Test-Path -LiteralPath $networkRootOwnerSource)) {
    $violations += 'main/services/connectivity_network_root_owner.[ch]: private physical network-root transaction owner is missing'
} else {
    $networkRootOwnerHeaderText = Get-Content -LiteralPath $networkRootOwnerHeader -Raw
    $networkRootOwnerText = Get-Content -LiteralPath $networkRootOwnerSource -Raw
    $mainNetworkRootText = Get-Content -LiteralPath $compositionRootForWifiCallbackSleep -Raw
    foreach ($networkRootRequirement in @(
            'connectivity_network_root_owner_configure_lifecycle_host\s*\(',
            'connectivity_network_root_owner_ensure_core\s*\(',
            'connectivity_network_root_owner_core_ready\s*\(',
            'connectivity_network_root_owner_has_resources\s*\(',
            'connectivity_network_root_owner_initialize_wifi\s*\(',
            'connectivity_network_root_owner_wifi_ready\s*\(',
            'connectivity_network_root_owner_wifi_has_resources\s*\(',
            'connectivity_network_root_owner_stop_provisioning\s*\(',
            'connectivity_network_root_owner_stop\s*\(')) {
        if ($networkRootOwnerHeaderText -notmatch $networkRootRequirement) {
            $violations += "main/services/connectivity_network_root_owner.h: physical-root transaction contract is incomplete (${networkRootRequirement})"
        }
    }
    if ($networkRootOwnerHeaderText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|esp_netif_t|esp_err_t)\b') {
        $violations += 'main/services/connectivity_network_root_owner.h: physical-root contract must remain SDK/RTOS handle neutral'
    }
    foreach ($networkRootOrder in @(
            'stop_callback_admission[\s\S]*?stop_clock_sync[\s\S]*?connectivity_wifi_driver_owner_stop[\s\S]*?connectivity_wifi_driver_owner_unregister_application_handlers[\s\S]*?provisioning_network_owner_release_setup_ap[\s\S]*?provisioning_network_owner_release_station[\s\S]*?connectivity_wifi_driver_owner_deinitialize[\s\S]*?connectivity_network_core_owner_release',
            'stop_provisioning_under_deadline[\s\S]*?stop_provisioning[\s\S]*?status\s*!=\s*DEVICE_STATUS_OK[\s\S]*?provisioning_has_live_resources[\s\S]*?DEVICE_STATUS_BUSY',
            'stop_provisioning_under_deadline[\s\S]*?status\s*!=\s*DEVICE_STATUS_OK[\s\S]*?remaining_timeout_ms\s*\(\s*deadline_us\s*\)\s*==\s*0[\s\S]*?provisioning_has_live_resources',
            'stop_callback_admission[\s\S]*?provisioning_has_live_resources[\s\S]*?stop_clock_sync[\s\S]*?provisioning_has_live_resources',
            'provisioning_has_live_resources[\s\S]*?return\s+DEVICE_STATUS_BUSY',
            'connectivity_network_root_owner_ensure_core\s*\([^)]*\)\s*\{[\s\S]*?!s_lifecycle_host_configured[\s\S]*?DEVICE_STATUS_UNAVAILABLE',
            'connectivity_network_root_owner_initialize_wifi\s*\([^)]*\)\s*\{[\s\S]*?!s_lifecycle_host_configured[\s\S]*?DEVICE_STATUS_UNAVAILABLE',
            'lifecycle_host_matches[\s\S]*?connectivity_network_core_owner_has_resources[\s\S]*?return\s+DEVICE_STATUS_BUSY',
            'if\s*\(status\s*!=\s*DEVICE_STATUS_OK\)\s*return\s+status',
            'remaining_timeout_ms\s*\(',
            'connectivity_wifi_driver_owner_stop\s*\([\s\S]*?connectivity_wifi_driver_owner_started\s*\(\)',
            'provisioning_network_owner_release_setup_ap\s*\([\s\S]*?provisioning_network_owner_setup_ap_ready\s*\(\)',
            'provisioning_network_owner_release_station\s*\([\s\S]*?provisioning_network_owner_has_resources\s*\(\)',
            'connectivity_wifi_driver_owner_deinitialize\s*\([\s\S]*?connectivity_network_core_owner_release\s*\(\)')) {
        if ($networkRootOwnerText -notmatch $networkRootOrder) {
            $violations += "main/services/connectivity_network_root_owner.c: physical-root stop ordering/fail-closed fence is incomplete (${networkRootOrder})"
        }
    }
    if ($mainNetworkRootText -notmatch 'connectivity_network_root_owner_configure_lifecycle_host\s*\(' -or
        $mainNetworkRootText -notmatch 'connectivity_network_root_owner_stop_provisioning\s*\(' -or
        $mainNetworkRootText -notmatch 'connectivity_network_root_owner_stop\s*\(' -or
        $mainNetworkRootText -notmatch 'network_root_provisioning_has_live_resources\s*\(' -or
        $mainNetworkRootText -notmatch '\.provisioning_has_live_resources\s*=' -or
        $mainNetworkRootText -match 'static\s+esp_err_t\s+stop_network_core_transaction[\s\S]*?connectivity_wifi_driver_owner_unregister_application_handlers\s*\(') {
        $violations += 'main/main.c: physical Wi-Fi stop transaction must delegate to connectivity_network_root_owner'
    }
}

# B3/C7 runtime restart is deliberately prepared as a private, value-only
# transaction.  It has no production trigger yet: APSTA candidate confirmation
# and full root-level Gateway rearm remain separate work.  A failed transaction
# must stay terminal; System Sleep ABORT cannot revive work against a stopped
# physical network generation.
if (-not (Test-Path -LiteralPath $connectivityRestartCoordinatorHeader) -or
    -not (Test-Path -LiteralPath $connectivityRestartCoordinatorSource)) {
    $violations += 'main/services/connectivity_restart_coordinator.[ch]: B3/C7 runtime restart policy is missing'
} else {
    $restartHeaderText = Get-Content -LiteralPath $connectivityRestartCoordinatorHeader -Raw
    $restartSourceText = Get-Content -LiteralPath $connectivityRestartCoordinatorSource -Raw
    foreach ($restartRequirement in @(
            'CONNECTIVITY_RESTART_STAGE_QUIESCE_NETWORK_DEPENDENTS',
            'CONNECTIVITY_RESTART_STAGE_STOP_PROVISIONING',
            'CONNECTIVITY_RESTART_STAGE_STOP_PHYSICAL_ROOT',
            'CONNECTIVITY_RESTART_STAGE_INITIALIZE_LOGICAL_CONNECTIVITY',
            'CONNECTIVITY_RESTART_STAGE_INITIALIZE_PHYSICAL_ROOT',
            'CONNECTIVITY_RESTART_STAGE_START_SELECTED_UPLINK',
            'CONNECTIVITY_RESTART_STAGE_START_CLOCK_SYNC',
            'CONNECTIVITY_RESTART_STAGE_REARM_GATEWAY',
            'connectivity_restart_coordinator_restart\s*\(',
            'CONNECTIVITY_RESTART_STAGE_FAILED',
            'physical_root_stop_committed')) {
        if ($restartHeaderText -notmatch $restartRequirement -and
            $restartSourceText -notmatch $restartRequirement) {
            $violations += "main/services/connectivity_restart_coordinator.[ch]: runtime restart contract is incomplete (${restartRequirement})"
        }
    }
    if ($restartHeaderText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|esp_netif_t|esp_err_t|httpd_|esp_http_client)\b') {
        $violations += 'main/services/connectivity_restart_coordinator.h: runtime restart contract must remain value-only and SDK/RTOS/HTTP neutral'
    }
    if ($restartSourceText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|httpd_|esp_http_client|gateway_lifecycle_service_abort_system_sleep_prepare)\b') {
        $violations += 'main/services/connectivity_restart_coordinator.c: runtime restart policy must not own SDK/RTOS/HTTP objects or System Sleep ABORT'
    }
    if ($restartSourceText -notmatch 'quiesce_network_dependents[\s\S]*?stop_provisioning[\s\S]*?call_physical_root_stop[\s\S]*?initialize_logical_connectivity[\s\S]*?initialize_physical_root[\s\S]*?start_selected_uplink[\s\S]*?start_clock_sync[\s\S]*?rearm_gateway' -or
        $restartSourceText -notmatch 'CONNECTIVITY_RESTART_STAGE_FAILED[\s\S]*?return\s+status') {
        $violations += 'main/services/connectivity_restart_coordinator.c: runtime restart must preserve ordering and terminal fail-closed failure'
    }
    if ($restartSourceText -notmatch '(?s)call_physical_root_stop\s*\([^)]*\)\s*\{.*?if\s*\(timeout_ms\s*==\s*0\)\s*return\s+DEVICE_STATUS_TIMEOUT;.*?physical_root_stop_committed\s*=\s*true;.*?stop_physical_root') {
        $violations += 'main/services/connectivity_restart_coordinator.c: physical-root committed fact must mean the stop bridge was actually entered'
    }
}

# A12/B3 root cleanup: physical and logical Connectivity lifecycle ordering
# must be centralized in a value-only service. The composition root may bind
# concrete bridges, but must not regain direct init/rollback orchestration.
if (-not (Test-Path -LiteralPath $networkLifecycleHeader) -or
    -not (Test-Path -LiteralPath $networkLifecycleSource)) {
    $violations += 'main/services/connectivity_network_lifecycle_service.[ch]: connectivity lifecycle coordinator is missing'
} else {
    $networkLifecycleHeaderText = Get-Content -LiteralPath $networkLifecycleHeader -Raw
    $networkLifecycleSourceText = Get-Content -LiteralPath $networkLifecycleSource -Raw
    foreach ($lifecycleRequirement in @(
            'connectivity_network_lifecycle_service_init\s*\(',
            'connectivity_network_lifecycle_service_ensure_core\s*\(',
            'connectivity_network_lifecycle_service_ensure_wifi\s*\(',
            'connectivity_network_lifecycle_service_stop\s*\(',
            'configure_physical_lifecycle', 'stop_physical',
            'deinitialize_logical')) {
        if ($networkLifecycleHeaderText -notmatch $lifecycleRequirement) {
            $violations += "main/services/connectivity_network_lifecycle_service.h: lifecycle contract is incomplete (${lifecycleRequirement})"
        }
    }
    if ($networkLifecycleHeaderText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|esp_netif_t|esp_err_t|httpd_|esp_http_client)\b') {
        $violations += 'main/services/connectivity_network_lifecycle_service.h: lifecycle contract must remain SDK/RTOS/HTTP neutral'
    }
    if ($networkLifecycleSourceText -match '\b(?:esp_wifi|esp_netif|esp_http_client|TaskHandle_t|SemaphoreHandle_t|gateway_transport|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/connectivity_network_lifecycle_service.c: lifecycle policy must not absorb SDK/RTOS/HTTP/transport/board ownership'
    }
    if ($networkLifecycleSourceText -notmatch 'stop_physical[\s\S]*?deinitialize_logical' -or
        $networkLifecycleSourceText -notmatch 'rollback_failed_start' -or
        $networkLifecycleSourceText -notmatch 'physical_has_resources[\s\S]*?DEVICE_STATUS_BUSY') {
        $violations += 'main/services/connectivity_network_lifecycle_service.c: physical-before-logical stop and partial-root closure are incomplete'
    }
    if ($mainNetworkRootText -notmatch 'connectivity_network_lifecycle_service_ensure_core\s*\(' -or
        $mainNetworkRootText -notmatch 'connectivity_network_lifecycle_service_ensure_wifi\s*\(' -or
        $mainNetworkRootText -notmatch 'connectivity_network_lifecycle_service_stop\s*\(' -or
        $mainNetworkRootText -notmatch 'connectivity_network_lifecycle_service_init\s*\(') {
        $violations += 'main/main.c: Connectivity lifecycle composition must use the shared lifecycle coordinator'
    }
}

# A12 QR presentation is a narrow private SDK adapter.  The composition root
# publishes only semantic scene values; QR handles, encoder configuration and
# the short-lived module matrix must not drift back into main.c or its public
# contract.
$provisioningQrHeader = Join-Path $projectRoot 'main/services/provisioning_qr_service.h'
$provisioningQrSource = Join-Path $projectRoot 'main/services/provisioning_qr_service.c'
if (-not (Test-Path -LiteralPath $provisioningQrHeader) -or
    -not (Test-Path -LiteralPath $provisioningQrSource)) {
    $violations += 'main/services/provisioning_qr_service.[ch]: private QR presentation service is missing'
} else {
    $provisioningQrHeaderText = Get-Content -LiteralPath $provisioningQrHeader -Raw
    $provisioningQrSourceText = Get-Content -LiteralPath $provisioningQrSource -Raw
    foreach ($qrRequirement in @(
            'provisioning_qr_service_host_t',
            'publish_modules',
            'publish_fallback_message',
            'provisioning_qr_service_init\s*\(',
            'provisioning_qr_service_show\s*\(')) {
        if ($provisioningQrHeaderText -notmatch $qrRequirement) {
            $violations += "main/services/provisioning_qr_service.h: QR presentation contract is incomplete (${qrRequirement})"
        }
    }
    if ($provisioningQrHeaderText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|qrcode|malloc|free)\b') {
        $violations += 'main/services/provisioning_qr_service.h: QR presentation contract must remain SDK/RTOS/allocator neutral'
    }
    if ($provisioningQrSourceText -notmatch '#include\s*[<\"]qrcode\.h[>\"]' -or
        $provisioningQrSourceText -notmatch 'esp_qrcode_generate\s*\(' -or
        $provisioningQrSourceText -notmatch 'free\s*\(\s*modules\s*\)' -or
        $provisioningQrSourceText -notmatch 'length\s*>=\s*\(int\)sizeof\(payload\)' -or
        $provisioningQrSourceText -notmatch 'esp_log_level_set\s*\(\s*"QRCODE"\s*,\s*ESP_LOG_NONE\s*\)') {
        $violations += 'main/services/provisioning_qr_service.c: QR SDK, bounded payload, temporary matrix release, and payload-log suppression must remain private'
    }
    if ($provisioningQrSourceText -match '\b(?:scene_presenter|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/provisioning_qr_service.c: QR adapter must not absorb presentation implementation or board policy'
    }
    if ($mainNetworkRootText -match '\b(?:esp_qrcode_|esp_qrcode_handle_t|ESP_QRCODE_|show_setup_qrcode)\b' -or
        $mainNetworkRootText -notmatch 'provisioning_qr_service_init\s*\(' -or
        $mainNetworkRootText -notmatch 'provisioning_qr_service_show\s*\(') {
        $violations += 'main/main.c: QR SDK transaction must delegate to provisioning_qr_service'
    }
}

# A12/A8 downlink audio has a small independent presentation boundary. MIME
# dispatch must not pull MP3/WAV renderer policy or ESP error values back into
# the Gateway dispatcher/composition root; the root may only inject physical
# renderer callbacks into this value-only service.
$serverAudioPresentationHeader = Join-Path $projectRoot 'main/services/server_audio_presentation_service.h'
$serverAudioPresentationSource = Join-Path $projectRoot 'main/services/server_audio_presentation_service.c'
if (-not (Test-Path -LiteralPath $serverAudioPresentationHeader) -or
    -not (Test-Path -LiteralPath $serverAudioPresentationSource)) {
    $violations += 'main/services/server_audio_presentation_service.[ch]: server-audio presentation service is missing'
} else {
    $serverAudioPresentationHeaderText = Get-Content -LiteralPath $serverAudioPresentationHeader -Raw
    $serverAudioPresentationSourceText = Get-Content -LiteralPath $serverAudioPresentationSource -Raw
    foreach ($audioPresentationRequirement in @(
            'server_audio_presentation_service_host_t',
            'play_mp3', 'play_wav',
            'server_audio_presentation_service_init\s*\(',
            'server_audio_presentation_service_mime_supported\s*\(',
            'server_audio_presentation_service_url_allowed\s*\(',
            'server_audio_presentation_service_play\s*\(',
            'server_audio_presentation_service_error_is_permanent\s*\(')) {
        if ($serverAudioPresentationHeaderText -notmatch $audioPresentationRequirement) {
            $violations += "main/services/server_audio_presentation_service.h: server-audio contract is incomplete (${audioPresentationRequirement})"
        }
    }
    if ($serverAudioPresentationHeaderText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|QueueHandle_t|cJSON|http|mp3_player|audio_arbitration|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/server_audio_presentation_service.h: server-audio public contract must remain SDK/RTOS/JSON/HTTP/renderer/board neutral'
    }
    if ($serverAudioPresentationSourceText -match '\b(?:esp_|freertos/|TaskHandle_t|SemaphoreHandle_t|cJSON|http|mp3_player|audio_arbitration|scene_presenter|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/server_audio_presentation_service.c: server-audio policy must not absorb SDK/RTOS/JSON/HTTP/renderer/board ownership'
    }
    if ($serverAudioPresentationSourceText -notmatch 'payload_is_mp3' -or
        $serverAudioPresentationSourceText -notmatch 'DEVICE_STATUS_BUSY' -or
        $serverAudioPresentationSourceText -notmatch 'DEVICE_STATUS_TIMEOUT') {
        $violations += 'main/services/server_audio_presentation_service.c: MIME dispatch and retryable status classification are incomplete'
    }
    if ($mainNetworkRootText -match '\b(?:audio_payload_is_mp3)\b' -or
        $mainNetworkRootText -notmatch 'server_audio_presentation_service_init\s*\(' -or
        $mainNetworkRootText -notmatch 'server_audio_presentation_service_play\s*\(') {
        $violations += 'main/main.c: server-audio format/presentation policy must delegate to server_audio_presentation_service'
    }
}

# C7 SAFE_MODE has a deliberately narrower, proven-minimum service boundary
# than normal startup rollback.  It must stay value-only and terminal: any
# attempt to restore ordinary workers through rollback/ABORT after a partial
# safe entry could cross an unproven fault-domain generation.
$safeModeCoordinatorHeader = Join-Path $projectRoot 'main/services/safe_mode_coordinator.h'
$safeModeCoordinatorSource = Join-Path $projectRoot 'main/services/safe_mode_coordinator.c'
$safeModeCoordinatorGate = Join-Path $projectRoot 'tools/check-safe-mode-coordinator.ps1'
if (-not (Test-Path -LiteralPath $safeModeCoordinatorHeader) -or
    -not (Test-Path -LiteralPath $safeModeCoordinatorSource) -or
    -not (Test-Path -LiteralPath $safeModeCoordinatorGate)) {
    $violations += 'main/services/safe_mode_coordinator.[ch]: C7 SAFE_MODE minimum-service contract or host regression is missing'
} else {
    $safeModeHeaderText = Get-Content -LiteralPath $safeModeCoordinatorHeader -Raw
    $safeModeSourceText = Get-Content -LiteralPath $safeModeCoordinatorSource -Raw
    foreach ($safeModeRequirement in @(
            'SAFE_MODE_COORDINATOR_ABI_VERSION',
            'SAFE_MODE_STAGE_QUIESCE_NONESSENTIAL',
            'SAFE_MODE_STAGE_INITIALIZE_CLOCK_FEEDBACK',
            'SAFE_MODE_STAGE_INITIALIZE_ALARM',
            'SAFE_MODE_STAGE_PUBLISH_DIAGNOSTIC_SURFACE',
            'SAFE_MODE_STAGE_FAILED',
            'safe_mode_coordinator_enter\s*\(')) {
        if ($safeModeHeaderText -notmatch $safeModeRequirement -and
            $safeModeSourceText -notmatch $safeModeRequirement) {
            $violations += "main/services/safe_mode_coordinator.[ch]: SAFE_MODE contract is incomplete (${safeModeRequirement})"
        }
    }
    if ($safeModeHeaderText -match '\b(?:esp_|freertos/|TaskHandle_t|QueueHandle_t|SemaphoreHandle_t|esp_netif_t|httpd_|esp_http_client|gpio_|i2c_)\b') {
        $violations += 'main/services/safe_mode_coordinator.h: SAFE_MODE contract must remain value-only and SDK/RTOS/HTTP/board neutral'
    }
    if ($safeModeSourceText -match '\b(?:esp_|freertos/|TaskHandle_t|QueueHandle_t|SemaphoreHandle_t|httpd_|esp_http_client|abort_system_sleep_prepare|rollback)\b') {
        $violations += 'main/services/safe_mode_coordinator.c: SAFE_MODE must not own SDK/RTOS/HTTP objects or normal-startup rollback/ABORT'
    }
    if ($safeModeSourceText -notmatch 'quiesce_nonessential[\s\S]*?initialize_clock_feedback[\s\S]*?initialize_alarm[\s\S]*?publish_diagnostic_surface' -or
        $safeModeSourceText -notmatch 'SAFE_MODE_STAGE_FAILED[\s\S]*?return\s+status') {
        $violations += 'main/services/safe_mode_coordinator.c: SAFE_MODE must preserve minimum-service ordering and terminal failure closure'
    }
    $mainSource = Join-Path $projectRoot 'main/main.c'
    $inputBindingSource = Join-Path $projectRoot 'main/presentation/input_binding.c'
    $inputBindingHeader = Join-Path $projectRoot 'main/presentation/input_binding.h'
    $failureInjectionHeader = Join-Path $projectRoot 'main/provisioning_failure_injection.h'
    $failureInjectionSource = Join-Path $projectRoot 'main/provisioning_failure_injection.c'
    if (-not (Test-Path -LiteralPath $mainSource) -or
        -not (Test-Path -LiteralPath $inputBindingSource) -or
        -not (Test-Path -LiteralPath $inputBindingHeader) -or
        -not (Test-Path -LiteralPath $failureInjectionHeader) -or
        -not (Test-Path -LiteralPath $failureInjectionSource)) {
        $violations += 'SAFE_MODE composition-root/input/failure-injection sources are missing'
    } else {
        $safeModeMainText = Get-Content -LiteralPath $mainSource -Raw
        $safeModeInputText = Get-Content -LiteralPath $inputBindingSource -Raw
        $safeModeInputHeaderText = Get-Content -LiteralPath $inputBindingHeader -Raw
        $safeModeInjectionHeaderText = Get-Content -LiteralPath $failureInjectionHeader -Raw
        $safeModeInjectionSourceText = Get-Content -LiteralPath $failureInjectionSource -Raw
        foreach ($safeModeBridgeRequirement in @(
                'startup_enter_safe_mode\s*\(',
                'safe_mode_quiesce_nonessential\s*\(',
                'safe_mode_initialize_clock_feedback\s*\(',
                'safe_mode_initialize_alarm\s*\(',
                'safe_mode_publish_diagnostic_surface\s*\(',
                'SAFE_MODE_ENTRY_TIMEOUT_MS',
                'provisioning_failure_injection_safe_mode_at_local_ready\s*\(',
                'gateway_lifecycle_service_prepare_network_restart\s*\(',
                'gateway_lifecycle_service_commit_prepared_network_restart\s*\(')) {
            if ($safeModeMainText -notmatch $safeModeBridgeRequirement) {
                $violations += "main/main.c: SAFE_MODE composition bridge is incomplete ($safeModeBridgeRequirement)"
            }
        }
        $safeModeQuiesceMatch = [regex]::Match(
            $safeModeMainText,
            '(?s)static\s+device_status_t\s+safe_mode_quiesce_nonessential\s*\([^)]*\)\s*\{(.*?)\n\}')
        if ($safeModeQuiesceMatch.Success -and
            $safeModeQuiesceMatch.Groups[1].Value -match 'startup_stop_local_workers\s*\(') {
            $violations += 'main/main.c: SAFE_MODE must not invoke normal startup rollback, which releases retained minimum services'
        }
        if ($safeModeMainText -notmatch '(?s)lifecycle_service_reach\s*\(\s*DEVICE_RUNTIME_PHASE_LOCAL_READY\s*\)\s*;[\s\S]*?provisioning_failure_injection_safe_mode_at_local_ready\s*\(') {
            $violations += 'main/main.c: SAFE_MODE test entry must be after the proven LOCAL_READY minimum-service boundary'
        }
        if ($safeModeInputHeaderText -notmatch 'safe_mode_active' -or
            $safeModeInputText -notmatch '(?s)safe_mode_active\s*&&\s*s_host\.safe_mode_active\s*\(\)[\s\S]*?input ignored while SAFE_MODE' -or
            $safeModeInputText -notmatch '(?s)alarm_manager_is_initialized\s*\(\).*?alarm_manager_is_ringing\s*\(\)[\s\S]*?safe_mode_active') {
            $violations += 'main/presentation/input_binding.[ch]: SAFE_MODE must reject ordinary input only after local alarm-dismiss admission'
        }
        if ($safeModeInjectionHeaderText -notmatch 'provisioning_failure_injection_safe_mode_at_local_ready\s*\(' -or
            $safeModeInjectionSourceText -notmatch 'CONFIG_MACLAW_SAFE_MODE_TEST_LOCAL_READY_FAILURE') {
            $violations += 'main/provisioning_failure_injection.[ch]: SAFE_MODE compile-time local-ready proof seam is incomplete'
        }
        if ($safeModeInjectionHeaderText -notmatch 'provisioning_failure_injection_safe_mode_force_setup_take_fails\s*\(' -or
            $safeModeInjectionSourceText -notmatch 'CONFIG_MACLAW_SAFE_MODE_TEST_FORCE_SETUP_TAKE_FAILURE' -or
            $safeModeMainText -notmatch 'configuration_service_take_force_setup\s*\(') {
            $violations += 'C7 SAFE_MODE force-setup transaction-failure proof seam is incomplete'
        }
    }
}

# The Gateway lifecycle's existing PREPARE/ABORT pair is reversible System
# Sleep machinery. Runtime network restart may reuse its bounded quiesce but
# must commit the prepared generation without invoking ABORT, otherwise an old
# poll/startup/meeting worker could resume against the retired physical root.
$gatewayLifecycleHeader = Join-Path $projectRoot 'main/services/gateway_lifecycle_service.h'
$gatewayLifecycleSource = Join-Path $projectRoot 'main/services/gateway_lifecycle_service.c'
$gatewayRestartCommitGate = Join-Path $projectRoot 'tools/check-gateway-lifecycle-restart-commit.ps1'
$gatewayAssetCancellationGate = Join-Path $projectRoot 'tools/check-gateway-transport-asset-cancellation.ps1'
if (-not (Test-Path -LiteralPath $gatewayLifecycleHeader) -or
    -not (Test-Path -LiteralPath $gatewayLifecycleSource) -or
    -not (Test-Path -LiteralPath $gatewayRestartCommitGate) -or
    -not (Test-Path -LiteralPath $gatewayAssetCancellationGate)) {
    $violations += 'gateway lifecycle runtime-restart commit contract or host regression is missing'
} else {
    $gatewayLifecycleHeaderText = Get-Content -LiteralPath $gatewayLifecycleHeader -Raw
    $gatewayLifecycleSourceText = Get-Content -LiteralPath $gatewayLifecycleSource -Raw
    if ($gatewayLifecycleHeaderText -notmatch 'gateway_lifecycle_service_commit_prepared_network_restart\s*\(' -or
        $gatewayLifecycleSourceText -notmatch 'gateway_dispatcher_commit_prepared_network_restart\s*\([\s\S]*?gateway_transport_commit_prepared_network_restart\s*\([\s\S]*?meeting_service_commit_capability_refresh_network_restart\s*\([\s\S]*?meeting_service_commit_resumed_worker_network_restart\s*\([\s\S]*?meeting_service_commit_resume_supervisor_network_restart\s*\(') {
        $violations += 'main/services/gateway_lifecycle_service.[ch]: runtime restart must terminally commit every prepared Gateway/Meeting generation'
    }
    $gatewayCommitMatch = [regex]::Match(
        $gatewayLifecycleSourceText,
        '(?s)device_status_t\s+gateway_lifecycle_service_commit_prepared_network_restart\s*\([^)]*\)\s*\{.*?\n\}')
    if (-not $gatewayCommitMatch.Success -or
        $gatewayCommitMatch.Value -match 'gateway_lifecycle_service_abort_system_sleep_prepare\s*\(' -or
        $gatewayCommitMatch.Value -match 'restore_prepared_workers\s*\(') {
        $violations += 'main/services/gateway_lifecycle_service.c: runtime restart commit must not use System Sleep rollback'
    }
    if ($gatewayLifecycleSourceText -notmatch 'PREPARE_KIND_SYSTEM_SLEEP' -or
        $gatewayLifecycleSourceText -notmatch 'PREPARE_KIND_NETWORK_RESTART' -or
        $gatewayLifecycleSourceText -notmatch 's_prepare_kind\s*!=\s*PREPARE_KIND_SYSTEM_SLEEP' -or
        $gatewayLifecycleSourceText -notmatch 's_prepare_kind\s*!=\s*PREPARE_KIND_NETWORK_RESTART') {
        $violations += 'main/services/gateway_lifecycle_service.c: System Sleep ABORT must not reopen a terminal network-restart fence'
    }
}

# Root-owned Wake/portal/cellular participants now share the terminal restart
# domain with Gateway. Keep their contracts value-only and require the root
# bridge to prepare every old generation before committing it without an ABORT.
$restartParticipantFiles = @(
    @{ Name = 'wake restart'; Header = 'main/services/wake_restart_worker_service.h'; Source = 'main/services/wake_restart_worker_service.c'; Prepare = 'wake_restart_worker_service_prepare_network_restart'; Commit = 'wake_restart_worker_service_commit_prepared_network_restart' },
    @{ Name = 'deferred setup'; Header = 'main/services/deferred_setup_worker_service.h'; Source = 'main/services/deferred_setup_worker_service.c'; Prepare = 'deferred_setup_worker_service_prepare_network_restart'; Commit = 'deferred_setup_worker_service_commit_prepared_network_restart' },
    @{ Name = 'cellular recovery'; Header = 'main/services/cellular_recovery_service.h'; Source = 'main/services/cellular_recovery_service.c'; Prepare = 'cellular_recovery_service_prepare_network_restart'; Commit = 'cellular_recovery_service_commit_prepared_network_restart' }
)
foreach ($participant in $restartParticipantFiles) {
    $headerPath = Join-Path $projectRoot $participant.Header
    $sourcePath = Join-Path $projectRoot $participant.Source
    if (-not (Test-Path -LiteralPath $headerPath) -or -not (Test-Path -LiteralPath $sourcePath)) {
        $violations += "$($participant.Name) terminal network-restart participant source/header is missing"
        continue
    }
    $headerText = Get-Content -LiteralPath $headerPath -Raw
    $sourceText = Get-Content -LiteralPath $sourcePath -Raw
    if ($headerText -notmatch ($participant.Prepare + '\s*\(') -or
        $headerText -notmatch ($participant.Commit + '\s*\(') -or
        $sourceText -notmatch ($participant.Prepare + '\s*\(') -or
        $sourceText -notmatch ($participant.Commit + '\s*\(') -or
        $sourceText -notmatch 's_network_restart_preparing') {
        $violations += "$($participant.Source): terminal network-restart prepare/commit fence is incomplete"
    }
    $commitMatch = [regex]::Match(
        $sourceText, '(?s)device_status_t\s+' + $participant.Commit + '\s*\([^)]*\)\s*\{.*?\n\}')
    if (-not $commitMatch.Success -or
        $commitMatch.Value -match 'abort_system_sleep_prepare\s*\(' -or
        $commitMatch.Value -match 's_admission_open\s*=\s*true') {
        $violations += "$($participant.Source): terminal restart commit must not reopen System Sleep admission"
    }
}
if ($mainCompositionText -notmatch '(?s)static\s+device_status_t\s+quiesce_network_dependents_for_restart\s*\([^)]*\)\s*\{[\s\S]*?stop_startup_pet_asset_for_network_restart\s*\([\s\S]*?prepare_wake_restart_network_restart\s*\([\s\S]*?prepare_deferred_setup_network_restart\s*\([\s\S]*?cellular_recovery_service_prepare_network_restart\s*\([\s\S]*?gateway_lifecycle_service_prepare_network_restart\s*\([\s\S]*?gateway_lifecycle_service_commit_prepared_network_restart\s*\([\s\S]*?cellular_recovery_service_commit_prepared_network_restart\s*\([\s\S]*?commit_deferred_setup_network_restart\s*\([\s\S]*?commit_wake_restart_network_restart\s*\(') {
    $violations += 'main/main.c: terminal network-dependent restart bridge must prepare and commit every root-owned participant in dependency order'
}

# Alarm Manager is the common durable schedule participant. It may freeze
# new tool mutations and reject an already-active alarm, but must not expose
# RTC/ESP-sleep/GPIO details or stop the scheduler merely to prepare a
# reversible transaction.
$alarmManagerHeader = Join-Path $projectRoot 'main/alarm_manager.h'
$alarmManagerSource = Join-Path $projectRoot 'main/alarm_manager.c'
if (-not (Test-Path -LiteralPath $alarmManagerHeader) -or
    -not (Test-Path -LiteralPath $alarmManagerSource)) {
    $violations += 'main/alarm_manager.[ch]: System Sleep Alarm participant contract is missing'
} else {
    $alarmHeaderText = Get-Content -LiteralPath $alarmManagerHeader -Raw
    $alarmSourceText = Get-Content -LiteralPath $alarmManagerSource -Raw
    foreach ($alarmParticipantRequirement in @(
        'device_status_t\s+alarm_manager_prepare_system_sleep\s*\(\s*uint32_t\s+timeout_ms\s*\)',
        'void\s+alarm_manager_abort_system_sleep_prepare\s*\(\s*void\s*\)',
        's_system_sleep_preparing',
        's_tool_admissions',
        'xTaskNotifyGive'
    )) {
        if ($alarmHeaderText -notmatch $alarmParticipantRequirement -and
            $alarmSourceText -notmatch $alarmParticipantRequirement) {
            $violations += "main/alarm_manager.[ch]: System Sleep Alarm participant is incomplete (${alarmParticipantRequirement})"
        }
    }
    if ($alarmHeaderText -match '\b(?:rtc_|esp_sleep|gpio_|SemaphoreHandle_t|TaskHandle_t)\b') {
        $violations += 'main/alarm_manager.h: System Sleep Alarm contract must remain value-only and not expose electrical scheduling'
    }
    if ($alarmSourceText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += 'main/alarm_manager.c: System Sleep Alarm participant must not select board electrical wiring or enter MCU sleep'
    }
}
$powerProfileSources = @('main/platform_power_compact.c', 'main/platform_power_round.c')
foreach ($powerProfileSource in $powerProfileSources) {
    $powerProfilePath = Join-Path $projectRoot $powerProfileSource
    if (-not (Test-Path -LiteralPath $powerProfilePath)) {
        $violations += "${powerProfileSource}: selected System Sleep Power profile is missing"
        continue
    }
    $powerProfileText = Get-Content -LiteralPath $powerProfilePath -Raw
    foreach ($powerProfileRequirement in @(
        'platform_power_profile_prepare_verified_sleep',
        'platform_power_profile_abort_verified_sleep',
        'platform_power_profile_commit_verified_sleep',
        'platform_power_profile_resume_verified_sleep',
        'DEVICE_STATUS_UNAVAILABLE'
    )) {
        if ($powerProfileText -notmatch $powerProfileRequirement) {
            $violations += "${powerProfileSource}: must retain the fail-closed system sleep preparation contract (${powerProfileRequirement})"
        }
    }
    if ($powerProfileText -match '\besp_sleep|CONFIG_MACLAW_BOARD_|\bgpio_') {
        $violations += "${powerProfileSource}: family Power bridge must not select board wiring or enter MCU sleep"
    }
    if ($powerProfileText -notmatch 'entry_timeout_ms\s*==\s*0' -or
        $powerProfileText -notmatch 'return\s+DEVICE_STATUS_UNAVAILABLE') {
        $violations += "${powerProfileSource}: COMMIT must validate input then stay fail-closed until profile electrical HIL"
    }
    if ($powerProfileText -notmatch 'prepare_verified_sleep[\s\S]*?abort_system_sleep_prepare[\s\S]*?return\s+DEVICE_STATUS_UNAVAILABLE') {
        $violations += "${powerProfileSource}: unavailable electrical preflight must synchronously undo family worker parking"
    }
}

# Physical input scanners retain board-owned GPIO/touch/I2C sampling loops.
# App Intent drains their published business events, but cannot prove that a
# scanner will not issue one more electrical read while a future Power profile
# prepares rails or clocks. Keep their reversible park/ACK fence entirely
# below the Input/Power profile seams and reject a regression that makes Power
# learn the controller or task mechanics.
$compactInputServiceForSleep = Join-Path $projectRoot 'main/compact_input_service.c'
$roundInputServiceForSleep = Join-Path $projectRoot 'main/round_input_service.c'
$compactPowerProfileForInput = Join-Path $projectRoot 'main/platform_power_compact.c'
$roundPowerProfileForInput = Join-Path $projectRoot 'main/platform_power_round.c'
if (-not (Test-Path -LiteralPath $compactInputServiceForSleep) -or
    -not (Test-Path -LiteralPath $roundInputServiceForSleep) -or
    -not (Test-Path -LiteralPath $compactPowerProfileForInput) -or
    -not (Test-Path -LiteralPath $roundPowerProfileForInput)) {
    $violations += 'compact/round Input and Power profile sources are missing for System Sleep scanner audit'
} else {
    $compactInputSleepText = Get-Content -LiteralPath $compactInputServiceForSleep -Raw
    $roundInputSleepText = Get-Content -LiteralPath $roundInputServiceForSleep -Raw
    $compactPowerInputText = Get-Content -LiteralPath $compactPowerProfileForInput -Raw
    $roundPowerInputText = Get-Content -LiteralPath $roundPowerProfileForInput -Raw
    foreach ($compactScannerRequirement in @(
            'compact_input_service_prepare_system_sleep\s*\(',
            'compact_input_service_abort_system_sleep_prepare\s*\(',
            's_scanner_system_sleep_preparing',
            's_scanner_system_sleep_quiesced')) {
        if ($compactInputSleepText -notmatch $compactScannerRequirement -and
            $compactPowerInputText -notmatch $compactScannerRequirement) {
            $violations += "compact Input System Sleep scanner fence is incomplete (${compactScannerRequirement})"
        }
    }
    foreach ($roundScannerRequirement in @(
            'round_input_service_prepare_system_sleep\s*\(',
            'round_input_service_abort_system_sleep_prepare\s*\(',
            's_button_task_system_sleep_preparing',
            's_button_task_system_sleep_quiesced')) {
        if ($roundInputSleepText -notmatch $roundScannerRequirement -and
            $roundPowerInputText -notmatch $roundScannerRequirement) {
            $violations += "round Input System Sleep scanner fence is incomplete (${roundScannerRequirement})"
        }
    }
    foreach ($privateScanner in @(
            @{ Text = $compactInputSleepText; Prepare = 'compact_input_service_prepare_system_sleep'; Abort = 'compact_input_service_abort_system_sleep_prepare'; Marker = 's_scanner_system_sleep_preparing'; Name = 'compact' },
            @{ Text = $roundInputSleepText; Prepare = 'round_input_service_prepare_system_sleep'; Abort = 'round_input_service_abort_system_sleep_prepare'; Marker = 's_button_task_system_sleep_preparing'; Name = 'round' })) {
        $scannerPreparePattern = '(?:esp_err_t|device_status_t)\s+' + $privateScanner.Prepare +
                                 '\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*void\s+' +
                                 $privateScanner.Abort
        $scannerPrepareMatch = [regex]::Match($privateScanner.Text, $scannerPreparePattern)
        if (-not $scannerPrepareMatch.Success) {
            $violations += "$($privateScanner.Name) Input System Sleep scanner PREPARE/ABORT boundary cannot be inspected"
            continue
        }
        $scannerPrepareBody = $scannerPrepareMatch.Groups[1].Value
        if ($scannerPrepareBody -match ('\b' + $privateScanner.Abort + '\s*\(') -or
            $scannerPrepareBody -match ($privateScanner.Marker + '\s*=\s*false')) {
            $violations += "$($privateScanner.Name) Input System Sleep scanner must remain parked after PREPARE failure until Power rollback"
        }
    }
    if ($compactPowerInputText -match '\b(?:gpio_|i2c_|xTask|SemaphoreHandle_t|TaskHandle_t|esp_sleep)\b') {
        $violations += 'main/platform_power_compact.c: compact Power profile must not absorb input GPIO/I2C/RTOS or MCU sleep detail'
    }
    if ($roundPowerInputText -match '\b(?:gpio_|i2c_|xTask|SemaphoreHandle_t|TaskHandle_t|esp_sleep)\b') {
        $violations += 'main/platform_power_round.c: round Power profile must not absorb input GPIO/I2C/RTOS or MCU sleep detail'
    }
}

# Fangtang owns a retained ADC/charge monitor below the compact family.  A
# future verified sleep cannot silently park rails/clocks while that task is
# sampling.  The public Platform Power contract stays value-only; only the
# selected private peripheral adapter knows GPIO/ADC/task mechanics.
$compactPowerProfileForPeripheral = Join-Path $projectRoot 'main/platform_power_compact.c'
$compactPeripheralServiceForSleep = Join-Path $projectRoot 'main/compact_peripheral_service.c'
$fangtangPeripheralForSleep = Join-Path $projectRoot 'main/boards/fangtang_4g/fangtang_peripheral_adapter.c'
if (-not (Test-Path -LiteralPath $compactPowerProfileForPeripheral) -or
    -not (Test-Path -LiteralPath $compactPeripheralServiceForSleep) -or
    -not (Test-Path -LiteralPath $fangtangPeripheralForSleep)) {
    $violations += 'compact Power/Fangtang peripheral sources are missing for System Sleep monitor audit'
} else {
    $compactPowerProfileText = Get-Content -LiteralPath $compactPowerProfileForPeripheral -Raw
    $compactPeripheralServiceText = Get-Content -LiteralPath $compactPeripheralServiceForSleep -Raw
    $fangtangPeripheralText = Get-Content -LiteralPath $fangtangPeripheralForSleep -Raw
    foreach ($peripheralSleepRequirement in @(
            'compact_peripheral_service_prepare_system_sleep\s*\(',
            'compact_peripheral_service_abort_system_sleep_prepare\s*\(',
            'compact_peripheral_adapter_prepare_system_sleep\s*\(',
            'compact_peripheral_adapter_abort_system_sleep_prepare\s*\(',
            's_fangtang_power_task_system_sleep_preparing',
            's_fangtang_power_task_system_sleep_quiesced',
            's_fangtang_power_task_samples_inflight')) {
        if ($compactPowerProfileText -notmatch $peripheralSleepRequirement -and
            $compactPeripheralServiceText -notmatch $peripheralSleepRequirement -and
            $fangtangPeripheralText -notmatch $peripheralSleepRequirement) {
            $violations += "Fangtang peripheral System Sleep monitor fence is incomplete (${peripheralSleepRequirement})"
        }
    }
    $peripheralPreparePattern = '(?:esp_err_t|device_status_t)\s+compact_peripheral_adapter_prepare_system_sleep' +
                                '\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*void\s+' +
                                'compact_peripheral_adapter_abort_system_sleep_prepare'
    $peripheralPrepareMatch = [regex]::Match($fangtangPeripheralText, $peripheralPreparePattern)
    if (-not $peripheralPrepareMatch.Success) {
        $violations += 'Fangtang peripheral System Sleep PREPARE/ABORT boundary cannot be inspected'
    } else {
        $peripheralPrepareBody = $peripheralPrepareMatch.Groups[1].Value
        if ($peripheralPrepareBody -match '\bcompact_peripheral_adapter_abort_system_sleep_prepare\s*\(' -or
            $peripheralPrepareBody -match 's_fangtang_power_task_system_sleep_preparing\s*=\s*false') {
            $violations += 'Fangtang peripheral System Sleep monitor must remain parked after PREPARE failure until Power rollback'
        }
    }
    if ($compactPowerProfileText -match '\b(?:gpio_|adc_|xTask|SemaphoreHandle_t|TaskHandle_t|esp_sleep)\b') {
        $violations += 'main/platform_power_compact.c: compact Power profile must not absorb ADC/GPIO/RTOS or MCU sleep detail'
    }
}
$baselineBits = @(
    'DEVICE_CAPABILITY_DISPLAY',
    'DEVICE_CAPABILITY_PRIMARY_CONTROL',
    'DEVICE_CAPABILITY_OUTPUT_VOLUME',
    'DEVICE_CAPABILITY_AUDIO_CAPTURE',
    'DEVICE_CAPABILITY_AUDIO_PLAYBACK',
    'DEVICE_CAPABILITY_OFFLINE_WAKE_WORD',
    'DEVICE_CAPABILITY_PERSISTENT_STORAGE',
    'DEVICE_CAPABILITY_DISPLAY_OFF'
)
foreach ($profileSource in $productionProfiles) {
    $path = Join-Path $projectRoot $profileSource
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "${profileSource}: production board profile is missing"
        continue
    }
    $profileText = Get-Content -LiteralPath $path -Raw
    if ($profileText -notmatch '\.capabilities\s*=\s*DEVICE_CAPABILITY_REQUIRED_BASELINE') {
        $violations += "${profileSource}: capabilities must start from DEVICE_CAPABILITY_REQUIRED_BASELINE"
    }
    foreach ($bit in $baselineBits) {
        if ($profileText -match "\.capabilities[\s\S]*?${bit}") {
            $violations += "${profileSource}: must not hand-enumerate baseline bit ${bit}; use DEVICE_CAPABILITY_REQUIRED_BASELINE"
        }
    }
}

# Meter updates originate from audio capture and can be several times faster
# than a physical LCD/QSPI/SPI presentation.  Their only legal Display Service
# implementation is the owned latest-value handoff below; a future convenient
# `display_service_submit()` here would reintroduce panel back-pressure into
# every hardware profile's microphone path.
$displayServiceSource = Join-Path $projectRoot 'main/display_service.c'
if (-not (Test-Path -LiteralPath $displayServiceSource)) {
    $violations += 'main/display_service.c: shared Display Service is missing'
} else {
    $displayServiceText = Get-Content -LiteralPath $displayServiceSource -Raw
    foreach ($requiredMeterSeam in @(
        's_display_service_audio_level_request',
        's_display_service_audio_level_generation',
        'display_service_schedule_audio_level',
        'display_service_dispatch_audio_level_snapshot'
    )) {
        if ($displayServiceText -notmatch [regex]::Escape($requiredMeterSeam)) {
            $violations += "main/display_service.c: high-rate audio-level coalescing seam is missing ${requiredMeterSeam}"
        }
    }
    $audioLevelFunction = [regex]::Match(
        $displayServiceText,
        '(?s)void\s+display_service_set_audio_level\s*\([^)]*\)\s*\{.*?display_service_schedule_audio_level\s*\(\s*\)\s*;\s*\}')
    if (-not $audioLevelFunction.Success) {
        $violations += 'main/display_service.c: audio-level update must finish through the coalescing scheduler'
    } elseif ($audioLevelFunction.Value -match '\bdisplay_service_submit\s*\(' -or
              $audioLevelFunction.Value -match '\bxSemaphoreTake(?:Recursive)?\s*\(') {
        $violations += 'main/display_service.c: audio-level producer must not synchronously wait for Display Task/panel work'
    }
}

if (-not (Test-Path -LiteralPath $petAssetIntegrityHeader) -or
    -not (Test-Path -LiteralPath $petAssetIntegritySource)) {
    $violations += 'main/services/pet_asset_integrity_service.[ch]: digest transaction service is missing'
} else {
    $integrityHeaderText = Get-Content -LiteralPath $petAssetIntegrityHeader -Raw
    $integritySourceText = Get-Content -LiteralPath $petAssetIntegritySource -Raw
    foreach ($requiredIntegrity in @(
        'pet_asset_integrity_service_host_t',
        'pet_asset_integrity_service_verify_frame',
        'compute_sha256')) {
        if ($integrityHeaderText -notmatch [regex]::Escape($requiredIntegrity)) {
            $violations += "main/services/pet_asset_integrity_service.h: digest contract missing ${requiredIntegrity}"
        }
    }
    if ($integrityHeaderText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|psa_|gateway_|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $violations += 'main/services/pet_asset_integrity_service.h: public digest contract must remain value-only'
    }
    if ($integritySourceText -match '\b(?:psa_hash|esp_http_client|heap_caps|xTask|SemaphoreHandle_t|scene_presenter|gateway_transport)\b') {
        $violations += 'main/services/pet_asset_integrity_service.c: digest provider ownership must remain behind host callback'
    }
    if ($integritySourceText -notmatch 'pet_asset_service_sha256_matches_hex') {
        $violations += 'main/services/pet_asset_integrity_service.c: canonical digest comparison is missing'
    }
}

if ($violations.Count -ne 0) {
    Write-Error ("HAL boundary check failed:`n" + ($violations -join "`n"))
    exit 1
}

Write-Output 'HAL boundary check passed: shared Device/Platform headers expose no SDK, RTOS, JSON, or driver object types.'
