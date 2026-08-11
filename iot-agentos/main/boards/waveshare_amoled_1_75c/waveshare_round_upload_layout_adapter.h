#pragma once

#include "../round_upload_layout.h"

/* The 466px AMOLED's wide central chord gives the same shared upload scene a
 * larger progress rail and a lower text rhythm without changing upload
 * semantics or introducing a Waveshare branch into the renderer. */
static inline const round_upload_layout_t *waveshare_round_upload_layout(void) {
    static const round_upload_layout_t layout = {
        .accent_left = 76, .accent_y = 52,
        .title_y = 88, .stage_y = 138, .stage_continuation_y = 170,
        .text_max_width = 314,
        .progress_x = 78, .progress_y = 224,
        .progress_width = 310, .progress_height = 20,
        .percent_y = 276, .bytes_y = 314,
        .warning_y = 374, .warning_max_width = 300,
    };
    return &layout;
}
