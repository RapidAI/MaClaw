#pragma once

#include "../compact_standby_layout.h"

/* Fangtang's 240x240 standby surface has two information rows.  Its NV3023
 * transfer reliability contract is one viewport row per transaction. */
static inline const compact_standby_layout_t *fangtang_standby_layout_adapter(void) {
    static const compact_standby_layout_t layout = {
        .transfer_stripe_rows = 1,
        .weather_text_y = 0, /* Fangtang's compact standby has no weather row. */
        .weather_scale_num = 1,
        .weather_scale_den = 1,
        .pet_top = 62,
        .pet_max_width = 220,
        .native_pet_scale_percent = 93,
    };
    return &layout;
}
