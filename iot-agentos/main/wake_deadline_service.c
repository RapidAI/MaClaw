#include "wake_deadline_service.h"

#include <stdbool.h>
#include <string.h>
#include <sys/time.h>

#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "wake_deadline_sleep_gate.h"

#define WAKE_DEADLINE_MAX_SLOTS 8u
#define WAKE_DEADLINE_MIN_TIMER_US 1000u
#define WAKE_DEADLINE_TRUSTED_EPOCH_MS 1672531200000LL /* 2023-01-01 UTC */

typedef struct {
    uint16_t generation;
    bool registered;
    bool armed;
    /* A callback is copied out of s_lock before it runs.  Keep the slot
     * unavailable until that invocation returns so unregister can establish a
     * real callback/state ownership boundary instead of merely clearing a
     * function pointer. */
    bool callback_inflight;
    int64_t epoch_ms;
    wake_deadline_callback_t callback;
    void *arg;
} deadline_slot_t;

static const char *TAG = "wake_deadline";
static SemaphoreHandle_t s_lock;
/* The deadline API is called by Alarm/Sleep/Clock tasks that may already have
 * sampled service state when a rollback begins.  Keep this lock in permanent
 * static storage: deleting a mutex beneath such a waiter is a FreeRTOS UAF. */
static StaticSemaphore_t s_lock_storage;
/* Serializes the whole join/reclaim transaction.  The public admission mutex
 * is intentionally released while waiting for the worker, so it alone cannot
 * stop a second teardown caller from consuming the same completion signal. */
static SemaphoreHandle_t s_deinit_lock;
static StaticSemaphore_t s_deinit_lock_storage;
static TaskHandle_t s_task;
static esp_timer_handle_t s_timer;
static SemaphoreHandle_t s_stopped;
static deadline_slot_t s_slots[WAKE_DEADLINE_MAX_SLOTS];
static volatile bool s_initialized;
static volatile bool s_stop_requested;
/* This is an explicit trust assertion from Clock Sync, not a heuristic based
 * on the calendar value returned by gettimeofday().  It is boot-local on
 * purpose: a retained RTC value after reset is untrusted until an admitted
 * authenticated Hub or SNTP sample is applied in this boot. */
static volatile bool s_wall_clock_trusted;
/* System Sleep retains the dispatcher and every client slot; it only closes
 * physical timer delivery/callback selection until a parent Power rollback
 * decides whether those logical deadlines may resume. */
static volatile bool s_system_sleep_preparing;
static uint32_t s_system_sleep_callbacks_inflight;
/* `esp_timer_stop()` only rejects future alarms.  The timer-service callback
 * may already have copied the dispatcher handle, so deinit must drain that
 * tiny lease before it deletes the timer or recycles the stopped semaphore. */
static SemaphoreHandle_t s_timer_callback_drained;
static uint32_t s_timer_callbacks_inflight;
static bool s_timer_callback_admission_open;
static portMUX_TYPE s_timer_callback_lock = portMUX_INITIALIZER_UNLOCKED;
/* ESP-IDF remains an implementation detail of the dispatcher. Its public
 * service contract exposes only the hardware-neutral Device API result values
 * used by Alarm, Sleep Schedule and future Clock consumers. */
static device_status_t platform_status_to_device_status(esp_err_t status) {
    switch (status) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_NOT_SUPPORTED: return DEVICE_STATUS_UNAVAILABLE;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NOT_FOUND: return DEVICE_STATUS_NOT_FOUND;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

/* Lifecycle stop has one caller-owned budget.  The dispatcher may need to
 * serialize with both a public schedule mutation and its worker exit, so each
 * wait below must receive only the remaining portion of that one budget. */
static TickType_t stop_timeout_ticks(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}

static TickType_t stop_remaining_ticks(TickType_t started, TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}

static bool timer_callback_enter(void) {
    bool entered = false;
    taskENTER_CRITICAL(&s_timer_callback_lock);
    if (s_timer_callback_admission_open) {
        ++s_timer_callbacks_inflight;
        entered = true;
    }
    taskEXIT_CRITICAL(&s_timer_callback_lock);
    return entered;
}

static void timer_callback_leave(void) {
    taskENTER_CRITICAL(&s_timer_callback_lock);
    if (s_timer_callbacks_inflight > 0) {
        /* Publish completion before dropping the final lease. A deinit that
         * sees zero may immediately reclaim the semaphore, so the callback
         * must not touch it after the decrement becomes visible. */
        if (!s_timer_callback_admission_open &&
            s_timer_callbacks_inflight == 1 && s_timer_callback_drained) {
            xSemaphoreGive(s_timer_callback_drained);
        }
        --s_timer_callbacks_inflight;
    }
    taskEXIT_CRITICAL(&s_timer_callback_lock);
}

static int64_t current_epoch_ms(void) {
    struct timeval tv = {0};
    gettimeofday(&tv, NULL);
    return (int64_t)tv.tv_sec * 1000LL + tv.tv_usec / 1000;
}

static bool clock_is_plausible(int64_t epoch_ms) {
    return epoch_ms >= WAKE_DEADLINE_TRUSTED_EPOCH_MS;
}

static bool clock_is_trusted(int64_t epoch_ms) {
    return s_wall_clock_trusted && clock_is_plausible(epoch_ms);
}

static bool valid_handle(wake_deadline_handle_t handle, size_t *out_index) {
    if (!handle) return false;
    const size_t index = (size_t)((handle & 0xffu) - 1u);
    const uint16_t generation = (uint16_t)(handle >> 8);
    if (index >= WAKE_DEADLINE_MAX_SLOTS || !generation) return false;
    if (!s_slots[index].registered || s_slots[index].generation != generation) return false;
    if (out_index) *out_index = index;
    return true;
}

/* Unlike valid_handle(), this accepts a slot which unregister has already
 * closed but whose copied callback is still draining.  That makes a timed-out
 * unregister retryable without reopening admission. */
static bool handle_matches_generation(wake_deadline_handle_t handle, size_t *out_index) {
    if (!handle) return false;
    const size_t index = (size_t)((handle & 0xffu) - 1u);
    const uint16_t generation = (uint16_t)(handle >> 8);
    if (index >= WAKE_DEADLINE_MAX_SLOTS || !generation ||
        s_slots[index].generation != generation) {
        return false;
    }
    if (out_index) *out_index = index;
    return true;
}

/* s_lock is held. */
static void rearm_timer_locked(int64_t now_ms) {
    if (!s_timer || s_stop_requested || s_system_sleep_preparing) return;
    (void)esp_timer_stop(s_timer);
    if (!clock_is_trusted(now_ms)) return;

    int64_t earliest = 0;
    for (size_t i = 0; i < WAKE_DEADLINE_MAX_SLOTS; ++i) {
        const deadline_slot_t *slot = &s_slots[i];
        if (!slot->registered || !slot->armed) continue;
        if (!earliest || slot->epoch_ms < earliest) earliest = slot->epoch_ms;
    }
    if (!earliest) return;
    int64_t delay_ms = earliest - now_ms;
    uint64_t delay_us = delay_ms <= 0 ? WAKE_DEADLINE_MIN_TIMER_US
                                      : (uint64_t)delay_ms * 1000u;
    if (delay_us < WAKE_DEADLINE_MIN_TIMER_US) delay_us = WAKE_DEADLINE_MIN_TIMER_US;
    esp_err_t err = esp_timer_start_once(s_timer, delay_us);
    if (err != ESP_OK) ESP_LOGW(TAG, "cannot arm earliest deadline: %s", esp_err_to_name(err));
}

static void timer_callback(void *arg) {
    (void)arg;
    if (!timer_callback_enter()) return;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_timer_callback_lock);
    if (!s_stop_requested) task = s_task;
    taskEXIT_CRITICAL(&s_timer_callback_lock);
    if (task) xTaskNotifyGive(task);
    timer_callback_leave();
}

static void deadline_task(void *arg) {
    (void)arg;
    for (;;) {
        (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
        if (s_stop_requested) break;
        wake_deadline_callback_t callbacks[WAKE_DEADLINE_MAX_SLOTS] = {0};
        void *callback_args[WAKE_DEADLINE_MAX_SLOTS] = {0};
        size_t callback_slots[WAKE_DEADLINE_MAX_SLOTS] = {0};
        uint16_t callback_generations[WAKE_DEADLINE_MAX_SLOTS] = {0};
        size_t callback_count = 0;
        int64_t now_ms = current_epoch_ms();
        if (xSemaphoreTake(s_lock, portMAX_DELAY) == pdTRUE) {
            if (!s_system_sleep_preparing && clock_is_trusted(now_ms)) {
                for (size_t i = 0; i < WAKE_DEADLINE_MAX_SLOTS; ++i) {
                    deadline_slot_t *slot = &s_slots[i];
                    if (!slot->registered || !slot->armed || slot->epoch_ms > now_ms) continue;
                    slot->armed = false; /* callbacks explicitly re-arm repeating policy. */
                    if (slot->callback) {
                        slot->callback_inflight = true;
                        ++s_system_sleep_callbacks_inflight;
                        callbacks[callback_count] = slot->callback;
                        callback_args[callback_count] = slot->arg;
                        callback_slots[callback_count] = i;
                        callback_generations[callback_count] = slot->generation;
                        ++callback_count;
                    }
                }
            }
            rearm_timer_locked(now_ms);
            xSemaphoreGive(s_lock);
        }
        for (size_t i = 0; i < callback_count; ++i) {
            if (callbacks[i]) callbacks[i](callback_args[i]);
            /* unregister may have closed the slot while the callback ran. It
             * is nevertheless the same generation until this acknowledgement,
             * because register refuses an in-flight slot. */
            if (xSemaphoreTake(s_lock, portMAX_DELAY) == pdTRUE) {
                deadline_slot_t *slot = &s_slots[callback_slots[i]];
                if (slot->generation == callback_generations[i]) {
                    slot->callback_inflight = false;
                }
                if (s_system_sleep_callbacks_inflight > 0) {
                    --s_system_sleep_callbacks_inflight;
                }
                xSemaphoreGive(s_lock);
            }
        }
    }
    s_task = NULL;
    if (s_stopped) xSemaphoreGive(s_stopped);
    vTaskDelete(NULL);
}

device_status_t wake_deadline_service_init(void) {
    if (s_initialized) return DEVICE_STATUS_OK;
    /* A bounded stop remains closed until its original task/timer generation
     * has been joined and reclaimed.  Never create a second dispatcher while
     * the previous STOP transition is still in flight. */
    if (s_stop_requested) return DEVICE_STATUS_BUSY;
    if (!s_lock) s_lock = xSemaphoreCreateMutexStatic(&s_lock_storage);
    if (!s_deinit_lock) s_deinit_lock = xSemaphoreCreateMutexStatic(&s_deinit_lock_storage);
    if (!s_lock || !s_deinit_lock) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (xSemaphoreTake(s_deinit_lock, pdMS_TO_TICKS(3000)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (s_initialized) {
        xSemaphoreGive(s_deinit_lock);
        return DEVICE_STATUS_OK;
    }
    if (s_stop_requested || s_stopped || s_timer || s_task) {
        xSemaphoreGive(s_deinit_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_stopped = xSemaphoreCreateBinary();
    if (!s_stopped) {
        xSemaphoreGive(s_deinit_lock);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    s_timer_callback_drained = xSemaphoreCreateBinary();
    if (!s_timer_callback_drained) {
        vSemaphoreDelete(s_stopped);
        s_stopped = NULL;
        xSemaphoreGive(s_deinit_lock);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    esp_timer_create_args_t timer_args = {
        .callback = timer_callback,
        .name = "maclaw_deadline",
    };
    esp_err_t err = esp_timer_create(&timer_args, &s_timer);
    if (err != ESP_OK) {
        vSemaphoreDelete(s_timer_callback_drained);
        s_timer_callback_drained = NULL;
        vSemaphoreDelete(s_stopped);
        s_stopped = NULL;
        xSemaphoreGive(s_deinit_lock);
        return platform_status_to_device_status(err);
    }
    if (xTaskCreate(deadline_task, "maclaw_deadline", 3072, NULL, 6, &s_task) != pdPASS) {
        esp_timer_delete(s_timer);
        s_timer = NULL;
        vSemaphoreDelete(s_timer_callback_drained);
        s_timer_callback_drained = NULL;
        vSemaphoreDelete(s_stopped);
        s_stopped = NULL;
        xSemaphoreGive(s_deinit_lock);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    taskENTER_CRITICAL(&s_timer_callback_lock);
    s_timer_callbacks_inflight = 0;
    s_timer_callback_admission_open = true;
    taskEXIT_CRITICAL(&s_timer_callback_lock);
    s_initialized = true;
    s_system_sleep_preparing = false;
    s_system_sleep_callbacks_inflight = 0;
    s_wall_clock_trusted = false;
    xSemaphoreGive(s_deinit_lock);
    ESP_LOGI(TAG, "service ready: slots=%u", WAKE_DEADLINE_MAX_SLOTS);
    return DEVICE_STATUS_OK;
}

device_status_t wake_deadline_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!s_lock) return DEVICE_STATUS_OK;
    if (s_task && xTaskGetCurrentTaskHandle() == s_task) return DEVICE_STATUS_BUSY;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = stop_timeout_ticks(timeout_ms);
    if (!s_deinit_lock || xSemaphoreTake(s_deinit_lock, budget) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    TickType_t remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_lock, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_lock);
        return DEVICE_STATUS_TIMEOUT;
    }
    if (!s_initialized && !s_stop_requested) {
        xSemaphoreGive(s_lock);
        xSemaphoreGive(s_deinit_lock);
        return DEVICE_STATUS_OK;
    }
    const bool already_stopping = s_stop_requested;
    s_stop_requested = true;
    s_system_sleep_preparing = false;
    /* Close public admission while holding the same permanent lock used by
     * each public API. A caller that was already waiting rechecks this state
     * after it obtains the lock and will not touch timer/slot resources. */
    s_initialized = false;
    if (s_timer) (void)esp_timer_stop(s_timer);
    TaskHandle_t task = s_task;
    xSemaphoreGive(s_lock);
    bool callback_drained = false;
    taskENTER_CRITICAL(&s_timer_callback_lock);
    s_timer_callback_admission_open = false;
    callback_drained = s_timer_callbacks_inflight == 0;
    taskEXIT_CRITICAL(&s_timer_callback_lock);
    if (!callback_drained) {
        remaining = stop_remaining_ticks(started, budget);
        if (!s_timer_callback_drained || remaining == 0 ||
            xSemaphoreTake(s_timer_callback_drained, remaining) != pdTRUE) {
            xSemaphoreGive(s_deinit_lock);
            return DEVICE_STATUS_TIMEOUT;
        }
    }
    if (task) {
        xTaskNotifyGive(task);
        remaining = stop_remaining_ticks(started, budget);
        if (remaining == 0 || xSemaphoreTake(s_stopped, remaining) != pdTRUE) {
            xSemaphoreGive(s_deinit_lock);
            return DEVICE_STATUS_TIMEOUT;
        }
    } else if (already_stopping && s_stopped) {
        /* A previous caller timed out after the dispatcher had exited but
         * before it consumed the completion.  Drain that final signal before
         * reclaiming the timer/semaphore generation. */
        remaining = stop_remaining_ticks(started, budget);
        if (remaining == 0 || xSemaphoreTake(s_stopped, remaining) != pdTRUE) {
            xSemaphoreGive(s_deinit_lock);
            return DEVICE_STATUS_TIMEOUT;
        }
    }
    if (s_timer) {
        esp_err_t timer_err = esp_timer_delete(s_timer);
        if (timer_err != ESP_OK) {
            xSemaphoreGive(s_deinit_lock);
            return platform_status_to_device_status(timer_err);
        }
        s_timer = NULL;
    }
    memset(s_slots, 0, sizeof(s_slots));
    remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_lock, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_lock);
        return DEVICE_STATUS_TIMEOUT;
    }
    s_stop_requested = false;
    s_system_sleep_callbacks_inflight = 0;
    s_wall_clock_trusted = false;
    xSemaphoreGive(s_lock);
    /* No worker or queued timer callback can touch this completion object
     * after the joined task signal and timer deletion, so it is safe to
     * reclaim it outside the permanent admission lock. */
    vSemaphoreDelete(s_stopped);
    s_stopped = NULL;
    vSemaphoreDelete(s_timer_callback_drained);
    s_timer_callback_drained = NULL;
    xSemaphoreGive(s_deinit_lock);
    ESP_LOGI(TAG, "service stopped");
    return DEVICE_STATUS_OK;
}

device_status_t wake_deadline_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0 || !s_lock) return DEVICE_STATUS_INVALID_ARGUMENT;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = stop_timeout_ticks(timeout_ms);
    TickType_t remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_lock, remaining) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (!s_initialized || s_stop_requested) {
        xSemaphoreGive(s_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (s_system_sleep_preparing) {
        xSemaphoreGive(s_lock);
        return DEVICE_STATUS_BUSY;
    }
    if (!wake_deadline_sleep_gate_begin(&s_system_sleep_preparing)) {
        xSemaphoreGive(s_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* `esp_timer_stop()` suppresses future timer-service delivery; a callback
     * already copied by deadline_task is covered by the counter below. */
    if (s_timer) (void)esp_timer_stop(s_timer);
    xSemaphoreGive(s_lock);

    for (;;) {
        remaining = stop_remaining_ticks(started, budget);
        if (remaining == 0 || xSemaphoreTake(s_lock, remaining) != pdTRUE) {
            /* Preserve the fence until Power executes its mandatory ABORT.
             * A callback selected before this timeout may still be unwinding;
             * reopening here would admit a new deadline across a possible
             * future electrical commit. */
            return DEVICE_STATUS_TIMEOUT;
        }
        const bool drained = wake_deadline_sleep_gate_callbacks_drained(
            &s_system_sleep_callbacks_inflight);
        xSemaphoreGive(s_lock);
        if (drained) return DEVICE_STATUS_OK;
        if (stop_remaining_ticks(started, budget) == 0) {
            return DEVICE_STATUS_TIMEOUT;
        }
        vTaskDelay(1);
    }
}

void wake_deadline_service_abort_system_sleep_prepare(void) {
    if (!s_lock || xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) return;
    const bool was_preparing = s_system_sleep_preparing;
    wake_deadline_sleep_gate_abort(&s_system_sleep_preparing);
    if (was_preparing && s_initialized && !s_stop_requested) {
        /* Keep persisted epochs intact. A deadline that elapsed while PREPARE
         * was closed is re-evaluated by the normal client worker after this
         * one bounded re-arm, rather than being synthesized by Power. */
        rearm_timer_locked(current_epoch_ms());
    }
    xSemaphoreGive(s_lock);
}

device_status_t wake_deadline_service_register(wake_deadline_callback_t callback, void *arg,
                                         wake_deadline_handle_t *out_handle) {
    if (!callback || !out_handle || !s_lock) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) return DEVICE_STATUS_TIMEOUT;
    if (!s_initialized || s_stop_requested || s_system_sleep_preparing) {
        xSemaphoreGive(s_lock);
        return DEVICE_STATUS_BUSY;
    }
    device_status_t result = DEVICE_STATUS_RESOURCE_EXHAUSTED;
    for (size_t i = 0; i < WAKE_DEADLINE_MAX_SLOTS; ++i) {
        deadline_slot_t *slot = &s_slots[i];
        /* An unregistering client may already have cleared `registered` while
         * this worker still owns a copied callback/arg.  Reusing that slot
         * would make generation-based drain acknowledgement unsafe. */
        if (slot->registered || slot->callback_inflight) continue;
        uint16_t generation = (uint16_t)(slot->generation + 1u);
        if (!generation) generation = 1;
        *slot = (deadline_slot_t){
            .generation = generation,
            .registered = true,
            .callback = callback,
            .arg = arg,
        };
        *out_handle = ((uint32_t)generation << 8) | (uint32_t)(i + 1u);
        result = DEVICE_STATUS_OK;
        break;
    }
    xSemaphoreGive(s_lock);
    return result;
}

device_status_t wake_deadline_service_arm(wake_deadline_handle_t handle, int64_t epoch_ms) {
    if (!s_lock || epoch_ms <= 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) return DEVICE_STATUS_TIMEOUT;
    if (!s_initialized || s_stop_requested || s_system_sleep_preparing) {
        xSemaphoreGive(s_lock);
        return DEVICE_STATUS_BUSY;
    }
    size_t index = 0;
    if (!valid_handle(handle, &index)) {
        xSemaphoreGive(s_lock);
        return DEVICE_STATUS_NOT_FOUND;
    }
    s_slots[index].epoch_ms = epoch_ms;
    s_slots[index].armed = true;
    rearm_timer_locked(current_epoch_ms());
    xSemaphoreGive(s_lock);
    return DEVICE_STATUS_OK;
}

void wake_deadline_service_cancel(wake_deadline_handle_t handle) {
    if (!s_lock || xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) return;
    if (!s_initialized || s_stop_requested) {
        xSemaphoreGive(s_lock);
        return;
    }
    size_t index = 0;
    if (valid_handle(handle, &index)) {
        s_slots[index].armed = false;
        s_slots[index].epoch_ms = 0;
        rearm_timer_locked(current_epoch_ms());
    }
    xSemaphoreGive(s_lock);
}

device_status_t wake_deadline_service_unregister_with_timeout(wake_deadline_handle_t handle,
                                                        uint32_t timeout_ms) {
    if (!handle || timeout_ms == 0 || !s_lock) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (s_task && xTaskGetCurrentTaskHandle() == s_task) return DEVICE_STATUS_BUSY;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = stop_timeout_ticks(timeout_ms);
    TickType_t remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_lock, remaining) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (!s_initialized || s_stop_requested) {
        xSemaphoreGive(s_lock);
        return DEVICE_STATUS_BUSY;
    }
    size_t index = 0;
    if (!handle_matches_generation(handle, &index)) {
        xSemaphoreGive(s_lock);
        return DEVICE_STATUS_NOT_FOUND;
    }
    const uint16_t generation = s_slots[index].generation;
    deadline_slot_t *slot = &s_slots[index];
    if (slot->registered) {
        slot->registered = false;
        slot->armed = false;
        slot->epoch_ms = 0;
        slot->callback = NULL;
        slot->arg = NULL;
        rearm_timer_locked(current_epoch_ms());
    }
    xSemaphoreGive(s_lock);

    /* The worker deliberately executes callbacks outside the admission mutex.
     * Polling the small, fixed slot state avoids a shared completion semaphore
     * whose stale signal could acknowledge the wrong client's callback. */
    for (;;) {
        remaining = stop_remaining_ticks(started, budget);
        if (remaining == 0 || xSemaphoreTake(s_lock, remaining) != pdTRUE) {
            return DEVICE_STATUS_TIMEOUT;
        }
        const bool drained = s_slots[index].generation == generation &&
                             !s_slots[index].callback_inflight;
        xSemaphoreGive(s_lock);
        if (drained) return DEVICE_STATUS_OK;
        if (stop_remaining_ticks(started, budget) == 0) return DEVICE_STATUS_TIMEOUT;
        vTaskDelay(1);
    }
}

void wake_deadline_service_unregister(wake_deadline_handle_t handle) {
    (void)wake_deadline_service_unregister_with_timeout(handle, 1000);
}

void wake_deadline_service_on_trusted_wall_clock_updated(void) {
    if (!s_lock || xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) return;
    if (s_initialized && !s_stop_requested && clock_is_plausible(current_epoch_ms())) {
        s_wall_clock_trusted = true;
        if (!s_system_sleep_preparing && s_task) xTaskNotifyGive(s_task);
    }
    xSemaphoreGive(s_lock);
}

device_status_t wake_deadline_service_get_clock_status(int64_t *out_epoch_ms,
                                                       bool *out_trusted) {
    if (!out_epoch_ms || !out_trusted) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!s_lock || xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    const bool available = s_initialized && !s_stop_requested;
    const int64_t now_ms = current_epoch_ms();
    xSemaphoreGive(s_lock);
    if (!available) return DEVICE_STATUS_UNAVAILABLE;
    *out_epoch_ms = now_ms;
    *out_trusted = clock_is_trusted(now_ms);
    return DEVICE_STATUS_OK;
}
