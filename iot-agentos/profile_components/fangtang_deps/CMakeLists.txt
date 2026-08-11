# Source-free profile dependency carrier; selected only by the Fangtang build
# wrapper before Component Manager resolution.  esp-ml307 currently exports
# at_uart.h while declaring uart-uhci private upstream.  Make the directly
# consumed public header visible to `main` without modifying a generated
# managed-component checkout.
# `main` includes both the NV3023 display API and esp-ml307's public
# `at_uart.h` through its Fangtang-only board adapter.  ESP-IDF's preliminary
# requirement pass runs before main can reliably read the selected Kconfig
# profile, therefore this selected source-free carrier must export the three
# public dependency include trees itself.  This keeps those drivers out of
# all other profile closures.
idf_component_register(
    REQUIRES 78__esp_lcd_nv3023 78__esp-ml307 78__uart-uhci
)
