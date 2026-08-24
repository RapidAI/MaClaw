#include "services/gateway_transport.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>

#include "cJSON.h"
#include "esp_crt_bundle.h"
#include "esp_err.h"
#include "esp_heap_caps.h"
#include "esp_http_client.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "app_ui.h"
#include "presentation/scene_presenter.h"
#include "services/ambient_service.h"
#include "firmware_identity.h"
#include "services/command_service.h"
#include "services/gateway_dispatcher.h"
#include "services/interaction_service.h"
#include "services/meeting_service.h"
#include "task_registry.h"

/* Keep the log tag identical to the original main.c owner so existing gateway
 * / handshake / pairing trace filters and hardware baseline comparisons stay
 * valid. */
static const char *TAG = "maclaw_client";

#define RESPONSE_CAPACITY 16384
#define HANDSHAKE_RESPONSE_CAPACITY 24576
/* The unified tool registry makes the handshake descriptor larger than the
 * former 4 KiB stack buffer.  Rejecting it before the HTTP request leaves a
 * paired device permanently on its boot surface, so keep an explicit bounded
 * request capacity for this control-plane payload. */
#define HANDSHAKE_REQUEST_CAPACITY 8192
#define URL_CAPACITY 256
#define GATEWAY_RETRY_INITIAL_MS 2000
#define GATEWAY_RETRY_MAX_MS 60000
#define GATEWAY_STAGED_PROVISIONING_CONFIRM_DEADLINE_MS (10u * 60u * 1000u)
#define TRANSPORT_PAIR_CODE_CAPACITY 7
#define RESPONSE_IMAGE_MIME "application/vnd.maclaw.rgb565be"
#define MEETING_STREAM_RESPONSE_CAPACITY 2048
#define MEETING_STREAM_INTERNAL_TLS_RESERVE (16U * 1024U)

static portMUX_TYPE s_transport_state_lock = portMUX_INITIALIZER_UNLOCKED;

static char s_gateway_token[96];
static char s_gateway_url[URL_CAPACITY];
static char s_device_id[40];
/* This is only the active boot's copy of Configuration's one-time pairing
 * value. Configuration remains its durable owner; Gateway clears this local
 * copy only after Configuration has atomically committed Hub token evidence. */
static char s_pair_code[TRANSPORT_PAIR_CODE_CAPACITY];
/* Foreground traffic must never wait behind the outgoing long poll. Each lane
 * owns both its mutex and persistent esp_http_client handle; no handle is ever
 * operated by two tasks concurrently. */
static SemaphoreHandle_t s_http_mutex;
static esp_http_client_handle_t s_gateway_http_client;
static char s_gateway_http_origin[URL_CAPACITY];
static SemaphoreHandle_t s_gateway_poll_http_mutex;
static esp_http_client_handle_t s_gateway_poll_http_client;
static char s_gateway_poll_http_origin[URL_CAPACITY];
static SemaphoreHandle_t s_gateway_asset_http_mutex;
static esp_http_client_handle_t s_gateway_asset_http_client;
static char s_gateway_asset_http_origin[URL_CAPACITY];
/* Every active pointer is protected through cancellation and cleanup by its
 * dedicated registry guard. The pointers are private to this source
 * owner: callers receive only a lane bit and a bounded result. */
static SemaphoreHandle_t s_active_clients_mutex;
static esp_http_client_handle_t s_active_startup_client;
static esp_http_client_handle_t s_active_capability_refresh_client;
static esp_http_client_handle_t s_active_foreground_client;
static esp_http_client_handle_t s_active_poll_client;
static esp_http_client_handle_t s_active_asset_client;
/* The resumable meeting PUT is structurally different from a buffered
 * request, but it is still a Gateway Transport-owned HTTP lane.  Its owner
 * identity makes a late worker stop unable to cancel a successor's stream. */
static esp_http_client_handle_t s_active_meeting_stream_client;
static const void *s_active_meeting_stream_owner;
static esp_http_client_handle_t s_meeting_stream_reusable_client;
static TaskHandle_t s_gateway_task;
static volatile bool s_gateway_startup_running;
static SemaphoreHandle_t s_gateway_startup_start_gate;
static SemaphoreHandle_t s_gateway_startup_stopped;
static bool s_gateway_startup_stop_requested;
static bool s_gateway_startup_starting;
/* This is a worker-lifecycle fence, independent from Connectivity's request
 * admission fence. It prevents a late startup retry from recreating the
 * worker between PREPARE's bounded stop and Power's mandatory rollback. */
static bool s_system_sleep_preparing;
static bool s_system_sleep_restart_startup;
/* A bounded stop can observe completion before the old task has removed its
 * immutable Registry entry.  Leave replacement creation to that retiring
 * generation so ABORT never overlaps completion/Registry identities. */
static bool s_system_sleep_restart_pending;
static bool s_gateway_startup_retiring;
/* A completed startup coordinator still belongs to its old Connectivity
 * generation until the immutable Registry identity is gone. */
static esp_err_t s_gateway_startup_exit_status = ESP_OK;
static bool s_gateway_startup_registry_retirement_failed;

/* B5: local effective facts, Hub acceptance and observed availability are
 * three different layers.  This is kept with the Gateway transport's
 * handshake owner rather than duplicated by board profiles or consumers. */
static gateway_capability_projection_t s_capability_projection;

static gateway_transport_host_t s_host;
static bool s_host_installed;

typedef enum {
    GATEWAY_ACTIVE_LANE_STARTUP = 0,
    GATEWAY_ACTIVE_LANE_CAPABILITY_REFRESH,
    GATEWAY_ACTIVE_LANE_FOREGROUND,
    GATEWAY_ACTIVE_LANE_POLL,
    GATEWAY_ACTIVE_LANE_ASSET,
    GATEWAY_ACTIVE_LANE_MEETING_STREAM,
} gateway_active_lane_t;

static bool gateway_auth_failed(const gateway_transport_response_t *response,
                                esp_err_t err);
static esp_err_t stop_gateway_startup_task(uint32_t timeout_ms);
static esp_err_t on_http_event(esp_http_client_event_t *event);
static device_status_t cancel_active_client_locked(esp_http_client_handle_t client,
                                                   const char *lane);

static esp_http_client_handle_t *active_client_slot(
    gateway_active_lane_t lane) {
    switch (lane) {
        case GATEWAY_ACTIVE_LANE_STARTUP: return &s_active_startup_client;
        case GATEWAY_ACTIVE_LANE_CAPABILITY_REFRESH: return &s_active_capability_refresh_client;
        case GATEWAY_ACTIVE_LANE_FOREGROUND: return &s_active_foreground_client;
        case GATEWAY_ACTIVE_LANE_POLL: return &s_active_poll_client;
        case GATEWAY_ACTIVE_LANE_ASSET: return &s_active_asset_client;
        case GATEWAY_ACTIVE_LANE_MEETING_STREAM: return &s_active_meeting_stream_client;
        default: return NULL;
    }
}

static void publish_meeting_stream_client(esp_http_client_handle_t client,
                                          const void *owner) {
    if (!s_active_clients_mutex) return;
    xSemaphoreTake(s_active_clients_mutex, portMAX_DELAY);
    s_active_meeting_stream_client = client;
    s_active_meeting_stream_owner = owner;
    xSemaphoreGive(s_active_clients_mutex);
}

static void clear_meeting_stream_client(esp_http_client_handle_t client) {
    if (!s_active_clients_mutex) return;
    xSemaphoreTake(s_active_clients_mutex, portMAX_DELAY);
    if (s_active_meeting_stream_client == client) {
        s_active_meeting_stream_client = NULL;
        s_active_meeting_stream_owner = NULL;
    }
    xSemaphoreGive(s_active_clients_mutex);
}

static void publish_active_client(gateway_active_lane_t lane,
                                  esp_http_client_handle_t client) {
    esp_http_client_handle_t *slot = active_client_slot(lane);
    if (!slot || !s_active_clients_mutex) return;
    xSemaphoreTake(s_active_clients_mutex, portMAX_DELAY);
    *slot = client;
    xSemaphoreGive(s_active_clients_mutex);
}

static void clear_active_client(gateway_active_lane_t lane,
                                esp_http_client_handle_t client) {
    esp_http_client_handle_t *slot = active_client_slot(lane);
    if (!slot || !s_active_clients_mutex) return;
    xSemaphoreTake(s_active_clients_mutex, portMAX_DELAY);
    if (*slot == client) *slot = NULL;
    xSemaphoreGive(s_active_clients_mutex);
}

static device_status_t transport_status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
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

static gateway_capability_flags_t local_gateway_capabilities(void) {
    return GATEWAY_CAPABILITY_INPUT_TEXT | GATEWAY_CAPABILITY_INPUT_AUDIO |
           GATEWAY_CAPABILITY_OUTPUT_TEXT | GATEWAY_CAPABILITY_OUTPUT_AUDIO |
           GATEWAY_CAPABILITY_OUTPUT_IMAGE | GATEWAY_CAPABILITY_PET_STATE |
           GATEWAY_CAPABILITY_PET_ANIMATION | GATEWAY_CAPABILITY_PET_ASSET |
           GATEWAY_CAPABILITY_AMBIENT_DISPLAY |
           GATEWAY_CAPABILITY_MEETING_RECORDER |
           GATEWAY_CAPABILITY_VOLUME_CONTROL |
           GATEWAY_CAPABILITY_BRIGHTNESS_CONTROL |
           GATEWAY_CAPABILITY_SCREEN_SLEEP_CONTROL;
}

static bool add_local_gateway_capabilities(cJSON *capabilities) {
    if (!capabilities) return false;
    const gateway_capability_flags_t local = local_gateway_capabilities();
    cJSON *input = cJSON_AddObjectToObject(capabilities, "input");
    cJSON *input_modalities = input ? cJSON_AddArrayToObject(input, "modalities") : NULL;
    if (!input_modalities ||
        ((local & GATEWAY_CAPABILITY_INPUT_TEXT) &&
         !cJSON_AddItemToArray(input_modalities, cJSON_CreateString("text"))) ||
        ((local & GATEWAY_CAPABILITY_INPUT_AUDIO) &&
         !cJSON_AddItemToArray(input_modalities, cJSON_CreateString("audio")))) {
        return false;
    }
    if (local & GATEWAY_CAPABILITY_INPUT_AUDIO) {
        cJSON *input_audio = cJSON_AddObjectToObject(input, "audio");
        if (!input_audio ||
            !cJSON_AddItemToObject(input_audio, "mimeTypes",
                                   cJSON_CreateStringArray((const char *[]){"audio/wav"}, 1)) ||
            !cJSON_AddItemToObject(input_audio, "sampleRates",
                                   cJSON_CreateIntArray((const int[]){16000}, 1)) ||
            !cJSON_AddNumberToObject(input_audio, "channels", 1)) {
            return false;
        }
    }
    cJSON *output = cJSON_AddObjectToObject(capabilities, "output");
    cJSON *output_modalities = output ? cJSON_AddArrayToObject(output, "modalities") : NULL;
    if (!output_modalities ||
        ((local & GATEWAY_CAPABILITY_OUTPUT_TEXT) &&
         !cJSON_AddItemToArray(output_modalities, cJSON_CreateString("text"))) ||
        ((local & GATEWAY_CAPABILITY_OUTPUT_AUDIO) &&
         !cJSON_AddItemToArray(output_modalities, cJSON_CreateString("audio"))) ||
        ((local & GATEWAY_CAPABILITY_OUTPUT_IMAGE) &&
         !cJSON_AddItemToArray(output_modalities, cJSON_CreateString("image")))) {
        return false;
    }
    if ((local & (GATEWAY_CAPABILITY_OUTPUT_TEXT |
                  GATEWAY_CAPABILITY_OUTPUT_AUDIO |
                  GATEWAY_CAPABILITY_OUTPUT_IMAGE)) ==
            (GATEWAY_CAPABILITY_OUTPUT_TEXT |
             GATEWAY_CAPABILITY_OUTPUT_AUDIO |
             GATEWAY_CAPABILITY_OUTPUT_IMAGE)) {
        if (!cJSON_AddItemToObject(output, "preferred",
                                   cJSON_CreateStringArray((const char *[]){"audio", "image", "text"}, 3))) {
            return false;
        }
        cJSON *combinations = cJSON_AddArrayToObject(output, "combinations");
        if (!combinations ||
            !cJSON_AddItemToArray(combinations,
                                  cJSON_CreateStringArray((const char *[]){"text"}, 1)) ||
            !cJSON_AddItemToArray(combinations,
                                  cJSON_CreateStringArray((const char *[]){"audio", "text"}, 2)) ||
            !cJSON_AddItemToArray(combinations,
                                  cJSON_CreateStringArray((const char *[]){"image"}, 1))) {
            return false;
        }
    }
    if (local & GATEWAY_CAPABILITY_OUTPUT_TEXT) {
        cJSON *output_text = cJSON_AddObjectToObject(output, "text");
        if (!output_text || !cJSON_AddNumberToObject(output_text, "maxChars", 240) ||
            !cJSON_AddBoolToObject(output_text, "markdown", false) ||
            !cJSON_AddStringToObject(output_text, "locale", "zh-CN")) return false;
    }
    if (local & GATEWAY_CAPABILITY_OUTPUT_AUDIO) {
        cJSON *output_audio = cJSON_AddObjectToObject(output, "audio");
        if (!output_audio ||
            !cJSON_AddItemToObject(output_audio, "mimeTypes",
                                   cJSON_CreateStringArray((const char *[]){"audio/wav", "audio/mpeg", "audio/mp3"}, 3)) ||
            !cJSON_AddItemToObject(output_audio, "sampleRates",
                                   cJSON_CreateIntArray((const int[]){16000, 22050, 24000, 32000, 44100, 48000}, 6)) ||
            !cJSON_AddNumberToObject(output_audio, "channels", 2) ||
            !cJSON_AddBoolToObject(output_audio, "playback", true) ||
            !cJSON_AddItemToObject(output_audio, "deliveryModes",
                                   cJSON_CreateStringArray((const char *[]){"inline", "url"}, 2)) ||
            !cJSON_AddNumberToObject(output_audio, "maxInlineBytes", 8192) ||
            !cJSON_AddNumberToObject(output_audio, "maxDownloadBytes", 524288)) return false;
    }
    if (local & GATEWAY_CAPABILITY_OUTPUT_IMAGE) {
        cJSON *output_image = cJSON_AddObjectToObject(output, "image");
        if (!output_image ||
            !cJSON_AddItemToObject(output_image, "mimeTypes",
                                   cJSON_CreateStringArray((const char *[]){RESPONSE_IMAGE_MIME}, 1)) ||
            !cJSON_AddNumberToObject(output_image, "maxWidth", 64) ||
            !cJSON_AddNumberToObject(output_image, "maxHeight", 64) ||
            !cJSON_AddBoolToObject(output_image, "animated", false)) return false;
    }
    cJSON *features = cJSON_AddObjectToObject(capabilities, "features");
    struct { const char *name; gateway_capability_flags_t flag; } const feature_flags[] = {
        {"petStates", GATEWAY_CAPABILITY_PET_STATE},
        {"petAnimation", GATEWAY_CAPABILITY_PET_ANIMATION},
        {"petAsset", GATEWAY_CAPABILITY_PET_ASSET},
        {"ambientDisplay", GATEWAY_CAPABILITY_AMBIENT_DISPLAY},
        {"meetingRecorder", GATEWAY_CAPABILITY_MEETING_RECORDER},
        {"volumeControl", GATEWAY_CAPABILITY_VOLUME_CONTROL},
        {"brightnessControl", GATEWAY_CAPABILITY_BRIGHTNESS_CONTROL},
        {"screenSleepControl", GATEWAY_CAPABILITY_SCREEN_SLEEP_CONTROL},
    };
    if (!features) return false;
    for (size_t i = 0; i < sizeof(feature_flags) / sizeof(feature_flags[0]); ++i) {
        if (!cJSON_AddBoolToObject(features, feature_flags[i].name,
                                   (local & feature_flags[i].flag) != 0)) return false;
    }
    if ((local & GATEWAY_CAPABILITY_PET_ASSET) &&
        !cJSON_AddNumberToObject(features, "petAssetMaxFrames", 8)) return false;
    return true;
}

static bool json_array_has_string(cJSON *array, const char *value) {
    if (!array || !value) return false;
    cJSON *item = NULL;
    cJSON_ArrayForEach(item, array) {
        if (cJSON_IsString(item) && item->valuestring &&
            strcmp(item->valuestring, value) == 0) {
            return true;
        }
    }
    return false;
}

/* capabilitiesAccepted is an authorization contract, not a best-effort UI
 * hint.  Check array shape before projecting its known values so a proxy or
 * an incompatible/old Hub cannot turn a malformed field into a misleading
 * empty capability set that looks like a valid acceptance. */
static bool json_array_is_string_list(cJSON *array) {
    if (!cJSON_IsArray(array)) return false;
    cJSON *item = NULL;
    cJSON_ArrayForEach(item, array) {
        if (!cJSON_IsString(item) || !item->valuestring) return false;
    }
    return true;
}

/* Convert only the explicitly returned Hub acceptance schema to a plain bit
 * set.  Missing fields mean not accepted; they do not silently inherit what
 * this firmware advertised.  The normalized Hub contract always contains an
 * output modality list (at least text for a legacy client); require that
 * mandatory shape, but keep input/features optional because their empty Go
 * structs are deliberately omitted by json:\"omitempty\".  This makes an
 * older/partial or malformed Hub response fail closed without leaking cJSON
 * outside the transport parser. */
static bool parse_accepted_gateway_capabilities(
    cJSON *accepted, gateway_capability_flags_t *out_capabilities) {
    if (!cJSON_IsObject(accepted) || !out_capabilities) return false;
    gateway_capability_flags_t flags = 0;
    cJSON *input = cJSON_GetObjectItemCaseSensitive(accepted, "input");
    if (input && !cJSON_IsObject(input)) return false;
    cJSON *input_modalities = input
        ? cJSON_GetObjectItemCaseSensitive(input, "modalities") : NULL;
    if (input_modalities && !json_array_is_string_list(input_modalities)) return false;
    if (json_array_has_string(input_modalities, "text")) {
        flags |= GATEWAY_CAPABILITY_INPUT_TEXT;
    }
    if (json_array_has_string(input_modalities, "audio")) {
        flags |= GATEWAY_CAPABILITY_INPUT_AUDIO;
    }
    cJSON *output = cJSON_GetObjectItemCaseSensitive(accepted, "output");
    if (!cJSON_IsObject(output)) return false;
    cJSON *output_modalities = cJSON_GetObjectItemCaseSensitive(output, "modalities");
    if (!json_array_is_string_list(output_modalities)) return false;
    if (json_array_has_string(output_modalities, "text")) {
        flags |= GATEWAY_CAPABILITY_OUTPUT_TEXT;
    }
    if (json_array_has_string(output_modalities, "audio")) {
        flags |= GATEWAY_CAPABILITY_OUTPUT_AUDIO;
    }
    if (json_array_has_string(output_modalities, "image")) {
        flags |= GATEWAY_CAPABILITY_OUTPUT_IMAGE;
    }
    cJSON *features = cJSON_GetObjectItemCaseSensitive(accepted, "features");
    if (features && !cJSON_IsObject(features)) return false;
    struct {
        const char *name;
        gateway_capability_flags_t flag;
    } const feature_flags[] = {
        {"petStates", GATEWAY_CAPABILITY_PET_STATE},
        {"petAnimation", GATEWAY_CAPABILITY_PET_ANIMATION},
        {"petAsset", GATEWAY_CAPABILITY_PET_ASSET},
        {"ambientDisplay", GATEWAY_CAPABILITY_AMBIENT_DISPLAY},
        {"meetingRecorder", GATEWAY_CAPABILITY_MEETING_RECORDER},
        {"volumeControl", GATEWAY_CAPABILITY_VOLUME_CONTROL},
        {"brightnessControl", GATEWAY_CAPABILITY_BRIGHTNESS_CONTROL},
        {"screenSleepControl", GATEWAY_CAPABILITY_SCREEN_SLEEP_CONTROL},
    };
    for (size_t i = 0; i < sizeof(feature_flags) / sizeof(feature_flags[0]); ++i) {
        cJSON *feature = features
            ? cJSON_GetObjectItemCaseSensitive(features, feature_flags[i].name)
            : NULL;
        if (cJSON_IsTrue(feature)) flags |= feature_flags[i].flag;
        else if (feature && !cJSON_IsFalse(feature)) return false;
    }
    *out_capabilities = flags;
    return true;
}

static void observe_capability_transport_result(bool success) {
    bool meeting_operational = false;
    taskENTER_CRITICAL(&s_transport_state_lock);
    (void)gateway_capability_projection_observe_transport_result(
        &s_capability_projection, success);
    meeting_operational = (s_capability_projection.operational_capabilities &
                           GATEWAY_CAPABILITY_MEETING_RECORDER) != 0;
    taskEXIT_CRITICAL(&s_transport_state_lock);
    meeting_service_set_capability_operational(meeting_operational);
}

/* Rollback children receive only what remains of the parent's monotonic
 * deadline.  Round up a non-zero remainder so a child can observe and return
 * a real timeout instead of treating a sub-millisecond budget as invalid. */
static uint32_t remaining_timeout_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const uint64_t rounded_ms = ((uint64_t)remaining_us + 999u) / 1000u;
    return rounded_ms > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded_ms;
}

/* Pairing token commit: persistence stays with the composition root; the
 * in-memory authority updates only after the durable write succeeds. */
static esp_err_t transport_persist_token(const char *token) {
    if (!token || !token[0] || strlen(token) >= sizeof(s_gateway_token)) {
        return ESP_ERR_INVALID_SIZE;
    }
    int32_t err = s_host.persist_gateway_token(token);
    if (err == 0) {
        taskENTER_CRITICAL(&s_transport_state_lock);
        strlcpy(s_gateway_token, token, sizeof(s_gateway_token));
        s_pair_code[0] = '\0';
        taskEXIT_CRITICAL(&s_transport_state_lock);
    }
    return (esp_err_t)err;
}

bool gateway_transport_is_paired(void) {
    bool paired;
    taskENTER_CRITICAL(&s_transport_state_lock);
    paired = s_gateway_token[0] != '\0';
    taskEXIT_CRITICAL(&s_transport_state_lock);
    return paired;
}

bool gateway_transport_pairing_pending(void) {
    bool pending;
    taskENTER_CRITICAL(&s_transport_state_lock);
    pending = s_pair_code[0] != '\0';
    taskEXIT_CRITICAL(&s_transport_state_lock);
    return pending;
}

const char *gateway_transport_device_id(void) {
    return s_device_id;
}

void gateway_transport_set_device_id(const char *device_id) {
    taskENTER_CRITICAL(&s_transport_state_lock);
    strlcpy(s_device_id, device_id ? device_id : "", sizeof(s_device_id));
    taskEXIT_CRITICAL(&s_transport_state_lock);
}

void gateway_transport_set_gateway_credentials(const char *gateway_url,
                                               const char *gateway_token,
                                               const char *pair_code) {
    taskENTER_CRITICAL(&s_transport_state_lock);
    strlcpy(s_gateway_url, gateway_url ? gateway_url : "", sizeof(s_gateway_url));
    strlcpy(s_gateway_token, gateway_token ? gateway_token : "", sizeof(s_gateway_token));
    strlcpy(s_pair_code, pair_code ? pair_code : "", sizeof(s_pair_code));
    taskEXIT_CRITICAL(&s_transport_state_lock);
}

bool gateway_transport_get_capability_projection(
    gateway_capability_projection_t *out_projection) {
    if (!out_projection) return false;
    taskENTER_CRITICAL(&s_transport_state_lock);
    const bool valid = gateway_capability_projection_snapshot(
        &s_capability_projection, out_projection);
    taskEXIT_CRITICAL(&s_transport_state_lock);
    return valid;
}

bool gateway_transport_capabilities_operational(
    gateway_capability_flags_t required_capabilities) {
    if (required_capabilities == 0 ||
        (required_capabilities & ~GATEWAY_CAPABILITY_KNOWN_MASK) != 0) {
        return false;
    }
    gateway_capability_projection_t snapshot;
    if (!gateway_transport_get_capability_projection(&snapshot)) return false;
    return (snapshot.operational_capabilities & required_capabilities) ==
           required_capabilities;
}

bool gateway_transport_capture_capability_lease(
    gateway_capability_flags_t required_capabilities,
    gateway_capability_lease_t *out_lease) {
    if (!out_lease) return false;
    bool captured;
    taskENTER_CRITICAL(&s_transport_state_lock);
    captured = gateway_capability_projection_capture_lease(
        &s_capability_projection, required_capabilities, out_lease);
    taskEXIT_CRITICAL(&s_transport_state_lock);
    return captured;
}

bool gateway_transport_capability_lease_current(
    const gateway_capability_lease_t *lease) {
    bool current;
    taskENTER_CRITICAL(&s_transport_state_lock);
    current = gateway_capability_projection_lease_current(
        &s_capability_projection, lease);
    taskEXIT_CRITICAL(&s_transport_state_lock);
    return current;
}

void gateway_transport_observe_capability_control_plane_success(void) {
    observe_capability_transport_result(true);
}

uint32_t gateway_transport_gateway_url(char *out, uint32_t capacity) {
    taskENTER_CRITICAL(&s_transport_state_lock);
    uint32_t len = (uint32_t)strlen(s_gateway_url);
    strlcpy(out, s_gateway_url, capacity);
    taskEXIT_CRITICAL(&s_transport_state_lock);
    return len;
}

uint32_t gateway_transport_bearer_authorization(char *out, uint32_t capacity) {
    taskENTER_CRITICAL(&s_transport_state_lock);
    int n = snprintf(out, capacity, "Bearer %s", s_gateway_token);
    taskEXIT_CRITICAL(&s_transport_state_lock);
    return n > 0 ? (uint32_t)n : 0;
}

static bool meeting_stream_stop_requested(
    const gateway_transport_stream_request_t *request) {
    return request && request->stop_requested &&
           request->stop_requested(request->stop_context);
}

static void meeting_stream_publish_progress(
    const gateway_transport_stream_request_t *request, uint32_t transferred) {
    if (request && request->progress) {
        request->progress(request->progress_context, transferred);
    }
}

static device_status_t read_meeting_stream_body(void *context, void *buffer,
                                                 uint32_t requested,
                                                 uint32_t *out_read) {
    gateway_transport_stream_request_t *request = context;
    if (!request || !request->storage_context || !request->read_range || !buffer ||
        !out_read) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    const device_status_t status = request->read_range(
        request->storage_context, request->offset, buffer, requested, out_read);
    if (status == DEVICE_STATUS_OK && *out_read == requested) {
        request->offset += *out_read;
        return DEVICE_STATUS_OK;
    }
    return status == DEVICE_STATUS_OK ? DEVICE_STATUS_IO_ERROR : status;
}

int32_t gateway_transport_stream_meeting_chunk(
    const gateway_transport_stream_request_t *request) {
    if (!request || !request->path || !request->sha256_hex ||
        !request->storage_context || !request->read_range || !request->io_buffer ||
        request->io_buffer_size == 0 || request->length == 0 ||
        meeting_stream_stop_requested(request)) {
        return ESP_ERR_INVALID_ARG;
    }
    char gateway_url[URL_CAPACITY];
    (void)gateway_transport_gateway_url(gateway_url, sizeof(gateway_url));
    char url[URL_CAPACITY];
    const int url_len = snprintf(url, sizeof(url), "%s%s", gateway_url,
                                 request->path);
    if (url_len <= 0 || url_len >= (int)sizeof(url)) return ESP_ERR_INVALID_SIZE;

    const bool cellular = device_connectivity_is_active_cellular();
    if (cellular) {
        gateway_transport_response_t response = {0};
        response.data = heap_caps_malloc(MEETING_STREAM_RESPONSE_CAPACITY,
                                         MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!response.data) response.data = malloc(MEETING_STREAM_RESPONSE_CAPACITY);
        if (!response.data) return ESP_ERR_NO_MEM;
        response.capacity = MEETING_STREAM_RESPONSE_CAPACITY;
        char authorization[128];
        uint32_t response_len = 0;
        gateway_transport_stream_request_t reader = *request;
        (void)gateway_transport_bearer_authorization(authorization,
                                                      sizeof(authorization));
        device_connectivity_stream_request_t cellular_request = {
            .request = {
                .method = "PUT", .url = url, .content_type = "application/octet-stream",
                .authorization = authorization, .extra_header_name = "X-Chunk-SHA256",
                .extra_header_value = request->sha256_hex, .body_len = request->length,
                .response = response.data, .response_capacity = response.capacity,
                .response_len = &response_len, .status_code = &response.status,
                .truncated = &response.truncated, .timeout_ms = 60000,
                .cancellation_owner = (const void *)xTaskGetCurrentTaskHandle(),
            },
            .body_reader = read_meeting_stream_body, .body_reader_context = &reader,
            .stream_buffer = request->io_buffer,
            .stream_buffer_size = request->io_buffer_size,
        };
        esp_err_t err = device_status_to_platform_error(
            device_connectivity_cellular_http_stream_request(&cellular_request));
        response.len = response_len;
        if (meeting_stream_stop_requested(request) && err == ESP_OK) {
            err = ESP_ERR_INVALID_STATE;
        }
        if (err == ESP_OK && (response.status < 200 || response.status >= 300)) {
            err = ESP_FAIL;
        }
        gateway_transport_response_release(&response);
        return err;
    }

    device_status_t admission = device_connectivity_begin_network_request();
    if (admission != DEVICE_STATUS_OK) return device_status_to_platform_error(admission);
    if (!gateway_transport_general_lane_lock(35000)) {
        device_connectivity_end_network_request();
        return ESP_ERR_TIMEOUT;
    }
    esp_err_t err = ESP_OK;
    gateway_transport_response_t response = {0};
    void *tls_reserve = NULL;
    esp_http_client_handle_t client = s_meeting_stream_reusable_client;
    const bool reused = client != NULL;
    if (!client) {
        tls_reserve = heap_caps_malloc(MEETING_STREAM_INTERNAL_TLS_RESERVE,
                                       MALLOC_CAP_INTERNAL | MALLOC_CAP_DMA |
                                       MALLOC_CAP_8BIT);
        if (!tls_reserve) err = ESP_ERR_NO_MEM;
    }
    if (err == ESP_OK) {
        response.data = heap_caps_malloc(MEETING_STREAM_RESPONSE_CAPACITY,
                                         MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!response.data) response.data = malloc(MEETING_STREAM_RESPONSE_CAPACITY);
        if (!response.data) err = ESP_ERR_NO_MEM;
        else response.capacity = MEETING_STREAM_RESPONSE_CAPACITY;
    }
    if (err == ESP_OK && !client) {
        client = esp_http_client_init(&(esp_http_client_config_t){
            .url = url, .event_handler = on_http_event, .user_data = &response,
            .timeout_ms = 60000, .crt_bundle_attach = esp_crt_bundle_attach,
            .keep_alive_enable = true,
        });
        if (!client) err = ESP_ERR_NO_MEM;
    }
    if (err == ESP_OK) {
        char authorization[128];
        (void)gateway_transport_bearer_authorization(authorization,
                                                      sizeof(authorization));
        err = esp_http_client_set_url(client, url);
        if (err == ESP_OK) err = esp_http_client_set_user_data(client, &response);
        if (err == ESP_OK) err = esp_http_client_set_timeout_ms(client, 60000);
        if (err == ESP_OK) err = esp_http_client_set_method(client, HTTP_METHOD_PUT);
        if (err == ESP_OK) err = esp_http_client_set_header(
            client, "Content-Type", "application/octet-stream");
        if (err == ESP_OK) err = esp_http_client_set_header(
            client, "X-Chunk-SHA256", request->sha256_hex);
        if (err == ESP_OK) err = esp_http_client_set_header(client, "Accept", "application/json");
        if (err == ESP_OK) err = esp_http_client_delete_header(client, "Connection");
        if (err == ESP_OK) err = esp_http_client_set_header(client, "Authorization", authorization);
    }
    if (err == ESP_OK) {
        publish_meeting_stream_client(client, xTaskGetCurrentTaskHandle());
        err = esp_http_client_open(client, (int)request->length);
    }
    heap_caps_free(tls_reserve);
    uint32_t transferred = 0;
    while (err == ESP_OK && transferred < request->length) {
        if (meeting_stream_stop_requested(request)) {
            err = ESP_ERR_INVALID_STATE;
            break;
        }
        const uint32_t remaining = request->length - transferred;
        const uint32_t wanted = remaining < request->io_buffer_size ?
                                    remaining : request->io_buffer_size;
        uint32_t read = 0;
        device_status_t read_status = request->read_range(
            request->storage_context, request->offset + transferred,
            request->io_buffer, wanted, &read);
        if (read_status != DEVICE_STATUS_OK || read != wanted) {
            err = ESP_FAIL;
            break;
        }
        uint32_t written = 0;
        while (written < read) {
            int result = esp_http_client_write(client,
                                                (const char *)request->io_buffer + written,
                                                read - written);
            if (result <= 0) {
                err = ESP_FAIL;
                break;
            }
            written += (uint32_t)result;
        }
        transferred += read;
        meeting_stream_publish_progress(request, transferred);
        vTaskDelay(1);
    }
    if (err == ESP_OK) {
        if (esp_http_client_fetch_headers(client) < 0) err = ESP_FAIL;
        while (err == ESP_OK && !esp_http_client_is_complete_data_received(client)) {
            if (meeting_stream_stop_requested(request)) {
                err = ESP_ERR_INVALID_STATE;
                break;
            }
            int read = esp_http_client_read(client, (char *)request->io_buffer,
                                             request->io_buffer_size);
            if (read <= 0 && !esp_http_client_is_complete_data_received(client)) {
                err = ESP_FAIL;
            }
        }
    }
    if (client) response.status = esp_http_client_get_status_code(client);
    if (err == ESP_OK && (response.status < 200 || response.status >= 300)) {
        err = ESP_FAIL;
    }
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "meeting chunk %d rejected: status=%d body=%s",
                 (int)request->chunk_index, response.status,
                 response.data ? response.data : "");
    }
    if (client) {
        const bool keep = err == ESP_OK &&
                          esp_http_client_is_complete_data_received(client);
        esp_http_client_set_user_data(client, NULL);
        clear_meeting_stream_client(client);
        if (keep) s_meeting_stream_reusable_client = client;
        else {
            esp_http_client_cleanup(client);
            if (s_meeting_stream_reusable_client == client) {
                s_meeting_stream_reusable_client = NULL;
            }
        }
    }
    ESP_LOGI(TAG, "meeting chunk %d upload bytes=%u connection=%s status=%d err=%s",
             (int)request->chunk_index, (unsigned)request->length,
             reused ? "reused" : "new", response.status, esp_err_to_name(err));
    gateway_transport_response_release(&response);
    gateway_transport_general_lane_unlock();
    device_connectivity_end_network_request();
    return err;
}

device_status_t gateway_transport_cancel_meeting_stream(const void *owner_token,
                                                         uint32_t timeout_ms) {
    if (!owner_token || timeout_ms == 0 || !s_active_clients_mutex ||
        xSemaphoreTake(s_active_clients_mutex, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    device_status_t status = DEVICE_STATUS_OK;
    if (s_active_meeting_stream_owner == owner_token) {
        status = cancel_active_client_locked(s_active_meeting_stream_client,
                                             "meeting-stream");
    }
    xSemaphoreGive(s_active_clients_mutex);
    if (device_connectivity_is_active_cellular()) {
        (void)device_connectivity_cancel_cellular_requests_for_owner(owner_token);
    }
    return status;
}

void gateway_transport_reset_meeting_stream(void) {
    if (!s_http_mutex || xSemaphoreTake(s_http_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        ESP_LOGW(TAG, "meeting stream reset deferred: general lane busy");
        return;
    }
    if (s_meeting_stream_reusable_client) {
        esp_http_client_cleanup(s_meeting_stream_reusable_client);
        s_meeting_stream_reusable_client = NULL;
    }
    xSemaphoreGive(s_http_mutex);
}

bool gateway_transport_meeting_stream_ready(void) {
    return s_http_mutex && s_active_clients_mutex;
}

bool gateway_transport_startup_running(void) {
    bool running;
    taskENTER_CRITICAL(&s_transport_state_lock);
    running = s_gateway_startup_running;
    taskEXIT_CRITICAL(&s_transport_state_lock);
    return running;
}

static esp_err_t on_http_event(esp_http_client_event_t *event) {
    gateway_transport_response_t *out = event->user_data;
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

static int32_t request_with_capacity(const char *method, const char *path, const char *content_type,
                                     const char *body, int32_t body_len, uint32_t response_capacity,
                                     gateway_transport_response_t *out) {
    if (!out) return ESP_ERR_INVALID_ARG;
    memset(out, 0, sizeof(*out));
    if (!method || !path || response_capacity < 2) return ESP_ERR_INVALID_ARG;
    char url[URL_CAPACITY];
    int n = strncmp(path, "http://", 7) == 0 || strncmp(path, "https://", 8) == 0
                ? snprintf(url, sizeof(url), "%s", path)
                : snprintf(url, sizeof(url), "%s%s", s_gateway_url, path);
    if (n < 0 || n >= sizeof(url)) return ESP_ERR_INVALID_SIZE;
    /* Wi-Fi HTTP remains in the composition root, but this transport-neutral
     * admission lets Connectivity drain every shared request before a future
     * system-sleep commit.  No HTTP handle escapes into Power Service. */
    /* Cellular calls take the same admission inside Connectivity Service so
     * direct profile requests (voice pairing and streamed meeting chunks) are
     * covered too.  Wi-Fi reaches its ESP HTTP adapter only here. */
    const bool cellular_transport_request = device_connectivity_is_active_cellular();
    const bool network_request_admitted = !cellular_transport_request;
    if (network_request_admitted) {
        device_status_t network_admission = device_connectivity_begin_network_request();
        if (network_admission != DEVICE_STATUS_OK) {
            return device_status_to_platform_error(network_admission);
        }
    }
#define NETWORK_REQUEST_RETURN(value) \
    do { \
        if (network_request_admitted) device_connectivity_end_network_request(); \
        return (value); \
    } while (0)
    bool foreground_request = false;
    bool meeting_request = false;
    bool meeting_capability_refresh_request = false;
    bool gateway_startup_request = false;
    uint32_t foreground_generation = 0;
    TaskHandle_t current_task = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_transport_state_lock);
    foreground_request = interaction_service_current_task_is_worker();
    meeting_request = meeting_service_current_task_is_worker();
    meeting_capability_refresh_request =
        meeting_service_current_task_is_capability_refresh();
    gateway_startup_request = current_task == s_gateway_task;
    if (foreground_request) foreground_generation = interaction_service_generation();
    taskEXIT_CRITICAL(&s_transport_state_lock);

    bool poll_request = gateway_dispatcher_current_task_is_poll_worker();
    bool asset_request = s_host.current_task_is_startup_pet_asset();
    SemaphoreHandle_t request_mutex = asset_request
                                            ? s_gateway_asset_http_mutex
                                            : poll_request ? s_gateway_poll_http_mutex : s_http_mutex;
    if (!request_mutex) NETWORK_REQUEST_RETURN(ESP_ERR_INVALID_STATE);
    int64_t request_started_us = esp_timer_get_time();
    TickType_t lock_started = xTaskGetTickCount();
    bool cancellation_request = command_service_current_task_is_cancel_worker();
    const TickType_t lock_timeout = pdMS_TO_TICKS(cancellation_request ? 6000 : 35000);
    while (xSemaphoreTake(request_mutex, pdMS_TO_TICKS(100)) != pdTRUE) {
        if (foreground_request && command_service_cancel_requested_for(foreground_generation)) {
            ESP_LOGI(TAG, "foreground HTTP lock wait cancelled: %s %s", method, path);
            NETWORK_REQUEST_RETURN(ESP_ERR_INVALID_STATE);
        }
        if ((xTaskGetTickCount() - lock_started) >= lock_timeout) {
            ESP_LOGW(TAG, "HTTP request lock timeout: %s %s", method, path);
            NETWORK_REQUEST_RETURN(ESP_ERR_TIMEOUT);
        }
    }
    uint32_t lock_wait_ms = (uint32_t)((xTaskGetTickCount() - lock_started) * portTICK_PERIOD_MS);
    if (foreground_request && command_service_cancel_requested_for(foreground_generation)) {
        xSemaphoreGive(request_mutex);
        NETWORK_REQUEST_RETURN(ESP_ERR_INVALID_STATE);
    }
    // Prefer PSRAM for every HTTP body. Request buffers must not consume the
    // small internal heap reserved for the TLS handshake and Wi-Fi stacks.
    out->data = heap_caps_malloc(response_capacity, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!out->data) out->data = heap_caps_malloc(response_capacity, MALLOC_CAP_8BIT);
    if (!out->data) {
        ESP_LOGE(TAG, "HTTP buffer allocation failed: need=%u path=%s", (unsigned)response_capacity, path);
        s_host.log_heap_snapshot("http-buffer-fail");
        xSemaphoreGive(request_mutex);
        NETWORK_REQUEST_RETURN(ESP_ERR_NO_MEM);
    }
    out->capacity = response_capacity;
    out->data[0] = '\0';
    bool absolute_url = !strncmp(path, "http://", 7) || !strncmp(path, "https://", 8);
    bool reusable_gateway_request = !absolute_url || url_has_same_origin(s_gateway_url, url);
    bool bearer_request = !absolute_url;
    if (cellular_transport_request) {
        char authorization[128] = {0};
        uint32_t cellular_response_len = 0;
        if (s_gateway_token[0] && bearer_request) {
            snprintf(authorization, sizeof(authorization), "Bearer %s", s_gateway_token);
        }
        device_connectivity_http_request_t cellular_request = {
            .method = method, .url = url, .content_type = content_type,
            .authorization = authorization, .body = body,
            .body_len = body_len > 0 ? (uint32_t)body_len : 0,
            .response = out->data, .response_capacity = (uint32_t)out->capacity,
            .response_len = &cellular_response_len, .status_code = &out->status,
            .truncated = &out->truncated,
            .timeout_ms = cancellation_request ? 5000
                         : (foreground_request && body_len > 32768 ? 90000 : 30000),
            .cancellation_owner = foreground_request ? (const void *)current_task
                                : meeting_request ? (const void *)current_task : NULL,
            .foreground = foreground_request,
        };
        esp_err_t cellular_err = device_status_to_platform_error(
            device_connectivity_cellular_http_request(&cellular_request));
        out->len = cellular_response_len;
        xSemaphoreGive(request_mutex);
        ESP_LOGI(TAG, "ML307 HTTP %s %s status=%d err=%s response=%u%s",
                 method, absolute_url ? "<absolute URL>" : path, out->status,
                 esp_err_to_name(cellular_err), (unsigned)out->len,
                 out->truncated ? " truncated" : "");
        NETWORK_REQUEST_RETURN(cellular_err);
    }
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
            NETWORK_REQUEST_RETURN(setup_err);
        }
    }
    if (!client) {
        ESP_LOGE(TAG, "HTTP client allocation failed: path=%s", path);
        s_host.log_heap_snapshot("http-client-fail");
        free(out->data);
        out->data = NULL;
        xSemaphoreGive(request_mutex);
        NETWORK_REQUEST_RETURN(ESP_ERR_NO_MEM);
    }
    gateway_active_lane_t active_lane = GATEWAY_ACTIVE_LANE_FOREGROUND;
    bool active_client_published = false;
    if (gateway_startup_request) {
        active_lane = GATEWAY_ACTIVE_LANE_STARTUP;
        active_client_published = true;
    } else if (meeting_capability_refresh_request) {
        active_lane = GATEWAY_ACTIVE_LANE_CAPABILITY_REFRESH;
        active_client_published = true;
    } else if (foreground_request) {
        active_lane = GATEWAY_ACTIVE_LANE_FOREGROUND;
        active_client_published = true;
    } else if (poll_request) {
        active_lane = GATEWAY_ACTIVE_LANE_POLL;
        active_client_published = true;
    } else if (asset_request) {
        active_lane = GATEWAY_ACTIVE_LANE_ASSET;
        active_client_published = true;
    }
    if (active_client_published) publish_active_client(active_lane, client);
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
    if (foreground_request && command_service_cancel_requested_for(foreground_generation)) {
        // Cancellation skips the perform entirely. Do not report the pooled
        // handle's previous status (usually a stale 200) for a request that
        // never ran.
        err = ESP_ERR_INVALID_STATE;
        out->status = 0;
    } else {
        err = esp_http_client_perform(client);
        out->status = esp_http_client_get_status_code(client);
    }
    uint32_t perform_ms = (uint32_t)((esp_timer_get_time() - perform_started_us) / 1000);
    if (active_client_published) clear_active_client(active_lane, client);
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
        s_host.log_heap_snapshot("http-perform-fail");
    }
    if (out->truncated) {
        ESP_LOGE(TAG, "HTTP response truncated: capacity=%u path=%s", (unsigned)response_capacity, path);
        NETWORK_REQUEST_RETURN(ESP_ERR_INVALID_SIZE);
    }
    NETWORK_REQUEST_RETURN(err);
#undef NETWORK_REQUEST_RETURN
}

int32_t gateway_transport_request_with_capacity(const char *method, const char *path,
                                                const char *content_type,
                                                const char *body, int32_t body_len,
                                                uint32_t response_capacity,
                                                gateway_transport_response_t *out) {
    return request_with_capacity(method, path, content_type, body, body_len,
                                 response_capacity, out);
}

int32_t gateway_transport_request(const char *method, const char *path,
                                  const char *content_type,
                                  const char *body, int32_t body_len,
                                  gateway_transport_response_t *out) {
    return request_with_capacity(method, path, content_type, body, body_len,
                                 RESPONSE_CAPACITY, out);
}

void gateway_transport_response_release(gateway_transport_response_t *response) {
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

int32_t gateway_transport_handshake(bool cold_start) {
	char boot_field[64] = {0};
    gateway_transport_response_t response = {0};
    if (cold_start) {
        s_host.set_handshake_welcome_queued(false);
        snprintf(boot_field, sizeof(boot_field), "\"bootSessionId\":\"%s\",", s_host.boot_session_id());
    }
    // The screen renderer keeps several DMA buffers in internal RAM. Asking
    // Hub for embedded RGB565+A8 pet frames forces a 100+ KiB response and starves
    // the TLS allocation on this device. The built-in pet stays visible, while
    // the small handshake response still delivers city/weather immediately.
    firmware_identity_info_t identity = {0};
    if (firmware_identity_get(&identity) != DEVICE_STATUS_OK) return ESP_ERR_INVALID_STATE;
    cJSON *request_json = cJSON_CreateObject();
    if (!request_json) return ESP_ERR_NO_MEM;
    cJSON_AddStringToObject(request_json, "clientId", s_device_id);
    cJSON_AddStringToObject(request_json, "clientName", "ESP32-S3 Pet");
    if (cold_start) cJSON_AddStringToObject(request_json, "bootSessionId", s_host.boot_session_id());
    cJSON_AddStringToObject(request_json, "protocolVersion", "1.1");
    cJSON *capabilities = cJSON_AddObjectToObject(request_json, "clientCapabilities");
    if (!add_local_gateway_capabilities(capabilities)) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    cJSON *legacy_capabilities = cJSON_AddObjectToObject(request_json, "capabilities");
    cJSON *firmware = legacy_capabilities ? cJSON_AddObjectToObject(legacy_capabilities, "firmwareIdentity") : NULL;
    if (!firmware) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    cJSON_AddStringToObject(firmware, "deviceId", s_device_id);
    cJSON_AddStringToObject(firmware, "productId", identity.product_id);
    cJSON_AddStringToObject(firmware, "boardId", identity.board_id);
    cJSON_AddStringToObject(firmware, "hardwareRev", identity.hardware_rev);
    cJSON_AddStringToObject(firmware, "layoutId", identity.layout_id);
    cJSON_AddStringToObject(firmware, "compatibilityId", identity.compatibility_id);
    cJSON_AddNumberToObject(firmware, "releaseSequence", (double)identity.release_sequence);
    cJSON_AddStringToObject(firmware, "appVersion", identity.app_version);
    cJSON_AddStringToObject(firmware, "elfSha256", identity.elf_sha256);
    cJSON *profile = cJSON_AddObjectToObject(firmware, "deviceProfile");
    if (!profile ||
        !cJSON_AddNumberToObject(profile, "abiVersion", identity.profile.abi_version) ||
        !cJSON_AddStringToObject(profile, "id", identity.profile.id) ||
        !cJSON_AddNumberToObject(profile, "displayWidth", identity.profile.display_width) ||
        !cJSON_AddNumberToObject(profile, "displayHeight", identity.profile.display_height) ||
        !cJSON_AddNumberToObject(profile, "capabilities", identity.profile.capabilities) ||
        !cJSON_AddNumberToObject(profile, "primaryInteractionSource",
                                 identity.profile.primary_interaction_source)) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    cJSON *power = cJSON_AddObjectToObject(firmware, "power");
    if (!power ||
        !cJSON_AddBoolToObject(power, "available", identity.power_available) ||
        !cJSON_AddNumberToObject(power, "state", identity.power.state) ||
        !cJSON_AddBoolToObject(power, "displayOffArmed",
                                identity.power.display_off_armed) ||
        !cJSON_AddBoolToObject(power, "telemetryAvailable",
                                identity.power_telemetry_available) ||
        !cJSON_AddNumberToObject(power, "batteryLevelPercent",
                                 identity.power_telemetry_available
                                     ? identity.power_telemetry.level_percent
                                     : -1) ||
        !cJSON_AddBoolToObject(power, "charging",
                                identity.power_telemetry_available &&
                                    identity.power_telemetry.charging)) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    cJSON *battery = cJSON_AddObjectToObject(firmware, "batteryPolicy");
    if (!battery ||
        !cJSON_AddBoolToObject(battery, "available", identity.battery_policy_available) ||
        !cJSON_AddBoolToObject(battery, "telemetryAvailable",
                                identity.battery_policy.telemetry_available) ||
        !cJSON_AddNumberToObject(battery, "level", identity.battery_policy.level) ||
        !cJSON_AddBoolToObject(battery, "optionalWorkAllowed",
                                identity.battery_policy.optional_work_allowed) ||
        !cJSON_AddBoolToObject(battery, "highPowerWorkAllowed",
                                identity.battery_policy.high_power_work_allowed)) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    cJSON *connectivity = cJSON_AddObjectToObject(firmware, "connectivity");
    if (!connectivity ||
        !cJSON_AddNumberToObject(connectivity, "activeUplink",
                                 identity.connectivity.active_uplink) ||
        !cJSON_AddBoolToObject(connectivity, "wifiReady",
                               identity.connectivity.wifi_ready) ||
        !cJSON_AddBoolToObject(connectivity, "cellularReady",
                               identity.connectivity.cellular_ready) ||
        !cJSON_AddBoolToObject(connectivity, "ready", identity.connectivity.ready)) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    cJSON *tools = cJSON_AddArrayToObject(request_json, "tools");
    if (!s_host.append_tool_descriptors(tools)) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    /* The descriptor grows with the common tool registry.  Keep it out of
     * maclaw_gateway_startup's stack: even an 8 KiB automatic buffer trips the
     * FreeRTOS stack canary before TLS gets a chance to run. */
    char *payload = heap_caps_malloc(HANDSHAKE_REQUEST_CAPACITY,
                                     MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!payload) payload = heap_caps_malloc(HANDSHAKE_REQUEST_CAPACITY, MALLOC_CAP_8BIT);
    if (!payload) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    bool printed = cJSON_PrintPreallocated(request_json, payload,
                                            HANDSHAKE_REQUEST_CAPACITY, false);
    cJSON_Delete(request_json);
    if (!printed) {
        ESP_LOGE(TAG, "gateway handshake descriptor exceeds capacity=%u",
                 (unsigned)HANDSHAKE_REQUEST_CAPACITY);
        heap_caps_free(payload);
        return ESP_ERR_INVALID_SIZE;
    }
    int request_len = strlen(payload);
    if (request_len <= 0 || request_len >= HANDSHAKE_REQUEST_CAPACITY) {
        ESP_LOGE(TAG, "gateway handshake descriptor too large: bytes=%d capacity=%u",
                 request_len, (unsigned)HANDSHAKE_REQUEST_CAPACITY);
        heap_caps_free(payload);
        return ESP_ERR_INVALID_SIZE;
    }
    s_host.log_heap_snapshot("handshake-before");
    esp_err_t err = gateway_transport_request_with_capacity("POST", "/api/im-gateway/v1/handshake", "application/json",
                                          payload, (size_t)request_len, HANDSHAKE_RESPONSE_CAPACITY, &response);
    heap_caps_free(payload);
    if (err != ESP_OK || response.status != 200) {
        ESP_LOGE(TAG, "gateway handshake failed: err=%s status=%d body=%s", esp_err_to_name(err), response.status, response.data);
        s_host.log_heap_snapshot("handshake-fail");
        esp_err_t result = gateway_auth_failed(&response, err) ? ESP_ERR_INVALID_STATE
                           : err == ESP_OK ? ESP_FAIL : err;
        gateway_transport_response_release(&response);
        observe_capability_transport_result(false);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    cJSON *ok = json ? cJSON_GetObjectItemCaseSensitive(json, "ok") : NULL;
    if (!cJSON_IsTrue(ok)) {
        cJSON_Delete(json);
        gateway_transport_response_release(&response);
        observe_capability_transport_result(false);
        return ESP_ERR_INVALID_RESPONSE;
    }
    s_host.apply_server_time(json);
    cJSON *startup_welcome = cJSON_GetObjectItemCaseSensitive(json, "startupWelcomeQueued");
    bool startup_welcome_queued = cJSON_IsTrue(startup_welcome);
    if (cold_start) {
        s_host.set_handshake_welcome_queued(startup_welcome_queued);
        ESP_LOGI(TAG, "gateway startup Welcome queued: %s",
                 startup_welcome_queued ? "yes" : "no");
    } else if (startup_welcome_queued) {
        // Runtime capability refreshes deliberately omit bootSessionId. Treat
        // an unexpected legacy response as informational; it must never re-arm
        // or otherwise mutate the completed cold-start Welcome transaction.
        ESP_LOGW(TAG, "runtime handshake ignored unexpected startup Welcome flag");
    }
    cJSON *accepted = cJSON_GetObjectItemCaseSensitive(json, "capabilitiesAccepted");
    gateway_capability_flags_t accepted_capabilities = 0;
    bool accepted_projection_valid =
        parse_accepted_gateway_capabilities(accepted, &accepted_capabilities);
    if (accepted) {
        if (accepted_projection_valid) {
            taskENTER_CRITICAL(&s_transport_state_lock);
            accepted_projection_valid = gateway_capability_projection_observe_accepted(
                &s_capability_projection, accepted_capabilities);
            taskEXIT_CRITICAL(&s_transport_state_lock);
        }
        if (!accepted_projection_valid) {
            ESP_LOGW(TAG, "gateway returned malformed or incompatible capability acceptance");
        } else {
            ESP_LOGI(TAG, "client capabilities accepted: flags=0x%08lx",
                     (unsigned long)accepted_capabilities);
        }
    } else {
        ESP_LOGW(TAG, "gateway did not acknowledge client capabilities (legacy Hub?)");
    }
    if (!accepted_projection_valid) {
        taskENTER_CRITICAL(&s_transport_state_lock);
        (void)gateway_capability_projection_withdraw_acceptance(
            &s_capability_projection);
        taskEXIT_CRITICAL(&s_transport_state_lock);
    }
    cJSON *meeting = cJSON_GetObjectItemCaseSensitive(json, "meetingRecording");
    bool meeting_capability_advertised = accepted_projection_valid &&
        (accepted_capabilities & GATEWAY_CAPABILITY_MEETING_RECORDER) != 0 &&
        cJSON_IsObject(meeting);
    const char *meeting_base_path = NULL;
    int meeting_chunk_size = 0;
    const char *meeting_process_mode = "keep";
    if (meeting_capability_advertised) {
        meeting_base_path = json_string(meeting, "basePath");
        (void)json_number(meeting, "chunkSize", &meeting_chunk_size);
        cJSON *modes = cJSON_GetObjectItemCaseSensitive(meeting, "modes");
        cJSON *minutes = modes ? cJSON_GetObjectItemCaseSensitive(modes, "minutes") : NULL;
        cJSON *transcript = modes ? cJSON_GetObjectItemCaseSensitive(modes, "transcript") : NULL;
        meeting_process_mode = cJSON_IsTrue(minutes) ? "minutes" : cJSON_IsTrue(transcript) ? "transcript" : "keep";
    }
    (void)meeting_service_set_capability_descriptor(meeting_capability_advertised,
                                                     meeting_base_path,
                                                     meeting_chunk_size,
                                                     meeting_process_mode);
    cJSON *pet_profile = cJSON_GetObjectItemCaseSensitive(json, "pet");
    const char *skin = pet_profile ? json_string(pet_profile, "skin") : NULL;
    cJSON *motion = pet_profile ? cJSON_GetObjectItemCaseSensitive(pet_profile, "motionEnabled") : NULL;
    if (skin) ambient_service_apply_pet_profile(skin, !motion || cJSON_IsTrue(motion));
    cJSON *pet_asset = cJSON_GetObjectItemCaseSensitive(json, "petAsset");
    if (cold_start) {
        s_host.note_cold_start_pet_asset(pet_asset, skin);
    } else if (cJSON_IsObject(pet_asset)) {
        esp_err_t asset_err = (esp_err_t)s_host.apply_pet_asset(pet_asset);
        if (asset_err != ESP_OK) ESP_LOGW(TAG, "handshake pet asset ignored: %s", esp_err_to_name(asset_err));
    } else {
        // Runtime refreshes remain authoritative and can update the visible
        // asset synchronously; only the cold-start path is latency-sensitive.
        esp_err_t asset_err = (esp_err_t)s_host.clear_pet_asset();
        if (asset_err != ESP_OK) ESP_LOGW(TAG, "handshake pet asset clear failed: %s", esp_err_to_name(asset_err));
    }
    s_host.apply_ambient(cJSON_GetObjectItemCaseSensitive(json, "ambient"));
    s_host.process_update_metadata(cJSON_GetObjectItemCaseSensitive(json, "update"), cold_start);
    cJSON_Delete(json);
    gateway_transport_response_release(&response);
    if (accepted_projection_valid) observe_capability_transport_result(true);
    else meeting_service_set_capability_operational(false);
    s_host.log_heap_snapshot("handshake-ok");
    if (cold_start) {
        // The caller initializes ESP-SR immediately after this function
        // returns. Keep optional media work outside the authenticated response
        // parsing path; gateway_startup_task applies it only after wake ready.
        ESP_LOGI(TAG, "cold-start handshake essentials complete; optional pet asset remains deferred");
    }
    return ESP_OK;
}

static bool gateway_auth_failed(const gateway_transport_response_t *response, esp_err_t err) {
    if (!response) return false;
    if (response->status == 401 || response->status == 403) return true;
    return err == ESP_ERR_NOT_SUPPORTED && response->status == 401;
}

int32_t gateway_transport_pair_by_voice(const uint8_t *wav, uint32_t wav_len) {
    gateway_transport_response_t response;
    char client_header[96];
    snprintf(client_header, sizeof(client_header), "%s", s_device_id);
    // pair endpoint needs a client ID header rather than authorization; use a
    // short dedicated request because the normal helper only emits fixed headers.
    char url[URL_CAPACITY];
    int n = snprintf(url, sizeof(url), "%s/api/device-gateway/v1/pair/voice", s_gateway_url);
    if (n < 0 || n >= sizeof(url)) return ESP_ERR_INVALID_SIZE;
    const bool cellular_transport_request = device_connectivity_is_active_cellular();
    if (!cellular_transport_request) {
        device_status_t network_admission = device_connectivity_begin_network_request();
        if (network_admission != DEVICE_STATUS_OK) {
            return device_status_to_platform_error(network_admission);
        }
    }
#define PAIR_VOICE_RETURN(value) \
    do { \
        if (!cellular_transport_request) device_connectivity_end_network_request(); \
        return (value); \
    } while (0)
    memset(&response, 0, sizeof(response));
    if (!s_http_mutex || xSemaphoreTake(s_http_mutex, pdMS_TO_TICKS(35000)) != pdTRUE) {
        ESP_LOGW(TAG, "HTTP request lock timeout: POST pair/voice");
        PAIR_VOICE_RETURN(ESP_ERR_TIMEOUT);
    }
    response.data = heap_caps_malloc(RESPONSE_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!response.data) response.data = heap_caps_malloc(RESPONSE_CAPACITY, MALLOC_CAP_8BIT);
    if (!response.data) {
        xSemaphoreGive(s_http_mutex);
        PAIR_VOICE_RETURN(ESP_ERR_NO_MEM);
    }
    response.capacity = RESPONSE_CAPACITY;
    response.data[0] = '\0';
    if (cellular_transport_request) {
        bool truncated = false;
        uint32_t cellular_response_len = 0;
        device_connectivity_http_request_t cellular_request = {
            .method = "POST", .url = url, .content_type = "audio/wav",
            .extra_header_name = "X-MaClaw-Client-ID",
            .extra_header_value = client_header, .body = wav, .body_len = (uint32_t)wav_len,
            .response = response.data, .response_capacity = (uint32_t)response.capacity,
            .response_len = &cellular_response_len, .status_code = &response.status,
            .truncated = &truncated, .timeout_ms = 30000, .foreground = true,
        };
        esp_err_t err = device_status_to_platform_error(
            device_connectivity_cellular_http_request(&cellular_request));
        response.len = cellular_response_len;
        response.truncated = truncated;
        xSemaphoreGive(s_http_mutex);
        if (err != ESP_OK || response.status != 201) {
            esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
            gateway_transport_response_release(&response);
            PAIR_VOICE_RETURN(result);
        }
        cJSON *json = cJSON_Parse(response.data);
        const char *token = json ? json_string(json, "gatewayToken") : NULL;
        err = token ? transport_persist_token(token) : ESP_ERR_INVALID_RESPONSE;
        cJSON_Delete(json);
        gateway_transport_response_release(&response);
        PAIR_VOICE_RETURN(err);
    }
    esp_http_client_config_t config = {.url = url, .event_handler = on_http_event, .user_data = &response, .timeout_ms = 30000, .crt_bundle_attach = esp_crt_bundle_attach};
    esp_http_client_handle_t client = esp_http_client_init(&config);
    if (!client) {
        gateway_transport_response_release(&response);
        xSemaphoreGive(s_http_mutex);
        PAIR_VOICE_RETURN(ESP_ERR_NO_MEM);
    }
    /* Voice pairing is a foreground Wi-Fi request but deliberately uses a
     * dedicated client because it needs the pairing-only client-ID header.
     * Register it in this transport's foreground lane so cancellation remains
     * bounded and does not leak the concrete handle to a caller. */
    publish_active_client(GATEWAY_ACTIVE_LANE_FOREGROUND, client);
    esp_http_client_set_method(client, HTTP_METHOD_POST);
    esp_http_client_set_header(client, "Content-Type", "audio/wav");
    esp_http_client_set_header(client, "X-MaClaw-Client-ID", client_header);
    esp_http_client_set_post_field(client, (const char *)wav, wav_len);
    esp_err_t err = esp_http_client_perform(client);
    response.status = esp_http_client_get_status_code(client);
    /* Clear the active pointer before cleanup so a concurrent cancel never
     * dereferences a released ESP HTTP client. */
    clear_active_client(GATEWAY_ACTIVE_LANE_FOREGROUND, client);
    esp_http_client_cleanup(client);
    xSemaphoreGive(s_http_mutex);
    if (response.truncated) err = ESP_ERR_INVALID_SIZE;
    if (err != ESP_OK || response.status != 201) {
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        gateway_transport_response_release(&response);
        PAIR_VOICE_RETURN(result);
    }
    cJSON *json = cJSON_Parse(response.data);
    const char *token = json ? json_string(json, "gatewayToken") : NULL;
    err = token ? transport_persist_token(token) : ESP_ERR_INVALID_RESPONSE;
    cJSON_Delete(json);
    gateway_transport_response_release(&response);
    PAIR_VOICE_RETURN(err);
#undef PAIR_VOICE_RETURN
}

static esp_err_t pair_by_code(void) {
    char pair_code[TRANSPORT_PAIR_CODE_CAPACITY];
    taskENTER_CRITICAL(&s_transport_state_lock);
    strlcpy(pair_code, s_pair_code, sizeof(pair_code));
    taskEXIT_CRITICAL(&s_transport_state_lock);
    if (strlen(pair_code) != 6) return ESP_ERR_INVALID_STATE;
    cJSON *body = cJSON_CreateObject();
    cJSON_AddStringToObject(body, "clientId", s_device_id);
    // pairCode is the canonical device-gateway field across Hub and
    // MaClawSrv. Hub retains a server-side code alias solely for old firmware.
    cJSON_AddStringToObject(body, "pairCode", pair_code);
    char *payload = cJSON_PrintUnformatted(body);
    cJSON_Delete(body);
    if (!payload) return ESP_ERR_NO_MEM;
    /* Pair codes are one-time credentials, not diagnostics.  Keep only the
     * non-secret request identity in logs; a user may export serial logs
     * while investigating a failed pairing and that must not make an unused
     * code replayable within its Hub-side TTL. */
    ESP_LOGI(TAG, "pairing request: url=%s client=%s code_present=yes",
             s_gateway_url, s_device_id);
    gateway_transport_response_t response;
    esp_err_t err = (esp_err_t)gateway_transport_request("POST", "/api/device-gateway/v1/pair", "application/json", payload, (int32_t)strlen(payload), &response);
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
        gateway_transport_response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    const char *token = json ? json_string(json, "gatewayToken") : NULL;
    err = token ? transport_persist_token(token) : ESP_ERR_INVALID_RESPONSE;
    cJSON_Delete(json);
    gateway_transport_response_release(&response);
    return err;
}

static bool gateway_startup_stop_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_transport_state_lock);
    requested = s_gateway_startup_stop_requested;
    taskEXIT_CRITICAL(&s_transport_state_lock);
    return requested;
}

static void gateway_startup_task(void *arg) {
    (void)arg;
    if (!s_gateway_startup_start_gate ||
        xSemaphoreTake(s_gateway_startup_start_gate, portMAX_DELAY) != pdTRUE) {
        ESP_LOGW(TAG, "gateway startup start gate unavailable");
        goto finish;
    }
    if (gateway_startup_stop_requested()) goto finish;
    // Startup remains the clean ambient pet face. Connection progress belongs
    // in the serial log; it must never cover the clock, weather or pet.
    char pair_code[TRANSPORT_PAIR_CODE_CAPACITY];
    taskENTER_CRITICAL(&s_transport_state_lock);
    strlcpy(pair_code, s_pair_code, sizeof(pair_code));
    taskEXIT_CRITICAL(&s_transport_state_lock);
    ESP_LOGI(TAG, "gateway startup: url=%s paired=%s pair_code=%s", s_gateway_url,
             s_gateway_token[0] ? "yes" : "no",
             pair_code[0] ? "present" : "missing");
    // A pending one-time code always takes precedence. It is consumed exactly
    // once to obtain/replace the durable gateway token, then erased by
    // pair_by_code(). Normal boots with no pending code use only the token.
    if (pair_code[0]) {
        ambient_service_apply_pet_state("thinking");
        scene_presenter_publish_message("设备配对", "正在连接码卡龙界面");
        ESP_LOGI(TAG, "gateway pairing request starting");
        uint32_t retry_ms = GATEWAY_RETRY_INITIAL_MS;
        unsigned attempt = 0;
        bool paired = false;
        /* Candidate pairing cannot retry transient Hub failures forever.  The
         * monotonic per-boot deadline covers a live but unreachable Hub, while
         * Configuration's durable boot budget covers resets/power loss. */
        const bool staged_candidate = s_host.staged_provisioning_pending();
        const int64_t staged_confirmation_deadline_us = staged_candidate
            ? esp_timer_get_time() +
                  (int64_t)GATEWAY_STAGED_PROVISIONING_CONFIRM_DEADLINE_MS * 1000
            : 0;
        while (!gateway_startup_stop_requested()) {
            /* Hub token persistence is the candidate confirmation point. Once
             * pair_by_code() has completed it, a following ordinary handshake
             * retry must not consume the old candidate deadline or roll back
             * an already-confirmed owner/network. */
            if (!paired && staged_confirmation_deadline_us != 0 &&
                esp_timer_get_time() >= staged_confirmation_deadline_us) {
                ESP_LOGW(TAG, "unconfirmed provisioning candidate confirmation deadline expired");
                if (!s_host.rollback_staged_provisioning ||
                    !s_host.rollback_staged_provisioning()) {
                    ambient_service_apply_pet_state("alert");
                    scene_presenter_publish_message("新配置未确认",
                                                    "确认超时，无法恢复旧配置，请手动重启");
                }
                break;
            }
            ++attempt;
            esp_err_t err = paired ? gateway_transport_handshake(true) : pair_by_code();
            if (gateway_startup_stop_requested()) break;
            if (err == ESP_OK) {
                if (!paired) {
                    paired = true;
                    attempt = 0;
                    retry_ms = GATEWAY_RETRY_INITIAL_MS;
                    continue;
                }
                (void)s_host.start_gateway_ready_tasks();
                s_host.apply_deferred_startup_pet_asset();
                break;
            }
            if (err == ESP_ERR_INVALID_STATE) {
                if (!paired && staged_candidate) {
                    /* Pair code rejection is authoritative confirmation
                     * failure, unlike a transient HTTP outage. Restore the
                     * last confirmed network/owner before presenting any
                     * recovery portal; otherwise a reboot could keep trying a
                     * bad candidate indefinitely. The host resets only after
                     * its durable rollback succeeds. */
                    if (!s_host.rollback_staged_provisioning ||
                        !s_host.rollback_staged_provisioning()) {
                        ambient_service_apply_pet_state("alert");
                        scene_presenter_publish_message("新配置未确认",
                                                        "无法恢复旧配置，请手动重启");
                        break;
                    }
                    break;
                }
                ambient_service_apply_pet_state("alert");
                /* A valid code is the explicit authority to move a physical
                 * device to its issuing MaClaw.  Ownership is decided by the
                 * Hub; no board profile gets a separate old-owner flow. */
                scene_presenter_publish_message(paired ? "令牌认证失败" : "配对码已失效",
                                 "请检查或重新配对");
                s_host.start_setup_portal();
                break;
            }
            // Preserve the boot surface while the Hub or network is temporarily
            // unavailable. Pet/standby is published only after Welcome + wake.
            scene_presenter_publish_startup_splash();
            ESP_LOGW(TAG, "gateway %s attempt %u failed: %s; retry in %lu ms",
                      paired ? "handshake" : "pairing", attempt, esp_err_to_name(err),
                      (unsigned long)retry_ms);
            uint32_t wait_ms = retry_ms;
            if (!paired && staged_confirmation_deadline_us != 0) {
                const uint32_t remaining_ms =
                    remaining_timeout_ms(staged_confirmation_deadline_us);
                if (remaining_ms == 0) continue;
                if (wait_ms > remaining_ms) wait_ms = remaining_ms;
            }
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(wait_ms));
            if (retry_ms < GATEWAY_RETRY_MAX_MS) {
                retry_ms *= 2;
                if (retry_ms > GATEWAY_RETRY_MAX_MS) retry_ms = GATEWAY_RETRY_MAX_MS;
            }
        }
    } else if (!s_gateway_token[0]) {
        if (!gateway_startup_stop_requested()) {
            ambient_service_apply_pet_state("quiet");
            scene_presenter_publish_message("设备未配对", "正在开启配对热点");
            s_host.start_setup_portal();
        }
    } else {
        uint32_t retry_ms = GATEWAY_RETRY_INITIAL_MS;
        unsigned attempt = 0;
        while (!gateway_startup_stop_requested()) {
            ++attempt;
            esp_err_t err = gateway_transport_handshake(true);
            if (gateway_startup_stop_requested()) break;
            if (err == ESP_OK) {
                (void)s_host.start_gateway_ready_tasks();
                s_host.apply_deferred_startup_pet_asset();
                break;
            }
            if (err == ESP_ERR_INVALID_STATE) {
                // A 401/403 is not a transient outage: the stored credential
                // was revoked, disabled, or replaced. Keep it persisted for
                // diagnosis and expose recovery; do not confuse a connection
                // failure with permission to erase the device credential.
                ESP_LOGW(TAG, "gateway credential rejected; entering pairing recovery");
                ambient_service_apply_pet_state("alert");
                scene_presenter_publish_message("令牌认证失败", "请检查或重新配对");
                s_host.start_setup_portal();
                break;
            }
            // Keep the board-specific boot surface visible during retry. The actual
            // failure cause is logged with a heap/network snapshot for diagnosis.
            scene_presenter_publish_startup_splash();
            ESP_LOGW(TAG, "gateway handshake attempt %u failed: %s; retry in %lu ms",
                     attempt, esp_err_to_name(err), (unsigned long)retry_ms);
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(retry_ms));
            if (retry_ms < GATEWAY_RETRY_MAX_MS) {
                retry_ms *= 2;
                if (retry_ms > GATEWAY_RETRY_MAX_MS) retry_ms = GATEWAY_RETRY_MAX_MS;
            }
        }
    }
finish:
    bool restart_after_system_sleep_abort = false;
    taskENTER_CRITICAL(&s_transport_state_lock);
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    s_gateway_startup_retiring = true;
    taskEXIT_CRITICAL(&s_transport_state_lock);
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
    taskENTER_CRITICAL(&s_transport_state_lock);
    s_gateway_startup_exit_status = registry_err;
    if (s_gateway_task == self) s_gateway_task = NULL;
    s_gateway_startup_running = false;
    s_gateway_startup_starting = false;
    s_gateway_startup_retiring = false;
    if (registry_err != ESP_OK) {
        s_gateway_startup_stop_requested = true;
        s_gateway_startup_registry_retirement_failed = true;
    }
    if (s_system_sleep_restart_pending && !s_system_sleep_preparing &&
        registry_err == ESP_OK && !s_gateway_startup_registry_retirement_failed) {
        s_system_sleep_restart_pending = false;
        restart_after_system_sleep_abort = true;
    }
    taskEXIT_CRITICAL(&s_transport_state_lock);
    if (s_gateway_startup_stopped) xSemaphoreGive(s_gateway_startup_stopped);
    if (restart_after_system_sleep_abort && !gateway_transport_start_startup_task()) {
        ESP_LOGE(TAG, "cannot defer-restart gateway startup after system-sleep abort");
    }
    vTaskDelete(NULL);
}

static esp_err_t stop_gateway_startup_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_transport_state_lock);
    s_gateway_startup_stop_requested = true;
    task = s_gateway_task;
    const esp_err_t exit_status = s_gateway_startup_exit_status;
    taskEXIT_CRITICAL(&s_transport_state_lock);
    if (!task) return exit_status;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0 ||
        gateway_transport_cancel_active_requests(GATEWAY_TRANSPORT_CANCEL_STARTUP,
                                                 remaining_ms) != DEVICE_STATUS_OK) {
        return ESP_ERR_TIMEOUT;
    }
    xTaskNotifyGive(task);
    remaining_ms = remaining_timeout_ms(deadline_us);
    if (!s_gateway_startup_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_gateway_startup_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_transport_state_lock);
    const esp_err_t completed_status = s_gateway_startup_exit_status;
    taskEXIT_CRITICAL(&s_transport_state_lock);
    if (completed_status != ESP_OK) return completed_status;
    ESP_LOGI(TAG, "gateway startup coordinator stopped");
    return ESP_OK;
}

static esp_err_t stop_gateway_startup_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_transport_state_lock);
    task = s_gateway_task;
    taskEXIT_CRITICAL(&s_transport_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_gateway_startup_task(timeout_ms);
}

bool gateway_transport_start_startup_task(void) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_transport_state_lock);
    if (s_system_sleep_preparing || s_system_sleep_restart_pending ||
        s_gateway_startup_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_transport_state_lock);
        return false;
    }
    if (s_gateway_startup_running || s_gateway_startup_starting ||
        s_gateway_startup_retiring) {
        taskEXIT_CRITICAL(&s_transport_state_lock);
        return true;
    }
    s_gateway_startup_running = true;
    s_gateway_startup_starting = true;
    taskEXIT_CRITICAL(&s_transport_state_lock);

    if (!s_gateway_startup_start_gate || !s_gateway_startup_stopped ||
        !s_http_mutex) {
        taskENTER_CRITICAL(&s_transport_state_lock);
        s_gateway_startup_running = false;
        s_gateway_startup_starting = false;
        taskEXIT_CRITICAL(&s_transport_state_lock);
        ESP_LOGE(TAG, "gateway startup lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_gateway_startup_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_transport_state_lock);
    s_gateway_startup_stop_requested = false;
    s_gateway_startup_exit_status = ESP_OK;
    s_gateway_startup_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_transport_state_lock);

    /*
     * The gateway's TLS/HTTP work has a large but non-ISR stack.  Keeping it
     * in internal RAM competes with Wi-Fi, the round-screen DMA/display
     * adapter, and the ESP-SR/audio services.  On EchoEar this can make task
     * creation fail after a perfectly healthy local-board startup, leaving
     * the user on the misleading red "cannot start gateway" screen.
     *
     * PSRAM is safe for this task: it does not perform cache-disabled flash
     * mutations (those are isolated in dedicated internal-stack workers).
     * Reserve scarce internal memory for Wi-Fi/interrupt-facing work instead.
     */
    BaseType_t created = xTaskCreatePinnedToCoreWithCaps(gateway_startup_task,
                                                        "maclaw_gateway_startup",
                                                        12288, NULL, 4,
                                                        &task, 1,
                                                        MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (created != pdPASS) {
        taskENTER_CRITICAL(&s_transport_state_lock);
        s_gateway_startup_running = false;
        s_gateway_startup_starting = false;
        s_gateway_task = NULL;
        taskEXIT_CRITICAL(&s_transport_state_lock);
        ESP_LOGE(TAG, "cannot start gateway startup task");
        return false;
    }
    taskENTER_CRITICAL(&s_transport_state_lock);
    s_gateway_task = task;
    taskEXIT_CRITICAL(&s_transport_state_lock);
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
        .name = "gateway_startup",
        .context = (void *)task,
        .stop = stop_gateway_startup_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register gateway startup coordinator: %s",
                 esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_transport_state_lock);
        s_gateway_startup_stop_requested = true;
        taskEXIT_CRITICAL(&s_transport_state_lock);
        xSemaphoreGive(s_gateway_startup_start_gate);
        (void)stop_gateway_startup_task(500);
        return false;
    }
    xSemaphoreGive(s_gateway_startup_start_gate);
    return true;
}

device_status_t gateway_transport_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    bool restart_startup = false;
    taskENTER_CRITICAL(&s_transport_state_lock);
    if (!s_host_installed) {
        taskEXIT_CRITICAL(&s_transport_state_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_transport_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    if (s_gateway_startup_retiring || s_gateway_startup_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_transport_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_preparing = true;
    /* A task handle, rather than a historical running flag, is the resource
     * ownership fact. A completed coordinator is never restarted by abort. */
    restart_startup = s_gateway_task != NULL;
    s_system_sleep_restart_startup = restart_startup;
    taskEXIT_CRITICAL(&s_transport_state_lock);

    if (!restart_startup) return DEVICE_STATUS_OK;
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return DEVICE_STATUS_TIMEOUT;
    return transport_status_from_esp_err(stop_gateway_startup_task(remaining_ms));
}

void gateway_transport_abort_system_sleep_prepare(void) {
    bool restart_startup = false;
    taskENTER_CRITICAL(&s_transport_state_lock);
    restart_startup = s_system_sleep_restart_startup;
    s_system_sleep_restart_startup = false;
    /* The restart is idempotent and runs under the common Power/Connectivity
     * rollback ordering. Opening this local lifecycle fence does not reopen
     * logical network admission. */
    s_system_sleep_preparing = false;
    if (restart_startup && (s_gateway_startup_retiring ||
                            s_gateway_startup_registry_retirement_failed)) {
        s_system_sleep_restart_pending = true;
        restart_startup = false;
    }
    taskEXIT_CRITICAL(&s_transport_state_lock);
    if (restart_startup && !gateway_transport_start_startup_task()) {
        ESP_LOGE(TAG, "cannot restore gateway startup worker after system-sleep abort");
    }
}

device_status_t gateway_transport_commit_prepared_network_restart(void) {
    taskENTER_CRITICAL(&s_transport_state_lock);
    if (!s_host_installed) {
        taskEXIT_CRITICAL(&s_transport_state_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (!s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_transport_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* `prepare` has already stopped the published task. Forgetting the old
     * generation is deliberate: only the restart coordinator's rearm stage
     * may decide whether a new Gateway startup is appropriate. */
    s_system_sleep_restart_startup = false;
    s_system_sleep_restart_pending = false;
    s_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_transport_state_lock);
    return DEVICE_STATUS_OK;
}

bool gateway_transport_general_lane_lock(uint32_t timeout_ms) {
    return s_http_mutex && xSemaphoreTake(s_http_mutex, pdMS_TO_TICKS(timeout_ms)) == pdTRUE;
}

void gateway_transport_general_lane_unlock(void) {
    if (s_http_mutex) xSemaphoreGive(s_http_mutex);
}

static device_status_t cancel_active_client_locked(esp_http_client_handle_t client,
                                                   const char *lane) {
    if (!client) return DEVICE_STATUS_OK;
    esp_err_t err = esp_http_client_cancel_request(client);
    if (err == ESP_OK) return DEVICE_STATUS_OK;
    ESP_LOGW(TAG, "gateway HTTP cancel failed: lane=%s err=%s",
             lane ? lane : "?", esp_err_to_name(err));
    return transport_status_from_esp_err(err);
}

device_status_t gateway_transport_cancel_active_requests(
    gateway_transport_cancel_mask_t mask, uint32_t timeout_ms) {
    if (mask == 0 || (mask & ~GATEWAY_TRANSPORT_CANCEL_ALL) != 0 || timeout_ms == 0) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (!s_active_clients_mutex ||
        xSemaphoreTake(s_active_clients_mutex, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    device_status_t status = DEVICE_STATUS_OK;
#define CANCEL_ACTIVE_GATEWAY_LANE(bit, slot, label) \
    do { \
        if (status == DEVICE_STATUS_OK && ((mask) & (bit))) { \
            status = cancel_active_client_locked((slot), (label)); \
        } \
    } while (0)
    CANCEL_ACTIVE_GATEWAY_LANE(GATEWAY_TRANSPORT_CANCEL_POLL,
                               s_active_poll_client, "poll");
    CANCEL_ACTIVE_GATEWAY_LANE(GATEWAY_TRANSPORT_CANCEL_STARTUP,
                               s_active_startup_client, "startup");
    CANCEL_ACTIVE_GATEWAY_LANE(GATEWAY_TRANSPORT_CANCEL_CAPABILITY_REFRESH,
                               s_active_capability_refresh_client, "capability-refresh");
    CANCEL_ACTIVE_GATEWAY_LANE(GATEWAY_TRANSPORT_CANCEL_ASSET,
                               s_active_asset_client, "asset");
    CANCEL_ACTIVE_GATEWAY_LANE(GATEWAY_TRANSPORT_CANCEL_FOREGROUND,
                               s_active_foreground_client, "foreground");
#undef CANCEL_ACTIVE_GATEWAY_LANE
    xSemaphoreGive(s_active_clients_mutex);
    return status;
}

void gateway_transport_cancel_foreground_request(uint32_t timeout_ms) {
    device_status_t status = gateway_transport_cancel_active_requests(
        GATEWAY_TRANSPORT_CANCEL_FOREGROUND, timeout_ms);
    if (status != DEVICE_STATUS_OK && status != DEVICE_STATUS_TIMEOUT) {
        ESP_LOGW(TAG, "foreground HTTP cancellation failed: status=%d", (int)status);
    }
}

void gateway_transport_cancel_capability_refresh(uint32_t timeout_ms) {
    device_status_t status = gateway_transport_cancel_active_requests(
        GATEWAY_TRANSPORT_CANCEL_CAPABILITY_REFRESH, timeout_ms);
    if (status != DEVICE_STATUS_OK && status != DEVICE_STATUS_TIMEOUT) {
        ESP_LOGW(TAG, "capability refresh HTTP cancellation failed: status=%d", (int)status);
    }
}

void gateway_transport_discard_asset_client(void) {
    if (!s_gateway_asset_http_mutex) return;
    if (xSemaphoreTake(s_gateway_asset_http_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        ESP_LOGW(TAG, "optional pet HTTP client cleanup deferred: lane busy");
        return;
    }
    if (s_gateway_asset_http_client) {
        esp_http_client_cleanup(s_gateway_asset_http_client);
        s_gateway_asset_http_client = NULL;
        s_gateway_asset_http_origin[0] = '\0';
        ESP_LOGI(TAG, "optional pet HTTP client released before wake restore");
    }
    xSemaphoreGive(s_gateway_asset_http_mutex);
}

void gateway_transport_set_gateway_url(const char *gateway_url) {
    taskENTER_CRITICAL(&s_transport_state_lock);
    strlcpy(s_gateway_url, gateway_url ? gateway_url : "", sizeof(s_gateway_url));
    taskEXIT_CRITICAL(&s_transport_state_lock);
}

device_status_t gateway_transport_init(const gateway_transport_host_t *host) {
    if (!host || !host->current_task_is_startup_pet_asset ||
        !host->start_gateway_ready_tasks ||
        !host->apply_deferred_startup_pet_asset || !host->start_setup_portal ||
        !host->staged_provisioning_pending ||
        !host->rollback_staged_provisioning ||
        !host->log_heap_snapshot ||
        !host->apply_server_time || !host->apply_ambient ||
        !host->set_handshake_welcome_queued ||
        !host->boot_session_id || !host->note_cold_start_pet_asset ||
        !host->apply_pet_asset || !host->clear_pet_asset ||
        !host->process_update_metadata || !host->append_tool_descriptors ||
        !host->persist_gateway_token) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    taskENTER_CRITICAL(&s_transport_state_lock);
    gateway_capability_projection_init(&s_capability_projection);
    const bool capabilities_initialized = gateway_capability_projection_set_effective(
        &s_capability_projection, local_gateway_capabilities());
    taskEXIT_CRITICAL(&s_transport_state_lock);
    if (!capabilities_initialized) return DEVICE_STATUS_INTERNAL_ERROR;
    s_host = *host;
    s_host_installed = true;
    s_http_mutex = xSemaphoreCreateMutex();
    if (!s_http_mutex) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    s_active_clients_mutex = xSemaphoreCreateMutex();
    if (!s_active_clients_mutex) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    s_gateway_poll_http_mutex = xSemaphoreCreateMutex();
    if (!s_gateway_poll_http_mutex) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    s_gateway_asset_http_mutex = xSemaphoreCreateMutex();
    if (!s_gateway_asset_http_mutex) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    s_gateway_startup_start_gate = xSemaphoreCreateBinary();
    if (!s_gateway_startup_start_gate) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    s_gateway_startup_stopped = xSemaphoreCreateBinary();
    if (!s_gateway_startup_stopped) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    taskENTER_CRITICAL(&s_transport_state_lock);
    s_gateway_startup_retiring = false;
    s_gateway_startup_exit_status = ESP_OK;
    s_gateway_startup_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_transport_state_lock);
    return DEVICE_STATUS_OK;
}
