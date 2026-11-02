#pragma once

#include <stdbool.h>
#include <stdint.h>

/** Short press and long press shared by the touch panel and the front key.
 * The caller samples whether the backlight is dimmed or off and passes it as
 * resting. A press that starts in that state only wakes the glass. */
typedef struct {
    bool down;
    bool long_fired;
    bool wake_only;
    bool tap_wait;
    bool second;
    bool have_xy;
    bool swipe;
    /* 1 = emit swipe next, 2 = emit hold next. A second gesture can resolve
     * into something other than a double tap; the waiting tap goes out first. */
    uint8_t follow;
    int x0, y0, x, y;
    int max_dx, max_dy;
    int64_t down_ms;
    int64_t tap_ms;
} buddy_gesture_t;

typedef enum {
    BUDDY_GESTURE_NONE = 0,
    BUDDY_GESTURE_TAP,
    BUDDY_GESTURE_HOLD,
    BUDDY_GESTURE_DOUBLE,
    BUDDY_GESTURE_SWIPE,
} buddy_gesture_result_t;

/** Latest finger position. The front key never calls this, so it cannot swipe. */
void buddy_gesture_track(buddy_gesture_t *g, int x, int y);

/** The touch chip latched a horizontal slide. A 60ms poll can miss the travel. */
void buddy_gesture_mark_swipe(buddy_gesture_t *g);

buddy_gesture_result_t buddy_gesture_contact(buddy_gesture_t *g, bool contact, int64_t now_ms, bool resting);
