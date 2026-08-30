#include <stdio.h>
#include "trusted_time_policy.h"

#define CHECK(x) do { if (!(x)) { fprintf(stderr, "failed: %s\n", #x); return 1; } } while (0)

int main(void) {
    trusted_time_sample_t sample = {
        .struct_size = sizeof(sample),
        .abi_version = TRUSTED_TIME_SAMPLE_ABI_VERSION,
    };
    CHECK(trusted_time_policy_from_millis(1672531200000.0,
          TRUSTED_TIME_SOURCE_HUB_AUTHENTICATED, &sample));
    CHECK(sample.epoch_sec == 1672531200 && sample.usec == 0);
    CHECK(trusted_time_policy_from_millis(1700000000123.0,
          TRUSTED_TIME_SOURCE_SNTP, &sample));
    CHECK(sample.epoch_sec == 1700000000 && sample.usec == 123000);
    CHECK(!trusted_time_policy_from_millis(1700000000.5,
          TRUSTED_TIME_SOURCE_HUB_AUTHENTICATED, &sample));
    CHECK(!trusted_time_policy_from_millis(1672531199999.0,
          TRUSTED_TIME_SOURCE_HUB_AUTHENTICATED, &sample));
    CHECK(!trusted_time_policy_from_millis(4102444800000.0,
          TRUSTED_TIME_SOURCE_HUB_AUTHENTICATED, &sample));
    CHECK(!trusted_time_policy_from_millis(1700000000000.0,
          TRUSTED_TIME_SOURCE_INVALID, &sample));
    trusted_time_state_t state = {
        .struct_size = sizeof(state),
        .abi_version = TRUSTED_TIME_SAMPLE_ABI_VERSION,
    };
    sample.source = TRUSTED_TIME_SOURCE_INVALID;
    CHECK(trusted_time_policy_state_observe(&state, &sample, 1000) == false);
    sample.abi_version = TRUSTED_TIME_SAMPLE_ABI_VERSION;
    sample.source = TRUSTED_TIME_SOURCE_HUB_AUTHENTICATED;
    sample.epoch_sec = 1700000000;
    sample.usec = 0;
    CHECK(trusted_time_policy_state_observe(&state, &sample, 1000));
    CHECK(trusted_time_policy_state_is_trusted(&state));
    sample.epoch_sec -= 301;
    CHECK(!trusted_time_policy_state_observe(&state, &sample, 2000));
    CHECK(state.state == TRUSTED_TIME_STATE_ANOMALY);
    CHECK(!trusted_time_policy_state_is_trusted(&state));
    sample.abi_version++;
    CHECK(!trusted_time_policy_from_millis(1700000000000.0,
          TRUSTED_TIME_SOURCE_HUB_AUTHENTICATED, &sample));
    puts("PASS trusted time policy bounds and source admission");
    return 0;
}
