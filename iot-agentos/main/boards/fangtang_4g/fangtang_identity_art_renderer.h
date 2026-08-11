#pragma once

/*
 * Fangtang-private product-art raster contract.
 *
 * Shared compact scene code keeps ownership of composition-frame lifetime,
 * LCD serialization and presentation.  The Fangtang product renderer receives
 * only bounded temporary-bitmap and rectangular-present callbacks, so its
 * sugar/cube art cannot acquire a panel, submit an independent frame, or
 * observe shared scene globals.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef struct {
    void *context;
    int panel_width;
    int panel_height;
    uint16_t *(*allocate_bitmap)(void *context, size_t bytes);
    void (*free_bitmap)(void *context, void *bitmap);
    bool (*draw_bitmap)(void *context, int x, int y, int width, int height,
                        const uint16_t *pixels);
    void (*fill_rect)(void *context, int x, int y, int width, int height,
                      uint16_t color);
} fangtang_identity_raster_t;

void fangtang_identity_draw_cube(const fangtang_identity_raster_t *raster,
                                 const char *state, unsigned animation_phase,
                                 int offset_x, int offset_y, int scale_percent,
                                 uint16_t background);

bool fangtang_identity_draw_sugar(const fangtang_identity_raster_t *raster,
                                  const uint8_t *rgb565, size_t rgb565_bytes,
                                  const uint8_t *alpha, size_t alpha_bytes,
                                  int offset_x, int offset_y, int scale_percent,
                                  uint16_t background);

void fangtang_identity_draw_activity(const fangtang_identity_raster_t *raster,
                                     const char *state, unsigned animation_phase,
                                     int center_x, int center_y,
                                     uint16_t background);
