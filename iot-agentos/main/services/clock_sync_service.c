#include "services/clock_sync_service.h"
#include "trusted_time_policy.h"

#include <limits.h>
#include <errno.h>
#include <string.h>
#include <sys/time.h>

#include "esp_err.h"
#include "esp_log.h"
#include "esp_netif_sntp.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "task_registry.h"

#define CLOCK_SYNC_WAIT_MS 12000u
#define CLOCK_SYNC_RETRY_MS 30000u

static const char *TAG = "clock_sync";
static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static clock_sync_service_host_t s_host;
static bool s_initialized;
static bool s_sntp_initialized;
static bool s_complete;
static TaskHandle_t s_task;
static bool s_starting;
static SemaphoreHandle_t s_start_gate;
static SemaphoreHandle_t s_stopped;
static bool s_stop_requested;
static bool s_system_sleep_preparing;
static bool s_system_sleep_was_initialized;
static uint32_t s_system_sleep_callback_users;
static uint32_t s_system_sleep_start_users;
static bool s_system_sleep_restart_pending;
static bool s_retiring;
/* A completed monitor must not be treated as retired until its immutable
 * Connectivity Registry identity is gone. Keep the result and close future
 * start/ABORT admission if bookkeeping cannot prove that retirement. */
static esp_err_t s_exit_status = ESP_OK;
static bool s_registry_retirement_failed;
static trusted_time_state_t s_time_state;
/* settimeofday() is performed outside the critical section, so serialize the
 * whole admit -> apply -> publish window explicitly.  This prevents a SNTP
 * callback and an authenticated Hub sample from both validating against the
 * same prior state and then applying in reverse order. */
static bool s_time_apply_inflight;

static uint32_t remaining_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const int64_t rounded = (remaining_us + 999) / 1000;
    return rounded > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded;
}

static device_status_t status_from_esp_err(esp_err_t err) {
    if (err == ESP_OK) return DEVICE_STATUS_OK;
    if (err == ESP_ERR_TIMEOUT) return DEVICE_STATUS_TIMEOUT;
    if (err == ESP_ERR_NO_MEM) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (err == ESP_ERR_INVALID_ARG) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return DEVICE_STATUS_INTERNAL_ERROR;
}

static esp_err_t status_to_esp_err(device_status_t status) {
    switch (status) {
        case DEVICE_STATUS_OK: return ESP_OK;
        case DEVICE_STATUS_TIMEOUT: return ESP_ERR_TIMEOUT;
        case DEVICE_STATUS_RESOURCE_EXHAUSTED: return ESP_ERR_NO_MEM;
        case DEVICE_STATUS_INVALID_ARGUMENT: return ESP_ERR_INVALID_ARG;
        case DEVICE_STATUS_BUSY: return ESP_ERR_INVALID_STATE;
        default: return ESP_FAIL;
    }
}

static void publish_trusted_epoch(int64_t epoch_sec,
                                  trusted_time_source_t source) {
    if (epoch_sec < 1672531200) return; /* 2023-01-01 UTC */
    trusted_time_sample_t sample = {
        .struct_size = sizeof(sample),
        .abi_version = TRUSTED_TIME_SAMPLE_ABI_VERSION,
        .source = source,
        .epoch_sec = epoch_sec,
        .usec = 0,
    };
    const uint64_t monotonic_ms = (uint64_t)(esp_timer_get_time() / 1000);
    clock_sync_service_host_t host;
    trusted_time_state_t candidate_state;
    bool admitted = false;
    taskENTER_CRITICAL(&s_lock);
    candidate_state = s_time_state;
    if (s_initialized && !s_system_sleep_preparing && !s_time_apply_inflight &&
        trusted_time_policy_state_observe(&candidate_state, &sample,
                                          monotonic_ms)) {
        s_time_apply_inflight = true;
        ++s_system_sleep_callback_users;
        admitted = true;
    }
    host = s_host;
    taskEXIT_CRITICAL(&s_lock);
    /* A callback already selected before PREPARE must not mutate Ambient or
     * deadline state after the System Sleep boundary. */
    if (!admitted) return;

    /* SNTP invokes this callback after applying its accepted sample.  Only
     * authenticated Hub input performs an explicit settimeofday() below. */
    taskENTER_CRITICAL(&s_lock);
    s_time_state = candidate_state;
    taskEXIT_CRITICAL(&s_lock);
    host.note_wall_clock(epoch_sec, host.context);
    taskENTER_CRITICAL(&s_lock);
    s_complete = true;
    taskEXIT_CRITICAL(&s_lock);
    host.notify_wall_clock_updated(host.context);
    ESP_LOGI(TAG, "clock synchronized: epoch=%lld", (long long)epoch_sec);

    taskENTER_CRITICAL(&s_lock);
    s_time_apply_inflight = false;
    if (s_system_sleep_callback_users) --s_system_sleep_callback_users;
    taskEXIT_CRITICAL(&s_lock);
}

static void sntp_sync_cb(struct timeval *tv) {
    if (!tv) return;
    publish_trusted_epoch((int64_t)tv->tv_sec, TRUSTED_TIME_SOURCE_SNTP);
}

static device_status_t wait_time_callbacks_drained(int64_t deadline_us) {
    for (;;) {
        taskENTER_CRITICAL(&s_lock);
        const bool drained = s_system_sleep_callback_users == 0u &&
                             !s_time_apply_inflight;
        taskEXIT_CRITICAL(&s_lock);
        if (drained) return DEVICE_STATUS_OK;
        const uint32_t remaining = remaining_ms(deadline_us);
        if (remaining == 0u) return DEVICE_STATUS_TIMEOUT;
        vTaskDelay(1);
    }
}

bool clock_sync_service_apply_authenticated_millis(double epoch_ms) {
    trusted_time_sample_t sample = {
        .struct_size = sizeof(sample),
        .abi_version = TRUSTED_TIME_SAMPLE_ABI_VERSION,
    };
    if (!trusted_time_policy_from_millis(
            epoch_ms, TRUSTED_TIME_SOURCE_HUB_AUTHENTICATED, &sample)) {
        ESP_LOGW(TAG, "ignored invalid authenticated server time");
        return false;
    }

    const uint64_t monotonic_ms = (uint64_t)(esp_timer_get_time() / 1000);
    clock_sync_service_host_t host;
    trusted_time_state_t candidate_state;
    bool admitted = false;
    taskENTER_CRITICAL(&s_lock);
    candidate_state = s_time_state;
    if (s_initialized && !s_system_sleep_preparing && !s_time_apply_inflight &&
        trusted_time_policy_state_observe(&candidate_state, &sample,
                                          monotonic_ms)) {
        s_time_apply_inflight = true;
        ++s_system_sleep_callback_users;
        admitted = true;
    }
    host = s_host;
    taskEXIT_CRITICAL(&s_lock);
    if (!admitted) {
        ESP_LOGW(TAG, "ignored authenticated server time trust-state anomaly");
        return false;
    }

    struct timeval tv = {
        .tv_sec = (time_t)sample.epoch_sec,
        .tv_usec = (suseconds_t)sample.usec,
    };
    if (settimeofday(&tv, NULL) != 0) {
        ESP_LOGW(TAG, "cannot apply authenticated server time: errno=%d", errno);
        taskENTER_CRITICAL(&s_lock);
        s_time_apply_inflight = false;
        if (s_system_sleep_callback_users) --s_system_sleep_callback_users;
        taskEXIT_CRITICAL(&s_lock);
        return false;
    }
    /* Commit the trust state only after the platform clock accepted the
     * sample.  A failed settimeofday must not poison anomaly/rollback state.
     * If SNTP concurrently published a newer sample, preserve that newer
     * state rather than overwriting it with this stale candidate. */
    taskENTER_CRITICAL(&s_lock);
    s_time_state = candidate_state;
    taskEXIT_CRITICAL(&s_lock);
    host.note_wall_clock(sample.epoch_sec, host.context);
    /* Authenticated Hub time on cellular has no SNTP callback to start the
     * standby cadence; use the same host hook as the monitor path. */
    host.ensure_ambient_clock(host.context);
    taskENTER_CRITICAL(&s_lock);
    s_complete = true;
    taskEXIT_CRITICAL(&s_lock);
    host.notify_wall_clock_updated(host.context);
    ESP_LOGI(TAG, "clock source: gateway serverTime");
    taskENTER_CRITICAL(&s_lock);
    s_time_apply_inflight = false;
    if (s_system_sleep_callback_users) --s_system_sleep_callback_users;
    taskEXIT_CRITICAL(&s_lock);
    return true;
}

static bool stop_requested(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool requested = s_stop_requested;
    taskEXIT_CRITICAL(&s_lock);
    return requested;
}

static void start_internal(bool system_sleep_resume);

static void clock_sync_task(void *arg) {
    (void)arg;
    if (!s_start_gate || xSemaphoreTake(s_start_gate, portMAX_DELAY) != pdTRUE) {
        vTaskDelete(NULL);
        return;
    }
    unsigned attempt = 1;
    while (!stop_requested()) {
        taskENTER_CRITICAL(&s_lock);
        const bool complete = s_complete;
        taskEXIT_CRITICAL(&s_lock);
        if (complete) break;

        esp_err_t wait_err = ESP_ERR_TIMEOUT;
        const TickType_t started = xTaskGetTickCount();
        const TickType_t budget = pdMS_TO_TICKS(CLOCK_SYNC_WAIT_MS);
        while ((xTaskGetTickCount() - started) < budget) {
            const TickType_t elapsed = xTaskGetTickCount() - started;
            const TickType_t remaining = budget - elapsed;
            TickType_t slice = pdMS_TO_TICKS(250);
            if (slice > remaining) slice = remaining;
            wait_err = esp_netif_sntp_sync_wait(slice);
            taskENTER_CRITICAL(&s_lock);
            const bool complete_now = s_complete;
            taskEXIT_CRITICAL(&s_lock);
            if (stop_requested() || wait_err == ESP_OK || complete_now) break;
        }
        taskENTER_CRITICAL(&s_lock);
        const bool complete_now = s_complete;
        taskEXIT_CRITICAL(&s_lock);
        if (stop_requested() || wait_err == ESP_OK || complete_now) break;

        unsigned int reachability[CONFIG_LWIP_SNTP_MAX_SERVERS] = {0};
        for (unsigned i = 0; i < CONFIG_LWIP_SNTP_MAX_SERVERS; ++i) {
            if (esp_netif_sntp_reachability(i, &reachability[i]) != ESP_OK) {
                reachability[i] = 0;
            }
        }
        ESP_LOGW(TAG, "attempt %u timed out: wait=%s reachability=%02x/%02x/%02x; retrying",
                 attempt, esp_err_to_name(wait_err), reachability[0],
                 reachability[1], reachability[2]);
        esp_err_t restart_err = esp_netif_sntp_start();
        if (restart_err != ESP_OK) {
            ESP_LOGW(TAG, "SNTP restart failed: %s", esp_err_to_name(restart_err));
        }
        ++attempt;
        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(CLOCK_SYNC_RETRY_MS));
    }

    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    bool restart_after_abort = false;
    taskENTER_CRITICAL(&s_lock);
    if (s_task == self) s_retiring = true;
    taskEXIT_CRITICAL(&s_lock);
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
    taskENTER_CRITICAL(&s_lock);
    s_exit_status = registry_err;
    if (s_task == self) s_task = NULL;
    s_retiring = false;
    if (registry_err != ESP_OK) {
        s_stop_requested = true;
        s_registry_retirement_failed = true;
    }
    if (s_system_sleep_restart_pending && !s_system_sleep_preparing &&
        !s_registry_retirement_failed && registry_err == ESP_OK) {
        s_system_sleep_restart_pending = false;
        restart_after_abort = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (s_stopped) xSemaphoreGive(s_stopped);
    if (restart_after_abort) start_internal(true);
    vTaskDelete(NULL);
}

static device_status_t stop_monitor_until_deadline(int64_t deadline_us) {
    TaskHandle_t task;
    taskENTER_CRITICAL(&s_lock);
    s_stop_requested = true;
    task = s_task;
    taskEXIT_CRITICAL(&s_lock);
    if (!task) {
        taskENTER_CRITICAL(&s_lock);
        const esp_err_t exit_status = s_exit_status;
        taskEXIT_CRITICAL(&s_lock);
        return status_from_esp_err(exit_status);
    }
    if (xTaskGetCurrentTaskHandle() == task) return DEVICE_STATUS_BUSY;
    xTaskNotifyGive(task);
    uint32_t wait_ms = remaining_ms(deadline_us);
    if (!s_stopped || wait_ms == 0 ||
        xSemaphoreTake(s_stopped, pdMS_TO_TICKS(wait_ms)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_lock);
    const esp_err_t exit_status = s_exit_status;
    taskEXIT_CRITICAL(&s_lock);
    return status_from_esp_err(exit_status);
}

static device_status_t stop_monitor(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    return stop_monitor_until_deadline(deadline_us);
}

static esp_err_t stop_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task;
    taskENTER_CRITICAL(&s_lock);
    task = s_task;
    taskEXIT_CRITICAL(&s_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return status_to_esp_err(stop_monitor(timeout_ms));
}

static device_status_t stop_until_deadline(int64_t deadline_us) {
    const device_status_t monitor_status = stop_monitor_until_deadline(deadline_us);
    if (monitor_status != DEVICE_STATUS_OK) return monitor_status;
    /* The monitor task and SNTP singleton are independent of callback
     * delivery.  A Hub-authenticated sample may still be between admission
     * and consumer notification, so wait for that immutable callback lease
     * before deinitializing ESP-NETIF's SNTP state. */
    const device_status_t callback_status = wait_time_callbacks_drained(deadline_us);
    if (callback_status != DEVICE_STATUS_OK) return callback_status;
    bool initialized;
    taskENTER_CRITICAL(&s_lock);
    initialized = s_sntp_initialized;
    taskEXIT_CRITICAL(&s_lock);
    if (!initialized) {
        return remaining_ms(deadline_us) == 0u ? DEVICE_STATUS_TIMEOUT
                                              : DEVICE_STATUS_OK;
    }
    /* ESP-NETIF deinit has no timeout. It is safe only after the monitor join
     * above proves no task can query or restart the singleton. */
    esp_netif_sntp_deinit();
    taskENTER_CRITICAL(&s_lock);
    s_sntp_initialized = false;
    taskEXIT_CRITICAL(&s_lock);
    ESP_LOGI(TAG, "SNTP service deinitialized");
    return remaining_ms(deadline_us) == 0u ? DEVICE_STATUS_TIMEOUT
                                          : DEVICE_STATUS_OK;
}

device_status_t clock_sync_service_stop(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    return stop_until_deadline(deadline_us);
}

static void start_internal(bool system_sleep_resume) {
    clock_sync_service_host_t host;
    bool admitted = false;
    bool restart_pending;
    bool preparing;
    taskENTER_CRITICAL(&s_lock);
    preparing = s_system_sleep_preparing;
    restart_pending = s_system_sleep_restart_pending;
    if (s_initialized && !s_registry_retirement_failed &&
        (!preparing || system_sleep_resume) && !restart_pending) {
        ++s_system_sleep_start_users;
        admitted = true;
    }
    host = s_host;
    taskEXIT_CRITICAL(&s_lock);
    if (!admitted) {
        if (restart_pending) ESP_LOGW(TAG, "start deferred until retiring generation unregisters");
        return;
    }

    host.ensure_ambient_clock(host.context);
    bool sntp_initialized;
    bool task_exists;
    bool create_monitor = false;
    taskENTER_CRITICAL(&s_lock);
    sntp_initialized = s_sntp_initialized;
    task_exists = s_task != NULL || s_starting;
    if (!task_exists) {
        s_starting = true;
        create_monitor = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    esp_err_t err = ESP_OK;
    if (!sntp_initialized) {
        esp_sntp_config_t config = ESP_NETIF_SNTP_DEFAULT_CONFIG_MULTIPLE(
            3, ESP_SNTP_SERVER_LIST("ntp.aliyun.com", "time.cloudflare.com", "pool.ntp.org"));
        config.sync_cb = sntp_sync_cb;
        err = esp_netif_sntp_init(&config);
        if (err == ESP_OK) {
            taskENTER_CRITICAL(&s_lock);
            s_sntp_initialized = true;
            taskEXIT_CRITICAL(&s_lock);
        } else {
            ESP_LOGW(TAG, "SNTP init failed: %s", esp_err_to_name(err));
        }
    } else if (!system_sleep_resume) {
        ESP_LOGI(TAG, "SNTP service already initialized");
        taskENTER_CRITICAL(&s_lock);
        if (create_monitor) s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
        goto done;
    }
    if (err == ESP_OK && create_monitor) {
        taskENTER_CRITICAL(&s_lock);
        s_stop_requested = false;
        s_exit_status = ESP_OK;
        s_registry_retirement_failed = false;
        taskEXIT_CRITICAL(&s_lock);
        while (xSemaphoreTake(s_stopped, 0) == pdTRUE) {}
        TaskHandle_t task = NULL;
        BaseType_t created = xTaskCreateWithCaps(clock_sync_task, "maclaw_clock_sync",
                                                 3072, NULL, 3, &task,
                                                 MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (created != pdPASS) {
            taskENTER_CRITICAL(&s_lock);
            s_starting = false;
            taskEXIT_CRITICAL(&s_lock);
            ESP_LOGE(TAG, "cannot start clock sync monitor task");
        } else {
            taskENTER_CRITICAL(&s_lock);
            s_task = task;
            s_starting = false;
            taskEXIT_CRITICAL(&s_lock);
            const esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
                .struct_size = sizeof(task_registry_entry_t),
                .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
                .name = "clock_sync",
                .context = (void *)task,
                .stop = stop_registry_entry,
            });
            if (registry_err != ESP_OK) {
                ESP_LOGE(TAG, "cannot register clock sync monitor: %s",
                         esp_err_to_name(registry_err));
                xSemaphoreGive(s_start_gate);
                (void)stop_monitor(500);
            } else {
                xSemaphoreGive(s_start_gate);
            }
        }
    } else if (err != ESP_OK && create_monitor) {
        taskENTER_CRITICAL(&s_lock);
        s_starting = false;
        taskEXIT_CRITICAL(&s_lock);
    }
done:
    taskENTER_CRITICAL(&s_lock);
    if (s_system_sleep_start_users) --s_system_sleep_start_users;
    taskEXIT_CRITICAL(&s_lock);
}

device_status_t clock_sync_service_start(bool system_sleep_resume) {
    taskENTER_CRITICAL(&s_lock);
    const bool initialized = s_initialized;
    const bool preparing = s_system_sleep_preparing;
    const bool pending = s_system_sleep_restart_pending;
    taskEXIT_CRITICAL(&s_lock);
    if (!initialized) return DEVICE_STATUS_UNAVAILABLE;
    if (pending || s_registry_retirement_failed ||
        (preparing && !system_sleep_resume)) return DEVICE_STATUS_BUSY;
    start_internal(system_sleep_resume);
    return DEVICE_STATUS_OK;
}

device_status_t clock_sync_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    bool was_initialized;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (s_system_sleep_preparing || s_system_sleep_start_users ||
        s_system_sleep_restart_pending) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_was_initialized = s_sntp_initialized;
    s_system_sleep_preparing = true;
    was_initialized = s_system_sleep_was_initialized;
    taskEXIT_CRITICAL(&s_lock);

    for (;;) {
        taskENTER_CRITICAL(&s_lock);
        const bool drained = s_system_sleep_callback_users == 0;
        taskEXIT_CRITICAL(&s_lock);
        if (drained) break;
        if (remaining_ms(deadline_us) == 0u) return DEVICE_STATUS_TIMEOUT;
        vTaskDelay(1);
    }
    if (!was_initialized) return DEVICE_STATUS_OK;
    /* This preserves the historical clock_sync_service_stop(remaining)
     * PREPARE seam while retaining the original parent deadline instead of
     * starting a fresh child budget. */
    return remaining_ms(deadline_us) ? stop_until_deadline(deadline_us)
                                    : DEVICE_STATUS_TIMEOUT;
}

void clock_sync_service_abort_system_sleep_prepare(void) {
    bool restart = false;
    taskENTER_CRITICAL(&s_lock);
    if (!s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_lock);
        return;
    }
    restart = s_system_sleep_was_initialized && !s_registry_retirement_failed;
    s_system_sleep_was_initialized = false;
    s_system_sleep_preparing = false;
    if (restart) s_stop_requested = false;
    if (restart && s_retiring) {
        s_system_sleep_restart_pending = true;
        restart = false;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (restart) start_internal(true);
}

device_status_t clock_sync_service_init(const clock_sync_service_host_t *host) {
    if (!host || host->struct_size != sizeof(*host) ||
        !host->ensure_ambient_clock || !host->note_wall_clock ||
        !host->notify_wall_clock_updated) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) {
        const bool same_host = memcmp(&s_host, host, sizeof(*host)) == 0;
        taskEXIT_CRITICAL(&s_lock);
        return same_host ? DEVICE_STATUS_OK : DEVICE_STATUS_BUSY;
    }
    taskEXIT_CRITICAL(&s_lock);
    SemaphoreHandle_t start_gate = xSemaphoreCreateBinary();
    SemaphoreHandle_t stopped = xSemaphoreCreateBinary();
    if (!start_gate || !stopped) {
        if (start_gate) vSemaphoreDelete(start_gate);
        if (stopped) vSemaphoreDelete(stopped);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    taskENTER_CRITICAL(&s_lock);
    s_host = *host;
    trusted_time_policy_state_init(&s_time_state);
    s_start_gate = start_gate;
    s_stopped = stopped;
    s_initialized = true;
    s_exit_status = ESP_OK;
    s_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}
