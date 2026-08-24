#include "services/startup_pet_worker_service.h"

#include <string.h>

#include "esp_err.h"
#include "esp_heap_caps.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "task_registry.h"

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static startup_pet_worker_service_host_t s_host;
static bool s_initialized;
static TaskHandle_t s_task;
static SemaphoreHandle_t s_start_gate;
static SemaphoreHandle_t s_completion;
static bool s_starting;
static bool s_stop_requested;
static bool s_retiring;
static bool s_system_sleep_preparing;
static bool s_system_sleep_restart_pending;
static bool s_terminally_stopped;
/* The completion semaphore answers only whether a worker reached its terminal
 * lifecycle boundary.  Keep its status separately: a task that cannot remove
 * its immutable Registry identity must wake its joiner, but that joiner must
 * still fail closed instead of tearing down shared Connectivity dependencies. */
static device_status_t s_exit_status = DEVICE_STATUS_OK;

static device_status_t status_from_esp_err(esp_err_t err) {
    if (err == ESP_OK) return DEVICE_STATUS_OK;
    if (err == ESP_ERR_TIMEOUT) return DEVICE_STATUS_TIMEOUT;
    if (err == ESP_ERR_NO_MEM) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (err == ESP_ERR_INVALID_ARG) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return DEVICE_STATUS_INTERNAL_ERROR;
}

static esp_err_t stop_registry_entry(void *context, uint32_t timeout_ms);

static void startup_pet_worker_task(void *arg) {
    SemaphoreHandle_t gate = (SemaphoreHandle_t)arg;
    if (!gate || xSemaphoreTake(gate, portMAX_DELAY) != pdTRUE) {
        vTaskDeleteWithCaps(NULL);
        return;
    }

    if (!startup_pet_worker_service_stop_requested()) {
        s_host.run_transaction(s_host.context);
    }

    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_lock);
    /* Retain the published handle while unregistering. A concurrent Registry
     * sweep can therefore only observe this exact immutable generation; it
     * cannot mistake a completed worker for a future replacement. */
    s_retiring = true;
    taskEXIT_CRITICAL(&s_lock);

    const esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
    bool restart = false;
    SemaphoreHandle_t completion = NULL;
    taskENTER_CRITICAL(&s_lock);
    s_exit_status = status_from_esp_err(unregister_err);
    if (s_task == self) s_task = NULL;
    s_start_gate = NULL;
    completion = s_completion;
    if (unregister_err != ESP_OK) {
        /* An old immutable entry is still visible. Never create another
         * generation that could be stopped through that stale identity. */
        s_stop_requested = true;
        s_terminally_stopped = true;
    }
    taskEXIT_CRITICAL(&s_lock);

    /* Do not let a restart retire this generation's completion token before
     * the current task has published terminal completion. `s_retiring` stays
     * true until after this give, so an unrelated caller cannot start a new
     * generation in this small handoff window. */
    if (completion) xSemaphoreGive(completion);

    taskENTER_CRITICAL(&s_lock);
    s_retiring = false;
    if (s_system_sleep_restart_pending && !s_system_sleep_preparing &&
        unregister_err == ESP_OK) {
        s_system_sleep_restart_pending = false;
        restart = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (restart) s_host.restart_after_system_sleep_abort(s_host.context);
    vTaskDeleteWithCaps(NULL);
}

static device_status_t stop_worker(uint32_t timeout_ms, bool terminal) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    TaskHandle_t task = NULL;
    SemaphoreHandle_t completion = NULL;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_starting) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* `s_terminally_stopped` closes future starts; it is not evidence that a
     * preceding bounded join actually completed. A retry after timeout must
     * continue to observe/cancel the still-retiring generation rather than
     * silently report success and allow root dependency teardown to proceed. */
    if (terminal && s_terminally_stopped && !s_task && !s_retiring) {
        const device_status_t exit_status = s_exit_status;
        taskEXIT_CRITICAL(&s_lock);
        return exit_status;
    }
    s_stop_requested = true;
    if (terminal) s_terminally_stopped = true;
    task = s_task;
    completion = s_completion;
    device_status_t exit_status = s_exit_status;
    taskEXIT_CRITICAL(&s_lock);
    /* A worker clears its task handle only after publishing its Registry
     * unregister result. Preserve that terminal result for a late lifecycle
     * observer: reporting OK here would let root teardown proceed while a
     * stale immutable Registry entry still names this generation. */
    if (!task) return exit_status;
    if (xTaskGetCurrentTaskHandle() == task) return DEVICE_STATUS_BUSY;

    const uint32_t cancel_timeout_ms = timeout_ms > 100 ? 100 : timeout_ms;
    s_host.cancel_active_transaction(cancel_timeout_ms, s_host.context);
    xTaskNotifyGive(task);
    if (!completion ||
        xSemaphoreTake(completion, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    /* The worker owns the semaphore object until it has unregistered.  Do not
     * free a completed token here: a later restart retires it only after the
     * old generation cannot signal it again. */
    taskENTER_CRITICAL(&s_lock);
    exit_status = s_exit_status;
    taskEXIT_CRITICAL(&s_lock);
    return exit_status;
}

/* The start gate keeps the newly-created worker inert until the identity is in
 * Task Registry. A lifecycle stop during creation observes `s_starting` and
 * fails closed rather than reporting a task it cannot yet stop as quiesced. */
device_status_t startup_pet_worker_service_start(void) {
    SemaphoreHandle_t stale_completion = NULL;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_terminally_stopped || s_system_sleep_preparing ||
        s_starting || s_task || s_retiring) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* A naturally finished generation leaves its completion token available
     * for a later stop observer. Once no task is active or retiring, the next
     * generation owns a fresh token and may retire the old one. */
    stale_completion = s_completion;
    s_completion = NULL;
    s_starting = true;
    s_exit_status = DEVICE_STATUS_OK;
    taskEXIT_CRITICAL(&s_lock);
    if (stale_completion) vSemaphoreDelete(stale_completion);

    SemaphoreHandle_t completion = xSemaphoreCreateBinary();
    SemaphoreHandle_t gate = xSemaphoreCreateBinary();
    if (!completion || !gate) {
        if (completion) vSemaphoreDelete(completion);
        if (gate) vSemaphoreDelete(gate);
        taskENTER_CRITICAL(&s_lock);
        s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    taskENTER_CRITICAL(&s_lock);
    /* Publish wait objects before the scheduler can run the new task. The
     * gate pointer is also carried as the task argument, so the worker never
     * depends on a late static-handle read. */
    s_start_gate = gate;
    s_completion = completion;
    taskEXIT_CRITICAL(&s_lock);
    TaskHandle_t task = NULL;
    const BaseType_t created = xTaskCreatePinnedToCoreWithCaps(
        startup_pet_worker_task, "maclaw_pet_startup", 8192, gate, 0, &task, 1,
        MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (created != pdPASS) {
        taskENTER_CRITICAL(&s_lock);
        s_start_gate = NULL;
        s_completion = NULL;
        s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
        vSemaphoreDelete(completion);
        vSemaphoreDelete(gate);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }

    const esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
        .name = "startup_pet_asset",
        .context = (void *)task,
        .stop = stop_registry_entry,
    });
    taskENTER_CRITICAL(&s_lock);
    s_task = task;
    s_stop_requested = registry_err != ESP_OK;
    if (registry_err == ESP_OK) s_stop_requested = false;
    if (registry_err != ESP_OK) s_terminally_stopped = true;
    s_starting = false;
    taskEXIT_CRITICAL(&s_lock);
    xSemaphoreGive(gate);
    if (registry_err != ESP_OK) {
        xTaskNotifyGive(task);
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    return DEVICE_STATUS_OK;
}

device_status_t startup_pet_worker_service_init(
    const startup_pet_worker_service_host_t *host) {
    if (!host || host->struct_size != sizeof(*host) || !host->run_transaction ||
        !host->cancel_active_transaction || !host->restart_after_system_sleep_abort) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) {
        const bool same_host = memcmp(&s_host, host, sizeof(*host)) == 0;
        taskEXIT_CRITICAL(&s_lock);
        return same_host ? DEVICE_STATUS_OK : DEVICE_STATUS_BUSY;
    }
    s_host = *host;
    s_initialized = true;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

device_status_t startup_pet_worker_service_stop(uint32_t timeout_ms) {
    return stop_worker(timeout_ms, true);
}

device_status_t startup_pet_worker_service_prepare_system_sleep(uint32_t timeout_ms) {
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_system_sleep_preparing || s_starting) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_preparing = true;
    taskEXIT_CRITICAL(&s_lock);
    const device_status_t status = stop_worker(timeout_ms, false);
    return status;
}

void startup_pet_worker_service_abort_system_sleep_prepare(void) {
    bool restart = false;
    taskENTER_CRITICAL(&s_lock);
    if (s_system_sleep_preparing) {
        s_system_sleep_preparing = false;
        s_stop_requested = false;
        if (s_task || s_retiring) {
            s_system_sleep_restart_pending = true;
        } else {
            restart = true;
        }
    }
    taskEXIT_CRITICAL(&s_lock);
    if (restart) s_host.restart_after_system_sleep_abort(s_host.context);
}

bool startup_pet_worker_service_stop_requested(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool requested = s_stop_requested;
    taskEXIT_CRITICAL(&s_lock);
    return requested;
}

bool startup_pet_worker_service_active(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool active = s_starting || s_task || s_retiring;
    taskEXIT_CRITICAL(&s_lock);
    return active;
}

bool startup_pet_worker_service_is_current_worker(void) {
    const TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_lock);
    const bool current = self && self == s_task;
    taskEXIT_CRITICAL(&s_lock);
    return current;
}

static esp_err_t stop_registry_entry(void *context, uint32_t timeout_ms) {
    taskENTER_CRITICAL(&s_lock);
    const TaskHandle_t task = s_task;
    taskEXIT_CRITICAL(&s_lock);
    if (context && task != (TaskHandle_t)context) return ESP_ERR_INVALID_STATE;
    return status_from_esp_err(startup_pet_worker_service_stop(timeout_ms));
}
