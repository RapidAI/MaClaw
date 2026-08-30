#include "services/cellular_recovery_service.h"

#include <string.h>

#include "esp_err.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "services/cellular_recovery_policy.h"
#include "task_registry.h"

#define CELLULAR_RECOVERY_CONNECT_TIMEOUT_MS 60000u
#define CELLULAR_RECOVERY_IDLE_WAIT_MS 3000u

static const char *TAG = "cellular_recovery";
static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static cellular_recovery_service_host_t s_host;
static TaskHandle_t s_task;
static SemaphoreHandle_t s_start_gate;
static SemaphoreHandle_t s_stopped;
static bool s_stop_requested;
static bool s_starting;
static bool s_admission_open;
static bool s_system_sleep_preparing;
static bool s_system_sleep_was_running;
static bool s_system_sleep_was_admitted;
static bool s_system_sleep_restart_pending;
static bool s_network_restart_preparing;
static bool s_retiring;
/* Gateway startup crosses this service's value seam synchronously. Reserve
 * that tiny handoff so System Sleep cannot park the recovery domain between
 * the final admission check and the host's Gateway generation creation. */
static bool s_gateway_rearm_inflight;
/* Cold-start establish is a synchronous physical modem operation, not the
 * retry task. Keep its fact beside the recovery admission so System Sleep
 * PREPARE cannot report a safe point while an initial ML307 registration is
 * still touching the selected Connectivity generation. */
static bool s_initial_establishing;
static esp_err_t s_exit_status = ESP_OK;
static bool s_registry_retirement_failed;
static bool s_initialized;

static device_status_t status_from_esp_err(esp_err_t err) {
    if (err == ESP_OK) return DEVICE_STATUS_OK;
    if (err == ESP_ERR_TIMEOUT) return DEVICE_STATUS_TIMEOUT;
    if (err == ESP_ERR_INVALID_ARG) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    if (err == ESP_ERR_NO_MEM) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    return DEVICE_STATUS_INTERNAL_ERROR;
}

static esp_err_t status_to_esp_err(device_status_t status) {
    switch (status) {
        case DEVICE_STATUS_OK: return ESP_OK;
        case DEVICE_STATUS_INVALID_ARGUMENT: return ESP_ERR_INVALID_ARG;
        case DEVICE_STATUS_TIMEOUT: return ESP_ERR_TIMEOUT;
        case DEVICE_STATUS_RESOURCE_EXHAUSTED: return ESP_ERR_NO_MEM;
        case DEVICE_STATUS_BUSY: return ESP_ERR_INVALID_STATE;
        default: return ESP_FAIL;
    }
}

static uint32_t remaining_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const int64_t ms = (remaining_us + 999) / 1000;
    return ms > UINT32_MAX ? UINT32_MAX : (uint32_t)ms;
}

static bool stop_requested(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool requested = s_stop_requested;
    taskEXIT_CRITICAL(&s_lock);
    return requested;
}

/* A synchronous establish may complete after a parent lifecycle operation
 * closes this service's admission. Re-check the same logical generation
 * before publishing success or rearming Gateway; the physical Connectivity
 * operation has its own drain fence below this service. */
static bool recovery_admitted(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool admitted = s_initialized && s_admission_open &&
                          !s_system_sleep_preparing && !s_retiring &&
                          !s_network_restart_preparing &&
                          !s_registry_retirement_failed && !s_initial_establishing &&
                          !s_stop_requested;
    taskEXIT_CRITICAL(&s_lock);
    return admitted;
}

static bool begin_gateway_rearm(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool admitted = s_initialized && s_admission_open &&
                          !s_system_sleep_preparing && !s_retiring &&
                          !s_network_restart_preparing &&
                          !s_registry_retirement_failed && !s_initial_establishing &&
                          !s_stop_requested && !s_gateway_rearm_inflight;
    if (admitted) s_gateway_rearm_inflight = true;
    taskEXIT_CRITICAL(&s_lock);
    return admitted;
}

static void end_gateway_rearm(void) {
    taskENTER_CRITICAL(&s_lock);
    s_gateway_rearm_inflight = false;
    taskEXIT_CRITICAL(&s_lock);
}

static void publish_network_ready(bool ready) {
    cellular_recovery_service_host_t host;
    taskENTER_CRITICAL(&s_lock);
    host = s_host;
    taskEXIT_CRITICAL(&s_lock);
    if (host.publish_network_ready) host.publish_network_ready(ready, host.context);
}

static bool should_restart_gateway(void) {
    cellular_recovery_service_host_t host;
    taskENTER_CRITICAL(&s_lock);
    host = s_host;
    taskEXIT_CRITICAL(&s_lock);
    return host.gateway_startup_running && host.gateway_startup_eligible &&
           host.start_gateway_startup && !host.gateway_startup_running(host.context) &&
           host.gateway_startup_eligible(host.context);
}

static bool start_gateway(void) {
    if (!begin_gateway_rearm()) return false;
    cellular_recovery_service_host_t host;
    taskENTER_CRITICAL(&s_lock);
    host = s_host;
    taskEXIT_CRITICAL(&s_lock);
    const bool started = host.start_gateway_startup && host.start_gateway_startup(host.context);
    end_gateway_rearm();
    return started;
}

/* Wi-Fi boards use this service only for the value-level Gateway rearm
 * decision. Do not reserve two RTOS semaphores on every profile merely for
 * that path; cellular task primitives are allocated lazily only when the
 * selected 4G transport actually requests its retry coordinator. */
static bool ensure_cellular_task_primitives(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool already_available = s_start_gate && s_stopped;
    taskEXIT_CRITICAL(&s_lock);
    if (already_available) return true;

    SemaphoreHandle_t start_gate = xSemaphoreCreateBinary();
    SemaphoreHandle_t stopped = xSemaphoreCreateBinary();
    if (!start_gate || !stopped) {
        /* No partial publication: a failed first cellular start remains
         * unavailable and subsequent calls may retry allocation. */
        if (start_gate) vSemaphoreDelete(start_gate);
        if (stopped) vSemaphoreDelete(stopped);
        return false;
    }
    taskENTER_CRITICAL(&s_lock);
    if (!s_start_gate && !s_stopped) {
        s_start_gate = start_gate;
        s_stopped = stopped;
        start_gate = NULL;
        stopped = NULL;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (start_gate) vSemaphoreDelete(start_gate);
    if (stopped) vSemaphoreDelete(stopped);
    return true;
}

static void restart_gateway_after_wifi_ready(void) {
    cellular_recovery_service_host_t host;
    taskENTER_CRITICAL(&s_lock);
    const bool admitted = s_initialized && s_admission_open &&
                          !s_system_sleep_preparing && !s_retiring &&
                          !s_registry_retirement_failed && !s_initial_establishing &&
                          !s_stop_requested;
    host = s_host;
    taskEXIT_CRITICAL(&s_lock);
    if (!admitted || device_connectivity_is_provisioning_active() ||
        !host.wifi_gateway_startup_recovery_allowed ||
        !host.gateway_startup_running || !host.start_gateway_startup ||
        !host.wifi_gateway_startup_recovery_allowed(host.context) ||
        host.gateway_startup_running(host.context)) {
        return;
    }
    ESP_LOGI(TAG, "restarting gateway startup after Wi-Fi recovery");
    if (!start_gateway()) {
        ESP_LOGE(TAG, "cannot restart gateway startup after Wi-Fi recovery");
    }
}

static bool ensure_running_internal(void);

static void recovery_task(void *arg) {
    (void)arg;
    if (!s_start_gate || xSemaphoreTake(s_start_gate, portMAX_DELAY) != pdTRUE) {
        ESP_LOGW(TAG, "cellular recovery start gate unavailable");
        goto finish;
    }
    if (stop_requested()) goto finish;

    uint32_t retry_ms = CELLULAR_RECOVERY_RETRY_INITIAL_MS;
    bool needs_gateway_restart = !device_connectivity_is_cellular_transport_ready();
    while (recovery_admitted() && !device_connectivity_is_provisioning_active() &&
           device_connectivity_is_active_cellular()) {
        if (!device_connectivity_is_cellular_transport_ready()) {
            needs_gateway_restart = true;
            publish_network_ready(false);
            const device_status_t status = device_connectivity_establish_cellular_transport(
                CELLULAR_RECOVERY_CONNECT_TIMEOUT_MS);
            if (status != DEVICE_STATUS_OK) {
                ESP_LOGW(TAG, "cellular recovery failed: status=%d; retry in %lu ms",
                         status, (unsigned long)retry_ms);
                (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(retry_ms));
                retry_ms = cellular_recovery_policy_next_retry_ms(retry_ms);
                continue;
            }
            if (!recovery_admitted()) continue;
            publish_network_ready(true);
            ESP_LOGI(TAG, "cellular network recovered");
        }

        retry_ms = CELLULAR_RECOVERY_RETRY_INITIAL_MS;
        if (recovery_admitted() && needs_gateway_restart && should_restart_gateway()) {
            ESP_LOGI(TAG, "restarting gateway startup after cellular recovery");
            if (start_gateway()) needs_gateway_restart = false;
        }
        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(CELLULAR_RECOVERY_IDLE_WAIT_MS));
    }

finish: {
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    bool restart_after_abort = false;
    taskENTER_CRITICAL(&s_lock);
    s_retiring = true;
    taskEXIT_CRITICAL(&s_lock);
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
    taskENTER_CRITICAL(&s_lock);
    s_exit_status = registry_err;
    if (s_task == self) s_task = NULL;
    s_starting = false;
    s_retiring = false;
    if (registry_err != ESP_OK) {
        s_stop_requested = true;
        s_admission_open = false;
        s_registry_retirement_failed = true;
    }
    if (s_system_sleep_restart_pending && !s_system_sleep_preparing &&
        s_admission_open && registry_err == ESP_OK && !s_registry_retirement_failed) {
        s_system_sleep_restart_pending = false;
        restart_after_abort = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (s_stopped) xSemaphoreGive(s_stopped);
    if (restart_after_abort && !ensure_running_internal()) {
        ESP_LOGW(TAG, "cannot unregister recovery before deferred sleep rollback; "
                      "cannot restore cellular recovery after sleep rollback");
    }
    vTaskDelete(NULL);
}
}

static device_status_t stop_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task;
    taskENTER_CRITICAL(&s_lock);
    s_admission_open = false;
    s_stop_requested = true;
    task = s_task;
    const esp_err_t exit_status = s_exit_status;
    taskEXIT_CRITICAL(&s_lock);
    if (!task) return status_from_esp_err(exit_status);
    if (xTaskGetCurrentTaskHandle() == task) return DEVICE_STATUS_BUSY;
    xTaskNotifyGive(task);
    uint32_t wait_ms = remaining_ms(deadline_us);
    if (!s_stopped || wait_ms == 0 ||
        xSemaphoreTake(s_stopped, pdMS_TO_TICKS(wait_ms)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_lock);
    const esp_err_t completed_status = s_exit_status;
    taskEXIT_CRITICAL(&s_lock);
    return status_from_esp_err(completed_status);
}

static esp_err_t stop_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task;
    taskENTER_CRITICAL(&s_lock);
    task = s_task;
    taskEXIT_CRITICAL(&s_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return status_to_esp_err(stop_task(timeout_ms));
}

static bool ensure_running_internal(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool admitted = s_initialized && s_admission_open && !s_system_sleep_preparing &&
                          !s_network_restart_preparing && !s_registry_retirement_failed &&
                          !s_initial_establishing;
    const bool already_running = s_starting || s_task != NULL || s_retiring;
    if (admitted && !already_running) s_starting = true;
    taskEXIT_CRITICAL(&s_lock);
    if (!admitted) {
        ESP_LOGW(TAG, "cellular recovery start rejected: admission is closed");
        return false;
    }
    if (already_running) return true;
    if (!ensure_cellular_task_primitives()) {
        taskENTER_CRITICAL(&s_lock);
        s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
        ESP_LOGE(TAG, "cellular recovery lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_lock);
    s_stop_requested = false;
    s_exit_status = ESP_OK;
    s_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_lock);
    TaskHandle_t task = NULL;
    if (xTaskCreatePinnedToCore(recovery_task, "maclaw_cellular_recovery", 6144,
                                NULL, 3, &task, 1) != pdPASS) {
        taskENTER_CRITICAL(&s_lock);
        s_task = NULL;
        s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
        ESP_LOGE(TAG, "cannot start cellular recovery task");
        return false;
    }
    taskENTER_CRITICAL(&s_lock);
    s_task = task;
    taskEXIT_CRITICAL(&s_lock);
    const esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
        .name = "cellular_recovery",
        .context = (void *)task,
        .stop = stop_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register cellular recovery coordinator: %s",
                 esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_lock);
        s_stop_requested = true;
        taskEXIT_CRITICAL(&s_lock);
        xSemaphoreGive(s_start_gate);
        (void)stop_task(500);
        return false;
    }
    xSemaphoreGive(s_start_gate);
    return true;
}

device_status_t cellular_recovery_service_init(
    const cellular_recovery_service_host_t *host) {
    if (!host || host->struct_size != sizeof(*host) || !host->publish_network_ready ||
        !host->gateway_startup_running || !host->wifi_gateway_startup_recovery_allowed ||
        !host->gateway_startup_eligible || !host->start_gateway_startup) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) {
        const bool same_host = memcmp(&s_host, host, sizeof(*host)) == 0;
        taskEXIT_CRITICAL(&s_lock);
        return same_host ? DEVICE_STATUS_OK : DEVICE_STATUS_BUSY;
    }
    taskEXIT_CRITICAL(&s_lock);
    taskENTER_CRITICAL(&s_lock);
    s_host = *host;
    s_stop_requested = false;
    s_starting = false;
    s_admission_open = true;
    s_system_sleep_preparing = false;
    s_system_sleep_was_running = false;
    s_system_sleep_was_admitted = false;
    s_system_sleep_restart_pending = false;
    s_network_restart_preparing = false;
    s_retiring = false;
    s_gateway_rearm_inflight = false;
    s_initial_establishing = false;
    s_exit_status = ESP_OK;
    s_registry_retirement_failed = false;
    s_initialized = true;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

device_status_t cellular_recovery_service_establish_initial(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    const bool admitted = s_initialized && s_admission_open &&
                          !s_system_sleep_preparing && !s_retiring &&
                          !s_network_restart_preparing &&
                          !s_registry_retirement_failed && !s_initial_establishing &&
                          !s_starting && !s_task;
    if (admitted) s_initial_establishing = true;
    taskEXIT_CRITICAL(&s_lock);
    if (!admitted) {
        taskENTER_CRITICAL(&s_lock);
        const bool initialized = s_initialized;
        taskEXIT_CRITICAL(&s_lock);
        return initialized ? DEVICE_STATUS_BUSY : DEVICE_STATUS_UNAVAILABLE;
    }
    publish_network_ready(false);
    const device_status_t status =
        device_connectivity_establish_cellular_transport(timeout_ms);
    taskENTER_CRITICAL(&s_lock);
    const bool publish_ready = status == DEVICE_STATUS_OK && s_initialized &&
                               s_admission_open && !s_system_sleep_preparing &&
                               !s_network_restart_preparing && !s_retiring &&
                               !s_registry_retirement_failed;
    s_initial_establishing = false;
    taskEXIT_CRITICAL(&s_lock);
    if (publish_ready) {
        publish_network_ready(true);
        ESP_LOGI(TAG, "cellular network ready");
    }
    (void)ensure_running_internal();
    return status;
}

bool cellular_recovery_service_ensure_running(void) {
    return ensure_running_internal();
}

void cellular_recovery_service_note_wifi_ready(void) {
    restart_gateway_after_wifi_ready();
}

device_status_t cellular_recovery_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    bool was_running;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (s_system_sleep_preparing || s_network_restart_preparing || s_initial_establishing ||
        s_gateway_rearm_inflight ||
        (s_starting && !s_task) || s_retiring || s_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_preparing = true;
    s_system_sleep_was_admitted = s_admission_open;
    was_running = s_task != NULL;
    s_system_sleep_was_running = was_running;
    taskEXIT_CRITICAL(&s_lock);
    return was_running ? stop_task(timeout_ms) : DEVICE_STATUS_OK;
}

void cellular_recovery_service_abort_system_sleep_prepare(void) {
    bool restart = false;
    taskENTER_CRITICAL(&s_lock);
    if (!s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_lock);
        return;
    }
    restart = s_system_sleep_was_running;
    s_admission_open = s_system_sleep_was_admitted;
    s_system_sleep_was_admitted = false;
    s_system_sleep_preparing = false;
    if (restart && (s_task || s_starting || s_retiring ||
                    s_registry_retirement_failed)) {
        s_system_sleep_restart_pending = true;
        restart = false;
    }
    s_system_sleep_was_running = false;
    taskEXIT_CRITICAL(&s_lock);
    if (restart && !ensure_running_internal()) {
        ESP_LOGW(TAG, "cannot restore cellular recovery after sleep abort");
    }
}

device_status_t cellular_recovery_service_prepare_network_restart(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    bool was_running;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    /* An establish/rearm handoff may still touch the selected Connectivity
     * generation. Refuse to certify the retry domain quiescent until it has
     * reached its own value-level safe point. */
    if (s_system_sleep_preparing || s_network_restart_preparing ||
        s_initial_establishing || s_gateway_rearm_inflight ||
        (s_starting && !s_task) || s_retiring || s_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_network_restart_preparing = true;
    s_admission_open = false;
    was_running = s_task != NULL;
    taskEXIT_CRITICAL(&s_lock);
    return was_running ? stop_task(timeout_ms) : DEVICE_STATUS_OK;
}

device_status_t cellular_recovery_service_commit_prepared_network_restart(void) {
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (!s_network_restart_preparing || s_task || s_starting || s_retiring ||
        s_initial_establishing || s_gateway_rearm_inflight) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* Never turn this into a modem lifecycle claim. It only proves that the
     * common retry worker cannot publish/rearm against the retiring root. */
    s_network_restart_preparing = false;
    s_stop_requested = true;
    s_system_sleep_restart_pending = false;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}
