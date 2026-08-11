#pragma once

/* Physical geometry for the common short-status-message scene on circular
 * panels. Business ownership, copied message text, foreground hand-off and
 * fallback wording remain in the shared renderer. */

typedef struct {
    int avatar_center_x;
    int avatar_outer_center_y;
    int avatar_outer_radius;
    int avatar_inner_center_y;
    int avatar_inner_radius;
    int left_ear_x;
    int left_ear_y;
    int right_ear_x;
    int right_ear_y;
    int left_eye_x;
    int left_eye_y;
    int right_eye_x;
    int right_eye_y;
    int nose_center_x;
    int nose_center_y;
    int nose_radius;
    int mouth_left;
    int mouth_right;
    int mouth_top_y;
    int mouth_bottom_y;

    int divider_left;
    int divider_y;
    int divider_height;
    int title_y;
    int title_max_width;
    int body_y;
    int body_continuation_y;
    int body_max_width;
    int hint_y;
    int hint_max_width;
} round_message_layout_t;
