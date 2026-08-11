#pragma once

#include "../compact_recording_layout.h"

/* Fangtang has the same recording semantics but a 240x240 safe area. */
static inline const compact_recording_layout_t *fangtang_recording_layout_adapter(void) {
    static const compact_recording_layout_t layout = {
        .accent_top_y = 8, .accent_bottom_y = 232,
        .icon_x = 28, .icon_y = 23, .icon_outer_size = 18,
        .icon_inner_offset = 5, .icon_inner_size = 8,
        .status_text_x = 56, .status_text_y = 20, .status_scale = 7,
        .title_y = 52, .title_scale = 8, .timer_y = 82,
        .waveform_rule_y = 114, .waveform_center_y = 158,
        .waveform_half_height = 32, .waveform_clear_top = 116,
        .waveform_clear_height = 94, .microphone_label_y = 184,
        .instruction_y = 211, .instruction_scale = 8,
        .meeting_stop_instruction = "按激活键停止",
    };
    return &layout;
}
