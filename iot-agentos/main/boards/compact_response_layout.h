/* Physical response-screen geometry for compact display profiles.
 *
 * This describes only where a common response scene can be drawn and whether
 * a board needs timed paging because it has no page keys.  Text pagination,
 * scene ownership and Device Display API semantics remain in the shared
 * compact renderer.
 */
#pragma once

#include <stdint.h>

typedef struct {
    int lines_per_page;
    int text_x;
    int text_y;
    int line_height;
    int footer_y;
    int header_height;
    int title_accent_y;
    int title_accent_width;
    int title_accent_height;
    int title_x_offset;
    int title_y;
    int footer_hint_y;
    int footer_indicator_y;
    int footer_indicator_advance;
    int image_accent_y;
    int image_accent_width;
    int image_accent_height;
    int image_title_x_offset;
    int image_title_y;
    int image_caption_y;
    int image_caption_bottom;
    int64_t automatic_page_interval_us;
} compact_response_layout_t;
