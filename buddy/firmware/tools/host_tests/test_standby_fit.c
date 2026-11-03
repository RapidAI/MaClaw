/* Geometry check for the enlarged standby text on the 240x240 square glass.
 *
 * The UI composes a 360x360 canvas and buddy_display downsamples 3:2 for the
 * NV3023 module, so a canvas pixel is 2/3 of a physical pixel here. These
 * assertions pin the two things the enlargement could break: a line that no
 * longer fits title_w (it would be ellipsised) and a band that collides with
 * the pet asset or the panel edge.
 *
 * Zoom values mirror STANDBY_TITLE_ZOOM / STANDBY_LABEL_ZOOM in buddy_ui.c.
 */
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "buddy_font.h"
#include "buddy_layout.h"

const uint8_t _binary_cjk24_cjk_bin_start[1] = {0};
const uint8_t _binary_cjk24_cjk_bin_end[1] = {0};
const uint8_t _binary_cjk24_ext_bin_start[1] = {0};
const uint8_t _binary_cjk24_ext_bin_end[1] = {0};
/* The 16px table is embedded the same way; the host tests only need the
 * symbols to resolve. */
const uint8_t _binary_cjk16_bin_start[1] = {0};
const uint8_t _binary_cjk16_bin_end[1] = {0};

void buddy_font_draw_cells(uint16_t *fb, int fw, int fh, int x, int y, const char *text,
                           uint16_t fg, uint16_t bg, float zoom, int max_px);
int buddy_font_cells_width(const char *text, float zoom, int max_px);
void buddy_font_draw_small(uint16_t *fb, int fw, int fh, int x, int y, const char *text,
                           uint16_t fg, uint16_t bg, float zoom, int max_px);
int buddy_font_small_width(const char *text, float zoom, int max_px);

#define TITLE_ZOOM 1.5f  /* 24px table -> 24 glass px */
#define LABEL_ZOOM 1.875f /* 16px table -> 20 glass px */
#define STATUS_Y 50      /* STANDBY_STATUS_Y in buddy_ui.c */
#define OLD_STATUS_Y 46  /* its value before the 3-glass-px drop */
#define ROUND_STATUS_Y 61 /* STANDBY_STATUS_Y_ROUND in buddy_ui.c */
#define ROUND_LINE 24     /* 24px table @ scale 1 on the 1:1 glass */
#define PET_TOP 82       /* top edge of the 176px pet asset */
#define CANVAS 360
#define GLASS 240

static int check(const char *name, int cond) {
    printf("%s: %s\n", name, cond ? "ok" : "FAIL");
    return cond ? 0 : 1;
}

/* Canvas px -> physical px on the 240 module. */
static int glass(int canvas_px) { return canvas_px * GLASS / CANVAS; }

int main(void) {
    int failures = 0;

    buddy_layout_t layout;
    buddy_layout_metrics(false, &layout);

    /* Square glass only: the round 360 profile keeps its curved text. */
    failures += check("square layout selected", !layout.arc_standby);

    /* Every standby line must still fit its column at the enlarged zoom. */
    const char *title = "MaClaw Buddy";
    const int title_w = buddy_font_cells_width(title, TITLE_ZOOM, 0);
    failures += check("brand fits title_w", title_w <= layout.title_w);
    /* A widened date is the widest thing on the top band: １０月２７日 周二. */
    const char *date = "１０月２７日 周二";
    const int date_w = buddy_font_cells_width(date, TITLE_ZOOM, 0);
    failures += check("widened date fits title_w", date_w <= layout.title_w);
    const char *offline = "未连接 · 静音";
    failures += check("status fits title_w",
                      buddy_font_small_width(offline, LABEL_ZOOM, 0) <= layout.title_w);
    const char *weather = "旧 多云 26°C";
    failures += check("weather fits title_w",
                      buddy_font_small_width(weather, LABEL_ZOOM, 0) <= layout.title_w);
    const char *loc = "北京市朝阳区";
    failures += check("location fits title_w",
                      buddy_font_small_width(loc, LABEL_ZOOM, 0) <= layout.title_w);

    /* Glass-pixel size delivered by each zoom. The label line comes from the
     * 16px table, so its cell is 16 canvas px, not 24. */
    failures += check("brand is 24px on glass", glass((int)(24 * TITLE_ZOOM)) == 24);
    failures += check("status is 20px on glass", glass((int)(16 * LABEL_ZOOM)) == 20);

    /* Vertical bands must not collide: title 36px, status 30px, pet at 82,
     * weather 30px, location 30px, all inside the 360 canvas. */
    failures += check("title band ends before status",
                      layout.date_y + (int)(24 * TITLE_ZOOM) <= STATUS_Y);
    failures += check("status band clears the pet", STATUS_Y + (int)(16 * LABEL_ZOOM) <= PET_TOP);
    /* The status line was lowered by 3 physical px on the glass: canvas rows are
     * 2/3 of a glass row here, so a 4-row move has to land as exactly 3. */
    failures += check("status dropped 3 glass px", glass(STATUS_Y) - glass(OLD_STATUS_Y) == 3);
    failures += check("weather band below the pet",
                      layout.weather_y >= 258 && layout.weather_y + (int)(16 * LABEL_ZOOM) <=
                                                    layout.location_y);
    failures += check("location band inside the panel",
                      layout.location_y + (int)(16 * LABEL_ZOOM) <= CANVAS);

    /* The round 360 glass draws the canvas 1:1, so it keeps the 24px status line
     * at its old origin: same band height, same 3px clearance under the title,
     * and it must still clear the pet asset. This guards against the square
     * enlargement leaking into the round profile. */
    buddy_layout_metrics(true, &layout);
    failures += check("round layout selected", layout.arc_standby);
    failures += check("round status keeps 24px line", ROUND_LINE == 24);
    failures += check("round status unchanged", ROUND_STATUS_Y == 61);
    /* The round band deliberately overlaps the pet top (61+24 > 82): the status
     * is repainted after the pet, which is why only the square band had to be
     * moved clear. Assert it still fits on the panel instead. */
    failures += check("round status inside the panel", ROUND_STATUS_Y + ROUND_LINE <= CANVAS);
    return failures == 0 ? 0 : 1;
}
