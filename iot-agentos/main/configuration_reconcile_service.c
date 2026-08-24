#include "configuration_reconcile_service.h"

#include <limits.h>

#include "esp_err.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "esp_timer.h"

#include "configuration_runtime_override_service.h"
#include "configuration_reconcile_retry_policy.h"
#include "audio_service.h"
#include "display_service.h"
#include "presentation/scene_presenter.h"
#include "services/audio_arbitration_service.h"

static StaticSemaphore_t s_mutex_storage;
static SemaphoreHandle_t s_mutex;
static portMUX_TYPE s_state_lock = portMUX_INITIALIZER_UNLOCKED;
static configuration_apply_state_t s_apply_state;
static bool s_initialized;
static bool s_stopping;
static bool s_reconciling;
static device_status_t s_last_status = DEVICE_STATUS_UNAVAILABLE;
static configuration_reconcile_authorization_current_t s_authorization_current;
static void *s_authorization_context;
/* A retained retry must belong to the same external authorization epoch as
 * the reconcile pass that armed it. It is a copied value; no Gateway/HTTP or
 * board handle is retained by Configuration. */
static configuration_reconcile_authorization_t s_retry_authorization;
static bool s_retry_authorization_valid;
static bool s_retry_armed;
static uint32_t s_retry_attempt;
/* esp_timer_stop() prevents a future dispatch but cannot retract a callback
 * already selected by the timer task. Keep the absolute deadline owned by the
 * coordinator so an old callback cannot consume/reset a newly rearmed retry
 * generation. Zero means no retry is currently armed. */
static uint64_t s_retry_due_us;
static uint32_t s_retry_generation;
static uint32_t s_retry_delivered_generation;
#define CONFIGURATION_RECONCILE_EXPIRY_TASK_STACK_WORDS 3072u
#define CONFIGURATION_RECONCILE_EXPIRY_TASK_PRIORITY (tskIDLE_PRIORITY + 3u)
static StaticTask_t s_expiry_task_storage;
static StackType_t s_expiry_task_stack[CONFIGURATION_RECONCILE_EXPIRY_TASK_STACK_WORDS];
static TaskHandle_t s_expiry_task;
static StaticSemaphore_t s_expiry_stopped_storage;
static SemaphoreHandle_t s_expiry_stopped;
static bool s_expiry_stop_requested;
static esp_timer_handle_t s_expiry_timer;
static esp_timer_handle_t s_retry_timer;

#define CONFIGURATION_RECONCILE_NOTIFY_EXPIRY (1u << 0)
#define CONFIGURATION_RECONCILE_NOTIFY_RETRY  (1u << 1)

/* Timer ownership is serialized by s_mutex.  The public wrapper is for
 * mutation/worker paths that are outside a reconciliation operation; the
 * `_under_mutex` variant is used by the operation owner itself. */
static void rearm_expiry_timer_under_mutex(void);
static void rearm_expiry_timer(void);
static bool schedule_or_cancel_retry(device_status_t status,
                                     configuration_reconcile_service_reason_t reason,
                                     const configuration_reconcile_authorization_t *authorization);
static device_status_t reconcile_internal(
    configuration_reconcile_service_reason_t reason,
    const configuration_reconcile_authorization_t *authorization);

static bool retryable_status(device_status_t status) {
    return status == DEVICE_STATUS_BUSY || status == DEVICE_STATUS_TIMEOUT ||
           status == DEVICE_STATUS_IO_ERROR || status == DEVICE_STATUS_INTERNAL_ERROR;
}

static bool authorization_shape_is_valid(
    const configuration_reconcile_authorization_t *authorization) {
    return authorization &&
           authorization->struct_size == sizeof(*authorization) &&
           authorization->abi_version == CONFIGURATION_RECONCILE_AUTHORIZATION_ABI_VERSION &&
           authorization->authority_kind != 0u && authorization->generation != 0u &&
           authorization->required_permissions != 0u;
}

static bool authorization_current(
    const configuration_reconcile_authorization_t *authorization) {
    if (!authorization) return true;
    configuration_reconcile_authorization_current_t validator = NULL;
    void *context = NULL;
    taskENTER_CRITICAL(&s_state_lock);
    validator = s_authorization_current;
    context = s_authorization_context;
    taskEXIT_CRITICAL(&s_state_lock);
    return authorization_shape_is_valid(authorization) && validator &&
           validator(authorization, context);
}

void configuration_reconcile_service_set_authorization_validator(
    configuration_reconcile_authorization_current_t validator, void *context) {
    taskENTER_CRITICAL(&s_state_lock);
    s_authorization_current = validator;
    s_authorization_context = context;
    taskEXIT_CRITICAL(&s_state_lock);
}

static void expiry_timer_callback(void *unused) {
    (void)unused;
    taskENTER_CRITICAL(&s_state_lock);
    const TaskHandle_t task = (s_initialized && !s_stopping &&
                               !s_expiry_stop_requested) ? s_expiry_task : NULL;
    taskEXIT_CRITICAL(&s_state_lock);
    if (task) (void)xTaskNotify(task, CONFIGURATION_RECONCILE_NOTIFY_EXPIRY, eSetBits);
}

static void retry_timer_callback(void *unused) {
    /* The callback has exactly the same limitation as expiry: it can only
     * notify the retained owner. No Configuration read or consumer call is
     * legal from esp_timer task context. */
    (void)unused;
    const int64_t timer_now_us = esp_timer_get_time();
    const uint64_t now_us = timer_now_us > 0 ? (uint64_t)timer_now_us : 0u;
    taskENTER_CRITICAL(&s_state_lock);
    const bool due = s_retry_armed && s_retry_due_us != 0u &&
                     now_us >= s_retry_due_us;
    if (due) s_retry_delivered_generation = s_retry_generation;
    const TaskHandle_t task = (due && s_initialized && !s_stopping &&
                               !s_expiry_stop_requested) ? s_expiry_task : NULL;
    taskEXIT_CRITICAL(&s_state_lock);
    if (task) (void)xTaskNotify(task, CONFIGURATION_RECONCILE_NOTIFY_RETRY, eSetBits);
}

static void expiry_task(void *unused) {
    (void)unused;
    for (;;) {
        uint32_t notifications = 0u;
        (void)xTaskNotifyWait(0u, UINT32_MAX, &notifications, portMAX_DELAY);
        taskENTER_CRITICAL(&s_state_lock);
        const bool stop = s_expiry_stop_requested;
        const bool retry_current =
            (notifications & CONFIGURATION_RECONCILE_NOTIFY_RETRY) != 0u &&
            s_retry_armed &&
            s_retry_delivered_generation == s_retry_generation;
        if (retry_current) {
            s_retry_armed = false;
            s_retry_due_us = 0u;
        }
        taskEXIT_CRITICAL(&s_state_lock);
        if (stop) break;
        if ((notifications & CONFIGURATION_RECONCILE_NOTIFY_EXPIRY) == 0u &&
            !retry_current) {
            /* A stopped/rearmed esp_timer callback can already have posted
             * its bit. Its delivery generation no longer identifies the
             * currently armed retry, so consume it without perturbing the
             * new curve or touching any Configuration consumer. */
            rearm_expiry_timer();
            continue;
        }
        const configuration_reconcile_service_reason_t reason =
            (notifications & CONFIGURATION_RECONCILE_NOTIFY_EXPIRY)
                ? CONFIGURATION_RECONCILE_REASON_RUNTIME_OVERRIDE_EXPIRY
                : CONFIGURATION_RECONCILE_REASON_RETRY;
        /* A retry is a continuation of the authorization epoch which admitted
         * its original durable policy. Copy it while the retry generation is
         * still owned here; the generic Configuration layer never retains a
         * caller pointer, Gateway handle, or board object. Runtime-expiry work
         * is local policy restoration and deliberately has no external epoch. */
        configuration_reconcile_authorization_t retry_authorization = {0};
        bool retry_authorization_valid = false;
        if (reason == CONFIGURATION_RECONCILE_REASON_RETRY) {
            taskENTER_CRITICAL(&s_state_lock);
            retry_authorization_valid = s_retry_authorization_valid;
            if (retry_authorization_valid) {
                retry_authorization = s_retry_authorization;
            }
            taskEXIT_CRITICAL(&s_state_lock);
        }
        (void)reconcile_internal(reason, retry_authorization_valid
                                             ? &retry_authorization : NULL);
        /* A BUSY result means a serialized reconciliation already owns the
         * newest effective snapshot. That owner publishes its status and
         * timer decision before releasing the same mutex, so this worker must
         * not race it by independently stop/starting the retry timer. */
        rearm_expiry_timer();
    }
    taskENTER_CRITICAL(&s_state_lock);
    s_expiry_task = NULL;
    taskEXIT_CRITICAL(&s_state_lock);
    if (s_expiry_stopped) (void)xSemaphoreGive(s_expiry_stopped);
    vTaskDelete(NULL);
}

static void rearm_expiry_timer_under_mutex(void) {
    if (!s_expiry_timer) return;
    taskENTER_CRITICAL(&s_state_lock);
    const bool admitted = s_initialized && !s_stopping &&
                          !s_expiry_stop_requested;
    taskEXIT_CRITICAL(&s_state_lock);
    if (!admitted) return;
    uint64_t expiry_ms = 0u;
    const device_status_t status =
        configuration_service_next_runtime_override_expiry_ms(&expiry_ms);
    taskENTER_CRITICAL(&s_state_lock);
    const bool still_admitted = s_initialized && !s_stopping &&
                                !s_expiry_stop_requested;
    taskEXIT_CRITICAL(&s_state_lock);
    if (!still_admitted) return;
    (void)esp_timer_stop(s_expiry_timer);
    if (status != DEVICE_STATUS_OK || expiry_ms == 0u) return;
    const int64_t now_us = esp_timer_get_time();
    const uint64_t now_ms = now_us > 0 ? (uint64_t)now_us / 1000u : 0u;
    /* Store resolution owns expiry removal. The timer merely schedules that
     * one owner; retain a 1ms minimum so an expired record never creates a
     * zero-delay callback loop. */
    const uint64_t delay_ms = expiry_ms > now_ms ? expiry_ms - now_ms : 1u;
    const uint64_t delay_us = delay_ms > UINT64_MAX / 1000u
                                  ? UINT64_MAX : delay_ms * 1000u;
    (void)esp_timer_start_once(s_expiry_timer, delay_us);
}

static void rearm_expiry_timer(void) {
    if (!s_mutex) return;
    /* This wrapper deliberately does not wait: an active reconcile will
     * compute and publish the newest deadline before releasing s_mutex. A
     * later mutation/retry will retry its own rearm path rather than race it. */
    if (xSemaphoreTake(s_mutex, 0) != pdTRUE) return;
    rearm_expiry_timer_under_mutex();
    xSemaphoreGive(s_mutex);
}

static bool schedule_or_cancel_retry(device_status_t status,
                                     configuration_reconcile_service_reason_t reason,
                                     const configuration_reconcile_authorization_t *authorization) {
    /* The caller owns the serialized reconciliation mutex. esp_timer stop/
     * start and the associated generation publication must be one operation:
     * otherwise two completed reconciliations can interleave after releasing
     * that mutex and leave a timer armed for the older result. */
    if (!s_retry_timer) return !retryable_status(status);
    /* A caller who explicitly drives policy owns the new generation. Its
     * result may begin a fresh retry curve; a successful reconcile or a
     * non-retryable authorization/value error clears any stale retry. */
    if (!retryable_status(status)) {
        (void)esp_timer_stop(s_retry_timer);
        taskENTER_CRITICAL(&s_state_lock);
        if (++s_retry_generation == 0u) ++s_retry_generation;
        s_retry_armed = false;
        s_retry_attempt = 0u;
        s_retry_due_us = 0u;
        s_retry_delivered_generation = 0u;
        s_retry_authorization_valid = false;
        s_retry_authorization = (configuration_reconcile_authorization_t){0};
        taskEXIT_CRITICAL(&s_state_lock);
        return true;
    }

    taskENTER_CRITICAL(&s_state_lock);
    const bool admitted = s_initialized && !s_stopping;
    uint32_t next_attempt = s_retry_attempt;
    if (reason != CONFIGURATION_RECONCILE_REASON_RETRY || next_attempt == 0u) {
        next_attempt = 1u;
    } else if (next_attempt != UINT32_MAX) {
        ++next_attempt;
    }
    uint32_t delay_ms = 0u;
    if (!configuration_reconcile_retry_delay_ms(next_attempt, &delay_ms)) {
        s_retry_armed = false;
        s_retry_due_us = 0u;
        s_retry_authorization_valid = false;
        s_retry_authorization = (configuration_reconcile_authorization_t){0};
        taskEXIT_CRITICAL(&s_state_lock);
        return false;
    }
    const int64_t now_us = esp_timer_get_time();
    const uint64_t delay_us = (uint64_t)delay_ms * 1000u;
    const uint64_t base_us = now_us > 0 ? (uint64_t)now_us : 0u;
    const uint64_t due_us = UINT64_MAX - base_us < delay_us
                                ? UINT64_MAX : base_us + delay_us;
    if (++s_retry_generation == 0u) ++s_retry_generation;
    s_retry_attempt = next_attempt;
    s_retry_armed = admitted;
    s_retry_due_us = admitted ? due_us : 0u;
    s_retry_delivered_generation = 0u;
    s_retry_authorization_valid = authorization != NULL;
    s_retry_authorization = authorization
                                ? *authorization
                                : (configuration_reconcile_authorization_t){0};
    taskEXIT_CRITICAL(&s_state_lock);
    if (!admitted) return false;
    (void)esp_timer_stop(s_retry_timer);
    if (esp_timer_start_once(s_retry_timer, delay_us) == ESP_OK) return true;
    /* A retryable consumer result without a retained retry timer is degraded,
     * not armed. Do not keep a fictitious due deadline that a callback can
     * later interpret as real retry work. The next explicit policy revision
     * may establish a new bounded curve. */
    taskENTER_CRITICAL(&s_state_lock);
    if (s_retry_generation != 0u) {
        s_retry_armed = false;
        s_retry_due_us = 0u;
        s_retry_delivered_generation = 0u;
        s_retry_authorization_valid = false;
        s_retry_authorization = (configuration_reconcile_authorization_t){0};
    }
    taskEXIT_CRITICAL(&s_state_lock);
    return false;
}

static configuration_apply_observation_t observation_from_status(
    device_status_t status) {
    if (status == DEVICE_STATUS_OK) return CONFIGURATION_APPLY_OBSERVATION_APPLIED;
    /* A timeout/IO/internal error can occur after a panel/codec has crossed
     * its private side-effect boundary. Never retain a prior observed value
     * as proof in those cases. */
    switch (status) {
        case DEVICE_STATUS_TIMEOUT:
        case DEVICE_STATUS_IO_ERROR:
        case DEVICE_STATUS_INTERNAL_ERROR:
            return CONFIGURATION_APPLY_OBSERVATION_UNKNOWN;
        default:
            return CONFIGURATION_APPLY_OBSERVATION_FAILED;
    }
}

static configuration_apply_observation_t observation_from_volume_ack(
    device_status_t call_status, uint8_t expected_percent) {
    if (call_status != DEVICE_STATUS_OK) return observation_from_status(call_status);
    audio_service_output_volume_state_t state = {0};
    if (!audio_service_get_output_volume_state(&state) ||
        state.struct_size != sizeof(state) ||
        state.abi_version != AUDIO_SERVICE_OUTPUT_VOLUME_STATE_ABI_VERSION ||
        state.last_status != DEVICE_STATUS_OK || !state.known ||
        state.percent != expected_percent) {
        return CONFIGURATION_APPLY_OBSERVATION_UNKNOWN;
    }
    return CONFIGURATION_APPLY_OBSERVATION_APPLIED;
}

static configuration_apply_observation_t observation_from_brightness_ack(
    device_status_t call_status, uint8_t expected_percent) {
    if (call_status != DEVICE_STATUS_OK) return observation_from_status(call_status);
    display_service_brightness_state_t state = {0};
    if (!display_service_get_brightness_state(&state) ||
        state.struct_size != sizeof(state) ||
        state.abi_version != DISPLAY_SERVICE_BRIGHTNESS_STATE_ABI_VERSION ||
        state.last_status != DEVICE_STATUS_OK || !state.known ||
        state.percent != expected_percent) {
        return CONFIGURATION_APPLY_OBSERVATION_UNKNOWN;
    }
    return CONFIGURATION_APPLY_OBSERVATION_APPLIED;
}

static configuration_apply_observation_t observation_from_idle_policy_ack(
    device_status_t call_status, uint32_t expected_ms) {
    if (call_status != DEVICE_STATUS_OK) return observation_from_status(call_status);
    scene_display_off_idle_policy_state_t state = {0};
    if (!scene_presenter_get_display_off_idle_policy_state(&state) ||
        state.struct_size != sizeof(state) ||
        state.abi_version != SCENE_DISPLAY_OFF_IDLE_POLICY_STATE_ABI_VERSION ||
        state.last_status != DEVICE_STATUS_OK || !state.known ||
        state.idle_after_ms != expected_ms) {
        return CONFIGURATION_APPLY_OBSERVATION_UNKNOWN;
    }
    /* A foreground scene may deliberately defer a valid idle policy until it
     * returns to ambient. If this reconcile did request an immediate deadline,
     * require the Power scheduler's readback instead of treating a stored UI
     * timeout as proof that the deadline exists. */
    if (state.schedule_required &&
        (!state.schedule_known || !state.schedule_armed ||
         state.schedule_last_status != DEVICE_STATUS_OK)) {
        return CONFIGURATION_APPLY_OBSERVATION_UNKNOWN;
    }
    return CONFIGURATION_APPLY_OBSERVATION_APPLIED;
}

static device_status_t status_from_observation(
    device_status_t call_status, configuration_apply_observation_t observation) {
    if (call_status != DEVICE_STATUS_OK) return call_status;
    /* An OK consumer call is not a completed configuration apply until its
     * own service/UI receipt identifies the same desired value. Missing or
     * contradictory receipt can reflect a late/stale completion after an
     * external side-effect boundary, so surface it as retryable uncertainty
     * rather than returning success and cancelling the retained retry timer. */
    return observation == CONFIGURATION_APPLY_OBSERVATION_APPLIED
               ? DEVICE_STATUS_OK
               : DEVICE_STATUS_INTERNAL_ERROR;
}

static void retain_first_failure(device_status_t *first_failure,
                                 device_status_t candidate) {
    if (first_failure && *first_failure == DEVICE_STATUS_OK &&
        candidate != DEVICE_STATUS_OK) {
        *first_failure = candidate;
    }
}

static device_status_t reconcile_one_effective_snapshot(
    const configuration_effective_revisioned_snapshot_t *effective,
    configuration_reconcile_service_reason_t reason,
    const configuration_reconcile_authorization_t *authorization) {
    if (!effective) return DEVICE_STATUS_INTERNAL_ERROR;
    if (!authorization_current(authorization)) return DEVICE_STATUS_UNAVAILABLE;
    /* The external consumers below may block or cross a hardware side-effect
     * boundary, so never hold the spinlock while invoking them. Work on one
     * local revision-bound observation and publish it as a whole only after
     * this serialized reconciliation has its complete result; readers then
     * cannot observe a half-written volume/brightness/idle combination. */
    taskENTER_CRITICAL(&s_state_lock);
    configuration_apply_state_t apply_state = s_apply_state;
    taskEXIT_CRITICAL(&s_state_lock);
    const bool requested_output_volume =
        reason != CONFIGURATION_RECONCILE_REASON_BOOT_RESTORE ||
        effective->snapshot.output_volume_saved;
    /* Saved zero brightness is a live command, not a boot default. Boot must
     * retain a local visible recovery surface, so make that deliberate non-
     * apply explicit rather than leaving a falsely PENDING desired value. */
    const bool requested_display_brightness =
        !(reason == CONFIGURATION_RECONCILE_REASON_BOOT_RESTORE &&
          (!effective->snapshot.display_brightness_saved ||
           effective->snapshot.display_brightness == 0u));
    const bool requested_screen_sleep = effective->snapshot.screen_sleep_seconds_saved;
    if (!configuration_apply_state_begin_with_requirements(
            &apply_state, effective, requested_output_volume,
            requested_display_brightness, requested_screen_sleep)) {
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    /* A retry operates on an existing desired generation. Use its retained
     * requirements rather than re-deriving them from the retry ingress: a
     * BOOT_RESTORE generation that deliberately kept a visible brightness=0
     * recovery surface must remain a non-apply until a new revision arrives. */
    const bool apply_output_volume =
        configuration_apply_state_output_volume_needs_apply(&apply_state);
    const bool apply_display_brightness =
        configuration_apply_state_display_brightness_needs_apply(&apply_state);
    const bool apply_screen_sleep =
        configuration_apply_state_screen_sleep_seconds_needs_apply(&apply_state);
    const uint64_t durable_revision = effective->durable_revision;
    const uint64_t override_revision = effective->runtime_override_revision;

    device_status_t first_failure = DEVICE_STATUS_OK;
    if (apply_output_volume) {
        if (!authorization_current(authorization)) return DEVICE_STATUS_UNAVAILABLE;
        const device_status_t volume_status =
            audio_arbitration_set_output_volume(effective->snapshot.output_volume);
        const configuration_apply_observation_t volume_observation =
            observation_from_volume_ack(volume_status, effective->snapshot.output_volume);
        const bool recorded = configuration_apply_state_record_output_volume(
            &apply_state, durable_revision, override_revision, volume_observation);
        if (!recorded) retain_first_failure(&first_failure, DEVICE_STATUS_INTERNAL_ERROR);
        retain_first_failure(&first_failure,
                             status_from_observation(volume_status, volume_observation));
    }

    /* A persisted zero brightness is a live backlight command. On cold boot
     * retain a visible profile-owned recovery surface instead of replaying it
     * as black; that omission is explicit desired-vs-observed evidence. */
    if (apply_display_brightness) {
        if (!authorization_current(authorization)) return DEVICE_STATUS_UNAVAILABLE;
        const device_status_t brightness_status = scene_presenter_apply_remote_brightness(
            effective->snapshot.display_brightness);
        const configuration_apply_observation_t brightness_observation =
            observation_from_brightness_ack(brightness_status,
                                            effective->snapshot.display_brightness);
        const bool recorded = configuration_apply_state_record_display_brightness(
            &apply_state, durable_revision, override_revision, brightness_observation);
        if (!recorded) retain_first_failure(&first_failure, DEVICE_STATUS_INTERNAL_ERROR);
        retain_first_failure(
            &first_failure,
            status_from_observation(brightness_status, brightness_observation));
    }

    /* This acknowledges that the common UI accepted an ambient idle policy.
     * It does not claim a physical panel transition, whose timer/controller
     * work remains owned below the Display/Power HAL. An unsaved value leaves
     * the profile default untouched and is intentionally not required for
     * convergence. */
    if (apply_screen_sleep) {
        if (!authorization_current(authorization)) return DEVICE_STATUS_UNAVAILABLE;
        const device_status_t idle_status =
            scene_presenter_apply_display_off_idle_policy(
                effective->snapshot.screen_sleep_seconds * 1000u);
        const configuration_apply_observation_t idle_observation =
            observation_from_idle_policy_ack(
                idle_status, effective->snapshot.screen_sleep_seconds * 1000u);
        const bool recorded = configuration_apply_state_record_screen_sleep_seconds(
            &apply_state, durable_revision, override_revision, idle_observation);
        if (!recorded) retain_first_failure(&first_failure, DEVICE_STATUS_INTERNAL_ERROR);
        retain_first_failure(&first_failure,
                             status_from_observation(idle_status, idle_observation));
    }
    /* Do not publish a success observation after revocation raced an external
     * consumer completion. The desired durable value remains intact, but this
     * authorization epoch may not start another effect or retain a retry. */
    if (!authorization_current(authorization)) return DEVICE_STATUS_UNAVAILABLE;
    taskENTER_CRITICAL(&s_state_lock);
    s_apply_state = apply_state;
    taskEXIT_CRITICAL(&s_state_lock);
    return first_failure;
}

device_status_t configuration_reconcile_service_init(void) {
    /* Static allocation has no failure path and must not run inside a critical
     * section: FreeRTOS mutex construction may touch scheduler/allocator
     * internals. Startup is the only initializer; subsequent calls merely
     * reopen this retained owner after a controlled lifecycle restart. */
    if (!s_mutex) s_mutex = xSemaphoreCreateMutexStatic(&s_mutex_storage);
    if (!s_expiry_stopped) s_expiry_stopped = xSemaphoreCreateBinaryStatic(&s_expiry_stopped_storage);
    if (!s_expiry_timer) {
        const esp_timer_create_args_t args = {
            .callback = expiry_timer_callback,
            .arg = NULL,
            .dispatch_method = ESP_TIMER_TASK,
            .name = "config_override_expiry",
        };
        if (esp_timer_create(&args, &s_expiry_timer) != ESP_OK) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    if (!s_retry_timer) {
        const esp_timer_create_args_t args = {
            .callback = retry_timer_callback,
            .arg = NULL,
            .dispatch_method = ESP_TIMER_TASK,
            .name = "config_reconcile_retry",
        };
        if (esp_timer_create(&args, &s_retry_timer) != ESP_OK) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    taskENTER_CRITICAL(&s_state_lock);
    if (!s_mutex || !s_expiry_stopped || !s_expiry_timer || !s_retry_timer) {
        taskEXIT_CRITICAL(&s_state_lock);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    /* A timed-out deinit intentionally leaves admission closed while its
     * retained worker/mutex may still be unwinding. Do not let a fresh init
     * reopen that old generation; a caller must complete the same deinit
     * cleanup first. A fully completed deinit sets initialized=false and can
     * be restarted below. */
    if (s_initialized && s_stopping) {
        taskEXIT_CRITICAL(&s_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    if (!s_initialized) {
        configuration_apply_state_init(&s_apply_state);
        s_last_status = DEVICE_STATUS_UNAVAILABLE;
        s_retry_armed = false;
        s_retry_attempt = 0u;
        s_retry_due_us = 0u;
        s_retry_generation = 0u;
        s_retry_delivered_generation = 0u;
        s_retry_authorization_valid = false;
        s_retry_authorization = (configuration_reconcile_authorization_t){0};
    }
    s_stopping = false;
    s_initialized = true;
    s_expiry_stop_requested = false;
    const bool need_expiry_task = !s_expiry_task;
    taskEXIT_CRITICAL(&s_state_lock);
    if (need_expiry_task) {
        TaskHandle_t created = xTaskCreateStatic(
            expiry_task, "config_override_expiry",
            CONFIGURATION_RECONCILE_EXPIRY_TASK_STACK_WORDS, NULL,
            CONFIGURATION_RECONCILE_EXPIRY_TASK_PRIORITY, s_expiry_task_stack,
            &s_expiry_task_storage);
        if (!created) {
            taskENTER_CRITICAL(&s_state_lock);
            s_initialized = false;
            s_stopping = true;
            taskEXIT_CRITICAL(&s_state_lock);
            return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        }
        taskENTER_CRITICAL(&s_state_lock);
        /* Startup is the sole creator. If a future lifecycle race ever
         * publishes another task, delete this unadmitted generation rather
         * than overwrite its identity. */
        const bool accept_created = s_initialized && !s_stopping && !s_expiry_task;
        if (accept_created) s_expiry_task = created;
        taskEXIT_CRITICAL(&s_state_lock);
        if (!accept_created) {
            vTaskDelete(created);
            return DEVICE_STATUS_BUSY;
        }
    }
    rearm_expiry_timer();
    return DEVICE_STATUS_OK;
}

device_status_t configuration_reconcile_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0u || !s_mutex) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_state_lock);
    if (!s_initialized || s_stopping) {
        taskEXIT_CRITICAL(&s_state_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    s_stopping = true;
    s_expiry_stop_requested = true;
    const TaskHandle_t expiry_task_handle = s_expiry_task;
    taskEXIT_CRITICAL(&s_state_lock);
    if (s_expiry_timer) (void)esp_timer_stop(s_expiry_timer);
    if (s_retry_timer) (void)esp_timer_stop(s_retry_timer);
    if (expiry_task_handle) {
        while (xSemaphoreTake(s_expiry_stopped, 0) == pdTRUE) {}
        (void)xTaskNotify(expiry_task_handle, CONFIGURATION_RECONCILE_NOTIFY_EXPIRY,
                          eSetBits);
        if (xSemaphoreTake(s_expiry_stopped, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
            return DEVICE_STATUS_TIMEOUT;
        }
    }
    if (xSemaphoreTake(s_mutex, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_state_lock);
    s_initialized = false;
    s_reconciling = false;
    s_retry_authorization_valid = false;
    s_retry_authorization = (configuration_reconcile_authorization_t){0};
    taskEXIT_CRITICAL(&s_state_lock);
    xSemaphoreGive(s_mutex);
    return DEVICE_STATUS_OK;
}

static device_status_t reconcile_internal(
    configuration_reconcile_service_reason_t reason,
    const configuration_reconcile_authorization_t *authorization) {
    if (reason > CONFIGURATION_RECONCILE_REASON_RUNTIME_OVERRIDE_EXPIRY || !s_mutex) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    taskENTER_CRITICAL(&s_state_lock);
    const bool admitted = s_initialized && !s_stopping && !s_reconciling;
    if (admitted) s_reconciling = true;
    taskEXIT_CRITICAL(&s_state_lock);
    if (!admitted) return DEVICE_STATUS_BUSY;
    if (xSemaphoreTake(s_mutex, pdMS_TO_TICKS(5000)) != pdTRUE) {
        taskENTER_CRITICAL(&s_state_lock);
        s_reconciling = false;
        taskEXIT_CRITICAL(&s_state_lock);
        return DEVICE_STATUS_TIMEOUT;
    }

    /* Validate only after taking the serialized owner: a rejected/revoked
     * authorization must still cancel its retained retry generation. The
     * snapshot and every downstream effect revalidate again, so a revoke that
     * races this mutex wait cannot cross the first consumer boundary. */
    configuration_effective_revisioned_snapshot_t effective = {0};
    device_status_t status = authorization_current(authorization)
                                 ? configuration_service_load_effective_revisioned_snapshot(&effective)
                                 : DEVICE_STATUS_UNAVAILABLE;
    if (status == DEVICE_STATUS_OK) {
        status = reconcile_one_effective_snapshot(&effective, reason, authorization);
    }
    /* Keep the retry timer decision inside the same serialized ownership as
     * the consumer calls. A second Hub/GUI ingress may start as soon as this
     * mutex is released; it must never race a delayed stop/start belonging to
     * this completed revision. */
    if (!schedule_or_cancel_retry(status, reason, authorization) && retryable_status(status)) {
        status = DEVICE_STATUS_INTERNAL_ERROR;
    }
    if (reason != CONFIGURATION_RECONCILE_REASON_RUNTIME_OVERRIDE_EXPIRY) {
        rearm_expiry_timer_under_mutex();
    }
    taskENTER_CRITICAL(&s_state_lock);
    s_last_status = status;
    s_reconciling = false;
    taskEXIT_CRITICAL(&s_state_lock);
    xSemaphoreGive(s_mutex);
    return status;
}

device_status_t configuration_reconcile_service_reconcile(
    configuration_reconcile_service_reason_t reason) {
    return reconcile_internal(reason, NULL);
}

device_status_t configuration_reconcile_service_reconcile_authorized(
    configuration_reconcile_service_reason_t reason,
    const configuration_reconcile_authorization_t *authorization) {
    if (!authorization || !authorization_shape_is_valid(authorization)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    return reconcile_internal(reason, authorization);
}

device_status_t configuration_reconcile_service_apply_runtime_override(
    const configuration_runtime_override_t *override) {
    if (!override) return DEVICE_STATUS_INVALID_ARGUMENT;
    /* The value model can represent a future uplink preference so durable
     * Configuration migration has one vocabulary, but an actual live Wi-Fi ↔
     * cellular handover still needs request drain, old-link quiesce, new-link
     * readiness and a reversible rollback. Do not let the consumer-owning
     * ingress accept it and then report success after applying only Audio/UI
     * values from the same effective snapshot. */
    if (override->kind == CONFIGURATION_RUNTIME_OVERRIDE_VALUE_TRANSPORT_SELECTION) {
        return DEVICE_STATUS_UNAVAILABLE;
    }
    const device_status_t status = configuration_service_apply_runtime_override(override);
    if (status != DEVICE_STATUS_OK) return status;
    /* A replacement may move the earliest deadline in either direction. Rearm
     * before entering reconcile so even a temporarily busy consumer cannot
     * leave a newly earlier expiry behind the old timer deadline. */
    rearm_expiry_timer();
    return configuration_reconcile_service_reconcile(
        CONFIGURATION_RECONCILE_REASON_RUNTIME_POLICY);
}

device_status_t configuration_reconcile_service_remove_runtime_override(
    configuration_runtime_override_value_kind_t kind) {
    const device_status_t status = configuration_service_remove_runtime_override(kind);
    if (status != DEVICE_STATUS_OK) return status;
    rearm_expiry_timer();
    return configuration_reconcile_service_reconcile(
        CONFIGURATION_RECONCILE_REASON_RUNTIME_POLICY);
}

device_status_t configuration_reconcile_service_clear_runtime_overrides(void) {
    const device_status_t status = configuration_service_clear_runtime_overrides();
    if (status != DEVICE_STATUS_OK) return status;
    rearm_expiry_timer();
    return configuration_reconcile_service_reconcile(
        CONFIGURATION_RECONCILE_REASON_RUNTIME_POLICY);
}

bool configuration_reconcile_service_get_snapshot(
    configuration_reconcile_service_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_state_lock);
    *out_snapshot = (configuration_reconcile_service_snapshot_t){
        .struct_size = sizeof(*out_snapshot),
        .abi_version = CONFIGURATION_RECONCILE_SERVICE_SNAPSHOT_ABI_VERSION,
        .initialized = s_initialized,
        .reconciling = s_reconciling,
        .retry_armed = s_retry_armed,
        .retry_attempt = s_retry_attempt,
        .apply_state = s_apply_state,
        .last_status = s_last_status,
    };
    taskEXIT_CRITICAL(&s_state_lock);
    return true;
}
