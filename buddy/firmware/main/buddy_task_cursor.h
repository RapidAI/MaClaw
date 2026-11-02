#pragma once

#include <stdbool.h>
#include <stdint.h>

/** Remembers where a short press left the in-progress task list.
 * The shown index then advances one step every 3000 ms. */
typedef struct {
    int origin;
    int64_t origin_ms;
    bool pinned;
} buddy_task_cursor_t;

/** Index of the task to draw. count <= 1 always returns 0.
 * A pinned carousel stays on origin until a tap or another swipe. */
int buddy_task_cursor_index(const buddy_task_cursor_t *cursor, int count, int64_t now_ms);

/** Moves the carousel to the next task and restarts the 3s dwell. */
void buddy_task_cursor_advance(buddy_task_cursor_t *cursor, int count, int64_t now_ms);

/** Stops the 3s rotation on the task now showing, or lets it run again. */
void buddy_task_cursor_toggle_pin(buddy_task_cursor_t *cursor, int count, int64_t now_ms);

/** A pin only applies while two or more tasks are showing. Dropping to one
 * task also restarts the dwell, so the next list does not jump ahead. */
void buddy_task_cursor_note_count(buddy_task_cursor_t *cursor, int count, int64_t now_ms);

bool buddy_task_cursor_pinned(const buddy_task_cursor_t *cursor);
