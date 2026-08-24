#include "battery_policy_service.h"

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

static device_battery_policy_level_t next_level(const device_power_telemetry_t *telemetry,
                                                device_battery_policy_level_t previous) {
    if (!telemetry->available || telemetry->charging) return DEVICE_BATTERY_POLICY_NORMAL;
    const uint8_t percent = telemetry->level_percent;
    if (previous == DEVICE_BATTERY_POLICY_PROTECT) {
        if (percent < BATTERY_PROTECT_EXIT_PERCENT) return DEVICE_BATTERY_POLICY_PROTECT;
    }
    if (percent <= BATTERY_PROTECT_ENTER_PERCENT) return DEVICE_BATTERY_POLICY_PROTECT;
    if (previous == DEVICE_BATTERY_POLICY_CONSERVE) {
        if (percent < BATTERY_CONSERVE_EXIT_PERCENT) return DEVICE_BATTERY_POLICY_CONSERVE;
    }
    if (percent <= BATTERY_CONSERVE_ENTER_PERCENT) return DEVICE_BATTERY_POLICY_CONSERVE;
    return DEVICE_BATTERY_POLICY_NORMAL;
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
    const device_battery_policy_level_t previous = s_level;
    if (initialized) ++s_active_queries;
    taskEXIT_CRITICAL(&s_lock);
    if (!initialized) return false;

    device_power_telemetry_t telemetry = {0};
    (void)device_power_get_telemetry(&telemetry);
    const device_battery_policy_level_t calculated = next_level(&telemetry, previous);
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
    s_level = calculated;
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
