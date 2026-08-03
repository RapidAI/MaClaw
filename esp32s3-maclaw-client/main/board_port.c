#include "board_port.h"

#include <ctype.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "driver/gpio.h"
#include "driver/i2c_master.h"
#include "driver/i2s_std.h"
#include "driver/spi_master.h"
#include "esp_heap_caps.h"
#include "esp_check.h"
#include "esp_mn_models.h"
#include "esp_mn_speech_commands.h"
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"
#include "esp_lcd_st77916.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "mbedtls/base64.h"
#include "model_path.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "echoear_st77916_init.h"
#include "ack_voice_adpcm.h"
#include "font_cjk24.h"

// EchoEar-2ST board definition. GPIO values are the physical GPIO numbers;
// the original Zephyr board file uses gpio1 offsets for pins 40/41/48.
#define LCD_HOST        SPI2_HOST
#define LCD_WIDTH       360
#define LCD_HEIGHT      360
#define LCD_SCLK        GPIO_NUM_8
#define LCD_DATA0       GPIO_NUM_4
#define LCD_DATA1       GPIO_NUM_5
#define LCD_DATA2       GPIO_NUM_6
#define LCD_DATA3       GPIO_NUM_7
#define LCD_CS          GPIO_NUM_3
#define LCD_RESET       GPIO_NUM_9
#define LCD_BACKLIGHT   GPIO_NUM_41
#define FUNCTION_BUTTON GPIO_NUM_0
#define TOUCH_IRQ       GPIO_NUM_42
#define CST8XX_ADDRESS  0x15
#define AUDIO_I2C_SCL   GPIO_NUM_11
#define AUDIO_I2C_SDA   GPIO_NUM_12
#define AUDIO_MCLK      GPIO_NUM_10
#define AUDIO_BCLK      GPIO_NUM_15
#define AUDIO_WS        GPIO_NUM_16
#define AUDIO_DOUT      GPIO_NUM_14
#define AUDIO_DIN       GPIO_NUM_13
#define AUDIO_PA_ENABLE GPIO_NUM_18
#define ES7210_ADDRESS  0x40
#define ES8311_ADDRESS  0x18
#define AUDIO_RATE      16000
#define AUDIO_SECONDS   6
#define WAKE_WORD_COMMAND_ID 1
// Keep one recognizer running in standby. Running Chinese and English models
// together doubles inference work and makes normal-speed wake-up unreliable.
#define WAKE_WORD_CN_LABEL "码卡龙"
#define WAKE_WORD_CN_PHONETIC "ma ka long"
// The default command threshold favours deliberate, slow commands.  A modest
// reduction preserves a practical false-wake margin while accepting normal
// conversational delivery of the short product name.
#define WAKE_WORD_DETECTION_THRESHOLD 0.45f
#define WAKE_WORD_COOLDOWN_US (2LL * 1000 * 1000)

#define LCD_STRIPE_ROWS 40
#define TEXT_SCALE      2
#define TEXT_ADVANCE    7
#define CJK_FONT_SIZE   24
#define CJK_ADVANCE     25
#define TEXT24_ASCII_ADVANCE 12
#define TEXT24_SPACE_ADVANCE 8
#define DYNAMIC_GLYPH_BYTES 72
#define DYNAMIC_GLYPH_CACHE_CAPACITY 128
#define COMPACT_FONT_SIZE 16
#define COMPACT_CJK_ADVANCE 17
#define COMPACT_ASCII_ADVANCE 12
#define STATUS_TEXT_X   54
#define STATUS_TEXT_Y   254
#define STATUS_TEXT_GAP 38
#define STATUS_TEXT_W   (LCD_WIDTH - STATUS_TEXT_X)
#define STATUS_TEXT_H   (STATUS_TEXT_GAP + CJK_FONT_SIZE)
#define RESPONSE_TEXT_X 32
#define RESPONSE_TEXT_Y 94
#define RESPONSE_TEXT_W (LCD_WIDTH - RESPONSE_TEXT_X * 2)
#define RESPONSE_LINE_GAP 32
#define RESPONSE_LINES_PER_PAGE 4
#define RESPONSE_PAGE_INTERVAL_US (3LL * 1000 * 1000)
#define RESPONSE_TEXT_CAPACITY 768
#define AMBIENT_TOP_W   336
#define AMBIENT_TOP_H   60
#define AMBIENT_BOTTOM_W 316
#define AMBIENT_BOTTOM_H 72
#define AMBIENT_TRANSPARENT_KEY 0x0001u
#define LCD_FRAMEBUFFER_PIXELS ((size_t)LCD_WIDTH * LCD_HEIGHT)
#define LCD_FRAMEBUFFER_BYTES  (LCD_FRAMEBUFFER_PIXELS * sizeof(uint16_t))
// Full-scene vector composition plus the 360 px DMA transfer measures about
// 78 ms on this ESP32-S3. An 80 ms cadence matches that real throughput and
// prevents vTaskDelayUntil() from entering repeated catch-up iterations.
#define PET_ANIMATION_FRAME_MS 80
#define READY_PROMPT_TIMEOUT_US (60LL * 1000 * 1000)
#define IDLE_PET_SLEEP_TIMEOUT_US (30LL * 60 * 1000 * 1000)
#define PET_HALO_CENTER_Y 175
#define PET_HALO_RADIUS   106

static const char *TAG = "maclaw_board";
static esp_lcd_panel_handle_t s_panel;
static esp_lcd_panel_io_handle_t s_panel_io;
static board_port_button_cb_t s_on_button;
static void *s_on_press_arg;

static char s_pet_state[16] = "quiet";
static char s_pet_skin[32] = "clawmate";
static bool s_pet_motion_enabled = true;
static uint8_t s_pet_frame;
static uint32_t s_pet_motion_tick;
static bool s_recording_active;
static bool s_recording_paused;
static volatile uint32_t s_recording_elapsed_seconds;
static volatile uint16_t s_recording_audio_level;
// The recording screen is fed from the same signed PCM samples that are saved
// or uploaded. Each display column represents the measured, gated signal
// envelope from 128 samples (8 ms at 16 kHz), so the 96-column trace covers
// the most recent 768 ms. Using the envelope rather than literal extrema is
// important on this shared full-duplex I2S bus: a clipped bus word must not
// turn the whole waveform area into one opaque rectangle.
#define RECORDING_WAVE_COLUMNS 96
#define RECORDING_WAVE_SAMPLES_PER_COLUMN 128
static int16_t s_recording_wave_min[RECORDING_WAVE_COLUMNS];
static int16_t s_recording_wave_max[RECORDING_WAVE_COLUMNS];
static int16_t s_recording_wave_pending_min = INT16_MAX;
static int16_t s_recording_wave_pending_max = INT16_MIN;
static uint16_t s_recording_wave_pending_samples;
// ES7210 frames on EchoEar have a board-dependent DC offset. Track it slowly
// so only variation around the microphone's idle level reaches the waveform.
static int32_t s_recording_wave_dc;
static char s_ambient_time[9];
static char s_ambient_location[24];
static char s_ambient_date[8];
static char s_ambient_weekday[16];
static char s_ambient_weather[24];
static int s_ambient_temperature_c;
static bool s_ambient_weather_valid;
static bool s_ambient_weather_stale;
static int64_t s_ready_prompt_expires_us;
static int64_t s_idle_pet_sleep_expires_us;
static bool s_idle_pet_visible;
static bool s_display_sleeping;
// Provisioning is a task-focused screen, not a temporary pet overlay. Keep
// the animation task from repainting the QR code after it is shown.
static bool s_setup_qrcode_visible;
// A reply is a small, paged reading surface rather than a clipped status
// line.  The original two-line status buffer remains for short system states;
// replies use the full lower safe area and advance only after a calm pause.
static bool s_response_active;
static unsigned s_response_page;
static int64_t s_response_next_page_us;
static char s_response_title[48];
static char s_response_text[RESPONSE_TEXT_CAPACITY];
static volatile uint32_t s_ambient_revision;
static portMUX_TYPE s_state_lock = portMUX_INITIALIZER_UNLOCKED;
typedef struct {
    uint32_t codepoint;
    uint8_t bitmap[DYNAMIC_GLYPH_BYTES];
    uint32_t last_used;
    bool used;
} dynamic_glyph_t;
static dynamic_glyph_t s_dynamic_glyphs[DYNAMIC_GLYPH_CACHE_CAPACITY];
static uint32_t s_dynamic_glyph_clock;
// esp_lcd panel drawing and the shared stripe buffer cannot be used concurrently.
static SemaphoreHandle_t s_lcd_mutex;
static SemaphoreHandle_t s_lcd_transfer_done;
static DMA_ATTR uint16_t s_line[LCD_WIDTH * LCD_STRIPE_ROWS];
// Two complete RGB565 frames live in DMA-capable PSRAM. While the LCD sends
// one frame, the renderer composes the next one into the other buffer. This
// replaces hundreds of tiny scan-line transactions per pet frame with one
// contiguous DMA transfer and prevents the source pixels changing in flight.
static uint16_t *s_framebuffers[2];
static uint8_t s_next_framebuffer;
static uint16_t *s_render_target;
static volatile uint32_t s_presented_frames;
// Status text gets an immutable DMA buffer. Color transfers are queued by the
// LCD IO driver, so reusing s_line immediately can otherwise modify pixels
// that have not reached the panel yet and leave specks around the text rows.
static DMA_ATTR uint16_t s_status_text[STATUS_TEXT_W * STATUS_TEXT_H];
// Dedicated immutable buffers are required for the two ambient dirty regions.
// Reusing s_line while the LCD transaction is still queued is what produced
// the short white/purple fragments visible at the top of the round screen.
static DMA_ATTR uint16_t s_ambient_top[AMBIENT_TOP_W * AMBIENT_TOP_H];
static DMA_ATTR uint16_t s_ambient_bottom[AMBIENT_BOTTOM_W * AMBIENT_BOTTOM_H];
static uint16_t s_draw_transactions;
static volatile uint32_t s_skipped_pet_frames;
static i2c_master_bus_handle_t s_audio_i2c_bus;
static i2c_master_dev_handle_t s_es7210;
static i2c_master_dev_handle_t s_es8311;
static i2c_master_dev_handle_t s_touch;
static i2s_chan_handle_t s_audio_rx;
static i2s_chan_handle_t s_audio_tx;
static bool s_audio_ready;
static volatile bool s_command_display_locked;
static bool s_recording_is_meeting;
static SemaphoreHandle_t s_audio_mutex;
static TaskHandle_t s_wake_word_task;
static board_port_wake_word_cb_t s_on_wake_word;
static void *s_on_wake_word_arg;
static volatile bool s_wake_word_paused;
static volatile bool s_wake_word_stop_requested;
static portMUX_TYPE s_wake_word_lock = portMUX_INITIALIZER_UNLOCKED;

// esp_lcd queues color transfers asynchronously. The source buffer may only
// be reused after this callback fires; a command transaction is not a reliable
// color-DMA fence and was the cause of the horizontal rainbow blocks.
static bool lcd_color_transfer_done(esp_lcd_panel_io_handle_t panel_io,
                                    esp_lcd_panel_io_event_data_t *edata,
                                    void *user_ctx) {
    (void)panel_io;
    (void)edata;
    BaseType_t task_woken = pdFALSE;
    xSemaphoreGiveFromISR((SemaphoreHandle_t)user_ctx, &task_woken);
    return task_woken == pdTRUE;
}

static esp_err_t wait_for_lcd_color_transfer(void) {
    if (!s_lcd_transfer_done) return ESP_ERR_INVALID_STATE;
    return xSemaphoreTake(s_lcd_transfer_done, pdMS_TO_TICKS(1000)) == pdTRUE
               ? ESP_OK : ESP_ERR_TIMEOUT;
}

static esp_err_t draw_bitmap_sync(int x0, int y0, int x1, int y1,
                                  const void *pixels);
static unsigned response_page_count(void);
static void draw_response_page(void);

// Every frame present uses the same completion fence. Keeping this in one
// helper prevents the pet and recording paths from drifting apart, and makes
// a failed submission unable to consume a completion from a later transfer.
static esp_err_t present_frame_sync(const uint16_t *frame) {
    if (!s_panel || !frame) return ESP_ERR_INVALID_ARG;
    // The frame lives in PSRAM, while this ESP32-S3 shares its MSPI fabric
    // between PSRAM and the LCD. With psram_dma_direct disabled, esp_lcd must
    // allocate an internal bounce buffer for every submitted color transfer.
    // A 360x360 submission needs 259 KB of contiguous DMA memory, which is
    // unavailable after Wi-Fi/TLS start and manifests as a noisy screen.
    // Stage one 40-row stripe through the dedicated internal DMA buffer.
    for (int y = 0; y < LCD_HEIGHT; y += LCD_STRIPE_ROWS) {
        const int rows = (LCD_HEIGHT - y) < LCD_STRIPE_ROWS
                             ? (LCD_HEIGHT - y) : LCD_STRIPE_ROWS;
        memcpy(s_line, frame + (size_t)y * LCD_WIDTH,
               (size_t)LCD_WIDTH * rows * sizeof(s_line[0]));
        esp_err_t err = draw_bitmap_sync(0, y, LCD_WIDTH, y + rows, s_line);
        if (err != ESP_OK) return err;
        if ((++s_draw_transactions & 0x0Fu) == 0) vTaskDelay(1);
    }
    return ESP_OK;
}

// esp_lcd_panel_draw_bitmap() only queues the color transaction. Keep every
// caller's source buffer immutable until the color-complete callback fires.
// Centralizing this fence also avoids waiting for a callback when queueing the
// transaction itself failed, which could otherwise consume a later transfer's
// completion signal and desynchronize all subsequent draws.
static esp_err_t draw_bitmap_sync(int x0, int y0, int x1, int y1,
                                  const void *pixels) {
    if (!s_panel || !pixels) return ESP_ERR_INVALID_ARG;
    esp_err_t err = esp_lcd_panel_draw_bitmap(s_panel, x0, y0, x1, y1, pixels);
    if (err != ESP_OK) return err;
    return wait_for_lcd_color_transfer();
}

static esp_err_t es7210_write(uint8_t reg, uint8_t value) {
    uint8_t bytes[2] = {reg, value};
    return i2c_master_transmit(s_es7210, bytes, sizeof(bytes), 1000);
}

static esp_err_t es8311_write(uint8_t reg, uint8_t value) {
    uint8_t bytes[2] = {reg, value};
    return i2c_master_transmit(s_es8311, bytes, sizeof(bytes), 1000);
}

static esp_err_t es8311_init(void) {
    // EchoEar-2ST uses the ES8311 at 0x18 as I2S slave. This sequence is
    // Espressif's BSP initialization for 16-bit, 16 kHz playback with the
    // ESP32-S3 supplying its 4.096 MHz MCLK.
    static const uint8_t init[][2] = {
        {0x00, 0x1F}, {0x00, 0x00}, {0x00, 0x80},
        {0x01, 0x3F}, {0x02, 0x00}, {0x03, 0x10}, {0x04, 0x10},
        {0x05, 0x00}, {0x06, 0x04}, {0x07, 0x00}, {0x08, 0xFF},
        {0x09, 0x0C}, {0x0A, 0x0C}, {0x0D, 0x01}, {0x0E, 0x02},
        {0x12, 0x00}, {0x13, 0x10}, {0x1C, 0x6A}, {0x37, 0x08},
        {0x31, 0x00}, {0x32, 0xB2},
    };
    ESP_RETURN_ON_ERROR(es8311_write(init[0][0], init[0][1]), TAG, "ES8311 reset");
    vTaskDelay(pdMS_TO_TICKS(20));
    for (size_t i = 1; i < sizeof(init) / sizeof(init[0]); ++i) {
        ESP_RETURN_ON_ERROR(es8311_write(init[i][0], init[i][1]), TAG,
                            "ES8311 reg %02x", init[i][0]);
    }
    return ESP_OK;
}

static esp_err_t audio_init(void) {
    if (s_audio_ready) return ESP_OK;
    i2c_master_bus_config_t bus_cfg = {
        .i2c_port = I2C_NUM_0, .sda_io_num = AUDIO_I2C_SDA,
        .scl_io_num = AUDIO_I2C_SCL, .clk_source = I2C_CLK_SRC_DEFAULT,
        .glitch_ignore_cnt = 7, .flags.enable_internal_pullup = true,
    };
    ESP_RETURN_ON_ERROR(i2c_new_master_bus(&bus_cfg, &s_audio_i2c_bus), TAG, "audio I2C init");
    i2c_device_config_t dev_cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7, .device_address = ES7210_ADDRESS,
        .scl_speed_hz = 100000,
    };
    ESP_RETURN_ON_ERROR(i2c_master_bus_add_device(s_audio_i2c_bus, &dev_cfg, &s_es7210), TAG, "ES7210 add");
    i2c_device_config_t speaker_cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7, .device_address = ES8311_ADDRESS,
        .scl_speed_hz = 100000,
    };
    ESP_RETURN_ON_ERROR(i2c_master_bus_add_device(s_audio_i2c_bus, &speaker_cfg, &s_es8311), TAG, "ES8311 add");
    i2c_device_config_t touch_cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7, .device_address = CST8XX_ADDRESS,
        .scl_speed_hz = 100000,
    };
    esp_err_t touch_err = i2c_master_bus_add_device(s_audio_i2c_bus, &touch_cfg, &s_touch);
    if (touch_err != ESP_OK) {
        ESP_LOGW(TAG, "CST8xx touch add failed: %s", esp_err_to_name(touch_err));
        s_touch = NULL;
    }

    static const uint8_t init[][2] = {
        {0x00,0xFF},{0x00,0x32},{0x09,0x30},{0x0A,0x30},
        {0x23,0x2A},{0x22,0x0A},{0x21,0x2A},{0x20,0x0A},
        {0x11,0x60},{0x12,0x00},{0x40,0xC3},{0x41,0x70},{0x42,0x70},
        {0x43,0x1A},{0x44,0x1A},{0x45,0x1A},{0x46,0x1A},
        {0x47,0x08},{0x48,0x08},{0x49,0x00},{0x4A,0x00},
        {0x07,0x20},{0x02,0xC1},{0x04,0x01},{0x05,0x00},
        {0x06,0x04},{0x4B,0x0F},{0x4C,0x0F},{0x00,0x71},{0x00,0x41},
    };
    for (size_t i = 0; i < sizeof(init) / sizeof(init[0]); ++i) {
        ESP_RETURN_ON_ERROR(es7210_write(init[i][0], init[i][1]), TAG, "ES7210 reg %02x", init[i][0]);
    }

    i2s_chan_config_t chan_cfg = I2S_CHANNEL_DEFAULT_CONFIG(I2S_NUM_0, I2S_ROLE_MASTER);
    chan_cfg.dma_desc_num = 8;
    chan_cfg.dma_frame_num = 256;
    ESP_RETURN_ON_ERROR(i2s_new_channel(&chan_cfg, NULL, &s_audio_rx), TAG, "I2S RX create");
    i2s_std_config_t std_cfg = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(AUDIO_RATE),
        .slot_cfg = I2S_STD_PHILIPS_SLOT_DEFAULT_CONFIG(I2S_DATA_BIT_WIDTH_16BIT, I2S_SLOT_MODE_STEREO),
        .gpio_cfg = {
            .mclk = AUDIO_MCLK, .bclk = AUDIO_BCLK, .ws = AUDIO_WS,
            .dout = I2S_GPIO_UNUSED, .din = AUDIO_DIN,
            .invert_flags = {.mclk_inv = false, .bclk_inv = false, .ws_inv = false},
        },
    };
    ESP_RETURN_ON_ERROR(i2s_channel_init_std_mode(s_audio_rx, &std_cfg), TAG, "I2S RX mode");
    ESP_RETURN_ON_ERROR(i2s_channel_enable(s_audio_rx), TAG, "I2S RX enable");
    ESP_RETURN_ON_ERROR(i2s_new_channel(&chan_cfg, &s_audio_tx, NULL), TAG, "I2S TX create");
    std_cfg.gpio_cfg.dout = AUDIO_DOUT;
    ESP_RETURN_ON_ERROR(i2s_channel_init_std_mode(s_audio_tx, &std_cfg), TAG, "I2S TX mode");
    ESP_RETURN_ON_ERROR(i2s_channel_enable(s_audio_tx), TAG, "I2S TX enable");
    gpio_config_t pa_cfg = {
        .pin_bit_mask = 1ULL << AUDIO_PA_ENABLE, .mode = GPIO_MODE_OUTPUT,
        .pull_up_en = GPIO_PULLUP_DISABLE, .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    ESP_RETURN_ON_ERROR(gpio_config(&pa_cfg), TAG, "speaker PA GPIO");
    ESP_RETURN_ON_ERROR(gpio_set_level(AUDIO_PA_ENABLE, 0), TAG, "speaker PA off");
    ESP_RETURN_ON_ERROR(es8311_init(), TAG, "ES8311 init");
    s_audio_ready = true;
    ESP_LOGI(TAG, "EchoEar audio ready: ES7210 mic + ES8311 speaker at 16kHz");
    return ESP_OK;
}

static uint16_t rgb565(uint8_t r, uint8_t g, uint8_t b) {
    uint16_t logical = (uint16_t)(((r & 0xF8) << 8) |
                                  ((g & 0xFC) << 3) | (b >> 3));
    // The SPI panel consumes the most-significant RGB565 byte first, while an
    // ESP32 stores uint16_t least-significant byte first. Keep framebuffer
    // pixels in wire order, matching SPI_SWAP_DATA_TX(..., 16) in the vendor
    // driver's own ST77916 test. Without this swap, gradients turn into vivid
    // cyan/magenta/green bands even though the geometry remains correct.
    return __builtin_bswap16(logical);
}

// Interpolate in the panel's native RGB565 space. The previous pet renderer
// used only a handful of flat fills, which made a 65K-colour panel look like a
// 16-colour display. Per-scanline interpolation keeps the transfer format and
// memory footprint unchanged while exposing the colour depth that is already
// available in the ST77916 pipeline.
static uint16_t rgb565_lerp(uint16_t from, uint16_t to, uint16_t amount) {
    if (amount > 255) amount = 255;
    // rgb565() stores pixels in wire byte order; interpolate the logical color
    // components and convert the result back to wire order.
    from = __builtin_bswap16(from);
    to = __builtin_bswap16(to);
    uint16_t inv = 255 - amount;
    uint16_t fr = (from >> 11) & 0x1f;
    uint16_t fg = (from >> 5) & 0x3f;
    uint16_t fb = from & 0x1f;
    uint16_t tr = (to >> 11) & 0x1f;
    uint16_t tg = (to >> 5) & 0x3f;
    uint16_t tb = to & 0x1f;
    uint16_t r = (fr * inv + tr * amount + 127) / 255;
    uint16_t g = (fg * inv + tg * amount + 127) / 255;
    uint16_t b = (fb * inv + tb * amount + 127) / 255;
    return __builtin_bswap16((uint16_t)((r << 11) | (g << 5) | b));
}

static uint16_t state_color(const char *state) {
    if (state && !strcmp(state, "listening")) return rgb565(28, 105, 191);
    if (state && !strcmp(state, "thinking")) return rgb565(91, 62, 185);
    if (state && !strcmp(state, "speaking")) return rgb565(0, 145, 113);
    if (state && !strcmp(state, "done")) return rgb565(26, 120, 62);
    if (state && !strcmp(state, "alert")) return rgb565(180, 46, 45);
    if (state && !strcmp(state, "idle")) return rgb565(28, 82, 133);
    return rgb565(18, 30, 48);
}

static void fill_rect(int x0, int y0, int x1, int y1, uint16_t color) {
    if (!s_panel) return;
    if (x0 < 0) x0 = 0;
    if (y0 < 0) y0 = 0;
    if (x1 > LCD_WIDTH) x1 = LCD_WIDTH;
    if (y1 > LCD_HEIGHT) y1 = LCD_HEIGHT;
    if (x0 >= x1 || y0 >= y1) return;
    if (s_render_target) {
        for (int y = y0; y < y1; ++y) {
            uint16_t *row = s_render_target + (size_t)y * LCD_WIDTH + x0;
            for (int x = x0; x < x1; ++x) *row++ = color;
        }
        return;
    }
    for (int y = y0; y < y1; y += LCD_STRIPE_ROWS) {
        int rows = (y1 - y) < LCD_STRIPE_ROWS ? (y1 - y) : LCD_STRIPE_ROWS;
        for (int i = 0; i < (x1 - x0) * rows; ++i) s_line[i] = color;
        ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(x0, y, x1, y + rows, s_line));
        // `draw_bitmap` queues a DMA transaction. The shared stripe buffer
        // must not be refilled until it reaches the panel; otherwise a later
        // stripe overwrites pixels still in flight and leaves a partial pet.
        // The panel API uses polling SPI transactions. Periodically give the
        // scheduler a tick while complex vector art is rendered so Wi-Fi/TLS
        // housekeeping cannot trip the interrupt watchdog.
        if ((++s_draw_transactions & 0x0Fu) == 0) vTaskDelay(1);
    }
}

static void draw_or_compose_bitmap(int x0, int y0, int x1, int y1,
                                   const uint16_t *pixels, bool keyed_overlay) {
    if (!pixels || x0 < 0 || y0 < 0 || x1 > LCD_WIDTH || y1 > LCD_HEIGHT ||
        x0 >= x1 || y0 >= y1) return;
    if (s_render_target) {
        const int width = x1 - x0;
        for (int row = 0; row < y1 - y0; ++row) {
            uint16_t *dst = s_render_target + (size_t)(y0 + row) * LCD_WIDTH + x0;
            const uint16_t *src = pixels + (size_t)row * width;
            if (!keyed_overlay) {
                memcpy(dst, src, (size_t)width * sizeof(uint16_t));
                continue;
            }
            // The clock/calendar buffers are overlays, not opaque panels.
            // Copy only glyph pixels so their rectangular backing storage can
            // never erase the round halo, ears, head or feet underneath it.
            for (int col = 0; col < width; ++col) {
                if (src[col] != AMBIENT_TRANSPARENT_KEY) dst[col] = src[col];
            }
        }
        return;
    }
    ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(x0, y0, x1, y1, pixels));
}
// Compact 5x7 uppercase font. The return value has one bit per row; unknown
// UTF-8 bytes are deliberately rendered as a placeholder instead of corrupting
// the LCD. Chinese/emoji glyphs will be added with a CJK font in the UI phase.
static const uint8_t *glyph(char c) {
    static const uint8_t blank[5] = {0, 0, 0, 0, 0};
    static const uint8_t question[5] = {2, 1, 0x59, 9, 6};
    // The compact clock uses a single vertical colon, not two adjacent ones.
    static const uint8_t colon[5] = {0, 0x24, 0, 0, 0};
    static const uint8_t slash[5] = {0x40, 0x30, 0x0C, 0x03, 0};
    static const uint8_t hyphen[5] = {0x08, 0x08, 0x08, 0x08, 0x08};
    static const uint8_t table[][5] = {
        {0x7E,0x11,0x11,0x11,0x7E}, {0x7F,0x49,0x49,0x49,0x36}, {0x3E,0x41,0x41,0x41,0x22},
        {0x7F,0x41,0x41,0x22,0x1C}, {0x7F,0x49,0x49,0x49,0x41}, {0x7F,0x09,0x09,0x09,0x01},
        {0x3E,0x41,0x49,0x49,0x7A}, {0x7F,0x08,0x08,0x08,0x7F}, {0,0x41,0x7F,0x41,0},
        {0x20,0x40,0x41,0x3F,0x01}, {0x7F,0x08,0x14,0x22,0x41}, {0x7F,0x40,0x40,0x40,0x40},
        {0x7F,0x02,0x0C,0x02,0x7F}, {0x7F,0x04,0x08,0x10,0x7F}, {0x3E,0x41,0x41,0x41,0x3E},
        {0x7F,0x09,0x09,0x09,0x06}, {0x3E,0x41,0x51,0x21,0x5E}, {0x7F,0x09,0x19,0x29,0x46},
        {0x46,0x49,0x49,0x49,0x31}, {0x01,0x01,0x7F,0x01,0x01}, {0x3F,0x40,0x40,0x40,0x3F},
        {0x1F,0x20,0x40,0x20,0x1F}, {0x3F,0x40,0x38,0x40,0x3F}, {0x63,0x14,0x08,0x14,0x63},
        {0x03,0x04,0x78,0x04,0x03}, {0x61,0x51,0x49,0x45,0x43},
    };
    static const uint8_t digits[][5] = {
        {0x3E,0x51,0x49,0x45,0x3E}, {0,0x42,0x7F,0x40,0}, {0x42,0x61,0x51,0x49,0x46},
        {0x21,0x41,0x45,0x4B,0x31}, {0x18,0x14,0x12,0x7F,0x10}, {0x27,0x45,0x45,0x45,0x39},
        {0x3C,0x4A,0x49,0x49,0x30}, {0x01,0x71,0x09,0x05,0x03}, {0x36,0x49,0x49,0x49,0x36},
        {0x06,0x49,0x49,0x29,0x1E},
    };
    if (c >= 'a' && c <= 'z') c = (char)toupper((unsigned char)c);
    if (c >= 'A' && c <= 'Z') return table[c - 'A'];
    if (c >= '0' && c <= '9') return digits[c - '0'];
    if (c == ' ' || c == '\n') return blank;
    if (c == ':') return colon;
    if (c == '/') return slash;
    if (c == '-') return hyphen;
    return question;
}

static void draw_text(int x, int y, const char *text, uint16_t fg, uint16_t bg) {
    if (!s_panel || !text) return;
    size_t len = strlen(text);
    if (len > 25) len = 25;
    int width = (int)len * TEXT_ADVANCE * TEXT_SCALE;
    if (width <= 0) return;
    if (x < 0) x = 0;
    if (x + width > LCD_WIDTH) width = LCD_WIDTH - x;
    int height = 7 * TEXT_SCALE;
    if (s_render_target) {
        for (int row = 0; row < height; ++row) {
            int py = y + row;
            if (py < 0 || py >= LCD_HEIGHT) continue;
            uint16_t *dst = s_render_target + (size_t)py * LCD_WIDTH + x;
            for (int col = 0; col < width; ++col) dst[col] = bg;
        }
        for (size_t index = 0; index < len; ++index) {
            const uint8_t *g = glyph(text[index]);
            int base_x = (int)index * TEXT_ADVANCE * TEXT_SCALE;
            for (int gx = 0; gx < 5; ++gx) {
                for (int gy = 0; gy < 7; ++gy) {
                    if (!(g[gx] & (1u << gy))) continue;
                    for (int sx = 0; sx < TEXT_SCALE; ++sx) {
                        int px = base_x + gx * TEXT_SCALE + sx;
                        if (px < 0 || px >= width) continue;
                        for (int sy = 0; sy < TEXT_SCALE; ++sy) {
                            int py = y + gy * TEXT_SCALE + sy;
                            if (py >= 0 && py < LCD_HEIGHT) {
                                s_render_target[(size_t)py * LCD_WIDTH + x + px] = fg;
                            }
                        }
                    }
                }
            }
        }
        return;
    }
    // s_line is only LCD_STRIPE_ROWS high. Rendering an entire label into it
    // corrupts the heap whenever the label is wider than a few characters.
    for (int strip_y = 0; strip_y < height; strip_y += LCD_STRIPE_ROWS) {
        int rows = (height - strip_y) < LCD_STRIPE_ROWS
                       ? (height - strip_y)
                       : LCD_STRIPE_ROWS;
        size_t pixels = (size_t)width * rows;
        for (size_t i = 0; i < pixels; ++i) s_line[i] = bg;
        for (size_t col = 0; col < len; ++col) {
            const uint8_t *g = glyph(text[col]);
            int base_x = (int)col * TEXT_ADVANCE * TEXT_SCALE;
            for (int gx = 0; gx < 5; ++gx) {
                for (int gy = 0; gy < 7; ++gy) {
                    if (!(g[gx] & (1u << gy))) continue;
                    for (int sx = 0; sx < TEXT_SCALE; ++sx) {
                        int px = base_x + gx * TEXT_SCALE + sx;
                        if (px >= width) continue;
                        for (int sy = 0; sy < TEXT_SCALE; ++sy) {
                            int py = gy * TEXT_SCALE + sy;
                            if (py >= strip_y && py < strip_y + rows) {
                                s_line[(py - strip_y) * width + px] = fg;
                            }
                        }
                    }
                }
            }
        }
        ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(
            x, y + strip_y, x + width, y + strip_y + rows, s_line));
    }
}
static void draw_centered_text(int y, const char *text, uint16_t fg, uint16_t bg) {
    size_t len = text ? strlen(text) : 0;
    if (len > 25) len = 25;
    int width = (int)len * TEXT_ADVANCE * TEXT_SCALE;
    draw_text((LCD_WIDTH - width) / 2, y, text, fg, bg);
}

static const uint32_t *cjk24_rows(uint32_t codepoint) {
    for (size_t i = 0; i < sizeof(s_maclaw_cjk24) / sizeof(s_maclaw_cjk24[0]); ++i) {
        if (s_maclaw_cjk24[i].codepoint == codepoint) return s_maclaw_cjk24[i].rows;
    }
    return NULL;
}

static bool dynamic_glyph_copy(uint32_t codepoint,
                               uint8_t bitmap[DYNAMIC_GLYPH_BYTES]) {
    bool found = false;
    taskENTER_CRITICAL(&s_state_lock);
    for (size_t i = 0; i < DYNAMIC_GLYPH_CACHE_CAPACITY; ++i) {
        if (s_dynamic_glyphs[i].used && s_dynamic_glyphs[i].codepoint == codepoint) {
            s_dynamic_glyphs[i].last_used = ++s_dynamic_glyph_clock;
            memcpy(bitmap, s_dynamic_glyphs[i].bitmap, DYNAMIC_GLYPH_BYTES);
            found = true;
            break;
        }
    }
    taskEXIT_CRITICAL(&s_state_lock);
    return found;
}

int board_port_cache_glyph(uint32_t codepoint, const uint8_t bitmap[DYNAMIC_GLYPH_BYTES]) {
    if (!bitmap || codepoint < 0x20 || codepoint > 0xFFFF ||
        (codepoint >= 0xD800 && codepoint <= 0xDFFF)) return 0;
    size_t replacement = 0;
    uint32_t oldest = UINT32_MAX;
    taskENTER_CRITICAL(&s_state_lock);
    for (size_t i = 0; i < DYNAMIC_GLYPH_CACHE_CAPACITY; ++i) {
        if (s_dynamic_glyphs[i].used && s_dynamic_glyphs[i].codepoint == codepoint) {
            replacement = i;
            oldest = 0;
            break;
        }
        if (!s_dynamic_glyphs[i].used) {
            replacement = i;
            oldest = 0;
            break;
        }
        if (s_dynamic_glyphs[i].last_used < oldest) {
            oldest = s_dynamic_glyphs[i].last_used;
            replacement = i;
        }
    }
    s_dynamic_glyphs[replacement].codepoint = codepoint;
    memcpy(s_dynamic_glyphs[replacement].bitmap, bitmap, DYNAMIC_GLYPH_BYTES);
    s_dynamic_glyphs[replacement].last_used = ++s_dynamic_glyph_clock;
    s_dynamic_glyphs[replacement].used = true;
    // The weather string may already be on screen (from NVS or the same
    // ambient payload). Make the next display frame repaint it with the newly
    // arrived bitmap instead of waiting for some unrelated clock/state change.
    ++s_ambient_revision;
    taskEXIT_CRITICAL(&s_state_lock);
    return 1;
}

static bool glyph24_pixel(uint32_t codepoint, const uint32_t *rows, const uint8_t *dynamic,
                          int row, int col) {
    if (rows) return (rows[row] & (1u << (23 - col))) != 0;
    if (dynamic) return (dynamic[row * 3 + col / 8] & (1u << (7 - (col % 8)))) != 0;
    // Compact degree sign for temperatures. It occupies the upper-left part
    // of the normal 24-dot advance so the following C reads as a single °C
    // unit without requiring another built-in or downloaded glyph.
    if (codepoint == 0x00B0) {
        int dx = col - 4;
        int dy = row - 3;
        int distance = dx * dx + dy * dy;
        return distance >= 6 && distance <= 13;
    }
    return codepoint < 0x80 && row < 14 && col < 10 &&
           (glyph((char)codepoint)[col / 2] & (1u << (row / 2)));
}

static uint32_t utf8_next(const char **cursor) {
    const uint8_t *s = (const uint8_t *)*cursor;
    if (!*s) return 0;
    uint32_t cp;
    size_t count;
    if (s[0] < 0x80) { cp = s[0]; count = 1; }
    else if ((s[0] & 0xE0) == 0xC0 && s[1] && (s[1] & 0xC0) == 0x80) {
        cp = ((s[0] & 0x1F) << 6) | (s[1] & 0x3F); count = 2;
    }
    else if ((s[0] & 0xF0) == 0xE0 && s[1] && s[2] &&
             (s[1] & 0xC0) == 0x80 && (s[2] & 0xC0) == 0x80) {
        cp = ((s[0] & 0x0F) << 12) | ((s[1] & 0x3F) << 6) | (s[2] & 0x3F); count = 3;
    }
    else { cp = '?'; count = 1; }
    *cursor += count;
    return cp;
}

// The 24-dot renderer uses a full cell for Chinese glyphs, but the built-in
// ASCII fallback is only 10 pixels wide. Giving every Latin character a CJK
// cell made provisioning SSIDs look as though a space was inserted between
// each letter and clipped their suffix on the round display.
static int text24_advance(uint32_t codepoint) {
    if (codepoint == ' ') return TEXT24_SPACE_ADVANCE;
    return codepoint < 0x80 ? TEXT24_ASCII_ADVANCE : CJK_ADVANCE;
}

static void draw_text24(int x, int y, const char *text, uint16_t fg, uint16_t bg) {
    if (!s_panel || !text) return;
    uint32_t cps[21] = {0};
    int count = 0;
    const char *cursor = text;
    while (*cursor && count < 21) cps[count++] = utf8_next(&cursor);
    if (!count) return;
    // Measure with the same per-codepoint advance used by wrapping and the
    // status compositor.  The old `count * CJK_ADVANCE` allocation was left
    // over from the CJK-only renderer: it made every ASCII letter occupy a
    // full 24px cell even though the glyph itself is 10px wide.
    int width = 0;
    for (int index = 0; index < count; ++index) width += text24_advance(cps[index]);
    if (width > 0) --width; // do not paint one trailing inter-character pixel
    if (x < 0) x = 0;
    if (x + width > LCD_WIDTH) width = LCD_WIDTH - x;
    if (s_render_target) {
        for (int row = 0; row < CJK_FONT_SIZE; ++row) {
            int py = y + row;
            if (py < 0 || py >= LCD_HEIGHT) continue;
            uint16_t *dst = s_render_target + (size_t)py * LCD_WIDTH + x;
            for (int col = 0; col < width; ++col) dst[col] = bg;
        }
        int pen_x = 0;
        for (int index = 0; index < count; ++index) {
            uint32_t cp = cps[index];
            const uint32_t *rows = cjk24_rows(cp);
            uint8_t dynamic_bitmap[DYNAMIC_GLYPH_BYTES];
            const uint8_t *dynamic = !rows && dynamic_glyph_copy(cp, dynamic_bitmap)
                                         ? dynamic_bitmap : NULL;
            for (int row = 0; row < CJK_FONT_SIZE; ++row) {
                int py = y + row;
                if (py < 0 || py >= LCD_HEIGHT) continue;
                for (int col = 0; col < CJK_FONT_SIZE; ++col) {
                    int px = pen_x + col;
                    if (px >= 0 && px < width && glyph24_pixel(cp, rows, dynamic, row, col)) {
                        s_render_target[(size_t)py * LCD_WIDTH + x + px] = fg;
                    }
                }
            }
            pen_x += text24_advance(cp);
        }
        return;
    }
    // The shared DMA buffer is LCD_WIDTH * LCD_STRIPE_ROWS pixels.  A long
    // 24-dot status line can be wider than 360 pixels, so width * 24 would
    // overflow s_line and produced the bright specks immediately above and
    // below the two text rows.  Render in bounded horizontal strips just like
    // draw_text() does, and clip every glyph to the visible rectangle.
    for (int strip_y = 0; strip_y < CJK_FONT_SIZE; strip_y += LCD_STRIPE_ROWS) {
        int rows_in_strip = (CJK_FONT_SIZE - strip_y) < LCD_STRIPE_ROWS
                                ? (CJK_FONT_SIZE - strip_y)
                                : LCD_STRIPE_ROWS;
        size_t pixels = (size_t)width * rows_in_strip;
        for (size_t i = 0; i < pixels; ++i) s_line[i] = bg;
        int pen_x = 0;
        for (int index = 0; index < count; ++index) {
            uint32_t cp = cps[index];
            const uint32_t *rows = cjk24_rows(cp);
            uint8_t dynamic_bitmap[DYNAMIC_GLYPH_BYTES];
            const uint8_t *dynamic = !rows && dynamic_glyph_copy(cp, dynamic_bitmap)
                                         ? dynamic_bitmap : NULL;
            for (int row = strip_y; row < strip_y + rows_in_strip; ++row) {
                for (int col = 0; col < CJK_FONT_SIZE; ++col) {
					bool set = glyph24_pixel(cp, rows, dynamic, row, col);
                    int px = pen_x + col;
                    if (set && px >= 0 && px < width) {
                        s_line[(row - strip_y) * width + px] = fg;
                    }
                }
            }
            pen_x += text24_advance(cp);
        }
        ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(
            x, y + strip_y, x + width, y + strip_y + rows_in_strip, s_line));
    }
}

static void compose_text24(uint16_t *target, int stride, int width, int y,
                           const char *text, uint16_t fg) {
    if (!target || !text || stride <= 0 || width <= 0 || y < 0 || y + CJK_FONT_SIZE > STATUS_TEXT_H) return;
    const char *cursor = text;
    int pen_x = 0;
    while (*cursor && pen_x < width) {
        uint32_t cp = utf8_next(&cursor);
        const uint32_t *rows = cjk24_rows(cp);
        uint8_t dynamic_bitmap[DYNAMIC_GLYPH_BYTES];
        const uint8_t *dynamic = !rows && dynamic_glyph_copy(cp, dynamic_bitmap)
                                     ? dynamic_bitmap : NULL;
        for (int row = 0; row < CJK_FONT_SIZE; ++row) {
            for (int col = 0; col < CJK_FONT_SIZE; ++col) {
				bool set = glyph24_pixel(cp, rows, dynamic, row, col);
                int px = pen_x + col;
                if (set && px < width) target[(y + row) * stride + px] = fg;
            }
        }
        pen_x += text24_advance(cp);
    }
}

// A compact 16-dot renderer for the ambient header. CJK glyphs are sampled
// from the existing high-quality 24-dot table; ASCII keeps the crisp 5x7
// source at 2x scale. Their advances keep the clock and calendar compact
// enough to remain centred inside the round screen without colliding with Wi-Fi.
static void compose_text16_region(uint16_t *target, int stride, int width, int height,
                                  int x, int y, const char *text, uint16_t fg) {
    if (!target || !text || stride <= 0 || width <= 0 || height <= 0) return;
    const char *cursor = text;
    int pen_x = x;
    while (*cursor) {
        uint32_t cp = utf8_next(&cursor);
        if (cp < 0x80) {
            const uint8_t *g = glyph((char)cp);
            for (int gy = 0; gy < 7; ++gy) {
                for (int gx = 0; gx < 5; ++gx) {
                    if (!(g[gx] & (1u << gy))) continue;
                    for (int sy = 0; sy < 2; ++sy) {
                        int py = y + 1 + gy * 2 + sy;
                        if (py < 0 || py >= height) continue;
                        for (int sx = 0; sx < 2; ++sx) {
                            int px = pen_x + gx * 2 + sx;
                            if (px >= 0 && px < width) target[py * stride + px] = fg;
                        }
                    }
                }
            }
            // ':' is a 5-column glyph just like digits.  Advancing it by
            // only six pixels let the following digit overpaint its right dot,
            // leaving a misleading double-colon mark in the clock.
            pen_x += COMPACT_ASCII_ADVANCE;
            continue;
        }

        const uint32_t *rows = cjk24_rows(cp);
        for (int row = 0; row < COMPACT_FONT_SIZE; ++row) {
            int source_y0 = row * CJK_FONT_SIZE / COMPACT_FONT_SIZE;
            int source_y1 = (row + 1) * CJK_FONT_SIZE / COMPACT_FONT_SIZE;
            if (source_y1 <= source_y0) source_y1 = source_y0 + 1;
            int py = y + row;
            if (py < 0 || py >= height) continue;
            for (int col = 0; col < COMPACT_FONT_SIZE; ++col) {
                int source_x0 = col * CJK_FONT_SIZE / COMPACT_FONT_SIZE;
                int source_x1 = (col + 1) * CJK_FONT_SIZE / COMPACT_FONT_SIZE;
                if (source_x1 <= source_x0) source_x1 = source_x0 + 1;
                bool set = false;
                // Use area coverage rather than a single nearest-neighbour
                // sample. Thin strokes such as the horizontal bars in “日”
                // can otherwise fall between samples and disappear at 16px.
                for (int sy = source_y0; rows && !set && sy < source_y1; ++sy) {
                    for (int sx = source_x0; sx < source_x1; ++sx) {
                        if (rows[sy] & (1u << (23 - sx))) {
                            set = true;
                            break;
                        }
                    }
                }
                int px = pen_x + col;
                if (set && px >= 0 && px < width) target[py * stride + px] = fg;
            }
        }
        pen_x += COMPACT_CJK_ADVANCE;
    }
}

static void utf8_encode(uint32_t cp, char output[5]) {
    memset(output, 0, 5);
    if (cp < 0x80) {
        output[0] = (char)cp;
    } else if (cp < 0x800) {
        output[0] = (char)(0xC0 | (cp >> 6));
        output[1] = (char)(0x80 | (cp & 0x3F));
    } else {
        output[0] = (char)(0xE0 | (cp >> 12));
        output[1] = (char)(0x80 | ((cp >> 6) & 0x3F));
        output[2] = (char)(0x80 | (cp & 0x3F));
    }
}

// Place legible upright glyphs on a circular arc. Rotating the 16px CJK bitmap
// made strokes fragment on this LCD, so the glyphs stay upright while their
// baselines follow the rim. A positive divisor creates a top arc (centre high,
// sides low); a negative divisor creates the matching lower arc.
static void compose_text16_curve(uint16_t *target, int stride, int width, int height,
                                 int center_x, int apex_y, int curve_divisor,
                                 const char *text, uint16_t fg) {
    if (!target || !text || !text[0] || curve_divisor == 0) return;
    uint32_t glyphs[32] = {0};
    int advances[32] = {0};
    int count = 0;
    int total = 0;
    const char *cursor = text;
    while (*cursor && count < (int)(sizeof(glyphs) / sizeof(glyphs[0]))) {
        uint32_t cp = utf8_next(&cursor);
        glyphs[count] = cp;
        advances[count] = cp < 0x80 ? COMPACT_ASCII_ADVANCE : COMPACT_CJK_ADVANCE;
        total += advances[count++];
    }
    int pen = 0;
    for (int i = 0; i < count; ++i) {
        int midpoint = pen + advances[i] / 2 - total / 2;
        int glyph_width = glyphs[i] < 0x80 ? 10 : COMPACT_FONT_SIZE;
        int x = center_x + midpoint - glyph_width / 2;
        int y = apex_y + midpoint * midpoint / curve_divisor;
        char encoded[5];
        utf8_encode(glyphs[i], encoded);
        compose_text16_region(target, stride, width, height, x, y, encoded, fg);
        pen += advances[i];
    }
}

// The lower information ring uses the native 24-dot CJK glyphs so city and
// weather stay readable at arm's length.  Keep glyphs upright; only their
// baselines follow the arc around the pet.
static void compose_text24_curve(uint16_t *target, int stride, int width, int height,
                                 int center_x, int apex_y, int curve_divisor,
                                 const char *text,
                                 uint16_t fg) {
    if (!target || !text || !text[0] || curve_divisor == 0) return;
    uint32_t glyphs[32] = {0};
    int advances[32] = {0};
    int count = 0;
    int total = 0;
    const char *cursor = text;
    while (*cursor && count < (int)(sizeof(glyphs) / sizeof(glyphs[0]))) {
        uint32_t cp = utf8_next(&cursor);
        glyphs[count] = cp;
        // Keep separation between city/weather/temperature, but make it a
        // narrow gap rather than a full 24-dot blank glyph. This leaves room
        // for four-character cities such as “乌鲁木齐” on the same ring.
        // Latin glyphs use the 10-pixel ASCII fallback, not a 24-pixel CJK
        // cell.  The latter made "Buffalo" appear as "B U F F" and consumed
        // the four-glyph city limit before its remaining letters could draw.
        // Keep °C as a unit: a small gap follows the number, then C sits
        // close to the degree mark instead of being spaced like a CJK glyph.
        advances[count] = cp == ' ' ? 8 : cp == 0x00B0 ? 10
                        : cp < 0x80 ? TEXT24_ASCII_ADVANCE : CJK_ADVANCE;
        total += advances[count++];
    }
    int pen = 0;
    for (int i = 0; i < count; ++i) {
        int midpoint = pen + advances[i] / 2 - total / 2;
        // The degree symbol is a compact ASCII-like mark, despite its Unicode
        // codepoint. Center it in a narrow cell so it sits after the number,
        // while the following C remains close enough to read as one unit.
        int glyph_width = (glyphs[i] < 0x80 || glyphs[i] == 0x00B0)
                              ? 10 : CJK_FONT_SIZE;
        int x = center_x + midpoint - glyph_width / 2;
        // Deliberately mirror the upper date/weekday/time calculation. Every
        // glyph in city + weather + temperature participates in one centred
        // parabola; the negative divisor reverses the arc for the lower rim.
        int y = apex_y + midpoint * midpoint / curve_divisor;
        if (y < 1) y = 1;
        if (y > height - CJK_FONT_SIZE - 1) y = height - CJK_FONT_SIZE - 1;
        const uint32_t *rows = cjk24_rows(glyphs[i]);
        uint8_t dynamic_bitmap[DYNAMIC_GLYPH_BYTES];
        const uint8_t *dynamic = !rows && dynamic_glyph_copy(glyphs[i], dynamic_bitmap)
                                     ? dynamic_bitmap : NULL;
        for (int row = 0; row < CJK_FONT_SIZE; ++row) {
            int py = y + row;
            if (py < 0 || py >= height) continue;
            for (int col = 0; col < CJK_FONT_SIZE; ++col) {
				bool set = glyph24_pixel(glyphs[i], rows, dynamic, row, col);
                int px = x + col;
                if (set && px >= 0 && px < width) target[py * stride + px] = fg;
            }
        }
        pen += advances[i];
    }
}

static void copy_utf8_glyphs(char *out, size_t out_size, const char *source, int max_glyphs) {
    if (!out || !out_size) return;
    out[0] = '\0';
    if (!source || max_glyphs <= 0) return;
    const char *cursor = source;
    size_t used = 0;
    for (int i = 0; *cursor && i < max_glyphs; ++i) {
        const char *start = cursor;
        (void)utf8_next(&cursor);
        size_t bytes = (size_t)(cursor - start);
        if (used + bytes >= out_size) break;
        memcpy(out + used, start, bytes);
        used += bytes;
        out[used] = '\0';
    }
}

static void draw_clock_calendar(uint16_t bg) {
    uint16_t primary = rgb565(239, 248, 255);
    char ambient_time[sizeof(s_ambient_time)];
    char ambient_location[sizeof(s_ambient_location)];
    char ambient_date[sizeof(s_ambient_date)];
    char ambient_weekday[sizeof(s_ambient_weekday)];
    char ambient_weather[sizeof(s_ambient_weather)];
    int ambient_temperature_c;
    bool ambient_weather_valid;
    taskENTER_CRITICAL(&s_state_lock);
    memcpy(ambient_time, s_ambient_time, sizeof(ambient_time));
    memcpy(ambient_location, s_ambient_location, sizeof(ambient_location));
    memcpy(ambient_date, s_ambient_date, sizeof(ambient_date));
    memcpy(ambient_weekday, s_ambient_weekday, sizeof(ambient_weekday));
    memcpy(ambient_weather, s_ambient_weather, sizeof(ambient_weather));
    ambient_temperature_c = s_ambient_temperature_c;
    ambient_weather_valid = s_ambient_weather_valid;
    taskEXIT_CRITICAL(&s_state_lock);

    // During full-frame composition these are transparent text overlays. The
    // old opaque background fill created visible rectangular top/bottom masks
    // even though the physical 360 x 360 LCD has no such dead zones. Direct
    // standalone updates retain an opaque background because no backing frame
    // is available in that path.
    const bool keyed_overlay = s_render_target != NULL;
    const uint16_t overlay_bg = keyed_overlay ? AMBIENT_TRANSPARENT_KEY : bg;
    for (size_t i = 0; i < sizeof(s_ambient_top) / sizeof(s_ambient_top[0]); ++i) {
        s_ambient_top[i] = overlay_bg;
    }
    bool clock_valid = ambient_time[0] && strcmp(ambient_time, "--:--:--");
    if (clock_valid) {
        char ring[96];
        // Upper ring is time context only. It ends above y=62 so the pet's
        // ears and head always remain completely untouched.
        snprintf(ring, sizeof(ring), "%s %s %s", ambient_date,
                 ambient_weekday, ambient_time);
        // 36 px of rise across the text makes the circular form unmistakable,
        // while its lowest pixels still finish above the pet's ears.
        compose_text16_curve(s_ambient_top, AMBIENT_TOP_W, AMBIENT_TOP_W, AMBIENT_TOP_H,
                             AMBIENT_TOP_W / 2, 7, 520, ring, primary);
    }
    draw_or_compose_bitmap(12, 13, 12 + AMBIENT_TOP_W, 13 + AMBIENT_TOP_H,
                           s_ambient_top, keyed_overlay);

    // City precedes weather on the matching lower arc. Its physical region
    // starts below the native pet's 96..272 drawing area, so neither text nor
    // background clearing can cut into the pet circle.
    for (size_t i = 0; i < sizeof(s_ambient_bottom) / sizeof(s_ambient_bottom[0]); ++i) {
        s_ambient_bottom[i] = overlay_bg;
    }
    if (ambient_weather_valid && ambient_weather[0]) {
        char location[16];
        char weather[12];
        // Some weather providers omit the optional location field. The display
        // must nevertheless retain the city slot before the weather; use a
        // clear local fallback instead of leaving an invisible leading gap.
        const char *city = ambient_location;
        while (*city == ' ' || *city == '\t') ++city;
        // Four CJK glyphs cover normal long city names (e.g. “乌鲁木齐”),
        // while Latin cities need more characters to remain meaningful.
        copy_utf8_glyphs(location, sizeof(location), city[0] ? city : "本地",
                         (unsigned char)city[0] < 0x80 ? 10 : 4);
        copy_utf8_glyphs(weather, sizeof(weather), ambient_weather, 2);
        char lower_ring[40];
        snprintf(lower_ring, sizeof(lower_ring), "%s %s %d°C", location, weather,
                 ambient_temperature_c);

        // One continuous lower arc encloses the pet: the two ends (city and
        // temperature) rise toward the pet, while the middle (weather) sits
        // lower. Splitting it into two independent arcs made the direction
        // look reversed even when each individual curve had the right sign.
        compose_text24_curve(s_ambient_bottom, AMBIENT_BOTTOM_W, AMBIENT_BOTTOM_W, AMBIENT_BOTTOM_H,
                             // Same algorithm as the upper ring, with the sign
                             // reversed. -397 is another 5% tighter than the
                             // previous -418 arc; apex 35 moves the whole ring
                             // upward by another three pixels.
                             // text broad enough to fit the lower circumference.
                             AMBIENT_BOTTOM_W / 2, 34, -397, lower_ring, primary);
    }
    draw_or_compose_bitmap(
        // At its lowest animation frame the halo ends at y=287. Starting at
        // y=288 gives the rising city glyphs room without ever clearing pet
        // pixels, while still ending exactly at the screen bottom.
        22, 284, 22 + AMBIENT_BOTTOM_W, 284 + AMBIENT_BOTTOM_H,
        s_ambient_bottom, keyed_overlay);
}

// The quiet screen is also a passive pet surface.  Treat it like idle so a
// clock that has already been drawn never appears frozen while the device is
// waiting for its first interaction after boot or reconnect.
static bool ambient_visible_for_state(void) {
    return !strcmp(s_pet_state, "idle") || !strcmp(s_pet_state, "quiet");
}

static void fill_circle(int cx, int cy, int radius, uint16_t color) {
    int dx = radius;
    for (int abs_dy = 0; abs_dy <= radius; ++abs_dy) {
        while (dx > 0 && dx * dx + abs_dy * abs_dy > radius * radius) --dx;
        fill_rect(cx - dx, cy - abs_dy, cx + dx + 1, cy - abs_dy + 1, color);
        if (abs_dy) {
            fill_rect(cx - dx, cy + abs_dy, cx + dx + 1, cy + abs_dy + 1, color);
        }
    }
}

static void fill_circle_vertical_gradient(int cx, int cy, int radius,
                                          uint16_t top, uint16_t bottom) {
    if (radius <= 0) return;
    int dx = radius;
    for (int abs_dy = 0; abs_dy <= radius; ++abs_dy) {
        while (dx > 0 && dx * dx + abs_dy * abs_dy > radius * radius) --dx;
        uint16_t upper_amount = (uint16_t)((radius - abs_dy) * 255 / (radius * 2));
        fill_rect(cx - dx, cy - abs_dy, cx + dx + 1, cy - abs_dy + 1,
                  rgb565_lerp(top, bottom, upper_amount));
        if (abs_dy) {
            uint16_t lower_amount = (uint16_t)((radius + abs_dy) * 255 / (radius * 2));
            fill_rect(cx - dx, cy + abs_dy, cx + dx + 1, cy + abs_dy + 1,
                      rgb565_lerp(top, bottom, lower_amount));
        }
    }
}

static void draw_eye(int cx, int cy, uint16_t dark, uint16_t shine) {
    // A coloured iris, pupil and two highlights read much more naturally than
    // the former single black disc, while remaining crisp on a 360 px display.
    fill_circle_vertical_gradient(cx, cy, 25, rgb565(31, 67, 101), dark);
    fill_circle_vertical_gradient(cx, cy + 2, 16, rgb565(67, 207, 225),
                                  rgb565(21, 91, 145));
    fill_circle(cx, cy + 5, 9, rgb565(10, 24, 42));
    fill_circle(cx + 8, cy - 8, 7, shine);
    fill_circle(cx - 5, cy + 8, 3, rgb565(135, 235, 245));
    fill_rect(cx - 18, cy + 20, cx + 19, cy + 24, dark);
}

static void draw_ear(int x, int y, int dir, uint16_t outer, uint16_t inner) {
    for (int row = 0; row < 55; ++row) {
        int w = 20 + row / 2;
        int left = dir > 0 ? x : x - w;
        fill_rect(left, y + row, left + w, y + row + 1, outer);
        if (row > 13) {
            int inner_w = w - 15;
            int inner_left = dir > 0 ? x + 7 : x - inner_w - 7;
            fill_rect(inner_left, y + row, inner_left + inner_w, y + row + 1, inner);
        }
    }
}

static uint8_t pet_blink_stage(uint32_t motion_tick) {
    // One calm blink about every 5.75 seconds: half-close, close, half-open.
    // Three stages remove the abrupt open/closed flash without keeping the
    // eyes shut long enough to look sleepy.
    uint32_t phase = motion_tick % 72u;
    if (phase == 69u) return 2u;
    if (phase == 68u || phase == 70u) return 1u;
    return 0u;
}

static void ragdoll_tail_offsets(uint32_t motion_tick, int *root, int *mid, int *tip) {
    static const int8_t tail_sway[48] = {
         0,  1,  2,  3,  4,  5,  6,  6,  7,  7,  7,  7,
         7,  7,  7,  6,  6,  5,  4,  3,  2,  1,  1,  0,
         0, -1, -2, -3, -4, -5, -6, -6, -7, -7, -7, -7,
        -7, -7, -7, -6, -6, -5, -4, -3, -2, -1, -1,  0,
    };
    uint32_t phase = motion_tick % 48u;
    if (tip) *tip = tail_sway[phase];
    if (mid) *mid = tail_sway[(phase + 2u) % 48u] * 2 / 3;
    if (root) *root = tail_sway[(phase + 4u) % 48u] / 3;
}

static uint32_t pet_motion_signature(void) {
    uint32_t signature = pet_blink_stage(s_pet_motion_tick);
    // Processing is an active state, not a static acknowledgement. Advance a
    // small three-dot orbit at 320 ms steps so the user can tell that the
    // recorded request is still being handled while network work is pending.
    if (!strcmp(s_pet_state, "thinking")) {
        signature |= 0x10000u | ((s_pet_motion_tick / 4u) & 3u) << 17;
    }
    if (strstr(s_pet_skin, "ragdoll") != NULL) {
        int root = 0, mid = 0, tip = 0;
        ragdoll_tail_offsets(s_pet_motion_tick, &root, &mid, &tip);
        // Offsets are -7..7. Bias them into four-bit fields so equal rendered
        // geometry always produces an equal signature.
        signature |= (uint32_t)(root + 8) << 4;
        signature |= (uint32_t)(mid + 8) << 8;
        signature |= (uint32_t)(tip + 8) << 12;
    }
    return signature;
}

// Compact native rendering for Pearl the Ragdoll.  The Gateway supplies the
// selected pack ID, while a full desktop pack contains PNG/rig assets that
// cannot be streamed to the MCU as-is.  This profile preserves the selected
// pet's recognizable cream coat, seal-point mask, blue eyes, fluffy body and
// curled tail on the small circular display.
static void draw_ragdoll_pet(int bob, uint16_t bg) {
    const uint16_t halo_top = rgb565(222, 187, 255);
    const uint16_t halo_bottom = rgb565(122, 71, 211);
    const uint16_t fur_shadow = rgb565(224, 195, 168);
    const uint16_t seal = rgb565(92, 67, 69);
    const uint16_t pink = rgb565(241, 170, 182);
    const uint16_t blue = rgb565(48, 161, 232);
    const uint16_t blue_dark = rgb565(23, 85, 157);
    const uint16_t shine = rgb565(255, 255, 250);

    // The upper ambient band redraws y=13..66 after the pet. At the former
    // -4 px peak, the ragdoll head reached y=66 and the band replaced its top
    // scanline with background, producing a visible horizontal "haircut".
    // Preserve the breathing motion but keep every ragdoll layer below that
    // independently-owned display region.
    if (bob < -1) bob = -1;

    // A small optical shift gives the ears more air below the upper ring. The
    // transparent ambient overlays now allow the halo and feet to keep their
    // natural curved edges even when they extend behind rim text.
    bob += 4;

    // Keep the complete halo inside the pet-owned y=63..287 band even at the
    // highest/lowest animation offsets. The previous 125 px circle extended
    // beneath both ambient dirty regions, which replaced its top and bottom
    // with straight horizontal chords.
    fill_circle_vertical_gradient(180, PET_HALO_CENTER_Y + bob, PET_HALO_RADIUS,
                                  halo_top, halo_bottom);
    fill_circle_vertical_gradient(180, PET_HALO_CENTER_Y - 11 + bob, 92,
                                  rgb565(237, 216, 255), rgb565(173, 105, 237));
    // A relaxed ~3.8-second sway replaces the former 0/7 px every-frame toggle,
    // which reversed direction about eight times per second and looked like a
    // vibration. The eased endpoints and progressively larger displacement
    // from root to tip make the tail flex instead of translating as one block.
    int tail_root_shift = 0, tail_mid_shift = 0, tail_tip_shift = 0;
    if (s_pet_motion_enabled) {
        ragdoll_tail_offsets(s_pet_motion_tick, &tail_root_shift,
                             &tail_mid_shift, &tail_tip_shift);
    }
    fill_circle_vertical_gradient(244 + tail_root_shift, 224 + bob, 43,
                                  rgb565(126, 96, 99), rgb565(66, 46, 51));
    fill_circle_vertical_gradient(238 + tail_mid_shift, 213 + bob, 30,
                                  rgb565(212, 169, 252), rgb565(143, 82, 222));
    fill_circle_vertical_gradient(246 + tail_tip_shift, 192 + bob, 19,
                                  rgb565(139, 105, 108), rgb565(70, 48, 54));

    // Fluffy seated body and paws.
    fill_circle_vertical_gradient(180, 217 + bob, 73, rgb565(236, 211, 186),
                                  rgb565(194, 158, 131));
    fill_circle_vertical_gradient(180, 210 + bob, 70, rgb565(255, 247, 229),
                                  fur_shadow);
    fill_circle_vertical_gradient(130, 250 + bob, 31, rgb565(255, 244, 225),
                                  fur_shadow);
    fill_circle_vertical_gradient(230, 250 + bob, 31, rgb565(255, 244, 225),
                                  fur_shadow);
    fill_circle(130, 260 + bob, 17, seal);
    fill_circle(230, 260 + bob, 17, seal);
    fill_circle(126, 257 + bob, 5, pink);
    fill_circle(134, 257 + bob, 5, pink);
    fill_circle(226, 257 + bob, 5, pink);
    fill_circle(234, 257 + bob, 5, pink);

    // Head and seal-point ears.  The paired nested triangles read clearly at
    // 360 px while leaving the calendar unobscured.
    fill_circle_vertical_gradient(180, 146 + bob, 75, rgb565(241, 220, 199),
                                  rgb565(197, 164, 139));
    draw_ear(111, 80 + bob, 1, seal, pink);
    draw_ear(249, 80 + bob, -1, seal, pink);
    fill_circle_vertical_gradient(180, 142 + bob, 72, rgb565(255, 249, 234),
                                  rgb565(226, 197, 169));
    fill_circle_vertical_gradient(134, 143 + bob, 31, rgb565(129, 99, 102),
                                  rgb565(66, 46, 51));
    fill_circle_vertical_gradient(226, 143 + bob, 31, rgb565(129, 99, 102),
                                  rgb565(66, 46, 51));
    fill_circle_vertical_gradient(180, 171 + bob, 26, rgb565(132, 101, 103),
                                  rgb565(68, 46, 52));
    fill_circle(180, 165 + bob, 19, fur_shadow);

    // Blink briefly only every few seconds. The old 8-frame loop closed the
    // eyes roughly twice per second, which read as flicker rather than life.
    uint8_t blink_stage = s_pet_motion_enabled ? pet_blink_stage(s_pet_motion_tick) : 0u;
    if (blink_stage == 2u) {
        fill_rect(119, 138 + bob, 149, 144 + bob, blue_dark);
        fill_rect(211, 138 + bob, 241, 144 + bob, blue_dark);
    } else if (blink_stage == 1u) {
        fill_circle(134, 145 + bob, 11, blue_dark);
        fill_circle(226, 145 + bob, 11, blue_dark);
        fill_rect(121, 132 + bob, 148, 142 + bob, rgb565(129, 99, 102));
        fill_rect(213, 132 + bob, 240, 142 + bob, rgb565(129, 99, 102));
    } else {
        fill_circle(134, 142 + bob, 17, blue_dark);
        fill_circle(226, 142 + bob, 17, blue_dark);
        fill_circle(134, 139 + bob, 12, blue);
        fill_circle(226, 139 + bob, 12, blue);
        fill_circle(140, 134 + bob, 5, shine);
        fill_circle(232, 134 + bob, 5, shine);
    }
    fill_circle(180, 167 + bob, 7, pink);
    fill_circle(178, 165 + bob, 2, shine);
    fill_rect(177, 174 + bob, 183, 184 + bob, seal);
    fill_rect(163, 183 + bob, 180, 188 + bob, seal);
    fill_rect(180, 183 + bob, 198, 188 + bob, seal);
    (void)bg;
}

static void draw_pet_frame_contents(bool redraw_background) {
    if (s_display_sleeping) return;
    uint16_t bg = state_color(s_pet_state);
    uint16_t halo_top = rgb565(113, 211, 255);
    uint16_t halo_bottom = rgb565(34, 91, 204);
    bool ragdoll = strstr(s_pet_skin, "ragdoll") != NULL;
    bool mini = strstr(s_pet_skin, "mini") != NULL;
    bool claw = strstr(s_pet_skin, "claw") != NULL;
    bool robot = !claw && !mini;
    uint16_t face = robot ? rgb565(154, 230, 236) : (mini ? rgb565(239, 190, 255) : rgb565(255, 205, 105));
    uint16_t face_light = robot ? rgb565(218, 251, 250) : (mini ? rgb565(255, 230, 255) : rgb565(255, 238, 163));
    uint16_t face_shadow = robot ? rgb565(54, 132, 159) : (mini ? rgb565(183, 112, 222) : rgb565(232, 159, 61));
    uint16_t face_deep = robot ? rgb565(31, 91, 121) : (mini ? rgb565(137, 70, 189) : rgb565(199, 112, 37));
    uint16_t ear = robot ? rgb565(77, 174, 193) : (mini ? rgb565(227, 145, 242) : rgb565(222, 129, 71));
    uint16_t blush = rgb565(244, 139, 122);
    uint16_t ink = rgb565(27, 41, 65);
    uint16_t shine = rgb565(244, 250, 255);
    if (redraw_background) fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    // Keep the pet body anchored. Idle life now comes only from blinking and
    // the independently eased tail; moving the complete silhouette vertically
    // made the character look as if it were bouncing in place.
    int bob = 0;
    if (ragdoll) {
        draw_ragdoll_pet(bob, bg);
        if (ambient_visible_for_state()) draw_clock_calendar(bg);
        return;
    }
    // Keep the pet silhouette beneath the curved time band.  The previous
    // 64 px ear anchor let the animated upward bob enter the header's clear
    // region and visually shave the tops of both ears.
    const int pet_y_offset = 8;
    // The ambient header owns y=8..62 and the lower ring owns y=288..359.
    // Center/radius are shared with the ragdoll renderer so the whole circle
    // remains rounded across every selected pet and every bobbing frame.
    fill_circle_vertical_gradient(180, PET_HALO_CENTER_Y + bob, PET_HALO_RADIUS,
                                  halo_top, halo_bottom);
    // Offset inner light produces a soft dimensional halo without storing or
    // decoding any bitmap asset.
    fill_circle_vertical_gradient(180, PET_HALO_CENTER_Y - 12 + bob, 92,
                                  rgb565(153, 229, 255), rgb565(53, 126, 224));
    draw_ear(88, 64 + pet_y_offset, 1, face_shadow, ear);
    draw_ear(272, 64 + pet_y_offset, -1, face_shadow, ear);
    fill_circle_vertical_gradient(180, 170 + pet_y_offset + bob, 103,
                                  face_shadow, face_deep);
    fill_circle_vertical_gradient(180, 164 + pet_y_offset + bob, 100,
                                  face_light, face);
    // A restrained forehead sheen and lower-face shade add volume while
    // leaving the time and calendar rings visually dominant.
    fill_circle_vertical_gradient(180, 120 + pet_y_offset + bob, 54,
                                  rgb565_lerp(face_light, shine, 110), face_light);
    uint8_t blink_stage = s_pet_motion_enabled ? pet_blink_stage(s_pet_motion_tick) : 0u;
    if (blink_stage == 2u) {
        fill_rect(116, 151 + pet_y_offset + bob, 164, 157 + pet_y_offset + bob, ink);
        fill_rect(196, 151 + pet_y_offset + bob, 244, 157 + pet_y_offset + bob, ink);
    } else if (blink_stage == 1u) {
        fill_rect(116, 147 + pet_y_offset + bob, 164, 157 + pet_y_offset + bob, ink);
        fill_rect(196, 147 + pet_y_offset + bob, 244, 157 + pet_y_offset + bob, ink);
    } else {
        draw_eye(140, 151 + pet_y_offset + bob, ink, shine);
        draw_eye(220, 151 + pet_y_offset + bob, ink, shine);
    }
    fill_circle_vertical_gradient(180, 190 + pet_y_offset + bob, 15,
                                  rgb565(62, 79, 104), ink);
    fill_circle(176, 186 + pet_y_offset + bob, 4, rgb565(180, 205, 222));
    fill_rect(174, 204 + pet_y_offset + bob, 187, 211 + pet_y_offset + bob, ink);
    fill_rect(160, 210 + pet_y_offset + bob, 200, 216 + pet_y_offset + bob, ink);
    fill_circle_vertical_gradient(118, 191 + pet_y_offset + bob, 14,
                                  rgb565(255, 180, 164), blush);
    fill_circle_vertical_gradient(242, 191 + pet_y_offset + bob, 14,
                                  rgb565(255, 180, 164), blush);
    if (!strcmp(s_pet_state, "thinking")) {
        // A restrained orbit above the head is visible without competing with
        // the time band. The changing brightness gives the state an obvious
        // direction even on the small round LCD.
        const int dot_x[3] = {158, 180, 202};
        const int active = (int)((s_pet_motion_tick / 4u) % 3u);
        for (int i = 0; i < 3; ++i) {
            uint16_t dot = i == active ? rgb565(244, 250, 255) : rgb565(142, 190, 255);
            fill_circle(dot_x[i], 82, i == active ? 5 : 3, dot);
        }
    }
    fill_circle_vertical_gradient(180, 236 + pet_y_offset + bob, 39,
                                  rgb565_lerp(face, face_light, 90), face_shadow);
    // The pack identifier is implementation metadata, not the pet visual.
    // Do not show it as a substitute for the selected pet's appearance.
    // The idle pet remains paired with the local clock/calendar, but omits the
    // weather row so its selected MaClaw GUI animation has room to breathe.
    if (ambient_visible_for_state()) draw_clock_calendar(bg);
}

static void draw_pet_frame(bool redraw_background) {
    // A renderer can be selected by the animation task and then wait behind a
    // result/recording transfer for the LCD mutex. Re-check ownership after
    // taking the mutex so that this stale pet frame cannot paint over the
    // newer command surface once the transfer ahead of it has completed.
    if (s_display_sleeping || s_recording_active || s_response_active ||
        s_setup_qrcode_visible ||
        (s_command_display_locked && ambient_visible_for_state())) return;
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    if (s_display_sleeping || s_recording_active || s_response_active ||
        s_setup_qrcode_visible ||
        (s_command_display_locked && ambient_visible_for_state())) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }

    uint16_t *frame = redraw_background ? s_framebuffers[s_next_framebuffer] : NULL;
    if (frame) s_render_target = frame;
    draw_pet_frame_contents(redraw_background);
    s_render_target = NULL;

    if (frame) {
        // The controller exposes the complete 360 x 360 framebuffer; the
        // round shape comes from the panel/bezel, not software dead bands.
        // Present the entire physical framebuffer. Cropping at row 13 left the
        // top band stale, which appeared as colored noise after reset or DMA.
        // ESP32-S3 GDMA can read this DMA-capable PSRAM framebuffer directly.
        // One bounded transfer removes nine CASET/RASET/RAMWR sequences and
        // nine PSRAM-to-internal-RAM copies per animation frame. Waiting for
        // the real color-complete callback keeps the buffer immutable until
        // the panel has consumed it, preventing the corruption seen with the
        // older direct-DMA attempt.
        esp_err_t draw_err = present_frame_sync(frame);
        if (draw_err == ESP_OK) {
            s_next_framebuffer ^= 1u;
            ++s_presented_frames;
        } else {
            ESP_LOGE(TAG, "frame present failed: %s", esp_err_to_name(draw_err));
        }
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

static void draw_pet(void) {
    draw_pet_frame(true);
}

static void draw_recording_visual(void) {
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    // The animation task may have queued this draw just before capture ended.
    // Never allow that old waveform frame to replace the thinking/result page.
    if (!s_recording_active) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    // Compose the complete recording surface off-screen, then present it as
    // one DMA transaction. Direct drawing exposes every clear/bar/text step.
    uint16_t *frame = s_framebuffers[s_next_framebuffer];
    if (frame) s_render_target = frame;
    uint16_t bg = rgb565(10, 19, 30);
    uint16_t red = rgb565(241, 76, 85);
    uint16_t amber = rgb565(244, 178, 58);
    uint16_t cyan = rgb565(72, 205, 220);
    uint16_t muted = rgb565(81, 108, 130);
    uint16_t fg = rgb565(244, 250, 255);
    uint16_t accent = s_recording_paused ? amber : red;
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    fill_rect(16, 16, LCD_WIDTH - 16, 20, accent);
    fill_rect(16, LCD_HEIGHT - 20, LCD_WIDTH - 16, LCD_HEIGHT - 16, accent);
    bool pulse = !s_recording_paused && (s_pet_frame & 1u) == 0;
    fill_circle(62, 62, pulse ? 18 : 12, accent);
    draw_text24(96, 46, s_recording_paused ? "已暂停" : "正在听取", fg, bg);
    uint32_t minutes = s_recording_elapsed_seconds / 60;
    uint32_t seconds = s_recording_elapsed_seconds % 60;
    char elapsed[16];
    snprintf(elapsed, sizeof(elapsed), "%02lu %02lu", (unsigned long)minutes, (unsigned long)seconds);
    draw_centered_text(104, elapsed, fg, bg);

    // Render the same measured audio level as spaced columns instead of a
    // solid min/max fill. EchoEar's codec may report a large DC bias after
    // switching the speaker path; this presentation makes that a bounded
    // meter rather than an opaque cyan block, while still responding to voice.
    uint16_t visual_level = s_recording_paused ? 0 : s_recording_audio_level;
    if (visual_level > 700) visual_level = 700;
    const int wave_left = 26;
    const int wave_width = 308;
    const int wave_center = 220;
    const int wave_half_height = 42;
    fill_rect(wave_left, wave_center, wave_left + wave_width, wave_center + 1, muted);
    for (int column = 0; column < 24; ++column) {
        // Preserve a readable rhythm at quiet levels. The phase offset is
        // deterministic, so the user sees live motion rather than random UI.
        uint16_t variation = (uint16_t)((column * 37u + s_pet_frame * 29u) % 120u);
        uint16_t column_level = visual_level > variation ? visual_level - variation : 0;
        int half = 3 + (int)(column_level * wave_half_height / 700u);
        if (half > wave_half_height) half = wave_half_height;
        int x = wave_left + column * wave_width / 24;
        fill_rect(x, wave_center - half, x + 7, wave_center + half + 1,
                  s_recording_paused ? muted : cyan);
    }
    if (s_recording_is_meeting) {
        draw_text24(98, 266, s_recording_paused ? "会议记录已暂停" : "会议记录进行中",
                    s_recording_paused ? amber : red, bg);
        draw_text24(80, 302, "点屏停止保存", muted, bg);
    } else {
        draw_text24(98, 266, "正在记录命令", cyan, bg);
        draw_text24(80, 302, "说完后自动处理", muted, bg);
    }
    s_render_target = NULL;
    if (frame) {
        esp_err_t draw_err = present_frame_sync(frame);
        if (draw_err == ESP_OK) {
            s_next_framebuffer ^= 1u;
            ++s_presented_frames;
        } else {
            ESP_LOGE(TAG, "recording frame present failed: %s", esp_err_to_name(draw_err));
        }
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

static void pet_animation_task(void *arg) {
    (void)arg;
    uint32_t rendered_ambient_revision = s_ambient_revision;
    uint32_t rendered_motion_signature = pet_motion_signature();
    TickType_t next_frame = xTaskGetTickCount();
    int64_t next_diagnostic_us = esp_timer_get_time() + 5000000;
    while (true) {
        // Keep a stable cadence: render/transfer time must not accumulate on
        // top of the requested frame interval.
        vTaskDelayUntil(&next_frame, pdMS_TO_TICKS(PET_ANIMATION_FRAME_MS));
        // Read the revision after the delay. Reading it at the end of a frame
        // can acknowledge a clock update that arrived while the previous DMA
        // transfer was running even though that second was never rendered.
        uint32_t pending_ambient_revision = s_ambient_revision;
        if (s_setup_qrcode_visible) {
            // The QR code and its white quiet zone must stay pixel-stable for
            // phone cameras. Nothing else should draw while setup is active.
        } else if (s_recording_active) {
            s_pet_frame = (uint8_t)((s_pet_frame + 1u) % 8u);
            draw_recording_visual();
        } else if (s_response_active && s_response_next_page_us > 0 &&
                   esp_timer_get_time() >= s_response_next_page_us) {
            unsigned pages = response_page_count();
            if (pages > 1) {
                s_response_page = (s_response_page + 1) % pages;
                s_response_next_page_us = esp_timer_get_time() + RESPONSE_PAGE_INTERVAL_US;
                draw_response_page();
            } else {
                s_response_next_page_us = 0;
            }
        } else if (!s_command_display_locked && s_ready_prompt_expires_us > 0 &&
                   esp_timer_get_time() >= s_ready_prompt_expires_us) {
            s_ready_prompt_expires_us = 0;
            // The pet profile has already been received from MaClaw GUI. A
            // fresh idle draw removes the temporary instruction text as well.
            strlcpy(s_pet_state, "idle", sizeof(s_pet_state));
            s_idle_pet_visible = true;
            s_idle_pet_sleep_expires_us = esp_timer_get_time() + IDLE_PET_SLEEP_TIMEOUT_US;
            draw_pet();
        } else if (s_idle_pet_visible &&
                   esp_timer_get_time() >= s_idle_pet_sleep_expires_us) {
            s_idle_pet_visible = false;
            s_display_sleeping = true;
            ESP_ERROR_CHECK_WITHOUT_ABORT(esp_lcd_panel_disp_on_off(s_panel, false));
            ESP_ERROR_CHECK_WITHOUT_ABORT(gpio_set_level(LCD_BACKLIGHT, 0));
        } else if (!s_display_sleeping && s_pet_motion_enabled &&
                   (s_idle_pet_visible || !strcmp(s_pet_state, "thinking"))) {
            s_pet_frame = (uint8_t)((s_pet_frame + 1u) % 8u);
            ++s_pet_motion_tick;
            uint32_t motion_signature = pet_motion_signature();
            if (motion_signature != rendered_motion_signature ||
                pending_ambient_revision != rendered_ambient_revision) {
                // Compose only when visible geometry or ambient text changed.
                // The 80 ms motion clock still advances continuously, preserving
                // natural timing while skipping duplicate full-frame work.
                draw_pet_frame(true);
                rendered_motion_signature = motion_signature;
                rendered_ambient_revision = pending_ambient_revision;
            } else {
                ++s_skipped_pet_frames;
            }
        } else if (s_idle_pet_visible && !s_display_sleeping &&
                   rendered_ambient_revision != s_ambient_revision) {
            // A GUI profile may disable motion. Keep its clock ticking while
            // serializing the shared ambient DMA buffers with every other LCD
            // path. Animated frames already hold this recursive mutex through
            // draw_pet_frame(); this static path must acquire it explicitly.
            if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) == pdTRUE) {
                draw_clock_calendar(state_color(s_pet_state));
                xSemaphoreGiveRecursive(s_lcd_mutex);
            }
            rendered_ambient_revision = s_ambient_revision;
        }
        int64_t diagnostic_now_us = esp_timer_get_time();
        if (diagnostic_now_us >= next_diagnostic_us) {
            ESP_LOGI(TAG,
                     "display heartbeat: frames=%lu skipped=%lu ambient=%lu rendered=%lu idle=%s motion=%s sleeping=%s stack=%u",
                     (unsigned long)s_presented_frames,
                     (unsigned long)s_skipped_pet_frames,
                     (unsigned long)s_ambient_revision,
                     (unsigned long)rendered_ambient_revision,
                     s_idle_pet_visible ? "yes" : "no",
                     s_pet_motion_enabled ? "yes" : "no",
                     s_display_sleeping ? "yes" : "no",
                     (unsigned)uxTaskGetStackHighWaterMark(NULL));
            next_diagnostic_us = diagnostic_now_us + 5000000;
        }
        // vTaskDelayUntil() returns immediately if rendering ever overruns its
        // 80 ms budget. Always yield once so the independent one-second clock
        // task and networking cannot be starved by catch-up frames.
        taskYIELD();
    }
}

static bool touch_pressed(void) {
    if (!s_touch) return false;
    // Finger count is more reliable than the event bits when polling: the
    // CST816 may keep the last coordinate/event bytes after a release.
    uint8_t reg = 0x02;
    uint8_t fingers = 0;
    if (i2c_master_transmit_receive(s_touch, &reg, 1, &fingers, 1, 50) != ESP_OK) {
        return false;
    }
    return (fingers & 0x0Fu) != 0;
}

static void button_task(void *arg) {
    (void)arg;
    // EchoEar-2ST exposes BOOT on ESP-IDF GPIO0. The separately labelled
    // PWR/FUNCTION key in Zephyr's gpio1 bank is the board power-control key
    // and does not provide a dependable application GPIO while running.
    // Treat a panel tap as the normal interaction gesture as well, matching
    // the vendor user guide; both inputs feed the same short/double/long logic.
    bool pressed = gpio_get_level(FUNCTION_BUTTON) == 0 || touch_pressed();
    int64_t pressed_at_us = pressed ? esp_timer_get_time() : 0;
    int64_t released_at_us = 0;
    bool long_sent = false;
    bool short_pending = false;
    ESP_LOGI(TAG, "interaction monitor ready: boot_gpio=%d idle_level=%d touch=%s irq=%d",
             FUNCTION_BUTTON, gpio_get_level(FUNCTION_BUTTON), s_touch ? "yes" : "no", TOUCH_IRQ);
    while (true) {
        bool now_pressed = gpio_get_level(FUNCTION_BUTTON) == 0 || touch_pressed();
        int64_t now_us = esp_timer_get_time();
        if (now_pressed != pressed) {
            vTaskDelay(pdMS_TO_TICKS(25));
            now_pressed = gpio_get_level(FUNCTION_BUTTON) == 0 || touch_pressed();
            if (now_pressed != pressed) {
                pressed = now_pressed;
                if (pressed) {
                    pressed_at_us = now_us;
                    long_sent = false;
                    ESP_LOGI(TAG, "button/touch down");
                } else {
                    uint32_t held_ms = pressed_at_us > 0
                                           ? (uint32_t)((now_us - pressed_at_us) / 1000)
                                           : 0;
                    ESP_LOGI(TAG, "button/touch up: held=%lu ms", (unsigned long)held_ms);
                    if (!long_sent && held_ms >= 30) {
                        if (short_pending && now_us - released_at_us <= 500000) {
                            short_pending = false;
                            ESP_LOGI(TAG, "button gesture: double");
                            if (s_on_button) s_on_button(BOARD_BUTTON_DOUBLE, s_on_press_arg);
                        } else {
                            short_pending = true;
                            released_at_us = now_us;
                        }
                    }
                }
            }
        }
        if (pressed && !long_sent && pressed_at_us > 0 &&
            now_us - pressed_at_us >= 3000000) {
            long_sent = true;
            short_pending = false;
            ESP_LOGI(TAG, "button gesture: long");
            if (s_on_button) s_on_button(BOARD_BUTTON_LONG, s_on_press_arg);
        }
        if (!pressed && short_pending && now_us - released_at_us > 500000) {
            short_pending = false;
            ESP_LOGI(TAG, "button gesture: short");
            if (s_on_button) s_on_button(BOARD_BUTTON_SHORT, s_on_press_arg);
        }
        vTaskDelay(pdMS_TO_TICKS(15));
    }
}
esp_err_t board_port_init(board_port_button_cb_t on_button, void *arg) {
    s_on_button = on_button;
    s_on_press_arg = arg;
    s_lcd_mutex = xSemaphoreCreateRecursiveMutex();
    if (!s_lcd_mutex) return ESP_ERR_NO_MEM;
    s_audio_mutex = xSemaphoreCreateMutex();
    if (!s_audio_mutex) return ESP_ERR_NO_MEM;
    s_lcd_transfer_done = xSemaphoreCreateBinary();
    if (!s_lcd_transfer_done) return ESP_ERR_NO_MEM;

    ESP_ERROR_CHECK(gpio_set_direction(LCD_BACKLIGHT, GPIO_MODE_OUTPUT));
    ESP_ERROR_CHECK(gpio_set_level(LCD_BACKLIGHT, 0));
    const spi_bus_config_t bus_config = ST77916_PANEL_BUS_QSPI_CONFIG(
        LCD_SCLK, LCD_DATA0, LCD_DATA1, LCD_DATA2, LCD_DATA3, LCD_FRAMEBUFFER_BYTES);
    ESP_ERROR_CHECK(spi_bus_initialize(LCD_HOST, &bus_config, SPI_DMA_CH_AUTO));
    esp_lcd_panel_io_handle_t io = NULL;
    esp_lcd_panel_io_spi_config_t io_config = ST77916_PANEL_IO_QSPI_CONFIG(
        LCD_CS, lcd_color_transfer_done, s_lcd_transfer_done);
    // ST77916 uses the board's four QSPI data lines for RAMWR pixel payloads.
    // Keep the QSPI macro's quad_mode=true; forcing a one-wire payload while
    // the panel is initialized for QSPI produces the diagonal stripe pattern.
    // This board shares the MSPI fabric between octal PSRAM and the LCD.
    // Direct PSRAM DMA causes SPI TX underflows during full-frame QSPI writes,
    // leaving the last pet frame visible and preventing the setup QR from
    // reaching the panel. Let esp_lcd stage PSRAM buffers through its DMA-safe
    // internal bounce buffer instead.
    io_config.flags.psram_dma_direct = false;
    ESP_ERROR_CHECK(esp_lcd_new_panel_io_spi(LCD_HOST, &io_config, &io));
    s_panel_io = io;
    st77916_vendor_config_t vendor_config = {
        .init_cmds = s_echoear_init_cmds,
        .init_cmds_size = sizeof(s_echoear_init_cmds) / sizeof(s_echoear_init_cmds[0]),
        .flags = {.use_qspi_interface = 1},
    };
    const esp_lcd_panel_dev_config_t panel_config = {
        .reset_gpio_num = LCD_RESET,
        .rgb_ele_order = LCD_RGB_ELEMENT_ORDER_RGB,
        .bits_per_pixel = 16,
        .vendor_config = &vendor_config,
    };
    ESP_ERROR_CHECK(esp_lcd_new_panel_st77916(io, &panel_config, &s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_reset(s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_init(s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_disp_on_off(s_panel, true));
    ESP_ERROR_CHECK(gpio_set_level(LCD_BACKLIGHT, 1));

    for (size_t i = 0; i < 2; ++i) {
        s_framebuffers[i] = heap_caps_malloc(
            LCD_FRAMEBUFFER_BYTES,
            MALLOC_CAP_SPIRAM | MALLOC_CAP_DMA | MALLOC_CAP_8BIT);
        if (!s_framebuffers[i]) {
            ESP_LOGW(TAG, "DMA PSRAM framebuffer %u unavailable; using stripe renderer",
                     (unsigned)i);
            for (size_t j = 0; j < 2; ++j) {
                if (s_framebuffers[j]) heap_caps_free(s_framebuffers[j]);
                s_framebuffers[j] = NULL;
            }
            break;
        }
    }
    if (s_framebuffers[0] && s_framebuffers[1]) {
        ESP_LOGI(TAG, "LCD double buffer ready: 2 x %u bytes in DMA PSRAM, %u ms cadence",
                 (unsigned)LCD_FRAMEBUFFER_BYTES, (unsigned)PET_ANIMATION_FRAME_MS);
    }
    draw_pet();
    BaseType_t pet_task_created = xTaskCreate(pet_animation_task, "maclaw_pet_animation", 6144,
                                               NULL, 2, NULL);
    if (pet_task_created != pdPASS) {
        ESP_LOGE(TAG, "cannot start pet animation task");
        return ESP_ERR_NO_MEM;
    }

    // Initialize the shared I2C bus early so both touch interaction and later
    // microphone capture can use it without reconfiguring GPIO11/GPIO12.
    esp_err_t input_i2c_err = audio_init();
    if (input_i2c_err != ESP_OK) {
        ESP_LOGW(TAG, "touch/audio init deferred: %s", esp_err_to_name(input_i2c_err));
    }

    gpio_config_t button_cfg = {
        .pin_bit_mask = 1ULL << FUNCTION_BUTTON,
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    ESP_ERROR_CHECK(gpio_config(&button_cfg));
    BaseType_t button_task_created = xTaskCreate(button_task, "echoear_button", 3072, NULL, 4,
                                                  NULL);
    if (button_task_created != pdPASS) {
        ESP_LOGE(TAG, "cannot start button task");
        return ESP_ERR_NO_MEM;
    }
    ESP_LOGI(TAG, "EchoEar-2ST ST77916 QSPI display and function button ready");
    return ESP_OK;
}

void board_port_set_pet_state(const char *state) {
    const char *next_state = state ? state : "idle";
    // The provisioning screen is intentionally pixel-stable for phone cameras.
    // A delayed state message must not clear this guard and let a pet frame
    // overwrite the QR code halfway through scanning.
    if (s_setup_qrcode_visible) {
        ESP_LOGD(TAG, "pet state deferred while setup QR is visible: %s", next_state);
        return;
    }
    // The complete command path owns the LCD, not merely the final response.
    // Ignore both remote and locally generated ambient states from capture
    // through thinking/result so Wi-Fi/clock/weather can never appear between
    // two foreground frames.
    bool ambient_state = !strcmp(next_state, "idle") || !strcmp(next_state, "quiet");
    if (ambient_state && (s_command_display_locked || s_response_active)) return;
    if (strcmp(next_state, "speaking")) {
        s_response_active = false;
        s_response_next_page_us = 0;
    }
    strlcpy(s_pet_state, next_state, sizeof(s_pet_state));
    // Idle/quiet are the permanent ambient pet face. Previously every state
    // update cleared s_idle_pet_visible, so the animation task stopped owning
    // refreshes and both pet motion and clock seconds could freeze indefinitely.
    if (!strcmp(next_state, "idle") || !strcmp(next_state, "quiet")) {
        s_display_sleeping = false;
        s_idle_pet_visible = true;
        s_idle_pet_sleep_expires_us = esp_timer_get_time() + IDLE_PET_SLEEP_TIMEOUT_US;
    } else {
        s_idle_pet_visible = false;
        s_idle_pet_sleep_expires_us = 0;
    }
    if (!s_recording_active) draw_pet();
}

void board_port_set_command_display_lock(bool locked) {
    s_command_display_locked = locked;
    if (locked) s_ready_prompt_expires_us = 0;
}

void board_port_set_pet_profile(const char *skin, bool motion_enabled) {
	if (s_command_display_locked) return;
	const char *next_skin = (skin && skin[0]) ? skin : s_pet_skin;
	char normalized_skin[sizeof(s_pet_skin)];
	strlcpy(normalized_skin, next_skin, sizeof(normalized_skin));
	bool skin_changed = false;
	taskENTER_CRITICAL(&s_state_lock);
	skin_changed = strcmp(s_pet_skin, normalized_skin) != 0;
	if (skin_changed) {
		strlcpy(s_pet_skin, normalized_skin, sizeof(s_pet_skin));
		s_pet_frame = 0;
		s_pet_motion_tick = 0;
	}
	taskEXIT_CRITICAL(&s_state_lock);
	if (!skin_changed) {
		ESP_LOGD(TAG, "pet profile unchanged: skin=%s", normalized_skin);
		return;
	}
    // The remote motion flag describes whether the desktop pet pack contains
    // animation assets. This MCU always renders its own native idle motion;
    // treating a missing/false remote asset flag as "freeze the LCD" also
    // stopped full-frame presentation and made the seconds appear frozen.
    s_pet_motion_enabled = true;
	ESP_LOGI(TAG, "pet profile: skin=%s remote_motion=%s native_motion=enabled",
			 normalized_skin, motion_enabled ? "enabled" : "disabled");
    if (!s_recording_active) draw_pet();
}

void board_port_set_recording_visual(bool active, bool paused, uint32_t elapsed_seconds) {
    if (active) s_ready_prompt_expires_us = 0;
    if (active) {
        s_idle_pet_visible = false;
        s_idle_pet_sleep_expires_us = 0;
    }
    s_recording_active = active;
    s_recording_paused = paused;
    s_recording_elapsed_seconds = elapsed_seconds;
    if (!active || elapsed_seconds == 0) {
        taskENTER_CRITICAL(&s_state_lock);
        memset(s_recording_wave_min, 0, sizeof(s_recording_wave_min));
        memset(s_recording_wave_max, 0, sizeof(s_recording_wave_max));
        s_recording_wave_pending_min = INT16_MAX;
        s_recording_wave_pending_max = INT16_MIN;
        s_recording_wave_pending_samples = 0;
        s_recording_wave_dc = 0;
        taskEXIT_CRITICAL(&s_state_lock);
    }
    if (!active) s_recording_audio_level = 0;
    if (active) draw_recording_visual();
    else draw_pet();
}

void board_port_set_audio_level(uint16_t level, uint32_t elapsed_seconds) {
    if (level > 1000) level = 1000;
    s_recording_audio_level = level;
    s_recording_elapsed_seconds = elapsed_seconds;
}

static void recording_wave_push_pcm(const int16_t *samples, size_t count) {
    if (!samples || count == 0) return;
    // Only one capture task owns the pending bucket. Aggregate PCM outside the
    // critical section so I2S is never held behind hundreds of sample compares.
    // A single 8 ms column must remain legible even if the shared I2S bus
    // delivers saturated words while the speaker path changes state.
    int16_t completed_min[8];
    int16_t completed_max[8];
    size_t completed = 0;
    uint32_t amplitude_sum = 0;
    uint16_t usable_samples = 0;
    uint16_t clipped_samples = 0;
    for (size_t i = 0; i < count; ++i) {
        int16_t sample = samples[i];
        int32_t raw_magnitude = sample < 0 ? -(int32_t)sample : sample;
        // Values this close to full scale are transport/clock artefacts on
        // EchoEar's ES7210 path far more often than voice. Do not let them
        // dominate the visual signal. The original PCM is still retained.
        if (raw_magnitude >= 32500) {
            ++clipped_samples;
        } else {
            // A 1/64 low-pass baseline follows the analogue DC level but is
            // far too slow to follow speech. This makes silence a thin line
            // and spoken sound the visible changing envelope.
            s_recording_wave_dc += ((int32_t)sample - s_recording_wave_dc) / 64;
            int32_t deviation = (int32_t)sample - s_recording_wave_dc;
            uint32_t magnitude = deviation < 0 ? (uint32_t)-deviation : (uint32_t)deviation;
            amplitude_sum += (uint32_t)magnitude;
            ++usable_samples;
        }
        ++s_recording_wave_pending_samples;
        if (s_recording_wave_pending_samples >= RECORDING_WAVE_SAMPLES_PER_COLUMN) {
            if (completed < sizeof(completed_min) / sizeof(completed_min[0])) {
                // Discard a predominantly clipped bucket. Otherwise use a
                // gated mean absolute amplitude, which is naturally smoother
                // than min/max yet still follows consonants and speech rhythm.
                uint32_t mean = usable_samples ? amplitude_sum / usable_samples : 0;
                if (clipped_samples > RECORDING_WAVE_SAMPLES_PER_COLUMN / 4 || mean <= 180) mean = 0;
                if (mean > 9000) mean = 9000;
                completed_min[completed] = -(int16_t)mean;
                completed_max[completed] = (int16_t)mean;
                ++completed;
            }
            s_recording_wave_pending_min = INT16_MAX;
            s_recording_wave_pending_max = INT16_MIN;
            s_recording_wave_pending_samples = 0;
            amplitude_sum = 0;
            usable_samples = 0;
            clipped_samples = 0;
        }
    }
    if (completed == 0) return;
    if (completed > RECORDING_WAVE_COLUMNS) completed = RECORDING_WAVE_COLUMNS;
    taskENTER_CRITICAL(&s_state_lock);
    size_t retained = RECORDING_WAVE_COLUMNS - completed;
    memmove(&s_recording_wave_min[0], &s_recording_wave_min[completed],
            retained * sizeof(s_recording_wave_min[0]));
    memmove(&s_recording_wave_max[0], &s_recording_wave_max[completed],
            retained * sizeof(s_recording_wave_max[0]));
    memcpy(&s_recording_wave_min[retained], completed_min,
           completed * sizeof(completed_min[0]));
    memcpy(&s_recording_wave_max[retained], completed_max,
           completed * sizeof(completed_max[0]));
    taskEXIT_CRITICAL(&s_state_lock);
}

void board_port_show_text(const char *title, const char *text) {
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    uint16_t bg = state_color(s_pet_state);
    // The 24-dot CJK font is sized for the physical 360-pixel round display.
    // Keep the two lines inside the lower safe area masked by the enclosure.
    fill_rect(18, 246, 342, 326, bg);
    // Compose both 24-dot lines into one dedicated buffer and submit one contiguous
    // transfer. Besides clearing all padding between/around the glyphs, this
    // keeps the queued DMA source stable until no later status draw can reuse
    // it (the LCD mutex serializes subsequent updates).
    for (size_t i = 0; i < sizeof(s_status_text) / sizeof(s_status_text[0]); ++i) {
        s_status_text[i] = bg;
    }
    compose_text24(s_status_text, STATUS_TEXT_W, STATUS_TEXT_W, 0,
                   title ? title : "码卡龙", rgb565(255, 255, 255));
    compose_text24(s_status_text, STATUS_TEXT_W, STATUS_TEXT_W, STATUS_TEXT_GAP,
                   text ? text : "", rgb565(220, 235, 255));
    ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(
        STATUS_TEXT_X, STATUS_TEXT_Y,
        STATUS_TEXT_X + STATUS_TEXT_W, STATUS_TEXT_Y + STATUS_TEXT_H,
        s_status_text));
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "%s: %s", title ? title : "MaClaw", text ? text : "");
}

void board_port_show_qrcode(esp_qrcode_handle_t qrcode, const char *ssid) {
    if (!s_panel || !qrcode) return;
    const int size = esp_qrcode_get_size(qrcode);
    const int quiet_zone = 4;
    // Reserve the lower safe area for compact instructions on the round LCD.
    const int available = 228;
    const int module = available / (size + quiet_zone * 2);
    if (size <= 0 || module < 2) {
        ESP_LOGW(TAG, "QR code is too large for display: %d modules", size);
        return;
    }
    const int qr_pixels = (size + quiet_zone * 2) * module;
    const int x0 = (LCD_WIDTH - qr_pixels) / 2;
    const int y0 = 12;
    const uint16_t page_bg = state_color("quiet");
    const uint16_t white = rgb565(255, 255, 255);
    const uint16_t black = rgb565(0, 0, 0);

    s_ready_prompt_expires_us = 0;
    s_display_sleeping = false;
    s_idle_pet_visible = false;
    s_idle_pet_sleep_expires_us = 0;
    // Set this before drawing so the animation task cannot paint a pet frame
    // between QR stripes on the shared LCD.
    s_setup_qrcode_visible = true;
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, page_bg);
    // A white quiet zone is required for reliable recognition by WeChat and
    // phone cameras, especially when the device is held at an angle. Compose
    // each LCD stripe once rather than issuing one SPI transfer per black QR
    // module; a version-3 code would otherwise perform hundreds of transfers
    // and leave the provisioning screen visibly slow to appear.
    for (int strip_y = 0; strip_y < qr_pixels; strip_y += LCD_STRIPE_ROWS) {
        const int rows = (qr_pixels - strip_y) < LCD_STRIPE_ROWS
                             ? (qr_pixels - strip_y)
                             : LCD_STRIPE_ROWS;
        for (int py = 0; py < rows; ++py) {
            const int module_y = (strip_y + py) / module - quiet_zone;
            for (int px = 0; px < qr_pixels; ++px) {
                const int module_x = px / module - quiet_zone;
                const bool black_module = module_x >= 0 && module_x < size &&
                                          module_y >= 0 && module_y < size &&
                                          esp_qrcode_get_module(qrcode, module_x, module_y);
                s_line[py * qr_pixels + px] = black_module ? black : white;
            }
        }
        ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(
            x0, y0 + strip_y, x0 + qr_pixels, y0 + strip_y + rows, s_line));
        // The shared stripe buffer must remain stable until the queued transfer
        // reaches the display, before the next stripe overwrites it.
        if ((++s_draw_transactions & 0x0Fu) == 0) vTaskDelay(1);
    }
    fill_rect(18, 292, 342, 350, page_bg);
    for (size_t i = 0; i < sizeof(s_status_text) / sizeof(s_status_text[0]); ++i) {
        s_status_text[i] = page_bg;
    }
    compose_text24(s_status_text, STATUS_TEXT_W, STATUS_TEXT_W, 0,
                   "微信扫码加入热点", rgb565(255, 255, 255));
    char instruction[40];
    snprintf(instruction, sizeof(instruction), "热点 %s", ssid ? ssid : "");
    compose_text24(s_status_text, STATUS_TEXT_W, STATUS_TEXT_W, STATUS_TEXT_GAP,
                   instruction, rgb565(220, 235, 255));
    ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(
        STATUS_TEXT_X, STATUS_TEXT_Y,
        STATUS_TEXT_X + STATUS_TEXT_W, STATUS_TEXT_Y + STATUS_TEXT_H,
        s_status_text));
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "showing setup Wi-Fi QR for %s", ssid ? ssid : "");
}

void board_port_set_wifi_status(const char *ssid, bool connected) {
    if (!s_panel || s_recording_active || s_setup_qrcode_visible || s_command_display_locked) return;
    // Wi-Fi is transport state, not command UI. Never overlay it on the
    // thinking/result states, where it resembles a transition to startup.
    if (strcmp(s_pet_state, "idle") && strcmp(s_pet_state, "quiet")) return;
    // This indicator is cosmetic. Never let it hold Wi-Fi startup behind a
    // full animated frame; the next status event can paint it instead.
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, pdMS_TO_TICKS(40)) != pdTRUE) {
        ESP_LOGI(TAG, "Wi-Fi indicator deferred: %s", connected ? "connected" : "connecting");
        return;
    }
    uint16_t bg = state_color(s_pet_state);
    uint16_t signal = connected ? rgb565(82, 220, 146) : rgb565(245, 177, 76);
    uint16_t muted = rgb565(167, 189, 208);
    // Reserved status corner: update only this small rectangle, never redraw
    // the pet, so connection retries cannot produce a visible full-screen flash.
    fill_rect(242, 14, 350, 52, bg);
    fill_rect(250, 40, 258, 46, signal);
    fill_rect(262, 33, 270, 46, signal);
    fill_rect(274, 25, 282, 46, signal);
    draw_text(290, 24, connected ? "WIFI" : "WIFI?", signal, bg);
    if (!connected && ssid && ssid[0]) {
        fill_rect(246, 53, 346, 56, muted);
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "Wi-Fi indicator: %s (%s)", connected ? "connected" : "connecting", ssid ? ssid : "");
}

void board_port_set_ambient(const char *time, const char *location, const char *date, const char *weekday,
                            const char *weather_summary, int temperature_c,
                            bool weather_valid, bool weather_stale) {
    const char *next_time = time ? time : "";
    const char *next_location = location ? location : "";
    const char *next_date = date ? date : "";
    const char *next_weekday = weekday ? weekday : "";
    const char *next_weather = weather_summary ? weather_summary : "";
    taskENTER_CRITICAL(&s_state_lock);
    bool top_changed = strcmp(s_ambient_time, next_time) != 0 ||
                       strcmp(s_ambient_date, next_date) != 0 ||
                       strcmp(s_ambient_weekday, next_weekday) != 0;
    bool bottom_changed = strcmp(s_ambient_location, next_location) != 0 ||
                          strcmp(s_ambient_weather, next_weather) != 0 ||
                          s_ambient_temperature_c != temperature_c ||
                          s_ambient_weather_valid != weather_valid ||
                          s_ambient_weather_stale != weather_stale;
    strlcpy(s_ambient_time, next_time, sizeof(s_ambient_time));
    strlcpy(s_ambient_location, next_location, sizeof(s_ambient_location));
    strlcpy(s_ambient_date, next_date, sizeof(s_ambient_date));
    strlcpy(s_ambient_weekday, next_weekday, sizeof(s_ambient_weekday));
    strlcpy(s_ambient_weather, next_weather, sizeof(s_ambient_weather));
    s_ambient_temperature_c = temperature_c;
    s_ambient_weather_valid = weather_valid;
    s_ambient_weather_stale = weather_stale;
    if (top_changed || bottom_changed) ++s_ambient_revision;
    bool idle_pet_visible = s_idle_pet_visible;
    bool display_sleeping = s_display_sleeping;
    bool recording_active = s_recording_active;
    taskEXIT_CRITICAL(&s_state_lock);
    // Animated idle frames already redraw the complete pet and both ambient
    // rings. Let that task coalesce the update instead of issuing a competing
    // LCD transfer from the once-per-second clock task.
    if (idle_pet_visible || s_setup_qrcode_visible || s_command_display_locked) return;
    if ((top_changed || bottom_changed) && !display_sleeping && !recording_active &&
        ambient_visible_for_state()) {
        if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
        draw_clock_calendar(state_color(s_pet_state));
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    }
}

void board_port_show_ready_prompt(const char *title, const char *text) {
    // The ready state uses the current GUI-configured pet as its background;
    // only the brief action hint is transient.
    s_display_sleeping = false;
    ESP_ERROR_CHECK_WITHOUT_ABORT(esp_lcd_panel_disp_on_off(s_panel, true));
    ESP_ERROR_CHECK_WITHOUT_ABORT(gpio_set_level(LCD_BACKLIGHT, 1));
    board_port_set_pet_state("idle");
    board_port_show_text(title, text);
    s_ready_prompt_expires_us = esp_timer_get_time() + READY_PROMPT_TIMEOUT_US;
}

void board_port_cancel_ready_prompt(void) {
    s_ready_prompt_expires_us = 0;
}

bool board_port_wake_from_idle(void) {
    if (!s_display_sleeping) return false;
    s_display_sleeping = false;
    ESP_ERROR_CHECK_WITHOUT_ABORT(esp_lcd_panel_disp_on_off(s_panel, true));
    ESP_ERROR_CHECK_WITHOUT_ABORT(gpio_set_level(LCD_BACKLIGHT, 1));
    s_idle_pet_visible = false;
    s_idle_pet_sleep_expires_us = 0;
    return true;
}

esp_err_t board_port_audio_stream_start(void) {
    board_port_pause_wake_word(true);
    if (!s_audio_mutex || xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(1500)) != pdTRUE) {
        board_port_pause_wake_word(false);
        return ESP_ERR_TIMEOUT;
    }
    esp_err_t err = audio_init();
    if (err != ESP_OK) {
        xSemaphoreGive(s_audio_mutex);
        board_port_pause_wake_word(false);
    }
    return err;
}

esp_err_t board_port_audio_stream_read(int16_t *mono, size_t sample_capacity,
                                       size_t *samples_read, uint16_t *level) {
    if (samples_read) *samples_read = 0;
    if (level) *level = 0;
    if (!mono || !samples_read || sample_capacity == 0) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(audio_init(), TAG, "microphone init failed");
    int16_t stereo[512];
    size_t received = 0;
    esp_err_t err = i2s_channel_read(s_audio_rx, stereo, sizeof(stereo), &received,
                                     pdMS_TO_TICKS(1000));
    if (err != ESP_OK) return err;
    size_t frames = received / (sizeof(int16_t) * 2);
    if (frames > sample_capacity) frames = sample_capacity;
    int32_t chunk_peak = 0;
    for (size_t i = 0; i < frames; ++i) {
        int32_t left = stereo[i * 2];
        int32_t right = stereo[i * 2 + 1];
        int32_t left_abs = left < 0 ? -left : left;
        int32_t right_abs = right < 0 ? -right : right;
        int32_t sample = left_abs >= right_abs ? left : right;
        mono[i] = (int16_t)sample;
        int32_t magnitude = sample < 0 ? -sample : sample;
        if (magnitude > chunk_peak) chunk_peak = magnitude;
    }
    uint32_t scaled = chunk_peak <= 180 ? 0 : (uint32_t)(chunk_peak - 180) * 1000u / (12000u - 180u);
    if (scaled > 1000) scaled = 1000;
    if (level) *level = (uint16_t)scaled;
    recording_wave_push_pcm(mono, frames);
    *samples_read = frames;
    return ESP_OK;
}

void board_port_audio_stream_stop(void) {
    // Keep I2S enabled for the normal six-second command path. The next stream
    // read drains any DMA frames accumulated while a meeting was paused.
    if (s_audio_mutex) xSemaphoreGive(s_audio_mutex);
    board_port_pause_wake_word(false);
}

void board_port_pause_wake_word(bool paused) {
    s_wake_word_paused = paused;
}

static bool response_break(uint32_t cp) {
    return cp == '\n' || cp == '\r';
}

void board_port_set_recording_mode(bool meeting) {
    s_recording_is_meeting = meeting;
}

// Return the byte pointer after one wrapped line.  Wrapping is based on the
// same variable advances used by the renderer, so Chinese, Latin and numbers
// share a line naturally instead of being cut by an arbitrary byte count.
static const char *response_next_line(const char *cursor, char *line, size_t line_size) {
    if (!line || line_size == 0) return cursor;
    line[0] = '\0';
    if (!cursor) return cursor;
    int width = 0;
    size_t used = 0;
    while (*cursor) {
        const char *before = cursor;
        uint32_t cp = utf8_next(&cursor);
        if (response_break(cp)) {
            while (*cursor == '\n' || *cursor == '\r') ++cursor;
            break;
        }
        int advance = text24_advance(cp);
        size_t bytes = (size_t)(cursor - before);
        if (width && width + advance > RESPONSE_TEXT_W) {
            cursor = before;
            break;
        }
        if (used + bytes >= line_size) {
            cursor = before;
            break;
        }
        memcpy(line + used, before, bytes);
        used += bytes;
        line[used] = '\0';
        width += advance;
        if (width + TEXT24_ASCII_ADVANCE > RESPONSE_TEXT_W) break;
    }
    // Invalid or unusually wide input must always make progress.
    if (!line[0] && *cursor) {
        const char *before = cursor;
        (void)utf8_next(&cursor);
        size_t bytes = (size_t)(cursor - before);
        if (bytes < line_size) {
            memcpy(line, before, bytes);
            line[bytes] = '\0';
        }
    }
    return cursor;
}

static unsigned response_page_count(void) {
    const char *cursor = s_response_text;
    unsigned lines = 0;
    char line[96];
    while (*cursor && lines < 96) {
        cursor = response_next_line(cursor, line, sizeof(line));
        ++lines;
    }
    return lines ? (lines + RESPONSE_LINES_PER_PAGE - 1) / RESPONSE_LINES_PER_PAGE : 1;
}

static void draw_response_page(void) {
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    // A timed page turn can already be waiting when the next recording starts.
    // Check after serialization, otherwise the old answer can cover that newer
    // foreground screen in exactly the same way as a stale thinking frame.
    if (!s_response_active || s_recording_active || s_setup_qrcode_visible) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    uint16_t bg = state_color(s_pet_state);
    uint16_t title_color = rgb565(255, 255, 255);
    uint16_t body_color = rgb565(220, 235, 255);
    uint16_t rule = rgb565(117, 202, 177);
    // A reply is a dedicated reading surface.  Drawing it over the pet made
    // the face compete with the glyphs and, when pages changed, left the old
    // line positions visible.  A full repaint is inexpensive here because it
    // occurs only on arrival and at the three-second page boundary.
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    fill_rect(RESPONSE_TEXT_X, 68, LCD_WIDTH - RESPONSE_TEXT_X, 70, rule);
    char title[64];
    snprintf(title, sizeof(title), "%s", s_response_title[0] ? s_response_title : "码卡龙");
    draw_text24(RESPONSE_TEXT_X, 28, title, title_color, bg);
    const char *cursor = s_response_text;
    unsigned skip = s_response_page * RESPONSE_LINES_PER_PAGE;
    char line[96];
    while (*cursor && skip--) cursor = response_next_line(cursor, line, sizeof(line));
    for (int row = 0; row < RESPONSE_LINES_PER_PAGE && *cursor; ++row) {
        cursor = response_next_line(cursor, line, sizeof(line));
        draw_text24(RESPONSE_TEXT_X, RESPONSE_TEXT_Y + row * RESPONSE_LINE_GAP,
                    line, body_color, bg);
    }
    unsigned pages = response_page_count();
    if (pages > 1) {
        char indicator[16];
        snprintf(indicator, sizeof(indicator), "%u/%u", s_response_page + 1, pages);
        draw_text24(292, 310, indicator, rgb565(180, 211, 230), bg);
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

void board_port_show_response(const char *title, const char *text) {
    s_ready_prompt_expires_us = 0;
    // Enter the result state without calling board_port_set_pet_state(). That
    // public setter paints a complete pet frame immediately; doing so just
    // before this page produced a visible boot/idle-looking flash between the
    // thinking screen and every streamed response message.
    strlcpy(s_pet_state, "speaking", sizeof(s_pet_state));
    s_idle_pet_visible = false;
    s_idle_pet_sleep_expires_us = 0;
    s_display_sleeping = false;
    s_response_active = true;
    s_response_page = 0;
    strlcpy(s_response_title, title && title[0] ? title : "码卡龙", sizeof(s_response_title));
    strlcpy(s_response_text, text && text[0] ? text : "没有收到文字回复", sizeof(s_response_text));
    s_response_next_page_us = esp_timer_get_time() + RESPONSE_PAGE_INTERVAL_US;
    draw_response_page();
    ESP_LOGI(TAG, "response: %s", s_response_text);
}

static void wake_word_task(void *arg) {
    (void)arg;
    srmodel_list_t *models = esp_srmodel_init("model");
    if (!models) {
        ESP_LOGE(TAG, "offline wake disabled: cannot load ESP-SR model partition");
        goto finish;
    }

    char *model_name = esp_srmodel_filter(models, ESP_MN_PREFIX, ESP_MN_CHINESE);
    if (!model_name) {
        ESP_LOGE(TAG, "offline wake disabled: Chinese MultiNet model not found");
        esp_srmodel_deinit(models);
        goto finish;
    }
    esp_mn_iface_t *multinet = esp_mn_handle_from_name(model_name);
    if (!multinet) {
        ESP_LOGE(TAG, "offline wake disabled: unsupported model %s", model_name);
        esp_srmodel_deinit(models);
        goto finish;
    }
    model_iface_data_t *model_data = multinet->create(model_name, 4000);
    if (!model_data) {
        ESP_LOGE(TAG, "offline wake disabled: cannot create model %s", model_name);
        esp_srmodel_deinit(models);
        goto finish;
    }
    esp_err_t command_err = esp_mn_commands_alloc(multinet, model_data);
    if (command_err == ESP_OK) command_err = esp_mn_commands_add(WAKE_WORD_COMMAND_ID, WAKE_WORD_CN_PHONETIC);
    esp_mn_error_t *command_errors = command_err == ESP_OK ? esp_mn_commands_update() : NULL;
    if (command_err != ESP_OK || command_errors != NULL) {
        ESP_LOGE(TAG, "offline wake disabled: word '%s' is not accepted (err=%s, rejected=%d)",
                 WAKE_WORD_CN_LABEL, esp_err_to_name(command_err), command_errors ? command_errors->num : 0);
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }
    if (multinet->set_det_threshold) {
        int threshold_err = multinet->set_det_threshold(model_data, WAKE_WORD_DETECTION_THRESHOLD);
        if (threshold_err != 0) {
            ESP_LOGW(TAG, "offline wake threshold %.2f was not applied: %d",
                     (double)WAKE_WORD_DETECTION_THRESHOLD, threshold_err);
        }
    }
    const int chunk_samples = multinet->get_samp_chunksize(model_data);
    const int sample_rate = multinet->get_samp_rate(model_data);
    if (chunk_samples <= 0 || sample_rate != AUDIO_RATE) {
        ESP_LOGE(TAG, "offline wake disabled: model audio format is %d Hz / %d samples", sample_rate, chunk_samples);
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }

    int16_t *mono = heap_caps_malloc((size_t)chunk_samples * sizeof(int16_t),
                                     MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    int16_t *stereo = heap_caps_malloc((size_t)chunk_samples * 2 * sizeof(int16_t),
                                       MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!mono || !stereo) {
        ESP_LOGE(TAG, "offline wake disabled: no memory for %d-sample audio buffers", chunk_samples);
        heap_caps_free(mono);
        heap_caps_free(stereo);
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }

    ESP_LOGI(TAG, "offline wake listening: model=%s word='%s' threshold=%.2f rate=%d chunk=%d",
             model_name, WAKE_WORD_CN_LABEL,
             (double)WAKE_WORD_DETECTION_THRESHOLD, sample_rate, chunk_samples);
    multinet->print_active_speech_commands(model_data);
    bool model_was_paused = false;
    int64_t last_detection_us = 0;
    while (true) {
        if (s_wake_word_stop_requested) break;
        if (s_wake_word_paused) {
            if (!model_was_paused) {
                multinet->clean(model_data);
                model_was_paused = true;
            }
            vTaskDelay(pdMS_TO_TICKS(20));
            continue;
        }
        model_was_paused = false;
        if (!s_audio_mutex || xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(50)) != pdTRUE) {
            continue;
        }
        size_t received = 0;
        esp_err_t read_err = i2s_channel_read(
            s_audio_rx, stereo, (size_t)chunk_samples * 2 * sizeof(int16_t),
            &received, pdMS_TO_TICKS(250));
        xSemaphoreGive(s_audio_mutex);
        if (read_err != ESP_OK) {
            if (read_err != ESP_ERR_TIMEOUT) {
                ESP_LOGW(TAG, "offline wake microphone read failed: %s", esp_err_to_name(read_err));
            }
            continue;
        }

        size_t frames = received / (sizeof(int16_t) * 2);
        if (frames < (size_t)chunk_samples) {
            memset(stereo + frames * 2, 0,
                   ((size_t)chunk_samples - frames) * 2 * sizeof(int16_t));
        }
        for (int i = 0; i < chunk_samples; ++i) {
            int32_t left = stereo[i * 2];
            int32_t right = stereo[i * 2 + 1];
            int32_t left_abs = left < 0 ? -left : left;
            int32_t right_abs = right < 0 ? -right : right;
            mono[i] = (int16_t)(left_abs >= right_abs ? left : right);
        }

        esp_mn_state_t state = multinet->detect(model_data, mono);
        // MultiNet is compute-heavy and this task is pinned to CPU1. Yield once
        // per inference chunk so IDLE1 can feed the task watchdog and service
        // low-priority system work without affecting the audio cadence.
        vTaskDelay(1);
        if (state == ESP_MN_STATE_TIMEOUT) { multinet->clean(model_data); continue; }
        if (state != ESP_MN_STATE_DETECTED) continue;
        esp_mn_results_t *result = multinet->get_results(model_data);
        if (!result || result->num == 0 || result->command_id[0] != WAKE_WORD_COMMAND_ID) continue;
        int64_t now_us = esp_timer_get_time();
        if (now_us - last_detection_us >= WAKE_WORD_COOLDOWN_US) {
            last_detection_us = now_us;
            ESP_LOGI(TAG, "offline wake word detected: %s (prob=%.3f)",
                     WAKE_WORD_CN_LABEL, (double)result->prob[0]);
            board_port_wake_word_cb_t callback = s_on_wake_word;
            void *callback_arg = s_on_wake_word_arg;
            multinet->clean(model_data);
            if (callback) callback(callback_arg);
        } else {
            multinet->clean(model_data);
        }
    }

    heap_caps_free(mono);
    heap_caps_free(stereo);
    multinet->destroy(model_data);
    esp_srmodel_deinit(models);
    ESP_LOGI(TAG, "offline wake stopped and model memory released");

finish:
    taskENTER_CRITICAL(&s_wake_word_lock);
    s_wake_word_stop_requested = false;
    s_wake_word_task = NULL;
    taskEXIT_CRITICAL(&s_wake_word_lock);
    vTaskDelete(NULL);
}

esp_err_t board_port_start_wake_word(board_port_wake_word_cb_t on_wake, void *arg) {
    if (!on_wake) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(audio_init(), TAG, "offline wake microphone init failed");
    // Reserve the slot before creating the task. Otherwise two callers can
    // both observe NULL and instantiate process-global MultiNet state twice.
    TaskHandle_t caller = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_wake_word_lock);
    if (s_wake_word_task) {
        taskEXIT_CRITICAL(&s_wake_word_lock);
        return ESP_ERR_INVALID_STATE;
    }
    s_wake_word_task = caller;
    s_on_wake_word = on_wake;
    s_on_wake_word_arg = arg;
    s_wake_word_paused = false;
    s_wake_word_stop_requested = false;
    taskEXIT_CRITICAL(&s_wake_word_lock);
    TaskHandle_t task = NULL;
    BaseType_t created = xTaskCreatePinnedToCore(wake_word_task, "maclaw_offline_wake",
                                                 10240, NULL, 4, &task, 1);
    taskENTER_CRITICAL(&s_wake_word_lock);
    if (created != pdPASS) {
        s_wake_word_task = NULL;
        s_on_wake_word = NULL;
        s_on_wake_word_arg = NULL;
        taskEXIT_CRITICAL(&s_wake_word_lock);
        return ESP_ERR_NO_MEM;
    }
    s_wake_word_task = task;
    taskEXIT_CRITICAL(&s_wake_word_lock);
    return ESP_OK;
}

esp_err_t board_port_stop_wake_word(void) {
    taskENTER_CRITICAL(&s_wake_word_lock);
    if (!s_wake_word_task) {
        taskEXIT_CRITICAL(&s_wake_word_lock);
        return ESP_ERR_INVALID_STATE;
    }
    s_wake_word_paused = true;
    s_wake_word_stop_requested = true;
    taskEXIT_CRITICAL(&s_wake_word_lock);
    // The task checks the flag after at most one I2S read timeout. Do not
    // delete it externally: it owns MultiNet and must release it itself.
    // Model creation itself can take about two seconds on ESP32-S3. This call
    // is used only while entering a provisioning portal, so wait long enough
    // for initialization, one detect chunk, and deterministic model cleanup.
    for (unsigned i = 0; i < 200 && s_wake_word_task; ++i) {
        vTaskDelay(pdMS_TO_TICKS(25));
    }
    if (s_wake_word_task) return ESP_ERR_TIMEOUT;
    taskENTER_CRITICAL(&s_wake_word_lock);
    s_on_wake_word = NULL;
    s_on_wake_word_arg = NULL;
    taskEXIT_CRITICAL(&s_wake_word_lock);
    return ESP_OK;
}

esp_err_t board_port_capture_wav(uint8_t **out_wav, size_t *out_len) {
    if (out_wav) *out_wav = NULL;
    if (out_len) *out_len = 0;
    if (!out_wav || !out_len) return ESP_ERR_INVALID_ARG;
    board_port_pause_wake_word(true);
    if (!s_audio_mutex || xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(1500)) != pdTRUE) {
        board_port_pause_wake_word(false);
        return ESP_ERR_TIMEOUT;
    }
    esp_err_t init_err = audio_init();
    if (init_err != ESP_OK) {
        xSemaphoreGive(s_audio_mutex);
        board_port_pause_wake_word(false);
        return init_err;
    }

    const size_t mono_samples = AUDIO_RATE * AUDIO_SECONDS;
    const size_t wav_len = 44 + mono_samples * sizeof(int16_t);
    uint8_t *wav = heap_caps_malloc(wav_len, MALLOC_CAP_8BIT);
    if (!wav) {
        xSemaphoreGive(s_audio_mutex);
        board_port_pause_wake_word(false);
        return ESP_ERR_NO_MEM;
    }
    memset(wav, 0, 44);
    memcpy(wav, "RIFF", 4);
    uint32_t riff_size = (uint32_t)wav_len - 8;
    memcpy(wav + 4, &riff_size, 4);
    memcpy(wav + 8, "WAVEfmt ", 8);
    uint32_t fmt_size = 16; uint16_t pcm = 1, channels = 1, bits = 16;
    uint32_t rate = AUDIO_RATE, byte_rate = AUDIO_RATE * 2; uint16_t align = 2;
    memcpy(wav + 16, &fmt_size, 4); memcpy(wav + 20, &pcm, 2);
    memcpy(wav + 22, &channels, 2); memcpy(wav + 24, &rate, 4);
    memcpy(wav + 28, &byte_rate, 4); memcpy(wav + 32, &align, 2);
    memcpy(wav + 34, &bits, 2); memcpy(wav + 36, "data", 4);
    uint32_t data_size = (uint32_t)(mono_samples * 2);
    memcpy(wav + 40, &data_size, 4);

    int16_t stereo[512];
    int16_t *mono = (int16_t *)(wav + 44);
    size_t written_samples = 0;
    int32_t peak = 0;
    uint16_t smoothed_level = 0;
    while (written_samples < mono_samples) {
        size_t received = 0;
        esp_err_t err = i2s_channel_read(s_audio_rx, stereo, sizeof(stereo), &received, pdMS_TO_TICKS(1000));
        if (err != ESP_OK) {
            free(wav);
            xSemaphoreGive(s_audio_mutex);
            board_port_pause_wake_word(false);
            return err;
        }
        size_t frames = received / (sizeof(int16_t) * 2);
        if (frames > mono_samples - written_samples) frames = mono_samples - written_samples;
        int32_t chunk_peak = 0;
        for (size_t i = 0; i < frames; ++i) {
            // Preserve the louder microphone channel instead of averaging the
            // two capsules, which can attenuate speech when their phases differ.
            int32_t left = stereo[i * 2];
            int32_t right = stereo[i * 2 + 1];
            int32_t left_abs = left < 0 ? -left : left;
            int32_t right_abs = right < 0 ? -right : right;
            int32_t sample = left_abs >= right_abs ? left : right;
            mono[written_samples++] = (int16_t)sample;
            int32_t magnitude = sample < 0 ? -sample : sample;
            if (magnitude > peak) peak = magnitude;
            if (magnitude > chunk_peak) chunk_peak = magnitude;
        }
        // ES7210 speech peaks around 10k-12k on this board. Apply a small
        // noise gate and attack/release smoothing for a responsive stable UI.
        uint16_t raw_level = chunk_peak <= 180 ? 0
                             : (uint16_t)(((chunk_peak - 180) * 1000) / (12000 - 180));
        if (raw_level > 1000) raw_level = 1000;
        smoothed_level = raw_level > smoothed_level
                             ? (uint16_t)((smoothed_level + raw_level * 3) / 4)
                             : (uint16_t)((smoothed_level * 7 + raw_level) / 8);
        board_port_set_audio_level(smoothed_level,
                                   (uint32_t)(written_samples / AUDIO_RATE));
        recording_wave_push_pcm(&mono[written_samples - frames], frames);
    }
    ESP_LOGI(TAG, "captured %u mono samples, peak=%ld", (unsigned)written_samples, (long)peak);
    *out_wav = wav;
    *out_len = wav_len;
    xSemaphoreGive(s_audio_mutex);
    board_port_pause_wake_word(false);
    return ESP_OK;
}

esp_err_t board_port_play_wav(const uint8_t *wav, size_t wav_len) {
    if (!wav || wav_len < 44 || memcmp(wav, "RIFF", 4) != 0 || memcmp(wav + 8, "WAVE", 4) != 0) {
        return ESP_ERR_INVALID_ARG;
    }
    const uint8_t *fmt = NULL, *data = NULL;
    size_t fmt_len = 0, data_len = 0;
    for (size_t offset = 12; offset + 8 <= wav_len;) {
        const uint8_t *chunk = wav + offset;
        uint32_t chunk_len = (uint32_t)chunk[4] | ((uint32_t)chunk[5] << 8) |
                             ((uint32_t)chunk[6] << 16) | ((uint32_t)chunk[7] << 24);
        offset += 8;
        if (chunk_len > wav_len - offset) return ESP_ERR_INVALID_SIZE;
        if (memcmp(chunk, "fmt ", 4) == 0) { fmt = wav + offset; fmt_len = chunk_len; }
        if (memcmp(chunk, "data", 4) == 0) { data = wav + offset; data_len = chunk_len; }
        offset += chunk_len + (chunk_len & 1u);
    }
    if (!fmt || fmt_len < 16 || !data || !data_len) return ESP_ERR_INVALID_ARG;
    uint16_t format = (uint16_t)fmt[0] | ((uint16_t)fmt[1] << 8);
    uint16_t channels = (uint16_t)fmt[2] | ((uint16_t)fmt[3] << 8);
    uint32_t rate = (uint32_t)fmt[4] | ((uint32_t)fmt[5] << 8) |
                    ((uint32_t)fmt[6] << 16) | ((uint32_t)fmt[7] << 24);
    uint16_t bits = (uint16_t)fmt[14] | ((uint16_t)fmt[15] << 8);
    if (format != 1 || bits != 16 || rate != AUDIO_RATE || (channels != 1 && channels != 2)) {
        ESP_LOGW(TAG, "unsupported WAV: format=%u rate=%lu bits=%u channels=%u",
                 format, (unsigned long)rate, bits, channels);
        return ESP_ERR_NOT_SUPPORTED;
    }
    if (!s_audio_mutex || xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(1500)) != pdTRUE) return ESP_ERR_TIMEOUT;
    board_port_pause_wake_word(true);
    esp_err_t err = audio_init();
    if (err == ESP_OK) err = gpio_set_level(AUDIO_PA_ENABLE, 1);
    int16_t stereo[512];
    for (size_t offset = 0; err == ESP_OK && offset < data_len;) {
        size_t frames = (data_len - offset) / (channels * sizeof(int16_t));
        if (frames > 256) frames = 256;
        if (frames == 0) break;
        const int16_t *source = (const int16_t *)(data + offset);
        for (size_t i = 0; i < frames; ++i) {
            int16_t sample = source[i * channels];
            stereo[i * 2] = sample;
            stereo[i * 2 + 1] = channels == 2 ? source[i * 2 + 1] : sample;
        }
        size_t written = 0;
        err = i2s_channel_write(s_audio_tx, stereo, frames * 2 * sizeof(int16_t),
                                &written, pdMS_TO_TICKS(1000));
        if (written != frames * 2 * sizeof(int16_t) && err == ESP_OK) err = ESP_ERR_TIMEOUT;
        offset += frames * channels * sizeof(int16_t);
    }
    vTaskDelay(pdMS_TO_TICKS(30));
    (void)gpio_set_level(AUDIO_PA_ENABLE, 0);
    xSemaphoreGive(s_audio_mutex);
    board_port_pause_wake_word(false);
    return err;
}

esp_err_t board_port_play_ack_chime(void) {
    if (!s_audio_mutex || xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(1500)) != pdTRUE) return ESP_ERR_TIMEOUT;
    board_port_pause_wake_word(true);
    esp_err_t err = audio_init();
    if (err == ESP_OK) err = gpio_set_level(AUDIO_PA_ENABLE, 1);
    int16_t stereo[512];
    // A short two-note acknowledgement: distinct enough to hear, soft enough
    // not to be confused with an alarm. The waveform is generated locally so
    // the board can confirm receipt before network TTS is available.
    for (int note = 0; err == ESP_OK && note < 2; ++note) {
        const int half_period = note == 0 ? 20 : 15; // 400 Hz, then ~533 Hz.
        for (int frame = 0; frame < AUDIO_RATE / 7; frame += 256) {
            int frames = (AUDIO_RATE / 7 - frame) > 256 ? 256 : (AUDIO_RATE / 7 - frame);
            for (int i = 0; i < frames; ++i) {
                int phase = (frame + i) % (half_period * 2);
                int16_t sample = phase < half_period ? 2600 : -2600;
                stereo[i * 2] = sample;
                stereo[i * 2 + 1] = sample;
            }
            size_t written = 0;
            err = i2s_channel_write(s_audio_tx, stereo, frames * 2 * sizeof(int16_t),
                                    &written, pdMS_TO_TICKS(1000));
            if (written != (size_t)frames * 2 * sizeof(int16_t) && err == ESP_OK) err = ESP_ERR_TIMEOUT;
        }
    }
    vTaskDelay(pdMS_TO_TICKS(30));
    (void)gpio_set_level(AUDIO_PA_ENABLE, 0);
    xSemaphoreGive(s_audio_mutex);
    board_port_pause_wake_word(false);
    return err;
}

esp_err_t board_port_play_ack_voice(void) {
    return board_port_play_ack_chime();
#if 0
    // The acknowledgement is a compact IMA ADPCM asset generated from a
    // Mandarin voice. Decode it in small pieces so confirmation is immediate
    // and does not consume a large PCM buffer while Wi-Fi is uploading.
    static const int s_ima_index_delta[8] = {-1, -1, -1, -1, 2, 4, 6, 8};
    static const int s_ima_step[89] = {
        7, 8, 9, 10, 11, 12, 13, 14, 16, 17, 19, 21, 23, 25, 28, 31,
        34, 37, 41, 45, 50, 55, 60, 66, 73, 80, 88, 97, 107, 118, 130,
        143, 157, 173, 190, 209, 230, 253, 279, 307, 337, 371, 408,
        449, 494, 544, 598, 658, 724, 796, 876, 963, 1060, 1166, 1282,
        1411, 1552, 1707, 1878, 2066, 2272, 2499, 2749, 3024, 3327,
        3660, 4026, 4428, 4871, 5358, 5894, 6484, 7132, 7845, 8630,
        9493, 10442, 11487, 12635, 13899, 15289, 16818, 18500, 20350,
        22385, 24623, 27086, 29794, 32767,
    };
    if (!s_audio_mutex || xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(1500)) != pdTRUE) return ESP_ERR_TIMEOUT;
    board_port_pause_wake_word(true);
    esp_err_t err = audio_init();
    if (err == ESP_OK) err = gpio_set_level(AUDIO_PA_ENABLE, 1);
    uint8_t compressed[MACLAW_ACK_VOICE_SAMPLES / 2 + 1];
    size_t compressed_len = 0;
    int base64_err = mbedtls_base64_decode(compressed, sizeof(compressed), &compressed_len,
                                           (const unsigned char *)s_maclaw_ack_voice_b64,
                                           strlen(s_maclaw_ack_voice_b64));
    if (base64_err != 0 || compressed_len == 0) {
        ESP_LOGE(TAG, "acknowledgement voice decode failed: %d", base64_err);
        (void)gpio_set_level(AUDIO_PA_ENABLE, 0);
        xSemaphoreGive(s_audio_mutex);
        board_port_pause_wake_word(false);
        return ESP_FAIL;
    }
    int16_t stereo[512];
    int predictor = 0;
    int step_index = 0;
    size_t packed_offset = 0;
    size_t decoded = 0;
    while (err == ESP_OK && decoded < MACLAW_ACK_VOICE_SAMPLES && packed_offset < compressed_len) {
        int frames = (MACLAW_ACK_VOICE_SAMPLES - decoded) > 256 ? 256 : (int)(MACLAW_ACK_VOICE_SAMPLES - decoded);
        for (int i = 0; i < frames; ++i) {
            // The asset is 8 kHz ADPCM. Repeat every decoded sample once so
            // it plays at its intended pitch on the 16 kHz I2S clock.
            if ((i & 1) == 0) {
                uint8_t packed = compressed[packed_offset + ((size_t)i / 4)];
                int code = ((i / 2) & 1) ? (packed >> 4) : (packed & 0x0f);
                int step = s_ima_step[step_index];
                int delta = step >> 3;
                if (code & 4) delta += step;
                if (code & 2) delta += step >> 1;
                if (code & 1) delta += step >> 2;
                predictor += (code & 8) ? -delta : delta;
                if (predictor > INT16_MAX) predictor = INT16_MAX;
                if (predictor < INT16_MIN) predictor = INT16_MIN;
                step_index += s_ima_index_delta[code & 7];
                if (step_index < 0) step_index = 0;
                if (step_index > 88) step_index = 88;
            }
            stereo[i * 2] = (int16_t)predictor;
            stereo[i * 2 + 1] = (int16_t)predictor;
        }
        size_t written = 0;
        err = i2s_channel_write(s_audio_tx, stereo, (size_t)frames * 2 * sizeof(int16_t),
                                &written, pdMS_TO_TICKS(1000));
        if (written != (size_t)frames * 2 * sizeof(int16_t) && err == ESP_OK) err = ESP_ERR_TIMEOUT;
        decoded += frames;
        packed_offset += (size_t)(frames + 3) / 4;
    }
    vTaskDelay(pdMS_TO_TICKS(30));
    (void)gpio_set_level(AUDIO_PA_ENABLE, 0);
    xSemaphoreGive(s_audio_mutex);
    board_port_pause_wake_word(false);
    return err;
#endif
}
