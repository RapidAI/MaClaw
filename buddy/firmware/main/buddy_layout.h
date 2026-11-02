#pragma once

#include <stdbool.h>

/** Pixel metrics for the 360 compose buffer.
 * A round glass keeps copy inside the visible circle. A square glass uses the
 * rectangle, so the same phrases are not cropped to that circle. */
typedef struct {
    int header_y;
    int label_x;
    int value_right;
    int index_right;
    int index_y;
    int rule_x0;
    int rule_x1;
    int text_x;
    int title_w;
    int tail_w;
    int bar_x;
    int bar_w;
    bool arc_standby;
    int date_y;
    int weather_y;
    int location_y;
} buddy_layout_t;

void buddy_layout_metrics(bool round_glass, buddy_layout_t *out);
