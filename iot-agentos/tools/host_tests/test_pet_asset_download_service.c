#include <assert.h>
#include <stdio.h>
#include <string.h>

#include "services/pet_asset_download_service.h"

typedef struct {
    int request_calls;
    int releases;
    int waits;
    int pack_waits;
    int previews;
    bool admitted;
    bool cancel_on_pack_wait;
    bool lease_current;
    int fail_until;
    int http_status;
    uint8_t storage[PET_ASSET_SERVICE_MAX_FRAMES][3072];
} test_state_t;

static bool admitted(bool startup, void *context) {
    (void)startup;
    test_state_t *state = context;
    return state->admitted;
}

static bool lease_current(const gateway_capability_lease_t *lease, void *context) {
    test_state_t *state = context;
    return lease && state->lease_current;
}

static device_status_t request_frame(const char *url, uint32_t expected,
                                     bool startup,
                                     const gateway_capability_lease_t *lease,
                                     uint8_t **out_frame, uint32_t *out_length,
                                     int32_t *out_http_status, void *context) {
    (void)url;
    (void)startup;
    (void)lease;
    test_state_t *state = context;
    ++state->request_calls;
    *out_frame = NULL;
    *out_length = 0;
    *out_http_status = state->http_status;
    if (state->request_calls <= state->fail_until) return DEVICE_STATUS_IO_ERROR;
    const int slot = (state->request_calls - state->fail_until - 1) %
                     (int)PET_ASSET_SERVICE_MAX_FRAMES;
    memset(state->storage[slot], slot + 1, sizeof(state->storage[slot]));
    *out_frame = state->storage[slot];
    *out_length = expected;
    return DEVICE_STATUS_OK;
}

static device_status_t verify_frame(const uint8_t *frame, uint32_t bytes,
                                    const char expected[65], void *context) {
    (void)expected;
    (void)context;
    return frame && bytes == 3072 ? DEVICE_STATUS_OK : DEVICE_STATUS_INVALID_ARGUMENT;
}

static void release_frame(uint8_t *frame, void *context) {
    (void)frame;
    test_state_t *state = context;
    ++state->releases;
}

static bool wait_before_retry(uint32_t delay_ms, bool startup, void *context) {
    (void)delay_ms;
    (void)startup;
    test_state_t *state = context;
    ++state->waits;
    return true;
}

static bool wait_before_pack_retry(uint32_t delay_ms, void *context) {
    test_state_t *state = context;
    assert(delay_ms == 3000u * (uint32_t)(state->pack_waits + 1));
    ++state->pack_waits;
    if (state->cancel_on_pack_wait) state->admitted = false;
    return state->admitted;
}

static device_status_t preview(const pet_asset_descriptor_t *descriptor,
                               const uint8_t *frame,
                               const gateway_capability_lease_t *lease,
                               void *context) {
    (void)descriptor;
    (void)frame;
    (void)lease;
    test_state_t *state = context;
    ++state->previews;
    return DEVICE_STATUS_OK;
}

static pet_asset_descriptor_t descriptor(void) {
    pet_asset_descriptor_t value = {0};
    value.width = 32;
    value.height = 32;
    value.frame_count = 2;
    value.frame_ms = 100;
    strcpy(value.urls[0], "/asset/0");
    strcpy(value.urls[1], "/asset/1");
    return value;
}

static pet_asset_download_service_host_t host_for(test_state_t *state) {
    return (pet_asset_download_service_host_t){
        .struct_size = sizeof(pet_asset_download_service_host_t),
        .transaction_admitted = admitted,
        .gateway_lease_current = lease_current,
        .request_frame = request_frame,
        .verify_frame_sha256 = verify_frame,
        .release_frame = release_frame,
        .wait_before_retry = wait_before_retry,
        .wait_before_pack_retry = wait_before_pack_retry,
        .install_first_frame_preview = preview,
        .context = state,
    };
}

static gateway_capability_lease_t valid_lease(void) {
    return (gateway_capability_lease_t){
        .struct_size = sizeof(gateway_capability_lease_t),
        .abi_version = GATEWAY_CAPABILITY_LEASE_ABI_VERSION,
        .required_capabilities = GATEWAY_CAPABILITY_PET_ASSET,
        .generation = 1,
    };
}

int main(void) {
    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES] = {0};
    gateway_capability_lease_t lease = valid_lease();
    const pet_asset_descriptor_t asset = descriptor();

    test_state_t retry = {.admitted = true, .lease_current = true,
                          .fail_until = 3, .http_status = 200};
    pet_asset_download_service_host_t retry_host = host_for(&retry);
    assert(pet_asset_download_service_fetch_startup_pack(
               &retry_host, &asset, &lease, frames) == DEVICE_STATUS_OK);
    assert(retry.request_calls == 5);
    assert(retry.waits == 2);
    assert(retry.pack_waits == 1);
    assert(retry.previews == 1);
    assert(frames[0] && frames[1]);

    test_state_t permanent = {.admitted = true, .lease_current = true, .http_status = 404};
    pet_asset_download_service_host_t permanent_host = host_for(&permanent);
    memset(frames, 0, sizeof(frames));
    assert(pet_asset_download_service_fetch_startup_pack(&permanent_host, &asset,
                                                          &lease, frames) ==
           DEVICE_STATUS_INVALID_ARGUMENT);
    assert(permanent.request_calls == 1);
    assert(permanent.waits == 0);
    assert(permanent.pack_waits == 0);

    test_state_t withdrawn = {.admitted = true, .lease_current = false, .http_status = 200};
    pet_asset_download_service_host_t withdrawn_host = host_for(&withdrawn);
    assert(pet_asset_download_service_fetch(&withdrawn_host, &asset, false,
                                            &lease, frames) == DEVICE_STATUS_BUSY);
    assert(withdrawn.request_calls == 0);

    test_state_t cancelled = {.admitted = true, .cancel_on_pack_wait = true,
                              .lease_current = true, .fail_until = 3,
                              .http_status = 200};
    pet_asset_download_service_host_t cancelled_host = host_for(&cancelled);
    assert(pet_asset_download_service_fetch_startup_pack(&cancelled_host, &asset,
                                                          &lease, frames) ==
           DEVICE_STATUS_BUSY);
    assert(cancelled.request_calls == 3);
    assert(cancelled.pack_waits == 1);

    test_state_t exhausted = {.admitted = true, .lease_current = true,
                              .fail_until = 99, .http_status = 200};
    pet_asset_download_service_host_t exhausted_host = host_for(&exhausted);
    memset(frames, 0, sizeof(frames));
    assert(pet_asset_download_service_fetch_startup_pack(&exhausted_host, &asset,
                                                          &lease, frames) ==
           DEVICE_STATUS_IO_ERROR);
    assert(exhausted.request_calls == 9);
    assert(exhausted.waits == 6);
    assert(exhausted.pack_waits == 2);

    puts("PASS pet asset download transaction");
    return 0;
}
