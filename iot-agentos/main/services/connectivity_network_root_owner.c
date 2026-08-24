#include "services/connectivity_network_root_owner.h"

#include "esp_timer.h"

#include "services/connectivity_network_core_owner.h"
#include "services/provisioning_network_owner.h"

static connectivity_network_root_owner_lifecycle_host_t s_lifecycle_host;
static bool s_lifecycle_host_configured;

static bool lifecycle_host_matches(
    const connectivity_network_root_owner_lifecycle_host_t *host) {
    return host && s_lifecycle_host.stop_provisioning == host->stop_provisioning &&
           s_lifecycle_host.stop_callback_admission == host->stop_callback_admission &&
           s_lifecycle_host.stop_clock_sync == host->stop_clock_sync &&
           s_lifecycle_host.provisioning_has_live_resources ==
               host->provisioning_has_live_resources &&
           s_lifecycle_host.context == host->context;
}

static uint32_t remaining_timeout_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const int64_t rounded_ms = (remaining_us + 999) / 1000;
    return rounded_ms > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded_ms;
}

device_status_t connectivity_network_root_owner_configure_lifecycle_host(
    const connectivity_network_root_owner_lifecycle_host_t *host) {
    if (!host || !host->stop_provisioning || !host->stop_callback_admission ||
        !host->stop_clock_sync || !host->provisioning_has_live_resources) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    /* Callback bridges belong to one physical generation.  Rebinding them
     * after ESP-NETIF, Wi-Fi or either default netif exists could direct a
     * later stop into a different composition root.  The same host is safe
     * and keeps repeated cold-start checks idempotent. */
    if (s_lifecycle_host_configured && !lifecycle_host_matches(host) &&
        (connectivity_network_core_owner_has_resources() ||
         connectivity_wifi_driver_owner_initialized() ||
         provisioning_network_owner_has_resources())) {
        return DEVICE_STATUS_BUSY;
    }
    s_lifecycle_host = *host;
    s_lifecycle_host_configured = true;
    return DEVICE_STATUS_OK;
}

device_status_t connectivity_network_root_owner_ensure_core(void) {
    return connectivity_network_core_owner_ensure();
}

bool connectivity_network_root_owner_core_ready(void) {
    return connectivity_network_core_owner_ready();
}

bool connectivity_network_root_owner_has_resources(void) {
    return connectivity_network_core_owner_has_resources() ||
           connectivity_wifi_driver_owner_initialized() ||
           provisioning_network_owner_has_resources();
}

device_status_t connectivity_network_root_owner_initialize_wifi(
    connectivity_wifi_driver_event_callback_t callback, void *callback_arg) {
    if (!connectivity_network_core_owner_ready()) return DEVICE_STATUS_UNAVAILABLE;
    return connectivity_wifi_driver_owner_initialize(callback, callback_arg);
}

bool connectivity_network_root_owner_wifi_ready(void) {
    return connectivity_network_core_owner_ready() &&
           connectivity_wifi_driver_owner_ready();
}

bool connectivity_network_root_owner_wifi_has_resources(void) {
    return connectivity_wifi_driver_owner_initialized();
}

static device_status_t stop_provisioning_under_deadline(int64_t deadline_us) {
    const uint32_t remaining = remaining_timeout_ms(deadline_us);
    if (remaining == 0) return DEVICE_STATUS_TIMEOUT;
    return s_lifecycle_host.stop_provisioning(s_lifecycle_host.context, remaining);
}

device_status_t connectivity_network_root_owner_stop_provisioning(uint32_t timeout_ms) {
    if (timeout_ms == 0 || !s_lifecycle_host_configured) {
        return timeout_ms == 0 ? DEVICE_STATUS_INVALID_ARGUMENT : DEVICE_STATUS_UNAVAILABLE;
    }
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    return stop_provisioning_under_deadline(deadline_us);
}

device_status_t connectivity_network_root_owner_stop(
    uint32_t timeout_ms, bool *out_wifi_radio_stopped) {
    if (out_wifi_radio_stopped) *out_wifi_radio_stopped = false;
    if (timeout_ms == 0 || !s_lifecycle_host_configured) {
        return timeout_ms == 0 ? DEVICE_STATUS_INVALID_ARGUMENT : DEVICE_STATUS_UNAVAILABLE;
    }

    /* Do not rely solely on each caller remembering the two-phase order.
     * Portal HTTP/DNS and its post-save coordinator can still enter Wi-Fi,
     * so physical teardown remains unavailable until that owner reports its
     * generation fully retired. */
    if (s_lifecycle_host.provisioning_has_live_resources(s_lifecycle_host.context)) {
        return DEVICE_STATUS_BUSY;
    }

    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    uint32_t remaining = remaining_timeout_ms(deadline_us);
    if (remaining == 0) return DEVICE_STATUS_TIMEOUT;
    device_status_t status =
        s_lifecycle_host.stop_callback_admission(s_lifecycle_host.context, remaining);
    if (status != DEVICE_STATUS_OK) return status;

    remaining = remaining_timeout_ms(deadline_us);
    if (remaining == 0) return DEVICE_STATUS_TIMEOUT;
    status = s_lifecycle_host.stop_clock_sync(s_lifecycle_host.context, remaining);
    if (status != DEVICE_STATUS_OK) return status;

    if (connectivity_wifi_driver_owner_initialized() &&
        connectivity_wifi_driver_owner_started()) {
        status = connectivity_wifi_driver_owner_stop();
        if (status != DEVICE_STATUS_OK) return status;
        if (out_wifi_radio_stopped) *out_wifi_radio_stopped = true;
    }

    status = connectivity_wifi_driver_owner_unregister_application_handlers();
    if (status != DEVICE_STATUS_OK) return status;

    /* ESP-IDF exposes default-Wi-Fi-netif destruction as void. Each owner
     * retains its handle until this exact point, after radio/routes stop. */
    provisioning_network_owner_release_setup_ap();
    provisioning_network_owner_release_station();

    if (connectivity_wifi_driver_owner_initialized()) {
        status = connectivity_wifi_driver_owner_deinitialize();
        if (status != DEVICE_STATUS_OK) return status;
    }
    return connectivity_network_core_owner_release();
}
