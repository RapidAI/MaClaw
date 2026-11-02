#pragma once

#include <stddef.h>
#include <stdint.h>

#define BUDDY_BPT1_MAGIC "BPT1"
#define BUDDY_BPT1_MAGIC_LEN 4
#define BUDDY_BPT1_PREFIX 28
#define BUDDY_BPT1_MAX_DATA 1024
#define BUDDY_BPT1_MAX_FRAME (BUDDY_BPT1_PREFIX + BUDDY_BPT1_MAX_DATA)

/** One raw pet-asset chunk. Layout, little-endian:
 *  0..3 magic "BPT1"
 *  4 frame u8, 5 frames u8, 6 width u16, 8 height u16, 10 frame_ms u16
 *  12 offset u32, 16 total u32, 20 crc32 u32, 24 data_len u16, 26 reserved u16
 *  28 data[data_len]
 */
typedef struct {
    int frame;
    int frames;
    int width;
    int height;
    int frame_ms;
    int offset;
    int total;
    uint32_t crc32;
    int data_len;
} buddy_bpt1_info_t;

/** Parses one complete frame. On success returns the byte count and points
 * data at the payload inside buf. Returns -1 when the buffer is not a frame. */
int buddy_bpt1_parse(const uint8_t *buf, int len, buddy_bpt1_info_t *info, const uint8_t **data);
