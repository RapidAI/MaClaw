#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

#define ALARM_WAKE_PLAN_ABI_VERSION 1u

typedef enum {
    ALARM_WAKE_PLAN_ALLOW = 0,
    ALARM_WAKE_PLAN_DENY_INVALID_INPUT,
    ALARM_WAKE_PLAN_DENY_UNVERIFIED_WAKE,
    ALARM_WAKE_PLAN_DENY_UNTRUSTED_TIME,
    ALARM_WAKE_PLAN_DENY_ALARM_DUE,
} alarm_wake_plan_result_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    device_power_state_t target_state;
    device_wake_source_flags_t verified_sources;
    bool wall_clock_trusted;
    int64_t wall_clock_epoch_ms;
    int64_t earliest_alarm_epoch_ms;
    /* Profile-measured wake lead and drift guard.  Unmeasured profiles pass
     * zero and remain fail-closed at the electrical capability gate. */
    uint32_t boot_lead_ms;
    uint32_t drift_guard_ms;
} alarm_wake_plan_input_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    alarm_wake_plan_result_t result;
    bool allow_sleep;
    bool alarm_wake_required;
    int64_t wake_arm_epoch_ms;
    int64_t wake_deadline_epoch_ms;
} alarm_wake_plan_output_t;

alarm_wake_plan_result_t alarm_wake_plan_compute(
    const alarm_wake_plan_input_t *input,
    alarm_wake_plan_output_t *output);

/* Revalidates a previously planned deadline after wall-clock correction.
 * This is the drift-compensation seam for a future RTC adapter: it only
 * returns a still-future epoch value and never arms hardware. */
alarm_wake_plan_result_t alarm_wake_plan_revalidate_deadline(
    int64_t planned_deadline_epoch_ms,
    int64_t current_wall_clock_epoch_ms,
    bool wall_clock_trusted,
    int64_t *out_deadline_epoch_ms);
