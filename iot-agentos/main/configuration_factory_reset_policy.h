#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "configuration_policy.h"

#define CONFIGURATION_FACTORY_RESET_POLICY_ABI_VERSION 1u

/* The erase set is deliberately a closed value-only allow-list.  Callers
 * cannot supply an NVS namespace or key and therefore cannot turn this API
 * into an arbitrary storage eraser.  The personal-data classes cover both
 * NVS records and SPIFFS objects (meeting audio and pet assets). */
enum {
    CONFIGURATION_FACTORY_RESET_CLASS_CONFIGURATION = 1u << 0,
    CONFIGURATION_FACTORY_RESET_CLASS_ALARMS = 1u << 1,
    CONFIGURATION_FACTORY_RESET_CLASS_REPLAY = 1u << 2,
    CONFIGURATION_FACTORY_RESET_CLASS_GATEWAY_OUTBOX = 1u << 3,
    CONFIGURATION_FACTORY_RESET_CLASS_LOG_INDEX = 1u << 4,
    CONFIGURATION_FACTORY_RESET_CLASS_MEETING_RECORDING = 1u << 5,
    CONFIGURATION_FACTORY_RESET_CLASS_PET_CACHE = 1u << 6,
};

#define CONFIGURATION_FACTORY_RESET_CLASS_ALL \
    (CONFIGURATION_FACTORY_RESET_CLASS_CONFIGURATION | \
     CONFIGURATION_FACTORY_RESET_CLASS_ALARMS | \
     CONFIGURATION_FACTORY_RESET_CLASS_REPLAY | \
     CONFIGURATION_FACTORY_RESET_CLASS_GATEWAY_OUTBOX | \
     CONFIGURATION_FACTORY_RESET_CLASS_LOG_INDEX | \
     CONFIGURATION_FACTORY_RESET_CLASS_MEETING_RECORDING | \
     CONFIGURATION_FACTORY_RESET_CLASS_PET_CACHE)

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    configuration_source_t source;
    bool authenticated;
    bool explicit_confirmation;
    uint64_t generation;
    uint32_t classes;
} configuration_factory_reset_request_t;

typedef enum {
    CONFIGURATION_FACTORY_RESET_STAGE_PREPARED = 1,
    CONFIGURATION_FACTORY_RESET_STAGE_COMMITTED = 2,
} configuration_factory_reset_stage_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    uint32_t classes;
    uint64_t generation;
    configuration_factory_reset_stage_t stage;
    uint32_t checksum;
} configuration_factory_reset_journal_t;

bool configuration_factory_reset_authorize(
    const configuration_factory_reset_request_t *request);
bool configuration_factory_reset_journal_begin(
    configuration_factory_reset_journal_t *journal, uint32_t classes,
    uint64_t generation);
bool configuration_factory_reset_journal_validate(
    const configuration_factory_reset_journal_t *journal);
bool configuration_factory_reset_journal_commit(
    configuration_factory_reset_journal_t *journal);
bool configuration_factory_reset_journal_encode(
    const configuration_factory_reset_journal_t *journal,
    uint8_t out_bytes[sizeof(configuration_factory_reset_journal_t)]);
bool configuration_factory_reset_journal_decode(
    const uint8_t *bytes, size_t size,
    configuration_factory_reset_journal_t *out_journal);
