idf_component_register(
    SRCS "main.c"
    INCLUDE_DIRS "."
    REQUIRES esp_lcd_nv3023
    EMBED_FILES "doraemon_frame0.rgb565a8"
)
