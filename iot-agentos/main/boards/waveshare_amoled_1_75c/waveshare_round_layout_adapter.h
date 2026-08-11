#pragma once

#include "../round_display_layout.h"

/* Waveshare 1.75C's larger CO5300 aperture has a distinct safe text/pet
 * stage and PSRAM overlay policy. Keep these physical/layout facts local so
 * adding another round panel does not require business-layer conditionals. */
static const round_display_layout_t s_waveshare_round_display_layout = {
        .response_header_left = 72,
        .response_header_top_y = 30,
        .response_header_bottom_y = 102,
        .response_rule_left = 76,
        .response_title_y = 48,
        .response_rule_y = 100,
        .response_rule_height = 2,
        .response_text_x = 76,
        .response_text_y = 126,
        .response_text_width = 314,
        .response_line_gap = 34,
        .response_lines_per_page = 6,
        .response_footer_y = 364,
        .response_footer_rule_top_offset = -8,
        .response_footer_rule_height = 1,
        .ambient_top_width = 466,
        .ambient_top_height = 162,
        .ambient_bottom_width = 398,
        .ambient_bottom_height = 108,
        .ambient_bottom_x = 34,
        .ambient_bottom_y = 346,
        .ambient_top_ring_radius = 312,
        .ambient_ring_radius = 196,
        .pet_halo_center_y = 226,
        .pet_halo_radius = 106,
        /* Hub assets retain transparent authoring margins.  On the 466px
         * round aperture, a 200px square made tall/eye-heavy pets feel too
         * dominant and visually lean into the upper information arc.  Fit the
         * opaque silhouette in this narrower, lower stage: it leaves a
         * deliberate gap to both curved text bands without teaching the
         * shared renderer anything about this panel. */
        .remote_pet_trim_transparent_padding = true,
        .remote_pet_target = 176,
        .remote_pet_top = 150,
        .remote_pet_max_frames = 2,
        .ambient_overlay_uses_psram = true,
        /* CO5300 QSPI and PSRAM share the cache fabric. Rebuildable pet-cache
         * writes are therefore declined; mandatory storage flows are not. */
        .allows_optional_flash_work = false,
        .scene_reference_width = 360,
        .scene_reference_height = 360,
        /* Native 32-dot glyphs are already sized for the 466px aperture. */
        .curve_glyph_scale_num = 1,
        .curve_glyph_scale_den = 1,
        .standby_art_source_center = 180,
        .standby_art_reference_width = 360,
        .standby_art_source_center_tracks_target = false,
        /* The procedural pet has a tall eye/antenna silhouette.  Give it a
         * smaller, lower stage than the generic halo so it clears the header
         * and does not crowd the weather arc on the 466 px round aperture. */
        .standby_art_center_y = 238,
        .standby_art_scale_num = 3,
        .standby_art_scale_den = 4,
};

static inline const round_display_layout_t *waveshare_round_display_layout(void) {
    return &s_waveshare_round_display_layout;
}
