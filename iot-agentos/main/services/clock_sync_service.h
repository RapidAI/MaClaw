#pragma once

/*
 * Connectivity-domain wall-clock coordinator.
 *
 * Owns the ESP-NETIF SNTP singleton, its bounded retry monitor and its
 * reversible System Sleep participant.  Consumers receive only a trusted
 * epoch through value callbacks; they never observe SNTP, RTOS task handles,
 * callbacks or board/network implementation details.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef struct {
    uint32_t struct_size;
    /* Starts the independent standby cadence. It is deliberately separate
     * from time validity: a device can show a local/unsynchronised clock
     * while SNTP retries. */
    void (*ensure_ambient_clock)(void *context);
    /* Records the trusted epoch in the shared Ambient model. */
    void (*note_wall_clock)(int64_t epoch_sec, void *context);
    /* Re-evaluates timer-based business deadlines after a clock correction. */
    void (*notify_wall_clock_updated)(void *context);
    void *context;
} clock_sync_service_host_t;

device_status_t clock_sync_service_init(const clock_sync_service_host_t *host);

/* Starts the SNTP singleton and its retry monitor. A resume call can recreate
 * only the generation recorded by a failed System Sleep PREPARE. */
device_status_t clock_sync_service_start(bool system_sleep_resume);

/* Authenticated Hub time uses the same notification path as an SNTP callback.
 * The caller has already validated and applied the wall-clock value. */
void clock_sync_service_note_authenticated_epoch(int64_t epoch_sec);

/* Stops monitor first, then releases the ESP-NETIF SNTP singleton. Used only
 * by full Connectivity root teardown after callback admission has closed. */
device_status_t clock_sync_service_stop(uint32_t timeout_ms);

device_status_t clock_sync_service_prepare_system_sleep(uint32_t timeout_ms);
void clock_sync_service_abort_system_sleep_prepare(void);

