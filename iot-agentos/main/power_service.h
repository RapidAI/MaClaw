#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

/* A private internal-stack storage worker may serve request paths which cannot
 * yet run on their caller's stack. Power owns only this value-only reversible
 * participant seam; queue, task and NVS schema details remain private to that
 * worker service. */
typedef device_status_t (*power_service_system_sleep_storage_prepare_t)(
    uint32_t timeout_ms, void *context);
typedef void (*power_service_system_sleep_storage_abort_t)(void *context);

/* Internal service behind the ISO-C Device Power API.  Board-specific panel,
 * rail and wake-control details remain below this header. */
device_status_t power_service_init(void);
/* Stops the DISPLAY_OFF timer after any in-flight transition leaves the
 * board adapter. The Device API owns admission closure and final lease drain
 * around this narrower scheduler boundary. */
device_status_t power_service_deinit(uint32_t timeout_ms);
device_status_t power_service_request_verified_sleep(device_power_state_t target_state);
/* Must be registered while no System Sleep transition is active.  A future
 * physical commit fails closed when this composition-root participant has no
 * reversible PREPARE/ABORT contract. */
device_status_t power_service_set_system_sleep_storage_bridge(
    power_service_system_sleep_storage_prepare_t prepare,
    power_service_system_sleep_storage_abort_t abort,
    void *context);
bool power_service_get_transition_snapshot(
    device_power_transition_snapshot_t *out_snapshot);
device_status_t power_service_schedule_display_off(uint32_t idle_after_ms);
/* Cancellation is an observable deadline transaction. A non-OK result means
 * a former idle deadline may still be armed or committing, so callers must
 * keep their own semantic state fail-closed rather than claiming it vanished. */
device_status_t power_service_cancel_display_off(void);
device_status_t power_service_wake_display_from_user(void);
/* A domain deadline may restore a schedule-owned DISPLAY_OFF panel without
 * synthesizing a physical input event. */
device_status_t power_service_wake_display_from_schedule(void);
/* A remote management request may restore a DISPLAY_OFF panel without
 * synthesizing physical input or changing manual-wake scheduling policy. */
device_status_t power_service_wake_display_from_remote_control(void);
bool power_service_get_snapshot(device_power_snapshot_t *out_snapshot);
bool power_service_get_telemetry(device_power_telemetry_t *out_telemetry);

/* Private, compile-time-only Power Service hardware-in-the-loop proof.  It
 * is invoked by Device Power before normal App UI idle scheduling opens. */
device_status_t power_service_run_display_off_retry_hil_test(void);
