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

typedef struct {
    safe_mode_coordinator_host_t host;
    safe_mode_stage_t stage;
    device_status_t terminal_status;
    bool configured;
    bool in_progress;
} safe_mode_coordinator_state_t;

static safe_mode_coordinator_state_t s_state;

static bool same_host(const safe_mode_coordinator_host_t *host) {
    return host->now_ms == s_state.host.now_ms &&
           host->quiesce_nonessential == s_state.host.quiesce_nonessential &&
           host->initialize_clock_feedback == s_state.host.initialize_clock_feedback &&
           host->initialize_alarm == s_state.host.initialize_alarm &&
           host->publish_diagnostic_surface == s_state.host.publish_diagnostic_surface &&
           host->context == s_state.host.context;
}

static uint32_t remaining_ms(uint64_t deadline_ms) {
    const uint64_t now_ms = s_state.host.now_ms(s_state.host.context);
    if (now_ms >= deadline_ms) return 0;
    const uint64_t remaining = deadline_ms - now_ms;
    return remaining > UINT32_MAX ? UINT32_MAX : (uint32_t)remaining;
}

static device_status_t call_stage(safe_mode_stage_t stage,
                                  device_status_t (*operation)(void *, uint32_t),
                                  uint64_t deadline_ms) {
    s_state.stage = stage;
    const uint32_t timeout_ms = remaining_ms(deadline_ms);
    if (timeout_ms == 0) return DEVICE_STATUS_TIMEOUT;
    return operation(s_state.host.context, timeout_ms);
}

static device_status_t publish_diagnostic(const safe_mode_entry_t *entry,
                                          uint64_t deadline_ms) {
    s_state.stage = SAFE_MODE_STAGE_PUBLISH_DIAGNOSTIC_SURFACE;
    const uint32_t timeout_ms = remaining_ms(deadline_ms);
    if (timeout_ms == 0) return DEVICE_STATUS_TIMEOUT;
    return s_state.host.publish_diagnostic_surface(s_state.host.context, entry, timeout_ms);
}

device_status_t safe_mode_coordinator_configure_host(
    const safe_mode_coordinator_host_t *host) {
    if (!valid_host(host)) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (s_state.configured) return same_host(host) ? DEVICE_STATUS_OK : DEVICE_STATUS_BUSY;
    memset(&s_state, 0, sizeof(s_state));
    s_state.host = *host;
    s_state.stage = SAFE_MODE_STAGE_IDLE;
    s_state.terminal_status = DEVICE_STATUS_OK;
    s_state.configured = true;
    return DEVICE_STATUS_OK;
}

device_status_t safe_mode_coordinator_enter(const safe_mode_entry_t *entry,
                                            uint32_t timeout_ms) {
    if (!valid_entry(entry) || timeout_ms == 0) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (!s_state.configured) return DEVICE_STATUS_UNAVAILABLE;
    if (s_state.in_progress || s_state.stage == SAFE_MODE_STAGE_COMPLETE ||
        s_state.stage == SAFE_MODE_STAGE_FAILED) {
        return DEVICE_STATUS_BUSY;
    }

    s_state.in_progress = true;
    const uint64_t now_ms = s_state.host.now_ms(s_state.host.context);
    const uint64_t deadline_ms = UINT64_MAX - now_ms < timeout_ms
                                     ? UINT64_MAX
                                     : now_ms + (uint64_t)timeout_ms;
    device_status_t status = call_stage(SAFE_MODE_STAGE_QUIESCE_NONESSENTIAL,
                                        s_state.host.quiesce_nonessential, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;
    status = call_stage(SAFE_MODE_STAGE_INITIALIZE_CLOCK_FEEDBACK,
                        s_state.host.initialize_clock_feedback, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;
    status = call_stage(SAFE_MODE_STAGE_INITIALIZE_ALARM,
                        s_state.host.initialize_alarm, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;
    status = publish_diagnostic(entry, deadline_ms);
    if (status != DEVICE_STATUS_OK) goto failed;

    s_state.stage = SAFE_MODE_STAGE_COMPLETE;
    s_state.terminal_status = DEVICE_STATUS_OK;
    s_state.in_progress = false;
    return DEVICE_STATUS_OK;

failed:
    s_state.stage = SAFE_MODE_STAGE_FAILED;
    s_state.terminal_status = status;
    s_state.in_progress = false;
    return status;
}

bool safe_mode_coordinator_get_snapshot(safe_mode_stage_t *out_stage,
                                        device_status_t *out_terminal_status) {
    if (!s_state.configured) return false;
    if (out_stage) *out_stage = s_state.stage;
    if (out_terminal_status) *out_terminal_status = s_state.terminal_status;
    return true;
}
