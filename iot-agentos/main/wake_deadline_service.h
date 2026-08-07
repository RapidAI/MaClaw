#pragma once

#include <stdint.h>

#include "esp_err.h"

/*
 * Shared wall-clock deadline dispatcher.
 *
 * It owns the one ESP timer used for product deadlines (alarm delivery and
 * scheduled display-off transitions).  Clients provide a small non-blocking
 * callback which normally wakes their own worker task; callbacks never run in
 * the ESP timer task.  This is deliberately independent of board power depth:
 * it only dispatches while the current firmware is running.
 */

#define WAKE_DEADLINE_HANDLE_INVALID 0u

typedef uint32_t wake_deadline_handle_t;
typedef void (*wake_deadline_callback_t)(void *arg);

esp_err_t wake_deadline_service_init(void);
/* Stops the dispatcher after every client has stopped submitting deadlines.
 * It never deletes a running task: timeout leaves the service intact for
 * diagnostics and caller-side isolation. */
esp_err_t wake_deadline_service_deinit(uint32_t timeout_ms);
esp_err_t wake_deadline_service_register(wake_deadline_callback_t callback, void *arg,
                                         wake_deadline_handle_t *out_handle);
/* Releases a client slot after that client has stopped its own worker. */
void wake_deadline_service_unregister(wake_deadline_handle_t handle);

/* Arms a one-shot Unix-epoch millisecond deadline.  A deadline remains stored
 * while wall time is untrusted and is armed as soon as
 * wake_deadline_service_on_wall_clock_updated() is called after a valid sync. */
esp_err_t wake_deadline_service_arm(wake_deadline_handle_t handle,
                                    int64_t epoch_ms);
void wake_deadline_service_cancel(wake_deadline_handle_t handle);

/* Safe to call from a clock synchronisation callback: it only wakes the
 * service worker, which recalculates and arms the ESP timer in task context. */
void wake_deadline_service_on_wall_clock_updated(void);
