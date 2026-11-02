/*
 * Host tests for the task carousel and pet mood. Neither file touches ESP-IDF.
 *
 *   gcc -std=gnu11 -I../../main -o test_task_cursor test_task_cursor.c ^
 *       ../../main/buddy_task_cursor.c ../../main/buddy_mood.c
 */

#include <stdio.h>

#include "buddy_mood.h"
#include "buddy_task_cursor.h"

static int check(const char *name, int cond) {
    printf("%s: %s\n", name, cond ? "ok" : "FAIL");
    return cond ? 0 : 1;
}

int main(void) {
    int failed = 0;
    buddy_task_cursor_t cursor = {0, 0};

    failed += check("single task stays at 0", buddy_task_cursor_index(&cursor, 1, 9000) == 0);
    failed += check("dwell holds the origin", buddy_task_cursor_index(&cursor, 3, 2999) == 0);
    failed += check("step at 3s", buddy_task_cursor_index(&cursor, 3, 3000) == 1);
    failed += check("wrap at 9s", buddy_task_cursor_index(&cursor, 3, 9000) == 0);
    failed += check("negative elapsed clamps", buddy_task_cursor_index(&cursor, 3, -10) == 0);
    buddy_task_cursor_t aged = {0, 0};
    /* 3e9 steps does not fit in a 32-bit step count. */
    failed += check("long uptime wraps",
                    buddy_task_cursor_index(&aged, 3, (int64_t)3000 * 3000000000LL) == 0);
    failed += check("long uptime offset",
                    buddy_task_cursor_index(&aged, 3, (int64_t)3000 * 3000000001LL) == 1);
    buddy_task_cursor_t aged_origin = {2, 0};
    failed += check("long uptime keeps origin",
                    buddy_task_cursor_index(&aged_origin, 3, (int64_t)3000 * 3000000000LL) == 2);

    buddy_task_cursor_advance(&cursor, 3, 1000);
    failed += check("tap shows the next task", buddy_task_cursor_index(&cursor, 3, 1000) == 1);
    failed += check("tap restarts the dwell", buddy_task_cursor_index(&cursor, 3, 3999) == 1);
    failed += check("auto step after a tap", buddy_task_cursor_index(&cursor, 3, 4000) == 2);

    const int frozen = cursor.origin;
    buddy_task_cursor_advance(&cursor, 1, 5000);
    failed += check("tap ignored for one task", cursor.origin == frozen);

    buddy_task_cursor_t pinned = {0, 0, false};
    buddy_task_cursor_toggle_pin(&pinned, 3, 1000);
    failed += check("swipe pins the visible task", buddy_task_cursor_pinned(&pinned));
    failed += check("pinned task ignores the clock", buddy_task_cursor_index(&pinned, 3, 20000) == 0);
    buddy_task_cursor_advance(&pinned, 3, 21000);
    failed += check("tap still moves a pinned task", buddy_task_cursor_index(&pinned, 3, 40000) == 1);
    buddy_task_cursor_toggle_pin(&pinned, 3, 41000);
    failed += check("second swipe resumes", !buddy_task_cursor_pinned(&pinned));
    failed += check("resumed carousel steps again", buddy_task_cursor_index(&pinned, 3, 44000) == 2);
    buddy_task_cursor_toggle_pin(&pinned, 3, 45000);
    buddy_task_cursor_note_count(&pinned, 1, 50000);
    failed += check("one task releases the pin", !buddy_task_cursor_pinned(&pinned));
    buddy_task_cursor_note_count(&pinned, 1, 90000);
    failed += check("a single task restarts the dwell", buddy_task_cursor_index(&pinned, 3, 92000) == 0);
    failed += check("the restarted dwell holds", buddy_task_cursor_index(&pinned, 3, 92999) == 0);
    failed += check("dwell steps after three seconds", buddy_task_cursor_index(&pinned, 3, 93000) == 1);
    buddy_task_cursor_note_count(&pinned, 3, 93000);
    failed += check("a longer list does not pin itself", !buddy_task_cursor_pinned(&pinned));

    failed += check("empty pet is automatic", buddy_mood_parse("") == -1);
    failed += check("unknown pet is automatic", buddy_mood_parse("wave") == -1);
    failed += check("parse sleep", buddy_mood_parse("sleep") == BUDDY_MOOD_SLEEP);
    failed += check("offline forces sleep", buddy_mood_resolve(BUDDY_MOOD_HAPPY, 1, 0, 0) == BUDDY_MOOD_SLEEP);
    failed += check("host mood wins online", buddy_mood_resolve(BUDDY_MOOD_HAPPY, 1, 1, 0) == BUDDY_MOOD_HAPPY);
    failed += check("host mood wins while dimmed", buddy_mood_resolve(BUDDY_MOOD_HAPPY, 1, 1, 1) == BUDDY_MOOD_HAPPY);
    failed += check("busy stays while dimmed", buddy_mood_resolve(BUDDY_MOOD_BUSY, 1, 1, 1) == BUDDY_MOOD_BUSY);
    failed += check("idle still sleeps while dimmed", buddy_mood_resolve(BUDDY_MOOD_IDLE, 0, 1, 1) == BUDDY_MOOD_SLEEP);
    failed += check("dimmed glass sleeps", buddy_mood_resolve(-1, 0, 1, 1) == BUDDY_MOOD_SLEEP);
    failed += check("tasks derive busy", buddy_mood_resolve(-1, 0, 1, 0) == BUDDY_MOOD_BUSY);
    failed += check("celebrate derives happy", buddy_mood_resolve(-1, 3, 1, 0) == BUDDY_MOOD_HAPPY);
    failed += check("standby derives idle", buddy_mood_resolve(-1, 1, 1, 0) == BUDDY_MOOD_IDLE);

    failed += check("sleep slows frames", buddy_mood_frame_ms(BUDDY_MOOD_SLEEP, 450) == 1350);
    failed += check("happy speeds frames", buddy_mood_frame_ms(BUDDY_MOOD_HAPPY, 450) == 300);
    failed += check("busy speeds frames", buddy_mood_frame_ms(BUDDY_MOOD_BUSY, 450) == 337);
    failed += check("idle keeps the base", buddy_mood_frame_ms(BUDDY_MOOD_IDLE, 450) == 450);

    return failed == 0 ? 0 : 1;
}
