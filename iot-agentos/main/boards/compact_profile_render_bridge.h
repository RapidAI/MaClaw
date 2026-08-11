#pragma once

/*
 * Private render-primitive bridge from the shared compact renderer to a
 * selected product-identity composer.  It deliberately carries only
 * normalized raster operations: no LCD, DMA, FreeRTOS, GPIO or profile
 * controller object crosses this seam.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef struct {
    void *context;
    int panel_width;
    int panel_height;
    bool (*display_ready)(void *context);
    bool (*begin_frame)(void *context);
    void (*finish_frame)(void *context, bool composed);
    void (*fill_screen)(void *context, uint16_t background);
    uint16_t (*state_color)(void *context, const char *state);
    uint16_t (*color)(void *context, uint8_t red, uint8_t green, uint8_t blue);
    int (*text24_width)(void *context, const char *text, int max_glyphs);
    void (*draw_ascii)(void *context, int x, int y, const char *text,
                       uint16_t foreground, uint16_t background);
    void (*draw_text24)(void *context, int x, int y, const char *text,
                        uint16_t foreground, uint16_t background,
                        int max_glyphs);
    void (*draw_text24_centered)(void *context, int y, const char *text,
                                 uint16_t foreground, uint16_t background,
                                 int max_glyphs);
    bool (*draw_remote_pet)(void *context, uint16_t background);
    bool (*network_is_cellular)(void *context);
    uint16_t *(*allocate_bitmap)(void *context, size_t bytes);
    void (*free_bitmap)(void *context, void *bitmap);
    bool (*draw_bitmap)(void *context, int x, int y, int width, int height,
                        const uint16_t *pixels);
    void (*fill_rect)(void *context, int x, int y, int width, int height,
                      uint16_t fill);
} compact_profile_render_bridge_t;
