#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

/* Internal service behind the ISO-C Device Power API.  Board-specific panel,
 * rail and wake-control details remain below this header. */
device_status_t power_service_init(void);
/* Stops the DISPLAY_OFF timer after any in-flight transition leaves the
 * board adapter. The Device API owns admission closure and final lease drain
 * around this narrower scheduler boundary. */
device_status_t power_service_deinit(uint32_t timeout_ms);
device_status_t power_service_schedule_display_off(uint32_t idle_after_ms);
void power_service_cancel_display_off(void);
bool power_service_wake_display_from_user(void);
/* A domain deadline may restore a schedule-owned DISPLAY_OFF panel without
 * synthesizing a physical input event. */
bool power_service_wake_display_from_schedule(void);
/* A remote management request may restore a DISPLAY_OFF panel without
 * synthesizing physical input or changing manual-wake scheduling policy. */
bool power_service_wake_display_from_remote_control(void);
bool power_service_get_snapshot(device_power_snapshot_t *out_snapshot);
