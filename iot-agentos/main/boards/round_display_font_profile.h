#pragma once
/*
 * Profile-private font contract for the shared round-scene renderer.
 *
 * A renderer can ask for a native raster and its packed fallback without
 * learning the selected panel, font binary, or glyph-cell size.  This is a
 * display implementation seam: it is intentionally not part of Device API
 * or Platform API.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef const uint32_t *(*round_display_font_native_rows_fn)(uint32_t codepoint);
typedef bool (*round_display_font_copy_packed_fn)(uint32_t codepoint,
                                                   uint8_t *bitmap,
                                                   size_t bitmap_size);

typedef struct {
    /* Native square raster used by the curved information ring. */
    uint8_t curve_cell;
    round_display_font_native_rows_fn native_rows;
    round_display_font_copy_packed_fn copy_packed;
} round_display_font_profile_t;
