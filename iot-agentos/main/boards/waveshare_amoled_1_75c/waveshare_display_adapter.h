#pragma once

#include "provisioning_failure_injection.h"

/* CO5300 panel contract for the Waveshare ESP32-S3 Touch AMOLED 1.75C.
 * The shared round-scene renderer receives only the resolved panel geometry
 * and esp_lcd configuration. QSPI wiring, controller commands and panel
 * brightness encoding remain profile-private. */

#include "driver/gpio.h"
#include "driver/spi_master.h"
#include "esp_heap_caps.h"
#include "esp_check.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "esp_lcd_co5300.h"
#include "esp_attr.h"
#include "freertos/semphr.h"
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"

extern const uint8_t _binary_waveshare_totem_rgb565_start[];

#define WAVESHARE_DISPLAY_HOST SPI2_HOST
#define WAVESHARE_DISPLAY_WIDTH 466
#define WAVESHARE_DISPLAY_HEIGHT 466
#define WAVESHARE_DISPLAY_STRIPE_ROWS 40
#define WAVESHARE_DISPLAY_PCLK_HZ (20 * 1000 * 1000)
#define WAVESHARE_DISPLAY_RESET_GPIO GPIO_NUM_1
/* Native boot artwork is an implementation detail of this display profile.
 * It needs a readable, central mark on the 466px aperture; the normal
 * standby scene replaces it atomically before weather/pet rendering starts. */
#define WAVESHARE_DISPLAY_NATIVE_TOTEM_SOURCE_SIZE 312
#define WAVESHARE_DISPLAY_NATIVE_TOTEM_DRAW_SIZE 188
#define WAVESHARE_DISPLAY_NATIVE_TOTEM_TOP 38

/* Physical display facts stay with the CO5300 adapter.  They are consumed
 * through round_display_service, not the visual-profile/layout contract. */
static inline int round_display_adapter_width(void) { return WAVESHARE_DISPLAY_WIDTH; }
static inline int round_display_adapter_height(void) { return WAVESHARE_DISPLAY_HEIGHT; }
static inline int round_display_adapter_transfer_stripe_rows(void) {
    return WAVESHARE_DISPLAY_STRIPE_ROWS;
}
static inline uint32_t round_display_adapter_pet_animation_frame_ms(void) {
    return 150u;
}

static const uint8_t s_waveshare_co5300_qspi_mode[] = {0x20};
static const uint8_t s_waveshare_co5300_qspi_enable[] = {0x10};
static const uint8_t s_waveshare_co5300_qspi_clock[] = {0xA0};
static const uint8_t s_waveshare_co5300_page0[] = {0x00};
static const uint8_t s_waveshare_co5300_c4[] = {0x80};
static const uint8_t s_waveshare_co5300_rgb565[] = {0x55};
static const uint8_t s_waveshare_co5300_te[] = {0x00};
static const uint8_t s_waveshare_co5300_control[] = {0x20};
static const uint8_t s_waveshare_co5300_hbm_brightness[] = {0xFF};
static const uint8_t s_waveshare_co5300_contrast_off[] = {0x00};
static const uint8_t s_waveshare_co5300_column[] = {0x00, 0x06, 0x01, 0xD7};
static const uint8_t s_waveshare_co5300_row[] = {0x00, 0x00, 0x01, 0xD1};

static const co5300_lcd_init_cmd_t s_waveshare_co5300_init_cmds[] = {
    {0xFE, s_waveshare_co5300_qspi_mode, sizeof(s_waveshare_co5300_qspi_mode), 0},
    {0x19, s_waveshare_co5300_qspi_enable, sizeof(s_waveshare_co5300_qspi_enable), 0},
    {0x1C, s_waveshare_co5300_qspi_clock, sizeof(s_waveshare_co5300_qspi_clock), 0},
    {0xFE, s_waveshare_co5300_page0, sizeof(s_waveshare_co5300_page0), 0},
    {0xC4, s_waveshare_co5300_c4, sizeof(s_waveshare_co5300_c4), 0},
    {0x3A, s_waveshare_co5300_rgb565, sizeof(s_waveshare_co5300_rgb565), 0},
    {0x35, s_waveshare_co5300_te, sizeof(s_waveshare_co5300_te), 0},
    {0x53, s_waveshare_co5300_control, sizeof(s_waveshare_co5300_control), 0},
    {0x63, s_waveshare_co5300_hbm_brightness, sizeof(s_waveshare_co5300_hbm_brightness), 0},
    {0x2A, s_waveshare_co5300_column, sizeof(s_waveshare_co5300_column), 0},
    {0x2B, s_waveshare_co5300_row, sizeof(s_waveshare_co5300_row), 600},
    {0x11, NULL, 0, 600}, {0x29, NULL, 0, 0},
    {0x51, s_waveshare_co5300_hbm_brightness, sizeof(s_waveshare_co5300_hbm_brightness), 0},
    {0x58, s_waveshare_co5300_contrast_off, sizeof(s_waveshare_co5300_contrast_off), 0},
};

static spi_bus_config_t waveshare_display_bus_config(void) {
    return (spi_bus_config_t){
        .sclk_io_num = GPIO_NUM_38,
        .data0_io_num = GPIO_NUM_4,
        .data1_io_num = GPIO_NUM_5,
        .data2_io_num = GPIO_NUM_6,
        .data3_io_num = GPIO_NUM_7,
        .max_transfer_sz = WAVESHARE_DISPLAY_WIDTH * WAVESHARE_DISPLAY_STRIPE_ROWS *
                           sizeof(uint16_t),
        .flags = SPICOMMON_BUSFLAG_QUAD,
    };
}

static esp_lcd_panel_io_spi_config_t waveshare_display_io_config(
    esp_lcd_panel_io_color_trans_done_cb_t transfer_done, void *context) {
    return (esp_lcd_panel_io_spi_config_t){
        .cs_gpio_num = GPIO_NUM_12,
        .dc_gpio_num = GPIO_NUM_NC,
        .spi_mode = 0,
        .pclk_hz = WAVESHARE_DISPLAY_PCLK_HZ,
        .trans_queue_depth = 10,
        .lcd_cmd_bits = 32,
        .lcd_param_bits = 8,
        .on_color_trans_done = transfer_done,
        .user_ctx = context,
        .flags = {.quad_mode = true},
    };
}

static co5300_vendor_config_t waveshare_display_vendor_config(void) {
    return (co5300_vendor_config_t){
        .init_cmds = s_waveshare_co5300_init_cmds,
        .init_cmds_size = sizeof(s_waveshare_co5300_init_cmds) /
                          sizeof(s_waveshare_co5300_init_cmds[0]),
        .flags = {.use_qspi_interface = 1},
    };
}

static esp_err_t waveshare_display_new_panel(esp_lcd_panel_io_handle_t io,
                                             esp_lcd_panel_handle_t *out_panel) {
    co5300_vendor_config_t vendor = waveshare_display_vendor_config();
    const esp_lcd_panel_dev_config_t panel = {
        .reset_gpio_num = WAVESHARE_DISPLAY_RESET_GPIO,
        .rgb_ele_order = LCD_RGB_ELEMENT_ORDER_RGB,
        .bits_per_pixel = 16,
        .vendor_config = &vendor,
    };
    esp_err_t err = esp_lcd_new_panel_co5300(io, &panel, out_panel);
    if (err != ESP_OK) return err;
    return esp_lcd_panel_set_gap(*out_panel, 6, 0);
}

/* Neutral display-adapter entry points. The AMOLED has no independent
 * backlight: brightness is a CO5300 DCS transaction and its bus-release
 * quirk belongs exclusively to this hardware adapter. */
static esp_err_t round_display_adapter_init_backlight(void) {
    return ESP_OK;
}

static spi_host_device_t round_display_adapter_host(void) {
    return WAVESHARE_DISPLAY_HOST;
}

static spi_bus_config_t round_display_adapter_bus_config(void) {
    return waveshare_display_bus_config();
}

static esp_lcd_panel_io_spi_config_t round_display_adapter_io_config(
    esp_lcd_panel_io_color_trans_done_cb_t transfer_done, void *context) {
    return waveshare_display_io_config(transfer_done, context);
}

static esp_err_t round_display_adapter_new_panel(esp_lcd_panel_io_handle_t io,
                                                  esp_lcd_panel_handle_t *out_panel) {
    return waveshare_display_new_panel(io, out_panel);
}

static esp_lcd_panel_handle_t s_waveshare_display_panel;
static esp_lcd_panel_io_handle_t s_waveshare_display_io;
static SemaphoreHandle_t s_waveshare_display_transfer_done;
static volatile bool s_waveshare_display_transfer_pending;
static bool s_waveshare_display_spi_initialized;
/* CO5300 submits synchronous polling QSPI transfers. Keep the periodic
 * scheduler handoff with this bus implementation, not scene composition. */
static uint16_t s_waveshare_display_transactions;

/* Roll back only a partially created private panel transaction.  This is not
 * a renderer restart API: successful profile resources remain boot-lifetime. */
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
static void round_display_adapter_rollback_partial_init(void) {
    if (s_waveshare_display_panel) {
        (void)esp_lcd_panel_del(s_waveshare_display_panel);
        s_waveshare_display_panel = NULL;
    }
    if (s_waveshare_display_io) {
        (void)esp_lcd_panel_io_del(s_waveshare_display_io);
        s_waveshare_display_io = NULL;
    }
    if (s_waveshare_display_spi_initialized) {
        (void)spi_bus_free(round_display_adapter_host());
        s_waveshare_display_spi_initialized = false;
    }
    if (s_waveshare_display_transfer_done) {
        vSemaphoreDelete(s_waveshare_display_transfer_done);
        s_waveshare_display_transfer_done = NULL;
    }
    s_waveshare_display_transfer_pending = false;
}

static esp_err_t round_display_adapter_apply_brightness(unsigned percent) {
    if (!s_waveshare_display_panel) return ESP_ERR_INVALID_STATE;
    /* The colour-DMA completion callback can precede CO5300 polling-command
     * bus release. Retry only those two transient driver outcomes. */
    vTaskDelay(pdMS_TO_TICKS(20));
    esp_err_t err = ESP_FAIL;
    for (unsigned attempt = 0; attempt < 6; ++attempt) {
        err = esp_lcd_panel_co5300_set_brightness(s_waveshare_display_panel,
                                                   (uint8_t)percent);
        if (err == ESP_OK) return ESP_OK;
        if (err != ESP_ERR_INVALID_STATE && err != ESP_FAIL) break;
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    return err;
}

/* This is the complete profile-private AMOLED bring-up transaction.  CO5300
 * command framing, QSPI bus setup, DMA fence and initial DCS brightness
 * stay together; the shared round renderer receives only synchronous draw
 * success/failure semantics. */
static bool round_display_adapter_color_transfer_done(
    esp_lcd_panel_io_handle_t panel_io, esp_lcd_panel_io_event_data_t *edata,
    void *user_ctx);

static esp_err_t round_display_adapter_init_hardware(unsigned brightness) {
    if (brightness > 100) return ESP_ERR_INVALID_ARG;
    if (s_waveshare_display_panel || s_waveshare_display_io ||
        s_waveshare_display_spi_initialized) return ESP_ERR_INVALID_STATE;
    if (!s_waveshare_display_transfer_done) {
        s_waveshare_display_transfer_done = xSemaphoreCreateBinary();
        if (!s_waveshare_display_transfer_done) return ESP_ERR_NO_MEM;
    }
    esp_err_t err = round_display_adapter_init_backlight();
    if (err != ESP_OK) return err;
    DISPLAY_INIT_FAULT_POINT(1);
    const spi_host_device_t host = round_display_adapter_host();
    const spi_bus_config_t bus = round_display_adapter_bus_config();
    err = spi_bus_initialize(host, &bus, SPI_DMA_CH_AUTO);
    if (err != ESP_OK) goto fail;
    s_waveshare_display_spi_initialized = true;
    DISPLAY_INIT_FAULT_POINT(2);
    const esp_lcd_panel_io_spi_config_t io_config =
        round_display_adapter_io_config(round_display_adapter_color_transfer_done,
                                        s_waveshare_display_transfer_done);
    err = esp_lcd_new_panel_io_spi(host, &io_config, &s_waveshare_display_io);
    if (err != ESP_OK) goto fail;
    DISPLAY_INIT_FAULT_POINT(3);
    err = round_display_adapter_new_panel(s_waveshare_display_io,
                                          &s_waveshare_display_panel);
    if (err != ESP_OK) goto fail;
    DISPLAY_INIT_FAULT_POINT(4);
    err = esp_lcd_panel_reset(s_waveshare_display_panel);
    if (err != ESP_OK) goto fail;
    err = esp_lcd_panel_init(s_waveshare_display_panel);
    if (err != ESP_OK) goto fail;
    err = esp_lcd_panel_disp_on_off(s_waveshare_display_panel, true);
    if (err != ESP_OK) goto fail;
    err = round_display_adapter_apply_brightness(brightness);
    if (err != ESP_OK) goto fail;
    DISPLAY_INIT_FAULT_POINT(5);

    return ESP_OK;
fail:
    round_display_adapter_rollback_partial_init();
    return err;
}

static bool round_display_adapter_color_transfer_done(
    esp_lcd_panel_io_handle_t panel_io, esp_lcd_panel_io_event_data_t *edata,
    void *user_ctx) {
    (void)panel_io;
    (void)edata;
    BaseType_t task_woken = pdFALSE;
    xSemaphoreGiveFromISR((SemaphoreHandle_t)user_ctx, &task_woken);
    /* Publish the completion token while the ISR still owns execution, then
     * mark idle.  This closes the token/pending race across a timeout and a
     * later draw submission. */
    s_waveshare_display_transfer_pending = false;
    return task_woken == pdTRUE;
}

static esp_err_t round_display_adapter_wait_for_transfer_idle(void) {
    if (!s_waveshare_display_transfer_pending) return ESP_OK;
    if (!s_waveshare_display_transfer_done) return ESP_ERR_INVALID_STATE;
    if (xSemaphoreTake(s_waveshare_display_transfer_done, pdMS_TO_TICKS(1000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    return s_waveshare_display_transfer_pending ? ESP_ERR_INVALID_STATE : ESP_OK;
}

static bool round_display_adapter_ready(void) {
    return s_waveshare_display_panel != NULL;
}

/* The CO5300 DMA submission also requires an internal staging source.  Keep
 * this buffer and its fixed 40-row resource budget with the panel profile. */
static DMA_ATTR uint16_t s_waveshare_display_stripe[
    WAVESHARE_DISPLAY_WIDTH * WAVESHARE_DISPLAY_STRIPE_ROWS];

static uint16_t *round_display_adapter_stripe_buffer(void) {
    return s_waveshare_display_stripe;
}

static uint16_t round_display_adapter_rgb565(uint8_t r, uint8_t g, uint8_t b) {
    const uint16_t logical = (uint16_t)(((r & 0xF8) << 8) |
                                        ((g & 0xFC) << 3) | (b >> 3));
    /* CO5300 QSPI retains the selected round renderer's MSB-first RGB565 wire order. */
    return __builtin_bswap16(logical);
}

static uint16_t round_display_adapter_rgb565_lerp(uint16_t from, uint16_t to,
                                                   uint16_t amount) {
    if (amount > 255) amount = 255;
    from = __builtin_bswap16(from);
    to = __builtin_bswap16(to);
    const uint16_t inverse = 255 - amount;
    const uint16_t r = (((from >> 11) & 0x1f) * inverse +
                        ((to >> 11) & 0x1f) * amount + 127) / 255;
    const uint16_t g = (((from >> 5) & 0x3f) * inverse +
                        ((to >> 5) & 0x3f) * amount + 127) / 255;
    const uint16_t b = ((from & 0x1f) * inverse + (to & 0x1f) * amount + 127) / 255;
    return __builtin_bswap16((uint16_t)((r << 11) | (g << 5) | b));
}

/* CO5300 QSPI GRAM windows are column-pair aligned.  If a delta starts or
 * ends on an odd column the controller shifts its payload, leaving a stale
 * pet fragment. Keep this controller constraint below the shared renderer. */
static void round_display_adapter_align_dirty_columns(int *left, int *right,
                                                       int panel_width) {
    if (!left || !right || panel_width <= 0) return;
    *left &= ~1;
    *right |= 1;
    if (*right >= panel_width) *right = panel_width - 1;
}

/* CO5300 QSPI accepts PSRAM source data through its private bounce path.  The
 * larger 466px header must therefore stay out of scarce internal DMA memory. */
static uint16_t *round_display_adapter_allocate_framebuffer(size_t bytes) {
    return heap_caps_malloc(bytes,
                            MALLOC_CAP_SPIRAM | MALLOC_CAP_DMA | MALLOC_CAP_8BIT);
}

static uint16_t *round_display_adapter_allocate_ambient_overlay(size_t bytes) {
    return heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
}

static void round_display_adapter_free_render_buffer(void *buffer) {
    heap_caps_free(buffer);
}

/* A selected remote pet remains resident for the standby renderer's lifetime.
 * CO5300 accepts the resulting PSRAM-backed composed frame through its own
 * bounce path, so this placement is profile-private rather than a renderer
 * assumption. */
static uint8_t *round_display_adapter_allocate_remote_pet_frame(size_t bytes) {
    return heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
}

static void round_display_adapter_free_remote_pet_frame(void *buffer) {
    heap_caps_free(buffer);
}

/* Source pet frames originate in an ESP-IDF capability allocator. The scene
 * renderer transfers ownership by role; this profile owns the matching release.
 */
static void round_display_adapter_release_consumed_pet_source(void *frame) {
    heap_caps_free(frame);
}

/* The standby animator is display-only work.  Its stack placement and core
 * affinity are product-profile runtime constraints, not scene policy. */
static BaseType_t round_display_adapter_start_pet_animation_task(
    TaskFunction_t entry, TaskHandle_t *out_task) {
    if (!entry || !out_task) return pdFAIL;
    return xTaskCreatePinnedToCoreWithCaps(entry, "maclaw_pet_animation", 6144,
                                           NULL, 2, out_task, 1,
                                           MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
}

static const char *round_display_adapter_ambient_overlay_memory_name(void) {
    return "PSRAM";
}

/* The adapter owns the panel completion fence because its callback lifetime,
 * queue semantics and timeout are controller details.  The shared renderer
 * may immediately reuse composition/stripe memory after this call returns. */
static esp_err_t round_display_adapter_draw_bitmap_sync(
    int x0, int y0, int x1, int y1, const void *pixels) {
    if (!s_waveshare_display_panel || !s_waveshare_display_transfer_done || !pixels) {
        return ESP_ERR_INVALID_STATE;
    }
    /* A late CO5300 callback belongs to the old source buffer. Drain that
     * exact transaction first; do not let it complete a newly queued frame. */
    ESP_RETURN_ON_ERROR(round_display_adapter_wait_for_transfer_idle(),
                        "waveshare_display", "previous transfer still pending");
    while (xSemaphoreTake(s_waveshare_display_transfer_done, 0) == pdTRUE) {}
    s_waveshare_display_transfer_pending = true;
    esp_err_t err = esp_lcd_panel_draw_bitmap(s_waveshare_display_panel,
                                              x0, y0, x1, y1, pixels);
    if (err != ESP_OK) {
        s_waveshare_display_transfer_pending = false;
        return err;
    }
    if (provisioning_failure_injection_display_transfer_fence_timeout_once()) {
        ESP_LOGW("waveshare_display", "test: abandoning first transfer fence wait");
        return ESP_ERR_TIMEOUT;
    }
    if (xSemaphoreTake(s_waveshare_display_transfer_done, pdMS_TO_TICKS(1000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    if ((++s_waveshare_display_transactions & 0x0Fu) == 0) vTaskDelay(1);
    return ESP_OK;
}

/* The AMOLED's DISPLAY_OFF transition has no separate backlight GPIO: its
 * controller DCS brightness write must retain the same bus-release retry as
 * ordinary brightness updates.  Scene policy stays in the shared renderer. */
static esp_err_t round_display_adapter_enter_display_off(void) {
    if (!s_waveshare_display_panel) return ESP_ERR_INVALID_STATE;
    esp_err_t idle_err = round_display_adapter_wait_for_transfer_idle();
    if (idle_err != ESP_OK) return idle_err;
    esp_err_t err = esp_lcd_panel_disp_on_off(s_waveshare_display_panel, false);
    if (err != ESP_OK) return err;
    return round_display_adapter_apply_brightness(0);
}

static esp_err_t round_display_adapter_wake_from_display_off(unsigned brightness) {
    if (!s_waveshare_display_panel || brightness > 100) return ESP_ERR_INVALID_ARG;
    esp_err_t idle_err = round_display_adapter_wait_for_transfer_idle();
    if (idle_err != ESP_OK) return idle_err;
    esp_err_t err = esp_lcd_panel_disp_on_off(s_waveshare_display_panel, true);
    if (err != ESP_OK) return err;
    return round_display_adapter_apply_brightness(brightness);
}

/* The badge's eagle is product-only boot art, never a standby pet.  Keep the
 * asset and its pixel geometry inside the display profile so the common round
 * renderer can request a neutral startup surface without testing a board
 * macro or learning which image belongs to which device. */
static bool round_display_adapter_has_startup_art(void) {
    return true;
}

static void round_display_adapter_compose_startup_art(uint16_t *frame,
                                                       size_t frame_width,
                                                       size_t frame_height) {
    if (!frame || frame_width != WAVESHARE_DISPLAY_WIDTH ||
        frame_height != WAVESHARE_DISPLAY_HEIGHT) {
        return;
    }
    const uint16_t *totem = (const uint16_t *)_binary_waveshare_totem_rgb565_start;
    const int draw_size = WAVESHARE_DISPLAY_NATIVE_TOTEM_DRAW_SIZE;
    const int left = ((int)frame_width - draw_size) / 2;
    const int top = WAVESHARE_DISPLAY_NATIVE_TOTEM_TOP;
    for (int y = 0; y < draw_size; ++y) {
        const int source_y = y * WAVESHARE_DISPLAY_NATIVE_TOTEM_SOURCE_SIZE / draw_size;
        uint16_t *target = frame + (size_t)(top + y) * frame_width + left;
        for (int x = 0; x < draw_size; ++x) {
            const int source_x = x * WAVESHARE_DISPLAY_NATIVE_TOTEM_SOURCE_SIZE / draw_size;
            target[x] = totem[(size_t)source_y * WAVESHARE_DISPLAY_NATIVE_TOTEM_SOURCE_SIZE +
                              (size_t)source_x];
        }
    }
}
