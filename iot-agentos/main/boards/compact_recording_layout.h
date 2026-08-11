/* Physical recording-scene geometry for compact display profiles.
 *
 * The common renderer owns capture/paused/meeting state, elapsed time and the
 * normalized 24-column level history. A profile only describes where that
 * common scene fits on its panel. */
#pragma once

typedef struct {
    int accent_top_y;
    int accent_bottom_y;
    int icon_x;
    int icon_y;
    int icon_outer_size;
    int icon_inner_offset;
    int icon_inner_size;
    int status_text_x;
    int status_text_y;
    int status_scale;
    int title_y;
    int title_scale;
    int timer_y;
    int waveform_rule_y;
    int waveform_center_y;
    int waveform_half_height;
    int waveform_clear_top;
    int waveform_clear_height;
    int microphone_label_y;
    int instruction_y;
    int instruction_scale;
    /* Physical wording capacity for the standard stop action.  This avoids
     * letting a panel-height conditional change product copy in the shared
     * recording scene. */
    const char *meeting_stop_instruction;
} compact_recording_layout_t;
