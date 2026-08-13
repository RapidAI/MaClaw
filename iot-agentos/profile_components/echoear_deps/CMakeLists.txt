# This source-free carrier must re-export the controller's public header.
# The round input source is compiled as part of `main` and includes the chosen
# profile adapter before Kconfig's board branch is available to the early
# component-graph pass.  Keep that board-private dependency reachable through
# the selected profile closure rather than adding it to every hardware image.
idf_component_register(REQUIRES espressif__esp_lcd_st77916)
