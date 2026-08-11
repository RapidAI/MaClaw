#pragma once

#include "../round_alarm_layout.h"

/* EchoEar-2ST's established 360px alarm safe area. */
static inline const round_alarm_layout_t *echoear_round_alarm_layout(void) {
    static const round_alarm_layout_t layout = {
        .title_y = 40, .title_max_width = 208,
        .rule_left = 68, .rule_y = 78, .rule_height = 3,
        .clock_center_y = 164, .clock_max_width = 180,
        .label_y = 276, .label_max_width = 244,
        .hint_y = 318, .hint_max_width = 224,
        .bell_center_y = 183, .bell_outer_radius = 70, .bell_inner_radius = 57,
        .bell_side_offset_x = 65, .bell_side_center_y = 112, .bell_side_radius = 29,
        .bell_side_bar_inner_x = 50, .bell_side_bar_outer_x = 78,
        .bell_side_bar_y = 134, .bell_side_bar_height = 8,
        .bell_top_stem_top = 96, .bell_top_stem_bottom = 116,
        .bell_top_knob_y = 92, .bell_top_knob_radius = 8,
        .bell_leg_inner_x = 20, .bell_leg_outer_x = 34,
        .bell_leg_top_y = 244, .bell_leg_bottom_y = 265,
    };
    return &layout;
}
