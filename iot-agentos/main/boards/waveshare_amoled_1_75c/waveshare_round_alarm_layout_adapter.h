#pragma once

#include "../round_alarm_layout.h"

/* Wider/lower 466px AMOLED alarm composition.  The shared twin-bell scene is
 * unchanged; only the physical safe chord and optical spacing are adapted. */
static inline const round_alarm_layout_t *waveshare_round_alarm_layout(void) {
    static const round_alarm_layout_t layout = {
        .title_y = 54, .title_max_width = 286,
        .rule_left = 88, .rule_y = 98, .rule_height = 3,
        .clock_center_y = 208, .clock_max_width = 240,
        .label_y = 340, .label_max_width = 320,
        .hint_y = 390, .hint_max_width = 300,
        .bell_center_y = 232, .bell_outer_radius = 86, .bell_inner_radius = 70,
        .bell_side_offset_x = 80, .bell_side_center_y = 145, .bell_side_radius = 35,
        .bell_side_bar_inner_x = 62, .bell_side_bar_outer_x = 96,
        .bell_side_bar_y = 172, .bell_side_bar_height = 10,
        .bell_top_stem_top = 124, .bell_top_stem_bottom = 148,
        .bell_top_knob_y = 118, .bell_top_knob_radius = 10,
        .bell_leg_inner_x = 24, .bell_leg_outer_x = 42,
        .bell_leg_top_y = 304, .bell_leg_bottom_y = 330,
    };
    return &layout;
}
