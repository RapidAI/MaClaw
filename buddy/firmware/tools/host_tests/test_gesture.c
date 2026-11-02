/*
 * gcc -std=gnu11 -Wall -Werror -I../../main -o test_gesture test_gesture.c ../../main/buddy_gesture.c
 */
#include "buddy_gesture.h"

#include <stdio.h>
#include <stdlib.h>

static void expect(int cond, const char *msg) {
    if (cond) return;
    fprintf(stderr, "fail: %s\n", msg);
    exit(1);
}

int main(void) {
    buddy_gesture_t g = {0};

    expect(buddy_gesture_contact(&g, true, 0, false) == BUDDY_GESTURE_NONE, "press is not a tap yet");
    expect(buddy_gesture_contact(&g, false, 39, false) == BUDDY_GESTURE_NONE, "bounce is ignored");

    expect(buddy_gesture_contact(&g, true, 100, false) == BUDDY_GESTURE_NONE, "second press arms");
    expect(buddy_gesture_contact(&g, false, 140, false) == BUDDY_GESTURE_NONE, "release waits for a second tap");
    expect(buddy_gesture_contact(&g, false, 419, false) == BUDDY_GESTURE_NONE, "279ms is still the wait");
    expect(buddy_gesture_contact(&g, false, 420, false) == BUDDY_GESTURE_TAP, "280ms later the lone tap fires");

    expect(buddy_gesture_contact(&g, true, 500, false) == BUDDY_GESTURE_NONE, "hold arms");
    expect(buddy_gesture_contact(&g, true, 1199, false) == BUDDY_GESTURE_NONE, "699ms is still a press");
    expect(buddy_gesture_contact(&g, true, 1200, false) == BUDDY_GESTURE_HOLD, "700ms toggles mute");
    expect(buddy_gesture_contact(&g, false, 1250, false) == BUDDY_GESTURE_NONE, "release after a hold is not a tap");

    expect(buddy_gesture_contact(&g, true, 1400, true) == BUDDY_GESTURE_NONE, "dim press arms");
    expect(buddy_gesture_contact(&g, true, 2200, false) == BUDDY_GESTURE_NONE, "dim hold does not mute");
    expect(buddy_gesture_contact(&g, false, 2300, false) == BUDDY_GESTURE_NONE, "dim release does not tap");

    expect(buddy_gesture_contact(&g, true, 2500, false) == BUDDY_GESTURE_NONE, "bright press after a wake");
    expect(buddy_gesture_contact(&g, false, 2600, false) == BUDDY_GESTURE_NONE, "release arms the double window");
    expect(buddy_gesture_contact(&g, false, 2880, false) == BUDDY_GESTURE_TAP, "the next press is a tap again");

    expect(buddy_gesture_contact(&g, true, 3000, false) == BUDDY_GESTURE_NONE, "double first press");
    expect(buddy_gesture_contact(&g, false, 3080, false) == BUDDY_GESTURE_NONE, "double first release waits");
    expect(buddy_gesture_contact(&g, true, 3200, false) == BUDDY_GESTURE_NONE, "double second press");
    expect(buddy_gesture_contact(&g, false, 3280, false) == BUDDY_GESTURE_DOUBLE, "second tap confirms");
    expect(buddy_gesture_contact(&g, false, 3600, false) == BUDDY_GESTURE_NONE, "confirm does not also tap");

    buddy_gesture_track(&g, 10, 80);
    expect(buddy_gesture_contact(&g, true, 4000, false) == BUDDY_GESTURE_NONE, "swipe press");
    buddy_gesture_track(&g, 80, 90);
    expect(buddy_gesture_contact(&g, false, 4100, false) == BUDDY_GESTURE_SWIPE, "horizontal travel pins");
    expect(buddy_gesture_contact(&g, false, 4500, false) == BUDDY_GESTURE_NONE, "swipe does not also tap");

    expect(buddy_gesture_contact(&g, true, 5000, false) == BUDDY_GESTURE_NONE, "latched swipe press");
    buddy_gesture_mark_swipe(&g);
    expect(buddy_gesture_contact(&g, false, 5080, false) == BUDDY_GESTURE_SWIPE, "chip slide pins without travel samples");

    buddy_gesture_track(&g, 10, 80);
    expect(buddy_gesture_contact(&g, true, 6000, false) == BUDDY_GESTURE_NONE, "slow swipe press");
    buddy_gesture_track(&g, 80, 90);
    expect(buddy_gesture_contact(&g, true, 6700, false) == BUDDY_GESTURE_NONE, "travel past 700ms does not mute");
    expect(buddy_gesture_contact(&g, false, 6800, false) == BUDDY_GESTURE_SWIPE, "release after a long drag still pins");

    buddy_gesture_track(&g, 0, 40);
    expect(buddy_gesture_contact(&g, true, 7000, false) == BUDDY_GESTURE_NONE, "tap before a late swipe");
    expect(buddy_gesture_contact(&g, false, 7040, false) == BUDDY_GESTURE_NONE, "that tap waits");
    buddy_gesture_track(&g, 0, 40);
    expect(buddy_gesture_contact(&g, true, 7320, false) == BUDDY_GESTURE_TAP, "the lone tap fires as the next press starts");
    buddy_gesture_track(&g, 80, 40);
    expect(buddy_gesture_contact(&g, false, 7400, false) == BUDDY_GESTURE_SWIPE, "that press can still pin");
    expect(buddy_gesture_contact(&g, false, 7800, false) == BUDDY_GESTURE_NONE, "the pin does not also tap");

    buddy_gesture_track(&g, 10, 80);
    expect(buddy_gesture_contact(&g, true, 8000, false) == BUDDY_GESTURE_NONE, "out-and-back press");
    buddy_gesture_track(&g, 80, 90);
    buddy_gesture_track(&g, 12, 82);
    expect(buddy_gesture_contact(&g, false, 8100, false) == BUDDY_GESTURE_SWIPE, "peak travel still pins");

    expect(buddy_gesture_contact(&g, true, 9000, false) == BUDDY_GESTURE_NONE, "tap before a swipe");
    expect(buddy_gesture_contact(&g, false, 9040, false) == BUDDY_GESTURE_NONE, "tap waits");
    buddy_gesture_track(&g, 10, 40);
    expect(buddy_gesture_contact(&g, true, 9100, false) == BUDDY_GESTURE_NONE, "swipe starts inside the wait");
    buddy_gesture_track(&g, 80, 48);
    expect(buddy_gesture_contact(&g, false, 9180, false) == BUDDY_GESTURE_TAP, "the waiting tap is not swallowed");
    expect(buddy_gesture_contact(&g, false, 9190, false) == BUDDY_GESTURE_SWIPE, "then the swipe pins");
    expect(buddy_gesture_contact(&g, false, 9600, false) == BUDDY_GESTURE_NONE, "that pair does not tap again");

    expect(buddy_gesture_contact(&g, true, 10000, false) == BUDDY_GESTURE_NONE, "tap before a hold");
    expect(buddy_gesture_contact(&g, false, 10040, false) == BUDDY_GESTURE_NONE, "hold's tap waits");
    expect(buddy_gesture_contact(&g, true, 10100, false) == BUDDY_GESTURE_NONE, "hold starts inside the wait");
    expect(buddy_gesture_contact(&g, true, 10800, false) == BUDDY_GESTURE_TAP, "the waiting tap precedes mute");
    expect(buddy_gesture_contact(&g, true, 10820, false) == BUDDY_GESTURE_HOLD, "then the hold mutes");
    expect(buddy_gesture_contact(&g, false, 10900, false) == BUDDY_GESTURE_NONE, "release after that hold is empty");

    printf("gesture: ok\n");
    return 0;
}
