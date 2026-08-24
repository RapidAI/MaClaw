#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "cJSON.h"
#include "device_api.h"
#include "esp_err.h"

// Device-local alarm engine. Epoch milliseconds are authoritative; labels are
// display-only UTF-8 and are truncated to a bounded persisted size.
typedef void (*alarm_manager_ring_callback_t)(void *arg);

// Invoked once immediately before each ringing attempt takes display/audio
// focus. The application callback must remain non-blocking.
void alarm_manager_set_ring_callback(alarm_manager_ring_callback_t callback,
                                     void *arg);
device_status_t alarm_manager_init(void);
/* Stops the scheduler without completing an active alarm.  Its durable active
 * record is intentionally retained so a subsequent boot can recover it. */
device_status_t alarm_manager_deinit(uint32_t timeout_ms);
/*
 * Internal System Sleep participant. PREPARE closes new alarm-tool admission,
 * drains already-admitted tool calls, and prevents the deadline worker from
 * beginning a new ring sequence while the Power transaction is open. It does
 * not delete alarms, stop the scheduler, or program an RTC wake source; that
 * later electrical mapping remains profile-private. Abort is idempotent and
 * re-notifies the scheduler so an alarm that became due during rollback is
 * evaluated immediately.
 */
device_status_t alarm_manager_prepare_system_sleep(uint32_t timeout_ms);
void alarm_manager_abort_system_sleep_prepare(void);
esp_err_t alarm_manager_execute_tool(const char *name, cJSON *arguments,
                                     const char *idempotency_key,
                                     cJSON **out_result, char *error, size_t error_size);
bool alarm_manager_is_ringing(void);
bool alarm_manager_is_initialized(void);
void alarm_manager_dismiss(void);
