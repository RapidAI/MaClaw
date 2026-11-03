/* Locks the 16px-table phase invariant against the real renderer.
 *
 * The panel point-samples the 360 canvas onto 240 glass: glass row gy reads
 * canvas row (gy*360)//240, i.e. canvas rows with (c % 3) in {0,1}; glass column
 * gx reads canvas column 359-(gx*360)//240, i.e. canvas columns with (c % 3) in
 * {0,2}.
 *
 * A 16px cell at zoom 1.875 spans 30 canvas px -> 20 glass px. That is
 * magnification, so a source row legitimately maps to 1 or 2 glass rows -- but
 * NO source row may map to zero. The invariant is therefore source coverage:
 * every one of the 16 source rows and columns reaches the glass at least once.
 *
 * buddy_font.c guarantees it by snapping advances to multiples of 3 (so one
 * origin holds for the whole line); buddy_ui.c picks that origin with band_x /
 * band_y. The old 1.25 label zoom failed this at every possible origin: it
 * dropped 6 of 24 source rows and 6 of 24 columns.
 */
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "buddy_font.h"

/* The embedded tables are linked by the firmware; the host build only needs the
 * symbols to resolve. */
const uint8_t _binary_cjk24_cjk_bin_start[1] = {0};
const uint8_t _binary_cjk24_cjk_bin_end[1] = {0};
const uint8_t _binary_cjk24_ext_bin_start[1] = {0};
const uint8_t _binary_cjk24_ext_bin_end[1] = {0};
const uint8_t _binary_cjk16_bin_start[1] = {0};
const uint8_t _binary_cjk16_bin_end[1] = {0};

#define CANVAS 360
#define W CANVAS
#define H CANVAS
#define SRC 16

void buddy_font_draw_small(uint16_t *fb, int fw, int fh, int x, int y, const char *text,
                           uint16_t fg, uint16_t bg, float zoom, int max_px);
int buddy_font_small_width(const char *text, float zoom, int max_px);

static uint16_t s_fb[CANVAS * CANVAS];

static int check(const char *name, int cond) {
    printf("%s: %s\n", name, cond ? "ok" : "FAIL");
    return cond ? 0 : 1;
}

/* Mirrors band_x/band_y in buddy_ui.c. band_x snaps to phase 0: it is the only
 * origin that keeps all 16 source columns at zoom 1.875 on the mirrored axis. */
static int band_x(int x) {
    const int phase = ((x % 3) + 3) % 3;
    if (phase == 1) return x + 2;
    if (phase == 2) return x + 1;
    return x;
}
static int band_y(int y) {
    const int phase = ((y % 3) + 3) % 3;
    return phase == 0 ? y + 1 : y;
}

static int canvas_row_sampled(int cy) { return cy % 3 != 2; }
static int canvas_col_sampled(int cx) { return cx % 3 != 1; }

/* Source rows/cols of a cell at (x0,y0) that reach the glass at least once. */
static void source_coverage(int x0, int y0, float zoom, int *rows, int *cols) {
    int seen_r[SRC] = {0};
    int seen_c[SRC] = {0};
    const int px = (int)(SRC * zoom + 0.5f);
    for (int d = 0; d < px; ++d) {
        const int s = (int)((float)d / zoom);
        if (s < 0 || s >= SRC) continue;
        if (canvas_row_sampled(y0 + d)) seen_r[s] = 1;
        if (canvas_col_sampled(x0 + d)) seen_c[s] = 1;
    }
    *rows = 0;
    *cols = 0;
    for (int i = 0; i < SRC; ++i) {
        *rows += seen_r[i];
        *cols += seen_c[i];
    }
}

int main(void) {
    int failures = 0;
    const float zoom = 1.875f;

    /* 1. Advances are 3-snapped, so one band_x origin holds for the whole line.
     *    Without this, mixed-width text drifts a column per glyph and the last
     *    source column of later glyphs is dropped. */
    failures += check("cjk advance is a multiple of 3",
                      buddy_font_small_width("\xE4\xB8\xAD", zoom, 0) % 3 == 0);
    failures += check("ascii advance is a multiple of 3",
                      buddy_font_small_width("M", zoom, 0) % 3 == 0);

    /* 2. Every band_y phase keeps all source rows (band_y repairs phase 0). */
    int worst_rows = SRC;
    for (int y = 0; y < 12; ++y) {
        int rows = 0, cols = 0;
        source_coverage(band_x(0), band_y(y), zoom, &rows, &cols);
        if (rows < worst_rows) worst_rows = rows;
    }
    failures += check("band_y keeps all 16 source rows at every phase", worst_rows == SRC);

    /* 3. Same for the column axis (band_x repairs phase 1). */
    int worst_cols = SRC;
    for (int x = 0; x < 12; ++x) {
        int rows = 0, cols = 0;
        source_coverage(band_x(x), band_y(50), zoom, &rows, &cols);
        if (cols < worst_cols) worst_cols = cols;
    }
    failures += check("band_x keeps all 16 source cols at every phase", worst_cols == SRC);

    /* 4. Real standby strings, centred exactly as the UI does it. */
    const char *lines[] = {"未连接", "待确认 5", "静音", "北京市朝阳区"};
    for (size_t i = 0; i < sizeof(lines) / sizeof(lines[0]); ++i) {
        const int width = buddy_font_small_width(lines[i], zoom, 0);
        const int x = band_x((W - width) / 2);
        const int y = band_y(50);
        int rows = 0, cols = 0;
        source_coverage(x, y, zoom, &rows, &cols);
        char name[96];
        snprintf(name, sizeof(name), "'%s' keeps all source rows", lines[i]);
        failures += check(name, rows == SRC);
        snprintf(name, sizeof(name), "'%s' keeps all source cols", lines[i]);
        failures += check(name, cols == SRC);
    }

    /* 5. The renderer really paints in that band, so the checks above are not
     *    vacuous. */
    memset(s_fb, 0, sizeof(s_fb));
    const int width = buddy_font_small_width("未连接", zoom, 0);
    const int x = band_x((W - width) / 2);
    const int y = band_y(50);
    buddy_font_draw_small(s_fb, W, H, x, y, "未连接", 0xFFFF, 0x0000, zoom, width);
    int ink = 0, outside = 0;
    const int px = (int)(SRC * zoom + 0.5f);
    for (int r = 0; r < H; ++r) {
        for (int c = 0; c < W; ++c) {
            if (s_fb[r * W + c] != 0xFFFF) continue;
            ++ink;
            if (r < y || r >= y + px || c < x || c >= x + width) ++outside;
        }
    }
    failures += check("renderer paints the label", ink > 200);
    failures += check("ink stays inside the band", outside == 0);

    return failures == 0 ? 0 : 1;
}
