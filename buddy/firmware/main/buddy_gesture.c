#include "buddy_gesture.h"

/* A finger or key that is down for less than this is bounce. */
#define BUDDY_GESTURE_TAP_MIN_MS 40
/* Held at least this long toggles mute instead of advancing the carousel. */
#define BUDDY_GESTURE_HOLD_MS 700
/* A second tap inside this window confirms; the first tap waits it out. */
#define BUDDY_GESTURE_DOUBLE_MS 280
/* Horizontal travel, in panel pixels, that pins the carousel instead of tapping. */
#define BUDDY_GESTURE_SWIPE_PX 48
#define BUDDY_GESTURE_FOLLOW_SWIPE 1
#define BUDDY_GESTURE_FOLLOW_HOLD 2

static void begin_press(buddy_gesture_t *g, int64_t now_ms, bool resting) {
    g->down = true;
    g->long_fired = false;
    g->wake_only = resting;
    g->down_ms = now_ms;
    g->x0 = g->x;
    g->y0 = g->y;
    g->max_dx = 0;
    g->max_dy = 0;
    g->swipe = false;
}

void buddy_gesture_track(buddy_gesture_t *g, int x, int y) {
    if (!g) return;
    g->x = x;
    g->y = y;
    g->have_xy = true;
    if (!g->down) return;
    int dx = x - g->x0;
    int dy = y - g->y0;
    if (dx < 0) dx = -dx;
    if (dy < 0) dy = -dy;
    if (dx > g->max_dx) g->max_dx = dx;
    if (dy > g->max_dy) g->max_dy = dy;
}

void buddy_gesture_mark_swipe(buddy_gesture_t *g) {
    if (!g) return;
    g->swipe = true;
}

static int abs_int(int v) { return v < 0 ? -v : v; }

static bool swipe_travel(const buddy_gesture_t *g) {
    if (g->swipe) return true;
    if (!g->have_xy) return false;
    /* Peak travel, not only the last sample. A flick that returns toward
     * the start, or a bad final read, should still pin. */
    int dx = abs_int(g->x - g->x0);
    int dy = abs_int(g->y - g->y0);
    if (g->max_dx > dx) dx = g->max_dx;
    if (g->max_dy > dy) dy = g->max_dy;
    return dx >= BUDDY_GESTURE_SWIPE_PX && dx > dy;
}

buddy_gesture_result_t buddy_gesture_contact(buddy_gesture_t *g, bool contact, int64_t now_ms, bool resting) {
    if (!g) return BUDDY_GESTURE_NONE;
    if (g->follow) {
        const buddy_gesture_result_t event =
            g->follow == BUDDY_GESTURE_FOLLOW_HOLD ? BUDDY_GESTURE_HOLD : BUDDY_GESTURE_SWIPE;
        g->follow = 0;
        if (contact && !g->down) {
            g->second = false;
            g->tap_wait = false;
            begin_press(g, now_ms, resting);
        }
        return event;
    }
    if (!contact && g->tap_wait && now_ms - g->tap_ms >= BUDDY_GESTURE_DOUBLE_MS) {
        g->tap_wait = false;
        return BUDDY_GESTURE_TAP;
    }
    if (contact) {
        if (!g->down) {
            if (g->tap_wait && now_ms - g->tap_ms >= BUDDY_GESTURE_DOUBLE_MS) {
                /* The lone tap is late, and this sample is already the next press.
                 * Arm it now so a swipe keeps this point as its origin. */
                g->tap_wait = false;
                g->second = false;
                begin_press(g, now_ms, resting);
                return BUDDY_GESTURE_TAP;
            }
            g->second = g->tap_wait && !resting && now_ms - g->tap_ms < BUDDY_GESTURE_DOUBLE_MS;
            if (g->second) g->tap_wait = false;
            begin_press(g, now_ms, resting);
        } else if (!g->wake_only && !g->long_fired && now_ms - g->down_ms >= BUDDY_GESTURE_HOLD_MS) {
            /* A drag that already crossed the screen pins on release. Firing
             * mute here would throw that gesture away. */
            if (swipe_travel(g)) return BUDDY_GESTURE_NONE;
            const bool pending_tap = g->second;
            g->long_fired = true;
            g->tap_wait = false;
            g->second = false;
            g->swipe = false;
            if (pending_tap) {
                g->follow = BUDDY_GESTURE_FOLLOW_HOLD;
                return BUDDY_GESTURE_TAP;
            }
            return BUDDY_GESTURE_HOLD;
        }
        return BUDDY_GESTURE_NONE;
    }
    if (!g->down) return BUDDY_GESTURE_NONE;
    const bool second = g->second;
    const bool wake = g->wake_only;
    const bool held = !g->long_fired && now_ms - g->down_ms >= BUDDY_GESTURE_TAP_MIN_MS;
    const bool swiped = !wake && held && swipe_travel(g);
    g->down = false;
    g->second = false;
    g->have_xy = false;
    g->swipe = false;
    g->max_dx = 0;
    g->max_dy = 0;
    if (wake || g->long_fired || !held) return BUDDY_GESTURE_NONE;
    if (swiped) {
        g->tap_wait = false;
        if (second) {
            g->follow = BUDDY_GESTURE_FOLLOW_SWIPE;
            return BUDDY_GESTURE_TAP;
        }
        return BUDDY_GESTURE_SWIPE;
    }
    if (second) return BUDDY_GESTURE_DOUBLE;
    g->tap_wait = true;
    g->tap_ms = now_ms;
    return BUDDY_GESTURE_NONE;
}
