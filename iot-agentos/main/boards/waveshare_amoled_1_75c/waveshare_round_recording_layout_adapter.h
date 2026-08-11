#pragma once

#include "../round_recording_layout.h"

/* The 466px AMOLED exposes a wider, lower recording stage than EchoEar.
 * These values keep every shared recorder element inside the visible circular
 * chord; they do not select recording behaviour or gesture semantics. */
static inline const round_recording_layout_t *waveshare_round_recording_layout(void) {
    static const round_recording_layout_t layout = {
        .accent_left = 84, .accent_top_y = 44, .accent_bottom_y = 44,
        .icon_x = 86, .icon_y = 64, .icon_outer_size = 24,
        .icon_inner_offset = 7, .icon_inner_size = 10,
        .status_y = 72, .status_max_width = 258,
        .title_y = 110, .title_max_width = 280, .timer_y = 152,
        .waveform_left = 137, .waveform_pitch = 8, .waveform_bar_width = 5,
        .waveform_center_y = 260, .waveform_half_height = 48,
        .waveform_clear_left = 125, .waveform_clear_top = 208,
        .waveform_clear_width = 246, .waveform_clear_height = 136,
        .microphone_label_y = 322,
        .instruction_y = 366, .instruction_max_width = 300,
        .meeting_stop_instruction = "点屏停止保存",
        .command_completion_instruction = "说完自动处理",
    };
    return &layout;
}
