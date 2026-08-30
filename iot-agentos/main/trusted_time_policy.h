#pragma once

#include <stdbool.h>
#include <stdint.h>

typedef enum {
    TRUSTED_TIME_SOURCE_INVALID = 0,
    TRUSTED_TIME_SOURCE_SNTP,
    TRUSTED_TIME_SOURCE_HUB_AUTHENTICATED,
} trusted_time_source_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    trusted_time_source_t source;
    int64_t epoch_sec;
    int32_t usec;
} trusted_time_sample_t;

#define TRUSTED_TIME_SAMPLE_ABI_VERSION 1u

bool trusted_time_policy_from_millis(double epoch_ms,
                                     trusted_time_source_t source,
                                     trusted_time_sample_t *out);

typedef enum {
    TRUSTED_TIME_STATE_UNTRUSTED = 0,
    TRUSTED_TIME_STATE_AUTHENTICATED,
    TRUSTED_TIME_STATE_SNTP_CONFIRMED,
    TRUSTED_TIME_STATE_ANOMALY,
} trusted_time_state_kind_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    trusted_time_state_kind_t state;
    trusted_time_source_t last_source;
    int64_t last_epoch_sec;
    uint64_t last_monotonic_ms;
    bool observed;
} trusted_time_state_t;

/* Initialize/reset the state machine. No clock or network side effect occurs. */
void trusted_time_policy_state_init(trusted_time_state_t *state);

/* Admit a validated sample and advance state. `monotonic_ms` is the caller's
 * monotonic observation at receipt time; a large rollback or implausible drift
 * permanently marks this state as ANOMALY until explicitly reset. */
bool trusted_time_policy_state_observe(trusted_time_state_t *state,
                                       const trusted_time_sample_t *sample,
                                       uint64_t monotonic_ms);

bool trusted_time_policy_state_is_trusted(const trusted_time_state_t *state);
