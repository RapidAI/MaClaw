#pragma once

#include "../compact_standby_layout.h"

/* Fangtang's 240x240 standby surface has two information rows. Transfer
 * chunking belongs to the Display HAL, not this visual geometry contract. */
static inline const compact_standby_layout_t *fangtang_standby_layout_adapter(void) {
    static const compact_standby_layout_t layout = {
        .weather_text_y = 0, /* Fangtang's compact standby has no weather row. */
        .weather_scale_num = 1,
        .weather_scale_den = 1,
        /* A full 220 px pet rectangle forces every pose tick to compare and
         * transmit most of the 240 px panel.  HIL with the retained 176 px
         * pack showed that a dense two-pose blend can still exceed the 50 ms
         * target on the NV3023's 40 MHz single-lane transport.  Keep this
         * purely visual-profile geometry at 160 px: it reduces the maximum
         * changed pixel area by about 17% while retaining a stable margin for
         * the clock/calendar.  The shared scene continues to request a pet;
         * it has no Fangtang-specific size or transport knowledge. */
        .pet_top = 62,
        .pet_max_width = 160,
        .native_pet_scale_percent = 75,
    };
    return &layout;
}
