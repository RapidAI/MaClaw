#include "services/connectivity_restart_coordinator.h"

#include <limits.h>
#include <string.h>

static bool valid_host(const connectivity_restart_coordinator_host_t *host) {
    return host && host->now_ms && host->quiesce_network_dependents && host->stop_provisioning &&
           host->stop_physical_root && host->initialize_logical_connectivity &&
           host->initialize_physical_root && host->start_selected_uplink &&
           host->start_clock_sync && host->rearm_gateway;
}

static uint32_t remaining_ms(const connectivity_restart_coordinator_t *coordinator,
                             uint64_t deadline_ms) {
    const uint64_t now_ms = coordinator->host.now_ms(coordinator->host.context);
    if (now_ms >= deadline_ms) return 0;
    const uint64_t remaining = deadline_ms - now_ms;
    return remaining > UINT32_MAX ? UINT32_MAX : (uint32_t)remaining;
}

static device_status_t call_stage(connectivity_restart_coordinator_t *coordinator,
                                  connectivity_restart_stage_t stage,
                                  device_status_t (*operation)(void *, uint32_t),
                                  uint64_t deadline_ms) {
    coordinator->stage = stage;
    const uint32_t timeout_ms = remaining_ms(coordinator, deadline_ms);
    if (timeout_ms == 0) return DEVICE_STATUS_TIMEOUT;
    const device_status_t status =
        operation(coordinator->host.context, timeout_ms);
    /* A bridge may return OK after consuming its entire allowance.  Treat
     * that as a bounded transaction miss, not evidence that the next stage
     * (or final COMPLETE) is still admissible.  Physical state remains
     * owned by the bridge; the coordinator only records the terminal result. */
    if (status == DEVICE_STATUS_OK && remaining_ms(coordinator, deadline_ms) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    return status;
}

/* `physical_root_stop_committed` is an evidence fact, not an intention.  A
 * parent deadline may expire after provisioning has stopped but before the
 * physical-root bridge is ever entered; in that case a recovery policy must
 * not infer that Wi-Fi/netif resources were retired.  Set the fact only at
 * the final point immediately before the bounded bridge is called. */
static device_status_t call_physical_root_stop(
    connectivity_restart_coordinator_t *coordinator, uint64_t deadline_ms) {
    coordinator->stage = CONNECTIVITY_RESTART_STAGE_STOP_PHYSICAL_ROOT;
    const uint32_t timeout_ms = remaining_ms(coordinator, deadline_ms);
    if (timeout_ms == 0) return DEVICE_STATUS_TIMEOUT;
    coordinator->physical_root_stop_committed = true;
    const device_status_t status =
        coordinator->host.stop_physical_root(coordinator->host.context, timeout_ms);
    if (status == DEVICE_STATUS_OK && remaining_ms(coordinator, deadline_ms) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    return status;
}

device_status_t connectivity_restart_coordinator_init(
    connectivity_restart_coordinator_t *coordinator,
    const connectivity_restart_coordinator_host_t *host) {
    if (!coordinator || !valid_host(host)) return DEVICE_STATUS_INVALID_ARGUMENT;
    memset(coordinator, 0, sizeof(*coordinator));
    coordinator->host = *host;
    coordinator->stage = CONNECTIVITY_RESTART_STAGE_IDLE;
    coordinator->terminal_status = DEVICE_STATUS_OK;
    coordinator->initialized = true;
    return DEVICE_STATUS_OK;
}

device_status_t connectivity_restart_coordinator_restart(
    connectivity_restart_coordinator_t *coordinator, uint32_t timeout_ms) {
    if (!coordinator || timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!coordinator->initialized) return DEVICE_STATUS_UNAVAILABLE;
    if (coordinator->in_progress ||
        coordinator->stage == CONNECTIVITY_RESTART_STAGE_COMPLETE ||
        coordinator->stage == CONNECTIVITY_RESTART_STAGE_FAILED) {
        return DEVICE_STATUS_BUSY;
    }

    coordinator->in_progress = true;
    const uint64_t now_ms = coordinator->host.now_ms(coordinator->host.context);
    const uint64_t deadline_ms = UINT64_MAX - now_ms < timeout_ms
                                     ? UINT64_MAX
                                     : now_ms + (uint64_t)timeout_ms;
    device_status_t status = call_stage(
        coordinator, CONNECTIVITY_RESTART_STAGE_QUIESCE_NETWORK_DEPENDENTS,
        coordinator->host.quiesce_network_dependents, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;
    status = call_stage(coordinator, CONNECTIVITY_RESTART_STAGE_STOP_PROVISIONING,
                        coordinator->host.stop_provisioning, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;

    status = call_physical_root_stop(coordinator, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;
    status = call_stage(coordinator,
                        CONNECTIVITY_RESTART_STAGE_INITIALIZE_LOGICAL_CONNECTIVITY,
                        coordinator->host.initialize_logical_connectivity, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;
    status = call_stage(coordinator,
                        CONNECTIVITY_RESTART_STAGE_INITIALIZE_PHYSICAL_ROOT,
                        coordinator->host.initialize_physical_root, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;
    status = call_stage(coordinator, CONNECTIVITY_RESTART_STAGE_START_SELECTED_UPLINK,
                        coordinator->host.start_selected_uplink, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;
    status = call_stage(coordinator, CONNECTIVITY_RESTART_STAGE_START_CLOCK_SYNC,
                        coordinator->host.start_clock_sync, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;
    status = call_stage(coordinator, CONNECTIVITY_RESTART_STAGE_REARM_GATEWAY,
                        coordinator->host.rearm_gateway, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;

    coordinator->stage = CONNECTIVITY_RESTART_STAGE_COMPLETE;
    coordinator->terminal_status = DEVICE_STATUS_OK;
    coordinator->in_progress = false;
    return DEVICE_STATUS_OK;

failed:
    /* Never invoke an implicit rollback.  Before physical stop this avoids
     * resurrecting only a subset of workers; after it, an ABORT would revive
     * workers against a retired network root.  The composition root must
     * explicitly select a safe recovery or reboot fault-domain policy. */
    coordinator->stage = CONNECTIVITY_RESTART_STAGE_FAILED;
    coordinator->terminal_status = status;
    coordinator->in_progress = false;
    return status;
}

bool connectivity_restart_coordinator_get_snapshot(
    const connectivity_restart_coordinator_t *coordinator,
    connectivity_restart_stage_t *out_stage,
    device_status_t *out_terminal_status,
    bool *out_physical_root_stop_committed) {
    if (!coordinator || !coordinator->initialized) return false;
    if (out_stage) *out_stage = coordinator->stage;
    if (out_terminal_status) *out_terminal_status = coordinator->terminal_status;
    if (out_physical_root_stop_committed) {
        *out_physical_root_stop_committed = coordinator->physical_root_stop_committed;
    }
    return true;
}
