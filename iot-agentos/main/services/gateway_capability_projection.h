#pragma once

/*
 * Gateway capability projection (B5 foundation).
 *
 * The device's physical/compiled feature set, the Hub's handshake acceptance,
 * and the currently operational feature set are deliberately distinct values.
 * This keeps a future Hub message or link-health callback from turning a
 * board-specific capability into business policy.  The module is pure value
 * logic: transport, JSON parsing, clocks, tasks and board adapters remain at
 * their existing owners.
 */

#include <stdbool.h>
#include <stdint.h>

typedef uint32_t gateway_capability_flags_t;

enum {
    GATEWAY_CAPABILITY_INPUT_TEXT = 1u << 0,
    GATEWAY_CAPABILITY_INPUT_AUDIO = 1u << 1,
    GATEWAY_CAPABILITY_OUTPUT_TEXT = 1u << 2,
    GATEWAY_CAPABILITY_OUTPUT_AUDIO = 1u << 3,
    GATEWAY_CAPABILITY_OUTPUT_IMAGE = 1u << 4,
    GATEWAY_CAPABILITY_PET_STATE = 1u << 5,
    GATEWAY_CAPABILITY_PET_ANIMATION = 1u << 6,
    GATEWAY_CAPABILITY_PET_ASSET = 1u << 7,
    GATEWAY_CAPABILITY_AMBIENT_DISPLAY = 1u << 8,
    GATEWAY_CAPABILITY_MEETING_RECORDER = 1u << 9,
    GATEWAY_CAPABILITY_VOLUME_CONTROL = 1u << 10,
    GATEWAY_CAPABILITY_BRIGHTNESS_CONTROL = 1u << 11,
    GATEWAY_CAPABILITY_SCREEN_SLEEP_CONTROL = 1u << 12,
};

#define GATEWAY_CAPABILITY_KNOWN_MASK \
    (GATEWAY_CAPABILITY_INPUT_TEXT | GATEWAY_CAPABILITY_INPUT_AUDIO | \
     GATEWAY_CAPABILITY_OUTPUT_TEXT | GATEWAY_CAPABILITY_OUTPUT_AUDIO | \
     GATEWAY_CAPABILITY_OUTPUT_IMAGE | GATEWAY_CAPABILITY_PET_STATE | \
     GATEWAY_CAPABILITY_PET_ANIMATION | GATEWAY_CAPABILITY_PET_ASSET | \
     GATEWAY_CAPABILITY_AMBIENT_DISPLAY | \
     GATEWAY_CAPABILITY_MEETING_RECORDER | \
     GATEWAY_CAPABILITY_VOLUME_CONTROL | \
     GATEWAY_CAPABILITY_BRIGHTNESS_CONTROL | \
     GATEWAY_CAPABILITY_SCREEN_SLEEP_CONTROL)

typedef enum {
    GATEWAY_CAPABILITY_HEALTH_UNKNOWN = 0,
    GATEWAY_CAPABILITY_HEALTH_HEALTHY,
    GATEWAY_CAPABILITY_HEALTH_DEGRADED,
    GATEWAY_CAPABILITY_HEALTH_UNAVAILABLE,
} gateway_capability_health_t;

#define GATEWAY_CAPABILITY_PROJECTION_ABI_VERSION 1u
#define GATEWAY_CAPABILITY_FAILURES_TO_UNAVAILABLE 3u
#define GATEWAY_CAPABILITY_SUCCESSES_TO_HEALTHY 2u

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    gateway_capability_flags_t effective_capabilities;
    gateway_capability_flags_t accepted_capabilities;
    gateway_capability_flags_t negotiated_capabilities;
    gateway_capability_flags_t operational_capabilities;
    /* Increments whenever acceptance is withdrawn or the negotiated set
     * changes. Future asynchronous consumers must bind work to this value
     * before they retain handles across an operation boundary. */
    uint32_t generation;
    gateway_capability_health_t health;
    uint8_t consecutive_failures;
    uint8_t consecutive_successes;
    bool acceptance_observed;
} gateway_capability_projection_t;

/* A capability lease is the only value an asynchronous consumer retains
 * across an operation boundary. It deliberately contains neither a task,
 * transport handle nor board detail. A lease is invalid as soon as the
 * projection generation changes or its required surface is no longer
 * operational. */
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    gateway_capability_flags_t required_capabilities;
    uint32_t generation;
} gateway_capability_lease_t;

#define GATEWAY_CAPABILITY_LEASE_ABI_VERSION 1u

/* Initializes an empty projection. It is not usable until a valid local
 * effective set and a Hub acceptance observation have both been supplied. */
void gateway_capability_projection_init(gateway_capability_projection_t *projection);

/* Replaces the compiled/local effective set. Any former Hub acceptance and
 * health evidence belongs to the old generation and is cleared. */
bool gateway_capability_projection_set_effective(
    gateway_capability_projection_t *projection,
    gateway_capability_flags_t effective_capabilities);

/* Records the complete Hub acceptance set for the current effective set.
 * An unknown bit or an acceptance of a capability the device did not offer is
 * malformed evidence and leaves the prior projection unchanged. */
bool gateway_capability_projection_observe_accepted(
    gateway_capability_projection_t *projection,
    gateway_capability_flags_t accepted_capabilities);

/* Withdraws the current Hub acceptance after a missing, malformed or
 * incompatible handshake acknowledgement. This is intentionally distinct
 * from a transient transport failure: callers must not leave a previously
 * accepted business surface enabled when the Hub response is untrustworthy. */
bool gateway_capability_projection_withdraw_acceptance(
    gateway_capability_projection_t *projection);

/* Applies a completed capability-bearing transport observation. A single
 * transient failure only degrades a previously healthy projection; three
 * consecutive failures withdraw operational capabilities. Two consecutive
 * successes are required to restore HEALTHY after UNKNOWN/DEGRADED/UNAVAILABLE.
 */
bool gateway_capability_projection_observe_transport_result(
    gateway_capability_projection_t *projection, bool success);

/* Copies an internally valid snapshot. It rejects malformed/corrupt caller
 * supplied state rather than publishing a capability that was never accepted. */
bool gateway_capability_projection_snapshot(
    const gateway_capability_projection_t *projection,
    gateway_capability_projection_t *out_snapshot);

/* Captures an operation lease only from the current operational surface. A
 * zero, unknown, or currently unavailable requirement is rejected. */
bool gateway_capability_projection_capture_lease(
    const gateway_capability_projection_t *projection,
    gateway_capability_flags_t required_capabilities,
    gateway_capability_lease_t *out_lease);

/* Validates that an already captured lease still names the current projection
 * generation and that every required capability remains operational. */
bool gateway_capability_projection_lease_current(
    const gateway_capability_projection_t *projection,
    const gateway_capability_lease_t *lease);
