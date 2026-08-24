/* Bread Compact ST7789 physical display profile.
 *
 * Shared compact rendering owns scenes, frame buffers and the Device API
 * display semantics.  This adapter owns only the panel's electrical contract
 * so Fangtang's NV3023 path and future compact displays do not inherit Bread
 * GPIO, SPI or PWM facts.
 */
#pragma once

#include "sdkconfig.h"
#include "provisioning_failure_injection.h"
#include "boards/compact_display_animation.h"
#include "boards/compact_startup_art.h"

#if !CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD && !CONFIG_MACLAW_BOARD_REFERENCE_FAKE
#error "Bread display adapter may only be included by Bread Compact or the CI reference profile"
#endif

#ifndef MACLAW_COMPACT_DISPLAY_ADAPTER_IMPLEMENTATION
#error "Bread display adapter is owned exclusively by compact_display_service.c"
#endif

#include "driver/gpio.h"
#include "driver/ledc.h"
#include "driver/spi_master.h"
#include "esp_heap_caps.h"
#include "esp_check.h"
#include "esp_log.h"
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"
#include "esp_lcd_panel_vendor.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include <stddef.h>
#include <stdint.h>

#define BREAD_DISPLAY_HOST SPI3_HOST
#define BREAD_DISPLAY_WIDTH 240
#define BREAD_DISPLAY_HEIGHT 320

#define BREAD_DISPLAY_MOSI GPIO_NUM_47
#define BREAD_DISPLAY_CLK GPIO_NUM_21
#define BREAD_DISPLAY_DC GPIO_NUM_40
#define BREAD_DISPLAY_RESET GPIO_NUM_45
#define BREAD_DISPLAY_CS GPIO_NUM_41
#define BREAD_DISPLAY_BACKLIGHT GPIO_NUM_42

#define BREAD_DISPLAY_BACKLIGHT_LEDC_TIMER LEDC_TIMER_0
#define BREAD_DISPLAY_BACKLIGHT_LEDC_CHANNEL LEDC_CHANNEL_0
#define BREAD_DISPLAY_BACKLIGHT_LEDC_RESOLUTION LEDC_TIMER_10_BIT
#define BREAD_DISPLAY_DEFAULT_BRIGHTNESS 50u

/* This symbol and its geometry are a Bread product-art concern, not a
 * renderer concern.  The common boot presenter only receives a checked full
 * frame through the selected compact profile contract. */
extern const uint8_t _binary_bread_compact_splash_rgb565_start[];
extern const uint8_t _binary_bread_compact_splash_rgb565_end[];

static inline compact_startup_full_frame_t
compact_display_adapter_startup_full_frame(void) {
    return (compact_startup_full_frame_t){
        .pixels = (const uint16_t *)_binary_bread_compact_splash_rgb565_start,
        .bytes = (size_t)(_binary_bread_compact_splash_rgb565_end -
                        _binary_bread_compact_splash_rgb565_start),
        .width = BREAD_DISPLAY_WIDTH,
        .height = BREAD_DISPLAY_HEIGHT,
    };
}

/* Geometry and default brightness describe this physical panel.  Shared scene
 * code consumes only these normalized values and never selects a board model. */
static inline int compact_display_adapter_width(void) { return BREAD_DISPLAY_WIDTH; }
static inline int compact_display_adapter_height(void) { return BREAD_DISPLAY_HEIGHT; }
static inline unsigned compact_display_adapter_default_brightness(void) {
    return BREAD_DISPLAY_DEFAULT_BRIGHTNESS;
}

/* This is a DMA/staging capacity, not standby visual geometry. Keep it in
 * the physical panel adapter so a new compact profile can qualify transport
 * chunking without changing scene/layout data. */
static inline int compact_display_adapter_transfer_stripe_rows(void) { return 16; }

/* ST7789 accepts bounded dirty rectangles efficiently.  The shared renderer
 * owns frame comparison and recovery; this profile only declares the panel
 * transport capability. */
static inline bool compact_display_adapter_uses_delta_presentation(void) {
    return true;
}

/* Bread's robot-mouth animation remains in the shared compact scene. It has
 * no profile-specific partial-transfer patch to compose. */
static inline bool compact_display_adapter_uses_profile_thinking_patch(void) {
    return false;
}

static inline bool compact_display_adapter_compose_thinking_patch(
    uint16_t *pixels, size_t pixel_capacity, unsigned phase, uint16_t background,
    compact_display_animation_patch_t *out_patch) {
    (void)pixels;
    (void)pixel_capacity;
    (void)phase;
    (void)background;
    (void)out_patch;
    return false;
}

static inline esp_err_t bread_display_set_backlight(unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    const uint32_t duty = percent *
        ((1u << BREAD_DISPLAY_BACKLIGHT_LEDC_RESOLUTION) - 1u) / 100u;
    esp_err_t err = ledc_set_duty(LEDC_LOW_SPEED_MODE,
                                  BREAD_DISPLAY_BACKLIGHT_LEDC_CHANNEL, duty);
    if (err == ESP_OK) {
        err = ledc_update_duty(LEDC_LOW_SPEED_MODE,
                               BREAD_DISPLAY_BACKLIGHT_LEDC_CHANNEL);
    }
    return err;
}

/* These three entries are the compact display adapter contract consumed by
 * the shared renderer.  The renderer keeps scene/frame ownership, while each
 * profile keeps its panel IO and transfer-completion mechanics private. */
static inline esp_err_t compact_display_adapter_set_brightness(unsigned percent) {
    return bread_display_set_backlight(percent);
}

/* Panel and panel-IO are profile-private objects.  The shared compact
 * renderer observes only readiness and asks this adapter to submit pixels or
 * perform its normalized DISPLAY_OFF transaction. */
static esp_lcd_panel_handle_t s_bread_display_panel;
static esp_lcd_panel_io_handle_t s_bread_display_io;
/* The renderer normally waits for this fence before it releases a source
 * buffer.  Retain explicit pending state as well: a timed-out wait means a
 * later Power transition must not send panel commands across DMA still owned
 * by the controller. */
static volatile bool s_bread_display_transfer_pending;
static SemaphoreHandle_t s_bread_display_transfer_done;
static bool s_bread_display_spi_initialized;
static bool s_bread_display_backlight_initialized;
static bool s_bread_display_backlight_timer_configured;

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
    if (s_bread_display_panel) {
        (void)esp_lcd_panel_del(s_bread_display_panel);
        s_bread_display_panel = NULL;
    }
    if (s_bread_display_io) {
        (void)esp_lcd_panel_io_del(s_bread_display_io);
        s_bread_display_io = NULL;
    }
    if (s_bread_display_spi_initialized) {
        (void)spi_bus_free(BREAD_DISPLAY_HOST);
        s_bread_display_spi_initialized = false;
    }
    if (s_bread_display_backlight_initialized) {
        (void)ledc_stop(LEDC_LOW_SPEED_MODE,
                        BREAD_DISPLAY_BACKLIGHT_LEDC_CHANNEL, 0);
        s_bread_display_backlight_initialized = false;
    }
    if (s_bread_display_backlight_timer_configured) {
        (void)ledc_timer_rst(LEDC_LOW_SPEED_MODE,
                             BREAD_DISPLAY_BACKLIGHT_LEDC_TIMER);
        s_bread_display_backlight_timer_configured = false;
    }
    s_bread_display_transfer_pending = false;
    s_bread_display_transfer_done = NULL;
}

static bool compact_display_adapter_color_transfer_done(
    esp_lcd_panel_io_handle_t io, esp_lcd_panel_io_event_data_t *event,
    void *user_ctx) {
    (void)io;
    (void)event;
    BaseType_t task_woken = pdFALSE;
    xSemaphoreGiveFromISR((SemaphoreHandle_t)user_ctx, &task_woken);
    /* Publish the fence token before clearing pending.  An ISR cannot switch
     * to a waiter until it returns, so a waiter that observes !pending cannot
     * race ahead and leave this completion token stale for a later transfer. */
    s_bread_display_transfer_pending = false;
    return task_woken == pdTRUE;
}

static inline esp_err_t compact_display_adapter_wait_for_transfer_idle(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_bread_display_transfer_pending) return ESP_OK;
    if (!s_bread_display_transfer_done) return ESP_ERR_INVALID_STATE;
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    if (ticks == 0) ticks = 1;
    if (xSemaphoreTake(s_bread_display_transfer_done, ticks) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    return s_bread_display_transfer_pending ? ESP_ERR_INVALID_STATE : ESP_OK;
}

static inline bool compact_display_adapter_ready(void) {
    return s_bread_display_panel != NULL;
}

/* DISPLAY_OFF is a panel-and-backlight transaction, not MCU sleep.  Keep the
 * electrical ordering in the display profile so the shared Power Service only
 * requests a normalized display transition. */
static inline esp_err_t compact_display_enter_display_off(void) {
    if (!s_bread_display_panel) return ESP_ERR_INVALID_STATE;
    ESP_RETURN_ON_ERROR(compact_display_adapter_wait_for_transfer_idle(1000),
                        "bread_display", "pending transfer before display off");
    /* The backlight is the user-visible power boundary on this LCD.  Turn it
     * off before the optional controller low-power command: a pending/failed
     * SPI command must never leave a bright panel after the common Power HAL
     * has reached its idle deadline.  A later wake redraw re-applies both
     * controller-on and the remembered brightness. */
    ESP_RETURN_ON_ERROR(bread_display_set_backlight(0),
                        "bread_display", "backlight off");
    esp_err_t panel_err = esp_lcd_panel_disp_on_off(s_bread_display_panel, false);
    if (panel_err != ESP_OK) {
        ESP_LOGW("bread_display",
                 "controller display-off deferred (%s); backlight is off",
                 esp_err_to_name(panel_err));
    }
    return ESP_OK;
}

static inline esp_err_t compact_display_wake_from_display_off(unsigned brightness) {
    if (!s_bread_display_panel || brightness > 100) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(compact_display_adapter_wait_for_transfer_idle(1000),
                        "bread_display", "pending transfer before display wake");
    ESP_RETURN_ON_ERROR(esp_lcd_panel_disp_on_off(s_bread_display_panel, true),
                        "bread_display", "panel on");
    return bread_display_set_backlight(brightness);
}

/* This initializes the panel/bus only.  Render state, transfer ordering and
 * frame ownership stay with the shared compact renderer. */
static inline esp_err_t compact_display_adapter_init_hardware(
    SemaphoreHandle_t transfer_done) {
    if (!transfer_done) return ESP_ERR_INVALID_ARG;
    if (s_bread_display_panel || s_bread_display_io ||
        s_bread_display_spi_initialized || s_bread_display_backlight_initialized ||
        s_bread_display_backlight_timer_configured) {
        return ESP_ERR_INVALID_STATE;
    }
    s_bread_display_transfer_done = transfer_done;

    ledc_timer_config_t backlight_timer = {
        .speed_mode = LEDC_LOW_SPEED_MODE,
        .duty_resolution = BREAD_DISPLAY_BACKLIGHT_LEDC_RESOLUTION,
        .timer_num = BREAD_DISPLAY_BACKLIGHT_LEDC_TIMER,
        .freq_hz = 5000,
        .clk_cfg = LEDC_AUTO_CLK,
    };
    esp_err_t err = ledc_timer_config(&backlight_timer);
    if (err != ESP_OK) return err;
    s_bread_display_backlight_timer_configured = true;

    ledc_channel_config_t backlight_channel = {
        .gpio_num = BREAD_DISPLAY_BACKLIGHT,
        .speed_mode = LEDC_LOW_SPEED_MODE,
        .channel = BREAD_DISPLAY_BACKLIGHT_LEDC_CHANNEL,
        .intr_type = LEDC_INTR_DISABLE,
        .timer_sel = BREAD_DISPLAY_BACKLIGHT_LEDC_TIMER,
        .duty = 0,
        .hpoint = 0,
    };
    err = ledc_channel_config(&backlight_channel);
    if (err != ESP_OK) goto fail;
    s_bread_display_backlight_initialized = true;
    DISPLAY_INIT_FAULT_POINT(1);

    spi_bus_config_t bus = {
        .mosi_io_num = BREAD_DISPLAY_MOSI,
        .miso_io_num = GPIO_NUM_NC,
        .sclk_io_num = BREAD_DISPLAY_CLK,
        .quadwp_io_num = GPIO_NUM_NC,
        .quadhd_io_num = GPIO_NUM_NC,
        .max_transfer_sz = BREAD_DISPLAY_WIDTH * 16 * sizeof(uint16_t),
    };
    err = spi_bus_initialize(BREAD_DISPLAY_HOST, &bus, SPI_DMA_CH_AUTO);
    if (err != ESP_OK) goto fail;
    s_bread_display_spi_initialized = true;
    DISPLAY_INIT_FAULT_POINT(2);

    esp_lcd_panel_io_spi_config_t io_cfg = {
        .cs_gpio_num = BREAD_DISPLAY_CS,
        .dc_gpio_num = BREAD_DISPLAY_DC,
        .spi_mode = 3,
        .pclk_hz = 20 * 1000 * 1000,
        .trans_queue_depth = 10,
        .lcd_cmd_bits = 8,
        .lcd_param_bits = 8,
        .on_color_trans_done = compact_display_adapter_color_transfer_done,
        .user_ctx = transfer_done,
    };
    err = esp_lcd_new_panel_io_spi(BREAD_DISPLAY_HOST, &io_cfg,
                                   &s_bread_display_io);
    if (err != ESP_OK) goto fail;
    DISPLAY_INIT_FAULT_POINT(3);

    esp_lcd_panel_dev_config_t panel_cfg = {
        .reset_gpio_num = BREAD_DISPLAY_RESET,
        .rgb_ele_order = LCD_RGB_ELEMENT_ORDER_RGB,
        .bits_per_pixel = 16,
    };
    err = esp_lcd_new_panel_st7789(s_bread_display_io, &panel_cfg,
                                   &s_bread_display_panel);
    if (err != ESP_OK) goto fail;
    DISPLAY_INIT_FAULT_POINT(4);
    err = esp_lcd_panel_reset(s_bread_display_panel);
    if (err != ESP_OK) goto fail;
    err = esp_lcd_panel_init(s_bread_display_panel);
    if (err != ESP_OK) goto fail;
    err = esp_lcd_panel_invert_color(s_bread_display_panel, true);
    if (err != ESP_OK) goto fail;
    err = esp_lcd_panel_disp_on_off(s_bread_display_panel, true);
    if (err != ESP_OK) goto fail;

    DISPLAY_INIT_FAULT_POINT(5);

    return ESP_OK;
fail:
    compact_display_adapter_rollback_partial_init();
    return err;
}

static inline esp_err_t compact_display_adapter_draw_bitmap_sync(
    SemaphoreHandle_t transfer_done, int x0, int y0, int x1, int y1,
    const void *pixels) {
    if (!s_bread_display_panel || !transfer_done || !pixels) return ESP_ERR_INVALID_ARG;
    /* A previous timeout leaves its source and completion record retained.
     * Never queue a new transfer until that exact controller-owned source has
     * completed: otherwise its delayed callback could satisfy the new wait
     * and let the renderer reuse a framebuffer still read by SPI DMA. */
    ESP_RETURN_ON_ERROR(compact_display_adapter_wait_for_transfer_idle(1000),
                        "bread_display", "previous transfer still pending");
    while (xSemaphoreTake(transfer_done, 0) == pdTRUE) {}
    s_bread_display_transfer_pending = true;
    esp_err_t err = esp_lcd_panel_draw_bitmap(s_bread_display_panel,
                                              x0, y0, x1, y1, pixels);
    if (err != ESP_OK) {
        s_bread_display_transfer_pending = false;
        return err;
    }
    if (provisioning_failure_injection_display_transfer_fence_timeout_once()) {
        ESP_LOGW("bread_display", "test: abandoning first transfer fence wait");
        return ESP_ERR_TIMEOUT;
    }
    return xSemaphoreTake(transfer_done, pdMS_TO_TICKS(1000)) == pdTRUE
               ? ESP_OK : ESP_ERR_TIMEOUT;
}

/* Display buffers are renderer-owned, but their memory capability is a panel
 * contract.  Keep the PSRAM/DMA decision below this profile seam so the
 * shared renderer never needs to know what ST7789 SPI can consume. */
static inline uint16_t *compact_display_adapter_alloc_framebuffer(size_t bytes) {
    return heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
}

static inline uint16_t *compact_display_adapter_alloc_transfer_buffer(size_t bytes) {
    return heap_caps_malloc(bytes, MALLOC_CAP_DMA | MALLOC_CAP_INTERNAL);
}

static inline void compact_display_adapter_free_buffer(void *buffer) {
    heap_caps_free(buffer);
}

/* Short-lived renderer bitmaps follow the same physical-memory contract as
 * their eventual destination. A bitmap generated while composing a retained
 * PSRAM frame is copied by the CPU and therefore need not consume scarce DMA
 * RAM; a direct bitmap submission is read by SPI DMA and must remain DMA
 * capable. The shared scene renderer selects the role, never heap caps. */
static inline uint16_t *compact_display_adapter_allocate_temporary_composition_bitmap(
    size_t bytes) {
    if (!bytes) return NULL;
    uint16_t *bitmap = heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    /* Preserve the renderer's established degradation path: fragmented PSRAM
     * may fall back to a smaller-lived DMA allocation rather than dropping a
     * status or pet frame altogether. */
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

/* Remote pet frames are long-lived display assets, not generic heap data.
 * Keep their preferred memory capability and matching release operation in
 * this profile so the common scene renderer does not inherit ST7789 memory
 * placement assumptions.  The libc fallback preserves the prior behavior
 * when PSRAM is temporarily fragmented. */
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
    TaskFunction_t entry, void *context, TaskHandle_t *out_task) {
    if (!entry || !out_task) return pdFAIL;
    return xTaskCreate(entry, "maclaw_bread_thinking", 3072, context, 2, out_task);
}

/* Bread keeps the conservative established thinking cadence.  The board has
 * a different panel/transport budget from Fangtang, so this remains a
 * physical display profile decision rather than a scene-level board branch. */
static inline uint32_t compact_display_adapter_thinking_worker_wait_ms(
    uint32_t common_interval_ms) {
    return common_interval_ms;
}

static inline BaseType_t compact_display_adapter_start_pet_animation_task(
    TaskFunction_t entry, void *context, TaskHandle_t *out_task) {
    if (!entry || !out_task) return pdFAIL;
    return xTaskCreate(entry, "maclaw_bread_pet", 3072, context, 2, out_task);
}

/* Bread's pet worker also enforces the shared idle-display timeout.  When no
 * animated pack is active, wake it infrequently to avoid spending power on
 * an 80 ms decorative tick; installing a second frame restores the common
 * animation cadence immediately. */
static inline uint32_t compact_display_adapter_pet_worker_wait_ms(
    size_t remote_pet_frame_count, uint32_t animated_frame_ms) {
    return remote_pet_frame_count < 2 ? 500u : animated_frame_ms;
}

/* Bread's established presentation budget can retain the traditional
 * one-presented-tick phase progression. */
static inline bool compact_display_adapter_pet_animation_tracks_elapsed_time(void) {
    return false;
}

/* Bread does not currently require transport qualification telemetry.  Keep
 * the private service seam uniform so renderer business/scene behavior never
 * branches on the selected physical panel. */
static inline void compact_display_adapter_note_pet_animation_tick(
    uint32_t target_interval_ms, bool presented, uint32_t presentation_us) {
    (void)target_interval_ms;
    (void)presented;
    (void)presentation_us;
}
