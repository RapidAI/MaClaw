#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "cJSON.h"
#include "device_api.h"
#include "esp_err.h"

/*
 * Device-independent scheduled-rest policy.  This is deliberately a domain
 * service, rather than a board or renderer feature: profiles only expose the
 * physical power states they have proved.  The current common implementation
 * accepts DISPLAY_OFF exclusively; LIGHT/DEEP_SLEEP remain unavailable until
 * their wake matrix and PREPARE -> COMMIT transaction exist.
 */

#define SLEEP_SCHEDULE_TIMEZONE_CAPACITY 24
#define SLEEP_SCHEDULE_IDEMPOTENCY_KEY_CAPACITY 64

typedef enum {
    SLEEP_SCHEDULE_MODE_NONE = 0,
    SLEEP_SCHEDULE_MODE_ONCE = 1,
    SLEEP_SCHEDULE_MODE_PERIODIC = 2,
} sleep_schedule_mode_t;

typedef struct {
    bool enabled;
    uint32_t revision;
    sleep_schedule_mode_t mode;
    /* Kept explicit even though DISPLAY_OFF is currently the only accepted
     * target, so a later verified power depth is an additive contract. */
    uint8_t target_power_state;
    char timezone[SLEEP_SCHEDULE_TIMEZONE_CAPACITY];

    /* Once schedules use absolute Unix milliseconds and half-open [start,end).
     * When trusted wall time reaches `end`, the schedule worker durably retires
     * the one-shot record instead of treating it as a permanently enabled
     * historical policy. Periodic schedules use local minute-of-day values in
     * [0,1439]. */
    int64_t once_start_epoch_ms;
    int64_t once_end_epoch_ms;
    uint16_t start_minute_of_day;
    uint16_t end_minute_of_day;
    /* Bit 0 is Monday and bit 6 is Sunday.  A periodic window is active when
     * the civil day containing its start has the corresponding bit set. */
    uint8_t weekday_mask;
    /* A physical manual wake can keep an active periodic/once window awake
     * for this bounded period before the schedule is evaluated again. */
    uint32_t manual_wake_override_seconds;
} sleep_schedule_t;

typedef struct {
    bool initialized;
    bool enabled;
    bool active_window;
    bool override_active;
    bool display_off_requested;
    uint32_t revision;
    int64_t next_transition_epoch;
    int64_t manual_override_until_epoch;
} sleep_schedule_status_t;

/* Emitted after the shared schedule worker has attempted to restore a
 * DISPLAY_OFF panel at the end of a rest window. It is a domain-to-
 * composition seam: Sleep Schedule does not know renderers, board profiles,
 * or App UI surfaces. The observer runs in the schedule worker and must
 * return promptly. */
typedef void (*sleep_schedule_display_wake_observer_t)(device_status_t status,
                                                        void *context);

/* Lifecycle stays in the common Device contract. The JSON tool below keeps
 * esp_err_t because Device Tool Registry owns that protocol boundary. */
device_status_t sleep_schedule_service_init(void);
/* Quiesces the schedule worker and cancels its shared deadline.  Intended for
 * startup rollback before the alarm domain has been started. */
device_status_t sleep_schedule_service_deinit(uint32_t timeout_ms);
/* Installs the composition-owned observer for a schedule-end DISPLAY_OFF
 * wake. It is not a physical-input callback and must not create a
 * manual-wake override. */
device_status_t sleep_schedule_service_set_display_wake_observer(
    sleep_schedule_display_wake_observer_t observer, void *context);
esp_err_t sleep_schedule_service_execute_tool(const char *name, cJSON *arguments,
                                              const char *idempotency_key,
                                              cJSON **out_result, char *error,
                                              size_t error_size);
void sleep_schedule_service_get_status(sleep_schedule_status_t *out_status);

/* Returns the appropriate panel-off delay for a shared ambient transition.
 * During an active, non-overridden schedule window it returns one millisecond
 * so an earlier foreground scene cannot postpone the user-configured rest
 * window by the normal ambient-idle period. */
uint32_t sleep_schedule_service_adjust_display_off_delay(uint32_t ambient_delay_ms);

/* Called only after a real physical contact has actually restored a panel.
 * It never treats an ordinary input while the panel is already active as a
 * user override. */
void sleep_schedule_service_note_manual_wake(void);

/* Called after SNTP or authenticated Hub time has changed wall clock.  Periodic
 * windows must be recalculated from civil time rather than waiting for a
 * transition computed before the correction. */
void sleep_schedule_service_on_wall_clock_updated(void);

/* Future System Sleep participant. PREPARE closes policy/tool/clock/manual
 * admission and waits for an already-admitted evaluation to finish; ABORT
 * reopens the same running worker and asks it to re-evaluate durable policy.
 * It does not enter MCU sleep, alter a profile, or manufacture an RTC wake. */
device_status_t sleep_schedule_service_prepare_system_sleep(uint32_t timeout_ms);
void sleep_schedule_service_abort_system_sleep_prepare(void);
