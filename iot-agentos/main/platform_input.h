#pragma once

/*
 * Internal physical-input SPI.
 *
 * Platform Bootstrap establishes the selected profile's boot-lifetime
 * hardware first. Input Service owns the public event envelope and queueing;
 * this port owns only the selected adapter's normalized action/source
 * publisher and bounded scanner stop. It deliberately exposes no controller
 * handle, GPIO, touch coordinate, gesture timing or task handle. A successful
 * scanner stop/join may later accept a fresh publisher generation, but this
 * is not a panel/controller deinit or full hardware restart promise.
 */

#include <stdint.h>

#include "device_api.h"

typedef void (*platform_input_publish_cb_t)(device_input_action_t action,
                                            device_input_source_t source,
                                            void *context);

device_status_t platform_input_start(platform_input_publish_cb_t on_input,
                                     void *context);
device_status_t platform_input_stop(uint32_t timeout_ms);
/* A normalized, transient input-policy intent.  Touch/key implementation and
 * timing stay below this boundary. */
void platform_input_set_command_cancel_enabled(bool enabled);
