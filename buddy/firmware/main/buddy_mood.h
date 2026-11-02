#pragma once

enum {
    BUDDY_MOOD_IDLE = 0,
    BUDDY_MOOD_HAPPY = 1,
    BUDDY_MOOD_BUSY = 2,
    BUDDY_MOOD_SLEEP = 3,
};

/** Parses a pet.state string. Empty or unknown returns -1 (derive locally). */
int buddy_mood_parse(const char *state);

/** Offline forces sleep. busy and happy from the host win while the link is
 * up, including on a dimmed glass. idle does not: the host sends it whenever
 * the assistant is between turns, and a dimmed glass should still sleep.
 * Otherwise the page decides: tasks → busy, celebrate → happy, else idle.
 * page uses the BUDDY_PAGE_* values (0 tasks, 1 standby, 2 prompt, 3 celebrate). */
int buddy_mood_resolve(int explicit_mood, int page, int online, int resting);

/** Playback period for one pet frame. Sleep is slower, happy and busy faster. */
int buddy_mood_frame_ms(int mood, int base_ms);
