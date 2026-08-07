#pragma once

/* Internal boot coordinator seam.  It owns only lifecycle observation and
 * controlled local-startup failure; board resources remain in their services. */

#include "device_api.h"

void lifecycle_service_begin(void);
void lifecycle_service_reach(device_runtime_phase_t phase);
void lifecycle_service_degrade(device_runtime_phase_t failed_phase,
                               device_status_t failure_status);
bool lifecycle_service_get_snapshot(device_runtime_snapshot_t *out_snapshot);
