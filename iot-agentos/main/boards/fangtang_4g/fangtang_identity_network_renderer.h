#pragma once

/*
 * Fangtang-only standby radio-mark raster contract.
 *
 * The compact transition renderer owns the active framebuffer, LCD lock and
 * scene admission.  This profile-private renderer owns only the product
 * geometry for the 4G/WIFI mark and receives one bounded rectangle primitive
 * for its already-open composition frame.  It therefore cannot present a
 * frame, allocate display memory, or influence shared scene state.
 */

#include <stdbool.h>
#include <stdint.h>

typedef struct {
    void *context;
    void (*fill_rect)(void *context, int x, int y, int width, int height,
                      uint16_t color);
} fangtang_network_raster_t;

/* The returned width matches the existing header's reserved transport field. */
int fangtang_network_badge_width(bool cellular);

/* Draw the complete profile-specific radio mark at its established origin. */
void fangtang_network_draw_badge(const fangtang_network_raster_t *raster,
                                 int x, int y, bool cellular,
                                 uint16_t signal_foreground,
                                 uint16_t label_foreground,
                                 uint16_t background);
