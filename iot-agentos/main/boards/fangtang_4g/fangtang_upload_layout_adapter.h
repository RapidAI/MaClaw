#pragma once

#include "../compact_upload_layout.h"

static inline const compact_upload_layout_t *fangtang_upload_layout_adapter(void) {
    static const compact_upload_layout_t layout = {
        .title_y = 24, .title_scale = 8,
        .stage_y = 62, .stage_scale = 9,
        .progress_y = 108, .percent_y = 138,
        .warning_y = 190, .warning_scale = 9,
    };
    return &layout;
}
