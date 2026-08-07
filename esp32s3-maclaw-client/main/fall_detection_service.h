#pragma once

/*
 * Hardware-independent suspected-fall classifier.
 *
 * This is a domain service, not a QMI8658 driver.  It consumes normalized
 * Device API motion samples, so a future board only needs to implement its
 * Motion HAL to participate.  It deliberately reports a *suspected* fall;
 * product policy must not represent this as a medical diagnosis or an SOS
 * transport without a separately verified escalation workflow.
 */

#include <stdbool.h>
#include <stdint.h>

#include "cJSON.h"
#include "device_api.h"
#include "esp_err.h"

typedef enum {
    FALL_DETECTION_STATE_UNAVAILABLE = 0,
    FALL_DETECTION_STATE_DISABLED,
    FALL_DETECTION_STATE_MONITORING,
    FALL_DETECTION_STATE_PENDING_CONFIRMATION,
} fall_detection_state_t;

typedef enum {
    /* The local user may still cancel this candidate. */
    FALL_DETECTION_EVENT_SUSPECTED = 0,
    /* The local cancellation window elapsed.  This remains non-medical. */
    FALL_DETECTION_EVENT_CONFIRMED,
} fall_detection_event_t;

typedef struct {
    bool available;
    bool enabled;
    fall_detection_state_t state;
    uint32_t suspected_count;
    uint64_t confirmation_deadline_us;
    uint32_t configuration_revision;
} fall_detection_snapshot_t;

typedef void (*fall_detection_callback_t)(fall_detection_event_t event,
                                          void *context);

/*
 * Starts monitoring when the selected profile advertises a Motion HAL.
 * Profiles without one return DEVICE_STATUS_UNAVAILABLE without starting a
 * task.  A repeated call is idempotent only when it carries the same callback
 * pair; changing a live notification target is intentionally rejected.
 */
device_status_t fall_detection_service_init(fall_detection_callback_t callback,
                                            void *context);
/* Stops sampling and drains Gateway tool callers.  It releases only this
 * domain's task/locks; the board Motion HAL remains owned by its profile. */
device_status_t fall_detection_service_deinit(uint32_t timeout_ms);

bool fall_detection_service_is_initialized(void);
bool fall_detection_service_is_available(void);
bool fall_detection_service_get_snapshot(fall_detection_snapshot_t *out_snapshot);

/*
 * Cancels only an active local confirmation window.  This function has no
 * sensor or UI side effect, allowing the app's normalized intent handler to
 * restore its own foreground surface consistently for touch and buttons.
 */
bool fall_detection_service_cancel_from_user(void);

/* Gateway tool boundary.  The service owns its durable enable flag but never
 * exposes an IMU register, sensor-specific threshold or physical input to the
 * tool protocol.  Set is semantically idempotent: retrying the same desired
 * state has no extra side effect. */
esp_err_t fall_detection_service_execute_tool(const char *name, cJSON *arguments,
                                              const char *idempotency_key,
                                              cJSON **out_result, char *error,
                                              size_t error_size);
