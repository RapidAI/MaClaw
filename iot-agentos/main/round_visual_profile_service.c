#include "round_visual_profile_service.h"

#include <string.h>

#include "freertos/FreeRTOS.h"

#include "board_port.h"
#include "device_api.h"
#include "font_cjk24.h"
#include "boards/round_display_font_profile.h"

/* This is the only source owner that selects a round product's visual
 * profile.  The selected adapter contains layout/font data only; no panel,
 * GPIO, codec, DMA or task object is exposed through this service. */
#include "boards/round_visual_profile_adapter.h"

static const round_display_font_profile_t *round_visual_font_profile(void) {
    return round_visual_adapter_font();
}

const round_display_layout_t *round_visual_profile_display_layout(void) {
    return round_visual_adapter_display_layout();
}

const round_recording_layout_t *round_visual_profile_recording_layout(void) {
    return round_visual_adapter_recording_layout();
}

const round_upload_layout_t *round_visual_profile_upload_layout(void) {
    return round_visual_adapter_upload_layout();
}

const round_alarm_layout_t *round_visual_profile_alarm_layout(void) {
    return round_visual_adapter_alarm_layout();
}

const round_message_layout_t *round_visual_profile_message_layout(void) {
    return round_visual_adapter_message_layout();
}

const round_response_image_layout_t *round_visual_profile_response_image_layout(void) {
    return round_visual_adapter_response_image_layout();
}

const round_qrcode_layout_t *round_visual_profile_qrcode_layout(void) {
    return round_visual_adapter_qrcode_layout();
}

const uint32_t *round_visual_profile_cjk24_rows(uint32_t codepoint) {
    for (size_t i = 0; i < sizeof(s_maclaw_cjk24) / sizeof(s_maclaw_cjk24[0]); ++i) {
        if (s_maclaw_cjk24[i].codepoint == codepoint) return s_maclaw_cjk24[i].rows;
    }
    return NULL;
}

unsigned round_visual_profile_curve_glyph_cell(void) {
    const round_display_font_profile_t *profile = round_visual_font_profile();
    return profile && profile->curve_cell ? profile->curve_cell
                                          : ROUND_VISUAL_PROFILE_CJK24_CELL;
}

bool round_visual_profile_copy_cjk24(
    uint32_t codepoint, uint8_t bitmap[ROUND_VISUAL_PROFILE_CJK24_BYTES]) {
    const round_display_font_profile_t *profile = round_visual_font_profile();
    if (!profile || !profile->copy_packed || !bitmap) return false;
    const unsigned source_cell = round_visual_profile_curve_glyph_cell();
    const unsigned source_row_bytes = (source_cell + 7u) / 8u;
    const size_t source_bytes = (size_t)source_cell * source_row_bytes;
    if (source_cell < ROUND_VISUAL_PROFILE_CJK24_CELL ||
        source_bytes > ROUND_VISUAL_PROFILE_MAX_PACKED_GLYPH_BYTES) {
        return false;
    }
    uint8_t source[ROUND_VISUAL_PROFILE_MAX_PACKED_GLYPH_BYTES];
    if (!profile->copy_packed(codepoint, source, sizeof(source))) return false;
    memset(bitmap, 0, ROUND_VISUAL_PROFILE_CJK24_BYTES);
    for (int row = 0; row < ROUND_VISUAL_PROFILE_CJK24_CELL; ++row) {
        const int y0 = row * (int)source_cell / ROUND_VISUAL_PROFILE_CJK24_CELL;
        const int y1 = (row + 1) * (int)source_cell / ROUND_VISUAL_PROFILE_CJK24_CELL;
        for (int col = 0; col < ROUND_VISUAL_PROFILE_CJK24_CELL; ++col) {
            const int x0 = col * (int)source_cell / ROUND_VISUAL_PROFILE_CJK24_CELL;
            const int x1 = (col + 1) * (int)source_cell / ROUND_VISUAL_PROFILE_CJK24_CELL;
            bool set = false;
            for (int sy = y0; sy < y1 && !set; ++sy) {
                for (int sx = x0; sx < x1; ++sx) {
                    if (source[sy * (int)source_row_bytes + sx / 8] &
                        (1u << (7 - (sx % 8)))) {
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

void round_visual_profile_prepare_curve_glyph(
    uint32_t codepoint, const uint8_t *dynamic24, round_visual_curve_glyph_t *out) {
    if (!out) return;
    memset(out, 0, sizeof(*out));
    const round_display_font_profile_t *profile = round_visual_font_profile();
    out->cell = (uint8_t)round_visual_profile_curve_glyph_cell();
    if (!profile || out->cell < ROUND_VISUAL_PROFILE_CJK24_CELL ||
        ((size_t)out->cell * ((out->cell + 7u) / 8u) > sizeof(out->fallback))) {
        out->cell = ROUND_VISUAL_PROFILE_CJK24_CELL;
        return;
    }
    if (profile->native_rows) out->rows = profile->native_rows(codepoint);
    if (out->rows) {
        out->source = ROUND_VISUAL_GLYPH_NATIVE;
    } else if (profile->copy_packed &&
               profile->copy_packed(codepoint, out->fallback, sizeof(out->fallback))) {
        out->bitmap = out->fallback;
        out->source = ROUND_VISUAL_GLYPH_PACKED;
    } else if (dynamic24) {
        out->bitmap = dynamic24;
        out->source = ROUND_VISUAL_GLYPH_DYNAMIC24;
    }
}

bool round_visual_profile_curve_glyph_pixel(const round_visual_curve_glyph_t *glyph,
                                            int row, int col, bool ascii_pixel) {
    if (!glyph || row < 0 || col < 0 || row >= glyph->cell || col >= glyph->cell) {
        return ascii_pixel;
    }
    if (glyph->source == ROUND_VISUAL_GLYPH_NATIVE) {
        return (glyph->rows[row] & (1u << (glyph->cell - 1 - col))) != 0;
    }
    if (glyph->source == ROUND_VISUAL_GLYPH_PACKED) {
        const int row_bytes = (glyph->cell + 7) / 8;
        return (glyph->bitmap[row * row_bytes + col / 8] &
                (1u << (7 - (col % 8)))) != 0;
    }
    if (glyph->source == ROUND_VISUAL_GLYPH_DYNAMIC24) {
        const int y0 = row * ROUND_VISUAL_PROFILE_CJK24_CELL / glyph->cell;
        int y1 = (row + 1) * ROUND_VISUAL_PROFILE_CJK24_CELL / glyph->cell;
        const int x0 = col * ROUND_VISUAL_PROFILE_CJK24_CELL / glyph->cell;
        int x1 = (col + 1) * ROUND_VISUAL_PROFILE_CJK24_CELL / glyph->cell;
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
