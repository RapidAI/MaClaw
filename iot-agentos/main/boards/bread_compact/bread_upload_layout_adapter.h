#pragma once

#include "../compact_upload_layout.h"

static inline const compact_upload_layout_t *bread_upload_layout_adapter(void) {
    static const compact_upload_layout_t layout = {
        .title_y = 66, .title_scale = 8,
        .stage_y = 112, .stage_scale = 9,
        .progress_y = 184, .percent_y = 226,
        .warning_y = 272, .warning_scale = 9,
    };
    return &layout;
}
