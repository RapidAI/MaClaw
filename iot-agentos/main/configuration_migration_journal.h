#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "device_api.h"

#define CONFIGURATION_MIGRATION_JOURNAL_ABI_VERSION 1u
/* Explicit source identity used when legacy configuration exists only as
 * scattered scalar keys and therefore has no single blob length. */
#define CONFIGURATION_MIGRATION_LEGACY_SCALAR_SOURCE_BYTES UINT32_MAX

typedef enum {
    CONFIGURATION_MIGRATION_STAGE_NONE = 0,
    CONFIGURATION_MIGRATION_STAGE_PREPARED = 1,
    CONFIGURATION_MIGRATION_STAGE_VALIDATED = 2,
    CONFIGURATION_MIGRATION_STAGE_COMMITTED = 3,
} configuration_migration_stage_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    uint32_t source_bytes;
    uint32_t target_version;
    uint64_t generation;
    configuration_migration_stage_t stage;
    uint32_t checksum;
} configuration_migration_journal_t;

bool configuration_migration_journal_begin(configuration_migration_journal_t *journal,
                                           uint32_t source_bytes,
                                           uint32_t target_version,
                                           uint64_t generation);
bool configuration_migration_journal_validate(
    const configuration_migration_journal_t *journal);
bool configuration_migration_journal_transition(
    configuration_migration_journal_t *journal,
    configuration_migration_stage_t next_stage);

/* Bind the journal to the durable publication identity produced by the
 * migration.  This is deliberately a checked value update so callers cannot
 * change generation without refreshing the integrity checksum. */
bool configuration_migration_journal_set_generation(
    configuration_migration_journal_t *journal, uint64_t generation);


/* Recovery policy for a journal found after reset. Only PREPARED or VALIDATED
 * may be discarded; COMMITTED is evidence that the target publication was
 * durable and must never be rolled back to a legacy interpretation. */
bool configuration_migration_journal_recovery_is_safe(
    const configuration_migration_journal_t *journal);

/* A compact, fixed-size encoding suitable for one Persistence blob. */
bool configuration_migration_journal_encode(
    const configuration_migration_journal_t *journal,
    uint8_t out_bytes[sizeof(configuration_migration_journal_t)]);
bool configuration_migration_journal_decode(
    const uint8_t *bytes, size_t size,
    configuration_migration_journal_t *out_journal);

/* Decide what to do with a persisted journal before touching the target
 * record.  `discard_source` is set only for a valid, non-committed journal;
 * unknown/corrupt state must remain fail-closed. */
bool configuration_migration_journal_recover(
    const configuration_migration_journal_t *journal,
    bool *discard_source);
