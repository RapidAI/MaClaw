#include "buddy_bpt1.h"

#include <string.h>

static uint16_t rd16(const uint8_t *p) {
    return (uint16_t)p[0] | ((uint16_t)p[1] << 8);
}

static uint32_t rd32(const uint8_t *p) {
    return (uint32_t)p[0] | ((uint32_t)p[1] << 8) | ((uint32_t)p[2] << 16) | ((uint32_t)p[3] << 24);
}

int buddy_bpt1_parse(const uint8_t *buf, int len, buddy_bpt1_info_t *info, const uint8_t **data) {
    if (!buf || !info || len < BUDDY_BPT1_PREFIX) return -1;
    if (memcmp(buf, BUDDY_BPT1_MAGIC, BUDDY_BPT1_MAGIC_LEN) != 0) return -1;
    const int data_len = rd16(buf + 24);
    if (data_len <= 0 || data_len > BUDDY_BPT1_MAX_DATA) return -1;
    if (len < BUDDY_BPT1_PREFIX + data_len) return -1;
    info->frame = buf[4];
    info->frames = buf[5];
    info->width = rd16(buf + 6);
    info->height = rd16(buf + 8);
    info->frame_ms = rd16(buf + 10);
    info->offset = (int)rd32(buf + 12);
    info->total = (int)rd32(buf + 16);
    info->crc32 = rd32(buf + 20);
    info->data_len = data_len;
    if (data) *data = buf + BUDDY_BPT1_PREFIX;
    return BUDDY_BPT1_PREFIX + data_len;
}
