#pragma once

/*
 * Internal Wake HAL SPI.  Wake configuration/parsing remains separate from
 * future Power HAL sleep entry.  This first slice intentionally publishes a
 * verified DISPLAY_OFF matrix only; it does not configure ESP-IDF wake APIs.
 */
#include "device_api.h"

device_status_t platform_wake_get_depth_capability(
    device_power_state_t target_state,
    device_wake_depth_capability_t *out_capability);

/* Value-only authorization seam between Wake policy and Platform Power.
 * `requested_sources` must be a non-empty subset of the selected profile's
 * already verified sources for the requested MCU-sleep depth.  Candidate
 * matrix entries are never accepted here.  This validates a policy value; it
 * neither arms a wake source nor changes any profile-private electrical
 * state. */
device_status_t platform_wake_authorize_verified_sleep_sources(
    device_power_state_t target_state,
    device_wake_source_flags_t requested_sources);
