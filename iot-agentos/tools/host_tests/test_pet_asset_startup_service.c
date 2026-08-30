#include <assert.h>
#include <stdio.h>
#include <string.h>

#include "services/pet_asset_startup_service.h"

typedef struct {
    startup_pet_asset_state_snapshot_t snapshot;
    bool stopped;
    bool installed;
    bool lease_available;
    bool lease_current;
    bool admitted;
    bool withdraw_before_clear;
    bool withdraw_after_download;
    bool withdraw_after_install;
    device_status_t install_status;
    int clears;
    int downloads;
    int installs;
    int caches;
    int releases;
    int finishes;
    uint8_t source[PET_ASSET_SERVICE_MAX_FRAMES][8];
    uint8_t cache_storage[PET_ASSET_SERVICE_MAX_FRAMES][8];
} test_state_t;

static bool snapshot(startup_pet_asset_state_snapshot_t *out, void *context) {
    *out = ((test_state_t *)context)->snapshot;
    return true;
}
static bool stopped(void *context) { return ((test_state_t *)context)->stopped; }
static device_status_t clear_applied(uint32_t generation, void *context) {
    test_state_t *state = context;
    if (state->withdraw_before_clear) state->admitted = false;
    if (!state->admitted || state->snapshot.generation != generation) {
        return DEVICE_STATUS_BUSY;
    }
    ++state->clears;
    return DEVICE_STATUS_OK;
}
static bool prepare(const pet_asset_descriptor_t *source,
                    pet_asset_descriptor_t *out, void *context) {
    (void)context;
    *out = *source;
    return source->frame_count > 0;
}
static bool revision_installed(const pet_asset_descriptor_t *descriptor, void *context) {
    (void)descriptor;
    return ((test_state_t *)context)->installed;
}
static bool capture(gateway_capability_lease_t *out, void *context) {
    test_state_t *state = context;
    if (!state->lease_available) return false;
    *out = (gateway_capability_lease_t){
        .struct_size = sizeof(*out),
        .abi_version = GATEWAY_CAPABILITY_LEASE_ABI_VERSION,
        .required_capabilities = GATEWAY_CAPABILITY_PET_ASSET,
        .generation = 1,
    };
    return true;
}
static bool lease_current(const gateway_capability_lease_t *lease, void *context) {
    return lease && ((test_state_t *)context)->lease_current;
}
static bool admitted(uint32_t generation, void *context) {
    test_state_t *state = context;
    return state->admitted && state->snapshot.generation == generation;
}
static device_status_t download(const pet_asset_descriptor_t *descriptor,
                                const gateway_capability_lease_t *lease,
                                uint32_t generation,
                                uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES],
                                void *context) {
    (void)lease;
    (void)generation;
    test_state_t *state = context;
    ++state->downloads;
    for (int i = 0; i < descriptor->frame_count; ++i) frames[i] = state->source[i];
    if (state->withdraw_after_download) state->admitted = false;
    return DEVICE_STATUS_OK;
}
static bool prepare_cache(void *context) { (void)context; return true; }
static device_status_t install(const pet_asset_descriptor_t *descriptor,
                               uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES],
                               bool prepare_cache_mirror,
                               uint8_t *cache_frames[PET_ASSET_SERVICE_MAX_FRAMES],
                               uint32_t generation, int *out_count, int *out_ms,
                               void *context) {
    (void)generation;
    test_state_t *state = context;
    ++state->installs;
    assert(frames[0] != NULL);
    if (state->install_status != DEVICE_STATUS_OK) return state->install_status;
    if (prepare_cache_mirror) {
        for (int i = 0; i < descriptor->frame_count; ++i) {
            cache_frames[i] = state->cache_storage[i];
        }
    }
    if (state->withdraw_after_install) state->admitted = false;
    *out_count = descriptor->frame_count;
    *out_ms = descriptor->frame_ms;
    return DEVICE_STATUS_OK;
}
static void cache(const pet_asset_descriptor_t *descriptor,
                  uint8_t *cache_frames[PET_ASSET_SERVICE_MAX_FRAMES],
                  const gateway_capability_lease_t *lease, void *context) {
    (void)descriptor;
    (void)lease;
    test_state_t *state = context;
    ++state->caches;
    /* The background cache worker owns accepted mirrors, so the coordinator's
     * terminal release is deliberately a no-op for this array. */
    memset(cache_frames, 0, sizeof(uint8_t *) * PET_ASSET_SERVICE_MAX_FRAMES);
}
static void release(uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES], void *context) {
    ++((test_state_t *)context)->releases;
    memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_SERVICE_MAX_FRAMES);
}
static void finish(uint32_t generation, void *context) {
    test_state_t *state = context;
    assert(generation == state->snapshot.generation);
    ++state->finishes;
}

static pet_asset_startup_service_host_t host_for(test_state_t *state) {
    return (pet_asset_startup_service_host_t){
        .struct_size = sizeof(pet_asset_startup_service_host_t),
        .snapshot = snapshot, .stop_requested = stopped, .clear_applied = clear_applied,
        .prepare_for_display = prepare, .revision_installed = revision_installed,
        .capture_gateway_lease = capture, .gateway_lease_current = lease_current,
        .generation_admitted = admitted, .download = download,
        .prepare_cache_mirror = prepare_cache, .install_full = install,
        .cache_in_background = cache, .release_frames = release,
        .finish_generation = finish, .context = state,
    };
}
static test_state_t state_for(bool present) {
    test_state_t state = {.lease_available = true, .lease_current = true,
                          .admitted = true, .install_status = DEVICE_STATUS_OK};
    state.snapshot.pending = true;
    state.snapshot.present = present;
    state.snapshot.generation = 7;
    state.snapshot.descriptor.frame_count = 2;
    state.snapshot.descriptor.frame_ms = 100;
    return state;
}

int main(void) {
    test_state_t absent = state_for(false);
    pet_asset_startup_service_host_t absent_host = host_for(&absent);
    assert(pet_asset_startup_service_apply(&absent_host) == DEVICE_STATUS_OK);
    assert(absent.clears == 1 && absent.finishes == 1 && absent.downloads == 0);

    test_state_t superseded_clear = state_for(false);
    superseded_clear.withdraw_before_clear = true;
    pet_asset_startup_service_host_t superseded_clear_host = host_for(&superseded_clear);
    assert(pet_asset_startup_service_apply(&superseded_clear_host) == DEVICE_STATUS_BUSY);
    assert(superseded_clear.clears == 0 && superseded_clear.finishes == 1);

    test_state_t cached = state_for(true);
    cached.installed = true;
    pet_asset_startup_service_host_t cached_host = host_for(&cached);
    assert(pet_asset_startup_service_apply(&cached_host) == DEVICE_STATUS_OK);
    assert(cached.finishes == 1 && cached.downloads == 0);

    test_state_t withdrawn = state_for(true);
    withdrawn.lease_available = false;
    pet_asset_startup_service_host_t withdrawn_host = host_for(&withdrawn);
    assert(pet_asset_startup_service_apply(&withdrawn_host) == DEVICE_STATUS_BUSY);
    assert(withdrawn.finishes == 0 && withdrawn.downloads == 0);

    test_state_t success = state_for(true);
    pet_asset_startup_service_host_t success_host = host_for(&success);
    assert(pet_asset_startup_service_apply(&success_host) == DEVICE_STATUS_OK);
    assert(success.downloads == 1 && success.installs == 1 && success.caches == 1 &&
           success.releases == 2 && success.finishes == 1);

    test_state_t stale = state_for(true);
    stale.withdraw_after_download = true;
    pet_asset_startup_service_host_t stale_host = host_for(&stale);
    assert(pet_asset_startup_service_apply(&stale_host) == DEVICE_STATUS_BUSY);
    assert(stale.downloads == 1 && stale.installs == 0 && stale.releases == 2 &&
           stale.finishes == 1);

    test_state_t failed = state_for(true);
    failed.install_status = DEVICE_STATUS_RESOURCE_EXHAUSTED;
    pet_asset_startup_service_host_t failed_host = host_for(&failed);
    assert(pet_asset_startup_service_apply(&failed_host) == DEVICE_STATUS_RESOURCE_EXHAUSTED);
    assert(failed.installs == 1 && failed.caches == 0 && failed.releases == 2 &&
           failed.finishes == 1);

    test_state_t withdrawn_after_install = state_for(true);
    withdrawn_after_install.withdraw_after_install = true;
    pet_asset_startup_service_host_t withdrawn_after_install_host =
        host_for(&withdrawn_after_install);
    assert(pet_asset_startup_service_apply(&withdrawn_after_install_host) ==
           DEVICE_STATUS_BUSY);
    assert(withdrawn_after_install.downloads == 1 &&
           withdrawn_after_install.installs == 1 &&
           withdrawn_after_install.caches == 0 &&
           withdrawn_after_install.releases == 2 &&
           withdrawn_after_install.finishes == 1);

    puts("PASS startup pet asset transaction");
    return 0;
}
