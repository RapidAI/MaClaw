/* Bread Compact optional peripheral capability contract.
 *
 * This profile currently has no normalized inertial source.  Keeping that
 * fact here lets the shared compact board facade remain independent of which
 * hardware profile is compiled. */
#pragma once

#include "sdkconfig.h"

#if !CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD
#error "Bread peripheral adapter may only be included by the Bread Compact profile"
#endif

#ifndef MACLAW_COMPACT_PERIPHERAL_ADAPTER_IMPLEMENTATION
#error "Bread peripheral adapter is owned exclusively by compact_peripheral_service.c"
#endif

#include "device_api.h"
#include "esp_err.h"

static inline esp_err_t compact_peripheral_adapter_get_motion_sample(
    device_motion_sample_t *out_sample) {
    if (!out_sample) return ESP_ERR_INVALID_ARG;
    return ESP_ERR_NOT_SUPPORTED;
}

/* Bread has no profile-owned auxiliary monitor.  The shared compact renderer
 * can still ask every profile to stop its optional background peripherals
 * during startup rollback without learning which boards actually have one. */
static inline esp_err_t compact_peripheral_adapter_stop_background_tasks(
    uint32_t timeout_ms) {
    return timeout_ms ? ESP_OK : ESP_ERR_INVALID_ARG;
}

/* Bread has no profile-owned power monitor.  Keep startup orchestration
 * uniform: the shared compact renderer asks every selected profile to bring
 * up optional peripherals and receives the same normalized result. */
static inline esp_err_t compact_peripheral_adapter_init(void) {
    return ESP_OK;
}

/* Boards without a fuel gauge keep the normalized telemetry contract, but
 * report it unavailable instead of making Power Service know the profile. */
static inline bool compact_peripheral_adapter_get_power_status(
    unsigned *level_percent, bool *charging) {
    (void)level_percent;
    (void)charging;
    return false;
}
