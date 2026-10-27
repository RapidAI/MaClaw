#include "buddy_display.h"

#include <string.h>

#include "driver/gpio.h"
#include "driver/ledc.h"
#include "driver/spi_master.h"
#include "esp_heap_caps.h"
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"
#include "esp_lcd_st77916.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

#include "buddy_panel_init.h"
#include "buddy_protocol_generated.h"

#define LCD_HOST SPI2_HOST
#define LCD_WIDTH 360
#define LCD_HEIGHT 360
#define LCD_STRIPE_ROWS 40
#define LCD_SCLK GPIO_NUM_8
#define LCD_DATA0 GPIO_NUM_4
#define LCD_DATA1 GPIO_NUM_5
#define LCD_DATA2 GPIO_NUM_6
#define LCD_DATA3 GPIO_NUM_7
#define LCD_CS GPIO_NUM_3
#define LCD_RESET GPIO_NUM_9
#define LCD_BACKLIGHT GPIO_NUM_41
#define LCD_PCLK_HZ (20 * 1000 * 1000)
#define BACKLIGHT_TIMER LEDC_TIMER_0
#define BACKLIGHT_CHANNEL LEDC_CHANNEL_0
#define BACKLIGHT_RES LEDC_TIMER_10_BIT

#define BACKLIGHT_DEFAULT_PERCENT 70
#define BACKLIGHT_DIM_AFTER_MS 60000
#define BACKLIGHT_OFF_AFTER_MS 120000
#define BACKLIGHT_DIM_PERCENT 15

static esp_lcd_panel_handle_t s_panel;
static esp_lcd_panel_io_handle_t s_io;
static SemaphoreHandle_t s_transfer_done;
static uint16_t *s_fb;
static int64_t s_last_activity_ms;
static bool s_awake = true;

// DMA staging area: esp_lcd needs an internal bounce buffer because PSRAM DMA
// is disabled on this board's QSPI path.
static DMA_ATTR uint16_t s_stripe[LCD_WIDTH * LCD_STRIPE_ROWS];

static bool on_color_trans_done(esp_lcd_panel_io_handle_t panel_io,
                                esp_lcd_panel_io_event_data_t *edata, void *user_ctx) {
    (void)panel_io;
    (void)edata;
    BaseType_t woken = pdFALSE;
    xSemaphoreGiveFromISR((SemaphoreHandle_t)user_ctx, &woken);
    return woken == pdTRUE;
}

uint16_t buddy_color_swap(uint16_t rgb565) {
    return __builtin_bswap16(rgb565);
}

uint16_t buddy_color(uint8_t r, uint8_t g, uint8_t b) {
    const uint16_t logical =
        (uint16_t)(((r & 0xF8) << 8) | ((g & 0xFC) << 3) | (b >> 3));
    // ST77916 QSPI takes RGB565 MSB-first while ESP32 memory is little endian.
    return buddy_color_swap(logical);
}

static esp_err_t init_backlight(void) {
    const ledc_timer_config_t timer = {
        .speed_mode = LEDC_LOW_SPEED_MODE,
        .duty_resolution = BACKLIGHT_RES,
        .timer_num = BACKLIGHT_TIMER,
        .freq_hz = 5000,
        .clk_cfg = LEDC_AUTO_CLK,
    };
    esp_err_t err = ledc_timer_config(&timer);
    if (err != ESP_OK) return err;
    const ledc_channel_config_t channel = {
        .gpio_num = LCD_BACKLIGHT,
        .speed_mode = LEDC_LOW_SPEED_MODE,
        .channel = BACKLIGHT_CHANNEL,
        .intr_type = LEDC_INTR_DISABLE,
        .timer_sel = BACKLIGHT_TIMER,
        .duty = 0,
        .hpoint = 0,
    };
    return ledc_channel_config(&channel);
}

static void set_backlight(int percent) {
    if (percent < 0) percent = 0;
    if (percent > 100) percent = 100;
    const uint32_t duty = (uint32_t)percent * ((1u << BACKLIGHT_RES) - 1u) / 100u;
    ledc_set_duty(LEDC_LOW_SPEED_MODE, BACKLIGHT_CHANNEL, duty);
    ledc_update_duty(LEDC_LOW_SPEED_MODE, BACKLIGHT_CHANNEL);
}

esp_err_t buddy_display_init(void) {
    s_transfer_done = xSemaphoreCreateBinary();
    if (!s_transfer_done) return ESP_ERR_NO_MEM;

    s_fb = heap_caps_malloc((size_t)LCD_WIDTH * LCD_HEIGHT * sizeof(uint16_t),
                            MALLOC_CAP_SPIRAM | MALLOC_CAP_DMA | MALLOC_CAP_8BIT);
    if (!s_fb) return ESP_ERR_NO_MEM;
    memset(s_fb, 0, (size_t)LCD_WIDTH * LCD_HEIGHT * sizeof(uint16_t));

    esp_err_t err = init_backlight();
    if (err != ESP_OK) return err;

    const spi_bus_config_t bus = ST77916_PANEL_BUS_QSPI_CONFIG(
        LCD_SCLK, LCD_DATA0, LCD_DATA1, LCD_DATA2, LCD_DATA3,
        LCD_WIDTH * LCD_STRIPE_ROWS * sizeof(uint16_t));
    err = spi_bus_initialize(LCD_HOST, &bus, SPI_DMA_CH_AUTO);
    if (err != ESP_OK) return err;

    esp_lcd_panel_io_spi_config_t io = ST77916_PANEL_IO_QSPI_CONFIG(
        LCD_CS, on_color_trans_done, s_transfer_done);
    io.pclk_hz = LCD_PCLK_HZ;
    // Direct PSRAM DMA underflows on the long flex cable; stage through
    // internal RAM instead (same electrical finding as iot-agentos).
    io.flags.psram_dma_direct = false;
    err = esp_lcd_new_panel_io_spi(LCD_HOST, &io, &s_io);
    if (err != ESP_OK) return err;

    st77916_vendor_config_t vendor = {
        .init_cmds = s_echoear_init_cmds,
        .init_cmds_size = sizeof(s_echoear_init_cmds) / sizeof(s_echoear_init_cmds[0]),
        .flags = {.use_qspi_interface = 1},
    };
    const esp_lcd_panel_dev_config_t panel_cfg = {
        .reset_gpio_num = LCD_RESET,
        .rgb_ele_order = LCD_RGB_ELEMENT_ORDER_RGB,
        .bits_per_pixel = 16,
        .vendor_config = &vendor,
    };
    err = esp_lcd_new_panel_st77916(s_io, &panel_cfg, &s_panel);
    if (err != ESP_OK) return err;
    err = esp_lcd_panel_reset(s_panel);
    if (err != ESP_OK) return err;
    err = esp_lcd_panel_init(s_panel);
    if (err != ESP_OK) return err;
    err = esp_lcd_panel_disp_on_off(s_panel, true);
    if (err != ESP_OK) return err;

    set_backlight(BACKLIGHT_DEFAULT_PERCENT);
    s_last_activity_ms = esp_timer_get_time() / 1000;
    s_awake = true;
    return ESP_OK;
}

uint16_t *buddy_display_framebuffer(void) { return s_fb; }
int buddy_display_width(void) { return LCD_WIDTH; }
int buddy_display_height(void) { return LCD_HEIGHT; }

void buddy_display_flush_rows(int y0, int y1) {
    if (!s_panel || !s_fb) return;
    if (y0 < 0) y0 = 0;
    if (y1 > LCD_HEIGHT) y1 = LCD_HEIGHT;
    for (int y = y0; y < y1; y += LCD_STRIPE_ROWS) {
        int rows = y1 - y;
        if (rows > LCD_STRIPE_ROWS) rows = LCD_STRIPE_ROWS;
        memcpy(s_stripe, s_fb + (size_t)y * LCD_WIDTH,
               (size_t)rows * LCD_WIDTH * sizeof(uint16_t));
        // A stale completion token from a timed-out transfer would desync the
        // fence, so drain it before submitting.
        while (xSemaphoreTake(s_transfer_done, 0) == pdTRUE) {
        }
        esp_err_t err = esp_lcd_panel_draw_bitmap(s_panel, 0, y, LCD_WIDTH, y + rows, s_stripe);
        if (err != ESP_OK) return;
        xSemaphoreTake(s_transfer_done, pdMS_TO_TICKS(500));
    }
}

void buddy_display_note_activity(void) {
    s_last_activity_ms = esp_timer_get_time() / 1000;
    if (!s_awake) {
        set_backlight(BACKLIGHT_DEFAULT_PERCENT);
        s_awake = true;
    }
}

bool buddy_display_awake(void) { return s_awake; }

void buddy_display_wake(void) { buddy_display_note_activity(); }

void buddy_display_tick(void) {
    const int64_t idle_ms = (esp_timer_get_time() / 1000) - s_last_activity_ms;
    static bool dimmed = false;
    if (idle_ms >= BACKLIGHT_OFF_AFTER_MS) {
        if (s_awake) {
            set_backlight(0);
            s_awake = false;
            dimmed = false;
        }
        return;
    }
    if (idle_ms >= BACKLIGHT_DIM_AFTER_MS) {
        if (s_awake && !dimmed) {
            set_backlight(BACKLIGHT_DIM_PERCENT);
            dimmed = true;
        }
        return;
    }
    if (dimmed) {
        set_backlight(BACKLIGHT_DEFAULT_PERCENT);
        dimmed = false;
    }
}
