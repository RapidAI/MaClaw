idf_component_register(
    SRCS "hardware_test.c"
    INCLUDE_DIRS "."
    REQUIRES esp_driver_gpio esp_driver_i2s esp_psram spi_flash
)
