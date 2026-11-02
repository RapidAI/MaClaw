#include "buddy_task_cursor.h"

int buddy_task_cursor_index(const buddy_task_cursor_t *cursor, int count, int64_t now_ms) {
    if (count <= 1) return 0;
    if (cursor && cursor->pinned) {
        int origin = cursor->origin % count;
        if (origin < 0) origin += count;
        return origin;
    }
    int origin = 0;
    int64_t origin_ms = 0;
    if (cursor) {
        origin = cursor->origin;
        origin_ms = cursor->origin_ms;
    }
    if (origin < 0) origin = 0;
    int64_t elapsed = now_ms - origin_ms;
    if (elapsed < 0) elapsed = 0;
    /* Steps stay in 64 bits. Casting to int wraps after ~25 days and the
     * carousel jumps to the wrong task. */
    const int64_t steps = elapsed / 3000;
    int64_t index = (origin + steps) % (int64_t)count;
    if (index < 0) index += count;
    return (int)index;
}

void buddy_task_cursor_advance(buddy_task_cursor_t *cursor, int count, int64_t now_ms) {
    if (!cursor || count <= 1) return;
    const int current = buddy_task_cursor_index(cursor, count, now_ms);
    cursor->origin = (current + 1) % count;
    cursor->origin_ms = now_ms;
}

void buddy_task_cursor_toggle_pin(buddy_task_cursor_t *cursor, int count, int64_t now_ms) {
    if (!cursor || count <= 1) return;
    cursor->origin = buddy_task_cursor_index(cursor, count, now_ms);
    cursor->origin_ms = now_ms;
    cursor->pinned = !cursor->pinned;
}

void buddy_task_cursor_note_count(buddy_task_cursor_t *cursor, int count, int64_t now_ms) {
    if (!cursor || count > 1) return;
    cursor->pinned = false;
    cursor->origin = 0;
    cursor->origin_ms = now_ms;
}

bool buddy_task_cursor_pinned(const buddy_task_cursor_t *cursor) {
    return cursor && cursor->pinned;
}
