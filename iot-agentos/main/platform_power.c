#include "platform_power.h"

#include "display_service.h"
#include "platform_power_profile.h"
#include "platform_wake.h"

static bool system_sleep_target_is_valid(device_power_state_t target_state) {
    return target_state == DEVICE_POWER_STATE_LIGHT_SLEEP ||
           target_state == DEVICE_POWER_STATE_DEEP_SLEEP;
}

device_status_t platform_power_enter_display_off(void) {
    return display_service_enter_display_off();
}

device_status_t platform_power_wake_display(void) {
    return display_service_wake_display();
}

bool platform_power_display_is_off(void) {
    return display_service_display_is_off();
}

bool platform_power_get_telemetry(device_power_telemetry_t *out_telemetry) {
    if (!out_telemetry) return false;
    uint8_t level = 0;
    bool charging = false;
    const bool available = platform_power_profile_get_telemetry(&level, &charging);
    *out_telemetry = (device_power_telemetry_t){
        .available = available,
        .level_percent = level,
        .charging = charging,
    };
    return available;
}

device_status_t platform_power_prepare_verified_sleep(
    device_power_state_t target_state,
    device_wake_source_flags_t verified_sources,
    uint32_t timeout_ms) {
    if (!system_sleep_target_is_valid(target_state) || timeout_ms == 0 ||
        verified_sources == 0) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    const device_status_t authorization =
        platform_wake_authorize_verified_sleep_sources(target_state,
                                                       verified_sources);
    if (authorization != DEVICE_STATUS_OK) return authorization;
    return platform_power_profile_prepare_verified_sleep(target_state,
                                                         verified_sources,
                                                         timeout_ms);
}

device_status_t platform_power_abort_verified_sleep(
    device_power_state_t target_state, uint32_t timeout_ms) {
    if (!system_sleep_target_is_valid(target_state) || timeout_ms == 0) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    return platform_power_profile_abort_verified_sleep(target_state, timeout_ms);
}

device_status_t platform_power_commit_verified_sleep(
    device_power_state_t target_state,
    device_wake_source_flags_t verified_sources,
    uint32_t entry_timeout_ms) {
    if (!system_sleep_target_is_valid(target_state) || entry_timeout_ms == 0 ||
        verified_sources == 0) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    const device_status_t authorization =
        platform_wake_authorize_verified_sleep_sources(target_state,
                                                       verified_sources);
    if (authorization != DEVICE_STATUS_OK) return authorization;
    return platform_power_profile_commit_verified_sleep(target_state,
                                                        verified_sources,
                                                        entry_timeout_ms);
}

device_status_t platform_power_resume_verified_sleep(
    device_power_state_t target_state, uint32_t timeout_ms) {
    if (!system_sleep_target_is_valid(target_state) || timeout_ms == 0) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    return platform_power_profile_resume_verified_sleep(target_state, timeout_ms);
}
