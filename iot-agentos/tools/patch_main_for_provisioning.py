#!/usr/bin/env python3
"""Remove extracted portal user-space from main.c and leave host wrappers."""
from pathlib import Path

MAIN = Path(__file__).resolve().parents[1] / "main" / "main.c"
text = MAIN.read_text(encoding="utf-8")
lines = text.splitlines(keepends=True)

def delete_span(ls, start, end):
    return ls[: start - 1] + ls[end:]

# Delete bottom-up so earlier original line numbers stay valid.
for start, end in (
    (6049, 6440),  # recover + start_setup_portal*
    (5133, 5862),  # dns + handlers + stop transaction
    (4895, 5085),  # html/scan helpers
    (726, 738),    # admission helpers
    (421, 612),    # restart coordinator
):
    lines = delete_span(lines, start, end)

text = "".join(lines)

# Include the new service.
text = text.replace(
    '#include "services/ambient_service.h"\n',
    '#include "services/ambient_service.h"\n#include "services/provisioning_service.h"\n',
)

# Drop moved statics. Keep s_setup_ap_netif and portal radio flags.
replacements = [
    (
        "static httpd_handle_t s_setup_server;\n"
        "/* Portal HTTP is a separate resource from the AP, DHCP and captive DNS.  Its\n"
        " * admission flag is closed before httpd_stop(), so a teardown can never\n"
        " * accept a new credential-changing request while its sockets are draining. */\n"
        "static bool s_setup_http_admission_open;\n",
        "",
    ),
    (
        "static device_power_lease_t s_setup_power_lease;\n",
        "",
    ),
    (
        "static TaskHandle_t s_dns_task;\n"
        "static SemaphoreHandle_t s_dns_start_gate;\n"
        "static SemaphoreHandle_t s_dns_stopped;\n"
        "/* A created DNS task is not proof that UDP/53 was actually bound.  Portal\n"
        " * startup waits for this one-shot result before admitting the HTTP form. */\n"
        "static SemaphoreHandle_t s_dns_ready;\n"
        "static bool s_dns_ready_success;\n"
        "static bool s_dns_stop_requested;\n"
        "static bool s_dns_starting;\n"
        "static bool s_dns_admission_open;\n"
        "/* The provisioning portal is one composite resource: HTTP, captive DNS,\n"
        " * session scratch, a foreground power lease and a reversible radio snapshot.\n"
        " * Its callers originate in button/input, gateway recovery, boot and the\n"
        " * post-save reset coordinator, so a single transaction gate is required to\n"
        " * prevent one generation's stop from racing another generation's start. */\n"
        "static SemaphoreHandle_t s_setup_portal_mutex;\n"
        "static SemaphoreHandle_t s_setup_options_mutex;\n"
        "// Provisioning-only scratch storage is allocated when the portal starts. It\n"
        "// must not permanently shift ESP-IDF's prebuilt Wi-Fi globals in internal\n"
        "// DRAM during every configured station boot.\n"
        "static char *s_setup_ssid_options;\n"
        "static char *s_setup_ssid_choices;\n"
        "static wifi_ap_record_t *s_setup_scan_records;\n",
        "",
    ),
    (
        "static TaskHandle_t s_setup_restart_task;\n"
        "static SemaphoreHandle_t s_setup_restart_start_gate;\n"
        "static SemaphoreHandle_t s_setup_restart_stopped;\n"
        "static bool s_setup_restart_stop_requested;\n"
        "static bool s_setup_restart_starting;\n"
        "static bool s_setup_restart_admission_open;\n",
        "",
    ),
]
for old, new in replacements:
    if old not in text:
        print("WARN: block not found:\n", old[:80])
    else:
        text = text.replace(old, new, 1)

# Replace leftover declarations that pointed at moved functions.
text = text.replace(
    "static esp_err_t stop_setup_portal_transaction(uint32_t timeout_ms,\n"
    "                                               bool restore_wake_word);\n"
    "static esp_err_t stop_setup_portal_transaction_locked(uint32_t timeout_ms,\n"
    "                                                      bool restore_wake_word);\n",
    "",
)
text = text.replace(
    "static esp_err_t stop_captive_dns_task(uint32_t timeout_ms);\n"
    "static esp_err_t stop_captive_dns_registry_entry(void *context, uint32_t timeout_ms);\n"
    "static esp_err_t stop_setup_portal_http_server(void);\n"
    "static void release_setup_portal_scratch(void);\n",
    "",
)
text = text.replace(
    "static void start_setup_portal(bool keep_station);\n"
    "static void start_setup_portal_locked(bool keep_station);\n"
    "static void build_setup_saved_networks_html(void);\n"
    "/* 已存热点列表的页面片段在 httpd 任务上构建，沿用 PSRAM 缓冲。 */\n"
    "#define SETUP_SAVED_HTML_CAPACITY 2048\n"
    "static char *s_setup_saved_html;\n",
    "static void start_setup_portal(bool keep_station);\n",
)
text = text.replace(
    "static bool schedule_setup_restart(void);\n"
    "static esp_err_t stop_setup_restart_task(uint32_t timeout_ms);\n"
    "static esp_err_t stop_setup_restart_registry_entry(void *context, uint32_t timeout_ms);\n",
    "",
)

HOST = r'''
static setup_portal_radio_snapshot_t s_provisioning_radio_snapshot;

static device_status_t provisioning_host_init_network(void) {
    return startup_status_from_esp_err(init_network());
}

static device_status_t provisioning_host_ensure_ap_netif(void) {
    ensure_setup_ap_netif();
    return s_setup_ap_netif ? DEVICE_STATUS_OK : DEVICE_STATUS_RESOURCE_EXHAUSTED;
}

static bool provisioning_host_ap_netif_ready(void) {
    return s_setup_ap_netif != NULL;
}

static device_status_t provisioning_host_configure_ap_dhcp(void) {
    return startup_status_from_esp_err(configure_setup_ap_ip());
}

static device_status_t provisioning_host_ensure_sta_netif(void) {
    ensure_station_netif();
    return s_sta_netif ? DEVICE_STATUS_OK : DEVICE_STATUS_RESOURCE_EXHAUSTED;
}

static bool provisioning_host_sta_netif_ready(void) {
    return s_sta_netif != NULL;
}

static bool provisioning_host_wifi_started(void) { return s_wifi_started; }

static void provisioning_host_set_wifi_started(bool started) {
    s_wifi_started = started;
    if (started) device_connectivity_set_wifi_ready(true);
}

static void provisioning_host_set_station_policy(bool auto_connect, bool expected_disconnect) {
    s_station_auto_connect = auto_connect;
    s_station_expected_disconnect = expected_disconnect;
}

static device_status_t provisioning_host_wifi_disconnect(void) {
    esp_err_t err = esp_wifi_disconnect();
    if (err == ESP_ERR_WIFI_NOT_CONNECT) return DEVICE_STATUS_UNAVAILABLE;
    return startup_status_from_esp_err(err);
}

static device_status_t provisioning_host_wifi_set_mode(bool ap_only) {
    return startup_status_from_esp_err(
        esp_wifi_set_mode(ap_only ? WIFI_MODE_AP : WIFI_MODE_APSTA));
}

static device_status_t provisioning_host_wifi_configure_open_ap(const char *ssid) {
    wifi_config_t ap = { .ap = { .channel = 1, .max_connection = 4, .authmode = WIFI_AUTH_OPEN } };
    strlcpy((char *)ap.ap.ssid, ssid ? ssid : "", sizeof(ap.ap.ssid));
    ap.ap.ssid_len = strlen((const char *)ap.ap.ssid);
    return startup_status_from_esp_err(esp_wifi_set_config(WIFI_IF_AP, &ap));
}

static device_status_t provisioning_host_wifi_disable_ps(void) {
    return startup_status_from_esp_err(esp_wifi_set_ps(WIFI_PS_NONE));
}

static device_status_t provisioning_host_wifi_start(void) {
    return startup_status_from_esp_err(esp_wifi_start());
}

static device_status_t provisioning_host_wifi_connect(void) {
    esp_err_t err = esp_wifi_connect();
    if (err == ESP_ERR_WIFI_CONN) return DEVICE_STATUS_BUSY;
    return startup_status_from_esp_err(err);
}

static device_status_t provisioning_host_wifi_confirm_ap_mode(void) {
    wifi_mode_t active_mode = WIFI_MODE_NULL;
    esp_err_t err = esp_wifi_get_mode(&active_mode);
    if (err != ESP_OK || (active_mode != WIFI_MODE_AP && active_mode != WIFI_MODE_APSTA)) {
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    return DEVICE_STATUS_OK;
}

static device_status_t provisioning_host_read_softap_mac(uint8_t mac[6]) {
    if (!mac) return DEVICE_STATUS_INVALID_ARGUMENT;
    return startup_status_from_esp_err(esp_read_mac(mac, ESP_MAC_WIFI_SOFTAP));
}

static device_status_t provisioning_host_capture_radio(provisioning_radio_token_t *token) {
    if (!token) return DEVICE_STATUS_INVALID_ARGUMENT;
    memset(token, 0, sizeof(*token));
    esp_err_t err = capture_setup_portal_radio_snapshot(&s_provisioning_radio_snapshot);
    if (err != ESP_OK) return startup_status_from_esp_err(err);
    s_provisioning_radio_snapshot.radio_changed = true;
    token->valid = true;
    return DEVICE_STATUS_OK;
}

static device_status_t provisioning_host_restore_radio(const provisioning_radio_token_t *token) {
    if (!token || !token->valid) return DEVICE_STATUS_OK;
    return startup_status_from_esp_err(
        restore_setup_portal_radio_snapshot(&s_provisioning_radio_snapshot));
}

static device_status_t provisioning_host_wake_word_stop(void) {
    esp_err_t err = audio_wake_word_stop();
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return startup_status_from_esp_err(err);
}

static device_status_t provisioning_host_wake_word_start(void) {
    esp_err_t err = audio_wake_word_start(on_wake_word, NULL);
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return startup_status_from_esp_err(err);
}

static void provisioning_host_show_text(const char *title, const char *body) {
    app_ui_show_text(title, body);
}

static void provisioning_host_show_qr(const char *ap_ssid) {
    show_setup_qrcode(ap_ssid);
}

static void provisioning_host_copy_runtime_wifi(provisioning_runtime_wifi_t *out) {
    if (!out) return;
    memset(out, 0, sizeof(*out));
    strlcpy(out->wifi_ssid, s_wifi_ssid, sizeof(out->wifi_ssid));
    strlcpy(out->wifi_password, s_wifi_password, sizeof(out->wifi_password));
    strlcpy(out->wifi_security, s_wifi_security, sizeof(out->wifi_security));
    strlcpy(out->wifi_eap_method, s_wifi_eap_method, sizeof(out->wifi_eap_method));
    strlcpy(out->wifi_identity, s_wifi_identity, sizeof(out->wifi_identity));
    strlcpy(out->wifi_username, s_wifi_username, sizeof(out->wifi_username));
    strlcpy(out->wifi_ttls_phase2, s_wifi_ttls_phase2, sizeof(out->wifi_ttls_phase2));
    strlcpy(out->wifi_ca_mode, s_wifi_ca_mode, sizeof(out->wifi_ca_mode));
    strlcpy(out->wifi_server_domain, s_wifi_server_domain, sizeof(out->wifi_server_domain));
    (void)gateway_transport_gateway_url(out->gateway_url, sizeof(out->gateway_url));
}

static device_status_t provisioning_host_save_full_config(const provisioning_save_request_t *req) {
    if (!req) return DEVICE_STATUS_INVALID_ARGUMENT;
    return startup_status_from_esp_err(
        save_device_config(req->ssid, req->password, req->gateway, req->code,
                           req->security, req->eap_method, req->identity,
                           req->username, req->ttls_phase2, req->ca_mode,
                           req->server_domain));
}

static device_status_t provisioning_host_save_pairing_code(const char *code) {
    return startup_status_from_esp_err(save_pairing_code_only(code));
}

static void provisioning_host_sync_runtime_after_network_delete(const char *ssid) {
    (void)configuration_service_list_wifi_networks(
        s_wifi_networks, CONFIGURATION_WIFI_NETWORK_CAPACITY, &s_wifi_network_count);
    if (ssid && !strcmp(ssid, s_wifi_ssid) && !is_enterprise_wifi()) {
        s_wifi_ssid[0] = '\0';
        s_wifi_password[0] = '\0';
    }
}

static const char *provisioning_host_preferred_scan_ssid(void) {
    return s_wifi_ssid;
}

static const provisioning_service_host_t s_provisioning_service_host = {
    .init_network = provisioning_host_init_network,
    .ensure_ap_netif = provisioning_host_ensure_ap_netif,
    .ap_netif_ready = provisioning_host_ap_netif_ready,
    .configure_ap_dhcp = provisioning_host_configure_ap_dhcp,
    .ensure_sta_netif = provisioning_host_ensure_sta_netif,
    .sta_netif_ready = provisioning_host_sta_netif_ready,
    .wifi_started = provisioning_host_wifi_started,
    .set_wifi_started = provisioning_host_set_wifi_started,
    .set_station_policy = provisioning_host_set_station_policy,
    .wifi_disconnect = provisioning_host_wifi_disconnect,
    .wifi_set_mode = provisioning_host_wifi_set_mode,
    .wifi_configure_open_ap = provisioning_host_wifi_configure_open_ap,
    .wifi_disable_ps = provisioning_host_wifi_disable_ps,
    .wifi_start = provisioning_host_wifi_start,
    .wifi_connect = provisioning_host_wifi_connect,
    .wifi_confirm_ap_mode = provisioning_host_wifi_confirm_ap_mode,
    .read_softap_mac = provisioning_host_read_softap_mac,
    .capture_radio = provisioning_host_capture_radio,
    .restore_radio = provisioning_host_restore_radio,
    .refresh_ssid_scan = NULL,
    .wake_word_stop = provisioning_host_wake_word_stop,
    .wake_word_start = provisioning_host_wake_word_start,
    .show_text = provisioning_host_show_text,
    .show_qr = provisioning_host_show_qr,
    .copy_runtime_wifi = provisioning_host_copy_runtime_wifi,
    .save_full_config = provisioning_host_save_full_config,
    .save_pairing_code = provisioning_host_save_pairing_code,
    .sync_runtime_after_network_delete = provisioning_host_sync_runtime_after_network_delete,
    .preferred_scan_ssid = provisioning_host_preferred_scan_ssid,
};

static void start_setup_portal(bool keep_station) {
    provisioning_service_start_portal(keep_station);
}

'''

# Insert host after radio snapshot helpers so their types exist.
needle = "static esp_err_t restore_setup_portal_radio_snapshot("
idx = text.find(needle)
if idx < 0:
    raise SystemExit("restore_setup_portal_radio_snapshot not found")
end = text.find("\nstatic ", idx + 10)
if end < 0:
    raise SystemExit("cannot find end of restore_setup_portal_radio_snapshot")
text = text[:end] + "\n" + HOST + text[end:]

# Call sites
text = text.replace(
    "    start_setup_portal(true);\n",
    "    provisioning_service_start_portal(true);\n",
)
text = text.replace(
    "        start_setup_portal(false);\n",
    "        provisioning_service_start_portal(false);\n",
)
text = text.replace(
    "    start_setup_portal(false);\n",
    "    provisioning_service_start_portal(false);\n",
)
text = text.replace(
    "static void transport_host_start_setup_portal(void) {\n    start_setup_portal(true);\n}",
    "static void transport_host_start_setup_portal(void) {\n    provisioning_service_start_portal(true);\n}",
)
text = text.replace(
    "    esp_err_t setup_restart_stop_err = stop_setup_restart_task(timeout_ms);",
    "    device_status_t setup_restart_stop_err = provisioning_service_stop_restart(timeout_ms);",
)
text = text.replace(
    "                 esp_err_to_name(setup_restart_stop_err));",
    "                 (int)setup_restart_stop_err);",
)
text = text.replace(
    "    if (setup_restart_stop_err != ESP_OK) {",
    "    if (setup_restart_stop_err != DEVICE_STATUS_OK) {",
)
text = text.replace(
    "    if (device_connectivity_is_provisioning_active() || s_setup_server || s_dns_task) {\n"
    "        STARTUP_ROLLBACK_NEXT_TIMEOUT(\"provisioning transaction\");\n"
    "        esp_err_t portal_stop_err = stop_setup_portal_transaction(timeout_ms, false);\n"
    "        if (portal_stop_err != ESP_OK) {\n"
    "            ESP_LOGW(TAG, \"provisioning transaction did not stop during startup rollback: %s\",\n"
    "                     esp_err_to_name(portal_stop_err));",
    "    if (provisioning_service_has_live_resources()) {\n"
    "        STARTUP_ROLLBACK_NEXT_TIMEOUT(\"provisioning transaction\");\n"
    "        device_status_t portal_stop_err = provisioning_service_stop_portal(timeout_ms, false);\n"
    "        if (portal_stop_err != DEVICE_STATUS_OK) {\n"
    "            ESP_LOGW(TAG, \"provisioning transaction did not stop during startup rollback: status=%d\",\n"
    "                     (int)portal_stop_err);",
)
text = text.replace(
    "    esp_wifi_set_mode(s_setup_server ? WIFI_MODE_APSTA : WIFI_MODE_STA);",
    "    esp_wifi_set_mode(provisioning_service_has_live_resources() ? WIFI_MODE_APSTA : WIFI_MODE_STA);",
)

# Init: replace semaphore construction with service init.
old_init = """    s_setup_restart_start_gate = xSemaphoreCreateBinary();
    if (!s_setup_restart_start_gate) goto startup_core_no_memory;
    s_setup_restart_stopped = xSemaphoreCreateBinary();
    if (!s_setup_restart_stopped) goto startup_core_no_memory;
    s_deferred_setup_start_gate = xSemaphoreCreateBinary();
"""
new_init = """    if (provisioning_service_init(&s_provisioning_service_host) != DEVICE_STATUS_OK) goto startup_core_no_memory;
    s_deferred_setup_start_gate = xSemaphoreCreateBinary();
"""
if old_init not in text:
    print("WARN: init restart-gate block not found")
else:
    text = text.replace(old_init, new_init, 1)

# Remove leftover dns semaphore creates and portal mutex if still present.
for block in (
    """    s_dns_start_gate = xSemaphoreCreateBinary();
    if (!s_dns_start_gate) goto startup_core_no_memory;
    s_dns_stopped = xSemaphoreCreateBinary();
    if (!s_dns_stopped) goto startup_core_no_memory;
    s_dns_ready = xSemaphoreCreateBinary();
    if (!s_dns_ready) goto startup_core_no_memory;
    s_setup_portal_mutex = xSemaphoreCreateMutex();
    if (!s_setup_portal_mutex) goto startup_core_no_memory;
""",
    """    s_setup_restart_admission_open = true;
    s_dns_admission_open = true;
""",
):
    if block in text:
        text = text.replace(block, "")
    else:
        print("WARN: init block not found:\n", block[:60])

MAIN.write_text(text, encoding="utf-8")
print("patched main.c", len(text.splitlines()), "lines")
