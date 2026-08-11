#pragma once
/*
 * Private compact-display animation contract.
 *
 * The shared renderer owns animation admission, cadence, scene state, panel
 * submission and front-frame repair. A profile may only describe and compose
 * a bounded physical patch into renderer-owned DMA staging memory.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef struct {
    int left;
    int top;
    int width;
    int height;
} compact_display_animation_patch_t;
