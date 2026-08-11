/* Fangtang-only standby radio-mark implementation.
 *
 * This source deliberately has no dependency on the compact board-port
 * transition unit.  Its caller supplies the sole profile-private drawing
 * primitive for an already-open shared composition frame.
 */

#include "fangtang_identity_network_renderer.h"

#include <stddef.h>

static const uint8_t *fangtang_network_glyph5x7(char character) {
    /* Columns are low-bit first, matching the established compact 5x7 face. */
    static const uint8_t glyphs[26][5] = {
        {0x7E,0x11,0x11,0x11,0x7E}, {0x7F,0x49,0x49,0x49,0x36},
        {0x3E,0x41,0x41,0x41,0x22}, {0x7F,0x41,0x41,0x22,0x1C},
        {0x7F,0x49,0x49,0x49,0x41}, {0x7F,0x09,0x09,0x09,0x01},
        {0x3E,0x41,0x49,0x49,0x7A}, {0x7F,0x08,0x08,0x08,0x7F},
        {0,0x41,0x7F,0x41,0},       {0x20,0x40,0x41,0x3F,0x01},
        {0x7F,0x08,0x14,0x22,0x41}, {0x7F,0x40,0x40,0x40,0x40},
        {0x7F,0x02,0x0C,0x02,0x7F}, {0x7F,0x04,0x08,0x10,0x7F},
        {0x3E,0x41,0x41,0x41,0x3E}, {0x7F,0x09,0x09,0x09,0x06},
        {0x3E,0x41,0x51,0x21,0x5E}, {0x7F,0x09,0x19,0x29,0x46},
        {0x46,0x49,0x49,0x49,0x31}, {0x01,0x01,0x7F,0x01,0x01},
        {0x3F,0x40,0x40,0x40,0x3F}, {0x1F,0x20,0x40,0x20,0x1F},
        {0x3F,0x40,0x38,0x40,0x3F}, {0x63,0x14,0x08,0x14,0x63},
        {0x03,0x04,0x78,0x04,0x03}, {0x61,0x51,0x49,0x45,0x43},
    };
    static const uint8_t digits[10][5] = {
        {0x3E,0x51,0x49,0x45,0x3E}, {0,0x42,0x7F,0x40,0},
        {0x42,0x61,0x51,0x49,0x46}, {0x21,0x41,0x45,0x4B,0x31},
        {0x18,0x14,0x12,0x7F,0x10}, {0x27,0x45,0x45,0x45,0x39},
        {0x3C,0x4A,0x49,0x49,0x30}, {0x01,0x71,0x09,0x05,0x03},
        {0x36,0x49,0x49,0x49,0x36}, {0x06,0x49,0x49,0x29,0x1E},
    };
    if (character >= '0' && character <= '9') return digits[(unsigned)(character - '0')];
    if (character < 'A' || character > 'Z') return NULL;
    return glyphs[(unsigned)(character - 'A')];
}

static void fangtang_network_fill(const fangtang_network_raster_t *raster,
                                  int x, int y, int width, int height,
                                  uint16_t color) {
    if (!raster || !raster->fill_rect || width <= 0 || height <= 0) return;
    raster->fill_rect(raster->context, x, y, width, height, color);
}

static int fangtang_network_label_width(bool cellular) {
    const char *label = cellular ? "4G" : "WIFI";
    const int scale = cellular ? 1 : 2;
    size_t length = 0;
    while (label[length]) ++length;
    return ((int)length * 6 - 1) * scale;
}

static void fangtang_network_draw_label(const fangtang_network_raster_t *raster,
                                        int x, int y, bool cellular,
                                        uint16_t foreground, uint16_t background) {
    const char *label = cellular ? "4G" : "WIFI";
    const int scale = cellular ? 1 : 2;
    const int width = fangtang_network_label_width(cellular);
    const int height = 7 * scale;
    fangtang_network_fill(raster, x - 2, y - 2, width + 4, height + 4, background);
    for (size_t ch = 0; label[ch]; ++ch) {
        const uint8_t *glyph = fangtang_network_glyph5x7(label[ch]);
        if (!glyph) continue;
        for (int gx = 0; gx < 5; ++gx) {
            for (int gy = 0; gy < 7; ++gy) {
                if (!(glyph[gx] & (1u << gy))) continue;
                fangtang_network_fill(raster, x + (int)ch * 6 * scale + gx * scale,
                                      y + gy * scale, scale, scale, foreground);
            }
        }
    }
}

int fangtang_network_badge_width(bool cellular) {
    return cellular ? 38 : 68;
}

void fangtang_network_draw_badge(const fangtang_network_raster_t *raster,
                                 int x, int y, bool cellular,
                                 uint16_t signal_foreground,
                                 uint16_t label_foreground,
                                 uint16_t background) {
    if (!raster || !raster->fill_rect) return;
    if (cellular) {
        fangtang_network_fill(raster, x, y + 11, 2, 4, signal_foreground);
        fangtang_network_fill(raster, x + 4, y + 8, 2, 7, signal_foreground);
        fangtang_network_fill(raster, x + 8, y + 5, 2, 10, signal_foreground);
        fangtang_network_draw_label(raster, x + 14, y, true, label_foreground, background);
        return;
    }
    for (int local_y = 3; local_y <= 13; ++local_y) {
        for (int local_x = 0; local_x < 18; ++local_x) {
            const float dx = (float)local_x - 8.5f;
            const float dy = (float)local_y - 15.0f;
            const float radius_squared = dx * dx + dy * dy;
            const bool outer = radius_squared > 64.0f && radius_squared < 90.25f;
            const bool middle = radius_squared > 25.0f && radius_squared < 42.25f;
            const bool inner = radius_squared > 4.41f && radius_squared < 11.56f;
            if (dy < 0.0f && (outer || middle || inner)) {
                fangtang_network_fill(raster, x + local_x, y + local_y, 1, 1,
                                      signal_foreground);
            }
        }
    }
    fangtang_network_fill(raster, x + 8, y + 14, 3, 2, signal_foreground);
    fangtang_network_draw_label(raster, x + 22, y + 2, false, label_foreground, background);
}
