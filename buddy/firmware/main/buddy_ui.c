#include "buddy_ui.h"

#include <math.h>
#include <stdio.h>
#include <string.h>

#include "esp_timer.h"

#include "buddy_display.h"
#include "buddy_font.h"
#include "buddy_layout.h"
#include "buddy_mood.h"
#include "buddy_pet.h"
#include "buddy_sound.h"
#include "buddy_task_cursor.h"

/* Cell-grid text entry points implemented in buddy_font.c. Declared here rather
 * than in buddy_font.h so the header stays untouched by this change; the
 * straight-line cell renderer is used by the UI only. */
void buddy_font_draw_cells(uint16_t *fb, int fw, int fh, int x, int y, const char *text,
                           uint16_t fg, uint16_t bg, float zoom, int max_px);
int buddy_font_cells_width(const char *text, float zoom, int max_px);
void buddy_font_draw_small(uint16_t *fb, int fw, int fh, int x, int y, const char *text,
                           uint16_t fg, uint16_t bg, float zoom, int max_px);
int buddy_font_small_width(const char *text, float zoom, int max_px);

#define W 360
#define H 360

/** Magnification for the standby weather arc: the packed 24px cells are the
 * largest glyphs on the panel, but the weather reading is the one line the
 * user scans from a distance, so it is drawn 25% larger. */
#define WEATHER_TEXT_ZOOM 1.25f

/** Standby type scale for the square glass only.
 *
 * The UI composes a 360x360 canvas; the NV3023 module is 240x240, so
 * buddy_display point-samples it 3:2. A size is pixel-exact only when every
 * source row and column reaches exactly one glass pixel, which for the 24px
 * table happens at zoom 1.0 (16 glass), 1.5 (24) and 2.0 (32) -- and nowhere in
 * between. The labels below therefore come from the 16px Unifont table, whose
 * 16 source rows can hit 20 glass px exactly at zoom 1.875.
 *
 *   brand / date   24px table @ 1.5  -> 24 glass, exact
 *   status/weather 16px table @ 1.875 -> 20 glass, exact
 *
 * The old 1.25 label zoom (24px table -> 20 glass) was not mappable at any
 * origin phase: it dropped 6 of 24 source rows and 6 of 24 columns, which is
 * why small labels looked chewed rather than merely small.
 * The round 360 ST77916 keeps its curved text, where the canvas is the glass. */
#define STANDBY_TITLE_ZOOM 1.5f
#define STANDBY_LABEL_ZOOM 1.875f

/** Zoom for the task / attention / celebrate / prompt pages on the square glass.
 *
 * Those lines mix Latin with Chinese (a task name, a phase, a prompt), so they
 * all come from the 16px Unifont table. At 1.5 the cell is 24 canvas px and
 * lands on 16 glass px with every source row and column sampled exactly once
 * (see band_x/band_y). The 24px table at zoom 1.0 produced the same 16 glass px
 * but dropped 8 of its 24 source rows on every line, which is what made the
 * strokes look broken rather than small.
 *
 * The round 360 ST77916 has no downsampling, so it keeps its own sizes. */
#define TASK_TEXT_ZOOM 1.5f

/** Top of the standby status band. The enlarged label cell is 30px tall, so it
 * sits at y=50..80 and clears the 176px pet asset, whose top edge is y=82. The
 * 36px title band above it ends at y=44, leaving a 6px gap.
 *
 * 50 rather than 46 is deliberate: the panel shows canvas row py*1.5, so a
 * 4-canvas-row shift is 2.67 physical px and lands as exactly 3 glass rows
 * (the first ink row moves from glass 30 to glass 33). */
#define STANDBY_STATUS_Y 50

/** Status band top on the round 360 glass, which draws the canvas 1:1 and keeps
 * the 24px line it always had. */
#define STANDBY_STATUS_Y_ROUND 61

#define COL_BG buddy_color(0x0E, 0x11, 0x16)
#define COL_PANEL buddy_color(0x18, 0x1E, 0x28)
#define COL_TEXT buddy_color(0xF8, 0xFA, 0xFC)
#define COL_MUTED buddy_color(0x8B, 0x9A, 0xB4)
#define COL_ACCENT buddy_color(0x38, 0xBD, 0xF8)
#define COL_SUCCESS buddy_color(0x34, 0xD3, 0x99)
#define COL_WARN buddy_color(0xF5, 0x9E, 0x0B)
#define COL_DANGER buddy_color(0xF8, 0x71, 0x71)
#define COL_PET buddy_color(0x60, 0xA5, 0xFA)
#define COL_PET_DARK buddy_color(0x3B, 0x82, 0xF6)
#define COL_PET_CHEEK buddy_color(0xFB, 0x92, 0x9C)

static buddy_counts_t s_last_counts;
static char s_last_title[BUDDY_TITLE_BYTES + 1];
static char s_last_phase[BUDDY_PHASE_BYTES + 1];
static char s_last_elapsed[BUDDY_ELAPSED_BYTES + 1];
static char s_last_weather[BUDDY_LABEL_BYTES + 1];
static char s_last_location[BUDDY_LABEL_BYTES + 1];
static int s_last_temp;
static bool s_last_weather_stale;
static bool s_last_has_weather;
static char s_last_date[24];
static char s_last_weekday[16];
static char s_last_prompt[96];
static int s_last_page = -1;
static int s_last_active = -1;
static bool s_last_online;
static bool s_last_stale;
static int s_last_fill = -100;
static int s_last_mood = -2;
static bool s_last_muted;
static bool s_last_pinned;
static char s_last_attention_kind[12];
static char s_last_attention_title[BUDDY_TITLE_BYTES + 1];
static bool s_asleep;
static buddy_task_cursor_t s_cursor;

static const buddy_layout_t *ui_layout(void) {
    static buddy_layout_t layout;
    static bool ready;
    if (!ready) {
        buddy_layout_metrics(buddy_display_glass_round(), &layout);
        ready = true;
    }
    return &layout;
}

static void put_px(uint16_t *fb, int x, int y, uint16_t color) {
    if (x < 0 || y < 0 || x >= W || y >= H) return;
    fb[y * W + x] = color;
}

static void fill_rect(uint16_t *fb, int x, int y, int w, int h, uint16_t color) {
    if (w <= 0 || h <= 0) return;
    int x0 = x;
    int y0 = y;
    int x1 = x + w;
    int y1 = y + h;
    if (x0 < 0) x0 = 0;
    if (y0 < 0) y0 = 0;
    if (x1 > W) x1 = W;
    if (y1 > H) y1 = H;
    if (x0 >= x1 || y0 >= y1) return;
    const int n = x1 - x0;
    for (int yy = y0; yy < y1; ++yy) {
        uint16_t *row = fb + (size_t)yy * W + x0;
        for (int i = 0; i < n; ++i) row[i] = color;
    }
}

static void fill_circle(uint16_t *fb, int cx, int cy, int r, uint16_t color) {
    for (int y = -r; y <= r; ++y) {
        for (int x = -r; x <= r; ++x) {
            if (x * x + y * y <= r * r) put_px(fb, cx + x, cy + y, color);
        }
    }
}

static void fill_ring(uint16_t *fb, int cx, int cy, int r_outer, int r_inner, uint16_t color) {
    for (int y = -r_outer; y <= r_outer; ++y) {
        for (int x = -r_outer; x <= r_outer; ++x) {
            const int d = x * x + y * y;
            if (d <= r_outer * r_outer && d >= r_inner * r_inner) put_px(fb, cx + x, cy + y, color);
        }
    }
}

static void hline(uint16_t *fb, int x0, int x1, int y, uint16_t color) {
    if (x1 < x0) {
        int t = x0;
        x0 = x1;
        x1 = t;
    }
    for (int x = x0; x <= x1; ++x) put_px(fb, x, y, color);
}

static void draw_text(uint16_t *fb, int x, int y, const char *text, uint16_t fg, uint16_t bg,
                      int scale, int max_px) {
    buddy_font_draw(fb, W, H, x, y, text, fg, bg, scale, max_px);
}

static void draw_centered(uint16_t *fb, int y, const char *text, uint16_t fg, uint16_t bg,
                          int scale) {
    const int width = buddy_font_width(text, scale, W - 24);
    draw_text(fb, (W - width) / 2, y, text, fg, bg, scale, W - 24);
}

/** Straight text on the 24px cell grid (see buddy_font_draw_cells). Use this
 * for any line that can mix Latin with Chinese: the 5x7 ASCII table at scale 1
 * is 7px tall, so a name like "install buddy" rendered next to 24px ideographs
 * reads as a footnote. */
static void draw_text_cells(uint16_t *fb, int x, int y, const char *text, uint16_t fg,
                            uint16_t bg, float zoom, int max_px) {
    buddy_font_draw_cells(fb, W, H, x, y, text, fg, bg, zoom, max_px);
}

static void draw_centered_cells(uint16_t *fb, int y, const char *text, uint16_t fg, uint16_t bg,
                                float zoom, int max_px) {
    const int limit = max_px > 0 ? max_px : W - 24;
    const int width = buddy_font_cells_width(text, zoom, limit);
    draw_text_cells(fb, (W - width) / 2, y, text, fg, bg, zoom, limit);
}

/** Text from the 16px Unifont table, drawn at `zoom`.
 *
 * This is the only way to get a crisp 16-glass-px line on the 240x240 NV3023
 * module. The 24px table is crisp only at 24 glass px: at zoom 1.0 the panel's
 * 3:2 point sampling drops 8 of its 24 rows and 8 of its 24 columns, which is
 * why a small task name looked like a broken stroke instead of small text. A
 * 16px cell at zoom 1.5 is 24 canvas px -> 16 glass px, so every source pixel
 * reaches the panel exactly once.
 *
 * The origin is snapped by band_x/band_y below, so callers pass layout
 * coordinates unchanged and the 16->24->16 mapping stays one-to-one. */
static int band_x(int x);
static int band_y(int y);

static void draw_text_small(uint16_t *fb, int x, int y, const char *text, uint16_t fg,
                            uint16_t bg, float zoom, int max_px) {
    buddy_font_draw_small(fb, W, H, band_x(x), band_y(y), text, fg, bg, zoom, max_px);
}

static void draw_centered_small(uint16_t *fb, int y, const char *text, uint16_t fg, uint16_t bg,
                                float zoom, int max_px) {
    const int limit = max_px > 0 ? max_px : W - 24;
    const int width = buddy_font_small_width(text, zoom, limit);
    // draw_text_small applies band_x/band_y, so pass the plain centred origin.
    draw_text_small(fb, (W - width) / 2, y, text, fg, bg, zoom, limit);
}

/** True when the canvas is point-sampled down onto a smaller panel, i.e. the
 * 240x240 NV3023. The round 360 ST77916 draws the canvas 1:1. */
static bool small_glass(void) { return !ui_layout()->arc_standby; }

/** Page body text, sized to what each panel can show without resampling.
 *
 *   square 240, sampled -> 16px table @1.5: 24 canvas px -> 16 glass px, and
 *                          every source row/column lands exactly once
 *   round 360, 1:1      -> the 24px cell grid @1.0 it already shipped with
 *
 * Both branches fill the same 24 canvas px cell, so the layout constants in
 * buddy_layout.c stay valid for either glass. Only the square module changes. */
static void draw_text_body(uint16_t *fb, int x, int y, const char *text, uint16_t fg, uint16_t bg,
                           int max_px, int legacy_scale) {
    if (small_glass()) {
        draw_text_small(fb, x, y, text, fg, bg, TASK_TEXT_ZOOM, max_px);
    } else {
        draw_text(fb, x, y, text, fg, bg, legacy_scale, max_px);
    }
}

static void draw_centered_body(uint16_t *fb, int y, const char *text, uint16_t fg, uint16_t bg,
                               int max_px, int legacy_scale) {
    const int limit = max_px > 0 ? max_px : W - 24;
    if (small_glass()) {
        draw_centered_small(fb, y, text, fg, bg, TASK_TEXT_ZOOM, limit);
    } else {
        draw_centered(fb, y, text, fg, bg, legacy_scale);
    }
}

/** Standby status line: the only line whose zoom differs by glass. The sampled
 * 240 panel gets the 16px table at 1.875 (20 glass px, every source row and
 * column sampled once); the round 360 panel draws 1:1 and keeps its 24px line
 * at the origin it shipped with. */
static void draw_status_line(uint16_t *fb, int y, const char *text, uint16_t fg, int max_px) {
    if (small_glass()) {
        draw_centered_small(fb, y, text, fg, COL_BG, STANDBY_LABEL_ZOOM, max_px);
    } else {
        draw_centered(fb, y, text, fg, COL_BG, 1);
    }
}

/** fit_text twin of draw_text_body: measures with whichever table the branch
 * above will render, so truncation and drawing agree on both glasses. */
static size_t fit_text(char *dst, size_t dst_size, const char *src, int scale, int max_px,
                       bool ellipsis);
static size_t fit_text_small(char *dst, size_t dst_size, const char *src, float zoom, int max_px,
                             bool ellipsis);
static size_t fit_text_body(char *dst, size_t dst_size, const char *src, int max_px,
                            int legacy_scale, bool ellipsis) {
    if (small_glass()) return fit_text_small(dst, dst_size, src, TASK_TEXT_ZOOM, max_px, ellipsis);
    return fit_text(dst, dst_size, src, legacy_scale, max_px, ellipsis);
}

/** Width twin of draw_text_body, for right-aligned lines. */
static int body_width(const char *text, int max_px, int legacy_scale) {
    if (small_glass()) return buddy_font_small_width(text, TASK_TEXT_ZOOM, max_px);
    return buddy_font_width(text, legacy_scale, max_px);
}

/** Rounds a text-band top onto a canvas row the panel actually samples.
 *
 * buddy_display turns the 360 canvas into the 240 glass by point sampling:
 * glass row gy reads canvas row (gy * 360) // 240, which is the canvas rows
 * with (c % 3) in {0, 1}. Columns are mirrored (the panel's MADCTL MX bit does
 * nothing, so the 180 degree mount is corrected in software), so glass column
 * gx reads canvas column 359 - (gx * 360) // 240, i.e. the columns with
 * (c % 3) in {0, 2}.
 *
 * A band whose top is at canvas row 3k is therefore the worst case: its first
 * source row sits on a canvas row that is never sampled. Keeping the top at
 * 3k+1 or 3k+2 makes a 16px cell at zoom 1.5 map source row r to glass row r
 * with nothing lost and nothing duplicated. */
static int band_y(int y) {
    const int phase = ((y % 3) + 3) % 3;
    if (phase == 0) return y + 1;
    return y;
}

/** Column twin of band_y, and the reason it snaps to phase 0.
 *
 * On the mirrored column axis only origin % 3 == 0 keeps all 16 source columns
 * of a 16px cell at zoom 1.875: phase 2 loses source columns 7 and 15, phase 1
 * loses every odd column. Phase 0 is therefore the only exact choice, so both
 * other phases are pushed to it. Advances are 3-snapped (advance16 in
 * buddy_font.c), so this one correction holds for the whole line. */
static int band_x(int x) {
    const int phase = ((x % 3) + 3) % 3;
    if (phase == 1) return x + 2;
    if (phase == 2) return x + 1;
    return x;
}

/** Copies a prefix that fits `max_px`, appending an ellipsis when truncated.
 * Returns the number of source bytes consumed. */
static size_t fit_text(char *dst, size_t dst_size, const char *src, int scale, int max_px,
                       bool ellipsis) {
    if (!dst || dst_size == 0) return 0;
    dst[0] = '\0';
    if (!src) return 0;

    const char *ell = "\xE2\x80\xA6";  // U+2026
    const int ell_w = ellipsis ? buddy_font_width(ell, scale, max_px) : 0;
    size_t out = 0;
    int pen = 0;
    const char *p = src;
    while (*p) {
        // Measure one UTF-8 rune.
        const unsigned char *u = (const unsigned char *)p;
        size_t len = 1;
        if ((u[0] & 0xE0) == 0xC0) {
            len = 2;
        } else if ((u[0] & 0xF0) == 0xE0) {
            len = 3;
        } else if ((u[0] & 0xF8) == 0xF0) {
            len = 4;
        }
        char rune[5] = {0};
        memcpy(rune, p, len);
        const int adv = buddy_font_width(rune, scale, max_px);
        if (pen + adv > max_px - ell_w) {
            if (ellipsis && out + 3 < dst_size) {
                memcpy(dst + out, ell, 3);
                out += 3;
            }
            dst[out] = '\0';
            return (size_t)(p - src);
        }
        if (out + len >= dst_size) break;
        memcpy(dst + out, p, len);
        out += len;
        pen += adv;
        p += len;
    }
    dst[out] = '\0';
    return (size_t)(p - src);
}

/** Cell-mode twin of fit_text: same contract, but measures with the 24px grid
 * metrics so the truncation point matches what buddy_font_draw_cells renders. */
static size_t fit_text_cells(char *dst, size_t dst_size, const char *src, float zoom, int max_px,
                             bool ellipsis) {
    if (!dst || dst_size == 0) return 0;
    dst[0] = '\0';
    if (!src) return 0;
    if (!(zoom > 0.0f)) zoom = 1.0f;
    if (max_px <= 0) max_px = W - 24;

    const char *ell = "\xE2\x80\xA6";  // U+2026
    const int ell_adv = ellipsis ? (int)lroundf((float)buddy_font_arc_advance(0x2026) * zoom) : 0;
    size_t out = 0;
    int pen = 0;
    const char *p = src;
    while (*p) {
        uint32_t cp = 0;
        const int len = buddy_font_utf8_next(p, &cp);
        if (len <= 0) break;
        const int adv = (int)lroundf((float)buddy_font_arc_advance(cp) * zoom);
        if (pen + adv > max_px - ell_adv) {
            if (ellipsis && out + 3 < dst_size) {
                memcpy(dst + out, ell, 3);
                out += 3;
            }
            dst[out] = '\0';
            return (size_t)(p - src);
        }
        if (out + (size_t)len >= dst_size) break;
        memcpy(dst + out, p, (size_t)len);
        out += (size_t)len;
        pen += adv;
        p += len;
    }
    dst[out] = '\0';
    return (size_t)(p - src);
}

/** 16px-table twin of fit_text_cells. Advances differ (ASCII is 8 source
 * columns wide, not 18 canvas px), so truncation has to measure with
 * buddy_font_small_width to match what buddy_font_draw_small renders. */
static size_t fit_text_small(char *dst, size_t dst_size, const char *src, float zoom, int max_px,
                             bool ellipsis) {
    if (!dst || dst_size == 0) return 0;
    dst[0] = '\0';
    if (!src) return 0;
    if (!(zoom > 0.0f)) zoom = 1.0f;
    if (max_px <= 0) max_px = W - 24;

    const char *ell = "\xE2\x80\xA6";  // U+2026
    const int ell_adv = ellipsis ? buddy_font_small_width(ell, zoom, max_px) : 0;
    size_t out = 0;
    int pen = 0;
    const char *p = src;
    while (*p) {
        uint32_t cp = 0;
        const int len = buddy_font_utf8_next(p, &cp);
        if (len <= 0) break;
        // Measure through buddy_font_small_width so truncation uses the same
        // 3-snapped advance the renderer uses (see advance16 in buddy_font.c).
        char rune[5] = {0};
        memcpy(rune, p, (size_t)(len < 4 ? len : 4));
        const int adv = buddy_font_small_width(rune, zoom, 0);
        if (pen + adv > max_px - ell_adv) {
            if (ellipsis && out + 3 < dst_size) {
                memcpy(dst + out, ell, 3);
                out += 3;
            }
            dst[out] = '\0';
            return (size_t)(p - src);
        }
        if (out + (size_t)len >= dst_size) break;
        memcpy(dst + out, p, (size_t)len);
        out += (size_t)len;
        pen += adv;
        p += len;
    }
    dst[out] = '\0';
    return (size_t)(p - src);
}


/** Draws a right-aligned ASCII value, e.g. the counter numbers. */
static void draw_value_right(uint16_t *fb, int right_x, int y, int value, uint16_t fg,
                             uint16_t bg, int scale) {
    char buf[16];
    snprintf(buf, sizeof(buf), "%d", value);
    const int width = buddy_font_width(buf, scale, W);
    draw_text(fb, right_x - width, y, buf, fg, bg, scale, W);
}

static int page_of(const buddy_view_t *v) {
    if (v->prompt_active) return BUDDY_PAGE_PROMPT;
    if (v->celebrate_active) return BUDDY_PAGE_CELEBRATE;
    if (v->online && v->counts.in_progress > 0) return BUDDY_PAGE_TASKS;
    if (v->online && v->attention.title[0]) return BUDDY_PAGE_ATTENTION;
    return BUDDY_PAGE_STANDBY;
}

int buddy_ui_page(const buddy_view_t *view) {
    if (!view) return BUDDY_PAGE_STANDBY;
    return page_of(view);
}

const char *buddy_ui_page_name(int page) {
    switch (page) {
        case BUDDY_PAGE_TASKS:
            return "tasks";
        case BUDDY_PAGE_PROMPT:
            return "prompt";
        case BUDDY_PAGE_CELEBRATE:
            return "celebrate";
        case BUDDY_PAGE_ATTENTION:
            return "attention";
        default:
            return "standby";
    }
}

void buddy_ui_init(void) {
    memset(&s_last_counts, 0, sizeof(s_last_counts));
    s_last_title[0] = '\0';
    s_last_phase[0] = '\0';
    s_last_elapsed[0] = '\0';
    s_last_weather[0] = '\0';
    s_last_location[0] = '\0';
    s_last_temp = 0;
    s_last_weather_stale = false;
    s_last_has_weather = false;
    s_last_date[0] = '\0';
    s_last_weekday[0] = '\0';
    s_last_prompt[0] = '\0';
    s_last_attention_kind[0] = '\0';
    s_last_attention_title[0] = '\0';
    s_last_page = -1;
    s_last_active = -1;
    s_last_fill = -100;
}

/** Rotates the displayed in-progress task every three seconds.
 * A short press restarts that dwell on the next task. */
static int active_task_index(const buddy_view_t *v, int64_t now_ms) {
    const int count = v ? v->current_count : 0;
    buddy_task_cursor_note_count(&s_cursor, count, now_ms);
    if (count <= 0) return 0;
    return buddy_task_cursor_index(&s_cursor, count, now_ms);
}

void buddy_ui_on_tap(void) {
    buddy_view_t view;
    buddy_state_get(&view);
    buddy_task_cursor_advance(&s_cursor, view.current_count, esp_timer_get_time() / 1000);
}

void buddy_ui_on_swipe(void) {
    buddy_view_t view;
    buddy_state_get(&view);
    /* Prompt and celebrate cover the carousel. A swipe there would pin a
     * list the glass is not showing. */
    if (page_of(&view) != BUDDY_PAGE_TASKS) return;
    buddy_task_cursor_toggle_pin(&s_cursor, view.current_count, esp_timer_get_time() / 1000);
}

int buddy_ui_active_index(void) {
    buddy_view_t view;
    buddy_state_get(&view);
    if (view.current_count <= 0) return -1;
    return active_task_index(&view, esp_timer_get_time() / 1000);
}

/** Progress track. Unknown progress (the host sends -1) is a block that
 * travels the track; a later partial repaint moves only these rows. */
static void draw_task_bar(uint16_t *fb, const buddy_view_t *v, int active, int64_t now_ms) {
    const buddy_layout_t *layout = ui_layout();
    const int bar_x = layout->bar_x;
    const int bar_y = 298;
    const int bar_w = layout->bar_w;
    const int bar_h = 12;
    fill_rect(fb, bar_x, bar_y, bar_w, bar_h, COL_PANEL);
    float progress = -1.0f;
    if (v->current_count > 0 && active >= 0 && active < v->current_count) {
        progress = v->current[active].progress;
    }
    if (progress >= 0) {
        int fill_w = (int)(progress * (float)bar_w);
        if (fill_w < 0) fill_w = 0;
        if (fill_w > bar_w) fill_w = bar_w;
        fill_rect(fb, bar_x, bar_y, fill_w, bar_h, COL_ACCENT);
        char pct[8];
        snprintf(pct, sizeof(pct), "%d%%", (int)(progress * 100.0f + 0.5f));
        // Same treatment as the rest of the task body: the square panel gets the
        // 16px table at 16 glass px, the round panel keeps its 5x7 ASCII at 2x.
        draw_centered_body(fb, 312, pct, COL_MUTED, COL_BG, W, 2);
        return;
    }
    const int block = 30;
    const int travel = bar_w - block;
    /* ~4px per 50ms tick: /4 jumped a third of the block each frame. */
    const int pos = (int)((now_ms / 12) % (travel > 0 ? travel : 1));
    fill_rect(fb, bar_x + pos, bar_y, block, bar_h, COL_MUTED);
}

/** A still face so a task page can show the pet mood without redrawing it. */
static void draw_pet_chip(uint16_t *fb, int cx, int cy, int mood) {
    fill_circle(fb, cx, cy, 12, COL_PET);
    if (mood == BUDDY_MOOD_SLEEP) {
        hline(fb, cx - 6, cx - 2, cy - 1, COL_BG);
        hline(fb, cx + 2, cx + 6, cy - 1, COL_BG);
        return;
    }
    const int eye = mood == BUDDY_MOOD_BUSY ? 1 : 2;
    fill_circle(fb, cx - 4, cy - 2, eye, COL_BG);
    fill_circle(fb, cx + 4, cy - 2, eye, COL_BG);
    if (mood == BUDDY_MOOD_HAPPY) {
        hline(fb, cx - 4, cx + 4, cy + 3, COL_BG);
        hline(fb, cx - 2, cx + 2, cy + 5, COL_BG);
    } else if (mood == BUDDY_MOOD_BUSY) {
        hline(fb, cx - 4, cx + 4, cy + 4, COL_BG);
    }
}

static void draw_task_chip(uint16_t *fb, int mood) {
    /* Below the progress track, inside both the round aperture and the square. */
    draw_pet_chip(fb, W / 2, 332, mood);
}

static void widen_ascii_digits(char *dst, size_t dst_size, const char *src);
static void draw_arc_text_top(uint16_t *fb, const char *text, int radius, int y_offset, uint16_t fg);
static size_t fit_text_arc(char *dst, size_t dst_size, const char *src, int max_px, bool ellipsis);
static void draw_task_banner(uint16_t *fb, const buddy_view_t *v, int active);
static void draw_attention_date(uint16_t *fb, const buddy_view_t *v);

static const char *attention_label(const buddy_view_t *v) {
    if (strcmp(v->attention.kind, "failed") == 0) return "异常";
    if (strcmp(v->attention.kind, "paused") == 0) return "已暂停";
    return "待确认";
}

static void draw_task_page(uint16_t *fb, const buddy_view_t *v, int64_t now_ms, bool muted, int mood) {
    fill_rect(fb, 0, 0, W, H, COL_BG);
    const int active = active_task_index(v, now_ms);

    const buddy_layout_t *layout = ui_layout();
    // Phase and age take the brand line when the host sent them.
    draw_task_banner(fb, v, active);

    const int label_x = layout->label_x;
    const int value_right = layout->value_right;
    // The three counters keep the 24px cell height they had, but come from the
    // 16px table at 1.5 (see draw_text_small): 24 canvas px -> 16 glass px with
    // every source row and column landing on the panel exactly once. The 24px
    // table at zoom 1.0 drew the same 16 glass px while dropping 8 of its 24
    // source rows, which is what chewed the strokes.
    draw_text_body(fb, label_x, 74, "总任务", COL_TEXT, COL_BG, 120, 1);
    draw_value_right(fb, value_right, 62, v->counts.total, COL_TEXT, COL_BG, 4);

    draw_text_body(fb, label_x, 126, "进行中", COL_ACCENT, COL_BG, 120, 1);
    draw_value_right(fb, value_right, 114, v->counts.in_progress, COL_ACCENT, COL_BG, 4);

    draw_text_body(fb, label_x, 178, "已完成", COL_SUCCESS, COL_BG, 120, 1);
    draw_value_right(fb, value_right, 166, v->counts.done, COL_SUCCESS, COL_BG, 4);

    hline(fb, layout->rule_x0, layout->rule_x1, 204, COL_PANEL);

    const int text_x = layout->text_x;
    const int text_w = layout->title_w;
    char line1[BUDDY_TITLE_BYTES + 8];
    char line2[BUDDY_TITLE_BYTES + 8];
    line1[0] = '\0';
    line2[0] = '\0';
    if (v->current_count > 0 && v->current[active].title[0]) {
        // The first line is a clean break. An ellipsis here would mark a title
        // as cut off even when the rest is drawn on the next line.
        const size_t used =
            fit_text_body(line1, sizeof(line1), v->current[active].title, text_w, 1, false);
        const char *rest = v->current[active].title + used;
        if (*rest) {
            fit_text_body(line2, sizeof(line2), rest, text_w, 1, true);
        }
    } else {
        strlcpy(line1, "当前没有进行中的任务", sizeof(line1));
    }
    draw_text_body(fb, text_x, 206, line1, COL_TEXT, COL_BG, text_w, 1);
    if (line2[0]) draw_text_body(fb, text_x, 234, line2, COL_TEXT, COL_BG, text_w, 1);

    draw_task_bar(fb, v, active, now_ms);

    // One status line at the bottom. It combines at most two short pieces so
    // it stays inside the circle; stale is only shown when nothing else is.
    char tail[BUDDY_TITLE_BYTES + 24];
    tail[0] = '\0';
    uint16_t tail_color = COL_MUTED;
    if (!v->online) {
        strlcpy(tail, "未连接", sizeof(tail));
        tail_color = COL_MUTED;
    } else {
        if (v->attention.title[0]) {
            strlcpy(tail, attention_label(v), sizeof(tail));
            strlcat(tail, " ", sizeof(tail));
            strlcat(tail, v->attention.title, sizeof(tail));
            tail_color = strcmp(v->attention.kind, "failed") == 0 ? COL_DANGER : COL_WARN;
        }
        const int anomaly = v->counts.failed + v->counts.paused;
        if (!tail[0] && anomaly > 0) {
            snprintf(tail, sizeof(tail), "异常 %d", anomaly);
            tail_color = COL_DANGER;
        }
        if (!tail[0] && v->counts.pending > 0) {
            char piece[24];
            snprintf(piece, sizeof(piece), "待确认 %d", v->counts.pending);
            if (tail[0]) strlcat(tail, " · ", sizeof(tail));
            strlcat(tail, piece, sizeof(tail));
            if (anomaly == 0) tail_color = COL_WARN;
        }
        if (!tail[0] && v->stale) {
            strlcpy(tail, "数据陈旧", sizeof(tail));
            tail_color = COL_WARN;
        }
    }
    if (muted) {
        if (tail[0]) strlcat(tail, " · ", sizeof(tail));
        strlcat(tail, "静音", sizeof(tail));
        if (tail_color == COL_MUTED) tail_color = COL_WARN;
    }
    if (tail[0]) {
        char shown[BUDDY_TITLE_BYTES + 24];
        fit_text_body(shown, sizeof(shown), tail, layout->tail_w, 1, true);
        draw_centered_body(fb, 270, shown, tail_color, COL_BG, layout->tail_w, 1);
    }
    if (v->current_count > 1 && v->current_count <= BUDDY_MAX_TASKS && active >= 0 &&
        active < BUDDY_MAX_TASKS) {
        char index[4];
        index[0] = (char)('0' + active + 1);
        index[1] = '/';
        index[2] = (char)('0' + v->current_count);
        index[3] = '\0';
        const int width = body_width(index, 48, 1);
        const uint16_t index_color = buddy_task_cursor_pinned(&s_cursor) ? COL_ACCENT : COL_MUTED;
        draw_text_body(fb, layout->index_right - width, layout->index_y, index, index_color, COL_BG,
                       48, 1);
    }
    draw_task_chip(fb, mood);
}

static void draw_attention_page(uint16_t *fb, const buddy_view_t *v, int mood) {
    fill_rect(fb, 0, 0, W, H, COL_BG);
    draw_attention_date(fb, v);
    const buddy_layout_t *layout = ui_layout();
    const uint16_t label_color = strcmp(v->attention.kind, "failed") == 0 ? COL_DANGER : COL_WARN;
    int extra = 0;
    if (strcmp(v->attention.kind, "paused") == 0) extra = v->counts.paused;
    else if (strcmp(v->attention.kind, "failed") == 0) extra = v->counts.failed;
    else extra = v->counts.pending;
    char label[32];
    if (extra > 1) snprintf(label, sizeof(label), "%s %d", attention_label(v), extra);
    else strlcpy(label, attention_label(v), sizeof(label));
    draw_centered_body(fb, 88, label, label_color, COL_BG, layout->title_w, 1);

    char line1[BUDDY_TITLE_BYTES + 8];
    char line2[BUDDY_TITLE_BYTES + 8];
    line2[0] = '\0';
    const size_t used =
        fit_text_body(line1, sizeof(line1), v->attention.title, layout->title_w, 1, false);
    if (v->attention.title[used]) {
        fit_text_body(line2, sizeof(line2), v->attention.title + used, layout->title_w, 1, true);
    }
    draw_centered_body(fb, 132, line1, COL_TEXT, COL_BG, layout->title_w, 1);
    if (line2[0]) draw_centered_body(fb, 160, line2, COL_TEXT, COL_BG, layout->title_w, 1);
    draw_pet_chip(fb, W / 2, 250, mood);
}

static void draw_pet(uint16_t *fb, int cx, int cy, int64_t now_ms, int mood) {
    const int phase = (int)((now_ms / (mood == BUDDY_MOOD_SLEEP ? 360 : 120)) % 8);
    int bob = (phase < 4) ? -phase / 2 : -(7 - phase) / 2;
    if (mood == BUDDY_MOOD_SLEEP) bob = 2;
    if (mood == BUDDY_MOOD_HAPPY) bob *= 2;

    const int blink_cycle = (int)(now_ms % 3200);
    const bool blink = mood == BUDDY_MOOD_SLEEP || blink_cycle > 3000;

    const int ear_y = cy - (mood == BUDDY_MOOD_SLEEP ? 36 : 44) + bob;
    fill_circle(fb, cx - 40, ear_y, 17, COL_PET_DARK);
    fill_circle(fb, cx + 40, ear_y, 17, COL_PET_DARK);
    fill_circle(fb, cx - 40, ear_y, 9, COL_PET_CHEEK);
    fill_circle(fb, cx + 40, ear_y, 9, COL_PET_CHEEK);

    fill_circle(fb, cx, cy + bob, 58, COL_PET);
    fill_ring(fb, cx, cy + bob, 58, 55, COL_PET_DARK);

    if (blink) {
        hline(fb, cx - 30, cx - 12, cy - 6 + bob, COL_BG);
        hline(fb, cx + 12, cx + 30, cy - 6 + bob, COL_BG);
    } else if (mood == BUDDY_MOOD_BUSY) {
        fill_circle(fb, cx - 21, cy - 6 + bob, 4, COL_BG);
        fill_circle(fb, cx + 21, cy - 6 + bob, 4, COL_BG);
    } else {
        fill_circle(fb, cx - 21, cy - 8 + bob, 7, COL_BG);
        fill_circle(fb, cx + 21, cy - 8 + bob, 7, COL_BG);
        fill_circle(fb, cx - 19, cy - 10 + bob, 2, COL_TEXT);
        fill_circle(fb, cx + 23, cy - 10 + bob, 2, COL_TEXT);
    }

    const int cheek = mood == BUDDY_MOOD_HAPPY ? 8 : 6;
    fill_circle(fb, cx - 33, cy + 14 + bob, cheek, COL_PET_CHEEK);
    fill_circle(fb, cx + 33, cy + 14 + bob, cheek, COL_PET_CHEEK);

    if (mood == BUDDY_MOOD_SLEEP || mood == BUDDY_MOOD_BUSY) {
        hline(fb, cx - 8, cx + 8, cy + 22 + bob, COL_BG);
    } else if (mood == BUDDY_MOOD_HAPPY) {
        hline(fb, cx - 14, cx + 14, cy + 18 + bob, COL_BG);
        hline(fb, cx - 10, cx + 10, cy + 22 + bob, COL_BG);
        hline(fb, cx - 4, cx + 4, cy + 26 + bob, COL_BG);
    } else {
        hline(fb, cx - 10, cx + 10, cy + 20 + bob, COL_BG);
        hline(fb, cx - 6, cx + 6, cy + 24 + bob, COL_BG);
    }
}

/** Draws a 24x24 1-bit cell rotated around its centre, magnified by `zoom`.
 * `zoom` > 1 samples the cell with nearest-neighbour, so the packed 24px tables
 * can render larger on the arc without shipping a second font. */
static void draw_glyph_rotated(uint16_t *fb, int cx, int cy, const uint8_t cell[24 * 24],
                               float angle, uint16_t fg, float zoom) {
    if (!(zoom > 0.0f)) zoom = 1.0f;
    const float ca = cosf(angle);
    const float sa = sinf(angle);
    const int reach = (int)(17.0f * zoom) + 1;
    for (int dy = -reach; dy <= reach; ++dy) {
        for (int dx = -reach; dx <= reach; ++dx) {
            const float lx = (ca * dx + sa * dy) / zoom;
            const float ly = (-sa * dx + ca * dy) / zoom;
            const int ix = (int)(lx + 12.5f);
            const int iy = (int)(ly + 12.5f);
            if (ix < 0 || ix >= 24 || iy < 0 || iy >= 24) continue;
            if (cell[iy * 24 + ix]) put_px(fb, cx + dx, cy + dy, fg);
        }
    }
}

/** Curved text along the bottom of the dial. Characters are upright at the
 * bottom centre and rotate with the arc; tops point toward the centre. */
static void draw_arc_text_bottom(uint16_t *fb, const char *text, int radius, uint16_t fg,
                                 float zoom) {
    if (!text || !*text || radius <= 0) return;
    if (!(zoom > 0.0f)) zoom = 1.0f;
    const int width = (int)lroundf((float)buddy_font_arc_width(text) * zoom);
    if (width <= 0) return;
    const float span = (float)width / (float)radius;
    float angle = 1.5707963f + span * 0.5f;
    int pen = 0;
    const char *p = text;
    while (*p) {
        uint32_t cp = 0;
        const int len = buddy_font_utf8_next(p, &cp);
        if (len <= 0) break;
        p += len;
        const int adv = (int)lroundf((float)buddy_font_arc_advance(cp) * zoom);
        const float center = angle - (float)(pen + adv / 2) / (float)radius;
        uint8_t cell[24 * 24];
        if (!buddy_font_cell(cp, cell)) {
            memset(cell, 0, sizeof(cell));
            for (int i = 2; i < 22; ++i) {
                cell[2 * 24 + i] = 1;
                cell[21 * 24 + i] = 1;
                cell[i * 24 + 2] = 1;
                cell[i * 24 + 21] = 1;
            }
        }
        const int px = W / 2 + (int)lroundf(cosf(center) * (float)radius);
        const int py = H / 2 + (int)lroundf(sinf(center) * (float)radius);
        draw_glyph_rotated(fb, px, py, cell, center - 1.5707963f, fg, zoom);
        pen += adv;
    }
}

/** Curved text along the top of the dial. Characters are upright at the top
 * centre and follow the arc; tops point away from the centre. y_offset shifts
 * the whole ring vertically without changing its radius. */
static void draw_arc_text_top(uint16_t *fb, const char *text, int radius, int y_offset,
                              uint16_t fg) {
    if (!text || !*text || radius <= 0) return;
    const int width = buddy_font_arc_width(text);
    if (width <= 0) return;
    const float span = (float)width / (float)radius;
    float angle = -1.5707963f - span * 0.5f;
    int pen = 0;
    const char *p = text;
    while (*p) {
        uint32_t cp = 0;
        const int len = buddy_font_utf8_next(p, &cp);
        if (len <= 0) break;
        p += len;
        const int adv = buddy_font_arc_advance(cp);
        const float center = angle + (float)(pen + adv / 2) / (float)radius;
        uint8_t cell[24 * 24];
        if (!buddy_font_cell(cp, cell)) {
            memset(cell, 0, sizeof(cell));
            for (int i = 2; i < 22; ++i) {
                cell[2 * 24 + i] = 1;
                cell[21 * 24 + i] = 1;
                cell[i * 24 + 2] = 1;
                cell[i * 24 + 21] = 1;
            }
        }
        const int px = W / 2 + (int)lroundf(cosf(center) * (float)radius);
        const int py = H / 2 + y_offset + (int)lroundf(sinf(center) * (float)radius);
        draw_glyph_rotated(fb, px, py, cell, center + 1.5707963f, fg, 1.0f);
        pen += adv;
    }
}

/** Scales one 128x128 RGB565+A8 frame to `target` and alpha-blends it. */
static void draw_pet_asset_frame(uint16_t *fb, int cx, int cy, int target,
                                 const uint8_t *src) {
    const int sw = BUDDY_PET_WIDTH;
    const int sh = BUDDY_PET_HEIGHT;
    for (int dy = 0; dy < target; ++dy) {
        const int sy = dy * sh / target;
        const uint8_t *row = src + (size_t)sy * sw * 3;
        for (int dx = 0; dx < target; ++dx) {
            const int sx = dx * sw / target;
            const uint8_t *p = row + (size_t)sx * 3;
            const int a = p[2];
            if (a == 0) continue;
            const int x = cx - target / 2 + dx;
            const int y = cy - target / 2 + dy;
            if (x < 0 || y < 0 || x >= W || y >= H) continue;
            const uint16_t rgb = (uint16_t)(p[0] | ((uint16_t)p[1] << 8));
            if (a >= 255) {
                fb[y * W + x] = buddy_color_swap(rgb);
                continue;
            }
            const uint16_t dst = buddy_color_swap(fb[y * W + x]);
            const int dr = (dst >> 11) & 0x1F, dg = (dst >> 5) & 0x3F, db = dst & 0x1F;
            const int sr = (rgb >> 11) & 0x1F, sg = (rgb >> 5) & 0x3F, sb = rgb & 0x1F;
            const int r = (sr * a + dr * (255 - a) + 127) / 255;
            const int g = (sg * a + dg * (255 - a) + 127) / 255;
            const int b = (sb * a + db * (255 - a) + 127) / 255;
            fb[y * W + x] = buddy_color_swap((uint16_t)((r << 11) | (g << 5) | b));
        }
    }
}

/** Fits a UTF-8 string to an arc width (ASCII advances 18, CJK 24). */
static size_t fit_text_arc(char *dst, size_t dst_size, const char *src, int max_px,
                           bool ellipsis) {
    if (!dst || dst_size == 0) return 0;
    dst[0] = '\0';
    if (!src) return 0;
    const char *ell = "\xE2\x80\xA6";
    const int ell_adv = ellipsis ? buddy_font_arc_advance(0x2026) : 0;
    size_t out = 0;
    int pen = 0;
    const char *p = src;
    while (*p) {
        uint32_t cp = 0;
        const int len = buddy_font_utf8_next(p, &cp);
        if (len <= 0) break;
        const int adv = buddy_font_arc_advance(cp);
        if (pen + adv > max_px - ell_adv) {
            if (ellipsis && out + 3 < dst_size) {
                memcpy(dst + out, ell, 3);
                out += 3;
            }
            dst[out] = '\0';
            return (size_t)(p - src);
        }
        if (out + (size_t)len >= dst_size) break;
        memcpy(dst + out, p, (size_t)len);
        out += (size_t)len;
        pen += adv;
        p += len;
    }
    dst[out] = '\0';
    return (size_t)(p - src);
}

/** The clock's date uses ASCII digits, which are 7 px tall at scale 1 next to
 * the 24 px CJK glyphs for 月/日. Map them to U+FF10..U+FF19 (present in the
 * ext font) so one line has one visual weight. */
static void widen_ascii_digits(char *dst, size_t dst_size, const char *src) {
    size_t out = 0;
    for (const unsigned char *p = (const unsigned char *)src; *p; ++p) {
        const bool digit = *p >= '0' && *p <= '9';
        const size_t need = digit ? 3 : 1;
        if (out + need >= dst_size) break;
        if (digit) {
            dst[out++] = (char)0xEF;
            dst[out++] = (char)0xBC;
            dst[out++] = (char)(0x90 + (*p - '0'));
        } else {
            dst[out++] = (char)*p;
        }
    }
    dst[out] = '\0';
}

/** Phase name and age along the top. The counters, title and bar already fill
 * the middle, so this replaces the brand line instead of crowding the track. */
static void draw_task_banner(uint16_t *fb, const buddy_view_t *v, int active) {
    const buddy_layout_t *layout = ui_layout();
    char raw[BUDDY_PHASE_BYTES + BUDDY_ELAPSED_BYTES + 8];
    raw[0] = '\0';
    if (v && v->current_count > 0 && active >= 0 && active < v->current_count) {
        const buddy_task_t *task = &v->current[active];
        if (task->phase[0] && task->elapsed[0]) {
            strlcpy(raw, task->phase, sizeof(raw));
            strlcat(raw, " · ", sizeof(raw));
            strlcat(raw, task->elapsed, sizeof(raw));
        } else if (task->phase[0]) {
            strlcpy(raw, task->phase, sizeof(raw));
        } else if (task->elapsed[0]) {
            strlcpy(raw, task->elapsed, sizeof(raw));
        }
    }
    if (!raw[0]) {
        draw_centered_body(fb, layout->header_y, "MaClaw", COL_MUTED, COL_BG, 220, 2);
        return;
    }
    char wide[160];
    widen_ascii_digits(wide, sizeof(wide), raw);
    if (layout->arc_standby) {
        char fit[160];
        fit_text_arc(fit, sizeof(fit), wide, 220, true);
        draw_arc_text_top(fb, fit, 138, -3, COL_MUTED);
        return;
    }
    char fit[160];
    fit_text_body(fit, sizeof(fit), wide, 220, 1, true);
    draw_centered_body(fb, layout->header_y, fit, COL_MUTED, COL_BG, 220, 1);
}

/** The attention page replaces standby, so the date the host already sends
 * stays on the rim. No date yet leaves that rim empty. */
static void draw_attention_date(uint16_t *fb, const buddy_view_t *v) {
    if (!v || !v->has_clock || !v->date[0]) return;
    const buddy_layout_t *layout = ui_layout();
    char wide[96];
    widen_ascii_digits(wide, sizeof(wide), v->date);
    char line[128];
    strlcpy(line, wide, sizeof(line));
    if (v->weekday[0]) {
        strlcat(line, " ", sizeof(line));
        strlcat(line, v->weekday, sizeof(line));
    }
    if (layout->arc_standby) {
        char fit[128];
        fit_text_arc(fit, sizeof(fit), line, 250, true);
        draw_arc_text_top(fb, fit, 138, -3, COL_TEXT);
        return;
    }
    // Same rim line as the standby page, so the date keeps one size across pages.
    char fit[128];
    fit_text_cells(fit, sizeof(fit), line, STANDBY_TITLE_ZOOM, layout->title_w, true);
    draw_centered_cells(fb, layout->date_y, fit, COL_TEXT, COL_BG, STANDBY_TITLE_ZOOM, layout->title_w);
}

static void draw_standby_page(uint16_t *fb, const buddy_view_t *v, int64_t now_ms, int mood, bool muted,
                              bool pet_only) {
    // A pet tick keeps the date and weather pixels already in the framebuffer.
    // The 176px asset is y=82..257; clearing through y=78 also wipes the bottom
    // of the status line, which is painted again after the pet.
    if (pet_only) {
        fill_rect(fb, 0, 78, W, 180, COL_BG);
    } else {
        fill_rect(fb, 0, 0, W, H, COL_BG);
    }
    const buddy_layout_t *layout = ui_layout();
    if (!pet_only) {

    char date_line[128];
    date_line[0] = '\0';
    if (v->has_clock) {
        char wide_date[96];
        widen_ascii_digits(wide_date, sizeof(wide_date), v->date);
        snprintf(date_line, sizeof(date_line), "%s %s", wide_date, v->weekday);
    }
    if (layout->arc_standby) {
        if (date_line[0]) {
            char date_fit[128];
            fit_text_arc(date_fit, sizeof(date_fit), date_line, 250, true);
            draw_arc_text_top(fb, date_fit, 138, -3, COL_TEXT);
        } else {
            draw_arc_text_top(fb, "MaClaw Buddy", 138, -3, COL_TEXT);
        }
    } else if (date_line[0]) {
        char date_fit[128];
        fit_text_cells(date_fit, sizeof(date_fit), date_line, STANDBY_TITLE_ZOOM, layout->title_w, true);
        draw_centered_cells(fb, layout->date_y, date_fit, COL_TEXT, COL_BG, STANDBY_TITLE_ZOOM,
                            layout->title_w);
    } else {
        draw_centered_cells(fb, layout->date_y, "MaClaw Buddy", COL_TEXT, COL_BG,
                            STANDBY_TITLE_ZOOM, layout->title_w);
    }
    }

    // Pet: the GUI's animated asset when the host has delivered it, otherwise
    // the built-in vector mascot. Sleep has no matching frame in that asset,
    // so the closed-eye pose is always the vector one. The asset's bottom edge
    // stays at y=258 while the top moves up.
    if (mood != BUDDY_MOOD_SLEEP && buddy_pet_ready()) {
        buddy_pet_lock();
        const int count = buddy_pet_frame_count();
        const int frame_ms = buddy_mood_frame_ms(mood, buddy_pet_frame_ms());
        const int index = count > 0 ? (int)((now_ms / frame_ms) % count) : 0;
        const uint8_t *frame = buddy_pet_frame(index);
        if (frame) {
            draw_pet_asset_frame(fb, W / 2, 170, 176, frame);
        } else {
            draw_pet(fb, W / 2, 170, now_ms, mood);
        }
        buddy_pet_unlock();
    } else {
        draw_pet(fb, W / 2, 170, now_ms, mood);
    }

    // The 176px asset reaches y=82, which covers the bottom of the status band.
    // Paint the status after the pet so it stays readable. The round glass draws
    // the canvas 1:1 and keeps its 24px line at the old origin; only the sampled
    // 240 panel uses the enlarged 16px-table label.
    const int status_y = layout->arc_standby ? STANDBY_STATUS_Y_ROUND : STANDBY_STATUS_Y;
    if (!v->online) {
        draw_status_line(fb, status_y, muted ? "未连接 · 静音" : "未连接", COL_MUTED, layout->title_w);
    } else if (v->counts.pending > 0) {
        char raw[40];
        char buf[64];
        snprintf(raw, sizeof(raw), muted ? "待确认 %d · 静音" : "待确认 %d", v->counts.pending);
        widen_ascii_digits(buf, sizeof(buf), raw);
        draw_status_line(fb, status_y, buf, COL_WARN, layout->title_w);
    } else if (muted) {
        draw_status_line(fb, status_y, "静音", COL_WARN, layout->title_w);
    }

    if (pet_only) return;

    // Weather and location live on two bottom arcs, leaving the centre free
    // for the pet. The weather line is the primary reading, so it renders its
    // 24px cells magnified (1.25x) and sits closer to the rim; the location
    // arc moves up to keep a clear gap between the two bands.
    if (v->has_weather) {
        char wx[96];
        snprintf(wx, sizeof(wx), "%s%s %d°C", v->weather_stale ? "旧 " : "",
                 v->weather_summary, v->weather_temp);
        char wx_fit[96];
        if (layout->arc_standby) {
            fit_text_arc(wx_fit, sizeof(wx_fit), wx, 170, true);
            draw_arc_text_bottom(fb, wx_fit, 133, COL_ACCENT, WEATHER_TEXT_ZOOM);
            if (v->weather_location[0]) {
                char loc[64];
                fit_text_arc(loc, sizeof(loc), v->weather_location, 150, true);
                draw_arc_text_bottom(fb, loc, 97, COL_MUTED, 1.0f);
            }
        } else {
            fit_text_small(wx_fit, sizeof(wx_fit), wx, STANDBY_LABEL_ZOOM, layout->title_w, true);
            draw_centered_small(fb, layout->weather_y, wx_fit, COL_ACCENT, COL_BG, STANDBY_LABEL_ZOOM,
                                layout->title_w);
            if (v->weather_location[0]) {
                char loc[64];
                fit_text_small(loc, sizeof(loc), v->weather_location, STANDBY_LABEL_ZOOM,
                               layout->title_w, true);
                draw_centered_small(fb, layout->location_y, loc, COL_MUTED, COL_BG,
                                    STANDBY_LABEL_ZOOM, layout->title_w);
            }
        }
    } else if (layout->arc_standby) {
        draw_arc_text_bottom(fb, "正在获取天气", 133, COL_MUTED, WEATHER_TEXT_ZOOM);
    } else {
        draw_centered_small(fb, layout->weather_y, "正在获取天气", COL_MUTED, COL_BG,
                            STANDBY_LABEL_ZOOM, layout->title_w);
    }
}

/** Deterministic hash used by the firework particles (no RNG state). */
static float fw_rand(uint32_t seed) {
    seed ^= seed >> 16;
    seed *= 0x7feb352du;
    seed ^= seed >> 15;
    seed *= 0x846ca68bu;
    seed ^= seed >> 16;
    return (float)(seed & 0xFFFFu) / 65535.0f;
}

/** Scales a byte-swapped RGB565 colour toward black. */
static uint16_t dim_swapped(uint16_t swapped, int factor) {
    if (factor <= 0) return COL_BG;
    if (factor > 255) factor = 255;
    const uint16_t v = buddy_color_swap(swapped);
    int r = (v >> 11) & 0x1F;
    int g = (v >> 5) & 0x3F;
    int b = v & 0x1F;
    r = r * factor / 255;
    g = g * factor / 255;
    b = b * factor / 255;
    return buddy_color_swap((uint16_t)((r << 11) | (g << 5) | b));
}

/** Bresenham spark trail between two particle positions. */
static void draw_trail(uint16_t *fb, int x0, int y0, int x1, int y1, uint16_t color) {
    int dx = x1 > x0 ? x1 - x0 : x0 - x1;
    int sx = x0 < x1 ? 1 : -1;
    int dy = y1 > y0 ? y0 - y1 : y1 - y0;
    int sy = y0 < y1 ? 1 : -1;
    int err = dx + dy;
    for (int guard = 0; guard < 400; ++guard) {
        put_px(fb, x0, y0, color);
        if (x0 == x1 && y0 == y1) break;
        const int e2 = 2 * err;
        if (e2 >= dy) {
            err += dy;
            x0 += sx;
        }
        if (e2 <= dx) {
            err += dx;
            y0 += sy;
        }
    }
}

static void draw_celebrate_page(uint16_t *fb, const buddy_view_t *v, int64_t now_ms) {
    const int64_t elapsed = now_ms - v->celebrate_start_ms;
    // The UI tick and this draw sample the clock a moment apart. Clearing the
    // panel when the draw is the one that crosses the deadline flashes black
    // for a frame; keep the last fireworks instead.
    if (elapsed < 0 || elapsed > (int64_t)v->celebrate_seconds * 1000) return;
    fill_rect(fb, 0, 0, W, H, COL_BG);

    // Rich firework field: 12 shells of five shapes, each with a rising
    // rocket, a burst flash, a shock ring and 16-34 particles with drag,
    // gravity, spark trails and a decaying tail.
    typedef struct {
        int x;
        int y;
        int launch_ms;
        uint8_t type;
        uint8_t palette;
    } shell_t;
    enum { FW_PEONY, FW_RING, FW_WILLOW, FW_PALM, FW_DOUBLE };
    const uint16_t c_accent = COL_ACCENT;
    const uint16_t c_success = COL_SUCCESS;
    const uint16_t c_warn = COL_WARN;
    const uint16_t c_cheek = COL_PET_CHEEK;
    const uint16_t c_text = COL_TEXT;
    const uint16_t palettes[3][4] = {
        {c_accent, c_text, c_success, c_warn},
        {c_cheek, c_warn, c_text, c_accent},
        {c_success, c_accent, c_text, c_cheek},
    };
    static const shell_t shells[] = {
        {112, 148, 0, FW_PEONY, 0},     {248, 126, 320, FW_RING, 1},
        {180, 96, 640, FW_PEONY, 2},    {92, 196, 1150, FW_WILLOW, 1},
        {268, 184, 1420, FW_PALM, 0},   {180, 150, 1720, FW_RING, 2},
        {136, 108, 2300, FW_DOUBLE, 0}, {236, 140, 2520, FW_PEONY, 1},
        {180, 112, 2920, FW_PALM, 2},   {108, 172, 3320, FW_WILLOW, 0},
        {252, 168, 3540, FW_RING, 1},   {180, 124, 3840, FW_DOUBLE, 2},
    };

    // Sparse twinkling star field behind the shells.
    for (int star = 0; star < 34; ++star) {
        const int sx = 20 + (int)(fw_rand(0x51u + (uint32_t)star * 7u) * (float)(W - 40));
        const int sy = 16 + (int)(fw_rand(0x9Au + (uint32_t)star * 13u) * (float)(H - 32));
        if (((star + (int)(now_ms / 350)) % 7) == 0) continue;
        put_px(fb, sx, sy, dim_swapped(c_text, 30 + (star * 11) % 45));
    }

    for (size_t si = 0; si < sizeof(shells) / sizeof(shells[0]); ++si) {
        const shell_t *shell = &shells[si];
        const int64_t e = elapsed - shell->launch_ms;
        if (e < 0) continue;
        const int rise_ms = 500;
        if (e < rise_ms) {
            const float k = (float)e / (float)rise_ms;
            const int y = 320 - (int)((320 - shell->y) * k);
            fill_rect(fb, shell->x - 1, y, 3, 7, c_text);
            put_px(fb, shell->x, y - 2, c_warn);
            for (int tail = 1; tail <= 3; ++tail) {
                const int ty = y + tail * 9;
                if (ty < H) {
                    put_px(fb, shell->x + (tail & 1), ty,
                           dim_swapped(c_warn, 150 - tail * 35));
                }
            }
            continue;
        }

        const int pe = (int)(e - rise_ms);
        int life = 1300;
        if (shell->type == FW_WILLOW) life = 1600;
        else if (shell->type == FW_PALM) life = 1400;
        else if (shell->type == FW_RING) life = 1200;
        if (pe > life) continue;

        if (pe < 140) {
            const int radius = 16 - pe / 12;
            if (radius > 0) {
                fill_circle(fb, shell->x, shell->y, radius,
                            dim_swapped(c_text, 255 - pe));
            }
        }
        if (pe < 320) {
            const int rr = 10 + pe / 4;
            const int inner = rr > 3 ? rr - 3 : 1;
            fill_ring(fb, shell->x, shell->y, rr, inner,
                      dim_swapped(palettes[shell->palette][0], 220 - pe / 2));
        }

        int count = 30;
        if (shell->type == FW_WILLOW) count = 22;
        else if (shell->type == FW_PALM) count = 16;
        else if (shell->type == FW_RING) count = 24;
        else if (shell->type == FW_DOUBLE) count = 34;

        const float t = (float)pe / 1000.0f;
        const float drag = shell->type == FW_WILLOW ? 1.2f : 2.0f;
        const float travel = (1.0f - expf(-drag * t)) / drag;
        const float tp = t > 0.045f ? t - 0.045f : 0.0f;
        const float travel_prev = (1.0f - expf(-drag * tp)) / drag;
        const float gravity = shell->type == FW_WILLOW ? 150.0f
                              : (shell->type == FW_PALM ? 100.0f : 78.0f);
        const float life_f = 1.0f - (float)pe / (float)life;

        for (int p = 0; p < count; ++p) {
            const uint32_t seed = (uint32_t)(si * 97u + (uint32_t)p * 13u +
                                             (uint32_t)shell->type * 7u + 1u);
            const float r1 = fw_rand(seed);
            const float r2 = fw_rand(seed ^ 0x5bd1e995u);
            float ang;
            float speed;
            if (shell->type == FW_RING) {
                ang = (float)p * 6.2831853f / (float)count;
                speed = 64.0f + r1 * 8.0f;
            } else if (shell->type == FW_PALM) {
                ang = -1.5707963f + (r1 - 0.5f) * 0.7f;
                speed = 90.0f + r2 * 45.0f;
            } else if (shell->type == FW_WILLOW) {
                ang = (float)p * 6.2831853f / (float)count + (r1 - 0.5f) * 0.25f;
                speed = 38.0f + r2 * 18.0f;
            } else if (shell->type == FW_DOUBLE) {
                const int half = count / 2;
                ang = (float)(p % half) * 6.2831853f / (float)half;
                speed = (p < half ? 48.0f : 82.0f) + r1 * 6.0f;
            } else {
                ang = (float)p * 6.2831853f / (float)count + (r1 - 0.5f) * 0.5f;
                speed = 55.0f + r2 * 45.0f;
            }
            const int x = shell->x + (int)(cosf(ang) * speed * travel);
            const int y = shell->y +
                          (int)(sinf(ang) * speed * travel + 0.5f * gravity * t * t);
            uint16_t color = palettes[shell->palette][p & 3];
            if (life_f < 0.35f) color = dim_swapped(color, (int)(life_f * 255.0f));
            if (shell->type == FW_WILLOW || shell->type == FW_PALM) {
                const int px0 = shell->x + (int)(cosf(ang) * speed * travel_prev);
                const int py0 = shell->y +
                                (int)(sinf(ang) * speed * travel_prev +
                                      0.5f * gravity * tp * tp);
                draw_trail(fb, px0, py0, x, y, dim_swapped(color, 120));
            }
            fill_circle(fb, x, y, life_f > 0.55f ? 2 : 1, color);
            if (life_f > 0.4f && ((p + (int)(now_ms / 90)) % 5) == 0) {
                put_px(fb, x + 1, y - 1, c_text);
            }
        }

        if (pe > 550 && pe < 950) {
            for (int crack = 0; crack < 8; ++crack) {
                const uint32_t seed = (uint32_t)(si * 31u + (uint32_t)crack * 17u + 5u);
                const float ra = fw_rand(seed) * 6.2831853f;
                const float rd = 18.0f + fw_rand(seed ^ 0xA5u) * 34.0f;
                const int cx2 = shell->x + (int)(cosf(ra) * rd);
                const int cy2 = shell->y + (int)(sinf(ra) * rd);
                put_px(fb, cx2, cy2, dim_swapped(c_text, 255 - (pe - 550) / 2));
            }
        }
    }

    // The message is visible for the first 2.2s, then only fireworks remain.
    if (elapsed < 2200) {
        draw_centered_body(fb, 92, "✓ 任务完成", COL_ACCENT, COL_BG, 300, 1);
        char line1[BUDDY_TITLE_BYTES + 8];
        char line2[BUDDY_TITLE_BYTES + 8];
        line2[0] = '\0';
        const int title_w = ui_layout()->title_w;
        const size_t used =
            fit_text_body(line1, sizeof(line1), v->celebrate_title, title_w, 1, false);
        if (used < strlen(v->celebrate_title)) {
            fit_text_body(line2, sizeof(line2), v->celebrate_title + used, title_w, 1, true);
        }
        draw_centered_body(fb, 128, line1, COL_TEXT, COL_BG, title_w, 1);
        if (line2[0]) draw_centered_body(fb, 156, line2, COL_TEXT, COL_BG, title_w, 1);
    }
}

static void draw_prompt_page(uint16_t *fb, const buddy_view_t *v) {
    fill_rect(fb, 0, 0, W, H, COL_BG);
    draw_centered_body(fb, 140, "MaClaw Buddy", COL_ACCENT, COL_BG, 300, 2);
    char line1[80];
    char line2[80];
    // title_w keeps the second line (y=228, 24px tall) inside the round
    // aperture. W-60 reached past r=150 at that height.
    const int prompt_w = ui_layout()->title_w;
    const char *prompt = v->prompt[0] ? v->prompt : "等待主机连接";
    const size_t used = fit_text_body(line1, sizeof(line1), prompt, prompt_w, 1, false);
    line2[0] = '\0';
    if (prompt[used]) fit_text_body(line2, sizeof(line2), prompt + used, prompt_w, 1, true);
    draw_centered_body(fb, 200, line1, COL_TEXT, COL_BG, prompt_w, 1);
    if (line2[0]) draw_centered_body(fb, 228, line2, COL_TEXT, COL_BG, prompt_w, 1);
}

void buddy_ui_render(const buddy_view_t *v, int64_t now_ms) {
    uint16_t *fb = buddy_display_framebuffer();
    if (!fb || !v) return;

    // The panel keeps its last pixels while the backlight is off. Composing
    // a 360x360 frame into a dark panel only burns CPU and QSPI.
    static int64_t s_last_anim_ms;
    if (!buddy_display_awake()) {
        s_asleep = true;
        return;
    }
    if (s_asleep) {
        s_asleep = false;
        s_last_page = -1;
        s_last_anim_ms = 0;
    }

    const int page = page_of(v);
    const int active = active_task_index(v, now_ms);
    const int mood = buddy_mood_resolve(v->pet_explicit, page, v->online, buddy_display_resting());
    buddy_sound_config_t sound_cfg;
    memset(&sound_cfg, 0, sizeof(sound_cfg));
    sound_cfg.master = true;
    buddy_sound_get_config(&sound_cfg);
    const bool muted = !sound_cfg.master;
    // The celebration page is a 5s animation; draw it on every tick (the UI
    // task runs at 20fps) and let the state tick expire it.
    if (page == BUDDY_PAGE_CELEBRATE) {
        draw_celebrate_page(fb, v, now_ms);
        buddy_display_flush_rows(0, H);
        s_last_page = page;
        return;
    }
    // The pet uses a 120ms animation frame; redrawing a full 360x360 frame on
    // every tick would push several MB/s over QSPI for no visible gain.
    const bool anim_frame = now_ms - s_last_anim_ms >= 120;
    const bool counts_changed = memcmp(&s_last_counts, &v->counts, sizeof(s_last_counts)) != 0;
    const char *title = v->current_count > 0 ? v->current[active].title : "";
    const char *phase = v->current_count > 0 ? v->current[active].phase : "";
    const char *elapsed = v->current_count > 0 ? v->current[active].elapsed : "";
    const bool title_changed = strcmp(s_last_title, title) != 0;
    const bool detail_changed = strcmp(s_last_phase, phase) != 0 || strcmp(s_last_elapsed, elapsed) != 0;
    const bool weather_changed = v->has_weather != s_last_has_weather ||
                                 v->weather_stale != s_last_weather_stale ||
                                 v->weather_temp != s_last_temp ||
                                 strcmp(s_last_weather, v->weather_summary) != 0 ||
                                 strcmp(s_last_location, v->weather_location) != 0;
    const bool clock_changed = strcmp(s_last_date, v->date) != 0 ||
                               strcmp(s_last_weekday, v->weekday) != 0;
    const bool status_changed = s_last_online != v->online || s_last_stale != v->stale ||
                                s_last_page != page;
    const int fill = v->current_count > 0 ? (int)(v->current[active].progress * 100) : -100;
    const bool fill_changed = fill != s_last_fill;
    const bool rotation_changed = s_last_active != active;
    const bool pin_changed = buddy_task_cursor_pinned(&s_cursor) != s_last_pinned;
    const bool mood_changed = mood != s_last_mood;
    const bool mute_changed = muted != s_last_muted;
    const bool prompt_changed = strcmp(s_last_prompt, v->prompt) != 0;
    const bool attention_changed = strcmp(s_last_attention_kind, v->attention.kind) != 0 ||
                                   strcmp(s_last_attention_title, v->attention.title) != 0;

    // Standby animates the pet at ~8 fps; the task page repaints only when a
    // visible value changed or the three-second rotation advances.
    // The sleep pose does not move. A dark glass is not redrawn either: the
    // backlight is off, so a busy pet would only keep the panel bus awake.
    const bool animate = page == BUDDY_PAGE_STANDBY && anim_frame && mood != BUDDY_MOOD_SLEEP &&
                         buddy_display_awake();
    if (page == BUDDY_PAGE_STANDBY && !animate && !status_changed && !weather_changed &&
        !clock_changed && !counts_changed && !mood_changed && !mute_changed) {
        return;
    }
    // A minute tick must not repaint the task page, or the progress track
    // hitches. The attention page does redraw: its date lives on that frame.
    const bool content_changed = counts_changed || title_changed || detail_changed || status_changed ||
                                 fill_changed || rotation_changed || pin_changed || mood_changed ||
                                 mute_changed || prompt_changed || attention_changed ||
                                 (page == BUDDY_PAGE_ATTENTION && clock_changed);
    // Unknown progress stays an indeterminate block. A known ratio is a still
    // fill, so it does not take this path. Repaint just the track; a full
    // 360x360 frame here would keep the panel bus busy the whole time.
    if (page == BUDDY_PAGE_TASKS && !content_changed && v->current_count > 0 && fill < 0 &&
        !buddy_display_resting()) {
        draw_task_bar(fb, v, active, now_ms);
        buddy_display_flush_rows(298, 310);
        return;
    }
    if (!animate && page != BUDDY_PAGE_STANDBY && !content_changed) {
        return;
    }
    if (page == BUDDY_PAGE_STANDBY) {
        s_last_anim_ms = now_ms;
    }

    // The 176px pet covers y=82..257. A tick that only advances that pose leaves
    // the date and weather pixels already on the panel, and skips redrawing them.
    const bool pet_only = page == BUDDY_PAGE_STANDBY && animate && !status_changed &&
                          !weather_changed && !clock_changed && !counts_changed &&
                          !mood_changed && !mute_changed;
    switch (page) {
        case BUDDY_PAGE_TASKS:
            draw_task_page(fb, v, now_ms, muted, mood);
            break;
        case BUDDY_PAGE_ATTENTION:
            draw_attention_page(fb, v, mood);
            break;
        case BUDDY_PAGE_PROMPT:
            draw_prompt_page(fb, v);
            break;
        default:
            draw_standby_page(fb, v, now_ms, mood, muted, pet_only);
            break;
    }
    if (pet_only) {
        buddy_display_flush_rows(78, 262);
    } else {
        buddy_display_flush_rows(0, H);
    }

    memcpy(&s_last_counts, &v->counts, sizeof(s_last_counts));
    strlcpy(s_last_title, title, sizeof(s_last_title));
    strlcpy(s_last_phase, phase, sizeof(s_last_phase));
    strlcpy(s_last_elapsed, elapsed, sizeof(s_last_elapsed));
    if (page == BUDDY_PAGE_STANDBY) {
        strlcpy(s_last_weather, v->weather_summary, sizeof(s_last_weather));
        strlcpy(s_last_location, v->weather_location, sizeof(s_last_location));
        s_last_temp = v->weather_temp;
        s_last_weather_stale = v->weather_stale;
        s_last_has_weather = v->has_weather;
    }
    if (page == BUDDY_PAGE_STANDBY || page == BUDDY_PAGE_ATTENTION) {
        strlcpy(s_last_date, v->date, sizeof(s_last_date));
        strlcpy(s_last_weekday, v->weekday, sizeof(s_last_weekday));
    }
    s_last_page = page;
    strlcpy(s_last_prompt, v->prompt, sizeof(s_last_prompt));
    s_last_active = active;
    s_last_online = v->online;
    s_last_stale = v->stale;
    s_last_fill = fill;
    s_last_mood = mood;
    s_last_muted = muted;
    s_last_pinned = buddy_task_cursor_pinned(&s_cursor);
    strlcpy(s_last_attention_kind, v->attention.kind, sizeof(s_last_attention_kind));
    strlcpy(s_last_attention_title, v->attention.title, sizeof(s_last_attention_title));
}
