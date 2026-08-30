#include "services/provisioning_service.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "esp_err.h"
#include "esp_heap_caps.h"
#include "esp_http_server.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "esp_system.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "lwip/sockets.h"
#include "mbedtls/platform_util.h"
#include "mbedtls/constant_time.h"

#include "configuration_service.h"
#include "device_api.h"
#include "provisioning_failure_injection.h"
#include "services/audio_arbitration_service.h"
#include "services/entropy_service.h"
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
#define SETUP_PORTAL_TTL_MS (10u * 60u * 1000u)
#define SETUP_PORTAL_RATE_CLIENT_CAPACITY 4u
#define SETUP_PORTAL_MUTATION_WINDOW_US (60LL * 1000LL * 1000LL)
#define SETUP_PORTAL_MUTATION_LIMIT 4u
#define SETUP_PORTAL_REFRESH_WINDOW_US (30LL * 1000LL * 1000LL)
#define SETUP_PORTAL_REFRESH_LIMIT 3u
/* The portal has a single HTTP worker and carries credentials. Bound both the
 * incomplete-request socket lifetime and the POST body receive budget so a
 * slow client cannot occupy it for the SDK's five-second default repeatedly. */
#define SETUP_PORTAL_HTTP_MAX_OPEN_SOCKETS 3u
#define SETUP_PORTAL_HTTP_BACKLOG_CONNECTIONS 2u
#define SETUP_PORTAL_HTTP_RECV_WAIT_TIMEOUT_SECONDS 2u
#define SETUP_PORTAL_HTTP_SEND_WAIT_TIMEOUT_SECONDS 2u
#define SETUP_PORTAL_FORM_RECEIVE_DEADLINE_MS 4000u

static provisioning_service_host_t s_host;
static portMUX_TYPE s_task_state_lock = portMUX_INITIALIZER_UNLOCKED;
static SemaphoreHandle_t s_setup_portal_mutex;
static SemaphoreHandle_t s_setup_options_mutex;
static httpd_handle_t s_setup_server;
static bool s_setup_http_admission_open;
static device_power_lease_t s_setup_power_lease = DEVICE_POWER_LEASE_INVALID;
/* One unpredictable value per portal generation binds mutable POSTs to the
 * page that presented them. Captive probes never need it; save/delete do. */
static char s_setup_csrf_token[25];
static char *s_setup_ssid_options;
static char *s_setup_ssid_choices;
static char *s_setup_saved_html;
static char *s_setup_save_body;
typedef struct {
    uint32_t peer_ipv4;
    int64_t mutation_window_started_us;
    uint8_t mutation_requests;
    int64_t refresh_window_started_us;
    uint8_t refresh_requests;
} setup_portal_rate_client_t;
static setup_portal_rate_client_t s_setup_rate_clients[SETUP_PORTAL_RATE_CLIENT_CAPACITY];
static TaskHandle_t s_dns_task;
static SemaphoreHandle_t s_dns_start_gate;
static SemaphoreHandle_t s_dns_stopped;
static SemaphoreHandle_t s_dns_ready;
static bool s_dns_ready_success;
static bool s_dns_stop_requested;
static bool s_dns_starting;
static bool s_dns_admission_open;
/* Completion is not retirement.  Keep a terminal failure if the immutable
 * Registry identity cannot be removed, so a later portal generation never
 * reuses a DNS worker that lifecycle rollback can still address. */
static bool s_dns_retiring;
static esp_err_t s_dns_exit_status = ESP_OK;
static bool s_dns_registry_retirement_failed;
static TaskHandle_t s_setup_ttl_task;
static SemaphoreHandle_t s_setup_ttl_start_gate;
static SemaphoreHandle_t s_setup_ttl_stopped;
static bool s_setup_ttl_stop_requested;
static bool s_setup_ttl_starting;
static bool s_setup_ttl_admission_open;
static bool s_setup_ttl_retiring;
static esp_err_t s_setup_ttl_exit_status = ESP_OK;
static bool s_setup_ttl_registry_retirement_failed;
static TaskHandle_t s_setup_restart_task;
static SemaphoreHandle_t s_setup_restart_start_gate;
static SemaphoreHandle_t s_setup_restart_stopped;
static bool s_setup_restart_stop_requested;
static bool s_setup_restart_starting;
static bool s_setup_restart_admission_open;
static bool s_setup_restart_retiring;
static esp_err_t s_setup_restart_exit_status = ESP_OK;
static bool s_setup_restart_registry_retirement_failed;
/* This is a reversible admission fence only.  A live portal owns a Power
 * lease and normally rejects the wider transaction earlier, but the delayed
 * post-save restart releases that lease before it calls esp_restart().  Keep
 * that terminal window explicit rather than letting a future COMMIT race it. */
static bool s_system_sleep_preparing;

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


static void host_copy_preferred_ssid(char *out, size_t capacity) {
    if (!out || capacity == 0u) return;
    out[0] = '\0';
    if (s_host.copy_preferred_scan_ssid) {
        s_host.copy_preferred_scan_ssid(out, (uint32_t)capacity);
    }
    out[capacity - 1u] = '\0';
}

static void host_copy_runtime_wifi(provisioning_runtime_wifi_t *out) {
    if (!out) return;
    memset(out, 0, sizeof(*out));
    if (s_host.copy_runtime_wifi) s_host.copy_runtime_wifi(out);
}

static bool setup_portal_http_admission_open(void) {
    bool open;
    taskENTER_CRITICAL(&s_task_state_lock);
    open = s_setup_http_admission_open;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return open;
}

static void set_setup_portal_http_admission(bool open) {
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_http_admission_open = open;
    taskEXIT_CRITICAL(&s_task_state_lock);
}

static bool is_valid_choice(const char *value, const char *first, const char *second,
                            const char *third) {
    return value && (!strcmp(value, first) || (second && !strcmp(value, second)) ||
                     (third && !strcmp(value, third)));
}

static bool is_valid_gateway_url(const char *url) {
    if (!url || !url[0] || strlen(url) >= URL_CAPACITY) return false;
    const char *host = !strncmp(url, "https://", 8) ? url + 8 : NULL;
    if (!host || host[0] == '\0' || host[0] == '/') return false;
    for (const unsigned char *p = (const unsigned char *)host; *p; ++p) {
        if (*p < 0x20u || *p == 0x7fu || *p == '#' || *p == '?' || *p == '@' ||
            *p == '\\') return false;
    }
    return true;
}

static bool is_six_digit_pair_code(const char *code) {
    return configuration_service_valid_pairing_code(code);
}

typedef enum {
    SETUP_PORTAL_RATE_MUTATION = 0,
    SETUP_PORTAL_RATE_REFRESH,
} setup_portal_rate_class_t;

/* The captive redirect routes are intentionally not throttled: OS captive
 * assistants retry those probes aggressively and a 429 there can prevent the
 * setup sheet from opening at all.  Only credential mutation and active scan
 * routes consume this generation-local, peer-keyed budget. */
static bool setup_portal_rate_limit_allows(httpd_req_t *req,
                                           setup_portal_rate_class_t rate_class,
                                           uint32_t *retry_after_seconds) {
    if (retry_after_seconds) *retry_after_seconds = 1;
    if (!req) return false;
    struct sockaddr_in peer = {0};
    socklen_t peer_length = sizeof(peer);
    const int socket_fd = httpd_req_to_sockfd(req);
    if (socket_fd < 0 || getpeername(socket_fd, (struct sockaddr *)&peer,
                                     &peer_length) != 0 ||
        peer.sin_family != AF_INET || peer.sin_addr.s_addr == 0) {
        /* Do not make unauthenticated source identity ambiguous: all valid AP
         * clients have an IPv4 lease, and an unidentifiable socket must not
         * bypass a per-client budget. */
        return false;
    }
    const int64_t now_us = esp_timer_get_time();
    const int64_t window_us = rate_class == SETUP_PORTAL_RATE_MUTATION
                                  ? SETUP_PORTAL_MUTATION_WINDOW_US
                                  : SETUP_PORTAL_REFRESH_WINDOW_US;
    const uint8_t limit = rate_class == SETUP_PORTAL_RATE_MUTATION
                              ? SETUP_PORTAL_MUTATION_LIMIT
                              : SETUP_PORTAL_REFRESH_LIMIT;
    bool allowed = false;
    int64_t retry_us = window_us;
    taskENTER_CRITICAL(&s_task_state_lock);
    setup_portal_rate_client_t *entry = NULL;
    setup_portal_rate_client_t *oldest = &s_setup_rate_clients[0];
    for (size_t i = 0; i < SETUP_PORTAL_RATE_CLIENT_CAPACITY; ++i) {
        setup_portal_rate_client_t *candidate = &s_setup_rate_clients[i];
        if (candidate->peer_ipv4 == peer.sin_addr.s_addr) {
            entry = candidate;
            break;
        }
        if (candidate->peer_ipv4 == 0) {
            entry = candidate;
            break;
        }
        const int64_t candidate_recent = candidate->mutation_window_started_us >
                                              candidate->refresh_window_started_us
                                          ? candidate->mutation_window_started_us
                                          : candidate->refresh_window_started_us;
        const int64_t oldest_recent = oldest->mutation_window_started_us >
                                           oldest->refresh_window_started_us
                                       ? oldest->mutation_window_started_us
                                       : oldest->refresh_window_started_us;
        if (candidate_recent < oldest_recent) oldest = candidate;
    }
    if (!entry) {
        entry = oldest;
        memset(entry, 0, sizeof(*entry));
    }
    if (entry->peer_ipv4 == 0) entry->peer_ipv4 = peer.sin_addr.s_addr;
    int64_t *window_started = rate_class == SETUP_PORTAL_RATE_MUTATION
                                  ? &entry->mutation_window_started_us
                                  : &entry->refresh_window_started_us;
    uint8_t *requests = rate_class == SETUP_PORTAL_RATE_MUTATION
                            ? &entry->mutation_requests
                            : &entry->refresh_requests;
    if (*window_started == 0 || now_us - *window_started >= window_us) {
        *window_started = now_us;
        *requests = 0;
    }
    if (*requests < limit) {
        ++*requests;
        allowed = true;
    } else {
        retry_us = window_us - (now_us - *window_started);
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (retry_after_seconds) {
        const uint64_t rounded = retry_us <= 0 ? 1u : ((uint64_t)retry_us + 999999u) / 1000000u;
        *retry_after_seconds = rounded > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded;
    }
    return allowed;
}

static bool setup_portal_rate_limit_exceeded(httpd_req_t *req,
                                              setup_portal_rate_class_t rate_class) {
    uint32_t retry_after_seconds = 1;
    if (setup_portal_rate_limit_allows(req, rate_class, &retry_after_seconds)) return false;
    char retry_after[16];
    snprintf(retry_after, sizeof(retry_after), "%u", (unsigned)retry_after_seconds);
    httpd_resp_set_status(req, "429 Too Many Requests");
    httpd_resp_set_hdr(req, "Retry-After", retry_after);
    httpd_resp_set_hdr(req, "Connection", "close");
    (void)httpd_resp_sendstr(req, "Too many setup requests. Please wait and try again.");
    return true;
}

static bool form_value(const char *body, const char *key, char *out, size_t cap);

/* Every accepted setup POST can carry Wi-Fi/EAP credentials and a one-time
 * pairing code.  The portal keeps one reusable PSRAM form buffer because the
 * HTTP server has one handler task, but it must not retain a previous POST
 * until the whole portal generation ends.  GCC's cleanup hook runs on every
 * handler return (including validation and response errors), which keeps the
 * error paths from becoming a second secret-retention policy. */
typedef struct {
    char *body;
} setup_portal_form_scope_t;

typedef struct {
    char ssid[WIFI_VALUE_CAPACITY];
    char password[WIFI_VALUE_CAPACITY];
    char gateway[URL_CAPACITY];
    char code[PAIR_CODE_CAPACITY];
    char security[WIFI_EAP_MODE_CAPACITY];
    char eap_method[WIFI_EAP_MODE_CAPACITY];
    char identity[WIFI_ENTERPRISE_VALUE_CAPACITY];
    char username[WIFI_ENTERPRISE_VALUE_CAPACITY];
    char ttls_phase2[WIFI_EAP_MODE_CAPACITY];
    char ca_mode[WIFI_EAP_MODE_CAPACITY];
    char server_domain[WIFI_ENTERPRISE_VALUE_CAPACITY];
    char reuse[4];
    provisioning_runtime_wifi_t runtime;
    configuration_provisioning_request_t provisioning_request;
} setup_portal_save_secret_scope_t;

typedef struct {
    char ssid[WIFI_VALUE_CAPACITY];
} setup_portal_delete_secret_scope_t;

typedef struct {
    char passphrase[PROVISIONING_AP_PASSPHRASE_CAPACITY];
} setup_portal_ap_secret_scope_t;

static void cleanup_setup_portal_form_scope(setup_portal_form_scope_t *scope) {
    if (scope && scope->body) {
        mbedtls_platform_zeroize(scope->body, SETUP_SAVE_BODY_CAPACITY);
    }
}

static void cleanup_setup_portal_save_secret_scope(
    setup_portal_save_secret_scope_t *scope) {
    if (scope) mbedtls_platform_zeroize(scope, sizeof(*scope));
}

static void cleanup_setup_portal_delete_secret_scope(
    setup_portal_delete_secret_scope_t *scope) {
    if (scope) mbedtls_platform_zeroize(scope, sizeof(*scope));
}

static void cleanup_setup_portal_ap_secret_scope(
    setup_portal_ap_secret_scope_t *scope) {
    if (scope) mbedtls_platform_zeroize(scope, sizeof(*scope));
}

/* A setup hotspot carries Wi-Fi/EAP and Hub credentials, so an open AP would
 * expose those form posts to every nearby station. Generate a printable WPA2
 * passphrase for each portal generation; it stays only in this task's stack
 * and the Wi-Fi driver, never in NVS or the runtime Wi-Fi configuration. */
static bool generate_setup_ap_passphrase(char out[PROVISIONING_AP_PASSPHRASE_CAPACITY]) {
    static const char alphabet[] =
        "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
    uint8_t entropy[12] = {0};
    if (!entropy_service_fill(entropy, sizeof(entropy))) {
        memset(out, 0, PROVISIONING_AP_PASSPHRASE_CAPACITY);
        mbedtls_platform_zeroize(entropy, sizeof(entropy));
        return false;
    }
    for (size_t i = 0; i < sizeof(entropy); ++i) {
        out[i] = alphabet[entropy[i] % (sizeof(alphabet) - 1u)];
    }
    out[sizeof(entropy)] = '\0';
    mbedtls_platform_zeroize(entropy, sizeof(entropy));
    return true;
}

static bool generate_setup_csrf_token(char out[sizeof(s_setup_csrf_token)]) {
    static const char alphabet[] =
        "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
    uint8_t entropy[24] = {0};
    if (!entropy_service_fill(entropy, sizeof(entropy))) {
        memset(out, 0, sizeof(s_setup_csrf_token));
        mbedtls_platform_zeroize(entropy, sizeof(entropy));
        return false;
    }
    for (size_t i = 0; i < sizeof(entropy); ++i) {
        out[i] = alphabet[entropy[i] % (sizeof(alphabet) - 1u)];
    }
    out[sizeof(entropy)] = '\0';
    mbedtls_platform_zeroize(entropy, sizeof(entropy));
    return true;
}

static bool setup_portal_csrf_valid(const char *body) {
    char provided[sizeof(s_setup_csrf_token)] = {0};
    if (!body || !s_setup_csrf_token[0] ||
        !form_value(body, "csrf", provided, sizeof(provided))) {
        return false;
    }
    const size_t expected_length = strlen(s_setup_csrf_token);
    const bool valid = strlen(provided) == expected_length &&
                       mbedtls_ct_memcmp(provided, s_setup_csrf_token,
                                         expected_length) == 0;
    mbedtls_platform_zeroize(provided, sizeof(provided));
    return valid;
}

static esp_err_t stop_setup_portal_transaction(uint32_t timeout_ms,
                                               bool restore_wake_word);
static esp_err_t stop_setup_portal_transaction_locked(uint32_t timeout_ms,
                                                      bool restore_wake_word);
static bool setup_restart_is_pending(void);
static bool start_captive_dns(void);
static esp_err_t stop_captive_dns_task(uint32_t timeout_ms);
static bool refresh_setup_ssid_options(void);
static void release_setup_portal_scratch(void);
static bool schedule_setup_restart(void);
static void build_setup_saved_networks_html(void);
static esp_err_t receive_setup_form_body(httpd_req_t *req, char *body,
                                         size_t body_capacity);
static esp_err_t setup_get_handler(httpd_req_t *req);
static esp_err_t captive_redirect_handler(httpd_req_t *req);
static esp_err_t setup_refresh_handler(httpd_req_t *req);
static esp_err_t setup_save_handler(httpd_req_t *req);
static esp_err_t setup_delete_handler(httpd_req_t *req);
static esp_err_t stop_setup_portal_ttl_task(uint32_t timeout_ms);
static bool start_setup_portal_ttl_task(void);

/* The portal's credential-bearing HTTP/DNS surface must not remain available
 * indefinitely after its QR code was shown.  The TTL worker is deliberately a
 * regular registered task instead of an esp_timer callback: expiry needs the
 * same bounded, serialized portal transaction as a manual stop, and must not
 * race portal-handle destruction from timer context. */
static void setup_portal_ttl_task(void *arg) {
    (void)arg;
    if (!s_setup_ttl_start_gate ||
        xSemaphoreTake(s_setup_ttl_start_gate, portMAX_DELAY) != pdTRUE) {
        TaskHandle_t self = xTaskGetCurrentTaskHandle();
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_ttl_retiring = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        const esp_err_t registry_err = task_registry_unregister_with_timeout(
            TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_ttl_exit_status = registry_err;
        if (s_setup_ttl_task == self) s_setup_ttl_task = NULL;
        s_setup_ttl_starting = false;
        s_setup_ttl_retiring = false;
        if (registry_err != ESP_OK) s_setup_ttl_registry_retirement_failed = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_setup_ttl_stopped) xSemaphoreGive(s_setup_ttl_stopped);
        vTaskDelete(NULL);
        return;
    }

    const uint32_t notification =
        ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(SETUP_PORTAL_TTL_MS));
    taskENTER_CRITICAL(&s_task_state_lock);
    const bool stop_requested = s_setup_ttl_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!stop_requested && notification == 0) {
        /* Close request admission before contending for the composite portal
         * lock.  Should a concurrent lifecycle operation consume the bounded
         * stop budget, credential-changing POSTs remain fail-closed rather
         * than extending the exposure window. */
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_ttl_admission_open = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        set_setup_portal_http_admission(false);
        ESP_LOGI(TAG, "setup portal expired after %u ms", (unsigned)SETUP_PORTAL_TTL_MS);
        esp_err_t stop_err = stop_setup_portal_transaction(500, true);
        if (stop_err != ESP_OK) {
            ESP_LOGW(TAG, "expired setup portal drain incomplete: %s",
                     esp_err_to_name(stop_err));
        }
    }

    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_ttl_retiring = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_ttl_exit_status = registry_err;
    if (s_setup_ttl_task == self) s_setup_ttl_task = NULL;
    s_setup_ttl_starting = false;
    s_setup_ttl_retiring = false;
    if (registry_err != ESP_OK) s_setup_ttl_registry_retirement_failed = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_setup_ttl_stopped) xSemaphoreGive(s_setup_ttl_stopped);
    vTaskDelete(NULL);
}

static esp_err_t stop_setup_portal_ttl_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_ttl_admission_open = false;
    s_setup_ttl_stop_requested = true;
    task = s_setup_ttl_task;
    const esp_err_t exit_status = s_setup_ttl_exit_status;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return exit_status;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(task);
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (!s_setup_ttl_stopped || remaining_ms == 0 ||
        xSemaphoreTake(s_setup_ttl_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    const esp_err_t completed_status = s_setup_ttl_exit_status;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return completed_status;
}

static esp_err_t stop_setup_portal_ttl_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_setup_ttl_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_setup_portal_ttl_task(timeout_ms);
}

static bool start_setup_portal_ttl_task(void) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    const bool admission_open = s_setup_ttl_admission_open &&
                                !s_setup_ttl_registry_retirement_failed;
    const bool already_starting = s_setup_ttl_starting || s_setup_ttl_task != NULL ||
                                  s_setup_ttl_retiring;
    if (admission_open && !already_starting) s_setup_ttl_starting = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!admission_open) {
        ESP_LOGW(TAG, "setup portal TTL rejected: lifecycle admission is closed");
        return false;
    }
    if (already_starting) return true;
    if (!s_setup_ttl_start_gate || !s_setup_ttl_stopped ||
        provisioning_failure_injection_lifecycle_primitives_unavailable()) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_ttl_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "setup portal TTL lifecycle primitives unavailable%s",
                 provisioning_failure_injection_lifecycle_primitives_unavailable()
                     ? " (test injection)" : "");
        return false;
    }
    while (xSemaphoreTake(s_setup_ttl_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_ttl_stop_requested = false;
    s_setup_ttl_exit_status = ESP_OK;
    s_setup_ttl_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    BaseType_t create_result = provisioning_failure_injection_task_create_fails()
                                   ? pdFAIL
                                   : xTaskCreate(setup_portal_ttl_task,
                                                 "maclaw_setup_ttl", 2048,
                                                 NULL, 2, &task);
    if (create_result != pdPASS) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_ttl_task = NULL;
        s_setup_ttl_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "cannot start setup portal TTL%s",
                 provisioning_failure_injection_task_create_fails()
                     ? " (test injection)" : "");
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_ttl_task = task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    esp_err_t registry_err = provisioning_failure_injection_task_registry_register_fails()
                                 ? ESP_ERR_NO_MEM
                                 : task_registry_register(&(task_registry_entry_t){
                                       .struct_size = sizeof(task_registry_entry_t),
                                       .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
                                       .name = "setup_ttl",
                                       .context = (void *)task,
                                       .stop = stop_setup_portal_ttl_registry_entry,
                                   });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register setup portal TTL: %s%s",
                 esp_err_to_name(registry_err),
                 provisioning_failure_injection_task_registry_register_fails()
                     ? " (test injection)" : "");
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_ttl_stop_requested = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        xSemaphoreGive(s_setup_ttl_start_gate);
        (void)stop_setup_portal_ttl_task(500);
        return false;
    }
    xSemaphoreGive(s_setup_ttl_start_gate);
    return true;
}

static void setup_restart_task(void *arg) {
    (void)arg;
    /* The HTTP handler creates this task before it is visible to the lifecycle
     * registry.  Do no work until that publication has completed: an early
     * startup rollback must be able to close admission and join it rather than
     * allowing an untracked delayed reset to fire later. */
    if (!s_setup_restart_start_gate ||
        xSemaphoreTake(s_setup_restart_start_gate, portMAX_DELAY) != pdTRUE) {
        TaskHandle_t self = xTaskGetCurrentTaskHandle();
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_restart_retiring = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        const esp_err_t registry_err = task_registry_unregister_with_timeout(
            TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_restart_exit_status = registry_err;
        if (s_setup_restart_task == self) s_setup_restart_task = NULL;
        s_setup_restart_starting = false;
        s_setup_restart_retiring = false;
        if (registry_err != ESP_OK) s_setup_restart_registry_retirement_failed = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_setup_restart_stopped) xSemaphoreGive(s_setup_restart_stopped);
        vTaskDelete(NULL);
        return;
    }

    /* Let esp_http_server flush its response, but make the delay cancellable.
     * A task notification is deliberately used instead of vTaskDelay(): it
     * provides the stop safe point without taking ownership of the portal,
     * DNS responder, HTTP server or Wi-Fi mode. */
    (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(1200));
    taskENTER_CRITICAL(&s_task_state_lock);
    bool stop_requested = s_setup_restart_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (stop_requested) {
        goto finish;
    }
    /* Saving credentials is a terminal provisioning transition.  Reboot is
     * still the supported way to apply the new station configuration, but it
     * must not be the only cleanup mechanism: close portal admission, join
     * HTTP/DNS and release the logical session before the reset.  AP/DHCP and
     * Wi-Fi are intentionally left to that reset; this is not an APSTA->STA
     * runtime-restart claim. */
    esp_err_t portal_stop_err = stop_setup_portal_transaction(500, false);
    if (portal_stop_err != ESP_OK) {
        /* The saved configuration has already committed.  A physical restart
         * is safer than retaining an admission-closed but partially drained
         * portal whose new configuration cannot take effect in this boot. */
        ESP_LOGW(TAG, "setup portal cleanup before restart incomplete: %s",
                 esp_err_to_name(portal_stop_err));
    }
    /* `stop_setup_restart_task()` may arrive while HTTP/DNS are draining.
     * Observe its token a second time before committing the reset; otherwise
     * the caller could receive our completion and continue rollback while this
     * coordinator restarts the chip. */
    taskENTER_CRITICAL(&s_task_state_lock);
    stop_requested = s_setup_restart_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
finish:
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_restart_retiring = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_restart_exit_status = registry_err;
    if (s_setup_restart_task == self) s_setup_restart_task = NULL;
    s_setup_restart_starting = false;
    s_setup_restart_retiring = false;
    if (registry_err != ESP_OK) s_setup_restart_registry_retirement_failed = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_setup_restart_stopped) xSemaphoreGive(s_setup_restart_stopped);
    if (stop_requested) {
        ESP_LOGI(TAG, "setup restart coordinator stopped before reset");
        vTaskDelete(NULL);
        return;
    }
    ESP_LOGI(TAG, "setup saved; restarting into normal mode");
    esp_restart();
}

/* This coordinator owns the post-save delay and the terminal user-space
 * portal cleanup before an intentional reset.  It still does not own AP/STA,
 * DHCP or Wi-Fi-event lifetimes; those stay with the reset/physical network
 * composition root and are not claimed as runtime-restartable here. */
static esp_err_t stop_setup_restart_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_restart_admission_open = false;
    s_setup_restart_stop_requested = true;
    task = s_setup_restart_task;
    const esp_err_t exit_status = s_setup_restart_exit_status;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return exit_status;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(task);
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (!s_setup_restart_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_setup_restart_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    const esp_err_t completed_status = s_setup_restart_exit_status;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (completed_status != ESP_OK) return completed_status;
    ESP_LOGI(TAG, "setup restart coordinator stopped");
    return ESP_OK;
}

static esp_err_t stop_setup_restart_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_setup_restart_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_setup_restart_task(timeout_ms);
}

static bool schedule_setup_restart(void) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    bool admission_open = s_setup_restart_admission_open &&
                          !s_setup_restart_registry_retirement_failed;
    bool already_starting = s_setup_restart_starting || s_setup_restart_task != NULL ||
                            s_setup_restart_retiring;
    if (admission_open && !already_starting) s_setup_restart_starting = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!admission_open) {
        ESP_LOGW(TAG, "setup restart rejected: lifecycle admission is closed");
        return false;
    }
    if (already_starting) return true;
    if (!s_setup_restart_start_gate || !s_setup_restart_stopped ||
        provisioning_failure_injection_lifecycle_primitives_unavailable()) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_restart_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "setup restart lifecycle primitives unavailable%s",
                 provisioning_failure_injection_lifecycle_primitives_unavailable()
                     ? " (test injection)" : "");
        return false;
    }
    while (xSemaphoreTake(s_setup_restart_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_restart_stop_requested = false;
    s_setup_restart_exit_status = ESP_OK;
    s_setup_restart_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    BaseType_t create_result = provisioning_failure_injection_task_create_fails()
                                   ? pdFAIL
                                   : xTaskCreate(setup_restart_task,
                                                 "maclaw_setup_restart", 2048,
                                                 NULL, 2, &task);
    if (create_result != pdPASS) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_restart_starting = false;
        s_setup_restart_task = NULL;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "cannot schedule restart after setup save%s",
                 provisioning_failure_injection_task_create_fails()
                     ? " (test injection)" : "");
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_restart_task = task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    esp_err_t registry_err = provisioning_failure_injection_task_registry_register_fails()
                                 ? ESP_ERR_NO_MEM
                                 : task_registry_register(&(task_registry_entry_t){
                                       .struct_size = sizeof(task_registry_entry_t),
                                       .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
                                       .name = "setup_restart",
                                       .context = (void *)task,
                                       .stop = stop_setup_restart_registry_entry,
                                   });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register setup restart coordinator: %s%s",
                 esp_err_to_name(registry_err),
                 provisioning_failure_injection_task_registry_register_fails()
                     ? " (test injection)" : "");
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_restart_stop_requested = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        xSemaphoreGive(s_setup_restart_start_gate);
        (void)stop_setup_restart_task(500);
        return false;
    }
    xSemaphoreGive(s_setup_restart_start_gate);
    return true;
}

/* A post-save coordinator is deliberately terminal: once credentials have
 * committed, this generation must either reset or remain fail-closed.  Do not
 * reopen its admission from a later manual/recovery portal request before the
 * reset takes place; a second coordinator could otherwise reuse its stopped
 * token while the first task is still unwinding. */
static bool setup_restart_is_pending(void) {
    bool pending;
    taskENTER_CRITICAL(&s_task_state_lock);
    pending = s_setup_restart_starting || s_setup_restart_task != NULL ||
              s_setup_restart_retiring || s_setup_restart_registry_retirement_failed;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return pending;
}
static size_t append_setup_html_escaped(char *out, size_t used, size_t cap,
                                        const char *value) {
    for (const unsigned char *p = (const unsigned char *)(value ? value : ""); *p; ++p) {
        const char *replacement = NULL;
        switch (*p) {
            case '&': replacement = "&amp;"; break;
            case '<': replacement = "&lt;"; break;
            case '>': replacement = "&gt;"; break;
            case '\"': replacement = "&quot;"; break;
            case '\'': replacement = "&#39;"; break;
            default: break;
        }
        if (replacement) {
            size_t len = strlen(replacement);
            if (used + len >= cap) return cap;
            memcpy(out + used, replacement, len);
            used += len;
        } else {
            if (used + 1 >= cap) return cap;
            out[used++] = (char)*p;
        }
    }
    return used;
}

static size_t setup_html_escaped_length(const char *value) {
    size_t length = 0;
    for (const unsigned char *p = (const unsigned char *)(value ? value : ""); *p; ++p) {
        switch (*p) {
            case '&': length += 5; break;   // &amp;
            case '<':
            case '>': length += 4; break;   // &lt; / &gt;
            case '\"': length += 6; break; // &quot;
            case '\'': length += 5; break; // &#39;
            default: ++length; break;
        }
    }
    return length;
}

static bool setup_ssid_is_selectable(const char *ssid) {
    if (!ssid || !ssid[0]) return false;
    const char *choice = s_setup_ssid_choices;
    while (*choice) {
        if (!strcmp(choice, ssid)) return true;
        choice += strlen(choice) + 1;
    }
    return false;
}

static bool remember_setup_ssid_choice(const char *ssid) {
    if (!ssid || !ssid[0] || setup_ssid_is_selectable(ssid)) return true;
    size_t used = 0;
    while (used < SETUP_SSID_CHOICES_CAPACITY && s_setup_ssid_choices[used]) {
        used += strlen(s_setup_ssid_choices + used) + 1;
    }
    size_t length = strlen(ssid);
    if (used + length + 1 > SETUP_SSID_CHOICES_CAPACITY) return false;
    memcpy(s_setup_ssid_choices + used, ssid, length + 1);
    return true;
}

static bool can_remember_setup_ssid_choice(const char *ssid) {
    if (!ssid || !ssid[0] || setup_ssid_is_selectable(ssid)) return true;
    size_t used = 0;
    while (used < SETUP_SSID_CHOICES_CAPACITY && s_setup_ssid_choices[used]) {
        used += strlen(s_setup_ssid_choices + used) + 1;
    }
    return used + strlen(ssid) + 1 <= SETUP_SSID_CHOICES_CAPACITY;
}

static const char *setup_scan_security_label(provisioning_scan_security_t security);
static bool setup_scan_security_is_enterprise(provisioning_scan_security_t security);

static bool append_setup_ssid_option(const char *ssid, int rssi,
                                     provisioning_scan_security_t security_mode,
                                     bool selected) {
    if (!ssid || !ssid[0]) return true;
    size_t used = strlen(s_setup_ssid_options);
    if (setup_ssid_is_selectable(ssid)) return true;
    const char *prefix = "<option value=\"";
    const char *selected_attr = selected ? " selected" : "";
    const char *enterprise_attr = setup_scan_security_is_enterprise(security_mode)
                                       ? " data-enterprise=1" : "";
    const char *suffix = "</option>";
    size_t escaped_length = setup_html_escaped_length(ssid);
    const char *security = setup_scan_security_label(security_mode);
    // 2 bytes for the closing quote/bracket, 32 bytes for signal/security.
    if (used + strlen(prefix) + escaped_length * 2 + 2 + 32 +
        strlen(enterprise_attr) + strlen(selected_attr) + strlen(suffix) >=
            SETUP_SSID_OPTIONS_CAPACITY ||
        !can_remember_setup_ssid_choice(ssid)) return false;
    memcpy(s_setup_ssid_options + used, prefix, strlen(prefix));
    used += strlen(prefix);
    used = append_setup_html_escaped(s_setup_ssid_options, used, SETUP_SSID_OPTIONS_CAPACITY, ssid);
    int attribute_length = snprintf(s_setup_ssid_options + used,
                                    SETUP_SSID_OPTIONS_CAPACITY - used,
                                    "\"%s%s>", enterprise_attr, selected_attr);
    if (attribute_length <= 0 || (size_t)attribute_length >=
                                     SETUP_SSID_OPTIONS_CAPACITY - used) {
        return false;
    }
    used += (size_t)attribute_length;
    used = append_setup_html_escaped(s_setup_ssid_options, used, SETUP_SSID_OPTIONS_CAPACITY, ssid);
    int written = snprintf(s_setup_ssid_options + used, SETUP_SSID_OPTIONS_CAPACITY - used,
                           " (%d dBm, %s)%s", rssi, security, suffix);
    return written > 0 && (size_t)written < SETUP_SSID_OPTIONS_CAPACITY - used &&
           remember_setup_ssid_choice(ssid);
}

static const char *setup_scan_security_label(provisioning_scan_security_t mode) {
    switch (mode) {
        case PROVISIONING_SCAN_SECURITY_OPEN: return "open";
        case PROVISIONING_SCAN_SECURITY_WEP: return "WEP";
        case PROVISIONING_SCAN_SECURITY_WPA: return "WPA";
        case PROVISIONING_SCAN_SECURITY_WPA2: return "WPA2";
        case PROVISIONING_SCAN_SECURITY_WPA_WPA2: return "WPA/WPA2";
        case PROVISIONING_SCAN_SECURITY_WPA3: return "WPA3";
        case PROVISIONING_SCAN_SECURITY_WPA2_WPA3: return "WPA2/WPA3";
        case PROVISIONING_SCAN_SECURITY_ENTERPRISE: return "WPA-802.1X";
        default: return "secured";
    }
}

static bool setup_scan_security_is_enterprise(provisioning_scan_security_t security) {
    return security == PROVISIONING_SCAN_SECURITY_ENTERPRISE;
}

typedef struct {
    char ssid[PROVISIONING_WIFI_VALUE_CAPACITY];
    int8_t rssi;
    provisioning_scan_security_t security;
} setup_scan_entry_t;

typedef struct {
    setup_scan_entry_t entries[SETUP_SCAN_MAX_APS];
    uint16_t count;
} setup_scan_results_t;

static bool collect_setup_scan_entry(const char *ssid, int8_t rssi,
                                     provisioning_scan_security_t security, void *context) {
    setup_scan_results_t *results = context;
    if (!results || !ssid || !ssid[0] || results->count >= SETUP_SCAN_MAX_APS) return false;
    for (uint16_t index = 0; index < results->count; ++index) {
        if (!strcmp(results->entries[index].ssid, ssid)) return true;
    }
    setup_scan_entry_t *entry = &results->entries[results->count++];
    strlcpy(entry->ssid, ssid, sizeof(entry->ssid));
    entry->rssi = rssi;
    entry->security = security;
    return true;
}

static int compare_setup_scan_entries(const void *left, const void *right) {
    const setup_scan_entry_t *a = left;
    const setup_scan_entry_t *b = right;
    return (int)b->rssi - (int)a->rssi;
}

static bool refresh_setup_ssid_options(void) {
    if (!s_setup_options_mutex ||
        xSemaphoreTake(s_setup_options_mutex, pdMS_TO_TICKS(15000)) != pdTRUE) {
        ESP_LOGW(TAG, "setup Wi-Fi scan already in progress");
        return false;
    }

    setup_scan_results_t results = {0};
    char preferred_ssid[WIFI_VALUE_CAPACITY] = {0};
    host_copy_preferred_ssid(preferred_ssid, sizeof(preferred_ssid));
    device_status_t scan_status = !s_host.scan_visible_wifi
                                      ? DEVICE_STATUS_UNAVAILABLE
                                      : s_host.scan_visible_wifi(SETUP_SCAN_MAX_APS,
                                                                 collect_setup_scan_entry,
                                                                 &results);
    bool refreshed = false;
    if (scan_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "Wi-Fi scan for setup list failed: status=%d", (int)scan_status);
    } else {
        // A successful scan atomically replaces the selectable list while
        // this mutex is held. On a scan failure above, the old list remains
        // untouched so a user can safely retry without losing their choice.
        s_setup_ssid_options[0] = '\0';
        s_setup_ssid_choices[0] = '\0';
        qsort(results.entries, results.count, sizeof(results.entries[0]),
              compare_setup_scan_entries);
        for (uint16_t i = 0; i < results.count; ++i) {
            const setup_scan_entry_t *entry = &results.entries[i];
            if (setup_ssid_is_selectable(entry->ssid)) continue;
            if (!append_setup_ssid_option(
                    entry->ssid, entry->rssi, entry->security,
                    preferred_ssid[0] && !strcmp(entry->ssid, preferred_ssid))) {
                break;
            }
        }
        refreshed = true;
        ESP_LOGI(TAG, "setup Wi-Fi selection list contains %u scanned networks",
                 (unsigned)results.count);
    }
    if (refreshed && !s_setup_ssid_options[0]) {
        strlcpy(s_setup_ssid_options,
                "<option value=\"\" selected disabled>No visible Wi-Fi networks found; refresh the hotspot and try again.</option>",
                SETUP_SSID_OPTIONS_CAPACITY);
    }
    xSemaphoreGive(s_setup_options_mutex);
    return refreshed;
}
static void dns_server_task(void *arg) {
    (void)arg;
    /* The task is created before it is entered in Task Registry.  Keep it
     * dormant until the entry is published so a rollback never races an
     * untracked DNS socket with portal/AP teardown. */
    if (!s_dns_start_gate ||
        xSemaphoreTake(s_dns_start_gate, portMAX_DELAY) != pdTRUE) {
        TaskHandle_t self = xTaskGetCurrentTaskHandle();
        taskENTER_CRITICAL(&s_task_state_lock);
        s_dns_retiring = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        const esp_err_t registry_err = task_registry_unregister_with_timeout(
            TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
        taskENTER_CRITICAL(&s_task_state_lock);
        s_dns_exit_status = registry_err;
        if (s_dns_task == self) s_dns_task = NULL;
        s_dns_starting = false;
        s_dns_retiring = false;
        if (registry_err != ESP_OK) s_dns_registry_retirement_failed = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_dns_stopped) xSemaphoreGive(s_dns_stopped);
        vTaskDelete(NULL);
        return;
    }
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    bool ready_published = false;
    int socket_fd = socket(AF_INET, SOCK_DGRAM, IPPROTO_IP);
    if (socket_fd < 0) {
        ESP_LOGE(TAG, "cannot create captive DNS socket: errno=%d", errno);
        goto finish;
    }
    struct sockaddr_in address = {
        .sin_family = AF_INET,
        .sin_port = htons(DNS_PORT),
        .sin_addr.s_addr = htonl(INADDR_ANY),
    };
    /* Bounded receive is the DNS worker's cancellation safe point.  Do not
     * close the descriptor from another task: lwIP socket ownership stays with
     * this worker until it has left recvfrom() and completed its join. */
    struct timeval receive_timeout = {.tv_sec = 0, .tv_usec = 100000};
    (void)setsockopt(socket_fd, SOL_SOCKET, SO_RCVTIMEO, &receive_timeout, sizeof(receive_timeout));
    if (bind(socket_fd, (struct sockaddr *)&address, sizeof(address)) < 0) {
        ESP_LOGE(TAG, "cannot bind captive DNS socket: errno=%d", errno);
        goto finish;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_dns_ready_success = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_dns_ready) xSemaphoreGive(s_dns_ready);
    ready_published = true;
    ESP_LOGI(TAG, "captive DNS is answering all hostnames at %s", SETUP_AP_IP_ADDR);
    while (device_connectivity_is_provisioning_active()) {
        taskENTER_CRITICAL(&s_task_state_lock);
        bool stop_requested = s_dns_stop_requested;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (stop_requested) break;
        uint8_t packet[DNS_PACKET_CAPACITY];
        struct sockaddr_in source = {0};
        socklen_t source_len = sizeof(source);
        int received = recvfrom(socket_fd, packet, sizeof(packet), 0,
                                (struct sockaddr *)&source, &source_len);
        if (received < 12) continue;
        // This responder is authoritative only for ordinary DNS queries. Do
        // not turn a malformed response, UPDATE, or another opcode into a
        // seemingly valid captive-portal response.
        if ((packet[2] & 0x80) || (packet[2] & 0x78) ||
            packet[4] != 0 || packet[5] != 1) {
            continue;
        }
        /* Keep this wire behavior identical to Waveshare's official
         * esp-wifi-connect component: turn every one-question DNS query into
         * a one-record response and append the SoftAP A record to the original
         * datagram.  In particular, do not discard AAAA/HTTPS preflights: some
         * Android/iOS captive detectors only issue their HTTP request after
         * this deliberately permissive answer. */
        if ((size_t)received + 16u > sizeof(packet)) continue;
        packet[2] |= 0x80;  // response
        packet[3] |= 0x80;  // recursion available
        packet[6] = 0;
        packet[7] = 1;
        size_t cursor = (size_t)received;
        packet[cursor++] = 0xC0; packet[cursor++] = 0x0C; // answer name = question name
        packet[cursor++] = 0; packet[cursor++] = 1;        // A
        packet[cursor++] = 0; packet[cursor++] = 1;        // IN
        packet[cursor++] = 0; packet[cursor++] = 0;
        packet[cursor++] = 0; packet[cursor++] = 0; packet[cursor++] = 0; packet[cursor++] = 28;
        packet[cursor++] = 0; packet[cursor++] = 4;
        packet[cursor++] = 192; packet[cursor++] = 168; packet[cursor++] = 4; packet[cursor++] = 1;
        (void)sendto(socket_fd, packet, cursor, 0, (struct sockaddr *)&source, source_len);
    }
finish:
    if (socket_fd >= 0) close(socket_fd);
    if (!ready_published) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_dns_ready_success = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_dns_ready) xSemaphoreGive(s_dns_ready);
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_dns_retiring = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_dns_exit_status = registry_err;
    if (s_dns_task == self) s_dns_task = NULL;
    s_dns_starting = false;
    s_dns_retiring = false;
    if (registry_err != ESP_OK) s_dns_registry_retirement_failed = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_dns_stopped) xSemaphoreGive(s_dns_stopped);
    vTaskDelete(NULL);
}

/* Captive DNS owns only its UDP/53 socket and task.  This does not stop the
 * enclosing portal's HTTP server, DHCP server, SoftAP/STA mode or Wi-Fi event
 * handlers; those remain outside this Registry entry until they share a real
 * Provisioning Service shutdown contract. */
static esp_err_t stop_captive_dns_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_dns_admission_open = false;
    s_dns_stop_requested = true;
    task = s_dns_task;
    const esp_err_t exit_status = s_dns_exit_status;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return exit_status;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(task);
    const uint32_t join_timeout_ms = remaining_timeout_ms(deadline_us);
    if (join_timeout_ms == 0 || !s_dns_stopped ||
        xSemaphoreTake(s_dns_stopped, pdMS_TO_TICKS(join_timeout_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    const esp_err_t completed_status = s_dns_exit_status;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (completed_status != ESP_OK) return completed_status;
    ESP_LOGI(TAG, "captive DNS worker stopped");
    return ESP_OK;
}

static esp_err_t stop_captive_dns_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_dns_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_captive_dns_task(timeout_ms);
}

static bool start_captive_dns(void) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    bool admission_open = s_dns_admission_open && !s_dns_registry_retirement_failed;
    bool already_starting = s_dns_starting || s_dns_task != NULL || s_dns_retiring;
    if (admission_open && !already_starting) s_dns_starting = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!admission_open) {
        ESP_LOGW(TAG, "captive DNS start rejected: lifecycle admission is closed");
        return false;
    }
    if (already_starting) return true;
    if (!s_dns_start_gate || !s_dns_stopped) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_dns_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "captive DNS lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_dns_stopped, 0) == pdTRUE) {}
    while (s_dns_ready && xSemaphoreTake(s_dns_ready, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_dns_stop_requested = false;
    s_dns_ready_success = false;
    s_dns_exit_status = ESP_OK;
    s_dns_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (xTaskCreate(dns_server_task, "maclaw_captive_dns", 3072, NULL, 3, &task) != pdPASS) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_dns_task = NULL;
        s_dns_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGW(TAG, "cannot start captive DNS task");
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_dns_task = task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
        .name = "captive_dns",
        .context = (void *)task,
        .stop = stop_captive_dns_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register captive DNS worker: %s", esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_task_state_lock);
        s_dns_stop_requested = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        xSemaphoreGive(s_dns_start_gate);
        (void)stop_captive_dns_task(500);
        return false;
    }
    xSemaphoreGive(s_dns_start_gate);
    /* The worker reports only after bind(UDP/53). This stops the portal from
     * advertising a captive form whose DNS interceptor never came online. */
    if (!s_dns_ready ||
        xSemaphoreTake(s_dns_ready, pdMS_TO_TICKS(1200)) != pdTRUE) {
        ESP_LOGE(TAG, "captive DNS did not report readiness");
        (void)stop_captive_dns_task(500);
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    bool ready = s_dns_ready_success;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!ready) {
        ESP_LOGE(TAG, "captive DNS failed before readiness");
        (void)stop_captive_dns_task(500);
    }
    return ready;
}

static esp_err_t setup_get_handler(httpd_req_t *req) {
    if (!setup_portal_http_admission_open()) {
        httpd_resp_set_status(req, "503 Service Unavailable");
        httpd_resp_set_hdr(req, "Connection", "close");
        return httpd_resp_sendstr(req, "Setup portal is stopping.");
    }
    // Keep the setup page small and deterministic. The earlier generated page
    // could exceed its fixed stack buffer when many SSIDs were present, which
    // reset the ESP exactly when a phone requested the portal.
    static const char setup_page_prefix[] =
        "<!doctype html><html><head><meta name=viewport content='width=device-width,initial-scale=1'>"
        "<style>body{font-family:system-ui,sans-serif;max-width:26rem;margin:2rem auto;padding:0 1rem;color:#102a43}"
        "label{display:block;margin:1rem 0 .3rem}input,select{box-sizing:border-box;width:100%;padding:.7rem;font-size:1rem}"
        ".enterprise{margin-top:1rem;padding:.85rem;border:1px solid #b9c9d7;background:#f5f9fc}.hint{font-size:.85rem;color:#486581;line-height:1.45}"
        ".saved{display:flex;align-items:center;justify-content:space-between;gap:.6rem;padding:.45rem 0;border-bottom:1px solid #dbe4ec;word-break:break-all}"
        ".saved form{margin:0}.saved button{margin:0;padding:.45rem .8rem;background:#b23b3b}"
        "button{margin-top:1.3rem;padding:.8rem 1.2rem;font-size:1rem;background:#1769aa;color:#fff;border:0;border-radius:.4rem}</style>"
        "</head><body><h1>MaClaw Pet setup</h1><p>Choose your home or office Wi-Fi. The device will restart and connect automatically.</p>"
        "<form method=post action=/save>";
    static const char setup_page_suffix[] =
        "<label>Wi-Fi network</label><select name=ssid required>";
    static const char setup_page_options_suffix[] =
        "</select><p class=hint>Only visible Wi-Fi networks are shown. <a href=/refresh>Refresh network list</a>. Hidden networks must temporarily enable SSID broadcast.</p>"
        "<label>Security</label><select name=security id=security onchange='document.getElementById(\"enterprise\").hidden=this.value!==\"enterprise\";document.getElementById(\"passlabel\").textContent=this.value===\"enterprise\"?\"Password\":\"Wi-Fi password\"'><option value=personal selected>Personal (WPA/WPA2/WPA3)</option><option value=enterprise>Enterprise (802.1X)</option></select>"
        "<label id=passlabel>Wi-Fi password</label><input name=password type=password maxlength=64>"
        "<section class=enterprise id=enterprise hidden><strong>Enterprise Wi-Fi</strong><p class=hint>Defaults match typical phone settings: PEAP, MSCHAPv2, system certificates. Ask your IT administrator only if your network differs.</p>"
        "<label>EAP method</label><select name=eap_method><option value=peap selected>PEAP</option><option value=ttls>TTLS</option></select>"
        "<label>Identity (optional)</label><input name=identity maxlength=127 autocapitalize=none placeholder='Anonymous identity, if required'>"
        "<label>Username</label><input name=username maxlength=127 autocapitalize=none placeholder='Required'>"
        "<label>TTLS inner authentication</label><select name=ttls_phase2><option value=mschapv2 selected>MSCHAPv2 (default)</option><option value=pap>PAP</option></select>"
        "<label>CA certificate</label><select name=ca_mode><option value=system selected>Use system certificates</option></select>"
        "<label>Server domain (required for certificate binding)</label><input name=server_domain maxlength=127 required autocapitalize=none placeholder='Example: radius.company.com'></section>"
        "<label>MaClaw Hub URL</label><input name=gateway value='https://hub.mypapers.top' required maxlength=255>"
        "<label>6-digit pairing code</label><input name=code inputmode=numeric pattern='[0-9]{6}' maxlength=6 required>"
        "<button>Save and connect</button></form>";
    // 已存热点列表 chunk 之后是收尾脚本与页面结束标签。
    static const char setup_page_tail[] =
        "<script>(function(){var n=document.querySelector('[name=ssid]'),s=document.getElementById('security');function u(){if(n&&n.selectedOptions[0]&&n.selectedOptions[0].dataset.enterprise==='1'){s.value='enterprise';s.dispatchEvent(new Event('change'))}}n&&n.addEventListener('change',u);u()})()</script></body></html>";
    static const char scan_failed_notice[] =
        "<p class=hint role=alert>Could not refresh Wi-Fi networks. Showing the previous list; please try again.</p>";
    static const char pairing_page[] =
        "<!doctype html><html><head><meta name=viewport content='width=device-width,initial-scale=1'>"
        "<style>body{font-family:system-ui,sans-serif;max-width:26rem;margin:2rem auto;padding:0 1rem;color:#102a43}"
        ".ok{padding:.8rem;background:#e8f7ef;border-radius:.5rem}label{display:block;margin:1rem 0 .3rem}"
        "input{box-sizing:border-box;width:100%;padding:.8rem;font-size:1.2rem;letter-spacing:.25rem}"
        "button{margin-top:1.3rem;padding:.8rem 1.2rem;font-size:1rem;background:#1769aa;color:#fff;border:0;border-radius:.4rem}</style>"
        "</head><body><h1>Restore MaClaw access</h1><p class=ok>The selected network is connected. The saved device token was rejected by the Hub.</p>"
        "<p>Generate a temporary code in MaClaw GUI. It is used once to retrieve a replacement device token and, when needed, securely transfer this device to the MaClaw that generated the code.</p>"
        "<form method=post action=/save>";
    static const char pairing_page_suffix[] =
        "<input type=hidden name=reuse value=1>"
        "<label>New 6-digit pairing code</label><input name=code inputmode=numeric pattern='[0-9]{6}' maxlength=6 required autofocus>"
        "<button>Pair this device</button></form></body></html>";
    ESP_LOGI(TAG, "setup portal request: %s", req->uri);
    httpd_resp_set_type(req, "text/html; charset=utf-8");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    char csrf_field[64];
    int csrf_length = snprintf(csrf_field, sizeof(csrf_field),
                               "<input type=hidden name=csrf value='%s'>",
                               s_setup_csrf_token);
    if (csrf_length < 0 || csrf_length >= (int)sizeof(csrf_field)) return ESP_FAIL;
    if (device_connectivity_is_pairing_recovery_provisioning()) {
        if (httpd_resp_sendstr_chunk(req, pairing_page) != ESP_OK ||
            httpd_resp_sendstr_chunk(req, csrf_field) != ESP_OK ||
            httpd_resp_sendstr_chunk(req, pairing_page_suffix) != ESP_OK) {
            return ESP_FAIL;
        }
        return httpd_resp_sendstr_chunk(req, NULL);
    }
    if (!s_setup_options_mutex ||
        xSemaphoreTake(s_setup_options_mutex, 0) != pdTRUE) {
        /* The first AP scan can take several seconds.  The portal must remain
         * an HTTP success during that window: captive-network webviews often
         * treat a 503 from the redirect target as a failed portal and do not
         * retry it.  Serve a tiny, self-refreshing document instead; this
         * lets the automatic pop-up stay open until the scan has populated
         * the form. */
        static const char scanning_page[] =
            "<!doctype html><html><head><meta name=viewport content='width=device-width,initial-scale=1'>"
            "<meta http-equiv=refresh content='2'><title>Preparing Wi-Fi setup</title></head>"
            "<body><p>Searching for Wi-Fi networks…</p></body></html>";
        httpd_resp_set_hdr(req, "Retry-After", "2");
        return httpd_resp_send(req, scanning_page, HTTPD_RESP_USE_STRLEN);
    }
    char query[32] = {0};
    bool scan_failed = httpd_req_get_url_query_len(req) < sizeof(query) &&
                       httpd_req_get_url_query_str(req, query, sizeof(query)) == ESP_OK &&
                       !strcmp(query, "scan=failed");
    build_setup_saved_networks_html();
    if (httpd_resp_sendstr_chunk(req, setup_page_prefix) != ESP_OK ||
        httpd_resp_sendstr_chunk(req, csrf_field) != ESP_OK ||
        httpd_resp_sendstr_chunk(req, setup_page_suffix) != ESP_OK ||
        (scan_failed && httpd_resp_sendstr_chunk(req, scan_failed_notice) != ESP_OK) ||
        httpd_resp_sendstr_chunk(req, s_setup_ssid_options) != ESP_OK ||
        httpd_resp_sendstr_chunk(req, setup_page_options_suffix) != ESP_OK ||
        (s_setup_saved_html && s_setup_saved_html[0] &&
         httpd_resp_sendstr_chunk(req, s_setup_saved_html) != ESP_OK) ||
        httpd_resp_sendstr_chunk(req, setup_page_tail) != ESP_OK) {
        xSemaphoreGive(s_setup_options_mutex);
        return ESP_FAIL;
    }
    esp_err_t err = httpd_resp_sendstr_chunk(req, NULL);
    xSemaphoreGive(s_setup_options_mutex);
    return err;
}

static esp_err_t captive_redirect_handler(httpd_req_t *req) {
    if (!setup_portal_http_admission_open()) {
        httpd_resp_set_status(req, "503 Service Unavailable");
        httpd_resp_set_hdr(req, "Connection", "close");
        return httpd_resp_sendstr(req, "Setup portal is stopping.");
    }
    // A 302 is intentionally used here instead of a successful probe body:
    // the OS then identifies this as a captive network and presents its login
    // surface, which follows the redirect to the configuration page.
    // Captive probes arrive in parallel and are retried aggressively. Keep
    // the per-request trace out of the normal serial path so it cannot delay
    // the portal on constrained boards; enable debug logging when diagnosing
    // a particular phone/OS instead.
    ESP_LOGD(TAG, "captive probe: %s", req->uri);
    /* Match esp-wifi-connect's cache-busting redirect. Some captive-network
     * assistants cache the first probe redirect; a unique root URL makes the
     * assistant fetch the configuration document instead of reusing it. */
    char location[64];
    int location_len = snprintf(location, sizeof(location),
                                "http://" SETUP_AP_IP_ADDR "/?_%lld",
                                (long long)esp_timer_get_time());
    if (location_len < 0 || location_len >= (int)sizeof(location)) return ESP_FAIL;
    httpd_resp_set_status(req, "302 Found");
    httpd_resp_set_hdr(req, "Location", location);
    httpd_resp_set_type(req, "text/html; charset=utf-8");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    // Probe clients do not need a persistent HTTP connection. Closing it makes
    // the redirect deterministic for the small captive-portal web views used
    // by Android, iOS and Windows.
    httpd_resp_set_hdr(req, "Connection", "close");
    return httpd_resp_send(req, NULL, 0);
}

static esp_err_t setup_refresh_handler(httpd_req_t *req) {
    if (!setup_portal_http_admission_open()) {
        httpd_resp_set_status(req, "503 Service Unavailable");
        httpd_resp_set_hdr(req, "Connection", "close");
        return httpd_resp_sendstr(req, "Setup portal is stopping.");
    }
    if (setup_portal_rate_limit_exceeded(req, SETUP_PORTAL_RATE_REFRESH)) {
        ESP_LOGW(TAG, "setup refresh request rate limited");
        return ESP_OK;
    }
    // Refresh only on explicit user action.  Scanning on every GET would delay
    // the short captive-check requests that are meant to open this page.
    bool refreshed = refresh_setup_ssid_options();
    httpd_resp_set_status(req, "303 See Other");
    httpd_resp_set_hdr(req, "Location", refreshed ? "/" : "/?scan=failed");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    return httpd_resp_sendstr(req, "Refreshing Wi-Fi networks...");
}

static bool url_decode(const char *src, char *out, size_t cap) {
    size_t used = 0;
    for (; *src; src++) {
        if (used + 1 >= cap) return false;
        if (*src == '+') { out[used++] = ' '; continue; }
        if (*src == '%' && src[1] && src[2]) {
            char hex[] = {src[1], src[2], '\0'};
            char *end = NULL;
            long value = strtol(hex, &end, 16);
            if (!end || *end) return false;
            out[used++] = (char)value;
            src += 2;
            continue;
        }
        out[used++] = *src;
    }
    out[used] = '\0';
    return true;
}

static bool form_value(const char *body, const char *key, char *out, size_t cap) {
    char encoded[URL_CAPACITY + WIFI_VALUE_CAPACITY + 32];
    if (httpd_query_key_value(body, key, encoded, sizeof(encoded)) != ESP_OK) return false;
    return url_decode(encoded, out, cap);
}

/* The form body lived on the httpd task stack and, together with the
 * configuration write chain, overflowed its 6 KB stack mid-save (the
 * reboot swallowed every newly submitted pairing code).  esp_http_server
 * runs all handlers on its single task, so one PSRAM buffer is safe. */
/* Form input can contain Wi-Fi/EAP credentials and pairing input.  Free it
 * only after HTTP has joined; scrub the form body first. */
static void release_setup_portal_scratch(void) {
    mbedtls_platform_zeroize(s_setup_csrf_token, sizeof(s_setup_csrf_token));
    taskENTER_CRITICAL(&s_task_state_lock);
    memset(s_setup_rate_clients, 0, sizeof(s_setup_rate_clients));
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_setup_save_body) {
        mbedtls_platform_zeroize(s_setup_save_body, SETUP_SAVE_BODY_CAPACITY);
        heap_caps_free(s_setup_save_body);
        s_setup_save_body = NULL;
    }
    if (s_setup_ssid_options) {
        heap_caps_free(s_setup_ssid_options);
        s_setup_ssid_options = NULL;
    }
    if (s_setup_ssid_choices) {
        heap_caps_free(s_setup_ssid_choices);
        s_setup_ssid_choices = NULL;
    }
    if (s_setup_saved_html) {
        heap_caps_free(s_setup_saved_html);
        s_setup_saved_html = NULL;
    }
}

/* 生成"已存网络"页面片段：只显示 ssid（永不输出密码），每条带删除按钮，
 * 删除走 POST /delete 写回 NVS。 */
static void build_setup_saved_networks_html(void) {
    if (!s_setup_saved_html) return;
    s_setup_saved_html[0] = '\0';
    configuration_wifi_network_t saved_networks[CONFIGURATION_WIFI_NETWORK_CAPACITY];
    uint8_t saved_count = 0;
    (void)configuration_service_list_wifi_networks(
        saved_networks, CONFIGURATION_WIFI_NETWORK_CAPACITY, &saved_count);
    if (!saved_count) return;
    static const char header[] =
        "<h2>Saved networks</h2><p class=hint>The device connects to the strongest visible saved network automatically.</p>";
    char csrf_field[64];
    int csrf_length = snprintf(csrf_field, sizeof(csrf_field),
                               "<input type=hidden name=csrf value='%s'>",
                               s_setup_csrf_token);
    if (csrf_length < 0 || csrf_length >= (int)sizeof(csrf_field)) return;
    size_t used = strlen(header);
    memcpy(s_setup_saved_html, header, used + 1);
    for (uint8_t i = 0; i < saved_count; ++i) {
        static const char row_prefix[] = "<div class=saved><span>";
        static const char row_middle[] = "</span><form method=post action=/delete><input type=hidden name=ssid value=\"";
        static const char row_suffix[] = "\"><button type=submit>Delete</button></form></div>";
        // ssid 出现两次（显示文本 + 表单值），都按转义后的长度预留空间。
        size_t escaped = setup_html_escaped_length(saved_networks[i].ssid);
        if (used + strlen(row_prefix) + strlen(row_middle) + strlen(csrf_field) +
                strlen(row_suffix) +
                escaped * 2 + 1 >= SETUP_SAVED_HTML_CAPACITY) {
            break; // 空间不足时截断剩余条目，已生成部分仍可正常展示
        }
        memcpy(s_setup_saved_html + used, row_prefix, strlen(row_prefix));
        used += strlen(row_prefix);
        used = append_setup_html_escaped(s_setup_saved_html, used, SETUP_SAVED_HTML_CAPACITY,
                                         saved_networks[i].ssid);
        memcpy(s_setup_saved_html + used, csrf_field, strlen(csrf_field));
        used += strlen(csrf_field);
        memcpy(s_setup_saved_html + used, row_middle, strlen(row_middle));
        used += strlen(row_middle);
        used = append_setup_html_escaped(s_setup_saved_html, used, SETUP_SAVED_HTML_CAPACITY,
                                         saved_networks[i].ssid);
        memcpy(s_setup_saved_html + used, row_suffix, strlen(row_suffix) + 1);
        used += strlen(row_suffix);
    }
}

static esp_err_t setup_save_handler(httpd_req_t *req) {
    if (!setup_portal_http_admission_open()) {
        httpd_resp_set_status(req, "503 Service Unavailable");
        httpd_resp_set_hdr(req, "Connection", "close");
        return httpd_resp_sendstr(req, "Setup portal is stopping.");
    }
    if (setup_portal_rate_limit_exceeded(req, SETUP_PORTAL_RATE_MUTATION)) {
        ESP_LOGW(TAG, "setup save request rate limited");
        return ESP_OK;
    }
    if (!s_setup_save_body) {
        s_setup_save_body = heap_caps_malloc(SETUP_SAVE_BODY_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_setup_save_body) {
        httpd_resp_send_err(req, HTTPD_500_INTERNAL_SERVER_ERROR, "Out of memory");
        return ESP_FAIL;
    }
    setup_portal_form_scope_t form_scope
        __attribute__((cleanup(cleanup_setup_portal_form_scope))) = {
            .body = s_setup_save_body,
        };
    char *body = form_scope.body;
    body[0] = 0;
    setup_portal_save_secret_scope_t secrets
        __attribute__((cleanup(cleanup_setup_portal_save_secret_scope))) = {
            .security = "personal",
            .eap_method = "peap",
            .ttls_phase2 = "mschapv2",
            .ca_mode = "system",
        };
    if (receive_setup_form_body(req, body, SETUP_SAVE_BODY_CAPACITY) != ESP_OK) {
        return ESP_FAIL;
    }
    if (!setup_portal_csrf_valid(body)) {
        ESP_LOGW(TAG, "setup rejected save with missing or stale CSRF token");
        httpd_resp_send_err(req, HTTPD_403_FORBIDDEN, "Reload the setup page and try again.");
        return ESP_FAIL;
    }
    // Recovery preserves the already selected backhaul. On Wi-Fi boards this
    // means the saved station; on Fangtang 4G it means the ML307 connection.
    // The form field remains named "reuse" for wire compatibility.
    bool reuse_network = form_value(body, "reuse", secrets.reuse, sizeof(secrets.reuse)) &&
                         !strcmp(secrets.reuse, "1");
    if (reuse_network) {
        host_copy_runtime_wifi(&secrets.runtime);
        strlcpy(secrets.ssid, secrets.runtime.wifi_ssid, sizeof(secrets.ssid));
        strlcpy(secrets.password, secrets.runtime.wifi_password, sizeof(secrets.password));
        strlcpy(secrets.gateway, secrets.runtime.gateway_url, sizeof(secrets.gateway));
        strlcpy(secrets.security, secrets.runtime.wifi_security, sizeof(secrets.security));
        strlcpy(secrets.eap_method, secrets.runtime.wifi_eap_method, sizeof(secrets.eap_method));
        strlcpy(secrets.identity, secrets.runtime.wifi_identity, sizeof(secrets.identity));
        strlcpy(secrets.username, secrets.runtime.wifi_username, sizeof(secrets.username));
        strlcpy(secrets.ttls_phase2, secrets.runtime.wifi_ttls_phase2, sizeof(secrets.ttls_phase2));
        strlcpy(secrets.ca_mode, secrets.runtime.wifi_ca_mode, sizeof(secrets.ca_mode));
        strlcpy(secrets.server_domain, secrets.runtime.wifi_server_domain, sizeof(secrets.server_domain));
    }
    bool invalid_form = !form_value(body, "code", secrets.code, sizeof(secrets.code));
    if (!reuse_network) {
        invalid_form = invalid_form || !form_value(body, "ssid", secrets.ssid, sizeof(secrets.ssid)) ||
                       !form_value(body, "password", secrets.password, sizeof(secrets.password)) ||
                       !form_value(body, "gateway", secrets.gateway, sizeof(secrets.gateway)) ||
                       !form_value(body, "security", secrets.security, sizeof(secrets.security));
        if (!strcmp(secrets.security, "enterprise")) {
            invalid_form = invalid_form || !form_value(body, "eap_method", secrets.eap_method, sizeof(secrets.eap_method)) ||
                           !form_value(body, "identity", secrets.identity, sizeof(secrets.identity)) ||
                           !form_value(body, "username", secrets.username, sizeof(secrets.username)) ||
                           !form_value(body, "ttls_phase2", secrets.ttls_phase2, sizeof(secrets.ttls_phase2)) ||
                           !form_value(body, "ca_mode", secrets.ca_mode, sizeof(secrets.ca_mode)) ||
                           !form_value(body, "server_domain", secrets.server_domain, sizeof(secrets.server_domain));
        }
    }
    if (invalid_form) {
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "Invalid form: check Wi-Fi and enterprise authentication fields");
        return ESP_FAIL;
    }
    bool selectable = false;
    if (!reuse_network && s_setup_options_mutex &&
        xSemaphoreTake(s_setup_options_mutex, pdMS_TO_TICKS(2000)) == pdTRUE) {
        selectable = setup_ssid_is_selectable(secrets.ssid);
        xSemaphoreGive(s_setup_options_mutex);
    }
    if (!reuse_network && (!is_valid_setup_selected_ssid(secrets.ssid) || !selectable)) {
        ESP_LOGW(TAG, "setup rejected SSID that was not in the current scan list");
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST,
                            "Select a Wi-Fi network from the list, then try again.");
        return ESP_FAIL;
    }
    // Recovery changes only the one-time pairing code. Never erase a persisted
    // device token merely because the portal was opened; the code exists only
    // to retrieve a token after authentication has conclusively failed.
    if (!reuse_network) {
        strlcpy(secrets.provisioning_request.ssid, secrets.ssid,
                sizeof(secrets.provisioning_request.ssid));
        strlcpy(secrets.provisioning_request.password, secrets.password,
                sizeof(secrets.provisioning_request.password));
        strlcpy(secrets.provisioning_request.gateway, secrets.gateway,
                sizeof(secrets.provisioning_request.gateway));
        strlcpy(secrets.provisioning_request.code, secrets.code,
                sizeof(secrets.provisioning_request.code));
        strlcpy(secrets.provisioning_request.security, secrets.security,
                sizeof(secrets.provisioning_request.security));
        strlcpy(secrets.provisioning_request.eap_method, secrets.eap_method,
                sizeof(secrets.provisioning_request.eap_method));
        strlcpy(secrets.provisioning_request.identity, secrets.identity,
                sizeof(secrets.provisioning_request.identity));
        strlcpy(secrets.provisioning_request.username, secrets.username,
                sizeof(secrets.provisioning_request.username));
        strlcpy(secrets.provisioning_request.ttls_phase2, secrets.ttls_phase2,
                sizeof(secrets.provisioning_request.ttls_phase2));
        strlcpy(secrets.provisioning_request.ca_mode, secrets.ca_mode,
                sizeof(secrets.provisioning_request.ca_mode));
        strlcpy(secrets.provisioning_request.server_domain, secrets.server_domain,
                sizeof(secrets.provisioning_request.server_domain));
    }
    /* Configuration owns both durable provisioning routes. Reuse-network
     * recovery changes only the one-time pair code; a new network stages a
     * candidate snapshot. Neither route needs main.c to mirror/persist a
     * configuration field. */
    esp_err_t save_err = reuse_network
                             ? device_status_to_platform_error(
                                   configuration_service_set_pairing_code(secrets.code))
                             : device_status_to_platform_error(
                                   configuration_service_stage_provisioning(
                                       &secrets.provisioning_request));
    if (save_err != ESP_OK) {
        char reason[160];
        if (!secrets.ssid[0]) snprintf(reason, sizeof(reason), "Wi-Fi name is required");
        else if (strlen(secrets.ssid) > WIFI_SSID_MAX_LEN) snprintf(reason, sizeof(reason), "Wi-Fi name is too long (max 32 bytes)");
        else if (strlen(secrets.password) >= WIFI_VALUE_CAPACITY) snprintf(reason, sizeof(reason), "Wi-Fi password is too long (max 64 bytes)");
        else if (!strcmp(secrets.security, "enterprise") && !secrets.username[0]) snprintf(reason, sizeof(reason), "Enterprise Wi-Fi username is required");
        else if (!is_valid_choice(secrets.security, "personal", "enterprise", NULL)) snprintf(reason, sizeof(reason), "Unsupported Wi-Fi security mode");
        else if (!is_valid_gateway_url(secrets.gateway)) snprintf(reason, sizeof(reason), "Hub URL must start with https:// and contain a valid host");
        else if (!is_six_digit_pair_code(secrets.code)) snprintf(reason, sizeof(reason), "Pairing code must be exactly 6 digits");
        else snprintf(reason, sizeof(reason), "Could not save configuration: %s", esp_err_to_name(save_err));
        ESP_LOGW(TAG, "setup rejected: %s", reason);
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, reason);
        return ESP_FAIL;
    }
    // Do not reset from the HTTP server task.  esp_http_server sends responses
    // asynchronously, so a reset here can race its final socket write and, on
    // this board, leave the setup QR frame on screen indefinitely.  Publish
    // the gated restart coordinator before claiming that a restart will occur:
    // a task/registry allocation failure cannot roll back the already durable
    // credentials, but it must not return a false "restarting" success while
    // the old portal remains live.  The worker itself waits for the response
    // flush and performs the terminal portal cleanup before resetting.
    if (!schedule_setup_restart()) {
        /* Configuration has committed but there is no registered owner left
         * that can safely perform the terminal cleanup/reset.  Keep the HTTP
         * server process alive solely long enough to return this request's
         * unambiguous error, but close admission before doing so.  A browser,
         * captive probe or delayed POST must not continue mutating a portal
         * generation whose persisted credentials no longer describe its
         * runtime state.  Do not call httpd_stop() from its own handler: that
         * would reintroduce the response-flush/self-join race the coordinator
         * exists to avoid.  The explicit recovery action is a manual reset.
         */
        set_setup_portal_http_admission(false);
        ESP_LOGE(TAG, "setup saved but restart coordinator could not start");
        httpd_resp_set_status(req, "500 Internal Server Error");
        httpd_resp_set_hdr(req, "Connection", "close");
        return httpd_resp_sendstr(
            req, "Configuration was saved, but automatic restart is unavailable. "
                 "Please restart the device manually.");
    }
    return httpd_resp_sendstr(req,
                              "Saved. The device is restarting and will connect to MaClaw.");
}

/* 删除已存热点：按 ssid 从多热点列表移除并写回 NVS。主凭据若正是被删的
 * 个人热点，服务侧会一并清除，避免重启后单凭据回退把它又连回去。 */
static esp_err_t setup_delete_handler(httpd_req_t *req) {
    if (!setup_portal_http_admission_open()) {
        httpd_resp_set_status(req, "503 Service Unavailable");
        httpd_resp_set_hdr(req, "Connection", "close");
        return httpd_resp_sendstr(req, "Setup portal is stopping.");
    }
    if (setup_portal_rate_limit_exceeded(req, SETUP_PORTAL_RATE_MUTATION)) {
        ESP_LOGW(TAG, "setup delete request rate limited");
        return ESP_OK;
    }
    if (!s_setup_save_body) {
        s_setup_save_body = heap_caps_malloc(SETUP_SAVE_BODY_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_setup_save_body) {
        httpd_resp_send_err(req, HTTPD_500_INTERNAL_SERVER_ERROR, "Out of memory");
        return ESP_FAIL;
    }
    setup_portal_form_scope_t form_scope
        __attribute__((cleanup(cleanup_setup_portal_form_scope))) = {
            .body = s_setup_save_body,
        };
    char *body = form_scope.body;
    if (receive_setup_form_body(req, body, SETUP_SAVE_BODY_CAPACITY) != ESP_OK) {
        return ESP_FAIL;
    }
    if (!setup_portal_csrf_valid(body)) {
        ESP_LOGW(TAG, "setup rejected delete with missing or stale CSRF token");
        httpd_resp_send_err(req, HTTPD_403_FORBIDDEN, "Reload the setup page and try again.");
        return ESP_FAIL;
    }
    setup_portal_delete_secret_scope_t secrets
        __attribute__((cleanup(cleanup_setup_portal_delete_secret_scope))) = {0};
    if (!form_value(body, "ssid", secrets.ssid, sizeof(secrets.ssid)) ||
        !secrets.ssid[0]) {
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "Missing network name");
        return ESP_FAIL;
    }
    esp_err_t err = device_status_to_platform_error(
        configuration_service_delete_wifi_network(secrets.ssid));
    if (err == ESP_OK) {
        ESP_LOGI(TAG, "saved Wi-Fi deleted via portal: ssid_len=%u",
                 (unsigned)strlen(secrets.ssid));
        // 同步运行时镜像；刷新失败不影响已落库的删除结果。
        if (s_host.sync_runtime_after_network_delete) {
            s_host.sync_runtime_after_network_delete(secrets.ssid);
        }
    } else if (err != ESP_ERR_NOT_FOUND) {
        ESP_LOGW(TAG, "cannot delete saved Wi-Fi: %s", esp_err_to_name(err));
        httpd_resp_send_err(req, HTTPD_500_INTERNAL_SERVER_ERROR, "Could not delete the saved network");
        return ESP_FAIL;
    }
    // 删除成功（或本就不存在）都回门户首页刷新列表。
    httpd_resp_set_status(req, "303 See Other");
    httpd_resp_set_hdr(req, "Location", "/");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    return httpd_resp_sendstr(req, "Deleted.");
}

/* ESP-IDF HTTP server owns and joins its own worker/task set inside
 * httpd_stop().  The API has no caller-supplied bounded timeout, so this
 * narrow lifecycle slice deliberately owns only admission + handle teardown:
 * callers must invoke it before changing AP/DHCP/DNS state, but must not claim
 * that Wi-Fi/netif/event-loop deinitialization is now supported. */
static esp_err_t stop_setup_portal_http_server(void) {
    set_setup_portal_http_admission(false);
    httpd_handle_t server = s_setup_server;
    if (!server) return ESP_OK;
    s_setup_server = NULL;
    esp_err_t err = httpd_stop(server);
    if (err != ESP_OK) {
        /* A non-null handle is the only reliable indication that the server
         * remains live.  Restore it on failure so a later recovery/retry does
         * not accidentally create a second listener on the same port. */
        s_setup_server = server;
        ESP_LOGW(TAG, "cannot stop setup HTTP server: %s", esp_err_to_name(err));
    } else {
        ESP_LOGI(TAG, "setup HTTP server stopped");
    }
    return err;
}

/*
 * Provisioning shutdown has a strict, fail-closed dependency order:
 * admission -> HTTP -> DNS -> logical session -> credential scratch/lease.
 *
 * HTTP owns the portal handlers and DNS owns UDP/53.  If either join fails we
 * retain the remaining resources and keep the logical session active, because
 * clearing it would let other workers resume while an old credential handler
 * or captive responder can still run.  AP/STA, DHCP, netif, event-loop and
 * Wi-Fi driver lifetime are deliberately outside this narrow transaction.
 */
static esp_err_t stop_setup_portal_transaction_locked(uint32_t timeout_ms,
                                                      bool restore_wake_word) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    /* Also close TTL admission here: this locked helper is used by startup
     * rollback before a TTL task is necessarily published, and must never
     * leave a later portal generation inheriting an armed expiry policy. */
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_ttl_admission_open = false;
    s_setup_ttl_stop_requested = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    esp_err_t http_stop_err = stop_setup_portal_http_server();
    if (http_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "setup HTTP server did not stop: %s",
                 esp_err_to_name(http_stop_err));
        return http_stop_err;
    }
    /* `httpd_stop()` exposes no deadline in ESP-IDF.  It is the one documented
     * uncontrollable boundary of this transaction; the DNS join must consume
     * only the residual caller budget after it returns. */
    const uint32_t dns_timeout_ms = remaining_timeout_ms(deadline_us);
    if (dns_timeout_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t dns_stop_err = stop_captive_dns_task(dns_timeout_ms);
    if (dns_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "captive DNS did not stop: %s", esp_err_to_name(dns_stop_err));
        return dns_stop_err;
    }
    device_connectivity_end_provisioning();
    /* HTTP has joined and DNS has released UDP/53. No remaining worker may
     * dereference the session's form/scratch buffers. */
    release_setup_portal_scratch();
    if (s_setup_power_lease != DEVICE_POWER_LEASE_INVALID) {
        device_power_lease_release(s_setup_power_lease);
        s_setup_power_lease = DEVICE_POWER_LEASE_INVALID;
    }
    if (restore_wake_word) {
        if (s_host.wake_word_start) {
            device_status_t wake_err = s_host.wake_word_start();
            if (wake_err != DEVICE_STATUS_OK && wake_err != DEVICE_STATUS_BUSY) {
                ESP_LOGW(TAG, "cannot restore offline wake after setup transaction: status=%d",
                         (int)wake_err);
            }
        }
    }
    return ESP_OK;
}

/* Portal start and stop change the same generation's HTTP/DNS handles,
 * provisioning bit, scratch buffers and power lease.  Serialize that composite
 * ownership rather than relying on individual handles becoming non-null late
 * in startup.  `httpd_stop()` is still an ESP-IDF boundary without a caller
 * timeout; the lock merely makes its state transition linearizable with portal
 * starts. */
static esp_err_t stop_setup_portal_transaction(uint32_t timeout_ms,
                                               bool restore_wake_word) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    /* Quiesce the expiry worker before taking the portal mutex.  It may itself
     * be waiting to acquire that mutex after a real timeout, so joining it
     * while the caller holds the lock would deadlock.  The expiry worker owns
     * its own transaction and therefore skips the self-join path. */
    TaskHandle_t ttl_task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    ttl_task = s_setup_ttl_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (ttl_task && xTaskGetCurrentTaskHandle() != ttl_task) {
        const uint32_t ttl_timeout_ms = remaining_timeout_ms(deadline_us);
        if (ttl_timeout_ms == 0) return ESP_ERR_TIMEOUT;
        esp_err_t ttl_stop_err = stop_setup_portal_ttl_task(ttl_timeout_ms);
        if (ttl_stop_err != ESP_OK) return ttl_stop_err;
    }
    if (!s_setup_portal_mutex ||
        xSemaphoreTake(s_setup_portal_mutex,
                       pdMS_TO_TICKS(remaining_timeout_ms(deadline_us))) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    const uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    esp_err_t err = remaining_ms == 0
                        ? ESP_ERR_TIMEOUT
                        : stop_setup_portal_transaction_locked(remaining_ms, restore_wake_word);
    xSemaphoreGive(s_setup_portal_mutex);
    if (err == ESP_OK) {
        foreground_coordinator_observe_release(FOREGROUND_OWNER_SETUP);
    }
    return err;
}

static void recover_after_setup_portal_start_failure(
    bool wake_was_stopped, provisioning_radio_token_t radio_token) {
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
    taskENTER_CRITICAL(&s_task_state_lock);
    const bool system_sleep_preparing = s_system_sleep_preparing;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (system_sleep_preparing) {
        ESP_LOGW(TAG, "setup portal start rejected: system sleep prepare is active");
        return;
    }
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
    taskENTER_CRITICAL(&s_task_state_lock);
    const bool ttl_retiring = s_setup_ttl_task != NULL || s_setup_ttl_starting ||
                              s_setup_ttl_retiring ||
                              s_setup_ttl_registry_retirement_failed;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (ttl_retiring) {
        /* This start path already owns the portal mutex; joining an expiry
         * worker that is waiting for the same mutex would deadlock.  Refuse a
         * new generation until its registered owner has fully drained. */
        ESP_LOGW(TAG, "setup portal start rejected: previous expiry worker is still draining");
        if (s_host.show_text) s_host.show_text("设置失败", "配置页面正在关闭，请稍候再试");
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
    if (s_dns_task || s_dns_retiring || s_dns_registry_retirement_failed) {
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
    s_setup_ttl_admission_open = true;
    s_setup_ttl_stop_requested = false;
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
    audio_arbitration_wake_word_pause(true);
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
        recover_after_setup_portal_start_failure(wake_was_stopped, 0);
        ESP_LOGE(TAG, "cannot read SoftAP MAC for setup portal");
        if (s_host.show_text) {
            s_host.show_text("Setup failed", "Network identity unavailable; restart device");
        }
        return;
    }
    char ap_ssid[PROVISIONING_AP_SSID_CAPACITY];
    snprintf(ap_ssid, sizeof(ap_ssid), "MACLAW-SETUP-%02X%02X", mac[4], mac[5]);
    setup_portal_ap_secret_scope_t ap_secret
        __attribute__((cleanup(cleanup_setup_portal_ap_secret_scope))) = {0};
    if (!generate_setup_ap_passphrase(ap_secret.passphrase) ||
        !generate_setup_csrf_token(s_setup_csrf_token)) {
        ESP_LOGE(TAG, "setup portal entropy unavailable; keeping portal closed");
        mbedtls_platform_zeroize(s_setup_csrf_token, sizeof(s_setup_csrf_token));
        recover_after_setup_portal_start_failure(wake_was_stopped, 0);
        return;
    }
    if (!s_host.init_network || s_host.init_network() != DEVICE_STATUS_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, 0);
        ESP_LOGE(TAG, "cannot initialize network core for setup portal");
        if (s_host.show_text) {
            s_host.show_text("Setup failed", "Network service unavailable; restart device");
        }
        return;
    }
    provisioning_radio_token_t radio_token = 0;
    if (!s_host.capture_radio ||
        s_host.capture_radio(&radio_token) != DEVICE_STATUS_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, 0);
        ESP_LOGE(TAG, "cannot inspect Wi-Fi state before setup portal");
        if (s_host.show_text) {
            s_host.show_text("Setup failed", "Network state unavailable; restart device");
        }
        return;
    }
    if (s_host.ensure_ap_netif) s_host.ensure_ap_netif();
    if (!s_host.ap_netif_ready || !s_host.ap_netif_ready()) {
        recover_after_setup_portal_start_failure(wake_was_stopped, radio_token);
        ESP_LOGE(TAG, "cannot create setup AP netif");
        if (s_host.show_text) {
            s_host.show_text("Setup failed", "Network interface unavailable; restart device");
        }
        return;
    }
    if (!s_host.configure_ap_dhcp ||
        s_host.configure_ap_dhcp() != DEVICE_STATUS_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, radio_token);
        ESP_LOGE(TAG, "cannot configure setup AP/DHCP transaction");
        if (s_host.show_text) {
            s_host.show_text("Setup failed", "Hotspot network unavailable; restart device");
        }
        return;
    }
    /* APSTA is needed only for pairing recovery, where the device itself may
     * keep an authenticated station backhaul.  The AP clients must never gain
     * that backhaul: force the composition-root network policy to prove that
     * forwarding/NAPT is disabled before DNS/HTTP exposes credential forms. */
    if (!s_host.verify_ap_client_isolation ||
        s_host.verify_ap_client_isolation() != DEVICE_STATUS_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, radio_token);
        ESP_LOGE(TAG, "cannot verify setup AP client isolation");
        if (s_host.show_text) {
            s_host.show_text("设置失败", "配置热点网络隔离不可用，请重启设备");
        }
        return;
    }
    if (!start_captive_dns()) {
        recover_after_setup_portal_start_failure(wake_was_stopped, radio_token);
        ESP_LOGE(TAG, "cannot start captive DNS before setup hotspot");
        if (s_host.show_text) s_host.show_text("设置失败", "配网 DNS 服务启动失败，请重启后再试");
        return;
    }
    bool cellular_pairing_ap_only = device_connectivity_is_active_cellular() &&
                                    device_connectivity_is_pairing_recovery_provisioning();
    if (!cellular_pairing_ap_only) {
        if (s_host.ensure_sta_netif) s_host.ensure_sta_netif();
        if (!s_host.sta_netif_ready || !s_host.sta_netif_ready()) {
            recover_after_setup_portal_start_failure(wake_was_stopped, radio_token);
            ESP_LOGE(TAG, "cannot create setup station netif");
            if (s_host.show_text) {
                s_host.show_text("Setup failed", "Network interface unavailable; restart device");
            }
            return;
        }
    }
    bool keep_wifi_station = device_connectivity_is_pairing_recovery_provisioning() &&
                             !device_connectivity_is_active_cellular();
    if (s_host.note_radio_changed) s_host.note_radio_changed(radio_token);
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
        recover_after_setup_portal_start_failure(wake_was_stopped, radio_token);
        ESP_LOGE(TAG, "cannot enter setup Wi-Fi mode");
        if (s_host.show_text) s_host.show_text("设置失败", "请在网页重新设置");
        return;
    }
    if (!s_host.wifi_configure_protected_ap ||
        s_host.wifi_configure_protected_ap(ap_ssid, ap_secret.passphrase) != DEVICE_STATUS_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, radio_token);
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
            recover_after_setup_portal_start_failure(wake_was_stopped, radio_token);
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
        recover_after_setup_portal_start_failure(wake_was_stopped, radio_token);
        ESP_LOGE(TAG, "setup hotspot did not enter AP mode");
        if (s_host.show_text) s_host.show_text("设置热点失败", "请重启后再试");
        return;
    }
    httpd_config_t server_config = HTTPD_DEFAULT_CONFIG();
    server_config.stack_size = 6144;
    server_config.max_uri_handlers = 24;
    server_config.max_open_sockets = SETUP_PORTAL_HTTP_MAX_OPEN_SOCKETS;
    server_config.backlog_conn = SETUP_PORTAL_HTTP_BACKLOG_CONNECTIONS;
    server_config.recv_wait_timeout = SETUP_PORTAL_HTTP_RECV_WAIT_TIMEOUT_SECONDS;
    server_config.send_wait_timeout = SETUP_PORTAL_HTTP_SEND_WAIT_TIMEOUT_SECONDS;
    server_config.lru_purge_enable = true;
    server_config.uri_match_fn = httpd_uri_match_wildcard;
    esp_err_t portal_err = httpd_start(&s_setup_server, &server_config);
    if (portal_err != ESP_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, radio_token);
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
        recover_after_setup_portal_start_failure(wake_was_stopped, radio_token);
        if (s_host.show_text) s_host.show_text("设置失败", "配置网页路由启动失败");
        return;
    }
    set_setup_portal_http_admission(true);
    if (!start_setup_portal_ttl_task()) {
        recover_after_setup_portal_start_failure(wake_was_stopped, radio_token);
        ESP_LOGE(TAG, "cannot start setup portal expiry worker");
        if (s_host.show_text) s_host.show_text("设置失败", "配置页面生命周期启动失败");
        return;
    }
    if (!cellular_pairing_ap_only) refresh_setup_ssid_options();
    if (device_connectivity_is_pairing_recovery_provisioning()) {
        if (s_host.show_text) {
            char setup_hint[96];
            snprintf(setup_hint, sizeof(setup_hint), "%s  密码: %s",
                     ap_ssid, ap_secret.passphrase);
            s_host.show_text("设备配对设置", setup_hint);
        }
    } else if (s_host.show_qr) {
        s_host.show_qr(ap_ssid, ap_secret.passphrase);
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
    s_setup_ttl_start_gate = xSemaphoreCreateBinary();
    s_setup_ttl_stopped = xSemaphoreCreateBinary();
    s_dns_start_gate = xSemaphoreCreateBinary();
    s_dns_stopped = xSemaphoreCreateBinary();
    s_dns_ready = xSemaphoreCreateBinary();
    if (!s_setup_portal_mutex || !s_setup_options_mutex ||
        !s_setup_restart_start_gate || !s_setup_restart_stopped ||
        !s_setup_ttl_start_gate || !s_setup_ttl_stopped ||
        !s_dns_start_gate || !s_dns_stopped || !s_dns_ready) {
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_restart_admission_open = true;
    s_setup_ttl_admission_open = false;
    s_dns_admission_open = true;
    s_setup_restart_retiring = false;
    s_setup_restart_exit_status = ESP_OK;
    s_setup_restart_registry_retirement_failed = false;
    s_setup_ttl_retiring = false;
    s_setup_ttl_exit_status = ESP_OK;
    s_setup_ttl_registry_retirement_failed = false;
    s_dns_retiring = false;
    s_dns_exit_status = ESP_OK;
    s_dns_registry_retirement_failed = false;
    s_system_sleep_preparing = false;
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

/* Header parsing is bounded by ESP HTTP Server's per-socket receive timeout.
 * Once a POST has reached a handler, enforce a separate monotonic total-body
 * deadline: a byte trickle must not extend the portal's one HTTP worker
 * indefinitely. The caller provides the only credential-bearing buffer;
 * partial input is never parsed or persisted. */
static esp_err_t receive_setup_form_body(httpd_req_t *req, char *body,
                                         size_t body_capacity) {
    if (!req || !body || body_capacity < 2u || req->content_len <= 0 ||
        (size_t)req->content_len >= body_capacity) {
        if (req) {
            httpd_resp_set_hdr(req, "Connection", "close");
            httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "Form data is too large");
        }
        return ESP_ERR_INVALID_SIZE;
    }
    const int64_t deadline_us = esp_timer_get_time() +
        (int64_t)SETUP_PORTAL_FORM_RECEIVE_DEADLINE_MS * 1000;
    int received = 0;
    while (received < req->content_len) {
        if (esp_timer_get_time() >= deadline_us) {
            ESP_LOGW(TAG, "setup form receive deadline expired");
            httpd_resp_set_hdr(req, "Connection", "close");
            httpd_resp_send_err(req, HTTPD_408_REQ_TIMEOUT,
                                "Form receive deadline expired");
            return ESP_ERR_TIMEOUT;
        }
        int n = httpd_req_recv(req, body + received, req->content_len - received);
        if (n <= 0) {
            ESP_LOGW(TAG, "setup form receive failed before complete body: %d", n);
            httpd_resp_set_hdr(req, "Connection", "close");
            httpd_resp_send_err(req, HTTPD_408_REQ_TIMEOUT,
                                "Could not receive the complete form");
            return ESP_ERR_TIMEOUT;
        }
        received += n;
        if (esp_timer_get_time() > deadline_us) {
            ESP_LOGW(TAG, "setup form receive completed after deadline");
            httpd_resp_set_hdr(req, "Connection", "close");
            httpd_resp_send_err(req, HTTPD_408_REQ_TIMEOUT,
                                "Form receive deadline expired");
            return ESP_ERR_TIMEOUT;
        }
    }
    body[received] = '\0';
    return ESP_OK;
}

device_status_t provisioning_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0 || !s_setup_portal_mutex) {
        return timeout_ms == 0 ? DEVICE_STATUS_INVALID_ARGUMENT
                               : DEVICE_STATUS_UNAVAILABLE;
    }
    /* Share the composite portal mutex with start/stop so the marker and the
     * no-live-generation observation are linearizable.  We deliberately do
     * not call httpd_stop()/DNS stop here: abort must restore the same portal
     * generation, and a post-save reset is terminal rather than replayable. */
    if (xSemaphoreTake(s_setup_portal_mutex, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    bool busy = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_system_sleep_preparing) {
        busy = true;
    } else if (device_connectivity_is_provisioning_active() || s_setup_server ||
               s_dns_task || s_dns_starting || s_dns_retiring ||
               s_dns_registry_retirement_failed || s_setup_ttl_task ||
               s_setup_ttl_starting || s_setup_ttl_retiring ||
               s_setup_ttl_registry_retirement_failed || s_setup_restart_task ||
               s_setup_restart_starting || s_setup_restart_retiring ||
               s_setup_restart_registry_retirement_failed) {
        busy = true;
    } else {
        s_system_sleep_preparing = true;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    xSemaphoreGive(s_setup_portal_mutex);
    return busy ? DEVICE_STATUS_BUSY : DEVICE_STATUS_OK;
}

void provisioning_service_abort_system_sleep_prepare(void) {
    taskENTER_CRITICAL(&s_task_state_lock);
    s_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
}

bool provisioning_service_has_live_resources(void) {
    return device_connectivity_is_provisioning_active() ||
           s_setup_server != NULL || s_dns_task != NULL || s_dns_starting ||
           s_dns_retiring || s_dns_registry_retirement_failed ||
           s_setup_ttl_task != NULL || s_setup_ttl_starting || s_setup_ttl_retiring ||
           s_setup_ttl_registry_retirement_failed || s_setup_restart_task != NULL ||
           s_setup_restart_starting || s_setup_restart_retiring ||
           s_setup_restart_registry_retirement_failed;
}
