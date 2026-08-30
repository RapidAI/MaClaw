#include "alarm_wake_plan.h"

#include <stdint.h>

static bool valid_target(device_power_state_t state) {
    return state == DEVICE_POWER_STATE_LIGHT_SLEEP ||
           state == DEVICE_POWER_STATE_DEEP_SLEEP;
}

alarm_wake_plan_result_t alarm_wake_plan_compute(
    const alarm_wake_plan_input_t *input,
    alarm_wake_plan_output_t *output) {
    if (!output) {
        return ALARM_WAKE_PLAN_DENY_INVALID_INPUT;
    }
    *output = (alarm_wake_plan_output_t){
        .struct_size = sizeof(*output),
        .abi_version = ALARM_WAKE_PLAN_ABI_VERSION,
        .result = ALARM_WAKE_PLAN_DENY_INVALID_INPUT,
    };
    if (!input || input->struct_size < sizeof(*input) ||
        input->abi_version != ALARM_WAKE_PLAN_ABI_VERSION) {
        return output->result;
    }
    if (!valid_target(input->target_state) ||
        (input->verified_sources & ~DEVICE_WAKE_SOURCE_KNOWN_MASK) != 0u ||
        input->earliest_alarm_epoch_ms < 0) {
        return output->result;
    }
    if (input->verified_sources == 0u) {
        output->result = ALARM_WAKE_PLAN_DENY_UNVERIFIED_WAKE;
        return output->result;
    }
    output->alarm_wake_required = input->earliest_alarm_epoch_ms != 0;
    if (!output->alarm_wake_required) {
        output->allow_sleep = true;
        output->result = ALARM_WAKE_PLAN_ALLOW;
        return output->result;
    }
    /* A queued alarm cannot rely on a human input source to wake the MCU;
     * the verified matrix must explicitly include the hardware timer path. */
    if ((input->verified_sources & DEVICE_WAKE_SOURCE_TIMER) == 0u) {
        output->result = ALARM_WAKE_PLAN_DENY_UNVERIFIED_WAKE;
        return output->result;
    }
    /* A queued alarm requires a trusted, positive wall-clock sample.  With
     * no queued alarm, the verified profile wake matrix is sufficient and a
     * device may plan sleep while time synchronisation is still pending. */
    output->result = alarm_wake_plan_revalidate_deadline(
        input->earliest_alarm_epoch_ms, input->wall_clock_epoch_ms,
        input->wall_clock_trusted, &output->wake_deadline_epoch_ms);
    if (output->result != ALARM_WAKE_PLAN_ALLOW) return output->result;
    const uint64_t lead_ms = (uint64_t)input->boot_lead_ms +
                             (uint64_t)input->drift_guard_ms;
    if (lead_ms < (uint64_t)input->boot_lead_ms ||
        lead_ms > (uint64_t)input->earliest_alarm_epoch_ms) {
        output->result = ALARM_WAKE_PLAN_DENY_INVALID_INPUT;
        output->wake_deadline_epoch_ms = 0;
        return output->result;
    }
    output->wake_arm_epoch_ms = input->earliest_alarm_epoch_ms -
                                (int64_t)lead_ms;
    if (output->wake_arm_epoch_ms <= input->wall_clock_epoch_ms) {
        output->result = ALARM_WAKE_PLAN_DENY_ALARM_DUE;
        output->wake_arm_epoch_ms = 0;
        output->wake_deadline_epoch_ms = 0;
        return output->result;
    }
    output->allow_sleep = true;
    output->result = ALARM_WAKE_PLAN_ALLOW;
    return output->result;
}

alarm_wake_plan_result_t alarm_wake_plan_revalidate_deadline(
    int64_t planned_deadline_epoch_ms,
    int64_t current_wall_clock_epoch_ms,
    bool wall_clock_trusted,
    int64_t *out_deadline_epoch_ms) {
    if (!out_deadline_epoch_ms || planned_deadline_epoch_ms <= 0 ||
        current_wall_clock_epoch_ms <= 0) {
        return ALARM_WAKE_PLAN_DENY_INVALID_INPUT;
    }
    *out_deadline_epoch_ms = 0;
    if (!wall_clock_trusted) return ALARM_WAKE_PLAN_DENY_UNTRUSTED_TIME;
    if (planned_deadline_epoch_ms <= current_wall_clock_epoch_ms) {
        return ALARM_WAKE_PLAN_DENY_ALARM_DUE;
    }
    *out_deadline_epoch_ms = planned_deadline_epoch_ms;
    return ALARM_WAKE_PLAN_ALLOW;
}
