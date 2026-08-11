#pragma once

#include "../round_response_image_layout.h"

/* The 466px aperture can show the same response image larger while retaining
 * a clear title, two caption lines and lower back hint within the visible
 * circle. */
static inline const round_response_image_layout_t *waveshare_round_response_image_layout(void) {
    static const round_response_image_layout_t layout = {
        .header_left = 72, .header_top_y = 30, .header_bottom_y = 102,
        .rule_left = 76, .rule_y = 100, .rule_height = 2,
        .title_y = 48, .title_max_width = 290,
        .content_top_y = 116, .content_bottom_without_caption_y = 346,
        .content_bottom_with_caption_y = 306, .content_side_margin = 76,
        .caption_first_y = 322, .caption_second_y = 354, .caption_max_width = 314,
        .hint_x = 76, .hint_y = 374,
    };
    return &layout;
}
