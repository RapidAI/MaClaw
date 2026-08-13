#pragma once

/*
 * Normalized circular-board input policy.
 *
 * This private HAL seam deliberately contains only gesture scalar values.
 * GPIOs, touch-controller handles, FreeRTOS task placement and controller
 * gesture encodings remain in the selected profile-private input adapter.
 * The shared scanner consumes this policy without selecting a board model.
 */

#include <stdbool.h>
#include <stdint.h>

typedef struct {
    bool touch_initialization_required;
    uint16_t debounce_ms;
    uint16_t scan_poll_ms;
    uint16_t double_tap_window_ms;
    uint16_t long_hold_ms;
    uint16_t touch_regular_min_tap_ms;
    uint16_t touch_cancel_min_tap_ms;
    uint16_t touch_double_min_gap_ms;
    uint16_t touch_release_drain_ms;
} round_input_profile_t;
