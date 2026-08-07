#include "battery_policy_service.h"

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

/* Percentages are intentionally conservative product admission thresholds,
 * not a substitute for an electrical brownout strategy.  Hysteresis makes a
 * noisy calibrated percentage unable to flap expensive work on/off. */
#define BATTERY_CONSERVE_ENTER_PERCENT 25u
#define BATTERY_CONSERVE_EXIT_PERCENT 30u
#define BATTERY_PROTECT_ENTER_PERCENT 12u
#define BATTERY_PROTECT_EXIT_PERCENT 16u

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_initialized;
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
    taskENTER_CRITICAL(&s_lock);
    s_initialized = true;
    s_level = DEVICE_BATTERY_POLICY_NORMAL;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

bool battery_policy_service_get_snapshot(device_battery_policy_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_lock);
    const bool initialized = s_initialized;
    const device_battery_policy_level_t previous = s_level;
    taskEXIT_CRITICAL(&s_lock);
    if (!initialized) return false;

    device_power_telemetry_t telemetry = {0};
    (void)device_power_get_telemetry(&telemetry);
    const device_battery_policy_level_t calculated = next_level(&telemetry, previous);
    taskENTER_CRITICAL(&s_lock);
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
