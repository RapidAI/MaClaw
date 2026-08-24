#pragma once

/*
 * Revision-bound configuration apply observation.
 *
 * Configuration Service remains the only durable/volatile value owner.  This
 * small value object lets the composition-root reconciliation owner describe
 * what it asked reversible consumers to do and what those consumers have
 * actually acknowledged.  It contains no timer, mutex, task, NVS, network or
 * hardware handle: one serial reconciliation owner must retain and update it.
 *
 * Keeping "desired" distinct from "observed" prevents a failed panel/codec
 * call from being silently represented as an already-active configuration.
 * It also gives a later expiry/retry owner a stable way to reject a late
 * completion for an older durable/override revision pair.
 */

#include <stdbool.h>
#include <stdint.h>

#include "configuration_service.h"

#define CONFIGURATION_APPLY_STATE_ABI_VERSION 1u

typedef enum {
    CONFIGURATION_APPLY_OBSERVATION_PENDING = 0,
    CONFIGURATION_APPLY_OBSERVATION_APPLIED,
    CONFIGURATION_APPLY_OBSERVATION_FAILED,
    /* An external operation may have crossed its side-effect boundary before
     * returning failure.  In that case no retry path may assume the prior
     * observed value is still physically active. */
    CONFIGURATION_APPLY_OBSERVATION_UNKNOWN,
} configuration_apply_observation_t;

typedef struct {
    bool known;
    uint8_t value;
    configuration_apply_observation_t observation;
} configuration_apply_value_state_t;

typedef struct {
    bool known;
    uint32_t value;
    configuration_apply_observation_t observation;
} configuration_apply_u32_state_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    bool desired_valid;
    uint64_t durable_revision;
    uint64_t runtime_override_revision;
    uint32_t runtime_override_mask;
    uint8_t desired_output_volume;
    uint8_t desired_display_brightness;
    uint32_t desired_screen_sleep_seconds;
    /* A copied effective snapshot can contain profile/default values for a
     * key which was never persisted. Reconciliation may deliberately retain
     * the already-established boot default instead of issuing a hardware
     * command. Required flags therefore define exactly which consumer
     * completions contribute to this generation's convergence proof. */
    bool output_volume_policy_required;
    bool display_brightness_policy_required;
    /* An unsaved durable timeout deliberately leaves the profile default in
     * place; it has no common policy value to apply or observe. */
    bool screen_sleep_policy_required;
    configuration_apply_value_state_t output_volume;
    configuration_apply_value_state_t display_brightness;
    configuration_apply_u32_state_t screen_sleep_seconds;
} configuration_apply_state_t;

void configuration_apply_state_init(configuration_apply_state_t *state);

/* Starts a new desired generation from one copied effective snapshot.  The
 * previous observation is retained as evidence until each consumer reports a
 * result, but its outcome becomes PENDING.  Supplying the same generation is
 * idempotent and deliberately does not erase a completed observation or
 * reinterpret its required-consumer set. */
bool configuration_apply_state_begin(
    configuration_apply_state_t *state,
    const configuration_effective_revisioned_snapshot_t *effective);

/* Composition owners may intentionally retain a boot-visible profile default
 * instead of replaying an otherwise saved live command (for example persisted
 * brightness=0). This variant makes that omission explicit in the same
 * revision-bound value state: a non-required field receives no completion and
 * cannot be mistaken for a pending hardware apply.  Requirements are captured
 * only when this call creates a new revision pair; later calls for that pair
 * leave them intact. */
bool configuration_apply_state_begin_with_requirements(
    configuration_apply_state_t *state,
    const configuration_effective_revisioned_snapshot_t *effective,
    bool output_volume_policy_required,
    bool display_brightness_policy_required,
    bool screen_sleep_policy_required);

/* Records a consumer completion only for the currently desired revision pair.
 * `applied` retains a known actual value. `failed` retains prior evidence (if
 * any), while `unknown` removes it because a caller cannot prove rollback.
 * A stale revision pair is rejected without changing state. */
bool configuration_apply_state_record_output_volume(
    configuration_apply_state_t *state,
    uint64_t durable_revision,
    uint64_t runtime_override_revision,
    configuration_apply_observation_t observation);
bool configuration_apply_state_record_display_brightness(
    configuration_apply_state_t *state,
    uint64_t durable_revision,
    uint64_t runtime_override_revision,
    configuration_apply_observation_t observation);
bool configuration_apply_state_record_screen_sleep_seconds(
    configuration_apply_state_t *state,
    uint64_t durable_revision,
    uint64_t runtime_override_revision,
    configuration_apply_observation_t observation);

/* Reports whether one required field still lacks proof for this generation.
 * Reconciliation uses these value-only queries to retry only the failed or
 * unknown consumer, instead of reissuing already acknowledged codec/panel/
 * scheduler commands while another key is recovering. */
bool configuration_apply_state_output_volume_needs_apply(
    const configuration_apply_state_t *state);
bool configuration_apply_state_display_brightness_needs_apply(
    const configuration_apply_state_t *state);
bool configuration_apply_state_screen_sleep_seconds_needs_apply(
    const configuration_apply_state_t *state);

bool configuration_apply_state_is_converged(
    const configuration_apply_state_t *state);
