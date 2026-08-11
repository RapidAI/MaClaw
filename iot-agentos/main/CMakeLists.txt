set(BOARD_PORT_SOURCE "board_port.c")
set(BOARD_PROFILE_SOURCE "boards/echoear_2st/board_profile.c")
# ESP-IDF performs an early component-requirement expansion in a separate
# `cmake -P` process. Command-line cache variables from the top-level build
# are not forwarded to that process, whereas the profile wrapper's environment
# is. Restore the profile identity here before evaluating any profile-private
# dependency or source selection so both passes describe the same image.
if((NOT DEFINED MACLAW_PROFILE OR MACLAW_PROFILE STREQUAL "")
   AND DEFINED ENV{MACLAW_PROFILE})
    set(MACLAW_PROFILE "$ENV{MACLAW_PROFILE}")
endif()
if(CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD)
    # `compact_renderer.c` owns the common compact scene/session mechanics.
    # The selected profile owns all physical display, audio, input and
    # connectivity adapters below that renderer boundary.
    set(BOARD_PORT_SOURCE "compact_renderer.c")
    set(BOARD_PROFILE_SOURCE "boards/bread_compact/board_profile.c")
    # Kconfig is authoritative once configured; avoid stale profile-specific
    # component requirements from a reused build directory.
    set(MACLAW_PROFILE "bread-compact")
elseif(CONFIG_MACLAW_BOARD_FANGTANG_4G)
    # Both rectangular profiles compile the same board-neutral compact
    # renderer. Fangtang identity composition is a separate profile-private
    # source; it must not include Bread's board-port translation unit.
    set(BOARD_PORT_SOURCE "compact_renderer.c")
    set(BOARD_PROFILE_SOURCE "boards/fangtang_4g/board_profile.c")
    set(MACLAW_PROFILE "fangtang-4g")
elseif(CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C)
    # The shared round-scene implementation is parameterised for the two
    # circular boards, while controller/pin/codec selection stays below the
    # board-port boundary. This prevents the 466px CO5300 panel from being
    # treated as a reskinned rectangular Bread display.
    set(BOARD_PORT_SOURCE "board_port.c")
    set(BOARD_PROFILE_SOURCE "boards/waveshare_amoled_1_75c/board_profile.c")
    set(MACLAW_PROFILE "waveshare-amoled-1.75c")
elseif(CONFIG_MACLAW_BOARD_ECHOEAR_2ST)
    set(MACLAW_PROFILE "echoear-2st")
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
if(MACLAW_PROFILE STREQUAL "echoear-2st")
    # Component Manager resolves manifests before Kconfig exists.  The profile
    # wrapper therefore supplies this identity so the compiled main component
    # receives the selected controller's public headers as well.
    list(APPEND MACLAW_COMPONENT_REQUIRES espressif__esp_lcd_st77916)
elseif(MACLAW_PROFILE STREQUAL "fangtang-4g")
    list(APPEND MACLAW_COMPONENT_REQUIRES 78__esp_lcd_nv3023 78__esp-ml307
                                         78__uart-uhci)
elseif(MACLAW_PROFILE STREQUAL "waveshare-amoled-1.75c")
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
         "ml307_transport.cpp"
         "boards/fangtang_4g/fangtang_peripheral_adapter.c"
         "boards/fangtang_4g/fangtang_input_selector.c"
         "boards/fangtang_4g/fangtang_network_identity.c"
         "boards/fangtang_4g/fangtang_identity_art_renderer.c"
         "boards/fangtang_4g/fangtang_identity_composer.c"
         "boards/fangtang_4g/fangtang_identity_network_renderer.c"
         "boards/fangtang_4g/board_port_fangtang_4g.c")
endif()

set(MACLAW_EMBED_FILES)
if(CONFIG_MACLAW_BOARD_FANGTANG_4G)
    # A material sugar cube needs more tonal and crystal detail than the
    # procedural status renderer can carry at 188x164. Keep the physical
    # RGB565 artwork and its alpha mask isolated to the Fangtang image.
    list(APPEND MACLAW_EMBED_FILES "fangtang_sugar.rgb565" "fangtang_sugar.a8")
endif()
if(CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD)
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
    SRCS "main.c" "app_ui.c" "app_intent_service.c" "operation_context.c" "alarm_manager.c" "fall_detection_service.c" "sleep_schedule_service.c" "wake_deadline_service.c" "resource_pressure_service.c" "battery_policy_service.c" "persistence_service.c" "task_registry.c" "weather_cache_service.c" "meeting_recovery_service.c" "configuration_service.c" "device_tool_registry.c" "update_service.c" "provisioning_failure_injection.c" "mp3_player.c" "firmware_identity.c" "device_api.c" "audio_service.c" "display_service.c" "connectivity_service.c" "input_service.c" "lifecycle_service.c" "platform_audio.c" "platform_connectivity.c" "platform_display.c" "platform_input.c" "platform_lifecycle.c" "platform_power.c" "platform_resource.c" "platform_sensor.c" "platform_storage.c" "power_service.c" "power_lease_service.c" "${BOARD_PROFILE_SOURCE}" "${BOARD_PORT_SOURCE}" ${MACLAW_BOARD_SRCS}
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
