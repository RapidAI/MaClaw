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
    'main/platform_audio.h',
    'main/platform_connectivity.h',
    'main/platform_display.h',
    'main/platform_input.h',
    'main/platform_lifecycle.h',
    'main/platform_power.h',
    'main/platform_sensor.h',
    'main/platform_storage.h'
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
$allSourceFiles = Get-ChildItem -Path (Join-Path $projectRoot 'main') -Recurse -File -Include '*.c','*.cpp','*.h'
$allCFiles = $allSourceFiles | Where-Object { $_.Extension -in @('.c','.h') }
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
    $audioLifecycleExpected = @('main/round_audio_service.c', 'main/round_peripheral_service.c')
    if (($audioLifecycleActual -join '|') -ne ($audioLifecycleExpected -join '|')) {
        $violations += "${roundAudioLifecycle}: may be included only by $($audioLifecycleExpected -join ', '); found: $($audioLifecycleActual -join ', ')"
    }
}

# Shared renderers implement scenes and session policy only.  A private
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
$deviceApiSource = Join-Path $projectRoot 'main/device_api.c'
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
# Cellular lifecycle is one Connectivity Service transaction.  The composition
# root may still own UI/gateway retry policy, but it must not reconstruct that
# transaction from a ready flag plus separate prepare/start calls.  That would
# make a new 4G profile responsible for reproducing the stale-session and
# rollback-generation checks already owned by the service.
$mainConnectivitySource = Join-Path $projectRoot 'main/main.c'
if (Select-String -Path $mainConnectivitySource -Pattern 'device_connectivity_(?:prepare_cellular_transport|start_cellular_transport|set_cellular_ready)\s*\(' -Quiet) {
    $violations += 'main/main.c: cellular readiness and prepare/start sequencing must use device_connectivity_establish_cellular_transport()'
}
if (-not (Select-String -Path $mainConnectivitySource -Pattern 'device_connectivity_establish_cellular_transport\s*\(' -Quiet)) {
    $violations += 'main/main.c: cellular composition root must use the Connectivity Service establish transaction'
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

# Round PCM cleanup is a private Audio-HAL transaction.  The shared renderer
# may decide capture/VAD policy, but it must not grow another codec-scale AGC,
# DC blocker, or damaged-sample filter beside a future board adapter.
$roundRenderer = Join-Path $projectRoot 'main/board_port.c'
# The private round Input stack is already the source owner for touch/key
# sampling and gesture lifecycle.  It must use normalized Device Input values
# directly: including board_port.h here would make a future circular profile
# inherit the legacy board facade merely to publish a gesture.
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
        '(?s)esp_err_t\s+board_port_stop_background_tasks\s*\(\s*uint32_t\s+timeout_ms\s*\)\s*\{.*?\n\}')
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

if ($violations.Count -ne 0) {
    Write-Error ("HAL boundary check failed:`n" + ($violations -join "`n"))
    exit 1
}

Write-Output 'HAL boundary check passed: shared Device/Platform headers expose no SDK, RTOS, JSON, or driver object types.'
