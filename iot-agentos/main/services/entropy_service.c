#include "services/entropy_service.h"
#include <stdatomic.h>
#include "esp_random.h"
#include "mbedtls/platform_util.h"

enum {
    ENTROPY_STATE_COLD = 0u,
    ENTROPY_STATE_PROBING = 1u,
    ENTROPY_STATE_READY = 2u,
};

static atomic_uint_fast8_t s_state = ATOMIC_VAR_INIT(ENTROPY_STATE_COLD);

device_status_t entropy_service_init(void) {
    uint_fast8_t expected = ENTROPY_STATE_COLD;
    if (!atomic_compare_exchange_strong_explicit(&s_state, &expected,
                                                  ENTROPY_STATE_PROBING,
                                                  memory_order_acq_rel,
                                                  memory_order_acquire)) {
        if (expected == ENTROPY_STATE_READY) return DEVICE_STATUS_OK;
        /* A concurrent initializer is still probing. Do not expose a false
         * ready result; the caller must retry after that bounded probe. */
        return DEVICE_STATUS_BUSY;
    }
    /* A boot-time readiness barrier, not a deterministic PRNG seed marker:
     * sample two independent words and reject an all-zero or duplicated
     * sample.  This catches an unavailable/stuck RNG before credentials or
     * pairing material can be generated. */
    uint8_t probe[32] = {0};
    esp_fill_random(probe, sizeof(probe));
    bool any = false;
    bool equal_halves = true;
    for (size_t i = 0; i < 16u; ++i) {
        any = any || probe[i] != 0u || probe[i + 16u] != 0u;
        equal_halves = equal_halves && probe[i] == probe[i + 16u];
    }
    mbedtls_platform_zeroize(probe, sizeof(probe));
    if (!any || equal_halves) {
        atomic_store_explicit(&s_state, ENTROPY_STATE_COLD, memory_order_release);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    atomic_store_explicit(&s_state, ENTROPY_STATE_READY, memory_order_release);
    return DEVICE_STATUS_OK;
}

bool entropy_service_ready(void) {
    return atomic_load_explicit(&s_state, memory_order_acquire) == ENTROPY_STATE_READY;
}

bool entropy_service_fill(void *buffer, size_t length) {
    if (!buffer || length == 0u || !entropy_service_ready()) return false;
    esp_fill_random(buffer, length);
    return true;
}
