idf_component_register(SRCS "esp_lcd_st77916.c" "esp_lcd_st77916_spi.c" "esp_lcd_st77916_mipi.c"
                       INCLUDE_DIRS "include"
                       PRIV_INCLUDE_DIRS "priv_include"
                       REQUIRES "esp_lcd" "driver")

# Vendored from espressif/esp_lcd_st77916. The managed-component version call
# is only bookkeeping for the component manager; a local component defines the
# version macros the sources log directly.
target_compile_definitions(${COMPONENT_LIB} PUBLIC
    ESP_LCD_ST77916_VER_MAJOR=1
    ESP_LCD_ST77916_VER_MINOR=0
    ESP_LCD_ST77916_VER_PATCH=0)
