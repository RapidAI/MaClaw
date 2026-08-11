#include "power_service.h"

#include "platform_power.h"
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
static StaticSemaphore_t s_deinit_mutex_storage;
/* Serializes a deadline cancellation/rearm with the final physical DISPLAY_OFF
 * commit.  It is intentionally a task mutex, never a critical section: the
 * commit takes the board display mutex and may wait for an in-flight DMA. */
static SemaphoreHandle_t s_transition_mutex;
/* Transition ownership intentionally excludes the whole deinit transaction:
 * the timer callback must be able to finish or observe admission closed. This
 * separate static lock prevents two teardown callers from deleting the same
 * ESP timer or having one reopen the service while the other is still joining.
 */
static SemaphoreHandle_t s_deinit_mutex;
/* esp_timer owns a deliberately small service stack.  A DISPLAY_OFF commit is
 * not timer-safe: it may wait for an LCD DMA owner and issue profile-private
 * panel/backlight transactions.  The timer therefore only admits one event;
 * this ordinary task owns the actual Platform -> board transition. */
static TaskHandle_t s_display_off_worker_task;
static SemaphoreHandle_t s_display_off_worker_stopped;
/* `esp_timer_stop()` only blocks future alarm delivery. The callback may
 * already have admitted a worker notification, so lifecycle stop tracks that
 * tiny timer-service lease before deleting the timer or its worker boundary. */
static SemaphoreHandle_t s_display_off_timer_callback_drained;
static uint32_t s_display_off_timer_callbacks_inflight;
static bool s_display_off_timer_callback_admission_open;
static portMUX_TYPE s_display_off_timer_callback_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_initialized;
static bool s_initializing;
static bool s_stopping;
static bool s_display_off_armed;

/* All public/scheduler transition paths may serialize with a panel DMA or a
 * foreground wake.  They must not, however, turn a transient display lock
 * into an unbounded App Interaction, schedule, or esp_timer callback stall.
 * On expiry callers retain the safe state: no new DISPLAY_OFF commit is made
 * and a physical contact is not reinterpreted as a business action. */
#define POWER_TRANSITION_LOCK_TIMEOUT_MS 1500u

static TickType_t power_transition_lock_timeout(void) {
    return pdMS_TO_TICKS(POWER_TRANSITION_LOCK_TIMEOUT_MS);
}

/* A service stop is one transaction even though it has to acquire two locks
 * and join a worker.  Every step must consume the caller's original budget;
 * otherwise a transition lock held until the end of `timeout_ms` followed by
 * a worker join given another full `timeout_ms` doubles the shutdown time. */
static TickType_t power_stop_timeout_ticks(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}

static TickType_t power_stop_remaining_ticks(TickType_t started,
                                             TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}

/* Init and deinit may be requested by different composition-root paths (for
 * example a late startup rollback racing a failed readiness transition).  The
 * static teardown mutex is intentionally retained across generations, but it
 * is first published only after the initializer has acquired it.  Until then
 * a stopper must observe `s_initializing` rather than incorrectly concluding
 * that there is no Power Service to stop. */
static SemaphoreHandle_t power_snapshot_deinit_mutex(bool *out_initializing) {
    SemaphoreHandle_t mutex;
    taskENTER_CRITICAL(&s_power_lock);
    mutex = s_deinit_mutex;
    if (out_initializing) *out_initializing = s_initializing;
    taskEXIT_CRITICAL(&s_power_lock);
    return mutex;
}

static bool display_off_timer_callback_enter(void) {
    bool entered = false;
    taskENTER_CRITICAL(&s_display_off_timer_callback_lock);
    if (s_display_off_timer_callback_admission_open) {
        ++s_display_off_timer_callbacks_inflight;
        entered = true;
    }
    taskEXIT_CRITICAL(&s_display_off_timer_callback_lock);
    return entered;
}

static void display_off_timer_callback_leave(void) {
    taskENTER_CRITICAL(&s_display_off_timer_callback_lock);
    if (s_display_off_timer_callbacks_inflight > 0) {
        /* Give the final-drain token before publishing zero. Deinit may free
         * the semaphore as soon as it observes no callback lease, so a late
         * give after the decrement would be a callback-after-free race. */
        if (!s_display_off_timer_callback_admission_open &&
            s_display_off_timer_callbacks_inflight == 1 &&
            s_display_off_timer_callback_drained) {
            xSemaphoreGive(s_display_off_timer_callback_drained);
        }
        --s_display_off_timer_callbacks_inflight;
    }
    taskEXIT_CRITICAL(&s_display_off_timer_callback_lock);
}

/* Caller owns s_transition_mutex.  A caller may have sampled a former timer
 * handle before deinit closed admission, then waited behind the callback.  It
 * must revalidate under this lock before issuing any esp_timer/board call. */
static bool transition_is_current_locked(esp_timer_handle_t timer) {
    taskENTER_CRITICAL(&s_power_lock);
    const bool current = s_initialized && !s_stopping &&
                         timer != NULL && timer == s_display_off_timer;
    taskEXIT_CRITICAL(&s_power_lock);
    return current;
}

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

static void handle_display_off_deadline(void) {
    if (!s_transition_mutex ||
        xSemaphoreTake(s_transition_mutex, power_transition_lock_timeout()) != pdTRUE) {
        /* The timer is one-shot.  Do not retry after a
         * contention timeout: a later user/UI transition will re-arm an
         * eligible deadline, while an implicit retry could blank a newer
         * foreground scene. */
        ESP_LOGW(TAG, "idle deadline dropped: display transition remained busy");
        return;
    }
    /* A deadline notification can be queued just before lifecycle teardown.
     * Revalidate after acquiring the same transition mutex used by deinit so a
     * worker which was waiting behind foreground display work cannot commit a
     * physical DISPLAY_OFF after its service generation was closed. */
    taskENTER_CRITICAL(&s_power_lock);
    const bool current = s_initialized && !s_stopping &&
                         s_display_off_timer != NULL;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!current) {
        xSemaphoreGive(s_transition_mutex);
        return;
    }
    if (!power_lease_service_allows_display_off()) {
        /* Keep the idle request live while a foreground operation owns the
         * screen.  Releasing the final lease does not have to know which UI
         * timer originally armed this deadline, and a schedule-owned window
         * still converges to DISPLAY_OFF without an unrelated repaint. */
        taskENTER_CRITICAL(&s_power_lock);
        bool can_rearm = s_initialized && !s_stopping &&
                         s_display_off_timer != NULL;
        s_display_off_armed = can_rearm;
        taskEXIT_CRITICAL(&s_power_lock);
        if (!can_rearm || esp_timer_start_once(s_display_off_timer, 1000000) != ESP_OK) {
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
    if (platform_power_enter_display_off()) {
        ESP_LOGI(TAG, "idle deadline reached: DISPLAY_OFF entered");
    } else {
        ESP_LOGD(TAG, "idle deadline ignored: display is no longer eligible");
    }
    xSemaphoreGive(s_transition_mutex);
}

static void display_off_worker_task(void *arg) {
    (void)arg;
    while (true) {
        (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
        taskENTER_CRITICAL(&s_power_lock);
        const bool stopping = s_stopping;
        /* A notification is not itself proof that a deadline still belongs to
         * this generation: cancel/rearm may have happened between the timer
         * callback and this worker getting CPU time. Consume the armed bit
         * exactly once here, under the same lock used by every cancellation. */
        const bool armed = s_initialized && s_display_off_armed;
        if (armed) s_display_off_armed = false;
        taskEXIT_CRITICAL(&s_power_lock);
        if (stopping) break;
        if (armed) handle_display_off_deadline();
    }

    taskENTER_CRITICAL(&s_power_lock);
    if (s_display_off_worker_task == xTaskGetCurrentTaskHandle()) {
        s_display_off_worker_task = NULL;
    }
    SemaphoreHandle_t stopped = s_display_off_worker_stopped;
    taskEXIT_CRITICAL(&s_power_lock);
    if (stopped) xSemaphoreGive(stopped);
    vTaskDelete(NULL);
}

static void display_off_timer_callback(void *arg) {
    (void)arg;
    /* Never descend into Platform/board code here.  Besides avoiding an
     * esp_timer stack overflow, this preserves the Power Service contract that
     * physical display transitions are serialized by a normal task context. */
    if (!display_off_timer_callback_enter()) return;
    taskENTER_CRITICAL(&s_power_lock);
    TaskHandle_t worker = (!s_stopping && s_initialized)
                              ? s_display_off_worker_task
                              : NULL;
    taskEXIT_CRITICAL(&s_power_lock);
    if (worker) xTaskNotifyGive(worker);
    display_off_timer_callback_leave();
}

device_status_t power_service_init(void) {
    SemaphoreHandle_t transition_mutex;
    SemaphoreHandle_t deinit_mutex;
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    bool initializing = s_initializing;
    bool stopping = s_stopping;
    transition_mutex = s_transition_mutex;
    deinit_mutex = s_deinit_mutex;
    if (!initialized && !initializing) s_initializing = true;
    taskEXIT_CRITICAL(&s_power_lock);
    if (initialized) return DEVICE_STATUS_OK;
    if (initializing) return DEVICE_STATUS_BUSY;
    if (stopping) {
        taskENTER_CRITICAL(&s_power_lock);
        s_initializing = false;
        taskEXIT_CRITICAL(&s_power_lock);
        return DEVICE_STATUS_BUSY;
    }

    if (!transition_mutex) {
        transition_mutex = xSemaphoreCreateMutexStatic(&s_transition_mutex_storage);
    }
    if (!deinit_mutex) {
        deinit_mutex = xSemaphoreCreateMutexStatic(&s_deinit_mutex_storage);
    }
    if (!transition_mutex || !deinit_mutex) {
        taskENTER_CRITICAL(&s_power_lock);
        s_initializing = false;
        taskEXIT_CRITICAL(&s_power_lock);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    if (xSemaphoreTake(deinit_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        taskENTER_CRITICAL(&s_power_lock);
        s_initializing = false;
        taskEXIT_CRITICAL(&s_power_lock);
        return DEVICE_STATUS_TIMEOUT;
    }
    /* Publish only after the initializer owns the mutex. A concurrent stop
     * that sees this handle now blocks behind this init transaction, rather
     * than observing a half-created timer/worker as a completed service. */
    taskENTER_CRITICAL(&s_power_lock);
    s_transition_mutex = transition_mutex;
    s_deinit_mutex = deinit_mutex;
    taskEXIT_CRITICAL(&s_power_lock);
    taskENTER_CRITICAL(&s_power_lock);
    const bool still_startable = !s_initialized && !s_stopping;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!still_startable) {
        xSemaphoreGive(deinit_mutex);
        taskENTER_CRITICAL(&s_power_lock);
        s_initializing = false;
        taskEXIT_CRITICAL(&s_power_lock);
        return DEVICE_STATUS_BUSY;
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
        xSemaphoreGive(deinit_mutex);
        return status_from_esp_err(err);
    }

    SemaphoreHandle_t worker_stopped = xSemaphoreCreateBinary();
    if (!worker_stopped) {
        (void)esp_timer_delete(timer);
        taskENTER_CRITICAL(&s_power_lock);
        s_initializing = false;
        taskEXIT_CRITICAL(&s_power_lock);
        xSemaphoreGive(deinit_mutex);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    SemaphoreHandle_t timer_callback_drained = xSemaphoreCreateBinary();
    if (!timer_callback_drained) {
        vSemaphoreDelete(worker_stopped);
        (void)esp_timer_delete(timer);
        taskENTER_CRITICAL(&s_power_lock);
        s_initializing = false;
        taskEXIT_CRITICAL(&s_power_lock);
        xSemaphoreGive(deinit_mutex);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    TaskHandle_t worker = NULL;
    if (xTaskCreate(display_off_worker_task, "maclaw_power_evt", 4096, NULL,
                    3, &worker) != pdPASS) {
        vSemaphoreDelete(timer_callback_drained);
        vSemaphoreDelete(worker_stopped);
        (void)esp_timer_delete(timer);
        taskENTER_CRITICAL(&s_power_lock);
        s_initializing = false;
        taskEXIT_CRITICAL(&s_power_lock);
        xSemaphoreGive(deinit_mutex);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }

    taskENTER_CRITICAL(&s_power_lock);
    s_transition_mutex = transition_mutex;
    s_deinit_mutex = deinit_mutex;
    s_display_off_timer = timer;
    s_display_off_worker_task = worker;
    s_display_off_worker_stopped = worker_stopped;
    s_display_off_timer_callback_drained = timer_callback_drained;
    taskENTER_CRITICAL(&s_display_off_timer_callback_lock);
    s_display_off_timer_callbacks_inflight = 0;
    s_display_off_timer_callback_admission_open = true;
    taskEXIT_CRITICAL(&s_display_off_timer_callback_lock);
    s_initialized = true;
    s_initializing = false;
    s_stopping = false;
    timer = NULL;
    taskEXIT_CRITICAL(&s_power_lock);
    xSemaphoreGive(deinit_mutex);
    ESP_LOGI(TAG, "power service ready: DISPLAY_OFF scheduling only");
    return DEVICE_STATUS_OK;
}

device_status_t power_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = power_stop_timeout_ticks(timeout_ms);
    SemaphoreHandle_t deinit_mutex = NULL;
    bool initialized = false;
    bool stopping = false;
    esp_timer_handle_t timer = NULL;
    SemaphoreHandle_t transition_mutex = NULL;
    TaskHandle_t worker = NULL;
    SemaphoreHandle_t worker_stopped = NULL;
    SemaphoreHandle_t timer_callback_drained = NULL;
    TickType_t remaining;

    /* A newly-created static mutex can be invisible for a few instructions
     * while init is in flight. Wait for that transaction rather than returning
     * success and allowing Device Power to close its lease domain underneath a
     * subsequently published scheduler. If an older static mutex is already
     * visible, a stopper can win the mutex race; in that case retry until the
     * initializer has either completed or consumed the same parent budget. */
    for (;;) {
        bool initializing = false;
        deinit_mutex = power_snapshot_deinit_mutex(&initializing);
        if (!deinit_mutex) {
            if (!initializing) return DEVICE_STATUS_OK;
            remaining = power_stop_remaining_ticks(started, budget);
            if (remaining == 0) return DEVICE_STATUS_TIMEOUT;
            vTaskDelay(1);
            continue;
        }
        remaining = power_stop_remaining_ticks(started, budget);
        if (remaining == 0 || xSemaphoreTake(deinit_mutex, remaining) != pdTRUE) {
            return DEVICE_STATUS_TIMEOUT;
        }
        taskENTER_CRITICAL(&s_power_lock);
        initialized = s_initialized;
        stopping = s_stopping;
        initializing = s_initializing;
        if (initializing && !initialized) {
            taskEXIT_CRITICAL(&s_power_lock);
            xSemaphoreGive(deinit_mutex);
            remaining = power_stop_remaining_ticks(started, budget);
            if (remaining == 0) return DEVICE_STATUS_TIMEOUT;
            vTaskDelay(1);
            continue;
        }
        timer = s_display_off_timer;
        transition_mutex = s_transition_mutex;
        worker = s_display_off_worker_task;
        worker_stopped = s_display_off_worker_stopped;
        timer_callback_drained = s_display_off_timer_callback_drained;
        s_stopping = true;
        s_initialized = false;
        s_display_off_armed = false;
        taskEXIT_CRITICAL(&s_power_lock);
        break;
    }
    if (!initialized && !stopping) {
        taskENTER_CRITICAL(&s_power_lock);
        s_stopping = false;
        taskEXIT_CRITICAL(&s_power_lock);
        xSemaphoreGive(deinit_mutex);
        return DEVICE_STATUS_OK;
    }
    if (!timer || !transition_mutex) {
        xSemaphoreGive(deinit_mutex);
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    /* Stop first, then take the same mutex used by the callback.  Either a
     * callback has already finished its board transition, or it observes the
     * stopped state after we release this mutex. */
    (void)esp_timer_stop(timer);
    bool timer_callback_drained_now = false;
    taskENTER_CRITICAL(&s_display_off_timer_callback_lock);
    s_display_off_timer_callback_admission_open = false;
    timer_callback_drained_now = s_display_off_timer_callbacks_inflight == 0;
    taskEXIT_CRITICAL(&s_display_off_timer_callback_lock);
    if (!timer_callback_drained_now) {
        remaining = power_stop_remaining_ticks(started, budget);
        if (!timer_callback_drained || remaining == 0 ||
            xSemaphoreTake(timer_callback_drained, remaining) != pdTRUE) {
            xSemaphoreGive(deinit_mutex);
            return DEVICE_STATUS_TIMEOUT;
        }
    }
    remaining = power_stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(transition_mutex, remaining) != pdTRUE) {
        xSemaphoreGive(deinit_mutex);
        return DEVICE_STATUS_TIMEOUT;
    }
    (void)esp_timer_stop(timer);
    xSemaphoreGive(transition_mutex);
    esp_err_t delete_err = esp_timer_delete(timer);
    if (delete_err != ESP_OK) {
        xSemaphoreGive(deinit_mutex);
        return status_from_esp_err(delete_err);
    }
    /* `esp_timer_delete()` closes future callback dispatch.  Wake the normal
     * worker only after that boundary, then join it before clearing the task
     * handle so no late callback can notify a recycled FreeRTOS handle. */
    if (worker) xTaskNotifyGive(worker);
    remaining = power_stop_remaining_ticks(started, budget);
    if (worker && (!worker_stopped || remaining == 0 ||
                   xSemaphoreTake(worker_stopped, remaining) != pdTRUE)) {
        xSemaphoreGive(deinit_mutex);
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_power_lock);
    s_display_off_timer = NULL;
    s_display_off_worker_task = NULL;
    s_display_off_worker_stopped = NULL;
    s_display_off_timer_callback_drained = NULL;
    /* The transition mutex has static storage and remains valid. Retaining it
     * ensures a late caller only sees `initialized=false`, never a freed
     * synchronization object. */
    s_stopping = false;
    taskEXIT_CRITICAL(&s_power_lock);
    if (worker_stopped) vSemaphoreDelete(worker_stopped);
    if (timer_callback_drained) vSemaphoreDelete(timer_callback_drained);
    xSemaphoreGive(deinit_mutex);
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
    if (xSemaphoreTake(transition_mutex, power_transition_lock_timeout()) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }

    if (!transition_is_current_locked(timer)) {
        xSemaphoreGive(transition_mutex);
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
    if (xSemaphoreTake(transition_mutex, power_transition_lock_timeout()) != pdTRUE) {
        ESP_LOGW(TAG, "cannot cancel DISPLAY_OFF deadline: transition busy");
        return;
    }
    if (!transition_is_current_locked(timer)) {
        xSemaphoreGive(transition_mutex);
        return;
    }
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
    if (xSemaphoreTake(transition_mutex, power_transition_lock_timeout()) != pdTRUE) {
        ESP_LOGW(TAG, "cannot wake DISPLAY_OFF panel from user contact: transition busy");
        return false;
    }
    if (!transition_is_current_locked(timer)) {
        xSemaphoreGive(transition_mutex);
        return false;
    }
    /* Do not release this mutex between disarming the deadline and restoring
     * the physical panel.  The timer callback takes the same lock, so either
     * it wins and the contact wakes the resulting DISPLAY_OFF state, or the
     * contact wins and the now-stale timer observes an unarmed deadline. */
    disarm_display_off_locked(timer);
    bool woke = platform_power_wake_display();
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
    if (xSemaphoreTake(transition_mutex, power_transition_lock_timeout()) != pdTRUE) {
        ESP_LOGW(TAG, "cannot wake DISPLAY_OFF panel from domain deadline: transition busy");
        return false;
    }
    if (!transition_is_current_locked(timer)) {
        xSemaphoreGive(transition_mutex);
        return false;
    }
    disarm_display_off_locked(timer);
    bool woke = platform_power_wake_display();
    xSemaphoreGive(transition_mutex);
    return woke;
}

bool power_service_wake_display_from_remote_control(void) {
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    esp_timer_handle_t timer = s_display_off_timer;
    SemaphoreHandle_t transition_mutex = s_transition_mutex;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!initialized || !timer || !transition_mutex) return false;
    if (xSemaphoreTake(transition_mutex, power_transition_lock_timeout()) != pdTRUE) {
        ESP_LOGW(TAG, "cannot wake DISPLAY_OFF panel from remote control: transition busy");
        return false;
    }
    if (!transition_is_current_locked(timer)) {
        xSemaphoreGive(transition_mutex);
        return false;
    }
    /* Unlike a physical contact, a remote brightness update is not activity.
     * Do not consume an active panel's pending idle deadline merely because
     * the management plane changed its brightness. */
    if (!platform_power_display_is_off()) {
        xSemaphoreGive(transition_mutex);
        return false;
    }
    /* Remote display management has the same panel/timer atomicity as a
     * physical wake, but deliberately carries no input or schedule override
     * semantics.  The UI resumes the normal ambient deadline after success. */
    disarm_display_off_locked(timer);
    bool woke = platform_power_wake_display();
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
    out_snapshot->state = platform_power_display_is_off()
                            ? DEVICE_POWER_STATE_DISPLAY_OFF
                            : DEVICE_POWER_STATE_ACTIVE;
    return initialized;
}
