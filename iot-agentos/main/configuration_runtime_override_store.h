#pragma once

/*
 * Volatile runtime-override store.
 *
 * One composition-root owner allocates this value and serializes access to
 * it. The store deliberately owns no mutex, timer, NVS record, task or board
 * handle: a restart loses every override, and a future sleep/restart owner
 * explicitly calls clear(). This makes precedence and expiry common to all
 * hardware profiles without allowing a panel, codec or transport adapter to
 * keep a private interpretation of remote policy.
 */

#include <stdbool.h>
#include <stdint.h>

#include "configuration_effective_policy.h"

#define CONFIGURATION_RUNTIME_OVERRIDE_STORE_ABI_VERSION 1u
#define CONFIGURATION_RUNTIME_OVERRIDE_STORE_SLOT_COUNT 4u

typedef enum {
    CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK = 0,
    CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT,
    CONFIGURATION_RUNTIME_OVERRIDE_STORE_EXPIRED,
    CONFIGURATION_RUNTIME_OVERRIDE_STORE_NOT_FOUND,
    CONFIGURATION_RUNTIME_OVERRIDE_STORE_REVISION_EXHAUSTED,
} configuration_runtime_override_store_result_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    /* Starts at one. Every accepted replacement, explicit clear, or expiry
     * removal advances it so a consumer can bind one effective snapshot. */
    uint64_t effective_revision;
    bool occupied[CONFIGURATION_RUNTIME_OVERRIDE_STORE_SLOT_COUNT];
    configuration_runtime_override_t slots[CONFIGURATION_RUNTIME_OVERRIDE_STORE_SLOT_COUNT];
} configuration_runtime_override_store_t;

/* Initializes a caller-owned, empty volatile store. */
void configuration_runtime_override_store_init(
    configuration_runtime_override_store_t *store);

/* Atomically validates then installs/replaces the one slot for override->kind.
 * The input must carry authenticated runtime-override provenance and have a
 * live, bounded monotonic expiry. No durable Configuration data is changed. */
configuration_runtime_override_store_result_t
configuration_runtime_override_store_put(
    configuration_runtime_override_store_t *store,
    const configuration_runtime_override_t *override,
    uint64_t now_monotonic_ms);

/* Removes a one-key override. `clear_all` is the mandatory lifecycle action
 * before a clock domain is reset (restart or a sleep mode that cannot retain
 * the same monotonic epoch). */
configuration_runtime_override_store_result_t
configuration_runtime_override_store_remove(
    configuration_runtime_override_store_t *store,
    configuration_runtime_override_value_kind_t kind);
configuration_runtime_override_store_result_t
configuration_runtime_override_store_clear_all(
    configuration_runtime_override_store_t *store);

/* Removes elapsed entries. Calling this before consumer reconciliation gives
 * expiry one common effective revision rather than a per-consumer timeout. */
configuration_runtime_override_store_result_t
configuration_runtime_override_store_discard_expired(
    configuration_runtime_override_store_t *store,
    uint64_t now_monotonic_ms,
    bool *out_changed);

/* Resolves durable Configuration plus every live volatile override into one
 * copied snapshot. The function first discards elapsed records, never writes
 * durable state, and returns the effective revision/mask for reconciliation. */
configuration_runtime_override_store_result_t
configuration_runtime_override_store_resolve(
    configuration_runtime_override_store_t *store,
    const configuration_snapshot_t *durable,
    uint64_t now_monotonic_ms,
    configuration_snapshot_t *out_effective,
    uint64_t *out_effective_revision,
    uint32_t *out_active_mask);

/* Returns the earliest absolute monotonic expiry currently retained. Zero
 * means no entry. It does not discard records or advance effective_revision;
 * the single Configuration owner resolves expiry before consumer application. */
configuration_runtime_override_store_result_t
configuration_runtime_override_store_next_expiry(
    const configuration_runtime_override_store_t *store,
    uint64_t *out_expires_at_monotonic_ms);
