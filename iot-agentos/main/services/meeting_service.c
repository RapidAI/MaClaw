#include "services/meeting_service.h"

#include <string.h>

#include "esp_err.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "psa/crypto.h"

#include "presentation/scene_presenter.h"
#include "services/ambient_service.h"
#include "meeting_recovery_service.h"
#include "meeting_recording_storage.h"
#include "operation_context.h"
#include "services/audio_arbitration_service.h"
#include "services/command_service.h"
#include "services/foreground_coordinator.h"
#include "services/gateway_transport.h"
#include "services/interaction_service.h"
#include "task_registry.h"

/* Keep the log tag identical to the original main.c owner so existing meeting
 * trace filters and hardware baseline comparisons stay valid. */
static const char *TAG = "maclaw_client";

#define MEETING_SAMPLE_RATE 16000
#define MEETING_DEFAULT_CHUNK_SIZE (1U << 20)
#define MEETING_MIN_CHUNK_SIZE (64U << 10)
#define MEETING_MAX_CHUNK_SIZE (8U << 20)
#define MEETING_IO_BUFFER_SIZE 16384
#define MEETING_RESUME_RETRY_INITIAL_MS 5000
#define MEETING_RESUME_RETRY_MAX_MS 300000

static bool meeting_stream_stop_requested(void *context) {
    (void)context;
    return meeting_service_worker_stop_requested();
}

typedef struct {
    const meeting_service_chunk_upload_t *chunk;
    uint32_t last_published;
} meeting_stream_progress_context_t;

static void meeting_stream_publish_progress(void *context, uint32_t transferred) {
    meeting_stream_progress_context_t *progress = context;
    const meeting_service_chunk_upload_t *chunk = progress ? progress->chunk : NULL;
    if (!chunk || !chunk->publish_progress ||
        (transferred != chunk->length &&
         transferred - progress->last_published < 256u * 1024u)) {
        return;
    }
    progress->last_published = transferred;
    scene_presenter_publish_upload_progress(chunk->completed_before + transferred,
                                            chunk->total_bytes,
                                            "正在上传录音");
}

static portMUX_TYPE s_meeting_state_lock = portMUX_INITIALIZER_UNLOCKED;

static volatile meeting_service_state_t s_meeting_state = MEETING_SERVICE_IDLE;
static bool s_meeting_available;
static size_t s_meeting_chunk_size = MEETING_DEFAULT_CHUNK_SIZE;
static bool s_meeting_capability_advertised;
static bool s_meeting_capability_operational;
static char s_meeting_base_path[MEETING_SERVICE_BASE_PATH_CAPACITY] = "/api/device-gateway/v1/meeting-recordings";
static char s_meeting_process_mode[12] = "keep";
static bool s_meeting_pending;
static int32_t s_meeting_next_chunk;
static int32_t s_meeting_phase;
static char s_meeting_recording_id[MEETING_SERVICE_RECORDING_ID_CAPACITY];
static volatile uint32_t s_meeting_elapsed_seconds;

static TaskHandle_t s_meeting_task;
static device_power_lease_t s_meeting_power_lease;
static SemaphoreHandle_t s_meeting_task_start_gate;
static SemaphoreHandle_t s_meeting_task_stopped;
static bool s_meeting_task_stop_requested;
static bool s_meeting_task_running;
/* A completed worker remains lifecycle-live until its immutable AUDIO
 * Registry identity has retired.  Preserve the result so a stale owner entry
 * can never be mistaken for a safe replacement opportunity. */
static bool s_meeting_task_retiring;
static esp_err_t s_meeting_task_exit_status = ESP_OK;
static bool s_meeting_task_registry_retirement_failed;
/* Published with the task handle so a system-sleep transaction can distinguish
 * a durable background resume pass from a user-facing recording session. */
static bool s_meeting_task_resume_only;
static bool s_resumed_worker_system_sleep_preparing;
static TaskHandle_t s_meeting_resume_supervisor_task;
static SemaphoreHandle_t s_meeting_resume_supervisor_start_gate;
static SemaphoreHandle_t s_meeting_resume_supervisor_stopped;
static bool s_meeting_resume_supervisor_stop_requested;
static bool s_meeting_resume_supervisor_starting;
/* System-sleep lifecycle state for the supervisory retry loop only. */
static bool s_resume_supervisor_system_sleep_preparing;
static bool s_resume_supervisor_restart_after_system_sleep;
/* A timed-out stop may leave this supervisor in the short completion →
 * Registry-unregister interval.  Its retiring generation alone may create the
 * ABORT replacement, otherwise the shared completion semaphore/immutable
 * Registry identity can be attributed to the wrong task. */
static bool s_resume_supervisor_system_sleep_restart_pending;
static bool s_resume_supervisor_retiring;
static esp_err_t s_resume_supervisor_exit_status = ESP_OK;
static bool s_resume_supervisor_registry_retirement_failed;
static TaskHandle_t s_meeting_capability_refresh_task;
static SemaphoreHandle_t s_meeting_capability_refresh_start_gate;
static SemaphoreHandle_t s_meeting_capability_refresh_stopped;
static bool s_meeting_capability_refresh_stop_requested;
static bool s_meeting_capability_refresh_starting;
/* Separate from the meeting recording state machine: this only fences the
 * short, restartable capability-handshake worker while a future system sleep
 * transaction is being prepared. */
static bool s_capability_refresh_system_sleep_preparing;
static bool s_capability_refresh_restart_after_system_sleep;
static bool s_capability_refresh_system_sleep_restart_pending;
static bool s_capability_refresh_retiring;
static esp_err_t s_capability_refresh_exit_status = ESP_OK;
static bool s_capability_refresh_registry_retirement_failed;

/* The worker receives this explicit context instead of inferring ownership
 * from a naked resume flag or global task handle. */
typedef struct {
    uint32_t generation;
    bool resume_only;
    /* Captured before the worker is created. It is a value-only Gateway
     * capability lease, never an HTTP/task/board handle. */
    gateway_capability_lease_t gateway_lease;
} meeting_task_context_t;

static meeting_service_host_t s_host;
static bool s_host_installed;
static esp_err_t stop_meeting_capability_refresh_task(uint32_t timeout_ms);
static esp_err_t stop_meeting_resume_supervisor(uint32_t timeout_ms);
static esp_err_t stop_meeting_task(uint32_t timeout_ms);

/* Mirror of the composition root's Device-status mapping so transport-facing
 * log lines keep their original esp_err_to_name() text. */
static esp_err_t device_status_to_esp_err(device_status_t status) {
    switch (status) {
        case DEVICE_STATUS_OK: return ESP_OK;
        case DEVICE_STATUS_INVALID_ARGUMENT: return ESP_ERR_INVALID_ARG;
        case DEVICE_STATUS_UNAVAILABLE: return ESP_ERR_NOT_SUPPORTED;
        case DEVICE_STATUS_BUSY: return ESP_ERR_INVALID_STATE;
        case DEVICE_STATUS_TIMEOUT: return ESP_ERR_TIMEOUT;
        case DEVICE_STATUS_NOT_FOUND: return ESP_ERR_NOT_FOUND;
        case DEVICE_STATUS_RESOURCE_EXHAUSTED: return ESP_ERR_NO_MEM;
        case DEVICE_STATUS_IO_ERROR: return ESP_FAIL;
        default: return ESP_ERR_INVALID_RESPONSE;
    }
}

static device_status_t meeting_status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
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

static void host_log_heap_snapshot(const char *stage) {
    if (s_host_installed && s_host.log_heap_snapshot) s_host.log_heap_snapshot(stage);
}

static void meeting_set_state(meeting_service_state_t state) {
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_state = state;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    switch (state) {
        case MEETING_SERVICE_RECORDING:
            foreground_coordinator_observe_acquire(FOREGROUND_OWNER_MEETING,
                                                   FOREGROUND_PRIORITY_CAPTURE,
                                                   FOREGROUND_SCENE_MEETING_RECORD);
            break;
        case MEETING_SERVICE_FINALIZING:
        case MEETING_SERVICE_UPLOADING:
        case MEETING_SERVICE_PROCESSING:
            foreground_coordinator_observe_acquire(FOREGROUND_OWNER_MEETING,
                                                   FOREGROUND_PRIORITY_PROGRESS,
                                                   FOREGROUND_SCENE_MEETING_UPLOAD);
            break;
        case MEETING_SERVICE_DONE:
        case MEETING_SERVICE_ERROR:
            foreground_coordinator_observe_acquire(FOREGROUND_OWNER_MEETING,
                                                   FOREGROUND_PRIORITY_RESULT,
                                                   FOREGROUND_SCENE_MEETING_RESULT);
            break;
        default:
            break;
    }
}

meeting_service_state_t meeting_service_state(void) {
    meeting_service_state_t state;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    state = s_meeting_state;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return state;
}

bool meeting_service_is_active(void) {
    meeting_service_state_t state = meeting_service_state();
    return state != MEETING_SERVICE_IDLE && state != MEETING_SERVICE_DONE &&
           state != MEETING_SERVICE_ERROR;
}

void meeting_service_request_finalize(void) {
    meeting_set_state(MEETING_SERVICE_FINALIZING);
}

bool meeting_service_pending(void) {
    bool pending;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    pending = s_meeting_pending;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return pending;
}

bool meeting_service_available(void) {
    bool available;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    available = s_meeting_available;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return available;
}

bool meeting_service_worker_running(void) {
    bool running;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    running = s_meeting_task_running;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return running;
}

static void meeting_service_recompute_capability_locked(void) {
    s_meeting_available = s_meeting_capability_advertised &&
                          s_meeting_capability_operational;
}

bool meeting_service_set_capability_descriptor(bool advertised,
                                               const char *base_path,
                                               int32_t chunk_size,
                                               const char *process_mode) {
    const bool valid = advertised && base_path && base_path[0] != '\0' &&
                       strlen(base_path) < sizeof(s_meeting_base_path) &&
                       chunk_size >= (int32_t)MEETING_MIN_CHUNK_SIZE &&
                       chunk_size <= (int32_t)MEETING_MAX_CHUNK_SIZE;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_capability_advertised = valid;
    if (valid) {
        strlcpy(s_meeting_base_path, base_path, sizeof(s_meeting_base_path));
        s_meeting_chunk_size = (size_t)chunk_size;
        strlcpy(s_meeting_process_mode,
                process_mode ? process_mode : "keep",
                sizeof(s_meeting_process_mode));
    } else {
        s_meeting_base_path[0] = '\0';
        s_meeting_chunk_size = MEETING_DEFAULT_CHUNK_SIZE;
        strlcpy(s_meeting_process_mode, "keep", sizeof(s_meeting_process_mode));
    }
    meeting_service_recompute_capability_locked();
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (valid) {
        ESP_LOGI(TAG, "meeting recording descriptor accepted: base=%s chunk=%u mode=%s",
                 s_meeting_base_path, (unsigned)s_meeting_chunk_size, s_meeting_process_mode);
    } else {
        ESP_LOGW(TAG, "meeting recording descriptor unavailable or invalid");
    }
    return valid;
}

void meeting_service_set_capability_operational(bool operational) {
    bool available;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_capability_operational = operational;
    meeting_service_recompute_capability_locked();
    available = s_meeting_available;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    ESP_LOGI(TAG, "meeting recording Gateway capability is %s",
             available ? "operational" : "not operational");
}

void meeting_service_base_path(char *out_base_path, uint32_t capacity) {
    taskENTER_CRITICAL(&s_meeting_state_lock);
    strlcpy(out_base_path, s_meeting_base_path, capacity);
    taskEXIT_CRITICAL(&s_meeting_state_lock);
}

/* A foreground meeting is an operation-context owner.  Keep this guard local
 * to the meeting flow while its UI and recorder state are migrated out of
 * main.c; background recovery deliberately has no foreground generation. */
static bool meeting_operation_is_current(uint32_t generation) {
    return generation != 0 && operation_context_matches(generation);
}

static bool meeting_gateway_lease_current(
    const gateway_capability_lease_t *lease) {
    return lease && gateway_transport_capability_lease_current(lease);
}

bool meeting_service_worker_stop_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    requested = s_meeting_task_stop_requested;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return requested;
}

bool meeting_service_current_task_is_worker(void) {
    bool matches;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    matches = s_meeting_task != NULL && s_meeting_task == xTaskGetCurrentTaskHandle();
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return matches;
}

bool meeting_service_current_task_is_capability_refresh(void) {
    bool matches;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    matches = s_meeting_capability_refresh_task != NULL &&
              s_meeting_capability_refresh_task == xTaskGetCurrentTaskHandle();
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return matches;
}

uintptr_t meeting_service_worker_owner_token(void) {
    uintptr_t token;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    token = (uintptr_t)s_meeting_task;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return token;
}

static esp_err_t save_meeting_recovery(bool pending, const char *recording_id,
                                       int32_t next_chunk, int32_t phase);

void meeting_service_load_recovery(void) {
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_pending = false;
    s_meeting_next_chunk = 0;
    s_meeting_phase = 0;
    s_meeting_recording_id[0] = '\0';
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    meeting_recovery_snapshot_t snapshot;
    device_status_t load_status = meeting_recovery_service_load(&snapshot);
    if (load_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "meeting recovery metadata unavailable: device status=%d",
                 (int)load_status);
        return;
    }
    bool storage_mounted = s_host_installed && s_host.storage_mounted &&
                           s_host.storage_mounted();
    /* Metadata and the retained WAV are independent durable objects.  Use
     * the uploader's exact validation path here rather than a weaker startup
     * size check, so a restart cannot advertise an object the uploader would
     * later reject. */
    device_status_t recording_status = DEVICE_STATUS_NOT_FOUND;
    meeting_recording_storage_handle_t *recording = NULL;
    uint32_t recording_size = 0;
    if (snapshot.pending && storage_mounted) {
        recording_status = meeting_recording_storage_open_for_upload(
            &recording, &recording_size);
        meeting_recording_storage_close(recording);
    }
    bool pending = snapshot.pending && storage_mounted &&
                   recording_status == DEVICE_STATUS_OK && recording_size > 0;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    if (pending) {
        s_meeting_next_chunk = snapshot.next_chunk;
        s_meeting_phase = snapshot.phase;
        strlcpy(s_meeting_recording_id, snapshot.recording_id,
                sizeof(s_meeting_recording_id));
    }
    s_meeting_pending = pending;
    taskEXIT_CRITICAL(&s_meeting_state_lock);

    /* Only a missing object is an unambiguous stale marker.  Do not erase a
     * marker for malformed or I/O-failed media: it is the remaining durable
     * evidence of an interrupted recording and may become diagnosable once
     * storage is available again. */
    if (snapshot.pending && storage_mounted &&
        recording_status == DEVICE_STATUS_NOT_FOUND) {
        esp_err_t clear_err = save_meeting_recovery(false, "", 0, 0);
        if (clear_err != ESP_OK) {
            ESP_LOGW(TAG, "meeting recovery marker is stale but could not be cleared: %s",
                     esp_err_to_name(clear_err));
        } else {
            ESP_LOGI(TAG, "cleared stale meeting recovery marker without retained audio");
        }
    } else if (snapshot.pending && storage_mounted &&
               recording_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "retained meeting WAV failed recovery validation: device status=%d; "
                      "keeping recovery metadata for diagnosis",
                 (int)recording_status);
    }
}

static esp_err_t save_meeting_recovery(bool pending, const char *recording_id,
                                       int32_t next_chunk, int32_t phase) {
    meeting_recovery_snapshot_t snapshot = {
        .pending = pending,
        .next_chunk = next_chunk,
        .phase = phase,
    };
    strlcpy(snapshot.recording_id, recording_id ? recording_id : "",
            sizeof(snapshot.recording_id));
    device_status_t status = meeting_recovery_service_save(&snapshot);
    if (status == DEVICE_STATUS_OK) {
        taskENTER_CRITICAL(&s_meeting_state_lock);
        s_meeting_pending = pending;
        s_meeting_next_chunk = next_chunk;
        s_meeting_phase = phase;
        strlcpy(s_meeting_recording_id, recording_id ? recording_id : "",
                sizeof(s_meeting_recording_id));
        taskEXIT_CRITICAL(&s_meeting_state_lock);
    }
    return device_status_to_platform_error(status);
}

static esp_err_t clear_meeting_recovery(bool delete_audio) {
    if (!delete_audio) return save_meeting_recovery(false, "", 0, 0);

    /* Metadata is the recovery admission record.  Delete the retained object
     * first, while leaving that record intact: a reset or VFS failure between
     * these operations becomes the safe, self-healing "stale marker + missing
     * WAV" case at the next boot.  Clearing metadata first could instead
     * orphan a locally retained meeting after successful Hub delivery, where
     * later recovery has no durable evidence that the object needs cleanup. */
    esp_err_t err = device_status_to_esp_err(meeting_recording_storage_clear());
    if (err != ESP_OK) return err;
    return save_meeting_recovery(false, "", 0, 0);
}

static void digest_hex(const uint8_t digest[32], char out[65]) {
    static const char hex[] = "0123456789abcdef";
    for (size_t i = 0; i < 32; ++i) {
        out[i * 2] = hex[digest[i] >> 4];
        out[i * 2 + 1] = hex[digest[i] & 15];
    }
    out[64] = '\0';
}

static esp_err_t hash_recording_range(void *storage_context,
                                 meeting_service_recording_reader_t read_range,
                                 size_t offset, size_t length,
                                 uint8_t *buffer, size_t buffer_size, char out_hex[65]) {
    if (!storage_context || !read_range || !buffer || buffer_size == 0 ||
        offset > UINT32_MAX || length > UINT32_MAX - offset) {
        return ESP_ERR_INVALID_ARG;
    }
    psa_hash_operation_t operation = PSA_HASH_OPERATION_INIT;
    psa_status_t status = psa_hash_setup(&operation, PSA_ALG_SHA_256);
    size_t remaining = length;
    while (status == PSA_SUCCESS && remaining > 0) {
        size_t wanted = remaining < buffer_size ? remaining : buffer_size;
        uint32_t count = 0;
        device_status_t read_status = read_range(storage_context,
                                                 (uint32_t)(offset + length - remaining),
                                                 buffer, (uint32_t)wanted, &count);
        if (read_status != DEVICE_STATUS_OK || count != wanted) {
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

static esp_err_t upload_pending_meeting(bool publish_state,
                                        const gateway_capability_lease_t *gateway_lease) {
    if (!meeting_gateway_lease_current(gateway_lease)) {
        return ESP_ERR_INVALID_STATE;
    }
    bool storage_mounted = s_host_installed && s_host.storage_mounted &&
                           s_host.storage_mounted();
    if (!storage_mounted) {
        return ESP_ERR_NOT_FOUND;
    }
    meeting_recording_storage_handle_t *recording = NULL;
    uint32_t file_size = 0;
    esp_err_t open_err = device_status_to_esp_err(
        meeting_recording_storage_open_for_upload(&recording, &file_size));
    if (open_err != ESP_OK) {
        ESP_LOGE(TAG, "retained meeting WAV is not recoverable: %s", esp_err_to_name(open_err));
        return open_err;
    }
    uint8_t *buffer = heap_caps_malloc(MEETING_IO_BUFFER_SIZE, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!buffer) buffer = malloc(MEETING_IO_BUFFER_SIZE);
    if (!buffer) {
        meeting_recording_storage_close(recording);
        return ESP_ERR_NO_MEM;
    }
    char recording_id[MEETING_SERVICE_RECORDING_ID_CAPACITY];
    taskENTER_CRITICAL(&s_meeting_state_lock);
    strlcpy(recording_id, s_meeting_recording_id, sizeof(recording_id));
    int next_chunk = (int)s_meeting_next_chunk;
    int phase = (int)s_meeting_phase;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    esp_err_t err = ESP_OK;
    if (recording_id[0] != '\0') {
        if (!meeting_gateway_lease_current(gateway_lease)) {
            err = ESP_ERR_INVALID_STATE;
        }
        char status[20] = {0};
        esp_err_t status_err = err == ESP_OK
            ? (esp_err_t)s_host.recording_get_status(recording_id, status, sizeof(status))
            : err;
        if (status_err == ESP_ERR_NOT_FOUND) {
            recording_id[0] = '\0';
            next_chunk = 0;
            phase = 0;
            err = save_meeting_recovery(true, "", 0, 0);
        } else if (status_err != ESP_OK) {
            err = status_err;
        } else if (!strcmp(status, "processing") || !strcmp(status, "ready")) {
            phase = 2;
            next_chunk = (int)((size_t)file_size + s_meeting_chunk_size - 1) /
                         (int)s_meeting_chunk_size;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
        } else if (!strcmp(status, "uploaded") || !strcmp(status, "failed")) {
            phase = 1;
            next_chunk = (int)((size_t)file_size + s_meeting_chunk_size - 1) /
                         (int)s_meeting_chunk_size;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
        } else if (strcmp(status, "uploading")) {
            err = ESP_ERR_INVALID_STATE;
        }
    }
    if (err == ESP_OK && recording_id[0] == '\0') {
        if (!meeting_gateway_lease_current(gateway_lease)) {
            err = ESP_ERR_INVALID_STATE;
        } else {
            err = (esp_err_t)s_host.recording_create(recording_id, sizeof(recording_id));
        }
        if (err == ESP_OK) err = save_meeting_recovery(true, recording_id, 0, 0);
        next_chunk = 0;
        phase = 0;
    }
    int chunks = (int)((file_size + s_meeting_chunk_size - 1) / s_meeting_chunk_size);
    for (int index = next_chunk; err == ESP_OK && index < chunks; ++index) {
        if (meeting_service_worker_stop_requested() ||
            !meeting_gateway_lease_current(gateway_lease)) {
            err = ESP_ERR_INVALID_STATE;
            break;
        }
        size_t offset = (size_t)index * s_meeting_chunk_size;
        size_t length = file_size - offset;
        if (length > s_meeting_chunk_size) length = s_meeting_chunk_size;
        char chunk_hash[65];
        err = hash_recording_range(recording, meeting_recording_storage_read_range,
                                   offset, length, buffer, MEETING_IO_BUFFER_SIZE, chunk_hash);
        if (err == ESP_OK) {
            if (!meeting_gateway_lease_current(gateway_lease)) {
                err = ESP_ERR_INVALID_STATE;
            }
        }
        if (err == ESP_OK) {
            if (publish_state) {
                scene_presenter_publish_upload_progress(offset, file_size, "正在上传录音");
            }
            meeting_service_chunk_upload_t chunk = {
                .recording_id = recording_id,
                .index = index,
                .offset = (uint32_t)offset,
                .length = (uint32_t)length,
                .sha256_hex = chunk_hash,
                .completed_before = (uint32_t)offset,
                .total_bytes = (uint32_t)file_size,
                .publish_progress = publish_state,
                .storage_context = recording,
                .read_range = meeting_recording_storage_read_range,
                .io_buffer = buffer,
                .io_buffer_size = MEETING_IO_BUFFER_SIZE,
            };
            char path[MEETING_SERVICE_BASE_PATH_CAPACITY +
                      MEETING_SERVICE_RECORDING_ID_CAPACITY + 48];
            char base_path[MEETING_SERVICE_BASE_PATH_CAPACITY];
            meeting_service_base_path(base_path, sizeof(base_path));
            const int path_length = snprintf(path, sizeof(path), "%s/%s/chunks/%d",
                                             base_path, recording_id, index);
            if (path_length <= 0 || path_length >= (int)sizeof(path)) {
                err = ESP_ERR_INVALID_SIZE;
            } else {
                meeting_stream_progress_context_t progress = {
                    .chunk = &chunk,
                };
                err = (esp_err_t)gateway_transport_stream_meeting_chunk(
                    &(gateway_transport_stream_request_t){
                        .path = path,
                        .sha256_hex = chunk_hash,
                        .chunk_index = index,
                        .storage_context = chunk.storage_context,
                        .read_range = chunk.read_range,
                        .offset = chunk.offset,
                        .length = chunk.length,
                        .io_buffer = chunk.io_buffer,
                        .io_buffer_size = chunk.io_buffer_size,
                        .stop_requested = meeting_stream_stop_requested,
                        .progress = meeting_stream_publish_progress,
                        .progress_context = &progress,
                    });
            }
        }
        if (err == ESP_OK) {
            next_chunk = index + 1;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
            if (publish_state) {
                scene_presenter_publish_upload_progress(offset + length, file_size, "正在上传录音");
            }
        }
        if (!publish_state && err == ESP_OK &&
            interaction_service_foreground_http_requested()) {
            // Recovery metadata is already durable at this chunk boundary. End
            // this pass cleanly and let the foreground command acquire HTTP;
            // the reconnect/resume path continues from next_chunk later.
            ESP_LOGI(TAG, "background meeting resume yielded after chunk %d", index);
            err = ESP_ERR_TIMEOUT;
        }
    }
    gateway_transport_reset_meeting_stream();
    char whole_hash[65];
    if (err == ESP_OK && !meeting_service_worker_stop_requested() &&
        meeting_gateway_lease_current(gateway_lease) && phase < 1) {
        if (publish_state) meeting_set_state(MEETING_SERVICE_FINALIZING);
        if (publish_state) scene_presenter_publish_upload_progress(file_size, file_size, "正在校验录音");
        err = hash_recording_range(recording, meeting_recording_storage_read_range,
                                   0, file_size, buffer, MEETING_IO_BUFFER_SIZE, whole_hash);
        if (err == ESP_OK) {
            uint32_t pcm_bytes = file_size > 44 ? (uint32_t)(file_size - 44) : 0;
            double duration = (double)pcm_bytes / (MEETING_SAMPLE_RATE * 2.0);
            char payload[192];
            int length = snprintf(payload, sizeof(payload),
                                  "{\"chunks\":%d,\"sha256\":\"%s\",\"duration_sec\":%.3f}",
                                  chunks, whole_hash, duration);
            if (length <= 0 || length >= (int)sizeof(payload)) {
                err = ESP_ERR_INVALID_SIZE;
            } else if (!meeting_gateway_lease_current(gateway_lease)) {
                /* Hashing a retained recording can take long enough for the
                 * Hub's capability decision to change.  Revalidate directly
                 * at the Gateway transaction boundary, not only before the
                 * local CPU work began. */
                err = ESP_ERR_INVALID_STATE;
            } else {
                err = (esp_err_t)s_host.recording_post_action(
                    recording_id, "complete", payload, 200, 200);
            }
        }
        if (err == ESP_OK) {
            phase = 1;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
        }
    }
    if (err == ESP_OK && !meeting_service_worker_stop_requested() &&
        meeting_gateway_lease_current(gateway_lease) && phase >= 1) {
        char status[20] = {0};
        if ((esp_err_t)s_host.recording_get_status(recording_id, status, sizeof(status)) == ESP_OK &&
            (!strcmp(status, "processing") || !strcmp(status, "ready"))) {
            phase = 2;
            (void)save_meeting_recovery(true, recording_id, next_chunk, phase);
        }
    }
    if (err == ESP_OK && !meeting_service_worker_stop_requested() &&
        meeting_gateway_lease_current(gateway_lease) && phase < 2) {
        if (publish_state) meeting_set_state(MEETING_SERVICE_PROCESSING);
        if (publish_state) scene_presenter_publish_upload_progress(file_size, file_size, "正在提交处理");
        char payload[48];
        int length = snprintf(payload, sizeof(payload), "{\"mode\":\"%s\"}", s_meeting_process_mode);
        if (length <= 0 || length >= (int)sizeof(payload)) {
            err = ESP_ERR_INVALID_SIZE;
        } else if (!meeting_gateway_lease_current(gateway_lease)) {
            /* Keep the check adjacent to the external side effect: a state
             * publication or payload construction above must not create a
             * stale authorization window. */
            err = ESP_ERR_INVALID_STATE;
        } else {
            err = (esp_err_t)s_host.recording_post_action(
                recording_id, "process", payload, 200, 202);
        }
        if (err == ESP_OK) {
            phase = 2;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
        }
    }
    free(buffer);
    meeting_recording_storage_close(recording);
    if (err == ESP_OK && (meeting_service_worker_stop_requested() ||
                          !meeting_gateway_lease_current(gateway_lease))) {
        err = ESP_ERR_INVALID_STATE;
    }
    if (err == ESP_OK) {
        err = clear_meeting_recovery(true);
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "meeting delivered but local cleanup failed: %s", esp_err_to_name(err));
        }
    }
    return err;
}

static bool meeting_start(bool resume_only);

static void meeting_task(void *arg) {
    meeting_task_context_t *context = arg;
    uint32_t operation_generation = 0;
    bool resume_only = false;
    gateway_capability_lease_t gateway_lease = {0};
    if (!context) {
        vTaskDelete(NULL);
        return;
    }
    /* Publish s_meeting_task/s_meeting_task_running before this worker can
     * complete a fast local failure.  Otherwise a cross-core task create can
     * leave the creator writing a stale handle after the worker cleared it. */
    if (!s_meeting_task_start_gate ||
        xSemaphoreTake(s_meeting_task_start_gate, portMAX_DELAY) != pdTRUE) {
        free(context);
        context = NULL;
        ESP_LOGW(TAG, "meeting task start gate unavailable");
        goto finish;
    }
    operation_generation = context->generation;
    resume_only = context->resume_only;
    gateway_lease = context->gateway_lease;
    free(context);
    context = NULL;
    if (meeting_service_worker_stop_requested()) goto finish;
    if (!meeting_gateway_lease_current(&gateway_lease)) {
        ESP_LOGW(TAG, "stale meeting worker discarded: Gateway capability lease expired");
        goto finish;
    }
    if (!resume_only && !meeting_operation_is_current(operation_generation)) {
        ESP_LOGW(TAG, "stale meeting worker discarded: generation=%lu",
                 (unsigned long)operation_generation);
        goto finish;
    }
    if (resume_only) {
        // Recovery is a background transfer. It must not take over the pet UI,
        // publish an active meeting state, or block a new short voice command.
        ESP_LOGI(TAG, "background meeting resume started");
    } else {
        /* Do not publish a foreground meeting surface unless this worker still
         * owns the operation slot.  The current interaction lock makes this
         * normally true, but the explicit guard keeps UI ownership correct as
         * future cancellation and restart paths become asynchronous. */
        if (!meeting_operation_is_current(operation_generation)) goto finish;
        meeting_set_state(MEETING_SERVICE_STARTING);
        meeting_recording_storage_handle_t *recording = NULL;
        esp_err_t start_err = device_status_to_esp_err(
            meeting_recording_storage_create(&recording));
        if (start_err != ESP_OK) {
            meeting_set_state(MEETING_SERVICE_ERROR);
            ambient_service_apply_pet_state("alert");
            scene_presenter_publish_message("录音失败", "无法创建录音文件");
            goto finish;
        }
        if (start_err == ESP_OK) {
            start_err = save_meeting_recovery(true, "", 0, 0);
            if (start_err != ESP_OK) {
                ESP_LOGE(TAG, "meeting start: recovery metadata failed: %s",
                         esp_err_to_name(start_err));
            }
        }
        if (start_err == ESP_OK) {
            start_err = device_status_to_esp_err(audio_arbitration_stream_start());
            if (start_err != ESP_OK) {
                ESP_LOGE(TAG, "meeting start: audio stream failed: %s",
                         esp_err_to_name(start_err));
            }
        }
        if (start_err != ESP_OK) {
            meeting_recording_storage_close(recording);
            // Startup produced no PCM. Clear the marker and placeholder so a
            // transient microphone/mutex failure cannot permanently turn every
            // later double tap into a bogus retained-file recovery attempt.
            (void)clear_meeting_recovery(true);
            meeting_set_state(MEETING_SERVICE_ERROR);
            ambient_service_apply_pet_state("alert");
            scene_presenter_publish_message("录音失败", "麦克风或存储不可用");
            goto finish;
        }
        int16_t samples[512];
        uint64_t total_samples = 0;
        s_meeting_elapsed_seconds = 0;
        uint32_t last_elapsed = UINT32_MAX;
        bool last_paused = false;
        meeting_set_state(MEETING_SERVICE_RECORDING);
        ambient_service_apply_pet_state("listening");
        scene_presenter_publish_recording_mode(true);
        scene_presenter_publish_recording_visual(true, false, 0);
        while (s_meeting_state == MEETING_SERVICE_RECORDING ||
               s_meeting_state == MEETING_SERVICE_PAUSED) {
            if (meeting_service_worker_stop_requested()) {
                meeting_set_state(MEETING_SERVICE_ERROR);
                break;
            }
            if (!meeting_operation_is_current(operation_generation)) {
                meeting_set_state(MEETING_SERVICE_ERROR);
                break;
            }
            if (!meeting_gateway_lease_current(&gateway_lease)) {
                /* Keep any already captured PCM recoverable, but do not let
                 * an unaccepted meeting session cross into remote upload. */
                ESP_LOGW(TAG, "meeting recording stopped: Gateway capability lease expired");
                meeting_set_state(MEETING_SERVICE_ERROR);
                break;
            }
            uint32_t count = 0;
            uint16_t level = 0;
            esp_err_t capture = device_status_to_esp_err(
                audio_arbitration_stream_read(samples, 512, &count, &level));
            if (capture != ESP_OK) {
                meeting_set_state(MEETING_SERVICE_ERROR);
                break;
            }
            bool paused = s_meeting_state == MEETING_SERVICE_PAUSED;
            if (!paused && count > 0) {
                // A paused meeting keeps the I2S reader alive to retain bus
                // ownership, but its samples are neither persisted nor shown.
                // Pushing them into the renderer made resume reveal a strip of
                // audio captured while the user believed recording was paused.
                scene_presenter_push_recording_pcm(samples, count);
                if (meeting_recording_storage_append_pcm(recording, samples, count) !=
                    DEVICE_STATUS_OK) {
                    meeting_set_state(MEETING_SERVICE_ERROR);
                    break;
                }
                total_samples += count;
            }
            uint32_t elapsed = (uint32_t)(total_samples / MEETING_SAMPLE_RATE);
            s_meeting_elapsed_seconds = elapsed;
            // While paused, Bread Compact keeps the frozen meter exactly as it
            // was at the pause boundary. Passing a synthetic zero level through
            // the normal attack/release path made EchoEar's supposedly paused
            // waveform visibly decay for every discarded 512-sample block.
            // The visual-state transition below already recolours the frozen
            // bars and applies the paused quiet display treatment.
            if (!paused) scene_presenter_publish_audio_level(level, elapsed);
            // The timer deliberately freezes while paused, so elapsed alone
            // cannot represent this state transition. Publish it immediately
            // and let the shared recorder switch its rule, copy and waveform.
            if (elapsed != last_elapsed || paused != last_paused) {
                scene_presenter_publish_recording_visual(true, paused, elapsed);
                last_elapsed = elapsed;
                last_paused = paused;
            }
        }
        audio_arbitration_stream_stop();
        meeting_service_state_t stopped_state = s_meeting_state;
        esp_err_t finalize_err = total_samples > 0
                                      ? device_status_to_esp_err(
                                            meeting_recording_storage_finalize(recording, total_samples))
                                     : ESP_ERR_INVALID_SIZE;
        if (!meeting_operation_is_current(operation_generation)) {
            meeting_recording_storage_close(recording);
            goto finish;
        }
        if (stopped_state == MEETING_SERVICE_FINALIZING && finalize_err == ESP_OK) {
            meeting_recording_storage_close(recording);
            meeting_set_state(MEETING_SERVICE_UPLOADING);
            // Meeting delivery has its own status surface. Reusing the normal
            // command "thinking" pet made a completed meeting look like a
            // short voice command and allowed ambient frames to replace it.
            command_service_set_display_locked(true);
            scene_presenter_publish_command_display_lock(true);
            scene_presenter_publish_recording_visual(false, false, 0);
            scene_presenter_publish_upload_progress(0, 1, "正在准备上传");
        } else {
            meeting_recording_storage_close(recording);
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
            scene_presenter_publish_recording_visual(false, false, 0);
            meeting_set_state(MEETING_SERVICE_ERROR);
            ambient_service_apply_pet_state("alert");
            scene_presenter_publish_message("录音失败", "文件已保留待恢复");
            goto finish;
        }
    }
    // MultiNet keeps its model, task stack and inference buffers alive even
    // while microphone capture is merely paused. On this ESP32-S3 that leaves
    // the internal DMA heap too fragmented for hardware AES during HTTPS PUT
    // (mbedTLS reports -0x0084). Fully unload it for delivery, then restore the
    // hands-free listener after the HTTP/NVS work has finished.
    host_log_heap_snapshot("meeting-upload-before-wake-stop");
    int32_t wake_stop_err = s_host.wake_word_stop ? s_host.wake_word_stop()
                                                  : (int32_t)ESP_ERR_INVALID_STATE;
    if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "offline wake stop before meeting upload: %s",
                 esp_err_to_name((esp_err_t)wake_stop_err));
    }
    host_log_heap_snapshot("meeting-upload-after-wake-stop");

    if (meeting_service_worker_stop_requested()) goto finish;
    if (!resume_only && !meeting_operation_is_current(operation_generation)) goto finish;
    esp_err_t upload_err = upload_pending_meeting(!resume_only, &gateway_lease);

    if (upload_err == ESP_OK) {
        if (!resume_only && meeting_operation_is_current(operation_generation)) {
            meeting_set_state(MEETING_SERVICE_DONE);
            ambient_service_apply_pet_state("done");
            scene_presenter_publish_message("会议记录已保存", "可在文稿库中查看");
            /* A rollback must not spend the whole result dwell time waiting
             * for this worker. The same notification used by stop wakes this
             * cosmetic delay without changing the durable upload outcome. */
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(3000));
            command_service_set_display_locked(false);
            scene_presenter_publish_command_display_lock(false);
            ambient_service_apply_pet_state("idle");
        } else {
            ESP_LOGI(TAG, "background meeting resume delivered");
        }
    } else {
        char recording_id[MEETING_SERVICE_RECORDING_ID_CAPACITY];
        int32_t next_chunk;
        int32_t phase;
        taskENTER_CRITICAL(&s_meeting_state_lock);
        strlcpy(recording_id, s_meeting_recording_id, sizeof(recording_id));
        next_chunk = s_meeting_next_chunk;
        phase = s_meeting_phase;
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        ESP_LOGE(TAG, "meeting upload pass failed: %s resume=%s id=%s next=%ld phase=%ld",
                 esp_err_to_name(upload_err), resume_only ? "yes" : "no",
                 recording_id, (long)next_chunk, (long)phase);
        host_log_heap_snapshot("meeting-upload-fail");
        if (!resume_only && meeting_operation_is_current(operation_generation)) {
            meeting_set_state(MEETING_SERVICE_ERROR);
            ambient_service_apply_pet_state("alert");
            if (upload_err == ESP_ERR_INVALID_STATE &&
                !meeting_gateway_lease_current(&gateway_lease)) {
                /* This is an authorization/capability transition, not a
                 * connectivity failure. The retained audio remains durable,
                 * but retry must wait for a newly negotiated Gateway lease. */
                scene_presenter_publish_message("录音已保留", "网关能力变更后可续传");
            } else {
                scene_presenter_publish_message("上传未完成", "联网后将自动续传");
            }
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(2200));
            command_service_set_display_locked(false);
            scene_presenter_publish_command_display_lock(false);
            ambient_service_apply_pet_state("idle");
        } else {
            ESP_LOGW(TAG, "background meeting resume deferred until next reconnect");
        }
    }
finish:
    /* The meeting has no user cancel action once finalization/upload begins,
     * but every success/error/recovery exit still claims the same terminal
     * token.  This makes a late worker unable to publish a second terminal
     * outcome after another foreground operation owns the slot. */
    if (!resume_only) (void)operation_context_commit_terminal(operation_generation);
    taskENTER_CRITICAL(&s_meeting_state_lock);
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    s_meeting_task_retiring = true;
    device_power_lease_t meeting_lease = s_meeting_power_lease;
    s_meeting_power_lease = DEVICE_POWER_LEASE_INVALID;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (!resume_only) device_power_lease_release(meeting_lease);
    if (!resume_only) {
        // Error exits before the normal success/deferred UI cleanup still
        // need to release the display for the ambient screen.
        command_service_set_display_locked(false);
        scene_presenter_publish_command_display_lock(false);
        interaction_service_admission_give();
    }
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_AUDIO, (void *)self, 10);
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_task_exit_status = registry_err;
    if (s_meeting_task == self) s_meeting_task = NULL;
    s_meeting_task_running = false;
    s_meeting_task_resume_only = false;
    s_meeting_task_retiring = false;
    if (registry_err != ESP_OK) {
        s_meeting_task_stop_requested = true;
        s_meeting_task_registry_retirement_failed = true;
    }
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (s_meeting_task_stopped) xSemaphoreGive(s_meeting_task_stopped);
    if (registry_err == ESP_OK && s_host.schedule_wake_restart) s_host.schedule_wake_restart();
    vTaskDelete(NULL);
}

static esp_err_t stop_meeting_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_task_stop_requested = true;
    task = s_meeting_task;
    const esp_err_t exit_status = s_meeting_task_exit_status;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (!task) return exit_status;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    /* The meeting worker owns the audio stream mutex.  Do not release it from
     * this coordinator: FreeRTOS mutex ownership is task-local and a cross-
     * task give would corrupt the next foreground capture.  The worker checks
     * the token after its bounded read and releases the stream itself. */
    const uint32_t cancel_guard_ms = remaining_timeout_ms(deadline_us);
    if (cancel_guard_ms != 0) {
        (void)gateway_transport_cancel_meeting_stream((const void *)task,
                                                       cancel_guard_ms);
    }
    if (s_meeting_task_start_gate) xSemaphoreGive(s_meeting_task_start_gate);
    xTaskNotifyGive(task);
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (!s_meeting_task_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_meeting_task_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_meeting_state_lock);
    const esp_err_t completed_status = s_meeting_task_exit_status;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (completed_status != ESP_OK) return completed_status;
    ESP_LOGI(TAG, "meeting worker stopped; retained audio remains resumable");
    return ESP_OK;
}

static esp_err_t stop_meeting_task_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    task = s_meeting_task;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_meeting_task(timeout_ms);
}

static bool meeting_start(bool resume_only) {
    bool storage_mounted = s_host_installed && s_host.storage_mounted &&
                           s_host.storage_mounted();
    if (!storage_mounted) {
        ESP_LOGW(TAG, "meeting start refused: storage is not mounted");
        return false;
    }
    if (!resume_only && !meeting_service_available()) {
        ESP_LOGW(TAG, "meeting start refused: capability is unavailable");
        return false;
    }
    if (!gateway_transport_is_paired()) {
        ESP_LOGW(TAG, "meeting start refused: device is not paired");
        return false;
    }
    gateway_capability_lease_t gateway_lease = {0};
    if (!gateway_transport_capture_capability_lease(
            GATEWAY_CAPABILITY_MEETING_RECORDER, &gateway_lease)) {
        ESP_LOGW(TAG, "meeting start refused: Gateway capability is not operational");
        return false;
    }
    taskENTER_CRITICAL(&s_meeting_state_lock);
    if (s_resumed_worker_system_sleep_preparing || s_meeting_task_running ||
        s_meeting_task_retiring || s_meeting_task_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return false;
    }
    s_meeting_task_running = true;
    /* Clear any old completed-worker classification before the new handle is
     * published. The later successful publication installs the exact mode. */
    s_meeting_task_resume_only = false;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (!s_meeting_task_start_gate || !s_meeting_task_stopped ||
        !gateway_transport_meeting_stream_ready()) {
        taskENTER_CRITICAL(&s_meeting_state_lock);
        s_meeting_task_running = false;
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        ESP_LOGE(TAG, "meeting task lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_meeting_task_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_task_stop_requested = false;
    s_meeting_task_exit_status = ESP_OK;
    s_meeting_task_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (!resume_only && !interaction_service_admission_take(1500)) {
        ESP_LOGI(TAG, "meeting start deferred: foreground interaction owns the lock");
        taskENTER_CRITICAL(&s_meeting_state_lock);
        s_meeting_task_running = false;
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return false;
    }
    device_operation_context_t operation = {0};
    if (!resume_only) {
        device_status_t operation_status = operation_context_begin(
            DEVICE_OPERATION_KIND_MEETING_RECORDING, 0, &operation);
        if (operation_status != DEVICE_STATUS_OK) {
            interaction_service_admission_give();
            taskENTER_CRITICAL(&s_meeting_state_lock);
            s_meeting_task_running = false;
            taskEXIT_CRITICAL(&s_meeting_state_lock);
            ESP_LOGI(TAG, "meeting operation admission rejected: status=%d", (int)operation_status);
            return false;
        }
    }
    if (!resume_only) scene_presenter_cancel_ready_prompt();
    device_power_lease_t meeting_lease = DEVICE_POWER_LEASE_INVALID;
    if (!resume_only) {
        device_status_t lease_status = device_power_lease_acquire(
            DEVICE_POWER_LEASE_OWNER_MEETING_RECORDING, &meeting_lease);
        if (lease_status != DEVICE_STATUS_OK) {
            (void)operation_context_commit_terminal(operation.generation);
            interaction_service_admission_give();
            taskENTER_CRITICAL(&s_meeting_state_lock);
            s_meeting_task_running = false;
            taskEXIT_CRITICAL(&s_meeting_state_lock);
            ESP_LOGW(TAG, "meeting start rejected: power lease unavailable status=%d",
                     (int)lease_status);
            return false;
        }
        taskENTER_CRITICAL(&s_meeting_state_lock);
        s_meeting_power_lease = meeting_lease;
        taskEXIT_CRITICAL(&s_meeting_state_lock);
    }
    meeting_task_context_t *context = calloc(1, sizeof(*context));
    if (!context) {
        device_power_lease_release(meeting_lease);
        taskENTER_CRITICAL(&s_meeting_state_lock);
        s_meeting_power_lease = DEVICE_POWER_LEASE_INVALID;
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        if (!resume_only) (void)operation_context_commit_terminal(operation.generation);
        if (!resume_only) interaction_service_admission_give();
        taskENTER_CRITICAL(&s_meeting_state_lock);
        s_meeting_task_running = false;
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return false;
    }
    *context = (meeting_task_context_t){
        .generation = operation.generation,
        .resume_only = resume_only,
        .gateway_lease = gateway_lease,
    };
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
                                     context, 5, &handle);
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_task = created == pdPASS ? handle : NULL;
    if (created == pdPASS) s_meeting_task_resume_only = resume_only;
    else s_meeting_task_running = false;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (created != pdPASS) {
        free(context);
        device_power_lease_release(meeting_lease);
        taskENTER_CRITICAL(&s_meeting_state_lock);
        s_meeting_power_lease = DEVICE_POWER_LEASE_INVALID;
        s_meeting_task_resume_only = false;
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        if (!resume_only) (void)operation_context_commit_terminal(operation.generation);
        if (!resume_only) interaction_service_admission_give();
        host_log_heap_snapshot("meeting-task-create-fail");
        return false;
    }
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_AUDIO,
        .name = "meeting_worker",
        .context = (void *)handle,
        .stop = stop_meeting_task_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register meeting worker: %s", esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_meeting_state_lock);
        s_meeting_task_stop_requested = true;
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        xSemaphoreGive(s_meeting_task_start_gate);
        (void)stop_meeting_task(500);
        return false;
    }
    xSemaphoreGive(s_meeting_task_start_gate);
    return true;
}

device_status_t meeting_service_prepare_resumed_worker_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    bool stop_resumed_worker = false;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    if (!s_host_installed) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (s_resumed_worker_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* `running` becomes true before the task handle is published. This is a
     * real creation transaction, not an idle worker, so do not claim it can
     * safely be parked without a resume record. */
    if ((s_meeting_task_running && !s_meeting_task) || s_meeting_task_retiring ||
        s_meeting_task_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    if (s_meeting_task && !s_meeting_task_resume_only) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        /* A foreground recording should already be protected by its Power
         * lease. Keep a local fail-closed defence for future callers. */
        return DEVICE_STATUS_BUSY;
    }
    s_resumed_worker_system_sleep_preparing = true;
    stop_resumed_worker = s_meeting_task != NULL;
    taskEXIT_CRITICAL(&s_meeting_state_lock);

    if (!stop_resumed_worker) return DEVICE_STATUS_OK;
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return DEVICE_STATUS_TIMEOUT;
    return meeting_status_from_esp_err(stop_meeting_task(remaining_ms));
}

void meeting_service_abort_resumed_worker_system_sleep_prepare(void) {
    taskENTER_CRITICAL(&s_meeting_state_lock);
    /* Retained chunk cursor and pending marker are written by the worker.
     * The meeting resume supervisor is the sole owner that may recreate it,
     * so this abort intentionally only reopens its admission fence. */
    if (!s_meeting_task_registry_retirement_failed) {
        s_resumed_worker_system_sleep_preparing = false;
    }
    taskEXIT_CRITICAL(&s_meeting_state_lock);
}

device_status_t meeting_service_commit_resumed_worker_network_restart(void) {
    taskENTER_CRITICAL(&s_meeting_state_lock);
    if (!s_host_installed) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (!s_resumed_worker_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* Resume worker recreation belongs solely to the next explicit Gateway
     * rearm. The durable cursor remains intact for that new generation. */
    s_resumed_worker_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return DEVICE_STATUS_OK;
}

bool meeting_service_start_recording(void) {
    return meeting_start(false);
}

static bool meeting_resume_supervisor_stop_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    requested = s_meeting_resume_supervisor_stop_requested;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return requested;
}

/* This registry owner controls only the supervisory retry loop. Its ordinary
 * lifecycle stop deliberately leaves an existing resume worker alone; the
 * future system-sleep coordinator has a separate, ordered participant that
 * first stops this supervisor and then quiesces that worker's durable upload
 * transaction. */
static void meeting_resume_supervisor_task(void *arg) {
    (void)arg;
    if (!s_meeting_resume_supervisor_start_gate ||
        xSemaphoreTake(s_meeting_resume_supervisor_start_gate, portMAX_DELAY) != pdTRUE) {
        ESP_LOGW(TAG, "meeting resume supervisor start gate unavailable");
        goto finish;
    }

    uint32_t retry_ms = MEETING_RESUME_RETRY_INITIAL_MS;
    while (meeting_service_pending() && !meeting_resume_supervisor_stop_requested()) {
        bool network_available = device_connectivity_is_active_uplink_ready();
        bool gateway_paired = gateway_transport_is_paired();
        if (!device_connectivity_is_provisioning_active() && gateway_paired && network_available &&
            !meeting_service_worker_running() &&
            !interaction_service_foreground_http_requested()) {
            // MultiNet can consume the final internal task-stack block before
            // this low-priority supervisor gets scheduled. Unload it here so
            // the resumable worker can be created; meeting_task() restores it
            // after delivery.
            int32_t wake_stop_err = s_host.wake_word_stop ? s_host.wake_word_stop()
                                                          : (int32_t)ESP_ERR_INVALID_STATE;
            if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
                ESP_LOGW(TAG, "offline wake stop before resume worker: %s",
                         esp_err_to_name((esp_err_t)wake_stop_err));
            }
            if (meeting_resume_supervisor_stop_requested()) break;
            host_log_heap_snapshot("meeting-resume-before-task-create");
            if (meeting_start(true)) {
                // The worker persists progress at every chunk. Wait until that
                // pass finishes before deciding whether another retry is needed.
                while (meeting_service_worker_running() && !meeting_resume_supervisor_stop_requested()) {
                    (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(500));
                }
                if (meeting_resume_supervisor_stop_requested() || !meeting_service_pending()) break;
                // A foreground command may have intentionally preempted this
                // pass. Resume quickly after it releases HTTP instead of
                // escalating the outage backoff to several minutes.
                if (interaction_service_foreground_http_requested()) {
                    while (interaction_service_foreground_http_requested() &&
                           !meeting_resume_supervisor_stop_requested()) {
                        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(250));
                    }
                    if (meeting_resume_supervisor_stop_requested()) break;
                    retry_ms = MEETING_RESUME_RETRY_INITIAL_MS;
                    continue;
                }
            } else if (!device_connectivity_is_provisioning_active() && !meeting_resume_supervisor_stop_requested()) {
                int32_t wake_start_err = s_host.wake_word_start ? s_host.wake_word_start()
                                                                : (int32_t)ESP_ERR_INVALID_STATE;
                if (wake_start_err != ESP_OK && wake_start_err != ESP_ERR_INVALID_STATE) {
                    ESP_LOGW(TAG, "offline wake restart after resume create failure: %s",
                             esp_err_to_name((esp_err_t)wake_start_err));
                }
            }
        }
        if (meeting_resume_supervisor_stop_requested()) break;
        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(retry_ms));
        if (retry_ms < MEETING_RESUME_RETRY_MAX_MS) {
            retry_ms *= 2;
            if (retry_ms > MEETING_RESUME_RETRY_MAX_MS) retry_ms = MEETING_RESUME_RETRY_MAX_MS;
        }
    }

finish:
    bool restart_after_system_sleep_abort = false;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    s_resume_supervisor_retiring = true;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_resume_supervisor_exit_status = registry_err;
    if (s_meeting_resume_supervisor_task == self) {
        s_meeting_resume_supervisor_task = NULL;
    }
    s_meeting_resume_supervisor_starting = false;
    s_resume_supervisor_retiring = false;
    if (registry_err != ESP_OK) {
        s_meeting_resume_supervisor_stop_requested = true;
        s_resume_supervisor_registry_retirement_failed = true;
    }
    if (s_resume_supervisor_system_sleep_restart_pending &&
        !s_resume_supervisor_system_sleep_preparing && registry_err == ESP_OK &&
        !s_resume_supervisor_registry_retirement_failed) {
        s_resume_supervisor_system_sleep_restart_pending = false;
        restart_after_system_sleep_abort = true;
    }
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (s_meeting_resume_supervisor_stopped) xSemaphoreGive(s_meeting_resume_supervisor_stopped);
    if (restart_after_system_sleep_abort && !meeting_service_ensure_resume_supervisor()) {
        ESP_LOGE(TAG, "cannot defer-restart meeting resume supervisor after system-sleep abort");
    }
    vTaskDeleteWithCaps(NULL);
}

static esp_err_t stop_meeting_resume_supervisor(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_resume_supervisor_stop_requested = true;
    task = s_meeting_resume_supervisor_task;
    const esp_err_t exit_status = s_resume_supervisor_exit_status;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (!task) return exit_status;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(task);
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (!s_meeting_resume_supervisor_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_meeting_resume_supervisor_stopped,
                       pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_meeting_state_lock);
    const esp_err_t completed_status = s_resume_supervisor_exit_status;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (completed_status != ESP_OK) return completed_status;
    ESP_LOGI(TAG, "meeting resume supervisor stopped; active meeting worker, if any, was not interrupted");
    return ESP_OK;
}

static esp_err_t stop_meeting_resume_supervisor_registry_entry(void *context,
                                                                 uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    task = s_meeting_resume_supervisor_task;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_meeting_resume_supervisor(timeout_ms);
}

bool meeting_service_ensure_resume_supervisor(void) {
    if (!meeting_service_pending()) return true;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    if (s_resume_supervisor_system_sleep_preparing ||
        s_resume_supervisor_system_sleep_restart_pending ||
        s_resume_supervisor_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return false;
    }
    bool already_running = s_meeting_resume_supervisor_task != NULL ||
                           s_meeting_resume_supervisor_starting || s_resume_supervisor_retiring;
    if (!already_running) s_meeting_resume_supervisor_starting = true;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (already_running) return true;
    if (!s_meeting_resume_supervisor_start_gate || !s_meeting_resume_supervisor_stopped) {
        taskENTER_CRITICAL(&s_meeting_state_lock);
        s_meeting_resume_supervisor_starting = false;
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        ESP_LOGE(TAG, "meeting resume supervisor lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_meeting_resume_supervisor_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_resume_supervisor_stop_requested = false;
    s_resume_supervisor_exit_status = ESP_OK;
    s_resume_supervisor_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    // This supervisor only waits and starts a worker. Put its stack in PSRAM
    // so it cannot consume the last contiguous internal block needed by the
    // real upload worker. It never writes flash/NVS, so this is safe.
    TaskHandle_t task = NULL;
    BaseType_t created = xTaskCreateWithCaps(meeting_resume_supervisor_task,
                                             "maclaw_meeting_resume", 2048,
                                             NULL, 1, &task,
                                             MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (created != pdPASS) {
        taskENTER_CRITICAL(&s_meeting_state_lock);
        s_meeting_resume_supervisor_task = NULL;
        s_meeting_resume_supervisor_starting = false;
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        ESP_LOGE(TAG, "cannot start meeting resume supervisor");
        return false;
    }
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_resume_supervisor_task = task;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
        .name = "meeting_resume_supervisor",
        .context = (void *)task,
        .stop = stop_meeting_resume_supervisor_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register meeting resume supervisor: %s",
                 esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_meeting_state_lock);
        s_meeting_resume_supervisor_stop_requested = true;
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        xSemaphoreGive(s_meeting_resume_supervisor_start_gate);
        (void)stop_meeting_resume_supervisor(500);
        return false;
    }
    xSemaphoreGive(s_meeting_resume_supervisor_start_gate);
    return true;
}

device_status_t meeting_service_prepare_resume_supervisor_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    bool restart_supervisor = false;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    if (!s_host_installed) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (s_resume_supervisor_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* Like capability refresh, a creator that has published `starting` but
     * not its task handle cannot be safely classified as absent. Fail closed
     * so the outer transaction rolls back before the start gate is released. */
    if ((s_meeting_resume_supervisor_starting &&
         !s_meeting_resume_supervisor_task) || s_resume_supervisor_retiring ||
        s_resume_supervisor_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_resume_supervisor_system_sleep_preparing = true;
    restart_supervisor = s_meeting_resume_supervisor_task != NULL;
    s_resume_supervisor_restart_after_system_sleep = restart_supervisor;
    taskEXIT_CRITICAL(&s_meeting_state_lock);

    if (!restart_supervisor) return DEVICE_STATUS_OK;
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return DEVICE_STATUS_TIMEOUT;
    return meeting_status_from_esp_err(stop_meeting_resume_supervisor(remaining_ms));
}

void meeting_service_abort_resume_supervisor_system_sleep_prepare(void) {
    bool restart_supervisor = false;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    restart_supervisor = s_resume_supervisor_restart_after_system_sleep;
    s_resume_supervisor_restart_after_system_sleep = false;
    s_resume_supervisor_system_sleep_preparing = false;
    if (restart_supervisor && (s_resume_supervisor_retiring ||
                               s_resume_supervisor_registry_retirement_failed)) {
        s_resume_supervisor_system_sleep_restart_pending = true;
        restart_supervisor = false;
    }
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (restart_supervisor && !meeting_service_ensure_resume_supervisor()) {
        ESP_LOGE(TAG, "cannot restore meeting resume supervisor after system-sleep abort");
    }
}

device_status_t meeting_service_commit_resume_supervisor_network_restart(void) {
    taskENTER_CRITICAL(&s_meeting_state_lock);
    if (!s_host_installed) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (!s_resume_supervisor_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_resume_supervisor_restart_after_system_sleep = false;
    s_resume_supervisor_system_sleep_restart_pending = false;
    s_resume_supervisor_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return DEVICE_STATUS_OK;
}

static bool meeting_capability_refresh_stop_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    requested = s_meeting_capability_refresh_stop_requested;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return requested;
}

// A Hub can be upgraded while the watch remains online. Meeting capability is
// negotiated during handshake, so do not make the user reboot the device just
// because it still holds an older, capability-less response in RAM. The
// refresh runs outside the input scan task because TLS can take several
// seconds; after a successful refresh it retries the original double-tap.
//
// It is deliberately a small CONNECTIVITY owner rather than a disguised
// meeting-worker owner: stopping it cancels only its one handshake/retry pass.
// A meeting task it has already started retains its independent NVS/audio/HTTP
// recovery contract and is never force-deleted by this lifecycle slice.
static void meeting_capability_refresh_task(void *arg) {
    (void)arg;
    if (!s_meeting_capability_refresh_start_gate ||
        xSemaphoreTake(s_meeting_capability_refresh_start_gate, portMAX_DELAY) != pdTRUE) {
        ESP_LOGW(TAG, "meeting capability refresh start gate unavailable");
        goto finish;
    }
    if (meeting_capability_refresh_stop_requested()) goto finish;
    ESP_LOGI(TAG, "refreshing gateway handshake for meeting recording");
    scene_presenter_publish_message("会议录音", "正在检查网关支持");
    int32_t err = gateway_transport_handshake(false);
    if (!meeting_capability_refresh_stop_requested() && err == ESP_OK &&
        meeting_service_available()) {
        // A just-finished touch/voice action can still own the foreground
        // mutex for a moment.  This task is deliberately off the input scan
        // path, so wait and retry instead of turning that harmless race into
        // a visible recording failure.
        bool started = false;
        for (unsigned retry = 0; retry < 32 && !started &&
                                  !meeting_capability_refresh_stop_requested(); ++retry) {
            started = meeting_start(false);
            if (!started) {
                if (retry == 0) {
                    scene_presenter_publish_message("会议录音", "正在等待设备就绪");
                }
                (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(250));
            }
        }
        if (!started && !meeting_capability_refresh_stop_requested()) {
            ambient_service_apply_pet_state("alert");
            char meeting_retry_hint[72];
            snprintf(meeting_retry_hint, sizeof(meeting_retry_hint),
                     "请稍后再次双击%s", device_input_primary_interaction_label());
            scene_presenter_publish_message("录音启动失败", meeting_retry_hint);
        }
    } else if (!meeting_capability_refresh_stop_requested()) {
        ESP_LOGW(TAG, "meeting capability refresh failed: err=%s available=%s",
                 esp_err_to_name((esp_err_t)err),
                 meeting_service_available() ? "yes" : "no");
        ambient_service_apply_pet_state("alert");
        scene_presenter_publish_message("会议录音不可用", "请检查网关连接");
    }
finish:
    bool restart_after_system_sleep_abort = false;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    s_capability_refresh_retiring = true;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_capability_refresh_exit_status = registry_err;
    if (s_meeting_capability_refresh_task == self) {
        s_meeting_capability_refresh_task = NULL;
    }
    s_meeting_capability_refresh_starting = false;
    s_capability_refresh_retiring = false;
    if (registry_err != ESP_OK) {
        s_meeting_capability_refresh_stop_requested = true;
        s_capability_refresh_registry_retirement_failed = true;
    }
    if (s_capability_refresh_system_sleep_restart_pending &&
        !s_capability_refresh_system_sleep_preparing && registry_err == ESP_OK &&
        !s_capability_refresh_registry_retirement_failed) {
        s_capability_refresh_system_sleep_restart_pending = false;
        restart_after_system_sleep_abort = true;
    }
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (s_meeting_capability_refresh_stopped) {
        xSemaphoreGive(s_meeting_capability_refresh_stopped);
    }
    if (restart_after_system_sleep_abort && !meeting_service_refresh_capability()) {
        ESP_LOGE(TAG, "cannot defer-restart meeting capability refresh after system-sleep abort");
    }
    vTaskDeleteWithCaps(NULL);
}

static esp_err_t stop_meeting_capability_refresh_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_capability_refresh_stop_requested = true;
    task = s_meeting_capability_refresh_task;
    const esp_err_t exit_status = s_capability_refresh_exit_status;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (!task) return exit_status;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;

    /* The refresh uses the normal HTTP lane, but publishes its active ESP
     * client only for the perform interval. Hold the guard through cancel so
     * request cleanup cannot race a stale client pointer. */
    if (s_host.cancel_capability_http) s_host.cancel_capability_http(deadline_us);
    xTaskNotifyGive(task);
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (!s_meeting_capability_refresh_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_meeting_capability_refresh_stopped,
                       pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_meeting_state_lock);
    const esp_err_t completed_status = s_capability_refresh_exit_status;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (completed_status != ESP_OK) return completed_status;
    ESP_LOGI(TAG, "meeting capability refresh stopped");
    return ESP_OK;
}

static esp_err_t stop_meeting_capability_refresh_registry_entry(void *context,
                                                                  uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    task = s_meeting_capability_refresh_task;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_meeting_capability_refresh_task(timeout_ms);
}

bool meeting_service_refresh_capability(void) {
    taskENTER_CRITICAL(&s_meeting_state_lock);
    if (s_capability_refresh_system_sleep_preparing ||
        s_capability_refresh_system_sleep_restart_pending ||
        s_capability_refresh_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return false;
    }
    bool already_refreshing = s_meeting_capability_refresh_task != NULL ||
                              s_meeting_capability_refresh_starting ||
                              s_capability_refresh_retiring;
    if (!already_refreshing) s_meeting_capability_refresh_starting = true;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (already_refreshing) return true;
    if (!s_meeting_capability_refresh_start_gate ||
        !s_meeting_capability_refresh_stopped ||
        !s_host.capability_transport_ready ||
        !s_host.capability_transport_ready()) {
        taskENTER_CRITICAL(&s_meeting_state_lock);
        s_meeting_capability_refresh_starting = false;
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        ESP_LOGE(TAG, "meeting capability refresh lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_meeting_capability_refresh_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_capability_refresh_stop_requested = false;
    s_capability_refresh_exit_status = ESP_OK;
    s_capability_refresh_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    TaskHandle_t handle = NULL;
    // gateway_handshake() persists fresh ambient data in NVS.  Flash writes
    // temporarily disable the cache, so this task's stack must remain in
    // internal RAM; a PSRAM stack causes esp_task_stack_is_sane_cache_disabled
    // to assert and looks like a reboot immediately after a double tap.
    BaseType_t created = xTaskCreate(meeting_capability_refresh_task,
                                     "maclaw_meeting_cap", 8192, NULL, 4,
                                     &handle);
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_capability_refresh_task = created == pdPASS ? handle : NULL;
    if (created != pdPASS) s_meeting_capability_refresh_starting = false;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (created != pdPASS) {
        ESP_LOGE(TAG, "cannot start meeting capability refresh task");
        return false;
    }
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
        .name = "meeting_capability_refresh",
        .context = (void *)handle,
        .stop = stop_meeting_capability_refresh_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register meeting capability refresh: %s",
                 esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_meeting_state_lock);
        s_meeting_capability_refresh_stop_requested = true;
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        xSemaphoreGive(s_meeting_capability_refresh_start_gate);
        (void)stop_meeting_capability_refresh_task(500);
        return false;
    }
    xSemaphoreGive(s_meeting_capability_refresh_start_gate);
    return true;
}

device_status_t meeting_service_prepare_capability_refresh_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    bool restart_refresh = false;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    if (!s_host_installed) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (s_capability_refresh_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* Creation publishes `starting` before the task handle can exist. Do not
     * race that tiny hand-off with a sleep commit: reject the transaction so
     * its caller rolls back, rather than pretending there is no worker to
     * resume while the creator is still about to release its start gate. */
    if ((s_meeting_capability_refresh_starting &&
         !s_meeting_capability_refresh_task) || s_capability_refresh_retiring ||
        s_capability_refresh_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_capability_refresh_system_sleep_preparing = true;
    /* Only a published running task represents work to resume. A completed
     * or merely historical capability refresh must not be resurrected. */
    restart_refresh = s_meeting_capability_refresh_task != NULL;
    s_capability_refresh_restart_after_system_sleep = restart_refresh;
    taskEXIT_CRITICAL(&s_meeting_state_lock);

    if (!restart_refresh) return DEVICE_STATUS_OK;
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return DEVICE_STATUS_TIMEOUT;
    return meeting_status_from_esp_err(
        stop_meeting_capability_refresh_task(remaining_ms));
}

void meeting_service_abort_capability_refresh_system_sleep_prepare(void) {
    bool restart_refresh = false;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    restart_refresh = s_capability_refresh_restart_after_system_sleep;
    s_capability_refresh_restart_after_system_sleep = false;
    /* Reopening this local task fence does not reopen Connectivity request
     * admission; the outer Power rollback controls that ordering. */
    s_capability_refresh_system_sleep_preparing = false;
    if (restart_refresh && (s_capability_refresh_retiring ||
                            s_capability_refresh_registry_retirement_failed)) {
        s_capability_refresh_system_sleep_restart_pending = true;
        restart_refresh = false;
    }
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    if (restart_refresh && !meeting_service_refresh_capability()) {
        ESP_LOGE(TAG, "cannot restore meeting capability refresh after system-sleep abort");
    }
}

device_status_t meeting_service_commit_capability_refresh_network_restart(void) {
    taskENTER_CRITICAL(&s_meeting_state_lock);
    if (!s_host_installed) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (!s_capability_refresh_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_meeting_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_capability_refresh_restart_after_system_sleep = false;
    s_capability_refresh_system_sleep_restart_pending = false;
    s_capability_refresh_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return DEVICE_STATUS_OK;
}

device_status_t meeting_service_init(const meeting_service_host_t *host) {
    if (!host || !host->storage_mounted ||
        !host->wake_word_stop ||
        !host->wake_word_start || !host->recording_create ||
        !host->recording_get_status || !host->recording_post_action ||
        !host->capability_transport_ready || !host->cancel_capability_http ||
        !host->log_heap_snapshot ||
        !host->schedule_wake_restart) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    s_host = *host;
    s_host_installed = true;
    s_meeting_task_start_gate = xSemaphoreCreateBinary();
    if (!s_meeting_task_start_gate) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    s_meeting_task_stopped = xSemaphoreCreateBinary();
    if (!s_meeting_task_stopped) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    s_meeting_resume_supervisor_start_gate = xSemaphoreCreateBinary();
    if (!s_meeting_resume_supervisor_start_gate) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    s_meeting_resume_supervisor_stopped = xSemaphoreCreateBinary();
    if (!s_meeting_resume_supervisor_stopped) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    s_meeting_capability_refresh_start_gate = xSemaphoreCreateBinary();
    if (!s_meeting_capability_refresh_start_gate) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    s_meeting_capability_refresh_stopped = xSemaphoreCreateBinary();
    if (!s_meeting_capability_refresh_stopped) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    taskENTER_CRITICAL(&s_meeting_state_lock);
    s_meeting_task_retiring = false;
    s_meeting_task_exit_status = ESP_OK;
    s_meeting_task_registry_retirement_failed = false;
    s_resume_supervisor_retiring = false;
    s_resume_supervisor_exit_status = ESP_OK;
    s_resume_supervisor_registry_retirement_failed = false;
    s_capability_refresh_retiring = false;
    s_capability_refresh_exit_status = ESP_OK;
    s_capability_refresh_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_meeting_state_lock);
    return DEVICE_STATUS_OK;
}
