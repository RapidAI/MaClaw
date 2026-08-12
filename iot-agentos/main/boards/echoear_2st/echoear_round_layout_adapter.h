#pragma once

#include "../round_display_layout.h"

/* EchoEar-2ST's 360px circular aperture and its internal-DMA text overlay
 * allocation are display facts. The shared scene renderer owns only the
 * common clock, weather, pet and result semantics. */
static const round_display_layout_t s_echoear_round_display_layout = {
        .response_header_left = 48,
        .response_header_top_y = 22,
        .response_header_bottom_y = 80,
        .response_rule_left = 60,
        .response_title_y = 36,
        .response_rule_y = 78,
        .response_rule_height = 2,
        .response_text_x = 60,
        .response_text_y = 102,
        .response_text_width = 240,
        .response_line_gap = 30,
        .response_lines_per_page = 5,
        .response_footer_y = 278,
        .response_footer_rule_top_offset = -8,
        .response_footer_rule_height = 1,
        .ambient_top_width = 360,
        .ambient_top_height = 128,
        .ambient_bottom_width = 316,
        .ambient_bottom_height = 90,
        .ambient_bottom_x = 22,
        .ambient_bottom_y = 268,
        .ambient_top_ring_radius = 240,
        .ambient_ring_radius = 150,
        .pet_halo_center_y = 175,
        .pet_halo_radius = 106,
        .remote_pet_target = 220,
        .remote_pet_top = 72,
        .remote_pet_max_frames = 8,
        /* The 90 KiB curved-text surface is submitted through the same
         * DMA-capable PSRAM path as the two full-screen framebuffers.  Keeping
         * it out of internal RAM leaves the boot-time DMA reserve available
         * for audio, Wi-Fi and panel transactions. */
        .ambient_overlay_uses_psram = true,
        .allows_optional_flash_work = true,
        .scene_reference_width = 360,
        .scene_reference_height = 360,
        .curve_glyph_scale_num = 1,
        .curve_glyph_scale_den = 1,
        .standby_art_source_center = 180,
        .standby_art_reference_width = 360,
        .standby_art_source_center_tracks_target = true,
        .standby_art_center_y = 175,
        .standby_art_scale_num = 7,
        .standby_art_scale_den = 8,
};

static inline const round_display_layout_t *echoear_round_display_layout(void) {
    return &s_echoear_round_display_layout;
}
