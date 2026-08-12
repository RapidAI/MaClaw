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
#include "provisioning_failure_injection.h"
#include "boards/compact_display_animation.h"
#include "boards/compact_startup_art.h"

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
#error "Fangtang display adapter may only be included by the Fangtang profile"
#endif

#include "driver/gpio.h"
#include "driver/ledc.h"
#include "driver/spi_master.h"
#include "esp_heap_caps.h"
#include "esp_check.h"
#include "esp_log.h"
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"
#include "esp_lcd_nv3023.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include <stddef.h>
#include <stdint.h>

#define FANGTANG_DISPLAY_HOST SPI3_HOST
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

/* The backlight is wired to an LEDC-capable GPIO.  PWM dimming carries the
 * shared remote-brightness contract; this module was designed for plain
 * on/off drive, so the low-end dimming linearity still needs on-device
 * verification.  Bread Compact independently uses the same timer/channel:
 * the two board profiles are compile-time exclusive and never collide. */
#define FANGTANG_BACKLIGHT_LEDC_TIMER LEDC_TIMER_0
#define FANGTANG_BACKLIGHT_LEDC_CHANNEL LEDC_CHANNEL_0
#define FANGTANG_BACKLIGHT_LEDC_RESOLUTION LEDC_TIMER_10_BIT
#define FANGTANG_DISPLAY_DEFAULT_BRIGHTNESS 100u

/* Fangtang's boot mark is alpha-composed over its profile palette instead of
 * being a full-screen bitmap.  Its transition unit supplies that composition
 * through compact_profile_render_startup_art(); there is intentionally no
 * generic full-frame asset to expose here. */
static inline compact_startup_full_frame_t
compact_display_adapter_startup_full_frame(void) {
    return (compact_startup_full_frame_t){0};
}

/* Geometry and default brightness describe this physical panel.  Shared scene
 * code consumes only these normalized values and never selects a board model. */
static inline int compact_display_adapter_width(void) { return FANGTANG_DISPLAY_WIDTH; }
static inline int compact_display_adapter_height(void) { return FANGTANG_DISPLAY_HEIGHT; }
static inline unsigned compact_display_adapter_default_brightness(void) {
    return FANGTANG_DISPLAY_DEFAULT_BRIGHTNESS;
}

/* NV3023 presentation is verified as a full visible-frame transaction.  Its
 * GRAM offset and row transfer mechanics remain private below this adapter. */
static inline bool compact_display_adapter_uses_delta_presentation(void) {
    return false;
}

/* NV3023 has a bounded physical activity strip. The profile owns only this
 * geometry/raster; shared code owns cadence, scene admission and submission. */
static inline bool compact_display_adapter_uses_profile_thinking_patch(void) {
    return true;
}

static inline bool compact_display_adapter_compose_thinking_patch(
    uint16_t *pixels, size_t pixel_capacity, unsigned phase, uint16_t background,
    compact_display_animation_patch_t *out_patch) {
    const int width = 45;
    const int height = 11;
    if (!pixels || !out_patch || pixel_capacity < (size_t)width * height) return false;
    const uint16_t active = (uint16_t)(((196u >> 3) << 11) |
                                       ((169u >> 2) << 5) | (255u >> 3));
    for (int row = 0; row < height; ++row) {
        for (int x = 0; x < width; ++x) pixels[(size_t)row * width + x] = background;
        for (int dot = 0; dot < 3; ++dot) {
            const int radius = dot == (int)(phase % 3u) ? 4 : 2;
            const int center_x = 22 + (dot - 1) * 15;
            const int dy = row - 5;
            for (int dx = -4; dx <= 4; ++dx) {
                const int px = center_x + dx;
                if (px >= 0 && px < width && dx * dx + dy * dy <= radius * radius) {
                    pixels[(size_t)row * width + px] = active;
                }
            }
        }
    }
    *out_patch = (compact_display_animation_patch_t){
        .left = FANGTANG_DISPLAY_WIDTH / 2 - 22,
        .top = 145,
        .width = width,
        .height = height,
    };
    return true;
}

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

/* Applies a normalized 0..100 brightness level to the backlight PWM.  0 turns
 * the backlight fully off while the rest of the system keeps running. */
static inline esp_err_t fangtang_display_set_backlight(unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    const uint32_t duty =
        percent * ((1u << FANGTANG_BACKLIGHT_LEDC_RESOLUTION) - 1u) / 100u;
    esp_err_t err = ledc_set_duty(LEDC_LOW_SPEED_MODE,
                                  FANGTANG_BACKLIGHT_LEDC_CHANNEL, duty);
    if (err == ESP_OK) {
        err = ledc_update_duty(LEDC_LOW_SPEED_MODE, FANGTANG_BACKLIGHT_LEDC_CHANNEL);
    }
    return err;
}

static inline esp_err_t compact_display_adapter_set_brightness(unsigned percent) {
    return fangtang_display_set_backlight(percent);
}

/* Keep physical panel/IO ownership below the profile seam.  Shared compact
 * rendering gets only ordinary pixel submission and normalized power results,
 * never a controller handle or ESP-LCD callback signature. */
static esp_lcd_panel_handle_t s_fangtang_display_panel;
static esp_lcd_panel_io_handle_t s_fangtang_display_io;
/* If a renderer fence times out, the controller may still own the source.
 * Keep this profile-private state so power commands first drain DMA. */
static volatile bool s_fangtang_display_transfer_pending;
static SemaphoreHandle_t s_fangtang_display_transfer_done;
static bool s_fangtang_display_spi_initialized;
static bool s_fangtang_display_backlight_initialized;
static bool s_fangtang_display_backlight_timer_configured;

/* A successful panel remains boot-lifetime. This reverse-order cleanup is
 * only for a failed construction, so board-start rollback cannot leave an
 * owned SPI/panel object or visibly-on backlight behind. */
/* Profile-private acquisitions have a shared test numbering only so the
 * rollout matrix can assert cleanup after each owned resource boundary. */
static bool display_init_fault_after_stage(unsigned completed_stage,
                                           esp_err_t *out_err) {
    if (!provisioning_failure_injection_display_initialization_should_fail_after(
            completed_stage)) {
        return false;
    }
    ESP_LOGW("display_adapter", "display init fault injection after stage %u",
             completed_stage);
    if (out_err) *out_err = ESP_FAIL;
    return true;
}
/* All adapter steps share a test-only acquisition ordinal.  It is deliberately
 * local to this source boundary and is never surfaced in Device/Platform API. */
#define DISPLAY_INIT_FAULT_POINT(stage) \
    do { if (display_init_fault_after_stage((stage), &err)) goto fail; } while (0)
static void compact_display_adapter_rollback_partial_init(void) {
    if (s_fangtang_display_panel) {
        (void)esp_lcd_panel_del(s_fangtang_display_panel);
        s_fangtang_display_panel = NULL;
    }
    if (s_fangtang_display_io) {
        (void)esp_lcd_panel_io_del(s_fangtang_display_io);
        s_fangtang_display_io = NULL;
    }
    if (s_fangtang_display_spi_initialized) {
        (void)spi_bus_free(FANGTANG_DISPLAY_HOST);
        s_fangtang_display_spi_initialized = false;
    }
    if (s_fangtang_display_backlight_initialized) {
        (void)ledc_stop(LEDC_LOW_SPEED_MODE,
                        FANGTANG_BACKLIGHT_LEDC_CHANNEL, 0);
        s_fangtang_display_backlight_initialized = false;
    }
    if (s_fangtang_display_backlight_timer_configured) {
        (void)ledc_timer_rst(LEDC_LOW_SPEED_MODE,
                             FANGTANG_BACKLIGHT_LEDC_TIMER);
        s_fangtang_display_backlight_timer_configured = false;
    }
    s_fangtang_display_transfer_pending = false;
    s_fangtang_display_transfer_done = NULL;
}

static bool compact_display_adapter_color_transfer_done(
    esp_lcd_panel_io_handle_t io, esp_lcd_panel_io_event_data_t *event,
    void *user_ctx) {
    (void)io;
    (void)event;
    BaseType_t task_woken = pdFALSE;
    xSemaphoreGiveFromISR((SemaphoreHandle_t)user_ctx, &task_woken);
    /* ISR completion token first: after a timeout, the next transfer must
     * not mistake a delayed row completion for its own fence. */
    s_fangtang_display_transfer_pending = false;
    return task_woken == pdTRUE;
}

static inline esp_err_t compact_display_adapter_wait_for_transfer_idle(void) {
    if (!s_fangtang_display_transfer_pending) return ESP_OK;
    if (!s_fangtang_display_transfer_done) return ESP_ERR_INVALID_STATE;
    if (xSemaphoreTake(s_fangtang_display_transfer_done, pdMS_TO_TICKS(1000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    return s_fangtang_display_transfer_pending ? ESP_ERR_INVALID_STATE : ESP_OK;
}

static inline bool compact_display_adapter_ready(void) {
    return s_fangtang_display_panel != NULL;
}

/* DISPLAY_OFF changes only this panel and its backlight.  It deliberately
 * does not power down the modem, ADC, or MCU; those require a later Power/Wake
 * transaction with verified board-specific sequencing. */
static inline esp_err_t compact_display_enter_display_off(void) {
    if (!s_fangtang_display_panel) return ESP_ERR_INVALID_STATE;
    ESP_RETURN_ON_ERROR(compact_display_adapter_wait_for_transfer_idle(),
                        "fangtang_display", "pending transfer before display off");
    /* GPIO13 PWM is the physical light source.  Make it the required part of
     * DISPLAY_OFF so a transient NV3023 SPI-controller failure cannot leave
     * the screen visibly on even though the shared idle deadline expired. */
    esp_err_t err = fangtang_display_set_backlight(0);
    if (err != ESP_OK) return err;
    err = esp_lcd_panel_disp_on_off(s_fangtang_display_panel, false);
    if (err != ESP_OK) {
        ESP_LOGW("fangtang_display",
                 "controller display-off deferred (%s); backlight is off",
                 esp_err_to_name(err));
    }
    return ESP_OK;
}

static inline esp_err_t compact_display_wake_from_display_off(unsigned brightness) {
    if (!s_fangtang_display_panel || brightness > 100) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(compact_display_adapter_wait_for_transfer_idle(),
                        "fangtang_display", "pending transfer before display wake");
    esp_err_t err = esp_lcd_panel_disp_on_off(s_fangtang_display_panel, true);
    if (err != ESP_OK) return err;
    return fangtang_display_set_backlight(brightness);
}

/* Hardware-only composition root for the NV3023 path.  It deliberately does
 * not allocate frame buffers or decide scenes; those are Display Service
 * concerns owned by the shared renderer. */
static inline esp_err_t compact_display_adapter_init_hardware(
    SemaphoreHandle_t transfer_done) {
    if (!transfer_done) return ESP_ERR_INVALID_ARG;
    if (s_fangtang_display_panel || s_fangtang_display_io ||
        s_fangtang_display_spi_initialized || s_fangtang_display_backlight_initialized ||
        s_fangtang_display_backlight_timer_configured) {
        return ESP_ERR_INVALID_STATE;
    }
    s_fangtang_display_transfer_done = transfer_done;

    ledc_timer_config_t backlight_timer = {
        .speed_mode = LEDC_LOW_SPEED_MODE,
        .duty_resolution = FANGTANG_BACKLIGHT_LEDC_RESOLUTION,
        .timer_num = FANGTANG_BACKLIGHT_LEDC_TIMER,
        .freq_hz = 5000,
        .clk_cfg = LEDC_AUTO_CLK,
    };
    esp_err_t err = ledc_timer_config(&backlight_timer);
    if (err != ESP_OK) return err;
    s_fangtang_display_backlight_timer_configured = true;

    ledc_channel_config_t backlight_channel = {
        .gpio_num = FANGTANG_DISPLAY_BACKLIGHT,
        .speed_mode = LEDC_LOW_SPEED_MODE,
        .channel = FANGTANG_BACKLIGHT_LEDC_CHANNEL,
        .intr_type = LEDC_INTR_DISABLE,
        .timer_sel = FANGTANG_BACKLIGHT_LEDC_TIMER,
        .duty = 0,
        .hpoint = 0,
    };
    err = ledc_channel_config(&backlight_channel);
    if (err != ESP_OK) goto fail;
    s_fangtang_display_backlight_initialized = true;
    DISPLAY_INIT_FAULT_POINT(1);

    spi_bus_config_t bus = {
        .mosi_io_num = FANGTANG_DISPLAY_MOSI,
        .miso_io_num = GPIO_NUM_NC,
        .sclk_io_num = FANGTANG_DISPLAY_CLK,
        .quadwp_io_num = GPIO_NUM_NC,
        .quadhd_io_num = GPIO_NUM_NC,
        .max_transfer_sz = FANGTANG_DISPLAY_WIDTH * sizeof(uint16_t),
    };
    err = spi_bus_initialize(FANGTANG_DISPLAY_HOST, &bus, SPI_DMA_CH_AUTO);
    if (err != ESP_OK) goto fail;
    s_fangtang_display_spi_initialized = true;
    DISPLAY_INIT_FAULT_POINT(2);

    esp_lcd_panel_io_spi_config_t io_cfg = {
        .cs_gpio_num = FANGTANG_DISPLAY_CS,
        .dc_gpio_num = FANGTANG_DISPLAY_DC,
        .spi_mode = 0,
        .pclk_hz = 40 * 1000 * 1000,
        .trans_queue_depth = 10,
        .lcd_cmd_bits = 8,
        .lcd_param_bits = 8,
        .on_color_trans_done = compact_display_adapter_color_transfer_done,
        .user_ctx = transfer_done,
    };
    err = esp_lcd_new_panel_io_spi(FANGTANG_DISPLAY_HOST, &io_cfg,
                                   &s_fangtang_display_io);
    if (err != ESP_OK) goto fail;
    DISPLAY_INIT_FAULT_POINT(3);

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
    err = esp_lcd_new_panel_nv3023(s_fangtang_display_io, &panel_cfg,
                                   &s_fangtang_display_panel);
    if (err != ESP_OK) goto fail;
    DISPLAY_INIT_FAULT_POINT(4);
    err = esp_lcd_panel_reset(s_fangtang_display_panel);
    if (err != ESP_OK) goto fail;
    err = esp_lcd_panel_init(s_fangtang_display_panel);
    if (err != ESP_OK) goto fail;
    err = esp_lcd_panel_swap_xy(s_fangtang_display_panel, false);
    if (err != ESP_OK) goto fail;
    err = esp_lcd_panel_mirror(s_fangtang_display_panel, true, true);
    if (err != ESP_OK) goto fail;
    err = esp_lcd_panel_invert_color(s_fangtang_display_panel, true);
    if (err != ESP_OK) goto fail;
    /* NV3023A starts in its reduced idle colour mode. */
    err = esp_lcd_panel_io_tx_param(s_fangtang_display_io, 0x38, NULL, 0);
    if (err != ESP_OK) goto fail;
    err = esp_lcd_panel_disp_on_off(s_fangtang_display_panel, true);
    if (err != ESP_OK) goto fail;
    err = fangtang_display_set_backlight(100);
    if (err != ESP_OK) goto fail;

    DISPLAY_INIT_FAULT_POINT(5);

    return ESP_OK;
fail:
    compact_display_adapter_rollback_partial_init();
    return err;
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
    esp_lcd_panel_io_handle_t io, SemaphoreHandle_t transfer_done,
    int x0, int y0, int x1, int y1,
    const uint16_t *pixels) {
    if (!io || !transfer_done || !pixels || x0 < 0 || y0 < 0 ||
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
        s_fangtang_display_transfer_pending = true;
        err = esp_lcd_panel_io_tx_color(
            io, 0x2c, pixels + (size_t)(y - y0) * width,
            (size_t)width * sizeof(uint16_t));
        if (err != ESP_OK) {
            s_fangtang_display_transfer_pending = false;
            return err;
        }
        /* This is a binary completion semaphore.  Await each bounded row
         * before submitting the next one so callbacks cannot coalesce and a
         * renderer-owned framebuffer is never reused while SPI DMA reads it. */
        if (xSemaphoreTake(transfer_done, pdMS_TO_TICKS(1000)) != pdTRUE) {
            return ESP_ERR_TIMEOUT;
        }
    }
    return ESP_OK;
}

static inline esp_err_t compact_display_adapter_draw_bitmap_sync(
    SemaphoreHandle_t transfer_done, int x0, int y0, int x1, int y1,
    const void *pixels) {
    if (!s_fangtang_display_io || !transfer_done || !pixels) {
        return ESP_ERR_INVALID_ARG;
    }
    /* Do not overwrite the pending-row state after a timed-out fence.  The
     * retained source remains controller-owned until this adapter observes
     * its own completion, even though the shared renderer has moved on. */
    ESP_RETURN_ON_ERROR(compact_display_adapter_wait_for_transfer_idle(),
                        "fangtang_display", "previous transfer still pending");
    /* The shared renderer may reuse a framebuffer as soon as this call
     * returns.  Drain a stale completion token before the row-by-row writer
     * waits for every physical color transfer. */
    while (xSemaphoreTake(transfer_done, 0) == pdTRUE) {}
    esp_err_t err = fangtang_display_draw_bitmap_rows(
        s_fangtang_display_io, transfer_done,
        x0, y0, x1, y1, (const uint16_t *)pixels);
    return err;
}

/* NV3023's row transfer needs internal DMA memory, while the retained scene
 * frames live in PSRAM.  Expose only allocation roles to the shared renderer,
 * never ESP-IDF capability bits or panel-specific placement rules. */
static inline uint16_t *compact_display_adapter_alloc_framebuffer(size_t bytes) {
    return heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
}

static inline uint16_t *compact_display_adapter_alloc_transfer_buffer(size_t bytes) {
    return heap_caps_malloc(bytes, MALLOC_CAP_DMA | MALLOC_CAP_INTERNAL);
}

static inline void compact_display_adapter_free_buffer(void *buffer) {
    heap_caps_free(buffer);
}

/* Generated display bitmaps are either CPU-composed into a retained PSRAM
 * frame or submitted immediately through NV3023's SPI-DMA transfer path.
 * Keep that physical-memory choice profile-private: common scene code asks
 * only for the role it is about to perform. */
static inline uint16_t *compact_display_adapter_allocate_temporary_composition_bitmap(
    size_t bytes) {
    if (!bytes) return NULL;
    uint16_t *bitmap = heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    return bitmap ? bitmap : compact_display_adapter_alloc_transfer_buffer(bytes);
}

static inline uint16_t *compact_display_adapter_allocate_temporary_transfer_bitmap(
    size_t bytes) {
    if (!bytes) return NULL;
    return compact_display_adapter_alloc_transfer_buffer(bytes);
}

static inline void compact_display_adapter_free_temporary_bitmap(void *bitmap) {
    if (bitmap) heap_caps_free(bitmap);
}

/* Remote pet frames remain resident while the compact standby scene is
 * active.  Their placement is a display-profile decision: retain Fangtang's
 * PSRAM-first policy here and keep the common renderer independent of
 * allocator capabilities.  `heap_caps_free` is the matching ESP-IDF release
 * operation for both the capability allocator and the libc fallback. */
static inline uint8_t *compact_display_adapter_allocate_remote_pet_frame(size_t bytes) {
    if (!bytes) return NULL;
    uint8_t *frame = heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    return frame ? frame : malloc(bytes);
}

static inline void compact_display_adapter_free_remote_pet_frame(void *frame) {
    if (frame) heap_caps_free(frame);
}

/* Source pet frames are created by the ESP-IDF media/download path. Shared
 * rendering transfers only ownership; the profile matches the allocator.
 */
static inline void compact_display_adapter_release_consumed_pet_source(void *frame) {
    if (frame) heap_caps_free(frame);
}

/* Decorative renderer workers retain admission, stop/join and scene policy in
 * the shared renderer. This board profile owns their runtime footprint only. */
static inline BaseType_t compact_display_adapter_start_thinking_animation_task(
    TaskFunction_t entry, TaskHandle_t *out_task) {
    if (!entry || !out_task) return pdFAIL;
    return xTaskCreate(entry, "maclaw_fangtang_thinking", 3072, NULL, 2, out_task);
}

static inline BaseType_t compact_display_adapter_start_pet_animation_task(
    TaskFunction_t entry, TaskHandle_t *out_task) {
    if (!entry || !out_task) return pdFAIL;
    return xTaskCreate(entry, "maclaw_fangtang_pet", 3072, NULL, 2, out_task);
}

/* The Fangtang pet worker additionally drives its profile-private timed
 * response/thinking presentation hooks, so retain the shared animation
 * cadence even when a remote pet pack has fewer than two frames. */
static inline uint32_t compact_display_adapter_pet_worker_wait_ms(
    size_t remote_pet_frame_count, uint32_t animated_frame_ms) {
    (void)remote_pet_frame_count;
    return animated_frame_ms;
}
