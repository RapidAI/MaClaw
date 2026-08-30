#include "services/wake_restart_worker_service.h"

#include "esp_err.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "task_registry.h"

#define WAKE_RESTART_WORKER_STACK_SIZE 2048u
#define WAKE_RESTART_INITIAL_WAIT_MS 250u
#define WAKE_RESTART_RETRY_WAIT_MS 500u
#define WAKE_RESTART_FOREGROUND_WAIT_MS 100u
#define WAKE_RESTART_BACKOFF_WAIT_MS 1000u

static const char *TAG = "wake_restart_worker";
static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static wake_restart_worker_service_host_t s_host;
static TaskHandle_t s_task;
static SemaphoreHandle_t s_start_gate;
static SemaphoreHandle_t s_stopped;
static bool s_initialized;
static bool s_starting;
static bool s_stop_requested;
static bool s_admission_open;
static bool s_startup_teardown_pending;
static bool s_system_sleep_preparing;
static bool s_system_sleep_was_running;
static bool s_system_sleep_was_admitted;
static bool s_system_sleep_restart_pending;
static bool s_network_restart_preparing;
static bool s_retiring;
static bool s_registry_retirement_failed;
static device_status_t s_exit_status = DEVICE_STATUS_OK;

static device_status_t status_from_esp_err(esp_err_t err) {
    if (err == ESP_OK) return DEVICE_STATUS_OK;
    if (err == ESP_ERR_TIMEOUT) return DEVICE_STATUS_TIMEOUT;
    if (err == ESP_ERR_NO_MEM) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (err == ESP_ERR_INVALID_ARG) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return DEVICE_STATUS_INTERNAL_ERROR;
}

static uint32_t remaining_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const uint64_t rounded_ms = ((uint64_t)remaining_us + 999u) / 1000u;
    return rounded_ms > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded_ms;
}

static bool stop_requested(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool requested = s_stop_requested;
    taskEXIT_CRITICAL(&s_lock);
    return requested;
}

static esp_err_t stop_registry_entry(void *context, uint32_t timeout_ms);
static device_status_t start_worker(void);

static void finish_worker(void) {
    const TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_lock);
    s_retiring = true;
    taskEXIT_CRITICAL(&s_lock);
    /* Completion is not a replacement permission.  Retire this exact
     * immutable Registry identity first, then make terminal state visible. */
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_AUDIO, (void *)self, 10);
    bool restart_after_abort = false;
    taskENTER_CRITICAL(&s_lock);
    s_exit_status = status_from_esp_err(registry_err);
    if (s_task == self) s_task = NULL;
    s_starting = false;
    s_retiring = false;
    if (registry_err != ESP_OK) {
        s_stop_requested = true;
        s_admission_open = false;
        s_registry_retirement_failed = true;
    }
    if (registry_err == ESP_OK && s_system_sleep_restart_pending &&
        !s_system_sleep_preparing && s_admission_open) {
        s_system_sleep_restart_pending = false;
        restart_after_abort = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (s_stopped) xSemaphoreGive(s_stopped);
    if (restart_after_abort) (void)start_worker();
    vTaskDeleteWithCaps(NULL);
}

static void wake_restart_worker_task(void *arg) {
    (void)arg;
    if (!s_start_gate || xSemaphoreTake(s_start_gate, portMAX_DELAY) != pdTRUE) {
        finish_worker();
        return;
    }

    /* Give a foreground worker its chance to release the internal stack and
     * other optional memory before attempting to bring MultiNet back. */
    (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(WAKE_RESTART_INITIAL_WAIT_MS));
    device_status_t status = DEVICE_STATUS_INTERNAL_ERROR;
    unsigned attempt = 1;
    bool waiting_logged = false;
    while (!stop_requested() && s_host.restart_allowed(s_host.context)) {
        if (attempt > 12u) {
            ESP_LOGE(TAG, "offline wake restart exhausted; retrying after backoff");
            attempt = 1;
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(WAKE_RESTART_BACKOFF_WAIT_MS));
            continue;
        }
        if (s_host.foreground_active(s_host.context) || s_host.meeting_active(s_host.context) ||
            s_host.optional_pet_worker_active(s_host.context)) {
            if (!waiting_logged) {
                ESP_LOGI(TAG, "offline wake restart waiting for foreground owner");
                waiting_logged = true;
            }
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(WAKE_RESTART_FOREGROUND_WAIT_MS));
            continue;
        }
        waiting_logged = false;
        s_host.discard_asset_client(s_host.context);
        status = s_host.start_wake_word(s_host.context);
        if (status == DEVICE_STATUS_OK) break;
        ESP_LOGW(TAG, "offline wake restart attempt %u/12 failed: status=%d",
                 attempt, (int)status);
        ++attempt;
        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(WAKE_RESTART_RETRY_WAIT_MS));
    }
    if (status == DEVICE_STATUS_OK) {
        ESP_LOGI(TAG, "offline wake restarted after foreground interaction");
    }
    finish_worker();
}

static device_status_t stop_worker(uint32_t timeout_ms, bool terminal) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_starting) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_stop_requested = true;
    if (terminal) s_admission_open = false;
    const TaskHandle_t task = s_task;
    const device_status_t exit_status = s_exit_status;
    taskEXIT_CRITICAL(&s_lock);
    if (!task) return exit_status;
    if (xTaskGetCurrentTaskHandle() == task) return DEVICE_STATUS_BUSY;
    /* A Registry sweep can race registration; opening the start gate keeps a
     * newly-created worker from stranding a bounded join. */
    if (s_start_gate) xSemaphoreGive(s_start_gate);
    xTaskNotifyGive(task);
    const uint32_t remaining = remaining_ms(deadline_us);
    if (!s_stopped || !remaining ||
        xSemaphoreTake(s_stopped, pdMS_TO_TICKS(remaining)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_lock);
    const device_status_t status = s_exit_status;
    taskEXIT_CRITICAL(&s_lock);
    return status;
}

static esp_err_t stop_registry_entry(void *context, uint32_t timeout_ms) {
    taskENTER_CRITICAL(&s_lock);
    const TaskHandle_t task = s_task;
    taskEXIT_CRITICAL(&s_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    const device_status_t status = stop_worker(timeout_ms, true);
    switch (status) {
        case DEVICE_STATUS_OK: return ESP_OK;
        case DEVICE_STATUS_TIMEOUT: return ESP_ERR_TIMEOUT;
        case DEVICE_STATUS_INVALID_ARGUMENT: return ESP_ERR_INVALID_ARG;
        case DEVICE_STATUS_BUSY: return ESP_ERR_INVALID_STATE;
        default: return ESP_FAIL;
    }
}

static device_status_t start_worker(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool admission_open = s_initialized && s_admission_open &&
                                !s_registry_retirement_failed &&
                                !s_system_sleep_preparing &&
                                !s_network_restart_preparing;
    const bool already_running = s_task || s_starting || s_retiring;
    if (admission_open && !already_running) s_starting = true;
    taskEXIT_CRITICAL(&s_lock);
    if (!admission_open) return DEVICE_STATUS_BUSY;
    if (already_running) return DEVICE_STATUS_OK;
    if (!s_host.restart_allowed(s_host.context)) {
        taskENTER_CRITICAL(&s_lock);
        s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    if (!s_start_gate || !s_stopped) {
        taskENTER_CRITICAL(&s_lock);
        s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    while (xSemaphoreTake(s_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_lock);
    s_stop_requested = false;
    s_exit_status = DEVICE_STATUS_OK;
    taskEXIT_CRITICAL(&s_lock);
    TaskHandle_t task = NULL;
    if (xTaskCreateWithCaps(wake_restart_worker_task, "maclaw_wake_restart",
                            WAKE_RESTART_WORKER_STACK_SIZE, NULL, 2, &task,
                            MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT) != pdPASS) {
        taskENTER_CRITICAL(&s_lock);
        s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    taskENTER_CRITICAL(&s_lock);
    s_task = task;
    taskEXIT_CRITICAL(&s_lock);
    const esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_AUDIO,
        .name = "wake_restart",
        .context = (void *)task,
        .stop = stop_registry_entry,
    });
    if (registry_err != ESP_OK) {
        taskENTER_CRITICAL(&s_lock);
        s_stop_requested = true;
        taskEXIT_CRITICAL(&s_lock);
        xSemaphoreGive(s_start_gate);
        return status_from_esp_err(registry_err);
    }
    xSemaphoreGive(s_start_gate);
    ESP_LOGI(TAG, "offline wake restart scheduled");
    return DEVICE_STATUS_OK;
}

device_status_t wake_restart_worker_service_init(
    const wake_restart_worker_service_host_t *host) {
    if (!host || host->struct_size != sizeof(*host) || !host->restart_allowed ||
        !host->foreground_active || !host->meeting_active ||
        !host->optional_pet_worker_active || !host->discard_asset_client ||
        !host->start_wake_word) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    taskENTER_CRITICAL(&s_lock);
    const bool initialized = s_initialized;
    taskEXIT_CRITICAL(&s_lock);
    if (initialized) return DEVICE_STATUS_BUSY;
    SemaphoreHandle_t gate = xSemaphoreCreateBinary();
    SemaphoreHandle_t stopped = xSemaphoreCreateBinary();
    if (!gate || !stopped) {
        if (gate) vSemaphoreDelete(gate);
        if (stopped) vSemaphoreDelete(stopped);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    taskENTER_CRITICAL(&s_lock);
    s_host = *host;
    s_start_gate = gate;
    s_stopped = stopped;
    s_stop_requested = false;
    s_admission_open = true;
    s_startup_teardown_pending = false;
    s_system_sleep_preparing = false;
    s_system_sleep_was_running = false;
    s_system_sleep_was_admitted = false;
    s_system_sleep_restart_pending = false;
    s_network_restart_preparing = false;
    s_retiring = false;
    s_registry_retirement_failed = false;
    s_exit_status = DEVICE_STATUS_OK;
    s_initialized = true;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

device_status_t wake_restart_worker_service_start(void) {
    return start_worker();
}

void wake_restart_worker_service_note_startup_teardown(void) {
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized && s_admission_open && !s_registry_retirement_failed &&
        !s_system_sleep_preparing && !s_network_restart_preparing) {
        s_startup_teardown_pending = true;
    }
    taskEXIT_CRITICAL(&s_lock);
}

bool wake_restart_worker_service_consume_startup_teardown(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool pending = s_startup_teardown_pending;
    s_startup_teardown_pending = false;
    taskEXIT_CRITICAL(&s_lock);
    return pending;
}

device_status_t wake_restart_worker_service_stop(uint32_t timeout_ms) {
    return stop_worker(timeout_ms, true);
}

void wake_restart_worker_service_close_admission(void) {
    taskENTER_CRITICAL(&s_lock);
    s_admission_open = false;
    s_stop_requested = true;
    taskEXIT_CRITICAL(&s_lock);
}

device_status_t wake_restart_worker_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_system_sleep_preparing || s_network_restart_preparing || s_starting ||
        s_startup_teardown_pending || s_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_preparing = true;
    s_system_sleep_was_admitted = s_admission_open;
    s_system_sleep_was_running = s_task != NULL;
    s_admission_open = false;
    const bool was_running = s_system_sleep_was_running;
    taskEXIT_CRITICAL(&s_lock);
    if (!was_running) return DEVICE_STATUS_OK;
    return stop_worker(timeout_ms, false);
}

void wake_restart_worker_service_abort_system_sleep_prepare(void) {
    bool restart = false;
    taskENTER_CRITICAL(&s_lock);
    if (!s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_lock);
        return;
    }
    restart = s_system_sleep_was_running;
    s_admission_open = !s_registry_retirement_failed &&
                       s_system_sleep_was_admitted;
    s_system_sleep_was_running = false;
    s_system_sleep_was_admitted = false;
    s_system_sleep_preparing = false;
    if (restart && (s_task || s_starting || s_retiring)) {
        s_system_sleep_restart_pending = true;
        restart = false;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (restart) (void)start_worker();
}

device_status_t wake_restart_worker_service_prepare_network_restart(
    uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_system_sleep_preparing || s_network_restart_preparing ||
        s_starting || s_startup_teardown_pending || s_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* Do not describe this as a sleep participant: once a physical network
     * root is retired, an ABORT must never recreate this old audio worker. */
    s_network_restart_preparing = true;
    s_admission_open = false;
    const bool was_running = s_task != NULL;
    taskEXIT_CRITICAL(&s_lock);
    return was_running ? stop_worker(timeout_ms, false) : DEVICE_STATUS_OK;
}

device_status_t wake_restart_worker_service_commit_prepared_network_restart(void) {
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (!s_network_restart_preparing || s_task || s_starting || s_retiring) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* Keep admission closed and discard any possibility of a paired sleep
     * rollback. Replacement is deliberately outside this retired generation. */
    s_network_restart_preparing = false;
    s_stop_requested = true;
    s_system_sleep_restart_pending = false;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}
