#pragma once

#include "../round_upload_layout.h"

/* EchoEar-2ST's established 360px upload-progress safe area. */
static inline const round_upload_layout_t *echoear_round_upload_layout(void) {
    static const round_upload_layout_t layout = {
        .accent_left = 52, .accent_y = 34,
        .title_y = 62, .stage_y = 106, .stage_continuation_y = 134,
        .text_max_width = 240,
        .progress_x = 42, .progress_y = 178,
        .progress_width = 276, .progress_height = 18,
        .percent_y = 222, .bytes_y = 258,
        .warning_y = 304, .warning_max_width = 240,
    };
    return &layout;
}
