#pragma once

#include "../compact_standby_layout.h"

/* Bread's three-row 240x320 standby surface reserves the lower region for
 * the shared selected-pet scene. */
static inline const compact_standby_layout_t *bread_standby_layout_adapter(void) {
    static const compact_standby_layout_t layout = {
        .transfer_stripe_rows = 16,
        .weather_text_y = 66,
        .weather_scale_num = 1,
        .weather_scale_den = 1,
        .pet_top = 94,
        .pet_max_width = 224,
        .native_pet_scale_percent = 93,
    };
    return &layout;
}
