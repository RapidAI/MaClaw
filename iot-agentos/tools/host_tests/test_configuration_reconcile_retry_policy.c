#include <stdio.h>

#include "configuration_reconcile_retry_policy.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "check failed at %d: %s\n", __LINE__, #condition); \
        return 1; \
    } \
} while (0)

int main(void) {
    uint32_t delay = 0u;
    CHECK(!configuration_reconcile_retry_delay_ms(0u, &delay));
    CHECK(!configuration_reconcile_retry_delay_ms(1u, NULL));
    CHECK(configuration_reconcile_retry_delay_ms(1u, &delay) && delay == 1000u);
    CHECK(configuration_reconcile_retry_delay_ms(2u, &delay) && delay == 2000u);
    CHECK(configuration_reconcile_retry_delay_ms(6u, &delay) && delay == 32000u);
    CHECK(configuration_reconcile_retry_delay_ms(7u, &delay) && delay == 60000u);
    CHECK(configuration_reconcile_retry_delay_ms(UINT32_MAX, &delay) && delay == 60000u);
    return 0;
}
