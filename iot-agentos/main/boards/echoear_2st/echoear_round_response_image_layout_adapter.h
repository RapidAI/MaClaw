#pragma once

#include "../round_response_image_layout.h"

/* EchoEar-2ST's existing 360px image-response reading surface. */
static inline const round_response_image_layout_t *echoear_round_response_image_layout(void) {
    static const round_response_image_layout_t layout = {
        .header_left = 48, .header_top_y = 22, .header_bottom_y = 80,
        .rule_left = 60, .rule_y = 78, .rule_height = 2,
        .title_y = 36, .title_max_width = 216,
        .content_top_y = 92, .content_bottom_without_caption_y = 260,
        .content_bottom_with_caption_y = 234, .content_side_margin = 60,
        .caption_first_y = 246, .caption_second_y = 272, .caption_max_width = 240,
        /* Keep the back hint below a possible second 24px caption line; the
         * former footer coordinate overlapped it on long captions. */
        .hint_x = 60, .hint_y = 310,
    };
    return &layout;
}
