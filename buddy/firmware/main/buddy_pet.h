#pragma once

#include <stdbool.h>
#include <stdint.h>
#include <stddef.h>

#define BUDDY_PET_MAX_FRAMES 8
#define BUDDY_PET_WIDTH 128
#define BUDDY_PET_HEIGHT 128
#define BUDDY_PET_FRAME_BYTES (BUDDY_PET_WIDTH * BUDDY_PET_HEIGHT * 3)

/** Result of applying one transfer chunk. */
typedef enum {
    BUDDY_PET_CHUNK_REJECTED = 0,
    BUDDY_PET_CHUNK_ACCEPTED = 1,
    BUDDY_PET_CHUNK_READY = 2,
} buddy_pet_chunk_result_t;

/** Allocates the PSRAM frame store. */
bool buddy_pet_init(void);

/** Applies one transfer chunk. Chunks must arrive strictly in order; the
 * result drives the per-chunk ack that paces the host. */
buddy_pet_chunk_result_t buddy_pet_chunk(int frame, int frames, int width, int height, int offset,
                                         int total, uint32_t crc32, int frame_ms,
                                         const uint8_t *data, size_t len);

bool buddy_pet_ready(void);
uint32_t buddy_pet_version(void);

/** Locks the frame store for a complete draw of one frame. */
void buddy_pet_lock(void);
void buddy_pet_unlock(void);
const uint8_t *buddy_pet_frame(int index);
int buddy_pet_frame_count(void);
int buddy_pet_frame_ms(void);
