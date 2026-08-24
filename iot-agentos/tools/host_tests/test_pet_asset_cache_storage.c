/* Host test for the pet cache Storage adapter.  It uses a temporary local
 * directory via PET_CACHE_STORAGE_BASE_PATH and links no ESP/RTOS/display
 * implementation. */

#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "pet_asset_cache_storage.h"

static int fail(const char *message) {
    fprintf(stderr, "FAIL: %s\n", message);
    return 1;
}

static void make_descriptor(pet_asset_descriptor_t *descriptor, const char *revision) {
    static const char hash[] =
        "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
    memset(descriptor, 0, sizeof(*descriptor));
    snprintf(descriptor->encoding, sizeof(descriptor->encoding), "rgb565a8");
    snprintf(descriptor->revision, sizeof(descriptor->revision), "%s", revision);
    descriptor->width = 32;
    descriptor->height = 32;
    descriptor->frame_ms = 450;
    descriptor->frame_count = 2;
    for (int index = 0; index < descriptor->frame_count; ++index) {
        snprintf(descriptor->sha256[index], sizeof(descriptor->sha256[index]), "%s", hash);
    }
}

static bool cancelled_immediately(void *context) {
    (void)context;
    return true;
}

int main(void) {
    pet_asset_cache_storage_clear();

    pet_asset_descriptor_t descriptor;
    make_descriptor(&descriptor, "cache-r1");
    size_t frame_bytes = 0;
    if (!pet_asset_service_frame_bytes(descriptor.width, descriptor.height, &frame_bytes) ||
        frame_bytes > 4096u) return fail("frame geometry");
    uint8_t first[4096];
    uint8_t second[4096];
    for (size_t i = 0; i < frame_bytes; ++i) {
        first[i] = (uint8_t)i;
        second[i] = (uint8_t)(255u - (uint8_t)i);
    }
    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES] = {first, second};
    if (!pet_asset_cache_storage_write(&descriptor, frames, NULL)) {
        return fail("cache write");
    }
    pet_asset_descriptor_t restored = {0};
    if (!pet_asset_cache_storage_read_descriptor(&restored) ||
        strcmp(restored.revision, descriptor.revision) || restored.frame_count != 2) {
        return fail("descriptor restore");
    }
    uint8_t output[4096];
    if (!pet_asset_cache_storage_read_frame(&restored, 0, output, sizeof(output)) ||
        memcmp(output, first, frame_bytes) != 0 ||
        !pet_asset_cache_storage_read_frame(&restored, 1, output, sizeof(output)) ||
        memcmp(output, second, frame_bytes) != 0 ||
        pet_asset_cache_storage_read_frame(&restored, 2, output, sizeof(output))) {
        return fail("frame restore");
    }
    if (pet_asset_cache_storage_drop_if_stale("cache-r1")) {
        return fail("current revision dropped");
    }
    if (!pet_asset_cache_storage_drop_if_stale("cache-r2") ||
        pet_asset_cache_storage_read_descriptor(&restored)) {
        return fail("stale revision not cleared");
    }

    if (!pet_asset_cache_storage_write(&descriptor, frames, NULL)) {
        return fail("cache rewrite");
    }
    pet_asset_cache_storage_options_t cancelled = {.cancelled = cancelled_immediately};
    if (pet_asset_cache_storage_write(&descriptor, frames, &cancelled) ||
        pet_asset_cache_storage_read_descriptor(&restored)) {
        return fail("cancelled write was committed");
    }
    pet_asset_cache_storage_clear();
    if (pet_asset_cache_storage_read_descriptor(&restored)) return fail("clear left metadata");
    printf("PASS pet asset cache storage adapter\n");
    return 0;
}
