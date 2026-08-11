#pragma once

#include "../compact_alarm_layout.h"

static inline const compact_alarm_layout_t *bread_alarm_layout_adapter(void) {
    static const compact_alarm_layout_t layout = {
        .accent_y = 18, .title_y = 48, .title_scale = 8,
        .time_y = 105, .label_y = 176, .label_scale = 9,
        .attempt_y = 230, .instruction_y = 278, .instruction_scale = 8,
    };
    return &layout;
}
