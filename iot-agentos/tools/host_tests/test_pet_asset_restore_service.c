#include <assert.h>
#include <stdio.h>
#include <string.h>

#include "services/pet_asset_restore_service.h"

typedef struct {
    bool allowed;
    bool descriptor_present;
    int fail_frame;
    device_status_t install_status;
    int loads;
    int installs;
    int releases;
    int clears;
    int profiles;
    uint8_t storage[PET_ASSET_SERVICE_MAX_FRAMES][8];
} test_state_t;

static bool allowed(void *context) { return ((test_state_t *)context)->allowed; }
static bool read_descriptor(pet_asset_descriptor_t *out, void *context) {
    test_state_t *state = context;
    if (!state->descriptor_present) return false;
    *out = (pet_asset_descriptor_t){.frame_count = 2, .frame_ms = 100};
    return true;
}
static device_status_t load_frame(const pet_asset_descriptor_t *descriptor,
                                  uint32_t index, uint8_t **out, void *context) {
    (void)descriptor;
    test_state_t *state = context;
    ++state->loads;
    if ((int)index == state->fail_frame) return DEVICE_STATUS_INVALID_ARGUMENT;
    *out = state->storage[index];
    return DEVICE_STATUS_OK;
}
static device_status_t install_full(const pet_asset_descriptor_t *descriptor,
                                    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES],
                                    int *out_count, int *out_ms, void *context) {
    test_state_t *state = context;
    ++state->installs;
    assert(frames[0] && frames[1]);
    if (state->install_status != DEVICE_STATUS_OK) return state->install_status;
    *out_count = descriptor->frame_count;
    *out_ms = descriptor->frame_ms;
    return DEVICE_STATUS_OK;
}
static void release_frames(uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES], void *context) {
    ++((test_state_t *)context)->releases;
    memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_SERVICE_MAX_FRAMES);
}
static void clear_cache(void *context) { ++((test_state_t *)context)->clears; }
static void apply_profile(void *context) { ++((test_state_t *)context)->profiles; }

static pet_asset_restore_service_host_t host_for(test_state_t *state) {
    return (pet_asset_restore_service_host_t){
        .struct_size = sizeof(pet_asset_restore_service_host_t),
        .storage_restore_allowed = allowed,
        .read_descriptor = read_descriptor,
        .load_verified_frame = load_frame,
        .install_full = install_full,
        .release_frames = release_frames,
        .clear_cache = clear_cache,
        .apply_cached_profile = apply_profile,
        .context = state,
    };
}

int main(void) {
    test_state_t success = {.allowed = true, .descriptor_present = true,
                            .fail_frame = -1, .install_status = DEVICE_STATUS_OK};
    pet_asset_restore_service_host_t success_host = host_for(&success);
    assert(pet_asset_restore_service_restore(&success_host) == DEVICE_STATUS_OK);
    assert(success.loads == 2 && success.installs == 1 && success.releases == 1 &&
           success.clears == 0 && success.profiles == 1);

    test_state_t missing = {.allowed = true, .descriptor_present = false, .fail_frame = -1};
    pet_asset_restore_service_host_t missing_host = host_for(&missing);
    assert(pet_asset_restore_service_restore(&missing_host) == DEVICE_STATUS_NOT_FOUND);
    assert(missing.clears == 1 && missing.releases == 0 && missing.profiles == 0);

    test_state_t corrupt = {.allowed = true, .descriptor_present = true,
                            .fail_frame = 1, .install_status = DEVICE_STATUS_OK};
    pet_asset_restore_service_host_t corrupt_host = host_for(&corrupt);
    assert(pet_asset_restore_service_restore(&corrupt_host) == DEVICE_STATUS_INVALID_ARGUMENT);
    assert(corrupt.loads == 2 && corrupt.installs == 0 && corrupt.releases == 1 &&
           corrupt.clears == 1 && corrupt.profiles == 0);

    test_state_t closed = {.allowed = false, .descriptor_present = true, .fail_frame = -1};
    pet_asset_restore_service_host_t closed_host = host_for(&closed);
    assert(pet_asset_restore_service_restore(&closed_host) == DEVICE_STATUS_UNAVAILABLE);
    assert(closed.loads == 0 && closed.clears == 0 && closed.profiles == 0);

    puts("PASS pet asset cache restore transaction");
    return 0;
}
