#pragma once

#include "../compact_response_layout.h"

/* Fangtang's 240x240 viewport has a shorter reading column and no paging
 * keys.  The common renderer therefore uses the profile's bounded timed
 * presentation, rather than adding Fangtang branches to business code. */
static inline const compact_response_layout_t *fangtang_response_layout_adapter(void) {
    static const compact_response_layout_t layout = {
        .lines_per_page = 5,
        .text_x = 12,
        .text_y = 54,
        .line_height = 30,
        .footer_y = 208,
        .header_height = 46,
        .title_accent_y = 5,
        .title_accent_width = 3,
        .title_accent_height = 20,
        .title_x_offset = 12,
        .title_y = 10,
        .footer_hint_y = 214,
        .footer_indicator_y = 216,
        .footer_indicator_advance = 12,
        .image_accent_y = 10,
        .image_accent_width = 4,
        .image_accent_height = 24,
        .image_title_x_offset = 14,
        .image_title_y = 10,
        .image_caption_y = 178,
        .image_caption_bottom = 172,
        .automatic_page_interval_us = 6LL * 1000 * 1000,
    };
    return &layout;
}
