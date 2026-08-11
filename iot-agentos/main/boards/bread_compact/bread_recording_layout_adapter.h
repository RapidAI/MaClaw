#pragma once

#include "../compact_recording_layout.h"

static inline const compact_recording_layout_t *bread_recording_layout_adapter(void) {
    static const compact_recording_layout_t layout = {
        .accent_top_y = 16, .accent_bottom_y = 300,
        .icon_x = 28, .icon_y = 43, .icon_outer_size = 20,
        .icon_inner_offset = 6, .icon_inner_size = 8,
        .status_text_x = 62, .status_text_y = 42, .status_scale = 7,
        .title_y = 78, .title_scale = 8, .timer_y = 112,
        .waveform_rule_y = 158, .waveform_center_y = 205,
        .waveform_half_height = 42, .waveform_clear_top = 160,
        .waveform_clear_height = 90, .microphone_label_y = 226,
        .instruction_y = 260, .instruction_scale = 9,
        .meeting_stop_instruction = "按激活键停止保存",
    };
    return &layout;
}
