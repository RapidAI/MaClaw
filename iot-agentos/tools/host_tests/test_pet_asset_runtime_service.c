#include <assert.h>
#include <stdio.h>
#include <string.h>

#include "services/pet_asset_runtime_service.h"

typedef struct {
    bool installed;
    bool lease_available;
    bool lease_current;
    bool capacity_available;
    bool stale_cache_dropped;
    bool revoke_after_begin;
    bool revoke_after_drop;
    bool revoke_after_install;
    bool admitted;
    bool revoke_admission_after_begin;
    int begins;
    int finishes;
    int downloads;
    int installs;
    int cache_submits;
    int releases;
    uint8_t source[PET_ASSET_SERVICE_MAX_FRAMES][16];
    uint8_t cache[PET_ASSET_SERVICE_MAX_FRAMES][16];
} test_state_t;

static bool revision_installed(const pet_asset_descriptor_t *descriptor, void *context) {
    (void)descriptor;
    return ((test_state_t *)context)->installed;
}

static bool capture_lease(gateway_capability_lease_t *out_lease, void *context) {
    test_state_t *state = context;
    if (!state->lease_available) return false;
    *out_lease = (gateway_capability_lease_t){
        .struct_size = sizeof(*out_lease),
        .abi_version = GATEWAY_CAPABILITY_LEASE_ABI_VERSION,
        .required_capabilities = GATEWAY_CAPABILITY_PET_ASSET,
        .generation = 1,
    };
    return true;
}

static bool lease_current(const gateway_capability_lease_t *lease, void *context) {
    return lease && ((test_state_t *)context)->lease_current;
}

static bool transaction_admitted(void *context) {
    return ((test_state_t *)context)->admitted;
}

static void begin_work(void *context) {
    test_state_t *state = context;
    ++state->begins;
    if (state->revoke_after_begin) state->lease_current = false;
    if (state->revoke_admission_after_begin) state->admitted = false;
}
static void finish_work(void *context) { ++((test_state_t *)context)->finishes; }
static bool capacity_available(const pet_asset_descriptor_t *descriptor, void *context) {
    (void)descriptor;
    return ((test_state_t *)context)->capacity_available;
}
static bool drop_stale_cache(const pet_asset_descriptor_t *descriptor, void *context) {
    (void)descriptor;
    test_state_t *state = context;
    state->stale_cache_dropped = true;
    state->capacity_available = true;
    if (state->revoke_after_drop) state->lease_current = false;
    return true;
}
static device_status_t download(const pet_asset_descriptor_t *descriptor,
                                const gateway_capability_lease_t *lease,
                                uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES],
                                void *context) {
    (void)lease;
    test_state_t *state = context;
    ++state->downloads;
    for (int i = 0; i < descriptor->frame_count; ++i) frames[i] = state->source[i];
    return DEVICE_STATUS_OK;
}
static bool prepare_cache(void *context) { return ((test_state_t *)context)->capacity_available; }
static device_status_t install_full(const pet_asset_descriptor_t *descriptor,
                                    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES],
                                    bool prepare_cache_mirror,
                                    uint8_t *cache_frames[PET_ASSET_SERVICE_MAX_FRAMES],
                                    int *out_count, int *out_frame_ms, void *context) {
    test_state_t *state = context;
    ++state->installs;
    assert(frames[0] != NULL);
    if (prepare_cache_mirror) {
        for (int i = 0; i < descriptor->frame_count; ++i) cache_frames[i] = state->cache[i];
    }
    *out_count = descriptor->frame_count;
    *out_frame_ms = descriptor->frame_ms;
    if (state->revoke_after_install) state->lease_current = false;
    return DEVICE_STATUS_OK;
}
static void cache_in_background(const pet_asset_descriptor_t *descriptor,
                                uint8_t *cache_frames[PET_ASSET_SERVICE_MAX_FRAMES],
                                const gateway_capability_lease_t *lease, void *context) {
    (void)descriptor;
    (void)lease;
    test_state_t *state = context;
    ++state->cache_submits;
    memset(cache_frames, 0, sizeof(uint8_t *) * PET_ASSET_SERVICE_MAX_FRAMES);
}
static void release_frames(uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES], void *context) {
    test_state_t *state = context;
    ++state->releases;
    memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_SERVICE_MAX_FRAMES);
}

static pet_asset_runtime_service_host_t host_for(test_state_t *state) {
    return (pet_asset_runtime_service_host_t){
        .struct_size = sizeof(pet_asset_runtime_service_host_t),
        .revision_installed = revision_installed,
        .capture_gateway_lease = capture_lease,
        .gateway_lease_current = lease_current,
        .transaction_admitted = transaction_admitted,
        .begin_optional_media_work = begin_work,
        .finish_optional_media_work = finish_work,
        .capacity_available = capacity_available,
        .drop_stale_cache = drop_stale_cache,
        .download = download,
        .prepare_cache_mirror = prepare_cache,
        .install_full = install_full,
        .cache_in_background = cache_in_background,
        .release_frames = release_frames,
        .context = state,
    };
}

static pet_asset_descriptor_t descriptor(void) {
    return (pet_asset_descriptor_t){.frame_count = 2, .frame_ms = 100};
}

int main(void) {
    const pet_asset_descriptor_t asset = descriptor();

    test_state_t installed = {.installed = true, .lease_available = true,
                              .lease_current = true, .admitted = true,
                              .capacity_available = true};
    pet_asset_runtime_service_host_t installed_host = host_for(&installed);
    assert(pet_asset_runtime_service_apply(&installed_host, &asset) == DEVICE_STATUS_OK);
    assert(installed.begins == 0 && installed.downloads == 0 && installed.releases == 0);

    test_state_t normal = {.lease_available = true, .lease_current = true,
                           .admitted = true,
                           .capacity_available = true};
    pet_asset_runtime_service_host_t normal_host = host_for(&normal);
    assert(pet_asset_runtime_service_apply(&normal_host, &asset) == DEVICE_STATUS_OK);
    assert(normal.begins == 1 && normal.finishes == 1 && normal.downloads == 1);
    assert(normal.installs == 1 && normal.cache_submits == 1 && normal.releases == 2);

    test_state_t pressure = {.lease_available = true, .lease_current = true,
                             .admitted = true,
                             .capacity_available = false};
    pet_asset_runtime_service_host_t pressure_host = host_for(&pressure);
    assert(pet_asset_runtime_service_apply(&pressure_host, &asset) == DEVICE_STATUS_OK);
    assert(pressure.stale_cache_dropped && pressure.downloads == 1);

    test_state_t withdrawn = {.lease_available = true, .lease_current = false,
                              .admitted = true,
                              .capacity_available = true};
    pet_asset_runtime_service_host_t withdrawn_host = host_for(&withdrawn);
    assert(pet_asset_runtime_service_apply(&withdrawn_host, &asset) == DEVICE_STATUS_BUSY);
    assert(withdrawn.downloads == 0 && withdrawn.installs == 0 &&
           withdrawn.cache_submits == 0 && withdrawn.releases == 2);

    /* Optional-media admission itself can race a Connectivity generation
     * revocation.  The runtime coordinator must fence before reclaiming
     * cache/download work and always release the media lane. */
    test_state_t revoked_on_begin = {.lease_available = true, .lease_current = true,
                                     .admitted = true, .capacity_available = true,
                                     .revoke_after_begin = true};
    pet_asset_runtime_service_host_t revoked_on_begin_host = host_for(&revoked_on_begin);
    assert(pet_asset_runtime_service_apply(&revoked_on_begin_host, &asset) ==
           DEVICE_STATUS_BUSY);
    assert(revoked_on_begin.begins == 1 && revoked_on_begin.downloads == 0 &&
           revoked_on_begin.finishes == 1 && revoked_on_begin.releases == 2);

    test_state_t revoked_after_drop = {.lease_available = true, .lease_current = true,
                                       .admitted = true, .capacity_available = false,
                                       .revoke_after_drop = true};
    pet_asset_runtime_service_host_t revoked_after_drop_host = host_for(&revoked_after_drop);
    assert(pet_asset_runtime_service_apply(&revoked_after_drop_host, &asset) ==
           DEVICE_STATUS_BUSY);
    assert(revoked_after_drop.stale_cache_dropped && revoked_after_drop.downloads == 0 &&
           revoked_after_drop.finishes == 1 && revoked_after_drop.releases == 2);

    /* Renderer install is another late lease boundary: even a successful
     * install must not hand its mirror to Storage after Connectivity has
     * retired the generation while buffers were being consumed. */
    test_state_t revoked_after_install = {.lease_available = true, .lease_current = true,
                                          .admitted = true, .capacity_available = true,
                                          .revoke_after_install = true};
    pet_asset_runtime_service_host_t revoked_after_install_host =
        host_for(&revoked_after_install);
    assert(pet_asset_runtime_service_apply(&revoked_after_install_host, &asset) ==
           DEVICE_STATUS_BUSY);
    assert(revoked_after_install.downloads == 1 &&
           revoked_after_install.installs == 1 &&
           revoked_after_install.cache_submits == 0 &&
           revoked_after_install.finishes == 1 &&
           revoked_after_install.releases == 2);

    test_state_t admission_closed = {.lease_available = true, .lease_current = true,
                                     .admitted = true, .capacity_available = true,
                                     .revoke_admission_after_begin = true};
    pet_asset_runtime_service_host_t admission_closed_host = host_for(&admission_closed);
    assert(pet_asset_runtime_service_apply(&admission_closed_host, &asset) ==
           DEVICE_STATUS_BUSY);
    assert(admission_closed.downloads == 0 && admission_closed.installs == 0 &&
           admission_closed.finishes == 1 && admission_closed.releases == 2);

    puts("PASS runtime pet asset transaction");
    return 0;
}
