#pragma once

/* Internal resource-observation port. Resource Pressure Service owns product
 * thresholds and admission policy; this port samples allocator and mounted
 * storage facts into the stable Device API value type. */

#include <stdbool.h>

#include "device_api.h"

bool platform_resource_sample(const char *storage_label, bool storage_available,
                              device_resource_pressure_snapshot_t *out_snapshot);