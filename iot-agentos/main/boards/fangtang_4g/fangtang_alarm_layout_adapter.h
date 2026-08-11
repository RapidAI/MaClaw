#pragma once

#include "../compact_alarm_layout.h"

static inline const compact_alarm_layout_t *fangtang_alarm_layout_adapter(void) {
    static const compact_alarm_layout_t layout = {
        .accent_y = 8, .title_y = 27, .title_scale = 8,
        .time_y = 69, .label_y = 112, .label_scale = 9,
        .attempt_y = 157, .instruction_y = 199, .instruction_scale = 8,
    };
    return &layout;
}
