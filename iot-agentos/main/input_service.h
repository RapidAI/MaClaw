#pragma once

/* Internal implementation boundary for the Device API input lifecycle. */

#include "device_api.h"

device_status_t input_service_start(device_input_cb_t on_input, void *context);
device_status_t input_service_stop(uint32_t timeout_ms);
