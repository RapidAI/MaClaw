#pragma once

/* Physical layout for the common response-with-image scene on circular
 * panels. The shared renderer retains response ownership, source validation,
 * scaling algorithm and no-page navigation semantics. */

typedef struct {
    int header_left;
    int header_top_y;
    int header_bottom_y;
    int rule_left;
    int rule_y;
    int rule_height;
    int title_y;
    int title_max_width;

    int content_top_y;
    int content_bottom_without_caption_y;
    int content_bottom_with_caption_y;
    int content_side_margin;

    int caption_first_y;
    int caption_second_y;
    int caption_max_width;
    int hint_x;
    int hint_y;
} round_response_image_layout_t;
