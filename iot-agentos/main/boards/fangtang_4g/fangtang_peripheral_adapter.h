/* Fangtang-4G optional peripheral capability contract.
 *
 * ADC battery telemetry is exposed through its existing board adapter, but
 * this profile has no normalized inertial source.  Do not make that absence a
 * compile-time branch in the shared renderer or Device Motion HAL facade. */
#pragma once

#include "sdkconfig.h"

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
#error "Fangtang peripheral adapter may only be included by the Fangtang profile"
#endif

#ifndef MACLAW_COMPACT_PERIPHERAL_ADAPTER_IMPLEMENTATION
#error "Fangtang peripheral adapter is owned exclusively by compact_peripheral_service.c"
#endif

#include "device_api.h"
#include "esp_err.h"

static inline esp_err_t compact_peripheral_adapter_get_motion_sample(
    device_motion_sample_t *out_sample) {
    if (!out_sample) return ESP_ERR_INVALID_ARG;
    return ESP_ERR_NOT_SUPPORTED;
}

/* Starts the profile-owned battery/charging monitor.  The shared compact
 * renderer deliberately knows neither ADC nor charge GPIO details. */
esp_err_t compact_peripheral_adapter_init(void);

/* Stops only profile-owned peripheral workers (currently the battery
 * monitor).  It neither deletes ADC/GPIO objects nor makes the board port
 * restartable.  The profile implementation owns its task/semaphore handles;
 * the shared compact renderer sees only this bounded lifecycle contract. */
esp_err_t compact_peripheral_adapter_stop_background_tasks(uint32_t timeout_ms);

/* Reversible future-MCU-sleep boundary for the profile-owned ADC/GPIO
 * monitor. It parks the retained task at a no-sample point; after admission
 * has closed, including an ACK timeout, only the owning Power transaction's
 * ABORT wakes the same generation. Neither operation deinitializes the ADC
 * nor changes board rails. */
esp_err_t compact_peripheral_adapter_prepare_system_sleep(uint32_t timeout_ms);
void compact_peripheral_adapter_abort_system_sleep_prepare(void);

/* Reads the profile-owned normalized telemetry snapshot.  The board facade
 * forwards this narrow value contract to Platform Power; it never observes
 * the ADC, charge GPIO, worker handles or synchronization primitive. */
bool compact_peripheral_adapter_get_power_status(unsigned *level_percent,
                                                 bool *charging);
