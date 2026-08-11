#pragma once

/* Physical recording-scene geometry for circular display profiles.
 *
 * Capture state, elapsed time, waveform history and action semantics stay in
 * the shared round renderer.  A selected panel supplies only its drawable
 * chord/safe-area geometry and the concise wording its input affordance can
 * present.  This is deliberately profile-private, not a Device API.
 */

typedef struct {
    int accent_left;
    int accent_top_y;
    int accent_bottom_y;

    int icon_x;
    int icon_y;
    int icon_outer_size;
    int icon_inner_offset;
    int icon_inner_size;

    int status_y;
    int status_max_width;
    int title_y;
    int title_max_width;
    int timer_y;

    int waveform_left;
    int waveform_pitch;
    int waveform_bar_width;
    int waveform_center_y;
    int waveform_half_height;
    int waveform_clear_left;
    int waveform_clear_top;
    int waveform_clear_width;
    int waveform_clear_height;
    int microphone_label_y;

    int instruction_y;
    int instruction_max_width;
    const char *meeting_stop_instruction;
    const char *command_completion_instruction;
} round_recording_layout_t;
