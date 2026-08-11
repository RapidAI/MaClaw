#pragma once

/* Physical alarm-scene geometry for circular display profiles.
 *
 * Alarm scheduling/ringing, attempt semantics, time extraction, animation
 * cadence and foreground-scene restoration remain in the shared renderer.
 * A profile describes only the safe geometry of its circular aperture.
 */

typedef struct {
    int title_y;
    int title_max_width;
    int rule_left;
    int rule_y;
    int rule_height;

    int clock_center_y;
    int clock_max_width;
    int label_y;
    int label_max_width;
    int hint_y;
    int hint_max_width;

    int bell_center_y;
    int bell_outer_radius;
    int bell_inner_radius;
    int bell_side_offset_x;
    int bell_side_center_y;
    int bell_side_radius;
    int bell_side_bar_inner_x;
    int bell_side_bar_outer_x;
    int bell_side_bar_y;
    int bell_side_bar_height;
    int bell_top_stem_top;
    int bell_top_stem_bottom;
    int bell_top_knob_y;
    int bell_top_knob_radius;
    int bell_leg_inner_x;
    int bell_leg_outer_x;
    int bell_leg_top_y;
    int bell_leg_bottom_y;
} round_alarm_layout_t;
