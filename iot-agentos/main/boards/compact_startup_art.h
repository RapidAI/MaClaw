/*
 * Profile-private boot-art contract for compact displays.
 *
 * The common compact renderer owns LCD lifetime and safe stripe presentation.
 * A profile owns the immutable asset identity and may instead render a
 * composed product mark when its boot surface is not a full-frame bitmap.
 * This stays below Device/Platform APIs: application code never selects a
 * product splash or links a board-specific asset symbol.
 */
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef struct {
    const uint16_t *pixels;
    size_t bytes;
    int width;
    int height;
} compact_startup_full_frame_t;
