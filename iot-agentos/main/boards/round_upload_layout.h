#pragma once

/* Physical upload-progress geometry for circular display profiles.
 *
 * Upload ownership, stage text, percentage arithmetic and foreground
 * lifecycle remain shared.  The selected display profile describes only how
 * that information fits inside its circular aperture.
 */

typedef struct {
    int accent_left;
    int accent_y;
    int title_y;
    int stage_y;
    int stage_continuation_y;
    int text_max_width;
    int progress_x;
    int progress_y;
    int progress_width;
    int progress_height;
    int percent_y;
    int bytes_y;
    int warning_y;
    int warning_max_width;
} round_upload_layout_t;
