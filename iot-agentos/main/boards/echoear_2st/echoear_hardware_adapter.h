#pragma once

#include "provisioning_failure_injection.h"

/* EchoEar-2ST physical contract.  This file deliberately holds only pin,
 * controller-address and panel-clock facts.  Shared round-screen rendering,
 * audio session policy and normalized Device API contracts stay above it. */

#include "driver/gpio.h"
#include "driver/i2c_master.h"
#include "driver/ledc.h"
#include "driver/spi_master.h"
#include "esp_heap_caps.h"
#include "driver/i2s_std.h"
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"
#include "esp_err.h"
#include "esp_attr.h"
#include "esp_log.h"
#include "esp_lcd_st77916.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "echoear_st77916_init.h"

#define ECHOEAR_DISPLAY_HOST SPI2_HOST
#define ECHOEAR_DISPLAY_WIDTH 360
#define ECHOEAR_DISPLAY_HEIGHT 360
#define ECHOEAR_DISPLAY_STRIPE_ROWS 40
#define ECHOEAR_DISPLAY_SCLK GPIO_NUM_8
#define ECHOEAR_DISPLAY_DATA0 GPIO_NUM_4
#define ECHOEAR_DISPLAY_DATA1 GPIO_NUM_5
#define ECHOEAR_DISPLAY_DATA2 GPIO_NUM_6
#define ECHOEAR_DISPLAY_DATA3 GPIO_NUM_7
#define ECHOEAR_DISPLAY_CS GPIO_NUM_3
#define ECHOEAR_DISPLAY_RESET_GPIO GPIO_NUM_9
#define ECHOEAR_DISPLAY_BACKLIGHT_GPIO GPIO_NUM_41
#define ECHOEAR_DISPLAY_PCLK_HZ (20 * 1000 * 1000)
#define ECHOEAR_DISPLAY_BACKLIGHT_TIMER LEDC_TIMER_0
#define ECHOEAR_DISPLAY_BACKLIGHT_CHANNEL LEDC_CHANNEL_0
#define ECHOEAR_DISPLAY_BACKLIGHT_RESOLUTION LEDC_TIMER_10_BIT

#define ECHOEAR_INPUT_BUTTON_GPIO GPIO_NUM_0
#define ECHOEAR_INPUT_TOUCH_IRQ_GPIO GPIO_NUM_42
#define ECHOEAR_TOUCH_CST8XX_ADDRESS 0x15

#define ECHOEAR_AUDIO_I2C_SCL GPIO_NUM_11
#define ECHOEAR_AUDIO_I2C_SDA GPIO_NUM_12
#define ECHOEAR_AUDIO_MCLK GPIO_NUM_10
#define ECHOEAR_AUDIO_BCLK GPIO_NUM_15
#define ECHOEAR_AUDIO_WS GPIO_NUM_16
#define ECHOEAR_AUDIO_DOUT GPIO_NUM_14
#define ECHOEAR_AUDIO_DIN GPIO_NUM_13
#define ECHOEAR_AUDIO_PA_ENABLE GPIO_NUM_18
#define ECHOEAR_AUDIO_ES7210_ADDRESS 0x40
#define ECHOEAR_AUDIO_ES8311_ADDRESS 0x18
#define ECHOEAR_AUDIO_ES8311_DAC_MUTE_REG 0x31
#define ECHOEAR_AUDIO_ES8311_DAC_VOLUME_REG 0x32
#define ECHOEAR_AUDIO_OUTPUT_VOLUME_DEFAULT 70
#define ECHOEAR_AUDIO_RATE 16000
#define ECHOEAR_AUDIO_MCLK_MULTIPLE I2S_MCLK_MULTIPLE_256

static spi_bus_config_t echoear_display_bus_config(size_t max_transfer_bytes) {
    return (spi_bus_config_t)ST77916_PANEL_BUS_QSPI_CONFIG(
        ECHOEAR_DISPLAY_SCLK, ECHOEAR_DISPLAY_DATA0, ECHOEAR_DISPLAY_DATA1,
        ECHOEAR_DISPLAY_DATA2, ECHOEAR_DISPLAY_DATA3, max_transfer_bytes);
}

static esp_lcd_panel_io_spi_config_t echoear_display_io_config(
    esp_lcd_panel_io_color_trans_done_cb_t transfer_done, void *context) {
    esp_lcd_panel_io_spi_config_t io = ST77916_PANEL_IO_QSPI_CONFIG(
        ECHOEAR_DISPLAY_CS, transfer_done, context);
    /* The long flex cable is unstable at the component default 40 MHz; direct
     * PSRAM DMA also underflows on this board.  These are electrical facts,
     * not shared renderer policy. */
    io.pclk_hz = ECHOEAR_DISPLAY_PCLK_HZ;
    io.flags.psram_dma_direct = false;
    return io;
}

static esp_err_t echoear_display_new_panel(esp_lcd_panel_io_handle_t io,
                                           esp_lcd_panel_handle_t *out_panel) {
    st77916_vendor_config_t vendor = {
        .init_cmds = s_echoear_init_cmds,
        .init_cmds_size = sizeof(s_echoear_init_cmds) / sizeof(s_echoear_init_cmds[0]),
        .flags = {.use_qspi_interface = 1},
    };
    const esp_lcd_panel_dev_config_t panel = {
        .reset_gpio_num = ECHOEAR_DISPLAY_RESET_GPIO,
        .rgb_ele_order = LCD_RGB_ELEMENT_ORDER_RGB,
        .bits_per_pixel = 16,
        .vendor_config = &vendor,
    };
    return esp_lcd_new_panel_st77916(io, &panel, out_panel);
}

/* Neutral display-adapter entry points. `board_port.c` consumes these without
 * knowing whether a logical brightness level reaches a backlight PWM or a
 * controller register. They intentionally configure physical resources only;
 * scene ownership and UI policy remain in the shared renderer. */
static esp_err_t round_display_adapter_init_backlight(void) {
    const ledc_timer_config_t timer = {
        .speed_mode = LEDC_LOW_SPEED_MODE,
        .duty_resolution = ECHOEAR_DISPLAY_BACKLIGHT_RESOLUTION,
        .timer_num = ECHOEAR_DISPLAY_BACKLIGHT_TIMER,
        .freq_hz = 5000,
        .clk_cfg = LEDC_AUTO_CLK,
    };
    esp_err_t err = ledc_timer_config(&timer);
    if (err != ESP_OK) return err;
    const ledc_channel_config_t channel = {
        .gpio_num = ECHOEAR_DISPLAY_BACKLIGHT_GPIO,
        .speed_mode = LEDC_LOW_SPEED_MODE,
        .channel = ECHOEAR_DISPLAY_BACKLIGHT_CHANNEL,
        .intr_type = LEDC_INTR_DISABLE,
        .timer_sel = ECHOEAR_DISPLAY_BACKLIGHT_TIMER,
        .duty = 0,
        .hpoint = 0,
    };
    return ledc_channel_config(&channel);
}

static spi_host_device_t round_display_adapter_host(void) {
    return ECHOEAR_DISPLAY_HOST;
}

static spi_bus_config_t round_display_adapter_bus_config(size_t max_transfer_bytes) {
    return echoear_display_bus_config(max_transfer_bytes);
}

static esp_lcd_panel_io_spi_config_t round_display_adapter_io_config(
    esp_lcd_panel_io_color_trans_done_cb_t transfer_done, void *context) {
    return echoear_display_io_config(transfer_done, context);
}

static esp_err_t round_display_adapter_new_panel(esp_lcd_panel_io_handle_t io,
                                                  esp_lcd_panel_handle_t *out_panel) {
    return echoear_display_new_panel(io, out_panel);
}

static esp_lcd_panel_handle_t s_echoear_display_panel;
static esp_lcd_panel_io_handle_t s_echoear_display_io;
static SemaphoreHandle_t s_echoear_display_transfer_done;
static volatile bool s_echoear_display_transfer_pending;
static bool s_echoear_display_spi_initialized;
static bool s_echoear_display_backlight_initialized;
/* The panel API uses polling QSPI transfers. This pacing protects system
 * housekeeping during a long scene present, and is a bus/controller runtime
 * fact rather than a shared-scene concern. */
static uint16_t s_echoear_display_transactions;

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
    if (s_echoear_display_panel) {
        (void)esp_lcd_panel_del(s_echoear_display_panel);
        s_echoear_display_panel = NULL;
    }
    if (s_echoear_display_io) {
        (void)esp_lcd_panel_io_del(s_echoear_display_io);
        s_echoear_display_io = NULL;
    }
    if (s_echoear_display_backlight_initialized) {
        (void)ledc_stop(LEDC_LOW_SPEED_MODE,
                        ECHOEAR_DISPLAY_BACKLIGHT_CHANNEL, 0);
        (void)ledc_timer_rst(LEDC_LOW_SPEED_MODE,
                             ECHOEAR_DISPLAY_BACKLIGHT_TIMER);
        s_echoear_display_backlight_initialized = false;
    }
    if (s_echoear_display_spi_initialized) {
        (void)spi_bus_free(round_display_adapter_host());
        s_echoear_display_spi_initialized = false;
    }
    if (s_echoear_display_transfer_done) {
        vSemaphoreDelete(s_echoear_display_transfer_done);
        s_echoear_display_transfer_done = NULL;
    }
    s_echoear_display_transfer_pending = false;
}

static esp_err_t round_display_adapter_apply_brightness(unsigned percent) {
    if (!s_echoear_display_panel) return ESP_ERR_INVALID_STATE;
    const uint32_t duty = percent * ((1u << ECHOEAR_DISPLAY_BACKLIGHT_RESOLUTION) - 1u) / 100u;
    esp_err_t err = ledc_set_duty(LEDC_LOW_SPEED_MODE, ECHOEAR_DISPLAY_BACKLIGHT_CHANNEL, duty);
    if (err != ESP_OK) return err;
    return ledc_update_duty(LEDC_LOW_SPEED_MODE, ECHOEAR_DISPLAY_BACKLIGHT_CHANNEL);
}

/* This is the complete profile-private panel bring-up transaction.  The
 * shared renderer retains only framebuffer/scene ownership; it never
 * assembles an EchoEar SPI, callback or panel sequence itself. */
static bool round_display_adapter_color_transfer_done(
    esp_lcd_panel_io_handle_t panel_io, esp_lcd_panel_io_event_data_t *edata,
    void *user_ctx);

static esp_err_t round_display_adapter_init_hardware(
    size_t max_transfer_bytes, unsigned brightness) {
    if (brightness > 100) return ESP_ERR_INVALID_ARG;
    if (s_echoear_display_panel || s_echoear_display_io ||
        s_echoear_display_spi_initialized ||
        s_echoear_display_backlight_initialized) return ESP_ERR_INVALID_STATE;
    if (!s_echoear_display_transfer_done) {
        s_echoear_display_transfer_done = xSemaphoreCreateBinary();
        if (!s_echoear_display_transfer_done) return ESP_ERR_NO_MEM;
    }
    esp_err_t err = round_display_adapter_init_backlight();
    if (err != ESP_OK) return err;
    s_echoear_display_backlight_initialized = true;
    DISPLAY_INIT_FAULT_POINT(1);
    const spi_host_device_t host = round_display_adapter_host();
    const spi_bus_config_t bus = round_display_adapter_bus_config(max_transfer_bytes);
    err = spi_bus_initialize(host, &bus, SPI_DMA_CH_AUTO);
    if (err != ESP_OK) goto fail;
    s_echoear_display_spi_initialized = true;
    DISPLAY_INIT_FAULT_POINT(2);
    const esp_lcd_panel_io_spi_config_t io_config =
        round_display_adapter_io_config(round_display_adapter_color_transfer_done,
                                        s_echoear_display_transfer_done);
    err = esp_lcd_new_panel_io_spi(host, &io_config, &s_echoear_display_io);
    if (err != ESP_OK) goto fail;
    DISPLAY_INIT_FAULT_POINT(3);
    err = round_display_adapter_new_panel(s_echoear_display_io, &s_echoear_display_panel);
    if (err != ESP_OK) goto fail;
    DISPLAY_INIT_FAULT_POINT(4);
    err = esp_lcd_panel_reset(s_echoear_display_panel);
    if (err != ESP_OK) goto fail;
    err = esp_lcd_panel_init(s_echoear_display_panel);
    if (err != ESP_OK) goto fail;
    err = esp_lcd_panel_disp_on_off(s_echoear_display_panel, true);
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
    /* Give the fence before publishing idle.  Because the waiter cannot run
     * until this ISR returns, an idle observation can never strand a delayed
     * completion token for the next QSPI transfer. */
    s_echoear_display_transfer_pending = false;
    return task_woken == pdTRUE;
}

static esp_err_t round_display_adapter_wait_for_transfer_idle(void) {
    if (!s_echoear_display_transfer_pending) return ESP_OK;
    if (!s_echoear_display_transfer_done) return ESP_ERR_INVALID_STATE;
    if (xSemaphoreTake(s_echoear_display_transfer_done, pdMS_TO_TICKS(1000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    return s_echoear_display_transfer_pending ? ESP_ERR_INVALID_STATE : ESP_OK;
}

static bool round_display_adapter_ready(void) {
    return s_echoear_display_panel != NULL;
}

/* The stripe staging buffer is an electrical/DMA constraint, not scene state.
 * EchoEar's PSRAM panel path needs this source in internal DMA-capable RAM. */
static DMA_ATTR uint16_t s_echoear_display_stripe[
    ECHOEAR_DISPLAY_WIDTH * ECHOEAR_DISPLAY_STRIPE_ROWS];

static uint16_t *round_display_adapter_stripe_buffer(void) {
    return s_echoear_display_stripe;
}

static uint16_t round_display_adapter_rgb565(uint8_t r, uint8_t g, uint8_t b) {
    const uint16_t logical = (uint16_t)(((r & 0xF8) << 8) |
                                        ((g & 0xFC) << 3) | (b >> 3));
    /* ST77916 QSPI takes RGB565 MSB-first whereas ESP32 memory is little endian. */
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

/* ST77916 accepts arbitrary column windows, so shared delta bounds need no
 * electrical adjustment beyond their renderer-computed clipping. */
static void round_display_adapter_align_dirty_columns(int *left, int *right,
                                                       int panel_width) {
    (void)left;
    (void)right;
    (void)panel_width;
}

/* The renderer owns frame content and lifetime, but this profile owns the
 * memory capabilities required by its 20 MHz QSPI/bounce-DMA path. */
static uint16_t *round_display_adapter_allocate_framebuffer(size_t bytes) {
    return heap_caps_malloc(bytes,
                            MALLOC_CAP_SPIRAM | MALLOC_CAP_DMA | MALLOC_CAP_8BIT);
}

static uint16_t *round_display_adapter_allocate_ambient_overlay(size_t bytes) {
    return heap_caps_malloc(bytes,
                            MALLOC_CAP_SPIRAM | MALLOC_CAP_DMA | MALLOC_CAP_8BIT);
}

static void round_display_adapter_free_render_buffer(void *buffer) {
    heap_caps_free(buffer);
}

/* Remote pet frames are long-lived display inputs, not generic application
 * blobs. Keep their PSRAM contract with the panel profile so a future round
 * controller can change its cache/DMA constraints without branching the
 * shared scene renderer. */
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
    return "DMA PSRAM";
}

/* The adapter owns the panel completion fence because its callback lifetime,
 * queue semantics and timeout are controller details.  The shared renderer
 * may immediately reuse composition/stripe memory after this call returns. */
static esp_err_t round_display_adapter_draw_bitmap_sync(
    int x0, int y0, int x1, int y1, const void *pixels) {
    if (!s_echoear_display_panel || !s_echoear_display_transfer_done || !pixels) {
        return ESP_ERR_INVALID_STATE;
    }
    /* Retain a timed-out transfer fence instead of permitting a subsequent
     * frame to consume its late callback and reuse DMA-owned scene memory. */
    ESP_RETURN_ON_ERROR(round_display_adapter_wait_for_transfer_idle(),
                        "echoear_display", "previous transfer still pending");
    while (xSemaphoreTake(s_echoear_display_transfer_done, 0) == pdTRUE) {}
    s_echoear_display_transfer_pending = true;
    esp_err_t err = esp_lcd_panel_draw_bitmap(s_echoear_display_panel,
                                              x0, y0, x1, y1, pixels);
    if (err != ESP_OK) {
        s_echoear_display_transfer_pending = false;
        return err;
    }
    if (provisioning_failure_injection_display_transfer_fence_timeout_once()) {
        ESP_LOGW("echoear_display", "test: abandoning first transfer fence wait");
        return ESP_ERR_TIMEOUT;
    }
    if (xSemaphoreTake(s_echoear_display_transfer_done, pdMS_TO_TICKS(1000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    if ((++s_echoear_display_transactions & 0x0Fu) == 0) vTaskDelay(1);
    return ESP_OK;
}

/* DISPLAY_OFF is a panel-and-backlight transaction, not MCU sleep.  Its
 * electrical ordering is profile-private so the shared round renderer owns
 * only scene eligibility and the normalized wake semantics. */
static esp_err_t round_display_adapter_enter_display_off(void) {
    if (!s_echoear_display_panel) return ESP_ERR_INVALID_STATE;
    esp_err_t idle_err = round_display_adapter_wait_for_transfer_idle();
    if (idle_err != ESP_OK) return idle_err;
    /* The PWM backlight, rather than the ST77916 command, is the immediate
     * visible-off guarantee.  QSPI can be briefly busy after a frame; retain
     * the backlight-off result when that optional controller command is
     * deferred, and let the normal wake transaction restore both resources. */
    esp_err_t err = round_display_adapter_apply_brightness(0);
    if (err != ESP_OK) return err;
    err = esp_lcd_panel_disp_on_off(s_echoear_display_panel, false);
    if (err != ESP_OK) {
        ESP_LOGW("echoear_display",
                 "controller display-off deferred (%s); backlight is off",
                 esp_err_to_name(err));
    }
    return ESP_OK;
}

static esp_err_t round_display_adapter_wake_from_display_off(unsigned brightness) {
    if (!s_echoear_display_panel || brightness > 100) return ESP_ERR_INVALID_ARG;
    esp_err_t idle_err = round_display_adapter_wait_for_transfer_idle();
    if (idle_err != ESP_OK) return idle_err;
    esp_err_t err = esp_lcd_panel_disp_on_off(s_echoear_display_panel, true);
    if (err != ESP_OK) return err;
    return round_display_adapter_apply_brightness(brightness);
}

/* EchoEar starts directly on the common standby scene.  A profile may expose
 * a product-only boot artwork, but that artwork must be supplied by the
 * display adapter rather than selected by shared scene/business code. */
static bool round_display_adapter_has_startup_art(void) {
    return false;
}

static void round_display_adapter_compose_startup_art(uint16_t *frame,
                                                       size_t frame_width,
                                                       size_t frame_height) {
    (void)frame;
    (void)frame_width;
    (void)frame_height;
}

/* CST8xx is a simple register-readable controller on EchoEar.  Normalize it
 * to the same touch result as CST9217 so the shared gesture state machine has
 * no controller branch. The adapter owns the device handle lifetime. */
static i2c_master_dev_handle_t s_echoear_touch;

static esp_err_t round_touch_adapter_init(i2c_master_bus_handle_t bus) {
    if (!bus) return ESP_ERR_INVALID_ARG;
    if (s_echoear_touch) return ESP_OK;
    const i2c_device_config_t config = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7,
        .device_address = ECHOEAR_TOUCH_CST8XX_ADDRESS,
        .scl_speed_hz = 100000,
    };
    return i2c_master_bus_add_device(bus, &config, &s_echoear_touch);
}

/* Touch is an optional augmentation on this board: microphone/speaker startup
 * must remain retryable if an individual CST8xx controller is absent. */
static bool round_touch_adapter_init_is_required(void) {
    return false;
}

static void round_touch_adapter_deinit(void) {
    if (s_echoear_touch) {
        (void)i2c_master_bus_rm_device(s_echoear_touch);
        s_echoear_touch = NULL;
    }
}

static bool round_touch_adapter_read(bool *pressed, uint8_t *gesture) {
    if (pressed) *pressed = false;
    if (gesture) *gesture = 0;
    if (!s_echoear_touch) return false;
    uint8_t reg = 0x01;
    uint8_t status[2] = {0};
    if (i2c_master_transmit_receive(s_echoear_touch, &reg, 1, status,
                                    sizeof(status), 50) != ESP_OK) return false;
    if (gesture) *gesture = status[0];
    if (pressed) *pressed = (status[1] & 0x0Fu) != 0;
    return true;
}

/* This CST8xx family reports its controller-native double gesture as 0x0B.
 * Expose the semantic fact rather than letting the shared gesture classifier
 * name or encode a touch-controller register value. */
static bool round_touch_adapter_is_native_double_tap(uint8_t gesture) {
    return gesture == 0x0B;
}

static bool round_touch_adapter_ready(void) {
    return s_echoear_touch != NULL;
}


/* EchoEar exposes BOOT on ESP-IDF GPIO0. The separately labelled
 * PWR/FUNCTION key in Zephyr's gpio1 bank is a board power-control key and is
 * not a dependable application GPIO after boot. Keep that electrical fact in
 * this profile; the shared scanner sees only pressed/not-pressed. */
static esp_err_t round_input_adapter_init_activate_key(void) {
    const gpio_config_t config = {
        .pin_bit_mask = 1ULL << ECHOEAR_INPUT_BUTTON_GPIO,
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    return gpio_config(&config);
}

/* The shared scanner owns debounce, gesture timing and stop/join semantics.
 * Its task stack and priority are profile runtime choices. */
static BaseType_t round_input_adapter_start_scan_task(
    TaskFunction_t entry, TaskHandle_t *out_task) {
    if (!entry || !out_task) return pdFAIL;
    return xTaskCreate(entry, "maclaw_round_input", 3072, NULL, 4, out_task);
}
static bool round_input_adapter_activate_key_pressed(void) {
    return gpio_get_level(ECHOEAR_INPUT_BUTTON_GPIO) == 0;
}


/* These circular profiles have no boot-time transport-selector gesture.  Keep
 * that product-specific policy out of the common gesture classifier. */
static board_input_source_t round_input_adapter_resolve_source(bool key_pressed,
                                                                bool touch_pressed) {
    (void)key_pressed;
    return touch_pressed ? BOARD_INPUT_SOURCE_TOUCH : BOARD_INPUT_SOURCE_OTHER_KEY;
}

static bool round_input_adapter_consume_boot_gesture(board_input_action_t action,
                                                      board_input_source_t source) {
    (void)action;
    (void)source;
    return false;
}

static void round_input_adapter_begin_boot_window(void) {}

static bool round_input_adapter_wait_for_boot_network_toggle(uint32_t window_ms) {
    (void)window_ms;
    return false;
}

/* EchoEar has neither a calibrated battery telemetry source nor an IMU in
 * this profile.  Keep absence as an explicit adapter fact, rather than making
 * the shared board facade select a profile to decide it. */
static bool round_peripheral_adapter_get_power_status(unsigned *level_percent,
                                                       bool *charging) {
    (void)level_percent;
    (void)charging;
    return false;
}

static esp_err_t round_peripheral_adapter_get_motion_sample(
    device_motion_sample_t *out_sample) {
    if (!out_sample) return ESP_ERR_INVALID_ARG;
    return ESP_ERR_NOT_SUPPORTED;
}
