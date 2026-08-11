#pragma once

#include "../compact_response_layout.h"

/* Bread's physical 240x320 reading surface and its dedicated volume-key
 * paging affordance.  A zero automatic interval means the shared renderer
 * leaves page changes to the normalized input action. */
static inline const compact_response_layout_t *bread_response_layout_adapter(void) {
    static const compact_response_layout_t layout = {
        .lines_per_page = 6,
        .text_x = 16,
        .text_y = 78,
        .line_height = 32,
        .footer_y = 276,
        .header_height = 60,
        .title_accent_y = 19,
        .title_accent_width = 4,
        .title_accent_height = 23,
        .title_x_offset = 14,
        .title_y = 18,
        .footer_hint_y = 287,
        .footer_indicator_y = 289,
        .footer_indicator_advance = 18,
        .image_accent_y = 19,
        .image_accent_width = 4,
        .image_accent_height = 23,
        .image_title_x_offset = 14,
        .image_title_y = 18,
        .image_caption_y = 238,
        .image_caption_bottom = 222,
        .automatic_page_interval_us = 0,
    };
    return &layout;
}
