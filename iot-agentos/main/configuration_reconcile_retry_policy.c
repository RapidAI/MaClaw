#include "configuration_reconcile_retry_policy.h"

bool configuration_reconcile_retry_delay_ms(uint32_t retry_attempt,
                                            uint32_t *out_delay_ms) {
    if (retry_attempt == 0u || !out_delay_ms) return false;
    /* 1s << 5 is the last pre-cap value. Avoid a shift/loop whose work or
     * overflow depends on an untrusted retained attempt count. */
    if (retry_attempt >= 7u) {
        *out_delay_ms = CONFIGURATION_RECONCILE_RETRY_MAX_DELAY_MS;
        return true;
    }
    *out_delay_ms = CONFIGURATION_RECONCILE_RETRY_INITIAL_DELAY_MS
                    << (retry_attempt - 1u);
    return true;
}
