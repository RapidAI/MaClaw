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

/** Decodes one UTF-8 codepoint; returns bytes consumed (>=1). */
int buddy_font_utf8_next(const char *text, uint32_t *codepoint);

/** Curved-text advance: ASCII 18 (space 12), CJK 24. */
int buddy_font_arc_advance(uint32_t codepoint);

/** Total curved-text width of a UTF-8 string. */
int buddy_font_arc_width(const char *text);

/** Rasterizes one codepoint into a 24x24 1-bit cell for rotated drawing.
 * Returns false when the glyph is unsupported. */
bool buddy_font_cell(uint32_t codepoint, uint8_t cell[24 * 24]);
