/* Host test for the straight-line 24px cell renderer added for the standby
 * page: Latin and ideographs must share one cell grid, and the zoom must scale
 * both the advance and the cell height. */
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "buddy_font.h"

/* Declared in buddy_font.h's implementation but not exposed there yet; the UI
 * and this test forward-declare them identically. */
void buddy_font_draw_cells(uint16_t *fb, int fw, int fh, int x, int y, const char *text,
                           uint16_t fg, uint16_t bg, float zoom, int max_px);
int buddy_font_cells_width(const char *text, float zoom, int max_px);

const uint8_t _binary_cjk24_cjk_bin_start[1] = {0};
const uint8_t _binary_cjk24_cjk_bin_end[1] = {0};
const uint8_t _binary_cjk24_ext_bin_start[1] = {0};
const uint8_t _binary_cjk24_ext_bin_end[1] = {0};
const uint8_t _binary_cjk16_bin_start[1] = {0};
const uint8_t _binary_cjk16_bin_end[1] = {0};

#define FB_W 240
#define FB_H 80
#define FG 0xFFFFu
#define BG 0x0000u

static uint16_t s_fb[FB_W * FB_H];

static uint16_t px(int x, int y) { return s_fb[y * FB_W + x]; }

static int check(const char *name, int cond) {
    printf("%s: %s\n", name, cond ? "ok" : "FAIL");
    return cond ? 0 : 1;
}

/* Topmost and bottommost rows that carry ink, -1 when the band is empty. */
static void ink_rows(int *first, int *last) {
    *first = -1;
    *last = -1;
    for (int y = 0; y < FB_H; ++y) {
        for (int x = 0; x < FB_W; ++x) {
            if (px(x, y) == FG) {
                if (*first < 0) *first = y;
                *last = y;
                break;
            }
        }
    }
}

/* Ink present anywhere inside a column range. */
static int ink_in_cols(int x0, int x1) {
    for (int y = 0; y < FB_H; ++y) {
        for (int x = x0; x < x1; ++x) {
            if (px(x, y) == FG) return 1;
        }
    }
    return 0;
}

int main(void) {
    int failures = 0;

    /* Advances: ASCII 18, space 12, CJK 24 at zoom 1; all scaled by the zoom. */
    failures += check("cells width 'M' zoom1", buddy_font_cells_width("M", 1.0f, 0) == 18);
    failures += check("cells width 'M' zoom1.5", buddy_font_cells_width("M", 1.5f, 0) == 27);
    failures += check("cells width ' ' zoom1.5", buddy_font_cells_width(" ", 1.5f, 0) == 18);
    failures += check("cells width 'Ma' zoom1.5", buddy_font_cells_width("Ma", 1.5f, 0) == 54);

    /* A 1.5x cell is 36px tall; 'M' must reach into the top half of it, which
     * is exactly what the 5x7 table at scale 1 could not do (7px tall). */
    memset(s_fb, 0, sizeof(s_fb));
    buddy_font_draw_cells(s_fb, FB_W, FB_H, 0, 0, "M", FG, BG, 1.5f, FB_W);
    int first = -1, last = -1;
    ink_rows(&first, &last);
    failures += check("'M' has ink", first >= 0);
    failures += check("'M' starts near cell top", first <= 6);
    failures += check("'M' reaches cell bottom", last >= 30 && last <= 35);
    /* The 5x7 glyph at scale 3 is 19 source px wide against an 18px advance, so
     * a 1-2px overhang past the advance is expected; it can only land on the
     * next cell's blank left margin, never on the following glyph. */
    failures += check("'M' stays inside its advance", !ink_in_cols(30, FB_W));

    /* The background pass must clear the whole 36px band, including the gap a
     * narrow ASCII advance leaves between two cells. */
    memset(s_fb, FG, sizeof(s_fb));
    buddy_font_draw_cells(s_fb, FB_W, FB_H, 0, 0, "MM", FG, BG, 1.5f, FB_W);
    failures += check("band cleared to bg", px(20, 2) == BG);
    failures += check("band cleared between cells", px(30, 35) == BG);
    failures += check("band height is 36px", px(30, 36) == FG);

    /* A space draws nothing but still advances the pen by 18px at 1.5x. */
    memset(s_fb, 0, sizeof(s_fb));
    buddy_font_draw_cells(s_fb, FB_W, FB_H, 0, 0, "M M", FG, BG, 1.5f, FB_W);
    failures += check("space keeps pen moving", ink_in_cols(44, 46 + 27));

    /* An unsupported printable ASCII char is counted, like the 1-bit path. */
    buddy_font_missing_total(true);
    memset(s_fb, 0, sizeof(s_fb));
    buddy_font_draw_cells(s_fb, FB_W, FB_H, 0, 0, "#", FG, BG, 1.5f, FB_W);
    failures += check("'#' counted missing", buddy_font_missing_total(false) == 1);

    return failures == 0 ? 0 : 1;
}
