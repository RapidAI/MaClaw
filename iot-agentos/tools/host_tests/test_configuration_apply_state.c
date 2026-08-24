#include <stdio.h>
#include <string.h>

#include "configuration_apply_state.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "check failed at %d: %s\n", __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static configuration_effective_revisioned_snapshot_t effective(
    uint64_t durable_revision, uint64_t override_revision,
    uint8_t volume, uint8_t brightness, uint32_t screen_sleep_seconds,
    bool screen_sleep_saved) {
    return (configuration_effective_revisioned_snapshot_t){
        .struct_size = sizeof(configuration_effective_revisioned_snapshot_t),
        .abi_version = CONFIGURATION_EFFECTIVE_REVISIONED_SNAPSHOT_ABI_VERSION,
        .durable_revision = durable_revision,
        .runtime_override_revision = override_revision,
        .runtime_override_mask = 3u,
        .snapshot = {
            .output_volume = volume,
            .output_volume_saved = true,
            .display_brightness = brightness,
            .display_brightness_saved = true,
            .screen_sleep_seconds = screen_sleep_seconds,
            .screen_sleep_seconds_saved = screen_sleep_saved,
        },
    };
}

int main(void) {
    configuration_apply_state_t state = {0};
    configuration_apply_state_init(&state);
    CHECK(state.struct_size == sizeof(state));
    CHECK(!configuration_apply_state_is_converged(&state));

    configuration_effective_revisioned_snapshot_t first = effective(7u, 2u, 35u, 60u, 300u, true);
    CHECK(configuration_apply_state_begin(&state, &first));
    CHECK(state.desired_output_volume == 35u && state.desired_display_brightness == 60u);
    CHECK(state.output_volume.observation == CONFIGURATION_APPLY_OBSERVATION_PENDING);
    CHECK(!configuration_apply_state_is_converged(&state));
    CHECK(!configuration_apply_state_record_output_volume(&state, 6u, 2u,
                                                           CONFIGURATION_APPLY_OBSERVATION_APPLIED));
    CHECK(configuration_apply_state_record_output_volume(&state, 7u, 2u,
                                                          CONFIGURATION_APPLY_OBSERVATION_APPLIED));
    CHECK(state.output_volume.known && state.output_volume.value == 35u);
    CHECK(configuration_apply_state_record_display_brightness(&state, 7u, 2u,
                                                              CONFIGURATION_APPLY_OBSERVATION_APPLIED));
    CHECK(configuration_apply_state_record_screen_sleep_seconds(&state, 7u, 2u,
                                                                 CONFIGURATION_APPLY_OBSERVATION_APPLIED));
    CHECK(configuration_apply_state_is_converged(&state));
    CHECK(!configuration_apply_state_output_volume_needs_apply(&state));
    CHECK(!configuration_apply_state_display_brightness_needs_apply(&state));
    CHECK(!configuration_apply_state_screen_sleep_seconds_needs_apply(&state));

    /* Same generation is idempotent: a retry must not erase positive proof. */
    CHECK(configuration_apply_state_begin(&state, &first));
    CHECK(configuration_apply_state_is_converged(&state));

    /* Requirements are immutable within one durable/override generation.
     * A later retry must not reinterpret a boot-visible non-apply as an
     * instruction to send a hardware command for the same revision. */
    CHECK(configuration_apply_state_begin_with_requirements(
        &state, &first, false, true, true));
    CHECK(configuration_apply_state_is_converged(&state));

    configuration_effective_revisioned_snapshot_t second = effective(7u, 3u, 80u, 20u, 600u, true);
    CHECK(configuration_apply_state_begin(&state, &second));
    CHECK(!configuration_apply_state_is_converged(&state));
    CHECK(state.output_volume.known && state.output_volume.value == 35u);
    CHECK(state.output_volume.observation == CONFIGURATION_APPLY_OBSERVATION_PENDING);
    CHECK(configuration_apply_state_record_output_volume(&state, 7u, 3u,
                                                          CONFIGURATION_APPLY_OBSERVATION_FAILED));
    /* A confirmed failure retains the last known actual state; it is not
     * convergence with the new desired value. */
    CHECK(state.output_volume.known && state.output_volume.value == 35u);
    CHECK(!configuration_apply_state_is_converged(&state));
    CHECK(configuration_apply_state_output_volume_needs_apply(&state));
    CHECK(configuration_apply_state_record_display_brightness(&state, 7u, 3u,
                                                              CONFIGURATION_APPLY_OBSERVATION_UNKNOWN));
    CHECK(!state.display_brightness.known);
    CHECK(configuration_apply_state_record_screen_sleep_seconds(&state, 7u, 3u,
                                                                 CONFIGURATION_APPLY_OBSERVATION_FAILED));
    CHECK(state.screen_sleep_seconds.known && state.screen_sleep_seconds.value == 300u);
    CHECK(!configuration_apply_state_record_display_brightness(&state, 7u, 2u,
                                                               CONFIGURATION_APPLY_OBSERVATION_APPLIED));

    /* Once one field is proven, a retry for another failed field must not
     * resend that already acknowledged consumer command. */
    CHECK(configuration_apply_state_record_display_brightness(
        &state, 7u, 3u, CONFIGURATION_APPLY_OBSERVATION_APPLIED));
    CHECK(!configuration_apply_state_display_brightness_needs_apply(&state));
    CHECK(configuration_apply_state_output_volume_needs_apply(&state));
    CHECK(configuration_apply_state_screen_sleep_seconds_needs_apply(&state));

    /* Defaults that were never published do not require a consumer command.
     * They must not leave the generation falsely PENDING just because no
     * output-volume/brightness acknowledgement will ever arrive. */
    configuration_effective_revisioned_snapshot_t defaults = effective(8u, 4u, 70u, 50u, 0u, false);
    defaults.snapshot.output_volume_saved = false;
    defaults.snapshot.display_brightness_saved = false;
    CHECK(configuration_apply_state_begin(&state, &defaults));
    CHECK(!state.output_volume_policy_required && !state.display_brightness_policy_required);
    CHECK(state.output_volume.observation == CONFIGURATION_APPLY_OBSERVATION_APPLIED);
    CHECK(state.display_brightness.observation == CONFIGURATION_APPLY_OBSERVATION_APPLIED);
    CHECK(configuration_apply_state_is_converged(&state));
    CHECK(!configuration_apply_state_record_output_volume(
        &state, 8u, 4u, CONFIGURATION_APPLY_OBSERVATION_APPLIED));
    CHECK(!configuration_apply_state_record_display_brightness(
        &state, 8u, 4u, CONFIGURATION_APPLY_OBSERVATION_APPLIED));

    /* The same protection applies to the intentional boot-visible
     * brightness=0 omission: runtime retry must wait for a new generation,
     * not turn this generation into a black-panel command. */
    configuration_effective_revisioned_snapshot_t visible_boot =
        effective(9u, 5u, 45u, 0u, 300u, true);
    CHECK(configuration_apply_state_begin_with_requirements(
        &state, &visible_boot, true, false, true));
    CHECK(configuration_apply_state_begin_with_requirements(
        &state, &visible_boot, true, true, true));
    CHECK(!state.display_brightness_policy_required);

    configuration_effective_revisioned_snapshot_t no_idle =
        effective(8u, 3u, 80u, 20u, 0u, false);
    CHECK(configuration_apply_state_begin(&state, &no_idle));
    CHECK(!state.screen_sleep_policy_required);
    CHECK(!configuration_apply_state_record_screen_sleep_seconds(
        &state, 8u, 3u, CONFIGURATION_APPLY_OBSERVATION_APPLIED));

    configuration_effective_revisioned_snapshot_t invalid = second;
    invalid.runtime_override_revision = 0u;
    configuration_apply_state_t before = state;
    CHECK(!configuration_apply_state_begin(&state, &invalid));
    CHECK(memcmp(&before, &state, sizeof(state)) == 0);
    return 0;
}
