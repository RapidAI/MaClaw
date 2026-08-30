#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

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

device_status_t wake_deadline_service_init(void);
/* Stops the dispatcher after every client has stopped submitting deadlines.
 * It never deletes a running task: timeout leaves the service intact for
 * diagnostics and caller-side isolation. */
device_status_t wake_deadline_service_deinit(uint32_t timeout_ms);
/* Reversible System Sleep boundary for the shared wall-clock dispatcher.
 * PREPARE stops timer delivery and drains callbacks already selected by the
 * dispatcher, while retaining every client slot and epoch. ABORT recomputes
 * the earliest deadline for the same service generation. */
device_status_t wake_deadline_service_prepare_system_sleep(uint32_t timeout_ms);
void wake_deadline_service_abort_system_sleep_prepare(void);
device_status_t wake_deadline_service_register(wake_deadline_callback_t callback, void *arg,
                                               wake_deadline_handle_t *out_handle);
/*
 * Releases a client slot after that client has stopped its own worker.  This
 * legacy convenience form waits up to one second; lifecycle code that owns a
 * parent shutdown deadline must use the timeout-returning form below.
 */
void wake_deadline_service_unregister(wake_deadline_handle_t handle);

/*
 * Closes a slot to new dispatch, then waits until a callback which was already
 * selected by the deadline worker has returned.  A successful return is the
 * hand-off point after which `arg` and callback-owned client state may be
 * reclaimed.  It must not be called from the deadline callback itself, and a
 * caller must not hold a lock that its callback needs while waiting.
 *
 * `timeout_ms` is a caller-owned remaining lifecycle budget: timeout leaves
 * the slot closed and unreusable, preserving callback ownership fail-closed.
 */
device_status_t wake_deadline_service_unregister_with_timeout(wake_deadline_handle_t handle,
                                                              uint32_t timeout_ms);

/* Arms a one-shot Unix-epoch millisecond deadline.  A deadline remains stored
 * while wall time is untrusted and is armed as soon as
 * wake_deadline_service_on_wall_clock_updated() is called after a valid sync. */
device_status_t wake_deadline_service_arm(wake_deadline_handle_t handle,
                                          int64_t epoch_ms);
void wake_deadline_service_cancel(wake_deadline_handle_t handle);

/* Safe to call from the Clock Sync trusted-time callback after a sample has
 * been admitted and applied.  It records the boot-local trusted-time fact,
 * then wakes the service worker to recalculate and arm in task context.
 * Merely having a plausible RTC wall-clock must not unlock deadlines. */
void wake_deadline_service_on_trusted_wall_clock_updated(void);

/* Returns the current wall-clock sample used by the deadline dispatcher.
 * This is a value-only observation; it never arms a timer or changes state. */
device_status_t wake_deadline_service_get_clock_status(int64_t *out_epoch_ms,
                                                       bool *out_trusted);
