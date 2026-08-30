#pragma once

/* Connectivity-domain System Sleep coordinator for restartable Gateway work.
 * Gateway Transport owns its concrete HTTP registry and is called directly
 * through its value-typed cancellation API. No HAL-facing contract exposes
 * ESP-IDF, RTOS, HTTP, JSON, modem or board types. */

#include <stdint.h>

#include "device_api.h"

device_status_t gateway_lifecycle_service_init(void);

/* Cancels in-flight Gateway HTTP through the host seam, then stops only
 * workers with a paired, generation-aware ABORT contract. */
device_status_t gateway_lifecycle_service_prepare_system_sleep(uint32_t timeout_ms);

/* Restores only generations present before PREPARE, in reverse order. It does
 * not reopen Connectivity admission or configure physical Wi-Fi/4G hardware. */
void gateway_lifecycle_service_abort_system_sleep_prepare(void);

/* Establishes the same bounded Gateway/Meeting retirement fence for a
 * Connectivity fault-domain restart. Unlike the System Sleep entry point,
 * this function has no paired ABORT contract: its caller must follow it only
 * with commit_prepared_network_restart(), which permanently retires the old
 * generation before a physical network root is stopped. It exists so runtime
 * restart policy never has to describe a terminal fault-domain transition as
 * a reversible sleep transaction. */
device_status_t gateway_lifecycle_service_prepare_network_restart(uint32_t timeout_ms);

/* Commits a fully prepared network-restart fence. This is intentionally not
 * a System Sleep COMMIT: it only retires Gateway/Meeting worker generations
 * so a subsequent physical-network stop cannot leave a worker eligible for
 * ABORT against the old root. A new generation is created only by the
 * explicit post-uplink rearm stage. */
device_status_t gateway_lifecycle_service_commit_prepared_network_restart(void);
