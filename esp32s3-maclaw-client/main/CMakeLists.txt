set(BOARD_PORT_SOURCE "board_port.c")
if(CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD)
    set(BOARD_PORT_SOURCE "board_port_bread_compact.c")
endif()

idf_component_register(
    SRCS "main.c" "app_ui.c" "${BOARD_PORT_SOURCE}"
    INCLUDE_DIRS "."
    REQUIRES esp_event esp_wifi wpa_supplicant esp_http_client esp-tls esp_timer esp_netif nvs_flash cjson esp_lcd esp_driver_gpio esp_driver_spi esp_driver_i2c esp_driver_i2s esp_driver_ledc esp_adc esp_http_server spiffs mbedtls espressif__esp_lcd_st77916 espressif__qrcode espressif__esp-sr
)

# Both display boards must render arbitrary Chinese replies without depending
# on glyphs downloaded at runtime. Embed the packed 24-dot CJK fallback in the
# application image so the shared SPIFFS partition remains available for
# recoverable meeting recordings.
target_add_binary_data(${COMPONENT_LIB} "cjk24_cjk.bin" BINARY)
