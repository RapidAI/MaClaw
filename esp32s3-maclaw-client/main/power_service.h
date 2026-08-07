#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

/* Internal service behind the ISO-C Device Power API.  Board-specific panel,
 * rail and wake-control details remain below this header. */
device_status_t power_service_init(void);
device_status_t power_service_schedule_display_off(uint32_t idle_after_ms);
void power_service_cancel_display_off(void);
bool power_service_wake_display_from_user(void);
bool power_service_get_snapshot(device_power_snapshot_t *out_snapshot);
