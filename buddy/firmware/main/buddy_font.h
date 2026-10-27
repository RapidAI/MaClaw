#pragma once

#include <stdbool.h>
#include <stdint.h>

/** Draws UTF-8 text into an RGB565 framebuffer.
 *
 * ASCII uses a 5x7 cell scaled by `scale` (1..6); CJK uses the packed 24x24
 * tables at 1:1. Unsupported codepoints are drawn as a hollow box and counted
 * so the host can report a missing-glyph rate instead of silently dropping
 * characters.
 *
 * `fg`/`bg` are ST77916-ready colour words (already byte-swapped).
 */
void buddy_font_draw(uint16_t *fb, int fw, int fh, int x, int y, const char *text,
                     uint16_t fg, uint16_t bg, int scale, int max_px);

/** Width in pixels of up to `max_px`, without drawing. */
int buddy_font_width(const char *text, int scale, int max_px);

/** UTF-8 safe truncation to `max_bytes`, always ending on a rune boundary. */
void buddy_utf8_truncate(char *dst, size_t dst_size, const char *src, size_t max_bytes);

int buddy_font_missing_total(bool reset);
