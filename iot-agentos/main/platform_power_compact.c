#include "platform_power_profile.h"

#include "compact_input_service.h"
#include "compact_peripheral_service.h"

static device_status_t compact_status_from_esp_err(esp_err_t err) {
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

/* Bread and Fangtang share the compact peripheral family.  Each selected
 * profile adapter owns whether telemetry is available and, if so, its ADC /
 * charger implementation; this family bridge never exposes those facts. */
bool platform_power_profile_get_telemetry(uint8_t *out_level_percent,
                                          bool *out_charging) {
    if (!out_level_percent || !out_charging) return false;
    unsigned level = 0;
    bool charging = false;
    if (!compact_peripheral_service_get_power_status(&level, &charging)) {
        return false;
    }
    /* A profile adapter must publish a normalized percentage. Clamping an
     * impossible value would hide calibration/configuration faults and could
     * make Battery Policy act on a fabricated full-battery observation. */
    if (level > 100u) return false;
    *out_level_percent = (uint8_t)level;
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
    /* The retained input scanner owns GPIO reads on both compact profiles.
     * Park it before profile power can touch electrical state; this stays
     * below the value-only Power boundary. */
    device_status_t input_status = compact_status_from_esp_err(
        compact_input_service_prepare_system_sleep(timeout_ms));
    if (input_status != DEVICE_STATUS_OK) return input_status;
    /* Fangtang's ADC/charge monitor is profile-private but continuously owns
     * its peripheral path. Park it here, beneath the shared Power facade,
     * before a future electrical adapter can alter rails or clocks. Bread's
     * selected adapter is an explicit no-op with the same contract. */
    device_status_t peripheral_status = compact_status_from_esp_err(
        compact_peripheral_service_prepare_system_sleep(timeout_ms));
    if (peripheral_status != DEVICE_STATUS_OK) {
        compact_input_service_abort_system_sleep_prepare();
        return peripheral_status;
    }
    /* Bread and Fangtang require distinct GPIO0/rail/modem electrical HIL.
     * This family bridge must not leave an otherwise healthy scanner/monitor
     * parked merely because the physical profile is unavailable: a caller that
     * receives UNAVAILABLE has not entered a profile transaction. Power
     * Service will issue its normal idempotent ABORT too, but release the
     * local PREPARE effects here so this private contract is independently
     * fail-closed. */
    compact_peripheral_service_abort_system_sleep_prepare();
    compact_input_service_abort_system_sleep_prepare();
    return DEVICE_STATUS_UNAVAILABLE;
}

device_status_t platform_power_profile_abort_verified_sleep(
    device_power_state_t target_state, uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    (void)target_state;
    /* This is safe after both an unavailable electrical preflight and a
     * future successful rail prepare: only the selected profile knows
     * whether it owns a retained monitor, and ABORT preserves its generation. */
    compact_peripheral_service_abort_system_sleep_prepare();
    compact_input_service_abort_system_sleep_prepare();
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
    /* Bread's GPIO0 boot strap and Fangtang's GPIO0/ML307/charger/rail
     * sequence have no completed electrical wake HIL.  The shared family
     * bridge must remain unavailable rather than guessing a physical-sleep
     * wiring sequence. */
    return DEVICE_STATUS_UNAVAILABLE;
}

device_status_t platform_power_profile_resume_verified_sleep(
    device_power_state_t target_state, uint32_t timeout_ms) {
    if (timeout_ms == 0 ||
        (target_state != DEVICE_POWER_STATE_LIGHT_SLEEP &&
         target_state != DEVICE_POWER_STATE_DEEP_SLEEP)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    /* No compact profile can reach COMMIT today. Keep an explicit no-op
     * RESUME so future profile-local electrical implementations have one
     * stable transaction endpoint without teaching Power Service the board. */
    return DEVICE_STATUS_OK;
}
