#include "buddy_layout.h"

void buddy_layout_metrics(bool round_glass, buddy_layout_t *out) {
    if (!out) return;
    if (round_glass) {
        out->header_y = 36;
        out->label_x = 88;
        out->value_right = 262;
        out->index_right = 284;
        out->index_y = 186;
        out->rule_x0 = 90;
        out->rule_x1 = 270;
        out->text_x = 60;
        out->title_w = 240;
        /* 24px line at y=270. Its bottom corners must stay inside r=150. */
        out->tail_w = 190;
        out->bar_x = 108;
        out->bar_w = 144;
        out->arc_standby = true;
        out->date_y = 0;
        out->weather_y = 0;
        out->location_y = 0;
        return;
    }
    out->header_y = 16;
    out->label_x = 20;
    out->value_right = 340;
    out->index_right = 340;
    out->index_y = 16;
    out->rule_x0 = 20;
    out->rule_x1 = 340;
    out->text_x = 20;
    out->title_w = 320;
    out->tail_w = 320;
    out->bar_x = 20;
    out->bar_w = 320;
    out->arc_standby = false;
    /* Standby rows are drawn on the 24px cell grid with a zoom (see
     * STANDBY_TITLE_ZOOM / STANDBY_LABEL_ZOOM in buddy_ui.c), so these tops are
     * band origins, not baselines: the title band is 36px tall, the weather band
     * 30px and the location band 24px. date_y 8..44, status 50..80 (clears the
     * 176px pet asset at y=82), weather 290..320, location 322..346. */
    out->date_y = 8;
    out->weather_y = 290;
    out->location_y = 322;
}
