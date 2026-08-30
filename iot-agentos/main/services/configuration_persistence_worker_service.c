#include "services/configuration_persistence_worker_service.h"

#include <string.h>

#include "esp_err.h"
#include "esp_heap_caps.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "task_registry.h"

#define CONFIGURATION_PERSISTENCE_WORKER_STACK_BYTES 8192u

typedef struct {
    configuration_persistence_request_t request;
    uint32_t generation;
    bool stop;
    bool system_sleep_prepare;
} worker_message_t;

typedef struct {
    configuration_persistence_reply_t reply;
    uint32_t generation;
} worker_completion_t;

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static configuration_persistence_worker_service_host_t s_host;
static QueueHandle_t s_requests;
static QueueHandle_t s_completions;
static SemaphoreHandle_t s_request_mutex;
static SemaphoreHandle_t s_stopped;
static SemaphoreHandle_t s_system_sleep_quiesced;
static SemaphoreHandle_t s_start_gate;
static TaskHandle_t s_task;
static uint32_t s_generation;
static bool s_initialized;
static bool s_stop_requested;
static bool s_system_sleep_preparing;
static bool s_retiring;
static bool s_registry_retirement_failed;
static esp_err_t s_exit_status = ESP_OK;

static device_status_t status_from_esp_err(esp_err_t status) {
    if (status == ESP_OK) return DEVICE_STATUS_OK;
    if (status == ESP_ERR_TIMEOUT) return DEVICE_STATUS_TIMEOUT;
    if (status == ESP_ERR_NO_MEM) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (status == ESP_ERR_INVALID_ARG) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (status == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return DEVICE_STATUS_INTERNAL_ERROR;
}

static bool host_valid(const configuration_persistence_worker_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) && host->run_transaction;
}

static uint32_t remaining_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const uint64_t rounded_ms = ((uint64_t)remaining_us + 999u) / 1000u;
    return rounded_ms > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded_ms;
}

static esp_err_t stop_registry_entry(void *context, uint32_t timeout_ms);

static void worker_task(void *arg) {
    (void)arg;
    /* Publish the immutable Registry identity before this worker can execute
     * a flash transaction or begin retirement. This closes the same
     * create→publish window guarded by the other Storage workers. */
    if (!s_start_gate || xSemaphoreTake(s_start_gate, portMAX_DELAY) != pdTRUE) {
        vTaskDeleteWithCaps(NULL);
        return;
    }
    worker_message_t message = {0};
    while (xQueueReceive(s_requests, &message, portMAX_DELAY) == pdTRUE) {
        if (message.stop) break;
        if (message.system_sleep_prepare) {
            if (s_system_sleep_quiesced) xSemaphoreGive(s_system_sleep_quiesced);
            continue;
        }
        worker_completion_t completion = {
            .reply = {.status = DEVICE_STATUS_BUSY},
            .generation = message.generation,
        };
        const device_status_t status = s_host.run_transaction(
            &message.request, &completion.reply, s_host.context);
        if (completion.reply.status == DEVICE_STATUS_OK && status != DEVICE_STATUS_OK) {
            completion.reply.status = status;
        } else if (completion.reply.status == DEVICE_STATUS_BUSY) {
            completion.reply.status = status;
        }
        while (xQueueSend(s_completions, &completion, pdMS_TO_TICKS(50)) != pdTRUE) {
            taskENTER_CRITICAL(&s_lock);
            const bool stopping = s_stop_requested;
            taskEXIT_CRITICAL(&s_lock);
            if (stopping) break;
        }
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
    if (s_stopped) xSemaphoreGive(s_stopped);
    vTaskDeleteWithCaps(NULL);
}

device_status_t configuration_persistence_worker_service_init(
    const configuration_persistence_worker_service_host_t *host) {
    if (!host_valid(host)) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    const bool already_initialized = s_initialized;
    taskEXIT_CRITICAL(&s_lock);
    if (already_initialized) return DEVICE_STATUS_BUSY;

    QueueHandle_t requests = xQueueCreate(1, sizeof(worker_message_t));
    QueueHandle_t completions = xQueueCreate(2, sizeof(worker_completion_t));
    SemaphoreHandle_t request_mutex = xSemaphoreCreateMutex();
    SemaphoreHandle_t stopped = xSemaphoreCreateBinary();
    SemaphoreHandle_t quiesced = xSemaphoreCreateBinary();
    SemaphoreHandle_t start_gate = xSemaphoreCreateBinary();
    if (!requests || !completions || !request_mutex || !stopped || !quiesced || !start_gate) {
        if (requests) vQueueDelete(requests);
        if (completions) vQueueDelete(completions);
        if (request_mutex) vSemaphoreDelete(request_mutex);
        if (stopped) vSemaphoreDelete(stopped);
        if (quiesced) vSemaphoreDelete(quiesced);
        if (start_gate) vSemaphoreDelete(start_gate);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }

    taskENTER_CRITICAL(&s_lock);
    s_host = *host;
    s_requests = requests;
    s_completions = completions;
    s_request_mutex = request_mutex;
    s_stopped = stopped;
    s_system_sleep_quiesced = quiesced;
    s_start_gate = start_gate;
    s_generation = 0;
    s_stop_requested = false;
    s_system_sleep_preparing = false;
    s_retiring = false;
    s_registry_retirement_failed = false;
    s_exit_status = ESP_OK;
    s_initialized = true;
    taskEXIT_CRITICAL(&s_lock);

    if (xTaskCreatePinnedToCoreWithCaps(
            worker_task, "maclaw_volume_nvs", CONFIGURATION_PERSISTENCE_WORKER_STACK_BYTES,
            NULL, 4, &s_task, 1, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT) != pdPASS) {
        taskENTER_CRITICAL(&s_lock);
        s_initialized = false;
        s_task = NULL;
        taskEXIT_CRITICAL(&s_lock);
        vQueueDelete(requests);
        vQueueDelete(completions);
        vSemaphoreDelete(request_mutex);
        vSemaphoreDelete(stopped);
        vSemaphoreDelete(quiesced);
        vSemaphoreDelete(start_gate);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    const esp_err_t registry_status = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_STORAGE,
        .name = "output_volume_persist",
        .context = (void *)s_task,
        .stop = stop_registry_entry,
    });
    if (registry_status != ESP_OK) {
        xSemaphoreGive(s_start_gate);
        (void)configuration_persistence_worker_service_stop(500);
        return status_from_esp_err(registry_status);
    }
    xSemaphoreGive(s_start_gate);
    return DEVICE_STATUS_OK;
}

device_status_t configuration_persistence_worker_service_submit(
    const configuration_persistence_request_t *request,
    uint32_t mutex_timeout_ms, uint32_t queue_timeout_ms,
    uint32_t completion_timeout_ms,
    configuration_persistence_reply_t *out_reply) {
    if (!request || mutex_timeout_ms == 0 || queue_timeout_ms == 0 ||
        completion_timeout_ms == 0) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    taskENTER_CRITICAL(&s_lock);
    const bool unavailable = !s_initialized || !s_requests || !s_completions ||
                             !s_request_mutex || !s_task || s_stop_requested ||
                             s_retiring || s_system_sleep_preparing ||
                             s_registry_retirement_failed;
    taskEXIT_CRITICAL(&s_lock);
    if (unavailable) return DEVICE_STATUS_BUSY;
    if (xSemaphoreTake(s_request_mutex, pdMS_TO_TICKS(mutex_timeout_ms)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_lock);
    const bool stopped = s_stop_requested || s_system_sleep_preparing ||
                         s_retiring || s_registry_retirement_failed || !s_task;
    uint32_t generation = ++s_generation;
    if (generation == 0) generation = ++s_generation;
    taskEXIT_CRITICAL(&s_lock);
    if (stopped) {
        xSemaphoreGive(s_request_mutex);
        return DEVICE_STATUS_BUSY;
    }
    worker_completion_t stale = {0};
    while (xQueueReceive(s_completions, &stale, 0) == pdTRUE) {}
    const worker_message_t message = {.request = *request, .generation = generation};
    device_status_t result = DEVICE_STATUS_TIMEOUT;
    if (xQueueSend(s_requests, &message, pdMS_TO_TICKS(queue_timeout_ms)) == pdTRUE) {
        const TickType_t started = xTaskGetTickCount();
        const TickType_t timeout = pdMS_TO_TICKS(completion_timeout_ms);
        worker_completion_t completion = {0};
        while (true) {
            const TickType_t elapsed = xTaskGetTickCount() - started;
            if (elapsed >= timeout ||
                xQueueReceive(s_completions, &completion, timeout - elapsed) != pdTRUE) {
                break;
            }
            if (completion.generation == generation) {
                result = completion.reply.status;
                if (out_reply) *out_reply = completion.reply;
                break;
            }
        }
    }
    xSemaphoreGive(s_request_mutex);
    return result;
}

device_status_t configuration_persistence_worker_service_submit_until(
    const configuration_persistence_request_t *request,
    int64_t deadline_us, configuration_persistence_reply_t *out_reply) {
    if (!request || deadline_us <= esp_timer_get_time()) return DEVICE_STATUS_TIMEOUT;
    taskENTER_CRITICAL(&s_lock);
    const bool unavailable = !s_initialized || !s_requests || !s_completions ||
                             !s_request_mutex || !s_task || s_stop_requested ||
                             s_retiring || s_system_sleep_preparing ||
                             s_registry_retirement_failed;
    taskEXIT_CRITICAL(&s_lock);
    if (unavailable) return DEVICE_STATUS_BUSY;
    uint32_t remaining = remaining_ms(deadline_us);
    if (!remaining || xSemaphoreTake(s_request_mutex, pdMS_TO_TICKS(remaining)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_lock);
    const bool stopped = s_stop_requested || s_system_sleep_preparing ||
                         s_retiring || s_registry_retirement_failed || !s_task;
    uint32_t generation = ++s_generation;
    if (generation == 0) generation = ++s_generation;
    taskEXIT_CRITICAL(&s_lock);
    if (stopped) {
        xSemaphoreGive(s_request_mutex);
        return DEVICE_STATUS_BUSY;
    }
    worker_completion_t stale = {0};
    while (xQueueReceive(s_completions, &stale, 0) == pdTRUE) {}
    const worker_message_t message = {.request = *request, .generation = generation};
    device_status_t result = DEVICE_STATUS_TIMEOUT;
    remaining = remaining_ms(deadline_us);
    if (remaining && xQueueSend(s_requests, &message, pdMS_TO_TICKS(remaining)) == pdTRUE) {
        worker_completion_t completion = {0};
        while ((remaining = remaining_ms(deadline_us)) != 0u &&
               xQueueReceive(s_completions, &completion, pdMS_TO_TICKS(remaining)) == pdTRUE) {
            if (completion.generation == generation) {
                result = completion.reply.status;
                if (out_reply) *out_reply = completion.reply;
                break;
            }
        }
    }
    xSemaphoreGive(s_request_mutex);
    return result;
}

device_status_t configuration_persistence_worker_service_stop(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || !s_requests || !s_stopped || !s_request_mutex) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_OK;
    }
    s_stop_requested = true;
    s_system_sleep_preparing = false;
    TaskHandle_t task = s_task;
    taskEXIT_CRITICAL(&s_lock);
    if (!task) {
        taskENTER_CRITICAL(&s_lock);
        const device_status_t status = status_from_esp_err(s_exit_status);
        taskEXIT_CRITICAL(&s_lock);
        return status;
    }
    if (xTaskGetCurrentTaskHandle() == task) return DEVICE_STATUS_BUSY;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    uint32_t remaining = remaining_ms(deadline_us);
    if (!remaining || xSemaphoreTake(s_request_mutex, pdMS_TO_TICKS(remaining)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    worker_completion_t stale = {0};
    while (xQueueReceive(s_completions, &stale, 0) == pdTRUE) {}
    const worker_message_t stop_message = {.stop = true};
    remaining = remaining_ms(deadline_us);
    const BaseType_t queued = remaining
                                  ? xQueueSend(s_requests, &stop_message, pdMS_TO_TICKS(remaining))
                                  : pdFALSE;
    xSemaphoreGive(s_request_mutex);
    if (queued != pdTRUE) return DEVICE_STATUS_TIMEOUT;
    remaining = remaining_ms(deadline_us);
    if (!remaining || xSemaphoreTake(s_stopped, pdMS_TO_TICKS(remaining)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_lock);
    const device_status_t status = status_from_esp_err(s_exit_status);
    taskEXIT_CRITICAL(&s_lock);
    return status;
}

static esp_err_t stop_registry_entry(void *context, uint32_t timeout_ms) {
    taskENTER_CRITICAL(&s_lock);
    TaskHandle_t task = s_task;
    taskEXIT_CRITICAL(&s_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    const device_status_t status = configuration_persistence_worker_service_stop(timeout_ms);
    switch (status) {
        case DEVICE_STATUS_OK: return ESP_OK;
        case DEVICE_STATUS_TIMEOUT: return ESP_ERR_TIMEOUT;
        case DEVICE_STATUS_INVALID_ARGUMENT: return ESP_ERR_INVALID_ARG;
        case DEVICE_STATUS_BUSY: return ESP_ERR_INVALID_STATE;
        default: return ESP_FAIL;
    }
}

device_status_t configuration_persistence_worker_service_prepare_system_sleep(
    uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    const bool unavailable = !s_initialized || !s_requests || !s_request_mutex ||
                             !s_system_sleep_quiesced || !s_task || s_stop_requested ||
                             s_retiring || s_registry_retirement_failed ||
                             s_system_sleep_preparing;
    taskEXIT_CRITICAL(&s_lock);
    if (unavailable) return DEVICE_STATUS_BUSY;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    uint32_t remaining = remaining_ms(deadline_us);
    if (!remaining || xSemaphoreTake(s_request_mutex, pdMS_TO_TICKS(remaining)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_lock);
    const bool still_available = !s_stop_requested && !s_registry_retirement_failed &&
                                 !s_retiring && s_task && !s_system_sleep_preparing;
    if (still_available) s_system_sleep_preparing = true;
    taskEXIT_CRITICAL(&s_lock);
    if (!still_available) {
        xSemaphoreGive(s_request_mutex);
        return DEVICE_STATUS_BUSY;
    }
    while (xSemaphoreTake(s_system_sleep_quiesced, 0) == pdTRUE) {}
    const worker_message_t fence = {.system_sleep_prepare = true};
    remaining = remaining_ms(deadline_us);
    const BaseType_t queued = remaining
                                  ? xQueueSend(s_requests, &fence, pdMS_TO_TICKS(remaining))
                                  : pdFALSE;
    if (queued != pdTRUE) {
        xSemaphoreGive(s_request_mutex);
        return DEVICE_STATUS_TIMEOUT;
    }
    remaining = remaining_ms(deadline_us);
    const bool quiesced = remaining &&
                          xSemaphoreTake(s_system_sleep_quiesced,
                                         pdMS_TO_TICKS(remaining)) == pdTRUE;
    xSemaphoreGive(s_request_mutex);
    return quiesced ? DEVICE_STATUS_OK : DEVICE_STATUS_TIMEOUT;
}

void configuration_persistence_worker_service_abort_system_sleep_prepare(void) {
    taskENTER_CRITICAL(&s_lock);
    if (!s_registry_retirement_failed) s_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_lock);
}
