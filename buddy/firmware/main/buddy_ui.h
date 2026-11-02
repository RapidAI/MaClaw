#pragma once

#include <stdint.h>

#include "buddy_state.h"

enum {
    BUDDY_PAGE_TASKS = 0,
    BUDDY_PAGE_STANDBY = 1,
    BUDDY_PAGE_PROMPT = 2,
    BUDDY_PAGE_CELEBRATE = 3,
    BUDDY_PAGE_ATTENTION = 4,
};

void buddy_ui_init(void);

/** Current page implied by the view (tasks / standby / prompt). */
int buddy_ui_page(const buddy_view_t *view);
const char *buddy_ui_page_name(int page);

/** Renders the view and pushes changed rows. Call at 20 Hz.
 * A dark backlight skips the compose; the next wake forces a full redraw. */
void buddy_ui_render(const buddy_view_t *view, int64_t now_ms);

/** Short press on the glass: show the next in-progress task immediately. */
void buddy_ui_on_tap(void);

/** Horizontal swipe: stop the 3s rotation, or let it run again. */
void buddy_ui_on_swipe(void);

/** Index into the current-task list after the last tap or dwell. -1 when none. */
int buddy_ui_active_index(void);
