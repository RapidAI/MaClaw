#include "buddy_font.h"

#include <stdatomic.h>
#include <string.h>

#include "buddy_font_ext_index.h"

// Packed font binaries embedded by target_add_binary_data().
extern const uint8_t _binary_cjk24_cjk_bin_start[];
extern const uint8_t _binary_cjk24_cjk_bin_end[];
extern const uint8_t _binary_cjk24_ext_bin_start[];
extern const uint8_t _binary_cjk24_ext_bin_end[];

#define GLYPH_BYTES 72  // 24 rows * 3 bytes, MSB first

static atomic_int s_missing;

int buddy_font_missing_total(bool reset) {
    if (reset) {
        return atomic_exchange(&s_missing, 0);
    }
    return atomic_load(&s_missing);
}

static int utf8_decode(const char *s, uint32_t *out) {
    const uint8_t *p = (const uint8_t *)s;
    if (p[0] < 0x80) {
        *out = p[0];
        return 1;
    }
    if ((p[0] & 0xE0) == 0xC0 && (p[1] & 0xC0) == 0x80) {
        *out = ((uint32_t)(p[0] & 0x1F) << 6) | (p[1] & 0x3F);
        return 2;
    }
    if ((p[0] & 0xF0) == 0xE0 && (p[1] & 0xC0) == 0x80 && (p[2] & 0xC0) == 0x80) {
        *out = ((uint32_t)(p[0] & 0x0F) << 12) | ((uint32_t)(p[1] & 0x3F) << 6) |
               (p[2] & 0x3F);
        return 3;
    }
    if ((p[0] & 0xF8) == 0xF0 && (p[1] & 0xC0) == 0x80 && (p[2] & 0xC0) == 0x80 &&
        (p[3] & 0xC0) == 0x80) {
        *out = ((uint32_t)(p[0] & 0x07) << 18) | ((uint32_t)(p[1] & 0x3F) << 12) |
               ((uint32_t)(p[2] & 0x3F) << 6) | (p[3] & 0x3F);
        return 4;
    }
    // Invalid byte: consume one so a malformed title cannot loop forever.
    *out = 0xFFFD;
    return 1;
}

static bool ext_index_of(uint32_t cp, uint32_t *index) {
    int lo = 0;
    int hi = BUDDY_EXT_GLYPH_COUNT - 1;
    while (lo <= hi) {
        int mid = (lo + hi) / 2;
        uint32_t v = s_buddy_ext_codepoints[mid];
        if (v == cp) {
            *index = (uint32_t)mid;
            return true;
        }
        if (v < cp) {
            lo = mid + 1;
        } else {
            hi = mid - 1;
        }
    }
    return false;
}

/** Resolves a codepoint to a 72-byte packed bitmap, or NULL when unsupported. */
static const uint8_t *glyph24(uint32_t cp) {
    if (cp >= 0x4E00 && cp <= 0x9FFF) {
        const size_t offset = (size_t)(cp - 0x4E00) * GLYPH_BYTES;
        const size_t available = (size_t)(_binary_cjk24_cjk_bin_end - _binary_cjk24_cjk_bin_start);
        if (offset + GLYPH_BYTES <= available) {
            return _binary_cjk24_cjk_bin_start + offset;
        }
        return NULL;
    }
    uint32_t index = 0;
    if (!ext_index_of(cp, &index)) return NULL;
    const size_t offset = (size_t)index * GLYPH_BYTES;
    const size_t available = (size_t)(_binary_cjk24_ext_bin_end - _binary_cjk24_ext_bin_start);
    if (offset + GLYPH_BYTES > available) return NULL;
    return _binary_cjk24_ext_bin_start + offset;
}

static bool glyph24_pixel(const uint8_t *bitmap, uint32_t cp, int row, int col) {
    // The host rasterizer places the ideographic comma/period at the cell top;
    // simplified-Chinese typography expects them on the baseline.
    if (cp == 0x3002 || cp == 0xFF0C) row -= 14;
    if (row < 0 || row >= 24 || col < 0 || col >= 24) return false;
    return (bitmap[row * 3 + col / 8] & (1u << (7 - col % 8))) != 0;
}

static const uint8_t *glyph5x7(char c) {
    static const uint8_t blank[5] = {0, 0, 0, 0, 0};
    static const uint8_t colon[5] = {0, 0x36, 0x36, 0, 0};
    static const uint8_t slash[5] = {0x20, 0x10, 0x08, 0x04, 0x02};
    static const uint8_t percent[5] = {0x63, 0x13, 0x08, 0x64, 0x63};
    static const uint8_t dash[5] = {0x08, 0x08, 0x08, 0x08, 0x08};
    static const uint8_t dot[5] = {0, 0x60, 0x60, 0, 0};
    static const uint8_t letters[][5] = {
        {0x7E, 0x11, 0x11, 0x11, 0x7E}, {0x7F, 0x49, 0x49, 0x49, 0x36},
        {0x3E, 0x41, 0x41, 0x41, 0x22}, {0x7F, 0x41, 0x41, 0x22, 0x1C},
        {0x7F, 0x49, 0x49, 0x49, 0x41}, {0x7F, 0x09, 0x09, 0x09, 0x01},
        {0x3E, 0x41, 0x49, 0x49, 0x7A}, {0x7F, 0x08, 0x08, 0x08, 0x7F},
        {0, 0x41, 0x7F, 0x41, 0},       {0x20, 0x40, 0x41, 0x3F, 0x01},
        {0x7F, 0x08, 0x14, 0x22, 0x41}, {0x7F, 0x40, 0x40, 0x40, 0x40},
        {0x7F, 0x02, 0x0C, 0x02, 0x7F}, {0x7F, 0x04, 0x08, 0x10, 0x7F},
        {0x3E, 0x41, 0x41, 0x41, 0x3E}, {0x7F, 0x09, 0x09, 0x09, 0x06},
        {0x3E, 0x41, 0x51, 0x21, 0x5E}, {0x7F, 0x09, 0x19, 0x29, 0x46},
        {0x46, 0x49, 0x49, 0x49, 0x31}, {0x01, 0x01, 0x7F, 0x01, 0x01},
        {0x3F, 0x40, 0x40, 0x40, 0x3F}, {0x1F, 0x20, 0x40, 0x20, 0x1F},
        {0x3F, 0x40, 0x38, 0x40, 0x3F}, {0x63, 0x14, 0x08, 0x14, 0x63},
        {0x03, 0x04, 0x78, 0x04, 0x03}, {0x61, 0x51, 0x49, 0x45, 0x43},
    };
    static const uint8_t digits[][5] = {
        {0x3E, 0x51, 0x49, 0x45, 0x3E}, {0, 0x42, 0x7F, 0x40, 0},
        {0x42, 0x61, 0x51, 0x49, 0x46}, {0x21, 0x41, 0x45, 0x4B, 0x31},
        {0x18, 0x14, 0x12, 0x7F, 0x10}, {0x27, 0x45, 0x45, 0x45, 0x39},
        {0x3C, 0x4A, 0x49, 0x49, 0x30}, {0x01, 0x71, 0x09, 0x05, 0x03},
        {0x36, 0x49, 0x49, 0x49, 0x36}, {0x06, 0x49, 0x49, 0x29, 0x1E},
    };
    if (c >= 'a' && c <= 'z') c -= 'a' - 'A';
    if (c >= 'A' && c <= 'Z') return letters[c - 'A'];
    if (c >= '0' && c <= '9') return digits[c - '0'];
    if (c == ' ') return blank;
    if (c == ':') return colon;
    if (c == '/') return slash;
    if (c == '%') return percent;
    if (c == '-') return dash;
    if (c == '.') return dot;
    // Unsupported printable ASCII must be visible in the missing-glyph metric
    // instead of silently rendering a question mark.
    return NULL;
}

static void put_px(uint16_t *fb, int fw, int fh, int x, int y, uint16_t color) {
    if (x < 0 || y < 0 || x >= fw || y >= fh) return;
    fb[y * fw + x] = color;
}

static void fill_rect(uint16_t *fb, int fw, int fh, int x, int y, int w, int h, uint16_t color) {
    for (int yy = y; yy < y + h; ++yy) {
        for (int xx = x; xx < x + w; ++xx) {
            put_px(fb, fw, fh, xx, yy, color);
        }
    }
}

static void draw_box(uint16_t *fb, int fw, int fh, int x, int y, uint16_t fg, uint16_t bg) {
    fill_rect(fb, fw, fh, x, y, 24, 24, bg);
    for (int i = 4; i < 20; ++i) {
        put_px(fb, fw, fh, x + i, y + 4, fg);
        put_px(fb, fw, fh, x + i, y + 19, fg);
        put_px(fb, fw, fh, x + 4, y + i, fg);
        put_px(fb, fw, fh, x + 19, y + i, fg);
    }
}

/** Hollow box for an unsupported single-byte character. */
static void draw_ascii_box(uint16_t *fb, int fw, int fh, int x, int y, int scale,
                           uint16_t fg, uint16_t bg) {
    const int w = 5 * scale;
    const int h = 7 * scale;
    fill_rect(fb, fw, fh, x, y, w, h, bg);
    for (int i = 0; i < w; ++i) {
        put_px(fb, fw, fh, x + i, y, fg);
        put_px(fb, fw, fh, x + i, y + h - 1, fg);
    }
    for (int i = 0; i < h; ++i) {
        put_px(fb, fw, fh, x, y + i, fg);
        put_px(fb, fw, fh, x + w - 1, y + i, fg);
    }
}

void buddy_font_draw(uint16_t *fb, int fw, int fh, int x, int y, const char *text,
                     uint16_t fg, uint16_t bg, int scale, int max_px) {
    if (!fb || !text) return;
    if (scale < 1) scale = 1;
    if (scale > 6) scale = 6;
    int pen = 0;
    const char *p = text;
    while (*p) {
        uint32_t cp = 0;
        const int len = utf8_decode(p, &cp);
        p += len;
        if (cp == '\r' || cp == '\n') continue;

        if (cp < 0x80) {
            if (cp < 0x20 || cp == 0x7F) continue;  // control characters
            const int adv = 6 * scale;
            if (max_px > 0 && pen + adv > max_px) break;
            const uint8_t *rows = glyph5x7((char)cp);
            if (!rows) {
                draw_ascii_box(fb, fw, fh, x + pen, y, scale, fg, bg);
                atomic_fetch_add(&s_missing, 1);
                pen += adv;
                continue;
            }
            fill_rect(fb, fw, fh, x + pen, y, 5 * scale, 7 * scale, bg);
            for (int row = 0; row < 7; ++row) {
                for (int col = 0; col < 5; ++col) {
                    if (rows[col] & (1u << (6 - row))) {
                        fill_rect(fb, fw, fh, x + pen + col * scale, y + row * scale, scale,
                                  scale, fg);
                    }
                }
            }
            pen += adv;
            continue;
        }

        const int adv = 24;
        if (max_px > 0 && pen + adv > max_px) break;
        const uint8_t *bitmap = glyph24(cp);
        if (!bitmap) {
            draw_box(fb, fw, fh, x + pen, y, fg, bg);
            atomic_fetch_add(&s_missing, 1);
        } else {
            fill_rect(fb, fw, fh, x + pen, y, 24, 24, bg);
            for (int row = 0; row < 24; ++row) {
                for (int col = 0; col < 24; ++col) {
                    if (glyph24_pixel(bitmap, cp, row, col)) {
                        put_px(fb, fw, fh, x + pen + col, y + row, fg);
                    }
                }
            }
        }
        pen += adv;
    }
}

int buddy_font_width(const char *text, int scale, int max_px) {
    if (!text) return 0;
    if (scale < 1) scale = 1;
    if (scale > 6) scale = 6;
    int pen = 0;
    const char *p = text;
    while (*p) {
        uint32_t cp = 0;
        const int len = utf8_decode(p, &cp);
        p += len;
        const int adv = cp < 0x80 ? 6 * scale : 24;
        if (max_px > 0 && pen + adv > max_px) break;
        pen += adv;
    }
    return pen;
}

void buddy_utf8_truncate(char *dst, size_t dst_size, const char *src, size_t max_bytes) {
    if (!dst || dst_size == 0) return;
    dst[0] = '\0';
    if (!src || max_bytes == 0) return;
    size_t out = 0;
    const char *p = src;
    while (*p) {
        uint32_t cp = 0;
        const int len = utf8_decode(p, &cp);
        if (len <= 0) break;
        if (out + (size_t)len > max_bytes || out + (size_t)len >= dst_size) break;
        memcpy(dst + out, p, (size_t)len);
        out += (size_t)len;
        p += len;
    }
    dst[out] = '\0';
}
