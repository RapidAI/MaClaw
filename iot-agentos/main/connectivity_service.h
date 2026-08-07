#pragma once

/* Internal Connectivity Service state.  The public contract is declared in
 * device_api.h; this header exists only to keep the Device API facade free of
 * ESP-IDF synchronization details. */

#include <stdbool.h>

#include "device_api.h"

void connectivity_service_set_active_uplink(device_uplink_t uplink);
bool connectivity_service_is_active_cellular(void);
void connectivity_service_set_wifi_ready(bool ready);
void connectivity_service_set_cellular_ready(bool ready);
bool connectivity_service_is_active_uplink_ready(void);
bool connectivity_service_get_snapshot(device_connectivity_snapshot_t *out_snapshot);
