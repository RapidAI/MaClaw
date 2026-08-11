#pragma once

/*
 * Fangtang product-page composer contract.
 *
 * This private interface separates the square-panel product composition from
 * the transitional compact renderer.  Callbacks act only on the already
 * selected frame; frame/lifecycle decisions, remote-pet ownership and all
 * state facts remain with the caller.
 */

#include <stdbool.h>
#include <stdint.h>

#include "../compact_profile_identity.h"
#include "fangtang_identity_art_renderer.h"
#include "fangtang_identity_network_renderer.h"

typedef struct {
    void *context;
    int panel_width;
    bool (*display_ready)(void *context);
    bool (*begin_frame)(void *context);
    void (*finish_frame)(void *context, bool composed);
    void (*fill_screen)(void *context, uint16_t color);
    uint16_t (*state_color)(void *context, const char *state);
    uint16_t (*color)(void *context, uint8_t red, uint8_t green, uint8_t blue);
    int (*text24_width)(void *context, const char *text, int max_glyphs);
    void (*draw_ascii)(void *context, int x, int y, const char *text,
                       uint16_t foreground, uint16_t background);
    void (*draw_text24)(void *context, int x, int y, const char *text,
                        uint16_t foreground, uint16_t background, int max_glyphs);
    void (*draw_text24_centered)(void *context, int y, const char *text,
                                 uint16_t foreground, uint16_t background,
                                 int max_glyphs);
    bool (*draw_remote_pet)(void *context, uint16_t background);
    bool (*network_is_cellular)(void *context);
    fangtang_identity_raster_t art_raster;
    fangtang_network_raster_t network_raster;
    bool (*draw_sugar)(void *context, const fangtang_identity_raster_t *raster,
                       int x, int y, int scale_percent, uint16_t background);
} fangtang_identity_composer_t;

bool fangtang_identity_compose_startup(const fangtang_identity_composer_t *composer);
bool fangtang_identity_compose_state(const fangtang_identity_composer_t *composer,
                                     const compact_profile_identity_state_t *identity,
                                     bool ambient, uint16_t background);
bool fangtang_identity_compose_status(const fangtang_identity_composer_t *composer,
                                      const compact_profile_identity_state_t *identity,
                                      const char *title, const char *line,
                                      uint16_t background);
