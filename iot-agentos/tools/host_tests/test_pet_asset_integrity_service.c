#include <assert.h>
#include <stdint.h>
#include <string.h>

#include "services/pet_asset_integrity_service.h"

typedef struct {
    int calls;
    device_status_t status;
    uint8_t digest[32];
} test_state_t;

static device_status_t compute_digest(const uint8_t *frame, uint32_t bytes,
                                      uint8_t out[32], void *context) {
    test_state_t *state = context;
    ++state->calls;
    assert(frame && bytes == 4);
    if (state->status != DEVICE_STATUS_OK) return state->status;
    memcpy(out, state->digest, 32);
    return DEVICE_STATUS_OK;
}

int main(void) {
    static const uint8_t frame[4] = {1, 2, 3, 4};
    static const char digest_hex[] =
        "000102030405060708090a0b0c0d0e0f"
        "101112131415161718191a1b1c1d1e1f";
    test_state_t state = {0};
    for (int i = 0; i < 32; ++i) state.digest[i] = (uint8_t)i;
    pet_asset_integrity_service_host_t host = {
        .struct_size = sizeof(host), .compute_sha256 = compute_digest,
        .context = &state,
    };
    assert(pet_asset_integrity_service_verify_frame(
               &host, frame, sizeof(frame), digest_hex) == DEVICE_STATUS_OK);
    assert(state.calls == 1);
    state.digest[7] ^= 1;
    assert(pet_asset_integrity_service_verify_frame(
               &host, frame, sizeof(frame), digest_hex) == DEVICE_STATUS_INVALID_ARGUMENT);
    state.status = DEVICE_STATUS_INTERNAL_ERROR;
    assert(pet_asset_integrity_service_verify_frame(
               &host, frame, sizeof(frame), digest_hex) == DEVICE_STATUS_INTERNAL_ERROR);
    assert(pet_asset_integrity_service_verify_frame(
               &host, NULL, sizeof(frame), digest_hex) == DEVICE_STATUS_INVALID_ARGUMENT);
    return 0;
}
