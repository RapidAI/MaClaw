#include "trusted_time_policy.h"

#include <math.h>

#define TRUSTED_TIME_MIN_MS 1672531200000.0
#define TRUSTED_TIME_MAX_MS 4102444800000.0
#define TRUSTED_TIME_MAX_ROLLBACK_SEC (5 * 60)
#define TRUSTED_TIME_MAX_DRIFT_SEC (6 * 60 * 60)

bool trusted_time_policy_from_millis(double epoch_ms,
                                     trusted_time_source_t source,
                                     trusted_time_sample_t *out) {
    if (!out || out->struct_size != sizeof(*out) ||
        out->abi_version != TRUSTED_TIME_SAMPLE_ABI_VERSION ||
        (source != TRUSTED_TIME_SOURCE_SNTP &&
         source != TRUSTED_TIME_SOURCE_HUB_AUTHENTICATED) ||
        !isfinite(epoch_ms) || epoch_ms < TRUSTED_TIME_MIN_MS ||
        epoch_ms >= TRUSTED_TIME_MAX_MS || floor(epoch_ms) != epoch_ms) {
        return false;
    }
    const int64_t millis = (int64_t)epoch_ms;
    *out = (trusted_time_sample_t){
        .struct_size = sizeof(*out),
        .abi_version = TRUSTED_TIME_SAMPLE_ABI_VERSION,
        .source = source,
        .epoch_sec = millis / 1000,
        .usec = (int32_t)((millis % 1000) * 1000),
    };
    return true;
}

void trusted_time_policy_state_init(trusted_time_state_t *state) {
    if (!state) return;
    *state = (trusted_time_state_t){
        .struct_size = sizeof(*state),
        .abi_version = TRUSTED_TIME_SAMPLE_ABI_VERSION,
        .state = TRUSTED_TIME_STATE_UNTRUSTED,
    };
}

bool trusted_time_policy_state_observe(trusted_time_state_t *state,
                                       const trusted_time_sample_t *sample,
                                       uint64_t monotonic_ms) {
    if (!state || !sample || state->struct_size != sizeof(*state) ||
        state->abi_version != TRUSTED_TIME_SAMPLE_ABI_VERSION ||
        sample->struct_size != sizeof(*sample) ||
        sample->abi_version != TRUSTED_TIME_SAMPLE_ABI_VERSION ||
        (sample->source != TRUSTED_TIME_SOURCE_SNTP &&
         sample->source != TRUSTED_TIME_SOURCE_HUB_AUTHENTICATED) ||
        sample->usec < 0 || sample->usec >= 1000000 ||
        state->state == TRUSTED_TIME_STATE_ANOMALY) {
        return false;
    }

    if (state->observed) {
        const int64_t backwards = state->last_epoch_sec - sample->epoch_sec;
        if (backwards > TRUSTED_TIME_MAX_ROLLBACK_SEC) {
            state->state = TRUSTED_TIME_STATE_ANOMALY;
            return false;
        }
        if (monotonic_ms >= state->last_monotonic_ms) {
            const uint64_t elapsed_ms = monotonic_ms - state->last_monotonic_ms;
            const int64_t expected = state->last_epoch_sec +
                (int64_t)(elapsed_ms / 1000u);
            int64_t drift = sample->epoch_sec - expected;
            if (drift < 0) drift = -drift;
            if (drift > TRUSTED_TIME_MAX_DRIFT_SEC) {
                state->state = TRUSTED_TIME_STATE_ANOMALY;
                return false;
            }
        }
    }
    state->last_source = sample->source;
    state->last_epoch_sec = sample->epoch_sec;
    state->last_monotonic_ms = monotonic_ms;
    state->observed = true;
    state->state = sample->source == TRUSTED_TIME_SOURCE_SNTP
        ? TRUSTED_TIME_STATE_SNTP_CONFIRMED
        : TRUSTED_TIME_STATE_AUTHENTICATED;
    return true;
}

bool trusted_time_policy_state_is_trusted(const trusted_time_state_t *state) {
    return state && state->struct_size == sizeof(*state) &&
           state->abi_version == TRUSTED_TIME_SAMPLE_ABI_VERSION &&
           (state->state == TRUSTED_TIME_STATE_AUTHENTICATED ||
            state->state == TRUSTED_TIME_STATE_SNTP_CONFIRMED) &&
           state->observed;
}
