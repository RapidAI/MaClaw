#pragma once

/* Internal implementation boundary for the Device API input lifecycle. */

#include "device_api.h"

device_status_t input_service_start(device_input_cb_t on_input, void *context);
device_status_t input_service_stop(uint32_t timeout_ms);
/* Input-policy forwarding remains synchronous and carries no display state.
 * The selected platform adapter may treat this as a no-op when its physical
 * controls do not need a different gesture-recognition window. */
void input_service_set_command_cancel_enabled(bool enabled);
