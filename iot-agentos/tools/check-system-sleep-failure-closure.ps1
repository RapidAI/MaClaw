[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$participants = @(
    @{ File = 'main\audio_service.c'; Prepare = 'audio_service_prepare_system_sleep'; Abort = 'audio_service_abort_system_sleep_prepare' },
    @{ File = 'main\services\command_service.c'; Prepare = 'command_service_prepare_system_sleep'; Abort = 'command_service_abort_system_sleep_prepare' },
    @{ File = 'main\app_intent_service.c'; Prepare = 'app_intent_service_prepare_system_sleep'; Abort = 'app_intent_service_abort_system_sleep_prepare' },
    @{ File = 'main\firmware_identity.c'; Prepare = 'firmware_identity_prepare_system_sleep'; Abort = 'firmware_identity_abort_system_sleep_prepare' },
    @{ File = 'main\update_service.c'; Prepare = 'update_service_prepare_system_sleep'; Abort = 'update_service_abort_system_sleep_prepare' },
    @{ File = 'main\fall_detection_service.c'; Prepare = 'fall_detection_service_prepare_system_sleep'; Abort = 'fall_detection_service_abort_system_sleep_prepare' },
    @{ File = 'main\services\provisioning_service.c'; Prepare = 'provisioning_service_prepare_system_sleep'; Abort = 'provisioning_service_abort_system_sleep_prepare' },
    @{ File = 'main\meeting_recovery_service.c'; Prepare = 'meeting_recovery_service_prepare_system_sleep'; Abort = 'meeting_recovery_service_abort_system_sleep_prepare' },
    @{ File = 'main\weather_cache_service.c'; Prepare = 'weather_cache_service_prepare_system_sleep'; Abort = 'weather_cache_service_abort_system_sleep_prepare' },
    @{ File = 'main\configuration_service.c'; Prepare = 'configuration_service_prepare_system_sleep'; Abort = 'configuration_service_abort_system_sleep_prepare' },
    @{ File = 'main\persistence_service.c'; Prepare = 'persistence_service_prepare_system_sleep'; Abort = 'persistence_service_abort_system_sleep_prepare' },
    @{ File = 'main\display_service.c'; Prepare = 'display_service_prepare_system_sleep'; Abort = 'display_service_abort_system_sleep_prepare' },
    @{ File = 'main\services\ambient_service.c'; Prepare = 'ambient_service_prepare_system_sleep'; Abort = 'ambient_service_abort_system_sleep_prepare' },
    @{ File = 'main\alarm_manager.c'; Prepare = 'alarm_manager_prepare_system_sleep'; Abort = 'alarm_manager_abort_system_sleep_prepare' },
    @{ File = 'main\sleep_schedule_service.c'; Prepare = 'sleep_schedule_service_prepare_system_sleep'; Abort = 'sleep_schedule_service_abort_system_sleep_prepare' },
    @{ File = 'main\wake_deadline_service.c'; Prepare = 'wake_deadline_service_prepare_system_sleep'; Abort = 'wake_deadline_service_abort_system_sleep_prepare' },
    @{ File = 'main\connectivity_service.c'; Prepare = 'connectivity_service_prepare_system_sleep'; Abort = 'connectivity_service_abort_system_sleep_prepare' },
    @{ File = 'main\battery_policy_service.c'; Prepare = 'battery_policy_service_prepare_system_sleep'; Abort = 'battery_policy_service_abort_system_sleep_prepare' }
)
# Firmware Identity's USB query task is retained across normal System Sleep
# ABORT, but a terminal stop must first retire its immutable Diagnostics entry.
# Keep a failed retirement closed so a later start cannot overlap an owner
# sweep for the predecessor generation.
$firmwareIdentityPath = Join-Path $projectRoot 'main\firmware_identity.c'
if (-not (Test-Path -LiteralPath $firmwareIdentityPath)) {
    $failures += "missing $firmwareIdentityPath"
} else {
    $firmwareIdentityText = Get-Content -LiteralPath $firmwareIdentityPath -Raw
    foreach ($identityFence in @(
            's_query_task_retiring',
            's_query_task_exit_status',
            's_query_task_registry_retirement_failed',
            'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
            's_query_task_exit_status\s*=\s*registry_err',
            's_query_task_registry_retirement_failed\s*,\s*true',
            '!s_query_task_registry_retirement_failed')) {
        if ($firmwareIdentityText -notmatch $identityFence) {
            $failures += "main/firmware_identity.c: diagnostic worker retirement fence is incomplete ($identityFence)"
        }
    }
}
# These hardware-worker fences are private to their hardware families, so they
# do not belong in Power Service's shared-participant list. They still close
# admission as part of the same parent transaction and must therefore remain
# closed after an ACK timeout until Platform Power's reverse rollback ABORT.
$privateHardwareParticipants = @(
    @{ File = 'main\compact_input_service.c'; Prepare = 'compact_input_service_prepare_system_sleep'; Abort = 'compact_input_service_abort_system_sleep_prepare'; Marker = 's_scanner_system_sleep_preparing' },
    @{ File = 'main\round_input_service.c'; Prepare = 'round_input_service_prepare_system_sleep'; Abort = 'round_input_service_abort_system_sleep_prepare'; Marker = 's_button_task_system_sleep_preparing' },
    @{ File = 'main\boards\fangtang_4g\fangtang_peripheral_adapter.c'; Prepare = 'compact_peripheral_adapter_prepare_system_sleep'; Abort = 'compact_peripheral_adapter_abort_system_sleep_prepare'; Marker = 's_fangtang_power_task_system_sleep_preparing' },
    @{ File = 'main\compact_display_service.c'; Prepare = 'compact_display_service_prepare_system_sleep'; Abort = 'compact_display_service_abort_system_sleep_prepare'; Marker = 's_animation_system_sleep_preparing' },
    @{ File = 'main\round_display_service.c'; Prepare = 'round_display_service_prepare_system_sleep'; Abort = 'round_display_service_abort_system_sleep_prepare'; Marker = 's_round_display_animation_system_sleep_preparing' },
    @{ File = 'main\boards\fangtang_4g\fangtang_ml307_transport.cpp'; Prepare = 'ml307_transport_prepare_system_sleep'; Abort = 'ml307_transport_abort_system_sleep_prepare'; Marker = 's_system_sleep_preparing' }
)
# Gateway workers and their concrete ESP HTTP active-client registry are owned
# by Gateway Transport. Gateway Lifecycle calls its bounded, value-only
# cancellation API directly, so neither the composition root, Connectivity nor
# Device API learns client/task handles.
$gatewayLifecycleParticipants = @(
    @{ File = 'main\services\gateway_transport.c'; Prepare = 'gateway_transport_prepare_system_sleep'; Abort = 'gateway_transport_abort_system_sleep_prepare'; Marker = 's_system_sleep_preparing' },
    @{ File = 'main\services\gateway_dispatcher.c'; Prepare = 'gateway_dispatcher_prepare_system_sleep'; Abort = 'gateway_dispatcher_abort_system_sleep_prepare'; Marker = 's_system_sleep_preparing' }
)
$startupPetRetryParticipant = @{
    File = 'main\services\startup_pet_retry_service.c';
    Prepare = 'startup_pet_retry_service_prepare_system_sleep';
    Abort = 'startup_pet_retry_service_abort_system_sleep_prepare';
    Marker = 's_system_sleep_preparing'
}
$startupPetWorkerParticipant = @{
    File = 'main\services\startup_pet_worker_service.c';
    Prepare = 'startup_pet_worker_service_prepare_system_sleep';
    Abort = 'startup_pet_worker_service_abort_system_sleep_prepare';
    Marker = 's_system_sleep_preparing'
}
# These root-only coordinators are invoked by Connectivity's concrete
# cancellation bridge. Wi-Fi/IP default-loop callback admission is now owned
# by Connectivity Service itself; main.c retains only physical event instance
# registration/unregistration. The remaining root workers must nevertheless
# retain closed admission until that bridge's resumer executes the paired ABORT.
$mainCompositionParticipants = @(
    @{ Prepare = 'prepare_startup_pet_asset_system_sleep'; Abort = 'abort_startup_pet_asset_system_sleep_prepare'; Marker = 's_startup_pet_system_sleep_preparing' },
    @{ Prepare = 'prepare_wake_restart_system_sleep'; Abort = 'abort_wake_restart_system_sleep_prepare'; Marker = 's_wake_restart_system_sleep_preparing' },
    @{ Prepare = 'prepare_deferred_setup_system_sleep'; Abort = 'abort_deferred_setup_system_sleep_prepare'; Marker = 's_deferred_setup_system_sleep_preparing' },
    @{ Prepare = 'prepare_output_volume_persist_system_sleep'; Abort = 'abort_output_volume_persist_system_sleep_prepare'; Marker = 's_output_volume_persist_system_sleep_preparing' }
)
$failures = @()

foreach ($participant in $participants) {
    $path = Join-Path $projectRoot $participant.File
    if (-not (Test-Path -LiteralPath $path)) {
        $failures += "missing $path"
        continue
    }
    $text = Get-Content -LiteralPath $path -Raw
    $pattern = 'device_status_t\s+' + [regex]::Escape($participant.Prepare) +
               '\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*void\s+' +
               [regex]::Escape($participant.Abort)
    $match = [regex]::Match($text, $pattern)
    if (-not $match.Success) {
        $failures += "$($participant.File): cannot inspect $($participant.Prepare) through its paired ABORT"
        continue
    }
    $body = $match.Groups[1].Value
    if ($body -match ('\b' + [regex]::Escape($participant.Abort) + '\s*\(')) {
        $failures += "$($participant.File): PREPARE must not invoke its ABORT; Power owns reverse-order rollback"
    }
    if ($body -match '(?:s_system_sleep_preparing|system_sleep_preparing)\s*=\s*false|__atomic_store_n\s*\(\s*&s_system_sleep_preparing\s*,\s*false') {
        $failures += "$($participant.File): PREPARE failure must retain System Sleep admission closed until Power ABORT"
    }
}

# Cellular retry is a Connectivity-domain coordinator, not a composition-root
# task. It follows the same irreversible-on-timeout rule, but the root only
# calls its public participant through the registered cancellation bridge.
$cellularRecoveryPath = Join-Path $projectRoot 'main\services\cellular_recovery_service.c'
if (-not (Test-Path -LiteralPath $cellularRecoveryPath)) {
    $failures += "missing $cellularRecoveryPath"
} else {
    $cellularRecoveryText = Get-Content -LiteralPath $cellularRecoveryPath -Raw
    $cellularRecoveryPreparePattern =
        'device_status_t\s+cellular_recovery_service_prepare_system_sleep\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*void\s+cellular_recovery_service_abort_system_sleep_prepare'
    $cellularRecoveryPrepareMatch = [regex]::Match(
        $cellularRecoveryText, $cellularRecoveryPreparePattern)
    if (-not $cellularRecoveryPrepareMatch.Success) {
        $failures += 'main/services/cellular_recovery_service.c: cannot inspect Cellular Recovery PREPARE through its paired ABORT'
    } else {
        $cellularRecoveryPrepareBody = $cellularRecoveryPrepareMatch.Groups[1].Value
        if ($cellularRecoveryPrepareBody -match '\bcellular_recovery_service_abort_system_sleep_prepare\s*\(' -or
            $cellularRecoveryPrepareBody -match 's_system_sleep_preparing\s*=\s*false') {
            $failures += 'main/services/cellular_recovery_service.c: Cellular Recovery PREPARE failure must remain closed until Connectivity rollback'
        }
    }
}

foreach ($participant in $privateHardwareParticipants) {
    $path = Join-Path $projectRoot $participant.File
    if (-not (Test-Path -LiteralPath $path)) {
        $failures += "missing $path"
        continue
    }
    $text = Get-Content -LiteralPath $path -Raw
    $pattern = '(?:extern\s+"C"\s+)?(?:device_status_t|esp_err_t)\s+' + [regex]::Escape($participant.Prepare) +
                '\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*void\s+' +
                [regex]::Escape($participant.Abort)
    if ($participant.File -like '*.cpp') {
        $pattern = '(?:extern\s+"C"\s+)?(?:device_status_t|esp_err_t)\s+' + [regex]::Escape($participant.Prepare) +
                   '\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*(?:extern\s+"C"\s+)?void\s+' +
                   [regex]::Escape($participant.Abort)
    }
    $match = [regex]::Match($text, $pattern)
    if (-not $match.Success) {
        $failures += "$($participant.File): cannot inspect $($participant.Prepare) through its paired ABORT"
        continue
    }
    $body = $match.Groups[1].Value
    if ($body -match ('\b' + [regex]::Escape($participant.Abort) + '\s*\(')) {
        $failures += "$($participant.File): private worker PREPARE must not invoke its ABORT; Power owns reverse-order rollback"
    }
    if ($body -match ([regex]::Escape($participant.Marker) + '\s*=\s*false')) {
        $failures += "$($participant.File): private worker PREPARE failure must retain electrical admission closed until Power ABORT"
    }
}

foreach ($participant in $gatewayLifecycleParticipants) {
    $path = Join-Path $projectRoot $participant.File
    if (-not (Test-Path -LiteralPath $path)) {
        $failures += "missing $path"
        continue
    }
    $text = Get-Content -LiteralPath $path -Raw
    $pattern = 'device_status_t\s+' + [regex]::Escape($participant.Prepare) +
               '\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*void\s+' +
               [regex]::Escape($participant.Abort)
    $match = [regex]::Match($text, $pattern)
    if (-not $match.Success) {
            $failures += "$($participant.File): cannot inspect Gateway lifecycle $($participant.Prepare) through its paired ABORT"
        continue
    }
    $body = $match.Groups[1].Value
    if ($body -match ('\b' + [regex]::Escape($participant.Abort) + '\s*\(')) {
            $failures += "$($participant.File): Gateway worker PREPARE must not invoke its ABORT; Connectivity/Power owns reverse-order rollback"
    }
    if ($body -match ([regex]::Escape($participant.Marker) + '\s*=\s*false')) {
            $failures += "$($participant.File): Gateway worker PREPARE failure must retain admission closed until Connectivity/Power ABORT"
    }
}

$retryPath = Join-Path $projectRoot $startupPetRetryParticipant.File
if (-not (Test-Path -LiteralPath $retryPath)) {
    $failures += "missing $retryPath"
} else {
    $retryText = Get-Content -LiteralPath $retryPath -Raw
    $retryPattern = 'device_status_t\s+' + [regex]::Escape($startupPetRetryParticipant.Prepare) +
                    '\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*void\s+' +
                    [regex]::Escape($startupPetRetryParticipant.Abort)
    $retryMatch = [regex]::Match($retryText, $retryPattern)
    if (-not $retryMatch.Success) {
        $failures += 'main/services/startup_pet_retry_service.c: cannot inspect retry PREPARE through its paired ABORT'
    } else {
        $retryPrepareBody = $retryMatch.Groups[1].Value
        if ($retryPrepareBody -match ('\b' + [regex]::Escape($startupPetRetryParticipant.Abort) + '\s*\(') -or
            $retryPrepareBody -match 's_system_sleep_preparing\s*=\s*false') {
            $failures += 'main/services/startup_pet_retry_service.c: retry PREPARE failure must retain callback admission closed until root rollback'
        }
    }
}

$workerPath = Join-Path $projectRoot $startupPetWorkerParticipant.File
if (-not (Test-Path -LiteralPath $workerPath)) {
    $failures += "missing $workerPath"
} else {
    $workerText = Get-Content -LiteralPath $workerPath -Raw
    $workerPattern = 'device_status_t\s+' + [regex]::Escape($startupPetWorkerParticipant.Prepare) +
                     '\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*void\s+' +
                     [regex]::Escape($startupPetWorkerParticipant.Abort)
    $workerMatch = [regex]::Match($workerText, $workerPattern)
    if (-not $workerMatch.Success) {
        $failures += 'main/services/startup_pet_worker_service.c: cannot inspect worker PREPARE through its paired ABORT'
    } else {
        $workerPrepareBody = $workerMatch.Groups[1].Value
        if ($workerPrepareBody -match ('\b' + [regex]::Escape($startupPetWorkerParticipant.Abort) + '\s*\(') -or
            $workerPrepareBody -match 's_system_sleep_preparing\s*=\s*false') {
            $failures += 'main/services/startup_pet_worker_service.c: worker PREPARE failure must retain task admission closed until root rollback'
        }
    }
    foreach ($workerFence in @(
            'task_registry_register\s*\(',
            'task_registry_unregister_with_timeout\s*\(',
            's_system_sleep_restart_pending',
            'MALLOC_CAP_SPIRAM',
            'restart_after_system_sleep_abort')) {
        if ($workerText -notmatch $workerFence) {
            $failures += "main/services/startup_pet_worker_service.c: worker lifecycle fence is incomplete ($workerFence)"
        }
    }
}

$petCachePath = Join-Path $projectRoot 'main\services\pet_cache_service.c'
if (-not (Test-Path -LiteralPath $petCachePath)) {
    $failures += "missing $petCachePath"
} else {
    $petCacheText = Get-Content -LiteralPath $petCachePath -Raw
    foreach ($cacheFence in @(
            's_exit_status',
            's_registry_retirement_failed',
            'task_registry_unregister_with_timeout\s*\(',
            's_system_sleep_stop_was_requested',
            's_stop_requested\s*=\s*s_registry_retirement_failed')) {
        if ($petCacheText -notmatch $cacheFence) {
            $failures += "main/services/pet_cache_service.c: cache retirement fence is incomplete ($cacheFence)"
        }
    }
    $cacheAbortPattern = 'void\s+pet_cache_service_abort_system_sleep_prepare\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}'
    $cacheAbortMatch = [regex]::Match($petCacheText, $cacheAbortPattern)
    if (-not $cacheAbortMatch.Success -or
        $cacheAbortMatch.Groups[1].Value -notmatch 's_registry_retirement_failed') {
        $failures += 'main/services/pet_cache_service.c: cache System Sleep ABORT must retain failed Registry retirement closed'
    }
}

# Generic Persistence owns a Storage Registry worker as well. Its public
# System Sleep participant may reopen request admission only while that worker
# has positively retired; otherwise an old immutable identity could later be
# stopped by an owner sweep after ABORT admits new NVS work.
$sharedPersistencePath = Join-Path $projectRoot 'main\persistence_service.c'
if (-not (Test-Path -LiteralPath $sharedPersistencePath)) {
    $failures += "missing $sharedPersistencePath"
} else {
    $sharedPersistenceText = Get-Content -LiteralPath $sharedPersistencePath -Raw
    foreach ($persistenceFence in @(
            's_worker_start_gate',
            's_worker_retiring',
            's_worker_exit_status',
            's_worker_registry_retirement_failed',
            'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
            's_worker_exit_status\s*=\s*registry_err',
            '__atomic_store_n\s*\(\s*&s_accepting\s*,\s*false',
            '!s_worker_registry_retirement_failed')) {
        if ($sharedPersistenceText -notmatch $persistenceFence) {
            $failures += "main/persistence_service.c: shared Storage worker retirement fence is incomplete ($persistenceFence)"
        }
    }
    $sharedPersistenceAbortPattern =
        'void\s+persistence_service_abort_system_sleep_prepare\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}'
    $sharedPersistenceAbortMatch = [regex]::Match($sharedPersistenceText, $sharedPersistenceAbortPattern)
    if (-not $sharedPersistenceAbortMatch.Success -or
        $sharedPersistenceAbortMatch.Groups[1].Value -notmatch
        's_worker_registry_retirement_failed') {
        $failures += 'main/persistence_service.c: shared Storage System Sleep ABORT must retain failed Registry retirement closed'
    }
}

# Display Service owns a logical BOARD Registry worker while profile-private
# panel/DMA lifetime remains boot-scoped. A terminal display STOP must still
# retire that identity before it publishes completion, otherwise a future
# logical restart can overlap an old owner sweep.
$displayServicePath = Join-Path $projectRoot 'main\display_service.c'
if (-not (Test-Path -LiteralPath $displayServicePath)) {
    $failures += "missing $displayServicePath"
} else {
    $displayServiceText = Get-Content -LiteralPath $displayServicePath -Raw
    foreach ($displayFence in @(
            's_display_service_start_gate',
            's_display_service_retiring',
            's_display_service_exit_status',
            's_display_service_registry_retirement_failed',
            'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
            's_display_service_exit_status\s*=\s*registry_err',
            's_display_service_registry_retirement_failed\s*=\s*true',
            's_display_service_registry_retirement_failed\s*\|\|')) {
        if ($displayServiceText -notmatch $displayFence) {
            $failures += "main/display_service.c: logical Display worker retirement fence is incomplete ($displayFence)"
        }
    }
}

# The remaining root-owned internal-stack persistence worker must apply the
# same retirement rule as the extracted cache worker: a completed task whose
# immutable Storage Registry identity could not be removed is terminally
# closed, and System Sleep ABORT cannot reopen request admission.
$rootPersistencePath = Join-Path $projectRoot 'main\main.c'
if (-not (Test-Path -LiteralPath $rootPersistencePath)) {
    $failures += "missing $rootPersistencePath"
} else {
    $rootPersistenceText = Get-Content -LiteralPath $rootPersistencePath -Raw
    foreach ($persistenceFence in @(
            's_output_volume_persist_exit_status',
            's_output_volume_persist_retiring',
            's_output_volume_persist_registry_retirement_failed',
            'const esp_err_t registry_err = task_registry_unregister_with_timeout\s*\(',
            's_output_volume_persist_exit_status\s*=\s*registry_err',
            's_output_volume_persist_stop_requested\s*=\s*true',
            '!s_output_volume_persist_registry_retirement_failed')) {
        if ($rootPersistenceText -notmatch $persistenceFence) {
            $failures += "main/main.c: root Storage worker retirement fence is incomplete ($persistenceFence)"
        }
    }
    $rootPersistenceAbortPattern =
        'static\s+void\s+abort_output_volume_persist_system_sleep_prepare\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}'
    $rootPersistenceAbortMatch = [regex]::Match($rootPersistenceText, $rootPersistenceAbortPattern)
    if (-not $rootPersistenceAbortMatch.Success -or
        $rootPersistenceAbortMatch.Groups[1].Value -notmatch 's_output_volume_persist_registry_retirement_failed') {
        $failures += 'main/main.c: root Storage System Sleep ABORT must retain failed Registry retirement closed'
    }
}

# Wake restart and deferred portal setup are still root-private coordinators,
# but each publishes an immutable Registry identity. Their task handles and
# completion semaphores must not turn a failed retirement into a false success
# or let ABORT recreate a replacement against the old identity.
if (-not (Test-Path -LiteralPath $rootPersistencePath)) {
    $failures += "missing $rootPersistencePath"
} else {
    $rootCoordinatorText = Get-Content -LiteralPath $rootPersistencePath -Raw
    foreach ($coordinatorFence in @(
            's_wake_restart_exit_status',
            's_wake_restart_registry_retirement_failed',
            's_deferred_setup_exit_status',
            's_deferred_setup_registry_retirement_failed',
            's_wake_restart_exit_status\s*=\s*registry_err',
            's_deferred_setup_exit_status\s*=\s*registry_err',
            '!s_wake_restart_registry_retirement_failed',
            '!s_deferred_setup_registry_retirement_failed')) {
        if ($rootCoordinatorText -notmatch $coordinatorFence) {
            $failures += "main/main.c: root coordinator retirement fence is incomplete ($coordinatorFence)"
        }
    }
    foreach ($abortRequirement in @(
            @{ Function = 'abort_wake_restart_system_sleep_prepare'; Marker = 's_wake_restart_registry_retirement_failed' },
            @{ Function = 'abort_deferred_setup_system_sleep_prepare'; Marker = 's_deferred_setup_registry_retirement_failed' })) {
        $abortPattern = 'static\s+void\s+' + [regex]::Escape($abortRequirement.Function) +
                        '\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}'
        $abortMatch = [regex]::Match($rootCoordinatorText, $abortPattern)
        if (-not $abortMatch.Success -or $abortMatch.Groups[1].Value -notmatch $abortRequirement.Marker) {
            $failures += "main/main.c: $($abortRequirement.Function) must retain failed Registry retirement closed"
        }
    }
}

$powerPath = Join-Path $projectRoot 'main\power_service.c'
if (-not (Test-Path -LiteralPath $powerPath)) {
    $failures += "missing $powerPath"
} else {
    $powerText = Get-Content -LiteralPath $powerPath -Raw
    foreach ($participant in $participants) {
        $count = [regex]::Matches(
            $powerText, '\b' + [regex]::Escape($participant.Abort) + '\s*\(').Count
        if ($count -lt 2) {
            $failures += "main/power_service.c: insufficient rollback coverage for $($participant.Abort)"
        }
    }
    # Power's own retained DISPLAY_OFF scheduler is the first System Sleep
    # participant. Its paired ABORT appears before PREPARE in the source, so
    # inspect the bounded PREPARE slice separately and reject local reopening.
    $schedulerPreparePattern = 'static\s+device_status_t\s+prepare_display_off_scheduler_system_sleep' +
                               '\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*static\s+TickType_t\s+power_transition_lock_timeout'
    $schedulerPrepareMatch = [regex]::Match($powerText, $schedulerPreparePattern)
    if (-not $schedulerPrepareMatch.Success) {
        $failures += 'main/power_service.c: cannot inspect DISPLAY_OFF scheduler System Sleep PREPARE boundary'
    } else {
        $schedulerPrepareBody = $schedulerPrepareMatch.Groups[1].Value
        if ($schedulerPrepareBody -match '\babort_display_off_scheduler_system_sleep_prepare\s*\(' -or
            $schedulerPrepareBody -match 's_system_sleep_display_off_scheduler_preparing\s*=\s*false' -or
            $schedulerPrepareBody -match 's_display_off_timer_callback_admission_open\s*=\s*true') {
            $failures += 'main/power_service.c: DISPLAY_OFF scheduler PREPARE failure must remain parked until Power reverse rollback'
        }
    }
}

$compositionRootPath = Join-Path $projectRoot 'main\main.c'
if (-not (Test-Path -LiteralPath $compositionRootPath)) {
    $failures += "missing $compositionRootPath"
} else {
    $compositionRootText = Get-Content -LiteralPath $compositionRootPath -Raw
    $gatewayLifecyclePath = Join-Path $projectRoot 'main\services\gateway_lifecycle_service.c'
    if (-not (Test-Path -LiteralPath $gatewayLifecyclePath)) {
        $failures += "missing $gatewayLifecyclePath"
    } else {
        $gatewayLifecycleText = Get-Content -LiteralPath $gatewayLifecyclePath -Raw
        if ($gatewayLifecycleText -notmatch 'gateway_lifecycle_service_prepare_system_sleep' -or
            $gatewayLifecycleText -notmatch 'gateway_lifecycle_service_abort_system_sleep_prepare' -or
            $gatewayLifecycleText -notmatch 'restore_prepared_workers') {
            $failures += 'main/services/gateway_lifecycle_service.c: missing Gateway PREPARE/ABORT lifecycle ownership'
        }
    }
    foreach ($participant in $gatewayLifecycleParticipants) {
        # Their paired ABORT calls are intentionally owned by the service,
        # not duplicated in main.c alongside HTTP client registry state.
    }
    # Clock sync is now a Connectivity-domain service. Its ESP-NETIF singleton
    # and retry worker remain private there; main.c only wires its value API.
    $clockSyncPath = Join-Path $projectRoot 'main\services\clock_sync_service.c'
    if (-not (Test-Path -LiteralPath $clockSyncPath)) {
        $failures += "missing $clockSyncPath"
    } else {
        $clockSyncText = Get-Content -LiteralPath $clockSyncPath -Raw
        $clockSyncPreparePattern =
            'device_status_t\s+clock_sync_service_prepare_system_sleep\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*void\s+clock_sync_service_abort_system_sleep_prepare'
        $clockSyncPrepareMatch = [regex]::Match($clockSyncText, $clockSyncPreparePattern)
        if (-not $clockSyncPrepareMatch.Success) {
            $failures += 'main/services/clock_sync_service.c: cannot inspect Clock Sync PREPARE through its paired ABORT'
        } else {
            $clockSyncPrepareBody = $clockSyncPrepareMatch.Groups[1].Value
            if ($clockSyncPrepareBody -match '\bclock_sync_service_abort_system_sleep_prepare\s*\(' -or
                $clockSyncPrepareBody -match 's_system_sleep_preparing\s*=\s*false') {
                $failures += 'main/services/clock_sync_service.c: Clock Sync PREPARE failure must remain closed until Connectivity rollback'
            }
        }
        if ($clockSyncText -notmatch 's_system_sleep_restart_pending\s*=\s*true' -or
            $clockSyncText -notmatch 's_retiring' -or
            $clockSyncText -notmatch 'restart_after_abort\)\s*start_internal\s*\(\s*true\s*\)') {
            $failures += 'main/services/clock_sync_service.c: timed-out Clock Sync rollback must defer restart until old Registry identity exits'
        }
    }
    foreach ($participant in $mainCompositionParticipants) {
        $pattern = 'static\s+device_status_t\s+' + [regex]::Escape($participant.Prepare) +
                   '\s*\([^\)]*\)\s*\{([\s\S]*?)\n\}\s*\n\s*static\s+void\s+' +
                   [regex]::Escape($participant.Abort)
        $match = [regex]::Match($compositionRootText, $pattern)
        if (-not $match.Success) {
            $failures += "main/main.c: cannot inspect root-owned $($participant.Prepare) through its paired ABORT"
            continue
        }
        $body = $match.Groups[1].Value
        if ($body -match ('\b' + [regex]::Escape($participant.Abort) + '\s*\(')) {
            $failures += "main/main.c: root-owned PREPARE must not invoke $($participant.Abort); Connectivity/Power owns reverse rollback"
        }
        if ($body -match ([regex]::Escape($participant.Marker) + '\s*=\s*false')) {
            $failures += "main/main.c: root-owned $($participant.Prepare) failure must retain its admission marker until Connectivity/Power ABORT"
        }
        $count = [regex]::Matches(
            $compositionRootText, '\b' + [regex]::Escape($participant.Abort) + '\s*\(').Count
        if ($count -lt 2) {
            $failures += "main/main.c: insufficient cancellation/resumer rollback coverage for $($participant.Abort)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("System Sleep failure-closure check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'System Sleep failure-closure check passed: shared participants and private hardware workers remain closed until Power ABORT'
