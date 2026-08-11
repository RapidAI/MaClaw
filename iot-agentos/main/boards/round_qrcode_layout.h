#pragma once

/* Physical provisioning-QR geometry for circular display profiles.
 *
 * QR module content, quiet-zone policy, provisioning ownership and the
 * atomic foreground transition remain common.  Each panel supplies only the
 * largest safely scannable square and its explanatory text safe area.
 */

typedef struct {
    int maximum_qr_square;
    int qr_top_y;
    int title_y;
    int instruction_y;
    int continuation_y;
    int text_max_width;
} round_qrcode_layout_t;
