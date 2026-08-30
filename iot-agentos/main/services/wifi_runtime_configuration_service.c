#include "services/wifi_runtime_configuration_service.h"

#include <stdatomic.h>
#include <string.h>

static atomic_bool s_initialized = ATOMIC_VAR_INIT(false);
static atomic_bool s_boot_snapshot_captured = ATOMIC_VAR_INIT(false);
static atomic_flag s_lock = ATOMIC_FLAG_INIT;
static wifi_runtime_configuration_snapshot_t s_snapshot;

static void lock(void) {
    while (atomic_flag_test_and_set_explicit(&s_lock, memory_order_acquire)) {}
}

static void unlock(void) {
    atomic_flag_clear_explicit(&s_lock, memory_order_release);
}

static bool initialized(void) {
    return atomic_load_explicit(&s_initialized, memory_order_acquire);
}

static bool captured(void) {
    return atomic_load_explicit(&s_boot_snapshot_captured, memory_order_acquire);
}

static bool bounded(const char *value, size_t capacity) {
    return value && memchr(value, '\0', capacity) != NULL;
}

static void copy_value(char *out, size_t capacity, const char *value) {
    if (!out || capacity == 0u) return;
    if (!value) {
        out[0] = '\0';
        return;
    }
    size_t length = 0u;
    while (length + 1u < capacity && value[length]) ++length;
    memcpy(out, value, length);
    out[length] = '\0';
}

static bool snapshot_valid(const configuration_snapshot_t *snapshot) {
    if (!snapshot || snapshot->wifi_network_count > CONFIGURATION_WIFI_NETWORK_CAPACITY ||
        !bounded(snapshot->wifi_ssid, sizeof(snapshot->wifi_ssid)) ||
        !bounded(snapshot->wifi_password, sizeof(snapshot->wifi_password)) ||
        !bounded(snapshot->wifi_security, sizeof(snapshot->wifi_security)) ||
        !bounded(snapshot->wifi_eap_method, sizeof(snapshot->wifi_eap_method)) ||
        !bounded(snapshot->wifi_identity, sizeof(snapshot->wifi_identity)) ||
        !bounded(snapshot->wifi_username, sizeof(snapshot->wifi_username)) ||
        !bounded(snapshot->wifi_ttls_phase2, sizeof(snapshot->wifi_ttls_phase2)) ||
        !bounded(snapshot->wifi_ca_mode, sizeof(snapshot->wifi_ca_mode)) ||
        !bounded(snapshot->wifi_server_domain, sizeof(snapshot->wifi_server_domain))) {
        return false;
    }
    for (uint8_t index = 0; index < snapshot->wifi_network_count; ++index) {
        if (!bounded(snapshot->wifi_networks[index].ssid,
                     sizeof(snapshot->wifi_networks[index].ssid)) ||
            !bounded(snapshot->wifi_networks[index].password,
                     sizeof(snapshot->wifi_networks[index].password))) {
            return false;
        }
    }
    return true;
}

device_status_t wifi_runtime_configuration_service_init(void) {
    bool expected = false;
    if (atomic_compare_exchange_strong_explicit(
            &s_initialized, &expected, true, memory_order_acq_rel,
            memory_order_acquire)) {
        lock();
        memset(&s_snapshot, 0, sizeof(s_snapshot));
        atomic_store_explicit(&s_boot_snapshot_captured, false, memory_order_release);
        unlock();
    }
    return DEVICE_STATUS_OK;
}

bool wifi_runtime_configuration_service_capture_boot_snapshot(
    const configuration_snapshot_t *snapshot) {
    if (!initialized() || !snapshot_valid(snapshot)) return false;
    lock();
    if (captured()) {
        unlock();
        return false;
    }
    copy_value(s_snapshot.ssid, sizeof(s_snapshot.ssid), snapshot->wifi_ssid);
    copy_value(s_snapshot.password, sizeof(s_snapshot.password), snapshot->wifi_password);
    copy_value(s_snapshot.security, sizeof(s_snapshot.security), snapshot->wifi_security);
    copy_value(s_snapshot.eap_method, sizeof(s_snapshot.eap_method), snapshot->wifi_eap_method);
    copy_value(s_snapshot.identity, sizeof(s_snapshot.identity), snapshot->wifi_identity);
    copy_value(s_snapshot.username, sizeof(s_snapshot.username), snapshot->wifi_username);
    copy_value(s_snapshot.ttls_phase2, sizeof(s_snapshot.ttls_phase2), snapshot->wifi_ttls_phase2);
    copy_value(s_snapshot.ca_mode, sizeof(s_snapshot.ca_mode), snapshot->wifi_ca_mode);
    copy_value(s_snapshot.server_domain, sizeof(s_snapshot.server_domain), snapshot->wifi_server_domain);
    s_snapshot.saved_network_count = snapshot->wifi_network_count;
    memcpy(s_snapshot.saved_networks, snapshot->wifi_networks,
           (size_t)s_snapshot.saved_network_count * sizeof(s_snapshot.saved_networks[0]));
    atomic_store_explicit(&s_boot_snapshot_captured, true, memory_order_release);
    unlock();
    return true;
}

bool wifi_runtime_configuration_service_get_snapshot(
    wifi_runtime_configuration_snapshot_t *out_snapshot) {
    if (!out_snapshot || !initialized() || !captured()) return false;
    lock();
    *out_snapshot = s_snapshot;
    unlock();
    return true;
}

bool wifi_runtime_configuration_service_select_saved_network(const char *ssid,
                                                              const char *password) {
    if (!initialized() || !captured() || !bounded(ssid, sizeof(s_snapshot.ssid)) ||
        !bounded(password, sizeof(s_snapshot.password))) {
        return false;
    }
    lock();
    copy_value(s_snapshot.ssid, sizeof(s_snapshot.ssid), ssid);
    copy_value(s_snapshot.password, sizeof(s_snapshot.password), password);
    unlock();
    return true;
}

bool wifi_runtime_configuration_service_sync_saved_networks_after_delete(
    const configuration_wifi_network_t *networks, uint8_t network_count,
    const char *deleted_ssid) {
    if (!initialized() || !captured() ||
        network_count > CONFIGURATION_WIFI_NETWORK_CAPACITY ||
        (network_count != 0u && !networks)) {
        return false;
    }
    for (uint8_t index = 0; index < network_count; ++index) {
        if (!bounded(networks[index].ssid, sizeof(networks[index].ssid)) ||
            !bounded(networks[index].password, sizeof(networks[index].password))) {
            return false;
        }
    }
    lock();
    memset(s_snapshot.saved_networks, 0, sizeof(s_snapshot.saved_networks));
    memcpy(s_snapshot.saved_networks, networks,
           (size_t)network_count * sizeof(s_snapshot.saved_networks[0]));
    s_snapshot.saved_network_count = network_count;
    if (bounded(deleted_ssid, sizeof(s_snapshot.ssid)) && deleted_ssid[0] &&
        strcmp(s_snapshot.security, "enterprise") != 0 &&
        !strcmp(deleted_ssid, s_snapshot.ssid)) {
        s_snapshot.ssid[0] = '\0';
        s_snapshot.password[0] = '\0';
    }
    unlock();
    return true;
}
