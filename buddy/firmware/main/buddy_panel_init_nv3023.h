/* NV3023A initialization table for the 240x240 single-key module.
 *
 * This is the module vendor's sequence (xingzhi-cube-0.85tft-ml307), kept
 * verbatim and verified on hardware together with the surrounding settings:
 * COLMOD 0x65 (RGB565), RGB element order, INVON and an explicit IDMOFF.
 * The table deliberately contains no MADCTL write: orientation belongs to
 * esp_lcd_panel_mirror/swap_xy in buddy_display.c so the two cannot disagree.
 */
#pragma once

#include "esp_lcd_nv3023.h"

static const nv3023_lcd_init_cmd_t s_nv3023_init_cmds[] = {
    {0xff, (const uint8_t[]){0xa5}, 1, 0},
    {0x3e, (const uint8_t[]){0x09}, 1, 0},
    {0x3a, (const uint8_t[]){0x65}, 1, 0},
    {0x82, (const uint8_t[]){0x00}, 1, 0},
    {0x98, (const uint8_t[]){0x00}, 1, 0},
    {0x63, (const uint8_t[]){0x0f}, 1, 0},
    {0x64, (const uint8_t[]){0x0f}, 1, 0},
    {0xb4, (const uint8_t[]){0x34}, 1, 0},
    {0xb5, (const uint8_t[]){0x30}, 1, 0},
    {0x83, (const uint8_t[]){0x03}, 1, 0},
    {0x86, (const uint8_t[]){0x04}, 1, 0},
    {0x87, (const uint8_t[]){0x16}, 1, 0},
    {0x88, (const uint8_t[]){0x0a}, 1, 0},
    {0x89, (const uint8_t[]){0x27}, 1, 0},
    {0x93, (const uint8_t[]){0x63}, 1, 0},
    {0x96, (const uint8_t[]){0x81}, 1, 0},
    {0xc3, (const uint8_t[]){0x10}, 1, 0},
    {0xe6, (const uint8_t[]){0x00}, 1, 0},
    {0x99, (const uint8_t[]){0x01}, 1, 0},
    {0x70, (const uint8_t[]){0x09}, 1, 0},
    {0x71, (const uint8_t[]){0x1d}, 1, 0},
    {0x72, (const uint8_t[]){0x14}, 1, 0},
    {0x73, (const uint8_t[]){0x0a}, 1, 0},
    {0x74, (const uint8_t[]){0x11}, 1, 0},
    {0x75, (const uint8_t[]){0x16}, 1, 0},
    {0x76, (const uint8_t[]){0x38}, 1, 0},
    {0x77, (const uint8_t[]){0x0b}, 1, 0},
    {0x78, (const uint8_t[]){0x08}, 1, 0},
    {0x79, (const uint8_t[]){0x3e}, 1, 0},
    {0x7a, (const uint8_t[]){0x07}, 1, 0},
    {0x7b, (const uint8_t[]){0x0d}, 1, 0},
    {0x7c, (const uint8_t[]){0x16}, 1, 0},
    {0x7d, (const uint8_t[]){0x0f}, 1, 0},
    {0x7e, (const uint8_t[]){0x14}, 1, 0},
    {0x7f, (const uint8_t[]){0x05}, 1, 0},
    {0xa0, (const uint8_t[]){0x04}, 1, 0},
    {0xa1, (const uint8_t[]){0x28}, 1, 0},
    {0xa2, (const uint8_t[]){0x0c}, 1, 0},
    {0xa3, (const uint8_t[]){0x11}, 1, 0},
    {0xa4, (const uint8_t[]){0x0b}, 1, 0},
    {0xa5, (const uint8_t[]){0x23}, 1, 0},
    {0xa6, (const uint8_t[]){0x45}, 1, 0},
    {0xa7, (const uint8_t[]){0x07}, 1, 0},
    {0xa8, (const uint8_t[]){0x0a}, 1, 0},
    {0xa9, (const uint8_t[]){0x3b}, 1, 0},
    {0xaa, (const uint8_t[]){0x0d}, 1, 0},
    {0xab, (const uint8_t[]){0x18}, 1, 0},
    {0xac, (const uint8_t[]){0x14}, 1, 0},
    {0xad, (const uint8_t[]){0x0f}, 1, 0},
    {0xae, (const uint8_t[]){0x19}, 1, 0},
    {0xaf, (const uint8_t[]){0x08}, 1, 0},
    {0xff, (const uint8_t[]){0x00}, 1, 0},
    {0x11, NULL, 0, 120},
    {0x29, NULL, 0, 10},
};
