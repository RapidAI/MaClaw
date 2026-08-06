#include <stdio.h>
#include <errno.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/stat.h>
#include <sys/time.h>
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
#include "esp_random.h"
#include "esp_system.h"
#include "esp_spiffs.h"
#include "esp_timer.h"
#include "esp_wifi.h"
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
#include "driver/gpio.h"
#include "ml307_transport.h"
#endif
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
#include "app_ui.h"
#include "alarm_manager.h"
#include "mp3_player.h"
#include "firmware_identity.h"

// Application code owns UI state. Keep board_port_* below limited to the
// audio/input HAL; every display operation is routed through the shared UI
// model so touch and physical-key boards run the same state machine.
#define board_port_set_pet_state app_ui_set_pet_state
#define board_port_set_command_stage app_ui_set_command_stage
#define board_port_set_command_display_lock app_ui_set_command_display_lock
#define board_port_set_command_cancel_enabled app_ui_set_command_cancel_enabled
#define board_port_set_pet_profile app_ui_set_pet_profile
#define board_port_set_pet_asset app_ui_set_pet_asset
#define board_port_set_recording_visual app_ui_set_recording_visual
#define board_port_set_recording_mode app_ui_set_recording_mode
#define board_port_set_audio_level app_ui_set_audio_level
#define board_port_push_recording_pcm app_ui_push_recording_pcm
#define board_port_show_text app_ui_show_text
#define board_port_show_upload_progress app_ui_show_upload_progress
#define board_port_show_response app_ui_show_response
#define board_port_show_response_image app_ui_show_response_image
#define board_port_navigate_response app_ui_navigate_response
#define board_port_dismiss_response app_ui_dismiss_response
#define board_port_cache_glyph app_ui_cache_glyph
#define board_port_show_qrcode app_ui_show_qrcode
#define board_port_show_ready_prompt app_ui_show_ready_prompt
#define board_port_cancel_ready_prompt app_ui_cancel_ready_prompt
#define board_port_wake_from_idle app_ui_wake_from_idle
#define board_port_set_wifi_status app_ui_set_wifi_status
#define board_port_set_service_ready app_ui_set_service_ready
#define board_port_set_ambient app_ui_set_ambient
#define board_port_set_alarm_visual app_ui_set_alarm_visual

#define WIFI_CONNECTED_BIT BIT0
#define WIFI_CONNECT_TIMEOUT_MS 20000
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
#define CELLULAR_CONNECT_TIMEOUT_MS 60000
#define FANGTANG_BOOT_NETWORK_WINDOW_MS 1800
#endif
#define RESPONSE_CAPACITY 16384
#define HANDSHAKE_RESPONSE_CAPACITY 24576
#define HARDWARE_AUDIO_RESPONSE_CAPACITY (512 * 1024 + 1)
#define RESPONSE_IMAGE_MAX_DIMENSION 64
#define RESPONSE_IMAGE_MAX_BYTES (RESPONSE_IMAGE_MAX_DIMENSION * RESPONSE_IMAGE_MAX_DIMENSION * 2)
#define RESPONSE_IMAGE_MIME "application/vnd.maclaw.rgb565be"
#define URL_CAPACITY 256
#define WIFI_VALUE_CAPACITY 65
#define WIFI_SSID_MAX_LEN 32
#define WIFI_ENTERPRISE_VALUE_CAPACITY 128
#define WIFI_EAP_MODE_CAPACITY 12
#define PAIR_CODE_CAPACITY 7
#define DEVICE_ID_CAPACITY 40
#define GATEWAY_RETRY_INITIAL_MS 2000
#define GATEWAY_RETRY_MAX_MS 60000
// Once MultiNet is listening, give the optional boot greeting only a short
// grace period. Hardware/profile messages may precede it in the outgoing
// queue; they must not keep a ready microphone unavailable for another 15 s.
#define STARTUP_WELCOME_TIMEOUT_MS 8000
#define CLOCK_SYNC_WAIT_MS 12000
#define CLOCK_SYNC_RETRY_MS 30000
#define COMMAND_RESULT_PROGRESS_MS 15000
#define COMMAND_CANCEL_WORKER_TIMEOUT_MS 13000
#define COMMAND_CANCEL_ACKNOWLEDGEMENT_MS 450
// Rich text can carry dozens of 24x24 dynamic glyph bitmaps. Field captures
// reached 96 KiB for one (limit=1) item, so size this PSRAM-backed buffer with
// enough headroom to keep the outgoing cursor moving without burdening the
// scarce internal heap used by Wi-Fi/TLS and ESP-SR.
#define OUTGOING_RESPONSE_CAPACITY (256 * 1024)
#define COMMAND_SUBMIT_RETRY_COUNT 3
#define VOICE_UPLOAD_RETRY_COUNT 3
#define MEETING_RESUME_RETRY_INITIAL_MS 5000
#define MEETING_RESUME_RETRY_MAX_MS 300000
#define SETUP_AP_IP_ADDR "192.168.4.1"
#define SETUP_CAPTIVE_PORTAL_URI "http://192.168.4.1/"
#define DNS_PORT 53
#define DNS_PACKET_CAPACITY 512
#define DHCPS_OFFER_DNS 0x02
#define SETUP_SCAN_MAX_APS 24
#define SETUP_SSID_OPTIONS_CAPACITY 6144
#define SETUP_SSID_CHOICES_CAPACITY (SETUP_SCAN_MAX_APS * WIFI_VALUE_CAPACITY)
#define DYNAMIC_GLYPH_BYTES 72
#define DYNAMIC_GLYPH_MAX_PER_MESSAGE 96
#define PET_ASSET_MAX_DIMENSION 256
#define PET_ASSET_MAX_FRAMES 8
#define PET_ASSET_BYTES_PER_PIXEL 3
#define PET_ASSET_DEFAULT_FRAME_MS 450
#define PET_ASSET_MAX_BYTES (PET_ASSET_MAX_DIMENSION * PET_ASSET_MAX_DIMENSION * PET_ASSET_BYTES_PER_PIXEL)
#define PET_ASSET_STARTUP_TRANSACTION_ATTEMPTS 3
#define PET_ASSET_STARTUP_RETRY_DELAY_MS 3000
#define PET_ASSET_CACHE_META_PATH "/storage/pet_asset.meta"
#define PET_ASSET_CACHE_META_TMP_PATH "/storage/pet_asset.meta.tmp"
#define PET_ASSET_CACHE_FRAME_PATH_FORMAT "/storage/pet_asset_%u.rgb565a8"
#define PET_ASSET_CACHE_FRAME_TMP_PATH_FORMAT "/storage/pet_asset_%u.tmp"
#define MEETING_WAV_PATH "/storage/meeting.wav"
#define MEETING_SAMPLE_RATE 16000
#define MEETING_DEFAULT_CHUNK_SIZE (1U << 20)
#define MEETING_MIN_CHUNK_SIZE (64U << 10)
#define MEETING_MAX_CHUNK_SIZE (8U << 20)
#define MEETING_IO_BUFFER_SIZE 16384
#define MEETING_RESPONSE_CAPACITY 2048
#define MEETING_INTERNAL_TLS_RESERVE (16U * 1024U)
#define MEETING_BASE_PATH_CAPACITY 96
#define MEETING_RECORDING_ID_CAPACITY 96

static const char *TAG = "maclaw_client";
// ESP-IDF DHCP server retains this pointer for the duration of the AP. Keep
// it static: a stack buffer would become invalid after portal startup returns.
static const char s_setup_captive_portal_uri[] = SETUP_CAPTIVE_PORTAL_URI;
static EventGroupHandle_t s_wifi_events;
static int64_t s_cursor;
static char s_boot_session_id[33];
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
static char s_device_id[DEVICE_ID_CAPACITY];
static httpd_handle_t s_setup_server;
static bool s_network_initialized;
static bool s_wifi_driver_initialized;
static bool s_ap_netif_created;
static bool s_sta_netif_created;
static bool s_wifi_started;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
static bool s_fangtang_use_cellular;
static TaskHandle_t s_cellular_recovery_task;
#endif
// The provisioning portal needs APSTA mode to scan nearby networks, but a
// first-run portal must not repeatedly attempt an unconfigured STA join.
static bool s_station_auto_connect;
static bool s_station_expected_disconnect;
// ESP-IDF's enterprise Wi-Fi teardown path must only run after enterprise
// mode was actually enabled. Calling it during a cold personal-Wi-Fi boot can
// leave the Wi-Fi driver's scan timer with a stale task notification target,
// which then asserts just after esp_wifi_start().
static bool s_wifi_enterprise_enabled;
static bool s_setup_portal_active;
static esp_netif_t *s_setup_ap_netif;
static TaskHandle_t s_dns_task;
static SemaphoreHandle_t s_setup_options_mutex;
// Provisioning-only scratch storage is allocated when the portal starts. It
// must not permanently shift ESP-IDF's prebuilt Wi-Fi globals in internal
// DRAM during every configured station boot.
static char *s_setup_ssid_options;
static char *s_setup_ssid_choices;
static wifi_ap_record_t *s_setup_scan_records;
static TaskHandle_t s_gateway_task;
static volatile bool s_gateway_startup_running;
// Radio/IP callbacks can run before app_main() has finished the stability-
// sensitive startup boundary (Wi-Fi driver, clock and alarm scheduler).  They
// may only launch TLS/pairing after app_main explicitly opens this gate.
static volatile bool s_gateway_startup_allowed;
static TaskHandle_t s_interaction_task;
static TaskHandle_t s_meeting_task;
static TaskHandle_t s_meeting_resume_supervisor_task;
static volatile bool s_wake_restart_scheduled;
static TaskHandle_t s_meeting_capability_refresh_task;
static bool s_meeting_task_running;
static bool s_pairing_recovery_portal;
static TaskHandle_t s_ambient_task;
static TaskHandle_t s_clock_sync_task;
static TaskHandle_t s_gateway_poll_task;
static TaskHandle_t s_startup_pet_asset_task;
static TaskHandle_t s_setup_restart_task;
static SemaphoreHandle_t s_startup_welcome_done;
static volatile bool s_startup_welcome_gate_active;
static volatile bool s_startup_welcome_timed_out;
// Playback completion and Hub acknowledgement are separate transactions. If
// the ACK request is interrupted after the speaker has already finished, Hub
// legitimately redelivers the same queue entry. Remember that this boot's
// greeting has been consumed so the retry only repairs the ACK and can never
// make the device speak it a second time.
static volatile bool s_startup_welcome_consumed;
static volatile bool s_startup_sequence_complete;
static bool s_handshake_startup_welcome_queued;
static volatile bool s_command_display_locked;
static volatile bool s_command_cancel_requested;
static volatile bool s_command_cancel_enabled;
static bool s_command_cancel_ui_shown;
// The activation down edge is intentionally useful while recording: it stops
// capture immediately instead of waiting for the 500 ms single/double gesture
// decision. Consume the completed gesture from that same physical contact so
// its delayed SHORT can never dismiss the new thinking/result surface or start
// another command. A fresh down edge disarms this one-contact barrier.
static bool s_command_capture_stop_gesture_pending;
static board_input_source_t s_command_capture_stop_source = BOARD_INPUT_SOURCE_UNKNOWN;
#define CANCELLED_REPLY_SLOTS 4
#define COMMAND_REPLY_ID_CAPACITY 96
#define RESULT_SPEECH_IDLE_TIMEOUT_US (5LL * 60LL * 1000000LL)
static char s_active_command_reply_to[COMMAND_REPLY_ID_CAPACITY];
// A terminal text can deliberately precede its TTS parts. Retain only that
// exact correlation and its declared remaining part count after the command
// worker exits, so result-page speech is accepted without admitting stale audio.
// The idle deadline also bounds a partially generated/failed multipart reply;
// each successfully consumed part refreshes it for the next part.
static char s_result_speech_reply_to[COMMAND_REPLY_ID_CAPACITY];
static unsigned s_result_speech_parts_remaining;
static int64_t s_result_speech_deadline_us;
static char s_cancelled_command_reply_to[CANCELLED_REPLY_SLOTS][COMMAND_REPLY_ID_CAPACITY];
static unsigned s_cancelled_command_reply_next;
static int64_t s_ignore_command_input_until_us;
static uint32_t s_interaction_generation;
static uint32_t s_cancel_requested_generation;
static uint32_t s_cancel_ui_ready_generation;
static int64_t s_command_timing_started_us;
static int64_t s_command_timing_capture_done_us;
static int64_t s_command_timing_upload_done_us;
static int64_t s_command_timing_accepted_us;
static int64_t s_command_timing_first_progress_us;
// Foreground traffic must never wait behind the outgoing long poll. Each lane
// owns both its mutex and persistent esp_http_client handle; no handle is ever
// operated by two tasks concurrently.
static SemaphoreHandle_t s_http_mutex;
static esp_http_client_handle_t s_gateway_http_client;
static char s_gateway_http_origin[URL_CAPACITY];
static SemaphoreHandle_t s_gateway_poll_http_mutex;
static esp_http_client_handle_t s_gateway_poll_http_client;
static char s_gateway_poll_http_origin[URL_CAPACITY];
static SemaphoreHandle_t s_gateway_asset_http_mutex;
static esp_http_client_handle_t s_gateway_asset_http_client;
static char s_gateway_asset_http_origin[URL_CAPACITY];
// Protects the foreground client pointer through cancel/cleanup. The general
// HTTP mutex cannot serve this purpose because it remains owned for the whole
// request and cancellation must run concurrently with esp_http_client_perform.
static SemaphoreHandle_t s_foreground_http_client_mutex;
static esp_http_client_handle_t s_foreground_http_client;
static SemaphoreHandle_t s_command_cancel_ui_ready;
static TaskHandle_t s_command_cancel_task;
static SemaphoreHandle_t s_interaction_lock;
static SemaphoreHandle_t s_nvs_mutex;
static portMUX_TYPE s_task_state_lock = portMUX_INITIALIZER_UNLOCKED;
static char s_weather_summary[24];
static char s_weather_location[24];
static int s_weather_temperature_c;
static int64_t s_weather_expires_at_ms;
static bool s_weather_valid;
static void on_wake_word(void *arg);
static void setup_restart_task(void *arg) {
    (void)arg;
    // Let esp_http_server complete the response before tearing down Wi-Fi.
    vTaskDelay(pdMS_TO_TICKS(1200));
    ESP_LOGI(TAG, "setup saved; restarting into normal mode");
    esp_restart();
}
// Once SNTP supplies an epoch, the display advances from ESP32's monotonic
// microsecond counter. This keeps the visible seconds moving independently of
// network timing and avoids a network request or SNTP poll per screen update.
static time_t s_display_clock_epoch;
static int64_t s_display_clock_anchor_us;
static bool s_display_clock_valid;
static volatile bool s_clock_sync_complete;

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
// Set as soon as a short voice command is requested. Background meeting
// recovery yields between chunks so the interactive upload gets the HTTP lock.
static volatile bool s_foreground_http_requested;

// Hardware gestures are interpreted from this application-owned foreground
// phase, never from whatever screen happened to be painted most recently.
// In particular, a SECONDARY gesture during a voice command must not become a
// meeting request just because the interaction task is between creation and
// publishing its task handle.
typedef enum {
    INTERACTION_IDLE = 0,
    INTERACTION_RECORDING,
    INTERACTION_PROCESSING,
    INTERACTION_RESULT,
} interaction_phase_t;

static volatile interaction_phase_t s_interaction_phase = INTERACTION_IDLE;

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
static esp_err_t send_text_event(const char *text, const char *reply_to);
static bool hardware_audio_url_allowed(const char *url);
static void digest_hex(const uint8_t digest[32], char out[65]);
static esp_err_t handle_client_tool_call(cJSON *item);
static void response_release(http_response_t *response);
static esp_err_t request(const char *method, const char *path, const char *content_type,
                         const char *body, int body_len, http_response_t *out);
static const char *json_string(cJSON *root, const char *key);
static bool json_number(cJSON *root, const char *key, int *value);
static int apply_glyphs_json(cJSON *glyphs);
static bool start_meeting_task(bool resume_only);
static esp_err_t gateway_handshake(bool cold_start);
static void start_setup_portal(bool keep_station);
static void schedule_wake_restart(void);
static esp_err_t clear_meeting_recovery(bool delete_audio);
static void pet(const char *state);
static void apply_deferred_startup_pet_asset(void);
static bool start_gateway_startup_task(void);

static esp_err_t handle_client_tool_call(cJSON *item) {
    cJSON *call = cJSON_GetObjectItemCaseSensitive(item, "toolCall");
    const char *call_id = json_string(call, "id");
    const char *name = json_string(call, "name");
    const char *idempotency_key = json_string(call, "idempotencyKey");
    const char *conversation_id = json_string(item, "conversationId");
    cJSON *arguments = cJSON_GetObjectItemCaseSensitive(call, "arguments");
    if (!cJSON_IsObject(call) || !call_id || !name) return ESP_ERR_INVALID_ARG;
    bool missing_idempotency_key = !idempotency_key || !idempotency_key[0];
    bool invalid_arguments = arguments && !cJSON_IsObject(arguments);
    bool owned_arguments = false;
    if (!arguments) {
        arguments = cJSON_CreateObject();
        owned_arguments = true;
    }
    cJSON *result = NULL;
    char detail[128] = {0};
    esp_err_t execute_err;
    if (missing_idempotency_key) {
        snprintf(detail, sizeof(detail), "idempotencyKey is required");
        execute_err = ESP_ERR_INVALID_ARG;
    } else if (invalid_arguments) {
        snprintf(detail, sizeof(detail), "arguments must be an object");
        execute_err = ESP_ERR_INVALID_ARG;
    } else if (!arguments) {
        snprintf(detail, sizeof(detail), "cannot allocate arguments object");
        execute_err = ESP_ERR_NO_MEM;
    } else {
        execute_err = alarm_manager_execute_tool(name, arguments, idempotency_key,
                                                 &result, detail, sizeof(detail));
    }
    if (owned_arguments) cJSON_Delete(arguments);
    ESP_LOGI(TAG, "client tool executed: name=%s call=%s status=%s",
             name, call_id, execute_err == ESP_OK ? "succeeded" : "failed");

    cJSON *body = cJSON_CreateObject();
    if (!body) {
        cJSON_Delete(result);
        return ESP_ERR_NO_MEM;
    }
    cJSON_AddStringToObject(body, "clientId", s_device_id);
    cJSON_AddStringToObject(body, "resultId", call_id);
    cJSON_AddStringToObject(body, "toolCallId", call_id);
    cJSON_AddStringToObject(body, "conversationId", conversation_id && conversation_id[0] ? conversation_id : "default");
    if (!missing_idempotency_key) cJSON_AddStringToObject(body, "idempotencyKey", idempotency_key);
    if (execute_err == ESP_OK) {
        cJSON_AddStringToObject(body, "status", "succeeded");
        cJSON_AddItemToObject(body, "result", result);
        result = NULL;
    } else {
        cJSON_AddStringToObject(body, "status", "failed");
        cJSON *error = cJSON_AddObjectToObject(body, "error");
        bool persistent_capacity_error = execute_err == ESP_ERR_NO_MEM &&
                                         (strstr(detail, "alarm capacity") != NULL ||
                                          strstr(detail, "persistent replay capacity") != NULL);
        const char *error_code = execute_err == ESP_ERR_NOT_SUPPORTED ? "unknown_tool" :
                                 execute_err == ESP_ERR_TIMEOUT ? "device_busy" :
                                 persistent_capacity_error ? "capacity_exhausted" :
                                 execute_err == ESP_ERR_NO_MEM ? "device_busy" :
                                 execute_err == ESP_ERR_INVALID_ARG ? "invalid_arguments" :
                                 "device_error";
        cJSON_AddStringToObject(error, "code", error_code);
        cJSON_AddStringToObject(error, "message", detail[0] ? detail : esp_err_to_name(execute_err));
        cJSON_AddBoolToObject(error, "retryable",
                              execute_err == ESP_ERR_TIMEOUT ||
                              (execute_err == ESP_ERR_NO_MEM && !persistent_capacity_error) ||
                              (execute_err != ESP_ERR_NOT_SUPPORTED &&
                               execute_err != ESP_ERR_INVALID_ARG &&
                               execute_err != ESP_ERR_NO_MEM));
    }
    char *payload = cJSON_PrintUnformatted(body);
    cJSON_Delete(body);
    cJSON_Delete(result);
    if (!payload) return ESP_ERR_NO_MEM;
    http_response_t response;
    esp_err_t err = request("POST", "/api/im-gateway/v1/tool-result", "application/json",
                            payload, strlen(payload), &response);
    free(payload);
    if (err == ESP_OK && response.status != 200 && response.status != 202 && response.status != 204) err = ESP_FAIL;
    ESP_LOGI(TAG, "client tool result delivered: name=%s call=%s http=%d err=%s",
             name, call_id, response.status, esp_err_to_name(err));
    response_release(&response);
    return err;
}

static bool nvs_lock(void) {
    return s_nvs_mutex && xSemaphoreTake(s_nvs_mutex, pdMS_TO_TICKS(3000)) == pdTRUE;
}

static void nvs_unlock(void) {
    if (s_nvs_mutex) xSemaphoreGive(s_nvs_mutex);
}

static bool meeting_is_active(void) {
    meeting_state_t state = s_meeting_state;
    return state != MEETING_IDLE && state != MEETING_DONE && state != MEETING_ERROR;
}

static void meeting_set_state(meeting_state_t state) {
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_state = state;
    taskEXIT_CRITICAL(&s_task_state_lock);
}
static void finish_interaction_task_with_surface(uint32_t generation,
                                                 bool restore_standby) {
    board_port_set_command_cancel_enabled(false);
    taskENTER_CRITICAL(&s_task_state_lock);
    bool owns_interaction = s_interaction_generation == generation &&
                            s_interaction_task == xTaskGetCurrentTaskHandle();
    if (owns_interaction) {
        s_interaction_task = NULL;
        s_interaction_phase = restore_standby ? INTERACTION_IDLE : INTERACTION_RESULT;
        s_foreground_http_requested = false;
        s_command_cancel_enabled = false;
        s_command_cancel_requested = false;
        s_cancel_requested_generation = 0;
        s_active_command_reply_to[0] = '\0';
        if (restore_standby) {
            s_result_speech_reply_to[0] = '\0';
            s_result_speech_parts_remaining = 0;
            s_result_speech_deadline_us = 0;
            s_command_display_locked = false;
        }
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (owns_interaction && restore_standby) {
        // A cancelled command ends on APP_UI_SURFACE_MESSAGE ("已取消"), so the
        // normal response-only dismiss path cannot restore the ambient screen.
        // Restore the whole shared UI model before admitting another command.
        app_ui_restore_standby();
        ESP_LOGI(TAG, "cancelled command returned to standby: generation=%lu",
                 (unsigned long)generation);
    }
    if (!owns_interaction) {
        ESP_LOGW(TAG, "stale interaction finish ignored: generation=%lu current=%lu",
                 (unsigned long)generation, (unsigned long)s_interaction_generation);
        vTaskDelete(NULL);
        return;
    }
    // This is a binary admission token, not a mutex: the button task starts
    // the interaction task, which completes it on another task context.
    // Releasing a FreeRTOS mutex from that child task asserts and reboots.
    if (owns_interaction && s_interaction_lock) xSemaphoreGive(s_interaction_lock);
    // The interaction worker now uses ordinary xTaskCreate() with an internal
    // RAM stack, so it must be destroyed by the matching FreeRTOS API.
    // vTaskDeleteWithCaps() asserts when given a normally allocated task.
    if (owns_interaction) schedule_wake_restart();
    vTaskDelete(NULL);
}

static uint32_t elapsed_ms_between(int64_t started_us, int64_t finished_us) {
    return started_us > 0 && finished_us >= started_us
               ? (uint32_t)((finished_us - started_us) / 1000)
               : 0;
}

static void log_command_timing(const char *terminal) {
    int64_t now_us = esp_timer_get_time();
    ESP_LOGI(TAG,
             "command timing: terminal=%s capture=%ums upload=%ums submit=%ums firstProgress=%ums total=%ums",
             terminal ? terminal : "unknown",
             (unsigned)elapsed_ms_between(s_command_timing_started_us,
                                          s_command_timing_capture_done_us),
             (unsigned)elapsed_ms_between(s_command_timing_capture_done_us,
                                          s_command_timing_upload_done_us),
             (unsigned)elapsed_ms_between(s_command_timing_upload_done_us,
                                          s_command_timing_accepted_us),
             (unsigned)elapsed_ms_between(s_command_timing_accepted_us,
                                          s_command_timing_first_progress_us),
             (unsigned)elapsed_ms_between(s_command_timing_started_us, now_us));
}

static void finish_interaction_task(uint32_t generation) {
    finish_interaction_task_with_surface(generation, false);
}

// A local validation, capture, upload, or submission failure has no result to
// keep on screen.  Treat it like Bread Compact's short status acknowledgement:
// leave the message readable, then return every layer (application model,
// board renderer, and command admission state) to the ambient pet surface.
// Final remote replies deliberately continue to use finish_interaction_task(),
// so a user can read and dismiss them explicitly.
static void finish_interaction_message(uint32_t generation, uint32_t dwell_ms) {
    if (dwell_ms) vTaskDelay(pdMS_TO_TICKS(dwell_ms));
    finish_interaction_task_with_surface(generation, true);
}

static bool command_cancel_requested_for(uint32_t generation) {
    bool requested;
    taskENTER_CRITICAL(&s_task_state_lock);
    requested = s_command_cancel_requested &&
                s_cancel_requested_generation == generation;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return requested;
}

static void remember_cancelled_command_reply(void) {
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_active_command_reply_to[0]) {
        bool already_remembered = false;
        for (unsigned i = 0; i < CANCELLED_REPLY_SLOTS; ++i) {
            if (!strcmp(s_cancelled_command_reply_to[i], s_active_command_reply_to)) {
                already_remembered = true;
                break;
            }
        }
        if (!already_remembered) {
            strlcpy(s_cancelled_command_reply_to[s_cancelled_command_reply_next],
                    s_active_command_reply_to, COMMAND_REPLY_ID_CAPACITY);
            s_cancelled_command_reply_next =
                (s_cancelled_command_reply_next + 1) % CANCELLED_REPLY_SLOTS;
        }
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
}

static bool cancelled_command_reply_matches(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return false;
    bool matches = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    for (unsigned i = 0; i < CANCELLED_REPLY_SLOTS; ++i) {
        if (s_cancelled_command_reply_to[i][0] &&
            !strcmp(s_cancelled_command_reply_to[i], reply_to)) {
            matches = true;
            break;
        }
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    return matches;
}

static bool active_command_reply_matches(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return false;
    bool matches;
    taskENTER_CRITICAL(&s_task_state_lock);
    matches = s_interaction_task != NULL && s_active_command_reply_to[0] &&
              !strcmp(s_active_command_reply_to, reply_to);
    taskEXIT_CRITICAL(&s_task_state_lock);
    return matches;
}
static bool result_speech_reply_matches(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return false;
    bool matches = false;
    bool expired = false;
    unsigned expired_parts = 0;
    char expired_reply_to[COMMAND_REPLY_ID_CAPACITY] = {0};
    int64_t now_us = esp_timer_get_time();
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_result_speech_parts_remaining > 0 &&
        s_result_speech_reply_to[0] &&
        s_result_speech_deadline_us > 0 &&
        now_us >= s_result_speech_deadline_us) {
        expired = true;
        expired_parts = s_result_speech_parts_remaining;
        strlcpy(expired_reply_to, s_result_speech_reply_to,
                sizeof(expired_reply_to));
        s_result_speech_reply_to[0] = '\0';
        s_result_speech_parts_remaining = 0;
        s_result_speech_deadline_us = 0;
    } else {
        matches = s_result_speech_parts_remaining > 0 &&
                  s_result_speech_reply_to[0] &&
                  !strcmp(s_result_speech_reply_to, reply_to);
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (expired) {
        ESP_LOGW(TAG, "result speech expired after idle timeout: replyTo=%s missing=%u next=%s",
                 expired_reply_to, expired_parts, reply_to);
    }
    return matches;
}

static bool command_timing_matches(const char *reply_to) {
    bool matches = false;
    if (!reply_to || !reply_to[0]) return false;
    taskENTER_CRITICAL(&s_task_state_lock);
    matches = s_active_command_reply_to[0] &&
              !strcmp(s_active_command_reply_to, reply_to);
    taskEXIT_CRITICAL(&s_task_state_lock);
    return matches;
}

static void remember_result_speech_reply(const char *reply_to, unsigned parts) {
    if (!reply_to || !reply_to[0] || parts == 0) return;
    int64_t deadline_us = esp_timer_get_time() + RESULT_SPEECH_IDLE_TIMEOUT_US;
    taskENTER_CRITICAL(&s_task_state_lock);
    strlcpy(s_result_speech_reply_to, reply_to, sizeof(s_result_speech_reply_to));
    s_result_speech_parts_remaining = parts;
    s_result_speech_deadline_us = deadline_us;
    taskEXIT_CRITICAL(&s_task_state_lock);
    ESP_LOGI(TAG, "result speech armed: replyTo=%s parts=%u idleTimeout=%us",
             reply_to, parts,
             (unsigned)(RESULT_SPEECH_IDLE_TIMEOUT_US / 1000000LL));
}

static void finish_result_speech_part(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return;
    unsigned remaining = 0;
    int64_t next_deadline_us = esp_timer_get_time() + RESULT_SPEECH_IDLE_TIMEOUT_US;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_result_speech_parts_remaining > 0 &&
        !strcmp(s_result_speech_reply_to, reply_to)) {
        --s_result_speech_parts_remaining;
        remaining = s_result_speech_parts_remaining;
        if (remaining == 0) {
            s_result_speech_reply_to[0] = '\0';
            s_result_speech_deadline_us = 0;
        } else {
            s_result_speech_deadline_us = next_deadline_us;
        }
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    ESP_LOGI(TAG, "result speech part complete: replyTo=%s remaining=%u",
             reply_to, remaining);
}

static unsigned outgoing_pending_speech_parts(cJSON *item) {
    if (!cJSON_IsObject(item)) return 0;
    cJSON *sources[3] = {
        cJSON_GetObjectItemCaseSensitive(item, "metadata"),
        cJSON_GetObjectItemCaseSensitive(item, "extra"),
        item,
    };
    for (unsigned i = 0; i < 3; ++i) {
        if (!cJSON_IsObject(sources[i])) continue;
        cJSON *value = cJSON_GetObjectItemCaseSensitive(
            sources[i], "speech_parts_pending");
        if (cJSON_IsNumber(value) && value->valuedouble > 0 &&
            value->valuedouble <= 1000) {
            return (unsigned)value->valuedouble;
        }
        if (cJSON_IsString(value) && value->valuestring) {
            char *end = NULL;
            errno = 0;
            unsigned long parsed = strtoul(value->valuestring, &end, 10);
            if (errno == 0 && end != value->valuestring && *end == '\0' &&
                parsed > 0 && parsed <= 1000) {
                return (unsigned)parsed;
            }
        }
    }
    return 0;
}

static const char *outgoing_reply_correlation(cJSON *item) {
    if (!cJSON_IsObject(item)) return NULL;
    const char *value = json_string(item, "replyTo");
    if (!value || !value[0]) value = json_string(item, "replyToMessageId");
    if (!value || !value[0]) value = json_string(item, "source_message_id");
    if (!value || !value[0]) value = json_string(item, "sourceMessageId");
    if (!value || !value[0]) value = json_string(item, "sourceMessageID");
    cJSON *metadata = cJSON_GetObjectItemCaseSensitive(item, "metadata");
    if ((!value || !value[0]) && cJSON_IsObject(metadata)) {
        value = json_string(metadata, "replyTo");
        if (!value || !value[0]) value = json_string(metadata, "replyToMessageId");
        if (!value || !value[0]) value = json_string(metadata, "source_message_id");
        if (!value || !value[0]) value = json_string(metadata, "sourceMessageId");
        if (!value || !value[0]) value = json_string(metadata, "sourceMessageID");
    }
    cJSON *extra = cJSON_GetObjectItemCaseSensitive(item, "extra");
    if ((!value || !value[0]) && cJSON_IsObject(extra)) {
        value = json_string(extra, "replyTo");
        if (!value || !value[0]) value = json_string(extra, "replyToMessageId");
        if (!value || !value[0]) value = json_string(extra, "source_message_id");
        if (!value || !value[0]) value = json_string(extra, "sourceMessageId");
        if (!value || !value[0]) value = json_string(extra, "sourceMessageID");
    }
    return value;
}

static bool outgoing_message_is_progress(cJSON *item) {
    cJSON *progress = cJSON_GetObjectItemCaseSensitive(item, "progress");
    if (cJSON_IsTrue(progress)) return true;
    cJSON *metadata = cJSON_GetObjectItemCaseSensitive(item, "metadata");
    if (cJSON_IsObject(metadata)) {
        const char *turn = json_string(metadata, "acp_turn");
        if (turn && (!strcasecmp(turn, "progress") || !strcasecmp(turn, "working"))) return true;
    }
    return false;
}

static bool outgoing_message_is_final(cJSON *item) {
    if (!cJSON_IsObject(item)) return false;
    cJSON *metadata = cJSON_GetObjectItemCaseSensitive(item, "metadata");
    const char *turn = cJSON_IsObject(metadata) ? json_string(metadata, "acp_turn") : NULL;
    if (!turn) turn = json_string(item, "acp_turn");
    if (turn && (!strcasecmp(turn, "final") || !strcasecmp(turn, "complete") ||
                 !strcasecmp(turn, "completed"))) return true;
    cJSON *final = cJSON_GetObjectItemCaseSensitive(item, "final");
    if (cJSON_IsTrue(final)) return true;
    cJSON *complete = cJSON_GetObjectItemCaseSensitive(item, "complete");
    if (cJSON_IsTrue(complete)) return true;
    if (cJSON_IsObject(metadata)) {
        final = cJSON_GetObjectItemCaseSensitive(metadata, "final");
        complete = cJSON_GetObjectItemCaseSensitive(metadata, "complete");
        return cJSON_IsTrue(final) || cJSON_IsTrue(complete);
    }
    return false;
}

// The outgoing poll can resume as soon as the POST releases the shared HTTP
// lock. On a very fast reply it may therefore see the result during the few
// scheduler ticks in which interaction_task is still parsing/publishing the
// returned maclawMessageId. Give that correlation hand-off a short bounded
// grace period instead of acknowledging and losing the result as unrelated.
static bool active_command_reply_matches_after_handoff(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return false;
    for (unsigned attempt = 0; attempt < 20; ++attempt) {
        if (active_command_reply_matches(reply_to)) return true;
        bool awaiting_correlation;
        taskENTER_CRITICAL(&s_task_state_lock);
        awaiting_correlation = s_interaction_task != NULL &&
                               s_interaction_phase == INTERACTION_PROCESSING &&
                               !s_command_cancel_requested &&
                               !s_active_command_reply_to[0];
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (!awaiting_correlation) break;
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    return active_command_reply_matches(reply_to);
}

static TaskHandle_t begin_active_command_reply(void) {
    // Atomically close the cancellation window and take a stable waiter
    // snapshot before drawing. A simultaneous double tap then observes either
    // a cancellable command or a completed one, never a half-transition.
    TaskHandle_t waiter = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (!s_command_cancel_requested) {
        s_command_cancel_enabled = false;
        waiter = s_interaction_task;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    board_port_set_command_cancel_enabled(false);
    return waiter;
}

// Publish the terminal screen and wake the interaction task as one ordered UI
// transition. The waiter refreshes "远端处理中" every 15 seconds; notifying it
// only after a potentially slow full-frame LCD transfer lets that refresh race
// with the result draw and cover a reply that is already stored underneath.
// Once notified first, the worker exits its refresh loop and cannot repaint the
// processing surface while the poller commits the response page.
static void complete_active_command_text_reply(TaskHandle_t waiter,
                                               const char *title,
                                               const char *text) {
    if (!waiter) return;
    ESP_LOGI(TAG, "terminal text transition: waiter=%p bytes=%u", waiter,
             (unsigned)(text ? strlen(text) : 0));
    xTaskNotifyGive(waiter);
    // The notification only makes the higher-priority interaction worker
    // runnable; it may not actually leave the timed wait before this poll task
    // reaches the LCD.  Clear the thinking surface synchronously as part of
    // the terminal transition so its mouth animator is unable to repaint over
    // the first result frame even under TLS/HTTP load.
    pet("speaking");
    board_port_show_response(title, text);
}

static void complete_active_command_image_reply(TaskHandle_t waiter,
                                                const char *title,
                                                const char *caption,
                                                const uint16_t *pixels,
                                                size_t width,
                                                size_t height) {
    if (!waiter) return;
    ESP_LOGI(TAG, "terminal image transition: waiter=%p size=%ux%u", waiter,
             (unsigned)width, (unsigned)height);
    xTaskNotifyGive(waiter);
    pet("speaking");
    board_port_show_response_image(title, caption, pixels, width, height);
}

static void show_cancelled_command(uint32_t generation) {
    taskENTER_CRITICAL(&s_task_state_lock);
    bool cancellation_still_active = s_command_cancel_requested &&
                                     s_cancel_requested_generation == generation &&
                                     s_interaction_generation == generation;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!cancellation_still_active) return;
    remember_cancelled_command_reply();
    board_port_set_command_cancel_enabled(false);
    bool should_draw = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    // Let CST816 finish reporting the second contact before a SHORT gesture is
    // allowed to start another recording. This guard complements the board
    // driver's raw-event drain and also covers the physical BOOT button.
    s_ignore_command_input_until_us = esp_timer_get_time() + 1200000;
    if (!s_command_cancel_ui_shown) {
        s_command_cancel_ui_shown = true;
        should_draw = true;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (should_draw) {
        board_port_show_text("已取消", "本次操作已停止");
        ESP_LOGI(TAG, "voice command cancelled by double tap");
    }
}

static void finish_cancelled_command(uint32_t generation) {
    // The high-priority cancellation worker owns LCD rendering so the touch
    // scanner never blocks on a full display transfer. Wait briefly for that
    // final state before releasing the interaction token; this also prevents a
    // delayed cancellation frame from overwriting the next command screen.
    if (s_command_cancel_ui_ready) {
        TickType_t started = xTaskGetTickCount();
        bool worker_finished = false;
        while ((xTaskGetTickCount() - started) <
               pdMS_TO_TICKS(COMMAND_CANCEL_WORKER_TIMEOUT_MS)) {
            if (xSemaphoreTake(s_command_cancel_ui_ready, pdMS_TO_TICKS(50)) == pdTRUE) {
                taskENTER_CRITICAL(&s_task_state_lock);
                bool ready_for_this_command = s_cancel_ui_ready_generation == generation;
                taskEXIT_CRITICAL(&s_task_state_lock);
                if (ready_for_this_command) {
                    worker_finished = true;
                    break;
                }
            }
        }
        if (!worker_finished) {
            ESP_LOGW(TAG, "command cancellation worker timed out: generation=%lu",
                     (unsigned long)generation);
        }
    }
    if (command_cancel_requested_for(generation)) show_cancelled_command(generation);
    // Keep the acknowledgement long enough to be perceived, then perform one
    // explicit cancel -> idle transition. The gesture input guard remains in
    // force, so the second contact cannot immediately start another command.
    vTaskDelay(pdMS_TO_TICKS(COMMAND_CANCEL_ACKNOWLEDGEMENT_MS));
    finish_interaction_task_with_surface(generation, true);
}

static void command_cancel_worker(void *arg) {
    (void)arg;
    while (true) {
        (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);

        uint32_t cancel_generation = 0;
        taskENTER_CRITICAL(&s_task_state_lock);
        cancel_generation = s_cancel_requested_generation;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (!cancel_generation) continue;

        bool cancellation_still_active;
        taskENTER_CRITICAL(&s_task_state_lock);
        cancellation_still_active = s_command_cancel_requested &&
                                    s_cancel_requested_generation == cancel_generation &&
                                    s_interaction_generation == cancel_generation;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (!cancellation_still_active) continue;

        show_cancelled_command(cancel_generation);

        // Hold the pointer guard for the entire cancel call. The request task
        // must acquire the same guard before clearing/cleaning the handle, so
        // this can never race esp_http_client_cleanup() or dereference a stale
        // client pointer.
        if (s_foreground_http_client_mutex &&
            xSemaphoreTake(s_foreground_http_client_mutex, pdMS_TO_TICKS(1000)) == pdTRUE) {
            esp_http_client_handle_t http_client = s_foreground_http_client;
            if (http_client) {
                esp_err_t cancel_err = esp_http_client_cancel_request(http_client);
                if (cancel_err != ESP_OK) {
                    ESP_LOGW(TAG, "foreground HTTP cancel failed: %s",
                             esp_err_to_name(cancel_err));
                }
            }
            xSemaphoreGive(s_foreground_http_client_mutex);
        } else {
            ESP_LOGW(TAG, "foreground HTTP cancel skipped: client guard timeout");
        }
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        if (s_fangtang_use_cellular && ml307_transport_cancel_foreground()) {
            ESP_LOGI(TAG, "foreground ML307 HTTP request cancelled");
        }
#endif

        // Local cancellation stops waiting immediately, but the server-side
        // agent may already be executing after accepting the voice event. Send
        // the protocol's normal /cancel command before releasing the local
        // interaction token so it cannot accidentally target a newer command.
        taskENTER_CRITICAL(&s_task_state_lock);
        cancellation_still_active = s_command_cancel_requested &&
                                    s_cancel_requested_generation == cancel_generation &&
                                    s_interaction_generation == cancel_generation;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_gateway_token[0] && cancellation_still_active) {
            char cancelled_reply_to[COMMAND_REPLY_ID_CAPACITY] = {0};
            taskENTER_CRITICAL(&s_task_state_lock);
            strlcpy(cancelled_reply_to, s_active_command_reply_to,
                    sizeof(cancelled_reply_to));
            taskEXIT_CRITICAL(&s_task_state_lock);
            esp_err_t server_cancel_err = send_text_event(
                "/cancel", cancelled_reply_to[0] ? cancelled_reply_to : NULL);
            if (server_cancel_err != ESP_OK) {
                ESP_LOGW(TAG, "server command cancel failed: %s",
                         esp_err_to_name(server_cancel_err));
            } else {
                ESP_LOGI(TAG, "server command cancel accepted");
            }
        }

        taskENTER_CRITICAL(&s_task_state_lock);
        s_cancel_ui_ready_generation = cancel_generation;
        TaskHandle_t waiter = NULL;
        if (s_command_cancel_requested &&
            s_cancel_requested_generation == cancel_generation) {
            waiter = s_interaction_task;
        }
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_command_cancel_ui_ready) xSemaphoreGive(s_command_cancel_ui_ready);
        if (waiter) xTaskNotifyGive(waiter);
    }
}

static bool request_command_cancel(void) {
    TaskHandle_t waiter = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    // Cancellation belongs strictly to the thinking phase. Once the poller has
    // accepted a result it clears this flag before drawing the answer, so a
    // late double tap cannot replace a completed command with “已取消”.
    if (s_interaction_task && s_command_cancel_enabled &&
        !s_command_cancel_requested) {
        s_command_cancel_requested = true;
        s_cancel_requested_generation = s_interaction_generation;
        s_command_cancel_enabled = false;
        waiter = s_interaction_task;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!waiter) return false;
    board_port_set_command_cancel_enabled(false);
    // Keep the touch task responsive: a dedicated internal-RAM worker renders
    // the final frame and interrupts any in-flight HTTP operation safely.
    if (s_command_cancel_task) {
        xTaskNotifyGive(s_command_cancel_task);
    } else {
        // Startup treats creation failure as fatal, but retain a cooperative
        // fallback so a partially initialized device cannot wait for 90 s.
        xTaskNotifyGive(waiter);
    }
    ESP_LOGI(TAG, "voice command cancel requested by double tap");
    return true;
}

// A foreground command owns the LCD from the end of capture until a final
// answer or explicit error is displayed. Background updates may refresh data,
// but must not replace that flow with the ambient/weather screen.
static bool command_display_active(void) {
    bool active;
    taskENTER_CRITICAL(&s_task_state_lock);
    active = s_interaction_task != NULL || s_command_display_locked;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return active;
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

// Match the URL's textual scheme + authority (including an explicit port).
// Equivalent default-port spellings may miss reuse, which is preferable to
// ever pooling an untrusted absolute media URL or leaking gateway credentials.
static bool url_has_same_origin(const char *left, const char *right) {
    if (!left || !right) return false;
    const char *left_scheme = strstr(left, "://");
    const char *right_scheme = strstr(right, "://");
    if (!left_scheme || !right_scheme) return false;
    const char *left_end = strpbrk(left_scheme + 3, "/?#");
    const char *right_end = strpbrk(right_scheme + 3, "/?#");
    size_t left_len = left_end ? (size_t)(left_end - left) : strlen(left);
    size_t right_len = right_end ? (size_t)(right_end - right) : strlen(right);
    return left_len == right_len && strncasecmp(left, right, left_len) == 0;
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
    bool foreground_request = false;
    uint32_t foreground_generation = 0;
    TaskHandle_t current_task = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_task_state_lock);
    foreground_request = current_task == s_interaction_task;
    if (foreground_request) foreground_generation = s_interaction_generation;
    taskEXIT_CRITICAL(&s_task_state_lock);

    bool poll_request = current_task == s_gateway_poll_task;
    bool asset_request = current_task == s_startup_pet_asset_task;
    SemaphoreHandle_t request_mutex = asset_request
                                            ? s_gateway_asset_http_mutex
                                            : poll_request ? s_gateway_poll_http_mutex : s_http_mutex;
    if (!request_mutex) return ESP_ERR_INVALID_STATE;
    int64_t request_started_us = esp_timer_get_time();
    TickType_t lock_started = xTaskGetTickCount();
    bool cancellation_request = current_task == s_command_cancel_task;
    const TickType_t lock_timeout = pdMS_TO_TICKS(cancellation_request ? 6000 : 35000);
    while (xSemaphoreTake(request_mutex, pdMS_TO_TICKS(100)) != pdTRUE) {
        if (foreground_request && command_cancel_requested_for(foreground_generation)) {
            ESP_LOGI(TAG, "foreground HTTP lock wait cancelled: %s %s", method, path);
            return ESP_ERR_INVALID_STATE;
        }
        if ((xTaskGetTickCount() - lock_started) >= lock_timeout) {
            ESP_LOGW(TAG, "HTTP request lock timeout: %s %s", method, path);
            return ESP_ERR_TIMEOUT;
        }
    }
    uint32_t lock_wait_ms = (uint32_t)((xTaskGetTickCount() - lock_started) * portTICK_PERIOD_MS);
    if (foreground_request && command_cancel_requested_for(foreground_generation)) {
        xSemaphoreGive(request_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    // Prefer PSRAM for every HTTP body. Request buffers must not consume the
    // small internal heap reserved for the TLS handshake and Wi-Fi stacks.
    out->data = heap_caps_malloc(response_capacity, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!out->data) out->data = heap_caps_malloc(response_capacity, MALLOC_CAP_8BIT);
    if (!out->data) {
        ESP_LOGE(TAG, "HTTP buffer allocation failed: need=%u path=%s", (unsigned)response_capacity, path);
        log_heap_snapshot("http-buffer-fail");
        xSemaphoreGive(request_mutex);
        return ESP_ERR_NO_MEM;
    }
    out->capacity = response_capacity;
    out->data[0] = '\0';
    bool absolute_url = !strncmp(path, "http://", 7) || !strncmp(path, "https://", 8);
    bool reusable_gateway_request = !absolute_url || url_has_same_origin(s_gateway_url, url);
    bool bearer_request = !absolute_url;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    if (s_fangtang_use_cellular) {
        char authorization[128] = {0};
        if (s_gateway_token[0] && bearer_request) {
            snprintf(authorization, sizeof(authorization), "Bearer %s", s_gateway_token);
        }
        esp_err_t cellular_err = ml307_transport_http_request(
            method, url, content_type, authorization, NULL, NULL,
            body, body_len > 0 ? (size_t)body_len : 0,
            out->data, out->capacity, &out->len, &out->status,
            &out->truncated,
            cancellation_request ? 5000
                                 : (foreground_request && body_len > 32768 ? 90000 : 30000),
            foreground_request);
        xSemaphoreGive(request_mutex);
        ESP_LOGI(TAG, "ML307 HTTP %s %s status=%d err=%s response=%u%s",
                 method, absolute_url ? "<absolute URL>" : path, out->status,
                 esp_err_to_name(cellular_err), (unsigned)out->len,
                 out->truncated ? " truncated" : "");
        return cellular_err;
    }
#endif
    esp_http_client_handle_t *pool_client = asset_request
                                                ? &s_gateway_asset_http_client
                                                : poll_request ? &s_gateway_poll_http_client
                                                               : &s_gateway_http_client;
    char *pool_origin = asset_request
                            ? s_gateway_asset_http_origin
                            : poll_request ? s_gateway_poll_http_origin
                                           : s_gateway_http_origin;
    esp_http_client_handle_t client = NULL;
    bool owns_client = false;
    bool pooled_client = false;
    if (reusable_gateway_request) {
        if (*pool_client && strcmp(pool_origin, s_gateway_url)) {
            esp_http_client_cleanup(*pool_client);
            *pool_client = NULL;
            pool_origin[0] = '\0';
        }
        client = *pool_client;
        pooled_client = client != NULL;
    }
    if (!client) {
        esp_http_client_config_t config = {
            .url = url, .event_handler = on_http_event, .user_data = out,
            .timeout_ms = cancellation_request ? 5000 : 30000,
            .crt_bundle_attach = esp_crt_bundle_attach,
            .keep_alive_enable = true,
        };
        client = esp_http_client_init(&config);
        owns_client = true;
        if (client && reusable_gateway_request) {
            *pool_client = client;
            strlcpy(pool_origin, s_gateway_url, URL_CAPACITY);
            owns_client = false;
        }
    } else {
        esp_err_t setup_err = esp_http_client_set_url(client, url);
        if (setup_err == ESP_OK) setup_err = esp_http_client_set_user_data(client, out);
        if (setup_err == ESP_OK) setup_err = esp_http_client_set_timeout_ms(client, cancellation_request ? 5000 : 30000);
        if (setup_err != ESP_OK) {
            ESP_LOGW(TAG, "pooled HTTP client setup failed: %s", esp_err_to_name(setup_err));
            esp_http_client_cleanup(client);
            if (*pool_client == client) {
                *pool_client = NULL;
                pool_origin[0] = '\0';
            }
            free(out->data);
            out->data = NULL;
            xSemaphoreGive(request_mutex);
            return setup_err;
        }
    }
    if (!client) {
        ESP_LOGE(TAG, "HTTP client allocation failed: path=%s", path);
        log_heap_snapshot("http-client-fail");
        free(out->data);
        out->data = NULL;
        xSemaphoreGive(request_mutex);
        return ESP_ERR_NO_MEM;
    }
    if (foreground_request) {
        xSemaphoreTake(s_foreground_http_client_mutex, portMAX_DELAY);
        s_foreground_http_client = client;
        xSemaphoreGive(s_foreground_http_client_mutex);
    }
    esp_http_client_method_t http_method = HTTP_METHOD_GET;
    if (!strcmp(method, "POST")) http_method = HTTP_METHOD_POST;
    else if (!strcmp(method, "PUT")) http_method = HTTP_METHOD_PUT;
    esp_http_client_set_method(client, http_method);
    if (content_type) esp_http_client_set_header(client, "Content-Type", content_type);
    else esp_http_client_delete_header(client, "Content-Type");
    esp_http_client_set_header(client, "Accept", "application/json");
    if (s_gateway_token[0] && bearer_request) {
        char authorization[128];
        snprintf(authorization, sizeof(authorization), "Bearer %s", s_gateway_token);
        esp_http_client_set_header(client, "Authorization", authorization);
    } else {
        // A reused gateway handle may still carry the previous bearer. Never
        // let it leak to an absolute media URL or survive after token removal.
        esp_http_client_delete_header(client, "Authorization");
    }
    if (body && body_len > 0) {
        esp_http_client_set_post_field(client, body, body_len);
    } else {
        // Clear a previous POST payload before reusing the handle for GET/ACK.
        esp_http_client_set_post_field(client, NULL, 0);
    }
    int64_t perform_started_us = esp_timer_get_time();
    esp_err_t err;
    if (foreground_request && command_cancel_requested_for(foreground_generation)) {
        err = ESP_ERR_INVALID_STATE;
    } else {
        err = esp_http_client_perform(client);
    }
    out->status = esp_http_client_get_status_code(client);
    uint32_t perform_ms = (uint32_t)((esp_timer_get_time() - perform_started_us) / 1000);
    if (foreground_request) {
        xSemaphoreTake(s_foreground_http_client_mutex, portMAX_DELAY);
        if (s_foreground_http_client == client) s_foreground_http_client = NULL;
        xSemaphoreGive(s_foreground_http_client_mutex);
    }
    // The body and callback point at caller-owned memory. Clear both before a
    // pooled handle can outlive this stack frame.
    esp_http_client_set_post_field(client, NULL, 0);
    esp_http_client_set_user_data(client, NULL);
    if (owns_client) {
        esp_http_client_cleanup(client);
    } else if (err != ESP_OK) {
        // A failed perform can leave the reusable handle's transport/parser in
        // an indeterminate state. Discard it so the retry starts with a clean
        // TLS connection instead of repeatedly failing on the poisoned one.
        esp_http_client_cleanup(client);
        if (*pool_client == client) {
            *pool_client = NULL;
            pool_origin[0] = '\0';
        }
    }
    xSemaphoreGive(request_mutex);
    char target[96];
    if (!strncmp(path, "http://", 7) || !strncmp(path, "https://", 8)) {
        strlcpy(target, "<absolute media URL>", sizeof(target));
    } else {
        strlcpy(target, path, sizeof(target));
        char *query = strchr(target, '?');
        if (query) *query = '\0';
    }
    ESP_LOGI(TAG, "HTTP %s %s status=%d err=%s lane=%s client=%s lock=%ums perform=%ums total=%ums response=%u%s",
             method, target, out->status, esp_err_to_name(err),
             asset_request ? "asset" : poll_request ? "poll" : "foreground",
             pooled_client ? "pooled" : "dedicated", (unsigned)lock_wait_ms,
             (unsigned)perform_ms, (unsigned)((esp_timer_get_time() - request_started_us) / 1000),
             (unsigned)out->len, out->truncated ? " truncated" : "");
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

static esp_err_t download_audio(const char *url, uint8_t **out_audio, size_t *out_len) {
    if (!url || !url[0] || !out_audio || !out_len) return ESP_ERR_INVALID_ARG;
    *out_audio = NULL;
    *out_len = 0;
    http_response_t response;
    esp_err_t err = request_with_capacity("GET", url, NULL, NULL, 0,
                                          HARDWARE_AUDIO_RESPONSE_CAPACITY, &response);
    if (err != ESP_OK || response.status != 200 || response.len < 2) {
        esp_err_t result = err;
        if (err == ESP_OK) {
            // A successful but empty/malformed media object, or a permanent
            // client/media-token HTTP rejection, cannot heal on another poll.
            // Classify it as invalid content so it is ACKed instead of pinning
            // the outgoing cursor forever. Server failures remain retryable.
            result = (response.status >= 400 && response.status < 500) ||
                     (response.status == 200 && response.len < 2)
                         ? ESP_ERR_INVALID_ARG
                         : ESP_FAIL;
        }
        response_release(&response);
        return result;
    }
    *out_audio = (uint8_t *)response.data;
    *out_len = response.len;
    response.data = NULL;
    response_release(&response);
    return ESP_OK;
}

typedef struct {
    char encoding[16];
    char revision[40];
    int width;
    int height;
    int frame_ms;
    int frame_count;
    char urls[PET_ASSET_MAX_FRAMES][URL_CAPACITY];
    char sha256[PET_ASSET_MAX_FRAMES][65];
} pet_asset_ref_t;

// Pet artwork is optional startup decoration. Keep its small descriptor here
// so the authenticated handshake can release TLS/JSON memory and initialize
// ESP-SR before any media download or SPIFFS write takes place.
static bool s_startup_pet_asset_pending;
static bool s_startup_pet_asset_present;
static pet_asset_ref_t *s_startup_pet_asset_ref;
static char s_loaded_pet_asset_revision[40];
static int s_loaded_pet_asset_frame_count;

static bool pet_asset_url_allowed(const char *url) {
    return hardware_audio_url_allowed(url);
}

static bool parse_pet_asset_ref(cJSON *object, pet_asset_ref_t *out) {
    if (!cJSON_IsObject(object) || !out) return false;
    memset(out, 0, sizeof(*out));
    const char *encoding = json_string(object, "encoding");
    const char *revision = json_string(object, "revision");
    cJSON *urls = cJSON_GetObjectItemCaseSensitive(object, "urls");
    cJSON *hashes = cJSON_GetObjectItemCaseSensitive(object, "sha256");
    if (!encoding || strcmp(encoding, "rgb565a8") || !revision || !revision[0] ||
        strlen(revision) >= sizeof(out->revision) ||
        !json_number(object, "width", &out->width) ||
        !json_number(object, "height", &out->height) ||
        out->width < 32 || out->width > PET_ASSET_MAX_DIMENSION ||
        out->height < 32 || out->height > PET_ASSET_MAX_DIMENSION ||
        !cJSON_IsArray(urls) || !cJSON_IsArray(hashes)) return false;
    strlcpy(out->encoding, encoding, sizeof(out->encoding));
    strlcpy(out->revision, revision, sizeof(out->revision));
    if (!json_number(object, "frameMs", &out->frame_ms) || out->frame_ms < 50 || out->frame_ms > 10000) {
        out->frame_ms = PET_ASSET_DEFAULT_FRAME_MS;
    }
    int count = cJSON_GetArraySize(urls);
    if (count < 1 || count > PET_ASSET_MAX_FRAMES || cJSON_GetArraySize(hashes) != count) return false;
    for (int i = 0; i < count; ++i) {
        cJSON *entry = cJSON_GetArrayItem(urls, i);
        cJSON *hash = cJSON_GetArrayItem(hashes, i);
        if (!cJSON_IsString(entry) || !entry->valuestring || !pet_asset_url_allowed(entry->valuestring) ||
            strlen(entry->valuestring) >= sizeof(out->urls[i]) || !cJSON_IsString(hash) ||
            !hash->valuestring || strlen(hash->valuestring) != 64) return false;
        for (size_t j = 0; j < 64; ++j) {
            char ch = hash->valuestring[j];
            if (!((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F'))) return false;
        }
        strlcpy(out->urls[i], entry->valuestring, sizeof(out->urls[i]));
        strlcpy(out->sha256[i], hash->valuestring, sizeof(out->sha256[i]));
    }
    out->frame_count = count;
    return true;
}

static void free_pet_asset_frames(uint8_t *frames[PET_ASSET_MAX_FRAMES], size_t frame_count) {
    for (size_t i = 0; i < frame_count && i < PET_ASSET_MAX_FRAMES; ++i) {
        heap_caps_free(frames[i]);
    }
}

static esp_err_t install_pet_asset_first_frame(const pet_asset_ref_t *ref,
                                               uint8_t *const frames[PET_ASSET_MAX_FRAMES]);

static esp_err_t download_pet_asset_frames(const pet_asset_ref_t *ref,
                                           uint8_t *frames[PET_ASSET_MAX_FRAMES],
                                           bool startup_transaction) {
    if (!ref || !frames) return ESP_ERR_INVALID_ARG;
    size_t expected = (size_t)ref->width * (size_t)ref->height * PET_ASSET_BYTES_PER_PIXEL;
    if (expected == 0 || expected > PET_ASSET_MAX_BYTES) return ESP_ERR_INVALID_SIZE;
    memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
    for (int i = 0; i < ref->frame_count; ++i) {
        // Optional artwork must stay below an interactive voice turn in both
        // connection priority and Wi-Fi airtime. Pause between frames while a
        // command is recording, uploading, or waiting for its result. A frame
        // already in flight is bounded to one 192 KiB response; the next one
        // cannot start until foreground ownership is released.
        while (startup_transaction && s_foreground_http_requested &&
               s_startup_pet_asset_pending) {
            vTaskDelay(pdMS_TO_TICKS(100));
        }
        if (startup_transaction && !s_startup_pet_asset_pending) {
            free_pet_asset_frames(frames, (size_t)i);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return ESP_ERR_INVALID_STATE;
        }
        // Each frame is only 192 KiB, but on EchoEar it can race a TLS
        // handshake or wake-model transition for internal heap.  A transient
        // incomplete transfer must not discard the whole idle animation pack;
        // failed HTTP handles are discarded by request_with_capacity(), so a
        // bounded retry gets a clean connection and buffer on every attempt.
        http_response_t response = {0};
        esp_err_t err = ESP_FAIL;
        const unsigned max_attempts = startup_transaction ? 3 : 2;
        for (unsigned attempt = 1; attempt <= max_attempts; ++attempt) {
            err = request_with_capacity("GET", ref->urls[i], NULL, NULL, 0,
                                        expected + 1, &response);
            if (err == ESP_OK && response.status == 200 && response.len == expected) {
                break;
            }
            // Do not retry a bad/expired asset descriptor.  It cannot recover
            // locally and should be rejected by the normal profile refresh.
            int failed_status = response.status;
            size_t failed_len = response.len;
            bool permanent = err == ESP_OK && failed_status >= 400 && failed_status < 500;
            response_release(&response);
            memset(&response, 0, sizeof(response));
            if (permanent || attempt == max_attempts) break;
            ESP_LOGW(TAG, "pet asset frame retry: frame=%d attempt=%u/%u err=%s status=%d bytes=%u",
                     i, attempt, max_attempts, esp_err_to_name(err), failed_status,
                     (unsigned)failed_len);
            vTaskDelay(pdMS_TO_TICKS(250u * attempt));
            if (startup_transaction && !s_startup_pet_asset_pending) break;
        }
        if (err != ESP_OK || response.status != 200 || response.len != expected) {
            esp_err_t result = err != ESP_OK ? err :
                               response.status >= 400 && response.status < 500
                                   ? ESP_ERR_INVALID_ARG : ESP_FAIL;
            response_release(&response);
            free_pet_asset_frames(frames, (size_t)i);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return result;
        }
        frames[i] = (uint8_t *)response.data;
        response.data = NULL;
        response_release(&response);
        uint8_t digest[32];
        size_t digest_len = 0;
        psa_status_t status = psa_hash_compute(PSA_ALG_SHA_256, (const uint8_t *)frames[i], expected,
                                               digest, sizeof(digest), &digest_len);
        char actual[65];
        if (status != PSA_SUCCESS || digest_len != sizeof(digest)) {
            free_pet_asset_frames(frames, (size_t)i + 1);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return ESP_FAIL;
        }
        digest_hex(digest, actual);
        if (strcasecmp(actual, ref->sha256[i])) {
            ESP_LOGW(TAG, "pet asset SHA-256 mismatch: frame=%d", i);
            free_pet_asset_frames(frames, (size_t)i + 1);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return ESP_ERR_INVALID_CRC;
        }
        // Make standby useful as soon as the first verified frame is available.
        // The remaining frames can be retried without leaving EchoEar's round
        // idle screen blank when a later TLS transfer is interrupted.
        if (startup_transaction && i == 0) {
            esp_err_t preview_err = install_pet_asset_first_frame(ref, frames);
            if (preview_err == ESP_OK) {
                ESP_LOGI(TAG, "startup pet first frame applied while animation downloads");
            } else {
                ESP_LOGW(TAG, "startup pet first-frame preview failed: %s",
                         esp_err_to_name(preview_err));
            }
        }
        if (startup_transaction && !s_startup_pet_asset_pending) {
            free_pet_asset_frames(frames, (size_t)i + 1);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return ESP_ERR_INVALID_STATE;
        }
    }
    return ESP_OK;
}

static bool write_all(FILE *file, const void *data, size_t size,
                      const char *path, int frame_index) {
    if (!file || !data) {
        ESP_LOGE(TAG, "pet cache frame %d open failed: path=%s errno=%d (%s)",
                 frame_index, path ? path : "<null>", errno, strerror(errno));
        return false;
    }
    // SPIFFS garbage collection is bounded by CONFIG_SPIFFS_GC_MAX_RUNS.
    // A single 192 KiB write can therefore report ENOSPC on a mostly empty but
    // fragmented partition. Page-sized writes let GC make bounded progress.
    const uint8_t *cursor = (const uint8_t *)data;
    size_t written = 0;
    while (written < size) {
        size_t chunk = size - written;
        if (chunk > 4096) chunk = 4096;
        size_t count = fwrite(cursor + written, 1, chunk, file);
        if (count != chunk) {
            ESP_LOGE(TAG, "pet cache frame %d write failed: path=%s bytes=%u/%u errno=%d (%s)",
                     frame_index, path, (unsigned)(written + count), (unsigned)size,
                     errno, strerror(errno));
            return false;
        }
        written += count;
        if ((written & 0x7fffu) == 0) vTaskDelay(1);
    }
    if (fflush(file) != 0) {
        ESP_LOGE(TAG, "pet cache frame %d flush failed: path=%s errno=%d (%s)",
                 frame_index, path, errno, strerror(errno));
        return false;
    }
    return true;
}

static bool replace_cached_file(const char *temp_path, const char *final_path,
                                const char *kind, int frame_index) {
    if (!temp_path || !final_path) return false;
    // SPIFFS does not reliably replace an existing destination via rename().
    // The temporary file is already complete here, so remove only the stale
    // destination before installing it and always clean up on failure.
    if (unlink(final_path) != 0 && errno != ENOENT) {
        ESP_LOGE(TAG, "pet cache %s %d remove failed: path=%s errno=%d (%s)",
                 kind, frame_index, final_path, errno, strerror(errno));
        unlink(temp_path);
        return false;
    }
    if (rename(temp_path, final_path) != 0) {
        ESP_LOGE(TAG, "pet cache %s %d rename failed: %s -> %s errno=%d (%s)",
                 kind, frame_index, temp_path, final_path, errno, strerror(errno));
        unlink(temp_path);
        return false;
    }
    return true;
}

static esp_err_t cache_pet_asset(const pet_asset_ref_t *ref,
                                 uint8_t *const frames[PET_ASSET_MAX_FRAMES]) {
    if (!s_storage_mounted || !ref || !frames) return ESP_ERR_INVALID_STATE;
    size_t frame_bytes = (size_t)ref->width * (size_t)ref->height * PET_ASSET_BYTES_PER_PIXEL;
    char final_path[64], temp_path[64];
    // The metadata is the commit record. Remove it before modifying frames so
    // an interrupted update can never make a mixture of revisions look valid.
    // Writing each final frame directly avoids keeping a second 192 KiB copy
    // in fragmented SPIFFS and eliminates the multi-minute GC stalls observed
    // with per-frame temp-file renames.
    unlink(PET_ASSET_CACHE_META_PATH);
    unlink(PET_ASSET_CACHE_META_TMP_PATH);
    for (int i = 0; i < ref->frame_count; ++i) {
        snprintf(final_path, sizeof(final_path), PET_ASSET_CACHE_FRAME_PATH_FORMAT, (unsigned)i);
        snprintf(temp_path, sizeof(temp_path), PET_ASSET_CACHE_FRAME_TMP_PATH_FORMAT, (unsigned)i);
        unlink(temp_path);
        // Truncating the previous frame leaves deleted pages behind. Reclaim
        // enough physical blocks for one complete replacement before fwrite;
        // CONFIG_SPIFFS_GC_MAX_RUNS is sized for this bounded request. This
        // runs only on the background pet installer after its preview is live.
        unlink(final_path);
        esp_err_t gc_err = esp_spiffs_gc("storage", frame_bytes + 4096);
        if (gc_err != ESP_OK) {
            ESP_LOGW(TAG, "pet cache pre-write GC incomplete: frame=%d need=%u err=%s",
                     i, (unsigned)(frame_bytes + 4096), esp_err_to_name(gc_err));
        }
        errno = 0;
        FILE *file = fopen(final_path, "wb");
        bool ok = write_all(file, frames[i], frame_bytes, final_path, i);
        if (file && fclose(file) != 0) {
            ESP_LOGE(TAG, "pet cache frame %d close failed: path=%s errno=%d (%s)",
                     i, final_path, errno, strerror(errno));
            ok = false;
        }
        if (!ok) {
            unlink(final_path);
            return ESP_FAIL;
        }
    }
    for (int i = ref->frame_count; i < PET_ASSET_MAX_FRAMES; ++i) {
        snprintf(final_path, sizeof(final_path), PET_ASSET_CACHE_FRAME_PATH_FORMAT, (unsigned)i);
        unlink(final_path);
    }
    errno = 0;
    FILE *meta_file = fopen(PET_ASSET_CACHE_META_TMP_PATH, "wb");
    if (!meta_file || fprintf(meta_file, "MACLAW_PET_V2\n%s\n%d %d %d %d\n",
                             ref->revision, ref->width, ref->height, ref->frame_ms,
                             ref->frame_count) < 0) {
        ESP_LOGE(TAG, "pet cache metadata open/header failed: path=%s errno=%d (%s)",
                 PET_ASSET_CACHE_META_TMP_PATH, errno, strerror(errno));
        if (meta_file) fclose(meta_file);
        unlink(PET_ASSET_CACHE_META_TMP_PATH);
        return ESP_FAIL;
    }
    bool meta_ok = true;
    for (int i = 0; i < ref->frame_count; ++i) {
        if (fprintf(meta_file, "%s\n", ref->sha256[i]) < 0) {
            ESP_LOGE(TAG, "pet cache metadata hash %d failed: errno=%d (%s)",
                     i, errno, strerror(errno));
            meta_ok = false;
            break;
        }
    }
    if (fclose(meta_file) != 0) {
        ESP_LOGE(TAG, "pet cache metadata close failed: errno=%d (%s)", errno, strerror(errno));
        meta_ok = false;
    }
    if (!meta_ok || !replace_cached_file(PET_ASSET_CACHE_META_TMP_PATH,
                                         PET_ASSET_CACHE_META_PATH, "metadata", -1)) {
        unlink(PET_ASSET_CACHE_META_TMP_PATH);
        return ESP_FAIL;
    }
    ESP_LOGI(TAG, "pet asset cached: revision=%s frames=%d bytes_per_frame=%u",
             ref->revision, ref->frame_count, (unsigned)frame_bytes);
    return ESP_OK;
}

static esp_err_t cache_pet_asset_first_frame(const pet_asset_ref_t *ref,
                                             uint8_t *const frames[PET_ASSET_MAX_FRAMES]) {
    if (!s_storage_mounted || !ref || !frames || !frames[0]) return ESP_ERR_INVALID_STATE;
    pet_asset_ref_t preview = *ref;
    preview.frame_count = 1;
    return cache_pet_asset(&preview, frames);
}

#if CONFIG_MACLAW_BOARD_FANGTANG_4G
typedef struct {
    pet_asset_ref_t preview;
    uint8_t *frames[PET_ASSET_MAX_FRAMES];
    SemaphoreHandle_t complete;
    esp_err_t result;
} fangtang_pet_cache_job_t;

static void fangtang_pet_cache_task(void *arg) {
    fangtang_pet_cache_job_t *job = (fangtang_pet_cache_job_t *)arg;
    job->result = cache_pet_asset_first_frame(&job->preview, job->frames);
    xSemaphoreGive(job->complete);
    vTaskDelete(NULL);
}

static esp_err_t cache_fangtang_startup_pet_first_frame(
    const pet_asset_ref_t *ref,
    uint8_t *const frames[PET_ASSET_MAX_FRAMES]) {
    if (!ref || !frames || !frames[0]) return ESP_ERR_INVALID_ARG;
    /* SPIFFS/esp_flash disables the shared flash/PSRAM cache while programming.
     * The startup media worker deliberately has a PSRAM stack, so it must not
     * execute even the first unlink/fopen itself. Keep the descriptor, wait
     * state and complete cache call on internal RAM while the large RGB565A8
     * source frame remains borrowed from the waiting owner. */
    fangtang_pet_cache_job_t *job = heap_caps_calloc(
        1, sizeof(*job), MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!job) return ESP_ERR_NO_MEM;
    job->preview = *ref;
    job->preview.frame_count = 1;
    job->frames[0] = frames[0];
    job->complete = xSemaphoreCreateBinary();
    if (!job->complete) {
        heap_caps_free(job);
        return ESP_ERR_NO_MEM;
    }
    job->result = ESP_FAIL;
    TaskHandle_t task = NULL;
    BaseType_t created = xTaskCreatePinnedToCore(
        fangtang_pet_cache_task, "fangtang_pet_cache", 8192,
        job, 3, &task, 1);
    if (created != pdPASS) {
        vSemaphoreDelete(job->complete);
        heap_caps_free(job);
        return ESP_ERR_NO_MEM;
    }
    xSemaphoreTake(job->complete, portMAX_DELAY);
    esp_err_t result = job->result;
    vSemaphoreDelete(job->complete);
    heap_caps_free(job);
    return result;
}
#endif

static void clear_pet_asset_cache(void) {
    unlink(PET_ASSET_CACHE_META_PATH);
    unlink(PET_ASSET_CACHE_META_TMP_PATH);
    char path[64];
    for (int i = 0; i < PET_ASSET_MAX_FRAMES; ++i) {
        snprintf(path, sizeof(path), PET_ASSET_CACHE_FRAME_PATH_FORMAT, (unsigned)i);
        unlink(path);
        snprintf(path, sizeof(path), PET_ASSET_CACHE_FRAME_TMP_PATH_FORMAT, (unsigned)i);
        unlink(path);
    }
    // Remove opaque V1 assets written by builds that used the old two-byte
    // RGB565 format. Their metadata can otherwise survive an OTA update and
    // be misread as transparent frames.
    for (int i = 0; i < PET_ASSET_MAX_FRAMES; ++i) {
        snprintf(path, sizeof(path), "/storage/pet_asset_%u.rgb565le", (unsigned)i);
        unlink(path);
    }
}

static esp_err_t clear_applied_pet_asset(void) {
    esp_err_t err = board_port_set_pet_asset(NULL, 0, 0, 0, 0);
    if (err == ESP_OK) {
        s_loaded_pet_asset_revision[0] = '\0';
        s_loaded_pet_asset_frame_count = 0;
        clear_pet_asset_cache();
    }
    return err;
}

static esp_err_t install_pet_asset_with_fallback(const pet_asset_ref_t *ref,
                                                 uint8_t *const frames[PET_ASSET_MAX_FRAMES],
                                                 int *installed_frame_count,
                                                 int *installed_frame_ms);

static bool load_cached_pet_asset(void) {
    if (!s_storage_mounted) return false;
    FILE *meta = fopen(PET_ASSET_CACHE_META_PATH, "rb");
    if (!meta) return false;
    char magic[24] = {0}, revision[40] = {0};
    pet_asset_ref_t ref = {0};
    char hashes[PET_ASSET_MAX_FRAMES][66] = {{0}};
    bool valid = fgets(magic, sizeof(magic), meta) && !strcmp(magic, "MACLAW_PET_V2\n") &&
                 fgets(revision, sizeof(revision), meta) &&
                 fscanf(meta, "%d %d %d %d", &ref.width, &ref.height,
                        &ref.frame_ms, &ref.frame_count) == 4 &&
                 ref.frame_count >= 1 && ref.frame_count <= PET_ASSET_MAX_FRAMES;
    for (int i = 0; valid && i < ref.frame_count; ++i) {
        valid = fscanf(meta, "%65s", hashes[i]) == 1;
    }
    fclose(meta);
    char *revision_newline = strpbrk(revision, "\r\n");
    if (revision_newline) *revision_newline = '\0';
    if (!valid || ref.width < 32 || ref.width > PET_ASSET_MAX_DIMENSION ||
        ref.height < 32 || ref.height > PET_ASSET_MAX_DIMENSION ||
        ref.frame_count < 1 || ref.frame_count > PET_ASSET_MAX_FRAMES) {
        clear_pet_asset_cache();
        return false;
    }
    size_t frame_bytes = (size_t)ref.width * (size_t)ref.height * PET_ASSET_BYTES_PER_PIXEL;
    uint8_t *frames[PET_ASSET_MAX_FRAMES] = {0};
    char path[64];
    for (int i = 0; i < ref.frame_count; ++i) {
        snprintf(path, sizeof(path), PET_ASSET_CACHE_FRAME_PATH_FORMAT, (unsigned)i);
        struct stat info;
        if (stat(path, &info) != 0 || info.st_size != (off_t)frame_bytes) break;
        FILE *file = fopen(path, "rb");
        frames[i] = heap_caps_malloc(frame_bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!frames[i]) frames[i] = malloc(frame_bytes);
        bool ok = frames[i] && file && fread(frames[i], 1, frame_bytes, file) == frame_bytes;
        if (file) fclose(file);
        if (ok) {
            uint8_t digest[32]; size_t digest_len = 0; char actual[65];
            ok = psa_hash_compute(PSA_ALG_SHA_256, (const uint8_t *)frames[i], frame_bytes, digest,
                                  sizeof(digest), &digest_len) == PSA_SUCCESS &&
                 digest_len == sizeof(digest);
            if (ok) { digest_hex(digest, actual); ok = !strcasecmp(actual, hashes[i]); }
        }
        if (!ok) break;
    }
    bool loaded = true;
    for (int i = 0; i < ref.frame_count; ++i) loaded = loaded && frames[i] != NULL;
    if (loaded) {
        int installed_frames = 0, installed_frame_ms = 0;
        loaded = install_pet_asset_with_fallback(&ref, frames, &installed_frames,
                                                 &installed_frame_ms) == ESP_OK;
        if (loaded) {
            strlcpy(s_loaded_pet_asset_revision, revision,
                    sizeof(s_loaded_pet_asset_revision));
            s_loaded_pet_asset_frame_count = ref.frame_count;
            ESP_LOGI(TAG, "cached pet asset applied: revision=%s frames=%d/%d frame_ms=%d",
                     revision, installed_frames, ref.frame_count, installed_frame_ms);
        }
    }
    free_pet_asset_frames(frames, PET_ASSET_MAX_FRAMES);
    if (!loaded) clear_pet_asset_cache();
    return loaded;
}

static esp_err_t install_pet_asset_with_fallback(const pet_asset_ref_t *ref,
                                                 uint8_t *const frames[PET_ASSET_MAX_FRAMES],
                                                 int *installed_frame_count,
                                                 int *installed_frame_ms) {
    if (!ref || !frames) return ESP_ERR_INVALID_ARG;
    const uint8_t *views[PET_ASSET_MAX_FRAMES] = {0};
    for (int i = 0; i < ref->frame_count; ++i) views[i] = frames[i];
    esp_err_t err = board_port_set_pet_asset(views, (size_t)ref->frame_count,
                                              (size_t)ref->width, (size_t)ref->height,
                                              (uint32_t)ref->frame_ms);
    int used_count = ref->frame_count;
    int used_frame_ms = ref->frame_ms;
    // Keep the selected GUI pet visible on boards with less free PSRAM. A
    // lower keyframe count preserves the animation period and is preferable to
    // falling all the way back to the native robot head.
    while (err == ESP_ERR_NO_MEM && used_count > 1) {
        int next_count = used_count > 4 ? 4 : used_count > 2 ? 2 : 1;
        for (int i = 0; i < next_count; ++i) {
            views[i] = frames[(i * ref->frame_count) / next_count];
        }
        used_frame_ms = ref->frame_ms * ref->frame_count / next_count;
        ESP_LOGW(TAG, "pet asset memory pressure; retrying with %d/%d frames",
                 next_count, ref->frame_count);
        err = board_port_set_pet_asset(views, (size_t)next_count,
                                       (size_t)ref->width, (size_t)ref->height,
                                       (uint32_t)used_frame_ms);
        used_count = next_count;
    }
    if (installed_frame_count) *installed_frame_count = err == ESP_OK ? used_count : 0;
    if (installed_frame_ms) *installed_frame_ms = err == ESP_OK ? used_frame_ms : 0;
    return err;
}
static esp_err_t install_pet_asset_first_frame(const pet_asset_ref_t *ref,
                                               uint8_t *const frames[PET_ASSET_MAX_FRAMES]) {
    if (!ref || !frames || !frames[0]) return ESP_ERR_INVALID_ARG;
    const uint8_t *first[1] = {frames[0]};
    return board_port_set_pet_asset(first, 1, (size_t)ref->width,
                                    (size_t)ref->height,
                                    (uint32_t)ref->frame_ms);
}
static esp_err_t apply_pet_asset_ref(cJSON *object) {
    pet_asset_ref_t ref;
    if (!parse_pet_asset_ref(object, &ref)) return ESP_ERR_INVALID_ARG;
    uint8_t *frames[PET_ASSET_MAX_FRAMES] = {0};
    esp_err_t err = download_pet_asset_frames(&ref, frames, false);
    if (err == ESP_OK) {
        int installed_frames = 0, installed_frame_ms = 0;
        err = install_pet_asset_with_fallback(&ref, frames, &installed_frames,
                                              &installed_frame_ms);
        if (err == ESP_OK) {
            strlcpy(s_loaded_pet_asset_revision, ref.revision,
                    sizeof(s_loaded_pet_asset_revision));
            s_loaded_pet_asset_frame_count = ref.frame_count;
            esp_err_t cache_err = cache_pet_asset_first_frame(&ref, frames);
            if (cache_err != ESP_OK) ESP_LOGW(TAG, "pet asset cache failed: %s", esp_err_to_name(cache_err));
            ESP_LOGI(TAG, "GUI pet asset applied: revision=%s frames=%d/%d frame_ms=%d size=%dx%d",
                     ref.revision, installed_frames, ref.frame_count, installed_frame_ms,
                     ref.width, ref.height);
        }
    }
    free_pet_asset_frames(frames, PET_ASSET_MAX_FRAMES);
    return err;
}

static esp_err_t apply_deferred_pet_asset(void) {
    if (!s_startup_pet_asset_pending) return ESP_OK;
    if (!s_startup_pet_asset_present) {
        s_loaded_pet_asset_revision[0] = '\0';
        esp_err_t err = clear_applied_pet_asset();
        s_startup_pet_asset_pending = false;
        return err;
    }
    if (!s_startup_pet_asset_ref) {
        s_startup_pet_asset_pending = false;
        return ESP_ERR_INVALID_STATE;
    }
    if (s_loaded_pet_asset_revision[0] &&
        !strcmp(s_loaded_pet_asset_revision, s_startup_pet_asset_ref->revision) &&
        s_loaded_pet_asset_frame_count >= s_startup_pet_asset_ref->frame_count) {
        ESP_LOGI(TAG, "startup pet asset already cached: revision=%s",
                 s_startup_pet_asset_ref->revision);
        s_startup_pet_asset_pending = false;
        return ESP_OK;
    }
    uint8_t *frames[PET_ASSET_MAX_FRAMES] = {0};
    esp_err_t err = ESP_FAIL;
    // A fully installed pack is the smooth-animation target, but the first
    // verified frame has already made standby usable.  Continue the complete
    // transaction after transport-level failures instead of leaving EchoEar on
    // that preview for the rest of the boot.  Each failed pass frees its own
    // partial source frames and request_with_capacity discards the failed TLS
    // handle, so the next pass starts cleanly without growing PSRAM usage.
    for (unsigned attempt = 1; attempt <= PET_ASSET_STARTUP_TRANSACTION_ATTEMPTS;
         ++attempt) {
        err = download_pet_asset_frames(s_startup_pet_asset_ref, frames, true);
        if (err == ESP_OK && !s_startup_pet_asset_pending) err = ESP_ERR_INVALID_STATE;
        if (err == ESP_OK || err == ESP_ERR_INVALID_ARG || err == ESP_ERR_INVALID_CRC ||
            err == ESP_ERR_INVALID_SIZE || err == ESP_ERR_INVALID_STATE) {
            break;
        }
        if (attempt == PET_ASSET_STARTUP_TRANSACTION_ATTEMPTS) break;
        ESP_LOGW(TAG, "startup pet pack retry: attempt=%u/%u err=%s; preview remains visible",
                 attempt, PET_ASSET_STARTUP_TRANSACTION_ATTEMPTS, esp_err_to_name(err));
        vTaskDelay(pdMS_TO_TICKS(PET_ASSET_STARTUP_RETRY_DELAY_MS * attempt));
        if (!s_startup_pet_asset_pending) {
            err = ESP_ERR_INVALID_STATE;
            break;
        }
    }
    if (err == ESP_OK) {
        ESP_LOGI(TAG, "startup pet frames downloaded; installing first frame");
        // Put a real pet on the standby surface immediately. Scaling one frame
        // is quick; the remaining seven animated frames can be installed after
        // the durable cache commit completes.
        esp_err_t preview_err = install_pet_asset_first_frame(s_startup_pet_asset_ref, frames);
        if (preview_err != ESP_OK) {
            ESP_LOGW(TAG, "startup pet preview failed: %s", esp_err_to_name(preview_err));
        } else {
            ESP_LOGI(TAG, "startup pet first frame applied");
        }
        int installed_frames = 0, installed_frame_ms = 0;
        err = install_pet_asset_with_fallback(s_startup_pet_asset_ref, frames,
                                              &installed_frames, &installed_frame_ms);
        if (err == ESP_OK) {
            strlcpy(s_loaded_pet_asset_revision, s_startup_pet_asset_ref->revision,
                    sizeof(s_loaded_pet_asset_revision));
            s_loaded_pet_asset_frame_count = s_startup_pet_asset_ref->frame_count;
            ESP_LOGI(TAG, "deferred pet asset applied: revision=%s frames=%d/%d frame_ms=%d size=%dx%d",
                     s_startup_pet_asset_ref->revision, installed_frames,
                     s_startup_pet_asset_ref->frame_count, installed_frame_ms,
                     s_startup_pet_asset_ref->width, s_startup_pet_asset_ref->height);
            // Persist only after the live animation is installed successfully.
            // Caching a preview from a failed install can make the next boot
            // restore a revision that was never actually usable. EchoEar runs
            // this transfer worker on a PSRAM stack; SPIFFS programming turns
            // off the shared cache and cannot execute safely on that stack.
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
            // Fangtang still persists the same first-frame cache as Bread, but
            // delegates the flash transaction to a short-lived internal-stack
            // worker and waits before releasing the borrowed source frame.
            esp_err_t cache_err = cache_fangtang_startup_pet_first_frame(
                s_startup_pet_asset_ref, frames);
            if (cache_err != ESP_OK) {
                ESP_LOGW(TAG, "deferred pet preview cache failed: %s",
                         esp_err_to_name(cache_err));
            }
#elif !CONFIG_MACLAW_BOARD_ECHOEAR_2ST
            esp_err_t cache_err = cache_pet_asset_first_frame(s_startup_pet_asset_ref, frames);
            if (cache_err != ESP_OK) {
                ESP_LOGW(TAG, "deferred pet preview cache failed: %s", esp_err_to_name(cache_err));
            }
#endif
        }
    }
    // The display port retains its own scaled PSRAM copies.  The source HTTP
    // buffers are normally released here, but on EchoEar that deallocation
    // races the QSPI full-frame path and causes a cache-disable assertion
    // immediately after the visible pet has been installed. Retain the tiny
    // one-shot source set for this boot; it is bounded (8 × 192 KiB) and avoids
    // a restart that would otherwise erase the successful standby transition.
#if CONFIG_MACLAW_BOARD_ECHOEAR_2ST
    ESP_LOGI(TAG, "EchoEar retained startup pet source buffers until reboot");
#else
    free_pet_asset_frames(frames, PET_ASSET_MAX_FRAMES);
#endif
    // Keep pending true for the entire download/install/cache transaction so
    // a queued pet_profile mirror is ACKed without starting a competing copy.
    s_startup_pet_asset_pending = false;
    return err;
}

static void startup_pet_asset_task(void *arg) {
    (void)arg;
    int64_t started_us = esp_timer_get_time();
    // EchoEar's 8 MB PSRAM is sufficient for the scaled animation, but its
    // concurrent ESP-SR/TLS start-up peak can leave too little contiguous
    // internal heap for mbedTLS AES.  The verified preview is already on the
    // standby screen at this point, so temporarily release MultiNet while the
    // optional full pack is fetched.  This mirrors Bread's ownership rule:
    // one high-memory foreground/media operation at a time.  A direct touch
    // still starts capture while the recognizer is down; it is restarted
    // immediately after the install transaction.
    bool wake_was_stopped = false;
#if CONFIG_MACLAW_BOARD_ECHOEAR_2ST
    esp_err_t stop_err = board_port_stop_wake_word();
    wake_was_stopped = stop_err == ESP_OK;
    if (stop_err != ESP_OK && stop_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "startup pet animation could not pause offline wake: %s",
                 esp_err_to_name(stop_err));
    }
#endif
    esp_err_t err = apply_deferred_pet_asset();
#if CONFIG_MACLAW_BOARD_ECHOEAR_2ST
    if (wake_was_stopped && !s_setup_portal_active) {
        esp_err_t wake_err = board_port_start_wake_word(on_wake_word, NULL);
        if (wake_err == ESP_OK) {
            ESP_LOGI(TAG, "offline wake restored after startup pet animation install");
        } else {
            ESP_LOGW(TAG, "offline wake restore after startup pet animation failed: %s",
                     esp_err_to_name(wake_err));
            schedule_wake_restart();
        }
    }
#endif
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "deferred startup pet asset ignored: %s", esp_err_to_name(err));
    }
    ESP_LOGI(TAG, "post-ready pet asset work complete in %lu ms",
             (unsigned long)((esp_timer_get_time() - started_us) / 1000));
    s_startup_pet_asset_task = NULL;
    // This worker's stack comes from xTaskCreatePinnedToCoreWithCaps().  The
    // regular FreeRTOS deleter frees it through the internal heap and asserts
    // immediately after the full pet pack is installed; use the paired caps
    // deleter so its PSRAM stack is released by the right allocator.
    vTaskDeleteWithCaps(NULL);
}

static void apply_deferred_startup_pet_asset(void) {
    if (!s_startup_pet_asset_pending) return;
    // Never block the gateway startup owner for an optional asset. Downloads
    // and SPIFFS GC may take minutes on a fragmented partition; the independent
    // worker keeps handshake retries and subsequent runtime state responsive.
    if (s_startup_pet_asset_task) return;
    // The ready pet is a visible part of the normal standby flow.  After the
    // wake model and long-poll worker are live, EchoEar no longer has an 8 KiB
    // contiguous internal block, so use the PSRAM-backed task-stack path used
    // by the other deferred workers instead of silently dropping the asset.
    BaseType_t created = xTaskCreatePinnedToCoreWithCaps(
        startup_pet_asset_task, "maclaw_pet_startup", 8192, NULL, 4,
        &s_startup_pet_asset_task, 1, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (created != pdPASS) {
        s_startup_pet_asset_task = NULL;
        ESP_LOGW(TAG, "cannot start deferred pet asset worker");
    }
}

static bool audio_mime_supported(const char *mime) {
    return !mime || !strcmp(mime, "audio/wav") || !strcmp(mime, "audio/x-wav") ||
           !strcmp(mime, "audio/mpeg") || !strcmp(mime, "audio/mp3");
}

static bool audio_payload_is_mp3(const char *mime, const uint8_t *data, size_t len) {
    if (mime && (!strcmp(mime, "audio/mpeg") || !strcmp(mime, "audio/mp3"))) {
        return true;
    }
    if (!data || len < 2) return false;
    if (len >= 3 && memcmp(data, "ID3", 3) == 0) return true;
    // MPEG audio sync: eleven leading one bits. The decoder validates the
    // layer/version fields; this is only format dispatch when MIME is absent.
    return data[0] == 0xFF && (data[1] & 0xE0) == 0xE0;
}

static esp_err_t play_audio_payload(const char *mime, const uint8_t *data, size_t len) {
    if (!data || len == 0) return ESP_ERR_INVALID_ARG;
    if (audio_payload_is_mp3(mime, data, len)) return mp3_player_play(data, len);
    return board_port_play_wav(data, len);
}

static bool hardware_audio_url_allowed(const char *url) {
    if (!url || url[0] != '/') return false;
    return !strncmp(url, "/api/im-gateway/v1/media/", strlen("/api/im-gateway/v1/media/"));
}

static bool audio_error_is_permanent(esp_err_t err) {
    return err == ESP_ERR_INVALID_ARG || err == ESP_ERR_INVALID_SIZE ||
           err == ESP_ERR_INVALID_RESPONSE || err == ESP_ERR_NOT_SUPPORTED ||
           err == ESP_ERR_INVALID_STATE;
}

static void response_release(http_response_t *response) {
    if (!response) return;
    // HTTP bodies are allocated with heap_caps_malloc() in PSRAM (with an
    // internal-capable fallback). Release them through the same allocator;
    // the ordinary libc heap path can assert while the LCD transfer briefly
    // suspends flash-cache activity during a large pet install.
    heap_caps_free(response->data);
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
    // The long-poll worker intentionally has a PSRAM stack to leave internal
    // memory for TLS/I2S. NVS disables caches during flash operations, where a
    // PSRAM-backed stack is illegal and asserts. Persist only from an
    // internal-stack execution context; the in-memory weather model is already
    // authoritative and a later handshake will safely refresh the cache.
    if (esp_ptr_internal((const void *)&ambient)) {
        save_ambient_weather();
    } else {
        ESP_LOGI(TAG, "ambient weather cache deferred from external-stack poll task");
    }
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
    taskENTER_CRITICAL(&s_task_state_lock);
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
    bool display_clock_valid = s_display_clock_valid;
    time_t display_clock_epoch = s_display_clock_epoch;
    int64_t display_clock_anchor_us = s_display_clock_anchor_us;
    taskEXIT_CRITICAL(&s_task_state_lock);
    time_t now = display_clock_valid
                     ? display_clock_epoch + (time_t)((monotonic_us - display_clock_anchor_us) / 1000000)
                     : 0;
    struct tm local = {0};
    localtime_r(&now, &local);
    char current_time[9] = "--:--:--";
    char date[8] = "--/--";
    const char *weekdays[] = {"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"};
    const char *weekday = "时间同步中";
    if (display_clock_valid) {
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
// interaction. Its dedicated client lane prevents an idle long poll from
// adding several seconds to foreground voice upload and command submission.
static void gateway_poll_task(void *arg) {
    (void)arg;
    unsigned consecutive_failures = 0;
    while (true) {
        if (s_gateway_token[0]) {
            int64_t started_us = esp_timer_get_time();
            esp_err_t err = poll_reply();
            int64_t elapsed_ms = (esp_timer_get_time() - started_us) / 1000;
            if (err != ESP_OK) {
                if (++consecutive_failures >= 2) {
                    board_port_set_service_ready(false);
                    firmware_identity_set_service_ready(false);
                }
                vTaskDelay(pdMS_TO_TICKS(3000));
            } else {
                consecutive_failures = 0;
                board_port_set_service_ready(true);
                firmware_identity_set_service_ready(true);
                if (elapsed_ms >= 4000) continue;
                // Legacy Hub versions return an empty poll immediately. Avoid
                // a tight TLS reconnect loop until that Hub is upgraded to
                // the v1.1 long-poll implementation.
                // During a foreground command, avoid repeated two-second
                // blind spots while still preventing a hot reconnect loop.
                vTaskDelay(pdMS_TO_TICKS(command_display_active() ? 80 : 2000));
            }
        } else {
            consecutive_failures = 0;
            board_port_set_service_ready(false);
            firmware_identity_set_service_ready(false);
            vTaskDelay(pdMS_TO_TICKS(3000));
        }
    }
}

static bool startup_welcome_is_current_boot(cJSON *item, bool welcome_audio) {
    if (!welcome_audio || !cJSON_IsObject(item)) return false;
    const char *boot_session_id = json_string(item, "bootSessionId");
    // Only a greeting explicitly correlated to this cold boot is allowed to
    // control the gate. Reserved-ID messages without a boot ID remain ordinary
    // compatibility audio and cannot release (or be discarded by) startup.
    return boot_session_id && boot_session_id[0] &&
           strcmp(boot_session_id, s_boot_session_id) == 0;
}

static void finish_startup_welcome_gate(const char *reason) {
    bool notify = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_startup_welcome_gate_active) {
        s_startup_welcome_gate_active = false;
        notify = true;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!notify) return;
    ESP_LOGI(TAG, "startup Welcome gate released: %s", reason ? reason : "complete");
    if (s_startup_welcome_done) xSemaphoreGive(s_startup_welcome_done);
}

static bool ensure_gateway_poll_task(void) {
    if (!s_gateway_poll_task) {
        // MP3 is decoded synchronously when an outgoing audio message arrives.
        // The official decoder needs substantially more stack than JSON/TLS
        // polling alone, especially for stereo Layer III frames.  EchoEar's
        // wake model leaves less than a 16 KiB contiguous internal block at
        // this point, so an internal-stack task fails to start and prevents
        // the final ready/standby pet transition.  This worker only performs
        // HTTP/JSON/MP3 work; keep its large stack in PSRAM, like the clock
        // and recovery workers, to preserve internal RAM for Wi-Fi and I2S.
        BaseType_t created = xTaskCreateWithCaps(gateway_poll_task,
                                                 "maclaw_gateway_poll", 16384,
                                                 NULL, 3, &s_gateway_poll_task,
                                                 MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (created != pdPASS) {
            s_gateway_poll_task = NULL;
            ESP_LOGE(TAG, "cannot start gateway poll task");
            return false;
        }
    }
    return true;
}

static void meeting_resume_supervisor_task(void *arg) {
    (void)arg;
    uint32_t retry_ms = MEETING_RESUME_RETRY_INITIAL_MS;
    while (s_meeting_pending) {
        EventBits_t wifi = s_wifi_events ? xEventGroupGetBits(s_wifi_events) : 0;
        bool network_available = (wifi & WIFI_CONNECTED_BIT) != 0;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        if (s_fangtang_use_cellular) network_available = ml307_transport_is_ready();
#endif
        if (!s_setup_portal_active && s_gateway_token[0] && network_available &&
            !s_meeting_task_running && !s_foreground_http_requested) {
            // MultiNet can consume the final internal task-stack block before
            // this low-priority supervisor gets scheduled. Unload it here so
            // the resumable worker can be created; meeting_task() restores it
            // after delivery.
            esp_err_t wake_stop_err = board_port_stop_wake_word();
            if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
                ESP_LOGW(TAG, "offline wake stop before resume worker: %s",
                         esp_err_to_name(wake_stop_err));
            }
            log_heap_snapshot("meeting-resume-before-task-create");
            if (start_meeting_task(true)) {
                // The worker persists progress at every chunk. Wait until that
                // pass finishes before deciding whether another retry is needed.
                while (s_meeting_task_running) vTaskDelay(pdMS_TO_TICKS(500));
                if (!s_meeting_pending) break;
                // A foreground command may have intentionally preempted this
                // pass. Resume quickly after it releases HTTP instead of
                // escalating the outage backoff to several minutes.
                if (s_foreground_http_requested) {
                    while (s_foreground_http_requested) vTaskDelay(pdMS_TO_TICKS(250));
                    retry_ms = MEETING_RESUME_RETRY_INITIAL_MS;
                    continue;
                }
            } else if (!s_setup_portal_active) {
                esp_err_t wake_start_err = board_port_start_wake_word(on_wake_word, NULL);
                if (wake_start_err != ESP_OK && wake_start_err != ESP_ERR_INVALID_STATE) {
                    ESP_LOGW(TAG, "offline wake restart after resume create failure: %s",
                             esp_err_to_name(wake_start_err));
                }
            }
        }
        vTaskDelay(pdMS_TO_TICKS(retry_ms));
        if (retry_ms < MEETING_RESUME_RETRY_MAX_MS) {
            retry_ms *= 2;
            if (retry_ms > MEETING_RESUME_RETRY_MAX_MS) retry_ms = MEETING_RESUME_RETRY_MAX_MS;
        }
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_resume_supervisor_task = NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    vTaskDeleteWithCaps(NULL);
}

static bool ensure_meeting_resume_supervisor(void) {
    if (!s_meeting_pending || s_meeting_resume_supervisor_task) return true;
    // This supervisor only waits and starts a worker. Put its stack in PSRAM
    // so it cannot consume the last contiguous internal block needed by the
    // real upload worker. It never writes flash/NVS, so this is safe.
    BaseType_t created = xTaskCreateWithCaps(meeting_resume_supervisor_task,
                                             "maclaw_meeting_resume", 2048,
                                             NULL, 1,
                                             &s_meeting_resume_supervisor_task,
                                             MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (created != pdPASS) {
        s_meeting_resume_supervisor_task = NULL;
        ESP_LOGE(TAG, "cannot start meeting resume supervisor");
        return false;
    }
    return true;
}

static bool start_gateway_ready_tasks(void) {
    // The handshake queues this boot's optional greeting before it returns.
    // Initialize MultiNet before the outgoing reader can play the greeting or
    // apply a queued hardware-volume update. This removes the cold-start race
    // for the shared audio bus and makes wake readiness the first service
    // published after the authenticated handshake.
    while (s_startup_welcome_done && xSemaphoreTake(s_startup_welcome_done, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_startup_sequence_complete = false;
    s_startup_welcome_gate_active = s_handshake_startup_welcome_queued;
    s_startup_welcome_timed_out = false;
    s_startup_welcome_consumed = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    // Re-assert the board-specific boot artwork at the exact Welcome boundary.
    // It may already be visible from app_main(), but this closes every path where
    // pairing/status work temporarily owned the display before the handshake.
    app_ui_show_startup_screen();
    int64_t wake_start_us = esp_timer_get_time();
    esp_err_t wake_err = board_port_start_wake_word(on_wake_word, NULL);
    // The board API waits for MultiNet's explicit ready flag and returns OK
    // only when inference is listening. INVALID_STATE is not a success signal:
    // it may mean that audio/model initialization failed or that a stale task
    // is still being cleaned up. Publishing the normal standby surface in that
    // state recreates the exact "screen ready, wake still unavailable" gap.
    bool wake_ready = wake_err == ESP_OK;
    ESP_LOGI(TAG, "startup wake initialization complete: ready=%s elapsed=%lu ms",
             wake_ready ? "yes" : "no",
             (unsigned long)((esp_timer_get_time() - wake_start_us) / 1000));
    if (!wake_ready) {
        ESP_LOGW(TAG, "offline wake start failed: %s", esp_err_to_name(wake_err));
    }
    if (!ensure_gateway_poll_task()) {
        if (wake_ready) {
            esp_err_t stop_err = board_port_stop_wake_word();
            if (stop_err != ESP_OK && stop_err != ESP_ERR_INVALID_STATE) {
                ESP_LOGW(TAG, "offline wake cleanup after poll failure: %s",
                         esp_err_to_name(stop_err));
            }
        }
        taskENTER_CRITICAL(&s_task_state_lock);
        s_startup_welcome_gate_active = false;
        s_startup_welcome_timed_out = s_handshake_startup_welcome_queued;
        s_startup_sequence_complete = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        pet("alert");
        board_port_show_text("设备启动失败", "无法启动网关轮询");
        return false;
    }
    if (s_handshake_startup_welcome_queued) {
        ESP_LOGI(TAG, "startup Welcome gate armed; wake listener ready=%s",
                 wake_ready ? "yes" : "no");
        if (xSemaphoreTake(s_startup_welcome_done,
                           pdMS_TO_TICKS(STARTUP_WELCOME_TIMEOUT_MS)) != pdTRUE) {
            taskENTER_CRITICAL(&s_task_state_lock);
            bool still_pending = s_startup_welcome_gate_active;
            s_startup_welcome_gate_active = false;
            s_startup_welcome_timed_out = still_pending;
            taskEXIT_CRITICAL(&s_task_state_lock);
            if (still_pending) {
                ESP_LOGW(TAG, "startup Welcome gate timed out after %u ms; late greeting will be discarded",
                         STARTUP_WELCOME_TIMEOUT_MS);
            }
        }
    } else {
        ESP_LOGI(TAG, "startup Welcome unavailable or disabled; continuing without playback");
    }
    // The normal standby surface is still published last. Touch/wake callbacks
    // remain blocked by s_startup_sequence_complete while the greeting owns
    // the startup surface, although recognition itself is already hot.
    taskENTER_CRITICAL(&s_task_state_lock);
    s_startup_sequence_complete = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    // A phrase can be detected while the optional Welcome audio is still
    // authoritative. EchoEar deliberately unloads MultiNet before delivering
    // that callback so the foreground task can get contiguous internal RAM;
    // re-arm the listener here even when no command was admitted. If it is
    // already listening this is a no-op; if its cleanup is still in flight the
    // bounded restart worker retries after it releases the model.
    schedule_wake_restart();
    firmware_identity_set_service_ready(true);
    board_port_set_service_ready(true);
#if CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD || CONFIG_MACLAW_BOARD_FANGTANG_4G
    board_port_show_ready_prompt(wake_ready ? "设备已就绪" : "设备基本就绪",
                                 wake_ready ? "按激活键说话 双击开会议"
                                            : "唤醒加载失败，可按激活键说话");
#else
    board_port_show_ready_prompt(wake_ready ? "设备已就绪" : "设备基本就绪",
                                 wake_ready ? "点屏说话 双点会议"
                                            : "唤醒加载失败，可点屏说话");
#endif
    if (!wake_ready) schedule_wake_restart();
    return true;
}

static void clock_sync_cb(struct timeval *tv) {
    if (!tv || tv->tv_sec < 1672531200) return; // 2023-01-01 UTC
    taskENTER_CRITICAL(&s_task_state_lock);
    s_display_clock_epoch = tv->tv_sec;
    s_display_clock_anchor_us = esp_timer_get_time();
    s_display_clock_valid = true;
    // An authenticated handshake can refine an earlier Wi-Fi/SNTP clock, but
    // the SNTP monitor must not interpret that valid first sync as incomplete
    // and unnecessarily restart the client. Both sources feed this callback.
    s_clock_sync_complete = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    ESP_LOGI(TAG, "clock synchronized: epoch=%lld", (long long)tv->tv_sec);
}

static void start_ambient_clock_task(void) {
    setenv("TZ", "CST-8", 1);
    tzset();
    if (s_ambient_task) return;

    // Clock cadence must remain independent of animation/render load. A higher
    // priority lets the once-per-second update preempt a slow LCD presentation.
    BaseType_t created = xTaskCreate(ambient_task, "maclaw_ambient", 3072, NULL, 3,
                                     &s_ambient_task);
    if (created != pdPASS) {
        s_ambient_task = NULL;
        ESP_LOGE(TAG, "cannot start ambient clock task");
    }
}

static void apply_gateway_server_time(cJSON *json) {
    cJSON *server_time = json ? cJSON_GetObjectItemCaseSensitive(json, "serverTime") : NULL;
    if (!cJSON_IsNumber(server_time)) return;

    // Milliseconds are exact at current Unix epochs in cJSON's double. Reject
    // malformed or implausible values before changing the device wall clock.
    const double server_time_ms = server_time->valuedouble;
    const double minimum_time_ms = 1672531200000.0; // 2023-01-01 UTC
    const double maximum_time_ms = 4102444800000.0; // 2100-01-01 UTC
    if (server_time_ms < minimum_time_ms || server_time_ms >= maximum_time_ms) {
        ESP_LOGW(TAG, "ignored invalid gateway serverTime: %.0f", server_time_ms);
        return;
    }

    int64_t epoch_ms = (int64_t)server_time_ms;
    struct timeval tv = {
        .tv_sec = (time_t)(epoch_ms / 1000),
        .tv_usec = (suseconds_t)((epoch_ms % 1000) * 1000),
    };
    setenv("TZ", "CST-8", 1);
    tzset();
    if (settimeofday(&tv, NULL) != 0) {
        ESP_LOGW(TAG, "cannot apply gateway serverTime: errno=%d", errno);
        return;
    }
    clock_sync_cb(&tv);
    // ML307 has no ESP-NETIF route for SNTP. Start the display cadence only
    // after authenticated Hub time exists; an unpaired device remains on the
    // recovery portal and does not need a competing once-per-second LCD task.
    start_ambient_clock_task();
    ESP_LOGI(TAG, "clock source: gateway serverTime");
}

static void clock_sync_task(void *arg) {
    (void)arg;
    unsigned attempt = 1;
    while (!s_clock_sync_complete) {
        esp_err_t wait_err = esp_netif_sntp_sync_wait(pdMS_TO_TICKS(CLOCK_SYNC_WAIT_MS));
        if (wait_err == ESP_OK || s_clock_sync_complete) break;

        unsigned int reachability[CONFIG_LWIP_SNTP_MAX_SERVERS] = {0};
        for (unsigned i = 0; i < CONFIG_LWIP_SNTP_MAX_SERVERS; ++i) {
            if (esp_netif_sntp_reachability(i, &reachability[i]) != ESP_OK) {
                reachability[i] = 0;
            }
        }
        ESP_LOGW(TAG,
                 "clock sync attempt %u timed out: wait=%s reachability=%02x/%02x/%02x; retrying",
                 attempt, esp_err_to_name(wait_err),
                 reachability[0], reachability[1], reachability[2]);
        esp_err_t restart_err = esp_netif_sntp_start();
        if (restart_err != ESP_OK) {
            ESP_LOGW(TAG, "SNTP restart failed: %s", esp_err_to_name(restart_err));
        }
        ++attempt;
        vTaskDelay(pdMS_TO_TICKS(CLOCK_SYNC_RETRY_MS));
    }
    s_clock_sync_task = NULL;
    vTaskDeleteWithCaps(NULL);
}

static void start_clock_sync(void) {
    start_ambient_clock_task();
    esp_sntp_config_t config = ESP_NETIF_SNTP_DEFAULT_CONFIG_MULTIPLE(
        3, ESP_SNTP_SERVER_LIST("ntp.aliyun.com", "time.cloudflare.com", "pool.ntp.org"));
    config.sync_cb = clock_sync_cb;
    esp_err_t err = esp_netif_sntp_init(&config);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "SNTP init failed: %s", esp_err_to_name(err));
    } else if (!s_clock_sync_task) {
        // This monitor mostly sleeps; keep its stack in PSRAM so clock recovery
        // cannot take the last internal block needed by Wi-Fi/TLS or ESP-SR.
        BaseType_t created = xTaskCreateWithCaps(clock_sync_task, "maclaw_clock_sync",
                                                 3072, NULL, 3,
                                                 &s_clock_sync_task,
                                                 MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (created != pdPASS) {
            s_clock_sync_task = NULL;
            ESP_LOGE(TAG, "cannot start clock sync monitor task");
        }
    }
}

static void save_ambient_weather(void) {
    if (!nvs_lock()) {
        ESP_LOGW(TAG, "weather cache save deferred: NVS busy");
        return;
    }
    nvs_handle_t nvs;
    if (nvs_open("maclaw", NVS_READWRITE, &nvs) != ESP_OK) {
        nvs_unlock();
        return;
    }
    (void)nvs_set_str(nvs, "weather", s_weather_summary);
    (void)nvs_set_str(nvs, "weather_loc", s_weather_location);
    (void)nvs_set_i32(nvs, "weather_temp", s_weather_temperature_c);
    (void)nvs_set_i64(nvs, "weather_exp", s_weather_expires_at_ms);
    (void)nvs_commit(nvs);
    nvs_close(nvs);
    nvs_unlock();
}

static void wake_restart_task(void *arg) {
    (void)arg;
    // Let the meeting worker delete its internal stack before MultiNet claims
    // memory again. This task uses a PSRAM stack and does not write flash.
    vTaskDelay(pdMS_TO_TICKS(250));
    esp_err_t err = ESP_FAIL;
    unsigned attempt = 1;
    bool waiting_for_foreground = false;
    while (attempt <= 12 && !s_setup_portal_active) {
        taskENTER_CRITICAL(&s_task_state_lock);
        bool foreground_active = s_interaction_task != NULL || s_foreground_http_requested;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (foreground_active || meeting_is_active()) {
            if (!waiting_for_foreground) {
                ESP_LOGI(TAG, "offline wake restart waiting for foreground audio owner");
                waiting_for_foreground = true;
            }
            vTaskDelay(pdMS_TO_TICKS(100));
            continue;
        }
        waiting_for_foreground = false;
        err = board_port_start_wake_word(on_wake_word, NULL);
        if (err == ESP_OK) break;
        ESP_LOGW(TAG, "offline wake restart attempt %u/12 failed: %s",
                 attempt, esp_err_to_name(err));
        ++attempt;
        vTaskDelay(pdMS_TO_TICKS(500));
    }
    if (err == ESP_OK) {
        ESP_LOGI(TAG, "offline wake restarted after foreground interaction");
    } else if (!s_setup_portal_active) {
        ESP_LOGE(TAG, "offline wake restart exhausted: %s", esp_err_to_name(err));
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_wake_restart_scheduled = false;
    bool retry_needed = err != ESP_OK && !s_setup_portal_active &&
                        s_interaction_task == NULL && !s_foreground_http_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (retry_needed) {
        // A temporary fragmented-heap or delayed model teardown should not
        // permanently disable hands-free input. Start a fresh bounded retry
        // cycle after this worker releases its own stack.
        vTaskDelay(pdMS_TO_TICKS(1000));
        schedule_wake_restart();
    }
    vTaskDeleteWithCaps(NULL);
}

static void schedule_wake_restart(void) {
    if (s_setup_portal_active || !s_startup_sequence_complete) return;
    taskENTER_CRITICAL(&s_task_state_lock);
    bool already_scheduled = s_wake_restart_scheduled;
    if (!already_scheduled) s_wake_restart_scheduled = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (already_scheduled) return;
    BaseType_t created = xTaskCreateWithCaps(wake_restart_task, "maclaw_wake_restart",
                                             2048, NULL, 2, NULL,
                                             MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (created != pdPASS) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_wake_restart_scheduled = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "cannot schedule offline wake restart");
    } else {
        ESP_LOGI(TAG, "offline wake restart scheduled");
    }
}

static void load_ambient_weather(void) {
    if (!nvs_lock()) return;
    nvs_handle_t nvs;
    if (nvs_open("maclaw", NVS_READONLY, &nvs) != ESP_OK) {
        nvs_unlock();
        return;
    }
    size_t summary_len = sizeof(s_weather_summary);
    size_t location_len = sizeof(s_weather_location);
    int32_t temperature = 0;
    bool found = nvs_get_str(nvs, "weather", s_weather_summary, &summary_len) == ESP_OK;
    (void)nvs_get_str(nvs, "weather_loc", s_weather_location, &location_len);
    (void)nvs_get_i32(nvs, "weather_temp", &temperature);
    (void)nvs_get_i64(nvs, "weather_exp", &s_weather_expires_at_ms);
    nvs_close(nvs);
    nvs_unlock();
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

static void finish_result_speech_transaction(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return;
    unsigned missing = 0;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_result_speech_parts_remaining > 0 &&
        !strcmp(s_result_speech_reply_to, reply_to)) {
        missing = s_result_speech_parts_remaining;
        s_result_speech_reply_to[0] = '\0';
        s_result_speech_parts_remaining = 0;
        s_result_speech_deadline_us = 0;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    ESP_LOGW(TAG, "result speech transaction closed early: replyTo=%s missing=%u",
             reply_to, missing);
}

// Closing the visible result is also an explicit choice to leave that command
// behind.  Do not let a delayed TTS part from the same reply pull audio back
// into the ambient screen minutes later.  Clearing the exact correlation makes
// such queued parts ordinary orphaned command output, which the poller safely
// acknowledges without playback.
static void dismiss_result_speech_transaction(void) {
    unsigned missing = 0;
    char reply_to[COMMAND_REPLY_ID_CAPACITY] = {0};
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_result_speech_parts_remaining > 0 && s_result_speech_reply_to[0]) {
        missing = s_result_speech_parts_remaining;
        strlcpy(reply_to, s_result_speech_reply_to, sizeof(reply_to));
        s_result_speech_reply_to[0] = '\0';
        s_result_speech_parts_remaining = 0;
        s_result_speech_deadline_us = 0;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (missing) {
        ESP_LOGI(TAG, "result speech dismissed with response: replyTo=%s skipped=%u",
                 reply_to, missing);
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

#if CONFIG_MACLAW_BOARD_FANGTANG_4G
// The original 无名星智 firmware uses NVS to keep the Wi-Fi/ML307 choice across
// resets. Preserve a prior stock selection when this app is flashed without
// erasing NVS, then migrate it to the MaClaw namespace for later boots.
static void load_fangtang_network_choice(void) {
    uint8_t choice = CONFIG_MACLAW_FANGTANG_DEFAULT_4G ? 1 : 0;
    bool loaded = false;
    nvs_handle_t nvs;
    if (nvs_open("maclaw", NVS_READONLY, &nvs) == ESP_OK) {
        uint8_t saved = 0;
        if (nvs_get_u8(nvs, "net_transport", &saved) == ESP_OK && saved <= 1) {
            choice = saved;
            loaded = true;
        }
        nvs_close(nvs);
    }
    if (!loaded && nvs_open("network", NVS_READONLY, &nvs) == ESP_OK) {
        int32_t stock_type = 0;
        // Xiaozhi DualNetworkBoard stores ML307 as 1 and Wi-Fi as 0.
        if (nvs_get_i32(nvs, "type", &stock_type) == ESP_OK &&
            (stock_type == 0 || stock_type == 1)) {
            choice = stock_type == 1 ? 1 : 0;
        }
        nvs_close(nvs);
    }
    s_fangtang_use_cellular = choice != 0;
    ESP_LOGI(TAG, "Fangtang saved network choice: %s",
             s_fangtang_use_cellular ? "4G" : "Wi-Fi");
}

static void apply_fangtang_cellular_gateway_compatibility(void) {
    // ML307R-DL-MBRH0S01 cannot negotiate the Hub's current ECDSA-only TLS
    // certificate with its built-in HTTPS engine. The Hub service also exposes
    // a directly reachable HTTP listener on 9399 for the modem-owned IP stack.
    // Preserve every user-configured host and keep Wi-Fi on normal HTTPS; only
    // translate the standard production endpoint while 4G is selected.
    if (s_fangtang_use_cellular &&
        (!strcmp(s_gateway_url, "https://hub.mypapers.top") ||
         !strcmp(s_gateway_url, "http://hub.mypapers.top"))) {
        strlcpy(s_gateway_url, "http://hub.mypapers.top:9399", sizeof(s_gateway_url));
        ESP_LOGW(TAG, "Fangtang 4G uses Hub direct HTTP endpoint because ML307 TLS lacks ECDSA support");
    }
}

static void save_fangtang_network_choice(bool use_cellular) {
    nvs_handle_t nvs;
    if (nvs_open("maclaw", NVS_READWRITE, &nvs) == ESP_OK) {
        if (nvs_set_u8(nvs, "net_transport", use_cellular ? 1 : 0) == ESP_OK) {
            (void)nvs_commit(nvs);
        }
        nvs_close(nvs);
    }
    s_fangtang_use_cellular = use_cellular;
}
#endif

static bool is_valid_setup_selected_ssid(const char *ssid) {
    if (!ssid || !ssid[0] || strlen(ssid) > WIFI_SSID_MAX_LEN) return false;
    for (const unsigned char *p = (const unsigned char *)ssid; *p; ++p) {
        // SSIDs may contain UTF-8, but controls can alter form/log parsing and
        // are not present in the visible scan list presented to the user.
        if (*p < 0x20 || *p == 0x7f) return false;
    }
    return true;
}

static void load_device_id(void) {
    // Always derive the physical identity from the chip MAC. Reading an NVS
    // copy first makes cloned factory NVS partitions duplicate client IDs across
    // devices, which defeats independent tokens. Keep a best-effort copy only
    // for diagnostics and future migrations.
    uint8_t mac[6] = {0};
    if (esp_read_mac(mac, ESP_MAC_WIFI_STA) == ESP_OK) {
        snprintf(s_device_id, sizeof(s_device_id), "esp32s3-%02x%02x%02x%02x%02x%02x",
                 mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]);
        nvs_handle_t nvs;
        if (nvs_open("maclaw", NVS_READWRITE, &nvs) == ESP_OK) {
            (void)nvs_set_str(nvs, "device_id", s_device_id);
            (void)nvs_commit(nvs);
            nvs_close(nvs);
        }
    }
    if (!s_device_id[0]) snprintf(s_device_id, sizeof(s_device_id), "%s", CONFIG_MACLAW_CLIENT_ID);
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
        // The pet cache keeps one metadata file plus up to eight animation
        // frames open over its save/load lifetime. Four descriptors was enough
        // for meeting audio, but it makes fopen() fail partway through a full
        // eight-frame pet update while the HTTP/audio tasks also hold files.
        .max_files = 16,
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
    // Gateway polling can persist weather/glyph state while a meeting upload
    // advances its recovery cursor. Serialize every NVS writer: an overlapping
    // flash commit can reset the MCU exactly at a successful chunk boundary.
    if (!nvs_lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open("maclaw", NVS_READWRITE, &nvs);
    if (err != ESP_OK) {
        nvs_unlock();
        return err;
    }
    err = nvs_set_u8(nvs, "meet_pending", pending ? 1 : 0);
    if (err == ESP_OK) err = nvs_set_str(nvs, "meet_id", recording_id ? recording_id : "");
    if (err == ESP_OK) err = nvs_set_i32(nvs, "meet_next", next_chunk);
    if (err == ESP_OK) err = nvs_set_i32(nvs, "meet_phase", phase);
    if (err == ESP_OK) err = nvs_commit(nvs);
    nvs_close(nvs);
    nvs_unlock();
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

static esp_err_t gateway_handshake(bool cold_start) {
	char payload[4096];
    char boot_field[64] = {0};
    http_response_t response = {0};
    if (cold_start) {
        s_handshake_startup_welcome_queued = false;
        snprintf(boot_field, sizeof(boot_field), "\"bootSessionId\":\"%s\",", s_boot_session_id);
    }
    // The screen renderer keeps several DMA buffers in internal RAM. Asking
    // Hub for embedded RGB565+A8 pet frames forces a 100+ KiB response and starves
    // the TLS allocation on this device. The built-in pet stays visible, while
    // the small handshake response still delivers city/weather immediately.
    int request_len = snprintf(payload, sizeof(payload),
        "{\"clientId\":\"%s\",\"clientName\":\"ESP32-S3 Pet\",%s\"protocolVersion\":\"1.1\","
        "\"clientCapabilities\":{\"input\":{\"modalities\":[\"text\",\"audio\"],"
        "\"audio\":{\"mimeTypes\":[\"audio/wav\"],\"sampleRates\":[16000],\"channels\":1}},"
		"\"output\":{\"modalities\":[\"text\",\"audio\",\"image\"],\"preferred\":[\"audio\",\"image\",\"text\"],"
		"\"combinations\":[[\"text\"],[\"audio\",\"text\"],[\"image\"]],\"text\":{\"maxChars\":240,\"markdown\":false,\"locale\":\"zh-CN\"},"
		"\"audio\":{\"mimeTypes\":[\"audio/wav\",\"audio/mpeg\",\"audio/mp3\"],\"sampleRates\":[16000,22050,24000,32000,44100,48000],\"channels\":2,\"playback\":true,"
		"\"deliveryModes\":[\"inline\",\"url\"],\"maxInlineBytes\":8192,\"maxDownloadBytes\":524288},"
		"\"image\":{\"mimeTypes\":[\"" RESPONSE_IMAGE_MIME "\"],\"maxWidth\":64,\"maxHeight\":64,\"animated\":false}},"
        "\"features\":{\"petStates\":true,\"petAnimation\":true,"
        "\"petAsset\":true,\"petAssetMaxFrames\":8,"
        "\"ambientDisplay\":true,\"meetingRecorder\":true,\"volumeControl\":"
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        "false"
#else
        "true"
#endif
        "}},\"tools\":["
        "{\"name\":\"alarm_create\",\"description\":\"Create one alarm on this device. Resolve relative spoken time to an absolute future epoch in the device timezone before calling.\",\"risk\":\"write\",\"timeoutMs\":5000,\"inputSchema\":{\"type\":\"object\",\"properties\":{\"triggerAtEpochMs\":{\"type\":\"integer\",\"description\":\"Absolute Unix epoch milliseconds in the future\"},\"label\":{\"type\":\"string\",\"maxLength\":48}},\"required\":[\"triggerAtEpochMs\"]},\"outputSchema\":{\"type\":\"object\"}},"
        "{\"name\":\"alarm_clear_all\",\"description\":\"Clear every alarm on this device.\",\"risk\":\"write\",\"timeoutMs\":5000,\"inputSchema\":{\"type\":\"object\",\"properties\":{}}},"
        "{\"name\":\"alarm_clear\",\"description\":\"Clear one alarm by its current 1-based list index.\",\"risk\":\"write\",\"timeoutMs\":5000,\"inputSchema\":{\"type\":\"object\",\"properties\":{\"index\":{\"type\":\"integer\",\"minimum\":1}},\"required\":[\"index\"]}},"
        "{\"name\":\"alarm_list\",\"description\":\"List all alarms on this device in chronological order with 1-based indices.\",\"risk\":\"read\",\"timeoutMs\":5000,\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}]}", s_device_id,
        boot_field);
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
    apply_gateway_server_time(json);
    cJSON *startup_welcome = cJSON_GetObjectItemCaseSensitive(json, "startupWelcomeQueued");
    bool startup_welcome_queued = cJSON_IsTrue(startup_welcome);
    if (cold_start) {
        s_handshake_startup_welcome_queued = startup_welcome_queued;
        ESP_LOGI(TAG, "gateway startup Welcome queued: %s",
                 s_handshake_startup_welcome_queued ? "yes" : "no");
    } else if (startup_welcome_queued) {
        // Runtime capability refreshes deliberately omit bootSessionId. Treat
        // an unexpected legacy response as informational; it must never re-arm
        // or otherwise mutate the completed cold-start Welcome transaction.
        ESP_LOGW(TAG, "runtime handshake ignored unexpected startup Welcome flag");
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
        ESP_LOGI(TAG, "client capabilities accepted: output=%s+audio maxChars=240",
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
    }
    cJSON *pet_profile = cJSON_GetObjectItemCaseSensitive(json, "pet");
    const char *skin = pet_profile ? json_string(pet_profile, "skin") : NULL;
    cJSON *motion = pet_profile ? cJSON_GetObjectItemCaseSensitive(pet_profile, "motionEnabled") : NULL;
    if (skin) board_port_set_pet_profile(skin, !motion || cJSON_IsTrue(motion));
    cJSON *pet_asset = cJSON_GetObjectItemCaseSensitive(json, "petAsset");
    if (cold_start) {
        s_startup_pet_asset_pending = true;
        if (!s_startup_pet_asset_ref) {
            s_startup_pet_asset_ref = heap_caps_calloc(
                1, sizeof(*s_startup_pet_asset_ref), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
            if (!s_startup_pet_asset_ref) {
                s_startup_pet_asset_ref = calloc(1, sizeof(*s_startup_pet_asset_ref));
            }
        }
        s_startup_pet_asset_present = s_startup_pet_asset_ref &&
                                      cJSON_IsObject(pet_asset) &&
                                      parse_pet_asset_ref(pet_asset, s_startup_pet_asset_ref);
        if (cJSON_IsObject(pet_asset) && !s_startup_pet_asset_present) {
            ESP_LOGW(TAG, "startup pet asset descriptor is invalid; cached asset will be cleared after wake readiness");
        }
        ESP_LOGI(TAG, "startup pet asset deferred until wake ready: %s",
                 s_startup_pet_asset_present ? s_startup_pet_asset_ref->revision : "none");
    } else if (cJSON_IsObject(pet_asset)) {
        esp_err_t asset_err = apply_pet_asset_ref(pet_asset);
        if (asset_err != ESP_OK) ESP_LOGW(TAG, "handshake pet asset ignored: %s", esp_err_to_name(asset_err));
    } else {
        // Runtime refreshes remain authoritative and can update the visible
        // asset synchronously; only the cold-start path is latency-sensitive.
        esp_err_t asset_err = clear_applied_pet_asset();
        if (asset_err == ESP_OK) s_loaded_pet_asset_revision[0] = '\0';
        if (asset_err != ESP_OK) ESP_LOGW(TAG, "handshake pet asset clear failed: %s", esp_err_to_name(asset_err));
    }
    apply_ambient_json(cJSON_GetObjectItemCaseSensitive(json, "ambient"));
    cJSON_Delete(json);
    response_release(&response);
    log_heap_snapshot("handshake-ok");
    if (cold_start) {
        // The caller initializes ESP-SR immediately after this function
        // returns. Keep optional media work outside the authenticated response
        // parsing path; gateway_startup_task applies it only after wake ready.
        ESP_LOGI(TAG, "cold-start handshake essentials complete; optional pet asset remains deferred");
    }
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
    snprintf(client_header, sizeof(client_header), "%s", s_device_id);
    // pair endpoint needs a client ID header rather than authorization; use a
    // short dedicated request because the normal helper only emits fixed headers.
    char url[URL_CAPACITY];
    int n = snprintf(url, sizeof(url), "%s/api/device-gateway/v1/pair/voice", s_gateway_url);
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
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    if (s_fangtang_use_cellular) {
        bool truncated = false;
        esp_err_t err = ml307_transport_http_request(
            "POST", url, "audio/wav", NULL, "X-MaClaw-Client-ID", client_header,
            wav, wav_len, response.data, response.capacity, &response.len,
            &response.status, &truncated, 30000, true);
        response.truncated = truncated;
        xSemaphoreGive(s_http_mutex);
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
#endif
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
    cJSON_AddStringToObject(body, "clientId", s_device_id);
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

static bool voice_upload_should_retry(esp_err_t err, int status) {
    switch (err) {
        case ESP_ERR_TIMEOUT:
        case ESP_ERR_HTTP_CONNECT:
        case ESP_ERR_HTTP_WRITE_DATA:
        case ESP_ERR_HTTP_FETCH_HEADER:
        case ESP_ERR_HTTP_CONNECTING:
        case ESP_ERR_HTTP_EAGAIN:
        case ESP_ERR_HTTP_CONNECTION_CLOSED:
        case ESP_ERR_HTTP_READ_TIMEOUT:
        case ESP_ERR_HTTP_INCOMPLETE_DATA:
            return true;
        default:
            break;
    }
    return err == ESP_OK &&
           (status == 408 || status == 425 || status == 429 || status >= 500);
}

static void voice_upload_retry_delay(unsigned attempt) {
    vTaskDelay(pdMS_TO_TICKS(250u << (attempt - 1u)));
}

static esp_err_t upload_voice(const uint8_t *wav, size_t wav_len, char *media_id, size_t media_id_cap) {
    int64_t upload_started_us = esp_timer_get_time();
    cJSON *body = cJSON_CreateObject();
    cJSON_AddStringToObject(body, "clientId", s_device_id);
    cJSON_AddStringToObject(body, "type", "voice");
    cJSON_AddStringToObject(body, "fileName", "voice.wav");
    cJSON_AddStringToObject(body, "mimeType", "audio/wav");
    cJSON_AddNumberToObject(body, "sizeBytes", (double)wav_len);
    char *payload = cJSON_PrintUnformatted(body);
    cJSON_Delete(body);
    if (!payload) return ESP_ERR_NO_MEM;
    http_response_t response = {0};
    esp_err_t err = ESP_FAIL;
    for (unsigned attempt = 1; attempt <= VOICE_UPLOAD_RETRY_COUNT; ++attempt) {
        err = request("POST", "/api/im-gateway/v1/media/upload-url", "application/json",
                      payload, strlen(payload), &response);
        if (err == ESP_OK && response.status == 200) break;
        bool retry = voice_upload_should_retry(err, response.status) &&
                     attempt < VOICE_UPLOAD_RETRY_COUNT;
        ESP_LOGW(TAG, "media prepare attempt %u/%u failed: err=%s status=%d retry=%s",
                 attempt, VOICE_UPLOAD_RETRY_COUNT, esp_err_to_name(err), response.status,
                 retry ? "yes" : "no");
        response_release(&response);
        if (!retry) break;
        voice_upload_retry_delay(attempt);
    }
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
    ESP_LOGI(TAG, "voice upload prepared: wav=%u bytes elapsed=%lldms",
             (unsigned)wav_len, (long long)((esp_timer_get_time() - upload_started_us) / 1000));
    char id_copy[96];
    char url_copy[URL_CAPACITY];
    strlcpy(id_copy, id, sizeof(id_copy));
    strlcpy(url_copy, url, sizeof(url_copy));
    cJSON_Delete(json);
    response_release(&response);
    http_response_t put_response = {0};
    for (unsigned attempt = 1; attempt <= VOICE_UPLOAD_RETRY_COUNT; ++attempt) {
        err = request("PUT", url_copy, "audio/wav", (const char *)wav, wav_len, &put_response);
        if (err == ESP_OK && (put_response.status == 200 || put_response.status == 201)) break;
        bool retry = voice_upload_should_retry(err, put_response.status) &&
                     attempt < VOICE_UPLOAD_RETRY_COUNT;
        ESP_LOGW(TAG, "media upload attempt %u/%u failed: err=%s status=%d wav=%u retry=%s",
                 attempt, VOICE_UPLOAD_RETRY_COUNT, esp_err_to_name(err), put_response.status,
                 (unsigned)wav_len, retry ? "yes" : "no");
        response_release(&put_response);
        if (!retry) break;
        voice_upload_retry_delay(attempt);
    }
    if (err != ESP_OK || (put_response.status != 200 && put_response.status != 201)) {
        ESP_LOGE(TAG, "media upload failed: err=%s status=%d", esp_err_to_name(err), put_response.status);
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&put_response);
        return result;
    }
    strlcpy(media_id, id_copy, media_id_cap);
    int64_t upload_elapsed_ms = (esp_timer_get_time() - upload_started_us) / 1000;
    unsigned throughput_kbps = upload_elapsed_ms > 0
                                   ? (unsigned)((wav_len * 1000ULL / (unsigned long long)upload_elapsed_ms) / 1024ULL)
                                   : 0;
    ESP_LOGI(TAG, "voice upload complete: media=%s wav=%u bytes elapsed=%lldms throughput=%uKiB/s",
             id_copy, (unsigned)wav_len, (long long)upload_elapsed_ms, throughput_kbps);
    response_release(&put_response);
    return ESP_OK;
}

static esp_err_t send_voice_event(const char *media_id, const char *event_id,
                                  char *reply_to, size_t reply_to_cap) {
    cJSON *body = cJSON_CreateObject();
    cJSON_AddStringToObject(body, "clientId", s_device_id);
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
    int64_t submit_started_us = esp_timer_get_time();
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
    // MaClawSrv returns the canonical `maclawMessageId`, while the embedded
    // Hub relay returns the accepted client message as `messageId`.  Both
    // identify the same reply correlation key.  Keep accepting the canonical
    // response first, but do not reject a command merely because it travelled
    // through the Hub-compatible response shape.
    const char *reply_message_id = json ? json_string(json, "maclawMessageId") : NULL;
    if ((!reply_message_id || !reply_message_id[0]) && json) {
        reply_message_id = json_string(json, "messageId");
    }
    if (!cJSON_IsTrue(accepted)) {
        cJSON_Delete(json);
        response_release(&response);
        return ESP_ERR_INVALID_RESPONSE;
    }
    if (reply_to && reply_to_cap > 0) {
        // Some older gateways acknowledge the event without echoing either ID.
        // Their outgoing reply is correlated to the submitted message/event ID,
        // so the idempotency key remains the safe protocol-compatible fallback.
        strlcpy(reply_to,
                reply_message_id && reply_message_id[0] ? reply_message_id : event_id,
                reply_to_cap);
    }
    ESP_LOGI(TAG, "voice command accepted: event=%s replyTo=%s duplicate=%s elapsed=%lldms",
             event_id,
             reply_message_id && reply_message_id[0] ? reply_message_id : event_id,
             cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(json, "duplicate")) ? "yes" : "no",
             (long long)((esp_timer_get_time() - submit_started_us) / 1000));
    cJSON_Delete(json);
    response_release(&response);
    return ESP_OK;
}

static const char *command_submit_error_detail(esp_err_t err) {
    switch (err) {
        case ESP_ERR_TIMEOUT: return "网关响应超时，请稍后重试";
        case ESP_ERR_HTTP_CONNECT: return "网络连接失败，请检查 Wi-Fi";
        case ESP_ERR_HTTP_WRITE_DATA: return "语音发送中断，请重新尝试";
        case ESP_ERR_HTTP_FETCH_HEADER:
        case ESP_ERR_HTTP_READ_TIMEOUT:
        case ESP_ERR_HTTP_CONNECTION_CLOSED:
        case ESP_ERR_HTTP_INCOMPLETE_DATA:
            return "网关连接不稳定，请重新尝试";
        case ESP_ERR_NO_MEM: return "设备内存不足，请重启后重试";
        case ESP_ERR_INVALID_RESPONSE: return "网关响应格式不兼容";
        case ESP_ERR_INVALID_STATE: return "请求已取消或网络状态异常";
        case ESP_FAIL: return "网关拒绝请求或服务异常";
        default: return esp_err_to_name(err);
    }
}

static esp_err_t send_text_event(const char *text, const char *reply_to) {
    if (!text || !text[0]) return ESP_ERR_INVALID_ARG;
    cJSON *body = cJSON_CreateObject();
    char event_id[80];
    snprintf(event_id, sizeof(event_id), "text-%lld", (long long)esp_timer_get_time());
    cJSON_AddStringToObject(body, "clientId", s_device_id);
    cJSON_AddStringToObject(body, "eventId", event_id);
    cJSON_AddStringToObject(body, "messageId", event_id);
    cJSON_AddStringToObject(body, "conversationId", CONFIG_MACLAW_CONVERSATION_ID);
	if (reply_to && reply_to[0]) {
		// Cancellation is a control for the active command, not an independent
		// result-producing turn. Preserve that relationship end-to-end so Hub/GUI
		// can suppress its acknowledgement even if the new control message ID is
		// absent from an older relay envelope.
		cJSON_AddStringToObject(body, "replyTo", reply_to);
		cJSON_AddStringToObject(body, "replyToMessageId", reply_to);
	}
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
    /* The boot greeting is queued by the handshake and should be consumed
     * immediately.  A long-poll request made after the hardware-config item
     * can otherwise remain stuck behind a flaky keep-alive socket until the
     * Welcome gate expires, even though the greeting is already queued. */
    int poll_timeout_seconds = s_startup_welcome_gate_active
                                   ? 0
                                   : (command_display_active() ? 2 : 5);
    int64_t poll_started_us = esp_timer_get_time();
    long long previous_cursor = s_cursor;
	// A 64x64 RGB565 image expands to about 10.7 KiB in JSON. Fetch one
    // message at a time and retain enough space for queued dynamic glyphs and
    // rich replies. A full glyph preload observed in the field exceeded the
    // old 16 KiB buffer and pinned cursor zero forever, starving later replies.
    snprintf(path, sizeof(path), "/api/im-gateway/v1/outgoing?clientId=%s&cursor=%lld&limit=1&timeout=%d",
             s_device_id, s_cursor, poll_timeout_seconds);
    http_response_t response;
    esp_err_t err = request_with_capacity("GET", path, NULL, NULL, 0,
                                          OUTGOING_RESPONSE_CAPACITY, &response);
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
    cJSON *delivered_ack_ids = cJSON_CreateArray();
    cJSON *failed_ack_ids = cJSON_CreateArray();
    if (!delivered_ack_ids || !failed_ack_ids) {
        cJSON_Delete(delivered_ack_ids);
        cJSON_Delete(failed_ack_ids);
        cJSON_Delete(json);
        response_release(&response);
        return ESP_ERR_NO_MEM;
    }
    bool keep_cursor_for_retry = false;
    int message_count = cJSON_GetArraySize(messages);
    if (message_count > 0) {
        ESP_LOGI(TAG, "gateway poll: messages=%d cursor=%lld->%lld elapsed=%lldms",
                 message_count, previous_cursor, parsed_cursor,
                 (long long)((esp_timer_get_time() - poll_started_us) / 1000));
    }
    cJSON *item = NULL;
    cJSON_ArrayForEach(item, messages) {
        const char *type = json_string(item, "type");
        const char *text = json_string(item, "text");
        const char *audio_data = json_string(item, "file_data");
        if (!audio_data) audio_data = json_string(item, "data");
        // Some producers serialize an absent inline body as an empty string
        // while also supplying the media URL. Treat that as absent so the URL
        // path remains usable instead of permanently failing an empty Base64
        // payload and discarding valid audio.
        if (audio_data && !audio_data[0]) audio_data = NULL;
        const char *audio_mime = json_string(item, "mime_type");
        if (!audio_mime) audio_mime = json_string(item, "mimeType");
        const char *audio_url = json_string(item, "url");
        bool invalid_audio_url = audio_url && !hardware_audio_url_allowed(audio_url);
        if (invalid_audio_url) {
            ESP_LOGW(TAG, "ignored unsafe server audio URL");
            audio_url = NULL;
        }
        const char *reply_to = outgoing_reply_correlation(item);
        const char *id = json_string(item, "id");
        cJSON *extra = cJSON_GetObjectItemCaseSensitive(item, "extra");
        bool audio_message = type && (!strcmp(type, "voice") || !strcmp(type, "audio"));
        bool speech_end_message = type && !strcmp(type, "speech_end");
		bool image_message = type && !strcmp(type, "image");
		bool image_handled = !image_message;
		bool image_permanently_invalid = false;
		bool text_message = type && !strcmp(type, "text");
		bool text_handled = !text_message;
		bool text_permanently_invalid = false;
		bool tool_message = type && !strcmp(type, "tool_call");
		bool tool_handled = !tool_message;
        bool hardware_config_message = type && !strcmp(type, "hardware_config");
        bool hardware_config_handled = !hardware_config_message;
        bool hardware_config_permanently_invalid = false;
        bool welcome_audio = id && (!strncmp(id, "mc_welcome_", 11) || !strncmp(id, "hub_welcome_", 12));
		bool preview_audio = cJSON_IsObject(extra) &&
			cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(extra, "hardware_audio_preview"));
		bool startup_welcome = !preview_audio &&
			startup_welcome_is_current_boot(item, welcome_audio);
		// Reserved Welcome messages are boot-scoped transactions. A greeting left
		// pending by an interrupted ACK from an earlier boot must never be treated
		// as ordinary speech: doing so plays the stale greeting and then this boot's
		// greeting. Explicit GUI previews are exempt and remain user-triggered.
		bool stale_startup_welcome = welcome_audio && !preview_audio && !startup_welcome;
		bool discard_startup_welcome = stale_startup_welcome;
		bool startup_welcome_already_consumed = false;
		if (startup_welcome) {
			taskENTER_CRITICAL(&s_task_state_lock);
			startup_welcome_already_consumed = s_startup_welcome_consumed;
			discard_startup_welcome = s_startup_welcome_timed_out ||
			                          startup_welcome_already_consumed;
			taskEXIT_CRITICAL(&s_task_state_lock);
			if (discard_startup_welcome) {
				// Never play a boot greeting after MultiNet has been started. ACK it
				// as handled so a late delivery cannot retry forever. The same rule
				// also turns an ACK retry after successful playback into a silent,
				// idempotent delivery instead of replaying the greeting.
				ESP_LOGW(TAG, "%s startup Welcome discarded: id=%s",
				         startup_welcome_already_consumed ? "already consumed" : "late",
					         id);
			}
		} else if (stale_startup_welcome) {
			ESP_LOGW(TAG, "stale or unscoped startup Welcome discarded: id=%s", id);
		}
        // Resolve correlation once. The hand-off helper deliberately waits for
        // up to 200 ms while interaction_task publishes its accepted message ID;
        // calling it again for the same item adds avoidable poll latency and can
        // make a multipart spoken reply feel stalled.
        bool cancelled_reply = cancelled_command_reply_matches(reply_to);
        bool active_reply = !cancelled_reply &&
                            active_command_reply_matches_after_handoff(reply_to);
        bool result_speech_reply = !cancelled_reply && audio_message &&
                                   result_speech_reply_matches(reply_to);
		if (speech_end_message) {
			finish_result_speech_transaction(reply_to);
			tool_handled = true;
		}
        // A reboot has no live correlation for the command that produced an
        // older queued result. Treat every unmatched replyTo as an orphan and
        // acknowledge it silently. This also protects against older Hub/GUI
        // versions that do not clear their runtime queue on cold handshake.
        bool orphaned_command_result = reply_to && reply_to[0] &&
                                       !active_reply && !result_speech_reply &&
                                       !cancelled_reply;
        // Non-system speech may arrive while the foreground command still owns
        // the display/audio bus. Preserve those messages for retry. Greetings
        // and explicit GUI previews are safe to play during initialization or
        // while a previous command result remains on screen.
        // Correlated speech normally follows terminal text. The result page
        // arms a bounded correlation/count gate before waking the command
        // worker, so only that answer's declared parts may play while the
        // result surface remains foregrounded.
        bool audio_can_play = !command_display_active() || welcome_audio ||
                              preview_audio || active_reply || result_speech_reply;
        bool audio_handled = discard_startup_welcome ||
                             (audio_message && orphaned_command_result);
        bool audio_permanently_invalid = false;
        bool progress = outgoing_message_is_progress(item);
        bool final = outgoing_message_is_final(item);
        const char *skin = json_string(item, "pet_skin");
        cJSON *motion = cJSON_GetObjectItemCaseSensitive(item, "pet_motion_enabled");
        cJSON *metadata = cJSON_GetObjectItemCaseSensitive(item, "metadata");
        const char *turn = cJSON_IsObject(metadata) ? json_string(metadata, "acp_turn") : NULL;
        if (!turn) turn = json_string(item, "acp_turn");
        cJSON *seq_item = cJSON_GetObjectItemCaseSensitive(item, "seq");
        long long message_seq = cJSON_IsNumber(seq_item) ? (long long)seq_item->valuedouble : 0;
        ESP_LOGI(TAG, "outgoing message: id=%s seq=%lld type=%s replyTo=%s progress=%s final=%s turn=%s text=%u active=%s",
                 id && id[0] ? id : "<none>", message_seq,
                 type && type[0] ? type : "<none>",
                 reply_to && reply_to[0] ? reply_to : "<none>", progress ? "yes" : "no",
                 final ? "yes" : "no", turn && turn[0] ? turn : "<none>",
                 (unsigned)(text ? strlen(text) : 0),
                 s_active_command_reply_to[0] ? s_active_command_reply_to : "<none>");
        if (!skin && cJSON_IsObject(extra)) skin = json_string(extra, "pet_skin");
		if (tool_message) {
			esp_err_t tool_err = handle_client_tool_call(item);
			tool_handled = tool_err == ESP_OK;
			if (!tool_handled) {
				ESP_LOGW(TAG, "client tool execution/result delivery failed: %s", esp_err_to_name(tool_err));
				keep_cursor_for_retry = true;
			}
		}
        bool pet_profile_message = type && !strcmp(type, "pet_profile");
        bool pet_profile_handled = !pet_profile_message;
        bool pet_profile_permanently_invalid = false;
        if (!skin && cJSON_IsObject(metadata)) skin = json_string(metadata, "pet_skin");
        cJSON *pet_asset = cJSON_GetObjectItemCaseSensitive(item, "pet_asset");
        if (!cJSON_IsObject(pet_asset) && cJSON_IsObject(extra)) {
            pet_asset = cJSON_GetObjectItemCaseSensitive(extra, "pet_asset");
        }
        if (skin) board_port_set_pet_profile(skin, !motion || cJSON_IsTrue(motion));
        if (pet_profile_message) {
            /* The handshake descriptor owns the initial high-resolution asset.
             * It is already being installed after Welcome on the startup task;
             * downloading the queued mirror here races it, doubles PSRAM usage,
             * and can reduce both attempts to the native robot fallback. */
            bool defer_to_startup_installer = s_startup_pet_asset_pending &&
                s_startup_pet_asset_ref && cJSON_IsObject(pet_asset) &&
                !strcmp(s_startup_pet_asset_ref->revision,
                        json_string(pet_asset, "revision") ?
                            json_string(pet_asset, "revision") : "");
            pet_profile_handled = true;
            if (defer_to_startup_installer) {
                ESP_LOGI(TAG, "startup pet_profile asset deferred to handshake installer");
                /* handled by the handshake installer */
            } else if (cJSON_IsObject(pet_asset)) {
                if (s_startup_pet_asset_pending) {
                    ESP_LOGI(TAG, "new GUI pet revision supersedes startup asset");
                    // Cancel the older boot transaction before downloading the
                    // GUI-selected revision. The startup worker checks this
                    // flag between frames and cannot overwrite the new result.
                    s_startup_pet_asset_pending = false;
                }
                esp_err_t asset_err = apply_pet_asset_ref(pet_asset);
                pet_profile_handled = asset_err == ESP_OK;
                pet_profile_permanently_invalid = audio_error_is_permanent(asset_err) ||
                                                  asset_err == ESP_ERR_INVALID_CRC;
                if (!pet_profile_handled) ESP_LOGW(TAG, "pet asset update failed: %s", esp_err_to_name(asset_err));
            } else {
                // An asset-less profile means the server selected the native
                // fallback (or rejected malformed GUI data). Remove the old
                // transparent raster and its boot cache as part of the same
                // acknowledged state transition.
                esp_err_t asset_err = clear_applied_pet_asset();
                pet_profile_handled = asset_err == ESP_OK;
                pet_profile_permanently_invalid = audio_error_is_permanent(asset_err);
                if (!pet_profile_handled) ESP_LOGW(TAG, "pet asset clear failed: %s", esp_err_to_name(asset_err));
            }
        }
        apply_glyphs_json(cJSON_GetObjectItemCaseSensitive(item, "glyphs"));
        apply_ambient_json(cJSON_GetObjectItemCaseSensitive(item, "ambient"));
        if (type && !strcmp(type, "ambient")) apply_ambient_json(item);
        if (hardware_config_message && cJSON_IsObject(extra)) {
            int volume = 0;
            if (json_number(extra, "volume", &volume) && volume >= 0 && volume <= 100) {
                esp_err_t volume_err = board_port_set_output_volume((unsigned)volume);
                if (volume_err == ESP_OK) {
                    hardware_config_handled = true;
                    ESP_LOGI(TAG, "server output volume: %d%%", volume);
                } else if (volume_err != ESP_ERR_NOT_SUPPORTED) {
                    ESP_LOGW(TAG, "server output volume failed: %s", esp_err_to_name(volume_err));
                } else {
                    hardware_config_permanently_invalid = true;
                }
            } else {
                hardware_config_permanently_invalid = true;
                ESP_LOGW(TAG, "ignored invalid server output volume");
            }
        } else if (hardware_config_message) {
            hardware_config_permanently_invalid = true;
            ESP_LOGW(TAG, "ignored hardware config without extra object");
        }
        if (type && !strcmp(type, "pet_state")) {
            const char *state = cJSON_IsObject(extra) ? json_string(extra, "state") : NULL;
            if (!state) state = json_string(item, "state");
            // An unsolicited idle/quiet state must never interrupt the
            // foreground thinking -> result transition.
            if (state && !command_display_active()) pet(state);
        }
        if (orphaned_command_result) {
            ESP_LOGW(TAG, "orphaned result discarded: id=%s replyTo=%s type=%s",
                     id && id[0] ? id : "<none>", reply_to,
                     type && type[0] ? type : "<none>");
            text_handled = text_message;
            image_handled = image_message;
        } else if (type && !strcmp(type, "meeting_result")) {
            const char *summary = cJSON_IsObject(extra) ? json_string(extra, "summary") : NULL;
            const char *status = cJSON_IsObject(extra) ? json_string(extra, "status") : NULL;
            const char *message = summary && summary[0] ? summary :
                                  text && text[0] ? text :
                                  status && status[0] ? status : "已保存到文稿库";
            pet("done");
            board_port_show_response("会议处理完成", message);
        }
		if (image_message && !orphaned_command_result) {
			const char *image_data = json_string(item, "data");
			const char *image_mime = json_string(item, "mimeType");
			if (!image_mime) image_mime = json_string(item, "mime_type");
			const char *caption = json_string(item, "caption");
			int image_width = 0, image_height = 0;
			bool dimensions_valid = json_number(item, "width", &image_width) &&
				json_number(item, "height", &image_height) && image_width >= 1 &&
				image_width <= RESPONSE_IMAGE_MAX_DIMENSION && image_height >= 1 &&
				image_height <= RESPONSE_IMAGE_MAX_DIMENSION;
			size_t expected = dimensions_valid ? (size_t)image_width * (size_t)image_height * 2u : 0;
			if (!image_data || !image_mime || strcmp(image_mime, RESPONSE_IMAGE_MIME) ||
				!dimensions_valid || expected > RESPONSE_IMAGE_MAX_BYTES) {
				image_permanently_invalid = true;
			} else {
				uint8_t *pixels = heap_caps_malloc(expected, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
				if (!pixels) pixels = malloc(expected);
				size_t decoded = 0;
				if (pixels && mbedtls_base64_decode(pixels, expected, &decoded,
						(const unsigned char *)image_data, strlen(image_data)) == 0 && decoded == expected) {
					if (cancelled_reply) {
						// A cancelled command deliberately discards late media so it
						// cannot reappear after the cancellation screen.
						image_handled = true;
					} else if (active_reply) {
						TaskHandle_t waiter = begin_active_command_reply();
						if (waiter) {
							complete_active_command_image_reply(waiter, "码卡龙", caption,
									(const uint16_t *)pixels, (size_t)image_width,
									(size_t)image_height);
							if (command_timing_matches(reply_to)) log_command_timing("image-result");
							image_handled = true;
						}
					} else if (!command_display_active()) {
						board_port_show_response_image("码卡龙", caption, (const uint16_t *)pixels,
								(size_t)image_width, (size_t)image_height);
						image_handled = true;
					}
				} else if (pixels) {
					image_permanently_invalid = true;
				}
				free(pixels);
			}
			if (image_permanently_invalid) {
				ESP_LOGW(TAG, "ignored invalid response image: mime=%s size=%dx%d",
						 image_mime ? image_mime : "<none>", image_width, image_height);
			}
		}
		if (text_message && text && text[0] && !orphaned_command_result) {
			if (cancelled_reply) {
				ESP_LOGI(TAG, "ignored late reply for cancelled command: %s", reply_to);
				text_handled = true;
            } else if (progress && !final && active_reply) {
                // Progress refreshes the thinking state but is not the answer.
                // A few Hub paths retain progress=true on the terminal envelope;
                // final must win so a completed answer cannot remain hidden
                // behind the remote-processing surface.
                if (command_timing_matches(reply_to) && !s_command_timing_first_progress_us) {
                    s_command_timing_first_progress_us = esp_timer_get_time();
                    ESP_LOGI(TAG, "command first progress: replyTo=%s afterAccepted=%ums",
                             reply_to,
                             (unsigned)elapsed_ms_between(s_command_timing_accepted_us,
                                                          s_command_timing_first_progress_us));
                }
                ESP_LOGI(TAG, "remote progress received: replyTo=%s", reply_to);
                text_handled = true;
			} else if (active_reply) {
                // Once a reply is present the thinking phase has ended; a
                // double tap arriving while this frame is drawn must not turn
                // an already completed command into a cancellation.
                TaskHandle_t waiter = begin_active_command_reply();
                if (!waiter) {
                    ESP_LOGI(TAG, "reply arrived while cancellation owns command: %s", reply_to);
                } else {
                    // Arm the exact post-terminal speech transaction before
                    // waking the command worker; it may clear active replyTo
                    // immediately after the result frame is published.
                    unsigned pending_speech_parts = outgoing_pending_speech_parts(item);
                    if (pending_speech_parts > 0) {
                        remember_result_speech_reply(reply_to, pending_speech_parts);
                    }
                    // Keep the final response surface continuous with the
                    // thinking surface. Do not briefly switch to idle here.
                    complete_active_command_text_reply(waiter, "码卡龙", text);
                    if (command_timing_matches(reply_to)) log_command_timing("text-result");
                    text_handled = true;
                }
			} else {
                // The outgoing stream can contain unrelated notifications or
                // late replies from before this boot. They may still be shown
                // when the device is idle, but must never complete or replace
                // an active command unless replyTo identifies that command.
				if (!command_display_active()) {
					board_port_show_response("码卡龙", text);
					text_handled = true;
				} else {
                    ESP_LOGI(TAG, "deferred unrelated text during active command: replyTo=%s",
                             reply_to && reply_to[0] ? reply_to : "<none>");
			}
		}
		if (text_message && (!text || !text[0])) {
			text_permanently_invalid = true;
			ESP_LOGW(TAG, "ignored text response without content");
		}
        }
        if (type && !strcmp(type, "error") && orphaned_command_result) {
            // The generic acknowledgement path has no separate error handled
            // flag. Logging above is sufficient; the queue entry is consumed.
        } else if (type && !strcmp(type, "error")) {
            if (cancelled_reply) {
                ESP_LOGI(TAG, "ignored late error for cancelled command: %s",
                         reply_to && reply_to[0] ? reply_to : "<none>");
            } else if (active_reply) {
                TaskHandle_t waiter = begin_active_command_reply();
                if (waiter) {
                    const char *detail = text && text[0] ? text : "远端返回错误，但没有详细说明";
                    pet("alert");
                    complete_active_command_text_reply(waiter, "远端处理失败", detail);
                    ESP_LOGE(TAG, "remote command failed: replyTo=%s error=%s detail=%s",
                             reply_to, json_string(item, "error") ? json_string(item, "error") : "<none>", detail);
                }
            } else {
                ESP_LOGW(TAG, "unmatched remote error: replyTo=%s detail=%s",
                         reply_to && reply_to[0] ? reply_to : "<none>",
                         text && text[0] ? text : "<none>");
            }
        } else if (final && active_reply && (!type || (strcmp(type, "text") && strcmp(type, "image") &&
                                                      strcmp(type, "voice") && strcmp(type, "audio")))) {
            TaskHandle_t waiter = begin_active_command_reply();
            if (waiter) {
                complete_active_command_text_reply(
                    waiter, "任务已完成",
                    text && text[0] ? text : "远端已完成，但没有可显示的文字结果");
            }
        }
        if (audio_message && audio_data && !discard_startup_welcome &&
            !orphaned_command_result &&
            !cancelled_reply && audio_can_play &&
            audio_mime_supported(audio_mime)) {
            size_t audio_capacity = 0;
            int decode_status = mbedtls_base64_decode(NULL, 0, &audio_capacity,
                                                       (const unsigned char *)audio_data,
                                                       strlen(audio_data));
            if (decode_status != MBEDTLS_ERR_BASE64_BUFFER_TOO_SMALL || audio_capacity < 2 ||
                audio_capacity >= HARDWARE_AUDIO_RESPONSE_CAPACITY) {
                audio_permanently_invalid = true;
            }
            if (decode_status == MBEDTLS_ERR_BASE64_BUFFER_TOO_SMALL && audio_capacity >= 2 &&
                audio_capacity < HARDWARE_AUDIO_RESPONSE_CAPACITY) {
                uint8_t *audio = heap_caps_malloc(audio_capacity, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
                if (!audio) audio = malloc(audio_capacity);
                size_t audio_len = 0;
                if (audio && mbedtls_base64_decode(audio, audio_capacity, &audio_len,
                                                  (const unsigned char *)audio_data,
                                                  strlen(audio_data)) == 0) {
                    ESP_LOGI(TAG, "playing server audio: %u bytes mime=%s",
                             (unsigned)audio_len, audio_mime ? audio_mime : "auto");
                    esp_err_t play_err = play_audio_payload(audio_mime, audio, audio_len);
                    if (play_err != ESP_OK) ESP_LOGW(TAG, "server speech playback failed: %s", esp_err_to_name(play_err));
                    audio_handled = play_err == ESP_OK;
                    audio_permanently_invalid = audio_error_is_permanent(play_err);

                } else if (audio) {
                    ESP_LOGW(TAG, "invalid server speech payload");
                    audio_permanently_invalid = true;
                } else {
                    ESP_LOGW(TAG, "server speech allocation failed: %u bytes",
                             (unsigned)audio_capacity);
                }
                free(audio);
            } else {
                ESP_LOGW(TAG, "ignored server audio payload: base64=%d size=%u", decode_status, (unsigned)audio_capacity);
            }
        }
        if (audio_message && !audio_data && audio_url && !discard_startup_welcome &&
            !orphaned_command_result &&
            !cancelled_reply && audio_can_play &&
            audio_mime_supported(audio_mime)) {
            uint8_t *audio = NULL;
            size_t audio_len = 0;
            esp_err_t fetch_err = download_audio(audio_url, &audio, &audio_len);
            if (fetch_err == ESP_OK) {
                ESP_LOGI(TAG, "playing downloaded server audio: %u bytes mime=%s",
                         (unsigned)audio_len, audio_mime ? audio_mime : "auto");
                esp_err_t play_err = play_audio_payload(audio_mime, audio, audio_len);
                if (play_err != ESP_OK) ESP_LOGW(TAG, "downloaded server speech playback failed: %s", esp_err_to_name(play_err));
                audio_handled = play_err == ESP_OK;
                audio_permanently_invalid = audio_error_is_permanent(play_err);

            } else {
                ESP_LOGW(TAG, "server speech download failed: %s", esp_err_to_name(fetch_err));
                audio_permanently_invalid = audio_error_is_permanent(fetch_err);
            }
            free(audio);
        }
        // Do not acknowledge an audio message that we could neither fetch nor
        // play. Keeping it pending lets a transient network/I2S failure retry
        // on the next poll instead of silently losing the welcome sound. Late
        // cancelled audio is intentionally discarded so it cannot retry forever.
        // Permanent protocol/content errors must not pin the page cursor and
        // create a hot retry loop. Transient states (busy audio bus, download,
        // allocation or I2S failure) remain pending and retry on the next poll.
        audio_permanently_invalid = audio_permanently_invalid || (audio_message &&
            (invalid_audio_url || !audio_mime_supported(audio_mime) ||
             (!audio_data && !audio_url)));
        if (result_speech_reply && (audio_handled || audio_permanently_invalid)) {
            finish_result_speech_part(reply_to);
        }
        if (startup_welcome && !discard_startup_welcome &&
            (audio_handled || audio_permanently_invalid)) {
            taskENTER_CRITICAL(&s_task_state_lock);
            s_startup_welcome_consumed = true;
            taskEXIT_CRITICAL(&s_task_state_lock);
            finish_startup_welcome_gate(audio_handled ? "playback complete" : "playback unavailable");
        }
		bool ack_message = tool_handled &&
            (hardware_config_handled || hardware_config_permanently_invalid) &&
			(pet_profile_handled || pet_profile_permanently_invalid) &&
			(!text_message || text_handled || cancelled_reply || text_permanently_invalid) &&
			(!audio_message || audio_handled || cancelled_reply || audio_permanently_invalid) &&
			(!image_message || image_handled || cancelled_reply || image_permanently_invalid);
        if (id && !ack_message) {
            keep_cursor_for_retry = true;
            // The page cursor is shared by all messages. Stop at the first
            // transient failure so a later speech part or terminal text cannot
            // overtake it and complete the command with missing audio. Already
            // handled messages are acknowledged below; the server then resends
            // only this item and the untouched tail of the page.
            ESP_LOGW(TAG, "halting outgoing page for ordered retry: id=%s type=%s",
                     id, type && type[0] ? type : "<none>");
            break;
        }
        if (id && ack_message) {
            cJSON *ack_id = cJSON_CreateString(id);
			bool permanently_failed = hardware_config_permanently_invalid ||
				pet_profile_permanently_invalid ||
				(text_message && text_permanently_invalid && !cancelled_reply) ||
				(audio_message && audio_permanently_invalid && !audio_handled && !cancelled_reply) ||
				(image_message && image_permanently_invalid && !image_handled && !cancelled_reply);
            cJSON *target = permanently_failed
                                ? failed_ack_ids : delivered_ack_ids;
            if (!ack_id || !cJSON_AddItemToArray(target, ack_id)) {
                cJSON_Delete(ack_id);
                cJSON_Delete(delivered_ack_ids);
                cJSON_Delete(failed_ack_ids);
                cJSON_Delete(json);
                response_release(&response);
                return ESP_ERR_NO_MEM;
            }
        }
    }
    cJSON *ack_groups[2] = {delivered_ack_ids, failed_ack_ids};
    const char *ack_statuses[2] = {"delivered", "failed"};
    for (size_t ack_index = 0; ack_index < 2; ++ack_index) {
        cJSON *ack_ids = ack_groups[ack_index];
        if (cJSON_GetArraySize(ack_ids) == 0) continue;
        cJSON *ack = cJSON_CreateObject();
        if (!ack) {
            cJSON_Delete(delivered_ack_ids);
            cJSON_Delete(failed_ack_ids);
            cJSON_Delete(json);
            response_release(&response);
            return ESP_ERR_NO_MEM;
        }
        cJSON_AddStringToObject(ack, "clientId", s_device_id);
        cJSON_AddItemReferenceToObject(ack, "messageIds", ack_ids);
        cJSON_AddStringToObject(ack, "status", ack_statuses[ack_index]);
        char *payload = cJSON_PrintUnformatted(ack);
        cJSON_Delete(ack);
        if (!payload) {
            cJSON_Delete(delivered_ack_ids);
            cJSON_Delete(failed_ack_ids);
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
            cJSON_Delete(delivered_ack_ids);
            cJSON_Delete(failed_ack_ids);
            cJSON_Delete(json);
            response_release(&response);
            return result;
        }
        response_release(&ack_resp);
    }
    cJSON_Delete(delivered_ack_ids);
    cJSON_Delete(failed_ack_ids);
    // Cursor is page-level while acknowledgements are message-level. If one
    // audio item was intentionally left unacknowledged, advancing the cursor
    // would hide it from the next poll despite the missing ACK.
    if (!keep_cursor_for_retry) s_cursor = (int64_t)parsed_cursor;
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

static esp_err_t configure_meeting_chunk_client(esp_http_client_handle_t client,
                                                const char *url,
                                                const char sha256_hex[65],
                                                http_response_t *response) {
    if (!client || !url || !sha256_hex || !response) return ESP_ERR_INVALID_ARG;
    esp_err_t err = esp_http_client_set_url(client, url);
    if (err == ESP_OK) err = esp_http_client_set_user_data(client, response);
    if (err == ESP_OK) err = esp_http_client_set_timeout_ms(client, 60000);
    if (err == ESP_OK) err = esp_http_client_set_method(client, HTTP_METHOD_PUT);
    if (err == ESP_OK) err = esp_http_client_set_header(client, "Content-Type", "application/octet-stream");
    if (err == ESP_OK) err = esp_http_client_set_header(client, "X-Chunk-SHA256", sha256_hex);
    if (err == ESP_OK) err = esp_http_client_set_header(client, "Accept", "application/json");
    if (err == ESP_OK) err = esp_http_client_delete_header(client, "Connection");
    char authorization[128];
    snprintf(authorization, sizeof(authorization), "Bearer %s", s_gateway_token);
    if (err == ESP_OK) err = esp_http_client_set_header(client, "Authorization", authorization);
    return err;
}

static esp_err_t stream_meeting_chunk(const char *recording_id, int index, FILE *file,
                                      size_t offset, size_t length, const char sha256_hex[65],
                                      uint8_t *buffer, size_t buffer_size,
                                      size_t completed_before, size_t total_bytes,
                                      bool publish_progress,
                                      esp_http_client_handle_t *reusable_client) {
    char path[MEETING_BASE_PATH_CAPACITY + MEETING_RECORDING_ID_CAPACITY + 48];
    char url[URL_CAPACITY];
    int path_len = snprintf(path, sizeof(path), "%s/%s/chunks/%d",
                            s_meeting_base_path, recording_id, index);
    int url_len = snprintf(url, sizeof(url), "%s%s", s_gateway_url, path);
    if (path_len < 0 || path_len >= (int)sizeof(path) ||
        url_len < 0 || url_len >= (int)sizeof(url) ||
        fseek(file, (long)offset, SEEK_SET) != 0) return ESP_ERR_INVALID_SIZE;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    if (s_fangtang_use_cellular) {
        char *chunk = heap_caps_malloc(length, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!chunk) chunk = malloc(length);
        if (!chunk) return ESP_ERR_NO_MEM;
        if (fread(chunk, 1, length, file) != length) {
            free(chunk);
            return ESP_FAIL;
        }
        http_response_t response = {0};
        response.data = heap_caps_malloc(MEETING_RESPONSE_CAPACITY,
                                         MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!response.data) response.data = malloc(MEETING_RESPONSE_CAPACITY);
        if (!response.data) {
            free(chunk);
            return ESP_ERR_NO_MEM;
        }
        response.capacity = MEETING_RESPONSE_CAPACITY;
        char authorization[128];
        snprintf(authorization, sizeof(authorization), "Bearer %s", s_gateway_token);
        int64_t started_us = esp_timer_get_time();
        esp_err_t err = ml307_transport_http_request(
            "PUT", url, "application/octet-stream", authorization,
            "X-Chunk-SHA256", sha256_hex, chunk, length,
            response.data, response.capacity, &response.len, &response.status,
            &response.truncated, 60000, false);
        free(chunk);
        uint32_t total_ms = (uint32_t)((esp_timer_get_time() - started_us) / 1000);
        ESP_LOGI(TAG, "meeting chunk %d upload bytes=%u connection=ML307 total=%ums status=%d err=%s",
                 index, (unsigned)length, (unsigned)total_ms, response.status,
                 esp_err_to_name(err));
        if (publish_progress && err == ESP_OK) {
            board_port_show_upload_progress(completed_before + length, total_bytes,
                                            "正在上传录音");
        }
        if (err == ESP_OK && response.status != 200 && response.status != 201) err = ESP_FAIL;
        response_release(&response);
        return err;
    }
#endif
    if (!s_http_mutex || xSemaphoreTake(s_http_mutex, pdMS_TO_TICKS(35000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    // Hardware AES needs short-lived DMA-capable memory only when a new TLS
    // transport is opened. A retained keep-alive connection already owns its
    // crypto buffers, so avoid reserving another 16 KiB at every chunk.
    bool reused_connection = reusable_client && *reusable_client;
    void *tls_internal_reserve = NULL;
    if (!reused_connection) {
        tls_internal_reserve = heap_caps_malloc(MEETING_INTERNAL_TLS_RESERVE,
                                                MALLOC_CAP_INTERNAL |
                                                MALLOC_CAP_DMA |
                                                MALLOC_CAP_8BIT);
        if (!tls_internal_reserve) {
            ESP_LOGE(TAG, "meeting TLS reserve failed: need=%u", (unsigned)MEETING_INTERNAL_TLS_RESERVE);
            log_heap_snapshot("meeting-tls-reserve-fail");
            xSemaphoreGive(s_http_mutex);
            return ESP_ERR_NO_MEM;
        }
    }
    http_response_t response = {0};
    response.data = heap_caps_malloc(MEETING_RESPONSE_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!response.data) response.data = malloc(MEETING_RESPONSE_CAPACITY);
    if (!response.data) {
        heap_caps_free(tls_internal_reserve);
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_NO_MEM;
    }
    response.capacity = MEETING_RESPONSE_CAPACITY;
    response.data[0] = '\0';
    esp_http_client_handle_t client = reused_connection ? *reusable_client : NULL;
    esp_err_t err = ESP_OK;
    if (!client) {
        esp_http_client_config_t config = {
            .url = url, .event_handler = on_http_event, .user_data = &response,
            .timeout_ms = 60000, .crt_bundle_attach = esp_crt_bundle_attach,
            .keep_alive_enable = true,
        };
        client = esp_http_client_init(&config);
    }
    if (client) err = configure_meeting_chunk_client(client, url, sha256_hex, &response);
    if (!client || err != ESP_OK) {
        if (client) esp_http_client_cleanup(client);
        if (reusable_client) *reusable_client = NULL;
        heap_caps_free(tls_internal_reserve);
        response_release(&response);
        xSemaphoreGive(s_http_mutex);
        return client ? err : ESP_ERR_NO_MEM;
    }
    int64_t request_started_us = esp_timer_get_time();
    err = esp_http_client_open(client, (int)length);
    bool stale_connection_retry = false;
    if (err != ESP_OK && reused_connection) {
        // A server or intermediary can expire an otherwise valid keep-alive
        // socket between chunks. PUT is idempotent for this indexed/hash-checked
        // endpoint, and no body was written when open failed, so retry once on
        // a fresh TLS transport instead of aborting the resumable upload.
        ESP_LOGW(TAG, "meeting chunk %d stale keep-alive open failed: %s; retrying fresh TLS",
                 index, esp_err_to_name(err));
        stale_connection_retry = true;
        esp_http_client_cleanup(client);
        client = NULL;
        if (reusable_client) *reusable_client = NULL;
        tls_internal_reserve = heap_caps_malloc(MEETING_INTERNAL_TLS_RESERVE,
                                                MALLOC_CAP_INTERNAL |
                                                MALLOC_CAP_DMA |
                                                MALLOC_CAP_8BIT);
        if (!tls_internal_reserve) {
            err = ESP_ERR_NO_MEM;
        } else {
            esp_http_client_config_t retry_config = {
                .url = url, .event_handler = on_http_event, .user_data = &response,
                .timeout_ms = 60000, .crt_bundle_attach = esp_crt_bundle_attach,
                .keep_alive_enable = true,
            };
            client = esp_http_client_init(&retry_config);
            err = client ? configure_meeting_chunk_client(client, url, sha256_hex, &response)
                         : ESP_ERR_NO_MEM;
            if (err == ESP_OK) err = esp_http_client_open(client, (int)length);
        }
    }
    uint32_t open_ms = (uint32_t)((esp_timer_get_time() - request_started_us) / 1000);
    heap_caps_free(tls_internal_reserve);
    tls_internal_reserve = NULL;
    if (!client) {
        ESP_LOGE(TAG,
                 "meeting chunk %d fresh TLS retry setup failed after reused connection: %s",
                 index, esp_err_to_name(err));
        response_release(&response);
        xSemaphoreGive(s_http_mutex);
        return err;
    }
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
        // Repainting the complete 360x360 LCD after each 16 KiB TLS write
        // overlaps QSPI DMA, PSRAM traffic and Wi-Fi for the whole upload. On
        // this board that causes a repeatable brownout/watchdog-style reset.
        // Update once per 256 KiB (and at completion) instead.
        size_t transferred = length - remaining;
        if (publish_progress &&
            (remaining == 0 || (transferred % (256u * 1024u)) < count)) {
            board_port_show_upload_progress(completed_before + transferred,
                                            total_bytes, "正在上传录音");
        }
        // A multi-megabyte HTTPS PUT can otherwise monopolize this task long
        // enough to starve the idle watchdog on a slow Wi-Fi link.
        vTaskDelay(1);
    }
    if (err == ESP_OK) {
        int headers = esp_http_client_fetch_headers(client);
        if (headers < 0) err = ESP_FAIL;
        while (err == ESP_OK && !esp_http_client_is_complete_data_received(client)) {
            int count = esp_http_client_read(client, (char *)buffer, buffer_size);
            if (count < 0) err = ESP_FAIL;
            if (count <= 0 && !esp_http_client_is_complete_data_received(client)) err = ESP_FAIL;
        }
    }
    response.status = esp_http_client_get_status_code(client);
    if (err == ESP_OK && (response.status < 200 || response.status >= 300)) {
        ESP_LOGE(TAG, "meeting chunk %d rejected: status=%d body=%s",
                 index, response.status, response.data ? response.data : "");
        err = ESP_FAIL;
    }
    uint32_t total_ms = (uint32_t)((esp_timer_get_time() - request_started_us) / 1000);
    // Once the full response body has been drained, ESP-IDF leaves a
    // keep-alive transport in CONNECTED state. Retain that handle for the next
    // same-origin PUT; discard any failed/closed parser state immediately.
    bool keep_client = err == ESP_OK &&
                       esp_http_client_is_complete_data_received(client) &&
                       reusable_client;
    esp_http_client_set_user_data(client, NULL);
    if (keep_client) {
        *reusable_client = client;
    } else {
        esp_http_client_cleanup(client);
        if (reusable_client) *reusable_client = NULL;
    }
    xSemaphoreGive(s_http_mutex);
    ESP_LOGI(TAG,
             "meeting chunk %d upload bytes=%u connection=%s open=%ums total=%ums status=%d err=%s keep=%s",
             index, (unsigned)length,
             stale_connection_retry ? "reused->new" : reused_connection ? "reused" : "new",
             (unsigned)open_ms, (unsigned)total_ms, response.status,
             esp_err_to_name(err), keep_client ? "yes" : "no");
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
    esp_http_client_handle_t meeting_upload_client = NULL;
    for (int index = next_chunk; err == ESP_OK && index < chunks; ++index) {
        size_t offset = (size_t)index * s_meeting_chunk_size;
        size_t length = file_size - offset;
        if (length > s_meeting_chunk_size) length = s_meeting_chunk_size;
        char chunk_hash[65];
        err = hash_file_range(file, offset, length, buffer, MEETING_IO_BUFFER_SIZE, chunk_hash);
        if (err == ESP_OK) {
            if (publish_state) {
                board_port_show_upload_progress(offset, file_size, "正在上传录音");
            }
            err = stream_meeting_chunk(recording_id, index, file, offset, length,
                                       chunk_hash, buffer, MEETING_IO_BUFFER_SIZE,
                                       offset, file_size, publish_state,
                                       &meeting_upload_client);
        }
        if (err == ESP_OK) {
            next_chunk = index + 1;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
            if (publish_state) {
                board_port_show_upload_progress(offset + length, file_size, "正在上传录音");
            }
        }
        if (!publish_state && err == ESP_OK && s_foreground_http_requested) {
            // Recovery metadata is already durable at this chunk boundary. End
            // this pass cleanly and let the foreground command acquire HTTP;
            // the reconnect/resume path continues from next_chunk later.
            ESP_LOGI(TAG, "background meeting resume yielded after chunk %d", index);
            err = ESP_ERR_TIMEOUT;
        }
    }
    if (meeting_upload_client) {
        esp_http_client_cleanup(meeting_upload_client);
        meeting_upload_client = NULL;
    }
    char whole_hash[65];
    if (err == ESP_OK && phase < 1) {
        if (publish_state) meeting_set_state(MEETING_FINALIZING);
        if (publish_state) board_port_show_upload_progress(file_size, file_size, "正在校验录音");
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
        if (publish_state) board_port_show_upload_progress(file_size, file_size, "正在提交处理");
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
        esp_err_t start_err = ESP_OK;
        if (fwrite(header, 1, sizeof(header), file) != sizeof(header)) {
            start_err = ESP_FAIL;
            ESP_LOGE(TAG, "meeting start: WAV header write failed");
        }
        if (start_err == ESP_OK) {
            start_err = save_meeting_recovery(true, "", 0, 0);
            if (start_err != ESP_OK) {
                ESP_LOGE(TAG, "meeting start: recovery metadata failed: %s",
                         esp_err_to_name(start_err));
            }
        }
        if (start_err == ESP_OK) {
            start_err = board_port_audio_stream_start();
            if (start_err != ESP_OK) {
                ESP_LOGE(TAG, "meeting start: audio stream failed: %s",
                         esp_err_to_name(start_err));
            }
        }
        if (start_err != ESP_OK) {
            fclose(file);
            // Startup produced no PCM. Clear the marker and placeholder so a
            // transient microphone/mutex failure cannot permanently turn every
            // later double tap into a bogus retained-file recovery attempt.
            (void)clear_meeting_recovery(true);
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
        board_port_set_recording_mode(true);
        board_port_set_recording_visual(true, false, 0);
        while (s_meeting_state == MEETING_RECORDING || s_meeting_state == MEETING_PAUSED) {
            size_t count = 0;
            uint16_t level = 0;
            esp_err_t capture = board_port_audio_stream_read(samples, 512, &count, &level);
            if (capture == ESP_OK && count > 0) board_port_push_recording_pcm(samples, count);
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
            // Meeting delivery has its own status surface. Reusing the normal
            // command "thinking" pet made a completed meeting look like a
            // short voice command and allowed ambient frames to replace it.
            s_command_display_locked = true;
            board_port_set_command_display_lock(true);
            board_port_set_recording_visual(false, false, 0);
            board_port_show_upload_progress(0, 1, "正在准备上传");
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
    // MultiNet keeps its model, task stack and inference buffers alive even
    // while microphone capture is merely paused. On this ESP32-S3 that leaves
    // the internal DMA heap too fragmented for hardware AES during HTTPS PUT
    // (mbedTLS reports -0x0084). Fully unload it for delivery, then restore the
    // hands-free listener after the HTTP/NVS work has finished.
    log_heap_snapshot("meeting-upload-before-wake-stop");
    esp_err_t wake_stop_err = board_port_stop_wake_word();
    if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "offline wake stop before meeting upload: %s",
                 esp_err_to_name(wake_stop_err));
    }
    log_heap_snapshot("meeting-upload-after-wake-stop");

    esp_err_t upload_err = upload_pending_meeting(!resume_only);

    if (upload_err == ESP_OK) {
        if (!resume_only) {
            meeting_set_state(MEETING_DONE);
            pet("done");
            board_port_show_text("会议记录已保存", "可在文稿库中查看");
            vTaskDelay(pdMS_TO_TICKS(3000));
            s_command_display_locked = false;
            board_port_set_command_display_lock(false);
            pet("idle");
        } else {
            ESP_LOGI(TAG, "background meeting resume delivered");
        }
    } else {
        ESP_LOGE(TAG, "meeting upload pass failed: %s resume=%s id=%s next=%ld phase=%ld",
                 esp_err_to_name(upload_err), resume_only ? "yes" : "no",
                 s_meeting_recording_id,
                 (long)s_meeting_next_chunk, (long)s_meeting_phase);
        log_heap_snapshot("meeting-upload-fail");
        if (!resume_only) {
            meeting_set_state(MEETING_ERROR);
            pet("alert");
            board_port_show_text("上传未完成", "联网后将自动续传");
            vTaskDelay(pdMS_TO_TICKS(2200));
            s_command_display_locked = false;
            board_port_set_command_display_lock(false);
            pet("idle");
        } else {
            ESP_LOGW(TAG, "background meeting resume deferred until next reconnect");
        }
    }
finish:
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_task = NULL;
    s_meeting_task_running = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!resume_only) {
        // Error exits before the normal success/deferred UI cleanup still
        // need to release the display for the ambient screen.
        s_command_display_locked = false;
        board_port_set_command_display_lock(false);
        if (s_interaction_lock) xSemaphoreGive(s_interaction_lock);
    }
    schedule_wake_restart();
    vTaskDelete(NULL);
}

static bool start_meeting_task(bool resume_only) {
    if (!s_storage_mounted) {
        ESP_LOGW(TAG, "meeting start refused: storage is not mounted");
        return false;
    }
    if (!resume_only && !s_meeting_available) {
        ESP_LOGW(TAG, "meeting start refused: capability is unavailable");
        return false;
    }
    if (!s_gateway_token[0]) {
        ESP_LOGW(TAG, "meeting start refused: device is not paired");
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_meeting_task_running) {
        taskEXIT_CRITICAL(&s_task_state_lock);
        return false;
    }
    s_meeting_task_running = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!resume_only && (!s_interaction_lock || xSemaphoreTake(s_interaction_lock, pdMS_TO_TICKS(1500)) != pdTRUE)) {
        ESP_LOGI(TAG, "meeting start deferred: foreground interaction owns the lock");
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_task_running = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        return false;
    }
    if (!resume_only) board_port_cancel_ready_prompt();
    TaskHandle_t handle = NULL;
    // Meeting startup writes recovery metadata to NVS before the microphone
    // begins.  Flash writes disable the cache, so this task must keep its
    // stack in internal RAM; a PSRAM stack causes a cache-disabled assertion
    // and an apparent reboot on a double tap.
    // At steady state the offline speech model leaves roughly 21 KB internal
    // heap, whose largest contiguous block is only about 9 KB. A 12 KB stack
    // therefore cannot be created even though total RAM appears sufficient.
    // The worker's large audio/network buffers are heap/PSRAM allocations;
    // 8 KB internal stack is enough for its bounded local frames and keeps NVS
    // writes safe while flash cache is disabled.
    // Foreground and resumed uploads both persist progress to NVS. Flash
    // commits disable the external-memory cache, therefore both modes need an
    // internal stack. A PSRAM stack here reset the MCU just after chunk PUT.
    BaseType_t created = xTaskCreate(meeting_task, "maclaw_meeting", 8192,
                                     resume_only ? (void *)1 : NULL, 5, &handle);
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

// A Hub can be upgraded while the watch remains online.  Meeting capability is
// negotiated during handshake, so do not make the user reboot the device just
// because it still holds an older, capability-less response in RAM.  The
// refresh runs outside the input scan task because TLS can take several
// seconds; after a successful refresh it retries the original double-tap.
static void meeting_capability_refresh_task(void *arg) {
    (void)arg;
    ESP_LOGI(TAG, "refreshing gateway handshake for meeting recording");
    board_port_show_text("会议录音", "正在检查网关支持");
    esp_err_t err = gateway_handshake(false);
    if (err == ESP_OK && s_meeting_available) {
        // A just-finished touch/voice action can still own the foreground
        // mutex for a moment.  This task is deliberately off the input scan
        // path, so wait and retry instead of turning that harmless race into
        // a visible recording failure.
        bool started = false;
        for (unsigned retry = 0; retry < 32 && !started; ++retry) {
            started = start_meeting_task(false);
            if (!started) {
                if (retry == 0) {
                    board_port_show_text("会议录音", "正在等待设备就绪");
                }
                vTaskDelay(pdMS_TO_TICKS(250));
            }
        }
        if (!started) {
            pet("alert");
#if CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD || CONFIG_MACLAW_BOARD_FANGTANG_4G
            board_port_show_text("录音启动失败", "请稍后再次双击激活键");
#else
            board_port_show_text("录音启动失败", "请稍后再次双击屏幕");
#endif
        }
    } else {
        ESP_LOGW(TAG, "meeting capability refresh failed: err=%s available=%s",
                 esp_err_to_name(err), s_meeting_available ? "yes" : "no");
        pet("alert");
        board_port_show_text("会议录音不可用", "请检查网关连接");
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_capability_refresh_task = NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    vTaskDeleteWithCaps(NULL);
}

static bool refresh_meeting_capability(void) {
    taskENTER_CRITICAL(&s_task_state_lock);
    bool already_refreshing = s_meeting_capability_refresh_task != NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (already_refreshing) return true;
    TaskHandle_t handle = NULL;
    // gateway_handshake() persists fresh ambient data in NVS.  Flash writes
    // temporarily disable the cache, so this task's stack must remain in
    // internal RAM; a PSRAM stack causes esp_task_stack_is_sane_cache_disabled
    // to assert and looks like a reboot immediately after a double tap.
    BaseType_t created = xTaskCreate(meeting_capability_refresh_task,
                                     "maclaw_meeting_cap", 8192, NULL, 4,
                                     &handle);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_capability_refresh_task = created == pdPASS ? handle : NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (created != pdPASS) {
        ESP_LOGE(TAG, "cannot start meeting capability refresh task");
        return false;
    }
    return true;
}

static void interaction_task(void *arg) {
    uint32_t interaction_generation = (uint32_t)(uintptr_t)arg;
    int64_t interaction_started_us = esp_timer_get_time();
    // The wake-phrase path creates this worker from inside MultiNet, while a
    // panel tap unloads it before task creation. Converge both paths here so
    // command HTTPS upload always has enough contiguous DMA RAM for TLS AES.
    esp_err_t wake_stop_err = board_port_stop_wake_word();
    if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "offline wake stop before voice capture: %s",
                 esp_err_to_name(wake_stop_err));
    }
    log_heap_snapshot("voice-after-wake-stop");
    // Keep capture screen-neutral. Once a spoken command is accepted, the
    // visible path is only thinking -> result (or an explicit error).
    board_port_set_recording_mode(false);
    board_port_set_recording_visual(true, false, 0);
    uint8_t *wav = NULL;
    size_t wav_len = 0;
    esp_err_t err = board_port_capture_wav(&wav, &wav_len);
    s_command_timing_capture_done_us = esp_timer_get_time();
    ESP_LOGI(TAG, "voice capture complete: generation=%lu err=%s wav=%u elapsed=%lldms",
             (unsigned long)interaction_generation, esp_err_to_name(err), (unsigned)wav_len,
             (long long)((esp_timer_get_time() - interaction_started_us) / 1000));
    if (command_cancel_requested_for(interaction_generation)) {
        board_port_set_recording_visual(false, false, 0);
        free(wav);
        finish_cancelled_command(interaction_generation);
        return;
    }
    if (err != ESP_OK || !wav || wav_len == 0) {
        // The natural endpoint did not observe speech. This is an expected
        // cancellation-like outcome, not a microphone failure and certainly
        // not a request to send the legacy text probe to the gateway.
        if (err == ESP_ERR_NOT_FOUND) {
            board_port_set_recording_visual(false, false, 0);
            board_port_show_text("未检测到语音", "请再试一次");
            free(wav);
            finish_interaction_message(interaction_generation, 1400);
            return;
        }
        // Do not turn a local capture failure into an unrelated server text
        // command. That legacy probe leaves the command correlation empty and
        // can strand the EchoEar in a foreground message. Bread treats this as
        // a local, retryable status and returns to standby after the notice.
        pet("alert");
        board_port_set_recording_visual(false, false, 0);
        board_port_show_text("麦克风不可用", "请稍后再试");
        free(wav);
        finish_interaction_message(interaction_generation, 1800);
        return;
    }
    if (!s_gateway_token[0]) {
        board_port_show_text("设备配对", "请说出六位配对码");
        err = pair_by_voice(wav, wav_len);
        free(wav);
        if (err == ESP_OK && gateway_handshake(false) == ESP_OK) {
            if (ensure_gateway_poll_task()) {
                pet("done");
#if CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD || CONFIG_MACLAW_BOARD_FANGTANG_4G
                board_port_show_ready_prompt("配对成功", "按激活键后说话");
#else
                board_port_show_ready_prompt("配对成功", "点击屏幕后说话");
#endif
            } else {
                err = ESP_ERR_NO_MEM;
                pet("alert");
                board_port_show_text("设备启动失败", "无法启动网关轮询");
            }
        }
        else { pet("alert"); board_port_show_text("配对失败", "请生成新的配对码"); }
        finish_interaction_message(interaction_generation, 1800);
        return;
    }
    // The server is the interaction runtime: it owns ASR, intent routing,
    // authorization, agent/tool execution, IM delivery, and the final reply.
    // The ESP32 only submits a server-owned `voice` media attachment.
    char media_id[96] = {0};
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_interaction_generation == interaction_generation) {
        s_interaction_phase = INTERACTION_PROCESSING;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    // Switch state before closing the recorder. board_port_set_recording_visual
    // redraws the pet when it removes the waveform; doing that while the
    // previous state is idle briefly drew the time/weather face between
    // “received” and “thinking”.
    board_port_set_command_stage("正在上传语音");
    pet("thinking");
    // Keep the foreground screen locked after capture as well.  The task can
    // receive its reply and clear its task handle before a delayed gateway
    // `pet_state: idle` notification is processed; that notification used to
    // repaint the Wi-Fi/time face in the gap before the final response draw.
    s_command_display_locked = true;
    board_port_set_command_display_lock(true);
    board_port_set_recording_visual(false, false, 0);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_command_cancel_enabled = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    board_port_set_command_cancel_enabled(true);
    // Keep the pet's animated thinking state on screen during upload and the
    // server-side reply wait. Do not switch the shared I2S bus to playback
    // here: on EchoEar it races the just-stopped microphone DMA and resets
    // the CPU. The thinking screen is the immediate acknowledgement.
    err = upload_voice(wav, wav_len, media_id, sizeof(media_id));
    if (err == ESP_OK) s_command_timing_upload_done_us = esp_timer_get_time();
    free(wav);
    if (command_cancel_requested_for(interaction_generation)) { finish_cancelled_command(interaction_generation); return; }
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "voice media upload failed: %s (0x%x)",
                 esp_err_to_name(err), (unsigned)err);
        pet("alert");
        board_port_show_text("语音上传失败", command_submit_error_detail(err));
        finish_interaction_message(interaction_generation, 1800);
        return;
    }
    board_port_set_command_stage("正在提交指令");
    char reply_to[COMMAND_REPLY_ID_CAPACITY] = {0};
    char command_event_id[80];
    snprintf(command_event_id, sizeof(command_event_id), "voice-%lld",
             (long long)esp_timer_get_time());
    for (unsigned attempt = 1; attempt <= COMMAND_SUBMIT_RETRY_COUNT; ++attempt) {
        err = send_voice_event(media_id, command_event_id, reply_to, sizeof(reply_to));
        if (err == ESP_OK || command_cancel_requested_for(interaction_generation)) break;
        ESP_LOGW(TAG, "voice command submit attempt %u/%u failed: %s",
                 attempt, COMMAND_SUBMIT_RETRY_COUNT, esp_err_to_name(err));
        if (attempt < COMMAND_SUBMIT_RETRY_COUNT) {
            // Reuse the idempotency key. If the Hub accepted an attempt but
            // its response was lost, the retry resolves the same command
            // instead of starting a duplicate Agent task.
            vTaskDelay(pdMS_TO_TICKS(500u << (attempt - 1u)));
        }
    }
    if (err == ESP_OK && !reply_to[0]) {
        ESP_LOGE(TAG, "incoming voice accepted without maclawMessageId");
        err = ESP_ERR_INVALID_RESPONSE;
    }
    if (err == ESP_OK) {
        s_command_timing_accepted_us = esp_timer_get_time();
        board_port_set_command_stage("远端处理中");
        ESP_LOGI(TAG, "voice command waiting: generation=%lu replyTo=%s total=%lldms",
                 (unsigned long)interaction_generation, reply_to,
                 (long long)((esp_timer_get_time() - interaction_started_us) / 1000));
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    if (err == ESP_OK) {
        strlcpy(s_active_command_reply_to, reply_to, sizeof(s_active_command_reply_to));
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (command_cancel_requested_for(interaction_generation)) { finish_cancelled_command(interaction_generation); return; }
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "voice command submit failed: %s (0x%x)",
                 esp_err_to_name(err), (unsigned)err);
        pet("alert");
        board_port_show_text("指令提交失败", command_submit_error_detail(err));
        finish_interaction_message(interaction_generation, 1800);
        return;
    }
    // Agent work is not bounded like a normal HTTP request. Complex remote
    // tasks routinely take longer than the old 90-second deadline; treating
    // that deadline as final also cleared replyTo, so the poller discarded the
    // eventual result. Keep the correlated command alive until a reply arrives
    // or the user explicitly cancels it. Refresh the message periodically so
    // the device never looks stalled while the remote Agent is still working.
    while (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(COMMAND_RESULT_PROGRESS_MS)) == 0) {
        if (command_cancel_requested_for(interaction_generation)) break;
        // Keep the animated thinking surface intact. This is a state
        // reassertion, not a full-screen refresh; unchanged labels do no LCD IO.
        board_port_set_command_stage("远端处理中");
        ESP_LOGI(TAG, "remote Agent still processing command generation=%lu",
                 (unsigned long)interaction_generation);
        ESP_LOGI(TAG, "remote wait detail: replyTo=%s elapsed=%lldms",
                 reply_to, (long long)((esp_timer_get_time() - interaction_started_us) / 1000));
    }
    if (command_cancel_requested_for(interaction_generation)) { finish_cancelled_command(interaction_generation); return; }
    // The poller has already painted the final reply in the speaking state.
    // Returning through done/idle immediately after the notification repaints
    // the ambient face over it, producing the distracting apparent reboot.
    // Leave the response visible until the next user interaction or a later
    // server state update explicitly changes it.
    finish_interaction_task(interaction_generation);
}

static bool start_voice_interaction(bool physical_screen_wake) {
    bool input_guarded;
    taskENTER_CRITICAL(&s_task_state_lock);
    input_guarded = esp_timer_get_time() < s_ignore_command_input_until_us;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (input_guarded) {
        ESP_LOGI(TAG, "voice interaction ignored while cancel gesture drains");
        return false;
    }
    if (meeting_is_active()) {
        ESP_LOGW(TAG, "voice interaction ignored: meeting transition/upload active");
        return false;
    }
    EventBits_t network_bits = s_wifi_events ? xEventGroupGetBits(s_wifi_events) : 0;
    bool network_available = (network_bits & WIFI_CONNECTED_BIT) != 0;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    if (s_fangtang_use_cellular) network_available = ml307_transport_is_ready();
#endif
    if (s_setup_portal_active || !s_gateway_token[0] || !network_available) {
        ESP_LOGW(TAG,
                 "voice interaction rejected before capture: setup=%s paired=%s network=%s",
                 s_setup_portal_active ? "active" : "inactive",
                 s_gateway_token[0] ? "yes" : "no",
                 network_available ? "connected" : "offline");
        board_port_show_text("暂时无法说话",
                             !network_available ? "网络未连接，请稍后重试"
                                                : "设备尚未配对或正在设置");
        return false;
    }
    // A physical tap after ambient sleep only restores the ready pet. A
    // hands-free wake phrase, however, is an intentional voice action: wake
    // the panel and continue into this same capture rather than asking the
    // user to repeat the phrase.
    if (physical_screen_wake && board_port_wake_from_idle()) {
        ESP_LOGI(TAG, "sleeping display restored; voice capture deferred to next press");
        return false;
    }
    if (!physical_screen_wake && board_port_wake_from_idle()) {
        ESP_LOGI(TAG, "offline wake restored sleeping display; continuing into voice capture");
    }
    if (!s_interaction_lock || xSemaphoreTake(s_interaction_lock, 0) != pdTRUE) {
        ESP_LOGW(TAG, "voice interaction ignored: interaction already active");
        return false;
    }
    if (physical_screen_wake) {
        // MultiNet leaves the largest internal block below this worker's 10 KiB
        // stack requirement. A physical tap can safely release the model here;
        // wake-phrase entry instead releases it inside interaction_task().
        esp_err_t wake_stop_err = board_port_stop_wake_word();
        if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
            ESP_LOGW(TAG, "offline wake stop before voice task: %s",
                     esp_err_to_name(wake_stop_err));
        }
        log_heap_snapshot("voice-before-task-create");
    }
    s_foreground_http_requested = true;
    s_command_display_locked = true;
    s_command_timing_started_us = esp_timer_get_time();
    s_command_timing_capture_done_us = 0;
    s_command_timing_upload_done_us = 0;
    s_command_timing_accepted_us = 0;
    s_command_timing_first_progress_us = 0;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_command_cancel_requested = false;
    s_command_cancel_enabled = false;
    s_command_cancel_ui_shown = false;
    s_cancel_requested_generation = 0;
    s_cancel_ui_ready_generation = 0;
    // A stop request belongs to the preceding capture only. Clear it before
    // RECORDING becomes visible to the input task, so a rapid next tap is
    // retained and can end this newly started command.
    board_port_reset_capture_stop();
    uint32_t interaction_generation = ++s_interaction_generation;
    if (!interaction_generation) interaction_generation = ++s_interaction_generation;
    s_interaction_phase = INTERACTION_RECORDING;
    s_active_command_reply_to[0] = '\0';
    s_result_speech_reply_to[0] = '\0';
    s_result_speech_parts_remaining = 0;
    s_result_speech_deadline_us = 0;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_command_cancel_ui_ready) {
        while (xSemaphoreTake(s_command_cancel_ui_ready, 0) == pdTRUE) {}
    }
    board_port_set_command_display_lock(true);
    board_port_cancel_ready_prompt();
    TaskHandle_t created_handle = NULL;
    // Keep the command worker stack in internal RAM.  It calls Wi-Fi/TLS and
    // its callbacks can run while the flash cache is temporarily disabled;
    // a PSRAM-backed task stack is then unsafe and manifests as an intermittent
    // reboot immediately after the six-second recording completes.  Payloads
    // and HTTP buffers still use PSRAM, so this only reserves a small, stable
    // internal stack for control flow.
    BaseType_t created = xTaskCreate(interaction_task, "maclaw_interaction",
                                     10240, (void *)(uintptr_t)interaction_generation,
                                     5, &created_handle);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_interaction_task = created == pdPASS ? created_handle : NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (created != pdPASS) {
        s_foreground_http_requested = false;
        taskENTER_CRITICAL(&s_task_state_lock);
        s_interaction_phase = INTERACTION_RESULT;
        taskEXIT_CRITICAL(&s_task_state_lock);
        log_heap_snapshot("interaction-task-create-fail");
        xSemaphoreGive(s_interaction_lock);
        schedule_wake_restart();
        pet("alert");
        board_port_show_text("操作失败", "无法启动语音任务");
        return false;
    }
    return true;
}

static void on_wake_word(void *arg) {
    (void)arg;
    if (!s_startup_sequence_complete) {
        ESP_LOGI(TAG, "offline wake detected while startup greeting owns audio; ignored until ready");
        // The board has already retired MultiNet to safely hand this callback
        // off. Startup completion performs the matching re-arm.
        return;
    }
    EventBits_t wifi = s_wifi_events ? xEventGroupGetBits(s_wifi_events) : 0;
    bool network_available = (wifi & WIFI_CONNECTED_BIT) != 0;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    if (s_fangtang_use_cellular) network_available = ml307_transport_is_ready();
#endif
    if (s_setup_portal_active || !s_gateway_token[0] || !network_available) {
        ESP_LOGW(TAG, "offline wake detected but online interaction is unavailable: setup=%s paired=%s network=%s",
                 s_setup_portal_active ? "active" : "inactive",
                 s_gateway_token[0] ? "yes" : "no",
                 network_available ? "connected" : "offline");
        // Recognition is one-shot on EchoEar: the model is released before
        // this callback to make room for a possible voice worker. A rejected
        // phrase must therefore explicitly restore it, otherwise one wake
        // while Wi-Fi reconnects leaves hands-free input disabled forever.
        schedule_wake_restart();
        return;
    }
    ESP_LOGI(TAG, "offline wake accepted; starting voice interaction");
    (void)start_voice_interaction(false);
}

static void enter_setup_portal(void) {
    // Reconfiguration is explicitly requested by a long press. Do not erase
    // the working Wi-Fi or paired token before the replacement form is saved:
    // an accidental press, power loss, or abandoned phone session must leave
    // the device recoverable. The full setup form will atomically replace the
    // saved values and its normal save path will invalidate the old token only
    // when a new pairing code has actually been committed.
    pet("quiet");
    board_port_show_text("重新配置设备", "正在开启设置热点");
    /* Keep the existing STA up while enabling the setup AP. This avoids
     * tearing down an outstanding gateway long-poll inside the button task,
     * which previously stalled the portal before the QR screen appeared. */
    start_setup_portal(true);
}

static bool s_deferred_setup_running;

static void deferred_setup_task(void *arg) {
    (void)arg;
    /* The setup operation changes Wi-Fi mode, starts HTTP/DNS tasks and paints
     * a QR page. Always run it outside the hardware button task so the GPIO
     * scanner stays responsive and networking callbacks can make progress. */
    int64_t deadline = esp_timer_get_time() + 5000000;
    while (meeting_is_active() && esp_timer_get_time() < deadline) {
        vTaskDelay(pdMS_TO_TICKS(100));
    }
    ESP_LOGI(TAG, "deferred configuration portal starting");
    enter_setup_portal();
    s_deferred_setup_running = false;
    vTaskDelete(NULL);
}

static void on_user_input(board_input_action_t action, board_input_source_t source,
                          void *arg) {
    (void)arg;
    static bool suppress_alarm_dismiss_gesture;
    static board_input_source_t alarm_dismiss_source = BOARD_INPUT_SOURCE_UNKNOWN;

    if (s_command_capture_stop_gesture_pending &&
        source == s_command_capture_stop_source) {
        if (action == BOARD_INPUT_PRESSED) {
            // This is a genuinely new contact, not completion of the stop
            // contact. Admit it normally after retiring the old barrier.
            s_command_capture_stop_gesture_pending = false;
            s_command_capture_stop_source = BOARD_INPUT_SOURCE_UNKNOWN;
        } else {
            ESP_LOGI(TAG, "completed command-capture stop gesture consumed");
            return;
        }
    }

    // An alarm is an urgent local foreground owner and may become due while
    // networking or Welcome playback is still finishing. Keep its physical
    // dismiss control available before applying the normal startup gate.
#if CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD || CONFIG_MACLAW_BOARD_FANGTANG_4G
    bool alarm_dismiss_input = source == BOARD_INPUT_SOURCE_ACTIVATE_KEY;
#else
    bool alarm_dismiss_input = source == BOARD_INPUT_SOURCE_TOUCH;
#endif
    if (alarm_manager_is_ringing()) {
        if (alarm_dismiss_input) {
            alarm_manager_dismiss();
            if (action == BOARD_INPUT_PRESSED) {
                suppress_alarm_dismiss_gesture = true;
                alarm_dismiss_source = source;
            }
            ESP_LOGI(TAG, "ringing alarm dismissed by input source=%d action=%d",
                     (int)source, (int)action);
        } else {
            ESP_LOGI(TAG, "input ignored while alarm rings: source=%d action=%d",
                     (int)source, (int)action);
        }
        return;
    }

    if (!s_startup_sequence_complete) {
        // Startup owns the audio/display path until the optional greeting has
        // completed and the wake listener is loaded. Volume keys remain useful,
        // but activation gestures must not overtake this ordering boundary.
        if (action != BOARD_INPUT_VOLUME_UP && action != BOARD_INPUT_VOLUME_DOWN) {
            ESP_LOGI(TAG, "input ignored until startup Welcome sequence completes");
            return;
        }
    }

    // A down edge dismisses immediately; consume the completed gesture from
    // that same contact so it cannot also start voice, cancel, or configure.
    if (suppress_alarm_dismiss_gesture && action != BOARD_INPUT_PRESSED &&
        source == alarm_dismiss_source) {
        // A native double gesture may be followed by a delayed short from the
        // same contact-drain window. Keep suppression armed; the next real
        // down edge disarms it below before being handled normally.
        ESP_LOGI(TAG, "completed alarm-dismiss gesture consumed");
        return;
    }
    if (suppress_alarm_dismiss_gesture && action == BOARD_INPUT_PRESSED) {
        suppress_alarm_dismiss_gesture = false;
        alarm_dismiss_source = BOARD_INPUT_SOURCE_UNKNOWN;
    }
    // The down-edge action exists only for latency-sensitive foreground
    // surfaces. Preserve all established behavior on the completed gesture.
    if (action == BOARD_INPUT_PRESSED) {
        interaction_phase_t interaction_phase;
        taskENTER_CRITICAL(&s_task_state_lock);
        interaction_phase = s_interaction_phase;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (interaction_phase == INTERACTION_RECORDING) {
            board_port_request_capture_stop();
            s_command_capture_stop_gesture_pending = true;
            s_command_capture_stop_source = source;
            ESP_LOGI(TAG, "command recording stop requested by input source=%d",
                     (int)source);
        }
        return;
    }
    if (action == BOARD_INPUT_VOLUME_UP || action == BOARD_INPUT_VOLUME_DOWN) {
        // On a response page the available upper side key advances through the
        // reply. This keeps one-key reading in the natural 1 -> 2 -> 3 order;
        // the board renderer wraps the final page back to page 1. If the lower
        // key is confirmed later, it can use the opposite direction.
        int page_delta = action == BOARD_INPUT_VOLUME_UP ? 1 : -1;
        bool page_handled = app_ui_navigate_response(page_delta);
        ESP_LOGI(TAG, "volume key: %s page_delta=%d response_handled=%s",
                 action == BOARD_INPUT_VOLUME_UP ? "up" : "down", page_delta,
                 page_handled ? "yes" : "no");
        if (page_handled) return;
        unsigned volume = 0;
        int delta = action == BOARD_INPUT_VOLUME_UP ? 10 : -10;
        esp_err_t volume_err = board_port_adjust_output_volume(delta, &volume);
        if (volume_err == ESP_OK) {
            ESP_LOGI(TAG, "output volume: %u%%", volume);
        }
        return;
    }
    ESP_LOGI(TAG, "input action received: %s",
             action == BOARD_INPUT_PRIMARY ? "primary" :
             action == BOARD_INPUT_SECONDARY ? "secondary" : "configure");
    // The setup screen owns both the display and the radio. Treat touch/BOOT
    // input as inert until the submitted form deliberately restarts the
    // device; otherwise a stray tap starts normal voice UI and repaints the
    // QR while the phone is trying to configure the AP.
    if (s_setup_portal_active) {
        ESP_LOGI(TAG, "button ignored while setup portal is active");
        return;
    }
    meeting_state_t meeting = s_meeting_state;
    /* Reconfiguration is the emergency/maintenance gesture and must take
     * precedence over voice, meeting and upload state. Previously a long hold
     * was detected correctly but silently consumed by the meeting guards. */
    if (action == BOARD_INPUT_CONFIGURE) {
        if (!s_wifi_ssid[0]
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
            && !s_fangtang_use_cellular
#endif
        ) {
            ESP_LOGI(TAG, "long press ignored while setup portal is active");
            return;
        }
        ESP_LOGW(TAG, "long press: configuration requested (meeting state=%d)", (int)meeting);
        /* Use a clean reboot as the transaction boundary. The next boot sees
         * the persisted setup request before starting STA/TLS, so it can enter
         * AP mode deterministically without racing an active long poll. */
        nvs_handle_t setup_nvs;
        esp_err_t setup_err = nvs_open("maclaw", NVS_READWRITE, &setup_nvs);
        if (setup_err == ESP_OK) {
            setup_err = nvs_set_u8(setup_nvs, "force_setup", 1);
            if (setup_err == ESP_OK) setup_err = nvs_commit(setup_nvs);
            nvs_close(setup_nvs);
        }
        if (setup_err == ESP_OK) {
            ESP_LOGW(TAG, "configuration request saved; rebooting into setup");
            esp_restart();
        }
        ESP_LOGE(TAG, "cannot persist configuration request: %s", esp_err_to_name(setup_err));
        if (meeting == MEETING_RECORDING || meeting == MEETING_PAUSED) {
            meeting_set_state(MEETING_FINALIZING);
        }
        if (!s_deferred_setup_running) {
            s_deferred_setup_running = true;
            if (xTaskCreate(deferred_setup_task, "maclaw_setup_wait", 12288, NULL, 5, NULL) != pdPASS) {
                s_deferred_setup_running = false;
                ESP_LOGE(TAG, "cannot create configuration portal worker");
            } else {
                ESP_LOGI(TAG, "configuration portal worker created");
            }
        }
        return;
    }
    if (meeting == MEETING_RECORDING || meeting == MEETING_PAUSED) {
        // Stopping must work with the one dependable primary input fitted to
        // each enclosure: touch on EchoEar, or the activation key on Bread and
        // Fangtang. Accept every completed gesture as stop/save; a user should
        // not need a tight double tap while recording.
        // Do not repaint here: this callback runs in a hardware input task and
        // a full LCD DMA present can block it long enough to trip task_wdt. The
        // meeting task observes FINALIZING and owns the following UI updates.
        meeting_set_state(MEETING_FINALIZING);
        ESP_LOGI(TAG, "meeting stop requested: gesture=%s",
                 action == BOARD_INPUT_PRIMARY ? "primary" :
                 action == BOARD_INPUT_SECONDARY ? "secondary" : "configure");
        return;
    }
    if (meeting_is_active()) {
        ESP_LOGW(TAG, "button ignored: meeting transition/upload active");
        return;
    }
    if (action == BOARD_INPUT_SECONDARY) {
        bool interaction_active;
        interaction_phase_t interaction_phase;
        taskENTER_CRITICAL(&s_task_state_lock);
        interaction_active = s_interaction_task != NULL;
        interaction_phase = s_interaction_phase;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (interaction_active || interaction_phase == INTERACTION_RECORDING ||
            interaction_phase == INTERACTION_PROCESSING) {
            // One foreground action owns the activation key until it reaches a
            // result. During processing a double press means cancel; during the
            // fixed-length capture it is simply consumed. It can never fall
            // through and start a meeting recording in either phase.
            if (interaction_phase == INTERACTION_PROCESSING) {
                (void)request_command_cancel();
            } else {
                ESP_LOGI(TAG, "secondary input consumed by command recording");
            }
            return;
        }
        if (s_meeting_pending) {
            bool resume_running;
            taskENTER_CRITICAL(&s_task_state_lock);
            resume_running = s_meeting_task_running;
            taskEXIT_CRITICAL(&s_task_state_lock);
            if (resume_running) {
                // A worker is already transferring the retained file. Calling
                // start_meeting_task() again only reports a busy condition; it
                // is not a network failure and must not be labelled as one.
                board_port_show_text("会议记录续传中", "完成后可开始新会议");
            } else if (ensure_meeting_resume_supervisor()) {
                board_port_show_text("正在续传上次录音", "完成后可开始新会议");
            } else {
                pet("alert");
                board_port_show_text("续传任务未启动", "设备将稍后自动重试");
            }
            return;
        }
        if (!s_meeting_available) {
            // A stale handshake must not permanently disable a local hardware
            // feature. Re-negotiate on demand, then continue the same double
            // tap if the current Hub advertises meeting recording.
            if (!refresh_meeting_capability()) {
                pet("alert");
                board_port_show_text("录音启动失败", "无法检查网关支持");
            }
            return;
        }
        // A previous answer may deliberately remain on screen after its task
        // completes. Release that presentation lock as part of the explicit
        // transition into meeting mode so old command UI cannot interleave
        // with the meeting recorder.
        s_command_display_locked = false;
        board_port_set_command_display_lock(false);
        if (!start_meeting_task(false)) {
            pet("alert");
            board_port_show_text("录音启动失败", "设备正在处理其它操作");
        }
        return;
    }
    if (action != BOARD_INPUT_PRIMARY) return;
    // The result is a deliberate terminal step in the command flow. The first
    // activation press closes it and returns to the clock/date/weather screen;
    // only a later press starts a new recording. This avoids accidentally
    // recording while the user is still reading the answer.
    if (board_port_dismiss_response()) {
        dismiss_result_speech_transaction();
        taskENTER_CRITICAL(&s_task_state_lock);
        s_interaction_phase = INTERACTION_IDLE;
        taskEXIT_CRITICAL(&s_task_state_lock);
        s_command_display_locked = false;
        board_port_set_command_display_lock(false);
        pet("idle");
        ESP_LOGI(TAG, "response dismissed; ambient screen restored");
        return;
    }
    // A physical press only wakes a sleeping LCD; the offline wake phrase is
    // hands-free and therefore wakes the panel and records in the same event.
    (void)start_voice_interaction(true);
}
static void init_network_core(void) {
    if (s_network_initialized) return;
    s_wifi_events = xEventGroupCreate();
    ESP_ERROR_CHECK(s_wifi_events ? ESP_OK : ESP_ERR_NO_MEM);
    ESP_ERROR_CHECK(esp_netif_init());
    ESP_ERROR_CHECK(esp_event_loop_create_default());
    s_network_initialized = true;
}

static void init_network(void) {
    init_network_core();
    if (s_wifi_driver_initialized) return;
    wifi_init_config_t init = WIFI_INIT_CONFIG_DEFAULT();
    // This access point triggers an ESP-IDF 6.0.2 Wi-Fi RX timer crash after
    // the first block-ack (BA) setup.  EchoEar's command traffic is tiny, so
    // disable aggregation before driver startup for a stable station link.
    init.ampdu_rx_enable = 0;
    init.ampdu_tx_enable = 0;
    ESP_ERROR_CHECK(esp_wifi_init(&init));
    ESP_ERROR_CHECK(esp_event_handler_instance_register(WIFI_EVENT, ESP_EVENT_ANY_ID, wifi_event, NULL, NULL));
    ESP_ERROR_CHECK(esp_event_handler_instance_register(IP_EVENT, IP_EVENT_STA_GOT_IP, wifi_event, NULL, NULL));
    ESP_ERROR_CHECK(esp_event_handler_instance_register(IP_EVENT, IP_EVENT_ASSIGNED_IP_TO_CLIENT,
                                                         wifi_event, NULL, NULL));
    s_wifi_driver_initialized = true;
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

static const char *setup_auth_mode_label(wifi_auth_mode_t mode);

static bool setup_auth_mode_is_enterprise(wifi_auth_mode_t mode) {
    // ESP-IDF 6 distinguishes WPA, WPA2, WPA3-transition and WPA3-192-bit
    // enterprise networks.  All of them need the 802.1X part of the setup
    // form; limiting this to the WPA2 alias silently selected "Personal" for
    // newer office networks.
    return mode == WIFI_AUTH_WPA_ENTERPRISE ||
           mode == WIFI_AUTH_WPA2_ENTERPRISE ||
           mode == WIFI_AUTH_WPA3_ENTERPRISE ||
           mode == WIFI_AUTH_WPA2_WPA3_ENTERPRISE ||
           mode == WIFI_AUTH_WPA3_ENT_192;
}

static bool append_setup_ssid_option(const char *ssid, int rssi, wifi_auth_mode_t authmode,
                                     bool selected) {
    if (!ssid || !ssid[0]) return true;
    size_t used = strlen(s_setup_ssid_options);
    if (setup_ssid_is_selectable(ssid)) return true;
    const char *prefix = "<option value=\"";
    const char *selected_attr = selected ? " selected" : "";
    const char *enterprise_attr = setup_auth_mode_is_enterprise(authmode)
                                       ? " data-enterprise=1" : "";
    const char *suffix = "</option>";
    size_t escaped_length = setup_html_escaped_length(ssid);
    const char *security = setup_auth_mode_label(authmode);
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

static int compare_setup_ap_records(const void *left, const void *right) {
    const wifi_ap_record_t *a = left;
    const wifi_ap_record_t *b = right;
    return (int)b->rssi - (int)a->rssi;
}

static const char *setup_auth_mode_label(wifi_auth_mode_t mode) {
    switch (mode) {
        case WIFI_AUTH_OPEN: return "open";
        case WIFI_AUTH_WEP: return "WEP";
        case WIFI_AUTH_WPA_PSK: return "WPA";
        case WIFI_AUTH_WPA2_PSK: return "WPA2";
        case WIFI_AUTH_WPA_WPA2_PSK: return "WPA/WPA2";
        case WIFI_AUTH_WPA3_PSK: return "WPA3";
        case WIFI_AUTH_WPA2_WPA3_PSK: return "WPA2/WPA3";
        case WIFI_AUTH_WPA_ENTERPRISE: return "WPA-802.1X";
        case WIFI_AUTH_WPA2_ENTERPRISE: return "WPA2-802.1X";
        case WIFI_AUTH_WPA3_ENTERPRISE: return "WPA3-802.1X";
        case WIFI_AUTH_WPA2_WPA3_ENTERPRISE: return "WPA2/WPA3-802.1X";
        case WIFI_AUTH_WPA3_ENT_192: return "WPA3-192 802.1X";
        default: return "secured";
    }
}

static bool refresh_setup_ssid_options(void) {
    if (!s_setup_options_mutex ||
        xSemaphoreTake(s_setup_options_mutex, pdMS_TO_TICKS(15000)) != pdTRUE) {
        ESP_LOGW(TAG, "setup Wi-Fi scan already in progress");
        return false;
    }

    wifi_scan_config_t config = {0};
    config.show_hidden = false;
    esp_err_t err = esp_wifi_scan_start(&config, true);
    bool refreshed = false;
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "Wi-Fi scan for setup list failed: %s", esp_err_to_name(err));
    } else {
        uint16_t count = SETUP_SCAN_MAX_APS;
        memset(s_setup_scan_records, 0, SETUP_SCAN_MAX_APS * sizeof(*s_setup_scan_records));
        err = esp_wifi_scan_get_ap_records(&count, s_setup_scan_records);
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "cannot read Wi-Fi scan results: %s", esp_err_to_name(err));
        } else {
            // A successful scan atomically replaces the selectable list while
            // this mutex is held.  On a scan error above, the old list remains
            // untouched so a user can safely retry without losing their choice.
            s_setup_ssid_options[0] = '\0';
            s_setup_ssid_choices[0] = '\0';
            qsort(s_setup_scan_records, count, sizeof(s_setup_scan_records[0]),
                  compare_setup_ap_records);
            for (uint16_t i = 0; i < count; ++i) {
                const char *ssid = (const char *)s_setup_scan_records[i].ssid;
                // A scan may report the same SSID from multiple radios.
                if (setup_ssid_is_selectable(ssid)) continue;
                if (!append_setup_ssid_option(ssid, s_setup_scan_records[i].rssi,
                                                s_setup_scan_records[i].authmode,
                                                s_wifi_ssid[0] && !strcmp(ssid, s_wifi_ssid))) break;
            }
            refreshed = true;
            ESP_LOGI(TAG, "setup Wi-Fi selection list contains %u scanned networks", (unsigned)count);
        }
    }
    if (refreshed && !s_setup_ssid_options[0]) {
        strlcpy(s_setup_ssid_options,
                "<option value=\"\" selected disabled>No visible Wi-Fi networks found; refresh the hotspot and try again.</option>",
                SETUP_SSID_OPTIONS_CAPACITY);
    }
    xSemaphoreGive(s_setup_options_mutex);
    return refreshed;
}

static void configure_setup_ap_ip(void) {
    if (!s_setup_ap_netif) return;
    esp_netif_ip_info_t ip_info = {0};
    IP4_ADDR(&ip_info.ip, 192, 168, 4, 1);
    IP4_ADDR(&ip_info.gw, 192, 168, 4, 1);
    IP4_ADDR(&ip_info.netmask, 255, 255, 255, 0);
    esp_err_t stop_err = esp_netif_dhcps_stop(s_setup_ap_netif);
    if (stop_err != ESP_OK && stop_err != ESP_ERR_ESP_NETIF_DHCP_ALREADY_STOPPED) {
        ESP_LOGW(TAG, "cannot pause DHCP server to configure setup IP: %s", esp_err_to_name(stop_err));
        return;
    }
    esp_err_t ip_err = esp_netif_set_ip_info(s_setup_ap_netif, &ip_info);
    // Explicitly advertise the SoftAP as DNS. On IDF 6 the DHCP server can
    // otherwise inherit a stale upstream resolver while APSTA is entered from
    // a connected station, so the phone never sends its captive probe to us.
    esp_netif_dns_info_t dns = {0};
    IP4_ADDR(&dns.ip.u_addr.ip4, 192, 168, 4, 1);
    dns.ip.type = ESP_IPADDR_TYPE_V4;
    uint8_t offer_dns = DHCPS_OFFER_DNS;
    esp_err_t dns_offer_err = ip_err == ESP_OK
                                  ? esp_netif_dhcps_option(s_setup_ap_netif, ESP_NETIF_OP_SET,
                                                           ESP_NETIF_DOMAIN_NAME_SERVER,
                                                           &offer_dns, sizeof(offer_dns))
                                  : ip_err;
    esp_err_t dns_err = dns_offer_err == ESP_OK
                            ? esp_netif_set_dns_info(s_setup_ap_netif, ESP_NETIF_DNS_MAIN, &dns)
                            : dns_offer_err;
    // DHCP option 114 is the standards-based captive-portal signal used by
    // recent Android and iOS releases. DNS interception remains necessary for
    // older clients and for Windows.
    esp_err_t portal_uri_err = dns_err == ESP_OK
                                   ? esp_netif_dhcps_option(s_setup_ap_netif, ESP_NETIF_OP_SET,
                                                            ESP_NETIF_CAPTIVEPORTAL_URI,
                                                            (void *)s_setup_captive_portal_uri,
                                                            sizeof(s_setup_captive_portal_uri))
                                   : dns_err;
    esp_err_t start_err = esp_netif_dhcps_start(s_setup_ap_netif);
    if (ip_err != ESP_OK || dns_err != ESP_OK || portal_uri_err != ESP_OK ||
        (start_err != ESP_OK && start_err != ESP_ERR_ESP_NETIF_DHCP_ALREADY_STARTED)) {
        ESP_LOGW(TAG, "cannot configure setup DHCP server: ip=%s dns=%s portal=%s start=%s",
                 esp_err_to_name(ip_err), esp_err_to_name(dns_err),
                 esp_err_to_name(portal_uri_err), esp_err_to_name(start_err));
    } else {
        ESP_LOGI(TAG, "setup DHCP advertises gateway/DNS/portal=%s", SETUP_CAPTIVE_PORTAL_URI);
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
    while (s_setup_portal_active) {
        uint8_t packet[DNS_PACKET_CAPACITY];
        struct sockaddr_in source = {0};
        socklen_t source_len = sizeof(source);
        int received = recvfrom(socket_fd, packet, sizeof(packet), 0,
                                (struct sockaddr *)&source, &source_len);
        if (received < 12 || received + 16 > sizeof(packet)) continue;
        // Match the proven reference implementation: reply to every DNS
        // question with the portal address.  Phones often send HTTPS, AAAA,
        // and A probes together; treating every request uniformly prevents an
        // early non-A lookup from delaying captive-network detection.
        size_t cursor = (size_t)received;
        packet[2] |= 0x80;
        packet[3] |= 0x80;
        packet[6] = 0;
        packet[7] = 1;
        packet[8] = 0; packet[9] = 0;
        packet[10] = 0; packet[11] = 0;
        packet[cursor++] = 0xC0; packet[cursor++] = 0x0C; // answer name = question name
        packet[cursor++] = 0; packet[cursor++] = 1;        // A
        packet[cursor++] = 0; packet[cursor++] = 1;        // IN
        packet[cursor++] = 0; packet[cursor++] = 0;
        packet[cursor++] = 0; packet[cursor++] = 0; packet[cursor++] = 0; packet[cursor++] = 28;
        packet[cursor++] = 0; packet[cursor++] = 4;
        packet[cursor++] = 192; packet[cursor++] = 168; packet[cursor++] = 4; packet[cursor++] = 1;
        (void)sendto(socket_fd, packet, cursor, 0, (struct sockaddr *)&source, source_len);
    }
    close(socket_fd);
    s_dns_task = NULL;
    vTaskDelete(NULL);
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
    static const char setup_page_prefix[] =
        "<!doctype html><html><head><meta name=viewport content='width=device-width,initial-scale=1'>"
        "<style>body{font-family:system-ui,sans-serif;max-width:26rem;margin:2rem auto;padding:0 1rem;color:#102a43}"
        "label{display:block;margin:1rem 0 .3rem}input,select{box-sizing:border-box;width:100%;padding:.7rem;font-size:1rem}"
        ".enterprise{margin-top:1rem;padding:.85rem;border:1px solid #b9c9d7;background:#f5f9fc}.hint{font-size:.85rem;color:#486581;line-height:1.45}"
        "button{margin-top:1.3rem;padding:.8rem 1.2rem;font-size:1rem;background:#1769aa;color:#fff;border:0;border-radius:.4rem}</style>"
        "</head><body><h1>MaClaw Pet setup</h1><p>Choose your home or office Wi-Fi. The device will restart and connect automatically.</p>"
        "<form method=post action=/save><label>Wi-Fi network</label><select name=ssid required>";
    static const char setup_page_suffix[] =
        "</select><p class=hint>Only visible Wi-Fi networks are shown. <a href=/refresh>Refresh network list</a>. Hidden networks must temporarily enable SSID broadcast.</p>"
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
        "<button>Save and connect</button></form><script>(function(){var n=document.querySelector('[name=ssid]'),s=document.getElementById('security');function u(){if(n&&n.selectedOptions[0]&&n.selectedOptions[0].dataset.enterprise==='1'){s.value='enterprise';s.dispatchEvent(new Event('change'))}}n&&n.addEventListener('change',u);u()})()</script></body></html>";
    static const char scan_failed_notice[] =
        "<p class=hint role=alert>Could not refresh Wi-Fi networks. Showing the previous list; please try again.</p>";
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
    if (s_pairing_recovery_portal) return httpd_resp_send(req, pairing_page, HTTPD_RESP_USE_STRLEN);
    if (!s_setup_options_mutex ||
        xSemaphoreTake(s_setup_options_mutex, pdMS_TO_TICKS(2000)) != pdTRUE) {
        httpd_resp_set_status(req, "503 Service Unavailable");
        return httpd_resp_sendstr(req, "Wi-Fi scan is in progress; please retry.");
    }
    char query[32] = {0};
    bool scan_failed = httpd_req_get_url_query_len(req) < sizeof(query) &&
                       httpd_req_get_url_query_str(req, query, sizeof(query)) == ESP_OK &&
                       !strcmp(query, "scan=failed");
    if (httpd_resp_sendstr_chunk(req, setup_page_prefix) != ESP_OK ||
        (scan_failed && httpd_resp_sendstr_chunk(req, scan_failed_notice) != ESP_OK) ||
        httpd_resp_sendstr_chunk(req, s_setup_ssid_options) != ESP_OK ||
        httpd_resp_sendstr_chunk(req, setup_page_suffix) != ESP_OK) {
        xSemaphoreGive(s_setup_options_mutex);
        return ESP_FAIL;
    }
    esp_err_t err = httpd_resp_sendstr_chunk(req, NULL);
    xSemaphoreGive(s_setup_options_mutex);
    return err;
}

static esp_err_t captive_redirect_handler(httpd_req_t *req) {
    // A 302 is intentionally used here instead of a successful probe body:
    // the OS then identifies this as a captive network and presents its login
    // surface, which follows the redirect to the configuration page.
    httpd_resp_set_status(req, "302 Found");
    httpd_resp_set_hdr(req, "Location", "http://" SETUP_AP_IP_ADDR "/");
    httpd_resp_set_type(req, "text/html; charset=utf-8");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    // Probe clients do not need a persistent HTTP connection. Closing it makes
    // the redirect deterministic for the small captive-portal web views used
    // by Android, iOS and Windows.
    httpd_resp_set_hdr(req, "Connection", "close");
    return httpd_resp_send(req, NULL, 0);
}

static esp_err_t setup_refresh_handler(httpd_req_t *req) {
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
    // Recovery preserves the already selected backhaul. On Wi-Fi boards this
    // means the saved station; on Fangtang 4G it means the ML307 connection.
    // The form field remains named "reuse" for wire compatibility.
    bool reuse_network = form_value(body, "reuse", reuse, sizeof(reuse)) && !strcmp(reuse, "1");
    if (reuse_network) {
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
    if (!reuse_network) {
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
    bool selectable = false;
    if (!reuse_network && s_setup_options_mutex &&
        xSemaphoreTake(s_setup_options_mutex, pdMS_TO_TICKS(2000)) == pdTRUE) {
        selectable = setup_ssid_is_selectable(ssid);
        xSemaphoreGive(s_setup_options_mutex);
    }
    if (!reuse_network && (!is_valid_setup_selected_ssid(ssid) || !selectable)) {
        ESP_LOGW(TAG, "setup rejected SSID that was not in the current scan list");
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST,
                            "Select a Wi-Fi network from the list, then try again.");
        return ESP_FAIL;
    }
    // Recovery changes only the one-time pairing code. Never erase a persisted
    // device token merely because the portal was opened; the code exists only
    // to retrieve a token after authentication has conclusively failed.
    esp_err_t save_err = reuse_network ? save_pairing_code_only(code)
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
    // Do not reset from the HTTP server task.  esp_http_server sends responses
    // asynchronously, so a reset here can race its final socket write and, on
    // this board, leave the setup QR frame on screen indefinitely.  Schedule
    // the reset after this handler has returned and the response is flushed.
    httpd_resp_sendstr(req, "Saved. The device is restarting and will connect to MaClaw.");
    if (!s_setup_restart_task) {
        BaseType_t created = xTaskCreate(setup_restart_task, "maclaw_setup_restart", 2048,
                                         NULL, 2, &s_setup_restart_task);
        if (created != pdPASS) {
            s_setup_restart_task = NULL;
            ESP_LOGE(TAG, "cannot schedule restart after setup save");
        }
    }
    return ESP_OK;
}

static void recover_after_setup_portal_start_failure(bool wake_was_stopped) {
    s_setup_portal_active = false;
    s_pairing_recovery_portal = false;
    // The portal stops MultiNet before allocating its HTTP server.  If one of
    // those allocations fails on a configured device, restore the normal local
    // interaction path instead of leaving the device silent until a reboot.
    if (!wake_was_stopped) return;
    esp_err_t err = board_port_start_wake_word(on_wake_word, NULL);
    if (err != ESP_OK && err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "cannot restore offline wake after setup portal failure: %s",
                 esp_err_to_name(err));
    }
}

static void start_setup_portal(bool keep_station) {
    // Set this before any slow display or Wi-Fi operation. A button event can
    // be delivered by its independent task while the QR page is being drawn.
    if (s_setup_portal_active && s_setup_server) {
        ESP_LOGI(TAG, "setup portal already active");
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
    if (!s_setup_scan_records) {
        s_setup_scan_records = heap_caps_calloc(
            SETUP_SCAN_MAX_APS, sizeof(*s_setup_scan_records),
            MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_setup_ssid_options || !s_setup_ssid_choices || !s_setup_scan_records) {
        s_pairing_recovery_portal = false;
        ESP_LOGE(TAG, "cannot allocate setup portal Wi-Fi list buffers");
        board_port_show_text("设置失败", "内存不足，请重启后再试");
        return;
    }
    // A prior DNS task may still be in its one-second receive timeout after an
    // earlier failed startup. Ask it to exit before creating another responder.
    if (s_dns_task) {
        s_setup_portal_active = false;
        ESP_LOGW(TAG, "waiting for previous captive DNS task before starting portal");
        for (unsigned wait = 0; wait < 12 && s_dns_task; ++wait) {
            vTaskDelay(pdMS_TO_TICKS(100));
        }
        if (s_dns_task) {
            s_pairing_recovery_portal = false;
            ESP_LOGE(TAG, "previous captive DNS task did not exit");
            board_port_show_text("配置失败", "请重启设备后再试");
            return;
        }
    }
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
    bool wake_was_stopped = wake_stop_err == ESP_OK;
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
    // Use the same AP address/DHCP sequence as the working Nulllab AI Vox3
    // provisioning component before any client can associate.
    configure_setup_ap_ip();
    // Bind the captive DNS responder before enabling the AP, matching the
    // working Nulllab implementation.  This closes the gap where a phone can
    // obtain a lease and send its first probe before DNS is listening.
    start_captive_dns();
    // A failed first-time Wi-Fi join should show the full form again, even
    // though the submitted SSID is now persisted. Pairing recovery is the
    // only flow that intentionally reuses Wi-Fi and asks solely for a code.
    s_pairing_recovery_portal = keep_station;
    // Wi-Fi scans require a STA interface, so normal provisioning uses APSTA.
    // Fangtang cellular pairing recovery only asks for a new pairing code: its
    // ML307 remains the backhaul, and starting an unused STA/scan path here can
    // leave ESP-IDF's scan timer with a stale notification target. Use a plain
    // SoftAP for that one flow.
    bool cellular_pairing_ap_only = false;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    cellular_pairing_ap_only = s_fangtang_use_cellular && s_pairing_recovery_portal;
#endif
    if (!cellular_pairing_ap_only) ensure_station_netif();
    // A Fangtang in 4G mode still uses the Wi-Fi AP for local provisioning,
    // but its backhaul is the independent ML307.  Do not reconnect the saved
    // Wi-Fi STA while bringing up that AP: doing so races the setup scan and
    // needlessly runs two network bring-up paths at once.
    bool keep_wifi_station = s_pairing_recovery_portal;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    if (s_fangtang_use_cellular) keep_wifi_station = false;
#endif
    s_station_auto_connect = keep_wifi_station;
    if (!keep_wifi_station && s_wifi_started) {
        s_station_expected_disconnect = true;
        esp_err_t disconnect_err = esp_wifi_disconnect();
        if (disconnect_err != ESP_OK && disconnect_err != ESP_ERR_WIFI_NOT_CONNECT) {
            s_station_expected_disconnect = false;
            ESP_LOGW(TAG, "cannot stop station while entering setup portal: %s",
                     esp_err_to_name(disconnect_err));
        }
    }
    wifi_config_t ap = { .ap = { .channel = 1, .max_connection = 4, .authmode = WIFI_AUTH_OPEN } };
    strlcpy((char *)ap.ap.ssid, ap_ssid, sizeof(ap.ap.ssid));
    ap.ap.ssid_len = strlen(ap_ssid);
    esp_err_t portal_err = esp_wifi_set_mode(cellular_pairing_ap_only
                                                 ? WIFI_MODE_AP
                                                 : WIFI_MODE_APSTA);
    if (portal_err != ESP_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped);
        ESP_LOGE(TAG, "cannot enter setup Wi-Fi mode: %s", esp_err_to_name(portal_err));
        board_port_show_text("设置失败", "请在网页重新设置");
        return;
    }
    portal_err = esp_wifi_set_config(WIFI_IF_AP, &ap);
    if (portal_err != ESP_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped);
        ESP_LOGE(TAG, "cannot configure setup hotspot: %s", esp_err_to_name(portal_err));
        board_port_show_text("设置失败", "请在网页重新设置");
        return;
    }
    portal_err = esp_wifi_set_ps(WIFI_PS_NONE);
    if (portal_err != ESP_OK) {
        ESP_LOGW(TAG, "cannot disable Wi-Fi power save for setup portal: %s",
                 esp_err_to_name(portal_err));
    }
    if (!s_wifi_started) {
        portal_err = esp_wifi_start();
        if (portal_err != ESP_OK) {
            recover_after_setup_portal_start_failure(wake_was_stopped);
            ESP_LOGE(TAG, "cannot start setup hotspot: %s", esp_err_to_name(portal_err));
            board_port_show_text("设置失败", "请在网页重新设置");
            return;
        }
        s_wifi_started = true;
    }
    // When the radio was already running in STA mode, set_mode(APSTA) and
    // set_config() do not always immediately publish the new SoftAP beacon.
    // Reconnect the AP interface explicitly and verify that it is active.
    if (keep_wifi_station) {
        esp_err_t connect_err = esp_wifi_connect();
        if (connect_err != ESP_OK && connect_err != ESP_ERR_WIFI_CONN) {
            ESP_LOGW(TAG, "station reconnect while enabling portal: %s", esp_err_to_name(connect_err));
        }
    }
    wifi_mode_t active_mode = WIFI_MODE_NULL;
    portal_err = esp_wifi_get_mode(&active_mode);
    if (portal_err != ESP_OK || (active_mode != WIFI_MODE_AP && active_mode != WIFI_MODE_APSTA)) {
        recover_after_setup_portal_start_failure(wake_was_stopped);
        ESP_LOGE(TAG, "setup hotspot did not enter AP mode: err=%s mode=%d",
                 esp_err_to_name(portal_err), (int)active_mode);
        board_port_show_text("设置热点失败", "请重启后再试");
        return;
    }
    // Build the choice list before serving the form.  The scan is performed
    // only once per portal entry, keeping the SoftAP responsive while the
    // phone completes captive-portal checks.
    if (!cellular_pairing_ap_only) refresh_setup_ssid_options();
    httpd_config_t server_config = HTTPD_DEFAULT_CONFIG();
    // ESP-SR consumes a meaningful part of internal RAM. IDF 6 needs more than
    // the default 4 KB while serving the setup form. This task must remain in
    // internal RAM because the handler writes NVS and flash operations disable
    // the external-RAM cache while checking the current task stack.
    server_config.stack_size = 6144;
    // Thirteen platform-specific captive-check endpoints, exact root, refresh,
    // GET wildcard and POST /save. iOS, Android, Windows and Firefox vary by
    // OS version, carrier and whether they are retrying a prior probe.
    // This capacity is checked when routes are registered at runtime.
    server_config.max_uri_handlers = 24;
    // Captive checks are usually parallel.  Keep enough connections for their
    // redirects and the portal web view to coexist; otherwise a probe can
    // occupy the tiny server while the OS decides the hotspot has no portal.
    server_config.max_open_sockets = 5;
    server_config.lru_purge_enable = true;
    // Make the AP behave like a captive portal. Android, iOS and Windows all
    // probe different HTTP paths before showing the setup page; the wildcard
    // returns the same deterministic form for those paths and manual URLs.
    server_config.uri_match_fn = httpd_uri_match_wildcard;
    portal_err = httpd_start(&s_setup_server, &server_config);
    if (portal_err != ESP_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped);
        ESP_LOGE(TAG, "cannot start setup web server: %s, free_heap=%u",
                 esp_err_to_name(portal_err), (unsigned)esp_get_free_heap_size());
        board_port_show_text("设置失败", "网页服务内存不足，请重启");
        return;
    }
    httpd_uri_t apple_success = {.uri = "/hotspot-detect.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t apple_library_success = {.uri = "/library/test/success.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_generate_204 = {.uri = "/generate_204", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_gen_204 = {.uri = "/gen_204", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_redirect = {.uri = "/redirect", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_mobile_status = {.uri = "/mobile/status.php", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t windows_connect = {.uri = "/connecttest.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    // Older Windows releases use the NCSI probe host/path rather than
    // msftconnecttest.com/connecttest.txt.  It needs the same redirect to
    // make the operating system open its captive-network sign-in surface.
    httpd_uri_t windows_ncsi = {.uri = "/ncsi.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t windows_network_status = {.uri = "/check_network_status.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t windows_fwlink = {.uri = "/fwlink/", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t firefox_connectivity = {.uri = "/connectivity-check.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t generic_success = {.uri = "/success.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t generic_portal = {.uri = "/portal.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    // ESP-IDF wildcard matching treats "/*" as paths with a slash after the
    // root; register the exact root separately so a direct 192.168.4.1 request
    // and the redirect target never depend on wildcard edge-case behaviour.
    httpd_uri_t root = {.uri = "/", .method = HTTP_GET, .handler = setup_get_handler};
    httpd_uri_t refresh = {.uri = "/refresh", .method = HTTP_GET, .handler = setup_refresh_handler};
    httpd_uri_t captive = {.uri = "/*", .method = HTTP_GET, .handler = setup_get_handler};
    httpd_uri_t save = {.uri = "/save", .method = HTTP_POST, .handler = setup_save_handler};
    // Register the wildcard last: ESP-IDF preserves registration order during
    // matching, so it must not shadow the platform-specific probe routes.
    portal_err = httpd_register_uri_handler(s_setup_server, &apple_success);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &apple_library_success);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_generate_204);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_gen_204);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_redirect);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_mobile_status);
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
    if (portal_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register setup portal routes: %s, free_heap=%u",
                 esp_err_to_name(portal_err), (unsigned)esp_get_free_heap_size());
        httpd_stop(s_setup_server);
        s_setup_server = NULL;
        recover_after_setup_portal_start_failure(wake_was_stopped);
        board_port_show_text("设置失败", "配置网页路由启动失败");
        return;
    }
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
    if (base == WIFI_EVENT && id == WIFI_EVENT_AP_STACONNECTED) {
        const wifi_event_ap_staconnected_t *event = data;
        if (event) {
            ESP_LOGI(TAG, "setup client associated: " MACSTR, MAC2STR(event->mac));
        }
        return;
    }
    if (base == IP_EVENT && id == IP_EVENT_ASSIGNED_IP_TO_CLIENT) {
        const ip_event_assigned_ip_to_client_t *event = data;
        char address[16] = {0};
        if (event) esp_ip4addr_ntoa(&event->ip, address, sizeof(address));
        ESP_LOGI(TAG, "setup client leased IP=%s hostname=%s", address[0] ? address : "unknown",
                 event && event->hostname[0] ? event->hostname : "unknown");
        return;
    }
    if (base == WIFI_EVENT && id == WIFI_EVENT_STA_START) {
        if (s_station_auto_connect) esp_wifi_connect();
        return;
    }
    if (base == WIFI_EVENT && id == WIFI_EVENT_STA_DISCONNECTED) {
        xEventGroupClearBits(s_wifi_events, WIFI_CONNECTED_BIT);
        board_port_set_wifi_status(s_wifi_ssid, false);
        board_port_set_service_ready(false);
        firmware_identity_set_service_ready(false);
        if (s_station_expected_disconnect) {
            s_station_expected_disconnect = false;
            ESP_LOGI(TAG, "station disconnected for setup scan");
            return;
        }
        if (s_station_auto_connect) {
            ESP_LOGW(TAG, "Wi-Fi disconnected from %s; retrying", s_wifi_ssid);
            esp_wifi_connect();
        }
        return;
    }
    if (base == IP_EVENT && id == IP_EVENT_STA_GOT_IP) {
        xEventGroupSetBits(s_wifi_events, WIFI_CONNECTED_BIT);
        // The normal status surface is still covered by the explicit startup
        // screen here. Avoid a full LCD transfer from the IP event loop; the
        // ready transition will publish the connected state after handshake.
        ESP_LOGI(TAG, "Wi-Fi connected to %s", s_wifi_ssid);
        taskENTER_CRITICAL(&s_task_state_lock);
        bool recover_gateway = s_gateway_startup_allowed &&
                               !s_gateway_startup_running &&
                               !s_startup_sequence_complete &&
                               !s_setup_portal_active;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (recover_gateway) {
            ESP_LOGI(TAG, "starting gateway startup after Wi-Fi recovery");
            if (!start_gateway_startup_task()) {
                ESP_LOGE(TAG, "cannot restart gateway startup after Wi-Fi recovery");
            }
        }
    }
}

#if CONFIG_MACLAW_BOARD_FANGTANG_4G
static void cellular_recovery_task(void *arg) {
    (void)arg;
    uint32_t retry_ms = GATEWAY_RETRY_INITIAL_MS;
    bool needs_gateway_restart = !ml307_transport_is_ready();
    while (!s_setup_portal_active && s_fangtang_use_cellular) {
        if (!ml307_transport_is_ready()) {
            needs_gateway_restart = true;
            board_port_set_wifi_status("4G", false);
            board_port_set_service_ready(false);
            firmware_identity_set_service_ready(false);
            esp_err_t modem_err = ml307_transport_start(
                CONFIG_MACLAW_FANGTANG_MODEM_UART_TX_GPIO,
                CONFIG_MACLAW_FANGTANG_MODEM_UART_RX_GPIO,
                CONFIG_MACLAW_FANGTANG_MODEM_UART_BAUD,
                CELLULAR_CONNECT_TIMEOUT_MS,
                CONFIG_MACLAW_FANGTANG_MODEM_APN);
            if (modem_err != ESP_OK) {
                ESP_LOGW(TAG, "ML307 recovery failed: %s; retry in %lu ms",
                         esp_err_to_name(modem_err), (unsigned long)retry_ms);
                vTaskDelay(pdMS_TO_TICKS(retry_ms));
                if (retry_ms < GATEWAY_RETRY_MAX_MS) {
                    retry_ms *= 2;
                    if (retry_ms > GATEWAY_RETRY_MAX_MS) retry_ms = GATEWAY_RETRY_MAX_MS;
                }
                continue;
            }
            board_port_set_wifi_status("4G", true);
            ESP_LOGI(TAG, "ML307 network recovered");
        }

        retry_ms = GATEWAY_RETRY_INITIAL_MS;
        if (needs_gateway_restart && !s_gateway_startup_running &&
            (s_gateway_token[0] || s_pair_code[0])) {
            ESP_LOGI(TAG, "restarting gateway startup after ML307 recovery");
            if (start_gateway_startup_task()) needs_gateway_restart = false;
        }
        vTaskDelay(pdMS_TO_TICKS(3000));
    }
    s_cellular_recovery_task = NULL;
    vTaskDelete(NULL);
}

static bool ensure_cellular_recovery_task(void) {
    if (s_cellular_recovery_task) return true;
    BaseType_t created = xTaskCreatePinnedToCore(
        cellular_recovery_task, "maclaw_cellular_recovery", 6144,
        NULL, 3, &s_cellular_recovery_task, 1);
    if (created != pdPASS) {
        s_cellular_recovery_task = NULL;
        ESP_LOGE(TAG, "cannot start ML307 recovery task");
        return false;
    }
    return true;
}

static bool start_cellular(void) {
    if (CONFIG_MACLAW_FANGTANG_MODEM_UART_TX_GPIO < 0 ||
        CONFIG_MACLAW_FANGTANG_MODEM_UART_RX_GPIO < 0) {
        ESP_LOGE(TAG, "4G selected, but Fangtang modem UART GPIOs are not configured");
        board_port_show_text("4G 未配置", "请先确认模块 UART 引脚");
        return false;
    }
    if (CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO >= 0) {
        gpio_config_t guard = {
            .pin_bit_mask = 1ULL << CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO,
            .mode = GPIO_MODE_OUTPUT,
            .pull_down_en = GPIO_PULLDOWN_ENABLE,
        };
        ESP_ERROR_CHECK(gpio_config(&guard));
        ESP_ERROR_CHECK(gpio_set_level(CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO,
                                       CONFIG_MACLAW_FANGTANG_MODEM_GUARD_LEVEL));
    }
    if (CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO >= 0) {
        gpio_config_t power = {
            .pin_bit_mask = 1ULL << CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO,
            .mode = GPIO_MODE_OUTPUT,
        };
        ESP_ERROR_CHECK(gpio_config(&power));
        ESP_ERROR_CHECK(gpio_set_level(CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO,
                                       CONFIG_MACLAW_FANGTANG_MODEM_POWER_ACTIVE_LEVEL));
        vTaskDelay(pdMS_TO_TICKS(500));
    }
    // ML307 is controlled through its native AT HTTP/HTTPS/TCP stack. It does
    // not implement the generic ATD*99# PPP path used by esp_modem.
    board_port_set_wifi_status("4G", false);
    board_port_set_service_ready(false);
    firmware_identity_set_service_ready(false);
    esp_err_t modem_err = ml307_transport_start(
        CONFIG_MACLAW_FANGTANG_MODEM_UART_TX_GPIO,
        CONFIG_MACLAW_FANGTANG_MODEM_UART_RX_GPIO,
        CONFIG_MACLAW_FANGTANG_MODEM_UART_BAUD,
        CELLULAR_CONNECT_TIMEOUT_MS,
        CONFIG_MACLAW_FANGTANG_MODEM_APN);
    if (modem_err != ESP_OK) {
        ESP_LOGE(TAG, "ML307 network start failed on UART%d GPIO%d/GPIO%d: %s",
                 CONFIG_MACLAW_FANGTANG_MODEM_UART_PORT,
                 CONFIG_MACLAW_FANGTANG_MODEM_UART_TX_GPIO,
                 CONFIG_MACLAW_FANGTANG_MODEM_UART_RX_GPIO,
                 esp_err_to_name(modem_err));
        board_port_show_text("4G 模块未响应", "检查 SIM、供电与天线");
        (void)ensure_cellular_recovery_task();
        return false;
    }
    board_port_set_wifi_status("4G", true);
    ESP_LOGI(TAG, "ML307 native network ready");
    (void)ensure_cellular_recovery_task();
    return true;
}
#endif

static bool start_wifi(void) {
    init_network();
    ensure_station_netif();
    s_station_auto_connect = true;
    s_station_expected_disconnect = false;
    bool enterprise = is_enterprise_wifi();
    wifi_config_t config = { .sta = { .threshold.authmode = enterprise ? WIFI_AUTH_WPA2_ENTERPRISE : WIFI_AUTH_WPA2_PSK } };
    strlcpy((char *)config.sta.ssid, s_wifi_ssid, sizeof(config.sta.ssid));
    if (!enterprise) strlcpy((char *)config.sta.password, s_wifi_password, sizeof(config.sta.password));
    ESP_ERROR_CHECK(esp_wifi_set_mode(s_setup_server ? WIFI_MODE_APSTA : WIFI_MODE_STA));
    // The connected router's 802.11n management traffic triggers a double
    // exception in this ESP-IDF 6.0.2 S3 Wi-Fi binary immediately after DHCP.
    // EchoEar needs reliability rather than throughput, so use legacy b/g
    // station negotiation until that driver path is upgraded.
    ESP_ERROR_CHECK(esp_wifi_set_protocol(WIFI_IF_STA,
                                          WIFI_PROTOCOL_11B | WIFI_PROTOCOL_11G));
    ESP_ERROR_CHECK(esp_wifi_set_config(WIFI_IF_STA, &config));
    // ESP-IDF 6.0.2's modem-sleep path can tear down the PHY's esp_timer while
    // its ISR is still armed on this S3 build. The resulting task-notify ISR
    // assertion occurs just after association and reboots the device. EchoEar
    // is USB-powered during normal use, so keep station power-save disabled.
    esp_err_t ps_err = esp_wifi_set_ps(WIFI_PS_NONE);
    if (ps_err != ESP_OK) {
        ESP_LOGW(TAG, "cannot disable Wi-Fi power save: %s", esp_err_to_name(ps_err));
    }
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
        s_wifi_enterprise_enabled = true;
    } else {
        // Enterprise state can only exist after a prior runtime enterprise
        // connection. Do not call this API on a cold personal-Wi-Fi boot:
        // ESP-IDF 6.0.2 can assert from the scan timer in that case.
        if (s_wifi_enterprise_enabled) {
            esp_err_t eap_err = esp_wifi_sta_enterprise_disable();
            if (eap_err != ESP_OK) {
                ESP_LOGW(TAG, "cannot disable prior enterprise Wi-Fi state: %s",
                         esp_err_to_name(eap_err));
            }
            s_wifi_enterprise_enabled = false;
        }
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
            esp_err_t err = paired ? gateway_handshake(true) : pair_by_code();
            if (err == ESP_OK) {
                if (!paired) {
                    paired = true;
                    attempt = 0;
                    retry_ms = GATEWAY_RETRY_INITIAL_MS;
                    continue;
                }
                (void)start_gateway_ready_tasks();
                apply_deferred_startup_pet_asset();
                break;
            }
            if (err == ESP_ERR_INVALID_STATE) {
                pet("alert");
                board_port_show_text(paired ? "令牌认证失败" : "配对码已失效",
                                     "请检查或重新配对");
                start_setup_portal(true);
                break;
            }
            // Preserve the boot surface while the Hub or network is temporarily
            // unavailable. Pet/standby is published only after Welcome + wake.
            app_ui_show_startup_screen();
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
            esp_err_t err = gateway_handshake(true);
            if (err == ESP_OK) {
                (void)start_gateway_ready_tasks();
                apply_deferred_startup_pet_asset();
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
            // Keep the board-specific boot surface visible during retry. The actual
            // failure cause is logged with a heap/network snapshot for diagnosis.
            app_ui_show_startup_screen();
            ESP_LOGW(TAG, "gateway handshake attempt %u failed: %s; retry in %lu ms",
                     attempt, esp_err_to_name(err), (unsigned long)retry_ms);
            vTaskDelay(pdMS_TO_TICKS(retry_ms));
            if (retry_ms < GATEWAY_RETRY_MAX_MS) {
                retry_ms *= 2;
                if (retry_ms > GATEWAY_RETRY_MAX_MS) retry_ms = GATEWAY_RETRY_MAX_MS;
            }
        }
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_gateway_startup_running = false;
    s_gateway_task = NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    vTaskDelete(NULL);
}

static bool start_gateway_startup_task(void) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_gateway_startup_running) {
        taskEXIT_CRITICAL(&s_task_state_lock);
        return true;
    }
    s_gateway_startup_running = true;
    taskEXIT_CRITICAL(&s_task_state_lock);

    BaseType_t created = xTaskCreatePinnedToCore(gateway_startup_task,
                                                "maclaw_gateway_startup",
                                                12288, NULL, 4,
                                                &task, 1);
    if (created != pdPASS) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_gateway_startup_running = false;
        s_gateway_task = NULL;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "cannot start gateway startup task");
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    // A very fast terminal path can finish before task creation returns. Do
    // not resurrect its stale FreeRTOS handle after it has cleared the running
    // flag in gateway_startup_task().
    if (s_gateway_startup_running) s_gateway_task = task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return true;
}

void app_main(void) {
    ESP_LOGW(TAG, "boot reset reason=%d", (int)esp_reset_reason());
    ESP_ERROR_CHECK(firmware_identity_start());
    esp_err_t nvs_err = nvs_flash_init();
    if (nvs_err == ESP_ERR_NVS_NO_FREE_PAGES || nvs_err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_ERROR_CHECK(nvs_flash_erase());
        nvs_err = nvs_flash_init();
    }
    ESP_ERROR_CHECK(nvs_err);
	load_device_id();
    uint8_t boot_random[16];
    esp_fill_random(boot_random, sizeof(boot_random));
    for (size_t i = 0; i < sizeof(boot_random); ++i) {
        snprintf(s_boot_session_id + i * 2, 3, "%02x", boot_random[i]);
    }
    ESP_ERROR_CHECK(psa_crypto_init() == PSA_SUCCESS ? ESP_OK : ESP_FAIL);
    (void)mount_meeting_storage();
    load_meeting_recovery();
    s_http_mutex = xSemaphoreCreateMutex();
    ESP_ERROR_CHECK(s_http_mutex ? ESP_OK : ESP_ERR_NO_MEM);
    s_gateway_poll_http_mutex = xSemaphoreCreateMutex();
    ESP_ERROR_CHECK(s_gateway_poll_http_mutex ? ESP_OK : ESP_ERR_NO_MEM);
    s_gateway_asset_http_mutex = xSemaphoreCreateMutex();
    ESP_ERROR_CHECK(s_gateway_asset_http_mutex ? ESP_OK : ESP_ERR_NO_MEM);
    s_foreground_http_client_mutex = xSemaphoreCreateMutex();
    ESP_ERROR_CHECK(s_foreground_http_client_mutex ? ESP_OK : ESP_ERR_NO_MEM);
    s_command_cancel_ui_ready = xSemaphoreCreateBinary();
    ESP_ERROR_CHECK(s_command_cancel_ui_ready ? ESP_OK : ESP_ERR_NO_MEM);
    s_startup_welcome_done = xSemaphoreCreateBinary();
    ESP_ERROR_CHECK(s_startup_welcome_done ? ESP_OK : ESP_ERR_NO_MEM);
    ESP_ERROR_CHECK(xTaskCreate(command_cancel_worker, "maclaw_cancel", 4096, NULL, 6,
                                &s_command_cancel_task) == pdPASS
                        ? ESP_OK : ESP_ERR_NO_MEM);
    s_nvs_mutex = xSemaphoreCreateMutex();
    ESP_ERROR_CHECK(s_nvs_mutex ? ESP_OK : ESP_ERR_NO_MEM);
    s_setup_options_mutex = xSemaphoreCreateMutex();
    ESP_ERROR_CHECK(s_setup_options_mutex ? ESP_OK : ESP_ERR_NO_MEM);
    // A foreground interaction starts in the button callback but finishes in
    // its worker task, therefore mutual exclusion must use a binary semaphore
    // rather than an ownership-tracked mutex.
    s_interaction_lock = xSemaphoreCreateBinary();
    ESP_ERROR_CHECK(s_interaction_lock ? ESP_OK : ESP_ERR_NO_MEM);
    ESP_ERROR_CHECK(xSemaphoreGive(s_interaction_lock) == pdTRUE ? ESP_OK : ESP_FAIL);
    load_device_config();
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    load_fangtang_network_choice();
    board_port_set_network_transport(s_fangtang_use_cellular);
#endif
    load_gateway_token();
    load_ambient_weather();
    app_ui_init();
    ESP_ERROR_CHECK(board_port_init(on_user_input, NULL));

#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    // GPIO0 double-click is only a startup gesture. It toggles the persisted
    // Wi-Fi/ML307 selection, matching the original 无名星智 firmware; normal
    // button gestures resume as soon as this bounded window ends.
    bool toggle_network = board_port_wait_for_boot_network_toggle(
        FANGTANG_BOOT_NETWORK_WINDOW_MS);
    if (toggle_network) {
        save_fangtang_network_choice(!s_fangtang_use_cellular);
        board_port_set_network_transport(s_fangtang_use_cellular);
        ESP_LOGI(TAG, "Fangtang startup toggle: %s selected",
                 s_fangtang_use_cellular ? "4G" : "Wi-Fi");
    }
    // Resolve the Hub endpoint only after the bounded startup gesture has
    // selected the final transport. Applying this before the gesture made a
    // Wi-Fi->4G toggle retain ECDSA HTTPS, while 4G->Wi-Fi retained port 9399.
    apply_fangtang_cellular_gateway_compatibility();
#endif
	// board initialization may briefly show and then clear its ROM/embedded
	// artwork. Re-present it as an explicit foreground UI surface so ambient
	// clock/profile updates cannot replace it while Welcome is being fetched and
	// played. The ready transition releases this surface after wake-word setup.
	app_ui_show_startup_screen();
    // Keep optional background work quiescent until esp_wifi_start() has
    // completed.  Both cached-pet installation (which may create its animation
    // task) and the alarm manager create work that can run while the Wi-Fi ROM
    // is enabling TSF.  On Bread Compact that startup overlap can corrupt the
    // Wi-Fi task's first callback and jump to PC 0x1 (InstrFetchProhibited).
    // The LCD mutex already exists here; only the timing is intentionally
    // deferred.
    firmware_identity_set_local_ready(true);
    nvs_handle_t setup_nvs;
    uint8_t force_setup = 0;
    if (nvs_open("maclaw", NVS_READWRITE, &setup_nvs) == ESP_OK) {
        (void)nvs_get_u8(setup_nvs, "force_setup", &force_setup);
        if (force_setup) {
            (void)nvs_erase_key(setup_nvs, "force_setup");
            (void)nvs_commit(setup_nvs);
        }
        nvs_close(setup_nvs);
    }
    if (force_setup) {
        ESP_LOGW(TAG, "booting directly into requested configuration portal");
        start_setup_portal(false);
        return;
    }
    // Keep the explicit board-specific startup surface until the Welcome/wake-word
    // sequence publishes ready. Do not transition to standby here.
    if (!s_wifi_ssid[0]
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        && !s_fangtang_use_cellular
#endif
    ) {
        start_setup_portal(false);
        return;
    }
    // A configured device runs as a normal Wi-Fi station. Being out of range
    // is an offline runtime condition, not evidence that provisioning was
    // lost. Keep both Wi-Fi credentials and the paired gateway token in NVS,
    // leave the normal pet/status surface visible, and let the Wi-Fi event
    // handler reconnect automatically when this SSID is reachable again.
    // Setup is entered only when no SSID was ever saved or after the user's
    // deliberate long-press reset.
    bool network_ready =
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        s_fangtang_use_cellular ? start_cellular() :
#endif
        start_wifi();
    // Wi-Fi boards have an independent wall-clock source and must not depend
    // on a successful Hub handshake before the standby clock or persisted
    // alarms can advance.  Start SNTP only after esp_wifi_start() has returned,
    // preserving the startup stability boundary above.  ML307 has no
    // ESP-NETIF route, so Fangtang 4G continues to use authenticated
    // handshake serverTime in apply_gateway_server_time().
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    if (!s_fangtang_use_cellular) start_clock_sync();
#else
    start_clock_sync();
#endif
    // Alarm storage and its scheduler must exist before the Hub can dispatch
    // any advertised alarm_* client tool.  Keep initialization after radio
    // startup: on Bread Compact, creating the alarm task while esp_wifi_start()
    // enables TSF has previously made that first Wi-Fi callback unstable.  Do
    // it even when the current connection attempt timed out, so persisted
    // alarms remain local/offline functionality while station or ML307
    // recovery continues in the background.
    ESP_ERROR_CHECK(alarm_manager_init());
    // From this point onward a late Wi-Fi DHCP event may safely start the Hub
    // transaction.  This is deliberately after alarm initialization: starting
    // TLS from IP_EVENT_STA_GOT_IP during esp_wifi_start() recreated the same
    // startup overlap that the ordering above is designed to prevent.
    taskENTER_CRITICAL(&s_task_state_lock);
    s_gateway_startup_allowed = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    if (!network_ready && !s_fangtang_use_cellular && s_wifi_events) {
#else
    if (!network_ready && s_wifi_events) {
#endif
        network_ready = (xEventGroupGetBits(s_wifi_events) & WIFI_CONNECTED_BIT) != 0;
        if (network_ready) {
            ESP_LOGI(TAG, "Wi-Fi recovered at startup boundary; continuing gateway startup");
        }
    }
    if (!network_ready) {
        pet("alert");
        ESP_LOGW(TAG, "saved Wi-Fi is currently unavailable; preserving configuration and retrying in station mode");
        board_port_show_text("网络暂时不可用", "配置已保留，正在自动重连");
        return;
    }
    // Do not allocate the ESP-SR model while the first TLS pairing/handshake
    // is being established. Both are PSRAM-heavy; starting them concurrently
    // can make mbedtls_ssl_setup() fail with PSA_ERROR_INSUFFICIENT_MEMORY
    // (-0x008D). start_gateway_ready_tasks() starts the listener immediately
    // after the authenticated handshake has released its TLS allocations.
    // Run TLS/HTTP work on core 1. Performing it in the framework main task on
    // core 0 starves that core's interrupt watchdog during TLS initialization.
    if (!start_gateway_startup_task()) {
        pet("alert");
        board_port_show_text("设备启动失败", "无法启动网关任务");
    }
}
