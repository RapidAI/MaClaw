#include "services/pet_asset_apply_service.h"

#include <stdlib.h>
#include <string.h>

#include "esp_heap_caps.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

#include "presentation/scene_presenter.h"

static SemaphoreHandle_t s_apply_mutex;
static portMUX_TYPE s_init_lock = portMUX_INITIALIZER_UNLOCKED;
static char s_loaded_revision[PET_ASSET_SERVICE_REVISION_CAPACITY];
static int s_loaded_frame_count;

static void clear_frame_array(uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES]) {
    if (frames) memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_SERVICE_MAX_FRAMES);
}

void pet_asset_apply_service_free_frames(
    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES], uint32_t frame_count) {
    if (!frames) return;
    const uint32_t count = frame_count > PET_ASSET_SERVICE_MAX_FRAMES
                               ? PET_ASSET_SERVICE_MAX_FRAMES
                               : frame_count;
    for (uint32_t i = 0; i < count; ++i) {
        heap_caps_free(frames[i]);
        frames[i] = NULL;
    }
}

static bool clone_frames(const pet_asset_descriptor_t *descriptor,
                         uint8_t *const source[PET_ASSET_SERVICE_MAX_FRAMES],
                         uint8_t *copies[PET_ASSET_SERVICE_MAX_FRAMES]) {
    if (!descriptor || !source || !copies || descriptor->frame_count < 1 ||
        descriptor->frame_count > (int)PET_ASSET_SERVICE_MAX_FRAMES) {
        return false;
    }
    size_t bytes = 0;
    if (!pet_asset_service_frame_bytes(descriptor->width, descriptor->height, &bytes)) {
        return false;
    }
    clear_frame_array(copies);
    for (int i = 0; i < descriptor->frame_count; ++i) {
        if (!source[i]) goto fail;
        copies[i] = heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!copies[i]) copies[i] = malloc(bytes);
        if (!copies[i]) goto fail;
        memcpy(copies[i], source[i], bytes);
    }
    return true;

fail:
    pet_asset_apply_service_free_frames(copies, PET_ASSET_SERVICE_MAX_FRAMES);
    return false;
}

static device_status_t install_consuming_with_fallback(
    const pet_asset_descriptor_t *descriptor,
    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES],
    int *out_installed_frame_count,
    int *out_installed_frame_ms) {
    if (!descriptor || !frames) return DEVICE_STATUS_INVALID_ARGUMENT;

    device_status_t status = scene_presenter_set_pet_asset_consuming(
        frames, (size_t)descriptor->frame_count, (size_t)descriptor->width,
        (size_t)descriptor->height, (uint32_t)descriptor->frame_ms);
    int used_count = descriptor->frame_count;
    int used_frame_ms = descriptor->frame_ms;
    while (status == DEVICE_STATUS_RESOURCE_EXHAUSTED && used_count > 1) {
        int remaining_count = 0;
        for (int i = 0; i < descriptor->frame_count; ++i) {
            if (frames[i]) frames[remaining_count++] = frames[i];
        }
        for (int i = remaining_count; i < descriptor->frame_count; ++i) frames[i] = NULL;

        uint32_t next_count = 0;
        uint32_t next_frame_ms = 0;
        if (!pet_asset_service_next_memory_fallback(
                descriptor, (uint32_t)used_count, (uint32_t)remaining_count,
                &next_count, &next_frame_ms)) {
            break;
        }
        used_count = (int)next_count;
        used_frame_ms = (int)next_frame_ms;
        status = scene_presenter_set_pet_asset_consuming(
            frames, (size_t)used_count, (size_t)descriptor->width,
            (size_t)descriptor->height, (uint32_t)used_frame_ms);
    }
    if (out_installed_frame_count) {
        *out_installed_frame_count = status == DEVICE_STATUS_OK ? used_count : 0;
    }
    if (out_installed_frame_ms) {
        *out_installed_frame_ms = status == DEVICE_STATUS_OK ? used_frame_ms : 0;
    }
    return status;
}

device_status_t pet_asset_apply_service_init(void) {
    if (s_apply_mutex) return DEVICE_STATUS_OK;
    SemaphoreHandle_t mutex = xSemaphoreCreateMutex();
    if (!mutex) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    taskENTER_CRITICAL(&s_init_lock);
    if (!s_apply_mutex) {
        s_apply_mutex = mutex;
        mutex = NULL;
    }
    taskEXIT_CRITICAL(&s_init_lock);
    if (mutex) vSemaphoreDelete(mutex);
    return DEVICE_STATUS_OK;
}

bool pet_asset_apply_service_revision_installed(
    const pet_asset_descriptor_t *descriptor) {
    if (!descriptor || !s_apply_mutex || descriptor->frame_count < 1) return false;
    if (xSemaphoreTake(s_apply_mutex, portMAX_DELAY) != pdTRUE) return false;
    const bool installed = s_loaded_revision[0] != '\0' &&
                           !strcmp(s_loaded_revision, descriptor->revision) &&
                           s_loaded_frame_count >= descriptor->frame_count;
    xSemaphoreGive(s_apply_mutex);
    return installed;
}

device_status_t pet_asset_apply_service_clear(
    pet_asset_apply_service_admitted_fn admitted, void *admission_context) {
    if (!s_apply_mutex || xSemaphoreTake(s_apply_mutex, portMAX_DELAY) != pdTRUE) {
        return DEVICE_STATUS_BUSY;
    }
    if (admitted && !admitted(admission_context)) {
        xSemaphoreGive(s_apply_mutex);
        return DEVICE_STATUS_BUSY;
    }
    const device_status_t status = scene_presenter_set_pet_asset(NULL, 0, 0, 0, 0);
    if (status == DEVICE_STATUS_OK) {
        s_loaded_revision[0] = '\0';
        s_loaded_frame_count = 0;
    }
    xSemaphoreGive(s_apply_mutex);
    return status;
}

device_status_t pet_asset_apply_service_install_preview(
    const pet_asset_descriptor_t *descriptor,
    uint8_t *const frames[PET_ASSET_SERVICE_MAX_FRAMES]) {
    if (!descriptor || !frames || !frames[0] || !s_apply_mutex) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (xSemaphoreTake(s_apply_mutex, portMAX_DELAY) != pdTRUE) return DEVICE_STATUS_BUSY;
    const uint8_t *first[1] = {frames[0]};
    const device_status_t status = scene_presenter_set_pet_asset(
        first, 1, (size_t)descriptor->width, (size_t)descriptor->height,
        (uint32_t)descriptor->frame_ms);
    xSemaphoreGive(s_apply_mutex);
    return status;
}

device_status_t pet_asset_apply_service_install_full(
    const pet_asset_descriptor_t *descriptor,
    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES],
    bool prepare_cache_mirror,
    uint8_t *cache_frames[PET_ASSET_SERVICE_MAX_FRAMES],
    pet_asset_apply_service_admitted_fn admitted,
    void *admission_context,
    int *out_installed_frame_count,
    int *out_installed_frame_ms) {
    if (!descriptor || !frames || !s_apply_mutex || descriptor->frame_count < 1 ||
        descriptor->frame_count > (int)PET_ASSET_SERVICE_MAX_FRAMES) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (cache_frames) clear_frame_array(cache_frames);
    if (out_installed_frame_count) *out_installed_frame_count = 0;
    if (out_installed_frame_ms) *out_installed_frame_ms = 0;
    if (xSemaphoreTake(s_apply_mutex, portMAX_DELAY) != pdTRUE) return DEVICE_STATUS_BUSY;

    if (admitted && !admitted(admission_context)) {
        xSemaphoreGive(s_apply_mutex);
        return DEVICE_STATUS_BUSY;
    }
    if (prepare_cache_mirror && cache_frames && !clone_frames(descriptor, frames, cache_frames)) {
        /* Cache persistence is an optional optimization.  Preserve the live
         * transaction when its second source set cannot be reserved. */
        clear_frame_array(cache_frames);
    }
    const device_status_t status = install_consuming_with_fallback(
        descriptor, frames, out_installed_frame_count, out_installed_frame_ms);
    if (status == DEVICE_STATUS_OK) {
        strlcpy(s_loaded_revision, descriptor->revision, sizeof(s_loaded_revision));
        s_loaded_frame_count = out_installed_frame_count ? *out_installed_frame_count
                                                          : descriptor->frame_count;
    }
    xSemaphoreGive(s_apply_mutex);
    return status;
}
