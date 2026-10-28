#include "buddy_pet.h"

#include <string.h>

#include "esp_heap_caps.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

static uint8_t *s_frames;
static SemaphoreHandle_t s_lock;
static int s_expected_frames;
static int s_expected_w;
static int s_expected_h;
static int s_frame_ms;
static size_t s_received[BUDDY_PET_MAX_FRAMES];
static uint32_t s_crc[BUDDY_PET_MAX_FRAMES];
static bool s_frame_ready[BUDDY_PET_MAX_FRAMES];
static bool s_ready;
static uint32_t s_version;

static uint32_t crc32_ieee(const uint8_t *data, size_t len) {
    uint32_t crc = 0xFFFFFFFFu;
    for (size_t i = 0; i < len; ++i) {
        crc ^= data[i];
        for (int bit = 0; bit < 8; ++bit) {
            crc = (crc >> 1) ^ (0xEDB88320u & (uint32_t)(-(int32_t)(crc & 1u)));
        }
    }
    return ~crc;
}

static void reset_transfer_locked(int frames) {
    s_expected_frames = frames;
    s_ready = false;
    s_version++;
    memset(s_received, 0, sizeof(s_received));
    memset(s_frame_ready, 0, sizeof(s_frame_ready));
    memset(s_crc, 0, sizeof(s_crc));
}

bool buddy_pet_init(void) {
    if (!s_lock) s_lock = xSemaphoreCreateMutex();
    if (!s_lock) return false;
    s_frames = heap_caps_malloc((size_t)BUDDY_PET_MAX_FRAMES * BUDDY_PET_FRAME_BYTES,
                                MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!s_frames) return false;
    memset(s_frames, 0, (size_t)BUDDY_PET_MAX_FRAMES * BUDDY_PET_FRAME_BYTES);
    return true;
}

buddy_pet_chunk_result_t buddy_pet_chunk(int frame, int frames, int width, int height, int offset,
                                         int total, uint32_t crc32, int frame_ms,
                                         const uint8_t *data, size_t len) {
    if (!s_frames || !s_lock || !data || len == 0) return BUDDY_PET_CHUNK_REJECTED;
    if (frame < 0 || frame >= BUDDY_PET_MAX_FRAMES || frames < 1 ||
        frames > BUDDY_PET_MAX_FRAMES) {
        return BUDDY_PET_CHUNK_REJECTED;
    }
    if (width != BUDDY_PET_WIDTH || height != BUDDY_PET_HEIGHT ||
        total != BUDDY_PET_FRAME_BYTES) {
        return BUDDY_PET_CHUNK_REJECTED;
    }
    if (offset < 0 || offset >= total || offset + (int)len > total) {
        return BUDDY_PET_CHUNK_REJECTED;
    }

    buddy_pet_chunk_result_t result = BUDDY_PET_CHUNK_ACCEPTED;
    xSemaphoreTake(s_lock, portMAX_DELAY);
    if (frame == 0 && offset == 0) {
        // A new asset starts: stop rendering the old buffer while it is being
        // overwritten, and reset all frame progress.
        reset_transfer_locked(frames);
        s_expected_w = width;
        s_expected_h = height;
        s_frame_ms = frame_ms > 0 ? frame_ms : 450;
    } else if (!s_expected_frames) {
        xSemaphoreGive(s_lock);
        return BUDDY_PET_CHUNK_REJECTED;
    }
    if (frames != s_expected_frames || width != s_expected_w || height != s_expected_h) {
        xSemaphoreGive(s_lock);
        return BUDDY_PET_CHUNK_REJECTED;
    }
    // Reject gaps instead of memcpy-ing past them: a dropped line would
    // otherwise only surface as a CRC mismatch after the whole transfer.
    if (offset != (int)s_received[frame]) {
        xSemaphoreGive(s_lock);
        return BUDDY_PET_CHUNK_REJECTED;
    }
    uint8_t *dst = s_frames + (size_t)frame * BUDDY_PET_FRAME_BYTES + offset;
    memcpy(dst, data, len);
    s_received[frame] = (size_t)(offset + (int)len);
    s_crc[frame] = crc32;
    if (s_received[frame] == (size_t)BUDDY_PET_FRAME_BYTES) {
        const uint8_t *base = s_frames + (size_t)frame * BUDDY_PET_FRAME_BYTES;
        if (crc32_ieee(base, BUDDY_PET_FRAME_BYTES) == s_crc[frame]) {
            s_frame_ready[frame] = true;
            bool all = true;
            for (int i = 0; i < s_expected_frames; ++i) {
                if (!s_frame_ready[i]) {
                    all = false;
                    break;
                }
            }
            if (all) {
                s_ready = true;
                s_version++;
                result = BUDDY_PET_CHUNK_READY;
            }
        } else {
            // Corrupt frame: drop the whole transfer and fall back to the
            // vector pet instead of showing torn pixels.
            reset_transfer_locked(s_expected_frames);
            result = BUDDY_PET_CHUNK_REJECTED;
        }
    }
    xSemaphoreGive(s_lock);
    return result;
}

bool buddy_pet_ready(void) {
    if (!s_lock) return false;
    xSemaphoreTake(s_lock, portMAX_DELAY);
    const bool ready = s_ready;
    xSemaphoreGive(s_lock);
    return ready;
}

uint32_t buddy_pet_version(void) {
    if (!s_lock) return 0;
    xSemaphoreTake(s_lock, portMAX_DELAY);
    const uint32_t version = s_version;
    xSemaphoreGive(s_lock);
    return version;
}

void buddy_pet_lock(void) {
    if (s_lock) xSemaphoreTake(s_lock, portMAX_DELAY);
}

void buddy_pet_unlock(void) {
    if (s_lock) xSemaphoreGive(s_lock);
}

const uint8_t *buddy_pet_frame(int index) {
    if (!s_frames || index < 0 || index >= s_expected_frames || !s_ready) return NULL;
    if (!s_frame_ready[index]) return NULL;
    return s_frames + (size_t)index * BUDDY_PET_FRAME_BYTES;
}

int buddy_pet_frame_count(void) {
    return s_frames ? s_expected_frames : 0;
}

int buddy_pet_frame_ms(void) {
    return s_frame_ms > 0 ? s_frame_ms : 450;
}
