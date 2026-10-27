/*
 * Host-side pixel test for the 5x7 ASCII orientation.
 *
 * The firmware table stores one byte per glyph column with bit 0 = top row.
 * A vertically flipped renderer still looks plausible in a photo, so this
 * test pins the orientation to the original iot-agentos convention.
 *
 * Build with the host gcc (no ESP-IDF headers are needed by buddy_font.c):
 *   gcc -std=gnu11 -I .. -o test_font_ascii test_font_ascii.c ../buddy_font.c
 * Run: ./test_font_ascii
 */

#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "buddy_font.h"

/* buddy_font.c references the packed CJK tables; empty stubs are enough here
 * because this test only draws ASCII. */
const uint8_t _binary_cjk24_cjk_bin_start[1] = {0};
const uint8_t _binary_cjk24_cjk_bin_end[1] = {0};
const uint8_t _binary_cjk24_ext_bin_start[1] = {0};
const uint8_t _binary_cjk24_ext_bin_end[1] = {0};

#define FB_W 64
#define FB_H 16
#define FG 0xFFFFu
#define BG 0x0000u

static uint16_t s_fb[FB_W * FB_H];

static uint16_t px(int x, int y) { return s_fb[y * FB_W + x]; }

static int check(const char *name, int cond) {
    printf("%s: %s\n", name, cond ? "ok" : "FAIL");
    return cond ? 0 : 1;
}

int main(void) {
    int failures = 0;

    memset(s_fb, 0, sizeof(s_fb));
    buddy_font_draw(s_fb, FB_W, FB_H, 0, 0, "L", FG, BG, 1, FB_W);

    /* 'L' column 0 is the full left stroke (0x7F): rows 0..6 set. */
    failures += check("L top-left set", px(0, 0) == FG);
    failures += check("L bottom-left set", px(0, 6) == FG);
    /* Columns 1..4 only carry the bottom bar (0x40): top must be clear. */
    failures += check("L top-right clear", px(4, 0) == BG);
    failures += check("L bottom-right set", px(4, 6) == FG);

    /* '1' = {0,0x42,0x7F,0x40,0}: the vertical stem is in column 2. */
    memset(s_fb, 0, sizeof(s_fb));
    buddy_font_draw(s_fb, FB_W, FB_H, 0, 0, "1", FG, BG, 1, FB_W);
    failures += check("'1' stem top set", px(2, 0) == FG);
    failures += check("'1' stem bottom set", px(2, 6) == FG);

    /* A space must stay blank and must not count as a missing glyph. */
    memset(s_fb, 0, sizeof(s_fb));
    buddy_font_draw(s_fb, FB_W, FB_H, 0, 0, " ", FG, BG, 1, FB_W);
    failures += check("space blank", px(0, 0) == BG && px(4, 6) == BG);
    failures += check("space not missing", buddy_font_missing_total(false) == 0);

    /* Unsupported printable ASCII is counted (box fallback). */
    memset(s_fb, 0, sizeof(s_fb));
    buddy_font_draw(s_fb, FB_W, FB_H, 0, 0, "#", FG, BG, 1, FB_W);
    failures += check("'#' counted missing", buddy_font_missing_total(false) == 1);

    return failures == 0 ? 0 : 1;
}
