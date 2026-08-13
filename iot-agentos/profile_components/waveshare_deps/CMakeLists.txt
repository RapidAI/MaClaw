# Source-free profile dependency carrier; selected only by the Waveshare build
# wrapper before Component Manager resolution. `main` includes the public
# CO5300 panel and CST9217 touch APIs solely through Waveshare-private board
# adapters. Export their public include paths at the selected carrier seam so
# the preliminary component-requirement pass and the final component graph
# agree; do not add these dependencies to other board profiles.
idf_component_register(
    REQUIRES espressif__esp_lcd_co5300
             espressif__esp_lcd_touch
             waveshare__esp_lcd_touch_cst9217
)
