#include <stdio.h>
#include <inttypes.h>
#include <errno.h>
#include <stddef.h>
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
#include "esp_attr.h"

#if CONFIG_MACLAW_BATTERY_ADC_ENABLE
#include "esp_adc/adc_oneshot.h"
#endif

#include "board_port.h"
#include "audio_common.h"

#define WIFI_CONNECTED_BIT BIT0
#define WIFI_CONNECT_TIMEOUT_MS 20000
#define RESPONSE_CAPACITY 16384
#define HANDSHAKE_RESPONSE_CAPACITY 24576
#define SERVER_AUDIO_MAX_BYTES (256U * 1024U)
#define PET_ASSET_MAX_FRAMES 2
#define PET_ASSET_MAX_FRAME_BYTES (128U * 128U * 2U)
#define PET_ASSET_CACHE_PATH "/storage/pet_asset.bin"
#define PET_ASSET_CACHE_TEMP_PATH "/storage/pet_asset.tmp"
#define PET_ASSET_CACHE_BACKUP_PATH "/storage/pet_asset.bak"
#define PET_ASSET_CACHE_MAGIC 0x50414348u
#define PET_ASSET_CACHE_VERSION 1u
#define PET_ASSET_REVISION_CAPACITY 24
#define URL_CAPACITY 256
#define WIFI_VALUE_CAPACITY 65
#define WIFI_SSID_MAX_LEN 32
#define WIFI_ENTERPRISE_VALUE_CAPACITY 128
#define WIFI_EAP_MODE_CAPACITY 12
#define PAIR_CODE_CAPACITY 7
#define GATEWAY_RETRY_INITIAL_MS 2000
#define GATEWAY_RETRY_MAX_MS 60000
#define MEETING_RESUME_RETRY_INITIAL_MS 5000
#define MEETING_RESUME_RETRY_MAX_MS 300000
#define SETUP_AP_IP_ADDR "192.168.4.1"
#define DNS_PORT 53
#define DNS_PACKET_CAPACITY 512
#define DHCPS_OFFER_DNS 0x02
#define DYNAMIC_GLYPH_BYTES 72
#define DYNAMIC_GLYPH_MAX_PER_MESSAGE 96
#define MEETING_WAV_PATH "/storage/meeting.wav"
// MEETING_SAMPLE_RATE comes from audio_common.h (single source of truth).
#define MEETING_DEFAULT_CHUNK_SIZE (1U << 20)
#define MEETING_MIN_CHUNK_SIZE (64U << 10)
#define MEETING_MAX_CHUNK_SIZE (8U << 20)
#define MEETING_IO_BUFFER_SIZE 16384
#define MEETING_RESPONSE_CAPACITY 2048
#define MEETING_INTERNAL_TLS_RESERVE (16U * 1024U)
#define MEETING_BASE_PATH_CAPACITY 96
#define MEETING_RECORDING_ID_CAPACITY 96

static const char *TAG = "maclaw_client";
static EventGroupHandle_t s_wifi_events;
static int64_t s_cursor;
static char s_gateway_token[96];
static char s_boot_session_id[24];
static bool s_welcome_played_for_boot;
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
// Mirror of the motion flag last applied to the board layer. Hub messages
// may omit pet_motion_enabled; an absent field must not reset the current
// setting (see the poll message parser).
static bool s_pet_motion_enabled = true;
// Bounded retry state for transient server-audio failures, keyed by message
// id: a message whose media can never be fetched (for example an expired
// mediaToken that 401s forever) must not pin the poll queue head.
#define AUDIO_RETRY_LIMIT 5
static char s_audio_retry_id[96];  // message ids share the 96-byte convention
static unsigned s_audio_retry_count;
static httpd_handle_t s_setup_server;
static bool s_network_initialized;
static bool s_ap_netif_created;
static bool s_sta_netif_created;
static bool s_wifi_started;
static bool s_setup_portal_active;
// Reconnect backoff state for WIFI_EVENT_STA_DISCONNECTED. The event handler
// must not block, so the delay is implemented by a one-shot esp_timer whose
// callback performs esp_wifi_connect() outside the event loop.
static esp_timer_handle_t s_wifi_reconnect_timer;
static unsigned s_wifi_disconnect_retry;
// Separate streak for four-way-handshake timeouts: many APs answer a wrong
// PSK with reason 204 instead of 202, so only a run of *this* reason may hint
// "wrong password" at the user. Mixing it into the generic disconnect counter
// would fire the hint after any five disconnects of mixed causes.
static unsigned s_wifi_handshake_timeout_streak;
static esp_netif_t *s_setup_ap_netif;
static TaskHandle_t s_dns_task;
static TaskHandle_t s_gateway_task;
static TaskHandle_t s_interaction_task;
static TaskHandle_t s_meeting_task;
static TaskHandle_t s_meeting_resume_supervisor_task;
static TaskHandle_t s_wake_restart_task;
static TaskHandle_t s_meeting_capability_refresh_task;
static bool s_meeting_task_running;
static bool s_pairing_recovery_portal;
static TaskHandle_t s_ambient_task;
static TaskHandle_t s_gateway_poll_task;
static TaskHandle_t s_setup_restart_task;
static TaskHandle_t s_setup_portal_watchdog_task;
// Last user activity seen by the captive portal (start + form submits). The
// watchdog below reboots a configured device out of an abandoned portal.
static volatile int64_t s_setup_portal_activity_us;
// Counts watchdog-forced restarts across esp_restart() (RTC memory) so a
// broken saved configuration cannot reboot-loop the portal forever.
// Cleared once the station link comes up again.
static RTC_DATA_ATTR unsigned s_setup_portal_auto_restarts;
#define SETUP_PORTAL_IDLE_TIMEOUT_US (5LL * 60LL * 1000000LL)
static volatile bool s_command_display_locked;
static volatile bool s_command_cancel_requested;
static volatile bool s_command_cancel_enabled;
static bool s_command_cancel_ui_shown;
#define CANCELLED_REPLY_SLOTS 4
#define COMMAND_REPLY_ID_CAPACITY 96
static char s_active_command_reply_to[COMMAND_REPLY_ID_CAPACITY];
static char s_cancelled_command_reply_to[CANCELLED_REPLY_SLOTS][COMMAND_REPLY_ID_CAPACITY];
static unsigned s_cancelled_command_reply_next;
// Completed-command replyTo slots with completion timestamps. The server
// attaches a voice message to every reply and delivers it after the text, by
// which time the interaction task has already finished and the response page
// may still hold the display; these slots keep such trailing audio matched
// for a bounded window instead of being retried and dropped.
#define COMPLETED_REPLY_SLOTS 4
#define COMPLETED_REPLY_WINDOW_US (60LL * 1000000LL)
static char s_completed_command_reply_to[COMPLETED_REPLY_SLOTS][COMMAND_REPLY_ID_CAPACITY];
static int64_t s_completed_command_reply_at_us[COMPLETED_REPLY_SLOTS];
static unsigned s_completed_command_reply_next;
static int64_t s_ignore_command_input_until_us;
static uint32_t s_interaction_generation;
static uint32_t s_cancel_requested_generation;
static uint32_t s_cancel_ui_ready_generation;
static SemaphoreHandle_t s_http_mutex;
// Protects the foreground client pointer through cancel/cleanup. The general
// HTTP mutex cannot serve this purpose because it remains owned for the whole
// request and cancellation must run concurrently with esp_http_client_perform.
static SemaphoreHandle_t s_foreground_http_client_mutex;
static esp_http_client_handle_t s_foreground_http_client;
static SemaphoreHandle_t s_command_cancel_ui_ready;
static TaskHandle_t s_command_cancel_task;
static TaskHandle_t s_ui_request_task;
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
static SemaphoreHandle_t s_storage_mutex;
static char s_cached_pet_asset_revision[PET_ASSET_REVISION_CAPACITY];
static char s_desired_pet_asset_revision[PET_ASSET_REVISION_CAPACITY];
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

static void wifi_event(void *arg, esp_event_base_t base, int32_t id, void *data);

typedef struct {
    char *data;
    size_t capacity;
    size_t len;
    int status;
    bool truncated;
} http_response_t;

typedef enum {
    HTTP_FAILURE_NONE = 0,
    HTTP_FAILURE_AUTH,
    HTTP_FAILURE_TRANSIENT,
    HTTP_FAILURE_PERMANENT,
} http_failure_kind_t;

static bool gateway_auth_failed(const http_response_t *response, esp_err_t err);
static http_failure_kind_t classify_http_failure(int status, esp_err_t err);
static void save_ambient_weather(void);
static void load_ambient_weather(void);
static esp_err_t poll_reply(void);
static esp_err_t send_text_event(const char *text);
static bool json_number(cJSON *root, const char *key, int *value);
static int apply_glyphs_json(cJSON *glyphs);
static bool start_meeting_task(bool resume_only);
static esp_err_t gateway_handshake(void);
static void start_setup_portal(bool keep_station);
static void schedule_wake_restart(void);
static esp_err_t apply_pet_asset_json(cJSON *asset);
static esp_err_t load_cached_pet_asset(void);
static bool nvs_lock(void);
static void nvs_unlock(void);

typedef struct __attribute__((packed)) {
	uint32_t magic;
	uint16_t version;
	uint16_t width;
	uint16_t height;
	uint8_t frame_count;
	uint8_t reserved;
	uint32_t frame_interval_ms;
	uint32_t payload_bytes;
	char revision[PET_ASSET_REVISION_CAPACITY];
	uint8_t sha256[32];
} pet_asset_cache_header_t;

static esp_err_t pet_asset_sha256(const pet_asset_cache_header_t *header,
								 const uint8_t *frames, size_t size, uint8_t output[32]) {
	if (!header || !frames || !size || !output) return ESP_ERR_INVALID_ARG;
	psa_hash_operation_t operation = PSA_HASH_OPERATION_INIT;
	psa_status_t status = psa_hash_setup(&operation, PSA_ALG_SHA_256);
	if (status == PSA_SUCCESS) {
		status = psa_hash_update(&operation, (const uint8_t *)header,
			offsetof(pet_asset_cache_header_t, sha256));
	}
	if (status == PSA_SUCCESS) status = psa_hash_update(&operation, frames, size);
	size_t output_length = 0;
	if (status == PSA_SUCCESS) status = psa_hash_finish(&operation, output, 32, &output_length);
	if (status != PSA_SUCCESS) (void)psa_hash_abort(&operation);
	return status == PSA_SUCCESS && output_length == 32 ? ESP_OK : ESP_FAIL;
}

static esp_err_t persist_pet_asset(const uint8_t *frames, size_t frame_count,
									uint16_t width, uint16_t height,
									uint32_t frame_interval_ms, const char *revision) {
	if (!s_storage_mounted || !s_storage_mutex || !frames || !revision || !revision[0]) {
		return ESP_ERR_INVALID_STATE;
	}
	size_t payload_bytes = (size_t)width * height * 2 * frame_count;
	if (payload_bytes == 0 || payload_bytes > PET_ASSET_MAX_FRAME_BYTES * PET_ASSET_MAX_FRAMES) {
		return ESP_ERR_INVALID_SIZE;
	}
	pet_asset_cache_header_t header = {
		.magic = PET_ASSET_CACHE_MAGIC,
		.version = PET_ASSET_CACHE_VERSION,
		.width = width,
		.height = height,
		.frame_count = (uint8_t)frame_count,
		.frame_interval_ms = frame_interval_ms,
		.payload_bytes = (uint32_t)payload_bytes,
	};
	strlcpy(header.revision, revision, sizeof(header.revision));
	esp_err_t result = pet_asset_sha256(&header, frames, payload_bytes, header.sha256);
	if (result != ESP_OK) return result;
	if (xSemaphoreTake(s_storage_mutex, pdMS_TO_TICKS(5000)) != pdTRUE) return ESP_ERR_TIMEOUT;
	FILE *file = fopen(PET_ASSET_CACHE_TEMP_PATH, "wb");
	if (!file) {
		result = ESP_FAIL;
	} else {
		bool written = fwrite(&header, 1, sizeof(header), file) == sizeof(header) &&
			fwrite(frames, 1, payload_bytes, file) == payload_bytes &&
			fflush(file) == 0;
		if (fclose(file) != 0) written = false;
		if (!written) {
			result = ESP_FAIL;
		} else {
			struct stat current_info;
			bool current_exists = stat(PET_ASSET_CACHE_PATH, &current_info) == 0;
			if (current_exists) {
				(void)unlink(PET_ASSET_CACHE_BACKUP_PATH);
				if (rename(PET_ASSET_CACHE_PATH, PET_ASSET_CACHE_BACKUP_PATH) != 0) {
					result = ESP_FAIL;
				}
			}
			if (result == ESP_OK && rename(PET_ASSET_CACHE_TEMP_PATH, PET_ASSET_CACHE_PATH) != 0) {
				result = ESP_FAIL;
				if (current_exists) {
					(void)rename(PET_ASSET_CACHE_BACKUP_PATH, PET_ASSET_CACHE_PATH);
				}
			}
			if (result == ESP_OK) (void)unlink(PET_ASSET_CACHE_BACKUP_PATH);
		}
	}
	if (result == ESP_OK) {
		strlcpy(s_cached_pet_asset_revision, revision, sizeof(s_cached_pet_asset_revision));
	} else {
		(void)unlink(PET_ASSET_CACHE_TEMP_PATH);
	}
	xSemaphoreGive(s_storage_mutex);
	return result;
}

static bool pet_asset_cache_has_revision(const char *revision) {
	if (!revision || !revision[0] || !s_storage_mutex) return false;
	if (xSemaphoreTake(s_storage_mutex, pdMS_TO_TICKS(1000)) != pdTRUE) return false;
	bool matches = !strcmp(s_cached_pet_asset_revision, revision);
	xSemaphoreGive(s_storage_mutex);
	return matches;
}

static void load_desired_pet_asset_revision(void) {
	s_desired_pet_asset_revision[0] = '\0';
	nvs_handle_t nvs;
	size_t length = sizeof(s_desired_pet_asset_revision);
	if (nvs_open("maclaw", NVS_READONLY, &nvs) == ESP_OK) {
		if (nvs_get_str(nvs, "pet_asset_rev", s_desired_pet_asset_revision, &length) != ESP_OK) {
			s_desired_pet_asset_revision[0] = '\0';
		}
		nvs_close(nvs);
	}
}

static esp_err_t save_desired_pet_asset_revision(const char *revision) {
	if (!revision || !revision[0] || strlen(revision) >= sizeof(s_desired_pet_asset_revision)) {
		return ESP_ERR_INVALID_SIZE;
	}
	if (!strcmp(s_desired_pet_asset_revision, revision)) return ESP_OK;
	if (!nvs_lock()) return ESP_ERR_TIMEOUT;
	nvs_handle_t nvs;
	esp_err_t result = nvs_open("maclaw", NVS_READWRITE, &nvs);
	if (result == ESP_OK) {
		result = nvs_set_str(nvs, "pet_asset_rev", revision);
		if (result == ESP_OK) result = nvs_commit(nvs);
		nvs_close(nvs);
	}
	nvs_unlock();
	if (result == ESP_OK) {
		strlcpy(s_desired_pet_asset_revision, revision,
			sizeof(s_desired_pet_asset_revision));
	}
	return result;
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
// Caller must already hold s_task_state_lock.
static void remember_cancelled_command_reply_locked(void) {
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
}

static void finish_interaction_task(uint32_t generation) {
    board_port_set_command_cancel_enabled(false);
    taskENTER_CRITICAL(&s_task_state_lock);
    bool owns_interaction = s_interaction_generation == generation &&
                            s_interaction_task == xTaskGetCurrentTaskHandle();
    if (owns_interaction) {
        // Move the finished command's replyTo into the completed slots before
        // clearing it: the trailing voice message of this reply arrives after
        // the text and must still match for a short window once this task is
        // gone. A command whose cancel request raced this finish (the user
        // double-tapped after the task passed its last cancellation
        // checkpoint) must instead land in the cancelled slots: the cancel
        // worker may already have observed the flag as cleared without ever
        // recording the replyTo, and a replyTo present in neither slot set
        // would make the late voice retry forever and pin the poll queue head.
        if (s_active_command_reply_to[0]) {
            if (s_command_cancel_requested) {
                remember_cancelled_command_reply_locked();
            } else {
                strlcpy(s_completed_command_reply_to[s_completed_command_reply_next],
                        s_active_command_reply_to, COMMAND_REPLY_ID_CAPACITY);
                s_completed_command_reply_at_us[s_completed_command_reply_next] =
                    esp_timer_get_time();
                s_completed_command_reply_next =
                    (s_completed_command_reply_next + 1) % COMPLETED_REPLY_SLOTS;
            }
        }
        s_interaction_task = NULL;
        s_foreground_http_requested = false;
        s_command_cancel_enabled = false;
        s_command_cancel_requested = false;
        s_cancel_requested_generation = 0;
        s_active_command_reply_to[0] = '\0';
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    // This is a binary admission token, not a mutex: the button task starts
    // the interaction task, which completes it on another task context.
    // Releasing a FreeRTOS mutex from that child task asserts and reboots.
    if (owns_interaction && s_interaction_lock) xSemaphoreGive(s_interaction_lock);
    // The interaction worker now uses ordinary xTaskCreate() with an internal
    // RAM stack, so it must be destroyed by the matching FreeRTOS API.
    // vTaskDeleteWithCaps() asserts when given a normally allocated task.
    schedule_wake_restart();
    vTaskDelete(NULL);
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
    remember_cancelled_command_reply_locked();
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

static bool completed_command_reply_matches(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return false;
    bool matches = false;
    int64_t now_us = esp_timer_get_time();
    taskENTER_CRITICAL(&s_task_state_lock);
    for (unsigned i = 0; i < COMPLETED_REPLY_SLOTS; ++i) {
        if (s_completed_command_reply_to[i][0] &&
            !strcmp(s_completed_command_reply_to[i], reply_to) &&
            now_us - s_completed_command_reply_at_us[i] < COMPLETED_REPLY_WINDOW_US) {
            matches = true;
            break;
        }
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    return matches;
}

static TaskHandle_t begin_active_command_reply(void) {
    // Atomically close the cancellation window, snapshot the waiter and
    // deliver its completion notification, all while holding the task state
    // lock. finish_interaction_task() clears s_interaction_task under the
    // same lock, so the snapshot cannot outlive the worker it points to.
    // Notifying after the critical section could target an already deleted
    // interaction task, which is undefined behavior.
    TaskHandle_t waiter = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (!s_command_cancel_requested) {
        s_command_cancel_enabled = false;
        waiter = s_interaction_task;
        if (waiter) xTaskNotifyGive(waiter);
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    board_port_set_command_cancel_enabled(false);
    return waiter;
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
        while ((xTaskGetTickCount() - started) < pdMS_TO_TICKS(7500)) {
            if (xSemaphoreTake(s_command_cancel_ui_ready, pdMS_TO_TICKS(50)) == pdTRUE) {
                taskENTER_CRITICAL(&s_task_state_lock);
                bool ready_for_this_command = s_cancel_ui_ready_generation == generation;
                taskEXIT_CRITICAL(&s_task_state_lock);
                if (ready_for_this_command) break;
            }
        }
    }
    if (command_cancel_requested_for(generation)) show_cancelled_command(generation);
    finish_interaction_task(generation);
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

        // Local cancellation stops waiting immediately, but the server-side
        // agent may already be executing after accepting the voice event. Send
        // the protocol's normal /cancel command before releasing the local
        // interaction token so it cannot accidentally target a newer command.
        if (s_gateway_token[0]) {
            esp_err_t server_cancel_err = send_text_event("/cancel");
            if (server_cancel_err != ESP_OK) {
                ESP_LOGW(TAG, "server command cancel failed: %s",
                         esp_err_to_name(server_cancel_err));
            } else {
                ESP_LOGI(TAG, "server command cancel accepted");
            }
        }

        taskENTER_CRITICAL(&s_task_state_lock);
        s_cancel_ui_ready_generation = cancel_generation;
        if (s_command_cancel_requested &&
            s_cancel_requested_generation == cancel_generation) {
            // Notify while still holding the lock: finish_interaction_task()
            // clears s_interaction_task under the same lock, so this handle
            // can never name a worker that has already deleted itself.
            TaskHandle_t waiter = s_interaction_task;
            if (waiter) xTaskNotifyGive(waiter);
        }
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_command_cancel_ui_ready) xSemaphoreGive(s_command_cancel_ui_ready);
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

    if (!s_http_mutex) return ESP_ERR_INVALID_STATE;
    TickType_t lock_started = xTaskGetTickCount();
    bool cancellation_request = current_task == s_command_cancel_task;
    const TickType_t lock_timeout = pdMS_TO_TICKS(cancellation_request ? 6000 : 35000);
    while (xSemaphoreTake(s_http_mutex, pdMS_TO_TICKS(100)) != pdTRUE) {
        if (foreground_request && command_cancel_requested_for(foreground_generation)) {
            ESP_LOGI(TAG, "foreground HTTP lock wait cancelled: %s %s", method, path);
            return ESP_ERR_INVALID_STATE;
        }
        if ((xTaskGetTickCount() - lock_started) >= lock_timeout) {
            ESP_LOGW(TAG, "HTTP request lock timeout: %s %s", method, path);
            if (foreground_request) {
                bool resume_active;
                taskENTER_CRITICAL(&s_task_state_lock);
                resume_active = s_meeting_task_running;
                taskEXIT_CRITICAL(&s_task_state_lock);
                if (resume_active) {
                    // A retained meeting upload owns the HTTP lock for whole
                    // chunks, so a voice command can starve behind it. Explain
                    // the wait instead of surfacing a bare timeout error.
                    board_port_show_text("正在续传录音", "请稍候重试");
                }
            }
            return ESP_ERR_TIMEOUT;
        }
    }
    if (foreground_request && command_cancel_requested_for(foreground_generation)) {
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    // Prefer PSRAM for every HTTP body. Request buffers must not consume the
    // small internal heap reserved for the TLS handshake and Wi-Fi stacks. The
    // internal fallback is limited to small buffers: a multi-KB internal
    // allocation would compete with that reserve and accelerate fragmentation.
    out->data = heap_caps_malloc(response_capacity, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!out->data && response_capacity <= 4096) {
        out->data = heap_caps_malloc(response_capacity, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    }
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
        .timeout_ms = cancellation_request ? 5000 : 30000,
        .crt_bundle_attach = esp_crt_bundle_attach,
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
    if (foreground_request) {
        xSemaphoreTake(s_foreground_http_client_mutex, portMAX_DELAY);
        s_foreground_http_client = client;
        xSemaphoreGive(s_foreground_http_client_mutex);
    }
    esp_http_client_method_t http_method = HTTP_METHOD_GET;
    if (!strcmp(method, "POST")) http_method = HTTP_METHOD_POST;
    else if (!strcmp(method, "PUT")) http_method = HTTP_METHOD_PUT;
    else if (strcmp(method, "GET") != 0) {
        esp_http_client_cleanup(client);
        free(out->data);
        out->data = NULL;
        out->capacity = 0;
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_INVALID_ARG;
    }
    esp_http_client_set_method(client, http_method);
    if (content_type) esp_http_client_set_header(client, "Content-Type", content_type);
    esp_http_client_set_header(client, "Accept", "application/json");
    // Do not set a manual "Connection: close" header: keep_alive_enable above
    // already defines the connection lifecycle, and the two directives
    // contradict each other.
    if (s_gateway_token[0]) {
        char authorization[128];
        snprintf(authorization, sizeof(authorization), "Bearer %s", s_gateway_token);
        esp_http_client_set_header(client, "Authorization", authorization);
    }
    if (body && body_len > 0) esp_http_client_set_post_field(client, body, body_len);
    esp_err_t err;
    if (foreground_request && command_cancel_requested_for(foreground_generation)) {
        err = ESP_ERR_INVALID_STATE;
    } else {
        err = esp_http_client_perform(client);
    }
    out->status = esp_http_client_get_status_code(client);
    if (foreground_request) {
        xSemaphoreTake(s_foreground_http_client_mutex, portMAX_DELAY);
        if (s_foreground_http_client == client) s_foreground_http_client = NULL;
        xSemaphoreGive(s_foreground_http_client_mutex);
    }
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

// Retry only transport errors and status codes that are explicitly transient.
// Replaying a permanent 4xx (for example an undeclared media capability) wastes
// time and can create needless upload objects. 401/403 are kept distinct so a
// revoked durable token can move the device into pairing recovery immediately.
static http_failure_kind_t classify_http_failure(int status, esp_err_t err) {
    if (status == 401 || status == 403) return HTTP_FAILURE_AUTH;
    if (status == 408 || status == 425 || status == 429 || status >= 500) {
        return HTTP_FAILURE_TRANSIENT;
    }
    if (status >= 400) return HTTP_FAILURE_PERMANENT;
    if (err != ESP_OK) return HTTP_FAILURE_TRANSIENT;
    return HTTP_FAILURE_NONE;
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

static esp_err_t apply_pet_asset_json(cJSON *asset) {
	if (!cJSON_IsObject(asset)) return ESP_ERR_NOT_FOUND;
	const char *encoding = json_string(asset, "encoding");
	const char *revision = json_string(asset, "revision");
	cJSON *urls = cJSON_GetObjectItemCaseSensitive(asset, "urls");
	int width = 0, height = 0, frame_ms = 700;
	if (!encoding || strcmp(encoding, "rgb565le") || !revision ||
	    !json_number(asset, "width", &width) || !json_number(asset, "height", &height) ||
	    width < 32 || width > 128 || height < 32 || height > 128 || !cJSON_IsArray(urls)) {
		return ESP_ERR_INVALID_ARG;
	}
	if (strlen(revision) >= PET_ASSET_REVISION_CAPACITY) return ESP_ERR_INVALID_SIZE;
	// Record the server-selected revision before downloading. On a reset during
	// a failed update, boot must reject an older cached character rather than
	// briefly displaying a pet that is no longer selected in the GUI.
	esp_err_t desired_err = save_desired_pet_asset_revision(revision);
	if (desired_err != ESP_OK) {
		ESP_LOGW(TAG, "cannot persist desired pet revision %s: %s",
			revision, esp_err_to_name(desired_err));
	}
	if (board_port_has_pet_asset_revision(revision) && pet_asset_cache_has_revision(revision)) {
		ESP_LOGD(TAG, "GUI pet asset already installed: revision=%s", revision);
		return ESP_OK;
	}
	if (!board_port_has_pet_asset_revision(revision)) {
		// The new profile has already been applied by the caller. Remove the old
		// remote bitmap now so any fetch failure visibly falls back to that new
		// skin's native renderer instead of retaining the previous GUI pet.
		(void)board_port_set_pet_asset(NULL, 0, 0, 0, 0, NULL);
	}
	(void)json_number(asset, "frameMs", &frame_ms);
	int frame_count = cJSON_GetArraySize(urls);
	if (frame_count < 1 || frame_count > PET_ASSET_MAX_FRAMES) return ESP_ERR_INVALID_SIZE;
	size_t frame_bytes = (size_t)width * height * 2;
	uint8_t *frames = heap_caps_malloc(frame_bytes * frame_count, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
	if (!frames) return ESP_ERR_NO_MEM;
	esp_err_t result = ESP_OK;
	for (int index = 0; index < frame_count; ++index) {
		cJSON *url_node = cJSON_GetArrayItem(urls, index);
		const char *url = cJSON_IsString(url_node) ? url_node->valuestring : NULL;
		if (!url || strncmp(url, "/api/im-gateway/v1/media/", 25) != 0) {
			result = ESP_ERR_INVALID_ARG;
			break;
		}
		http_response_t media = {0};
		result = request_with_capacity("GET", url, NULL, NULL, 0, frame_bytes + 1, &media);
		if (result != ESP_OK || media.status != 200 || media.len != frame_bytes) {
			ESP_LOGW(TAG, "pet asset frame %d failed: err=%s status=%d bytes=%u/%u",
					 index, esp_err_to_name(result), media.status,
					 (unsigned)media.len, (unsigned)frame_bytes);
			result = result == ESP_OK ? ESP_ERR_INVALID_SIZE : result;
			response_release(&media);
			break;
		}
		memcpy(frames + (size_t)index * frame_bytes, media.data, frame_bytes);
		response_release(&media);
	}
	if (result == ESP_OK) {
		result = board_port_set_pet_asset(frames, (size_t)frame_count, (uint16_t)width,
										  (uint16_t)height, (uint32_t)frame_ms, revision);
		if (result == ESP_OK) {
			// Never contend with a meeting file for SPIFFS bandwidth. The asset is
			// already live in PSRAM and the next handshake can retry persistence.
			bool meeting_busy;
			taskENTER_CRITICAL(&s_task_state_lock);
			meeting_busy = s_meeting_task_running;
			taskEXIT_CRITICAL(&s_task_state_lock);
			if (!meeting_busy) {
				esp_err_t cache_err = persist_pet_asset(frames, (size_t)frame_count,
					(uint16_t)width, (uint16_t)height, (uint32_t)frame_ms, revision);
				if (cache_err != ESP_OK) {
					// Rendering success is authoritative. A full/busy flash partition must
					// not discard a valid asset that is already installed in PSRAM.
					ESP_LOGW(TAG, "pet asset cache update skipped: %s", esp_err_to_name(cache_err));
				}
			} else {
				ESP_LOGI(TAG, "pet asset cache deferred while meeting storage is active");
			}
		}
	}
	free(frames);
	return result;
}

static esp_err_t load_cached_pet_asset(void) {
	if (!s_storage_mounted || !s_storage_mutex) return ESP_ERR_INVALID_STATE;
	if (xSemaphoreTake(s_storage_mutex, pdMS_TO_TICKS(5000)) != pdTRUE) return ESP_ERR_TIMEOUT;
	FILE *file = fopen(PET_ASSET_CACHE_PATH, "rb");
	if (!file) {
		int open_errno = errno;
		// A reset can happen after current -> backup but before temp -> current.
		// Recover the last complete generation before treating the cache as
		// absent; the backup is protected by the same header and SHA check below.
		if (open_errno == ENOENT &&
			rename(PET_ASSET_CACHE_BACKUP_PATH, PET_ASSET_CACHE_PATH) == 0) {
			ESP_LOGW(TAG, "recovering GUI pet asset from interrupted cache update");
			file = fopen(PET_ASSET_CACHE_PATH, "rb");
		}
		if (!file) {
			xSemaphoreGive(s_storage_mutex);
			return open_errno == ENOENT ? ESP_ERR_NOT_FOUND : ESP_FAIL;
		}
	}
	pet_asset_cache_header_t header = {0};
	bool valid = fread(&header, 1, sizeof(header), file) == sizeof(header) &&
		header.magic == PET_ASSET_CACHE_MAGIC && header.version == PET_ASSET_CACHE_VERSION &&
		header.width >= 32 && header.width <= 128 && header.height >= 32 && header.height <= 128 &&
		header.frame_count >= 1 && header.frame_count <= PET_ASSET_MAX_FRAMES &&
		header.revision[0] && memchr(header.revision, '\0', sizeof(header.revision)) &&
		header.payload_bytes == (uint32_t)((size_t)header.width * header.height * 2 * header.frame_count) &&
		header.payload_bytes <= PET_ASSET_MAX_FRAME_BYTES * PET_ASSET_MAX_FRAMES;
	if (valid && (!s_desired_pet_asset_revision[0] ||
		strcmp(header.revision, s_desired_pet_asset_revision) != 0)) {
		ESP_LOGI(TAG, "cached pet revision %s is stale; GUI expects %s",
			header.revision, s_desired_pet_asset_revision[0] ? s_desired_pet_asset_revision : "none");
		valid = false;
	}
	uint8_t *frames = NULL;
	bool allocation_failed = false;
	if (valid) {
		frames = heap_caps_malloc(header.payload_bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
		allocation_failed = frames == NULL;
		valid = !allocation_failed &&
			fread(frames, 1, header.payload_bytes, file) == header.payload_bytes && fgetc(file) == EOF;
	}
	if (fclose(file) != 0) valid = false;
	esp_err_t result = ESP_ERR_INVALID_CRC;
	if (valid) {
		uint8_t digest[32];
		valid = pet_asset_sha256(&header, frames, header.payload_bytes, digest) == ESP_OK &&
			memcmp(digest, header.sha256, sizeof(digest)) == 0;
	}
	if (valid) {
		result = board_port_set_pet_asset(frames, header.frame_count, header.width, header.height,
			header.frame_interval_ms, header.revision);
		if (result == ESP_OK) {
			strlcpy(s_cached_pet_asset_revision, header.revision,
				sizeof(s_cached_pet_asset_revision));
			ESP_LOGI(TAG, "cached GUI pet asset restored: %ux%u frames=%u revision=%s",
				header.width, header.height, (unsigned)header.frame_count, header.revision);
		}
	} else if (allocation_failed) {
		result = ESP_ERR_NO_MEM;
		ESP_LOGW(TAG, "cached GUI pet asset restore deferred: PSRAM unavailable");
	} else {
		ESP_LOGW(TAG, "cached GUI pet asset is invalid; removing it");
		(void)unlink(PET_ASSET_CACHE_PATH);
	}
	// A valid current generation makes any leftover backup obsolete. A corrupt
	// current is deliberately not replaced by an unverified backup here; the
	// next authenticated handshake will fetch the selected revision again.
	if (result == ESP_OK) (void)unlink(PET_ASSET_CACHE_BACKUP_PATH);
	(void)unlink(PET_ASSET_CACHE_TEMP_PATH);
	free(frames);
	xSemaphoreGive(s_storage_mutex);
	return result;
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

#if CONFIG_MACLAW_BATTERY_ADC_ENABLE
// Battery divider on ADC1, sampled once per minute from the ambient loop.
// Assumes a 1:1 divider (Vbat = 2 x Vadc) and a Li-ion cell: 3.3 V is empty,
// 4.2 V is full. Below 10% the user is warned once; below 3% the device shows
// a shutdown notice and enters deep sleep (no wake source: it stays off until
// manual reset or charger attach) so the cell is never deep-discharged.
static adc_oneshot_unit_handle_t s_battery_adc;
static adc_channel_t s_battery_channel;

static void battery_monitor_init(void) {
    adc_unit_t unit = ADC_UNIT_1;
    adc_channel_t channel;
    if (adc_oneshot_io_to_channel(CONFIG_MACLAW_BATTERY_ADC_GPIO, &unit, &channel) != ESP_OK ||
        unit != ADC_UNIT_1) {
        ESP_LOGE(TAG, "battery ADC: GPIO %d does not map to ADC1; monitoring disabled",
                 CONFIG_MACLAW_BATTERY_ADC_GPIO);
        return;
    }
    adc_oneshot_unit_init_cfg_t unit_cfg = { .unit_id = ADC_UNIT_1 };
    if (adc_oneshot_new_unit(&unit_cfg, &s_battery_adc) != ESP_OK) {
        s_battery_adc = NULL;
        ESP_LOGE(TAG, "battery ADC: unit init failed; monitoring disabled");
        return;
    }
    adc_oneshot_chan_cfg_t chan_cfg = { .atten = ADC_ATTEN_DB_12,
                                        .bitwidth = ADC_BITWIDTH_DEFAULT };
    if (adc_oneshot_config_channel(s_battery_adc, channel, &chan_cfg) != ESP_OK) {
        adc_oneshot_del_unit(s_battery_adc);
        s_battery_adc = NULL;
        ESP_LOGE(TAG, "battery ADC: channel config failed; monitoring disabled");
        return;
    }
    s_battery_channel = channel;
    ESP_LOGI(TAG, "battery ADC monitoring on GPIO %d", CONFIG_MACLAW_BATTERY_ADC_GPIO);
}

static void battery_monitor_poll(void) {
    if (!s_battery_adc) return;
    static unsigned s_battery_tick;
    if (++s_battery_tick % 60 != 0) return;  // 1 Hz caller -> once per minute
    int raw = 0;
    if (adc_oneshot_read(s_battery_adc, s_battery_channel, &raw) != ESP_OK) return;
    int mv_bat = raw * 3100 / 4095 * 2;  // 12 dB atten ~3.1 V full scale, 1:1 divider
    int pct = (mv_bat - 3300) * 100 / (4200 - 3300);
    if (pct < 0) pct = 0;
    if (pct > 100) pct = 100;
    static int s_last_pct = -1;
    if (pct != s_last_pct) {
        ESP_LOGI(TAG, "battery %d%% (%d mV)", pct, mv_bat);
        s_last_pct = pct;
    }
    static bool s_low_warned;
    static unsigned s_critical_streak;
    if (pct > 3) s_critical_streak = 0;
    if (pct <= 3) {
        // Demand three consecutive minute-scale samples before powering off:
        // a single noisy ADC read must never deep-sleep the device.
        if (++s_critical_streak < 3) {
            ESP_LOGW(TAG, "battery critical sample %u/3 (%d%%)", s_critical_streak, pct);
            return;
        }
        ESP_LOGE(TAG, "battery critical (%d%%); entering deep sleep", pct);
        pet("alert");
        board_port_show_text("电量耗尽", "设备即将关机");
        vTaskDelay(pdMS_TO_TICKS(2000));
        // The backlight LED draws from the same rail even in deep sleep.
        board_port_prepare_deep_sleep();
        esp_deep_sleep_start();
    } else if (pct <= 10 && !s_low_warned) {
        s_low_warned = true;
        board_port_show_text("电量不足", "请及时充电");
    } else if (pct > 15) {
        s_low_warned = false;
    }
}
#endif

#if CONFIG_MACLAW_HEAP_MONITOR
// Called from the 1 Hz ambient loop; self-throttles to one report per minute.
// The measurements feed two decisions: how much headroom the esp-sr AFE can
// rely on, and whether task stack sizes match reality (high-water marks).
static void log_memory_watermarks_once_per_minute(void) {
    static unsigned s_heap_monitor_tick;
    if (++s_heap_monitor_tick % 60 != 0) return;
    ESP_LOGI(TAG, "heap internal free=%u largest=%u | spiram free=%u largest=%u",
             (unsigned)heap_caps_get_free_size(MALLOC_CAP_INTERNAL),
             (unsigned)heap_caps_get_largest_free_block(MALLOC_CAP_INTERNAL),
             (unsigned)heap_caps_get_free_size(MALLOC_CAP_SPIRAM),
             (unsigned)heap_caps_get_largest_free_block(MALLOC_CAP_SPIRAM));
    static const struct {
        const char *name;
        TaskHandle_t *handle;
    } watched[] = {
        {"gateway", &s_gateway_task},
        {"poll", &s_gateway_poll_task},
        {"interaction", &s_interaction_task},
        {"meeting", &s_meeting_task},
        {"ambient", &s_ambient_task},
        {"cancel", &s_command_cancel_task},
    };
    for (size_t i = 0; i < sizeof(watched) / sizeof(watched[0]); ++i) {
        if (*watched[i].handle) {
            ESP_LOGI(TAG, "stack %-12s hwm=%u bytes free", watched[i].name,
                     (unsigned)(uxTaskGetStackHighWaterMark(*watched[i].handle) *
                                sizeof(StackType_t)));
        }
    }
}
#endif

static void ambient_task(void *arg) {
    (void)arg;
    while (true) {
        refresh_ambient_display();
#if CONFIG_MACLAW_HEAP_MONITOR
        log_memory_watermarks_once_per_minute();
#endif
#if CONFIG_MACLAW_BATTERY_ADC_ENABLE
        battery_monitor_poll();
#endif
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
    uint32_t retry_ms = GATEWAY_RETRY_INITIAL_MS;
    while (true) {
        if (s_gateway_token[0]) {
            int64_t started_us = esp_timer_get_time();
            esp_err_t err = poll_reply();
            if (err == ESP_ERR_INVALID_STATE) {
                // Same self-heal as gateway_startup_task: a 401/403 means the
                // stored token was revoked, so open the pairing recovery
                // portal (AP+STA, code only) instead of polling forever. The
                // portal owns the display and radio afterwards; this task is
                // finished. Clear the handle first so ensure_gateway_poll_task
                // could restart a poller after a successful re-pair.
                ESP_LOGW(TAG, "gateway poll: credential rejected; entering pairing recovery");
                taskENTER_CRITICAL(&s_task_state_lock);
                s_gateway_poll_task = NULL;
                taskEXIT_CRITICAL(&s_task_state_lock);
                pet("alert");
                board_port_show_text("令牌认证失败", "请检查或重新配对");
                start_setup_portal(true);
                vTaskDelete(NULL);
            }
            int64_t elapsed_ms = (esp_timer_get_time() - started_us) / 1000;
            if (err != ESP_OK) {
                ESP_LOGW(TAG, "gateway poll failed: %s; retry in %lu ms",
                         esp_err_to_name(err), (unsigned long)retry_ms);
                vTaskDelay(pdMS_TO_TICKS(retry_ms));
                if (retry_ms < GATEWAY_RETRY_MAX_MS) {
                    retry_ms *= 2;
                    if (retry_ms > GATEWAY_RETRY_MAX_MS) retry_ms = GATEWAY_RETRY_MAX_MS;
                }
            } else if (elapsed_ms < 4000) {
                // Legacy Hub versions return an empty poll immediately. Avoid
                // a tight TLS reconnect loop until that Hub is upgraded to
                // the v1.1 long-poll implementation.
                vTaskDelay(pdMS_TO_TICKS(2000));
                retry_ms = GATEWAY_RETRY_INITIAL_MS;
            } else {
                retry_ms = GATEWAY_RETRY_INITIAL_MS;
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

static void meeting_resume_supervisor_task(void *arg) {
    (void)arg;
    uint32_t retry_ms = MEETING_RESUME_RETRY_INITIAL_MS;
    while (s_meeting_pending) {
        EventBits_t wifi = s_wifi_events ? xEventGroupGetBits(s_wifi_events) : 0;
        if (!s_setup_portal_active && s_gateway_token[0] && (wifi & WIFI_CONNECTED_BIT) &&
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
    if (!ensure_gateway_poll_task()) {
        pet("alert");
        board_port_show_text("设备启动失败", "无法启动网关轮询");
        return false;
    }
    // Start the single Chinese MultiNet listener only after the authenticated
    // handshake has released its TLS allocations. This is what makes “码卡龙”
    // work from the time/weather standby screen.
    esp_err_t wake_err = board_port_start_wake_word(on_wake_word, NULL);
    if (wake_err != ESP_OK && wake_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "offline wake start failed: %s", esp_err_to_name(wake_err));
    }
    board_port_show_ready_prompt("设备已就绪", "点屏说话 双点会议");
    if (s_meeting_pending) {
        ESP_LOGI(TAG, "pending meeting upload found; scheduling resumable retries");
        (void)ensure_meeting_resume_supervisor();
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
    esp_err_t err = board_port_start_wake_word(on_wake_word, NULL);
    if (err != ESP_OK && err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "offline wake restart after meeting upload: %s",
                 esp_err_to_name(err));
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_wake_restart_task = NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    vTaskDeleteWithCaps(NULL);
}

static void schedule_wake_restart(void) {
    if (s_setup_portal_active) return;
    taskENTER_CRITICAL(&s_task_state_lock);
    bool already_scheduled = s_wake_restart_task != NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (already_scheduled) return;
    TaskHandle_t handle = NULL;
    BaseType_t created = xTaskCreateWithCaps(wake_restart_task, "maclaw_wake_restart",
                                             2048, NULL, 2, &handle,
                                             MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_wake_restart_task = created == pdPASS ? handle : NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (created != pdPASS) ESP_LOGE(TAG, "cannot schedule offline wake restart");
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
	if (host[0] == '\0' || host[0] == '/' || strchr(host, ' ') || strchr(host, '\\') ||
		strchr(host, '@') || strchr(host, '#') || strchr(host, '?')) return false;
	// Store only an origin. request() appends protocol paths; accepting a user
	// supplied path produced malformed URLs such as /base/api/im-gateway/....
	const char *path = strchr(host, '/');
	if (path && path[1] != '\0') return false;
	// Reject an empty port and control characters. IPv6 literals remain valid
	// because their colons are enclosed by brackets.
	const char *port = strrchr(host, ':');
	if (port && port[1] == '\0') return false;
	for (const unsigned char *p = (const unsigned char *)host; *p; ++p) {
		if (*p < 0x21 || *p == 0x7f) return false;
	}
	return true;
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

static void load_volume(void) {
    uint8_t volume = 70;
    nvs_handle_t nvs;
    if (nvs_open("maclaw", NVS_READONLY, &nvs) == ESP_OK) {
        uint8_t persisted = 0;
        if (nvs_get_u8(nvs, "volume_pct", &persisted) == ESP_OK && persisted <= 100) {
            volume = persisted;
        }
        nvs_close(nvs);
    }
    // board_port_init() has not initialised ES8311 yet. This stores the value
    // in the board layer; codec init applies it when audio becomes available.
    (void)board_port_set_volume(volume);
}

static void load_screen_timeout(void) {
    uint32_t seconds = 300;
    nvs_handle_t nvs;
    if (nvs_open("maclaw", NVS_READONLY, &nvs) == ESP_OK) {
        uint32_t persisted = 0;
        // Only values the settings menu can produce are trusted: 0 (never
        // sleep) or one minute to five hours in seconds.
        if (nvs_get_u32(nvs, "scr_timeout", &persisted) == ESP_OK &&
            (persisted == 0 || (persisted >= 60 && persisted <= 18000))) {
            seconds = persisted;
        }
        nvs_close(nvs);
    }
    // board_port_init() has not armed the idle countdown yet. This stores the
    // value in the board layer; the first idle pet frame picks it up.
    board_port_set_screen_timeout(seconds);
}

static void init_boot_session_id(void) {
    // esp_random() runs before the RF is up at this point, where its entropy
    // is weak; mix in the monotonic boot timer so two consecutive boots can
    // never mint the same ID (a repeated ID would skip or replay the welcome).
    uint32_t high = esp_random();
    uint32_t low = esp_random() ^ (uint32_t)esp_timer_get_time();
    snprintf(s_boot_session_id, sizeof(s_boot_session_id), "%08" PRIx32 "%08" PRIx32, high, low);
}

static esp_err_t save_volume(uint8_t percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    if (!nvs_lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open("maclaw", NVS_READWRITE, &nvs);
    if (err == ESP_OK) {
        err = nvs_set_u8(nvs, "volume_pct", percent);
        if (err == ESP_OK) err = nvs_commit(nvs);
        nvs_close(nvs);
    }
    nvs_unlock();
    return err;
}

static esp_err_t save_screen_timeout(uint32_t seconds) {
    if (!nvs_lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open("maclaw", NVS_READWRITE, &nvs);
    if (err == ESP_OK) {
        err = nvs_set_u32(nvs, "scr_timeout", seconds);
        if (err == ESP_OK) err = nvs_commit(nvs);
        nvs_close(nvs);
    }
    nvs_unlock();
    return err;
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
    // A physical reconfiguration of Wi-Fi must not consume another one-time
    // code when the device is already paired to this exact Hub. An empty code
    // means "keep the durable token" only in that narrow case. First setup,
    // pairing reset, and Hub migration still require a fresh six-digit code.
    bool preserve_pairing = pair_code && !pair_code[0] && s_gateway_token[0] &&
                            gateway_url && !strcmp(gateway_url, s_gateway_url);
    if (!ssid || !ssid[0] || strlen(ssid) > WIFI_SSID_MAX_LEN ||
        strlen(password) >= sizeof(s_wifi_password) || !is_valid_gateway_url(gateway_url) ||
        (!preserve_pairing && !is_six_digit_pair_code(pair_code)) ||
        !is_valid_choice(security, "personal", "enterprise", NULL) ||
        (enterprise && (!is_valid_choice(eap_method, "peap", "ttls", NULL) || !username || !username[0] ||
                        strlen(username) >= sizeof(s_wifi_username) || strlen(identity) >= sizeof(s_wifi_identity) ||
                        !is_valid_choice(ttls_phase2, "mschapv2", "pap", NULL) ||
                        !is_valid_choice(ca_mode, "system", "none", NULL) ||
                         strlen(server_domain) >= sizeof(s_wifi_server_domain)))) return ESP_ERR_INVALID_ARG;
    if (!nvs_lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open("maclaw", NVS_READWRITE, &nvs);
    if (err != ESP_OK) {
        nvs_unlock();
        return err;
    }
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
    if (preserve_pairing) {
        // Remove any stale one-time code left by a previous best-effort
        // cleanup. On restart a pending code takes precedence over a token,
        // so retaining it would accidentally trigger another pairing attempt.
        if (err == ESP_OK) {
            esp_err_t erase_err = nvs_erase_key(nvs, "pair_code");
            if (erase_err != ESP_OK && erase_err != ESP_ERR_NVS_NOT_FOUND) err = erase_err;
        }
    } else {
        if (err == ESP_OK) err = nvs_set_str(nvs, "pair_code", pair_code);
        if (err == ESP_OK) {
            esp_err_t erase_err = nvs_erase_key(nvs, "gateway_token");
            // First-time provisioning has no token yet; that is a successful
            // state, not an NVS error that should reject the submitted form.
            if (erase_err != ESP_OK && erase_err != ESP_ERR_NVS_NOT_FOUND) err = erase_err;
        }
    }
    if (err == ESP_OK) err = nvs_commit(nvs);
    nvs_close(nvs);
    nvs_unlock();
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
        if (preserve_pairing) {
            s_pair_code[0] = '\0';
        } else {
            strlcpy(s_pair_code, pair_code, sizeof(s_pair_code));
            s_gateway_token[0] = '\0';
        }
    }
    ESP_LOGI(TAG, "config save: ssid_len=%u security=%s gateway_len=%u pairing=%s result=%s",
             (unsigned)strlen(ssid), security, (unsigned)strlen(gateway_url),
             preserve_pairing ? "token-preserved" : "new-code", esp_err_to_name(err));
    return err;
}

static esp_err_t save_pairing_code_only(const char *pair_code) {
    if (!is_six_digit_pair_code(pair_code)) return ESP_ERR_INVALID_ARG;
    if (!nvs_lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open("maclaw", NVS_READWRITE, &nvs);
    if (err != ESP_OK) {
        nvs_unlock();
        return err;
    }
    err = nvs_set_str(nvs, "pair_code", pair_code);
    if (err == ESP_OK) err = nvs_commit(nvs);
    nvs_close(nvs);
    nvs_unlock();
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
    // later SPIFFS blocks still contain recoverable meeting audio. Scan in
    // 4 KB chunks so a large partition does not stall boot noticeably. Static
    // storage: the app_main stack is only 3.5 KB.
    static uint8_t sample[4096];
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
		// meeting.wav plus the current/temporary pet cache can be open during a
		// reconnect, with headroom for diagnostics and SPIFFS internals.
		.max_files = 8,
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
    if (!nvs_lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open("maclaw", NVS_READWRITE, &nvs);
    if (err != ESP_OK) {
        nvs_unlock();
        return err;
    }
    err = nvs_set_str(nvs, "gateway_token", token);
    if (err == ESP_OK) err = nvs_commit(nvs);
    nvs_close(nvs);
    nvs_unlock();
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
        "{\"clientId\":\"%s\",\"clientName\":\"ESP32-S3 Pet\",\"bootSessionId\":\"%s\",\"protocolVersion\":\"1.1\","
        "\"capabilities\":{\"input\":{\"modalities\":[\"text\",\"audio\"],"
        "\"audio\":{\"mimeTypes\":[\"audio/wav\"],\"sampleRates\":[16000],\"channels\":1}},"
        "\"output\":{\"modalities\":[\"text\",\"audio\"],\"preferred\":[\"audio\",\"text\"],"
        "\"combinations\":[[\"text\"],[\"audio\"],[\"audio\",\"text\"]],\"text\":{\"maxChars\":240,\"markdown\":false,\"locale\":\"zh-CN\"},"
        "\"audio\":{\"mimeTypes\":[\"audio/wav\"],\"sampleRates\":[16000],\"channels\":1,\"playback\":true,"
        "\"deliveryModes\":[\"inline\",\"url\"],\"maxInlineBytes\":8192,\"maxDownloadBytes\":262144}},"
		"\"features\":{\"petStates\":true,\"petAnimation\":true,\"petAsset\":true,"
        "\"ambientDisplay\":true,\"meetingRecorder\":true,\"volumeControl\":true}}}", CONFIG_MACLAW_CLIENT_ID, s_boot_session_id);
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
    // Same tri-state rule as the poll parser: an absent motion field keeps
    // the current setting instead of forcing motion back on.
	if (motion) s_pet_motion_enabled = cJSON_IsTrue(motion);
	if (skin || motion) board_port_set_pet_profile(skin, s_pet_motion_enabled);
	esp_err_t pet_asset_err = apply_pet_asset_json(cJSON_GetObjectItemCaseSensitive(json, "petAsset"));
	if (pet_asset_err != ESP_OK && pet_asset_err != ESP_ERR_NOT_FOUND) {
		ESP_LOGW(TAG, "GUI pet asset unavailable; native skin remains active: %s", esp_err_to_name(pet_asset_err));
	}
    apply_ambient_json(cJSON_GetObjectItemCaseSensitive(json, "ambient"));
    cJSON_Delete(json);
    response_release(&response);
    // A successful handshake is what actually proves the saved configuration
    // works: GOT_IP fires on every boot even when the Hub rejects the token,
    // which is exactly the loop the portal restart limiter protects against.
    s_setup_portal_auto_restarts = 0;
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
    // Use the provisioned gateway, not the compile-time default. The device is
    // specifically intended to work through a user-selected Hub; using the
    // factory URL here made spoken pairing silently bypass that setting.
    int n = snprintf(url, sizeof(url), "%s/api/device-gateway/v1/pair/voice", s_gateway_url);
    if (n < 0 || n >= sizeof(url)) return ESP_ERR_INVALID_SIZE;
    memset(&response, 0, sizeof(response));
    if (!s_http_mutex || xSemaphoreTake(s_http_mutex, pdMS_TO_TICKS(35000)) != pdTRUE) {
        ESP_LOGW(TAG, "HTTP request lock timeout: POST pair/voice");
        return ESP_ERR_TIMEOUT;
    }
    response.data = heap_caps_malloc(RESPONSE_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!response.data && RESPONSE_CAPACITY <= 4096) {
        response.data = heap_caps_malloc(RESPONSE_CAPACITY, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    }
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
        ESP_LOGE(TAG, "voice pair failed: err=%s status=%d body=%s",
                 esp_err_to_name(err), response.status, response.data ? response.data : "");
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        // Match pair_by_code(): a conclusive client/auth response means the
        // spoken code cannot succeed on retry, while 408/429/5xx and transport
        // failures remain temporary. This lets the normal interaction surface
        // distinguish an expired/misheard code from a network outage.
        if (response.status > 0) {
            switch (response.status) {
                case 401:
                case 403:
                case 404:
                case 409:
                case 410:
                case 422:
                    result = ESP_ERR_INVALID_STATE;
                    break;
                case 400:
                    // `bad_pair_code` means ASR heard an ambiguous/non-six-digit
                    // utterance. The one-time code may still be perfectly valid,
                    // so invite a fresh recording rather than tell the user to
                    // generate a replacement code.
                    result = ESP_ERR_INVALID_ARG;
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
        // Voice pairing is the same one-time exchange as numeric pairing. A
        // code left over from a previous setup-page attempt must not take
        // precedence over the newly issued durable token after the next boot.
        // Token persistence is authoritative; pair-code cleanup is best effort
        // and must never downgrade a successful pairing.
        bool locked = nvs_lock();
        bool code_erased = false;
        nvs_handle_t nvs;
        if (locked && nvs_open("maclaw", NVS_READWRITE, &nvs) == ESP_OK) {
            esp_err_t erase_err = nvs_erase_key(nvs, "pair_code");
            if (erase_err == ESP_OK || erase_err == ESP_ERR_NVS_NOT_FOUND) {
                code_erased = nvs_commit(nvs) == ESP_OK;
            }
            nvs_close(nvs);
        }
        if (locked) nvs_unlock();
        if (code_erased) s_pair_code[0] = '\0';
        else ESP_LOGW(TAG, "voice-paired token saved; stale pair code cleanup deferred");
    }
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
        // Pairing has already succeeded and the durable token is sufficient
        // for every later connection. Removing the one-time code is cleanup;
        // an NVS-lock timeout must not turn success into a new pairing retry.
        bool locked = nvs_lock();
        bool code_erased = false;
        nvs_handle_t nvs;
        if (locked && nvs_open("maclaw", NVS_READWRITE, &nvs) == ESP_OK) {
            esp_err_t erase_err = nvs_erase_key(nvs, "pair_code");
            if (erase_err == ESP_OK || erase_err == ESP_ERR_NVS_NOT_FOUND) {
                code_erased = nvs_commit(nvs) == ESP_OK;
            }
            nvs_close(nvs);
        }
        if (locked) nvs_unlock();
        else ESP_LOGW(TAG, "paired token saved; pair-code cleanup deferred (NVS busy)");
        if (code_erased) s_pair_code[0] = '\0';
        else ESP_LOGW(TAG, "paired token saved; stale pair code remains only for later cleanup");
    }
    response_release(&response);
    return err;
}

static esp_err_t upload_voice_once(const uint8_t *wav, size_t wav_len, char *media_id, size_t media_id_cap) {
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
        http_failure_kind_t failure = classify_http_failure(response.status, err);
        esp_err_t result = failure == HTTP_FAILURE_AUTH ? ESP_ERR_INVALID_STATE
                           : failure == HTTP_FAILURE_PERMANENT ? ESP_ERR_INVALID_ARG
                           : err == ESP_OK ? ESP_FAIL : err;
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
        http_failure_kind_t failure = classify_http_failure(put_response.status, err);
        esp_err_t result = failure == HTTP_FAILURE_AUTH ? ESP_ERR_INVALID_STATE
                           : failure == HTTP_FAILURE_PERMANENT ? ESP_ERR_INVALID_ARG
                           : err == ESP_OK ? ESP_FAIL : err;
        response_release(&put_response);
        return result;
    }
    strlcpy(media_id, id_copy, media_id_cap);
    response_release(&put_response);
    return ESP_OK;
}

// Weak-network resilience: both steps are idempotent (the upload URL can be
// re-requested, the PUT can be replayed), so retry the whole upload with a
// short backoff instead of failing the command after a single TCP/TLS wobble.
static esp_err_t upload_voice(const uint8_t *wav, size_t wav_len, char *media_id, size_t media_id_cap) {
    esp_err_t err = ESP_FAIL;
    for (unsigned attempt = 0; attempt < 3; ++attempt) {
        if (attempt > 0) {
            ESP_LOGW(TAG, "voice upload retry %u/2", attempt);
            vTaskDelay(pdMS_TO_TICKS(attempt == 1 ? 1000 : 3000));
        }
        err = upload_voice_once(wav, wav_len, media_id, media_id_cap);
        if (err == ESP_OK) return ESP_OK;
		// A permanent response has already been logged with its body. The
		// capability contract is fixed for this boot, so a 4xx retry cannot
		// recover. Transport and 5xx failures remain eligible for the bounded
		// retry loop.
		if (err == ESP_ERR_INVALID_ARG || err == ESP_ERR_INVALID_SIZE ||
			err == ESP_ERR_INVALID_RESPONSE || err == ESP_ERR_INVALID_STATE) {
			break;
		}
    }
    return err;
}

static esp_err_t send_voice_event(const char *media_id, char *reply_to, size_t reply_to_cap) {
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
    const char *maclaw_message_id = json ? json_string(json, "maclawMessageId") : NULL;
    if (!cJSON_IsTrue(accepted)) {
        cJSON_Delete(json);
        response_release(&response);
        return ESP_ERR_INVALID_RESPONSE;
    }
    if (reply_to && reply_to_cap > 0) {
        strlcpy(reply_to, maclaw_message_id ? maclaw_message_id : "", reply_to_cap);
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

static esp_err_t play_server_audio_url(const char *url, size_t expected_size) {
    if (!url || strncmp(url, "/api/im-gateway/v1/media/", 25) != 0) {
        // Never forward the durable Hub bearer to an arbitrary absolute URL.
        // Gateway-generated media references are same-origin relative paths;
        // the mediaToken query parameter authorizes the actual object.
        ESP_LOGW(TAG, "rejected non-gateway server audio URL");
        return ESP_ERR_INVALID_ARG;
    }
    if (expected_size > SERVER_AUDIO_MAX_BYTES) {
        ESP_LOGW(TAG, "server audio URL exceeds device limit: %u bytes", (unsigned)expected_size);
        return ESP_ERR_INVALID_SIZE;
    }
    size_t capacity = expected_size > 0 ? expected_size + 1 : SERVER_AUDIO_MAX_BYTES + 1;
    http_response_t media = {0};
    esp_err_t err = request_with_capacity("GET", url, NULL, NULL, 0, capacity, &media);
    if (err != ESP_OK || media.status != 200 || media.len <= 44 ||
        (expected_size > 0 && media.len != expected_size)) {
        ESP_LOGW(TAG, "server audio download failed: err=%s status=%d got=%u want=%u",
                 esp_err_to_name(err), media.status, (unsigned)media.len, (unsigned)expected_size);
        // The caller ACKs away permanent failures and retries transient ones,
        // so map precisely: overload/auth are retryable; other 4xx, truncation
        // and sizeBytes mismatches can never succeed on retry.
        esp_err_t result;
        if (err != ESP_OK) {
            // esp_http_client can surface ESP_ERR_NOT_SUPPORTED for transport-
            // level auth/redirect issues; that name is reserved here for
            // unplayable WAV data, so normalize it to a retryable error.
            result = err == ESP_ERR_NOT_SUPPORTED ? ESP_FAIL : err;
        } else if (media.status == 401 || media.status == 403) {
            result = ESP_ERR_INVALID_STATE;  // token refresh re-handshake may recover
        } else if (media.status == 408 || media.status == 429 || media.status >= 500) {
            result = ESP_FAIL;  // Hub overload or proxy wobble: retry
        } else {
            result = ESP_ERR_INVALID_SIZE;  // permanent: other 4xx, short body, size mismatch
        }
        response_release(&media);
        return result;
    }
    ESP_LOGI(TAG, "playing downloaded server speech: %u bytes", (unsigned)media.len);
    err = board_port_play_wav((const uint8_t *)media.data, media.len);
    response_release(&media);
    return err;
}

static esp_err_t play_server_audio_inline(const char *audio_data) {
    if (!audio_data || !audio_data[0]) return ESP_ERR_INVALID_ARG;
    size_t wav_capacity = 0;
    int decode_status = mbedtls_base64_decode(NULL, 0, &wav_capacity,
                                               (const unsigned char *)audio_data,
                                               strlen(audio_data));
    if (decode_status != MBEDTLS_ERR_BASE64_BUFFER_TOO_SMALL || wav_capacity <= 44 ||
        wav_capacity > SERVER_AUDIO_MAX_BYTES) {
        ESP_LOGW(TAG, "ignored server speech payload: base64=%d size=%u",
                 decode_status, (unsigned)wav_capacity);
        return ESP_ERR_INVALID_SIZE;
    }
    uint8_t *wav = heap_caps_malloc(wav_capacity, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!wav) wav = malloc(wav_capacity);
    if (!wav) return ESP_ERR_NO_MEM;
    size_t wav_len = 0;
    esp_err_t result = ESP_ERR_INVALID_RESPONSE;
    if (mbedtls_base64_decode(wav, wav_capacity, &wav_len,
                              (const unsigned char *)audio_data,
                              strlen(audio_data)) == 0) {
        ESP_LOGI(TAG, "playing inline server speech: %u bytes", (unsigned)wav_len);
        result = board_port_play_wav(wav, wav_len);
    } else {
        ESP_LOGW(TAG, "invalid inline server speech payload");
    }
    free(wav);
    return result;
}

// Bounded retry for rejected server audio, keyed by message id: consecutive
// rejections of the same id count up, and once AUDIO_RETRY_LIMIT is reached
// the media is treated as consumed (returns true) so it cannot pin the poll
// queue head forever. Shared by every audio rejection path (playback failure
// and may_play=false); permanent validation-class failures bypass this and
// are acknowledged immediately without counting.
static bool audio_reject_should_ack(const char *id) {
    // Messages without an id cannot be ACKed individually; they still count
    // under the empty key so they reach the limit and get consumed by cursor
    // advance instead of pinning the queue head forever.
    const char *key = id ? id : "";
    if (s_audio_retry_count > 0 && !strcmp(key, s_audio_retry_id)) {
        ++s_audio_retry_count;
    } else {
        strlcpy(s_audio_retry_id, key, sizeof(s_audio_retry_id));
        s_audio_retry_count = 1;
    }
    if (s_audio_retry_count >= AUDIO_RETRY_LIMIT) {
        ESP_LOGE(TAG, "server speech abandoned after %u retries: %s",
                 s_audio_retry_count, id ? id : "<none>");
        s_audio_retry_count = 0;
        s_audio_retry_id[0] = '\0';
        return true;
    }
    return false;
}

static esp_err_t poll_reply(void) {
    char path[320];
    // Keep one and only one reader for the outgoing stream. A bounded long
    // poll removes the old TLS reconnect loop while still letting interaction
    // uploads run without waiting behind a 30-second request.
    // One message per poll keeps the negotiated 8 KiB inline audio payload,
    // its base64 expansion and optional glyph metadata safely inside the
    // fixed 16 KiB JSON response buffer. A limit of four could combine four
    // individually valid messages into one truncated response. Long polling
    // immediately repeats while the server reports more queued messages, so
    // this does not add the five-second idle delay between backlog entries.
    snprintf(path, sizeof(path), "/api/im-gateway/v1/outgoing?clientId=%s&cursor=%lld&limit=1&timeout=5", CONFIG_MACLAW_CLIENT_ID, s_cursor);
    http_response_t response;
    esp_err_t err = request("GET", path, NULL, NULL, 0, &response);
    if (err != ESP_OK || response.status != 200) {
        // A rejected credential is permanent until the user re-pairs. Surface
        // it to the poll loop as ESP_ERR_INVALID_STATE, the same contract the
        // startup handshake uses, instead of retrying every three seconds.
        esp_err_t result = gateway_auth_failed(&response, err) ? ESP_ERR_INVALID_STATE
                           : err == ESP_OK ? ESP_FAIL : err;
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
    bool batch_complete = true;
	bool ack_failed = false;  // poll uses limit=1, so one receipt status per batch
    cJSON *item = NULL;
    cJSON_ArrayForEach(item, messages) {
        bool acknowledge = true;
        const char *type = json_string(item, "type");
        const char *text = json_string(item, "text");
        const char *audio_data = json_string(item, "file_data");
        if (!audio_data) audio_data = json_string(item, "data");
        const char *audio_mime = json_string(item, "mime_type");
		if (!audio_mime) audio_mime = json_string(item, "mimeType");
		if (!audio_mime) audio_mime = json_string(item, "contentType");
		const char *audio_url = json_string(item, "url");
		size_t audio_size = 0;
		cJSON *audio_size_node = cJSON_GetObjectItemCaseSensitive(item, "sizeBytes");
		if (!cJSON_IsNumber(audio_size_node)) {
			audio_size_node = cJSON_GetObjectItemCaseSensitive(item, "size_bytes");
		}
		if (cJSON_IsNumber(audio_size_node) && audio_size_node->valuedouble > 0 &&
			audio_size_node->valuedouble <= SIZE_MAX) {
			audio_size = (size_t)audio_size_node->valuedouble;
		}
		cJSON *attachments = cJSON_GetObjectItemCaseSensitive(item, "attachments");
		cJSON *audio_attachment = cJSON_IsArray(attachments) ? cJSON_GetArrayItem(attachments, 0) : NULL;
		if (cJSON_IsObject(audio_attachment)) {
			if (!audio_url) audio_url = json_string(audio_attachment, "url");
			if (!audio_mime) audio_mime = json_string(audio_attachment, "mimeType");
			if (!audio_data) audio_data = json_string(audio_attachment, "data");
			if (audio_size == 0) {
				cJSON *attachment_size = cJSON_GetObjectItemCaseSensitive(audio_attachment, "sizeBytes");
				if (cJSON_IsNumber(attachment_size) && attachment_size->valuedouble > 0 &&
					attachment_size->valuedouble <= SIZE_MAX) {
					audio_size = (size_t)attachment_size->valuedouble;
				}
			}
		}
		const char *welcome_boot_id = json_string(item, "bootSessionId");
        const char *id = json_string(item, "id");
        const char *reply_to = json_string(item, "replyTo");
        const char *skin = json_string(item, "pet_skin");
        cJSON *motion = cJSON_GetObjectItemCaseSensitive(item, "pet_motion_enabled");
        cJSON *extra = cJSON_GetObjectItemCaseSensitive(item, "extra");
        cJSON *metadata = cJSON_GetObjectItemCaseSensitive(item, "metadata");
		if (!welcome_boot_id && cJSON_IsObject(extra)) welcome_boot_id = json_string(extra, "bootSessionId");
        if (!skin && cJSON_IsObject(extra)) skin = json_string(extra, "pet_skin");
        if (!skin && cJSON_IsObject(metadata)) skin = json_string(metadata, "pet_skin");
        if (!motion && cJSON_IsObject(extra)) {
            motion = cJSON_GetObjectItemCaseSensitive(extra, "pet_motion_enabled");
        }
        if (!motion && cJSON_IsObject(metadata)) {
            motion = cJSON_GetObjectItemCaseSensitive(metadata, "pet_motion_enabled");
        }
        // pet_motion_enabled is tri-state: an absent field keeps the current
        // setting instead of silently re-enabling motion on every poll.
		if (motion) s_pet_motion_enabled = cJSON_IsTrue(motion);
		if (skin || motion) board_port_set_pet_profile(skin, s_pet_motion_enabled);
		cJSON *pet_asset = cJSON_GetObjectItemCaseSensitive(item, "pet_asset");
		if (!cJSON_IsObject(pet_asset) && cJSON_IsObject(extra)) {
			pet_asset = cJSON_GetObjectItemCaseSensitive(extra, "pet_asset");
		}
		if (cJSON_IsObject(pet_asset)) {
			esp_err_t asset_err = apply_pet_asset_json(pet_asset);
			if (asset_err != ESP_OK) {
				ESP_LOGW(TAG, "pet profile asset update failed: %s", esp_err_to_name(asset_err));
			}
		}
		apply_glyphs_json(cJSON_GetObjectItemCaseSensitive(item, "glyphs"));
        apply_ambient_json(cJSON_GetObjectItemCaseSensitive(item, "ambient"));
        if (type && !strcmp(type, "ambient")) apply_ambient_json(item);
        if (type && !strcmp(type, "pet_state")) {
            const char *state = cJSON_IsObject(extra) ? json_string(extra, "state") : NULL;
            if (!state) state = json_string(item, "state");
            // An unsolicited idle/quiet state must never interrupt the
            // foreground thinking -> result transition.
            if (state && !command_display_active()) pet(state);
        }
        // Hub-pushed speaker volume, either at message level or inside extra.
        cJSON *volume = cJSON_GetObjectItemCaseSensitive(item, "volume");
        if (!cJSON_IsNumber(volume) && cJSON_IsObject(extra)) {
            volume = cJSON_GetObjectItemCaseSensitive(extra, "volume");
        }
        if (cJSON_IsNumber(volume)) {
            int pct = (int)volume->valuedouble;
            if (pct >= 0 && pct <= 100 && pct != board_port_get_volume()) {
                if (board_port_set_volume((uint8_t)pct) == ESP_OK) {
                    esp_err_t save_err = save_volume((uint8_t)pct);
                    if (save_err == ESP_OK) {
                        ESP_LOGI(TAG, "speaker volume set to %d%% by Hub", pct);
                    } else {
                        ESP_LOGW(TAG, "Hub volume applied but not saved: %s",
                                 esp_err_to_name(save_err));
                    }
                }
            }
        }
        if (type && !strcmp(type, "meeting_result")) {
            const char *summary = cJSON_IsObject(extra) ? json_string(extra, "summary") : NULL;
            const char *status = cJSON_IsObject(extra) ? json_string(extra, "status") : NULL;
            const char *message = summary && summary[0] ? summary :
                                  text && text[0] ? text :
                                  status && status[0] ? status : "已保存到文稿库";
            pet("done");
            board_port_show_response("会议处理完成", message);
        }
        if (type && !strcmp(type, "text") && text) {
            if (cancelled_command_reply_matches(reply_to)) {
                ESP_LOGI(TAG, "ignored late reply for cancelled command: %s", reply_to);
            } else if (active_command_reply_matches(reply_to)) {
                // Once a reply is present the thinking phase has ended; a
                // double tap arriving while this frame is drawn must not turn
                // an already completed command into a cancellation.
                TaskHandle_t waiter = begin_active_command_reply();
                if (!waiter) {
                    ESP_LOGI(TAG, "reply arrived while cancellation owns command: %s", reply_to);
                } else {
                    // Keep the final response surface continuous with the
                    // thinking surface. Do not briefly switch to idle here.
                    // The waiter was already notified inside the state lock
                    // by begin_active_command_reply().
                    board_port_show_response("码卡龙", text);
                }
            } else {
                // The outgoing stream can contain unrelated notifications or
                // late replies from before this boot. They may still be shown
                // when the device is idle, but must never complete or replace
                // an active command unless replyTo identifies that command.
                if (!command_display_active()) {
                    board_port_show_response("码卡龙", text);
                } else {
                    ESP_LOGI(TAG, "deferred unrelated text during active command: replyTo=%s",
                             reply_to && reply_to[0] ? reply_to : "<none>");
                }
            }
        }
		bool is_boot_welcome = welcome_boot_id && welcome_boot_id[0];
		bool accepts_boot_welcome = !is_boot_welcome ||
									(!strcmp(welcome_boot_id, s_boot_session_id) && !s_welcome_played_for_boot);
        if (type && (!strcmp(type, "voice") || !strcmp(type, "audio")) &&
            (audio_data || audio_url)) {
            bool cancelled = cancelled_command_reply_matches(reply_to);
            bool command_match = active_command_reply_matches(reply_to);
            // The voice half of a reply arrives after the text, once the
            // command has already completed; match it against the completed
            // slots so it plays instead of being retried and dropped.
            bool completed_match = completed_command_reply_matches(reply_to);
            bool may_play = !cancelled && (command_match || completed_match || !command_display_active()) &&
							accepts_boot_welcome &&
                            (!audio_mime || !strcmp(audio_mime, "audio/wav"));
            if (may_play) {
                if (command_match && !begin_active_command_reply()) {
                    may_play = false;
                }
            }
            if (may_play) {
                esp_err_t play_err = audio_url ? play_server_audio_url(audio_url, audio_size)
                                               : play_server_audio_inline(audio_data);
				if (play_err == ESP_OK && is_boot_welcome) s_welcome_played_for_boot = true;
                if (play_err != ESP_OK) {
                    // Validation-class failures (malformed WAV, unsupported
                    // format, size mismatch, unexpected URL, corrupt inline
                    // payload) can never succeed no matter how often they are
                    // retried: treat the media as consumed. Only
                    // transport-class failures (TLS, timeout, OOM, Hub 5xx,
                    // auth refresh) stay queued for a later poll, because an
                    // unacknowledged permanent failure pins the queue head and
                    // blocks every message behind it forever.
                    bool permanent = play_err == ESP_ERR_INVALID_ARG ||
                                     play_err == ESP_ERR_INVALID_SIZE ||
                                     play_err == ESP_ERR_INVALID_RESPONSE ||
                                     play_err == ESP_ERR_NOT_SUPPORTED;
                    if (permanent) {
                        ESP_LOGE(TAG, "server speech permanently unplayable (%s); acknowledging to unblock the queue",
                                 esp_err_to_name(play_err));
                        if (is_boot_welcome) s_welcome_played_for_boot = true;
                        // Report the same "failed" status as a retry-exhausted
                        // drop: the user definitely never heard this audio.
                        ack_failed = true;
                    } else {
                        ESP_LOGW(TAG, "server speech playback failed: %s", esp_err_to_name(play_err));
                        // Transient failures retry, but only up to a bound:
                        // after the limit the media is treated as consumed so
                        // traffic queued behind it can flow again.
                        if (!audio_reject_should_ack(id)) {
                            acknowledge = false;
						} else {
							ack_failed = true;
                        }
                    }
                }
            } else if (!cancelled) {
                if (is_boot_welcome && !accepts_boot_welcome) {
                    // Already played this boot, or addressed to a previous
                    // boot: consumed, just not playable. ACK it away — an
                    // unacknowledged welcome is redelivered by every poll and
                    // would block every message queued behind it forever.
                    ESP_LOGI(TAG, "boot welcome %s; acknowledging without playback",
                             s_welcome_played_for_boot ? "already played" : "is stale");
                } else {
                    // Rejected for now: an unrelated foreground command owns
                    // the screen, the completed-reply window has expired, or
                    // the media carries no matching replyTo. Retry, but only
                    // up to the same bound as playback failures — an
                    // unacknowledged rejection would otherwise pin the queue
                    // head forever.
					if (!audio_reject_should_ack(id)) {
						acknowledge = false;
					} else {
						ack_failed = true;
					}
                }
            }
        }
        if (!acknowledge) batch_complete = false;
        if (id && acknowledge) {
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
		// With limit=1 there is one receipt per request. `failed` distinguishes
		// bounded abandonment from successful playback while still unblocking
		// the at-least-once queue.
		cJSON_AddStringToObject(ack, "status", ack_failed ? "failed" : "delivered");
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
    // Cursor progression is a second delivery acknowledgement. If one media
    // item was not consumed, keep the old cursor; already ACKed siblings are
    // filtered by the server, while the failed item is offered again.
    if (batch_complete) s_cursor = (int64_t)parsed_cursor;
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
    unsigned iterations = 0;
    while (status == PSA_SUCCESS && remaining > 0) {
        size_t wanted = remaining < buffer_size ? remaining : buffer_size;
        size_t count = fread(buffer, 1, wanted, file);
        if (count != wanted) {
            psa_hash_abort(&operation);
            return ESP_FAIL;
        }
        status = psa_hash_update(&operation, buffer, count);
        remaining -= count;
        // Hashing a multi-megabyte meeting file keeps this task busy for
        // seconds; yield periodically so the idle-task watchdog stays fed on
        // a slow flash read.
        if ((++iterations & 7u) == 0) vTaskDelay(1);
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
                                      uint8_t *buffer, size_t buffer_size,
                                      size_t completed_before, size_t total_bytes,
                                      bool publish_progress) {
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
    // ESP32-S3 hardware AES needs short-lived DMA-capable internal buffers for
    // each TLS write. MultiNet leaves the internal heap highly fragmented, so
    // reserve one contiguous block before opening TLS and release it only once
    // the connection owns its crypto buffers. This prevents -0x0084 failures.
    void *tls_internal_reserve = heap_caps_malloc(MEETING_INTERNAL_TLS_RESERVE,
                                                   MALLOC_CAP_INTERNAL |
                                                   MALLOC_CAP_DMA |
                                                   MALLOC_CAP_8BIT);
    if (!tls_internal_reserve) {
        ESP_LOGE(TAG, "meeting TLS reserve failed: need=%u", (unsigned)MEETING_INTERNAL_TLS_RESERVE);
        log_heap_snapshot("meeting-tls-reserve-fail");
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_NO_MEM;
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
    esp_http_client_config_t config = {
        .url = url, .event_handler = on_http_event, .user_data = &response,
        .timeout_ms = 60000, .crt_bundle_attach = esp_crt_bundle_attach,
        .keep_alive_enable = true,
    };
    esp_http_client_handle_t client = esp_http_client_init(&config);
    if (!client) {
        heap_caps_free(tls_internal_reserve);
        response_release(&response);
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_NO_MEM;
    }
    esp_http_client_set_method(client, HTTP_METHOD_PUT);
    esp_http_client_set_header(client, "Content-Type", "application/octet-stream");
    esp_http_client_set_header(client, "X-Chunk-SHA256", sha256_hex);
    esp_http_client_set_header(client, "Accept", "application/json");
    // keep_alive_enable in the client config owns the connection lifecycle;
    // a manual "Connection: close" header would contradict it.
    char authorization[128];
    snprintf(authorization, sizeof(authorization), "Bearer %s", s_gateway_token);
    esp_http_client_set_header(client, "Authorization", authorization);
    esp_err_t err = esp_http_client_open(client, (int)length);
    heap_caps_free(tls_internal_reserve);
    tls_internal_reserve = NULL;
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
            if (publish_state) {
                board_port_show_upload_progress(offset, file_size, "正在上传录音");
            }
            err = stream_meeting_chunk(recording_id, index, file, offset, length,
                                       chunk_hash, buffer, MEETING_IO_BUFFER_SIZE,
                                       offset, file_size, publish_state);
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
    if (err == ESP_OK && phase >= 2) {
        err = clear_meeting_recovery(true);
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "meeting delivered but local cleanup failed: %s", esp_err_to_name(err));
        }
    } else if (err == ESP_OK) {
        // The WAV must remain recoverable until Hub has accepted processing.
        // A successful upload/finalize response alone is not the same as a
        // durable Mobile-library delivery.
        ESP_LOGW(TAG, "meeting upload stopped before process acceptance: phase=%d id=%s",
                 phase, recording_id);
        err = ESP_ERR_INVALID_STATE;
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
        // Buffer 8 KB per SPIFFS write instead of the newlib default; the
        // 20 ms capture loop otherwise hits flash several times per second.
        // finalize_meeting_wav() fseek()s back to the header, which flushes
        // the buffer first, so the size patch stays correct.
        static char s_meeting_file_buf[8192];
        (void)setvbuf(file, s_meeting_file_buf, _IOFBF, sizeof(s_meeting_file_buf));
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
        unsigned consecutive_capture_errors = 0;
        meeting_set_state(MEETING_RECORDING);
        pet("listening");
        board_port_set_recording_mode(true);
        board_port_set_recording_visual(true, false, 0);
        while (s_meeting_state == MEETING_RECORDING || s_meeting_state == MEETING_PAUSED) {
            size_t count = 0;
            uint16_t level = 0;
            esp_err_t capture = board_port_audio_stream_read(samples, 512, &count, &level);
            if (capture != ESP_OK) {
                // A flash erase can stall one read; only a persistent failure
                // should abort the whole meeting. The board layer counts these
                // overruns for the end-of-meeting report.
                if (++consecutive_capture_errors >= 10) {
                    ESP_LOGE(TAG, "meeting capture failing persistently: %s",
                             esp_err_to_name(capture));
                    meeting_set_state(MEETING_ERROR);
                    break;
                }
                ESP_LOGW(TAG, "meeting capture read failed (%u/10): %s",
                         consecutive_capture_errors, esp_err_to_name(capture));
                // A stuck bus would otherwise spin this loop at full CPU.
                vTaskDelay(pdMS_TO_TICKS(20));
                continue;
            }
            consecutive_capture_errors = 0;
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
        uint32_t record_overruns = 0, record_short_reads = 0;
        board_port_get_record_stats(&record_overruns, &record_short_reads);
        if (record_overruns || record_short_reads) {
            ESP_LOGW(TAG, "meeting recording health: overruns=%lu short_reads=%lu (audio may contain gaps)",
                     (unsigned long)record_overruns, (unsigned long)record_short_reads);
        }
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
            if (record_overruns > 0) {
                char drop_note[48];
                snprintf(drop_note, sizeof(drop_note), "录音丢帧 %lu 次，继续上传",
                         (unsigned long)record_overruns);
                board_port_show_upload_progress(0, 1, drop_note);
            } else {
                board_port_show_upload_progress(0, 1, "正在准备上传");
            }
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
    // Never wait here: this runs on the touch scan context, where a 1.5 s
    // semaphore wait stalls gesture recognition. A busy interaction simply
    // defers the meeting with a short explanation instead.
    if (!resume_only && (!s_interaction_lock || xSemaphoreTake(s_interaction_lock, 0) != pdTRUE)) {
        ESP_LOGI(TAG, "meeting start deferred: foreground interaction owns the lock");
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_task_running = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        board_port_show_text("请稍后", "正在处理上一条指令");
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
    esp_err_t err = gateway_handshake();
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
            board_port_show_text("录音启动失败", "请稍后再次双击屏幕");
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
    // An unpaired device must tell the user what to say *before* the six
    // second capture starts; prompting afterwards would waste the first
    // recording on whatever the user happened to say. Keep the prompt on
    // screen instead of the waveform UI for this one-time flow.
    if (!s_gateway_token[0]) {
        board_port_show_text("设备配对", "请说出六位配对码");
    } else {
        board_port_set_recording_visual(true, false, 0);
    }
    uint8_t *wav = NULL;
    size_t wav_len = 0;
    esp_err_t err = board_port_capture_wav(&wav, &wav_len);
    if (command_cancel_requested_for(interaction_generation)) {
        board_port_set_recording_visual(false, false, 0);
        free(wav);
        finish_cancelled_command(interaction_generation);
        return;
    }
    if (err != ESP_OK || !wav || wav_len == 0) {
        // Audio is board-specific. Keep the hardware interface useful while
        // the I2S driver is brought up: a button press sends a text probe that
        // exercises the complete ESP 鈫?Hub 鈫?GUI relay.
        if (s_gateway_token[0]) {
            pet("thinking");
            // Switch to thinking before removing the recorder.  Otherwise
            // closing the recorder redraws the previous idle face for one
            // frame, exposing the weather/clock screen mid-command.
            board_port_set_recording_visual(false, false, 0);
            board_port_show_text("码卡龙", "正在检查连接");
            (void)ulTaskNotifyTake(pdTRUE, 0);
            if (send_text_event("Hello from my ESP32-S3 pet. Confirm the Hub relay is online.") == ESP_OK) {
                if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(60000)) == 0) {
                    board_port_show_text("码卡龙", "网关已连接，等待回复");
                }
                // Preserve the result screen if the reply arrived.
            } else {
                pet("alert");
                board_port_show_text("请求失败", "请检查网关连接");
            }
        } else {
            pet("alert");
            board_port_set_recording_visual(false, false, 0);
            board_port_show_text("麦克风不可用", "语音驱动未配置");
        }
        free(wav);
        // The probe path waits up to 60 s for a reply notification, so a
        // double tap can land while it sleeps. Honor the same cancel contract
        // as the voice path instead of always finishing as a completed probe.
        if (command_cancel_requested_for(interaction_generation)) {
            finish_cancelled_command(interaction_generation);
            return;
        }
        finish_interaction_task(interaction_generation);
        return;
    }
    if (!s_gateway_token[0]) {
        board_port_show_text("设备配对", "正在验证配对码");
        err = pair_by_voice(wav, wav_len);
        free(wav);
        if (err == ESP_OK && gateway_handshake() == ESP_OK) {
            if (start_gateway_ready_tasks()) {
                pet("done");
                board_port_show_ready_prompt("配对成功", "点击屏幕后说话");
            } else {
                err = ESP_ERR_NO_MEM;
                pet("alert");
                board_port_show_text("设备启动失败", "无法启动网关轮询");
            }
        }
        else {
            pet("alert");
            board_port_show_text("配对失败", err == ESP_ERR_INVALID_STATE
                                                  ? "配对码无效或已过期"
                                                  : err == ESP_ERR_INVALID_ARG
                                                        ? "请逐位说出六个数字"
                                                        : "网络异常，请稍后重试");
        }
        finish_interaction_task(interaction_generation);
        return;
    }
    // The server is the interaction runtime: it owns ASR, intent routing,
    // authorization, agent/tool execution, IM delivery, and the final reply.
    // The ESP32 only submits a server-owned `voice` media attachment.
    char media_id[96] = {0};
    // Switch state before closing the recorder. board_port_set_recording_visual
    // redraws the pet when it removes the waveform; doing that while the
    // previous state is idle briefly drew the time/weather face between
    // “received” and “thinking”.
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
    free(wav);
    if (command_cancel_requested_for(interaction_generation)) { finish_cancelled_command(interaction_generation); return; }
    if (err != ESP_OK) { pet("alert"); board_port_show_text("上传失败", "请检查网关语音支持"); finish_interaction_task(interaction_generation); return; }
    char reply_to[COMMAND_REPLY_ID_CAPACITY] = {0};
    err = send_voice_event(media_id, reply_to, sizeof(reply_to));
    taskENTER_CRITICAL(&s_task_state_lock);
    strlcpy(s_active_command_reply_to, reply_to, sizeof(s_active_command_reply_to));
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (command_cancel_requested_for(interaction_generation)) { finish_cancelled_command(interaction_generation); return; }
    if (err != ESP_OK) { pet("alert"); board_port_show_text("码卡龙错误", "请求失败"); finish_interaction_task(interaction_generation); return; }
    if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(90000)) == 0) {
        board_port_show_text("等待超时", "没有收到回复");
    }
    if (command_cancel_requested_for(interaction_generation)) { finish_cancelled_command(interaction_generation); return; }
    // The poller has already painted the final reply in the speaking state.
    // Returning through done/idle immediately after the notification repaints
    // the ambient face over it, producing the distracting apparent reboot.
    // Leave the response visible until the next user interaction or a later
    // server state update explicitly changes it.
    finish_interaction_task(interaction_generation);
}

static bool start_voice_interaction(bool consume_screen_wake) {
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
    if (!s_interaction_lock || xSemaphoreTake(s_interaction_lock, 0) != pdTRUE) {
        ESP_LOGW(TAG, "voice interaction ignored: interaction already active");
        return false;
    }
    if (consume_screen_wake) {
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
    taskENTER_CRITICAL(&s_task_state_lock);
    s_command_cancel_requested = false;
    s_command_cancel_enabled = false;
    s_command_cancel_ui_shown = false;
    s_cancel_requested_generation = 0;
    s_cancel_ui_ready_generation = 0;
    uint32_t interaction_generation = ++s_interaction_generation;
    if (!interaction_generation) interaction_generation = ++s_interaction_generation;
    s_active_command_reply_to[0] = '\0';
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_command_cancel_ui_ready) {
        while (xSemaphoreTake(s_command_cancel_ui_ready, 0) == pdTRUE) {}
    }
    board_port_set_command_display_lock(true);
    // A command press starts the command flow even when it wakes a sleeping
    // panel. Do not consume it to show the time/weather ready screen.
    (void)consume_screen_wake;
    (void)board_port_wake_from_idle();
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

static esp_err_t erase_device_config(bool erase_wifi) {
    if (!nvs_lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open("maclaw", NVS_READWRITE, &nvs);
    if (err == ESP_OK) {
		static const char *pairing_keys[] = {"gateway_token", "pair_code"};
		for (size_t i = 0; i < sizeof(pairing_keys) / sizeof(pairing_keys[0]); ++i) {
            esp_err_t erase_err = nvs_erase_key(nvs, pairing_keys[i]);
            if (erase_err != ESP_OK && erase_err != ESP_ERR_NVS_NOT_FOUND && err == ESP_OK) {
                err = erase_err;
			}
		}
		{
			esp_err_t erase_err = nvs_erase_key(nvs, "pet_asset_rev");
			if (erase_err != ESP_OK && erase_err != ESP_ERR_NVS_NOT_FOUND && err == ESP_OK) {
				err = erase_err;
			}
		}
        if (erase_wifi) {
            static const char *config_keys[] = {
                "wifi_ssid", "wifi_pass", "wifi_sec", "wifi_eap", "wifi_ident",
                "wifi_user", "wifi_ttls", "wifi_ca", "wifi_domain", "gateway_url",
                "weather", "weather_loc", "weather_temp", "weather_exp", "volume_pct",
                "scr_timeout",
            };
            for (size_t i = 0; i < sizeof(config_keys) / sizeof(config_keys[0]); ++i) {
                esp_err_t erase_err = nvs_erase_key(nvs, config_keys[i]);
                if (erase_err != ESP_OK && erase_err != ESP_ERR_NVS_NOT_FOUND && err == ESP_OK) {
                    err = erase_err;
                }
            }
        }
        if (err == ESP_OK) err = nvs_commit(nvs);
        nvs_close(nvs);
    }
    nvs_unlock();
    if (err != ESP_OK) return err;

    s_gateway_token[0] = '\0';
    s_pair_code[0] = '\0';
    if (erase_wifi) {
        s_wifi_ssid[0] = '\0';
        s_wifi_password[0] = '\0';
        s_wifi_identity[0] = '\0';
        s_wifi_username[0] = '\0';
        s_wifi_server_domain[0] = '\0';
        strlcpy(s_wifi_security, "personal", sizeof(s_wifi_security));
        strlcpy(s_wifi_eap_method, "peap", sizeof(s_wifi_eap_method));
        strlcpy(s_wifi_ttls_phase2, "mschapv2", sizeof(s_wifi_ttls_phase2));
        strlcpy(s_wifi_ca_mode, "system", sizeof(s_wifi_ca_mode));
        strlcpy(s_gateway_url, CONFIG_MACLAW_SERVER_URL, sizeof(s_gateway_url));
        s_weather_summary[0] = '\0';
        s_weather_location[0] = '\0';
        s_weather_temperature_c = 0;
        s_weather_expires_at_ms = 0;
        s_weather_valid = false;
        (void)board_port_set_volume(70);
        // NVS erasure alone does not reset the runtime copies: if the user
        // abandons the portal without restarting, the old screen timeout
        // would linger until the next boot.
        board_port_set_screen_timeout(300);
    }
    return ESP_OK;
}

// Performs the settings-menu clear confirmed on device. Runs on
// ui_request_worker: the board settings callback executes on the settings
// worker context, which must not block on the NVS erase plus provisioning
// portal transition (both can take seconds).
static void clear_settings_config(bool erase_wifi) {
    esp_err_t err = erase_device_config(erase_wifi);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "cannot clear device configuration: %s", esp_err_to_name(err));
        pet("alert");
        board_port_show_text("清除失败", "请重新进入设置后重试");
        return;
    }
	ESP_LOGI(TAG, "%s cleared from local settings", erase_wifi ? "all configuration" : "pairing");
	if (s_storage_mounted && s_storage_mutex &&
		xSemaphoreTake(s_storage_mutex, pdMS_TO_TICKS(3000)) == pdTRUE) {
		(void)unlink(PET_ASSET_CACHE_PATH);
		(void)unlink(PET_ASSET_CACHE_TEMP_PATH);
		(void)unlink(PET_ASSET_CACHE_BACKUP_PATH);
		s_cached_pet_asset_revision[0] = '\0';
		s_desired_pet_asset_revision[0] = '\0';
		xSemaphoreGive(s_storage_mutex);
	}
	(void)board_port_set_pet_asset(NULL, 0, 0, 0, 0, NULL);
	// Pairing-only recovery keeps STA active. Full reset deliberately enters
    // AP-only provisioning. Meeting audio/recovery data is user content and
    // is preserved by both operations.
    start_setup_portal(!erase_wifi);
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
    start_setup_portal(false);
}

// Requests the touch scan task must not execute itself: full LCD status draws
// and the setup-portal transition can block for seconds and starve the gesture
// debounce. The button callback only posts a request; this worker (mirroring
// command_cancel_worker) performs the heavy part on its own context.
typedef enum {
    UI_REQUEST_NONE = 0,
    UI_REQUEST_RESUME_RUNNING,
    UI_REQUEST_RESUME_STARTED,
    UI_REQUEST_RESUME_FAILED,
    UI_REQUEST_ENTER_SETUP_PORTAL,
    UI_REQUEST_WIFI_AUTH_FAILED,
    UI_REQUEST_CLEAR_PAIRING,
    UI_REQUEST_CLEAR_ALL,
} ui_request_t;

// Mode changes, as opposed to transient status hints: a queued repaint
// must not replace them before the worker consumes them, and they must
// still run while the setup portal owns the screen.
static bool ui_request_is_mode_change(ui_request_t request) {
    return request == UI_REQUEST_ENTER_SETUP_PORTAL ||
           request == UI_REQUEST_CLEAR_PAIRING ||
           request == UI_REQUEST_CLEAR_ALL;
}

static void post_ui_request(ui_request_t request) {
    if (!s_ui_request_task) return;
    // Overwrite is intentional: only the latest UI hint matters, and a stale
    // queued message must not paint over a newer screen seconds later. Mode
    // changes are the exception: a queued RESUME_* repaint must not replace
    // one before the worker has consumed it.
    uint32_t pending = UI_REQUEST_NONE;
    if (!ui_request_is_mode_change(request) &&
        // eNoAction only reads the current value; the pending notification
        // stays queued for the worker either way.
        xTaskNotifyAndQuery(s_ui_request_task, 0, eNoAction, &pending) == pdPASS &&
        ui_request_is_mode_change((ui_request_t)pending)) {
        return;
    }
    xTaskNotify(s_ui_request_task, (uint32_t)request, eSetValueWithOverwrite);
}

static void ui_request_worker(void *arg) {
    (void)arg;
    while (true) {
        uint32_t request = UI_REQUEST_NONE;
        if (xTaskNotifyWait(0, UINT32_MAX, &request, portMAX_DELAY) != pdTRUE) continue;
        // While the setup portal owns the screen, drop transient status
        // repaints; they would paint over the QR/form page. Mode-change
        // requests (portal entry, settings clears) must still run.
        if (!ui_request_is_mode_change((ui_request_t)request) && s_setup_portal_active) {
            continue;
        }
        switch ((ui_request_t)request) {
            case UI_REQUEST_RESUME_RUNNING:
                board_port_show_text("会议记录续传中", "完成后可开始新会议");
                break;
            case UI_REQUEST_RESUME_STARTED:
                board_port_show_text("正在续传上次录音", "完成后可开始新会议");
                break;
            case UI_REQUEST_RESUME_FAILED:
                pet("alert");
                board_port_show_text("续传任务未启动", "设备将稍后自动重试");
                break;
            case UI_REQUEST_ENTER_SETUP_PORTAL:
                enter_setup_portal();
                break;
            case UI_REQUEST_WIFI_AUTH_FAILED:
                pet("alert");
                board_port_show_text("Wi-Fi 密码错误", "请长按屏幕重新配网");
                break;
            case UI_REQUEST_CLEAR_PAIRING:
                clear_settings_config(false);
                break;
            case UI_REQUEST_CLEAR_ALL:
                clear_settings_config(true);
                break;
            default:
                break;
        }
    }
}

// Board settings menu events. Only the small NVS writes (volume, screen
// timeout) are cheap enough for the caller's context (settings worker /
// touch scan task); the destructive clear flows are posted to
// ui_request_worker.
static void on_board_settings(board_port_settings_event_t event, uint8_t value, void *arg) {
    (void)arg;
    switch (event) {
        case BOARD_SETTINGS_VOLUME_CHANGED:
            {
                esp_err_t err = save_volume(value);
                if (err == ESP_OK) {
                    ESP_LOGI(TAG, "local speaker volume saved: %u%%", (unsigned)value);
                } else {
                    ESP_LOGE(TAG, "local speaker volume persistence failed: %s",
                             esp_err_to_name(err));
                    board_port_show_text("音量保存失败", "本次调节仍已生效");
                }
            }
            break;
        case BOARD_SETTINGS_TIMEOUT_CHANGED:
            {
                // The event payload is the preset index; the board layer has
                // already applied the new timeout, so read back the seconds.
                uint32_t seconds = board_port_get_screen_timeout();
                esp_err_t err = save_screen_timeout(seconds);
                if (err == ESP_OK) {
                    ESP_LOGI(TAG, "screen timeout saved: %lu s", (unsigned long)seconds);
                } else {
                    ESP_LOGE(TAG, "screen timeout persistence failed: %s",
                             esp_err_to_name(err));
                    board_port_show_text("熄屏时间保存失败", "本次设置仍已生效");
                }
            }
            break;
        case BOARD_SETTINGS_CLEAR_PAIRING:
            post_ui_request(UI_REQUEST_CLEAR_PAIRING);
            break;
        case BOARD_SETTINGS_CLEAR_ALL:
            post_ui_request(UI_REQUEST_CLEAR_ALL);
            break;
        case BOARD_SETTINGS_CLOSED:
        default:
            break;
    }
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
    // A second tap during a short voice command means “submit what I have”.
    // Consume any gesture while the microphone worker is live; once capture
    // has ended, double tap resumes its separate thinking-phase cancel role.
    if (board_port_request_capture_stop()) {
        ESP_LOGI(TAG, "short voice capture stop requested: gesture=%s",
                 event == BOARD_BUTTON_SHORT ? "short" :
                 event == BOARD_BUTTON_DOUBLE ? "double" : "long");
        return;
    }
    if (event == BOARD_BUTTON_LONG) {
        // With no saved credentials there is nothing left to reset.
        if (!s_wifi_ssid[0]) {
            ESP_LOGI(TAG, "long press ignored while setup portal is active");
            return;
        }
        ESP_LOGW(TAG, "long press: entering configuration portal without erasing saved credentials");
        // Opening the portal stops the wake recognizer and starts an HTTP
        // server; that is far too slow for the touch scan task, so a worker
        // performs the transition (see ui_request_worker).
        post_ui_request(UI_REQUEST_ENTER_SETUP_PORTAL);
        return;
    }
    if (event == BOARD_BUTTON_DOUBLE) {
        bool interaction_active;
        taskENTER_CRITICAL(&s_task_state_lock);
        interaction_active = s_interaction_task != NULL;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (interaction_active) {
            // Never reinterpret a double tap made during a voice command as a
            // meeting-recording request, even if cancellation was already
            // requested by the controller's native double-click event.
            (void)request_command_cancel();
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
                // The status draw itself runs in ui_request_worker so this
                // scan task never blocks on an LCD transfer.
                post_ui_request(UI_REQUEST_RESUME_RUNNING);
            } else if (ensure_meeting_resume_supervisor()) {
                post_ui_request(UI_REQUEST_RESUME_STARTED);
            } else {
                post_ui_request(UI_REQUEST_RESUME_FAILED);
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
        if (!start_meeting_task(false)) {
            pet("alert");
            board_port_show_text("录音启动失败", "设备正在处理其它操作");
        }
        return;
    }
    if (event != BOARD_BUTTON_SHORT) return;
    // Only reached with the panel awake: a short press on a slept display is
    // consumed as wake-only by the board layer and never delivered here. The
    // offline wake phrase is hands-free and therefore wakes the panel and
    // records in the same event.
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

// Clears the portal-active flag under the same lock the entry guard uses, so
// a concurrent start_setup_portal() caller never observes a half-torn state.
static void setup_portal_mark_inactive(void) {
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_portal_active = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
}

// The 64-bit activity timestamp is shared between the httpd task and the
// portal watchdog task; update it under the lock so neither reads a torn
// value. The clock read itself may stay outside.
static void setup_portal_note_activity(void) {
    int64_t now = esp_timer_get_time();
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_portal_activity_us = now;
    taskEXIT_CRITICAL(&s_task_state_lock);
}

static esp_err_t setup_get_handler(httpd_req_t *req) {
    // Serving the form is user activity; hold off the portal idle
    // watchdog while the owner is slowly filling it in.
    setup_portal_note_activity();
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
        "<form method=post action=/save><label>Wi-Fi name</label><input name=ssid list=ssidlist required maxlength=32 autocapitalize=none autocomplete=off><datalist id=ssidlist></datalist>"
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
        "<button>Save and connect</button></form>"
        "<script>fetch('/scan').then(function(r){return r.json()}).then(function(a){"
        "var d=document.getElementById('ssidlist');"
        "a.forEach(function(s){var o=document.createElement('option');o.value=s;d.appendChild(o)})"
        "}).catch(function(){})</script></body></html>";
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
    static const char reconfigure_page[] =
        "<!doctype html><html><head><meta name=viewport content='width=device-width,initial-scale=1'>"
        "<style>body{font-family:system-ui,sans-serif;max-width:26rem;margin:2rem auto;padding:0 1rem;color:#102a43}"
        "label{display:block;margin:1rem 0 .3rem}input,select{box-sizing:border-box;width:100%;padding:.7rem;font-size:1rem}"
        ".enterprise{margin-top:1rem;padding:.85rem;border:1px solid #b9c9d7;background:#f5f9fc}.hint{font-size:.85rem;color:#486581;line-height:1.45}"
        "button{margin-top:1.3rem;padding:.8rem 1.2rem;font-size:1rem;background:#1769aa;color:#fff;border:0;border-radius:.4rem}</style>"
        "</head><body><h1>Change Wi-Fi</h1><p>设备配对仍然有效。只修改 Wi-Fi 时无需重新输入配对码。</p>"
        "<form method=post action=/save><input type=hidden name=preserve_pairing value=1>"
        "<label>Wi-Fi name</label><input name=ssid list=ssidlist required maxlength=32 autocapitalize=none autocomplete=off><datalist id=ssidlist></datalist>"
        "<label>Security</label><select name=security id=security onchange='document.getElementById(\"enterprise\").hidden=this.value!==\"enterprise\";document.getElementById(\"passlabel\").textContent=this.value===\"enterprise\"?\"Password\":\"Wi-Fi password\"'><option value=personal selected>Personal (WPA/WPA2/WPA3)</option><option value=enterprise>Enterprise (802.1X)</option></select>"
        "<label id=passlabel>Wi-Fi password</label><input name=password type=password maxlength=64>"
        "<section class=enterprise id=enterprise hidden><strong>Enterprise Wi-Fi</strong>"
        "<label>EAP method</label><select name=eap_method><option value=peap selected>PEAP</option><option value=ttls>TTLS</option></select>"
        "<label>Identity (optional)</label><input name=identity maxlength=127 autocapitalize=none>"
        "<label>Username</label><input name=username maxlength=127 autocapitalize=none>"
        "<label>TTLS inner authentication</label><select name=ttls_phase2><option value=mschapv2 selected>MSCHAPv2</option><option value=pap>PAP</option></select>"
        "<label>CA certificate</label><select name=ca_mode><option value=system selected>Use system certificates</option><option value=none>Do not validate</option></select>"
        "<label>Server domain (optional)</label><input name=server_domain maxlength=127 autocapitalize=none></section>"
        "<button>Save Wi-Fi and reconnect</button></form>"
        "<script>fetch('/scan').then(function(r){return r.json()}).then(function(a){var d=document.getElementById('ssidlist');a.forEach(function(s){var o=document.createElement('option');o.value=s;d.appendChild(o)})}).catch(function(){})</script></body></html>";
    ESP_LOGI(TAG, "setup portal request: %s", req->uri);
    httpd_resp_set_type(req, "text/html; charset=utf-8");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    // AP+STA is also used for rejected-token recovery, which must show the
    // pairing-only page. A deliberate long press keeps a valid token and opens
    // the full Wi-Fi form without requiring a second one-time code.
    const char *page = s_pairing_recovery_portal
                           ? pairing_page
                           : (s_gateway_token[0] ? reconfigure_page : setup_page);
    return httpd_resp_send(req, page, HTTPD_RESP_USE_STRLEN);
}

// SSID scan results for the setup-page datalist. A blocking scan takes ~2 s,
// so the JSON is cached for 30 s and concurrent portal clients share one scan.
static char s_scan_json[1024];
static int64_t s_scan_cached_at_us;

static esp_err_t setup_scan_handler(httpd_req_t *req) {
    // A scan request means someone is actively configuring; refresh the
    // portal idle watchdog the same way a form submission does.
    setup_portal_note_activity();
    int64_t now = esp_timer_get_time();
    taskENTER_CRITICAL(&s_task_state_lock);
    bool expired = !s_scan_json[0] || now - s_scan_cached_at_us > 30LL * 1000 * 1000;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (expired) {
        // Build into a stack buffer and publish under the lock, so concurrent
        // portal clients never read a half-written JSON. The ~2 s blocking
        // scan itself stays outside the lock.
        char json[sizeof(s_scan_json)];
        json[0] = '\0';
        wifi_scan_config_t scan_cfg = { .show_hidden = false };
        wifi_mode_t mode = WIFI_MODE_NULL;
        esp_err_t mode_err = esp_wifi_get_mode(&mode);
        // ESP-IDF cannot perform a station scan in AP-only mode. First-time
        // provisioning deliberately starts AP-only for stability, so promote
        // it to AP+STA just for (and after) the user's scan. This keeps the
        // setup hotspot alive while allowing the browser to list nearby SSIDs.
        if (mode_err == ESP_OK && mode == WIFI_MODE_AP) {
            mode_err = esp_wifi_set_mode(WIFI_MODE_APSTA);
            if (mode_err != ESP_OK) {
                ESP_LOGW(TAG, "cannot enable STA for setup scan: %s",
                         esp_err_to_name(mode_err));
            }
        }
        esp_err_t scan_err = mode_err == ESP_OK
                                 ? esp_wifi_scan_start(&scan_cfg, true)
                                 : mode_err;
        if (scan_err == ESP_OK) {
            uint16_t ap_num = 0;
            esp_wifi_scan_get_ap_num(&ap_num);
            if (ap_num > 20) ap_num = 20;
            wifi_ap_record_t *records = malloc((size_t)ap_num * sizeof(wifi_ap_record_t));
            if (records) {
                uint16_t fetched = ap_num;
                size_t used = 0;
                if (esp_wifi_scan_get_ap_records(&fetched, records) == ESP_OK) {
                    used += snprintf(json + used, sizeof(json) - used, "[");
                    bool first = true;
                    for (uint16_t i = 0; i < fetched && used + 40 < sizeof(json); ++i) {
                        // The SSID field is 32 bytes with no guaranteed NUL
                        // terminator; bound the copy before treating it as a
                        // C string.
                        char ssid[33];
                        size_t ssid_len = strnlen((const char *)records[i].ssid, 32);
                        memcpy(ssid, records[i].ssid, ssid_len);
                        ssid[ssid_len] = '\0';
                        if (!ssid[0]) continue;
                        used += snprintf(json + used, sizeof(json) - used,
                                         first ? "\"" : ",\"");
                        first = false;
                        for (const char *p = ssid; *p && used + 3 < sizeof(json); ++p) {
                            // Control characters would corrupt the JSON and
                            // the phone's datalist rendering; skip them.
                            if ((unsigned char)*p < 0x20) continue;
                            if (*p == '"' || *p == '\\') json[used++] = '\\';
                            json[used++] = *p;
                        }
                        used += snprintf(json + used, sizeof(json) - used, "\"");
                    }
                    snprintf(json + used, sizeof(json) - used, "]");
                }
                free(records);
            }
        } else {
            ESP_LOGW(TAG, "setup Wi-Fi scan failed: %s", esp_err_to_name(scan_err));
        }
        if (!json[0]) strlcpy(json, "[]", sizeof(json));
        // Cache failures as well: an empty result is still valid for 30 s,
        // otherwise every page load pays for a fresh ~2 s blocking scan.
        taskENTER_CRITICAL(&s_task_state_lock);
        strlcpy(s_scan_json, json, sizeof(s_scan_json));
        s_scan_cached_at_us = now;
        taskEXIT_CRITICAL(&s_task_state_lock);
    }
    httpd_resp_set_type(req, "application/json");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    return httpd_resp_sendstr(req, s_scan_json);
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
            if (end && !*end) {
                out[used++] = (char)value;
                src += 2;
                continue;
            }
            // A '%' that is not a valid hex escape is a literal character
            // (SSIDs may contain one); pass it through instead of rejecting
            // the whole form.
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
    // A form submission is proof of user activity; hold off the portal idle
    // watchdog while the owner is actively configuring the device.
    setup_portal_note_activity();
    char body[2048] = {0}, ssid[WIFI_VALUE_CAPACITY] = {0}, password[WIFI_VALUE_CAPACITY] = {0},
         gateway[URL_CAPACITY] = {0}, code[PAIR_CODE_CAPACITY] = {0}, security[WIFI_EAP_MODE_CAPACITY] = "personal",
         eap_method[WIFI_EAP_MODE_CAPACITY] = "peap", identity[WIFI_ENTERPRISE_VALUE_CAPACITY] = {0},
         username[WIFI_ENTERPRISE_VALUE_CAPACITY] = {0}, ttls_phase2[WIFI_EAP_MODE_CAPACITY] = "mschapv2",
         ca_mode[WIFI_EAP_MODE_CAPACITY] = "system", server_domain[WIFI_ENTERPRISE_VALUE_CAPACITY] = {0};
    if (req->content_len <= 0 || req->content_len >= sizeof(body)) {
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "表单数据过大");
        return ESP_FAIL;
    }
    int received = 0;
    while (received < req->content_len) {
        int n = httpd_req_recv(req, body + received, req->content_len - received);
        if (n <= 0) {
            httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "表单接收不完整，请重试");
            return ESP_FAIL;
        }
        received += n;
    }
    body[received] = '\0';
    char reuse[4] = {0};
    bool reuse_wifi = form_value(body, "reuse", reuse, sizeof(reuse)) && !strcmp(reuse, "1");
    char preserve[4] = {0};
    bool preserve_pairing = !reuse_wifi && s_gateway_token[0] &&
                            form_value(body, "preserve_pairing", preserve, sizeof(preserve)) &&
                            !strcmp(preserve, "1");
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
    bool invalid_form = !preserve_pairing && !form_value(body, "code", code, sizeof(code));
    if (!reuse_wifi) {
        invalid_form = invalid_form || !form_value(body, "ssid", ssid, sizeof(ssid)) ||
                       !form_value(body, "password", password, sizeof(password)) ||
                       !form_value(body, "security", security, sizeof(security));
        if (preserve_pairing) {
            // The reconfiguration page changes the radio credentials only.
            // Keeping the Hub URL server-side prevents a modified form from
            // redirecting the durable bearer token to another origin.
            strlcpy(gateway, s_gateway_url, sizeof(gateway));
        } else {
            invalid_form = invalid_form || !form_value(body, "gateway", gateway, sizeof(gateway));
        }
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
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "表单不完整：请检查 Wi-Fi 与企业网认证字段");
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
        if (!ssid[0]) snprintf(reason, sizeof(reason), "请填写 Wi-Fi 名称");
        else if (strlen(ssid) > WIFI_SSID_MAX_LEN) snprintf(reason, sizeof(reason), "Wi-Fi 名称过长（最多 32 字节）");
        else if (strlen(password) >= sizeof(s_wifi_password)) snprintf(reason, sizeof(reason), "Wi-Fi 密码过长（最多 64 字节）");
        else if (!strcmp(security, "enterprise") && !username[0]) snprintf(reason, sizeof(reason), "请填写企业网用户名");
        else if (!is_valid_choice(security, "personal", "enterprise", NULL)) snprintf(reason, sizeof(reason), "不支持的 Wi-Fi 加密方式");
        else if (!is_valid_gateway_url(gateway)) snprintf(reason, sizeof(reason), "Hub 地址必须以 http:// 或 https:// 开头");
        else if (!preserve_pairing && !is_six_digit_pair_code(code)) snprintf(reason, sizeof(reason), "配对码必须是 6 位数字");
        else snprintf(reason, sizeof(reason), "配置保存失败：%s", esp_err_to_name(save_err));
        ESP_LOGW(TAG, "setup rejected: %s", reason);
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, reason);
        return ESP_FAIL;
    }
    // Do not reset from the HTTP server task.  esp_http_server sends responses
    // asynchronously, so a reset here can race its final socket write and, on
    // this board, leave the setup QR frame on screen indefinitely.  Schedule
    // the reset after this handler has returned and the response is flushed.
    httpd_resp_sendstr(req, "已保存。设备正在重启并连接码卡龙。");
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

// An abandoned setup portal would otherwise hold the device offline forever:
// the AP is open, the user walked away, and nothing re-enters normal mode.
// The ambient clock task is not guaranteed to run in every portal entry path
// (e.g. first-time provisioning returns before start_clock_sync), so a tiny
// dedicated watchdog checks the inactivity deadline. Only a device with a
// saved STA configuration is restarted; an unprovisioned one has nothing to
// fall back to and must keep serving the form.
static void setup_portal_watchdog_task(void *arg) {
    (void)arg;
    while (true) {
        vTaskDelay(pdMS_TO_TICKS(30000));
        if (!s_setup_portal_active) break;
        int64_t last_activity_us;
        taskENTER_CRITICAL(&s_task_state_lock);
        last_activity_us = s_setup_portal_activity_us;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_wifi_ssid[0] &&
            esp_timer_get_time() - last_activity_us >= SETUP_PORTAL_IDLE_TIMEOUT_US) {
            // A broken saved configuration used to loop forever: 5 min idle
            // -> restart -> join fails -> portal -> 5 min idle. After three
            // automatic restarts keep serving the form and wait for the user
            // instead of rebooting the device out from under them.
            if (++s_setup_portal_auto_restarts > 3) {
                ESP_LOGW(TAG, "setup portal idle timeout hit %u times; staying in portal for manual recovery",
                         s_setup_portal_auto_restarts);
                break;
            }
            ESP_LOGW(TAG, "setup portal idle for %lld s; restarting into normal mode (%u/3)",
                     (long long)(SETUP_PORTAL_IDLE_TIMEOUT_US / 1000000LL),
                     s_setup_portal_auto_restarts);
            esp_restart();
        }
    }
    s_setup_portal_watchdog_task = NULL;
    vTaskDelete(NULL);
}

static void ensure_setup_portal_watchdog(void) {
    setup_portal_note_activity();
    if (s_setup_portal_watchdog_task) return;
    BaseType_t created = xTaskCreate(setup_portal_watchdog_task, "maclaw_setup_wd",
                                     2048, NULL, 1, &s_setup_portal_watchdog_task);
    if (created != pdPASS) {
        s_setup_portal_watchdog_task = NULL;
        ESP_LOGW(TAG, "cannot start setup portal watchdog");
    }
}

static void start_setup_portal(bool keep_station) {
    // Several tasks can reach this entry concurrently (gateway poll/startup,
    // start_wifi, ui_request_worker). Check-and-set under the state lock so
    // only the first caller tears down audio/Wi-Fi and starts the server; a
    // later caller keeps the already-running portal and its keep_station mode.
    // Set this before any slow display or Wi-Fi operation. A button event can
    // be delivered by its independent task while the QR page is being drawn.
    taskENTER_CRITICAL(&s_task_state_lock);
    bool already_active = s_setup_portal_active;
    if (!already_active) s_setup_portal_active = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (already_active) {
        // Another task is starting (or already runs) the portal. Wait for the
        // server to appear instead of silently dropping this caller's intent:
        // if the first attempt failed and cleared the flag, retry once here.
        for (unsigned i = 0; i < 100 && !s_setup_server; ++i) {
            bool still_starting;
            taskENTER_CRITICAL(&s_task_state_lock);
            still_starting = s_setup_portal_active;
            taskEXIT_CRITICAL(&s_task_state_lock);
            if (!still_starting) break;
            vTaskDelay(pdMS_TO_TICKS(100));
        }
        if (s_setup_server) {
            // The server handle alone is not proof: the route-registration
            // failure path stops the server right after publishing it. Accept
            // the portal only while it is still marked active.
            bool portal_live;
            taskENTER_CRITICAL(&s_task_state_lock);
            portal_live = s_setup_portal_active;
            taskEXIT_CRITICAL(&s_task_state_lock);
            if (portal_live) return;  // The portal is up; the intent is served.
        }
        taskENTER_CRITICAL(&s_task_state_lock);
        already_active = s_setup_portal_active;
        if (!already_active) s_setup_portal_active = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (already_active) {
            // A third caller won the retry race; its outcome serves us too.
            ESP_LOGW(TAG, "setup portal start superseded by concurrent caller");
            return;
        }
        ESP_LOGW(TAG, "first setup portal attempt failed; retrying once");
    }
    ensure_setup_portal_watchdog();
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
        setup_portal_mark_inactive();
        ESP_LOGE(TAG, "cannot enter setup Wi-Fi mode: %s", esp_err_to_name(portal_err));
        board_port_show_text("设置失败", "请在网页重新设置");
        return;
    }
    portal_err = esp_wifi_set_config(WIFI_IF_AP, &ap);
    if (portal_err != ESP_OK) {
        setup_portal_mark_inactive();
        ESP_LOGE(TAG, "cannot configure setup hotspot: %s", esp_err_to_name(portal_err));
        board_port_show_text("设置失败", "请在网页重新设置");
        return;
    }
    if (!s_wifi_started) {
        portal_err = esp_wifi_start();
        if (portal_err != ESP_OK) {
            setup_portal_mark_inactive();
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
        setup_portal_mark_inactive();
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
    // Four captive-check endpoints, /scan, the GET wildcard and POST /save.
    // This capacity is checked when routes are registered at runtime.
    server_config.max_uri_handlers = 7;
    // The provisioning page is static and tiny; a single socket is enough for
    // a phone browser and captive-check probe, and saves several server task
    // stacks compared with the desktop-oriented default of 7.
    server_config.max_open_sockets = 3;
    server_config.lru_purge_enable = true;
    // Make the AP behave like a captive portal. Android, iOS and Windows all
    // probe different HTTP paths before showing the setup page; the wildcard
    // returns the same deterministic form for those paths and manual URLs.
    server_config.uri_match_fn = httpd_uri_match_wildcard;
    // Start into a local handle and publish s_setup_server only after every
    // route is registered: concurrent start_setup_portal() callers treat a
    // non-NULL handle as "portal is up" and must never observe a server whose
    // routes are still being registered (or about to be torn down).
    httpd_handle_t server = NULL;
    portal_err = httpd_start(&server, &server_config);
    if (portal_err != ESP_OK) {
        setup_portal_mark_inactive();
        ESP_LOGE(TAG, "cannot start setup web server: %s, free_heap=%u",
                 esp_err_to_name(portal_err), (unsigned)esp_get_free_heap_size());
        board_port_show_text("设置失败", "网页服务内存不足，请重启");
        return;
    }
    httpd_uri_t apple_success = {.uri = "/hotspot-detect.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_generate_204 = {.uri = "/generate_204", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_gen_204 = {.uri = "/gen_204", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t windows_connect = {.uri = "/connecttest.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t scan = {.uri = "/scan", .method = HTTP_GET, .handler = setup_scan_handler};
    httpd_uri_t captive = {.uri = "/*", .method = HTTP_GET, .handler = setup_get_handler};
    httpd_uri_t save = {.uri = "/save", .method = HTTP_POST, .handler = setup_save_handler};
    // Register the wildcard last: ESP-IDF preserves registration order during
    // matching, so it must not shadow the platform-specific probe routes.
    portal_err = httpd_register_uri_handler(server, &apple_success);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(server, &android_generate_204);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(server, &android_gen_204);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(server, &windows_connect);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(server, &scan);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(server, &captive);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(server, &save);
    if (portal_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register setup portal routes: %s, free_heap=%u",
                 esp_err_to_name(portal_err), (unsigned)esp_get_free_heap_size());
        httpd_stop(server);
        setup_portal_mark_inactive();
        board_port_show_text("设置失败", "配置网页路由启动失败");
        return;
    }
    s_setup_server = server;
    configure_setup_dhcp_dns();
    start_captive_dns();
    if (s_pairing_recovery_portal) {
        // A lower-band text overlay did not make it clear that the AP was
        // actually ready, nor where to open the form. Keep a stable portal
        // surface just like first-time provisioning, with both pieces of
        // information visible without relying on captive-portal detection.
        board_port_show_setup_portal(ap_ssid, SETUP_AP_IP_ADDR, true);
    } else {
        show_setup_qrcode(ap_ssid);
    }
    ESP_LOGI(TAG, "%s portal ready: join %s and open http://192.168.4.1",
             s_pairing_recovery_portal ? "pairing recovery" : "setup", ap_ssid);
}

static void wifi_reconnect_timer_callback(void *arg) {
    (void)arg;
    // Runs in the esp_timer task, never inside the Wi-Fi event handler, so the
    // backoff delay cannot stall event delivery for the rest of the stack.
    esp_wifi_connect();
}

static void schedule_wifi_reconnect(void) {
    if (!s_wifi_reconnect_timer) {
        const esp_timer_create_args_t timer_args = {
            .callback = wifi_reconnect_timer_callback,
            .name = "wifi_reconnect",
        };
        if (esp_timer_create(&timer_args, &s_wifi_reconnect_timer) != ESP_OK) {
            s_wifi_reconnect_timer = NULL;
            // Fall back to the previous immediate behavior rather than never
            // reconnecting after a one-shot allocation failure.
            esp_wifi_connect();
            return;
        }
    }
    esp_timer_stop(s_wifi_reconnect_timer);
    // Exponential backoff 1s -> 2s -> ... capped at 60s. Immediate reconnect
    // hammering kept the radio busy enough to delay association with a busy
    // or distant AP even further.
    unsigned shift = s_wifi_disconnect_retry > 6 ? 6 : s_wifi_disconnect_retry;
    uint32_t delay_ms = 1000u << shift;
    if (delay_ms > 60000u) delay_ms = 60000u;
    if (s_wifi_disconnect_retry < 6) ++s_wifi_disconnect_retry;
    ESP_LOGW(TAG, "Wi-Fi disconnected from %s; retry in %lu ms", s_wifi_ssid,
             (unsigned long)delay_ms);
    esp_timer_start_once(s_wifi_reconnect_timer, (uint64_t)delay_ms * 1000u);
}

static void wifi_event(void *arg, esp_event_base_t base, int32_t id, void *data) {
    (void)arg;
    if (base == WIFI_EVENT && id == WIFI_EVENT_STA_START) {
        esp_wifi_connect();
        return;
    }
    if (base == WIFI_EVENT && id == WIFI_EVENT_STA_DISCONNECTED) {
        wifi_event_sta_disconnected_t *disconnected = (wifi_event_sta_disconnected_t *)data;
        xEventGroupClearBits(s_wifi_events, WIFI_CONNECTED_BIT);
        board_port_set_wifi_status(s_wifi_ssid, false);
        if (!s_setup_portal_active && disconnected &&
            (disconnected->reason == WIFI_REASON_AUTH_FAIL ||
             disconnected->reason == WIFI_REASON_802_1X_AUTH_FAILED)) {
            // Authentication-class failures cannot be fixed by reconnecting:
            // the stored password or 802.1X credential is wrong. A handshake
            // timeout is usually transient (busy AP, weak signal) and stays
            // on the normal retry path below. Stop the
            // retry loop and point the user at the long-press re-setup flow
            // instead of burning radio time until the next reboot.
            s_wifi_disconnect_retry = 0;
            s_wifi_handshake_timeout_streak = 0;
            if (s_wifi_reconnect_timer) esp_timer_stop(s_wifi_reconnect_timer);
            ESP_LOGE(TAG, "Wi-Fi authentication failed (reason=%d): %s",
                     (int)disconnected->reason, s_wifi_ssid);
            // A full LCD repaint can block for too long in event-handler
            // context; let ui_request_worker own the alert screen.
            post_ui_request(UI_REQUEST_WIFI_AUTH_FAILED);
            return;
        }
        schedule_wifi_reconnect();
        // Five handshake timeouts in a row is almost always a wrong password
        // rather than radio noise; keep retrying, but tell the user how to
        // reconfigure. The streak reaches 5 exactly once per run, so this
        // posts a single hint. Any other disconnect reason resets the run.
        if (disconnected && disconnected->reason == WIFI_REASON_HANDSHAKE_TIMEOUT) {
            if (++s_wifi_handshake_timeout_streak == 5) {
                post_ui_request(UI_REQUEST_WIFI_AUTH_FAILED);
            }
        } else {
            s_wifi_handshake_timeout_streak = 0;
        }
        return;
    }
    if (base == IP_EVENT && id == IP_EVENT_STA_GOT_IP) {
        s_wifi_disconnect_retry = 0;
        s_wifi_handshake_timeout_streak = 0;
        xEventGroupSetBits(s_wifi_events, WIFI_CONNECTED_BIT);
        board_port_set_wifi_status(s_wifi_ssid, true);
        ESP_LOGI(TAG, "Wi-Fi connected to %s", s_wifi_ssid);
    }
}

static bool start_wifi(void) {
    init_network();
    ensure_station_netif();
    bool enterprise = is_enterprise_wifi();
    // An empty personal password means an open network: accept it (with a log
    // warning) instead of refusing to connect at all. WPA2 remains the floor
    // for any network that does have a password.
    bool open_network = !enterprise && !s_wifi_password[0];
    if (open_network) ESP_LOGW(TAG, "connecting to open Wi-Fi network: %s", s_wifi_ssid);
    wifi_config_t config = { .sta = { .threshold.authmode = enterprise ? WIFI_AUTH_WPA2_ENTERPRISE
                                      : open_network ? WIFI_AUTH_OPEN : WIFI_AUTH_WPA2_PSK } };
    strlcpy((char *)config.sta.ssid, s_wifi_ssid, sizeof(config.sta.ssid));
    if (!enterprise) strlcpy((char *)config.sta.password, s_wifi_password, sizeof(config.sta.password));
    ESP_ERROR_CHECK(esp_wifi_set_mode(s_setup_server ? WIFI_MODE_APSTA : WIFI_MODE_STA));
    ESP_ERROR_CHECK(esp_wifi_set_config(WIFI_IF_STA, &config));
    if (enterprise) {
        // Android/iOS-style defaults: PEAP + MSCHAPv2, username as identity
        // when anonymous identity is omitted, and platform trust anchors.
        // Never ESP_ERROR_CHECK these: a rejected EAP setting used to abort()
        // and reboot-loop the device forever. Treat it as a recoverable
        // configuration error and send the user to the setup portal instead.
        const char *identity = s_wifi_identity[0] ? s_wifi_identity : s_wifi_username;
        esp_eap_method_t method = !strcmp(s_wifi_eap_method, "ttls") ? ESP_EAP_TYPE_TTLS : ESP_EAP_TYPE_PEAP;
        esp_err_t eap_err = esp_eap_client_set_identity((const unsigned char *)identity, strlen(identity));
        if (eap_err == ESP_OK) eap_err = esp_eap_client_set_username((const unsigned char *)s_wifi_username, strlen(s_wifi_username));
        if (eap_err == ESP_OK) eap_err = esp_eap_client_set_password((const unsigned char *)s_wifi_password, strlen(s_wifi_password));
        if (eap_err == ESP_OK && !strcmp(s_wifi_eap_method, "ttls")) {
            eap_err = esp_eap_client_set_ttls_phase2_method(
                !strcmp(s_wifi_ttls_phase2, "pap") ? ESP_EAP_TTLS_PHASE2_PAP : ESP_EAP_TTLS_PHASE2_MSCHAPV2);
        }
        if (eap_err == ESP_OK && !strcmp(s_wifi_ca_mode, "system")) {
            eap_err = esp_eap_client_use_default_cert_bundle(true);
        }
        if (eap_err == ESP_OK && s_wifi_server_domain[0]) {
            eap_err = esp_eap_client_set_domain_name(s_wifi_server_domain);
        }
        if (eap_err == ESP_OK) eap_err = esp_eap_client_set_eap_methods(method);
        if (eap_err == ESP_OK) eap_err = esp_wifi_sta_enterprise_enable();
        if (eap_err != ESP_OK) {
            ESP_LOGE(TAG, "enterprise Wi-Fi (EAP) configuration failed: %s",
                     esp_err_to_name(eap_err));
            pet("alert");
            board_port_show_text("企业网配置错误", "请长按屏幕重新配网");
            // The pairing-recovery page only asks for a new code; a broken
            // enterprise configuration needs the full form so the user can
            // correct the EAP fields instead of re-pairing a working token.
            start_setup_portal(false);
            return false;
        }
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
    // app_main starts this task even when the saved Wi-Fi was unreachable at
    // boot, so the device joins the Hub as soon as the network comes back.
    // Wait for the event-group bit instead of hammering TLS without a route.
    // An unpaired device skips the wait and opens the setup portal directly:
    // the portal is served over the local AP and does not need the STA link.
    if ((s_pair_code[0] || s_gateway_token[0]) &&
        !(xEventGroupGetBits(s_wifi_events) & WIFI_CONNECTED_BIT)) {
        ESP_LOGI(TAG, "gateway startup: waiting for Wi-Fi to connect");
        xEventGroupWaitBits(s_wifi_events, WIFI_CONNECTED_BIT, pdFALSE, pdTRUE, portMAX_DELAY);
        ESP_LOGI(TAG, "gateway startup: Wi-Fi connected");
    }
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
    ESP_LOGW(TAG, "boot reset reason=%d", (int)esp_reset_reason());
    esp_err_t nvs_err = nvs_flash_init();
    if (nvs_err == ESP_ERR_NVS_NO_FREE_PAGES || nvs_err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_ERROR_CHECK(nvs_flash_erase());
        nvs_err = nvs_flash_init();
    }
	ESP_ERROR_CHECK(nvs_err);
	ESP_ERROR_CHECK(psa_crypto_init() == PSA_SUCCESS ? ESP_OK : ESP_FAIL);
	s_storage_mutex = xSemaphoreCreateMutex();
	ESP_ERROR_CHECK(s_storage_mutex ? ESP_OK : ESP_ERR_NO_MEM);
	(void)mount_meeting_storage();
    load_meeting_recovery();
    s_http_mutex = xSemaphoreCreateMutex();
    ESP_ERROR_CHECK(s_http_mutex ? ESP_OK : ESP_ERR_NO_MEM);
    s_foreground_http_client_mutex = xSemaphoreCreateMutex();
    ESP_ERROR_CHECK(s_foreground_http_client_mutex ? ESP_OK : ESP_ERR_NO_MEM);
    s_command_cancel_ui_ready = xSemaphoreCreateBinary();
    ESP_ERROR_CHECK(s_command_cancel_ui_ready ? ESP_OK : ESP_ERR_NO_MEM);
    ESP_ERROR_CHECK(xTaskCreate(command_cancel_worker, "maclaw_cancel", 4096, NULL, 6,
                                &s_command_cancel_task) == pdPASS
                        ? ESP_OK : ESP_ERR_NO_MEM);
    // Runs deferred UI work for the touch scan task (status pages, the setup
    // portal transition). Must exist before board_port_init() starts the
    // button scanner.
    ESP_ERROR_CHECK(xTaskCreate(ui_request_worker, "maclaw_ui_request", 4096, NULL, 5,
                                &s_ui_request_task) == pdPASS
                        ? ESP_OK : ESP_ERR_NO_MEM);
    s_nvs_mutex = xSemaphoreCreateMutex();
    ESP_ERROR_CHECK(s_nvs_mutex ? ESP_OK : ESP_ERR_NO_MEM);
    // A foreground interaction starts in the button callback but finishes in
    // its worker task, therefore mutual exclusion must use a binary semaphore
    // rather than an ownership-tracked mutex.
    s_interaction_lock = xSemaphoreCreateBinary();
    ESP_ERROR_CHECK(s_interaction_lock ? ESP_OK : ESP_ERR_NO_MEM);
    ESP_ERROR_CHECK(xSemaphoreGive(s_interaction_lock) == pdTRUE ? ESP_OK : ESP_FAIL);
	init_boot_session_id();
    load_device_config();
    load_volume();
    load_screen_timeout();
	load_gateway_token();
	load_desired_pet_asset_revision();
	load_ambient_weather();
#if CONFIG_MACLAW_BATTERY_ADC_ENABLE
    battery_monitor_init();
#endif
    // Register before board_port_init() creates the settings and button
    // tasks so no settings event raised during init is silently dropped.
	board_port_set_settings_callback(on_board_settings, NULL);
	ESP_ERROR_CHECK(board_port_init(on_user_button, NULL));
	// Restore the last GUI-rendered character before any network work. A cached
	// asset keeps the selected pet visible while Wi-Fi/Hub are unavailable; the
	// next authenticated handshake refreshes it when the GUI revision changes.
	esp_err_t cached_pet_err = load_cached_pet_asset();
	if (cached_pet_err != ESP_OK && cached_pet_err != ESP_ERR_NOT_FOUND &&
		cached_pet_err != ESP_ERR_INVALID_STATE) {
		ESP_LOGW(TAG, "cached pet restore failed; native skin remains active: %s",
				 esp_err_to_name(cached_pet_err));
	}
	// A clean native pet is the only startup display until Wi-Fi is ready.
    // No transient messages are painted on top of it.
    pet("idle");
    if (!s_wifi_ssid[0]) {
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
    if (!start_wifi()) {
        if (s_setup_portal_active) {
            // start_wifi() already entered a recovery portal (e.g. unusable
            // enterprise EAP settings). Keep its screen and flow untouched
            // instead of painting a generic network error over it.
            return;
        }
        pet("alert");
        ESP_LOGW(TAG, "saved Wi-Fi is currently unavailable; preserving configuration and retrying in station mode");
        board_port_show_text("网络暂时不可用", "配置已保留，正在自动重连");
        start_clock_sync();
        // Previously this branch returned without creating the gateway startup
        // task, so a device that booted while its saved Wi-Fi was out of range
        // never joined the Hub until a manual reboot — even though the Wi-Fi
        // driver reconnected on its own minutes later. The startup task waits
        // for WIFI_CONNECTED_BIT before its first handshake and then retries
        // with its regular exponential backoff.
        BaseType_t created = xTaskCreatePinnedToCore(gateway_startup_task,
                                                    "maclaw_gateway_startup",
                                                    12288, NULL, 4,
                                                    &s_gateway_task, 1);
        if (created != pdPASS) {
            s_gateway_task = NULL;
            ESP_LOGE(TAG, "failed to create gateway startup task after Wi-Fi timeout");
        }
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
