#pragma once

/*
 * Internal platform lifecycle boundary.
 *
 * Composition roots may request a bounded stop of board-owned background
 * work during a failed startup, but they must not take a dependency on the
 * legacy board_port facade or infer individual renderer task handles.  This
 * is intentionally not a board deinit or restart contract: the adapter keeps
 * ownership of LCD, audio, buses and board-lifetime driver objects.
 */

#include <stdint.h>

#include "device_api.h"

device_status_t platform_lifecycle_stop_board_background_tasks(uint32_t timeout_ms);
