#include "services/wifi_startup_service.h"

#include <string.h>

typedef struct {
    uint8_t order[WIFI_STARTUP_SERVICE_MAX_SAVED_NETWORKS];
    int8_t best_rssi[WIFI_STARTUP_SERVICE_MAX_SAVED_NETWORKS];
    uint8_t count;
    const wifi_startup_service_request_t *request;
} saved_candidates_t;

static bool host_valid(const wifi_startup_service_host_t *host) {
    return host && host->ensure_network && host->ensure_station &&
           host->set_station_policy && host->configure_station &&
           host->enterprise_enabled && host->configure_enterprise &&
           host->disable_enterprise && host->wifi_started && host->wifi_start &&
           host->wifi_connect && host->wifi_disconnect && host->scan_visible &&
           host->select_saved_network && host->begin_attempt && host->wait_attempt &&
           host->publish_network_ready && host->setup_portal_active;
}

static bool request_valid(const wifi_startup_service_request_t *request) {
    return request && request->ssid && request->password &&
           request->connect_timeout_ms != 0 &&
           request->saved_network_count <= WIFI_STARTUP_SERVICE_MAX_SAVED_NETWORKS &&
           (!request->saved_network_count || request->saved_networks);
}

static bool collect_saved_candidate(const char *ssid, int8_t rssi, void *context) {
    saved_candidates_t *candidates = context;
    if (!ssid || !candidates || !candidates->request) return false;
    for (uint32_t index = 0; index < candidates->request->saved_network_count; ++index) {
        const wifi_startup_service_saved_network_t *network =
            &candidates->request->saved_networks[index];
        if (!network->ssid || !network->password || strcmp(ssid, network->ssid) != 0) {
            continue;
        }
        uint8_t position = 0;
        while (position < candidates->count && candidates->order[position] != index) {
            ++position;
        }
        if (position < candidates->count && candidates->best_rssi[position] >= rssi) {
            return true;
        }
        if (position < candidates->count) {
            for (uint8_t shift = position; shift + 1u < candidates->count; ++shift) {
                candidates->order[shift] = candidates->order[shift + 1u];
                candidates->best_rssi[shift] = candidates->best_rssi[shift + 1u];
            }
            --candidates->count;
        }
        position = candidates->count;
        while (position > 0 && candidates->best_rssi[position - 1u] < rssi) {
            candidates->order[position] = candidates->order[position - 1u];
            candidates->best_rssi[position] = candidates->best_rssi[position - 1u];
            --position;
        }
        candidates->order[position] = (uint8_t)index;
        candidates->best_rssi[position] = rssi;
        ++candidates->count;
        return true;
    }
    return true;
}

static bool try_saved_networks(const wifi_startup_service_host_t *host,
                               const wifi_startup_service_request_t *request,
                               const wifi_startup_service_saved_network_t **out_selected) {
    if (out_selected) *out_selected = NULL;
    if (request->boot_provisioning_staged || request->enterprise ||
        request->saved_network_count == 0 || request->scan_maximum_records == 0 ||
        request->candidate_connect_timeout_ms == 0) {
        return false;
    }

    host->set_station_policy(false, false, host->context);
    if (!host->wifi_started(host->context) &&
        host->wifi_start(host->context) != DEVICE_STATUS_OK) {
        host->set_station_policy(true, false, host->context);
        return false;
    }

    saved_candidates_t candidates = {.request = request};
    if (host->scan_visible(request->scan_maximum_records, collect_saved_candidate,
                           &candidates, host->context) != DEVICE_STATUS_OK ||
        candidates.count == 0) {
        host->set_station_policy(true, false, host->context);
        return false;
    }

    bool connected = false;
    const wifi_startup_service_saved_network_t *selected_network = NULL;
    for (uint8_t index = 0; index < candidates.count && !connected; ++index) {
        const wifi_startup_service_saved_network_t *network =
            &request->saved_networks[candidates.order[index]];
        /* Keep the selected runtime credentials in step with the original
         * cold-start path even if physical configuration of this candidate
         * fails. The regular fallback below deliberately reconnects using
         * the last candidate the user selected, not necessarily the strongest
         * visible candidate. */
        host->select_saved_network(network->ssid, network->password, host->context);
        selected_network = network;
        const wifi_startup_service_station_config_t station = {
            .ssid = network->ssid,
            .password = network->password,
            .enterprise = false,
            .keep_setup_ap = host->setup_portal_active(host->context),
        };
        if (host->configure_station(&station, host->context) != DEVICE_STATUS_OK) {
            continue;
        }
        host->publish_network_ready(network->ssid, false, host->context);
        if (index > 0) {
            const device_status_t disconnect = host->wifi_disconnect(host->context);
            /* A best-effort disconnect cannot suppress the next candidate:
             * the original radio owner merely reported this condition, then
             * created a fresh Connectivity attempt epoch. */
            (void)disconnect;
        }
        const uint32_t attempt = host->begin_attempt(network->ssid, host->context);
        if (attempt == 0) break;
        const device_status_t connect = host->wifi_connect(host->context);
        if (connect != DEVICE_STATUS_OK && connect != DEVICE_STATUS_BUSY) continue;
        connected = host->wait_attempt(attempt, request->candidate_connect_timeout_ms,
                                       host->context);
    }
    host->set_station_policy(true, false, host->context);
    if (!connected) {
        /* The normal fallback preserves legacy auto-reconnect behavior for
         * the actual final selection. Do not reconfigure the station here:
         * this is intentionally the same physical state the candidate loop
         * left with the driver owner. */
        const char *selected = selected_network ? selected_network->ssid : request->ssid;
        host->publish_network_ready(selected, false, host->context);
        if (out_selected) {
            *out_selected = selected_network;
        }
    }
    return connected;
}

device_status_t wifi_startup_service_connect(
    const wifi_startup_service_host_t *host,
    const wifi_startup_service_request_t *request) {
    if (!host_valid(host) || !request_valid(request)) return DEVICE_STATUS_INVALID_ARGUMENT;

    device_status_t status = host->ensure_network(host->context);
    if (status != DEVICE_STATUS_OK) return status;
    status = host->ensure_station(host->context);
    if (status != DEVICE_STATUS_OK) return status;

    host->set_station_policy(true, false, host->context);
    wifi_startup_service_station_config_t station = {
        .ssid = request->ssid,
        .password = request->password,
        .enterprise = request->enterprise,
        .keep_setup_ap = host->setup_portal_active(host->context),
    };
    status = host->configure_station(&station, host->context);
    if (status != DEVICE_STATUS_OK) return status;

    const wifi_startup_service_saved_network_t *saved_selection = NULL;
    if (try_saved_networks(host, request, &saved_selection)) return DEVICE_STATUS_OK;

    const char *active_ssid = saved_selection ? saved_selection->ssid : request->ssid;

    if (request->enterprise) {
        status = host->configure_enterprise(&request->enterprise_config, host->context);
        if (status != DEVICE_STATUS_OK) {
            if (host->enterprise_enabled(host->context)) {
                (void)host->disable_enterprise(host->context);
            }
            return status;
        }
    } else if (host->enterprise_enabled(host->context)) {
        status = host->disable_enterprise(host->context);
        if (status != DEVICE_STATUS_OK) return status;
    }

    const uint32_t attempt = host->begin_attempt(active_ssid, host->context);
    if (attempt == 0) return DEVICE_STATUS_BUSY;
    host->publish_network_ready(active_ssid, false, host->context);
    if (!host->wifi_started(host->context)) {
        status = host->wifi_start(host->context);
    } else {
        status = host->wifi_connect(host->context);
    }
    if (status != DEVICE_STATUS_OK && status != DEVICE_STATUS_BUSY) return status;
    if (host->wait_attempt(attempt, request->connect_timeout_ms, host->context)) {
        return DEVICE_STATUS_OK;
    }
    host->publish_network_ready(active_ssid, false, host->context);
    return DEVICE_STATUS_TIMEOUT;
}
