/*
 * Host test for the raw pet-frame header. The byte layout is shared with
 * buddy/protocol/bpt1.go.
 *
 *   gcc -std=gnu11 -I../../main -o test_bpt1 test_bpt1.c ../../main/buddy_bpt1.c
 */

#include <stdio.h>
#include <string.h>

#include "buddy_bpt1.h"

static int check(const char *name, int cond) {
    printf("%s: %s\n", name, cond ? "ok" : "FAIL");
    return cond ? 0 : 1;
}

int main(void) {
    int failed = 0;
    uint8_t frame[BUDDY_BPT1_PREFIX + 4];
    memset(frame, 0, sizeof(frame));
    memcpy(frame, "BPT1", 4);
    frame[4] = 1;
    frame[5] = 8;
    frame[6] = 128;
    frame[7] = 0;
    frame[8] = 128;
    frame[9] = 0;
    frame[10] = 450 & 0xff;
    frame[11] = (450 >> 8) & 0xff;
    frame[12] = 4; /* offset 4 */
    frame[24] = 4; /* data_len 4 */
    frame[28] = 9;
    frame[29] = 8;
    frame[30] = 7;
    frame[31] = 6;

    buddy_bpt1_info_t info;
    const uint8_t *data = NULL;
    const int n = buddy_bpt1_parse(frame, (int)sizeof(frame), &info, &data);
    failed += check("consumes prefix plus data", n == BUDDY_BPT1_PREFIX + 4);
    failed += check("frame index", info.frame == 1 && info.frames == 8);
    failed += check("geometry", info.width == 128 && info.height == 128 && info.frame_ms == 450);
    failed += check("offset", info.offset == 4 && info.data_len == 4);
    failed += check("payload", data && data[0] == 9 && data[3] == 6);
    failed += check("rejects a short buffer", buddy_bpt1_parse(frame, 10, &info, &data) < 0);
    frame[0] = 'X';
    failed += check("rejects a bad magic", buddy_bpt1_parse(frame, (int)sizeof(frame), &info, &data) < 0);
    return failed == 0 ? 0 : 1;
}
