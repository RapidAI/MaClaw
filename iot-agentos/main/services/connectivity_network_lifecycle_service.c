#include "services/connectivity_network_lifecycle_service.h"

#include <limits.h>
#include <string.h>

static connectivity_network_lifecycle_service_host_t s_host;
static bool s_initialized;

static bool host_valid(const connectivity_network_lifecycle_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) && host->now_ms &&
           host->initialize_logical && host->configure_physical_lifecycle &&
           host->physical_has_resources && host->physical_core_ready &&
           host->ensure_physical_core && host->wifi_has_resources &&
           host->wifi_ready && host->initialize_wifi &&
           host->open_wifi_callback_admission && host->stop_physical &&
           host->deinitialize_logical;
}

static uint32_t remaining_ms(uint64_t deadline_ms) {
    const uint64_t now_ms = s_host.now_ms(s_host.context);
    if (now_ms >= deadline_ms) return 0;
    const uint64_t remaining = deadline_ms - now_ms;
    return remaining > UINT32_MAX ? UINT32_MAX : (uint32_t)remaining;
}

static device_status_t stop_under_deadline(uint64_t deadline_ms,
                                           bool *out_wifi_radio_stopped) {
    if (out_wifi_radio_stopped) *out_wifi_radio_stopped = false;
    uint32_t remaining = remaining_ms(deadline_ms);
    if (remaining == 0) return DEVICE_STATUS_TIMEOUT;
    device_status_t status = s_host.stop_physical(
        remaining, out_wifi_radio_stopped, s_host.context);
    if (status != DEVICE_STATUS_OK) return status;
    /* A physical stop callback is only an attempt boundary.  Some SDK
     * teardown APIs are asynchronous or can report success after retaining a
     * partial netif/driver generation.  Never deinitialize the logical
     * Connectivity state while such a generation can still call back into it;
     * leave the whole domain closed and require a later terminal recovery. */
    if (s_host.physical_has_resources(s_host.context)) {
        return DEVICE_STATUS_BUSY;
    }
    remaining = remaining_ms(deadline_ms);
    if (remaining == 0) return DEVICE_STATUS_TIMEOUT;
    const device_status_t deinitialize_status =
        s_host.deinitialize_logical(remaining, s_host.context);
    if (deinitialize_status != DEVICE_STATUS_OK) return deinitialize_status;
    /* Logical teardown is the terminal step, but a host may report success
     * only after consuming its entire remaining allowance.  Do not surface a
     * completed stop when the parent deadline can no longer prove that the
     * generation was retired within budget. */
    return remaining_ms(deadline_ms) == 0u ? DEVICE_STATUS_TIMEOUT
                                          : DEVICE_STATUS_OK;
}

static device_status_t rollback_failed_start(void) {
    /* The old root has no useful logical lifetime if its first physical
     * allocation failed. If any physical marker remains, force the complete
     * physical->logical terminal order; otherwise only release logical state. */
    const uint64_t now_ms = s_host.now_ms(s_host.context);
    const uint64_t deadline_ms = UINT64_MAX - now_ms < 500u ? UINT64_MAX : now_ms + 500u;
    if (s_host.physical_has_resources(s_host.context)) {
        return stop_under_deadline(deadline_ms, NULL);
    }
    const uint32_t remaining = remaining_ms(deadline_ms);
    return remaining ? s_host.deinitialize_logical(remaining, s_host.context)
                     : DEVICE_STATUS_TIMEOUT;
}

device_status_t connectivity_network_lifecycle_service_init(
    const connectivity_network_lifecycle_service_host_t *host) {
    if (!host_valid(host)) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (s_initialized) return memcmp(&s_host, host, sizeof(*host)) == 0
                                  ? DEVICE_STATUS_OK
                                  : DEVICE_STATUS_BUSY;
    s_host = *host;
    s_initialized = true;
    return DEVICE_STATUS_OK;
}

device_status_t connectivity_network_lifecycle_service_ensure_core(void) {
    if (!s_initialized) return DEVICE_STATUS_UNAVAILABLE;
    if (s_host.physical_core_ready(s_host.context)) return DEVICE_STATUS_OK;
    if (s_host.physical_has_resources(s_host.context)) return DEVICE_STATUS_BUSY;

    device_status_t status = s_host.initialize_logical(s_host.context);
    if (status != DEVICE_STATUS_OK) return status;
    status = s_host.configure_physical_lifecycle(s_host.context);
    if (status != DEVICE_STATUS_OK) {
        (void)rollback_failed_start();
        return status;
    }
    status = s_host.ensure_physical_core(s_host.context);
    if (status == DEVICE_STATUS_OK &&
        !s_host.physical_core_ready(s_host.context)) {
        /* Do not publish a logical generation behind a core callback which
         * claimed success without leaving a ready singleton root. */
        status = DEVICE_STATUS_BUSY;
    }
    if (status != DEVICE_STATUS_OK) {
        (void)rollback_failed_start();
    }
    return status;
}

device_status_t connectivity_network_lifecycle_service_ensure_wifi(void) {
    if (!s_initialized) return DEVICE_STATUS_UNAVAILABLE;
    device_status_t status = connectivity_network_lifecycle_service_ensure_core();
    if (status != DEVICE_STATUS_OK) return status;
    if (s_host.wifi_has_resources(s_host.context)) {
        return s_host.wifi_ready(s_host.context) ? DEVICE_STATUS_OK : DEVICE_STATUS_BUSY;
    }
    status = s_host.initialize_wifi(s_host.context);
    if (status != DEVICE_STATUS_OK) {
        (void)rollback_failed_start();
        return status;
    }
    if (!s_host.wifi_ready(s_host.context)) {
        /* Driver/event-route initialization must publish a complete ready
         * generation before callback admission opens. */
        (void)rollback_failed_start();
        return DEVICE_STATUS_BUSY;
    }
    s_host.open_wifi_callback_admission(s_host.context);
    return DEVICE_STATUS_OK;
}

device_status_t connectivity_network_lifecycle_service_stop(
    uint32_t timeout_ms, bool *out_wifi_radio_stopped) {
    if (out_wifi_radio_stopped) *out_wifi_radio_stopped = false;
    if (!s_initialized) return DEVICE_STATUS_UNAVAILABLE;
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const uint64_t now_ms = s_host.now_ms(s_host.context);
    const uint64_t deadline_ms = UINT64_MAX - now_ms < timeout_ms
                                     ? UINT64_MAX
                                     : now_ms + (uint64_t)timeout_ms;
    return stop_under_deadline(deadline_ms, out_wifi_radio_stopped);
}
