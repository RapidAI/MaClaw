#include "configuration_migration_journal.h"

#include <string.h>

static uint32_t journal_checksum(const configuration_migration_journal_t *journal) {
    const uint8_t *bytes = (const uint8_t *)journal;
    uint32_t hash = 2166136261u;
    for (size_t i = 0; i < offsetof(configuration_migration_journal_t, checksum); ++i) {
        hash ^= bytes[i];
        hash *= 16777619u;
    }
    return hash;
}

bool configuration_migration_journal_begin(configuration_migration_journal_t *journal,
                                           uint32_t source_bytes,
                                           uint32_t target_version,
                                           uint64_t generation) {
    if (!journal || source_bytes == 0u || target_version == 0u || generation == 0u)
        return false;
    *journal = (configuration_migration_journal_t){
        .struct_size = sizeof(*journal),
        .abi_version = CONFIGURATION_MIGRATION_JOURNAL_ABI_VERSION,
        .source_bytes = source_bytes,
        .target_version = target_version,
        .generation = generation,
        .stage = CONFIGURATION_MIGRATION_STAGE_PREPARED,
        .checksum = 0u,
    };
    journal->checksum = journal_checksum(journal);
    return true;
}

bool configuration_migration_journal_validate(
    const configuration_migration_journal_t *journal) {
    return journal && journal->struct_size == sizeof(*journal) &&
           journal->abi_version == CONFIGURATION_MIGRATION_JOURNAL_ABI_VERSION &&
           journal->source_bytes != 0u && journal->target_version != 0u &&
           journal->generation != 0u &&
           journal->stage >= CONFIGURATION_MIGRATION_STAGE_PREPARED &&
           journal->stage <= CONFIGURATION_MIGRATION_STAGE_COMMITTED &&
           journal->checksum == journal_checksum(journal);
}

bool configuration_migration_journal_transition(
    configuration_migration_journal_t *journal,
    configuration_migration_stage_t next_stage) {
    /* Stages form a durable transaction protocol. Require exactly one
     * forward edge so callers cannot manufacture COMMITTED evidence while
     * skipping the VALIDATED publication boundary. */
    if (!configuration_migration_journal_validate(journal) ||
        next_stage != (configuration_migration_stage_t)(journal->stage + 1) ||
        next_stage > CONFIGURATION_MIGRATION_STAGE_COMMITTED)
        return false;
    journal->stage = next_stage;
    journal->checksum = journal_checksum(journal);
    return true;
}

bool configuration_migration_journal_set_generation(
    configuration_migration_journal_t *journal, uint64_t generation) {
    if (!configuration_migration_journal_validate(journal) || generation == 0u ||
        /* The revision is the publication identity selected while PREPARED.
         * Once VALIDATED is durable, changing it would let a caller retarget
         * the marker at an unrelated V7 record before COMMITTED. */
        journal->stage != CONFIGURATION_MIGRATION_STAGE_PREPARED) {
        return false;
    }
    journal->generation = generation;
    journal->checksum = journal_checksum(journal);
    return true;
}

bool configuration_migration_journal_recovery_is_safe(
    const configuration_migration_journal_t *journal) {
    return configuration_migration_journal_validate(journal) &&
           journal->stage != CONFIGURATION_MIGRATION_STAGE_COMMITTED;
}

bool configuration_migration_journal_encode(
    const configuration_migration_journal_t *journal,
    uint8_t out_bytes[sizeof(configuration_migration_journal_t)]) {
    /* Encoding is a persistence admission boundary, not a repair helper.
     * Recomputing a checksum for an otherwise malformed record could turn a
     * corrupted stage/generation into apparently valid recovery evidence. */
    if (!out_bytes || !configuration_migration_journal_validate(journal)) return false;
    configuration_migration_journal_t copy = *journal;
    copy.checksum = journal_checksum(&copy);
    memcpy(out_bytes, &copy, sizeof(copy));
    return true;
}

bool configuration_migration_journal_decode(
    const uint8_t *bytes, size_t size,
    configuration_migration_journal_t *out_journal) {
    if (!bytes || !out_journal || size != sizeof(*out_journal)) return false;
    memcpy(out_journal, bytes, sizeof(*out_journal));
    return configuration_migration_journal_validate(out_journal);
}

bool configuration_migration_journal_recover(
    const configuration_migration_journal_t *journal,
    bool *discard_source) {
    if (!discard_source || !configuration_migration_journal_validate(journal)) return false;
    *discard_source = configuration_migration_journal_recovery_is_safe(journal);
    return true;
}
