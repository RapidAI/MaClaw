#include "services/deferred_setup_worker_service.h"

#include <string.h>

#include "esp_err.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "task_registry.h"

#define DEFERRED_SETUP_WAIT_US 5000000LL
#define DEFERRED_SETUP_STACK_SIZE 12288u

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static deferred_setup_worker_service_host_t s_host;
static TaskHandle_t s_task;
static SemaphoreHandle_t s_start_gate;
static SemaphoreHandle_t s_stopped;
static bool s_initialized;
static bool s_starting;
static bool s_stop_requested;
static bool s_admission_open;
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

static void deferred_setup_worker_task(void *arg) {
    (void)arg;
    if (!s_start_gate || xSemaphoreTake(s_start_gate, portMAX_DELAY) != pdTRUE) {
        vTaskDelete(NULL);
        return;
    }

    /* Keep portal/radio work out of the GPIO scanner.  A currently active
     * meeting receives at most five seconds to reach a safe boundary. */
    const int64_t deadline_us = esp_timer_get_time() + DEFERRED_SETUP_WAIT_US;
    while (!stop_requested() && s_host.meeting_active(s_host.context) &&
           esp_timer_get_time() < deadline_us) {
        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(100));
    }
    if (!stop_requested()) s_host.start_setup_portal(s_host.context);

    const TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_lock);
    s_retiring = true;
    taskEXIT_CRITICAL(&s_lock);
    /* Do not publish completion before the old immutable identity has gone:
     * a replacement must never be stopped through this generation's entry. */
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
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
    vTaskDelete(NULL);
}

static device_status_t stop_worker(uint32_t timeout_ms, bool terminal) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_starting) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_stop_requested = true;
    if (terminal) s_admission_open = false;
    task = s_task;
    const device_status_t exit_status = s_exit_status;
    taskEXIT_CRITICAL(&s_lock);
    if (!task) return exit_status;
    if (xTaskGetCurrentTaskHandle() == task) return DEVICE_STATUS_BUSY;
    /* A Registry sweep may reach us before the creator releases the gate. */
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
    if (xTaskCreate(deferred_setup_worker_task, "maclaw_setup_wait",
                    DEFERRED_SETUP_STACK_SIZE, NULL, 5, &task) != pdPASS) {
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
        .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
        .name = "deferred_setup",
        .context = (void *)task,
        .stop = stop_registry_entry,
    });
    if (registry_err != ESP_OK) {
        taskENTER_CRITICAL(&s_lock);
        s_stop_requested = true;
        s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
        xSemaphoreGive(s_start_gate);
        (void)stop_worker(500, true);
        return status_from_esp_err(registry_err);
    }
    xSemaphoreGive(s_start_gate);
    return DEVICE_STATUS_OK;
}

device_status_t deferred_setup_worker_service_init(
    const deferred_setup_worker_service_host_t *host) {
    if (!host || host->struct_size != sizeof(*host) || !host->meeting_active ||
        !host->start_setup_portal) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    taskENTER_CRITICAL(&s_lock);
    const bool already_initialized = s_initialized;
    taskEXIT_CRITICAL(&s_lock);
    if (already_initialized) return DEVICE_STATUS_BUSY;
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

device_status_t deferred_setup_worker_service_start(void) {
    return start_worker();
}

device_status_t deferred_setup_worker_service_stop(uint32_t timeout_ms) {
    return stop_worker(timeout_ms, true);
}

device_status_t deferred_setup_worker_service_prepare_system_sleep(
    uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_system_sleep_preparing || s_network_restart_preparing || s_starting ||
        s_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_preparing = true;
    s_system_sleep_was_admitted = s_admission_open;
    s_admission_open = false;
    s_system_sleep_was_running = s_task != NULL;
    const bool was_running = s_system_sleep_was_running;
    taskEXIT_CRITICAL(&s_lock);
    if (!was_running) return DEVICE_STATUS_OK;
    return stop_worker(timeout_ms, false);
}

void deferred_setup_worker_service_abort_system_sleep_prepare(void) {
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

device_status_t deferred_setup_worker_service_prepare_network_restart(
    uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_system_sleep_preparing || s_network_restart_preparing ||
        s_starting || s_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_network_restart_preparing = true;
    s_admission_open = false;
    const bool was_running = s_task != NULL;
    taskEXIT_CRITICAL(&s_lock);
    return was_running ? stop_worker(timeout_ms, false) : DEVICE_STATUS_OK;
}

device_status_t deferred_setup_worker_service_commit_prepared_network_restart(void) {
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (!s_network_restart_preparing || s_task || s_starting || s_retiring) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_network_restart_preparing = false;
    s_stop_requested = true;
    s_system_sleep_restart_pending = false;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}
