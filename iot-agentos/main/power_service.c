#include "power_service.h"

#include "board_port.h"
#include "power_lease_service.h"
#include "esp_err.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

static const char *TAG = "maclaw_power";
static esp_timer_handle_t s_display_off_timer;
static portMUX_TYPE s_power_lock = portMUX_INITIALIZER_UNLOCKED;
static StaticSemaphore_t s_transition_mutex_storage;
/* Serializes a deadline cancellation/rearm with the final physical DISPLAY_OFF
 * commit.  It is intentionally a task mutex, never a critical section: the
 * commit takes the board display mutex and may wait for an in-flight DMA. */
static SemaphoreHandle_t s_transition_mutex;
static bool s_initialized;
static bool s_initializing;
static bool s_stopping;
static bool s_display_off_armed;

/* The caller holds s_transition_mutex.  Keeping timer cancellation and the
 * following physical transition in one critical transaction prevents the
 * queued idle callback from turning the panel off just after a real user wake
 * has restored it. */
static void disarm_display_off_locked(esp_timer_handle_t timer) {
    taskENTER_CRITICAL(&s_power_lock);
    s_display_off_armed = false;
    taskEXIT_CRITICAL(&s_power_lock);
    (void)esp_timer_stop(timer);
}

static device_status_t status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

static void display_off_timer_callback(void *arg) {
    (void)arg;
    if (!s_transition_mutex ||
        xSemaphoreTake(s_transition_mutex, portMAX_DELAY) != pdTRUE) {
        return;
    }
    taskENTER_CRITICAL(&s_power_lock);
    bool armed = s_initialized && !s_stopping && s_display_off_armed;
    s_display_off_armed = false;
    taskEXIT_CRITICAL(&s_power_lock);
    /* A concurrent foreground transition may have cancelled this deadline
     * after the timer callback was queued.  The transition mutex is still
     * owned on that path; release it before treating the callback as stale,
     * otherwise every later schedule/cancel/wake request can deadlock. */
    if (!armed) {
        xSemaphoreGive(s_transition_mutex);
        return;
    }

    if (!power_lease_service_allows_display_off()) {
        /* Keep the idle request live while a foreground operation owns the
         * screen.  Releasing the final lease does not have to know which UI
         * timer originally armed this deadline, and a schedule-owned window
         * still converges to DISPLAY_OFF without an unrelated repaint. */
        taskENTER_CRITICAL(&s_power_lock);
        s_display_off_armed = true;
        taskEXIT_CRITICAL(&s_power_lock);
        if (esp_timer_start_once(s_display_off_timer, 1000000) != ESP_OK) {
            taskENTER_CRITICAL(&s_power_lock);
            s_display_off_armed = false;
            taskEXIT_CRITICAL(&s_power_lock);
            ESP_LOGW(TAG, "cannot defer idle deadline while power lease is active");
        } else {
            ESP_LOGD(TAG, "idle deadline deferred: foreground power lease active");
        }
        xSemaphoreGive(s_transition_mutex);
        return;
    }
    /* The adapter rechecks that its current scene is an eligible ambient
     * scene before committing the physical transaction. That closes the race
     * where a foreground UI transition and a timer deadline cross. */
    if (board_port_enter_display_off()) {
        ESP_LOGI(TAG, "idle deadline reached: DISPLAY_OFF entered");
    } else {
        ESP_LOGD(TAG, "idle deadline ignored: display is no longer eligible");
    }
    xSemaphoreGive(s_transition_mutex);
}

device_status_t power_service_init(void) {
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    bool initializing = s_initializing;
    if (!initialized && !initializing) s_initializing = true;
    taskEXIT_CRITICAL(&s_power_lock);
    if (initialized) return DEVICE_STATUS_OK;
    if (initializing) return DEVICE_STATUS_BUSY;

    SemaphoreHandle_t transition_mutex =
        xSemaphoreCreateMutexStatic(&s_transition_mutex_storage);
    if (!transition_mutex) {
        taskENTER_CRITICAL(&s_power_lock);
        s_initializing = false;
        taskEXIT_CRITICAL(&s_power_lock);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }

    esp_timer_create_args_t timer_args = {
        .callback = display_off_timer_callback,
        .name = "maclaw_display_off",
    };
    esp_timer_handle_t timer = NULL;
    esp_err_t err = esp_timer_create(&timer_args, &timer);
    if (err != ESP_OK) {
        taskENTER_CRITICAL(&s_power_lock);
        s_initializing = false;
        taskEXIT_CRITICAL(&s_power_lock);
        return status_from_esp_err(err);
    }

    taskENTER_CRITICAL(&s_power_lock);
    s_transition_mutex = transition_mutex;
    s_display_off_timer = timer;
    s_initialized = true;
    s_initializing = false;
    s_stopping = false;
    timer = NULL;
    taskEXIT_CRITICAL(&s_power_lock);
    ESP_LOGI(TAG, "power service ready: DISPLAY_OFF scheduling only");
    return DEVICE_STATUS_OK;
}

device_status_t power_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    esp_timer_handle_t timer = s_display_off_timer;
    SemaphoreHandle_t transition_mutex = s_transition_mutex;
    s_stopping = true;
    s_initialized = false;
    s_display_off_armed = false;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!initialized) {
        taskENTER_CRITICAL(&s_power_lock);
        s_stopping = false;
        taskEXIT_CRITICAL(&s_power_lock);
        return DEVICE_STATUS_OK;
    }
    if (!timer || !transition_mutex) return DEVICE_STATUS_INTERNAL_ERROR;
    /* Stop first, then take the same mutex used by the callback.  Either a
     * callback has already finished its board transition, or it observes the
     * stopped state after we release this mutex. */
    (void)esp_timer_stop(timer);
    if (xSemaphoreTake(transition_mutex, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    (void)esp_timer_stop(timer);
    xSemaphoreGive(transition_mutex);
    esp_err_t delete_err = esp_timer_delete(timer);
    if (delete_err != ESP_OK) return status_from_esp_err(delete_err);
    taskENTER_CRITICAL(&s_power_lock);
    s_display_off_timer = NULL;
    /* The transition mutex has static storage and remains valid. Retaining it
     * ensures a late caller only sees `initialized=false`, never a freed
     * synchronization object. */
    s_stopping = false;
    taskEXIT_CRITICAL(&s_power_lock);
    ESP_LOGI(TAG, "power service stopped");
    return DEVICE_STATUS_OK;
}

device_status_t power_service_schedule_display_off(uint32_t idle_after_ms) {
    if (idle_after_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    esp_timer_handle_t timer = s_display_off_timer;
    SemaphoreHandle_t transition_mutex = s_transition_mutex;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!initialized || !timer || !transition_mutex) return DEVICE_STATUS_BUSY;
    if (xSemaphoreTake(transition_mutex, portMAX_DELAY) != pdTRUE) {
        return DEVICE_STATUS_BUSY;
    }

    (void)esp_timer_stop(timer);
    taskENTER_CRITICAL(&s_power_lock);
    s_display_off_armed = true;
    taskEXIT_CRITICAL(&s_power_lock);
    esp_err_t err = esp_timer_start_once(timer,
                                         (uint64_t)idle_after_ms * 1000u);
    if (err != ESP_OK) {
        taskENTER_CRITICAL(&s_power_lock);
        s_display_off_armed = false;
        taskEXIT_CRITICAL(&s_power_lock);
        xSemaphoreGive(transition_mutex);
        return status_from_esp_err(err);
    }
    xSemaphoreGive(transition_mutex);
    return DEVICE_STATUS_OK;
}

void power_service_cancel_display_off(void) {
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    esp_timer_handle_t timer = s_display_off_timer;
    SemaphoreHandle_t transition_mutex = s_transition_mutex;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!initialized || !timer || !transition_mutex) return;
    if (xSemaphoreTake(transition_mutex, portMAX_DELAY) != pdTRUE) return;
    disarm_display_off_locked(timer);
    xSemaphoreGive(transition_mutex);
}

bool power_service_wake_display_from_user(void) {
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    esp_timer_handle_t timer = s_display_off_timer;
    SemaphoreHandle_t transition_mutex = s_transition_mutex;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!initialized || !timer || !transition_mutex) return false;
    if (xSemaphoreTake(transition_mutex, portMAX_DELAY) != pdTRUE) return false;
    /* Do not release this mutex between disarming the deadline and restoring
     * the physical panel.  The timer callback takes the same lock, so either
     * it wins and the contact wakes the resulting DISPLAY_OFF state, or the
     * contact wins and the now-stale timer observes an unarmed deadline. */
    disarm_display_off_locked(timer);
    bool woke = board_port_wake_from_idle();
    xSemaphoreGive(transition_mutex);
    return woke;
}

bool power_service_wake_display_from_schedule(void) {
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    esp_timer_handle_t timer = s_display_off_timer;
    SemaphoreHandle_t transition_mutex = s_transition_mutex;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!initialized || !timer || !transition_mutex) return false;
    if (xSemaphoreTake(transition_mutex, portMAX_DELAY) != pdTRUE) return false;
    disarm_display_off_locked(timer);
    bool woke = board_port_wake_from_idle();
    xSemaphoreGive(transition_mutex);
    return woke;
}

bool power_service_get_snapshot(device_power_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    out_snapshot->display_off_armed = s_display_off_armed;
    taskEXIT_CRITICAL(&s_power_lock);
    /* The board renderer can legitimately wake the physical panel to present
     * an urgent scene.  Ask the adapter for its observed state instead of
     * replaying the last Power Service transition as if it were authoritative. */
    out_snapshot->state = board_port_display_is_off()
                            ? DEVICE_POWER_STATE_DISPLAY_OFF
                            : DEVICE_POWER_STATE_ACTIVE;
    return initialized;
}
