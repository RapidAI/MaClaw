#include "services/cellular_recovery_policy.h"

uint32_t cellular_recovery_policy_next_retry_ms(uint32_t current_ms) {
    if (current_ms < CELLULAR_RECOVERY_RETRY_INITIAL_MS) {
        return CELLULAR_RECOVERY_RETRY_INITIAL_MS;
    }
    if (current_ms >= CELLULAR_RECOVERY_RETRY_MAX_MS / 2u) {
        return CELLULAR_RECOVERY_RETRY_MAX_MS;
    }
    return current_ms * 2u;
}

