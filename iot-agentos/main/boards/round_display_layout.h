#pragma once

/* Profile-private geometry contract consumed by the shared round renderer.
 * These values describe the drawable aperture and memory topology only; they
 * never select a business state, input gesture, or feature set. */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef struct {
    /* Text-response reading surface: content flow remains shared, while the
     * panel supplies its circular header/footer safe chords. */
    int response_header_left;
    int response_header_top_y;
    int response_header_bottom_y;
    int response_rule_left;
    int response_title_y;
    int response_rule_y;
    int response_rule_height;
    int response_text_x;
    int response_text_y;
    int response_text_width;
    int response_line_gap;
    unsigned response_lines_per_page;
    int response_footer_y;
    int response_footer_rule_top_offset;
    int response_footer_rule_height;

    int ambient_top_width;
    int ambient_top_height;
    int ambient_bottom_width;
    int ambient_bottom_height;
    int ambient_bottom_x;
    int ambient_bottom_y;
    int ambient_top_ring_radius;
    int ambient_ring_radius;

    int pet_halo_center_y;
    int pet_halo_radius;
    /* Remote packs are delivered in a common source canvas.  A panel can
     * elect to fit its opaque pet silhouette instead: this removes authoring
     * padding without leaking a display-specific workaround into the UI. */
    bool remote_pet_trim_transparent_padding;
    int remote_pet_target;
    int remote_pet_top;
    size_t remote_pet_max_frames;

    bool ambient_overlay_uses_psram;
    bool allows_optional_flash_work;
    int scene_reference_width;
    int scene_reference_height;
    int curve_glyph_scale_num;
    int curve_glyph_scale_den;
    int standby_art_source_center;
    int standby_art_reference_width;
    bool standby_art_source_center_tracks_target;
    /* The native standby pet uses its own optical centre.  It can differ from
     * the generic pet halo used by other fallback skins, because artwork may
     * have antennae/ears that need more clearance under the top text arc. */
    int standby_art_center_y;
    int standby_art_scale_num;
    int standby_art_scale_den;
} round_display_layout_t;
