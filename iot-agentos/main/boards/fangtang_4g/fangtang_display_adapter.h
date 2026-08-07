/*
 * Fangtang-4G display hardware profile.
 *
 * This file owns the physical panel contract only: pins, viewport/GRAM
 * placement and the vendor controller initialization sequence.  Shared scene
 * state and Device API rendering remain outside this profile so another
 * board can supply an equivalent display adapter without copying business
 * behaviour.
 */
#pragma once

#include "sdkconfig.h"

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
#error "Fangtang display adapter may only be included by the Fangtang profile"
#endif

#include "driver/gpio.h"
#include "driver/spi_master.h"
#include "esp_check.h"
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"
#include "esp_lcd_nv3023.h"

#define FANGTANG_DISPLAY_WIDTH 240
#define FANGTANG_DISPLAY_HEIGHT 240
/* The visible square is located at rows 80..319 of the NV3023 GRAM. */
#define FANGTANG_DISPLAY_GRAM_Y_OFFSET 80

#define FANGTANG_DISPLAY_MOSI GPIO_NUM_10
#define FANGTANG_DISPLAY_CLK GPIO_NUM_9
#define FANGTANG_DISPLAY_DC GPIO_NUM_8
#define FANGTANG_DISPLAY_RESET GPIO_NUM_18
#define FANGTANG_DISPLAY_CS GPIO_NUM_14
#define FANGTANG_DISPLAY_BACKLIGHT GPIO_NUM_13

/*
 * The production module's controller sequence.  The renderer supplies
 * canonical RGB565 buffers; COLMOD, MADCTL/polarity and IDMOFF are therefore
 * physical-panel responsibilities and must not be guessed by business/UI
 * code.
 */
static const nv3023_lcd_init_cmd_t s_fangtang_nv3023_init_cmds[] = {
    {0xff, (const uint8_t[]){0xa5}, 1, 0}, {0x3e, (const uint8_t[]){0x09}, 1, 0},
    {0x3a, (const uint8_t[]){0x65}, 1, 0}, {0x82, (const uint8_t[]){0x00}, 1, 0},
    {0x98, (const uint8_t[]){0x00}, 1, 0}, {0x63, (const uint8_t[]){0x0f}, 1, 0},
    {0x64, (const uint8_t[]){0x0f}, 1, 0}, {0xb4, (const uint8_t[]){0x34}, 1, 0},
    {0xb5, (const uint8_t[]){0x30}, 1, 0}, {0x83, (const uint8_t[]){0x03}, 1, 0},
    {0x86, (const uint8_t[]){0x04}, 1, 0}, {0x87, (const uint8_t[]){0x16}, 1, 0},
    {0x88, (const uint8_t[]){0x0a}, 1, 0}, {0x89, (const uint8_t[]){0x27}, 1, 0},
    {0x93, (const uint8_t[]){0x63}, 1, 0}, {0x96, (const uint8_t[]){0x81}, 1, 0},
    {0xc3, (const uint8_t[]){0x10}, 1, 0}, {0xe6, (const uint8_t[]){0x00}, 1, 0},
    {0x99, (const uint8_t[]){0x01}, 1, 0}, {0x70, (const uint8_t[]){0x09}, 1, 0},
    {0x71, (const uint8_t[]){0x1d}, 1, 0}, {0x72, (const uint8_t[]){0x14}, 1, 0},
    {0x73, (const uint8_t[]){0x0a}, 1, 0}, {0x74, (const uint8_t[]){0x11}, 1, 0},
    {0x75, (const uint8_t[]){0x16}, 1, 0}, {0x76, (const uint8_t[]){0x38}, 1, 0},
    {0x77, (const uint8_t[]){0x0b}, 1, 0}, {0x78, (const uint8_t[]){0x08}, 1, 0},
    {0x79, (const uint8_t[]){0x3e}, 1, 0}, {0x7a, (const uint8_t[]){0x07}, 1, 0},
    {0x7b, (const uint8_t[]){0x0d}, 1, 0}, {0x7c, (const uint8_t[]){0x16}, 1, 0},
    {0x7d, (const uint8_t[]){0x0f}, 1, 0}, {0x7e, (const uint8_t[]){0x14}, 1, 0},
    {0x7f, (const uint8_t[]){0x05}, 1, 0}, {0xa0, (const uint8_t[]){0x04}, 1, 0},
    {0xa1, (const uint8_t[]){0x28}, 1, 0}, {0xa2, (const uint8_t[]){0x0c}, 1, 0},
    {0xa3, (const uint8_t[]){0x11}, 1, 0}, {0xa4, (const uint8_t[]){0x0b}, 1, 0},
    {0xa5, (const uint8_t[]){0x23}, 1, 0}, {0xa6, (const uint8_t[]){0x45}, 1, 0},
    {0xa7, (const uint8_t[]){0x07}, 1, 0}, {0xa8, (const uint8_t[]){0x0a}, 1, 0},
    {0xa9, (const uint8_t[]){0x3b}, 1, 0}, {0xaa, (const uint8_t[]){0x0d}, 1, 0},
    {0xab, (const uint8_t[]){0x18}, 1, 0}, {0xac, (const uint8_t[]){0x14}, 1, 0},
    {0xad, (const uint8_t[]){0x0f}, 1, 0}, {0xae, (const uint8_t[]){0x19}, 1, 0},
    {0xaf, (const uint8_t[]){0x08}, 1, 0}, {0xff, (const uint8_t[]){0x00}, 1, 0},
    {0x11, NULL, 0, 120}, {0x29, NULL, 0, 10},
};

/* Hardware-only composition root for the NV3023 path.  It deliberately does
 * not allocate frame buffers or decide scenes; those are Display Service
 * concerns owned by the shared renderer. */
static inline esp_err_t fangtang_display_init_hardware(
    spi_host_device_t host, esp_lcd_panel_io_color_trans_done_cb_t on_transfer_done,
    void *transfer_context, esp_lcd_panel_handle_t *out_panel,
    esp_lcd_panel_io_handle_t *out_io) {
    if (!out_panel || !out_io) return ESP_ERR_INVALID_ARG;
    *out_panel = NULL;
    *out_io = NULL;

    gpio_config_t backlight = {
        .pin_bit_mask = 1ULL << FANGTANG_DISPLAY_BACKLIGHT,
        .mode = GPIO_MODE_OUTPUT,
        .pull_up_en = GPIO_PULLUP_DISABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    ESP_RETURN_ON_ERROR(gpio_config(&backlight), "fangtang_display", "backlight GPIO");
    ESP_RETURN_ON_ERROR(gpio_set_level(FANGTANG_DISPLAY_BACKLIGHT, 0),
                        "fangtang_display", "backlight off");

    spi_bus_config_t bus = {
        .mosi_io_num = FANGTANG_DISPLAY_MOSI,
        .miso_io_num = GPIO_NUM_NC,
        .sclk_io_num = FANGTANG_DISPLAY_CLK,
        .quadwp_io_num = GPIO_NUM_NC,
        .quadhd_io_num = GPIO_NUM_NC,
        .max_transfer_sz = FANGTANG_DISPLAY_WIDTH * sizeof(uint16_t),
    };
    ESP_RETURN_ON_ERROR(spi_bus_initialize(host, &bus, SPI_DMA_CH_AUTO),
                        "fangtang_display", "SPI bus");

    esp_lcd_panel_io_spi_config_t io_cfg = {
        .cs_gpio_num = FANGTANG_DISPLAY_CS,
        .dc_gpio_num = FANGTANG_DISPLAY_DC,
        .spi_mode = 0,
        .pclk_hz = 40 * 1000 * 1000,
        .trans_queue_depth = 10,
        .lcd_cmd_bits = 8,
        .lcd_param_bits = 8,
        .on_color_trans_done = on_transfer_done,
        .user_ctx = transfer_context,
    };
    esp_lcd_panel_io_handle_t io = NULL;
    esp_err_t err = esp_lcd_new_panel_io_spi(host, &io_cfg, &io);
    if (err != ESP_OK) return err;

    nv3023_vendor_config_t vendor_cfg = {
        .init_cmds = s_fangtang_nv3023_init_cmds,
        .init_cmds_size = sizeof(s_fangtang_nv3023_init_cmds) /
                          sizeof(s_fangtang_nv3023_init_cmds[0]),
    };
    esp_lcd_panel_dev_config_t panel_cfg = {
        .reset_gpio_num = FANGTANG_DISPLAY_RESET,
        .rgb_ele_order = LCD_RGB_ELEMENT_ORDER_RGB,
        .bits_per_pixel = 16,
        .vendor_config = &vendor_cfg,
    };
    esp_lcd_panel_handle_t panel = NULL;
    err = esp_lcd_new_panel_nv3023(io, &panel_cfg, &panel);
    if (err != ESP_OK) return err;
    err = esp_lcd_panel_reset(panel);
    if (err != ESP_OK) return err;
    err = esp_lcd_panel_init(panel);
    if (err != ESP_OK) return err;
    err = esp_lcd_panel_swap_xy(panel, false);
    if (err != ESP_OK) return err;
    err = esp_lcd_panel_mirror(panel, true, true);
    if (err != ESP_OK) return err;
    err = esp_lcd_panel_invert_color(panel, true);
    if (err != ESP_OK) return err;
    /* NV3023A starts in its reduced idle colour mode. */
    err = esp_lcd_panel_io_tx_param(io, 0x38, NULL, 0);
    if (err != ESP_OK) return err;
    err = esp_lcd_panel_disp_on_off(panel, true);
    if (err != ESP_OK) return err;
    err = gpio_set_level(FANGTANG_DISPLAY_BACKLIGHT, 1);
    if (err != ESP_OK) return err;

    *out_panel = panel;
    *out_io = io;
    return ESP_OK;
}

static inline esp_err_t fangtang_display_set_backlight(bool enabled) {
    return gpio_set_level(FANGTANG_DISPLAY_BACKLIGHT, enabled ? 1 : 0);
}

/*
 * The NV3023 module exposes its visible 240x240 viewport at a non-zero GRAM
 * row.  Keeping that addressing detail here ensures the shared renderer only
 * submits a normal viewport rectangle; it neither knows the controller
 * command sequence nor the module-specific GRAM origin.
 *
 * Transfers are deliberately row-wise.  The production panel has proved more
 * reliable with a bounded SPI transaction than with a large multi-row DMA
 * transfer, and the caller serializes display access through its renderer
 * mutex.
 */
static inline esp_err_t fangtang_display_draw_bitmap_rows(
    esp_lcd_panel_io_handle_t io, int x0, int y0, int x1, int y1,
    const uint16_t *pixels) {
    if (!io || !pixels || x0 < 0 || y0 < 0 ||
        x1 > FANGTANG_DISPLAY_WIDTH || y1 > FANGTANG_DISPLAY_HEIGHT ||
        x1 <= x0 || y1 <= y0) {
        return ESP_ERR_INVALID_ARG;
    }

    const uint8_t columns[] = {
        (uint8_t)(x0 >> 8), (uint8_t)x0,
        (uint8_t)((x1 - 1) >> 8), (uint8_t)(x1 - 1),
    };
    const int width = x1 - x0;
    for (int y = y0; y < y1; ++y) {
        const int gram_y = FANGTANG_DISPLAY_GRAM_Y_OFFSET + y;
        const uint8_t rows[] = {
            (uint8_t)(gram_y >> 8), (uint8_t)gram_y,
            (uint8_t)(gram_y >> 8), (uint8_t)gram_y,
        };
        esp_err_t err = esp_lcd_panel_io_tx_param(io, 0x2a,
                                                   columns, sizeof(columns));
        if (err != ESP_OK) return err;
        err = esp_lcd_panel_io_tx_param(io, 0x2b, rows, sizeof(rows));
        if (err != ESP_OK) return err;
        err = esp_lcd_panel_io_tx_color(
            io, 0x2c, pixels + (size_t)(y - y0) * width,
            (size_t)width * sizeof(uint16_t));
        if (err != ESP_OK) return err;
    }
    return ESP_OK;
}
