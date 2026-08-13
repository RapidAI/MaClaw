#pragma once

/*
 * Private circular visual-profile boundary.
 *
 * The common circular renderer consumes aperture geometry, scene-safe layout
 * and glyph rasters through this neutral contract.  It never selects a board
 * nor includes a panel/codec/input profile.  A circular product adds its
 * visual adapters behind round_visual_profile_service.c; electrical details
 * continue to belong to the display, audio and input HAL services.
 */

#include <stdbool.h>
#include <stdint.h>

#include "boards/round_alarm_layout.h"
#include "boards/round_display_layout.h"
#include "boards/round_message_layout.h"
#include "boards/round_qrcode_layout.h"
#include "boards/round_recording_layout.h"
#include "boards/round_response_image_layout.h"
#include "boards/round_upload_layout.h"

#define ROUND_VISUAL_PROFILE_CJK24_CELL 24u
#define ROUND_VISUAL_PROFILE_CJK24_BYTES 72u
/* Current round visual profiles expose at most a 32x32 native CJK raster. */
#define ROUND_VISUAL_PROFILE_MAX_PACKED_GLYPH_BYTES 128u

typedef enum {
    ROUND_VISUAL_GLYPH_NONE,
    ROUND_VISUAL_GLYPH_NATIVE,
    ROUND_VISUAL_GLYPH_PACKED,
    ROUND_VISUAL_GLYPH_DYNAMIC24,
} round_visual_glyph_source_t;

typedef struct {
    const uint32_t *rows;
    const uint8_t *bitmap;
    uint8_t fallback[ROUND_VISUAL_PROFILE_MAX_PACKED_GLYPH_BYTES];
    uint8_t cell;
    round_visual_glyph_source_t source;
} round_visual_curve_glyph_t;

const round_display_layout_t *round_visual_profile_display_layout(void);
const round_recording_layout_t *round_visual_profile_recording_layout(void);
const round_upload_layout_t *round_visual_profile_upload_layout(void);
const round_alarm_layout_t *round_visual_profile_alarm_layout(void);
const round_message_layout_t *round_visual_profile_message_layout(void);
const round_response_image_layout_t *round_visual_profile_response_image_layout(void);
const round_qrcode_layout_t *round_visual_profile_qrcode_layout(void);

const uint32_t *round_visual_profile_cjk24_rows(uint32_t codepoint);
unsigned round_visual_profile_curve_glyph_cell(void);
bool round_visual_profile_copy_cjk24(uint32_t codepoint,
                                     uint8_t bitmap[ROUND_VISUAL_PROFILE_CJK24_BYTES]);
void round_visual_profile_prepare_curve_glyph(uint32_t codepoint,
                                              const uint8_t *dynamic24,
                                              round_visual_curve_glyph_t *out);
bool round_visual_profile_curve_glyph_pixel(const round_visual_curve_glyph_t *glyph,
                                            int row, int col, bool ascii_pixel);
