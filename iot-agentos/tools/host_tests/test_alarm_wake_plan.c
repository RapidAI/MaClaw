#include <stdio.h>
#include "alarm_wake_plan.h"

#define CHECK(x) do { if (!(x)) { fprintf(stderr, "FAIL: %s\n", #x); return 1; } } while (0)

static alarm_wake_plan_input_t base_input(void) {
    return (alarm_wake_plan_input_t){
        .struct_size = sizeof(alarm_wake_plan_input_t),
        .abi_version = ALARM_WAKE_PLAN_ABI_VERSION,
        .target_state = DEVICE_POWER_STATE_LIGHT_SLEEP,
        .verified_sources = DEVICE_WAKE_SOURCE_PRIMARY_CONTROL | DEVICE_WAKE_SOURCE_TIMER,
        .wall_clock_trusted = true,
        .wall_clock_epoch_ms = 2000000,
    };
}

int main(void) {
    alarm_wake_plan_input_t input = base_input();
    alarm_wake_plan_output_t output = {0};
    output.allow_sleep = true;
    output.wake_deadline_epoch_ms = 99;
    input.struct_size = 0;
    CHECK(alarm_wake_plan_compute(&input, &output) == ALARM_WAKE_PLAN_DENY_INVALID_INPUT);
    CHECK(!output.allow_sleep && output.wake_deadline_epoch_ms == 0);
    input.struct_size = sizeof(input);
    input.earliest_alarm_epoch_ms = 3000000;
    input.boot_lead_ms = 1000;
    input.drift_guard_ms = 500;
    CHECK(alarm_wake_plan_compute(&input, &output) == ALARM_WAKE_PLAN_ALLOW);
    CHECK(output.allow_sleep && output.alarm_wake_required &&
          output.wake_deadline_epoch_ms == 3000000 &&
          output.wake_arm_epoch_ms == 2998500);
    input.verified_sources = DEVICE_WAKE_SOURCE_PRIMARY_CONTROL;
    CHECK(alarm_wake_plan_compute(&input, &output) == ALARM_WAKE_PLAN_DENY_UNVERIFIED_WAKE);
    input.verified_sources = DEVICE_WAKE_SOURCE_PRIMARY_CONTROL | DEVICE_WAKE_SOURCE_TIMER;
    input.earliest_alarm_epoch_ms = 0;
    input.wall_clock_epoch_ms = 0;
    input.wall_clock_trusted = false;
    CHECK(alarm_wake_plan_compute(&input, &output) == ALARM_WAKE_PLAN_ALLOW);
    CHECK(output.allow_sleep && !output.alarm_wake_required && output.wake_deadline_epoch_ms == 0);
    input.earliest_alarm_epoch_ms = 3000000;
    input.wall_clock_epoch_ms = 2000000;
    input.boot_lead_ms = 0;
    input.drift_guard_ms = 0;
    input.wall_clock_trusted = false;
    CHECK(alarm_wake_plan_compute(&input, &output) == ALARM_WAKE_PLAN_DENY_UNTRUSTED_TIME);
    input.wall_clock_trusted = true;
    input.wall_clock_epoch_ms = 3000000;
    CHECK(alarm_wake_plan_compute(&input, &output) == ALARM_WAKE_PLAN_DENY_ALARM_DUE);
    input.wall_clock_epoch_ms = 1000;
    input.verified_sources = 0;
    CHECK(alarm_wake_plan_compute(&input, &output) == ALARM_WAKE_PLAN_DENY_UNVERIFIED_WAKE);
    input.verified_sources = DEVICE_WAKE_SOURCE_PRIMARY_CONTROL;
    input.target_state = DEVICE_POWER_STATE_DISPLAY_OFF;
    CHECK(alarm_wake_plan_compute(&input, &output) == ALARM_WAKE_PLAN_DENY_INVALID_INPUT);
    int64_t revalidated = 99;
    CHECK(alarm_wake_plan_revalidate_deadline(3000000, 2500000, true,
                                              &revalidated) == ALARM_WAKE_PLAN_ALLOW);
    CHECK(revalidated == 3000000);
    CHECK(alarm_wake_plan_revalidate_deadline(3000000, 3000000, true,
                                              &revalidated) == ALARM_WAKE_PLAN_DENY_ALARM_DUE);
    CHECK(revalidated == 0);
    CHECK(alarm_wake_plan_revalidate_deadline(3000000, 2500000, false,
                                              &revalidated) == ALARM_WAKE_PLAN_DENY_UNTRUSTED_TIME);
    CHECK(alarm_wake_plan_revalidate_deadline(0, 2500000, true,
                                              &revalidated) == ALARM_WAKE_PLAN_DENY_INVALID_INPUT);
    input.target_state = DEVICE_POWER_STATE_LIGHT_SLEEP;
    input.verified_sources = DEVICE_WAKE_SOURCE_PRIMARY_CONTROL | DEVICE_WAKE_SOURCE_TIMER;
    input.earliest_alarm_epoch_ms = 3000000;
    input.wall_clock_epoch_ms = 2000000;
    input.wall_clock_trusted = true;
    input.boot_lead_ms = UINT32_MAX;
    input.drift_guard_ms = UINT32_MAX;
    CHECK(alarm_wake_plan_compute(&input, &output) == ALARM_WAKE_PLAN_DENY_INVALID_INPUT);
    CHECK(!output.allow_sleep && output.wake_deadline_epoch_ms == 0);
    puts("PASS alarm wake plan value contract");
    return 0;
}
