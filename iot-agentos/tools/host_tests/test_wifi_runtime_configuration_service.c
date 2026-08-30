#include <assert.h>
#include <stdbool.h>
#include <stdio.h>
#include <string.h>

#include "configuration_service.h"
#include "services/wifi_runtime_configuration_service.h"

static configuration_snapshot_t snapshot_with_network(const char *security) {
    configuration_snapshot_t snapshot = {0};
    strcpy(snapshot.wifi_ssid, "primary");
    strcpy(snapshot.wifi_password, "primary-password");
    strcpy(snapshot.wifi_security, security);
    strcpy(snapshot.wifi_eap_method, "peap");
    strcpy(snapshot.wifi_ttls_phase2, "mschapv2");
    strcpy(snapshot.wifi_ca_mode, "system");
    strcpy(snapshot.wifi_networks[0].ssid, "primary");
    strcpy(snapshot.wifi_networks[0].password, "primary-password");
    strcpy(snapshot.wifi_networks[1].ssid, "fallback");
    strcpy(snapshot.wifi_networks[1].password, "fallback-password");
    snapshot.wifi_network_count = 2u;
    return snapshot;
}

int main(int argc, char **argv) {
    const bool enterprise = argc > 1 && !strcmp(argv[1], "enterprise");
    wifi_runtime_configuration_snapshot_t runtime = {0};
    configuration_snapshot_t boot = snapshot_with_network(enterprise ? "enterprise" : "personal");

    assert(!wifi_runtime_configuration_service_get_snapshot(&runtime));
    assert(!wifi_runtime_configuration_service_capture_boot_snapshot(&boot));
    assert(wifi_runtime_configuration_service_init() == DEVICE_STATUS_OK);
    assert(!wifi_runtime_configuration_service_get_snapshot(&runtime));
    assert(wifi_runtime_configuration_service_capture_boot_snapshot(&boot));
    assert(!wifi_runtime_configuration_service_capture_boot_snapshot(&boot));
    assert(wifi_runtime_configuration_service_get_snapshot(&runtime));
    assert(!strcmp(runtime.ssid, "primary"));
    assert(runtime.saved_network_count == 2u);

    assert(wifi_runtime_configuration_service_select_saved_network(
        "fallback", "fallback-password"));
    assert(wifi_runtime_configuration_service_get_snapshot(&runtime));
    assert(!strcmp(runtime.ssid, "fallback"));
    assert(!strcmp(runtime.password, "fallback-password"));
    assert(runtime.saved_network_count == 2u);

    configuration_wifi_network_t after_delete[CONFIGURATION_WIFI_NETWORK_CAPACITY] = {0};
    strcpy(after_delete[0].ssid, "primary");
    strcpy(after_delete[0].password, "primary-password");
    assert(wifi_runtime_configuration_service_sync_saved_networks_after_delete(
        after_delete, 1u, "fallback"));
    assert(wifi_runtime_configuration_service_get_snapshot(&runtime));
    assert(runtime.saved_network_count == 1u);
    if (enterprise) {
        assert(!strcmp(runtime.ssid, "fallback"));
        assert(!strcmp(runtime.password, "fallback-password"));
    } else {
        assert(runtime.ssid[0] == '\0');
        assert(runtime.password[0] == '\0');
    }

    assert(!wifi_runtime_configuration_service_select_saved_network(
        "this-ssid-is-deliberately-too-long-for-the-64-byte-runtime-buffer-xxxxxxxx", "x"));

    puts("PASS Wi-Fi runtime configuration state");
    return 0;
}
