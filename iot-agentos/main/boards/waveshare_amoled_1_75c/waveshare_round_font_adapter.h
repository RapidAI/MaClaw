#pragma once

#include <string.h>

#include "../round_display_font_profile.h"
#include "font_cjk32.h"

#define WAVESHARE_ROUND_FONT_CELL 32u
#define WAVESHARE_ROUND_FONT_BYTES 128u

extern const uint8_t _binary_cjk32_cjk_bin_start[];
extern const uint8_t _binary_cjk32_cjk_bin_end[];

static const uint32_t *waveshare_round_font_native_rows(uint32_t codepoint) {
    for (size_t i = 0; i < sizeof(s_maclaw_cjk32) / sizeof(s_maclaw_cjk32[0]); ++i) {
        if (s_maclaw_cjk32[i].codepoint == codepoint) return s_maclaw_cjk32[i].rows;
    }
    return NULL;
}

static bool waveshare_round_font_copy_packed(uint32_t codepoint,
                                             uint8_t *bitmap,
                                             size_t bitmap_size) {
    if (!bitmap || bitmap_size < WAVESHARE_ROUND_FONT_BYTES ||
        codepoint < 0x4E00 || codepoint >= 0xA000) {
        return false;
    }
    const size_t offset = (size_t)(codepoint - 0x4E00) * WAVESHARE_ROUND_FONT_BYTES;
    const size_t available = (size_t)(_binary_cjk32_cjk_bin_end -
                                      _binary_cjk32_cjk_bin_start);
    if (offset + WAVESHARE_ROUND_FONT_BYTES > available) return false;
    memcpy(bitmap, _binary_cjk32_cjk_bin_start + offset, WAVESHARE_ROUND_FONT_BYTES);
    return true;
}

static inline const round_display_font_profile_t *waveshare_round_font_profile(void) {
    static const round_display_font_profile_t profile = {
        .curve_cell = WAVESHARE_ROUND_FONT_CELL,
        .native_rows = waveshare_round_font_native_rows,
        .copy_packed = waveshare_round_font_copy_packed,
    };
    return &profile;
}
