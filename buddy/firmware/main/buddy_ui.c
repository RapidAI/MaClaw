#include "buddy_ui.h"

#include <math.h>
#include <stdio.h>
#include <string.h>

#include "buddy_display.h"
#include "buddy_font.h"

#define W 360
#define H 360

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
static char s_last_weather[BUDDY_LABEL_BYTES + 1];
static char s_last_date[24];
static char s_last_weekday[16];
static int s_last_page = -1;
static int s_last_active = -1;
static bool s_last_online;
static bool s_last_stale;
static int s_last_fill = -100;

static void put_px(uint16_t *fb, int x, int y, uint16_t color) {
    if (x < 0 || y < 0 || x >= W || y >= H) return;
    fb[y * W + x] = color;
}

static void fill_rect(uint16_t *fb, int x, int y, int w, int h, uint16_t color) {
    for (int yy = y; yy < y + h; ++yy) {
        for (int xx = x; xx < x + w; ++xx) put_px(fb, xx, yy, color);
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

/** Draws a right-aligned ASCII value, e.g. the counter numbers. */
static void draw_value_right(uint16_t *fb, int right_x, int y, int value, uint16_t fg,
                             uint16_t bg, int scale) {
    char buf[16];
    snprintf(buf, sizeof(buf), "%d", value);
    const int width = buddy_font_width(buf, scale, W);
    draw_text(fb, right_x - width, y, buf, fg, bg, scale, W);
}

static int page_of(const buddy_view_t *v) { return v->prompt_active ? BUDDY_PAGE_PROMPT
                                        : (v->online && v->counts.in_progress > 0)
                                            ? BUDDY_PAGE_TASKS
                                            : BUDDY_PAGE_STANDBY; }

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
        default:
            return "standby";
    }
}

void buddy_ui_init(void) {
    memset(&s_last_counts, 0, sizeof(s_last_counts));
    s_last_title[0] = '\0';
    s_last_weather[0] = '\0';
    s_last_date[0] = '\0';
    s_last_weekday[0] = '\0';
    s_last_page = -1;
    s_last_active = -1;
    s_last_fill = -100;
}

/** Rotates the displayed in-progress task every three seconds. */
static int active_task_index(const buddy_view_t *v, int64_t now_ms) {
    if (!v || v->current_count <= 0) return 0;
    if (v->current_count == 1) return 0;
    return (int)((now_ms / 3000) % v->current_count);
}

static void draw_task_page(uint16_t *fb, const buddy_view_t *v, int64_t now_ms) {
    fill_rect(fb, 0, 0, W, H, COL_BG);
    const int active = active_task_index(v, now_ms);

    // Header stays inside the circle: the top corners are bezel, not pixels.
    draw_centered(fb, 36, "MaClaw", COL_MUTED, COL_BG, 2);

    // Three counters. The 360px panel's visible aperture is ~300px, so keep
    // content inside r=150 from (180,180): values end at x=270, labels at x=88.
    const int label_x = 88;
    const int value_right = 266;
    draw_text(fb, label_x, 74, "总任务", COL_TEXT, COL_BG, 1, 120);
    draw_value_right(fb, value_right, 62, v->counts.total, COL_TEXT, COL_BG, 4);

    draw_text(fb, label_x, 126, "进行中", COL_ACCENT, COL_BG, 1, 120);
    draw_value_right(fb, value_right, 114, v->counts.in_progress, COL_ACCENT, COL_BG, 4);

    draw_text(fb, label_x, 178, "已完成", COL_SUCCESS, COL_BG, 1, 120);
    draw_value_right(fb, value_right, 166, v->counts.done, COL_SUCCESS, COL_BG, 4);

    hline(fb, 90, 270, 204, COL_PANEL);

    // Current task title, two lines. 240px wide keeps the second line inside
    // the lower chord.
    const int text_x = 60;
    const int text_w = 240;
    char line1[BUDDY_TITLE_BYTES + 8];
    char line2[BUDDY_TITLE_BYTES + 8];
    line1[0] = '\0';
    line2[0] = '\0';
    if (v->current_count > 0 && v->current[active].title[0]) {
        const size_t used = fit_text(line1, sizeof(line1), v->current[active].title, 1, text_w, true);
        const char *rest = v->current[active].title + used;
        if (*rest) {
            fit_text(line2, sizeof(line2), rest, 1, text_w, true);
        }
    } else {
        strlcpy(line1, "当前没有进行中的任务", sizeof(line1));
    }
    draw_text(fb, text_x, 210, line1, COL_TEXT, COL_BG, 1, text_w);
    if (line2[0]) draw_text(fb, text_x, 238, line2, COL_TEXT, COL_BG, 1, text_w);

    // Progress bar, narrowed so its bottom corners stay inside the aperture.
    const int bar_x = 108;
    const int bar_y = 296;
    const int bar_w = 144;
    const int bar_h = 12;
    fill_rect(fb, bar_x, bar_y, bar_w, bar_h, COL_PANEL);
    float progress = v->current_count > 0 ? v->current[active].progress : -1;
    if (progress >= 0) {
        int fill_w = (int)(progress * (float)bar_w);
        if (fill_w < 0) fill_w = 0;
        if (fill_w > bar_w) fill_w = bar_w;
        fill_rect(fb, bar_x, bar_y, fill_w, bar_h, COL_ACCENT);
        char pct[8];
        snprintf(pct, sizeof(pct), "%d%%", (int)(progress * 100.0f + 0.5f));
        draw_centered(fb, 310, pct, COL_MUTED, COL_BG, 2);
    } else {
        // Indeterminate: a moving bright block.
        const int block = 30;
        const int travel = bar_w - block;
        const int pos = (int)((now_ms / 4) % (travel > 0 ? travel : 1));
        fill_rect(fb, bar_x + pos, bar_y, block, bar_h, COL_MUTED);
    }

    // One status line at the bottom. It combines at most two short pieces so
    // it stays inside the circle; stale is only shown when nothing else is.
    char tail[64];
    tail[0] = '\0';
    uint16_t tail_color = COL_MUTED;
    if (!v->online) {
        strlcpy(tail, "未连接", sizeof(tail));
        tail_color = COL_MUTED;
    } else {
        if (v->counts.failed > 0) {
            snprintf(tail, sizeof(tail), "异常 %d", v->counts.failed);
            tail_color = COL_DANGER;
        }
        if (v->counts.pending > 0) {
            char piece[24];
            snprintf(piece, sizeof(piece), "待处理 %d", v->counts.pending);
            if (tail[0]) strlcat(tail, " · ", sizeof(tail));
            strlcat(tail, piece, sizeof(tail));
            if (v->counts.failed == 0) tail_color = COL_WARN;
        }
        if (!tail[0] && v->stale) {
            strlcpy(tail, "数据陈旧", sizeof(tail));
            tail_color = COL_WARN;
        }
    }
    if (tail[0]) draw_centered(fb, 268, tail, tail_color, COL_BG, 1);
}

static void draw_pet(uint16_t *fb, int cx, int cy, int64_t now_ms) {
    const int phase = (int)((now_ms / 120) % 8);
    const int bob = (phase < 4) ? -phase / 2 : -(7 - phase) / 2;

    const int blink_cycle = (int)(now_ms % 3200);
    const bool blink = blink_cycle > 3000;

    // Ears.
    fill_circle(fb, cx - 40, cy - 44 + bob, 17, COL_PET_DARK);
    fill_circle(fb, cx + 40, cy - 44 + bob, 17, COL_PET_DARK);
    fill_circle(fb, cx - 40, cy - 44 + bob, 9, COL_PET_CHEEK);
    fill_circle(fb, cx + 40, cy - 44 + bob, 9, COL_PET_CHEEK);

    // Head.
    fill_circle(fb, cx, cy + bob, 58, COL_PET);
    fill_ring(fb, cx, cy + bob, 58, 55, COL_PET_DARK);

    // Eyes.
    if (blink) {
        hline(fb, cx - 30, cx - 12, cy - 6 + bob, COL_BG);
        hline(fb, cx + 12, cx + 30, cy - 6 + bob, COL_BG);
    } else {
        fill_circle(fb, cx - 21, cy - 8 + bob, 7, COL_BG);
        fill_circle(fb, cx + 21, cy - 8 + bob, 7, COL_BG);
        fill_circle(fb, cx - 19, cy - 10 + bob, 2, COL_TEXT);
        fill_circle(fb, cx + 23, cy - 10 + bob, 2, COL_TEXT);
    }

    // Cheeks.
    fill_circle(fb, cx - 33, cy + 14 + bob, 6, COL_PET_CHEEK);
    fill_circle(fb, cx + 33, cy + 14 + bob, 6, COL_PET_CHEEK);

    // Mouth: a small smile built from three dots rows.
    hline(fb, cx - 10, cx + 10, cy + 20 + bob, COL_BG);
    hline(fb, cx - 6, cx + 6, cy + 24 + bob, COL_BG);
}

static void draw_standby_page(uint16_t *fb, const buddy_view_t *v, int64_t now_ms) {
    fill_rect(fb, 0, 0, W, H, COL_BG);

    char date_line[48];
    if (v->has_clock) {
        snprintf(date_line, sizeof(date_line), "%s %s", v->date, v->weekday);
    } else {
        strlcpy(date_line, "MaClaw Buddy", sizeof(date_line));
    }
    draw_centered(fb, 48, date_line, COL_TEXT, COL_BG, 1);

    draw_pet(fb, W / 2, 146, now_ms);

    if (v->has_weather) {
        char summary[64];
        fit_text(summary, sizeof(summary), v->weather_summary, 1, 230, true);
        draw_centered(fb, 208, summary, COL_TEXT, COL_BG, 1);
        char temp[24];
        snprintf(temp, sizeof(temp), "%d", v->weather_temp);
        const int temp_w = buddy_font_width(temp, 3, W);
        const int cx = W / 2;
        const int temp_x = cx - (temp_w + 22) / 2;
        draw_text(fb, temp_x, 236, temp, COL_ACCENT, COL_BG, 3, 120);
        // Draw the degree ring and C with primitives so the unit matches the
        // digit height; a 24px glyph next to 21px digits looked oversized.
        fill_circle(fb, temp_x + temp_w + 5, 242, 3, COL_BG);
        fill_ring(fb, temp_x + temp_w + 5, 242, 3, 2, COL_ACCENT);
        draw_text(fb, temp_x + temp_w + 10, 238, "C", COL_ACCENT, COL_BG, 2, 24);
        if (v->weather_stale) {
            draw_text(fb, 222, 242, "旧", COL_WARN, COL_BG, 1, 40);
        }
    } else {
        draw_centered(fb, 240, "正在获取天气", COL_MUTED, COL_BG, 1);
    }

    if (v->has_weather && v->weather_location[0]) {
        // Keep the location inside the circle's lower chord.
        char loc[64];
        fit_text(loc, sizeof(loc), v->weather_location, 1, 190, true);
        draw_centered(fb, 262, loc, COL_MUTED, COL_BG, 1);
    }

    if (!v->online) {
        draw_centered(fb, 292, "未连接", COL_MUTED, COL_BG, 1);
    } else if (v->counts.pending > 0) {
        char buf[32];
        snprintf(buf, sizeof(buf), "待处理 %d", v->counts.pending);
        draw_centered(fb, 292, buf, COL_WARN, COL_BG, 1);
    }
}

static void draw_prompt_page(uint16_t *fb, const buddy_view_t *v) {
    fill_rect(fb, 0, 0, W, H, COL_BG);
    draw_centered(fb, 140, "MaClaw Buddy", COL_ACCENT, COL_BG, 2);
    char line1[80];
    char line2[80];
    fit_text(line1, sizeof(line1), v->prompt[0] ? v->prompt : "等待主机连接", 1, W - 60, false);
    line2[0] = '\0';
    if (v->prompt[0]) {
        const size_t used = strlen(line1);
        if (v->prompt[used]) fit_text(line2, sizeof(line2), v->prompt + used, 1, W - 60, true);
    }
    draw_centered(fb, 200, line1, COL_TEXT, COL_BG, 1);
    if (line2[0]) draw_centered(fb, 228, line2, COL_TEXT, COL_BG, 1);
}

void buddy_ui_render(const buddy_view_t *v, int64_t now_ms) {
    uint16_t *fb = buddy_display_framebuffer();
    if (!fb || !v) return;

    const int page = page_of(v);
    const int active = active_task_index(v, now_ms);
    // The pet uses a 120ms animation frame; redrawing a full 360x360 frame on
    // every 100ms tick would push ~2.6MB/s over QSPI for no visible gain.
    static int64_t s_last_anim_ms;
    const bool anim_frame = now_ms - s_last_anim_ms >= 120;
    if (page == BUDDY_PAGE_STANDBY && !anim_frame) {
        return;
    }
    if (page == BUDDY_PAGE_STANDBY) {
        s_last_anim_ms = now_ms;
    }
    const bool counts_changed = memcmp(&s_last_counts, &v->counts, sizeof(s_last_counts)) != 0;
    const char *title = v->current_count > 0 ? v->current[active].title : "";
    const bool title_changed = strcmp(s_last_title, title) != 0;
    const bool weather_changed = strcmp(s_last_weather, v->weather_summary) != 0;
    const bool clock_changed = strcmp(s_last_date, v->date) != 0 ||
                               strcmp(s_last_weekday, v->weekday) != 0;
    const bool status_changed = s_last_online != v->online || s_last_stale != v->stale ||
                                s_last_page != page;
    const int fill = v->current_count > 0 ? (int)(v->current[active].progress * 100) : -100;
    const bool fill_changed = fill != s_last_fill;
    const bool rotation_changed = s_last_active != active;

    // Standby animates the pet at ~8 fps; the task page repaints only when a
    // visible value changed or the three-second rotation advances.
    const bool animate = page == BUDDY_PAGE_STANDBY;
    if (!animate && !counts_changed && !title_changed && !weather_changed && !clock_changed &&
        !status_changed && !fill_changed && !rotation_changed) {
        return;
    }

    switch (page) {
        case BUDDY_PAGE_TASKS:
            draw_task_page(fb, v, now_ms);
            break;
        case BUDDY_PAGE_PROMPT:
            draw_prompt_page(fb, v);
            break;
        default:
            draw_standby_page(fb, v, now_ms);
            break;
    }
    buddy_display_flush_rows(0, H);

    memcpy(&s_last_counts, &v->counts, sizeof(s_last_counts));
    strlcpy(s_last_title, title, sizeof(s_last_title));
    strlcpy(s_last_weather, v->weather_summary, sizeof(s_last_weather));
    strlcpy(s_last_date, v->date, sizeof(s_last_date));
    strlcpy(s_last_weekday, v->weekday, sizeof(s_last_weekday));
    s_last_page = page;
    s_last_active = active;
    s_last_online = v->online;
    s_last_stale = v->stale;
    s_last_fill = fill;
}
