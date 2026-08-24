#pragma once

/*
 * Private, value-only policy for a full Connectivity fault-domain restart.
 *
 * This coordinator intentionally owns no ESP-IDF, FreeRTOS, HTTP, Wi-Fi,
 * portal or modem object.  The composition root supplies bounded bridges for
 * the concrete owners.  In particular it is not a System Sleep participant:
 * a restart crosses the physical network-root boundary, so a failed restart
 * must remain closed instead of using System Sleep ABORT to revive workers
 * that belonged to the retired physical generation.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef enum {
    CONNECTIVITY_RESTART_STAGE_IDLE = 0,
    CONNECTIVITY_RESTART_STAGE_QUIESCE_NETWORK_DEPENDENTS,
    CONNECTIVITY_RESTART_STAGE_STOP_PROVISIONING,
    CONNECTIVITY_RESTART_STAGE_STOP_PHYSICAL_ROOT,
    CONNECTIVITY_RESTART_STAGE_INITIALIZE_LOGICAL_CONNECTIVITY,
    CONNECTIVITY_RESTART_STAGE_INITIALIZE_PHYSICAL_ROOT,
    CONNECTIVITY_RESTART_STAGE_START_SELECTED_UPLINK,
    CONNECTIVITY_RESTART_STAGE_START_CLOCK_SYNC,
    CONNECTIVITY_RESTART_STAGE_REARM_GATEWAY,
    CONNECTIVITY_RESTART_STAGE_COMPLETE,
    CONNECTIVITY_RESTART_STAGE_FAILED,
} connectivity_restart_stage_t;

typedef struct {
    /* Monotonic value only.  The coordinator derives one parent deadline and
     * never grants a later stage more time than remains in that deadline. */
    uint64_t (*now_ms)(void *context);
    /* Each bridge must fully stop or initialize its own domain before it
     * returns OK.  They receive only a remaining timeout; SDK handles stay
     * private to their respective owners. */
    /* Stops every app-level owner that could enter a network client or
     * callback: Gateway/poll/meeting work plus root-owned optional media,
     * wake-restart and cellular-recovery participants. A future composition
     * bridge may delegate internally, but it must return OK only once the
     * complete dependent set is no longer able to touch the physical root. */
    device_status_t (*quiesce_network_dependents)(void *context,
                                                   uint32_t timeout_ms);
    device_status_t (*stop_provisioning)(void *context, uint32_t timeout_ms);
    device_status_t (*stop_physical_root)(void *context, uint32_t timeout_ms);
    device_status_t (*initialize_logical_connectivity)(void *context,
                                                        uint32_t timeout_ms);
    device_status_t (*initialize_physical_root)(void *context,
                                                 uint32_t timeout_ms);
    device_status_t (*start_selected_uplink)(void *context, uint32_t timeout_ms);
    device_status_t (*start_clock_sync)(void *context, uint32_t timeout_ms);
    device_status_t (*rearm_gateway)(void *context, uint32_t timeout_ms);
    void *context;
} connectivity_restart_coordinator_host_t;

typedef struct {
    connectivity_restart_coordinator_host_t host;
    connectivity_restart_stage_t stage;
    device_status_t terminal_status;
    bool initialized;
    bool in_progress;
    /* Once physical stop is attempted, no generic rollback is legal: all
     * later failures must remain terminal and fail-closed. */
    bool physical_root_stop_committed;
} connectivity_restart_coordinator_t;

device_status_t connectivity_restart_coordinator_init(
    connectivity_restart_coordinator_t *coordinator,
    const connectivity_restart_coordinator_host_t *host);

/* Executes one complete restart transaction.  A coordinator in COMPLETE or
 * FAILED requires reinitialization with an explicit new composition-root
 * generation; it cannot silently reuse prior callback admission or workers. */
device_status_t connectivity_restart_coordinator_restart(
    connectivity_restart_coordinator_t *coordinator, uint32_t timeout_ms);

bool connectivity_restart_coordinator_get_snapshot(
    const connectivity_restart_coordinator_t *coordinator,
    connectivity_restart_stage_t *out_stage,
    device_status_t *out_terminal_status,
    bool *out_physical_root_stop_committed);
