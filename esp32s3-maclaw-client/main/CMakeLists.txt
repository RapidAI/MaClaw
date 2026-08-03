idf_component_register(
    SRCS "main.c" "board_port.c"
    INCLUDE_DIRS "."
    REQUIRES esp_event esp_wifi wpa_supplicant esp_http_client esp-tls esp_timer esp_netif nvs_flash cjson esp_lcd esp_driver_gpio esp_driver_spi esp_driver_i2c esp_driver_i2s esp_http_server spiffs mbedtls espressif__esp_lcd_st77916 espressif__qrcode espressif__esp-sr
)
