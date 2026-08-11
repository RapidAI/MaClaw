#pragma once

#include "../round_recording_layout.h"

/* EchoEar-2ST's 360px circular recording safe area. */
static inline const round_recording_layout_t *echoear_round_recording_layout(void) {
    static const round_recording_layout_t layout = {
        .accent_left = 78, .accent_top_y = 34, .accent_bottom_y = 34,
        .icon_x = 58, .icon_y = 52, .icon_outer_size = 20,
        .icon_inner_offset = 6, .icon_inner_size = 8,
        .status_y = 48, .status_max_width = 198,
        .title_y = 84, .title_max_width = 198, .timer_y = 122,
        .waveform_left = 84, .waveform_pitch = 8, .waveform_bar_width = 5,
        .waveform_center_y = 205, .waveform_half_height = 42,
        .waveform_clear_left = 72, .waveform_clear_top = 157,
        .waveform_clear_width = 216, .waveform_clear_height = 123,
        .microphone_label_y = 260,
        .instruction_y = 298, .instruction_max_width = 180,
        .meeting_stop_instruction = "点屏停止保存",
        .command_completion_instruction = "说完自动处理",
    };
    return &layout;
}
