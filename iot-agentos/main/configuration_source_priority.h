#pragma once

/*
 * Per-key configuration source precedence.
 *
 * This is a value-only selector for a single already-decoded configuration
 * field.  It owns neither typed configuration values nor their storage: the
 * Configuration Service retains durable snapshots, while an ingress owner
 * supplies one candidate fact for every source it has admitted.  Keeping the
 * source decision here prevents a Hub handler, UI, profile or hardware
 * adapter from inventing a private precedence order.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "configuration_policy.h"

#define CONFIGURATION_SOURCE_CANDIDATE_ABI_VERSION 1u
#define CONFIGURATION_SOURCE_SELECTION_ABI_VERSION 1u

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    configuration_source_t source;
    /* False means this source has no value for the requested key.  Its
     * provenance is still ABI-checked so a malformed optional source cannot
     * quietly weaken a selection. */
    bool present;
    /* For MANUFACTURING_MANIFEST authenticated means its signature/identity
     * was verified by the ingress owner.  The selector never parses a
     * manifest, credential or Hub message itself. */
    configuration_policy_request_t provenance;
} configuration_source_candidate_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    configuration_source_t source;
    uint32_t priority;
    size_t candidate_index;
} configuration_source_selection_t;

typedef enum {
    CONFIGURATION_SOURCE_RESOLVE_OK = 0,
    CONFIGURATION_SOURCE_RESOLVE_INVALID_ARGUMENT,
    CONFIGURATION_SOURCE_RESOLVE_INVALID_CANDIDATE,
    CONFIGURATION_SOURCE_RESOLVE_NO_CANDIDATE,
} configuration_source_resolve_result_t;

/* Returns a stable comparison rank for a source. Zero means an invalid
 * source. Higher rank wins. Rank is intentionally not a wire identifier. */
uint32_t configuration_source_priority(configuration_source_t source);

/* Selects exactly one source for `key`. Every supplied candidate must have a
 * current ABI, matching provenance and be permitted for the key; duplicate
 * present candidates for the same source are rejected rather than receiving
 * an accidental array-order tiebreak. The selected field's actual typed value
 * remains with its Configuration Service owner. */
configuration_source_resolve_result_t configuration_source_priority_resolve(
    configuration_key_t key,
    const configuration_source_candidate_t *candidates,
    size_t candidate_count,
    configuration_source_selection_t *out_selection);
