# Historical physical filename only: this translation unit is the private
# shared-round renderer source owner, not a public board-port facade.
set(RENDERER_SOURCE "board_port.c")
set(BOARD_PROFILE_SOURCE "boards/echoear_2st/board_profile.c")
set(ROUND_INPUT_SERVICE_SOURCE)
set(ROUND_INPUT_PROFILE_SERVICE_SOURCE)
set(ROUND_AUDIO_SERVICE_SOURCE)
set(ROUND_WAKE_SERVICE_SOURCE)
set(ROUND_DISPLAY_SERVICE_SOURCE)
set(ROUND_VISUAL_PROFILE_SERVICE_SOURCE)
set(ROUND_PERIPHERAL_SERVICE_SOURCE)
set(COMPACT_DISPLAY_SERVICE_SOURCE)
set(COMPACT_AUDIO_SERVICE_SOURCE)
set(COMPACT_WAKE_SERVICE_SOURCE)
set(COMPACT_INPUT_SERVICE_SOURCE)
set(COMPACT_CONNECTIVITY_SERVICE_SOURCE)
set(COMPACT_PERIPHERAL_SERVICE_SOURCE)
set(COMPACT_VISUAL_PROFILE_SERVICE_SOURCE)
set(PLATFORM_AUDIO_PROFILE_SOURCE)
set(PLATFORM_POWER_PROFILE_SOURCE)
set(PLATFORM_WAKE_PROFILE_SOURCE)
set(PLATFORM_SENSOR_PROFILE_SOURCE)
set(PLATFORM_DISPLAY_PROFILE_SOURCE)
set(PLATFORM_INPUT_PROFILE_SOURCE)
set(PLATFORM_CONNECTIVITY_PROFILE_SOURCE)
set(PLATFORM_LIFECYCLE_PROFILE_SOURCE)
set(PLATFORM_STORAGE_PROFILE_SOURCE)
set(PLATFORM_BOOTSTRAP_PROFILE_SOURCE)
# ESP-IDF performs an early component-requirement expansion in a separate
# `cmake -P` process. Command-line cache variables from the top-level build
# are not forwarded to that process, whereas the profile wrapper's environment
# is. Restore the profile identity here before evaluating any profile-private
# dependency or source selection so both passes describe the same image.
if((NOT DEFINED MACLAW_PROFILE OR MACLAW_PROFILE STREQUAL "")
   AND DEFINED ENV{MACLAW_PROFILE})
    set(MACLAW_PROFILE "$ENV{MACLAW_PROFILE}")
endif()
# ESP-IDF evaluates component requirements in a separate ``cmake -P`` pass
# before Kconfig is available.  Keep a profile hint captured from the wrapper
# environment for that pass: the normal pass may later refine MACLAW_PROFILE
# from the selected board symbol, but it is too late to repair an already
# pruned public include graph.  This must remain an internal build-selection
# detail; it never reaches shared Device/Platform headers or business code.
set(MACLAW_DEPENDENCY_PROFILE "${MACLAW_PROFILE}")
if(DEFINED ENV{MACLAW_PROFILE} AND NOT "$ENV{MACLAW_PROFILE}" STREQUAL "")
    set(MACLAW_DEPENDENCY_PROFILE "$ENV{MACLAW_PROFILE}")
endif()
if(CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD OR CONFIG_MACLAW_BOARD_REFERENCE_FAKE)
    # `compact_renderer.c` owns the common compact scene/session mechanics.
    # The selected profile owns all physical display, audio, input and
    # connectivity adapters below that renderer boundary.
    set(RENDERER_SOURCE "compact_renderer.c")
    set(COMPACT_DISPLAY_SERVICE_SOURCE "compact_display_service.c")
    set(COMPACT_AUDIO_SERVICE_SOURCE "compact_audio_service.c")
    set(COMPACT_WAKE_SERVICE_SOURCE "compact_wake_service.c")
    set(COMPACT_INPUT_SERVICE_SOURCE "compact_input_service.c")
    set(COMPACT_CONNECTIVITY_SERVICE_SOURCE "compact_connectivity_service.c")
    set(COMPACT_PERIPHERAL_SERVICE_SOURCE "compact_peripheral_service.c")
    set(COMPACT_VISUAL_PROFILE_SERVICE_SOURCE "compact_visual_profile_service.c")
    set(PLATFORM_AUDIO_PROFILE_SOURCE "platform_audio_compact.c")
    set(PLATFORM_POWER_PROFILE_SOURCE "platform_power_compact.c")
    set(PLATFORM_WAKE_PROFILE_SOURCE "platform_wake_bread_compact.c")
    set(PLATFORM_SENSOR_PROFILE_SOURCE "platform_sensor_compact.c")
    set(PLATFORM_DISPLAY_PROFILE_SOURCE "platform_display_compact.c")
    set(PLATFORM_INPUT_PROFILE_SOURCE "platform_input_compact.c")
    set(PLATFORM_CONNECTIVITY_PROFILE_SOURCE "platform_connectivity_compact.c")
    set(PLATFORM_LIFECYCLE_PROFILE_SOURCE "platform_lifecycle_compact.c")
    set(PLATFORM_STORAGE_PROFILE_SOURCE "platform_storage_compact.c")
    set(PLATFORM_BOOTSTRAP_PROFILE_SOURCE "platform_bootstrap_compact.c")
    if(CONFIG_MACLAW_BOARD_REFERENCE_FAKE)
        # This CI-only profile deliberately reuses the compact physical HAL
        # contract, but supplies an independently compiled immutable profile
        # declaration. Shared business/presentation sources remain unchanged.
        set(BOARD_PROFILE_SOURCE "boards/reference_fake/board_profile.c")
        set(MACLAW_PROFILE "reference-fake")
    else()
        set(BOARD_PROFILE_SOURCE "boards/bread_compact/board_profile.c")
        set(MACLAW_PROFILE "bread-compact")
    endif()
    # Kconfig is authoritative once configured; avoid stale profile-specific
    # component requirements from a reused build directory.
elseif(CONFIG_MACLAW_BOARD_FANGTANG_4G)
    # Both rectangular profiles compile the same board-neutral compact
    # renderer. Fangtang identity composition is a separate profile-private
    # source; it must not include Bread's renderer translation unit.
    set(RENDERER_SOURCE "compact_renderer.c")
    set(COMPACT_DISPLAY_SERVICE_SOURCE "compact_display_service.c")
    set(COMPACT_AUDIO_SERVICE_SOURCE "compact_audio_service.c")
    set(COMPACT_WAKE_SERVICE_SOURCE "compact_wake_service.c")
    set(COMPACT_INPUT_SERVICE_SOURCE "compact_input_service.c")
    set(COMPACT_CONNECTIVITY_SERVICE_SOURCE "compact_connectivity_service.c")
    set(COMPACT_PERIPHERAL_SERVICE_SOURCE "compact_peripheral_service.c")
    set(COMPACT_VISUAL_PROFILE_SERVICE_SOURCE "compact_visual_profile_service.c")
    set(PLATFORM_AUDIO_PROFILE_SOURCE "platform_audio_compact.c")
    set(PLATFORM_POWER_PROFILE_SOURCE "platform_power_compact.c")
    set(PLATFORM_WAKE_PROFILE_SOURCE "platform_wake_fangtang_4g.c")
    set(PLATFORM_SENSOR_PROFILE_SOURCE "platform_sensor_compact.c")
    set(PLATFORM_DISPLAY_PROFILE_SOURCE "platform_display_compact.c")
    set(PLATFORM_INPUT_PROFILE_SOURCE "platform_input_compact.c")
    set(PLATFORM_CONNECTIVITY_PROFILE_SOURCE "platform_connectivity_compact.c")
    set(PLATFORM_LIFECYCLE_PROFILE_SOURCE "platform_lifecycle_compact.c")
    set(PLATFORM_STORAGE_PROFILE_SOURCE "platform_storage_compact.c")
    set(PLATFORM_BOOTSTRAP_PROFILE_SOURCE "platform_bootstrap_compact.c")
    set(BOARD_PROFILE_SOURCE "boards/fangtang_4g/board_profile.c")
    set(MACLAW_PROFILE "fangtang-4g")
elseif(CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C)
    # The shared round-scene implementation is parameterised for the two
    # circular boards, while controller/pin/codec selection stays below the
    # private renderer boundary. This prevents the 466px CO5300 panel from being
    # treated as a reskinned rectangular Bread display.
    set(RENDERER_SOURCE "board_port.c")
    set(BOARD_PROFILE_SOURCE "boards/waveshare_amoled_1_75c/board_profile.c")
    set(ROUND_INPUT_SERVICE_SOURCE "round_input_service.c")
    set(ROUND_INPUT_PROFILE_SERVICE_SOURCE "round_input_profile_service.c")
    set(ROUND_AUDIO_SERVICE_SOURCE "round_audio_service.c")
    set(ROUND_WAKE_SERVICE_SOURCE "round_wake_service.c")
    set(ROUND_DISPLAY_SERVICE_SOURCE "round_display_service.c")
    set(ROUND_VISUAL_PROFILE_SERVICE_SOURCE "round_visual_profile_service.c")
    set(ROUND_PERIPHERAL_SERVICE_SOURCE "round_peripheral_service.c")
    set(PLATFORM_AUDIO_PROFILE_SOURCE "platform_audio_round.c")
    set(PLATFORM_POWER_PROFILE_SOURCE "platform_power_round.c")
    set(PLATFORM_WAKE_PROFILE_SOURCE "platform_wake_waveshare_amoled_1_75c.c")
    set(PLATFORM_SENSOR_PROFILE_SOURCE "platform_sensor_round.c")
    set(PLATFORM_DISPLAY_PROFILE_SOURCE "platform_display_round.c")
    set(PLATFORM_INPUT_PROFILE_SOURCE "platform_input_round.c")
    set(PLATFORM_CONNECTIVITY_PROFILE_SOURCE "platform_connectivity_round.c")
    set(PLATFORM_LIFECYCLE_PROFILE_SOURCE "platform_lifecycle_round.c")
    set(PLATFORM_STORAGE_PROFILE_SOURCE "platform_storage_round.c")
    set(PLATFORM_BOOTSTRAP_PROFILE_SOURCE "platform_bootstrap_round.c")
    set(MACLAW_PROFILE "waveshare-amoled-1.75c")
elseif(CONFIG_MACLAW_BOARD_ECHOEAR_2ST)
    set(MACLAW_PROFILE "echoear-2st")
    set(ROUND_INPUT_SERVICE_SOURCE "round_input_service.c")
    set(ROUND_INPUT_PROFILE_SERVICE_SOURCE "round_input_profile_service.c")
    set(ROUND_AUDIO_SERVICE_SOURCE "round_audio_service.c")
    set(ROUND_WAKE_SERVICE_SOURCE "round_wake_service.c")
    set(ROUND_DISPLAY_SERVICE_SOURCE "round_display_service.c")
    set(ROUND_VISUAL_PROFILE_SERVICE_SOURCE "round_visual_profile_service.c")
    set(ROUND_PERIPHERAL_SERVICE_SOURCE "round_peripheral_service.c")
    set(PLATFORM_AUDIO_PROFILE_SOURCE "platform_audio_round.c")
    set(PLATFORM_POWER_PROFILE_SOURCE "platform_power_round.c")
    set(PLATFORM_WAKE_PROFILE_SOURCE "platform_wake_echoear_2st.c")
    set(PLATFORM_SENSOR_PROFILE_SOURCE "platform_sensor_round.c")
    set(PLATFORM_DISPLAY_PROFILE_SOURCE "platform_display_round.c")
    set(PLATFORM_INPUT_PROFILE_SOURCE "platform_input_round.c")
    set(PLATFORM_CONNECTIVITY_PROFILE_SOURCE "platform_connectivity_round.c")
    set(PLATFORM_LIFECYCLE_PROFILE_SOURCE "platform_lifecycle_round.c")
    set(PLATFORM_STORAGE_PROFILE_SOURCE "platform_storage_round.c")
    set(PLATFORM_BOOTSTRAP_PROFILE_SOURCE "platform_bootstrap_round.c")
endif()

# Keep the cellular implementation entirely out of the established EchoEar and
# Bread Compact images.  The source has its own Kconfig guards as well; this
# conditional also prevents the modem library from consuming their flash.
set(MACLAW_COMPONENT_REQUIRES
    esp_app_format esp_event esp_wifi wpa_supplicant esp_http_client esp-tls
    esp_timer esp_netif nvs_flash cjson esp_lcd esp_driver_gpio esp_driver_spi
    esp_driver_i2c esp_driver_i2s esp_driver_ledc esp_adc esp_http_server spiffs
    mbedtls spi_flash esp_psram espressif__qrcode
    espressif__esp-sr espressif__esp_audio_codec)
if(MACLAW_DEPENDENCY_PROFILE STREQUAL "echoear-2st")
    # Component Manager resolves manifests before Kconfig exists.  The profile
    # wrapper therefore supplies this identity so the compiled main component
    # receives the selected controller's public headers as well.
    list(APPEND MACLAW_COMPONENT_REQUIRES espressif__esp_lcd_st77916)
elseif(MACLAW_DEPENDENCY_PROFILE STREQUAL "fangtang-4g")
    list(APPEND MACLAW_COMPONENT_REQUIRES 78__esp_lcd_nv3023 78__esp-ml307
                                         78__uart-uhci)
elseif(MACLAW_DEPENDENCY_PROFILE STREQUAL "waveshare-amoled-1.75c")
    list(APPEND MACLAW_COMPONENT_REQUIRES espressif__esp_lcd_co5300
                                        espressif__esp_lcd_touch
                                        waveshare__esp_lcd_touch_cst9217)
elseif(CONFIG_MACLAW_BOARD_ECHOEAR_2ST)
    # ST77916 is an EchoEar-only QSPI controller. Keep the component out of
    # rectangular and CO5300 builds so a clean profile manifest describes
    # exactly the selected physical panel.
    list(APPEND MACLAW_COMPONENT_REQUIRES espressif__esp_lcd_st77916)
elseif(CONFIG_MACLAW_BOARD_FANGTANG_4G)
    # Fangtang owns the NV3023 controller and its 80-row GRAM offset through
    # its profile-private display adapter.
    list(APPEND MACLAW_COMPONENT_REQUIRES 78__esp_lcd_nv3023)
elseif(CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C)
    # CO5300 and CST9217 are both exclusive to the Waveshare round profile.
    list(APPEND MACLAW_COMPONENT_REQUIRES espressif__esp_lcd_co5300
                                        espressif__esp_lcd_touch
                                        waveshare__esp_lcd_touch_cst9217)
endif()

if(MACLAW_PROFILE STREQUAL "fangtang-4g" OR CONFIG_MACLAW_BOARD_FANGTANG_4G)
    list(APPEND MACLAW_BOARD_SRCS
         "boards/fangtang_4g/fangtang_ml307_transport.cpp"
         "boards/fangtang_4g/fangtang_network_identity.c"
         "boards/fangtang_4g/fangtang_identity_art_renderer.c"
         "boards/fangtang_4g/fangtang_identity_composer.c"
         "boards/fangtang_4g/fangtang_identity_network_renderer.c"
         "boards/fangtang_4g/fangtang_visual_profile.c")
endif()

set(MACLAW_EMBED_FILES)
if(CONFIG_MACLAW_BOARD_FANGTANG_4G)
    # A material sugar cube needs more tonal and crystal detail than the
    # procedural status renderer can carry at 188x164. Keep the physical
    # RGB565 artwork and its alpha mask isolated to the Fangtang image.
    list(APPEND MACLAW_EMBED_FILES "fangtang_sugar.rgb565" "fangtang_sugar.a8")
endif()
if(CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD OR CONFIG_MACLAW_BOARD_REFERENCE_FAKE)
    # The 240x320 robot splash is board-specific and costs 150 KiB. Fangtang
    # renders its own 240x240 sugar-cube startup surface, while EchoEar has an
    # independent round-panel renderer; neither image should carry this asset.
    list(APPEND MACLAW_EMBED_FILES "bread_compact_splash.rgb565")
endif()
if(CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C)
    # The Waveshare badge uses the dragon-totem artwork only on its boot
    # surface.  Normal standby is the common selected-pet scene, adapted by
    # the Waveshare round-display layout descriptor.
    list(APPEND MACLAW_EMBED_FILES "waveshare_totem.rgb565")
endif()

idf_component_register(
    SRCS "main.c" "app_ui.c" "app_intent_service.c" "operation_context.c" "alarm_manager.c" "fall_detection_classifier.c" "fall_detection_service.c" "sleep_schedule_service.c" "wake_deadline_service.c" "wake_service.c" "resource_pressure_service.c" "battery_policy_service.c" "persistence_service.c" "task_registry.c" "weather_cache_service.c" "meeting_recovery_service.c" "meeting_recording_storage.c" "configuration_policy.c" "configuration_source_priority.c" "configuration_effective_policy.c" "configuration_runtime_override_store.c" "configuration_apply_state.c" "configuration_reconcile_retry_policy.c" "configuration_reconcile_service.c" "configuration_revision.c" "configuration_reconcile.c" "transport_selection_transaction.c" "shared_bus_lifecycle.c" "shared_bus_recovery_transaction.c" "configuration_transaction.c" "configuration_service.c" "device_tool_registry.c" "update_service.c" "provisioning_failure_injection.c" "mp3_player.c" "firmware_identity.c" "device_api.c" "device_profile_validation.c" "fault_domain.c" "audio_service.c" "display_service.c" "connectivity_service.c" "input_service.c" "lifecycle_service.c" "motion_service.c" "storage_service.c" "pet_asset_cache_storage.c" "services/command_service.c" "services/reply_service.c" "services/meeting_service.c" "services/alarm_service.c" "services/interaction_service.c" "services/foreground_coordinator.c" "services/gateway_dispatcher.c" "services/gateway_transport.c" "services/gateway_lifecycle_service.c" "services/gateway_capability_projection.c" "services/cellular_recovery_policy.c" "services/cellular_recovery_service.c" "services/clock_sync_service.c" "services/ambient_service.c" "services/pet_asset_service.c" "services/pet_cache_service.c" "services/startup_pet_retry_service.c" "services/startup_pet_worker_service.c" "services/audio_arbitration_service.c" "services/provisioning_service.c" "services/provisioning_network_owner.c" "services/connectivity_network_core_owner.c" "services/connectivity_wifi_driver_owner.c" "services/connectivity_network_root_owner.c" "services/connectivity_restart_coordinator.c" "services/safe_mode_coordinator.c" "presentation/input_binding.c" "presentation/safe_mode_input_policy.c" "presentation/scene_presenter.c" "platform_bootstrap.c" ${PLATFORM_BOOTSTRAP_PROFILE_SOURCE} "platform_audio.c" ${PLATFORM_AUDIO_PROFILE_SOURCE} "platform_connectivity.c" ${PLATFORM_CONNECTIVITY_PROFILE_SOURCE} "platform_display.c" ${PLATFORM_DISPLAY_PROFILE_SOURCE} "platform_input.c" ${PLATFORM_INPUT_PROFILE_SOURCE} "platform_lifecycle.c" ${PLATFORM_LIFECYCLE_PROFILE_SOURCE} "platform_power.c" ${PLATFORM_POWER_PROFILE_SOURCE} "platform_wake.c" ${PLATFORM_WAKE_PROFILE_SOURCE} "platform_resource.c" "platform_sensor.c" ${PLATFORM_SENSOR_PROFILE_SOURCE} "platform_storage.c" ${PLATFORM_STORAGE_PROFILE_SOURCE} "platform_nvs.c" "power_service.c" "power_lease_service.c" ${ROUND_INPUT_SERVICE_SOURCE} ${ROUND_INPUT_PROFILE_SERVICE_SOURCE} ${ROUND_AUDIO_SERVICE_SOURCE} ${ROUND_WAKE_SERVICE_SOURCE} ${ROUND_DISPLAY_SERVICE_SOURCE} ${ROUND_VISUAL_PROFILE_SERVICE_SOURCE} ${ROUND_PERIPHERAL_SERVICE_SOURCE} ${COMPACT_DISPLAY_SERVICE_SOURCE} ${COMPACT_AUDIO_SERVICE_SOURCE} ${COMPACT_WAKE_SERVICE_SOURCE} ${COMPACT_INPUT_SERVICE_SOURCE} ${COMPACT_CONNECTIVITY_SERVICE_SOURCE} ${COMPACT_PERIPHERAL_SERVICE_SOURCE} ${COMPACT_VISUAL_PROFILE_SERVICE_SOURCE} "${BOARD_PROFILE_SOURCE}" "${RENDERER_SOURCE}" ${MACLAW_BOARD_SRCS}
    INCLUDE_DIRS "."
    EMBED_FILES ${MACLAW_EMBED_FILES}
    REQUIRES ${MACLAW_COMPONENT_REQUIRES}
)

# Both display boards must render arbitrary Chinese replies without depending
# on glyphs downloaded at runtime. Embed the packed 24-dot CJK fallback in the
# application image so the shared SPIFFS partition remains available for
# recoverable meeting recordings.
if(CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C)
    # The 466px AMOLED's larger standby arc uses the profile-native 32-dot
    # fallback; other boards retain the existing 24-dot resource.
    target_add_binary_data(${COMPONENT_LIB} "cjk32_cjk.bin" BINARY)
else()
    target_add_binary_data(${COMPONENT_LIB} "cjk24_cjk.bin" BINARY)
endif()
