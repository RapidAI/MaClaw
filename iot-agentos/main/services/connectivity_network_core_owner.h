#pragma once

/*
 * Private ESP-NETIF/default-event-loop singleton owner.
 *
 * This is intentionally below Connectivity Service and outside Device/Platform
 * APIs.  The composition root still owns Wi-Fi driver, event-handler
 * instances, radio policy and the encompassing rollback order.  Callers may
 * observe only a normalized status/availability fact, never ESP-IDF objects.
 */

#include <stdbool.h>

#include "device_api.h"

device_status_t connectivity_network_core_owner_ensure(void);
bool connectivity_network_core_owner_ready(void);
bool connectivity_network_core_owner_has_resources(void);

/* Called only after Wi-Fi driver, application event handlers and all default
 * Wi-Fi netifs have been stopped/released by the physical root transaction. */
device_status_t connectivity_network_core_owner_release(void);
