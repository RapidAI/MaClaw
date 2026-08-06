set(BOARD_PORT_SOURCE "board_port.c")
if(CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD OR CONFIG_MACLAW_BOARD_FANGTANG_4G)
    set(BOARD_PORT_SOURCE "board_port_bread_compact.c")
endif()

# Keep the cellular implementation entirely out of the established EchoEar and
# Bread Compact images.  The source has its own Kconfig guards as well; this
# conditional also prevents the modem library from consuming their flash.
set(MACLAW_COMPONENT_REQUIRES
    esp_app_format esp_event esp_wifi wpa_supplicant esp_http_client esp-tls
    esp_timer esp_netif nvs_flash cjson esp_lcd esp_driver_gpio esp_driver_spi
    esp_driver_i2c esp_driver_i2s esp_driver_ledc esp_adc esp_http_server spiffs
    mbedtls spi_flash esp_psram espressif__esp_lcd_st77916 78__esp_lcd_nv3023 espressif__qrcode
    espressif__esp-sr espressif__esp_audio_codec)
if(CONFIG_MACLAW_BOARD_FANGTANG_4G)
    list(APPEND MACLAW_COMPONENT_REQUIRES 78__esp-ml307)
    list(APPEND MACLAW_BOARD_SRCS "ml307_transport.cpp")
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

idf_component_register(
    SRCS "main.c" "app_ui.c" "alarm_manager.c" "mp3_player.c" "firmware_identity.c" "${BOARD_PORT_SOURCE}" ${MACLAW_BOARD_SRCS}
    INCLUDE_DIRS "."
    EMBED_FILES ${MACLAW_EMBED_FILES}
    REQUIRES ${MACLAW_COMPONENT_REQUIRES}
)

# Both display boards must render arbitrary Chinese replies without depending
# on glyphs downloaded at runtime. Embed the packed 24-dot CJK fallback in the
# application image so the shared SPIFFS partition remains available for
# recoverable meeting recordings.
target_add_binary_data(${COMPONENT_LIB} "cjk24_cjk.bin" BINARY)
