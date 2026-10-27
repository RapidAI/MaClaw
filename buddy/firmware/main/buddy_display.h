#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"

/** EchoEar-2ST ST77916 QSPI panel + backlight + a PSRAM framebuffer. */
esp_err_t buddy_display_init(void);

uint16_t *buddy_display_framebuffer(void);
int buddy_display_width(void);
int buddy_display_height(void);

/** Pushes full-width rows [y0, y1) from the framebuffer to the panel. */
void buddy_display_flush_rows(int y0, int y1);

/** Marks user/host activity; keeps the backlight on. */
void buddy_display_note_activity(void);
/** Applies the idle backlight policy (dim after 60s, off after 120s). */
void buddy_display_tick(void);
bool buddy_display_awake(void);
void buddy_display_wake(void);

/** Converts logical RGB888 to the byte order ST77916 QSPI expects. */
uint16_t buddy_color(uint8_t r, uint8_t g, uint8_t b);
uint16_t buddy_color_swap(uint16_t rgb565);
