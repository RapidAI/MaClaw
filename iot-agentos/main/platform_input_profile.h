#pragma once

/* Selected physical Input profile seam.  Platform Bootstrap owns one-time
 * panel/audio/peripheral construction; Input Service owns event envelopes and
 * queueing; this private family bridge owns scanner lifecycle and gesture
 * implementation below the neutral Platform Input contract. A completed
 * scanner stop may be followed by a fresh scanner task on already initialized
 * hardware; this is not a full hardware deinit/restart contract. */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef void (*platform_input_profile_publish_cb_t)(device_input_action_t action,
                                                    device_input_source_t source,
                                                    void *context);

device_status_t platform_input_profile_start(platform_input_profile_publish_cb_t on_input,
                                             void *context);
device_status_t platform_input_profile_stop(uint32_t timeout_ms);
void platform_input_profile_set_command_cancel_enabled(bool enabled);
