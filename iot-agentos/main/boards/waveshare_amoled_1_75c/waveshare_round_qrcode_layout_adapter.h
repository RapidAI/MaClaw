#pragma once

#include "../round_qrcode_layout.h"

/* The 466px aperture can host a larger quiet-zone-preserving QR tile without
 * pushing its corners behind the circular bezel. */
static inline const round_qrcode_layout_t *waveshare_round_qrcode_layout(void) {
    static const round_qrcode_layout_t layout = {
        .maximum_qr_square = 272, .qr_top_y = 46,
        .title_y = 338, .instruction_y = 374, .continuation_y = 406,
        .text_max_width = 300,
    };
    return &layout;
}
