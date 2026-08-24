#include "services/safe_mode_coordinator.h"

#include <limits.h>
#include <string.h>

static bool valid_host(const safe_mode_coordinator_host_t *host) {
    return host && host->now_ms && host->quiesce_nonessential &&
           host->initialize_clock_feedback && host->initialize_alarm &&
           host->publish_diagnostic_surface;
}

static bool valid_entry(const safe_mode_entry_t *entry) {
    return entry && entry->struct_size == sizeof(*entry) &&
           entry->abi_version == SAFE_MODE_COORDINATOR_ABI_VERSION &&
           entry->failure_status != DEVICE_STATUS_OK;
}

static uint32_t remaining_ms(const safe_mode_coordinator_t *coordinator,
                             uint64_t deadline_ms) {
    const uint64_t now_ms = coordinator->host.now_ms(coordinator->host.context);
    if (now_ms >= deadline_ms) return 0;
    const uint64_t remaining = deadline_ms - now_ms;
    return remaining > UINT32_MAX ? UINT32_MAX : (uint32_t)remaining;
}

static device_status_t call_stage(safe_mode_coordinator_t *coordinator,
                                  safe_mode_stage_t stage,
                                  device_status_t (*operation)(void *, uint32_t),
                                  uint64_t deadline_ms) {
    coordinator->stage = stage;
    const uint32_t timeout_ms = remaining_ms(coordinator, deadline_ms);
    if (timeout_ms == 0) return DEVICE_STATUS_TIMEOUT;
    return operation(coordinator->host.context, timeout_ms);
}

static device_status_t publish_diagnostic(safe_mode_coordinator_t *coordinator,
                                          const safe_mode_entry_t *entry,
                                          uint64_t deadline_ms) {
    coordinator->stage = SAFE_MODE_STAGE_PUBLISH_DIAGNOSTIC_SURFACE;
    const uint32_t timeout_ms = remaining_ms(coordinator, deadline_ms);
    if (timeout_ms == 0) return DEVICE_STATUS_TIMEOUT;
    return coordinator->host.publish_diagnostic_surface(coordinator->host.context,
                                                         entry, timeout_ms);
}

device_status_t safe_mode_coordinator_init(
    safe_mode_coordinator_t *coordinator,
    const safe_mode_coordinator_host_t *host) {
    if (!coordinator || !valid_host(host)) return DEVICE_STATUS_INVALID_ARGUMENT;
    memset(coordinator, 0, sizeof(*coordinator));
    coordinator->host = *host;
    coordinator->stage = SAFE_MODE_STAGE_IDLE;
    coordinator->terminal_status = DEVICE_STATUS_OK;
    coordinator->initialized = true;
    return DEVICE_STATUS_OK;
}

device_status_t safe_mode_coordinator_enter(safe_mode_coordinator_t *coordinator,
                                            const safe_mode_entry_t *entry,
                                            uint32_t timeout_ms) {
    if (!coordinator || !valid_entry(entry) || timeout_ms == 0) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (!coordinator->initialized) return DEVICE_STATUS_UNAVAILABLE;
    if (coordinator->in_progress || coordinator->stage == SAFE_MODE_STAGE_COMPLETE ||
        coordinator->stage == SAFE_MODE_STAGE_FAILED) {
        return DEVICE_STATUS_BUSY;
    }

    coordinator->in_progress = true;
    const uint64_t now_ms = coordinator->host.now_ms(coordinator->host.context);
    const uint64_t deadline_ms = UINT64_MAX - now_ms < timeout_ms
                                     ? UINT64_MAX
                                     : now_ms + (uint64_t)timeout_ms;
    device_status_t status = call_stage(coordinator, SAFE_MODE_STAGE_QUIESCE_NONESSENTIAL,
                                        coordinator->host.quiesce_nonessential, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;
    status = call_stage(coordinator, SAFE_MODE_STAGE_INITIALIZE_CLOCK_FEEDBACK,
                        coordinator->host.initialize_clock_feedback, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;
    status = call_stage(coordinator, SAFE_MODE_STAGE_INITIALIZE_ALARM,
                        coordinator->host.initialize_alarm, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;
    status = publish_diagnostic(coordinator, entry, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;

    coordinator->stage = SAFE_MODE_STAGE_COMPLETE;
    coordinator->terminal_status = DEVICE_STATUS_OK;
    coordinator->in_progress = false;
    return DEVICE_STATUS_OK;

failed:
    coordinator->stage = SAFE_MODE_STAGE_FAILED;
    coordinator->terminal_status = status;
    coordinator->in_progress = false;
    return status;
}

bool safe_mode_coordinator_get_snapshot(const safe_mode_coordinator_t *coordinator,
                                        safe_mode_stage_t *out_stage,
                                        device_status_t *out_terminal_status) {
    if (!coordinator || !coordinator->initialized) return false;
    if (out_stage) *out_stage = coordinator->stage;
    if (out_terminal_status) *out_terminal_status = coordinator->terminal_status;
    return true;
}
