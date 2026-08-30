#pragma once

/*
 * Connectivity-domain recovery coordinator.
 *
 * The profile-local modem driver remains below Device API / Platform
 * Connectivity.  This service owns only the common recovery lifecycle:
 * bounded cellular retry, Wi-Fi recovery notification, Gateway-startup rearm
 * and the reversible System Sleep participant.  Composition root injects its
 * presentation and Gateway seams, so no service imports main.c state.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef struct {
    uint32_t struct_size;
    /* Publish the common 4G network state to ambient/UI/identity surfaces. */
    void (*publish_network_ready)(bool ready, void *context);
    bool (*gateway_startup_running)(void *context);
    /* The Wi-Fi callback owns only physical event observation. This value
     * seam lets the coordinator preserve the existing boot admission gate
     * without importing composition-root state. Unlike cellular recovery,
     * initial Wi-Fi Gateway start is intentionally blocked after startup has
     * completed. */
    bool (*wifi_gateway_startup_recovery_allowed)(void *context);
    bool (*gateway_startup_eligible)(void *context);
    bool (*start_gateway_startup)(void *context);
    void *context;
} cellular_recovery_service_host_t;

device_status_t cellular_recovery_service_init(
    const cellular_recovery_service_host_t *host);

/* Establishes the initial bounded cellular session, publishes fail-closed
 * state first, and keeps a recovery coordinator available after either
 * outcome.  It never makes a board/profile-specific modem call directly. */
device_status_t cellular_recovery_service_establish_initial(uint32_t timeout_ms);

/* Idempotently starts the selected-uplink recovery coordinator. */
bool cellular_recovery_service_ensure_running(void);

/* Called only after Connectivity Service has accepted a matching GOT_IP
 * observation. The coordinator applies the common provisioning/sleep/Gateway
 * guards and idempotently restarts a pre-startup Gateway worker when allowed.
 * It neither reads ESP event data nor starts or configures Wi-Fi hardware. */
void cellular_recovery_service_note_wifi_ready(void);

/* Reversible participant: PREPARE stops only a pre-existing coordinator;
 * ABORT restores only that generation after a failed parent transaction. */
device_status_t cellular_recovery_service_prepare_system_sleep(uint32_t timeout_ms);
void cellular_recovery_service_abort_system_sleep_prepare(void);

/* Terminal fault-domain fence for the retry coordinator. The caller must
 * commit it before stopping a physical network root; COMMIT deliberately
 * leaves recovery admission closed and does not deinitialize a modem/UART. */
device_status_t cellular_recovery_service_prepare_network_restart(uint32_t timeout_ms);
device_status_t cellular_recovery_service_commit_prepared_network_restart(void);
