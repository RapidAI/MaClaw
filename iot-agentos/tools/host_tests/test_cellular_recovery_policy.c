#include <assert.h>
#include <stdint.h>

#include "services/cellular_recovery_policy.h"

int main(void) {
    assert(cellular_recovery_policy_next_retry_ms(0) ==
           CELLULAR_RECOVERY_RETRY_INITIAL_MS);
    assert(cellular_recovery_policy_next_retry_ms(CELLULAR_RECOVERY_RETRY_INITIAL_MS) ==
           4000u);
    assert(cellular_recovery_policy_next_retry_ms(30000u) ==
           CELLULAR_RECOVERY_RETRY_MAX_MS);
    assert(cellular_recovery_policy_next_retry_ms(CELLULAR_RECOVERY_RETRY_MAX_MS) ==
           CELLULAR_RECOVERY_RETRY_MAX_MS);
    assert(cellular_recovery_policy_next_retry_ms(UINT32_MAX) ==
           CELLULAR_RECOVERY_RETRY_MAX_MS);
    return 0;
}
