#pragma once

#include "../round_message_layout.h"

/* EchoEar-2ST's existing 360px short-status surface. */
static inline const round_message_layout_t *echoear_round_message_layout(void) {
    static const round_message_layout_t layout = {
        .avatar_center_x = 180, .avatar_outer_center_y = 146,
        .avatar_outer_radius = 82, .avatar_inner_center_y = 141,
        .avatar_inner_radius = 70,
        .left_ear_x = 119, .left_ear_y = 80,
        .right_ear_x = 241, .right_ear_y = 80,
        .left_eye_x = 153, .left_eye_y = 144,
        .right_eye_x = 207, .right_eye_y = 144,
        .nose_center_x = 180, .nose_center_y = 170, .nose_radius = 11,
        .mouth_left = 166, .mouth_right = 194,
        .mouth_top_y = 184, .mouth_bottom_y = 190,
        .divider_left = 50, .divider_y = 220, .divider_height = 2,
        .title_y = 240, .title_max_width = 240,
        .body_y = 278, .body_continuation_y = 306, .body_max_width = 240,
        .hint_y = 336, .hint_max_width = 240,
    };
    return &layout;
}
