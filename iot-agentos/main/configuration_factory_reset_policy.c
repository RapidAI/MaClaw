#include "configuration_factory_reset_policy.h"

#include <stddef.h>
#include <string.h>

static uint32_t checksum(const configuration_factory_reset_journal_t *journal) {
    const uint8_t *bytes = (const uint8_t *)journal;
    uint32_t hash = 2166136261u;
    for (size_t i = 0; i < offsetof(configuration_factory_reset_journal_t, checksum); ++i) {
        hash ^= bytes[i];
        hash *= 16777619u;
    }
    return hash;
}

bool configuration_factory_reset_authorize(
    const configuration_factory_reset_request_t *request) {
    return request && request->struct_size == sizeof(*request) &&
           request->abi_version == CONFIGURATION_FACTORY_RESET_POLICY_ABI_VERSION &&
           (request->source == CONFIGURATION_SOURCE_USER_LOCAL ||
            request->source == CONFIGURATION_SOURCE_HUB_AUTHENTICATED) &&
           request->authenticated && request->explicit_confirmation &&
           request->generation != 0u &&
           request->classes == CONFIGURATION_FACTORY_RESET_CLASS_ALL;
}

bool configuration_factory_reset_journal_begin(
    configuration_factory_reset_journal_t *journal, uint32_t classes,
    uint64_t generation) {
    if (!journal || classes != CONFIGURATION_FACTORY_RESET_CLASS_ALL || generation == 0u) {
        return false;
    }
    *journal = (configuration_factory_reset_journal_t){
        .struct_size = sizeof(*journal),
        .abi_version = CONFIGURATION_FACTORY_RESET_POLICY_ABI_VERSION,
        .classes = classes,
        .generation = generation,
        .stage = CONFIGURATION_FACTORY_RESET_STAGE_PREPARED,
    };
    journal->checksum = checksum(journal);
    return true;
}

bool configuration_factory_reset_journal_validate(
    const configuration_factory_reset_journal_t *journal) {
    return journal && journal->struct_size == sizeof(*journal) &&
           journal->abi_version == CONFIGURATION_FACTORY_RESET_POLICY_ABI_VERSION &&
           journal->classes == CONFIGURATION_FACTORY_RESET_CLASS_ALL &&
           journal->generation != 0u &&
           (journal->stage == CONFIGURATION_FACTORY_RESET_STAGE_PREPARED ||
            journal->stage == CONFIGURATION_FACTORY_RESET_STAGE_COMMITTED) &&
           journal->checksum == checksum(journal);
}

bool configuration_factory_reset_journal_commit(
    configuration_factory_reset_journal_t *journal) {
    if (!configuration_factory_reset_journal_validate(journal) ||
        journal->stage != CONFIGURATION_FACTORY_RESET_STAGE_PREPARED) {
        return false;
    }
    journal->stage = CONFIGURATION_FACTORY_RESET_STAGE_COMMITTED;
    journal->checksum = checksum(journal);
    return true;
}


bool configuration_factory_reset_journal_encode(
    const configuration_factory_reset_journal_t *journal,
    uint8_t out_bytes[sizeof(configuration_factory_reset_journal_t)]) {
    if (!out_bytes || !configuration_factory_reset_journal_validate(journal)) return false;
    memcpy(out_bytes, journal, sizeof(*journal));
    return true;
}

bool configuration_factory_reset_journal_decode(
    const uint8_t *bytes, size_t size,
    configuration_factory_reset_journal_t *out_journal) {
    if (!bytes || !out_journal || size != sizeof(*out_journal)) return false;
    memcpy(out_journal, bytes, sizeof(*out_journal));
    return configuration_factory_reset_journal_validate(out_journal);
}
