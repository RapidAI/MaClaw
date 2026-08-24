#include "configuration_apply_state.h"

#include <string.h>

static bool valid_state(const configuration_apply_state_t *state) {
    return state && state->struct_size == sizeof(*state) &&
           state->abi_version == CONFIGURATION_APPLY_STATE_ABI_VERSION;
}

static bool valid_effective(
    const configuration_effective_revisioned_snapshot_t *effective) {
    return effective && effective->struct_size == sizeof(*effective) &&
           effective->abi_version == CONFIGURATION_EFFECTIVE_REVISIONED_SNAPSHOT_ABI_VERSION &&
           effective->durable_revision != 0u &&
           effective->runtime_override_revision != 0u &&
           effective->snapshot.output_volume <= 100u &&
           effective->snapshot.display_brightness <= 100u;
}

static bool current_generation(const configuration_apply_state_t *state,
                               uint64_t durable_revision,
                               uint64_t runtime_override_revision) {
    return valid_state(state) && state->desired_valid &&
           state->durable_revision == durable_revision &&
           state->runtime_override_revision == runtime_override_revision;
}

static bool record(configuration_apply_value_state_t *value,
                   uint8_t desired,
                   configuration_apply_observation_t observation) {
    if (!value || observation > CONFIGURATION_APPLY_OBSERVATION_UNKNOWN) return false;
    value->observation = observation;
    if (observation == CONFIGURATION_APPLY_OBSERVATION_APPLIED) {
        value->known = true;
        value->value = desired;
    } else if (observation == CONFIGURATION_APPLY_OBSERVATION_UNKNOWN) {
        value->known = false;
        value->value = 0u;
    }
    return true;
}

static bool record_u32(configuration_apply_u32_state_t *value,
                       uint32_t desired,
                       configuration_apply_observation_t observation) {
    if (!value || observation > CONFIGURATION_APPLY_OBSERVATION_UNKNOWN) return false;
    value->observation = observation;
    if (observation == CONFIGURATION_APPLY_OBSERVATION_APPLIED) {
        value->known = true;
        value->value = desired;
    } else if (observation == CONFIGURATION_APPLY_OBSERVATION_UNKNOWN) {
        value->known = false;
        value->value = 0u;
    }
    return true;
}

static bool value_needs_apply(const configuration_apply_value_state_t *value,
                              bool required,
                              uint8_t desired) {
    return required &&
           (!value || !value->known || value->value != desired ||
            value->observation != CONFIGURATION_APPLY_OBSERVATION_APPLIED);
}

static bool u32_needs_apply(const configuration_apply_u32_state_t *value,
                            bool required,
                            uint32_t desired) {
    return required &&
           (!value || !value->known || value->value != desired ||
            value->observation != CONFIGURATION_APPLY_OBSERVATION_APPLIED);
}

void configuration_apply_state_init(configuration_apply_state_t *state) {
    if (!state) return;
    *state = (configuration_apply_state_t){
        .struct_size = sizeof(*state),
        .abi_version = CONFIGURATION_APPLY_STATE_ABI_VERSION,
    };
}

bool configuration_apply_state_begin(
    configuration_apply_state_t *state,
    const configuration_effective_revisioned_snapshot_t *effective) {
    if (!effective) return false;
    return configuration_apply_state_begin_with_requirements(
        state, effective, effective->snapshot.output_volume_saved,
        effective->snapshot.display_brightness_saved,
        effective->snapshot.screen_sleep_seconds_saved);
}

bool configuration_apply_state_begin_with_requirements(
    configuration_apply_state_t *state,
    const configuration_effective_revisioned_snapshot_t *effective,
    bool output_volume_policy_required,
    bool display_brightness_policy_required,
    bool screen_sleep_policy_required) {
    if (!valid_state(state) || !valid_effective(effective)) return false;
    if (current_generation(state, effective->durable_revision,
                           effective->runtime_override_revision)) {
        /* Required-ness is part of the semantic apply generation, not a
         * transient caller preference.  In particular BOOT_RESTORE may keep
         * a visible profile default for a persisted brightness=0, while the
         * retained retry worker must not later reinterpret that same revision
         * as a command to black the panel.  The serial owner reads these
         * retained flags after begin; ignore later caller metadata so it
         * neither erases prior evidence nor manufactures a second meaning for
         * one revision pair. */
        return true;
    }
    state->desired_valid = true;
    state->durable_revision = effective->durable_revision;
    state->runtime_override_revision = effective->runtime_override_revision;
    state->runtime_override_mask = effective->runtime_override_mask;
    state->desired_output_volume = effective->snapshot.output_volume;
    state->desired_display_brightness = effective->snapshot.display_brightness;
    state->desired_screen_sleep_seconds = effective->snapshot.screen_sleep_seconds;
    state->output_volume_policy_required = output_volume_policy_required;
    state->display_brightness_policy_required = display_brightness_policy_required;
    state->screen_sleep_policy_required = screen_sleep_policy_required;
    state->output_volume.observation = state->output_volume_policy_required
                                           ? CONFIGURATION_APPLY_OBSERVATION_PENDING
                                           : CONFIGURATION_APPLY_OBSERVATION_APPLIED;
    state->display_brightness.observation = state->display_brightness_policy_required
                                                ? CONFIGURATION_APPLY_OBSERVATION_PENDING
                                                : CONFIGURATION_APPLY_OBSERVATION_APPLIED;
    state->screen_sleep_seconds.observation =
        state->screen_sleep_policy_required ? CONFIGURATION_APPLY_OBSERVATION_PENDING
                                            : CONFIGURATION_APPLY_OBSERVATION_APPLIED;
    return true;
}

bool configuration_apply_state_record_output_volume(
    configuration_apply_state_t *state,
    uint64_t durable_revision,
    uint64_t runtime_override_revision,
    configuration_apply_observation_t observation) {
    if (!current_generation(state, durable_revision, runtime_override_revision) ||
        !state->output_volume_policy_required) return false;
    return record(&state->output_volume, state->desired_output_volume, observation);
}

bool configuration_apply_state_record_display_brightness(
    configuration_apply_state_t *state,
    uint64_t durable_revision,
    uint64_t runtime_override_revision,
    configuration_apply_observation_t observation) {
    if (!current_generation(state, durable_revision, runtime_override_revision) ||
        !state->display_brightness_policy_required) return false;
    return record(&state->display_brightness, state->desired_display_brightness, observation);
}

bool configuration_apply_state_record_screen_sleep_seconds(
    configuration_apply_state_t *state,
    uint64_t durable_revision,
    uint64_t runtime_override_revision,
    configuration_apply_observation_t observation) {
    if (!current_generation(state, durable_revision, runtime_override_revision) ||
        !state->screen_sleep_policy_required) {
        return false;
    }
    return record_u32(&state->screen_sleep_seconds,
                      state->desired_screen_sleep_seconds, observation);
}

bool configuration_apply_state_output_volume_needs_apply(
    const configuration_apply_state_t *state) {
    return valid_state(state) && state->desired_valid &&
           value_needs_apply(&state->output_volume,
                             state->output_volume_policy_required,
                             state->desired_output_volume);
}

bool configuration_apply_state_display_brightness_needs_apply(
    const configuration_apply_state_t *state) {
    return valid_state(state) && state->desired_valid &&
           value_needs_apply(&state->display_brightness,
                             state->display_brightness_policy_required,
                             state->desired_display_brightness);
}

bool configuration_apply_state_screen_sleep_seconds_needs_apply(
    const configuration_apply_state_t *state) {
    return valid_state(state) && state->desired_valid &&
           u32_needs_apply(&state->screen_sleep_seconds,
                           state->screen_sleep_policy_required,
                           state->desired_screen_sleep_seconds);
}

bool configuration_apply_state_is_converged(
    const configuration_apply_state_t *state) {
    return valid_state(state) && state->desired_valid &&
           !configuration_apply_state_output_volume_needs_apply(state) &&
           !configuration_apply_state_display_brightness_needs_apply(state) &&
           !configuration_apply_state_screen_sleep_seconds_needs_apply(state);
}
