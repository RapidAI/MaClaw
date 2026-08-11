#pragma once

#include "../round_qrcode_layout.h"

/* EchoEar-2ST's tested 360px QR code safe square and text positions. */
static inline const round_qrcode_layout_t *echoear_round_qrcode_layout(void) {
    static const round_qrcode_layout_t layout = {
        .maximum_qr_square = 204, .qr_top_y = 40,
        .title_y = 270, .instruction_y = 304, .continuation_y = 330,
        .text_max_width = 240,
    };
    return &layout;
}
