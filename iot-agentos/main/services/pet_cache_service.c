#include "services/pet_cache_service.h"

#include <string.h>

#include "esp_err.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "pet_asset_cache_storage.h"
#include "task_registry.h"

static const char *TAG = "pet_cache";
static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static pet_cache_service_host_t s_host;
static bool s_initialized;
static SemaphoreHandle_t s_flash_mutex;
static TaskHandle_t s_task;
/* Task creation is a separate admission phase: a lifecycle stop must never
 * observe NULL in the create->publish gap and declare Storage quiesced while a
 * Flash/VFS worker can still register and start. */
static bool s_starting;
static bool s_stop_requested;
static bool s_retiring;
static bool s_system_sleep_preparing;
static bool s_system_sleep_stop_was_requested;
/* A failed unregister leaves an immutable Storage identity behind.  It is not
 * safe to restore optional Flash admission on System Sleep ABORT in that case:
 * a new worker could be targeted through the stale entry. */
static bool s_registry_retirement_failed;
/* Completion of Flash work and completion of its immutable Registry retirement
 * are distinct facts. Keep the latter so a late lifecycle observer cannot
 * accept a closed cache domain while a stale Storage Registry identity remains
 * visible to a future owner sweep. */
static esp_err_t s_exit_status = ESP_OK;

typedef enum {
    PET_CACHE_WRITE,
    PET_CACHE_CLEAR,
    PET_CACHE_DROP_STALE,
} pet_cache_operation_t;

typedef struct {
    pet_cache_operation_t operation;
    pet_asset_descriptor_t descriptor;
    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES];
    gateway_capability_lease_t lease;
    bool require_lease;
    bool owns_frames_and_job;
    bool dropped;
    esp_err_t result;
    pet_cache_service_cancelled_fn cancelled;
    void *cancel_context;
} pet_cache_job_t;

static device_status_t status_from_esp_err(esp_err_t err) {
    if (err == ESP_OK) return DEVICE_STATUS_OK;
    if (err == ESP_ERR_TIMEOUT) return DEVICE_STATUS_TIMEOUT;
    if (err == ESP_ERR_NO_MEM) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (err == ESP_ERR_INVALID_ARG) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (err == ESP_ERR_NOT_SUPPORTED) return DEVICE_STATUS_UNAVAILABLE;
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return DEVICE_STATUS_INTERNAL_ERROR;
}

static bool service_stop_requested(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool requested = s_stop_requested;
    taskEXIT_CRITICAL(&s_lock);
    return requested;
}

static bool service_admission_open(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool open = s_initialized && !s_stop_requested &&
                       !s_system_sleep_preparing && !s_registry_retirement_failed;
    taskEXIT_CRITICAL(&s_lock);
    return open;
}

/* Never call a host probe while s_lock is held: composition-root callbacks may
 * read their own lifecycle state, and this one-way lock order prevents an
 * otherwise subtle root-lock <-> cache-lock deadlock during System Sleep. */
static bool job_cancelled(const pet_cache_job_t *job) {
    if (service_stop_requested()) return true;
    if (job && job->cancelled && job->cancelled(job->cancel_context)) return true;
    if (job && job->require_lease &&
        !s_host.gateway_lease_current(&job->lease, s_host.context)) return true;
    return false;
}

static bool storage_cancelled(void *context) {
    return job_cancelled((const pet_cache_job_t *)context);
}

static void storage_yield(void *context) {
    (void)context;
    vTaskDelay(1);
}

static void free_frames(uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES]) {
    if (!frames) return;
    for (uint32_t i = 0; i < PET_ASSET_SERVICE_MAX_FRAMES; ++i) {
        heap_caps_free(frames[i]);
        frames[i] = NULL;
    }
}

static esp_err_t perform_job(pet_cache_job_t *job) {
    if (!job || job_cancelled(job)) return ESP_ERR_INVALID_STATE;
    if (!s_host.storage_mounted(s_host.context) ||
        !s_host.allows_optional_flash_work(s_host.context)) return ESP_ERR_INVALID_STATE;
    if (job->operation == PET_CACHE_CLEAR) {
        /* A clear can remove several retained files. Re-check immediately
         * before the irreversible storage operation so a superseded startup
         * withdrawal cannot erase a newer descriptor's cache after waiting
         * for the internal Flash worker. */
        if (job_cancelled(job)) return ESP_ERR_INVALID_STATE;
        pet_asset_cache_storage_clear();
        return ESP_OK;
    }
    if (job->operation == PET_CACHE_DROP_STALE) {
        pet_asset_descriptor_t cached = {0};
        (void)pet_asset_cache_storage_read_descriptor(&cached);
        if (job_cancelled(job)) return ESP_ERR_INVALID_STATE;
        job->dropped = pet_asset_cache_storage_drop_if_stale(job->descriptor.revision);
        if (job->dropped) {
            ESP_LOGI(TAG, "stale pet cache dropped: cached=%s new=%s",
                     cached.revision[0] ? cached.revision : "none",
                     job->descriptor.revision);
        }
        return ESP_OK;
    }
    pet_asset_cache_storage_options_t options = {
        .cancelled = storage_cancelled,
        .yield = storage_yield,
        .context = job,
    };
    if (!pet_asset_cache_storage_write(&job->descriptor, job->frames, &options)) {
        return job_cancelled(job) ? ESP_ERR_INVALID_STATE : ESP_FAIL;
    }
    size_t frame_bytes = 0;
    (void)pet_asset_service_frame_bytes(job->descriptor.width, job->descriptor.height,
                                        &frame_bytes);
    ESP_LOGI(TAG, "pet asset cached: revision=%s frames=%d bytes_per_frame=%u",
             job->descriptor.revision, job->descriptor.frame_count,
             (unsigned)frame_bytes);
    return ESP_OK;
}

static esp_err_t stop_registry_entry(void *context, uint32_t timeout_ms);

static void pet_cache_task(void *arg) {
    pet_cache_job_t *job = (pet_cache_job_t *)arg;
    const bool owns_frames_and_job = job && job->owns_frames_and_job;
    (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
    job->result = perform_job(job);
    if (owns_frames_and_job) {
        free_frames(job->frames);
        heap_caps_free(job);
    }

    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_lock);
    s_retiring = true;
    taskEXIT_CRITICAL(&s_lock);
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_STORAGE, (void *)self, 10);
    taskENTER_CRITICAL(&s_lock);
    s_exit_status = registry_err;
    if (s_task == self) s_task = NULL;
    s_retiring = false;
    if (registry_err != ESP_OK) {
        s_stop_requested = true;
        s_registry_retirement_failed = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (owns_frames_and_job) xSemaphoreGive(s_flash_mutex);
    vTaskDeleteWithCaps(NULL);
}

static esp_err_t stop_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    TaskHandle_t task;
    taskENTER_CRITICAL(&s_lock);
    if (s_starting) {
        taskEXIT_CRITICAL(&s_lock);
        return ESP_ERR_INVALID_STATE;
    }
    s_stop_requested = true;
    task = s_task;
    taskEXIT_CRITICAL(&s_lock);
    if (!task) {
        taskENTER_CRITICAL(&s_lock);
        const esp_err_t exit_status = s_exit_status;
        taskEXIT_CRITICAL(&s_lock);
        return exit_status;
    }
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(task);
    const TickType_t started = xTaskGetTickCount();
    while (true) {
        taskENTER_CRITICAL(&s_lock);
        const bool stopped = s_task != task;
        taskEXIT_CRITICAL(&s_lock);
        if (stopped) {
            taskENTER_CRITICAL(&s_lock);
            const esp_err_t exit_status = s_exit_status;
            taskEXIT_CRITICAL(&s_lock);
            return exit_status;
        }
        if (xTaskGetTickCount() - started >= pdMS_TO_TICKS(timeout_ms)) {
            return ESP_ERR_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(10));
    }
}

static esp_err_t stop_registry_entry(void *context, uint32_t timeout_ms) {
    taskENTER_CRITICAL(&s_lock);
    TaskHandle_t task = s_task;
    taskEXIT_CRITICAL(&s_lock);
    if (context && task != (void *)context) return ESP_ERR_INVALID_STATE;
    return stop_task(timeout_ms);
}

static esp_err_t run_operation(pet_cache_operation_t operation,
                               const pet_asset_descriptor_t *descriptor,
                               bool *out_dropped,
                               pet_cache_service_cancelled_fn cancelled,
                               void *cancel_context) {
    if ((operation == PET_CACHE_DROP_STALE && !descriptor) ||
        !s_initialized || !s_flash_mutex || !service_admission_open()) {
        return ESP_ERR_INVALID_STATE;
    }
    if (!s_host.storage_mounted(s_host.context) ||
        !s_host.allows_optional_flash_work(s_host.context)) return ESP_ERR_NOT_SUPPORTED;
    const TickType_t admitted_at = xTaskGetTickCount();
    while (xSemaphoreTake(s_flash_mutex, pdMS_TO_TICKS(50)) != pdTRUE) {
        if (!service_admission_open() || (cancelled && cancelled(cancel_context))) {
            return ESP_ERR_INVALID_STATE;
        }
        if (xTaskGetTickCount() - admitted_at >= pdMS_TO_TICKS(500)) return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_lock);
    if (s_starting || s_task || s_stop_requested) {
        taskEXIT_CRITICAL(&s_lock);
        xSemaphoreGive(s_flash_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    s_starting = true;
    s_exit_status = ESP_OK;
    taskEXIT_CRITICAL(&s_lock);
    pet_cache_job_t *job = heap_caps_calloc(1, sizeof(*job),
                                            MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!job) {
        taskENTER_CRITICAL(&s_lock);
        s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
        xSemaphoreGive(s_flash_mutex);
        return ESP_ERR_NO_MEM;
    }
    job->operation = operation;
    if (descriptor) job->descriptor = *descriptor;
    job->cancelled = cancelled;
    job->cancel_context = cancel_context;
    TaskHandle_t task = NULL;
    const BaseType_t created = xTaskCreatePinnedToCoreWithCaps(
        pet_cache_task, "maclaw_pet_cache", 8192, job, 1, &task, 1,
        MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (created != pdPASS) {
        heap_caps_free(job);
        taskENTER_CRITICAL(&s_lock);
        s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
        xSemaphoreGive(s_flash_mutex);
        return ESP_ERR_NO_MEM;
    }
    const esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_STORAGE,
        .name = "pet_cache",
        .context = (void *)task,
        .stop = stop_registry_entry,
    });
    /* Keep the creator phase closed until the immutable Registry identity is
     * installed.  The worker is waiting on its start notification, but a
     * concurrent System-Sleep stop must still not observe a published task
     * which the Registry cannot yet stop. */
    taskENTER_CRITICAL(&s_lock);
    s_task = task;
    s_starting = false;
    taskEXIT_CRITICAL(&s_lock);
    if (registry_err != ESP_OK) {
        taskENTER_CRITICAL(&s_lock);
        s_stop_requested = true;
        taskEXIT_CRITICAL(&s_lock);
    }
    xTaskNotifyGive(task);
    while (true) {
        taskENTER_CRITICAL(&s_lock);
        const bool running = s_task == task;
        taskEXIT_CRITICAL(&s_lock);
        if (!running) break;
        if (service_stop_requested() || (cancelled && cancelled(cancel_context))) {
            (void)stop_task(500);
        }
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    const esp_err_t result = job->result;
    if (out_dropped) *out_dropped = job->dropped;
    heap_caps_free(job);
    xSemaphoreGive(s_flash_mutex);
    return result;
}

device_status_t pet_cache_service_init(const pet_cache_service_host_t *host) {
    if (!host || host->struct_size != sizeof(*host) || !host->storage_mounted ||
        !host->allows_optional_flash_work || !host->gateway_lease_current) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) {
        const bool same_host = memcmp(&s_host, host, sizeof(*host)) == 0;
        taskEXIT_CRITICAL(&s_lock);
        return same_host ? DEVICE_STATUS_OK : DEVICE_STATUS_BUSY;
    }
    taskEXIT_CRITICAL(&s_lock);
    SemaphoreHandle_t mutex = xSemaphoreCreateMutex();
    if (!mutex) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    taskENTER_CRITICAL(&s_lock);
    s_host = *host;
    s_flash_mutex = mutex;
    s_initialized = true;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

device_status_t pet_cache_service_clear(pet_cache_service_cancelled_fn cancelled,
                                        void *cancel_context) {
    return status_from_esp_err(run_operation(PET_CACHE_CLEAR, NULL, NULL,
                                             cancelled, cancel_context));
}

device_status_t pet_cache_service_drop_if_stale(
    const pet_asset_descriptor_t *descriptor, bool *out_dropped,
    pet_cache_service_cancelled_fn cancelled, void *cancel_context) {
    if (out_dropped) *out_dropped = false;
    return status_from_esp_err(run_operation(PET_CACHE_DROP_STALE, descriptor,
                                             out_dropped, cancelled, cancel_context));
}

void pet_cache_service_cache_in_background(
    const pet_asset_descriptor_t *descriptor,
    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES],
    const gateway_capability_lease_t *lease) {
    if (!frames) return;
    if (!descriptor || descriptor->frame_count < 1 ||
        descriptor->frame_count > (int)PET_ASSET_SERVICE_MAX_FRAMES || !lease ||
        !s_initialized || !s_flash_mutex || !service_admission_open() ||
        !s_host.storage_mounted(s_host.context) ||
        !s_host.allows_optional_flash_work(s_host.context) ||
        !s_host.gateway_lease_current(lease, s_host.context) ||
        xSemaphoreTake(s_flash_mutex, 0) != pdTRUE) {
        free_frames(frames);
        return;
    }
    for (int i = 0; i < descriptor->frame_count; ++i) {
        if (!frames[i]) {
            free_frames(frames);
            return;
        }
    }
    taskENTER_CRITICAL(&s_lock);
    if (s_starting || s_task || s_stop_requested) {
        taskEXIT_CRITICAL(&s_lock);
        xSemaphoreGive(s_flash_mutex);
        free_frames(frames);
        return;
    }
    s_starting = true;
    s_exit_status = ESP_OK;
    taskEXIT_CRITICAL(&s_lock);
    pet_cache_job_t *job = heap_caps_calloc(1, sizeof(*job),
                                            MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!job) {
        taskENTER_CRITICAL(&s_lock);
        s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
        xSemaphoreGive(s_flash_mutex);
        free_frames(frames);
        return;
    }
    job->operation = PET_CACHE_WRITE;
    job->descriptor = *descriptor;
    job->lease = *lease;
    job->require_lease = true;
    job->owns_frames_and_job = true;
    for (int i = 0; i < descriptor->frame_count; ++i) {
        job->frames[i] = frames[i];
        frames[i] = NULL;
    }
    TaskHandle_t task = NULL;
    const BaseType_t created = xTaskCreatePinnedToCoreWithCaps(
        pet_cache_task, "maclaw_pet_cache", 8192, job, 1, &task, 1,
        MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (created != pdPASS) {
        free_frames(job->frames);
        heap_caps_free(job);
        taskENTER_CRITICAL(&s_lock);
        s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
        xSemaphoreGive(s_flash_mutex);
        return;
    }
    const esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_STORAGE,
        .name = "pet_cache",
        .context = (void *)task,
        .stop = stop_registry_entry,
    });
    /* As with synchronous work, expose the worker only after its lifecycle
     * identity is registered.  This closes the create->publish window for
     * retry/sleep rollback without ever executing the worker before the
     * registry is ready. */
    taskENTER_CRITICAL(&s_lock);
    s_task = task;
    s_starting = false;
    taskEXIT_CRITICAL(&s_lock);
    if (registry_err != ESP_OK) {
        taskENTER_CRITICAL(&s_lock);
        s_stop_requested = true;
        taskEXIT_CRITICAL(&s_lock);
    }
    xTaskNotifyGive(task);
}

device_status_t pet_cache_service_stop(uint32_t timeout_ms) {
    return status_from_esp_err(stop_task(timeout_ms));
}

device_status_t pet_cache_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (s_system_sleep_preparing || s_retiring) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_preparing = true;
    s_system_sleep_stop_was_requested = s_stop_requested;
    taskEXIT_CRITICAL(&s_lock);
    return status_from_esp_err(stop_task(timeout_ms));
}

void pet_cache_service_abort_system_sleep_prepare(void) {
    taskENTER_CRITICAL(&s_lock);
    if (s_system_sleep_preparing) {
        /* Do not reopen a domain whose retiring worker could not remove its
         * immutable Registry identity. This is terminal for the boot, unlike
         * an ordinary reversible PREPARE stop. */
        s_stop_requested = s_registry_retirement_failed ||
                           s_system_sleep_stop_was_requested;
        s_system_sleep_stop_was_requested = false;
        s_system_sleep_preparing = false;
    }
    taskEXIT_CRITICAL(&s_lock);
}
