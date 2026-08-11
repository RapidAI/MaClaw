#pragma once

/*
 * Shared raster helpers for the circular renderer.
 *
 * The selected profile supplies its native curved-text raster through
 * round_profile_font().  This file deliberately contains no board selection:
 * a new round panel adds a profile-private font adapter, not another renderer
 * conditional.
 */

#include <stdbool.h>
#include <stdint.h>
#include <string.h>

#include "font_cjk24.h"
#include "round_profile_adapter.h"

#define ROUND_DISPLAY_FONT_24_CELL 24
#define ROUND_DISPLAY_FONT_24_BYTES 72
/* Current round panels expose at most a 32x32 native CJK raster. */
#define ROUND_DISPLAY_FONT_MAX_PACKED_BYTES 128

static inline const uint32_t *round_display_font_cjk24_rows(uint32_t codepoint) {
    for (size_t i = 0; i < sizeof(s_maclaw_cjk24) / sizeof(s_maclaw_cjk24[0]); ++i) {
        if (s_maclaw_cjk24[i].codepoint == codepoint) return s_maclaw_cjk24[i].rows;
    }
    return NULL;
}

static inline unsigned round_display_font_curve_cell(void) {
    const round_display_font_profile_t *profile = round_profile_font();
    return profile && profile->curve_cell ? profile->curve_cell : ROUND_DISPLAY_FONT_24_CELL;
}

/* Non-curved scene text retains the established 24-dot compositor.  Its
 * selected profile may provide a larger packed fallback; area-resampling
 * avoids a second complete CJK binary in constrained application images. */
static inline bool round_display_font_copy_cjk24(uint32_t codepoint,
                                                  uint8_t bitmap[ROUND_DISPLAY_FONT_24_BYTES]) {
    const round_display_font_profile_t *profile = round_profile_font();
    if (!profile || !profile->copy_packed) return false;
    const unsigned source_cell = round_display_font_curve_cell();
    const unsigned source_row_bytes = (source_cell + 7u) / 8u;
    const size_t source_bytes = (size_t)source_cell * source_row_bytes;
    if (source_cell < ROUND_DISPLAY_FONT_24_CELL ||
        source_bytes > ROUND_DISPLAY_FONT_MAX_PACKED_BYTES) {
        return false;
    }
    uint8_t source[ROUND_DISPLAY_FONT_MAX_PACKED_BYTES];
    if (!profile->copy_packed(codepoint, source, sizeof(source))) return false;
    memset(bitmap, 0, ROUND_DISPLAY_FONT_24_BYTES);
    for (int row = 0; row < ROUND_DISPLAY_FONT_24_CELL; ++row) {
        const int y0 = row * (int)source_cell / ROUND_DISPLAY_FONT_24_CELL;
        const int y1 = (row + 1) * (int)source_cell / ROUND_DISPLAY_FONT_24_CELL;
        for (int col = 0; col < ROUND_DISPLAY_FONT_24_CELL; ++col) {
            const int x0 = col * (int)source_cell / ROUND_DISPLAY_FONT_24_CELL;
            const int x1 = (col + 1) * (int)source_cell / ROUND_DISPLAY_FONT_24_CELL;
            bool set = false;
            for (int sy = y0; sy < y1 && !set; ++sy) {
                for (int sx = x0; sx < x1; ++sx) {
                    if ((source[sy * (int)source_row_bytes + sx / 8] &
                         (1u << (7 - (sx % 8)))) != 0) {
                        set = true;
                        break;
                    }
                }
            }
            if (set) bitmap[row * 3 + col / 8] |= 1u << (7 - (col % 8));
        }
    }
    return true;
}

typedef enum {
    ROUND_DISPLAY_CURVE_GLYPH_NONE,
    ROUND_DISPLAY_CURVE_GLYPH_NATIVE,
    ROUND_DISPLAY_CURVE_GLYPH_PACKED,
    ROUND_DISPLAY_CURVE_GLYPH_DYNAMIC24,
} round_display_curve_glyph_source_t;

typedef struct {
    const uint32_t *rows;
    const uint8_t *bitmap;
    uint8_t fallback[ROUND_DISPLAY_FONT_MAX_PACKED_BYTES];
    uint8_t cell;
    round_display_curve_glyph_source_t source;
} round_display_curve_glyph_t;

/* Prepare one glyph once per layout cell.  The renderer then rasterizes it
 * without knowing whether the profile supplied a 24/32-dot table, a packed
 * fallback, or a Hub-delivered 24-dot glyph. */
static inline void round_display_font_prepare_curve_glyph(
    uint32_t codepoint, const uint8_t *dynamic24, round_display_curve_glyph_t *out) {
    if (!out) return;
    memset(out, 0, sizeof(*out));
    const round_display_font_profile_t *profile = round_profile_font();
    out->cell = (uint8_t)round_display_font_curve_cell();
    if (!profile || out->cell < ROUND_DISPLAY_FONT_24_CELL ||
        ((size_t)out->cell * ((out->cell + 7u) / 8u) > sizeof(out->fallback))) {
        out->cell = ROUND_DISPLAY_FONT_24_CELL;
        return;
    }
    if (profile->native_rows) out->rows = profile->native_rows(codepoint);
    if (out->rows) {
        out->source = ROUND_DISPLAY_CURVE_GLYPH_NATIVE;
    } else if (profile->copy_packed &&
               profile->copy_packed(codepoint, out->fallback, sizeof(out->fallback))) {
        out->bitmap = out->fallback;
        out->source = ROUND_DISPLAY_CURVE_GLYPH_PACKED;
    } else if (dynamic24) {
        out->bitmap = dynamic24;
        out->source = ROUND_DISPLAY_CURVE_GLYPH_DYNAMIC24;
    }
}

static inline bool round_display_font_curve_glyph_pixel(
    const round_display_curve_glyph_t *glyph, int row, int col, bool ascii_pixel) {
    if (!glyph || row < 0 || col < 0 || row >= glyph->cell || col >= glyph->cell) {
        return ascii_pixel;
    }
    if (glyph->source == ROUND_DISPLAY_CURVE_GLYPH_NATIVE) {
        return (glyph->rows[row] & (1u << (glyph->cell - 1 - col))) != 0;
    }
    if (glyph->source == ROUND_DISPLAY_CURVE_GLYPH_PACKED) {
        const int row_bytes = (glyph->cell + 7) / 8;
        return (glyph->bitmap[row * row_bytes + col / 8] &
                (1u << (7 - (col % 8)))) != 0;
    }
    if (glyph->source == ROUND_DISPLAY_CURVE_GLYPH_DYNAMIC24) {
        const int y0 = row * ROUND_DISPLAY_FONT_24_CELL / glyph->cell;
        int y1 = (row + 1) * ROUND_DISPLAY_FONT_24_CELL / glyph->cell;
        const int x0 = col * ROUND_DISPLAY_FONT_24_CELL / glyph->cell;
        int x1 = (col + 1) * ROUND_DISPLAY_FONT_24_CELL / glyph->cell;
        if (y1 <= y0) y1 = y0 + 1;
        if (x1 <= x0) x1 = x0 + 1;
        for (int sy = y0; sy < y1; ++sy) {
            for (int sx = x0; sx < x1; ++sx) {
                if (glyph->bitmap[sy * 3 + sx / 8] & (1u << (7 - (sx % 8)))) return true;
            }
        }
        return false;
    }
    return ascii_pixel;
}
