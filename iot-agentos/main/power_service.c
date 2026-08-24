#include "power_service.h"

#include "app_intent_service.h"
#include "battery_policy_service.h"
#include "platform_power.h"
#include "power_lease_service.h"
#include "provisioning_failure_injection.h"
#include "wake_service.h"
#include "audio_service.h"
#include "services/command_service.h"
#include "services/provisioning_service.h"
#include "firmware_identity.h"
#include "update_service.h"
#include "configuration_service.h"
#include "meeting_recovery_service.h"
#include "weather_cache_service.h"
#include "persistence_service.h"
#include "display_service.h"
#include "fall_detection_service.h"
#include "alarm_manager.h"
#include "sleep_schedule_service.h"
#include "wake_deadline_service.h"
#include "connectivity_service.h"
#include "services/ambient_service.h"
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
/* The DISPLAY_OFF scheduler is Power's own retained physical submitter.  A
 * future Light/Deep Sleep transaction must park it separately from Display
 * Service: this worker calls Platform Power directly, so semantic Display
 * admission alone cannot prove that an idle deadline will not touch a panel
 * while a profile prepares rails or clocks. */
static SemaphoreHandle_t s_system_sleep_display_off_scheduler_quiesced;
static bool s_system_sleep_display_off_scheduler_preparing;
static bool s_initialized;
static bool s_initializing;
static bool s_stopping;
static bool s_display_off_armed;
/* Each arm/cancel/wake starts a new logical idle-deadline generation.  The
 * esp_timer callback is deliberately tiny and can already have notified the
 * worker when a foreground path replaces the timer.  Carrying this value into
 * the worker prevents a lock-contention retry from later committing an older
 * ambient deadline over the replacement. */
static uint32_t s_display_off_generation;
/* Monotonic due time is authoritative for worker admission.  esp_timer is a
 * precision notifier only: its callback can be queued across a cancel/rearm,
 * so the worker must never treat a bare notification as proof that the most
 * recently armed deadline has actually elapsed. */
static int64_t s_display_off_due_us;
/* A transition-mutex timeout means a valid ambient deadline could not yet
 * reach the board adapter.  It is still pending even though the esp_timer is
 * one-shot and no longer armed; expose and cancel it as part of the same idle
 * request rather than silently losing screen sleep. */
static bool s_display_off_retry_pending;
/* State is diagnostic and transaction ownership only; it is not a second
 * power-state authority. Physical DISPLAY_OFF observation stays with the
 * display adapter, and future Light/Deep resume will be published only after
 * an actual profile Power HAL is available. */
/* A request generation belongs to the public system-sleep transaction, not
 * to the private Power Lease fence.  In particular, a rejected preflight has
 * no lease generation at all but must still be observable as a new request;
 * conversely, a future replacement of the Lease Service must not make old
 * Power transition diagnostics appear to go backwards. */
static uint32_t s_system_sleep_transition_generation;
static device_power_state_t s_system_sleep_transition_target;
static device_power_transition_phase_t s_system_sleep_transition_phase;
static device_status_t s_system_sleep_transition_last_status;
/* A few legacy NVS mutations still require a composition-root-owned internal
 * stack worker.  Keep its task/queue details out of shared Power and make it
 * a value-only participant immediately before Persistence seals the generic
 * durable boundary. */
static power_service_system_sleep_storage_prepare_t s_system_sleep_storage_prepare;
static power_service_system_sleep_storage_abort_t s_system_sleep_storage_abort;
static void *s_system_sleep_storage_context;
/* This private test admission is deliberately held in Power Service rather
 * than Device API: it proves only the scheduler's stale-generation handling
 * and must not manufacture a panel or input test surface. */

/* All public/scheduler transition paths may serialize with a panel DMA or a
 * foreground wake.  They must not, however, turn a transient display lock
 * into an unbounded App Interaction, schedule, or esp_timer callback stall.
 * On expiry callers retain the safe state: no new DISPLAY_OFF commit is made
 * and a physical contact is not reinterpreted as a business action. */
#define POWER_TRANSITION_LOCK_TIMEOUT_MS 1500u
#define POWER_DISPLAY_OFF_RETRY_DELAY_MS 1000u
/* This is the parent budget for the currently single profile-private
 * electrical PREPARE step. When Audio/Display/Connectivity/Persistence
 * participants land, every one must consume the same original deadline. */
#define POWER_SYSTEM_SLEEP_PREPARE_TIMEOUT_MS 3000u
/* Once every shared participant is parked, a physical sleep may last much
 * longer than its setup transaction. These independent, bounded budgets apply
 * only to profile-local final entry work and to post-wake restoration. They
 * must never be calculated from the PREPARE deadline. */
#define POWER_SYSTEM_SLEEP_COMMIT_ENTRY_TIMEOUT_MS 3000u
#define POWER_SYSTEM_SLEEP_RESUME_TIMEOUT_MS 3000u
/* PREPARE shares one parent deadline, but rollback must not inherit an
 * already-expired value.  A future profile can have armed a wake source or
 * parked a private peripheral immediately before discovering a late failure;
 * passing zero through Platform Power would reject ABORT before that adapter
 * can restore its own baseline.  Keep cleanup bounded and separate from the
 * policy transaction budget. */
#define POWER_SYSTEM_SLEEP_ROLLBACK_TIMEOUT_MS 3000u

static uint32_t system_sleep_prepare_remaining_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const uint64_t rounded_ms = ((uint64_t)remaining_us + 999u) / 1000u;
    return rounded_ms > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded_ms;
}

device_status_t power_service_set_system_sleep_storage_bridge(
    power_service_system_sleep_storage_prepare_t prepare,
    power_service_system_sleep_storage_abort_t abort,
    void *context) {
    if (!prepare || !abort) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_power_lock);
    const bool transition_active =
        s_system_sleep_transition_phase != DEVICE_POWER_TRANSITION_IDLE;
    if (!transition_active) {
        s_system_sleep_storage_prepare = prepare;
        s_system_sleep_storage_abort = abort;
        s_system_sleep_storage_context = context;
    }
    taskEXIT_CRITICAL(&s_power_lock);
    return transition_active ? DEVICE_STATUS_BUSY : DEVICE_STATUS_OK;
}

static void abort_system_sleep_storage_bridge(void) {
    power_service_system_sleep_storage_abort_t abort = NULL;
    void *context = NULL;
    taskENTER_CRITICAL(&s_power_lock);
    abort = s_system_sleep_storage_abort;
    context = s_system_sleep_storage_context;
    taskEXIT_CRITICAL(&s_power_lock);
    if (abort) abort(context);
}

/* This is an internal participant of the Power transaction owner, not a new
 * Device/Platform API.  It preserves the existing logical idle deadline and
 * retained worker generation: ABORT merely lets that worker recalculate the
 * preserved monotonic due time.  The ESP timer is stopped because an already
 * queued callback is not sufficient proof that a later profile electrical
 * PREPARE will be free of direct Platform-Power display work. */
static void abort_display_off_scheduler_system_sleep_prepare(void) {
    TaskHandle_t worker = NULL;
    bool resume_callback_admission = false;
    taskENTER_CRITICAL(&s_power_lock);
    if (s_system_sleep_display_off_scheduler_preparing) {
        s_system_sleep_display_off_scheduler_preparing = false;
        worker = s_initialized && !s_stopping ? s_display_off_worker_task : NULL;
        resume_callback_admission = s_initialized && !s_stopping;
    }
    taskEXIT_CRITICAL(&s_power_lock);
    if (resume_callback_admission) {
        taskENTER_CRITICAL(&s_display_off_timer_callback_lock);
        s_display_off_timer_callback_admission_open = true;
        taskEXIT_CRITICAL(&s_display_off_timer_callback_lock);
    }
    /* A stopped one-shot timer need not be recreated: the retained worker
     * owns the authoritative due timestamp and its notification below makes
     * it wait until that original deadline (or commit it immediately if it
     * elapsed while the future transaction was being prepared). */
    if (worker) xTaskNotifyGive(worker);
}

static device_status_t prepare_display_off_scheduler_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const TickType_t started = xTaskGetTickCount();
    TickType_t budget = pdMS_TO_TICKS(timeout_ms);
    if (budget == 0) budget = 1;

    SemaphoreHandle_t deinit_mutex = NULL;
    taskENTER_CRITICAL(&s_power_lock);
    deinit_mutex = s_deinit_mutex;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!deinit_mutex || xSemaphoreTake(deinit_mutex, budget) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }

    TickType_t elapsed = xTaskGetTickCount() - started;
    TickType_t remaining = elapsed >= budget ? 0 : budget - elapsed;
    if (remaining == 0 || !s_transition_mutex ||
        xSemaphoreTake(s_transition_mutex, remaining) != pdTRUE) {
        xSemaphoreGive(deinit_mutex);
        return DEVICE_STATUS_TIMEOUT;
    }

    esp_timer_handle_t timer = NULL;
    TaskHandle_t worker = NULL;
    SemaphoreHandle_t quiesced = NULL;
    taskENTER_CRITICAL(&s_power_lock);
    if (!s_initialized || s_stopping ||
        s_system_sleep_display_off_scheduler_preparing ||
        !s_display_off_timer || !s_display_off_worker_task ||
        !s_system_sleep_display_off_scheduler_quiesced) {
        const device_status_t status = !s_initialized || s_stopping
                                           ? DEVICE_STATUS_UNAVAILABLE
                                           : DEVICE_STATUS_BUSY;
        taskEXIT_CRITICAL(&s_power_lock);
        xSemaphoreGive(s_transition_mutex);
        xSemaphoreGive(deinit_mutex);
        return status;
    }
    timer = s_display_off_timer;
    worker = s_display_off_worker_task;
    quiesced = s_system_sleep_display_off_scheduler_quiesced;
    s_system_sleep_display_off_scheduler_preparing = true;
    taskEXIT_CRITICAL(&s_power_lock);

    /* Consume a late token from a timed-out old attempt.  Only the current
     * worker observation after this marker is allowed to acknowledge. */
    while (xSemaphoreTake(quiesced, 0) == pdTRUE) {
    }
    (void)esp_timer_stop(timer);
    bool callbacks_drained = false;
    taskENTER_CRITICAL(&s_display_off_timer_callback_lock);
    s_display_off_timer_callback_admission_open = false;
    callbacks_drained = s_display_off_timer_callbacks_inflight == 0;
    taskEXIT_CRITICAL(&s_display_off_timer_callback_lock);
    xSemaphoreGive(s_transition_mutex);
    xSemaphoreGive(deinit_mutex);

    elapsed = xTaskGetTickCount() - started;
    remaining = elapsed >= budget ? 0 : budget - elapsed;
    if (!callbacks_drained) {
        if (!s_display_off_timer_callback_drained || remaining == 0 ||
            xSemaphoreTake(s_display_off_timer_callback_drained, remaining) != pdTRUE) {
            /* A timer callback accepted before closure may still be retiring.
             * Keep scheduler and callback admission parked until this parent
             * transaction executes its reverse-order ABORT; reopening here
             * could let a stale idle deadline race a later participant. */
            return DEVICE_STATUS_TIMEOUT;
        }
    }
    xTaskNotifyGive(worker);
    elapsed = xTaskGetTickCount() - started;
    remaining = elapsed >= budget ? 0 : budget - elapsed;
    if (remaining == 0 || xSemaphoreTake(quiesced, remaining) != pdTRUE) {
        /* The worker may acknowledge just after timeout. It must remain
         * parked until Power's common rollback owns the generation restore. */
        return DEVICE_STATUS_TIMEOUT;
    }
    return DEVICE_STATUS_OK;
}

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
                         !s_system_sleep_display_off_scheduler_preparing &&
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
    s_display_off_retry_pending = false;
    ++s_display_off_generation;
    if (!s_display_off_generation) s_display_off_generation = 1;
    s_display_off_due_us = 0;
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

static void handle_display_off_deadline(uint32_t generation) {
    if (!s_transition_mutex ||
        xSemaphoreTake(s_transition_mutex, power_transition_lock_timeout()) != pdTRUE) {
        /* The timer is one-shot, but a display/DMA owner may temporarily hold
         * this mutex while the common UI remains ambient.  Retain a bounded
         * worker-side retry.  Every retry revalidates the arm generation
         * before it can reach the physical panel, so a later cancel/wake/new
         * schedule wins deterministically. */
        taskENTER_CRITICAL(&s_power_lock);
        const bool retry_current = s_initialized && !s_stopping &&
                                   s_display_off_timer != NULL &&
                                   s_display_off_generation == generation;
        if (retry_current) {
            s_display_off_retry_pending = true;
            s_display_off_armed = true;
            s_display_off_due_us = esp_timer_get_time() +
                                       (int64_t)POWER_DISPLAY_OFF_RETRY_DELAY_MS * 1000;
        }
        taskEXIT_CRITICAL(&s_power_lock);
        if (retry_current) {
            ESP_LOGW(TAG, "idle deadline deferred: display transition remained busy");
            return;
        }
        return;
    }
    /* A deadline notification can be queued just before lifecycle teardown.
     * Revalidate after acquiring the same transition mutex used by deinit so a
     * worker which was waiting behind foreground display work cannot commit a
     * physical DISPLAY_OFF after its service generation was closed. */
    taskENTER_CRITICAL(&s_power_lock);
    const bool current = s_initialized && !s_stopping &&
                         !s_system_sleep_display_off_scheduler_preparing &&
                         s_display_off_timer != NULL &&
                         s_display_off_generation == generation;
    if (current) s_display_off_retry_pending = false;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!current) {
        xSemaphoreGive(s_transition_mutex);
        return;
    }
    uint32_t commit_generation = 0;
    device_status_t commit_status =
        power_lease_service_begin_display_off_commit(&commit_generation);
    if (commit_status != DEVICE_STATUS_OK) {
        /* Keep the idle request live while a foreground operation owns the
         * screen.  Releasing the final lease does not have to know which UI
         * timer originally armed this deadline, and a schedule-owned window
         * still converges to DISPLAY_OFF without an unrelated repaint. */
        taskENTER_CRITICAL(&s_power_lock);
        bool can_rearm = s_initialized && !s_stopping &&
                         s_display_off_timer != NULL &&
                         s_display_off_generation == generation;
        s_display_off_armed = can_rearm;
        s_display_off_retry_pending = false;
        if (can_rearm) {
            s_display_off_due_us = esp_timer_get_time() + 1000000;
        } else {
            s_display_off_due_us = 0;
        }
        taskEXIT_CRITICAL(&s_power_lock);
        if (!can_rearm || esp_timer_start_once(s_display_off_timer, 1000000) != ESP_OK) {
            taskENTER_CRITICAL(&s_power_lock);
            s_display_off_armed = false;
            s_display_off_due_us = 0;
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
     * where a foreground UI transition and a timer deadline cross.  The
     * lease service's second check is immediately before this call; its
     * generation fence rejects any new foreground lease until we finish. */
    const device_status_t transition =
        power_lease_service_display_off_commit_is_current(commit_generation)
            ? platform_power_enter_display_off()
            : DEVICE_STATUS_BUSY;
    power_lease_service_end_display_off_commit(commit_generation);
    if (transition == DEVICE_STATUS_OK) {
        ESP_LOGI(TAG, "idle deadline reached: DISPLAY_OFF entered");
    } else {
        ESP_LOGD(TAG, "idle deadline ignored: display transition status=%d",
                 (int)transition);
    }
    xSemaphoreGive(s_transition_mutex);
}

static void display_off_worker_task(void *arg) {
    (void)arg;
    for (;;) {
        /* A timer callback, a rearm, a cancellation and stop all use the same
         * notification.  Re-sample authoritative state after every wake; a
         * stale esp_timer callback cannot advance a newer deadline. */
        (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
        for (;;) {
            taskENTER_CRITICAL(&s_power_lock);
            const bool stopping = s_stopping;
            const bool system_sleep_preparing =
                s_system_sleep_display_off_scheduler_preparing;
            const bool armed = s_initialized && s_display_off_armed;
            const uint32_t generation = s_display_off_generation;
            const int64_t due_us = s_display_off_due_us;
            taskEXIT_CRITICAL(&s_power_lock);
            if (stopping) goto stopped;
            if (system_sleep_preparing) {
                /* The transaction owner already stopped callback admission
                 * and holds the transition mutex while it publishes this
                 * marker.  Acknowledging here therefore proves no queued
                 * idle deadline can still enter Platform Power. */
                SemaphoreHandle_t quiesced = NULL;
                taskENTER_CRITICAL(&s_power_lock);
                quiesced = s_system_sleep_display_off_scheduler_quiesced;
                taskEXIT_CRITICAL(&s_power_lock);
                if (quiesced) xSemaphoreGive(quiesced);
                do {
                    (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
                    taskENTER_CRITICAL(&s_power_lock);
                    const bool still_preparing =
                        s_system_sleep_display_off_scheduler_preparing;
                    const bool now_stopping = s_stopping;
                    taskEXIT_CRITICAL(&s_power_lock);
                    if (now_stopping) goto stopped;
                    if (!still_preparing) break;
                } while (true);
                continue;
            }
            if (!armed || due_us <= 0) break;

            const int64_t remaining_us = due_us - esp_timer_get_time();
            if (remaining_us > 0) {
                const uint64_t rounded_ms = ((uint64_t)remaining_us + 999u) / 1000u;
                const TickType_t wait_ticks = rounded_ms > UINT32_MAX
                                                  ? portMAX_DELAY
                                                  : pdMS_TO_TICKS((uint32_t)rounded_ms);
                /* A stale notification merely makes us re-evaluate the same
                 * due time. A new/cancelled deadline wakes this wait early. */
                (void)ulTaskNotifyTake(pdTRUE, wait_ticks ? wait_ticks : 1);
                continue;
            }

            taskENTER_CRITICAL(&s_power_lock);
            const bool owns_due = s_initialized && !s_stopping &&
                                  s_display_off_armed &&
                                  s_display_off_generation == generation &&
                                  s_display_off_due_us == due_us &&
                                  esp_timer_get_time() >= due_us;
            if (owns_due) {
                s_display_off_armed = false;
                s_display_off_retry_pending = false;
            }
            taskEXIT_CRITICAL(&s_power_lock);
            if (!owns_due) continue;
            handle_display_off_deadline(generation);
            /* A contention or foreground-lease deferral re-publishes an
             * armed monotonic deadline; a completed/stale transition leaves
             * no work and returns to the outer notification wait. */
        }
    }

stopped:

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

/* This is intentionally an internal, boot-only HIL companion rather than a
 * Device/Platform test API.  The test owns the actual Power Service mutex so
 * the normal worker reaches its real contention/retry path.  It never needs
 * to know which panel, bus, touch controller or key was selected. */
typedef struct {
    SemaphoreHandle_t acquired;
    SemaphoreHandle_t released;
    SemaphoreHandle_t user_wake_requested;
    SemaphoreHandle_t user_wake_done;
    volatile device_status_t user_wake_status;
} power_retry_hil_holder_t;

static void power_retry_hil_hold_transition_task(void *arg) {
    power_retry_hil_holder_t *holder = (power_retry_hil_holder_t *)arg;
    if (!holder || !s_transition_mutex ||
        xSemaphoreTake(s_transition_mutex, portMAX_DELAY) != pdTRUE) {
        if (holder && holder->released) xSemaphoreGive(holder->released);
        vTaskDelete(NULL);
        return;
    }
    if (holder->acquired) xSemaphoreGive(holder->acquired);
    /* Leave a real margin past the worker's bounded take.  Releasing exactly
     * on the timeout tick would test scheduler tie-breaking rather than the
     * retry path. */
    TickType_t hold_ticks = pdMS_TO_TICKS(POWER_TRANSITION_LOCK_TIMEOUT_MS + 750u);
    if (hold_ticks == 0) hold_ticks = 1;
    vTaskDelay(hold_ticks);
    /* Make the real user-wake caller runnable before dropping the mutex. Its
     * higher test-only priority means it queues on this mutex first, so it
     * deterministically wins the hand-off over the normal retry worker. */
    if (holder->user_wake_requested) xSemaphoreGive(holder->user_wake_requested);
    xSemaphoreGive(s_transition_mutex);
    if (holder->released) xSemaphoreGive(holder->released);
    vTaskDelete(NULL);
}

static void power_retry_hil_user_wake_task(void *arg) {
    power_retry_hil_holder_t *holder = (power_retry_hil_holder_t *)arg;
    if (!holder || !holder->user_wake_requested || !holder->user_wake_done) {
        vTaskDelete(NULL);
        return;
    }
    if (xSemaphoreTake(holder->user_wake_requested, portMAX_DELAY) == pdTRUE) {
        holder->user_wake_status = power_service_wake_display_from_user();
        xSemaphoreGive(holder->user_wake_done);
    }
    vTaskDelete(NULL);
}

static bool power_retry_hil_wait_for_display_off(uint32_t timeout_ms) {
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    while (esp_timer_get_time() < deadline_us) {
        if (platform_power_display_is_off()) return true;
        vTaskDelay(pdMS_TO_TICKS(20));
    }
    return platform_power_display_is_off();
}

static bool power_retry_hil_wait_for_active_display(uint32_t timeout_ms) {
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    while (esp_timer_get_time() < deadline_us) {
        if (!platform_power_display_is_off()) return true;
        vTaskDelay(pdMS_TO_TICKS(20));
    }
    return !platform_power_display_is_off();
}

static bool power_retry_hil_start_holder(power_retry_hil_holder_t *holder,
                                         StaticSemaphore_t *acquired_storage,
                                         StaticSemaphore_t *released_storage) {
    if (!holder || !acquired_storage || !released_storage) return false;
    holder->acquired = xSemaphoreCreateBinaryStatic(acquired_storage);
    holder->released = xSemaphoreCreateBinaryStatic(released_storage);
    if (!holder->acquired || !holder->released) return false;
    TaskHandle_t task = NULL;
    return xTaskCreate(power_retry_hil_hold_transition_task, "power_retry_hil",
                       3072, holder, 4, &task) == pdPASS;
}

static bool power_retry_hil_wait_holder_acquired(power_retry_hil_holder_t *holder,
                                                 uint32_t timeout_ms) {
    if (!holder || !holder->acquired) return false;
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    if (ticks == 0) ticks = 1;
    return xSemaphoreTake(holder->acquired, ticks) == pdTRUE;
}

static bool power_retry_hil_wait_holder(power_retry_hil_holder_t *holder,
                                        uint32_t timeout_ms) {
    if (!holder || !holder->released) return false;
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    if (ticks == 0) ticks = 1;
    return xSemaphoreTake(holder->released, ticks) == pdTRUE;
}

static bool power_retry_hil_start_user_waker(power_retry_hil_holder_t *holder,
                                             StaticSemaphore_t *request_storage,
                                             StaticSemaphore_t *done_storage) {
    if (!holder || !request_storage || !done_storage) return false;
    holder->user_wake_requested = xSemaphoreCreateBinaryStatic(request_storage);
    holder->user_wake_done = xSemaphoreCreateBinaryStatic(done_storage);
    holder->user_wake_status = DEVICE_STATUS_INTERNAL_ERROR;
    if (!holder->user_wake_requested || !holder->user_wake_done) return false;
    TaskHandle_t task = NULL;
    return xTaskCreate(power_retry_hil_user_wake_task, "power_retry_wake",
                       3072, holder, 5, &task) == pdPASS;
}

static bool power_retry_hil_wait_user_waker(power_retry_hil_holder_t *holder,
                                            uint32_t timeout_ms) {
    if (!holder || !holder->user_wake_done) return false;
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    if (ticks == 0) ticks = 1;
    /* A first contact that wins while the panel is still active still
     * consumes the old deadline, but the adapter rightly reports that no
     * physical wake was necessary.  Completion plus the later active-panel
     * observation is therefore the HIL assertion, not a forced OK code. */
    return xSemaphoreTake(holder->user_wake_done, ticks) == pdTRUE;
}

device_status_t power_service_run_display_off_retry_hil_test(void) {
    if (!provisioning_failure_injection_power_display_off_retry_hil_test_enabled()) {
        return DEVICE_STATUS_OK;
    }

    /* This proof runs before App UI starts its ordinary idle policy.  Start
     * from an explicitly visible panel so every following observation is a
     * physical Platform/Display/adapter transaction, not a stale boot flag. */
    if (platform_power_display_is_off() &&
        power_service_wake_display_from_schedule() != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "test: cannot establish active panel");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    if (!power_retry_hil_wait_for_active_display(1000)) {
        ESP_LOGE(TAG, "test: active panel observation failed");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }

    power_retry_hil_holder_t holder = {0};
    StaticSemaphore_t acquired_storage;
    StaticSemaphore_t released_storage;
    StaticSemaphore_t user_wake_request_storage;
    StaticSemaphore_t user_wake_done_storage;
    if (power_service_schedule_display_off(250) != DEVICE_STATUS_OK ||
        !power_retry_hil_start_holder(&holder, &acquired_storage,
                                      &released_storage) ||
        !power_retry_hil_start_user_waker(&holder, &user_wake_request_storage,
                                          &user_wake_done_storage) ||
        !power_retry_hil_wait_holder_acquired(&holder, 1000)) {
        ESP_LOGE(TAG, "test: cannot create initial transition holder");
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    /* A user wake which wins after the original deadline could not obtain the
     * mutex, but before its retry due time, must invalidate that retry. */
    if (!power_retry_hil_wait_holder(&holder, POWER_TRANSITION_LOCK_TIMEOUT_MS + 1000u)) {
        ESP_LOGE(TAG, "test: transition holder did not release");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    if (!power_retry_hil_wait_user_waker(&holder, 1000)) {
        ESP_LOGE(TAG, "test: user wake could not cancel stale retry");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    vTaskDelay(pdMS_TO_TICKS(1200));
    if (!power_retry_hil_wait_for_active_display(100)) {
        ESP_LOGE(TAG, "test: user wake allowed stale retry DISPLAY_OFF");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }

    power_retry_hil_holder_t retry_holder = {0};
    StaticSemaphore_t retry_acquired_storage;
    StaticSemaphore_t retry_released_storage;
    if (power_service_schedule_display_off(250) != DEVICE_STATUS_OK ||
        !power_retry_hil_start_holder(&retry_holder, &retry_acquired_storage,
                                      &retry_released_storage) ||
        !power_retry_hil_wait_holder_acquired(&retry_holder, 1000) ||
        !power_retry_hil_wait_holder(&retry_holder, POWER_TRANSITION_LOCK_TIMEOUT_MS + 1000u) ||
        !power_retry_hil_wait_for_display_off(2500)) {
        ESP_LOGE(TAG, "test: retry did not commit one physical DISPLAY_OFF");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    ESP_LOGI(TAG, "test: stale DISPLAY_OFF retry reached selected panel");

    /* Remote wake must use exactly the same stale cancellation discipline,
     * while retaining its distinct no-input semantics. */
    if (power_service_schedule_display_off(50) != DEVICE_STATUS_OK ||
        !power_retry_hil_wait_for_display_off(1000) ||
        power_service_wake_display_from_remote_control() != DEVICE_STATUS_OK ||
        !power_retry_hil_wait_for_active_display(1000)) {
        ESP_LOGE(TAG, "test: remote wake did not restore selected panel");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }

    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    if (power_lease_service_acquire(DEVICE_POWER_LEASE_OWNER_VOICE_INTERACTION,
                                    &lease) != DEVICE_STATUS_OK ||
        power_service_schedule_display_off(50) != DEVICE_STATUS_OK) {
        power_lease_service_release(lease);
        ESP_LOGE(TAG, "test: cannot set foreground lease deferral");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    vTaskDelay(pdMS_TO_TICKS(150));
    if (platform_power_display_is_off()) {
        power_lease_service_release(lease);
        ESP_LOGE(TAG, "test: foreground lease allowed DISPLAY_OFF");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    power_lease_service_release(lease);
    if (!power_retry_hil_wait_for_display_off(2500)) {
        ESP_LOGE(TAG, "test: foreground lease release did not retry DISPLAY_OFF");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    if (power_service_wake_display_from_user() != DEVICE_STATUS_OK ||
        !power_retry_hil_wait_for_active_display(1000)) {
        ESP_LOGE(TAG, "test: final user wake failed");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }

    /* A final delayed deadline is explicitly cancelled; leave the normal
     * release image service intact rather than testing full startup teardown
     * from inside the boot-time proof.  Lifecycle invalidation itself is
     * separately covered by the Power Lease lifecycle test. */
    if (power_service_schedule_display_off(50) != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "test: cannot arm cancellation deadline");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    if (power_service_cancel_display_off() != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "test: cannot cancel DISPLAY_OFF deadline");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    vTaskDelay(pdMS_TO_TICKS(1200));
    if (platform_power_display_is_off()) {
        ESP_LOGE(TAG, "test: cancelled deadline committed late DISPLAY_OFF");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    ESP_LOGI(TAG, "test: DISPLAY_OFF stale retry HIL passed");
    return DEVICE_STATUS_OK;
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
    SemaphoreHandle_t system_sleep_quiesced = xSemaphoreCreateBinary();
    if (!system_sleep_quiesced) {
        vSemaphoreDelete(timer_callback_drained);
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
        vSemaphoreDelete(system_sleep_quiesced);
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
    s_system_sleep_display_off_scheduler_quiesced = system_sleep_quiesced;
    s_system_sleep_display_off_scheduler_preparing = false;
    taskENTER_CRITICAL(&s_display_off_timer_callback_lock);
    s_display_off_timer_callbacks_inflight = 0;
    s_display_off_timer_callback_admission_open = true;
    taskEXIT_CRITICAL(&s_display_off_timer_callback_lock);
    s_initialized = true;
    s_initializing = false;
    s_stopping = false;
    s_display_off_armed = false;
    s_display_off_retry_pending = false;
    s_display_off_generation = 0;
    s_display_off_due_us = 0;
    s_system_sleep_transition_generation = 0;
    s_system_sleep_transition_target = DEVICE_POWER_STATE_ACTIVE;
    s_system_sleep_transition_phase = DEVICE_POWER_TRANSITION_IDLE;
    s_system_sleep_transition_last_status = DEVICE_STATUS_OK;
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
    SemaphoreHandle_t system_sleep_quiesced = NULL;
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
        system_sleep_quiesced = s_system_sleep_display_off_scheduler_quiesced;
        s_stopping = true;
        s_initialized = false;
        s_display_off_armed = false;
        s_display_off_retry_pending = false;
        ++s_display_off_generation;
        if (!s_display_off_generation) s_display_off_generation = 1;
        s_display_off_due_us = 0;
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
    s_system_sleep_display_off_scheduler_quiesced = NULL;
    s_system_sleep_display_off_scheduler_preparing = false;
    /* The transition mutex has static storage and remains valid. Retaining it
     * ensures a late caller only sees `initialized=false`, never a freed
     * synchronization object. */
    s_stopping = false;
    s_display_off_retry_pending = false;
    s_display_off_due_us = 0;
    taskEXIT_CRITICAL(&s_power_lock);
    if (worker_stopped) vSemaphoreDelete(worker_stopped);
    if (timer_callback_drained) vSemaphoreDelete(timer_callback_drained);
    if (system_sleep_quiesced) vSemaphoreDelete(system_sleep_quiesced);
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
    s_display_off_retry_pending = false;
    ++s_display_off_generation;
    if (!s_display_off_generation) s_display_off_generation = 1;
    s_display_off_due_us = esp_timer_get_time() + (int64_t)idle_after_ms * 1000;
    taskEXIT_CRITICAL(&s_power_lock);
    esp_err_t err = esp_timer_start_once(timer,
                                         (uint64_t)idle_after_ms * 1000u);
    if (err != ESP_OK) {
        taskENTER_CRITICAL(&s_power_lock);
        s_display_off_armed = false;
        s_display_off_due_us = 0;
        taskEXIT_CRITICAL(&s_power_lock);
        xSemaphoreGive(transition_mutex);
        return status_from_esp_err(err);
    }
    xSemaphoreGive(transition_mutex);
    taskENTER_CRITICAL(&s_power_lock);
    TaskHandle_t worker = s_initialized && !s_stopping ? s_display_off_worker_task : NULL;
    taskEXIT_CRITICAL(&s_power_lock);
    if (worker) xTaskNotifyGive(worker);
    return DEVICE_STATUS_OK;
}

device_status_t power_service_cancel_display_off(void) {
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    esp_timer_handle_t timer = s_display_off_timer;
    SemaphoreHandle_t transition_mutex = s_transition_mutex;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!initialized || !timer || !transition_mutex) return DEVICE_STATUS_UNAVAILABLE;
    if (xSemaphoreTake(transition_mutex, power_transition_lock_timeout()) != pdTRUE) {
        ESP_LOGW(TAG, "cannot cancel DISPLAY_OFF deadline: transition busy");
        return DEVICE_STATUS_TIMEOUT;
    }
    if (!transition_is_current_locked(timer)) {
        xSemaphoreGive(transition_mutex);
        return DEVICE_STATUS_BUSY;
    }
    disarm_display_off_locked(timer);
    xSemaphoreGive(transition_mutex);
    taskENTER_CRITICAL(&s_power_lock);
    TaskHandle_t worker = s_initialized && !s_stopping ? s_display_off_worker_task : NULL;
    taskEXIT_CRITICAL(&s_power_lock);
    if (worker) xTaskNotifyGive(worker);
    return DEVICE_STATUS_OK;
}

device_status_t power_service_wake_display_from_user(void) {
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    esp_timer_handle_t timer = s_display_off_timer;
    SemaphoreHandle_t transition_mutex = s_transition_mutex;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!initialized || !timer || !transition_mutex) return DEVICE_STATUS_UNAVAILABLE;
    if (xSemaphoreTake(transition_mutex, power_transition_lock_timeout()) != pdTRUE) {
        ESP_LOGW(TAG, "cannot wake DISPLAY_OFF panel from user contact: transition busy");
        return DEVICE_STATUS_TIMEOUT;
    }
    if (!transition_is_current_locked(timer)) {
        xSemaphoreGive(transition_mutex);
        return DEVICE_STATUS_BUSY;
    }
    /* Do not release this mutex between disarming the deadline and restoring
     * the physical panel.  The timer callback takes the same lock, so either
     * it wins and the contact wakes the resulting DISPLAY_OFF state, or the
     * contact wins and the now-stale timer observes an unarmed deadline. */
    disarm_display_off_locked(timer);
    const device_status_t woke = platform_power_wake_display();
    xSemaphoreGive(transition_mutex);
    taskENTER_CRITICAL(&s_power_lock);
    TaskHandle_t worker = s_initialized && !s_stopping ? s_display_off_worker_task : NULL;
    taskEXIT_CRITICAL(&s_power_lock);
    if (worker) xTaskNotifyGive(worker);
    return woke;
}

device_status_t power_service_wake_display_from_schedule(void) {
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    esp_timer_handle_t timer = s_display_off_timer;
    SemaphoreHandle_t transition_mutex = s_transition_mutex;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!initialized || !timer || !transition_mutex) return DEVICE_STATUS_UNAVAILABLE;
    if (xSemaphoreTake(transition_mutex, power_transition_lock_timeout()) != pdTRUE) {
        ESP_LOGW(TAG, "cannot wake DISPLAY_OFF panel from domain deadline: transition busy");
        return DEVICE_STATUS_TIMEOUT;
    }
    if (!transition_is_current_locked(timer)) {
        xSemaphoreGive(transition_mutex);
        return DEVICE_STATUS_BUSY;
    }
    /* A schedule deadline is allowed to end while a foreground scene has
     * already restored the panel.  In that normal case its only remaining
     * job is to invalidate the old ambient timer; do not report a false
     * failure that would prevent App UI from owning the next idle window. */
    if (!platform_power_display_is_off()) {
        disarm_display_off_locked(timer);
        xSemaphoreGive(transition_mutex);
        taskENTER_CRITICAL(&s_power_lock);
        TaskHandle_t worker = s_initialized && !s_stopping
                                  ? s_display_off_worker_task : NULL;
        taskEXIT_CRITICAL(&s_power_lock);
        if (worker) xTaskNotifyGive(worker);
        return DEVICE_STATUS_OK;
    }
    disarm_display_off_locked(timer);
    const device_status_t woke = platform_power_wake_display();
    xSemaphoreGive(transition_mutex);
    taskENTER_CRITICAL(&s_power_lock);
    TaskHandle_t worker = s_initialized && !s_stopping ? s_display_off_worker_task : NULL;
    taskEXIT_CRITICAL(&s_power_lock);
    if (worker) xTaskNotifyGive(worker);
    return woke;
}

device_status_t power_service_wake_display_from_remote_control(void) {
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    esp_timer_handle_t timer = s_display_off_timer;
    SemaphoreHandle_t transition_mutex = s_transition_mutex;
    taskEXIT_CRITICAL(&s_power_lock);
    if (!initialized || !timer || !transition_mutex) return DEVICE_STATUS_UNAVAILABLE;
    if (xSemaphoreTake(transition_mutex, power_transition_lock_timeout()) != pdTRUE) {
        ESP_LOGW(TAG, "cannot wake DISPLAY_OFF panel from remote control: transition busy");
        return DEVICE_STATUS_TIMEOUT;
    }
    if (!transition_is_current_locked(timer)) {
        xSemaphoreGive(transition_mutex);
        return DEVICE_STATUS_BUSY;
    }
    /* Unlike a physical contact, a remote brightness update is not activity.
     * Do not consume an active panel's pending idle deadline merely because
     * the management plane changed its brightness. */
    if (!platform_power_display_is_off()) {
        xSemaphoreGive(transition_mutex);
        return DEVICE_STATUS_BUSY;
    }
    /* Remote display management has the same panel/timer atomicity as a
     * physical wake, but deliberately carries no input or schedule override
     * semantics.  The UI resumes the normal ambient deadline after success. */
    disarm_display_off_locked(timer);
    const device_status_t woke = platform_power_wake_display();
    xSemaphoreGive(transition_mutex);
    taskENTER_CRITICAL(&s_power_lock);
    TaskHandle_t worker = s_initialized && !s_stopping ? s_display_off_worker_task : NULL;
    taskEXIT_CRITICAL(&s_power_lock);
    if (worker) xTaskNotifyGive(worker);
    return woke;
}

bool power_service_get_snapshot(device_power_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_power_lock);
    bool initialized = s_initialized;
    out_snapshot->display_off_armed = s_display_off_armed || s_display_off_retry_pending;
    taskEXIT_CRITICAL(&s_power_lock);
    /* The board renderer can legitimately wake the physical panel to present
     * an urgent scene.  Ask the adapter for its observed state instead of
     * replaying the last Power Service transition as if it were authoritative. */
    out_snapshot->state = platform_power_display_is_off()
                            ? DEVICE_POWER_STATE_DISPLAY_OFF
                            : DEVICE_POWER_STATE_ACTIVE;
    return initialized;
}

static bool system_sleep_target_is_valid(device_power_state_t target_state) {
    return target_state == DEVICE_POWER_STATE_LIGHT_SLEEP ||
           target_state == DEVICE_POWER_STATE_DEEP_SLEEP;
}

static void system_sleep_transition_publish_locked(
    uint32_t generation, device_power_state_t target_state,
    device_power_transition_phase_t phase, device_status_t status) {
    s_system_sleep_transition_generation = generation;
    s_system_sleep_transition_target = target_state;
    s_system_sleep_transition_phase = phase;
    s_system_sleep_transition_last_status = status;
}

/* Caller holds s_power_lock.  A new request always advances this public
 * generation, including a fail-closed wake-capability preflight.  This keeps
 * a UI/diagnostic consumer from mistaking a new rejected request for the
 * terminal state of a previous request. */
static uint32_t system_sleep_transition_begin_locked(
    device_power_state_t target_state, device_status_t status) {
    uint32_t generation = s_system_sleep_transition_generation + 1u;
    if (generation == 0) generation = 1u;
    system_sleep_transition_publish_locked(generation, target_state,
                                           DEVICE_POWER_TRANSITION_IDLE,
                                           status);
    return generation;
}

device_status_t power_service_request_verified_sleep(device_power_state_t target_state) {
    if (!system_sleep_target_is_valid(target_state)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }

    /* Wake capability is checked before any global admission closes. A board
     * may advertise candidates for engineering/HIL, but only a non-zero
     * verified matrix authorizes the wider PREPARE transaction. */
    /* Allocate the public request identity before preflight.  No physical
     * path is touched here, and the state remains IDLE on a rejected request. */
    uint32_t request_generation = 0;
    taskENTER_CRITICAL(&s_power_lock);
    request_generation = system_sleep_transition_begin_locked(
        target_state, DEVICE_STATUS_UNAVAILABLE);
    taskEXIT_CRITICAL(&s_power_lock);

    device_wake_depth_capability_t wake = {0};
    device_status_t status = wake_service_get_depth_capability(target_state, &wake);
    if (status != DEVICE_STATUS_OK || wake.verified_sources == 0) {
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state,
            DEVICE_POWER_TRANSITION_IDLE,
            status == DEVICE_STATUS_OK ? DEVICE_STATUS_UNAVAILABLE : status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status == DEVICE_STATUS_OK ? DEVICE_STATUS_UNAVAILABLE : status;
    }

    /* No production profile currently reaches this branch: their candidate
     * GPIO/timer paths are deliberately unverified. Still keep the fence
     * explicit so enabling a matrix later cannot bypass the final foreground
     * lease check by accident. */
    uint32_t generation = 0;
    status = power_lease_service_begin_system_sleep_prepare(target_state, &generation);
    if (status != DEVICE_STATUS_OK) {
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(request_generation, target_state,
                                               DEVICE_POWER_TRANSITION_IDLE,
                                               status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }
    taskENTER_CRITICAL(&s_power_lock);
    system_sleep_transition_publish_locked(request_generation, target_state,
                                           DEVICE_POWER_TRANSITION_PREPARING,
                                           DEVICE_STATUS_OK);
    taskEXIT_CRITICAL(&s_power_lock);

    /* Every participant consumes one parent deadline. Park Power's own
     * retained DISPLAY_OFF scheduler first: it is the one worker that can
     * otherwise call Platform Power without crossing Display Service. */
    const int64_t prepare_deadline_us = esp_timer_get_time() +
                                        (int64_t)POWER_SYSTEM_SLEEP_PREPARE_TIMEOUT_MS * 1000;
    uint32_t remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms
                 ? prepare_display_off_scheduler_system_sleep(remaining_ms)
                 : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Audio's prepare blocks a late wake-word start and waits for its
     * profile-owned recognizer safe point; foreground audio is already
     * rejected by the common lease fence. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? audio_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Command cancellation has an independent retained worker that can draw
     * a terminal scene, abort a foreground request and send protocol /cancel.
     * A normal foreground command already owns the common power lease, but
     * fence this residual coordinator explicitly: an accepted cancellation is
     * terminal work that ABORT cannot recreate safely after Display or
     * Connectivity has begun to quiesce. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? command_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Input Service retains scanner/wake hardware below the HAL, but the
     * shared App Intent dispatcher is the only path from an abstract touch or
     * key event to command/meeting/configuration policy. Fence and drain it
     * before any later participant can assume the business plane is quiet. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? app_intent_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Firmware Identity owns a retained USB diagnostic reader.  Park that
     * observer before background policy/Persistence participants so no late
     * host query can format a status snapshot across future physical COMMIT. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? firmware_identity_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Update metadata/reminder handling is a synchronous Persistence consumer.
     * Fence it before Persistence closes NVS admission, allowing only callers
     * already admitted before this marker to complete their durable update
     * state. This is metadata-only: no firmware download/install joins Power. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? update_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Fall Detection is a profile-neutral Motion consumer with its own sample
     * worker and presentation callback. Park it before Persistence closes NVS
     * admission, so a tool that crossed Fall admission can complete its
     * durable mutation before the storage checkpoint is sealed. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? fall_detection_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Provisioning owns a composite SoftAP/HTTP/DNS generation and a terminal
     * post-save restart coordinator.  Neither can be stopped then faithfully
     * recreated by ABORT, so its participant is an admission fence: it closes
     * new portal starts and refuses the wider transaction while either kind of
     * live generation exists.  This must precede Configuration/Persistence,
     * because portal handlers are direct configuration writers. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? provisioning_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Meeting Recovery is a synchronous durable metadata service used by the
     * active meeting domain. Close its public admission before Configuration
     * and Persistence; a late checkpoint must not become a post-COMMIT NVS
     * mutation merely because its worker lives elsewhere. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? meeting_recovery_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        meeting_recovery_service_abort_system_sleep_prepare();
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Ambient owns the weather-cache producer, but Weather Cache itself has
     * independent public load/save admission. Fence that durable boundary
     * before later Persistence closure; Ambient PREPARE may then safely find
     * cache writes rejected rather than crossing the transaction. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? weather_cache_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        weather_cache_service_abort_system_sleep_prepare();
        meeting_recovery_service_abort_system_sleep_prepare();
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Configuration has its own serialized PSRAM-scratch mutation service.
     * Fence it before the root-owned worker and Persistence; provisioning,
     * pairing-code and uplink selection otherwise bypass both storage
     * participants through direct Configuration Service calls. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? configuration_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        configuration_service_abort_system_sleep_prepare();
        weather_cache_service_abort_system_sleep_prepare();
        meeting_recovery_service_abort_system_sleep_prepare();
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* The composition root still owns one internal-stack worker for legacy
     * volume/brightness/pairing-token writes. Quiesce that narrower mutable
     * boundary before the generic Persistence Service closes NVS admission;
     * otherwise a pre-PREPARE gateway pairing could outlive the durable
     * checkpoint even though all shared Persistence clients are quiet. */
    power_service_system_sleep_storage_prepare_t storage_prepare = NULL;
    void *storage_context = NULL;
    taskENTER_CRITICAL(&s_power_lock);
    storage_prepare = s_system_sleep_storage_prepare;
    storage_context = s_system_sleep_storage_context;
    taskEXIT_CRITICAL(&s_power_lock);
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = (storage_prepare && remaining_ms)
                 ? storage_prepare(remaining_ms, storage_context)
                 : DEVICE_STATUS_UNAVAILABLE;
    if (status != DEVICE_STATUS_OK) {
        abort_system_sleep_storage_bridge();
        configuration_service_abort_system_sleep_prepare();
        weather_cache_service_abort_system_sleep_prepare();
        meeting_recovery_service_abort_system_sleep_prepare();
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Persistence joins the same parent deadline only after all known
     * persistence consumers, including the composition-root bridge above,
     * have stopped accepting mutations. Its PREPARE leaves the NVS worker
     * alive but closes new writes and waits for in-flight transactions. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? persistence_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        persistence_service_abort_system_sleep_prepare();
        abort_system_sleep_storage_bridge();
        configuration_service_abort_system_sleep_prepare();
        weather_cache_service_abort_system_sleep_prepare();
        meeting_recovery_service_abort_system_sleep_prepare();
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Ambient cadence is an independent, low-priority scene submitter. Stop
     * its already-running worker first, so Display's subsequent drain observes
     * a closed set rather than racing the next once-per-second publish. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? ambient_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        ambient_service_abort_system_sleep_prepare();
        persistence_service_abort_system_sleep_prepare();
        abort_system_sleep_storage_bridge();
        configuration_service_abort_system_sleep_prepare();
        weather_cache_service_abort_system_sleep_prepare();
        meeting_recovery_service_abort_system_sleep_prepare();
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Display Service owns the single semantic panel submitter. Its prepare
     * closes new scene admission and drains already-accepted work, without
     * claiming a profile-private DMA/scan-out fence or changing panel power.
     * Those physical resume details remain required before any real commit. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? display_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        display_service_abort_system_sleep_prepare();
        ambient_service_abort_system_sleep_prepare();
        persistence_service_abort_system_sleep_prepare();
        abort_system_sleep_storage_bridge();
        configuration_service_abort_system_sleep_prepare();
        weather_cache_service_abort_system_sleep_prepare();
        meeting_recovery_service_abort_system_sleep_prepare();
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Alarm retains its deadline worker and durable queue, but closes tool
     * admission and refuses a currently active/ringing owner. This keeps
     * a future Light Sleep commit from racing an alarm mutation or silently
     * parking an alarm that has no verified RTC/electrical wake path yet. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? alarm_manager_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        alarm_manager_abort_system_sleep_prepare();
        display_service_abort_system_sleep_prepare();
        ambient_service_abort_system_sleep_prepare();
        persistence_service_abort_system_sleep_prepare();
        abort_system_sleep_storage_bridge();
        configuration_service_abort_system_sleep_prepare();
        weather_cache_service_abort_system_sleep_prepare();
        meeting_recovery_service_abort_system_sleep_prepare();
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Sleep Schedule is an independent policy worker and shared Wake Deadline
     * client. Fence its tool/manual-wake/clock/evaluation paths after Display
     * and Alarm have reached their semantic safe points, before Connectivity
     * cancellation can produce a late clock event. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? sleep_schedule_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        sleep_schedule_service_abort_system_sleep_prepare();
        alarm_manager_abort_system_sleep_prepare();
        display_service_abort_system_sleep_prepare();
        ambient_service_abort_system_sleep_prepare();
        persistence_service_abort_system_sleep_prepare();
        abort_system_sleep_storage_bridge();
        configuration_service_abort_system_sleep_prepare();
        weather_cache_service_abort_system_sleep_prepare();
        meeting_recovery_service_abort_system_sleep_prepare();
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Alarm and Schedule each retain their logical client state, but their
     * shared dispatcher still owns an ESP timer and can select callbacks that
     * wake policy workers. Stop that final delivery boundary only after both
     * clients have closed their own admissions. Rollback reopens the dispatcher
     * before Schedule/Alarm are notified, preserving their normal re-evaluate
     * rather than replaying a stale deadline callback. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? wake_deadline_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        wake_deadline_service_abort_system_sleep_prepare();
        sleep_schedule_service_abort_system_sleep_prepare();
        alarm_manager_abort_system_sleep_prepare();
        display_service_abort_system_sleep_prepare();
        ambient_service_abort_system_sleep_prepare();
        persistence_service_abort_system_sleep_prepare();
        abort_system_sleep_storage_bridge();
        configuration_service_abort_system_sleep_prepare();
        weather_cache_service_abort_system_sleep_prepare();
        meeting_recovery_service_abort_system_sleep_prepare();
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Connectivity owns a transport-neutral request fence.  Wi-Fi's concrete
     * HTTP client still lives in the legacy composition root and ML307 stays
     * profile-private, but both publish a common logical in-flight count here.
     * This is only a reversible gateway/network safe point: it neither stops
     * the radio nor proves modem/RTC resume, so a later failure always rolls
     * it back before the request returns. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? connectivity_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        connectivity_service_abort_system_sleep_prepare();
        wake_deadline_service_abort_system_sleep_prepare();
        sleep_schedule_service_abort_system_sleep_prepare();
        alarm_manager_abort_system_sleep_prepare();
        display_service_abort_system_sleep_prepare();
        ambient_service_abort_system_sleep_prepare();
        persistence_service_abort_system_sleep_prepare();
        abort_system_sleep_storage_bridge();
        configuration_service_abort_system_sleep_prepare();
        weather_cache_service_abort_system_sleep_prepare();
        meeting_recovery_service_abort_system_sleep_prepare();
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* Battery Policy has no retained worker, but a synchronous snapshot can
     * still be inside profile telemetry when the selected Power adapter is
     * about to touch an ADC, charger path or rail.  Close and drain that
     * observer at the final common safe point; it remains board-neutral and
     * ABORT simply reopens the same query generation. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? battery_policy_service_prepare_system_sleep(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        battery_policy_service_abort_system_sleep_prepare();
        connectivity_service_abort_system_sleep_prepare();
        wake_deadline_service_abort_system_sleep_prepare();
        sleep_schedule_service_abort_system_sleep_prepare();
        alarm_manager_abort_system_sleep_prepare();
        display_service_abort_system_sleep_prepare();
        ambient_service_abort_system_sleep_prepare();
        persistence_service_abort_system_sleep_prepare();
        abort_system_sleep_storage_bridge();
        configuration_service_abort_system_sleep_prepare();
        weather_cache_service_abort_system_sleep_prepare();
        meeting_recovery_service_abort_system_sleep_prepare();
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* The selected Power profile may prepare its private electrical path only
     * after Wake Service has supplied a non-zero verified matrix. All current
     * release profiles return UNAVAILABLE: their candidate GPIO/timer entries
     * have not passed rail/strapping/touch/modem HIL. Keep this real boundary
     * here so promoting a matrix later cannot skip the Power HAL contract. */
    remaining_ms = system_sleep_prepare_remaining_ms(prepare_deadline_us);
    status = remaining_ms ? platform_power_prepare_verified_sleep(
                                target_state, wake.verified_sources, remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) {
        (void)platform_power_abort_verified_sleep(
            target_state, POWER_SYSTEM_SLEEP_PREPARE_TIMEOUT_MS);
        battery_policy_service_abort_system_sleep_prepare();
        connectivity_service_abort_system_sleep_prepare();
        wake_deadline_service_abort_system_sleep_prepare();
        sleep_schedule_service_abort_system_sleep_prepare();
        alarm_manager_abort_system_sleep_prepare();
        display_service_abort_system_sleep_prepare();
        ambient_service_abort_system_sleep_prepare();
        persistence_service_abort_system_sleep_prepare();
        abort_system_sleep_storage_bridge();
        configuration_service_abort_system_sleep_prepare();
        weather_cache_service_abort_system_sleep_prepare();
        meeting_recovery_service_abort_system_sleep_prepare();
        provisioning_service_abort_system_sleep_prepare();
        fall_detection_service_abort_system_sleep_prepare();
        update_service_abort_system_sleep_prepare();
        firmware_identity_abort_system_sleep_prepare();
        app_intent_service_abort_system_sleep_prepare();
        command_service_abort_system_sleep_prepare();
        audio_service_abort_system_sleep_prepare();
        abort_display_off_scheduler_system_sleep_prepare();
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
            status);
        taskEXIT_CRITICAL(&s_power_lock);
        power_lease_service_end_system_sleep_prepare(generation);
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE, status);
        taskEXIT_CRITICAL(&s_power_lock);
        return status;
    }

    /* A final fence check is deliberately adjacent to COMMIT.  It protects
     * future profile implementations from entering MCU sleep after the wider
     * transaction has become stale.  All release profiles currently return
     * UNAVAILABLE from Platform Power COMMIT; their wake matrix/electrical
     * resume HIL remains incomplete.  Keeping the real COMMIT -> RESUME
     * sequence here means a later profile cannot bypass the common lease,
     * participant rollback or transition diagnostics merely by adding an
     * physical-sleep call below its adapter. */
    const bool current = power_lease_service_system_sleep_prepare_is_current(
        target_state, generation);
    taskENTER_CRITICAL(&s_power_lock);
    system_sleep_transition_publish_locked(
        request_generation, target_state,
        current ? DEVICE_POWER_TRANSITION_COMMITTING : DEVICE_POWER_TRANSITION_ROLLING_BACK,
        current ? DEVICE_STATUS_OK : DEVICE_STATUS_BUSY);
    taskEXIT_CRITICAL(&s_power_lock);

    device_status_t terminal_status = DEVICE_STATUS_BUSY;
    if (current) {
        /* A successful physical sleep can return after an alarm/timer hours
         * later. The PREPARE deadline only proves the shared world was quiet
         * before entry; using its remainder here would turn a correct long
         * sleep into a timeout. The profile receives a separate pre-entry
         * budget, and the fresh resume budget below starts only after wake. */
        const uint32_t pre_commit_remaining_ms =
            system_sleep_prepare_remaining_ms(prepare_deadline_us);
        terminal_status = pre_commit_remaining_ms
                              ? platform_power_commit_verified_sleep(
                                    target_state, wake.verified_sources,
                                    POWER_SYSTEM_SLEEP_COMMIT_ENTRY_TIMEOUT_MS)
                              : DEVICE_STATUS_TIMEOUT;
        /* A profile may have armed a subset of private electrical state before
         * detecting that it cannot commit. RESUME is therefore required after
         * every COMMIT attempt, not only a successful wake return. It has no
         * authority to reopen shared participants; that happens below only
         * after the profile has restored its own safe baseline. */
        taskENTER_CRITICAL(&s_power_lock);
        system_sleep_transition_publish_locked(
            request_generation, target_state, DEVICE_POWER_TRANSITION_RESUMING,
            terminal_status);
        taskEXIT_CRITICAL(&s_power_lock);
        const device_status_t resume_status =
            platform_power_resume_verified_sleep(
                target_state, POWER_SYSTEM_SLEEP_RESUME_TIMEOUT_MS);
        if (terminal_status == DEVICE_STATUS_OK) terminal_status = resume_status;
    }

    taskENTER_CRITICAL(&s_power_lock);
    system_sleep_transition_publish_locked(
        request_generation, target_state, DEVICE_POWER_TRANSITION_ROLLING_BACK,
        terminal_status);
    taskEXIT_CRITICAL(&s_power_lock);
    /* Do not reuse the exhausted PREPARE deadline for a profile-private
     * cleanup.  ABORT is the required counterpart to every successful or
     * partial electrical PREPARE/COMMIT attempt, including a COMMIT that
     * returns after the common deadline has expired. */
    (void)platform_power_abort_verified_sleep(
        target_state, POWER_SYSTEM_SLEEP_ROLLBACK_TIMEOUT_MS);
    battery_policy_service_abort_system_sleep_prepare();
    connectivity_service_abort_system_sleep_prepare();
    wake_deadline_service_abort_system_sleep_prepare();
    sleep_schedule_service_abort_system_sleep_prepare();
    alarm_manager_abort_system_sleep_prepare();
    display_service_abort_system_sleep_prepare();
    ambient_service_abort_system_sleep_prepare();
    persistence_service_abort_system_sleep_prepare();
    abort_system_sleep_storage_bridge();
    configuration_service_abort_system_sleep_prepare();
    weather_cache_service_abort_system_sleep_prepare();
    meeting_recovery_service_abort_system_sleep_prepare();
    provisioning_service_abort_system_sleep_prepare();
    fall_detection_service_abort_system_sleep_prepare();
    update_service_abort_system_sleep_prepare();
    firmware_identity_abort_system_sleep_prepare();
    app_intent_service_abort_system_sleep_prepare();
    command_service_abort_system_sleep_prepare();
    audio_service_abort_system_sleep_prepare();
    abort_display_off_scheduler_system_sleep_prepare();
    power_lease_service_end_system_sleep_prepare(generation);
    taskENTER_CRITICAL(&s_power_lock);
    system_sleep_transition_publish_locked(
        request_generation, target_state, DEVICE_POWER_TRANSITION_IDLE,
        terminal_status);
    taskEXIT_CRITICAL(&s_power_lock);
    return terminal_status;
}

bool power_service_get_transition_snapshot(
    device_power_transition_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_power_lock);
    *out_snapshot = (device_power_transition_snapshot_t){
        .struct_size = sizeof(*out_snapshot),
        .abi_version = DEVICE_POWER_TRANSITION_ABI_VERSION,
        .generation = s_system_sleep_transition_generation,
        .target_state = s_system_sleep_transition_target,
        .phase = s_system_sleep_transition_phase,
        .last_status = s_system_sleep_transition_last_status,
    };
    const bool initialized = s_initialized && !s_stopping;
    taskEXIT_CRITICAL(&s_power_lock);
    return initialized;
}

bool power_service_get_telemetry(device_power_telemetry_t *out_telemetry) {
    return platform_power_get_telemetry(out_telemetry);
}
