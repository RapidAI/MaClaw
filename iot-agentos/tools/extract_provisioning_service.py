#!/usr/bin/env python3
"""One-shot mechanical extract of portal user-space from main.c into
services/provisioning_service.c.  Radio/config seams stay as host calls."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MAIN = (ROOT / "main" / "main.c").read_text(encoding="utf-8")
LINES = MAIN.splitlines(keepends=True)

def span(start, end):
    return "".join(LINES[start - 1 : end])

PREAMBLE = r'''#include "services/provisioning_service.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "esp_err.h"
#include "esp_heap_caps.h"
#include "esp_http_server.h"
#include "esp_log.h"
#include "esp_mac.h"
#include "esp_timer.h"
#include "esp_system.h"
#include "esp_wifi.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "lwip/sockets.h"
#include "mbedtls/platform_util.h"

#include "configuration_service.h"
#include "device_api.h"
#include "provisioning_failure_injection.h"
#include "services/foreground_coordinator.h"
#include "task_registry.h"

static const char *TAG = "maclaw_client";

#define SETUP_AP_IP_ADDR "192.168.4.1"
#define DNS_PORT 53
#define DNS_PACKET_CAPACITY 512
#define SETUP_SCAN_MAX_APS 24
#define WIFI_VALUE_CAPACITY PROVISIONING_WIFI_VALUE_CAPACITY
#define WIFI_SSID_MAX_LEN 32
#define WIFI_ENTERPRISE_VALUE_CAPACITY PROVISIONING_WIFI_ENTERPRISE_CAPACITY
#define WIFI_EAP_MODE_CAPACITY PROVISIONING_WIFI_MODE_CAPACITY
#define URL_CAPACITY PROVISIONING_GATEWAY_URL_CAPACITY
#define PAIR_CODE_CAPACITY PROVISIONING_PAIR_CODE_CAPACITY
#define SETUP_SSID_OPTIONS_CAPACITY 6144
#define SETUP_SSID_CHOICES_CAPACITY (SETUP_SCAN_MAX_APS * WIFI_VALUE_CAPACITY)
#define SETUP_SAVED_HTML_CAPACITY 2048
#define SETUP_SAVE_BODY_CAPACITY 1536

static provisioning_service_host_t s_host;
static portMUX_TYPE s_task_state_lock = portMUX_INITIALIZER_UNLOCKED;
static SemaphoreHandle_t s_setup_portal_mutex;
static SemaphoreHandle_t s_setup_options_mutex;
static httpd_handle_t s_setup_server;
static bool s_setup_http_admission_open;
static device_power_lease_t s_setup_power_lease = DEVICE_POWER_LEASE_INVALID;
static char *s_setup_ssid_options;
static char *s_setup_ssid_choices;
static char *s_setup_saved_html;
static char *s_setup_save_body;
static TaskHandle_t s_dns_task;
static SemaphoreHandle_t s_dns_start_gate;
static SemaphoreHandle_t s_dns_stopped;
static SemaphoreHandle_t s_dns_ready;
static bool s_dns_ready_success;
static bool s_dns_stop_requested;
static bool s_dns_starting;
static bool s_dns_admission_open;
static TaskHandle_t s_setup_restart_task;
static SemaphoreHandle_t s_setup_restart_start_gate;
static SemaphoreHandle_t s_setup_restart_stopped;
static bool s_setup_restart_stop_requested;
static bool s_setup_restart_starting;
static bool s_setup_restart_admission_open;

static uint32_t remaining_timeout_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const uint64_t rounded_ms = ((uint64_t)remaining_us + 999u) / 1000u;
    return rounded_ms > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded_ms;
}

#define startup_rollback_remaining_timeout_ms remaining_timeout_ms

static device_status_t status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

static bool is_valid_setup_selected_ssid(const char *ssid) {
    if (!ssid || !ssid[0] || strlen(ssid) > WIFI_SSID_MAX_LEN) return false;
    for (const unsigned char *p = (const unsigned char *)ssid; *p; ++p) {
        if (*p < 0x20 || *p == 0x7f) return false;
    }
    return true;
}

'''

# After extraction we append handwritten start/init/public API.

POSTAMBLE = r'''
static void recover_after_setup_portal_start_failure(
    bool wake_was_stopped, const provisioning_radio_token_t *radio_token) {
    esp_err_t stop_err = stop_setup_portal_transaction_locked(500, wake_was_stopped);
    if (stop_err != ESP_OK) return;
    if (s_host.restore_radio) {
        device_status_t radio_err = s_host.restore_radio(radio_token);
        if (radio_err != DEVICE_STATUS_OK) {
            ESP_LOGW(TAG, "failed setup radio remains fail-closed: status=%d",
                     (int)radio_err);
        }
    }
}

static void start_setup_portal_locked(bool keep_station) {
    if (setup_restart_is_pending()) {
        ESP_LOGW(TAG, "setup portal start rejected: post-save reset is pending");
        if (s_host.show_text) s_host.show_text("配置已保存", "设备正在重启，请稍候");
        return;
    }
    if (device_connectivity_is_provisioning_active() && s_setup_server &&
        setup_portal_http_admission_open()) {
        ESP_LOGI(TAG, "setup portal already active");
        return;
    }
    if (s_setup_server) {
        set_setup_portal_http_admission(false);
        ESP_LOGE(TAG, "setup portal HTTP server is still active; refusing duplicate start");
        if (s_host.show_text) s_host.show_text("设置失败", "网页服务正在恢复，请重启设备");
        return;
    }
    if (!s_setup_ssid_options) {
        s_setup_ssid_options = heap_caps_calloc(
            1, SETUP_SSID_OPTIONS_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_setup_ssid_choices) {
        s_setup_ssid_choices = heap_caps_calloc(
            1, SETUP_SSID_CHOICES_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_setup_saved_html) {
        s_setup_saved_html = heap_caps_calloc(
            1, SETUP_SAVED_HTML_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_setup_ssid_options || !s_setup_ssid_choices || !s_setup_saved_html) {
        release_setup_portal_scratch();
        ESP_LOGE(TAG, "cannot allocate setup portal Wi-Fi list buffers");
        if (s_host.show_text) s_host.show_text("设置失败", "内存不足，请重启后再试");
        return;
    }
    if (s_dns_task) {
        ESP_LOGW(TAG, "waiting for previous captive DNS task before starting portal");
        esp_err_t dns_stop_err = stop_captive_dns_task(1200);
        if (dns_stop_err != ESP_OK || s_dns_task) {
            ESP_LOGE(TAG, "previous captive DNS task did not exit: %s",
                     esp_err_to_name(dns_stop_err));
            if (s_host.show_text) s_host.show_text("配置失败", "请重启设备后再试");
            return;
        }
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_dns_admission_open = true;
    s_dns_stop_requested = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_setup_power_lease == DEVICE_POWER_LEASE_INVALID) {
        device_status_t lease_status = device_power_lease_acquire(
            DEVICE_POWER_LEASE_OWNER_PROVISIONING, &s_setup_power_lease);
        if (lease_status != DEVICE_STATUS_OK) {
            ESP_LOGE(TAG, "cannot acquire power lease for setup portal: status=%d",
                     (int)lease_status);
            if (s_host.show_text) s_host.show_text("设置失败", "电源服务不可用，请重启后再试");
            return;
        }
        (void)device_power_wake_display_from_schedule();
    }
    device_connectivity_begin_provisioning(keep_station);
    foreground_coordinator_observe_acquire(FOREGROUND_OWNER_SETUP,
                                           FOREGROUND_PRIORITY_RECOVERY,
                                           FOREGROUND_SCENE_SETUP_PORTAL);
    device_wake_word_pause(true);
    device_status_t wake_stop_err = s_host.wake_word_stop
                                        ? s_host.wake_word_stop()
                                        : DEVICE_STATUS_OK;
    bool wake_was_stopped = wake_stop_err == DEVICE_STATUS_OK;
    if (wake_stop_err != DEVICE_STATUS_OK &&
        wake_stop_err != DEVICE_STATUS_BUSY) {
        ESP_LOGW(TAG, "cannot stop offline wake for setup portal: status=%d",
                 (int)wake_stop_err);
    }
    uint8_t mac[6];
    if (!s_host.read_softap_mac ||
        s_host.read_softap_mac(mac) != DEVICE_STATUS_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, NULL);
        ESP_LOGE(TAG, "cannot read SoftAP MAC for setup portal");
        if (s_host.show_text) {
            s_host.show_text("Setup failed", "Network identity unavailable; restart device");
        }
        return;
    }
    char ap_ssid[PROVISIONING_AP_SSID_CAPACITY];
    snprintf(ap_ssid, sizeof(ap_ssid), "MACLAW-SETUP-%02X%02X", mac[4], mac[5]);
    if (!s_host.init_network || s_host.init_network() != DEVICE_STATUS_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, NULL);
        ESP_LOGE(TAG, "cannot initialize network core for setup portal");
        if (s_host.show_text) {
            s_host.show_text("Setup failed", "Network service unavailable; restart device");
        }
        return;
    }
    provisioning_radio_token_t radio_token = {0};
    if (!s_host.capture_radio ||
        s_host.capture_radio(&radio_token) != DEVICE_STATUS_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, NULL);
        ESP_LOGE(TAG, "cannot inspect Wi-Fi state before setup portal");
        if (s_host.show_text) {
            s_host.show_text("Setup failed", "Network state unavailable; restart device");
        }
        return;
    }
    if (s_host.ensure_ap_netif) s_host.ensure_ap_netif();
    if (!s_host.ap_netif_ready || !s_host.ap_netif_ready()) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_token);
        ESP_LOGE(TAG, "cannot create setup AP netif");
        if (s_host.show_text) {
            s_host.show_text("Setup failed", "Network interface unavailable; restart device");
        }
        return;
    }
    if (!s_host.configure_ap_dhcp ||
        s_host.configure_ap_dhcp() != DEVICE_STATUS_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_token);
        ESP_LOGE(TAG, "cannot configure setup AP/DHCP transaction");
        if (s_host.show_text) {
            s_host.show_text("Setup failed", "Hotspot network unavailable; restart device");
        }
        return;
    }
    if (!start_captive_dns()) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_token);
        ESP_LOGE(TAG, "cannot start captive DNS before setup hotspot");
        if (s_host.show_text) s_host.show_text("设置失败", "配网 DNS 服务启动失败，请重启后再试");
        return;
    }
    bool cellular_pairing_ap_only = device_connectivity_is_active_cellular() &&
                                    device_connectivity_is_pairing_recovery_provisioning();
    if (!cellular_pairing_ap_only) {
        if (s_host.ensure_sta_netif) s_host.ensure_sta_netif();
        if (!s_host.sta_netif_ready || !s_host.sta_netif_ready()) {
            recover_after_setup_portal_start_failure(wake_was_stopped, &radio_token);
            ESP_LOGE(TAG, "cannot create setup station netif");
            if (s_host.show_text) {
                s_host.show_text("Setup failed", "Network interface unavailable; restart device");
            }
            return;
        }
    }
    bool keep_wifi_station = device_connectivity_is_pairing_recovery_provisioning() &&
                             !device_connectivity_is_active_cellular();
    if (s_host.set_station_policy) s_host.set_station_policy(keep_wifi_station, false);
    if (!keep_wifi_station && s_host.wifi_started && s_host.wifi_started()) {
        if (s_host.set_station_policy) s_host.set_station_policy(keep_wifi_station, true);
        device_status_t disconnect_err = s_host.wifi_disconnect
                                             ? s_host.wifi_disconnect()
                                             : DEVICE_STATUS_OK;
        if (disconnect_err != DEVICE_STATUS_OK &&
            disconnect_err != DEVICE_STATUS_UNAVAILABLE) {
            if (s_host.set_station_policy) {
                s_host.set_station_policy(keep_wifi_station, false);
            }
            ESP_LOGW(TAG, "cannot stop station while entering setup portal: status=%d",
                     (int)disconnect_err);
        }
    }
    if (!s_host.wifi_set_mode ||
        s_host.wifi_set_mode(cellular_pairing_ap_only) != DEVICE_STATUS_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_token);
        ESP_LOGE(TAG, "cannot enter setup Wi-Fi mode");
        if (s_host.show_text) s_host.show_text("设置失败", "请在网页重新设置");
        return;
    }
    if (!s_host.wifi_configure_open_ap ||
        s_host.wifi_configure_open_ap(ap_ssid) != DEVICE_STATUS_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_token);
        ESP_LOGE(TAG, "cannot configure setup hotspot");
        if (s_host.show_text) s_host.show_text("设置失败", "请在网页重新设置");
        return;
    }
    if (s_host.wifi_disable_ps) {
        device_status_t ps_err = s_host.wifi_disable_ps();
        if (ps_err != DEVICE_STATUS_OK) {
            ESP_LOGW(TAG, "cannot disable Wi-Fi power save for setup portal: status=%d",
                     (int)ps_err);
        }
    }
    if (s_host.wifi_started && !s_host.wifi_started()) {
        if (!s_host.wifi_start || s_host.wifi_start() != DEVICE_STATUS_OK) {
            recover_after_setup_portal_start_failure(wake_was_stopped, &radio_token);
            ESP_LOGE(TAG, "cannot start setup hotspot");
            if (s_host.show_text) s_host.show_text("设置失败", "请在网页重新设置");
            return;
        }
        if (s_host.set_wifi_started) s_host.set_wifi_started(true);
    }
    if (keep_wifi_station && s_host.wifi_connect) {
        device_status_t connect_err = s_host.wifi_connect();
        if (connect_err != DEVICE_STATUS_OK &&
            connect_err != DEVICE_STATUS_BUSY) {
            ESP_LOGW(TAG, "station reconnect while enabling portal: status=%d",
                     (int)connect_err);
        }
    }
    if (!s_host.wifi_confirm_ap_mode ||
        s_host.wifi_confirm_ap_mode() != DEVICE_STATUS_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_token);
        ESP_LOGE(TAG, "setup hotspot did not enter AP mode");
        if (s_host.show_text) s_host.show_text("设置热点失败", "请重启后再试");
        return;
    }
    httpd_config_t server_config = HTTPD_DEFAULT_CONFIG();
    server_config.stack_size = 6144;
    server_config.max_uri_handlers = 24;
    server_config.max_open_sockets = 5;
    server_config.lru_purge_enable = true;
    server_config.uri_match_fn = httpd_uri_match_wildcard;
    esp_err_t portal_err = httpd_start(&s_setup_server, &server_config);
    if (portal_err != ESP_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_token);
        ESP_LOGE(TAG, "cannot start setup web server: %s, free_heap=%u",
                 esp_err_to_name(portal_err), (unsigned)esp_get_free_heap_size());
        if (s_host.show_text) s_host.show_text("设置失败", "网页服务内存不足，请重启");
        return;
    }
    set_setup_portal_http_admission(false);
    httpd_uri_t apple_success = {.uri = "/hotspot-detect.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t apple_library_success = {.uri = "/library/test/success.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_generate_204 = {.uri = "/generate_204*", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_gen_204 = {.uri = "/gen_204", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_redirect = {.uri = "/redirect", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_mobile_status = {.uri = "/mobile/status.php", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_canonical = {.uri = "/canonical.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t windows_connect = {.uri = "/connecttest.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t windows_ncsi = {.uri = "/ncsi.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t windows_network_status = {.uri = "/check_network_status.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t windows_fwlink = {.uri = "/fwlink/", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t firefox_connectivity = {.uri = "/connectivity-check.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t generic_success = {.uri = "/success.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t generic_portal = {.uri = "/portal.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t root = {.uri = "/", .method = HTTP_GET, .handler = setup_get_handler};
    httpd_uri_t refresh = {.uri = "/refresh", .method = HTTP_GET, .handler = setup_refresh_handler};
    httpd_uri_t captive = {.uri = "/*", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t save = {.uri = "/save", .method = HTTP_POST, .handler = setup_save_handler};
    httpd_uri_t delete_saved = {.uri = "/delete", .method = HTTP_POST, .handler = setup_delete_handler};
    portal_err = httpd_register_uri_handler(s_setup_server, &apple_success);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &apple_library_success);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_generate_204);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_gen_204);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_redirect);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_mobile_status);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_canonical);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &windows_connect);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &windows_ncsi);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &windows_network_status);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &windows_fwlink);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &firefox_connectivity);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &generic_success);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &generic_portal);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &root);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &refresh);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &captive);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &save);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &delete_saved);
    if (portal_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register setup portal routes: %s, free_heap=%u",
                 esp_err_to_name(portal_err), (unsigned)esp_get_free_heap_size());
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_token);
        if (s_host.show_text) s_host.show_text("设置失败", "配置网页路由启动失败");
        return;
    }
    set_setup_portal_http_admission(true);
    if (!cellular_pairing_ap_only) refresh_setup_ssid_options();
    if (device_connectivity_is_pairing_recovery_provisioning()) {
        if (s_host.show_text) s_host.show_text("设备配对设置", ap_ssid);
    } else if (s_host.show_qr) {
        s_host.show_qr(ap_ssid);
    }
    ESP_LOGI(TAG, "%s portal ready: join %s and open http://192.168.4.1",
             device_connectivity_is_pairing_recovery_provisioning() ? "pairing recovery" : "setup",
             ap_ssid);
}

static void start_setup_portal(bool keep_station) {
    if (!s_setup_portal_mutex ||
        xSemaphoreTake(s_setup_portal_mutex, pdMS_TO_TICKS(1500)) != pdTRUE) {
        ESP_LOGW(TAG, "setup portal transition already in progress");
        return;
    }
    start_setup_portal_locked(keep_station);
    xSemaphoreGive(s_setup_portal_mutex);
}

device_status_t provisioning_service_init(const provisioning_service_host_t *host) {
    if (!host) return DEVICE_STATUS_INVALID_ARGUMENT;
    s_host = *host;
    if (s_setup_portal_mutex) return DEVICE_STATUS_OK;
    s_setup_portal_mutex = xSemaphoreCreateMutex();
    s_setup_options_mutex = xSemaphoreCreateMutex();
    s_setup_restart_start_gate = xSemaphoreCreateBinary();
    s_setup_restart_stopped = xSemaphoreCreateBinary();
    s_dns_start_gate = xSemaphoreCreateBinary();
    s_dns_stopped = xSemaphoreCreateBinary();
    s_dns_ready = xSemaphoreCreateBinary();
    if (!s_setup_portal_mutex || !s_setup_options_mutex ||
        !s_setup_restart_start_gate || !s_setup_restart_stopped ||
        !s_dns_start_gate || !s_dns_stopped || !s_dns_ready) {
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_restart_admission_open = true;
    s_dns_admission_open = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return DEVICE_STATUS_OK;
}

void provisioning_service_start_portal(bool keep_station) {
    start_setup_portal(keep_station);
}

device_status_t provisioning_service_stop_portal(uint32_t timeout_ms,
                                                 bool restore_wake_word) {
    return status_from_esp_err(
        stop_setup_portal_transaction(timeout_ms, restore_wake_word));
}

device_status_t provisioning_service_stop_restart(uint32_t timeout_ms) {
    return status_from_esp_err(stop_setup_restart_task(timeout_ms));
}

bool provisioning_service_has_live_resources(void) {
    return device_connectivity_is_provisioning_active() ||
           s_setup_server != NULL || s_dns_task != NULL;
}
'''

def rewrite_extracted(text: str) -> str:
    replacements = [
        ("save_pairing_code_only(code)",
         "device_status_to_platform_error(s_host.save_pairing_code(code))"),
        ("save_device_config(ssid, password, gateway, code, security, eap_method,\n"
         "                                                            identity, username, ttls_phase2, ca_mode, server_domain)",
         "host_save_full_config(ssid, password, gateway, code, security, eap_method, "
         "identity, username, ttls_phase2, ca_mode, server_domain)"),
        ("s_wifi_ssid", "host_preferred_ssid()"),
        ("s_wifi_password", "/*moved*/s_wifi_password"),  # handled below
    ]
    # More careful replacements done after.
    return text

# Extract function bodies. Line numbers are 1-based inclusive.
chunks = [
    span(421, 612),   # restart coordinator
    span(4895, 5085), # html + scan helpers
    span(5133, 5862), # dns + handlers + stop transaction
]

body = "".join(chunks)

# Host glue for save / runtime wifi / preferred ssid / scan preferred.
glue = r'''
static const char *host_preferred_ssid(void) {
    return s_host.preferred_scan_ssid ? s_host.preferred_scan_ssid() : "";
}

static esp_err_t host_save_full_config(const char *ssid, const char *password,
                                       const char *gateway, const char *code,
                                       const char *security, const char *eap_method,
                                       const char *identity, const char *username,
                                       const char *ttls_phase2, const char *ca_mode,
                                       const char *server_domain) {
    if (!s_host.save_full_config) return ESP_ERR_INVALID_STATE;
    provisioning_save_request_t req = {0};
    strlcpy(req.ssid, ssid ? ssid : "", sizeof(req.ssid));
    strlcpy(req.password, password ? password : "", sizeof(req.password));
    strlcpy(req.gateway, gateway ? gateway : "", sizeof(req.gateway));
    strlcpy(req.code, code ? code : "", sizeof(req.code));
    strlcpy(req.security, security ? security : "", sizeof(req.security));
    strlcpy(req.eap_method, eap_method ? eap_method : "", sizeof(req.eap_method));
    strlcpy(req.identity, identity ? identity : "", sizeof(req.identity));
    strlcpy(req.username, username ? username : "", sizeof(req.username));
    strlcpy(req.ttls_phase2, ttls_phase2 ? ttls_phase2 : "", sizeof(req.ttls_phase2));
    strlcpy(req.ca_mode, ca_mode ? ca_mode : "", sizeof(req.ca_mode));
    strlcpy(req.server_domain, server_domain ? server_domain : "", sizeof(req.server_domain));
    return device_status_to_platform_error(s_host.save_full_config(&req));
}

static void host_copy_runtime_wifi(provisioning_runtime_wifi_t *out) {
    if (!out) return;
    memset(out, 0, sizeof(*out));
    if (s_host.copy_runtime_wifi) s_host.copy_runtime_wifi(out);
}

'''

# Patch extracted body for symbols that stay in main.
body = body.replace(
    "startup_rollback_remaining_timeout_ms",
    "remaining_timeout_ms",
)
# save_device_config call (keep formatting flexible)
body = body.replace(
    "reuse_network ? save_pairing_code_only(code)\n"
    "                                       : save_device_config(ssid, password, gateway, code, security, eap_method,\n"
    "                                                            identity, username, ttls_phase2, ca_mode, server_domain)",
    "reuse_network ? device_status_to_platform_error(s_host.save_pairing_code(code))\n"
    "                                       : host_save_full_config(ssid, password, gateway, code, security, eap_method,\n"
    "                                                            identity, username, ttls_phase2, ca_mode, server_domain)",
)
body = body.replace(
    "strlcpy(ssid, s_wifi_ssid, sizeof(ssid));\n"
    "        strlcpy(password, s_wifi_password, sizeof(password));\n"
    "        (void)gateway_transport_gateway_url(gateway, sizeof(gateway));\n"
    "        strlcpy(security, s_wifi_security, sizeof(security));\n"
    "        strlcpy(eap_method, s_wifi_eap_method, sizeof(eap_method));\n"
    "        strlcpy(identity, s_wifi_identity, sizeof(identity));\n"
    "        strlcpy(username, s_wifi_username, sizeof(username));\n"
    "        strlcpy(ttls_phase2, s_wifi_ttls_phase2, sizeof(ttls_phase2));\n"
    "        strlcpy(ca_mode, s_wifi_ca_mode, sizeof(ca_mode));\n"
    "        strlcpy(server_domain, s_wifi_server_domain, sizeof(server_domain));",
    "provisioning_runtime_wifi_t runtime = {0};\n"
    "        host_copy_runtime_wifi(&runtime);\n"
    "        strlcpy(ssid, runtime.wifi_ssid, sizeof(ssid));\n"
    "        strlcpy(password, runtime.wifi_password, sizeof(password));\n"
    "        strlcpy(gateway, runtime.gateway_url, sizeof(gateway));\n"
    "        strlcpy(security, runtime.wifi_security, sizeof(security));\n"
    "        strlcpy(eap_method, runtime.wifi_eap_method, sizeof(eap_method));\n"
    "        strlcpy(identity, runtime.wifi_identity, sizeof(identity));\n"
    "        strlcpy(username, runtime.wifi_username, sizeof(username));\n"
    "        strlcpy(ttls_phase2, runtime.wifi_ttls_phase2, sizeof(ttls_phase2));\n"
    "        strlcpy(ca_mode, runtime.wifi_ca_mode, sizeof(ca_mode));\n"
    "        strlcpy(server_domain, runtime.wifi_server_domain, sizeof(server_domain));",
)
body = body.replace(
    "s_wifi_ssid[0] && !strcmp(ssid, s_wifi_ssid)",
    "host_preferred_ssid()[0] && !strcmp(ssid, host_preferred_ssid())",
)
body = body.replace(
    "if (!s_wifi_network_count) return;",
    "configuration_wifi_network_t saved_networks[CONFIGURATION_WIFI_NETWORK_CAPACITY];\n"
    "    uint8_t saved_count = 0;\n"
    "    (void)configuration_service_list_wifi_networks(\n"
    "        saved_networks, CONFIGURATION_WIFI_NETWORK_CAPACITY, &saved_count);\n"
    "    if (!saved_count) return;",
)
body = body.replace(
    "for (uint8_t i = 0; i < s_wifi_network_count; ++i) {",
    "for (uint8_t i = 0; i < saved_count; ++i) {",
)
body = body.replace(
    "s_wifi_networks[i].ssid",
    "saved_networks[i].ssid",
)
body = body.replace(
    "(void)configuration_service_list_wifi_networks(\n"
    "            s_wifi_networks, CONFIGURATION_WIFI_NETWORK_CAPACITY, &s_wifi_network_count);\n"
    "        if (!strcmp(ssid, s_wifi_ssid) && !is_enterprise_wifi()) {\n"
    "            s_wifi_ssid[0] = '\\0';\n"
    "            s_wifi_password[0] = '\\0';\n"
    "        }",
    "if (s_host.sync_runtime_after_network_delete) {\n"
    "            s_host.sync_runtime_after_network_delete(ssid);\n"
    "        }",
)
# Drop scan_records from scratch release if we no longer allocate them in service.
body = body.replace(
    "    if (s_setup_scan_records) {\n"
    "        heap_caps_free(s_setup_scan_records);\n"
    "        s_setup_scan_records = NULL;\n"
    "    }\n",
    "",
)
# refresh_setup_ssid_options uses s_setup_scan_records — keep scan in extracted
# body; add a local static scan buffer in service.
scan_fix = r'''
static wifi_ap_record_t *s_setup_scan_records;
'''

# audio_wake_word_start in stop transaction
body = body.replace(
    "esp_err_t wake_err = audio_wake_word_start(on_wake_word, NULL);\n"
    "        if (wake_err != ESP_OK && wake_err != ESP_ERR_INVALID_STATE) {\n"
    "            ESP_LOGW(TAG, \"cannot restore offline wake after setup transaction: %s\",\n"
    "                     esp_err_to_name(wake_err));\n"
    "        }",
    "if (s_host.wake_word_start) {\n"
    "            device_status_t wake_err = s_host.wake_word_start();\n"
    "            if (wake_err != DEVICE_STATUS_OK && wake_err != DEVICE_STATUS_BUSY) {\n"
    "                ESP_LOGW(TAG, \"cannot restore offline wake after setup transaction: status=%d\",\n"
    "                         (int)wake_err);\n"
    "            }\n"
    "        }",
)

out = PREAMBLE + scan_fix + glue + body + POSTAMBLE
(ROOT / "main" / "services" / "provisioning_service.c").write_text(out, encoding="utf-8")
print("wrote provisioning_service.c", len(out.splitlines()), "lines")
