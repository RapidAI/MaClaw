#include "battery_policy_service.h"

#include <stdint.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "esp_timer.h"

/* Percentages are intentionally conservative product admission thresholds,
 * not a substitute for an electrical brownout strategy.  Hysteresis makes a
 * noisy calibrated percentage unable to flap expensive work on/off. */
#define BATTERY_CONSERVE_ENTER_PERCENT 25u
#define BATTERY_CONSERVE_EXIT_PERCENT 30u
#define BATTERY_PROTECT_ENTER_PERCENT 12u
#define BATTERY_PROTECT_EXIT_PERCENT 16u
#define BATTERY_POLICY_PROTECT_CONFIRM_SAMPLES 3u
#define BATTERY_POLICY_RECOVERY_CONFIRM_SAMPLES 2u
#define BATTERY_POLICY_EMERGENCY_CHECKPOINT_TIMEOUT_MS 1500u

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_initialized;
static bool s_stopping;
static uint32_t s_active_queries;
/* This service does not own a worker, but its synchronous provider read can
 * call a profile-private ADC/charger adapter.  A future electrical PREPARE
 * must therefore close this observer before profile Power changes rails. */
static bool s_system_sleep_preparing;
/* Owns init/deinit serialization as well as the transient construction
 * publication. The spinlock protects state, but cannot safely cover a wait
 * for an in-flight telemetry read. */
static volatile bool s_lifecycle_transition;
static device_battery_policy_level_t s_level = DEVICE_BATTERY_POLICY_NORMAL;
static uint8_t s_protect_streak;
static uint8_t s_recovery_streak;
static bool s_emergency_checkpoint_in_flight;
static bool s_emergency_checkpoint_done;
/* A failed checkpoint is terminal for this PROTECT generation.  Retrying a
 * flash write from every telemetry poll can amplify brownout damage; only a
 * subsequent confirmed exit and re-entry to PROTECT creates a new budget. */
static bool s_emergency_checkpoint_failed;
static battery_policy_service_checkpoint_fn s_checkpoint_callback;
static void *s_checkpoint_context;

static uint8_t saturating_inc(uint8_t value) {
    return value == UINT8_MAX ? value : (uint8_t)(value + 1u);
}

static bool battery_policy_update(const device_power_telemetry_t *telemetry,
                                  device_battery_policy_level_t previous,
                                  uint8_t *protect_streak,
                                  uint8_t *recovery_streak,
                                  device_battery_policy_level_t *out_level) {
    if (!telemetry || !protect_streak || !recovery_streak || !out_level ||
        previous > DEVICE_BATTERY_POLICY_PROTECT ||
        (telemetry->available && telemetry->level_percent > 100u)) return false;
    device_battery_policy_level_t next = previous;
    if (telemetry->charging) {
        *protect_streak = 0u;
        *recovery_streak = 0u;
        next = DEVICE_BATTERY_POLICY_NORMAL;
    } else if (!telemetry->available) {
        /* A provider failure is not evidence of a healthy/full battery. Keep
         * the last confirmed policy generation while the signal is absent;
         * in particular, never clear PROTECT merely because an ADC/GPIO read
         * failed. Streaks are reset so recovery still requires fresh valid
         * samples after the provider returns. */
        *protect_streak = 0u;
        *recovery_streak = 0u;
        /* A previously confirmed PROTECT state is safety-critical and must
         * remain latched through a transient missing sample. For NORMAL or
         * CONSERVE, preserve the historical compatibility behavior of
         * treating unavailable telemetry as a neutral policy observation. */
        next = previous == DEVICE_BATTERY_POLICY_PROTECT
                   ? DEVICE_BATTERY_POLICY_PROTECT
                   : DEVICE_BATTERY_POLICY_NORMAL;
    } else {
        *protect_streak = telemetry->level_percent <= BATTERY_PROTECT_ENTER_PERCENT
                              ? saturating_inc(*protect_streak) : 0u;
        if (previous == DEVICE_BATTERY_POLICY_PROTECT) {
            if (telemetry->level_percent < BATTERY_PROTECT_EXIT_PERCENT) {
                *recovery_streak = 0u;
                next = DEVICE_BATTERY_POLICY_PROTECT;
            } else {
                *recovery_streak = saturating_inc(*recovery_streak);
                next = *recovery_streak >= BATTERY_POLICY_RECOVERY_CONFIRM_SAMPLES
                           ? DEVICE_BATTERY_POLICY_CONSERVE
                           : DEVICE_BATTERY_POLICY_PROTECT;
            }
        } else {
            *recovery_streak = 0u;
            next = *protect_streak >= BATTERY_POLICY_PROTECT_CONFIRM_SAMPLES
                       ? DEVICE_BATTERY_POLICY_PROTECT
                       : (previous == DEVICE_BATTERY_POLICY_CONSERVE
                              ? (telemetry->level_percent < BATTERY_CONSERVE_EXIT_PERCENT
                                     ? DEVICE_BATTERY_POLICY_CONSERVE
                                     : DEVICE_BATTERY_POLICY_NORMAL)
                              : (telemetry->level_percent <= BATTERY_CONSERVE_ENTER_PERCENT
                                     ? DEVICE_BATTERY_POLICY_CONSERVE
                                     : DEVICE_BATTERY_POLICY_NORMAL));
        }
    }
    *out_level = next;
    return true;
}

device_status_t battery_policy_service_init(void) {
    bool expected = false;
    if (!__atomic_compare_exchange_n(&s_lifecycle_transition, &expected, true, false,
                                     __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
        return DEVICE_STATUS_BUSY;
    }
    taskENTER_CRITICAL(&s_lock);
    if (s_stopping) {
        taskEXIT_CRITICAL(&s_lock);
        __atomic_store_n(&s_lifecycle_transition, false, __ATOMIC_RELEASE);
        return DEVICE_STATUS_BUSY;
    }
    s_initialized = true;
    s_active_queries = 0;
    s_system_sleep_preparing = false;
    s_level = DEVICE_BATTERY_POLICY_NORMAL;
    s_protect_streak = 0u;
    s_recovery_streak = 0u;
    s_emergency_checkpoint_in_flight = false;
    s_emergency_checkpoint_done = false;
    s_emergency_checkpoint_failed = false;
    s_checkpoint_callback = NULL;
    s_checkpoint_context = NULL;
    taskEXIT_CRITICAL(&s_lock);
    __atomic_store_n(&s_lifecycle_transition, false, __ATOMIC_RELEASE);
    return DEVICE_STATUS_OK;
}

device_status_t battery_policy_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    /* Claim a lifecycle transaction. A passive `s_stopping` observation is
     * insufficient: another init could otherwise reopen admission between
     * deinit's observation and provider teardown. */
    for (;;) {
        bool expected = false;
        if (__atomic_compare_exchange_n(&s_lifecycle_transition, &expected, true, false,
                                        __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
            break;
        }
        if (esp_timer_get_time() >= deadline_us) return DEVICE_STATUS_TIMEOUT;
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    /* There is no worker or callback to join, but a synchronous telemetry
     * read can already be in flight. Close new admission first, then drain
     * those readers before its provider is allowed to stop. */
    taskENTER_CRITICAL(&s_lock);
    const bool already_stopped = !s_initialized && !s_stopping;
    s_initialized = false;
    s_stopping = true;
    s_system_sleep_preparing = false;
    s_level = DEVICE_BATTERY_POLICY_NORMAL;
    s_protect_streak = 0u;
    s_recovery_streak = 0u;
    s_emergency_checkpoint_in_flight = false;
    s_emergency_checkpoint_done = false;
    s_emergency_checkpoint_failed = false;
    s_checkpoint_callback = NULL;
    s_checkpoint_context = NULL;
    taskEXIT_CRITICAL(&s_lock);

    /* Idempotent teardown must not manufacture a closed generation.  This
     * path is used by rollback after a sibling startup failure, including
     * before battery policy itself has ever been initialized. */
    if (already_stopped) {
        taskENTER_CRITICAL(&s_lock);
        s_stopping = false;
        taskEXIT_CRITICAL(&s_lock);
        __atomic_store_n(&s_lifecycle_transition, false, __ATOMIC_RELEASE);
        return DEVICE_STATUS_OK;
    }
    for (;;) {
        taskENTER_CRITICAL(&s_lock);
        const uint32_t active_queries = s_active_queries;
        taskEXIT_CRITICAL(&s_lock);
        if (active_queries == 0) break;
        if (esp_timer_get_time() >= deadline_us) {
            /* Keep admission closed. Init rejects reopening this timed-out
             * generation rather than letting a stale query publish into it. */
            __atomic_store_n(&s_lifecycle_transition, false, __ATOMIC_RELEASE);
            return DEVICE_STATUS_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    taskENTER_CRITICAL(&s_lock);
    s_stopping = false;
    taskEXIT_CRITICAL(&s_lock);
    __atomic_store_n(&s_lifecycle_transition, false, __ATOMIC_RELEASE);
    return DEVICE_STATUS_OK;
}

device_status_t battery_policy_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_stopping) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* Admission and the active-reader increment share this lock, so observing
     * zero after the marker is set is a true provider safe point. */
    s_system_sleep_preparing = true;
    taskEXIT_CRITICAL(&s_lock);

    for (;;) {
        taskENTER_CRITICAL(&s_lock);
        const uint32_t active_queries = s_active_queries;
        taskEXIT_CRITICAL(&s_lock);
        if (active_queries == 0) return DEVICE_STATUS_OK;
        if (esp_timer_get_time() >= deadline_us) {
            /* Keep the marker closed until the caller's mandatory ABORT.
             * A late reader must not cross the electrical COMMIT boundary. */
            return DEVICE_STATUS_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1));
    }
}

void battery_policy_service_abort_system_sleep_prepare(void) {
    taskENTER_CRITICAL(&s_lock);
    s_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_lock);
}

bool battery_policy_service_get_snapshot(device_battery_policy_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_lock);
    const bool initialized = s_initialized && !s_stopping &&
                             !s_system_sleep_preparing;
    if (initialized) ++s_active_queries;
    taskEXIT_CRITICAL(&s_lock);
    if (!initialized) return false;

    device_power_telemetry_t telemetry = {0};
    /* The provider's boolean result is part of the value contract.  Do not
     * trust a partially written/stale structure when a profile adapter
     * reports that its read failed; normalize that case to unavailable so
     * policy callers fail closed instead of acting on an old sample. */
    if (!device_power_get_telemetry(&telemetry)) {
        telemetry = (device_power_telemetry_t){0};
    }
    uint8_t protect_streak = 0u;
    uint8_t recovery_streak = 0u;
    device_battery_policy_level_t calculated = DEVICE_BATTERY_POLICY_NORMAL;
    bool entered_protect = false;
    taskENTER_CRITICAL(&s_lock);
    /* Deinit can run while the synchronous provider read above is in flight.
     * Do not publish that stale observation after its admission has closed. */
    /* PREPARE can close admission while the profile telemetry provider is
     * executing. That pre-fence reader is allowed to finish so PREPARE can
     * drain, but its result must not update policy after the electrical
     * transaction has become fail-closed. */
    const bool publish = s_initialized && !s_stopping && !s_system_sleep_preparing;
    --s_active_queries;
    if (!publish) {
        taskEXIT_CRITICAL(&s_lock);
        return false;
    }
    /* Serialize policy transitions after the provider read. Multiple callers
     * may have overlapping reads, but each result must advance from the
     * latest committed level/streak rather than overwrite a newer sample
     * with a stale copy captured before the read. */
    const device_battery_policy_level_t previous = s_level;
    protect_streak = s_protect_streak;
    recovery_streak = s_recovery_streak;
    if (!battery_policy_update(&telemetry, previous, &protect_streak,
                               &recovery_streak, &calculated)) {
        taskEXIT_CRITICAL(&s_lock);
        return false;
    }
    /* A PROTECT interval is a policy generation.  Leaving PROTECT and later
     * entering it again must create a fresh one-shot checkpoint budget; do
     * not carry completion evidence across unrelated low-voltage events. */
    if (previous != calculated &&
        (previous == DEVICE_BATTERY_POLICY_PROTECT ||
         calculated == DEVICE_BATTERY_POLICY_PROTECT)) {
        s_emergency_checkpoint_in_flight = false;
        s_emergency_checkpoint_done = false;
        s_emergency_checkpoint_failed = false;
    }
    entered_protect = previous != DEVICE_BATTERY_POLICY_PROTECT &&
                      calculated == DEVICE_BATTERY_POLICY_PROTECT;
    s_level = calculated;
    s_protect_streak = protect_streak;
    s_recovery_streak = recovery_streak;
    taskEXIT_CRITICAL(&s_lock);

    *out_snapshot = (device_battery_policy_snapshot_t){
        .struct_size = sizeof(*out_snapshot),
        .abi_version = DEVICE_BATTERY_POLICY_ABI_VERSION,
        .telemetry_available = telemetry.available,
        .charging = telemetry.charging,
        .level_percent = telemetry.level_percent,
        .level = calculated,
        /* A board without calibrated telemetry remains usable.  Policy can
         * limit only facts it knows; it must not reinterpret unavailable as
         * critically empty. */
        .optional_work_allowed = calculated != DEVICE_BATTERY_POLICY_PROTECT,
        .high_power_work_allowed = calculated == DEVICE_BATTERY_POLICY_NORMAL,
    };
    /* The first confirmed PROTECT sample transition is the electrical-risk
     * boundary. Trigger the one-shot durable checkpoint after publishing the
     * new policy state, never while the policy lock is held. A failed callback
     * is terminal for this generation; the one-shot budget prevents write
     * amplification when callers poll telemetry repeatedly. */
    if (entered_protect) {
        (void)battery_policy_service_run_emergency_checkpoint(
            BATTERY_POLICY_EMERGENCY_CHECKPOINT_TIMEOUT_MS);
    }
    return true;
}

bool battery_policy_service_allows_optional_work(void) {
    device_battery_policy_snapshot_t snapshot = {0};
    return battery_policy_service_get_snapshot(&snapshot) && snapshot.optional_work_allowed;
}

bool battery_policy_service_allows_high_power_work(void) {
    device_battery_policy_snapshot_t snapshot = {0};
    return battery_policy_service_get_snapshot(&snapshot) && snapshot.high_power_work_allowed;
}

uint8_t battery_policy_service_limit_backlight_percent(uint8_t requested_percent) {
    if (requested_percent > 100u) return requested_percent;
    /* This helper is called from Display Task and must not synchronously
     * re-enter the profile ADC provider.  Policy state is updated by the
     * normal telemetry observer; use that already-confirmed state here. */
    taskENTER_CRITICAL(&s_lock);
    const bool initialized = s_initialized && !s_stopping;
    const device_battery_policy_level_t level = s_level;
    taskEXIT_CRITICAL(&s_lock);
    if (!initialized) return requested_percent;
    if (level == DEVICE_BATTERY_POLICY_PROTECT && requested_percent > 35u) {
        return 35u;
    }
    if (level == DEVICE_BATTERY_POLICY_CONSERVE && requested_percent > 65u) {
        return 65u;
    }
    return requested_percent;
}

bool battery_policy_service_try_begin_emergency_checkpoint(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool allowed = s_initialized && !s_stopping && !s_system_sleep_preparing &&
                         s_level == DEVICE_BATTERY_POLICY_PROTECT &&
                         !s_emergency_checkpoint_in_flight &&
                         !s_emergency_checkpoint_done &&
                         !s_emergency_checkpoint_failed;
    if (allowed) s_emergency_checkpoint_in_flight = true;
    taskEXIT_CRITICAL(&s_lock);
    return allowed;
}

void battery_policy_service_complete_emergency_checkpoint(bool success) {
    taskENTER_CRITICAL(&s_lock);
    if (s_emergency_checkpoint_in_flight) {
        s_emergency_checkpoint_in_flight = false;
        if (success) s_emergency_checkpoint_done = true;
        else s_emergency_checkpoint_failed = true;
    }
    taskEXIT_CRITICAL(&s_lock);
}

device_status_t battery_policy_service_set_emergency_checkpoint_callback(
    battery_policy_service_checkpoint_fn callback, void *context) {
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_stopping || s_emergency_checkpoint_in_flight) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_checkpoint_callback = callback;
    s_checkpoint_context = context;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

device_status_t battery_policy_service_run_emergency_checkpoint(uint32_t timeout_ms) {
    if (timeout_ms == 0u) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    taskENTER_CRITICAL(&s_lock);
    battery_policy_service_checkpoint_fn callback = s_checkpoint_callback;
    void *context = s_checkpoint_context;
    taskEXIT_CRITICAL(&s_lock);
    if (!callback) return DEVICE_STATUS_UNAVAILABLE;
    if (!battery_policy_service_try_begin_emergency_checkpoint()) {
        return DEVICE_STATUS_BUSY;
    }
    const int64_t before_callback_us = esp_timer_get_time();
    if (before_callback_us >= deadline_us) {
        battery_policy_service_complete_emergency_checkpoint(false);
        return DEVICE_STATUS_TIMEOUT;
    }
    const uint64_t remaining_us = (uint64_t)(deadline_us - before_callback_us);
    uint32_t remaining_ms = (uint32_t)((remaining_us + 999u) / 1000u);
    if (remaining_ms == 0u) remaining_ms = 1u;
    const device_status_t callback_status = callback(remaining_ms, context);
    /* A persistence callback may return OK exactly as its allowance expires.
     * That is not evidence that the checkpoint completed within the parent
     * transaction; latch the generation as failed and keep callers closed. */
    const bool late_success = callback_status == DEVICE_STATUS_OK &&
                              esp_timer_get_time() >= deadline_us;
    const device_status_t status = late_success ? DEVICE_STATUS_TIMEOUT : callback_status;
    battery_policy_service_complete_emergency_checkpoint(status == DEVICE_STATUS_OK);
    return status;
}
