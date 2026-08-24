#pragma once

/* Shared, hardware-neutral Wake query service.  It owns no ESP-IDF wake
 * source and exposes only value types to domain/power policy. */
#include "device_api.h"

device_status_t wake_service_init(void);
void wake_service_deinit(void);
device_status_t wake_service_get_depth_capability(
    device_power_state_t target_state,
    device_wake_depth_capability_t *out_capability);
