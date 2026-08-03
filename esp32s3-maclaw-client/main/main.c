#include <stdio.h>
#include <errno.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

#include "cJSON.h"
#include "esp_crt_bundle.h"
#include "esp_event.h"
#include "esp_eap_client.h"
#include "esp_http_client.h"
#include "esp_http_server.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_mac.h"
#include "mbedtls/base64.h"
#include "esp_netif.h"
#include "esp_netif_sntp.h"
#include "esp_partition.h"
#include "esp_system.h"
#include "esp_spiffs.h"
#include "esp_timer.h"
#include "esp_wifi.h"
#include "freertos/FreeRTOS.h"
#include "freertos/event_groups.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "lwip/inet.h"
#include "lwip/sockets.h"
#include "nvs.h"
#include "nvs_flash.h"
#include "psa/crypto.h"
#include "qrcode.h"

#include "board_port.h"

#define WIFI_CONNECTED_BIT BIT0
#define WIFI_CONNECT_TIMEOUT_MS 20000
#define RESPONSE_CAPACITY 16384
#define HANDSHAKE_RESPONSE_CAPACITY 24576
#define URL_CAPACITY 256
#define WIFI_VALUE_CAPACITY 65
#define WIFI_SSID_MAX_LEN 32
#define WIFI_ENTERPRISE_VALUE_CAPACITY 128
#define WIFI_EAP_MODE_CAPACITY 12
#define PAIR_CODE_CAPACITY 7
#define GATEWAY_RETRY_INITIAL_MS 2000
#define GATEWAY_RETRY_MAX_MS 60000
#define SETUP_AP_IP_ADDR "192.168.4.1"
#define DNS_PORT 53
#define DNS_PACKET_CAPACITY 512
#define DHCPS_OFFER_DNS 0x02
#define DYNAMIC_GLYPH_BYTES 72
#define DYNAMIC_GLYPH_MAX_PER_MESSAGE 24
#define MEETING_WAV_PATH "/storage/meeting.wav"
#define MEETING_SAMPLE_RATE 16000
#define MEETING_DEFAULT_CHUNK_SIZE (1U << 20)
#define MEETING_MIN_CHUNK_SIZE (64U << 10)
#define MEETING_MAX_CHUNK_SIZE (8U << 20)
#define MEETING_IO_BUFFER_SIZE 16384
#define MEETING_RESPONSE_CAPACITY 2048
#define MEETING_BASE_PATH_CAPACITY 96
#define MEETING_RECORDING_ID_CAPACITY 96

static const char *TAG = "maclaw_client";
static EventGroupHandle_t s_wifi_events;
static int64_t s_cursor;
static char s_gateway_token[96];
static char s_wifi_ssid[WIFI_VALUE_CAPACITY];
static char s_wifi_password[WIFI_VALUE_CAPACITY];
static char s_wifi_security[WIFI_EAP_MODE_CAPACITY] = "personal";
static char s_wifi_eap_method[WIFI_EAP_MODE_CAPACITY] = "peap";
static char s_wifi_identity[WIFI_ENTERPRISE_VALUE_CAPACITY];
static char s_wifi_username[WIFI_ENTERPRISE_VALUE_CAPACITY];
static char s_wifi_ttls_phase2[WIFI_EAP_MODE_CAPACITY] = "mschapv2";
static char s_wifi_ca_mode[WIFI_EAP_MODE_CAPACITY] = "system";
static char s_wifi_server_domain[WIFI_ENTERPRISE_VALUE_CAPACITY];
static char s_gateway_url[URL_CAPACITY];
static char s_pair_code[PAIR_CODE_CAPACITY];
static httpd_handle_t s_setup_server;
static bool s_network_initialized;
static bool s_ap_netif_created;
static bool s_sta_netif_created;
static bool s_wifi_started;
static bool s_setup_portal_active;
static esp_netif_t *s_setup_ap_netif;
static TaskHandle_t s_dns_task;
static TaskHandle_t s_gateway_task;
static TaskHandle_t s_interaction_task;
static TaskHandle_t s_meeting_task;
static bool s_meeting_task_running;
static bool s_pairing_recovery_portal;
static TaskHandle_t s_ambient_task;
static TaskHandle_t s_gateway_poll_task;
static SemaphoreHandle_t s_http_mutex;
static SemaphoreHandle_t s_interaction_lock;
static portMUX_TYPE s_task_state_lock = portMUX_INITIALIZER_UNLOCKED;
static char s_weather_summary[24];
static char s_weather_location[24];
static int s_weather_temperature_c;
static int64_t s_weather_expires_at_ms;
static bool s_weather_valid;
static void on_wake_word(void *arg);
// Once SNTP supplies an epoch, the display advances from ESP32's monotonic
// microsecond counter. This keeps the visible seconds moving independently of
// network timing and avoids a network request or SNTP poll per screen update.
static time_t s_display_clock_epoch;
static int64_t s_display_clock_anchor_us;
static bool s_display_clock_valid;

typedef enum {
    MEETING_IDLE = 0,
    MEETING_STARTING,
    MEETING_RECORDING,
    MEETING_PAUSED,
    MEETING_FINALIZING,
    MEETING_UPLOADING,
    MEETING_PROCESSING,
    MEETING_DONE,
    MEETING_ERROR,
} meeting_state_t;

static volatile meeting_state_t s_meeting_state = MEETING_IDLE;
static bool s_storage_mounted;
static bool s_meeting_available;
static size_t s_meeting_chunk_size = MEETING_DEFAULT_CHUNK_SIZE;
static char s_meeting_base_path[MEETING_BASE_PATH_CAPACITY] = "/api/device-gateway/v1/meeting-recordings";
static char s_meeting_process_mode[12] = "keep";
static bool s_meeting_pending;
static int32_t s_meeting_next_chunk;
static int32_t s_meeting_phase;
static char s_meeting_recording_id[MEETING_RECORDING_ID_CAPACITY];
static volatile uint32_t s_meeting_elapsed_seconds;

static void wifi_event(void *arg, esp_event_base_t base, int32_t id, void *data);

typedef struct {
    char *data;
    size_t capacity;
    size_t len;
    int status;
    bool truncated;
} http_response_t;

static bool gateway_auth_failed(const http_response_t *response, esp_err_t err);
static void save_ambient_weather(void);
static void load_ambient_weather(void);
static esp_err_t poll_reply(void);
static bool json_number(cJSON *root, const char *key, int *value);
static int apply_glyphs_json(cJSON *glyphs);
static bool start_meeting_task(bool resume_only);

static bool meeting_is_active(void) {
    meeting_state_t state = s_meeting_state;
    return state != MEETING_IDLE && state != MEETING_DONE && state != MEETING_ERROR;
}

static void meeting_set_state(meeting_state_t state) {
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_state = state;
    taskEXIT_CRITICAL(&s_task_state_lock);
}
static void finish_interaction_task(void) {
    taskENTER_CRITICAL(&s_task_state_lock);
    s_interaction_task = NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_interaction_lock) xSemaphoreGive(s_interaction_lock);
    vTaskDeleteWithCaps(NULL);
}

static void log_heap_snapshot(const char *stage) {
    size_t internal_free = heap_caps_get_free_size(MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    size_t internal_largest = heap_caps_get_largest_free_block(MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    size_t psram_free = heap_caps_get_free_size(MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    size_t psram_largest = heap_caps_get_largest_free_block(MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    ESP_LOGI(TAG, "heap[%s] internal=%u/%u psram=%u/%u", stage ? stage : "?",
             (unsigned)internal_free, (unsigned)internal_largest,
             (unsigned)psram_free, (unsigned)psram_largest);
}

static void pet(const char *state) {
    board_port_set_pet_state(state);
}

static esp_err_t on_http_event(esp_http_client_event_t *event) {
    http_response_t *out = event->user_data;
    if (event->event_id == HTTP_EVENT_ON_DATA && out && out->data && event->data_len > 0) {
        if (out->capacity == 0 || out->len >= out->capacity - 1) {
            out->truncated = true;
            return ESP_OK;
        }
        size_t available = out->capacity - out->len - 1;
        size_t copy_len = event->data_len < available ? event->data_len : available;
        memcpy(out->data + out->len, event->data, copy_len);
        out->len += copy_len;
        out->data[out->len] = '\0';
        if (copy_len < (size_t)event->data_len) out->truncated = true;
    }
    return ESP_OK;
}

static esp_err_t request_with_capacity(const char *method, const char *path, const char *content_type,
                                       const char *body, int body_len, size_t response_capacity,
                                       http_response_t *out) {
    if (!out) return ESP_ERR_INVALID_ARG;
    memset(out, 0, sizeof(*out));
    if (!method || !path || response_capacity < 2) return ESP_ERR_INVALID_ARG;
    char url[URL_CAPACITY];
    int n = strncmp(path, "http://", 7) == 0 || strncmp(path, "https://", 8) == 0
                ? snprintf(url, sizeof(url), "%s", path)
                : snprintf(url, sizeof(url), "%s%s", s_gateway_url, path);
    if (n < 0 || n >= sizeof(url)) return ESP_ERR_INVALID_SIZE;
    if (!s_http_mutex || xSemaphoreTake(s_http_mutex, pdMS_TO_TICKS(35000)) != pdTRUE) {
        ESP_LOGW(TAG, "HTTP request lock timeout: %s %s", method, path);
        return ESP_ERR_TIMEOUT;
    }
    // Prefer PSRAM for every HTTP body. Request buffers must not consume the
    // small internal heap reserved for the TLS handshake and Wi-Fi stacks.
    out->data = heap_caps_malloc(response_capacity, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!out->data) out->data = heap_caps_malloc(response_capacity, MALLOC_CAP_8BIT);
    if (!out->data) {
        ESP_LOGE(TAG, "HTTP buffer allocation failed: need=%u path=%s", (unsigned)response_capacity, path);
        log_heap_snapshot("http-buffer-fail");
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_NO_MEM;
    }
    out->capacity = response_capacity;
    out->data[0] = '\0';
    esp_http_client_config_t config = {
        .url = url, .event_handler = on_http_event, .user_data = out,
        .timeout_ms = 30000, .crt_bundle_attach = esp_crt_bundle_attach,
        // The public Hub is fronted by nginx and answers with HTTP/1.1
        // keep-alive. Do not wait for the peer to close the TLS socket to
        // decide that a complete JSON response has ended.
        .keep_alive_enable = true,
    };
    esp_http_client_handle_t client = esp_http_client_init(&config);
    if (!client) {
        ESP_LOGE(TAG, "HTTP client allocation failed: path=%s", path);
        log_heap_snapshot("http-client-fail");
        free(out->data);
        out->data = NULL;
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_NO_MEM;
    }
    esp_http_client_method_t http_method = HTTP_METHOD_GET;
    if (!strcmp(method, "POST")) http_method = HTTP_METHOD_POST;
    else if (!strcmp(method, "PUT")) http_method = HTTP_METHOD_PUT;
    esp_http_client_set_method(client, http_method);
    if (content_type) esp_http_client_set_header(client, "Content-Type", content_type);
    esp_http_client_set_header(client, "Accept", "application/json");
    esp_http_client_set_header(client, "Connection", "close");
    if (s_gateway_token[0]) {
        char authorization[128];
        snprintf(authorization, sizeof(authorization), "Bearer %s", s_gateway_token);
        esp_http_client_set_header(client, "Authorization", authorization);
    }
    if (body && body_len > 0) esp_http_client_set_post_field(client, body, body_len);
    esp_err_t err = esp_http_client_perform(client);
    out->status = esp_http_client_get_status_code(client);
    esp_http_client_cleanup(client);
    xSemaphoreGive(s_http_mutex);
    if (err != ESP_OK) {
        log_heap_snapshot("http-perform-fail");
    }
    if (out->truncated) {
        ESP_LOGE(TAG, "HTTP response truncated: capacity=%u path=%s", (unsigned)response_capacity, path);
        return ESP_ERR_INVALID_SIZE;
    }
    return err;
}

static esp_err_t request(const char *method, const char *path, const char *content_type,
                         const char *body, int body_len, http_response_t *out) {
    return request_with_capacity(method, path, content_type, body, body_len, RESPONSE_CAPACITY, out);
}

static void response_release(http_response_t *response) {
    if (!response) return;
    free(response->data);
    response->data = NULL;
    response->capacity = 0;
    response->len = 0;
    response->status = 0;
    response->truncated = false;
}

static const char *json_string(cJSON *root, const char *key) {
    cJSON *node = cJSON_GetObjectItemCaseSensitive(root, key);
    return cJSON_IsString(node) && node->valuestring ? node->valuestring : NULL;
}

static bool json_number(cJSON *root, const char *key, int *value) {
    cJSON *node = root ? cJSON_GetObjectItemCaseSensitive(root, key) : NULL;
    if (!cJSON_IsNumber(node) || !value) return false;
    *value = node->valueint;
    return true;
}

static void apply_ambient_json(cJSON *ambient) {
    if (!cJSON_IsObject(ambient)) return;
    int glyphs_cached = apply_glyphs_json(cJSON_GetObjectItemCaseSensitive(ambient, "glyphs"));
    cJSON *weather = cJSON_GetObjectItemCaseSensitive(ambient, "weather");
    if (!cJSON_IsObject(weather)) return;
    const char *summary = json_string(weather, "summary");
    const char *location = json_string(weather, "location");
    int temperature_c = 0;
    if (!summary || !summary[0] || !json_number(weather, "temperatureC", &temperature_c) ||
        temperature_c < -80 || temperature_c > 80) {
        ESP_LOGW(TAG, "ignored invalid ambient weather payload");
        return;
    }
    strlcpy(s_weather_summary, summary, sizeof(s_weather_summary));
    strlcpy(s_weather_location, location ? location : "", sizeof(s_weather_location));
    s_weather_temperature_c = temperature_c;
    cJSON *expires = cJSON_GetObjectItemCaseSensitive(ambient, "expiresAt");
    s_weather_expires_at_ms = cJSON_IsNumber(expires) ? (int64_t)expires->valuedouble : 0;
    s_weather_valid = true;
    save_ambient_weather();
    ESP_LOGI(TAG, "ambient weather received: summary='%s' temp=%d location='%s' glyphs_cached=%d raw_location=%s",
             s_weather_summary, s_weather_temperature_c, s_weather_location,
             glyphs_cached, location ? "present" : "missing");
}

static bool glyph_codepoint_from_key(const char *key, uint32_t *codepoint) {
    if (!key || !codepoint || strlen(key) != 6 || key[0] != 'U' || key[1] != '+') return false;
    char *end = NULL;
    unsigned long value = strtoul(key + 2, &end, 16);
    if (!end || *end || value < 0x20 || value > 0xFFFF ||
        (value >= 0xD800 && value <= 0xDFFF)) return false;
    *codepoint = (uint32_t)value;
    return true;
}

// Decode every glyph before accepting it into the display cache. A bad value
// never invalidates previously cached glyphs, so a transient/corrupt payload
// cannot turn already-readable text back into blanks.
static int apply_glyphs_json(cJSON *glyphs) {
    if (!cJSON_IsObject(glyphs)) return 0;
    int accepted = 0;
    cJSON *entry = NULL;
    cJSON_ArrayForEach(entry, glyphs) {
        if (accepted >= DYNAMIC_GLYPH_MAX_PER_MESSAGE || !cJSON_IsString(entry) || !entry->string) continue;
        uint32_t codepoint = 0;
        if (!glyph_codepoint_from_key(entry->string, &codepoint)) continue;
        uint8_t bitmap[DYNAMIC_GLYPH_BYTES];
        size_t decoded = 0;
        int result = mbedtls_base64_decode(bitmap, sizeof(bitmap), &decoded,
                                           (const unsigned char *)entry->valuestring,
                                           strlen(entry->valuestring));
        if (result != 0 || decoded != sizeof(bitmap)) {
            ESP_LOGW(TAG, "ignored invalid dynamic glyph %s", entry->string);
            continue;
        }
        if (board_port_cache_glyph(codepoint, bitmap)) {
            ++accepted;
            ESP_LOGI(TAG, "dynamic glyph cached: U+%04lX", (unsigned long)codepoint);
        }
    }
    if (accepted) ESP_LOGI(TAG, "dynamic glyph cache updated: received=%d", accepted);
    return accepted;
}

static void refresh_ambient_display(void) {
    time_t system_now = 0;
    time(&system_now);
    int64_t monotonic_us = esp_timer_get_time();
    bool system_clock_ready = system_now >= 1672531200; // 2023-01-01 UTC
    if (system_clock_ready) {
        time_t predicted = s_display_clock_epoch;
        if (s_display_clock_valid) {
            predicted += (time_t)((monotonic_us - s_display_clock_anchor_us) / 1000000);
        }
        // Accept the initial SNTP value and any later material correction, but
        // otherwise advance only from the local ESP32 monotonic clock.
        if (!s_display_clock_valid || llabs((long long)(system_now - predicted)) > 2) {
            s_display_clock_epoch = system_now;
            s_display_clock_anchor_us = monotonic_us;
            s_display_clock_valid = true;
        }
    }
    time_t now = s_display_clock_valid
                     ? s_display_clock_epoch + (time_t)((monotonic_us - s_display_clock_anchor_us) / 1000000)
                     : 0;
    struct tm local = {0};
    localtime_r(&now, &local);
    char current_time[9] = "--:--:--";
    char date[8] = "--/--";
    const char *weekdays[] = {"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"};
    const char *weekday = "时间同步中";
    if (s_display_clock_valid) {
        unsigned month = (unsigned)(local.tm_mon + 1) % 100u;
        unsigned day = (unsigned)local.tm_mday % 100u;
        snprintf(current_time, sizeof(current_time), "%02d:%02d:%02d",
                 local.tm_hour, local.tm_min, local.tm_sec);
        snprintf(date, sizeof(date), "%02u/%02u", month, day);
        weekday = weekdays[local.tm_wday];
    }
    int64_t now_ms = (int64_t)now * 1000;
    bool stale = s_weather_valid && s_weather_expires_at_ms > 0 && now_ms > s_weather_expires_at_ms;
    board_port_set_ambient(current_time, s_weather_location, date, weekday,
                           s_weather_summary, s_weather_temperature_c,
                           s_weather_valid, stale);
}

static void ambient_task(void *arg) {
    (void)arg;
    while (true) {
        refresh_ambient_display();
        // Redraw immediately after the next monotonic second boundary rather
        // than drifting with scheduler latency. This keeps the displayed
        // seconds visibly advancing even after the task has been running for
        // a long time.
        int64_t now_us = esp_timer_get_time();
        int64_t wait_us = 1000000 - (now_us % 1000000) + 1000;
        vTaskDelay(pdMS_TO_TICKS((wait_us + 999) / 1000));
    }
}

// Ambient state and pet-profile updates are server initiated. Keep a single
// long-poll running even while the user is not speaking; otherwise weather
// pushed after the startup handshake would sit at Hub until the next button
// interaction. The request layer is serialized, so this safely coexists with
// voice uploads and acknowledgements.
static void gateway_poll_task(void *arg) {
    (void)arg;
    while (true) {
        if (s_gateway_token[0]) {
            int64_t started_us = esp_timer_get_time();
            esp_err_t err = poll_reply();
            int64_t elapsed_ms = (esp_timer_get_time() - started_us) / 1000;
            if (err != ESP_OK) {
                vTaskDelay(pdMS_TO_TICKS(3000));
            } else if (elapsed_ms < 4000) {
                // Legacy Hub versions return an empty poll immediately. Avoid
                // a tight TLS reconnect loop until that Hub is upgraded to
                // the v1.1 long-poll implementation.
                vTaskDelay(pdMS_TO_TICKS(2000));
            }
        } else {
            vTaskDelay(pdMS_TO_TICKS(3000));
        }
    }
}

static bool ensure_gateway_poll_task(void) {
    if (!s_gateway_poll_task) {
        BaseType_t created = xTaskCreate(gateway_poll_task, "maclaw_gateway_poll", 6144, NULL, 3,
                                         &s_gateway_poll_task);
        if (created != pdPASS) {
            s_gateway_poll_task = NULL;
            ESP_LOGE(TAG, "cannot start gateway poll task");
            return false;
        }
    }
    return true;
}

static bool start_gateway_ready_tasks(void) {
    if (!ensure_gateway_poll_task()) {
        pet("alert");
        board_port_show_text("设备启动失败", "无法启动网关轮询");
        return false;
    }
    // Provisioning recovery stops ESP-SR to return its internal RAM to the
    // HTTP server. A successful normal handshake may therefore need to bring
    // the listener back before the device announces that it is ready.
    esp_err_t wake_err = board_port_start_wake_word(on_wake_word, NULL);
    if (wake_err != ESP_OK && wake_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "offline wake restart failed: %s", esp_err_to_name(wake_err));
    }
    board_port_show_ready_prompt("设备已就绪", "点屏说话 双点会议");
    if (s_meeting_pending && !s_meeting_task) {
        ESP_LOGI(TAG, "pending meeting upload found; starting resume" );
        (void)start_meeting_task(true);
    }
    return true;
}

static void start_clock_sync(void) {
    setenv("TZ", "CST-8", 1);
    tzset();
    esp_sntp_config_t config = ESP_NETIF_SNTP_DEFAULT_CONFIG("pool.ntp.org");
    esp_err_t err = esp_netif_sntp_init(&config);
    if (err != ESP_OK && err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "SNTP init failed: %s", esp_err_to_name(err));
    }
    if (!s_ambient_task) {
        // Clock cadence must remain independent of animation/render load.
        // A higher priority lets the once-per-second update preempt a frame
        // that has exceeded its budget instead of freezing the displayed time.
        BaseType_t created = xTaskCreate(ambient_task, "maclaw_ambient", 3072, NULL, 3,
                                         &s_ambient_task);
        if (created != pdPASS) {
            s_ambient_task = NULL;
            ESP_LOGE(TAG, "cannot start ambient clock task");
        }
    }
}

static void save_ambient_weather(void) {
    nvs_handle_t nvs;
    if (nvs_open("maclaw", NVS_READWRITE, &nvs) != ESP_OK) return;
    (void)nvs_set_str(nvs, "weather", s_weather_summary);
    (void)nvs_set_str(nvs, "weather_loc", s_weather_location);
    (void)nvs_set_i32(nvs, "weather_temp", s_weather_temperature_c);
    (void)nvs_set_i64(nvs, "weather_exp", s_weather_expires_at_ms);
    (void)nvs_commit(nvs);
    nvs_close(nvs);
}

static void load_ambient_weather(void) {
    nvs_handle_t nvs;
    if (nvs_open("maclaw", NVS_READONLY, &nvs) != ESP_OK) return;
    size_t summary_len = sizeof(s_weather_summary);
    size_t location_len = sizeof(s_weather_location);
    int32_t temperature = 0;
    bool found = nvs_get_str(nvs, "weather", s_weather_summary, &summary_len) == ESP_OK;
    (void)nvs_get_str(nvs, "weather_loc", s_weather_location, &location_len);
    (void)nvs_get_i32(nvs, "weather_temp", &temperature);
    (void)nvs_get_i64(nvs, "weather_exp", &s_weather_expires_at_ms);
    nvs_close(nvs);
    s_weather_temperature_c = temperature;
    s_weather_valid = found && s_weather_summary[0] != '\0';
}

static bool load_nvs_string(nvs_handle_t nvs, const char *key, char *out, size_t cap) {
    size_t len = cap;
    return nvs_get_str(nvs, key, out, &len) == ESP_OK && out[0] != '\0';
}

static bool is_valid_gateway_url(const char *url) {
    if (!url || !url[0] || strlen(url) >= URL_CAPACITY) return false;
    const char *host = NULL;
    if (!strncmp(url, "https://", 8)) host = url + 8;
    else if (!strncmp(url, "http://", 7)) host = url + 7;
    else return false;
    return host[0] != '\0' && host[0] != '/' && !strchr(host, ' ');
}

static bool is_six_digit_pair_code(const char *code) {
    if (!code || strlen(code) != 6) return false;
    for (size_t i = 0; i < 6; ++i) {
        if (code[i] < '0' || code[i] > '9') return false;
    }
    return true;
}

static void load_device_config(void) {
    strlcpy(s_wifi_ssid, CONFIG_MACLAW_WIFI_SSID, sizeof(s_wifi_ssid));
    strlcpy(s_wifi_password, CONFIG_MACLAW_WIFI_PASSWORD, sizeof(s_wifi_password));
    strlcpy(s_gateway_url, CONFIG_MACLAW_SERVER_URL, sizeof(s_gateway_url));
    s_pair_code[0] = '\0';
    nvs_handle_t nvs;
    if (nvs_open("maclaw", NVS_READONLY, &nvs) == ESP_OK) {
        (void)load_nvs_string(nvs, "wifi_ssid", s_wifi_ssid, sizeof(s_wifi_ssid));
        (void)load_nvs_string(nvs, "wifi_pass", s_wifi_password, sizeof(s_wifi_password));
        (void)load_nvs_string(nvs, "wifi_sec", s_wifi_security, sizeof(s_wifi_security));
        (void)load_nvs_string(nvs, "wifi_eap", s_wifi_eap_method, sizeof(s_wifi_eap_method));
        (void)load_nvs_string(nvs, "wifi_ident", s_wifi_identity, sizeof(s_wifi_identity));
        (void)load_nvs_string(nvs, "wifi_user", s_wifi_username, sizeof(s_wifi_username));
        (void)load_nvs_string(nvs, "wifi_ttls", s_wifi_ttls_phase2, sizeof(s_wifi_ttls_phase2));
        (void)load_nvs_string(nvs, "wifi_ca", s_wifi_ca_mode, sizeof(s_wifi_ca_mode));
        (void)load_nvs_string(nvs, "wifi_domain", s_wifi_server_domain, sizeof(s_wifi_server_domain));
        (void)load_nvs_string(nvs, "gateway_url", s_gateway_url, sizeof(s_gateway_url));
        (void)load_nvs_string(nvs, "pair_code", s_pair_code, sizeof(s_pair_code));
        nvs_close(nvs);
    }
}

static bool is_enterprise_wifi(void) {
    return !strcmp(s_wifi_security, "enterprise");
}

static bool is_valid_choice(const char *value, const char *first, const char *second,
                            const char *third) {
    return value && (!strcmp(value, first) || (second && !strcmp(value, second)) ||
                     (third && !strcmp(value, third)));
}

static esp_err_t save_device_config(const char *ssid, const char *password, const char *gateway_url,
                                    const char *pair_code, const char *security,
                                    const char *eap_method, const char *identity,
                                    const char *username, const char *ttls_phase2,
                                    const char *ca_mode, const char *server_domain) {
    bool enterprise = security && !strcmp(security, "enterprise");
    if (!ssid || !ssid[0] || strlen(ssid) > WIFI_SSID_MAX_LEN ||
        strlen(password) >= sizeof(s_wifi_password) || !is_valid_gateway_url(gateway_url) ||
        !is_six_digit_pair_code(pair_code) ||
        !is_valid_choice(security, "personal", "enterprise", NULL) ||
        (enterprise && (!is_valid_choice(eap_method, "peap", "ttls", NULL) || !username || !username[0] ||
                        strlen(username) >= sizeof(s_wifi_username) || strlen(identity) >= sizeof(s_wifi_identity) ||
                        !is_valid_choice(ttls_phase2, "mschapv2", "pap", NULL) ||
                        !is_valid_choice(ca_mode, "system", "none", NULL) ||
                        strlen(server_domain) >= sizeof(s_wifi_server_domain)))) return ESP_ERR_INVALID_ARG;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open("maclaw", NVS_READWRITE, &nvs);
    if (err != ESP_OK) return err;
    err = nvs_set_str(nvs, "wifi_ssid", ssid);
    if (err == ESP_OK) err = nvs_set_str(nvs, "wifi_pass", password);
    if (err == ESP_OK) err = nvs_set_str(nvs, "wifi_sec", enterprise ? "enterprise" : "personal");
    if (err == ESP_OK) err = nvs_set_str(nvs, "wifi_eap", enterprise ? eap_method : "peap");
    if (err == ESP_OK) err = nvs_set_str(nvs, "wifi_ident", enterprise ? identity : "");
    if (err == ESP_OK) err = nvs_set_str(nvs, "wifi_user", enterprise ? username : "");
    if (err == ESP_OK) err = nvs_set_str(nvs, "wifi_ttls", enterprise ? ttls_phase2 : "mschapv2");
    if (err == ESP_OK) err = nvs_set_str(nvs, "wifi_ca", enterprise ? ca_mode : "system");
    if (err == ESP_OK) err = nvs_set_str(nvs, "wifi_domain", enterprise ? server_domain : "");
    if (err == ESP_OK) err = nvs_set_str(nvs, "gateway_url", gateway_url);
    if (err == ESP_OK) err = nvs_set_str(nvs, "pair_code", pair_code);
    if (err == ESP_OK) {
        esp_err_t erase_err = nvs_erase_key(nvs, "gateway_token");
        // First-time provisioning has no token yet; that is a successful state,
        // not an NVS error that should reject the submitted configuration.
        if (erase_err != ESP_OK && erase_err != ESP_ERR_NVS_NOT_FOUND) err = erase_err;
    }
    if (err == ESP_OK) err = nvs_commit(nvs);
    nvs_close(nvs);
    if (err == ESP_OK) {
        strlcpy(s_wifi_ssid, ssid, sizeof(s_wifi_ssid));
        strlcpy(s_wifi_password, password, sizeof(s_wifi_password));
        strlcpy(s_wifi_security, enterprise ? "enterprise" : "personal", sizeof(s_wifi_security));
        strlcpy(s_wifi_eap_method, enterprise ? eap_method : "peap", sizeof(s_wifi_eap_method));
        strlcpy(s_wifi_identity, enterprise ? identity : "", sizeof(s_wifi_identity));
        strlcpy(s_wifi_username, enterprise ? username : "", sizeof(s_wifi_username));
        strlcpy(s_wifi_ttls_phase2, enterprise ? ttls_phase2 : "mschapv2", sizeof(s_wifi_ttls_phase2));
        strlcpy(s_wifi_ca_mode, enterprise ? ca_mode : "system", sizeof(s_wifi_ca_mode));
        strlcpy(s_wifi_server_domain, enterprise ? server_domain : "", sizeof(s_wifi_server_domain));
        strlcpy(s_gateway_url, gateway_url, sizeof(s_gateway_url));
        strlcpy(s_pair_code, pair_code, sizeof(s_pair_code));
    }
    ESP_LOGI(TAG, "config save: ssid_len=%u security=%s gateway_len=%u code_len=%u result=%s",
             (unsigned)strlen(ssid), security, (unsigned)strlen(gateway_url),
             (unsigned)strlen(pair_code), esp_err_to_name(err));
    return err;
}

static esp_err_t save_pairing_code_only(const char *pair_code) {
    if (!is_six_digit_pair_code(pair_code)) return ESP_ERR_INVALID_ARG;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open("maclaw", NVS_READWRITE, &nvs);
    if (err != ESP_OK) return err;
    err = nvs_set_str(nvs, "pair_code", pair_code);
    if (err == ESP_OK) err = nvs_commit(nvs);
    nvs_close(nvs);
    if (err == ESP_OK) strlcpy(s_pair_code, pair_code, sizeof(s_pair_code));
    return err;
}

static void load_gateway_token(void) {
    nvs_handle_t nvs;
    size_t len = sizeof(s_gateway_token);
    if (nvs_open("maclaw", NVS_READONLY, &nvs) == ESP_OK) {
        if (nvs_get_str(nvs, "gateway_token", s_gateway_token, &len) != ESP_OK) s_gateway_token[0] = '\0';
        nvs_close(nvs);
    }
    if (!s_gateway_token[0]) strlcpy(s_gateway_token, CONFIG_MACLAW_GATEWAY_TOKEN, sizeof(s_gateway_token));
}

static bool meeting_storage_partition_is_blank(void) {
    const esp_partition_t *partition = esp_partition_find_first(
        ESP_PARTITION_TYPE_DATA, ESP_PARTITION_SUBTYPE_DATA_SPIFFS, "storage");
    if (!partition || partition->size == 0) return false;

    // Prove that the complete partition is factory-erased before allowing an
    // automatic format. Sampling only its first sector is unsafe: after wear
    // leveling or interrupted metadata updates that sector can be blank while
    // later SPIFFS blocks still contain recoverable meeting audio.
    uint8_t sample[1024];
    for (size_t offset = 0; offset < partition->size; offset += sizeof(sample)) {
        size_t count = partition->size - offset;
        if (count > sizeof(sample)) count = sizeof(sample);
        if (esp_partition_read(partition, offset, sample, count) != ESP_OK) {
            return false;
        }
        for (size_t i = 0; i < count; ++i) {
            if (sample[i] != 0xff) return false;
        }
    }
    return true;
}

static esp_err_t mount_meeting_storage(void) {
    esp_vfs_spiffs_conf_t config = {
        .base_path = "/storage",
        .partition_label = "storage",
        .max_files = 4,
        .format_if_mount_failed = false,
    };
    esp_err_t err = esp_vfs_spiffs_register(&config);
    if (err != ESP_OK && meeting_storage_partition_is_blank()) {
        // Production flashing preserves the recording partition. Initialize a
        // genuinely factory-blank device once, but never use mount failure by
        // itself as permission to erase potentially recoverable recordings.
        ESP_LOGW(TAG, "blank meeting storage detected; formatting once");
        config.format_if_mount_failed = true;
        err = esp_vfs_spiffs_register(&config);
    }
    if (err == ESP_OK || err == ESP_ERR_INVALID_STATE) {
        s_storage_mounted = true;
        size_t total = 0;
        size_t used = 0;
        if (esp_spiffs_info("storage", &total, &used) == ESP_OK) {
            ESP_LOGI(TAG, "meeting storage mounted: total=%u used=%u",
                     (unsigned)total, (unsigned)used);
        }
        return ESP_OK;
    }
    ESP_LOGE(TAG, "meeting storage mount failed; preserving existing contents: %s",
             esp_err_to_name(err));
    return err;
}

static void load_meeting_recovery(void) {
    s_meeting_pending = false;
    s_meeting_next_chunk = 0;
    s_meeting_phase = 0;
    s_meeting_recording_id[0] = '\0';
    nvs_handle_t nvs;
    if (nvs_open("maclaw", NVS_READONLY, &nvs) != ESP_OK) return;
    uint8_t pending = 0;
    size_t id_len = sizeof(s_meeting_recording_id);
    (void)nvs_get_u8(nvs, "meet_pending", &pending);
    (void)nvs_get_i32(nvs, "meet_next", &s_meeting_next_chunk);
    (void)nvs_get_i32(nvs, "meet_phase", &s_meeting_phase);
    (void)nvs_get_str(nvs, "meet_id", s_meeting_recording_id, &id_len);
    nvs_close(nvs);
    struct stat info;
    s_meeting_pending = pending != 0 && s_storage_mounted &&
                        stat(MEETING_WAV_PATH, &info) == 0 && info.st_size > 44;
    if (!s_meeting_pending) {
        s_meeting_recording_id[0] = '\0';
        s_meeting_next_chunk = 0;
        s_meeting_phase = 0;
    }
}

static esp_err_t save_meeting_recovery(bool pending, const char *recording_id,
                                       int32_t next_chunk, int32_t phase) {
    nvs_handle_t nvs;
    esp_err_t err = nvs_open("maclaw", NVS_READWRITE, &nvs);
    if (err != ESP_OK) return err;
    err = nvs_set_u8(nvs, "meet_pending", pending ? 1 : 0);
    if (err == ESP_OK) err = nvs_set_str(nvs, "meet_id", recording_id ? recording_id : "");
    if (err == ESP_OK) err = nvs_set_i32(nvs, "meet_next", next_chunk);
    if (err == ESP_OK) err = nvs_set_i32(nvs, "meet_phase", phase);
    if (err == ESP_OK) err = nvs_commit(nvs);
    nvs_close(nvs);
    if (err == ESP_OK) {
        s_meeting_pending = pending;
        s_meeting_next_chunk = next_chunk;
        s_meeting_phase = phase;
        strlcpy(s_meeting_recording_id, recording_id ? recording_id : "",
                sizeof(s_meeting_recording_id));
    }
    return err;
}

static esp_err_t clear_meeting_recovery(bool delete_audio) {
    esp_err_t err = save_meeting_recovery(false, "", 0, 0);
    if (delete_audio && unlink(MEETING_WAV_PATH) != 0 && errno != ENOENT && err == ESP_OK) {
        err = ESP_FAIL;
    }
    return err;
}
static esp_err_t save_gateway_token(const char *token) {
    if (!token || !token[0] || strlen(token) >= sizeof(s_gateway_token)) return ESP_ERR_INVALID_SIZE;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open("maclaw", NVS_READWRITE, &nvs);
    if (err != ESP_OK) return err;
    err = nvs_set_str(nvs, "gateway_token", token);
    if (err == ESP_OK) err = nvs_commit(nvs);
    nvs_close(nvs);
    if (err == ESP_OK) strlcpy(s_gateway_token, token, sizeof(s_gateway_token));
    return err;
}

static esp_err_t gateway_handshake(void) {
    char payload[1024];
    http_response_t response;
    // The screen renderer keeps several DMA buffers in internal RAM. Asking
    // Hub for embedded RGB565 pet frames forces a 100+ KiB response and starves
    // the TLS allocation on this device. The built-in pet stays visible, while
    // the small handshake response still delivers city/weather immediately.
    int request_len = snprintf(payload, sizeof(payload),
        "{\"clientId\":\"%s\",\"clientName\":\"ESP32-S3 Pet\",\"protocolVersion\":\"1.1\","
        "\"capabilities\":{\"input\":{\"modalities\":[\"text\",\"audio\"],"
        "\"audio\":{\"mimeTypes\":[\"audio/wav\"],\"sampleRates\":[16000],\"channels\":1}},"
        "\"output\":{\"modalities\":[\"text\"],\"preferred\":[\"text\"],"
        "\"combinations\":[[\"text\"]],\"text\":{\"maxChars\":240,\"markdown\":false,\"locale\":\"zh-CN\"}},"
        "\"features\":{\"petStates\":true,\"petAnimation\":false,\"petAsset\":false,"
        "\"ambientDisplay\":true,\"meetingRecorder\":true}}}", CONFIG_MACLAW_CLIENT_ID);
    if (request_len <= 0 || request_len >= (int)sizeof(payload)) return ESP_ERR_INVALID_SIZE;
    log_heap_snapshot("handshake-before");
    esp_err_t err = request_with_capacity("POST", "/api/im-gateway/v1/handshake", "application/json",
                                          payload, (size_t)request_len, HANDSHAKE_RESPONSE_CAPACITY, &response);
    if (err != ESP_OK || response.status != 200) {
        ESP_LOGE(TAG, "gateway handshake failed: err=%s status=%d body=%s", esp_err_to_name(err), response.status, response.data);
        log_heap_snapshot("handshake-fail");
        esp_err_t result = gateway_auth_failed(&response, err) ? ESP_ERR_INVALID_STATE
                           : err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    cJSON *ok = json ? cJSON_GetObjectItemCaseSensitive(json, "ok") : NULL;
    if (!cJSON_IsTrue(ok)) {
        cJSON_Delete(json);
        response_release(&response);
        return ESP_ERR_INVALID_RESPONSE;
    }
    cJSON *accepted = cJSON_GetObjectItemCaseSensitive(json, "capabilitiesAccepted");
    cJSON *accepted_output = accepted ? cJSON_GetObjectItemCaseSensitive(accepted, "output") : NULL;
    cJSON *accepted_modalities = accepted_output ? cJSON_GetObjectItemCaseSensitive(accepted_output, "modalities") : NULL;
    bool accepted_text = false;
    cJSON *accepted_modality = NULL;
    cJSON_ArrayForEach(accepted_modality, accepted_modalities) {
        if (cJSON_IsString(accepted_modality) && strcmp(accepted_modality->valuestring, "text") == 0) {
            accepted_text = true;
            break;
        }
    }
    if (accepted) {
        ESP_LOGI(TAG, "client capabilities accepted: output=%s maxChars=240",
                 accepted_text ? "text" : "unsupported");
    } else {
        ESP_LOGW(TAG, "gateway did not acknowledge client capabilities (legacy Hub?)");
    }
    cJSON *meeting = cJSON_GetObjectItemCaseSensitive(json, "meetingRecording");
    s_meeting_available = cJSON_IsObject(meeting);
    if (s_meeting_available) {
        const char *base_path = json_string(meeting, "basePath");
        int chunk_size = 0;
        if (base_path && strlen(base_path) < sizeof(s_meeting_base_path)) {
            strlcpy(s_meeting_base_path, base_path, sizeof(s_meeting_base_path));
        }
        if (json_number(meeting, "chunkSize", &chunk_size) &&
            chunk_size >= (int)MEETING_MIN_CHUNK_SIZE &&
            chunk_size <= (int)MEETING_MAX_CHUNK_SIZE) {
            s_meeting_chunk_size = (size_t)chunk_size;
        }
        cJSON *modes = cJSON_GetObjectItemCaseSensitive(meeting, "modes");
        cJSON *minutes = modes ? cJSON_GetObjectItemCaseSensitive(modes, "minutes") : NULL;
        cJSON *transcript = modes ? cJSON_GetObjectItemCaseSensitive(modes, "transcript") : NULL;
        strlcpy(s_meeting_process_mode,
                cJSON_IsTrue(minutes) ? "minutes" : cJSON_IsTrue(transcript) ? "transcript" : "keep",
                sizeof(s_meeting_process_mode));
        ESP_LOGI(TAG, "meeting recording accepted: base=%s chunk=%u mode=%s",
                 s_meeting_base_path, (unsigned)s_meeting_chunk_size, s_meeting_process_mode);
    } else {
        ESP_LOGW(TAG, "Hub does not advertise meeting recording support");
    }    cJSON *pet_profile = cJSON_GetObjectItemCaseSensitive(json, "pet");
    const char *skin = pet_profile ? json_string(pet_profile, "skin") : NULL;
    cJSON *motion = pet_profile ? cJSON_GetObjectItemCaseSensitive(pet_profile, "motionEnabled") : NULL;
    if (skin) board_port_set_pet_profile(skin, !motion || cJSON_IsTrue(motion));
    // Remote assets can be tightly cropped at their source; keeping them off
    // this small round panel preserves the complete native pet silhouette.
    apply_ambient_json(cJSON_GetObjectItemCaseSensitive(json, "ambient"));
    cJSON_Delete(json);
    response_release(&response);
    log_heap_snapshot("handshake-ok");
    return ESP_OK;
}

static bool gateway_auth_failed(const http_response_t *response, esp_err_t err) {
    if (!response) return false;
    if (response->status == 401 || response->status == 403) return true;
    return err == ESP_ERR_NOT_SUPPORTED && response->status == 401;
}

// Unpaired devices speak the one-time six-digit code shown in the owner's
// MaClaw UI. MaClawSrv performs ASR and returns the gateway bearer over TLS.
static esp_err_t pair_by_voice(const uint8_t *wav, size_t wav_len) {
    http_response_t response;
    char client_header[96];
    snprintf(client_header, sizeof(client_header), "%s", CONFIG_MACLAW_CLIENT_ID);
    // pair endpoint needs a client ID header rather than authorization; use a
    // short dedicated request because the normal helper only emits fixed headers.
    char url[URL_CAPACITY];
    int n = snprintf(url, sizeof(url), "%s/api/device-gateway/v1/pair/voice", CONFIG_MACLAW_SERVER_URL);
    if (n < 0 || n >= sizeof(url)) return ESP_ERR_INVALID_SIZE;
    memset(&response, 0, sizeof(response));
    if (!s_http_mutex || xSemaphoreTake(s_http_mutex, pdMS_TO_TICKS(35000)) != pdTRUE) {
        ESP_LOGW(TAG, "HTTP request lock timeout: POST pair/voice");
        return ESP_ERR_TIMEOUT;
    }
    response.data = heap_caps_malloc(RESPONSE_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!response.data) response.data = heap_caps_malloc(RESPONSE_CAPACITY, MALLOC_CAP_8BIT);
    if (!response.data) {
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_NO_MEM;
    }
    response.capacity = RESPONSE_CAPACITY;
    response.data[0] = '\0';
    esp_http_client_config_t config = {.url = url, .event_handler = on_http_event, .user_data = &response, .timeout_ms = 30000, .crt_bundle_attach = esp_crt_bundle_attach};
    esp_http_client_handle_t client = esp_http_client_init(&config);
    if (!client) {
        response_release(&response);
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_NO_MEM;
    }
    esp_http_client_set_method(client, HTTP_METHOD_POST);
    esp_http_client_set_header(client, "Content-Type", "audio/wav");
    esp_http_client_set_header(client, "X-MaClaw-Client-ID", client_header);
    esp_http_client_set_post_field(client, (const char *)wav, wav_len);
    esp_err_t err = esp_http_client_perform(client);
    response.status = esp_http_client_get_status_code(client);
    esp_http_client_cleanup(client);
    xSemaphoreGive(s_http_mutex);
    if (response.truncated) err = ESP_ERR_INVALID_SIZE;
    if (err != ESP_OK || response.status != 201) {
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    const char *token = json ? json_string(json, "gatewayToken") : NULL;
    err = token ? save_gateway_token(token) : ESP_ERR_INVALID_RESPONSE;
    cJSON_Delete(json);
    response_release(&response);
    return err;
}

static esp_err_t pair_by_code(void) {
    if (strlen(s_pair_code) != 6) return ESP_ERR_INVALID_STATE;
    cJSON *body = cJSON_CreateObject();
    cJSON_AddStringToObject(body, "clientId", CONFIG_MACLAW_CLIENT_ID);
    // pairCode is the canonical device-gateway field across Hub and
    // MaClawSrv. Hub retains a server-side code alias solely for old firmware.
    cJSON_AddStringToObject(body, "pairCode", s_pair_code);
    char *payload = cJSON_PrintUnformatted(body);
    cJSON_Delete(body);
    if (!payload) return ESP_ERR_NO_MEM;
    http_response_t response;
    esp_err_t err = request("POST", "/api/device-gateway/v1/pair", "application/json", payload, strlen(payload), &response);
    free(payload);
    if (err != ESP_OK || response.status != 201) {
        ESP_LOGE(TAG, "pair failed: err=%s status=%d body=%s", esp_err_to_name(err), response.status, response.data ? response.data : "");
        // Transport failures, rate limiting and server errors are temporary.
        // Keep the one-time code and retry instead of incorrectly telling the
        // user that the code expired and replacing the normal UI with a setup AP.
        esp_err_t result = err;
        // esp_http_client may return ESP_ERR_NOT_SUPPORTED after it has already
        // received an HTTP authentication error. The status and JSON body are
        // authoritative once a response exists.
        if (response.status > 0) {
            switch (response.status) {
                case 400:
                case 401:
                case 403:
                case 404:
                case 409:
                case 410:
                case 422:
                    result = ESP_ERR_INVALID_STATE;
                    break;
                default:
                    if (response.status >= 500 || response.status == 408 || response.status == 429) {
                        result = ESP_FAIL;
                    } else if (err == ESP_OK) {
                        result = ESP_FAIL;
                    }
                    break;
            }
        }
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    const char *token = json ? json_string(json, "gatewayToken") : NULL;
    err = token ? save_gateway_token(token) : ESP_ERR_INVALID_RESPONSE;
    cJSON_Delete(json);
    if (err == ESP_OK) {
        nvs_handle_t nvs;
        if (nvs_open("maclaw", NVS_READWRITE, &nvs) == ESP_OK) {
            (void)nvs_erase_key(nvs, "pair_code");
            (void)nvs_commit(nvs);
            nvs_close(nvs);
        }
        s_pair_code[0] = '\0';
    }
    response_release(&response);
    return err;
}

static esp_err_t upload_voice(const uint8_t *wav, size_t wav_len, char *media_id, size_t media_id_cap) {
    cJSON *body = cJSON_CreateObject();
    cJSON_AddStringToObject(body, "clientId", CONFIG_MACLAW_CLIENT_ID);
    cJSON_AddStringToObject(body, "type", "voice");
    cJSON_AddStringToObject(body, "fileName", "voice.wav");
    cJSON_AddStringToObject(body, "mimeType", "audio/wav");
    cJSON_AddNumberToObject(body, "sizeBytes", (double)wav_len);
    char *payload = cJSON_PrintUnformatted(body);
    cJSON_Delete(body);
    if (!payload) return ESP_ERR_NO_MEM;
    http_response_t response;
    esp_err_t err = request("POST", "/api/im-gateway/v1/media/upload-url", "application/json", payload, strlen(payload), &response);
    free(payload);
    if (err != ESP_OK || response.status != 200) {
        ESP_LOGE(TAG, "media prepare failed: err=%s status=%d body=%s", esp_err_to_name(err), response.status, response.data);
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    cJSON *media = json ? cJSON_GetObjectItemCaseSensitive(json, "media") : NULL;
    cJSON *upload = json ? cJSON_GetObjectItemCaseSensitive(json, "upload") : NULL;
    const char *id = media ? json_string(media, "id") : NULL;
    const char *url = upload ? json_string(upload, "url") : NULL;
    if (!id || !url) {
        cJSON_Delete(json);
        response_release(&response);
        return ESP_ERR_INVALID_RESPONSE;
    }
    char id_copy[96];
    char url_copy[URL_CAPACITY];
    strlcpy(id_copy, id, sizeof(id_copy));
    strlcpy(url_copy, url, sizeof(url_copy));
    cJSON_Delete(json);
    response_release(&response);
    http_response_t put_response;
    err = request("PUT", url_copy, "audio/wav", (const char *)wav, wav_len, &put_response);
    if (err != ESP_OK || (put_response.status != 200 && put_response.status != 201)) {
        ESP_LOGE(TAG, "media upload failed: err=%s status=%d", esp_err_to_name(err), put_response.status);
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&put_response);
        return result;
    }
    strlcpy(media_id, id_copy, media_id_cap);
    response_release(&put_response);
    return ESP_OK;
}

static esp_err_t send_voice_event(const char *media_id) {
    cJSON *body = cJSON_CreateObject();
    char event_id[80];
    snprintf(event_id, sizeof(event_id), "voice-%lld", (long long)esp_timer_get_time());
    cJSON_AddStringToObject(body, "clientId", CONFIG_MACLAW_CLIENT_ID);
    cJSON_AddStringToObject(body, "eventId", event_id);
    cJSON_AddStringToObject(body, "messageId", event_id);
    cJSON_AddStringToObject(body, "conversationId", CONFIG_MACLAW_CONVERSATION_ID);
    cJSON *user = cJSON_AddObjectToObject(body, "user");
    cJSON_AddStringToObject(user, "id", "local-user");
    cJSON_AddStringToObject(user, "displayName", "ESP32-S3 user");
    cJSON *message = cJSON_AddObjectToObject(body, "message");
    cJSON_AddStringToObject(message, "id", event_id);
    cJSON_AddStringToObject(message, "type", "voice");
    cJSON_AddStringToObject(message, "mimeType", "audio/wav");
    cJSON *attachments = cJSON_AddArrayToObject(message, "attachments");
    cJSON *attachment = cJSON_CreateObject();
    cJSON_AddStringToObject(attachment, "id", media_id);
    cJSON_AddStringToObject(attachment, "type", "voice");
    cJSON_AddStringToObject(attachment, "mimeType", "audio/wav");
    cJSON_AddItemToArray(attachments, attachment);
    char *payload = cJSON_PrintUnformatted(body);
    cJSON_Delete(body);
    if (!payload) return ESP_ERR_NO_MEM;
    http_response_t response;
    esp_err_t err = request("POST", "/api/im-gateway/v1/incoming", "application/json", payload, strlen(payload), &response);
    free(payload);
    if (err != ESP_OK || response.status != 200) {
        ESP_LOGE(TAG, "incoming event failed: err=%s status=%d body=%s", esp_err_to_name(err), response.status, response.data);
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    cJSON *accepted = json ? cJSON_GetObjectItemCaseSensitive(json, "accepted") : NULL;
    if (!cJSON_IsTrue(accepted)) {
        cJSON_Delete(json);
        response_release(&response);
        return ESP_ERR_INVALID_RESPONSE;
    }
    cJSON_Delete(json);
    response_release(&response);
    return ESP_OK;
}

static esp_err_t send_text_event(const char *text) {
    if (!text || !text[0]) return ESP_ERR_INVALID_ARG;
    cJSON *body = cJSON_CreateObject();
    char event_id[80];
    snprintf(event_id, sizeof(event_id), "text-%lld", (long long)esp_timer_get_time());
    cJSON_AddStringToObject(body, "clientId", CONFIG_MACLAW_CLIENT_ID);
    cJSON_AddStringToObject(body, "eventId", event_id);
    cJSON_AddStringToObject(body, "messageId", event_id);
    cJSON_AddStringToObject(body, "conversationId", CONFIG_MACLAW_CONVERSATION_ID);
    cJSON *user = cJSON_AddObjectToObject(body, "user");
    cJSON_AddStringToObject(user, "id", "local-user");
    cJSON_AddStringToObject(user, "displayName", "ESP32-S3 user");
    cJSON *message = cJSON_AddObjectToObject(body, "message");
    cJSON_AddStringToObject(message, "id", event_id);
    cJSON_AddStringToObject(message, "type", "text");
    cJSON_AddStringToObject(message, "text", text);
    char *payload = cJSON_PrintUnformatted(body);
    cJSON_Delete(body);
    if (!payload) return ESP_ERR_NO_MEM;
    http_response_t response;
    esp_err_t err = request("POST", "/api/im-gateway/v1/incoming", "application/json", payload, strlen(payload), &response);
    free(payload);
    if (err != ESP_OK || response.status != 200) {
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    cJSON *accepted = json ? cJSON_GetObjectItemCaseSensitive(json, "accepted") : NULL;
    bool ok = cJSON_IsTrue(accepted);
    cJSON_Delete(json);
    response_release(&response);
    return ok ? ESP_OK : ESP_ERR_INVALID_RESPONSE;
}

static esp_err_t poll_reply(void) {
    char path[320];
    // Keep one and only one reader for the outgoing stream. A bounded long
    // poll removes the old TLS reconnect loop while still letting interaction
    // uploads run without waiting behind a 30-second request.
    snprintf(path, sizeof(path), "/api/im-gateway/v1/outgoing?clientId=%s&cursor=%lld&limit=4&timeout=5", CONFIG_MACLAW_CLIENT_ID, s_cursor);
    http_response_t response;
    esp_err_t err = request("GET", path, NULL, NULL, 0, &response);
    if (err != ESP_OK || response.status != 200) {
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    if (!json) {
        ESP_LOGW(TAG, "outgoing response is not valid JSON");
        response_release(&response);
        return ESP_ERR_INVALID_RESPONSE;
    }
    const char *next = json_string(json, "nextCursor");
    cJSON *messages = cJSON_GetObjectItemCaseSensitive(json, "messages");
    if (!next || !cJSON_IsArray(messages)) {
        ESP_LOGW(TAG, "outgoing response missing nextCursor/messages");
        cJSON_Delete(json);
        response_release(&response);
        return ESP_ERR_INVALID_RESPONSE;
    }
    errno = 0;
    char *cursor_end = NULL;
    long long parsed_cursor = strtoll(next, &cursor_end, 10);
    if (errno == ERANGE || cursor_end == next || *cursor_end != '\0' || parsed_cursor < 0) {
        ESP_LOGW(TAG, "outgoing response has invalid cursor: %s", next);
        cJSON_Delete(json);
        response_release(&response);
        return ESP_ERR_INVALID_RESPONSE;
    }
    cJSON *ack_ids = cJSON_CreateArray();
    if (!ack_ids) {
        cJSON_Delete(json);
        response_release(&response);
        return ESP_ERR_NO_MEM;
    }
    cJSON *item = NULL;
    cJSON_ArrayForEach(item, messages) {
        const char *type = json_string(item, "type");
        const char *text = json_string(item, "text");
        const char *id = json_string(item, "id");
        const char *skin = json_string(item, "pet_skin");
        cJSON *motion = cJSON_GetObjectItemCaseSensitive(item, "pet_motion_enabled");
        cJSON *extra = cJSON_GetObjectItemCaseSensitive(item, "extra");
        cJSON *metadata = cJSON_GetObjectItemCaseSensitive(item, "metadata");
        if (!skin && cJSON_IsObject(extra)) skin = json_string(extra, "pet_skin");
        if (!skin && cJSON_IsObject(metadata)) skin = json_string(metadata, "pet_skin");
        if (skin) board_port_set_pet_profile(skin, !motion || cJSON_IsTrue(motion));
        apply_glyphs_json(cJSON_GetObjectItemCaseSensitive(item, "glyphs"));
        apply_ambient_json(cJSON_GetObjectItemCaseSensitive(item, "ambient"));
        if (type && !strcmp(type, "ambient")) apply_ambient_json(item);
        if (type && !strcmp(type, "pet_state")) {
            const char *state = cJSON_IsObject(extra) ? json_string(extra, "state") : NULL;
            if (!state) state = json_string(item, "state");
            if (state) pet(state);
        }
        if (type && !strcmp(type, "meeting_result")) {
            const char *summary = cJSON_IsObject(extra) ? json_string(extra, "summary") : NULL;
            const char *status = cJSON_IsObject(extra) ? json_string(extra, "status") : NULL;
            const char *message = summary && summary[0] ? summary :
                                  text && text[0] ? text :
                                  status && status[0] ? status : "已保存到文稿库";
            pet("done");
            board_port_show_text("会议处理完成", message);
        }
        if (type && !strcmp(type, "text") && text) {
            pet("speaking");
            board_port_show_text("MaClaw", text);
            // Keep the handle protected until the notification is queued.
            // Otherwise the interaction task can clear/delete itself after a
            // snapshot is taken and the poller would notify a stale TCB.
            taskENTER_CRITICAL(&s_task_state_lock);
            TaskHandle_t waiter = s_interaction_task;
            if (waiter) xTaskNotifyGive(waiter);
            taskEXIT_CRITICAL(&s_task_state_lock);
        }
        if (id) {
            cJSON *ack_id = cJSON_CreateString(id);
            if (!ack_id || !cJSON_AddItemToArray(ack_ids, ack_id)) {
                cJSON_Delete(ack_id);
                cJSON_Delete(ack_ids);
                cJSON_Delete(json);
                response_release(&response);
                return ESP_ERR_NO_MEM;
            }
        }
    }
    if (cJSON_GetArraySize(ack_ids) > 0) {
        cJSON *ack = cJSON_CreateObject();
        if (!ack) {
            cJSON_Delete(ack_ids);
            cJSON_Delete(json);
            response_release(&response);
            return ESP_ERR_NO_MEM;
        }
        cJSON_AddStringToObject(ack, "clientId", CONFIG_MACLAW_CLIENT_ID);
        cJSON_AddItemToObject(ack, "messageIds", ack_ids);
        cJSON_AddStringToObject(ack, "status", "delivered");
        char *payload = cJSON_PrintUnformatted(ack);
        cJSON_Delete(ack);
        if (!payload) {
            cJSON_Delete(json);
            response_release(&response);
            return ESP_ERR_NO_MEM;
        }
        http_response_t ack_resp;
        esp_err_t ack_err = request("POST", "/api/im-gateway/v1/ack", "application/json",
                                    payload, strlen(payload), &ack_resp);
        free(payload);
        if (ack_err != ESP_OK || (ack_resp.status != 200 && ack_resp.status != 204)) {
            ESP_LOGW(TAG, "gateway ack failed: err=%s status=%d",
                     esp_err_to_name(ack_err), ack_resp.status);
            esp_err_t result = ack_err == ESP_OK ? ESP_FAIL : ack_err;
            response_release(&ack_resp);
            cJSON_Delete(json);
            response_release(&response);
            return result;
        }
        response_release(&ack_resp);
    } else {
        cJSON_Delete(ack_ids);
    }
    s_cursor = (int64_t)parsed_cursor;
    cJSON_Delete(json);
    response_release(&response);
    return ESP_OK;
}

static void put_le16(uint8_t *out, uint16_t value) {
    out[0] = (uint8_t)value;
    out[1] = (uint8_t)(value >> 8);
}

static void put_le32(uint8_t *out, uint32_t value) {
    out[0] = (uint8_t)value;
    out[1] = (uint8_t)(value >> 8);
    out[2] = (uint8_t)(value >> 16);
    out[3] = (uint8_t)(value >> 24);
}

static void build_meeting_wav_header(uint8_t header[44], uint32_t pcm_bytes) {
    memset(header, 0, 44);
    memcpy(header, "RIFF", 4);
    put_le32(header + 4, 36u + pcm_bytes);
    memcpy(header + 8, "WAVEfmt ", 8);
    put_le32(header + 16, 16);
    put_le16(header + 20, 1);
    put_le16(header + 22, 1);
    put_le32(header + 24, MEETING_SAMPLE_RATE);
    put_le32(header + 28, MEETING_SAMPLE_RATE * 2u);
    put_le16(header + 32, 2);
    put_le16(header + 34, 16);
    memcpy(header + 36, "data", 4);
    put_le32(header + 40, pcm_bytes);
}

static esp_err_t finalize_meeting_wav(FILE *file, uint64_t samples) {
    if (!file || samples > (UINT32_MAX / sizeof(int16_t))) return ESP_ERR_INVALID_SIZE;
    uint8_t header[44];
    build_meeting_wav_header(header, (uint32_t)(samples * sizeof(int16_t)));
    if (fseek(file, 0, SEEK_SET) != 0 || fwrite(header, 1, sizeof(header), file) != sizeof(header)) {
        return ESP_FAIL;
    }
    if (fflush(file) != 0 || fsync(fileno(file)) != 0) return ESP_FAIL;
    return ESP_OK;
}

static esp_err_t ensure_meeting_wav_header(FILE *file, size_t file_size) {
    if (!file || file_size <= 44 || ((file_size - 44) % sizeof(int16_t)) != 0) {
        return ESP_ERR_INVALID_SIZE;
    }
    uint64_t samples = (file_size - 44) / sizeof(int16_t);
    if (samples > (UINT32_MAX / sizeof(int16_t))) return ESP_ERR_INVALID_SIZE;
    uint8_t expected[44];
    uint8_t existing[44];
    build_meeting_wav_header(expected, (uint32_t)(samples * sizeof(int16_t)));
    if (fseek(file, 0, SEEK_SET) != 0 || fread(existing, 1, sizeof(existing), file) != sizeof(existing)) {
        return ESP_FAIL;
    }
    if (memcmp(existing, expected, sizeof(expected)) == 0) return ESP_OK;
    // A reset or capture error may leave the initial zero-length placeholder
    // header in front of otherwise valid PCM. Repair it before any retry so a
    // retained meeting is always uploaded as a valid, self-describing WAV.
    ESP_LOGW(TAG, "repairing retained meeting WAV header: bytes=%u",
             (unsigned)file_size);
    return finalize_meeting_wav(file, samples);
}

static void digest_hex(const uint8_t digest[32], char out[65]) {
    static const char hex[] = "0123456789abcdef";
    for (size_t i = 0; i < 32; ++i) {
        out[i * 2] = hex[digest[i] >> 4];
        out[i * 2 + 1] = hex[digest[i] & 15];
    }
    out[64] = '\0';
}

static esp_err_t hash_file_range(FILE *file, size_t offset, size_t length,
                                 uint8_t *buffer, size_t buffer_size, char out_hex[65]) {
    if (!file || !buffer || buffer_size == 0 || fseek(file, (long)offset, SEEK_SET) != 0) {
        return ESP_ERR_INVALID_ARG;
    }
    psa_hash_operation_t operation = PSA_HASH_OPERATION_INIT;
    psa_status_t status = psa_hash_setup(&operation, PSA_ALG_SHA_256);
    size_t remaining = length;
    while (status == PSA_SUCCESS && remaining > 0) {
        size_t wanted = remaining < buffer_size ? remaining : buffer_size;
        size_t count = fread(buffer, 1, wanted, file);
        if (count != wanted) {
            psa_hash_abort(&operation);
            return ESP_FAIL;
        }
        status = psa_hash_update(&operation, buffer, count);
        remaining -= count;
    }
    uint8_t digest[32];
    size_t digest_length = 0;
    if (status == PSA_SUCCESS) {
        status = psa_hash_finish(&operation, digest, sizeof(digest), &digest_length);
    } else {
        psa_hash_abort(&operation);
    }
    if (status != PSA_SUCCESS || digest_length != sizeof(digest)) return ESP_FAIL;
    digest_hex(digest, out_hex);
    return ESP_OK;
}

static esp_err_t stream_meeting_chunk(const char *recording_id, int index, FILE *file,
                                      size_t offset, size_t length, const char sha256_hex[65],
                                      uint8_t *buffer, size_t buffer_size) {
    char path[MEETING_BASE_PATH_CAPACITY + MEETING_RECORDING_ID_CAPACITY + 48];
    char url[URL_CAPACITY];
    int path_len = snprintf(path, sizeof(path), "%s/%s/chunks/%d",
                            s_meeting_base_path, recording_id, index);
    int url_len = snprintf(url, sizeof(url), "%s%s", s_gateway_url, path);
    if (path_len < 0 || path_len >= (int)sizeof(path) ||
        url_len < 0 || url_len >= (int)sizeof(url) ||
        fseek(file, (long)offset, SEEK_SET) != 0) return ESP_ERR_INVALID_SIZE;
    if (!s_http_mutex || xSemaphoreTake(s_http_mutex, pdMS_TO_TICKS(35000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    http_response_t response = {0};
    response.data = heap_caps_malloc(MEETING_RESPONSE_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!response.data) response.data = malloc(MEETING_RESPONSE_CAPACITY);
    if (!response.data) {
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_NO_MEM;
    }
    response.capacity = MEETING_RESPONSE_CAPACITY;
    response.data[0] = '\0';
    esp_http_client_config_t config = {
        .url = url, .event_handler = on_http_event, .user_data = &response,
        .timeout_ms = 60000, .crt_bundle_attach = esp_crt_bundle_attach,
        .keep_alive_enable = true,
    };
    esp_http_client_handle_t client = esp_http_client_init(&config);
    if (!client) {
        response_release(&response);
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_NO_MEM;
    }
    esp_http_client_set_method(client, HTTP_METHOD_PUT);
    esp_http_client_set_header(client, "Content-Type", "application/octet-stream");
    esp_http_client_set_header(client, "X-Chunk-SHA256", sha256_hex);
    esp_http_client_set_header(client, "Accept", "application/json");
    esp_http_client_set_header(client, "Connection", "close");
    char authorization[128];
    snprintf(authorization, sizeof(authorization), "Bearer %s", s_gateway_token);
    esp_http_client_set_header(client, "Authorization", authorization);
    esp_err_t err = esp_http_client_open(client, (int)length);
    size_t remaining = length;
    while (err == ESP_OK && remaining > 0) {
        size_t wanted = remaining < buffer_size ? remaining : buffer_size;
        size_t count = fread(buffer, 1, wanted, file);
        if (count != wanted) {
            err = ESP_FAIL;
            break;
        }
        size_t written = 0;
        while (written < count) {
            int result = esp_http_client_write(client, (const char *)buffer + written, count - written);
            if (result <= 0) {
                err = ESP_FAIL;
                break;
            }
            written += (size_t)result;
        }
        remaining -= count;
    }
    if (err == ESP_OK) {
        int headers = esp_http_client_fetch_headers(client);
        if (headers < 0) err = ESP_FAIL;
        while (err == ESP_OK) {
            int count = esp_http_client_read(client, (char *)buffer, buffer_size);
            if (count < 0) err = ESP_FAIL;
            if (count <= 0) break;
        }
    }
    response.status = esp_http_client_get_status_code(client);
    esp_http_client_close(client);
    esp_http_client_cleanup(client);
    xSemaphoreGive(s_http_mutex);
    if (err == ESP_OK && (response.status < 200 || response.status >= 300)) {
        ESP_LOGE(TAG, "meeting chunk %d rejected: status=%d body=%s",
                 index, response.status, response.data ? response.data : "");
        err = ESP_FAIL;
    }
    response_release(&response);
    return err;
}

static esp_err_t create_meeting_recording(char recording_id[MEETING_RECORDING_ID_CAPACITY]) {
    char payload[192];
    int length = snprintf(payload, sizeof(payload),
                          "{\"title\":\"硬件会议录音\",\"purpose\":\"\","
                          "\"conversation_id\":\"%s\",\"content_type\":\"audio/wav\"}",
                          CONFIG_MACLAW_CONVERSATION_ID);
    if (length <= 0 || length >= (int)sizeof(payload)) return ESP_ERR_INVALID_SIZE;
    http_response_t response;
    esp_err_t err = request_with_capacity("POST", s_meeting_base_path, "application/json",
                                          payload, length, MEETING_RESPONSE_CAPACITY, &response);
    if (err != ESP_OK || response.status != 201) {
        ESP_LOGE(TAG, "meeting create failed: err=%s status=%d body=%s",
                 esp_err_to_name(err), response.status, response.data ? response.data : "");
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    const char *id = json ? json_string(json, "recording_id") : NULL;
    if (!id || strlen(id) >= MEETING_RECORDING_ID_CAPACITY) err = ESP_ERR_INVALID_RESPONSE;
    else strlcpy(recording_id, id, MEETING_RECORDING_ID_CAPACITY);
    cJSON_Delete(json);
    response_release(&response);
    return err;
}

static esp_err_t get_meeting_status(const char *recording_id, char *status, size_t status_cap) {
    char path[MEETING_BASE_PATH_CAPACITY + MEETING_RECORDING_ID_CAPACITY + 8];
    int length = snprintf(path, sizeof(path), "%s/%s", s_meeting_base_path, recording_id);
    if (length <= 0 || length >= (int)sizeof(path)) return ESP_ERR_INVALID_SIZE;
    http_response_t response;
    esp_err_t err = request_with_capacity("GET", path, NULL, NULL, 0,
                                          MEETING_RESPONSE_CAPACITY, &response);
    if (err != ESP_OK || response.status != 200) {
        esp_err_t result = response.status == 404 ? ESP_ERR_NOT_FOUND : err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    const char *value = json ? json_string(json, "status") : NULL;
    if (!value || strlen(value) >= status_cap) err = ESP_ERR_INVALID_RESPONSE;
    else strlcpy(status, value, status_cap);
    cJSON_Delete(json);
    response_release(&response);
    return err;
}
static esp_err_t post_meeting_action(const char *recording_id, const char *action,
                                     const char *payload, int expected_a, int expected_b) {
    char path[MEETING_BASE_PATH_CAPACITY + MEETING_RECORDING_ID_CAPACITY + 32];
    int length = snprintf(path, sizeof(path), "%s/%s/%s", s_meeting_base_path, recording_id, action);
    if (length <= 0 || length >= (int)sizeof(path)) return ESP_ERR_INVALID_SIZE;
    http_response_t response;
    esp_err_t err = request_with_capacity("POST", path, "application/json", payload, strlen(payload),
                                          MEETING_RESPONSE_CAPACITY, &response);
    if (err != ESP_OK || (response.status != expected_a && response.status != expected_b)) {
        ESP_LOGE(TAG, "meeting %s failed: err=%s status=%d body=%s",
                 action, esp_err_to_name(err), response.status, response.data ? response.data : "");
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    response_release(&response);
    return ESP_OK;
}

static esp_err_t upload_pending_meeting(bool publish_state) {
    struct stat info;
    if (!s_storage_mounted || stat(MEETING_WAV_PATH, &info) != 0 || info.st_size <= 44) {
        return ESP_ERR_NOT_FOUND;
    }
    FILE *file = fopen(MEETING_WAV_PATH, "rb+");
    if (!file) return ESP_FAIL;
    size_t file_size = (size_t)info.st_size;
    esp_err_t header_err = ensure_meeting_wav_header(file, file_size);
    if (header_err != ESP_OK) {
        ESP_LOGE(TAG, "retained meeting WAV is not recoverable: %s",
                 esp_err_to_name(header_err));
        fclose(file);
        return header_err;
    }
    uint8_t *buffer = heap_caps_malloc(MEETING_IO_BUFFER_SIZE, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!buffer) buffer = malloc(MEETING_IO_BUFFER_SIZE);
    if (!buffer) {
        fclose(file);
        return ESP_ERR_NO_MEM;
    }
    char recording_id[MEETING_RECORDING_ID_CAPACITY];
    strlcpy(recording_id, s_meeting_recording_id, sizeof(recording_id));
    int next_chunk = s_meeting_next_chunk;
    int phase = s_meeting_phase;
    esp_err_t err = ESP_OK;
    if (recording_id[0] != '\0') {
        char status[20] = {0};
        esp_err_t status_err = get_meeting_status(recording_id, status, sizeof(status));
        if (status_err == ESP_ERR_NOT_FOUND) {
            recording_id[0] = '\0';
            next_chunk = 0;
            phase = 0;
            err = save_meeting_recovery(true, "", 0, 0);
        } else if (status_err != ESP_OK) {
            err = status_err;
        } else if (!strcmp(status, "processing") || !strcmp(status, "ready")) {
            phase = 2;
            next_chunk = (int)((size_t)info.st_size + s_meeting_chunk_size - 1) /
                         (int)s_meeting_chunk_size;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
        } else if (!strcmp(status, "uploaded") || !strcmp(status, "failed")) {
            phase = 1;
            next_chunk = (int)((size_t)info.st_size + s_meeting_chunk_size - 1) /
                         (int)s_meeting_chunk_size;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
        } else if (strcmp(status, "uploading")) {
            err = ESP_ERR_INVALID_STATE;
        }
    }
    if (err == ESP_OK && recording_id[0] == '\0') {
        err = create_meeting_recording(recording_id);
        if (err == ESP_OK) err = save_meeting_recovery(true, recording_id, 0, 0);
        next_chunk = 0;
        phase = 0;
    }
    int chunks = (int)((file_size + s_meeting_chunk_size - 1) / s_meeting_chunk_size);
    for (int index = next_chunk; err == ESP_OK && index < chunks; ++index) {
        size_t offset = (size_t)index * s_meeting_chunk_size;
        size_t length = file_size - offset;
        if (length > s_meeting_chunk_size) length = s_meeting_chunk_size;
        char chunk_hash[65];
        err = hash_file_range(file, offset, length, buffer, MEETING_IO_BUFFER_SIZE, chunk_hash);
        if (err == ESP_OK) {
            err = stream_meeting_chunk(recording_id, index, file, offset, length,
                                       chunk_hash, buffer, MEETING_IO_BUFFER_SIZE);
        }
        if (err == ESP_OK) {
            next_chunk = index + 1;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
        }
    }
    char whole_hash[65];
    if (err == ESP_OK && phase < 1) {
        if (publish_state) meeting_set_state(MEETING_FINALIZING);
        err = hash_file_range(file, 0, file_size, buffer, MEETING_IO_BUFFER_SIZE, whole_hash);
        if (err == ESP_OK) {
            uint32_t pcm_bytes = file_size > 44 ? (uint32_t)(file_size - 44) : 0;
            double duration = (double)pcm_bytes / (MEETING_SAMPLE_RATE * 2.0);
            char payload[192];
            int length = snprintf(payload, sizeof(payload),
                                  "{\"chunks\":%d,\"sha256\":\"%s\",\"duration_sec\":%.3f}",
                                  chunks, whole_hash, duration);
            if (length <= 0 || length >= (int)sizeof(payload)) err = ESP_ERR_INVALID_SIZE;
            else err = post_meeting_action(recording_id, "complete", payload, 200, 200);
        }
        if (err == ESP_OK) {
            phase = 1;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
        }
    }
    if (err == ESP_OK && phase >= 1) {
        char status[20] = {0};
        if (get_meeting_status(recording_id, status, sizeof(status)) == ESP_OK &&
            (!strcmp(status, "processing") || !strcmp(status, "ready"))) {
            phase = 2;
            (void)save_meeting_recovery(true, recording_id, next_chunk, phase);
        }
    }
    if (err == ESP_OK && phase < 2) {
        if (publish_state) meeting_set_state(MEETING_PROCESSING);
        char payload[48];
        int length = snprintf(payload, sizeof(payload), "{\"mode\":\"%s\"}", s_meeting_process_mode);
        if (length <= 0 || length >= (int)sizeof(payload)) err = ESP_ERR_INVALID_SIZE;
        else err = post_meeting_action(recording_id, "process", payload, 200, 202);
        if (err == ESP_OK) {
            phase = 2;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
        }
    }
    free(buffer);
    fclose(file);
    if (err == ESP_OK) {
        err = clear_meeting_recovery(true);
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "meeting delivered but local cleanup failed: %s", esp_err_to_name(err));
        }
    }
    return err;
}

static void meeting_task(void *arg) {
    bool resume_only = arg != NULL;
    if (resume_only) {
        // Recovery is a background transfer. It must not take over the pet UI,
        // publish an active meeting state, or block a new short voice command.
        ESP_LOGI(TAG, "background meeting resume started");
    } else {
        meeting_set_state(MEETING_STARTING);
        FILE *file = fopen(MEETING_WAV_PATH, "wb+");
        if (!file) {
            meeting_set_state(MEETING_ERROR);
            pet("alert");
            board_port_show_text("录音失败", "无法创建录音文件");
            goto finish;
        }
        uint8_t header[44];
        build_meeting_wav_header(header, 0);
        if (fwrite(header, 1, sizeof(header), file) != sizeof(header) ||
            save_meeting_recovery(true, "", 0, 0) != ESP_OK ||
            board_port_audio_stream_start() != ESP_OK) {
            fclose(file);
            meeting_set_state(MEETING_ERROR);
            pet("alert");
            board_port_show_text("录音失败", "麦克风或存储不可用");
            goto finish;
        }
        int16_t samples[512];
        uint64_t total_samples = 0;
        s_meeting_elapsed_seconds = 0;
        uint32_t last_elapsed = UINT32_MAX;
        meeting_set_state(MEETING_RECORDING);
        pet("listening");
        board_port_set_recording_visual(true, false, 0);
        while (s_meeting_state == MEETING_RECORDING || s_meeting_state == MEETING_PAUSED) {
            size_t count = 0;
            uint16_t level = 0;
            esp_err_t capture = board_port_audio_stream_read(samples, 512, &count, &level);
            if (capture != ESP_OK) {
                meeting_set_state(MEETING_ERROR);
                break;
            }
            bool paused = s_meeting_state == MEETING_PAUSED;
            if (!paused && count > 0) {
                if (fwrite(samples, sizeof(int16_t), count, file) != count) {
                    meeting_set_state(MEETING_ERROR);
                    break;
                }
                total_samples += count;
            }
            uint32_t elapsed = (uint32_t)(total_samples / MEETING_SAMPLE_RATE);
            s_meeting_elapsed_seconds = elapsed;
            board_port_set_audio_level(paused ? 0 : level, elapsed);
            if (elapsed != last_elapsed) {
                board_port_set_recording_visual(true, paused, elapsed);
                last_elapsed = elapsed;
            }
        }
        board_port_audio_stream_stop();
        meeting_state_t stopped_state = s_meeting_state;
        esp_err_t finalize_err = total_samples > 0
                                     ? finalize_meeting_wav(file, total_samples)
                                     : ESP_ERR_INVALID_SIZE;
        if (stopped_state == MEETING_FINALIZING && finalize_err == ESP_OK) {
            fclose(file);
            meeting_set_state(MEETING_UPLOADING);
            pet("thinking");
            board_port_set_recording_visual(false, false, 0);
            board_port_show_text("会议录音", "正在安全上传");
        } else {
            fclose(file);
            if (total_samples == 0) {
                // There is no recoverable audio. Leaving the pending marker set
                // would make every later double press retry a 44-byte placeholder
                // forever, preventing the user from starting a fresh meeting.
                (void)clear_meeting_recovery(true);
            } else if (finalize_err == ESP_OK) {
                ESP_LOGW(TAG, "partial meeting finalized for recovery: samples=%llu",
                         (unsigned long long)total_samples);
            } else {
                // Keep both PCM and recovery metadata. upload_pending_meeting()
                // will retry header repair before it sends any bytes.
                ESP_LOGE(TAG, "partial meeting header finalize failed; preserving PCM: %s",
                         esp_err_to_name(finalize_err));
            }
            board_port_set_recording_visual(false, false, 0);
            meeting_set_state(MEETING_ERROR);
            pet("alert");
            board_port_show_text("录音失败", "文件已保留待恢复");
            goto finish;
        }
    }
    if (upload_pending_meeting(!resume_only) == ESP_OK) {
        if (!resume_only) {
            meeting_set_state(MEETING_DONE);
            pet("done");
            board_port_show_text("会议已保存", "可在文稿库中查看");
            vTaskDelay(pdMS_TO_TICKS(1800));
            pet("idle");
        } else {
            ESP_LOGI(TAG, "background meeting resume delivered");
        }
    } else {
        if (!resume_only) {
            meeting_set_state(MEETING_ERROR);
            pet("alert");
            board_port_show_text("上传未完成", "联网后将自动续传");
        } else {
            ESP_LOGW(TAG, "background meeting resume deferred until next reconnect");
        }
    }
finish:
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_task = NULL;
    s_meeting_task_running = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!resume_only && s_interaction_lock) xSemaphoreGive(s_interaction_lock);
    vTaskDeleteWithCaps(NULL);
}

static bool start_meeting_task(bool resume_only) {
    if (!s_storage_mounted || (!resume_only && !s_meeting_available) || !s_gateway_token[0]) return false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_meeting_task_running) {
        taskEXIT_CRITICAL(&s_task_state_lock);
        return false;
    }
    s_meeting_task_running = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!resume_only && (!s_interaction_lock || xSemaphoreTake(s_interaction_lock, 0) != pdTRUE)) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_task_running = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        return false;
    }
    if (!resume_only) board_port_cancel_ready_prompt();
    TaskHandle_t handle = NULL;
    BaseType_t created = xTaskCreateWithCaps(meeting_task, "maclaw_meeting", 12288,
                                             resume_only ? (void *)1 : NULL, 5, &handle,
                                             MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_task = created == pdPASS ? handle : NULL;
    if (created != pdPASS) s_meeting_task_running = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (created != pdPASS) {
        if (!resume_only) xSemaphoreGive(s_interaction_lock);
        log_heap_snapshot("meeting-task-create-fail");
        return false;
    }
    return true;
}
static void interaction_task(void *arg) {
    (void)arg;
    pet("listening");
    // Immediate visual acknowledgement: the animated waveform starts before
    // codec capture so a button press never appears to have been ignored.
    board_port_set_recording_visual(true, false, 0);
    board_port_show_text("码卡龙", "正在听取语音");
    uint8_t *wav = NULL;
    size_t wav_len = 0;
    esp_err_t err = board_port_capture_wav(&wav, &wav_len);
    if (err != ESP_OK || !wav || wav_len == 0) {
        board_port_set_recording_visual(false, false, 0);
        // Audio is board-specific. Keep the hardware interface useful while
        // the I2S driver is brought up: a button press sends a text probe that
        // exercises the complete ESP 鈫?Hub 鈫?GUI relay.
        if (s_gateway_token[0]) {
            pet("thinking");
            board_port_show_text("码卡龙", "正在检查连接");
            (void)ulTaskNotifyTake(pdTRUE, 0);
            if (send_text_event("Hello from my ESP32-S3 pet. Confirm the Hub relay is online.") == ESP_OK) {
                if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(60000)) == 0) {
                    board_port_show_text("码卡龙", "网关已连接，等待回复");
                }
                pet("idle");
            } else {
                pet("alert");
                board_port_show_text("请求失败", "请检查网关连接");
            }
        } else {
            pet("alert");
            board_port_show_text("麦克风不可用", "语音驱动未配置");
        }
        free(wav);
        finish_interaction_task();
        return;
    }
    if (!s_gateway_token[0]) {
        board_port_show_text("设备配对", "请说出六位配对码");
        err = pair_by_voice(wav, wav_len);
        free(wav);
        board_port_set_recording_visual(false, false, 0);
        if (err == ESP_OK && gateway_handshake() == ESP_OK) {
            if (ensure_gateway_poll_task()) {
                pet("done");
                board_port_show_ready_prompt("配对成功", "点击屏幕后说话");
            } else {
                err = ESP_ERR_NO_MEM;
                pet("alert");
                board_port_show_text("设备启动失败", "无法启动网关轮询");
            }
        }
        else { pet("alert"); board_port_show_text("配对失败", "请生成新的配对码"); }
        finish_interaction_task();
        return;
    }
    // The server is the interaction runtime: it owns ASR, intent routing,
    // authorization, agent/tool execution, IM delivery, and the final reply.
    // The ESP32 only submits a server-owned `voice` media attachment.
    char media_id[96] = {0};
    board_port_set_recording_visual(false, false, 0);
    pet("thinking");
    err = upload_voice(wav, wav_len, media_id, sizeof(media_id));
    free(wav);
    if (err != ESP_OK) { pet("alert"); board_port_show_text("上传失败", "请检查网关语音支持"); finish_interaction_task(); return; }
    err = send_voice_event(media_id);
    if (err != ESP_OK) { pet("alert"); board_port_show_text("码卡龙错误", "请求失败"); finish_interaction_task(); return; }
    board_port_show_text("码卡龙", "正在处理中");
    if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(90000)) == 0) {
        board_port_show_text("等待超时", "没有收到回复");
    }
    pet("done"); vTaskDelay(pdMS_TO_TICKS(800));
    pet("idle");
    finish_interaction_task();
}

static bool start_voice_interaction(bool consume_screen_wake) {
    if (meeting_is_active()) {
        ESP_LOGW(TAG, "voice interaction ignored: meeting transition/upload active");
        return false;
    }
    if (!s_interaction_lock || xSemaphoreTake(s_interaction_lock, 0) != pdTRUE) {
        ESP_LOGW(TAG, "voice interaction ignored: interaction already active");
        return false;
    }
    bool woke_display = board_port_wake_from_idle();
    if (woke_display && consume_screen_wake) {
        xSemaphoreGive(s_interaction_lock);
        board_port_show_ready_prompt("设备已就绪", "请再按一次说话");
        return false;
    }
    board_port_cancel_ready_prompt();
    TaskHandle_t created_handle = NULL;
    // ESP-SR and TLS leave enough total PSRAM but can fragment the internal
    // heap below a contiguous 12 KiB block. The interaction task does not use
    // DMA from its stack, so place that stack in PSRAM and keep only its TCB
    // internal. This makes a screen tap reliable after the wake model starts.
    BaseType_t created = xTaskCreateWithCaps(interaction_task, "maclaw_interaction",
                                             12288, NULL, 5, &created_handle,
                                             MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_interaction_task = created == pdPASS ? created_handle : NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (created != pdPASS) {
        log_heap_snapshot("interaction-task-create-fail");
        xSemaphoreGive(s_interaction_lock);
        pet("alert");
        board_port_show_text("操作失败", "无法启动语音任务");
        return false;
    }
    return true;
}

static void on_wake_word(void *arg) {
    (void)arg;
    EventBits_t wifi = s_wifi_events ? xEventGroupGetBits(s_wifi_events) : 0;
    if (s_setup_portal_active || !s_gateway_token[0] || !(wifi & WIFI_CONNECTED_BIT)) {
        ESP_LOGW(TAG, "offline wake detected but online interaction is unavailable: setup=%s paired=%s wifi=%s",
                 s_setup_portal_active ? "active" : "inactive",
                 s_gateway_token[0] ? "yes" : "no",
                 (wifi & WIFI_CONNECTED_BIT) ? "connected" : "offline");
        return;
    }
    ESP_LOGI(TAG, "offline wake accepted; starting voice interaction");
    (void)start_voice_interaction(false);
}

static void reset_to_setup_portal(void) {
    nvs_handle_t nvs;
    if (nvs_open("maclaw", NVS_READWRITE, &nvs) == ESP_OK) {
        (void)nvs_erase_key(nvs, "wifi_ssid");
        (void)nvs_erase_key(nvs, "wifi_pass");
        (void)nvs_erase_key(nvs, "wifi_sec");
        (void)nvs_erase_key(nvs, "wifi_eap");
        (void)nvs_erase_key(nvs, "wifi_ident");
        (void)nvs_erase_key(nvs, "wifi_user");
        (void)nvs_erase_key(nvs, "wifi_ttls");
        (void)nvs_erase_key(nvs, "wifi_ca");
        (void)nvs_erase_key(nvs, "wifi_domain");
        (void)nvs_erase_key(nvs, "gateway_url");
        (void)nvs_erase_key(nvs, "pair_code");
        (void)nvs_erase_key(nvs, "gateway_token");
        (void)nvs_commit(nvs);
        nvs_close(nvs);
    }
    s_wifi_ssid[0] = '\0';
    s_wifi_password[0] = '\0';
    s_pair_code[0] = '\0';
    s_gateway_token[0] = '\0';
    pet("quiet");
    board_port_show_text("网络已复位", "正在开启设置热点");
    vTaskDelay(pdMS_TO_TICKS(800));
    esp_restart();
}

static void on_user_button(board_port_button_event_t event, void *arg) {
    (void)arg;
    ESP_LOGI(TAG, "button event received: %s",
             event == BOARD_BUTTON_SHORT ? "short" :
             event == BOARD_BUTTON_DOUBLE ? "double" : "long");
    // The setup screen owns both the display and the radio. Treat touch/BOOT
    // input as inert until the submitted form deliberately restarts the
    // device; otherwise a stray tap starts normal voice UI and repaints the
    // QR while the phone is trying to configure the AP.
    if (s_setup_portal_active) {
        ESP_LOGI(TAG, "button ignored while setup portal is active");
        return;
    }
    meeting_state_t meeting = s_meeting_state;
    if (meeting == MEETING_RECORDING || meeting == MEETING_PAUSED) {
        // Stopping must work with the one dependable input this enclosure has:
        // a panel tap. The touch controller does not reliably sustain a long
        // press and users cannot be expected to reproduce a tight double tap
        // while recording. Accept every completed gesture as stop/save.
        // Do not repaint here: this callback runs in the touch scan task and a
        // full LCD DMA present can block it long enough to trip task_wdt. The
        // meeting task observes FINALIZING and owns the following UI updates.
        meeting_set_state(MEETING_FINALIZING);
        ESP_LOGI(TAG, "meeting stop requested: gesture=%s",
                 event == BOARD_BUTTON_SHORT ? "short" :
                 event == BOARD_BUTTON_DOUBLE ? "double" : "long");
        return;
    }
    if (meeting_is_active()) {
        ESP_LOGW(TAG, "button ignored: meeting transition/upload active");
        return;
    }
    if (event == BOARD_BUTTON_LONG) {
        // With no saved credentials there is nothing left to reset.
        if (!s_wifi_ssid[0]) {
            ESP_LOGI(TAG, "long press ignored while setup portal is active");
            return;
        }
        ESP_LOGW(TAG, "long press: clearing saved Wi-Fi and pairing");
        reset_to_setup_portal();
        return;
    }
    if (event == BOARD_BUTTON_DOUBLE) {
        if (s_meeting_pending) {
            if (!start_meeting_task(true)) {
                pet("alert");
                board_port_show_text("续传失败", "请检查网关连接");
            }
            return;
        }
        if (!s_meeting_available) {
            pet("alert");
            board_port_show_text("会议录音不可用", "请升级或配置网关");
            return;
        }
        if (!start_meeting_task(false)) {
            pet("alert");
            board_port_show_text("录音启动失败", "设备正在处理其它操作");
        }
        return;
    }
    if (event != BOARD_BUTTON_SHORT) return;
    // A physical press only wakes a sleeping LCD; the offline wake phrase is
    // hands-free and therefore wakes the panel and records in the same event.
    (void)start_voice_interaction(true);
}
static void init_network(void) {
    if (s_network_initialized) return;
    s_wifi_events = xEventGroupCreate();
    ESP_ERROR_CHECK(esp_netif_init());
    ESP_ERROR_CHECK(esp_event_loop_create_default());
    wifi_init_config_t init = WIFI_INIT_CONFIG_DEFAULT();
    ESP_ERROR_CHECK(esp_wifi_init(&init));
    ESP_ERROR_CHECK(esp_event_handler_instance_register(WIFI_EVENT, ESP_EVENT_ANY_ID, wifi_event, NULL, NULL));
    ESP_ERROR_CHECK(esp_event_handler_instance_register(IP_EVENT, IP_EVENT_STA_GOT_IP, wifi_event, NULL, NULL));
    s_network_initialized = true;
}

static void ensure_setup_ap_netif(void) {
    if (!s_ap_netif_created) {
        s_setup_ap_netif = esp_netif_create_default_wifi_ap();
        s_ap_netif_created = true;
    }
}

static void ensure_station_netif(void) {
    if (!s_sta_netif_created) {
        esp_netif_create_default_wifi_sta();
        s_sta_netif_created = true;
    }
}

static void setup_qrcode_display(esp_qrcode_handle_t qrcode, void *user_data) {
    board_port_show_qrcode(qrcode, user_data ? (const char *)user_data : NULL);
}

static void show_setup_qrcode(const char *ssid) {
    // This is the standard no-password Wi-Fi QR payload, understood by the
    // iOS/Android camera handlers and by WeChat's Wi-Fi scanner.
    char payload[96];
    int length = snprintf(payload, sizeof(payload), "WIFI:T:nopass;S:%s;;", ssid);
    if (length < 0 || length >= sizeof(payload)) {
        ESP_LOGW(TAG, "setup SSID is too long for QR payload");
        return;
    }
    esp_qrcode_config_t config = ESP_QRCODE_CONFIG_DEFAULT();
    config.display_func_with_cb = setup_qrcode_display;
    config.user_data = (void *)ssid;
    config.max_qrcode_version = 5;
    config.qrcode_ecc_level = ESP_QRCODE_ECC_MED;
    esp_err_t err = esp_qrcode_generate(&config, payload);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "cannot generate setup Wi-Fi QR: %s", esp_err_to_name(err));
        board_port_show_text("设备网络设置", ssid);
    }
}

static void configure_setup_dhcp_dns(void) {
    if (!s_setup_ap_netif) return;
    esp_netif_dns_info_t dns = {0};
    if (!inet_aton(SETUP_AP_IP_ADDR, &dns.ip.u_addr.ip4)) {
        ESP_LOGE(TAG, "invalid captive portal IP address: %s", SETUP_AP_IP_ADDR);
        return;
    }
    dns.ip.type = ESP_IPADDR_TYPE_V4;
    uint8_t offer_dns = DHCPS_OFFER_DNS;
    esp_err_t stop_err = esp_netif_dhcps_stop(s_setup_ap_netif);
    if (stop_err != ESP_OK && stop_err != ESP_ERR_ESP_NETIF_DHCP_ALREADY_STOPPED) {
        ESP_LOGW(TAG, "cannot pause DHCP server to configure DNS: %s", esp_err_to_name(stop_err));
        return;
    }
    esp_err_t option_err = esp_netif_dhcps_option(s_setup_ap_netif, ESP_NETIF_OP_SET,
                                                   ESP_NETIF_DOMAIN_NAME_SERVER,
                                                   &offer_dns, sizeof(offer_dns));
    esp_err_t dns_err = option_err == ESP_OK
                            ? esp_netif_set_dns_info(s_setup_ap_netif, ESP_NETIF_DNS_MAIN, &dns)
                            : option_err;
    esp_err_t start_err = esp_netif_dhcps_start(s_setup_ap_netif);
    if (dns_err != ESP_OK || (start_err != ESP_OK && start_err != ESP_ERR_ESP_NETIF_DHCP_ALREADY_STARTED)) {
        ESP_LOGW(TAG, "cannot advertise captive DNS through DHCP: dns=%s start=%s",
                 esp_err_to_name(dns_err), esp_err_to_name(start_err));
    }
}

static void dns_server_task(void *arg) {
    (void)arg;
    int socket_fd = socket(AF_INET, SOCK_DGRAM, IPPROTO_IP);
    if (socket_fd < 0) {
        ESP_LOGE(TAG, "cannot create captive DNS socket: errno=%d", errno);
        s_dns_task = NULL;
        vTaskDelete(NULL);
        return;
    }
    struct sockaddr_in address = {
        .sin_family = AF_INET,
        .sin_port = htons(DNS_PORT),
        .sin_addr.s_addr = htonl(INADDR_ANY),
    };
    struct timeval receive_timeout = {.tv_sec = 1, .tv_usec = 0};
    (void)setsockopt(socket_fd, SOL_SOCKET, SO_RCVTIMEO, &receive_timeout, sizeof(receive_timeout));
    if (bind(socket_fd, (struct sockaddr *)&address, sizeof(address)) < 0) {
        ESP_LOGE(TAG, "cannot bind captive DNS socket: errno=%d", errno);
        close(socket_fd);
        s_dns_task = NULL;
        vTaskDelete(NULL);
        return;
    }
    ESP_LOGI(TAG, "captive DNS is answering all hostnames at %s", SETUP_AP_IP_ADDR);
    while (true) {
        uint8_t packet[DNS_PACKET_CAPACITY];
        struct sockaddr_in source = {0};
        socklen_t source_len = sizeof(source);
        int received = recvfrom(socket_fd, packet, sizeof(packet), 0,
                                (struct sockaddr *)&source, &source_len);
        if (received < 12) continue;
        // Reply to one-question A/IN lookups. Returning the portal IP for each
        // hostname lets Android/iOS/Windows detect the captive portal and open
        // its setup view; HTTPS probes simply fall back to the manual URL.
        uint16_t flags = (uint16_t)((packet[2] << 8) | packet[3]);
        uint16_t questions = (uint16_t)((packet[4] << 8) | packet[5]);
        if ((flags & 0x8000u) || (flags & 0x7800u) != 0 || questions != 1) continue;
        size_t cursor = 12;
        while (cursor < (size_t)received && packet[cursor] != 0) {
            size_t label_len = packet[cursor];
            if (label_len == 0 || label_len > 63 || cursor + label_len >= (size_t)received) break;
            cursor += label_len + 1;
        }
        if (cursor + 5 > (size_t)received || packet[cursor] != 0) continue;
        cursor++;
        uint16_t qtype = (uint16_t)((packet[cursor] << 8) | packet[cursor + 1]);
        uint16_t qclass = (uint16_t)((packet[cursor + 2] << 8) | packet[cursor + 3]);
        cursor += 4;
        if (qtype != 1 || qclass != 1 || cursor + 16 > sizeof(packet)) continue;
        packet[2] = (uint8_t)(0x80u | (flags & 0x01u));
        packet[3] = 0x80; // response, recursion available, no error
        packet[6] = 0; packet[7] = 1;       // one answer
        packet[8] = 0; packet[9] = 0;
        packet[10] = 0; packet[11] = 0;
        packet[cursor++] = 0xC0; packet[cursor++] = 0x0C; // answer name = question name
        packet[cursor++] = 0; packet[cursor++] = 1;        // A
        packet[cursor++] = 0; packet[cursor++] = 1;        // IN
        packet[cursor++] = 0; packet[cursor++] = 0;
        packet[cursor++] = 0; packet[cursor++] = 0; packet[cursor++] = 0; packet[cursor++] = 30;
        packet[cursor++] = 0; packet[cursor++] = 4;
        packet[cursor++] = 192; packet[cursor++] = 168; packet[cursor++] = 4; packet[cursor++] = 1;
        (void)sendto(socket_fd, packet, cursor, 0, (struct sockaddr *)&source, source_len);
    }
}

static void start_captive_dns(void) {
    if (s_dns_task) return;
    BaseType_t created = xTaskCreate(dns_server_task, "maclaw_captive_dns", 3072, NULL, 3, &s_dns_task);
    if (created != pdPASS) {
        s_dns_task = NULL;
        ESP_LOGW(TAG, "cannot start captive DNS task");
    }
}

static esp_err_t setup_get_handler(httpd_req_t *req) {
    // Keep the setup page small and deterministic. The earlier generated page
    // could exceed its fixed stack buffer when many SSIDs were present, which
    // reset the ESP exactly when a phone requested the portal.
    static const char setup_page[] =
        "<!doctype html><html><head><meta name=viewport content='width=device-width,initial-scale=1'>"
        "<style>body{font-family:system-ui,sans-serif;max-width:26rem;margin:2rem auto;padding:0 1rem;color:#102a43}"
        "label{display:block;margin:1rem 0 .3rem}input,select{box-sizing:border-box;width:100%;padding:.7rem;font-size:1rem}"
        ".enterprise{margin-top:1rem;padding:.85rem;border:1px solid #b9c9d7;background:#f5f9fc}.hint{font-size:.85rem;color:#486581;line-height:1.45}"
        "button{margin-top:1.3rem;padding:.8rem 1.2rem;font-size:1rem;background:#1769aa;color:#fff;border:0;border-radius:.4rem}</style>"
        "</head><body><h1>MaClaw Pet setup</h1><p>已连接设备热点。填写家庭或办公 Wi-Fi 后，设备会自动重启并连接。</p>"
        "<form method=post action=/save><label>Wi-Fi name</label><input name=ssid required maxlength=32 autocapitalize=none>"
        "<label>Security</label><select name=security id=security onchange='document.getElementById(\"enterprise\").hidden=this.value!==\"enterprise\";document.getElementById(\"passlabel\").textContent=this.value===\"enterprise\"?\"Password\":\"Wi-Fi password\"'><option value=personal selected>Personal (WPA/WPA2/WPA3)</option><option value=enterprise>Enterprise (802.1X)</option></select>"
        "<label id=passlabel>Wi-Fi password</label><input name=password type=password maxlength=64>"
        "<section class=enterprise id=enterprise hidden><strong>Enterprise Wi-Fi</strong><p class=hint>Defaults match typical phone settings: PEAP, MSCHAPv2, system certificates. Ask your IT administrator only if your network differs.</p>"
        "<label>EAP method</label><select name=eap_method><option value=peap selected>PEAP</option><option value=ttls>TTLS</option></select>"
        "<label>Identity (optional)</label><input name=identity maxlength=127 autocapitalize=none placeholder='Anonymous identity, if required'>"
        "<label>Username</label><input name=username maxlength=127 autocapitalize=none placeholder='Required'>"
        "<label>TTLS inner authentication</label><select name=ttls_phase2><option value=mschapv2 selected>MSCHAPv2 (default)</option><option value=pap>PAP</option></select>"
        "<label>CA certificate</label><select name=ca_mode><option value=system selected>Use system certificates (recommended)</option><option value=none>Do not validate (not recommended)</option></select>"
        "<label>Server domain (optional)</label><input name=server_domain maxlength=127 autocapitalize=none placeholder='Example: radius.company.com'></section>"
        "<label>MaClaw Hub URL</label><input name=gateway value='https://hub.mypapers.top' required maxlength=255>"
        "<label>6-digit pairing code</label><input name=code inputmode=numeric pattern='[0-9]{6}' maxlength=6 required>"
        "<button>Save and connect</button></form></body></html>";
    static const char pairing_page[] =
        "<!doctype html><html><head><meta name=viewport content='width=device-width,initial-scale=1'>"
        "<style>body{font-family:system-ui,sans-serif;max-width:26rem;margin:2rem auto;padding:0 1rem;color:#102a43}"
        ".ok{padding:.8rem;background:#e8f7ef;border-radius:.5rem}label{display:block;margin:1rem 0 .3rem}"
        "input{box-sizing:border-box;width:100%;padding:.8rem;font-size:1.2rem;letter-spacing:.25rem}"
        "button{margin-top:1.3rem;padding:.8rem 1.2rem;font-size:1rem;background:#1769aa;color:#fff;border:0;border-radius:.4rem}</style>"
        "</head><body><h1>Restore MaClaw access</h1><p class=ok>Wi-Fi is connected. The saved device token was rejected by the Hub.</p>"
        "<p>Generate a temporary code in MaClaw GUI. It is used once to retrieve a replacement device token.</p>"
        "<form method=post action=/save><input type=hidden name=reuse value=1>"
        "<label>New 6-digit pairing code</label><input name=code inputmode=numeric pattern='[0-9]{6}' maxlength=6 required autofocus>"
        "<button>Pair this device</button></form></body></html>";
    ESP_LOGI(TAG, "setup portal request: %s", req->uri);
    httpd_resp_set_type(req, "text/html; charset=utf-8");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    const char *page = s_pairing_recovery_portal ? pairing_page : setup_page;
    return httpd_resp_send(req, page, HTTPD_RESP_USE_STRLEN);
}

static esp_err_t captive_redirect_handler(httpd_req_t *req) {
    // A 302 is intentionally used here instead of a successful probe body:
    // the OS then identifies this as a captive network and presents its login
    // surface, which follows the redirect to the configuration page.
    httpd_resp_set_status(req, "302 Found");
    httpd_resp_set_hdr(req, "Location", "http://" SETUP_AP_IP_ADDR "/");
    httpd_resp_set_type(req, "text/html; charset=utf-8");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    return httpd_resp_sendstr(req,
        "<!doctype html><meta http-equiv=refresh content='0;url=http://" SETUP_AP_IP_ADDR "/'>"
        "<a href='http://" SETUP_AP_IP_ADDR "/'>Open MaClaw setup</a>");
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

static esp_err_t setup_save_handler(httpd_req_t *req) {
    char body[1536] = {0}, ssid[WIFI_VALUE_CAPACITY] = {0}, password[WIFI_VALUE_CAPACITY] = {0},
         gateway[URL_CAPACITY] = {0}, code[PAIR_CODE_CAPACITY] = {0}, security[WIFI_EAP_MODE_CAPACITY] = "personal",
         eap_method[WIFI_EAP_MODE_CAPACITY] = "peap", identity[WIFI_ENTERPRISE_VALUE_CAPACITY] = {0},
         username[WIFI_ENTERPRISE_VALUE_CAPACITY] = {0}, ttls_phase2[WIFI_EAP_MODE_CAPACITY] = "mschapv2",
         ca_mode[WIFI_EAP_MODE_CAPACITY] = "system", server_domain[WIFI_ENTERPRISE_VALUE_CAPACITY] = {0};
    if (req->content_len <= 0 || req->content_len >= sizeof(body)) {
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "Form data is too large");
        return ESP_FAIL;
    }
    int received = 0;
    while (received < req->content_len) {
        int n = httpd_req_recv(req, body + received, req->content_len - received);
        if (n <= 0) {
            httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "Could not receive the complete form");
            return ESP_FAIL;
        }
        received += n;
    }
    body[received] = '\0';
    char reuse[4] = {0};
    bool reuse_wifi = form_value(body, "reuse", reuse, sizeof(reuse)) && !strcmp(reuse, "1");
    if (reuse_wifi) {
        strlcpy(ssid, s_wifi_ssid, sizeof(ssid));
        strlcpy(password, s_wifi_password, sizeof(password));
        strlcpy(gateway, s_gateway_url, sizeof(gateway));
        strlcpy(security, s_wifi_security, sizeof(security));
        strlcpy(eap_method, s_wifi_eap_method, sizeof(eap_method));
        strlcpy(identity, s_wifi_identity, sizeof(identity));
        strlcpy(username, s_wifi_username, sizeof(username));
        strlcpy(ttls_phase2, s_wifi_ttls_phase2, sizeof(ttls_phase2));
        strlcpy(ca_mode, s_wifi_ca_mode, sizeof(ca_mode));
        strlcpy(server_domain, s_wifi_server_domain, sizeof(server_domain));
    }
    bool invalid_form = !form_value(body, "code", code, sizeof(code));
    if (!reuse_wifi) {
        invalid_form = invalid_form || !form_value(body, "ssid", ssid, sizeof(ssid)) ||
                       !form_value(body, "password", password, sizeof(password)) ||
                       !form_value(body, "gateway", gateway, sizeof(gateway)) ||
                       !form_value(body, "security", security, sizeof(security));
        if (!strcmp(security, "enterprise")) {
            invalid_form = invalid_form || !form_value(body, "eap_method", eap_method, sizeof(eap_method)) ||
                           !form_value(body, "identity", identity, sizeof(identity)) ||
                           !form_value(body, "username", username, sizeof(username)) ||
                           !form_value(body, "ttls_phase2", ttls_phase2, sizeof(ttls_phase2)) ||
                           !form_value(body, "ca_mode", ca_mode, sizeof(ca_mode)) ||
                           !form_value(body, "server_domain", server_domain, sizeof(server_domain));
        }
    }
    if (invalid_form) {
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "Invalid form: check Wi-Fi and enterprise authentication fields");
        return ESP_FAIL;
    }
    // Recovery changes only the one-time pairing code. Never erase a persisted
    // device token merely because the portal was opened; the code exists only
    // to retrieve a token after authentication has conclusively failed.
    esp_err_t save_err = reuse_wifi ? save_pairing_code_only(code)
                                    : save_device_config(ssid, password, gateway, code, security, eap_method,
                                                         identity, username, ttls_phase2, ca_mode, server_domain);
    if (save_err != ESP_OK) {
        char reason[160];
        if (!ssid[0]) snprintf(reason, sizeof(reason), "Wi-Fi name is required");
        else if (strlen(ssid) > WIFI_SSID_MAX_LEN) snprintf(reason, sizeof(reason), "Wi-Fi name is too long (max 32 bytes)");
        else if (strlen(password) >= sizeof(s_wifi_password)) snprintf(reason, sizeof(reason), "Wi-Fi password is too long (max 64 bytes)");
        else if (!strcmp(security, "enterprise") && !username[0]) snprintf(reason, sizeof(reason), "Enterprise Wi-Fi username is required");
        else if (!is_valid_choice(security, "personal", "enterprise", NULL)) snprintf(reason, sizeof(reason), "Unsupported Wi-Fi security mode");
        else if (!is_valid_gateway_url(gateway)) snprintf(reason, sizeof(reason), "Hub URL must start with http:// or https://");
        else if (!is_six_digit_pair_code(code)) snprintf(reason, sizeof(reason), "Pairing code must be exactly 6 digits");
        else snprintf(reason, sizeof(reason), "Could not save configuration: %s", esp_err_to_name(save_err));
        ESP_LOGW(TAG, "setup rejected: %s", reason);
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, reason);
        return ESP_FAIL;
    }
    httpd_resp_sendstr(req, "Saved. The device is restarting and will connect to MaClaw.");
    vTaskDelay(pdMS_TO_TICKS(500));
    esp_restart();
    return ESP_OK;
}

static void start_setup_portal(bool keep_station) {
    // Set this before any slow display or Wi-Fi operation. A button event can
    // be delivered by its independent task while the QR page is being drawn.
    s_setup_portal_active = true;
    // Provisioning has no use for the always-listening recognizer. Pause it
    // so it cannot compete for audio/I2S work while the captive portal runs.
    board_port_pause_wake_word(true);
    // Pairing recovery arrives here with Wi-Fi already associated, and the
    // offline recognizer has already been allocated. Give the small captive
    // portal its memory back before httpd_start(), otherwise the SoftAP can
    // appear while its configuration page fails to start.
    // Stop in both AP and AP+STA paths. start_setup_portal(false) is also used
    // after a configured station times out, by which point ESP-SR may already
    // be alive in future boot sequencing changes.
    esp_err_t wake_stop_err = board_port_stop_wake_word();
    if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "cannot stop offline wake for setup portal: %s",
                 esp_err_to_name(wake_stop_err));
    }
    uint8_t mac[6];
    ESP_ERROR_CHECK(esp_read_mac(mac, ESP_MAC_WIFI_SOFTAP));
    char ap_ssid[33];
    snprintf(ap_ssid, sizeof(ap_ssid), "MACLAW-SETUP-%02X%02X", mac[4], mac[5]);
    init_network();
    ensure_setup_ap_netif();
    // A failed first-time Wi-Fi join should show the full form again, even
    // though the submitted SSID is now persisted. Pairing recovery is the
    // only flow that intentionally reuses Wi-Fi and asks solely for a code.
    s_pairing_recovery_portal = keep_station;
    // First-time provisioning is AP-only. Pairing recovery uses AP+STA so the
    // known-good Wi-Fi remains online while the phone submits a fresh code.
    wifi_config_t ap = { .ap = { .channel = 1, .max_connection = 4, .authmode = WIFI_AUTH_OPEN } };
    strlcpy((char *)ap.ap.ssid, ap_ssid, sizeof(ap.ap.ssid));
    ap.ap.ssid_len = strlen(ap_ssid);
    esp_err_t portal_err = esp_wifi_set_mode(s_pairing_recovery_portal ? WIFI_MODE_APSTA : WIFI_MODE_AP);
    if (portal_err != ESP_OK) {
        s_setup_portal_active = false;
        ESP_LOGE(TAG, "cannot enter setup Wi-Fi mode: %s", esp_err_to_name(portal_err));
        board_port_show_text("设置失败", "请在网页重新设置");
        return;
    }
    portal_err = esp_wifi_set_config(WIFI_IF_AP, &ap);
    if (portal_err != ESP_OK) {
        s_setup_portal_active = false;
        ESP_LOGE(TAG, "cannot configure setup hotspot: %s", esp_err_to_name(portal_err));
        board_port_show_text("设置失败", "请在网页重新设置");
        return;
    }
    if (!s_wifi_started) {
        portal_err = esp_wifi_start();
        if (portal_err != ESP_OK) {
            s_setup_portal_active = false;
            ESP_LOGE(TAG, "cannot start setup hotspot: %s", esp_err_to_name(portal_err));
            board_port_show_text("设置失败", "请在网页重新设置");
            return;
        }
        s_wifi_started = true;
    }
    // When the radio was already running in STA mode, set_mode(APSTA) and
    // set_config() do not always immediately publish the new SoftAP beacon.
    // Reconnect the AP interface explicitly and verify that it is active.
    if (s_pairing_recovery_portal) {
        esp_err_t connect_err = esp_wifi_connect();
        if (connect_err != ESP_OK && connect_err != ESP_ERR_WIFI_CONN) {
            ESP_LOGW(TAG, "station reconnect while enabling portal: %s", esp_err_to_name(connect_err));
        }
    }
    wifi_mode_t active_mode = WIFI_MODE_NULL;
    portal_err = esp_wifi_get_mode(&active_mode);
    if (portal_err != ESP_OK || (active_mode != WIFI_MODE_AP && active_mode != WIFI_MODE_APSTA)) {
        s_setup_portal_active = false;
        ESP_LOGE(TAG, "setup hotspot did not enter AP mode: err=%s mode=%d",
                 esp_err_to_name(portal_err), (int)active_mode);
        board_port_show_text("设置热点失败", "请重启后再试");
        return;
    }
    // Scanning is intentionally deferred: the stable provisioning portal is more important than a dynamic list.
    httpd_config_t server_config = HTTPD_DEFAULT_CONFIG();
    // ESP-SR consumes a meaningful part of internal RAM. IDF 6 needs more than
    // the default 4 KB while serving the setup form. This task must remain in
    // internal RAM because the handler writes NVS and flash operations disable
    // the external-RAM cache while checking the current task stack.
    server_config.stack_size = 6144;
    // Four captive-check endpoints, the GET wildcard and POST /save.
    // This capacity is checked when routes are registered at runtime.
    server_config.max_uri_handlers = 6;
    // The provisioning page is static and tiny; a single socket is enough for
    // a phone browser and captive-check probe, and saves several server task
    // stacks compared with the desktop-oriented default of 7.
    server_config.max_open_sockets = 3;
    server_config.lru_purge_enable = true;
    // Make the AP behave like a captive portal. Android, iOS and Windows all
    // probe different HTTP paths before showing the setup page; the wildcard
    // returns the same deterministic form for those paths and manual URLs.
    server_config.uri_match_fn = httpd_uri_match_wildcard;
    portal_err = httpd_start(&s_setup_server, &server_config);
    if (portal_err != ESP_OK) {
        s_setup_portal_active = false;
        ESP_LOGE(TAG, "cannot start setup web server: %s, free_heap=%u",
                 esp_err_to_name(portal_err), (unsigned)esp_get_free_heap_size());
        board_port_show_text("设置失败", "网页服务内存不足，请重启");
        return;
    }
    httpd_uri_t apple_success = {.uri = "/hotspot-detect.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_generate_204 = {.uri = "/generate_204", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_gen_204 = {.uri = "/gen_204", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t windows_connect = {.uri = "/connecttest.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t captive = {.uri = "/*", .method = HTTP_GET, .handler = setup_get_handler};
    httpd_uri_t save = {.uri = "/save", .method = HTTP_POST, .handler = setup_save_handler};
    // Register the wildcard last: ESP-IDF preserves registration order during
    // matching, so it must not shadow the platform-specific probe routes.
    portal_err = httpd_register_uri_handler(s_setup_server, &apple_success);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_generate_204);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_gen_204);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &windows_connect);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &captive);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &save);
    if (portal_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register setup portal routes: %s, free_heap=%u",
                 esp_err_to_name(portal_err), (unsigned)esp_get_free_heap_size());
        httpd_stop(s_setup_server);
        s_setup_server = NULL;
        s_setup_portal_active = false;
        board_port_show_text("设置失败", "配置网页路由启动失败");
        return;
    }
    configure_setup_dhcp_dns();
    start_captive_dns();
    if (s_pairing_recovery_portal) {
        board_port_show_text("设备配对设置", ap_ssid);
    } else {
        show_setup_qrcode(ap_ssid);
    }
    ESP_LOGI(TAG, "%s portal ready: join %s and open http://192.168.4.1",
             s_pairing_recovery_portal ? "pairing recovery" : "setup", ap_ssid);
}

static void wifi_event(void *arg, esp_event_base_t base, int32_t id, void *data) {
    (void)arg;
    (void)data;
    if (base == WIFI_EVENT && id == WIFI_EVENT_STA_START) {
        esp_wifi_connect();
        return;
    }
    if (base == WIFI_EVENT && id == WIFI_EVENT_STA_DISCONNECTED) {
        xEventGroupClearBits(s_wifi_events, WIFI_CONNECTED_BIT);
        board_port_set_wifi_status(s_wifi_ssid, false);
        ESP_LOGW(TAG, "Wi-Fi disconnected from %s; retrying", s_wifi_ssid);
        esp_wifi_connect();
        return;
    }
    if (base == IP_EVENT && id == IP_EVENT_STA_GOT_IP) {
        xEventGroupSetBits(s_wifi_events, WIFI_CONNECTED_BIT);
        board_port_set_wifi_status(s_wifi_ssid, true);
        ESP_LOGI(TAG, "Wi-Fi connected to %s", s_wifi_ssid);
    }
}

static bool start_wifi(void) {
    init_network();
    ensure_station_netif();
    bool enterprise = is_enterprise_wifi();
    wifi_config_t config = { .sta = { .threshold.authmode = enterprise ? WIFI_AUTH_WPA2_ENTERPRISE : WIFI_AUTH_WPA2_PSK } };
    strlcpy((char *)config.sta.ssid, s_wifi_ssid, sizeof(config.sta.ssid));
    if (!enterprise) strlcpy((char *)config.sta.password, s_wifi_password, sizeof(config.sta.password));
    ESP_ERROR_CHECK(esp_wifi_set_mode(s_setup_server ? WIFI_MODE_APSTA : WIFI_MODE_STA));
    ESP_ERROR_CHECK(esp_wifi_set_config(WIFI_IF_STA, &config));
    if (enterprise) {
        // Android/iOS-style defaults: PEAP + MSCHAPv2, username as identity
        // when anonymous identity is omitted, and platform trust anchors.
        const char *identity = s_wifi_identity[0] ? s_wifi_identity : s_wifi_username;
        esp_eap_method_t method = !strcmp(s_wifi_eap_method, "ttls") ? ESP_EAP_TYPE_TTLS : ESP_EAP_TYPE_PEAP;
        ESP_ERROR_CHECK(esp_eap_client_set_identity((const unsigned char *)identity, strlen(identity)));
        ESP_ERROR_CHECK(esp_eap_client_set_username((const unsigned char *)s_wifi_username, strlen(s_wifi_username)));
        ESP_ERROR_CHECK(esp_eap_client_set_password((const unsigned char *)s_wifi_password, strlen(s_wifi_password)));
        if (!strcmp(s_wifi_eap_method, "ttls")) {
            ESP_ERROR_CHECK(esp_eap_client_set_ttls_phase2_method(
                !strcmp(s_wifi_ttls_phase2, "pap") ? ESP_EAP_TTLS_PHASE2_PAP : ESP_EAP_TTLS_PHASE2_MSCHAPV2));
        }
        if (!strcmp(s_wifi_ca_mode, "system")) {
            ESP_ERROR_CHECK(esp_eap_client_use_default_cert_bundle(true));
        }
        if (s_wifi_server_domain[0]) {
            ESP_ERROR_CHECK(esp_eap_client_set_domain_name(s_wifi_server_domain));
        }
        ESP_ERROR_CHECK(esp_eap_client_set_eap_methods(method));
        ESP_ERROR_CHECK(esp_wifi_sta_enterprise_enable());
    } else {
        // Reset EAP state before connecting to a regular WPA personal network.
        (void)esp_wifi_sta_enterprise_disable();
    }
    board_port_set_wifi_status(s_wifi_ssid, false);
    if (!s_wifi_started) {
        ESP_ERROR_CHECK(esp_wifi_start());
        s_wifi_started = true;
    } else {
        ESP_ERROR_CHECK(esp_wifi_connect());
    }
    EventBits_t result = xEventGroupWaitBits(s_wifi_events, WIFI_CONNECTED_BIT, pdFALSE, pdTRUE,
                                             pdMS_TO_TICKS(WIFI_CONNECT_TIMEOUT_MS));
    if (result & WIFI_CONNECTED_BIT) return true;
    board_port_set_wifi_status(s_wifi_ssid, false);
    ESP_LOGW(TAG, "Wi-Fi did not connect within %u ms: %s", WIFI_CONNECT_TIMEOUT_MS, s_wifi_ssid);
    return false;
}

static void gateway_startup_task(void *arg) {
    (void)arg;
    // Startup remains the clean ambient pet face. Connection progress belongs
    // in the serial log; it must never cover the clock, weather or pet.
    ESP_LOGI(TAG, "gateway startup: url=%s paired=%s pair_code=%s", s_gateway_url, s_gateway_token[0] ? "yes" : "no", s_pair_code[0] ? "present" : "missing");
    // A pending one-time code always takes precedence. It is consumed exactly
    // once to obtain/replace the durable gateway token, then erased by
    // pair_by_code(). Normal boots with no pending code use only the token.
    if (s_pair_code[0]) {
        pet("thinking");
        board_port_show_text("设备配对", "正在连接码卡龙界面");
        ESP_LOGI(TAG, "gateway pairing request starting");
        uint32_t retry_ms = GATEWAY_RETRY_INITIAL_MS;
        unsigned attempt = 0;
        bool paired = false;
        while (true) {
            ++attempt;
            esp_err_t err = paired ? gateway_handshake() : pair_by_code();
            if (err == ESP_OK) {
                if (!paired) {
                    paired = true;
                    attempt = 0;
                    retry_ms = GATEWAY_RETRY_INITIAL_MS;
                    continue;
                }
                (void)start_gateway_ready_tasks();
                break;
            }
            if (err == ESP_ERR_INVALID_STATE) {
                pet("alert");
                board_port_show_text(paired ? "令牌认证失败" : "配对码已失效",
                                     "请检查或重新配对");
                start_setup_portal(true);
                break;
            }
            // Preserve the pending code/token and the regular display while the
            // Hub or network is temporarily unavailable.
            pet("idle");
            ESP_LOGW(TAG, "gateway %s attempt %u failed: %s; retry in %lu ms",
                     paired ? "handshake" : "pairing", attempt, esp_err_to_name(err),
                     (unsigned long)retry_ms);
            vTaskDelay(pdMS_TO_TICKS(retry_ms));
            if (retry_ms < GATEWAY_RETRY_MAX_MS) {
                retry_ms *= 2;
                if (retry_ms > GATEWAY_RETRY_MAX_MS) retry_ms = GATEWAY_RETRY_MAX_MS;
            }
        }
    } else if (!s_gateway_token[0]) {
        pet("quiet");
        board_port_show_text("设备未配对", "正在开启配对热点");
        start_setup_portal(true);
    } else {
        uint32_t retry_ms = GATEWAY_RETRY_INITIAL_MS;
        unsigned attempt = 0;
        while (true) {
            ++attempt;
            esp_err_t err = gateway_handshake();
            if (err == ESP_OK) {
                (void)start_gateway_ready_tasks();
                break;
            }
            if (err == ESP_ERR_INVALID_STATE) {
                // A 401/403 is not a transient outage: the stored credential
                // was revoked, disabled, or replaced. Keep it persisted for
                // diagnosis and expose recovery; do not confuse a connection
                // failure with permission to erase the device credential.
                ESP_LOGW(TAG, "gateway credential rejected; entering pairing recovery");
                pet("alert");
                board_port_show_text("令牌认证失败", "请检查或重新配对");
                start_setup_portal(true);
                break;
            }
            // Keep the ambient face visible during retry. The actual failure
            // cause is logged with a heap/network snapshot for diagnosis.
            pet("idle");
            ESP_LOGW(TAG, "gateway handshake attempt %u failed: %s; retry in %lu ms",
                     attempt, esp_err_to_name(err), (unsigned long)retry_ms);
            vTaskDelay(pdMS_TO_TICKS(retry_ms));
            if (retry_ms < GATEWAY_RETRY_MAX_MS) {
                retry_ms *= 2;
                if (retry_ms > GATEWAY_RETRY_MAX_MS) retry_ms = GATEWAY_RETRY_MAX_MS;
            }
        }
    }
    s_gateway_task = NULL;
    vTaskDelete(NULL);
}
void app_main(void) {
    esp_err_t nvs_err = nvs_flash_init();
    if (nvs_err == ESP_ERR_NVS_NO_FREE_PAGES || nvs_err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_ERROR_CHECK(nvs_flash_erase());
        nvs_err = nvs_flash_init();
    }
    ESP_ERROR_CHECK(nvs_err);
    ESP_ERROR_CHECK(psa_crypto_init() == PSA_SUCCESS ? ESP_OK : ESP_FAIL);
    (void)mount_meeting_storage();
    load_meeting_recovery();
    s_http_mutex = xSemaphoreCreateMutex();
    ESP_ERROR_CHECK(s_http_mutex ? ESP_OK : ESP_ERR_NO_MEM);
    s_interaction_lock = xSemaphoreCreateMutex();
    ESP_ERROR_CHECK(s_interaction_lock ? ESP_OK : ESP_ERR_NO_MEM);
    load_device_config();
    load_gateway_token();
    load_ambient_weather();
    ESP_ERROR_CHECK(board_port_init(on_user_button, NULL));
    // A clean native pet is the only startup display until Wi-Fi is ready.
    // No transient messages are painted on top of it.
    pet("idle");
    if (!s_wifi_ssid[0]) {
        start_setup_portal(false);
        return;
    }
    // A configured device runs as a normal Wi-Fi station. If it cannot join,
    // retain the submitted values and reopen the normal setup page. Erasing
    // them here turned a transient association failure (or a typo) into a
    // reboot loop and forced the user to type every field again.
    if (!start_wifi()) {
        pet("alert");
        ESP_LOGW(TAG, "saved Wi-Fi could not connect; preserving configuration and reopening setup portal");
        board_port_show_text("网络连接失败", "设置热点已重新开启");
        start_setup_portal(false);
        return;
    }
    // Do not allocate the ESP-SR model while the first TLS pairing/handshake
    // is being established. Both are PSRAM-heavy; starting them concurrently
    // can make mbedtls_ssl_setup() fail with PSA_ERROR_INSUFFICIENT_MEMORY
    // (-0x008D). start_gateway_ready_tasks() starts the listener immediately
    // after the authenticated handshake has released its TLS allocations.
    // Start the local display clock before network handshaking. Otherwise the
    // top status message can remain on screen long enough to make the seconds
    // look frozen even though Wi-Fi has already connected.
    start_clock_sync();
    // Run TLS/HTTP work on core 1. Performing it in the framework main task on
    // core 0 starves that core's interrupt watchdog during TLS initialization.
    BaseType_t created = xTaskCreatePinnedToCore(gateway_startup_task,
                                                "maclaw_gateway_startup",
                                                12288, NULL, 4,
                                                &s_gateway_task, 1);
    if (created != pdPASS) {
        s_gateway_task = NULL;
        pet("alert");
        board_port_show_text("设备启动失败", "无法启动网关任务");
    }
}
