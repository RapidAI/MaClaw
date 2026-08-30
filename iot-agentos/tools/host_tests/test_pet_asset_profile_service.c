#include <assert.h>
#include <stdio.h>
#include <string.h>

#include "services/pet_asset_profile_service.h"

typedef struct {
    bool startup_matches;
    bool startup_pending;
    device_status_t apply_status;
    device_status_t clear_status;
    bool permanent_status;
    uint32_t retry_count;
    bool retries_exhausted;
    int pending_sets;
    int applies;
    int clears;
    int resets;
} test_state_t;

static bool matches(const char *revision, const char *skin, void *context) {
    (void)revision; (void)skin;
    return ((test_state_t *)context)->startup_matches;
}
static bool pending(void *context) { return ((test_state_t *)context)->startup_pending; }
static void set_pending(bool value, void *context) {
    test_state_t *state = context;
    state->startup_pending = value;
    ++state->pending_sets;
}
static device_status_t apply(const pet_asset_descriptor_t *descriptor, void *context) {
    (void)descriptor;
    test_state_t *state = context;
    ++state->applies;
    return state->apply_status;
}
static device_status_t clear(void *context) {
    test_state_t *state = context;
    ++state->clears;
    return state->clear_status;
}
static bool permanent(device_status_t status, void *context) {
    (void)status;
    return ((test_state_t *)context)->permanent_status;
}
static uint32_t note(const char *id, void *context) {
    (void)id;
    return ++((test_state_t *)context)->retry_count;
}
static bool exhausted(uint32_t limit, void *context) {
    assert(limit == 3);
    return ((test_state_t *)context)->retries_exhausted;
}
static void reset(void *context) { ++((test_state_t *)context)->resets; }

static pet_asset_profile_service_host_t host_for(test_state_t *state) {
    return (pet_asset_profile_service_host_t){
        .struct_size = sizeof(pet_asset_profile_service_host_t),
        .startup_profile_matches = matches, .startup_pending = pending,
        .set_startup_pending = set_pending, .apply_asset = apply,
        .clear_asset = clear, .status_permanently_invalid = permanent,
        .note_transient_failure = note, .retry_exhausted = exhausted,
        .reset_retries = reset, .context = state,
    };
}
static pet_asset_descriptor_t descriptor(void) {
    pet_asset_descriptor_t asset = {0};
    strcpy(asset.revision, "rev-2");
    return asset;
}

int main(void) {
    const pet_asset_descriptor_t asset = descriptor();

    test_state_t matching = {.startup_matches = true, .apply_status = DEVICE_STATUS_OK};
    pet_asset_profile_service_host_t matching_host = host_for(&matching);
    pet_asset_profile_service_result_t result = pet_asset_profile_service_apply(
        &matching_host, &asset, "cat", "m-1", 3);
    assert(result.handled && result.deferred_to_startup && !result.superseded_startup);
    assert(matching.applies == 0 && matching.resets == 0);

    test_state_t newer = {.startup_pending = true, .apply_status = DEVICE_STATUS_OK};
    pet_asset_profile_service_host_t newer_host = host_for(&newer);
    result = pet_asset_profile_service_apply(&newer_host, &asset, "dog", "m-2", 3);
    assert(result.handled && result.superseded_startup && newer.pending_sets == 1 &&
           !newer.startup_pending && newer.applies == 1 && newer.resets == 1);

    test_state_t transient = {.apply_status = DEVICE_STATUS_TIMEOUT};
    pet_asset_profile_service_host_t transient_host = host_for(&transient);
    result = pet_asset_profile_service_apply(&transient_host, &asset, NULL, "m-3", 3);
    assert(!result.handled && !result.permanently_invalid && result.retry_count == 1 &&
           transient.applies == 1);

    test_state_t exhausted_retry = {.apply_status = DEVICE_STATUS_RESOURCE_EXHAUSTED,
                                    .retries_exhausted = true};
    pet_asset_profile_service_host_t exhausted_host = host_for(&exhausted_retry);
    result = pet_asset_profile_service_apply(&exhausted_host, &asset, NULL, "m-4", 3);
    assert(!result.handled && result.permanently_invalid && result.retry_count == 1);

    test_state_t fallback = {.clear_status = DEVICE_STATUS_OK};
    pet_asset_profile_service_host_t fallback_host = host_for(&fallback);
    result = pet_asset_profile_service_apply(&fallback_host, NULL, NULL, "m-5", 3);
    assert(result.handled && fallback.clears == 1 && fallback.resets == 1);

    test_state_t malformed = {.apply_status = DEVICE_STATUS_INVALID_ARGUMENT,
                              .permanent_status = true};
    pet_asset_profile_service_host_t malformed_host = host_for(&malformed);
    result = pet_asset_profile_service_apply(&malformed_host, &asset, NULL, "m-6", 3);
    assert(!result.handled && result.permanently_invalid && result.retry_count == 1);

    puts("PASS runtime pet profile transaction");
    return 0;
}
