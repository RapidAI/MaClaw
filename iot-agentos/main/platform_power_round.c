#include "platform_power_profile.h"

#include "round_input_service.h"
#include "round_peripheral_service.h"

static device_status_t round_status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        case ESP_ERR_NOT_SUPPORTED: return DEVICE_STATUS_UNAVAILABLE;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

/* EchoEar and Waveshare retain their different peripheral controllers below
 * Round Peripheral Service.  Platform Power sees the same value contract as
 * the compact family and therefore needs no board-specific branch. */
bool platform_power_profile_get_telemetry(uint8_t *out_level_percent,
                                          bool *out_charging) {
    if (!out_level_percent || !out_charging) return false;
    unsigned level = 0;
    bool charging = false;
    if (!round_peripheral_service_get_power_status(&level, &charging)) {
        return false;
    }
    *out_level_percent = (uint8_t)(level > 100 ? 100 : level);
    *out_charging = charging;
    return true;
}

device_status_t platform_power_profile_prepare_verified_sleep(
    device_power_state_t target_state,
    device_wake_source_flags_t verified_sources,
    uint32_t timeout_ms) {
    if (timeout_ms == 0 || verified_sources == 0 ||
        (target_state != DEVICE_POWER_STATE_LIGHT_SLEEP &&
         target_state != DEVICE_POWER_STATE_DEEP_SLEEP)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    /* EchoEar's CST8XX and Waveshare's CST9217 are read by the retained
     * scanner over their profile-private peripheral path. Park that scanner
     * before any future PMIC/I2C electrical prepare can alter the bus. */
    const device_status_t input_status = round_status_from_esp_err(
        round_input_service_prepare_system_sleep(timeout_ms));
    if (input_status != DEVICE_STATUS_OK) return input_status;
    /* EchoEar polling touch and Waveshare touch/GPIO power paths remain HIL
     * candidates. Restore the scanner before reporting UNAVAILABLE: no caller
     * may be left with a retained input generation parked when no physical
     * profile transaction exists. The public Power rollback remains idempotent
     * and will call this path's ABORT again. */
    round_input_service_abort_system_sleep_prepare();
    return DEVICE_STATUS_UNAVAILABLE;
}

device_status_t platform_power_profile_abort_verified_sleep(
    device_power_state_t target_state, uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    (void)target_state;
    /* The scanner fence is reversible even though the electrical sequence is
     * still unavailable. */
    round_input_service_abort_system_sleep_prepare();
    return DEVICE_STATUS_OK;
}

device_status_t platform_power_profile_commit_verified_sleep(
    device_power_state_t target_state,
    device_wake_source_flags_t verified_sources,
    uint32_t entry_timeout_ms) {
    if (entry_timeout_ms == 0 || verified_sources == 0 ||
        (target_state != DEVICE_POWER_STATE_LIGHT_SLEEP &&
         target_state != DEVICE_POWER_STATE_DEEP_SLEEP)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    /* EchoEar's touch path is polling and Waveshare's touch/PMIC wake path
     * lacks a complete rail/resume HIL sequence. Do not turn DISPLAY_OFF
     * sources into an inferred MCU-sleep implementation. */
    return DEVICE_STATUS_UNAVAILABLE;
}

device_status_t platform_power_profile_resume_verified_sleep(
    device_power_state_t target_state, uint32_t timeout_ms) {
    if (timeout_ms == 0 ||
        (target_state != DEVICE_POWER_STATE_LIGHT_SLEEP &&
         target_state != DEVICE_POWER_STATE_DEEP_SLEEP)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    /* Current round profiles cannot reach COMMIT; this is intentionally an
     * explicit transaction endpoint, not an assertion of electrical resume. */
    return DEVICE_STATUS_OK;
}
