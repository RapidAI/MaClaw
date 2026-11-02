#include "buddy_mood.h"

#include <stdbool.h>
#include <string.h>

int buddy_mood_parse(const char *state) {
    if (!state || !state[0]) return -1;
    if (strcmp(state, "idle") == 0) return BUDDY_MOOD_IDLE;
    if (strcmp(state, "happy") == 0) return BUDDY_MOOD_HAPPY;
    if (strcmp(state, "busy") == 0) return BUDDY_MOOD_BUSY;
    if (strcmp(state, "sleep") == 0) return BUDDY_MOOD_SLEEP;
    return -1;
}

int buddy_mood_resolve(int explicit_mood, int page, int online, int resting) {
    if (!online) return BUDDY_MOOD_SLEEP;
    const bool active = explicit_mood == BUDDY_MOOD_BUSY || explicit_mood == BUDDY_MOOD_HAPPY;
    if (resting && !active) return BUDDY_MOOD_SLEEP;
    if (explicit_mood >= BUDDY_MOOD_IDLE && explicit_mood <= BUDDY_MOOD_SLEEP) {
        return explicit_mood;
    }
    if (page == 0) return BUDDY_MOOD_BUSY;
    if (page == 3) return BUDDY_MOOD_HAPPY;
    return BUDDY_MOOD_IDLE;
}

int buddy_mood_frame_ms(int mood, int base_ms) {
    if (base_ms < 1) base_ms = 450;
    switch (mood) {
        case BUDDY_MOOD_SLEEP:
            return base_ms * 3;
        case BUDDY_MOOD_HAPPY: {
            const int paced = base_ms * 2 / 3;
            return paced < 80 ? 80 : paced;
        }
        case BUDDY_MOOD_BUSY: {
            const int paced = base_ms * 3 / 4;
            return paced < 80 ? 80 : paced;
        }
        default:
            return base_ms;
    }
}
