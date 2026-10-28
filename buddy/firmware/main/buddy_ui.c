#include "buddy_ui.h"

#include <math.h>
#include <stdio.h>
#include <string.h>

#include "buddy_display.h"
#include "buddy_font.h"
#include "buddy_pet.h"

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

static int page_of(const buddy_view_t *v) {
    if (v->prompt_active) return BUDDY_PAGE_PROMPT;
    if (v->celebrate_active) return BUDDY_PAGE_CELEBRATE;
    return (v->online && v->counts.in_progress > 0) ? BUDDY_PAGE_TASKS : BUDDY_PAGE_STANDBY;
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
    const int value_right = 262;
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
    draw_text(fb, text_x, 206, line1, COL_TEXT, COL_BG, 1, text_w);
    if (line2[0]) draw_text(fb, text_x, 234, line2, COL_TEXT, COL_BG, 1, text_w);

    // Progress bar, narrowed so its bottom corners stay inside the aperture.
    const int bar_x = 108;
    const int bar_y = 298;
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
        draw_centered(fb, 312, pct, COL_MUTED, COL_BG, 2);
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
    if (tail[0]) draw_centered(fb, 270, tail, tail_color, COL_BG, 1);
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

/** Draws a 24x24 1-bit cell rotated around its centre. */
static void draw_glyph_rotated(uint16_t *fb, int cx, int cy, const uint8_t cell[24 * 24],
                               float angle, uint16_t fg) {
    const float ca = cosf(angle);
    const float sa = sinf(angle);
    for (int dy = -17; dy <= 17; ++dy) {
        for (int dx = -17; dx <= 17; ++dx) {
            const float lx = ca * dx + sa * dy;
            const float ly = -sa * dx + ca * dy;
            const int ix = (int)(lx + 12.5f);
            const int iy = (int)(ly + 12.5f);
            if (ix < 0 || ix >= 24 || iy < 0 || iy >= 24) continue;
            if (cell[iy * 24 + ix]) put_px(fb, cx + dx, cy + dy, fg);
        }
    }
}

/** Curved text along the bottom of the dial. Characters are upright at the
 * bottom centre and rotate with the arc; tops point toward the centre. */
static void draw_arc_text_bottom(uint16_t *fb, const char *text, int radius, uint16_t fg) {
    if (!text || !*text || radius <= 0) return;
    const int width = buddy_font_arc_width(text);
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
        const int adv = buddy_font_arc_advance(cp);
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
        draw_glyph_rotated(fb, px, py, cell, center - 1.5707963f, fg);
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
        draw_glyph_rotated(fb, px, py, cell, center + 1.5707963f, fg);
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

static void draw_standby_page(uint16_t *fb, const buddy_view_t *v, int64_t now_ms) {
    fill_rect(fb, 0, 0, W, H, COL_BG);

    char date_line[128];
    if (v->has_clock) {
        char wide_date[96];
        widen_ascii_digits(wide_date, sizeof(wide_date), v->date);
        snprintf(date_line, sizeof(date_line), "%s %s", wide_date, v->weekday);
        char date_fit[128];
        fit_text_arc(date_fit, sizeof(date_fit), date_line, 250, true);
        draw_arc_text_top(fb, date_fit, 138, -3, COL_TEXT);
    } else {
        draw_arc_text_top(fb, "MaClaw Buddy", 138, -3, COL_TEXT);
    }

    if (!v->online) {
        draw_centered(fb, 61, "未连接", COL_MUTED, COL_BG, 1);
    } else if (v->counts.pending > 0) {
        char raw[32];
        char buf[64];
        snprintf(raw, sizeof(raw), "待处理 %d", v->counts.pending);
        widen_ascii_digits(buf, sizeof(buf), raw);
        draw_centered(fb, 61, buf, COL_WARN, COL_BG, 1);
    }

    // Pet: the GUI's animated asset when the host has delivered it, otherwise
    // the built-in vector mascot. The bottom edge stays at y=258 while the top
    // moves up, so the larger pet grows into the space freed above it.
    if (buddy_pet_ready()) {
        buddy_pet_lock();
        const int count = buddy_pet_frame_count();
        const int frame_ms = buddy_pet_frame_ms();
        const int index = count > 0 ? (int)((now_ms / frame_ms) % count) : 0;
        const uint8_t *frame = buddy_pet_frame(index);
        if (frame) {
            draw_pet_asset_frame(fb, W / 2, 170, 176, frame);
        } else {
            draw_pet(fb, W / 2, 170, now_ms);
        }
        buddy_pet_unlock();
    } else {
        draw_pet(fb, W / 2, 170, now_ms);
    }

    // Weather and location live on two bottom arcs, leaving the centre free
    // for the pet.
    if (v->has_weather) {
        char wx[96];
        snprintf(wx, sizeof(wx), "%s%s %d°C", v->weather_stale ? "旧 " : "",
                 v->weather_summary, v->weather_temp);
        char wx_fit[96];
        fit_text_arc(wx_fit, sizeof(wx_fit), wx, 170, true);
        draw_arc_text_bottom(fb, wx_fit, 142, COL_ACCENT);
        if (v->weather_location[0]) {
            char loc[64];
            fit_text_arc(loc, sizeof(loc), v->weather_location, 150, true);
            draw_arc_text_bottom(fb, loc, 112, COL_MUTED);
        }
    } else {
        draw_arc_text_bottom(fb, "正在获取天气", 142, COL_MUTED);
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
    fill_rect(fb, 0, 0, W, H, COL_BG);
    const int64_t elapsed = now_ms - v->celebrate_start_ms;
    if (elapsed < 0) return;
    if (elapsed > (int64_t)v->celebrate_seconds * 1000) return;

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
        draw_centered(fb, 92, "✓ 任务完成", COL_ACCENT, COL_BG, 1);
        char line1[BUDDY_TITLE_BYTES + 8];
        char line2[BUDDY_TITLE_BYTES + 8];
        line2[0] = '\0';
        const size_t used = fit_text(line1, sizeof(line1), v->celebrate_title, 1, 240, true);
        if (used < strlen(v->celebrate_title)) {
            fit_text(line2, sizeof(line2), v->celebrate_title + used, 1, 240, true);
        }
        draw_centered(fb, 128, line1, COL_TEXT, COL_BG, 1);
        if (line2[0]) draw_centered(fb, 156, line2, COL_TEXT, COL_BG, 1);
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
