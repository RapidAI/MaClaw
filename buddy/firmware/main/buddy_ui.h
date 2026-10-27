#pragma once

#include <stdint.h>

#include "buddy_state.h"

enum {
    BUDDY_PAGE_TASKS = 0,
    BUDDY_PAGE_STANDBY = 1,
    BUDDY_PAGE_PROMPT = 2,
};

void buddy_ui_init(void);

/** Current page implied by the view (tasks / standby / prompt). */
int buddy_ui_page(const buddy_view_t *view);
const char *buddy_ui_page_name(int page);

/** Renders the view and pushes changed rows. Call at 10 Hz. */
void buddy_ui_render(const buddy_view_t *view, int64_t now_ms);
