#pragma once

#include "../round_message_layout.h"

/* The AMOLED needs a centered, lower status avatar and a lower text column;
 * merely scaling the former 360px constants leaves its face visibly left of
 * centre and wastes the larger aperture. */
static inline const round_message_layout_t *waveshare_round_message_layout(void) {
    static const round_message_layout_t layout = {
        .avatar_center_x = 233, .avatar_outer_center_y = 184,
        .avatar_outer_radius = 105, .avatar_inner_center_y = 178,
        .avatar_inner_radius = 88,
        .left_ear_x = 166, .left_ear_y = 94,
        .right_ear_x = 300, .right_ear_y = 94,
        .left_eye_x = 198, .left_eye_y = 182,
        .right_eye_x = 268, .right_eye_y = 182,
        .nose_center_x = 233, .nose_center_y = 214, .nose_radius = 13,
        .mouth_left = 216, .mouth_right = 250,
        .mouth_top_y = 230, .mouth_bottom_y = 237,
        .divider_left = 76, .divider_y = 274, .divider_height = 2,
        .title_y = 296, .title_max_width = 300,
        .body_y = 334, .body_continuation_y = 366, .body_max_width = 300,
        .hint_y = 414, .hint_max_width = 220,
    };
    return &layout;
}
