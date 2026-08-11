/* Physical upload-progress geometry for compact display profiles.
 *
 * Upload state, percentage calculation and foreground ownership remain in
 * the shared renderer.  A profile supplies only the safe coordinates and
 * type scale for its physical viewport. */
#pragma once

typedef struct {
    int title_y;
    int title_scale;
    int stage_y;
    int stage_scale;
    int progress_y;
    int percent_y;
    int warning_y;
    int warning_scale;
} compact_upload_layout_t;
