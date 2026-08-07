#include "board_port.h"
#include "font_cjk24.h"

#include <math.h>
#include <stdlib.h>
#include <string.h>

/* Bread Compact and Fangtang have no normalized inertial adapter in the
 * current hardware profiles.  Keep an explicit board-port fallback so common
 * Device API linkage never depends on a board model branch. */
esp_err_t board_port_motion_get_sample(device_motion_sample_t *out_sample) {
    (void)out_sample;
    return ESP_ERR_NOT_SUPPORTED;
}

#include "driver/gpio.h"
#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
#include "driver/ledc.h"
#endif
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
#include "esp_adc/adc_oneshot.h"
#endif
#include "driver/i2s_std.h"
#include "driver/spi_master.h"
#include "esp_check.h"
#include "esp_heap_caps.h"
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"
#include "esp_lcd_panel_vendor.h"
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
#include "boards/fangtang_4g/fangtang_display_adapter.h"
#endif
#include "esp_log.h"
#include "esp_mn_models.h"
#include "esp_mn_speech_commands.h"
#include "nvs.h"
#include "esp_timer.h"
#include "model_path.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#define LCD_HOST SPI3_HOST
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
/* The Fangtang profile owns its physical NV3023 contract. These aliases keep
 * the legacy generic renderer working during the staged split, but no
 * controller pin or initialization sequence remains defined in Bread code. */
#define LCD_WIDTH FANGTANG_DISPLAY_WIDTH
#define LCD_HEIGHT FANGTANG_DISPLAY_HEIGHT
#define LCD_Y_OFFSET FANGTANG_DISPLAY_GRAM_Y_OFFSET
#define LCD_MOSI FANGTANG_DISPLAY_MOSI
#define LCD_CLK FANGTANG_DISPLAY_CLK
#define LCD_DC FANGTANG_DISPLAY_DC
#define LCD_RST FANGTANG_DISPLAY_RESET
#define LCD_CS FANGTANG_DISPLAY_CS
#define LCD_BACKLIGHT FANGTANG_DISPLAY_BACKLIGHT
#define BUTTON_ACTIVATE GPIO_NUM_0
#define FANGTANG_CHARGE_STATUS_GPIO ((gpio_num_t)CONFIG_MACLAW_FANGTANG_CHARGE_STATUS_GPIO)
#define MIC_WS GPIO_NUM_4
#define MIC_BCLK GPIO_NUM_5
#define MIC_DIN GPIO_NUM_6
#define SPK_DOUT GPIO_NUM_7
#define SPK_BCLK GPIO_NUM_15
#define SPK_WS GPIO_NUM_16

#else
#define LCD_WIDTH 240
#define LCD_HEIGHT 320
/* This assembled S3 unit uses a 240x320 ST7789 panel. The S3 carrier routes
 * CS and backlight separately on GPIO41/GPIO42; GPIO18 is only the optional
 * lamp output. */
#define LCD_MOSI GPIO_NUM_47
#define LCD_CLK GPIO_NUM_21
#define LCD_DC GPIO_NUM_40
#define LCD_RST GPIO_NUM_45
#define LCD_CS GPIO_NUM_41
#define LCD_BACKLIGHT GPIO_NUM_42
/* This panel's backlight is wired to an LEDC-capable GPIO.  Full DC drive is
 * uncomfortably bright in normal indoor use, so retain a modest, fixed PWM
 * headroom for the Bread Compact profile.  Fangtang keeps its original direct
 * drive below because its smaller panel has different optical characteristics. */
#define BREAD_BACKLIGHT_LEDC_TIMER LEDC_TIMER_0
#define BREAD_BACKLIGHT_LEDC_CHANNEL LEDC_CHANNEL_0
#define BREAD_BACKLIGHT_LEDC_RESOLUTION LEDC_TIMER_10_BIT
// Bread uses 50% as its normal indoor brightness.  This is one fixed step
// below the previous 65% setting while retaining enough headroom for legible
// standby content in ordinary room lighting.
#define BREAD_BACKLIGHT_DUTY 512u

#define BUTTON_BOOT GPIO_NUM_0
#define BUTTON_ACTIVATE GPIO_NUM_0
#define BUTTON_VOLUME_UP GPIO_NUM_38
// Factory images name GPIO39 as the second user key. GPIO37 is reserved by
// the octal-PSRAM interface and must not be repurposed as an input.
#define BUTTON_VOLUME_DOWN GPIO_NUM_39
#define MIC_WS GPIO_NUM_4
#define MIC_BCLK GPIO_NUM_5
#define MIC_DIN GPIO_NUM_6
#define SPK_DOUT GPIO_NUM_7
#define SPK_BCLK GPIO_NUM_15
#define SPK_WS GPIO_NUM_16
#endif
#define AUDIO_RATE 16000
// A command should finish at a natural pause rather than after a fixed window.
// Keep an upper bound so a noisy microphone cannot retain the command buffer
// indefinitely. Thirty seconds remains below 1 MiB for 16 kHz PCM, while
// allowing multi-step commands. Levels are normalized 0..1000 by read_mono.
#define COMMAND_CAPTURE_MAX_SECONDS 30
#define COMMAND_CAPTURE_START_TIMEOUT_MS 6000
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
// The Fangtang's compact single microphone has occasional one-frame spikes.
// Require a little more sustained energy before accepting speech, and give a
// spoken multi-step command a longer natural pause before finalising it.  The
// Fangtang's MEMS microphone can produce isolated high-amplitude samples even
// in a quiet room. Command completion therefore uses mean signal energy plus
// an adaptive idle floor, rather than the peak level used to recognise speech
// onset.
#define COMMAND_CAPTURE_SILENCE_MS 1500
#define COMMAND_CAPTURE_START_CONFIRM_MS 160
#define COMMAND_CAPTURE_START_LEVEL 45
#define COMMAND_CAPTURE_SILENCE_FLOOR 55
#define COMMAND_CAPTURE_SILENCE_MARGIN 35
#define COMMAND_CAPTURE_SILENCE_CEILING 180
#else
#define COMMAND_CAPTURE_SILENCE_MS 1200
#define COMMAND_CAPTURE_START_CONFIRM_MS 80
#define COMMAND_CAPTURE_START_LEVEL 55
#define COMMAND_CAPTURE_SILENCE_FLOOR 20
#define COMMAND_CAPTURE_SILENCE_MARGIN 15
#define COMMAND_CAPTURE_SILENCE_CEILING 90
#endif
#define COMMAND_CAPTURE_PREROLL_MS 300
#define WAKE_WORD_COMMAND_ID 1
#define WAKE_WORD_LABEL "ma ka long"
// Spaces delimit MultiNet pinyin tokens; callers do not need to pause between
// them. Natural connected speech can voice the middle consonant or reduce its
// vowel, so register those common acoustic paths under the same wake command.
static const char *const s_wake_word_phonetics[] = {
    "ma ka long",
    "ma ga long",
    "ma ke long",
};
// Bread Compact's direct I2S microphone is quieter at normal speaking
// distance than EchoEar's codec path. A small threshold reduction improves
// recall without making the short product name excessively eager in noise.
// Both compact boards benefit from the modest recall increase. Keep the
// conditional explicit so future board variants sharing this port do not
// silently inherit the tuned threshold.
#if CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD || CONFIG_MACLAW_BOARD_FANGTANG_4G
#define WAKE_WORD_DETECTION_THRESHOLD 0.20f
#else
#define WAKE_WORD_DETECTION_THRESHOLD 0.24f
#endif
#define WAKE_WORD_COOLDOWN_US (2LL * 1000 * 1000)
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
// MultiNet7 on the current ESP-SR build reports that its runtime threshold is
// not adjustable. Give Fangtang's quieter MEMS microphone a modest wake-only
// gain instead. Foreground recordings retain their established level, and
// saturation below protects the recognizer from close-range speech clipping.
#define WAKE_WORD_INPUT_GAIN_NUM 3
#define WAKE_WORD_INPUT_GAIN_DEN 2
#else
#define WAKE_WORD_INPUT_GAIN_NUM 1
#define WAKE_WORD_INPUT_GAIN_DEN 1
#endif
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
/* The real panel exposes a 240x240 viewport at GRAM rows 80..319. One-row
 * transfers exactly match the independently verified display test. */
#define LCD_STRIPE_ROWS 1
#else
#define LCD_STRIPE_ROWS 16
#endif
#define THINKING_MOUTH_FRAME_MS 420
#define REMOTE_PET_RENDER_FRAME_MS 80
#define REMOTE_PET_DEFAULT_KEYFRAME_MS 450
#define IDLE_PET_SLEEP_TIMEOUT_US (30LL * 60 * 1000 * 1000)
#define AMBIENT_WEATHER_TEXT_Y 66
// Weather is a primary standby datum on Bread Compact. Use the native 24px
// CJK raster so it remains legible at normal viewing distance. The formatter
// contracts only the non-essential condition text, preserving the city and
// temperature within the 240px panel.
#define AMBIENT_WEATHER_SCALE_NUM 1
#define AMBIENT_WEATHER_SCALE_DEN 1
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
// Fangtang has only two compact status rows. Give the selected pet every row
// below them on the 240x240 viewport; a square 256px source becomes 178x178.
#define AMBIENT_PET_TOP 62
#define AMBIENT_PET_MAX_WIDTH 220
#else
#define AMBIENT_PET_TOP 94
#define AMBIENT_PET_MAX_WIDTH 224
#endif
#define AMBIENT_PET_MAX_HEIGHT (LCD_HEIGHT - AMBIENT_PET_TOP)
#define AMBIENT_NATIVE_PET_SCALE 93
#define LCD_FRAME_PIXELS ((size_t)LCD_WIDTH * LCD_HEIGHT)
#define LCD_FRAME_BYTES (LCD_FRAME_PIXELS * sizeof(uint16_t))

static const char *TAG = "maclaw_bread";
static board_port_button_cb_t s_button_cb;
static void *s_button_arg;
/* Input Service may stop during a degraded-startup rollback.  Keep the
 * board-owned polling task joinable so it cannot publish through a queue that
 * Input Service is about to release.  This stops only the scanner; the board
 * port itself remains boot-lifetime because display/audio deinit is not yet a
 * complete, restartable transaction. */
static TaskHandle_t s_button_task;
static SemaphoreHandle_t s_button_task_stopped;
static SemaphoreHandle_t s_background_tasks_lock;
static TaskHandle_t s_remote_pet_animation_task;
static TaskHandle_t s_thinking_mouth_task;
static SemaphoreHandle_t s_remote_pet_animation_stopped;
static SemaphoreHandle_t s_thinking_mouth_stopped;
static board_port_wake_word_cb_t s_wake_cb;
static void *s_wake_arg;
static TaskHandle_t s_wake_task;
static volatile bool s_wake_task_starting;
static volatile bool s_wake_ready;
static volatile bool s_wake_stop_requested;
static portMUX_TYPE s_wake_lock = portMUX_INITIALIZER_UNLOCKED;
static esp_lcd_panel_handle_t s_panel;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
static esp_lcd_panel_io_handle_t s_panel_io;
static volatile bool s_boot_network_window_active;
static volatile bool s_boot_network_toggle_requested;
static adc_oneshot_unit_handle_t s_battery_adc;
static unsigned s_battery_level;
static bool s_battery_level_valid;
static bool s_battery_charging;
static portMUX_TYPE s_power_status_lock = portMUX_INITIALIZER_UNLOCKED;
#endif
static i2s_chan_handle_t s_rx;
static i2s_chan_handle_t s_tx;
static SemaphoreHandle_t s_audio_mutex;
static SemaphoreHandle_t s_lcd_mutex;
static SemaphoreHandle_t s_lcd_transfer_done;
static uint16_t *s_framebuffers[2];
static uint16_t *s_render_target;
static uint16_t *s_present_staging;
static unsigned s_front_frame;
static bool s_front_frame_valid;
static bool s_direct_draw_warning_logged;
static bool s_audio_ready;
static TaskHandle_t s_audio_playback_owner;
static volatile bool s_audio_playback_stop_requested;
static bool s_audio_stream_owned;
static unsigned s_thinking_mouth_frame;
static bool s_thinking_surface_visible;
static bool s_recording_mode;
static bool s_recording_active;
static volatile bool s_command_capture_active;
static volatile bool s_command_capture_stop_requested;
static bool s_foreground_surface;
static bool s_alarm_visual_active;
static bool s_display_sleeping;
static int64_t s_idle_pet_sleep_expires_us;
static bool s_recording_paused;
static uint32_t s_recording_elapsed;
static uint16_t s_recording_levels[24];
static uint16_t s_recording_smoothed_level;
static volatile bool s_wake_paused;
static volatile bool s_wake_pause_acknowledged;
static char s_state[16] = "idle";
static char s_command_stage[32] = "正在处理";
static char s_pet_skin[32] = "clawmate";
#define REMOTE_PET_MAX_FRAMES 8
static uint8_t *s_remote_pet_frames[REMOTE_PET_MAX_FRAMES];
static size_t s_remote_pet_frame_count, s_remote_pet_width, s_remote_pet_height;
static uint32_t s_remote_pet_frame_ms = REMOTE_PET_DEFAULT_KEYFRAME_MS;
static uint64_t s_remote_pet_animation_elapsed_ms;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
static int64_t s_fangtang_thinking_next_frame_us;
#endif
static char s_wifi_ssid[33];
static char s_ambient_time[16];
static char s_ambient_location[24];
static char s_ambient_date[24];
static char s_ambient_weekday[24];
static char s_ambient_weather[32];
static int s_ambient_temperature;
static bool s_ambient_weather_valid;
static bool s_ambient_weather_stale;
static bool s_alarm_scheduled;
static bool s_wifi_connected;
static bool s_gateway_ready;
static bool s_network_transport_cellular;
// Direct-I2S has no codec gain register. This software gain is shared by Bread and Fangtang;
// GUI updates can arrive while audio is active.
static volatile unsigned s_output_volume = 70;

#define RESPONSE_TEXT_CAPACITY 2048
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
#define RESPONSE_LINES_PER_PAGE 5
#define RESPONSE_TEXT_X 12
#define RESPONSE_TEXT_Y 54
#define RESPONSE_LINE_HEIGHT 30
#define RESPONSE_FOOTER_Y 208
#define RESPONSE_AUTO_PAGE_INTERVAL_US (6LL * 1000 * 1000)
#define FANGTANG_HEADER_H 46
#define FANGTANG_SUGAR_WIDTH 188
#define FANGTANG_SUGAR_HEIGHT 164
#else
#define RESPONSE_LINES_PER_PAGE 6
#define RESPONSE_TEXT_X 16
#define RESPONSE_TEXT_Y 78
#define RESPONSE_LINE_HEIGHT 32
#define RESPONSE_FOOTER_Y 276
#endif
#define RESPONSE_TEXT_WIDTH (LCD_WIDTH - RESPONSE_TEXT_X * 2)
static bool s_response_active;
// Image pixels are not retained after the synchronous present. Track the
// surface kind so page buttons cannot replace it with stale text state.
static bool s_response_image_active;
static unsigned s_response_page;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
static int64_t s_response_next_page_us;
#endif
static char s_response_title[64];
static char s_response_text[RESPONSE_TEXT_CAPACITY];

extern const uint8_t _binary_cjk24_cjk_bin_start[];
extern const uint8_t _binary_cjk24_cjk_bin_end[];

#define DYNAMIC_GLYPH_BYTES 72
#define DYNAMIC_GLYPH_CACHE_CAPACITY 24
typedef struct {
    uint32_t codepoint;
    uint32_t last_used;
    uint8_t bitmap[DYNAMIC_GLYPH_BYTES];
    bool used;
} compact_dynamic_glyph_t;
static compact_dynamic_glyph_t s_dynamic_glyphs[DYNAMIC_GLYPH_CACHE_CAPACITY];
static uint32_t s_dynamic_glyph_clock;
static portMUX_TYPE s_glyph_lock = portMUX_INITIALIZER_UNLOCKED;


static uint16_t state_color(const char *state);
static void present_composed_frame(void);
static void draw_text24_clipped(int x, int y, const char *text,
                                uint16_t fg, uint16_t bg, int max_glyphs);
static void draw_text24_centered(int y, const char *text,
                                 uint16_t fg, uint16_t bg, int max_glyphs);
static void draw_text24_scaled_centered(int y, const char *text,
                                        uint16_t fg, uint16_t bg, int max_glyphs,
                                        int scale_num, int scale_den);
static void format_ambient_weather_line(char *out, size_t out_size,
                                        const char *city, const char *summary,
                                        int temperature_c, bool stale);
static void fill_rect_solid(int x, int y, int width, int height, uint16_t fill);
static bool draw_remote_pet_frame(uint16_t bg);
static void show_state_screen(const char *state);

static void draw_alarm_indicator(int x, int y, uint16_t fg) {
    // A 14 px outline clock deliberately stays quieter than the 24 px
    // calendar text. It is pixel-drawn here to avoid consuming a CJK glyph
    // slot or changing the compact font's alignment.
    fill_rect_solid(x + 4, y, 6, 1, fg);
    fill_rect_solid(x + 2, y + 1, 2, 1, fg);
    fill_rect_solid(x + 10, y + 1, 2, 1, fg);
    fill_rect_solid(x + 1, y + 3, 1, 8, fg);
    fill_rect_solid(x + 12, y + 3, 1, 8, fg);
    fill_rect_solid(x + 2, y + 11, 2, 1, fg);
    fill_rect_solid(x + 10, y + 11, 2, 1, fg);
    fill_rect_solid(x + 4, y + 12, 6, 1, fg);
    fill_rect_solid(x + 6, y + 3, 1, 5, fg);
    fill_rect_solid(x + 6, y + 7, 4, 1, fg);
    fill_rect_solid(x + 3, y - 2, 2, 2, fg);
    fill_rect_solid(x + 9, y - 2, 2, 2, fg);
}

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
extern const uint8_t _binary_bread_compact_splash_rgb565_start[];
extern const uint8_t _binary_bread_compact_splash_rgb565_end[];
#else
extern const uint8_t _binary_fangtang_sugar_rgb565_start[];
extern const uint8_t _binary_fangtang_sugar_rgb565_end[];
extern const uint8_t _binary_fangtang_sugar_a8_start[];
extern const uint8_t _binary_fangtang_sugar_a8_end[];
#endif

static bool lcd_color_transfer_done(esp_lcd_panel_io_handle_t io,
                                    esp_lcd_panel_io_event_data_t *event,
                                    void *user_ctx) {
    (void)io;
    (void)event;
    BaseType_t task_woken = pdFALSE;
    xSemaphoreGiveFromISR((SemaphoreHandle_t)user_ctx, &task_woken);
    return task_woken == pdTRUE;
}

static esp_err_t panel_draw_bitmap_sync(int x0, int y0, int x1, int y1, const void *pixels) {
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    return fangtang_display_draw_bitmap_rows(s_panel_io, x0, y0, x1, y1, pixels);
#endif
    while (xSemaphoreTake(s_lcd_transfer_done, 0) == pdTRUE) {}
    esp_err_t err = esp_lcd_panel_draw_bitmap(s_panel, x0, y0, x1, y1, pixels);
    if (err != ESP_OK) return err;
    return xSemaphoreTake(s_lcd_transfer_done, pdMS_TO_TICKS(1000)) == pdTRUE
               ? ESP_OK : ESP_ERR_TIMEOUT;
}

static esp_err_t draw_bitmap_sync(int x0, int y0, int x1, int y1, const void *pixels) {
    if (!s_render_target) {
        return panel_draw_bitmap_sync(x0, y0, x1, y1, pixels);
    }
    if (!pixels || x0 < 0 || y0 < 0 || x1 > LCD_WIDTH || y1 > LCD_HEIGHT ||
        x1 <= x0 || y1 <= y0) return ESP_ERR_INVALID_ARG;
    int width = x1 - x0;
    int height = y1 - y0;
    const uint16_t *source = pixels;
    for (int row = 0; row < height; ++row) {
        memcpy(s_render_target + (size_t)(y0 + row) * LCD_WIDTH + x0,
               source + (size_t)row * width, (size_t)width * sizeof(uint16_t));
    }
    return ESP_OK;
}

static bool begin_composed_frame(void) {
    if (!s_framebuffers[0] || !s_framebuffers[1] || !s_present_staging) return false;
    s_render_target = s_framebuffers[s_front_frame ^ 1u];
    return true;
}

static void wake_display_for_draw_locked(void) {
    if (!s_display_sleeping) return;
    s_display_sleeping = false;
    s_idle_pet_sleep_expires_us = 0;
    // A controller is allowed to lose or alter GRAM while DISP is disabled.
    // Invalidate the front snapshot so the first wake draw transfers every row
    // even when its pixels happen to match the last pre-sleep ambient frame.
    s_front_frame_valid = false;
    ESP_ERROR_CHECK_WITHOUT_ABORT(esp_lcd_panel_disp_on_off(s_panel, true));
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    ESP_ERROR_CHECK_WITHOUT_ABORT(fangtang_display_set_backlight(true));
#else
    ESP_ERROR_CHECK_WITHOUT_ABORT(ledc_set_duty(LEDC_LOW_SPEED_MODE,
                                                BREAD_BACKLIGHT_LEDC_CHANNEL,
                                                BREAD_BACKLIGHT_DUTY));
    ESP_ERROR_CHECK_WITHOUT_ABORT(ledc_update_duty(LEDC_LOW_SPEED_MODE,
                                                   BREAD_BACKLIGHT_LEDC_CHANNEL));
#endif
}

static void enter_ambient_awake_locked(void) {
    wake_display_for_draw_locked();
    s_idle_pet_sleep_expires_us = esp_timer_get_time() + IDLE_PET_SLEEP_TIMEOUT_US;
}

static bool begin_screen_frame(void) {
    wake_display_for_draw_locked();
    if (begin_composed_frame()) return true;
    if (!s_direct_draw_warning_logged) {
        ESP_LOGW(TAG, "LCD framebuffer unavailable; drawing screen directly");
        s_direct_draw_warning_logged = true;
    }
    return false;
}

static void finish_screen_frame(bool composed) {
    if (composed) present_composed_frame();
}

static void present_composed_frame(void) {
    if (!s_render_target) return;
    unsigned back = s_front_frame ^ 1u;
    uint16_t *next = s_framebuffers[back];
    uint16_t *previous = s_framebuffers[s_front_frame];
    bool presentation_ok = true;
    s_render_target = NULL;
#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
    for (int y = 0; y < LCD_HEIGHT; y += LCD_STRIPE_ROWS) {
        int rows = LCD_HEIGHT - y < LCD_STRIPE_ROWS ? LCD_HEIGHT - y : LCD_STRIPE_ROWS;
        int first_changed = -1;
        int last_changed = -1;
        int left_changed = LCD_WIDTH;
        int right_changed = -1;
        for (int row = 0; row < rows; ++row) {
            const uint16_t *next_row = next + (size_t)(y + row) * LCD_WIDTH;
            const uint16_t *old_row = previous + (size_t)(y + row) * LCD_WIDTH;
            if (!s_front_frame_valid ||
                memcmp(next_row, old_row, LCD_WIDTH * sizeof(uint16_t)) != 0) {
                if (first_changed < 0) first_changed = row;
                last_changed = row;
                if (!s_front_frame_valid) {
                    left_changed = 0;
                    right_changed = LCD_WIDTH - 1;
                    continue;
                }
                int left = 0;
                while (left < LCD_WIDTH && next_row[left] == old_row[left]) ++left;
                int right = LCD_WIDTH - 1;
                while (right > left && next_row[right] == old_row[right]) --right;
                if (left < left_changed) left_changed = left;
                if (right > right_changed) right_changed = right;
            }
        }
        if (first_changed < 0) continue;
        int changed_rows = last_changed - first_changed + 1;
        int changed_width = right_changed - left_changed + 1;
        for (int row = 0; row < changed_rows; ++row) {
            memcpy(s_present_staging + (size_t)row * changed_width,
                   next + (size_t)(y + first_changed + row) * LCD_WIDTH + left_changed,
                   (size_t)changed_width * sizeof(uint16_t));
        }
        esp_err_t err = panel_draw_bitmap_sync(
            left_changed, y + first_changed, right_changed + 1,
            y + first_changed + changed_rows,
            s_present_staging);
        if (err != ESP_OK) {
            presentation_ok = false;
            ESP_LOGE(TAG, "LCD frame transfer failed at y=%d: %s",
                     y + first_changed, esp_err_to_name(err));
        }
    }
#else
    esp_err_t err = panel_draw_bitmap_sync(0, 0, LCD_WIDTH, LCD_HEIGHT, next);
    if (err != ESP_OK) {
        presentation_ok = false;
        ESP_LOGE(TAG, "LCD full-frame transfer failed: %s",
                 esp_err_to_name(err));
    }
#endif
    s_front_frame = back;
    // A failed stripe leaves the panel contents out of sync with both buffers.
    // Force the next presentation to refresh every row and repair the screen.
    s_front_frame_valid = presentation_ok;
}

static uint16_t color(uint8_t r, uint8_t g, uint8_t b) {
    uint16_t p = (uint16_t)(((r & 0xf8) << 8) | ((g & 0xfc) << 3) | (b >> 3));
    return (uint16_t)((p << 8) | (p >> 8));
}

static void fill_screen(uint16_t c) {
    if (!s_panel) return;
    if (s_render_target) {
        for (size_t i = 0; i < LCD_FRAME_PIXELS; ++i) s_render_target[i] = c;
        return;
    }
    uint16_t *line = s_present_staging;
    bool temporary_line = false;
    if (!line) {
        line = heap_caps_malloc((size_t)LCD_WIDTH * LCD_STRIPE_ROWS * sizeof(uint16_t),
                                MALLOC_CAP_DMA | MALLOC_CAP_INTERNAL);
        temporary_line = line != NULL;
    }
    if (!line) return;
    for (size_t i = 0; i < (size_t)LCD_WIDTH * LCD_STRIPE_ROWS; ++i) line[i] = c;
    for (int y = 0; y < LCD_HEIGHT; y += LCD_STRIPE_ROWS) {
        int y2 = y + LCD_STRIPE_ROWS < LCD_HEIGHT ? y + LCD_STRIPE_ROWS : LCD_HEIGHT;
        draw_bitmap_sync(0, y, LCD_WIDTH, y2, line);
    }
    if (temporary_line) heap_caps_free(line);
}

static const uint8_t *glyph5x7(char c) {
    static const uint8_t blank[5] = {0, 0, 0, 0, 0};
    static const uint8_t question[5] = {2, 1, 0x59, 9, 6};
    static const uint8_t colon[5] = {0, 0x36, 0x36, 0, 0};
    static const uint8_t slash[5] = {0x20, 0x10, 0x08, 0x04, 0x02};
    static const uint8_t percent[5] = {0x63, 0x13, 0x08, 0x64, 0x63};
    static const uint8_t dash[5] = {0x08, 0x08, 0x08, 0x08, 0x08};
    static const uint8_t dot[5] = {0, 0x60, 0x60, 0, 0};
    static const uint8_t letters[][5] = {
        {0x7E,0x11,0x11,0x11,0x7E},{0x7F,0x49,0x49,0x49,0x36},{0x3E,0x41,0x41,0x41,0x22},
        {0x7F,0x41,0x41,0x22,0x1C},{0x7F,0x49,0x49,0x49,0x41},{0x7F,0x09,0x09,0x09,0x01},
        {0x3E,0x41,0x49,0x49,0x7A},{0x7F,0x08,0x08,0x08,0x7F},{0,0x41,0x7F,0x41,0},
        {0x20,0x40,0x41,0x3F,0x01},{0x7F,0x08,0x14,0x22,0x41},{0x7F,0x40,0x40,0x40,0x40},
        {0x7F,0x02,0x0C,0x02,0x7F},{0x7F,0x04,0x08,0x10,0x7F},{0x3E,0x41,0x41,0x41,0x3E},
        {0x7F,0x09,0x09,0x09,0x06},{0x3E,0x41,0x51,0x21,0x5E},{0x7F,0x09,0x19,0x29,0x46},
        {0x46,0x49,0x49,0x49,0x31},{0x01,0x01,0x7F,0x01,0x01},{0x3F,0x40,0x40,0x40,0x3F},
        {0x1F,0x20,0x40,0x20,0x1F},{0x3F,0x40,0x38,0x40,0x3F},{0x63,0x14,0x08,0x14,0x63},
        {0x03,0x04,0x78,0x04,0x03},{0x61,0x51,0x49,0x45,0x43},
    };
    static const uint8_t digits[][5] = {
        {0x3E,0x51,0x49,0x45,0x3E},{0,0x42,0x7F,0x40,0},{0x42,0x61,0x51,0x49,0x46},
        {0x21,0x41,0x45,0x4B,0x31},{0x18,0x14,0x12,0x7F,0x10},{0x27,0x45,0x45,0x45,0x39},
        {0x3C,0x4A,0x49,0x49,0x30},{0x01,0x71,0x09,0x05,0x03},{0x36,0x49,0x49,0x49,0x36},
        {0x06,0x49,0x49,0x29,0x1E},
    };
    if (c >= 'a' && c <= 'z') c -= 'a' - 'A';
    if (c >= 'A' && c <= 'Z') return letters[c - 'A'];
    if (c >= '0' && c <= '9') return digits[c - '0'];
    if (c == ' ') return blank;
    if (c == ':') return colon;
    if (c == '/') return slash;
    if (c == '%') return percent;
    if (c == '-') return dash;
    if (c == '.') return dot;
    return question;
}

static void draw_ascii_scaled_at(int x0, int y, const char *text, int scale,
                                 uint16_t fg, uint16_t bg) {
    if (!text || !*text) return;
    if (scale < 1 || scale > 3) return;
    size_t len = strlen(text);
    if (len > 12) len = 12;
    int width = (int)len * 6 * scale;
    uint16_t *bitmap = heap_caps_malloc((size_t)width * 7 * scale * sizeof(uint16_t), MALLOC_CAP_DMA);
    if (!bitmap) return;
    for (int py = 0; py < 7 * scale; ++py) {
        for (int px = 0; px < width; ++px) bitmap[py * width + px] = bg;
    }
    for (size_t ch = 0; ch < len; ++ch) {
        const uint8_t *g = glyph5x7(text[ch]);
        for (int gx = 0; gx < 5; ++gx) for (int gy = 0; gy < 7; ++gy) {
            if (!(g[gx] & (1u << gy))) continue;
            for (int sx = 0; sx < scale; ++sx) for (int sy = 0; sy < scale; ++sy) {
                int x = (int)ch * 6 * scale + gx * scale + sx;
                bitmap[(gy * scale + sy) * width + x] = fg;
            }
        }
    }
    if (x0 < 0) x0 = 0;
    if (x0 + width > LCD_WIDTH) x0 = LCD_WIDTH - width;
    draw_bitmap_sync(x0, y, x0 + width, y + 7 * scale, bitmap);
    heap_caps_free(bitmap);
}

#if CONFIG_MACLAW_BOARD_FANGTANG_4G
static int fangtang_network_label_width(bool cellular) {
    const char *label = cellular ? "4G" : "WIFI";
    const int scale = cellular ? 1 : 2;
    return ((int)strlen(label) * 6 - 1) * scale;
}

static void draw_fangtang_network_label(int x, int y, bool cellular,
                                        uint16_t fg, uint16_t bg) {
    // Paint directly into the composed framebuffer. The generic ASCII helper
    // allocates a temporary DMA bitmap; on the Fangtang idle screen that
    // allocation can fail after the much larger sugar bitmap allocation,
    // leaving only the signal icon visible. These tiny fixed labels need no
    // allocation and are guaranteed to be part of the final full-frame flush.
    const char *label = cellular ? "4G" : "WIFI";
    // The cellular suffix is deliberately smaller and one row higher than
    // the Wi-Fi word. This matches the compact signal bars without competing
    // with the calendar. WIFI stays at 2x because the physical 0.85-inch
    // panel made a 1x word look like part of the radio-wave icon.
    const int scale = cellular ? 1 : 2;
    const int width = fangtang_network_label_width(cellular);
    const int height = 7 * scale;

    // Give the word its own quiet field. Besides improving contrast, clearing
    // this exact rectangle guarantees that stale pixels from the preceding
    // one-second idle frame cannot visually merge with the letters.
    fill_rect_solid(x - 2, y - 2, width + 4, height + 4, bg);
    for (size_t ch = 0; label[ch]; ++ch) {
        const uint8_t *glyph = glyph5x7(label[ch]);
        for (int gx = 0; gx < 5; ++gx) {
            for (int gy = 0; gy < 7; ++gy) {
                if (!(glyph[gx] & (1u << gy))) continue;
                fill_rect_solid(x + (int)ch * 6 * scale + gx * scale,
                                y + gy * scale, scale, scale, fg);
            }
        }
    }
}
#endif

static void draw_ascii_at(int x0, int y, const char *text, uint16_t fg, uint16_t bg) {
    draw_ascii_scaled_at(x0, y, text, 3, fg, bg);
}

static void draw_ascii_centered(int y, const char *text, uint16_t fg, uint16_t bg) {
    size_t len = text ? strlen(text) : 0;
    if (len > 12) len = 12;
    draw_ascii_at((LCD_WIDTH - (int)len * 18) / 2, y, text, fg, bg);
}

static uint32_t utf8_next(const char **cursor) {
    const uint8_t *s = (const uint8_t *)*cursor;
    if (!*s) return 0;
    uint32_t cp;
    size_t count;
    if (s[0] < 0x80) { cp = s[0]; count = 1; }
    else if ((s[0] & 0xe0) == 0xc0 && s[1] && (s[1] & 0xc0) == 0x80) {
        cp = ((s[0] & 0x1f) << 6) | (s[1] & 0x3f); count = 2;
    } else if ((s[0] & 0xf0) == 0xe0 && s[1] && s[2] &&
               (s[1] & 0xc0) == 0x80 && (s[2] & 0xc0) == 0x80) {
        cp = ((s[0] & 0x0f) << 12) | ((s[1] & 0x3f) << 6) | (s[2] & 0x3f); count = 3;
    } else if ((s[0] & 0xf8) == 0xf0 && s[1] && s[2] && s[3] &&
               (s[1] & 0xc0) == 0x80 && (s[2] & 0xc0) == 0x80 &&
               (s[3] & 0xc0) == 0x80) {
        cp = ((s[0] & 0x07) << 18) | ((s[1] & 0x3f) << 12) |
             ((s[2] & 0x3f) << 6) | (s[3] & 0x3f); count = 4;
    } else { cp = '?'; count = 1; }
    *cursor += count;
    return cp;
}

static const uint32_t *cjk24_rows(uint32_t codepoint) {
    for (size_t i = 0; i < sizeof(s_maclaw_cjk24) / sizeof(s_maclaw_cjk24[0]); ++i) {
        if (s_maclaw_cjk24[i].codepoint == codepoint) return s_maclaw_cjk24[i].rows;
    }
    return NULL;
}

static bool full_cjk24_copy(uint32_t codepoint, uint8_t bitmap[DYNAMIC_GLYPH_BYTES]) {
    if (codepoint < 0x4E00 || codepoint >= 0xA000) return false;
    size_t offset = (size_t)(codepoint - 0x4E00) * DYNAMIC_GLYPH_BYTES;
    size_t available = (size_t)(_binary_cjk24_cjk_bin_end - _binary_cjk24_cjk_bin_start);
    if (offset + DYNAMIC_GLYPH_BYTES > available) return false;
    memcpy(bitmap, _binary_cjk24_cjk_bin_start + offset, DYNAMIC_GLYPH_BYTES);
    return true;
}

static bool dynamic_glyph_copy(uint32_t codepoint, uint8_t bitmap[DYNAMIC_GLYPH_BYTES]) {
    bool found = false;
    taskENTER_CRITICAL(&s_glyph_lock);
    for (size_t i = 0; i < DYNAMIC_GLYPH_CACHE_CAPACITY; ++i) {
        if (s_dynamic_glyphs[i].used && s_dynamic_glyphs[i].codepoint == codepoint) {
            memcpy(bitmap, s_dynamic_glyphs[i].bitmap, DYNAMIC_GLYPH_BYTES);
            s_dynamic_glyphs[i].last_used = ++s_dynamic_glyph_clock;
            found = true;
            break;
        }
    }
    taskEXIT_CRITICAL(&s_glyph_lock);
    return found;
}

static bool glyph24_pixel(uint32_t codepoint, const uint32_t *rows,
                           const uint8_t *dynamic, int row, int col) {
	int source_row = row;
	// Simplified-Chinese horizontal typography places comma/period at the lower
	// left of the ideographic cell. The host font rasterizer can produce a
	// vertical-layout bitmap whose punctuation ink starts at the cell top.
	// Move only those two marks down; clipping naturally keeps them in 24 px.
	if (codepoint == 0x3002 || codepoint == 0xFF0C) source_row -= 14; // 。 ，
	if (source_row < 0 || source_row >= 24) return false;
    if (rows) return (rows[source_row] & (1u << (23 - col))) != 0;
    if (dynamic) return (dynamic[source_row * 3 + col / 8] & (1u << (7 - col % 8))) != 0;
    // Keep the temperature unit readable without needing a downloaded glyph.
    // The compact ring sits above the baseline so the following C forms °C.
    if (codepoint == 0x00B0) {
        int dx = col - 4;
        int dy = source_row - 3;
        int distance = dx * dx + dy * dy;
        return distance >= 6 && distance <= 13;
    }
    return codepoint < 0x80 && row < 14 && col < 10 &&
           (glyph5x7((char)codepoint)[col / 2] & (1u << (row / 2)));
}

static int text24_advance(uint32_t codepoint) {
    if (codepoint == ' ') return 7;
    if (codepoint == 0x00B0) return 10;
    return codepoint < 0x80 ? 11 : 25;
}

static bool response_break(uint32_t codepoint) {
    return codepoint == '\n' || codepoint == '\r';
}

// Formatting controls can legitimately arrive at the front of copied/model
// text (BOM, bidi isolates, zero-width marks and emoji variation selectors),
// but they have no visible meaning on this fixed left-to-right LCD. The compact
// fallback font renders them as question marks, so consume them without width.
static bool response_invisible_format(uint32_t codepoint) {
    return codepoint == 0xFEFF || codepoint == 0x200B || codepoint == 0x200C ||
           codepoint == 0x200D || codepoint == 0x200E || codepoint == 0x200F ||
           codepoint == 0xFE0E || codepoint == 0xFE0F ||
           (codepoint >= 0x202A && codepoint <= 0x202E) ||
           (codepoint >= 0x2060 && codepoint <= 0x2069);
}

static bool response_leading_decoration(uint32_t codepoint) {
    // Emoji, pictographs, dingbats and enclosed-symbol decorations are useful
    // in rich chat, but this compact bitmap font cannot represent them. At the
    // start of an answer they otherwise become a row such as "?I?" before the
    // actual content. Keep ASCII and ordinary language punctuation untouched.
    return response_invisible_format(codepoint) ||
           (codepoint >= 0x2190 && codepoint <= 0x21FF) ||
           (codepoint >= 0x2300 && codepoint <= 0x27FF) ||
           (codepoint >= 0x2B00 && codepoint <= 0x2BFF) ||
           (codepoint >= 0x1F000 && codepoint <= 0x1FAFF);
}

static const char *response_visible_start(const char *text) {
    const char *cursor = text ? text : "";
    while (*cursor) {
        // Old Hub builds prefixed normal informational responses with "[i]".
        // Strip only that complete, boundary-delimited marker. Do not hide
        // legitimate result text such as "[I/O] status" or an inline "[i]".
        if (cursor[0] == '[' && (cursor[1] == 'i' || cursor[1] == 'I') &&
            cursor[2] == ']' &&
            (cursor[3] == '\0' || cursor[3] == ' ' || cursor[3] == '\t' ||
             cursor[3] == '\r' || cursor[3] == '\n')) {
            cursor += 3;
            continue;
        }
        // Desktop-only route/model diagnostics can be present in replies from
        // older GUI builds. Drop only a leading metadata line; ordinary answer
        // text containing these words later remains untouched.
        static const char *const internal_prefixes[] = {
            "route task:", "route source:", "route model:",
            "route reason:", "route escalated:", "cost tier:",
            "input tokens:", "output tokens:", "total tokens:",
            "cache read tokens:", "cache write tokens:",
        };
        bool internal_line = false;
        for (size_t i = 0; i < sizeof(internal_prefixes) / sizeof(internal_prefixes[0]); ++i) {
            size_t prefix_len = strlen(internal_prefixes[i]);
            if (!strncasecmp(cursor, internal_prefixes[i], prefix_len)) {
                internal_line = true;
                break;
            }
        }
        if (internal_line) {
            const char *next_line = strchr(cursor, '\n');
            if (!next_line) return cursor + strlen(cursor);
            cursor = next_line + 1;
            continue;
        }
        const char *before = cursor;
        uint32_t cp = utf8_next(&cursor);
        if (cp == ' ' || cp == '\t' || response_break(cp) ||
            response_leading_decoration(cp)) {
            continue;
        }
        return before;
    }
    return cursor;
}

static bool response_closing_punctuation(uint32_t codepoint) {
    switch (codepoint) {
        case 0x3001: // 、
        case 0x3002: // 。
        case 0xFF0C: // ，
        case 0xFF0E: // ．
        case 0xFF01: // ！
        case 0xFF1A: // ：
        case 0xFF1B: // ；
        case 0xFF1F: // ？
        case 0xFF09: // ）
        case 0x3009: // 〉
        case 0x300B: // 》
        case 0x300D: // 」
        case 0x300F: // 』
        case 0x3011: // 】
        case 0x3015: // 〕
        case 0x3017: // 〗
        case 0x3019: // 〙
        case 0x301B: // 〛
        case 0x2026: // …
            return true;
        default:
            return false;
    }
}

static bool response_opening_punctuation(uint32_t codepoint) {
    switch (codepoint) {
        case 0xFF08: // （
        case 0x3008: // 〈
        case 0x300A: // 《
        case 0x300C: // 「
        case 0x300E: // 『
        case 0x3010: // 【
        case 0x3014: // 〔
        case 0x3016: // 〖
        case 0x3018: // 〘
        case 0x301A: // 〚
            return true;
        default:
            return false;
    }
}

// Returns the byte after one panel-width line. The line width uses the exact
// glyph advances of the renderer, so mixed Chinese, punctuation and numbers
// wrap without dropping UTF-8 bytes or splitting a character.
static const char *response_next_line(const char *cursor, char *line, size_t line_size) {
    if (!line || line_size == 0) return cursor;
    line[0] = '\0';
    if (!cursor) return cursor;
    int width = 0;
    size_t used = 0;
    const char *last_before = NULL;
    size_t last_used = 0;
    int last_width = 0;
    uint32_t last_cp = 0;
    while (*cursor) {
        const char *before = cursor;
        uint32_t cp = utf8_next(&cursor);
        if (response_break(cp)) {
            while (*cursor == '\n' || *cursor == '\r') ++cursor;
            break;
        }
        if (response_invisible_format(cp)) continue;
        int advance = text24_advance(cp);
        if (width + advance > RESPONSE_TEXT_WIDTH && used > 0) {
            if (response_closing_punctuation(cp)) {
                // A small optical overhang is preferable to starting a line
                // with punctuation. The renderer clips safely at the margin.
                size_t bytes = (size_t)(cursor - before);
                if (used + bytes < line_size) {
                    memcpy(line + used, before, bytes);
                    used += bytes;
                    width += advance;
                } else {
                    cursor = before;
                }
            } else {
                cursor = before;
                // Never strand an opening bracket at the end of a line.
                if (response_opening_punctuation(last_cp) && last_before && last_used > 0) {
                    cursor = last_before;
                    used = last_used;
                    width = last_width;
                }
            }
            break;
        }
        size_t bytes = (size_t)(cursor - before);
        if (used + bytes >= line_size) {
            cursor = before;
            break;
        }
        last_before = before;
        last_used = used;
        last_width = width;
        last_cp = cp;
        memcpy(line + used, before, bytes);
        used += bytes;
        width += advance;
    }
    line[used] = '\0';
    if (!used && *cursor) {
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
    while (*cursor && lines < 240) {
        const char *next = response_next_line(cursor, line, sizeof(line));
        if (next == cursor) break;
        cursor = next;
        ++lines;
    }
    return lines ? (lines + RESPONSE_LINES_PER_PAGE - 1) / RESPONSE_LINES_PER_PAGE : 1;
}

static void draw_response_page(void) {
    const uint16_t bg = color(8, 17, 28);
    const uint16_t header = color(14, 31, 47);
    const uint16_t footer = color(11, 24, 38);
    const uint16_t accent = color(76, 168, 207);
    const uint16_t title = color(244, 248, 251);
    const uint16_t body = color(214, 227, 237);
    const uint16_t muted = color(145, 172, 191);
    bool composed = begin_screen_frame();
    fill_screen(bg);
    fill_rect_solid(0, 0, LCD_WIDTH,
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                    FANGTANG_HEADER_H,
#else
                    60,
#endif
                    header);
    fill_rect_solid(RESPONSE_TEXT_X,
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                    5, 3, 20,
#else
                    19, 4, 23,
#endif
                    accent);
    draw_text24_clipped(RESPONSE_TEXT_X +
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                        12, 10,
#else
                        14, 18,
#endif
                        s_response_title[0] ? s_response_title : "处理结果",
                        title, header, 8);
    fill_rect_solid(RESPONSE_TEXT_X,
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                    FANGTANG_HEADER_H - 1,
#else
                    59,
#endif
                    RESPONSE_TEXT_WIDTH, 1, color(31, 62, 82));

    const char *cursor = s_response_text;
    unsigned skip = s_response_page * RESPONSE_LINES_PER_PAGE;
    char line[96];
    while (*cursor && skip--) cursor = response_next_line(cursor, line, sizeof(line));
    for (int row = 0; row < RESPONSE_LINES_PER_PAGE && *cursor; ++row) {
        cursor = response_next_line(cursor, line, sizeof(line));
        draw_text24_clipped(RESPONSE_TEXT_X, RESPONSE_TEXT_Y + row * RESPONSE_LINE_HEIGHT,
                            line, body, bg, 24);
    }

    unsigned pages = response_page_count();
    char indicator[16];
    snprintf(indicator, sizeof(indicator), "%u/%u", s_response_page + 1, pages);
    fill_rect_solid(0, RESPONSE_FOOTER_Y, LCD_WIDTH,
                    LCD_HEIGHT - RESPONSE_FOOTER_Y, footer);
    draw_text24_clipped(RESPONSE_TEXT_X,
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                        214,
#else
                        287,
#endif
                        pages > 1 ?
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                            "自动翻页" :
#else
                            "音量键翻页" :
#endif
                            "激活键返回",
                        muted, footer, 5);
    // The old centered page number occupied the same horizontal band as the
    // Chinese hint. Anchor it at the right edge in the compact ASCII renderer.
    const int indicator_width = (int)strlen(indicator) *
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                                12;
    draw_ascii_at(LCD_WIDTH - RESPONSE_TEXT_X - indicator_width, 216,
#else
                                18;
    draw_ascii_at(LCD_WIDTH - RESPONSE_TEXT_X - indicator_width, 289,
#endif
                  indicator, accent, footer);
    finish_screen_frame(composed);
}

static int text24_width(const char *text, int max_glyphs) {
    int width = 0;
    const char *cursor = text ? text : "";
    for (int count = 0; *cursor && count < max_glyphs; ++count) {
        width += text24_advance(utf8_next(&cursor));
    }
    return width > 0 ? width - 1 : 0;
}

static void draw_text24_clipped(int x, int y, const char *text, uint16_t fg, uint16_t bg,
                                int max_glyphs) {
    if (!text || !*text || y < 0 || y + 24 > LCD_HEIGHT) return;
    int width = text24_width(text, max_glyphs);
    if (x < 0) x = 0;
    if (x + width > LCD_WIDTH) width = LCD_WIDTH - x;
    if (width <= 0) return;
    const int stripe_rows = 8;
    uint16_t *bitmap = heap_caps_malloc((size_t)width * stripe_rows * sizeof(uint16_t), MALLOC_CAP_DMA);
    if (!bitmap) return;
    for (int strip = 0; strip < 24; strip += stripe_rows) {
        for (int i = 0; i < width * stripe_rows; ++i) bitmap[i] = bg;
        const char *cursor = text;
        int pen = 0;
        for (int count = 0; *cursor && count < max_glyphs; ++count) {
            uint32_t cp = utf8_next(&cursor);
            const uint32_t *rows = cjk24_rows(cp);
            uint8_t dynamic_bitmap[DYNAMIC_GLYPH_BYTES];
            const uint8_t *dynamic = !rows &&
                                             (dynamic_glyph_copy(cp, dynamic_bitmap) ||
                                              full_cjk24_copy(cp, dynamic_bitmap))
                                         ? dynamic_bitmap : NULL;
            for (int row = strip; row < strip + stripe_rows; ++row) {
                for (int col = 0; col < 24; ++col) {
                    int px = pen + col;
                    if (px >= 0 && px < width && glyph24_pixel(cp, rows, dynamic, row, col)) {
                        bitmap[(row - strip) * width + px] = fg;
                    }
                }
            }
            pen += text24_advance(cp);
        }
        draw_bitmap_sync(x, y + strip, x + width, y + strip + stripe_rows, bitmap);
    }
    heap_caps_free(bitmap);
}

static void draw_text24_centered(int y, const char *text, uint16_t fg, uint16_t bg,
                                 int max_glyphs) {
    int width = text24_width(text, max_glyphs);
    draw_text24_clipped((LCD_WIDTH - width) / 2, y, text, fg, bg, max_glyphs);
}

// Weather providers commonly return municipality names such as "北京市",
// while the rest of the device UI identifies the same place as "北京".  Keep
// that presentation rule in the display adapter. Do not strip every final
// "市": labels such as "东莞市" are real city names and must remain intact.
static void copy_ambient_city_label(char *out, size_t out_size, const char *location) {
    if (!out || out_size == 0) return;
    strlcpy(out, location ? location : "", out_size);
    size_t len = strlen(out);
    // Some weather providers include a trailing space in the administrative
    // label. Trim it before recognizing the suffix, otherwise "北京市 " would
    // bypass the presentation normalization below.
    while (len > 0 && (out[len - 1] == ' ' || out[len - 1] == '\t' ||
                       out[len - 1] == '\r' || out[len - 1] == '\n')) {
        out[--len] = '\0';
    }
    static const struct {
        const char *provider_label;
        const char *display_label;
    } municipalities[] = {
        { "\xE5\x8C\x97\xE4\xBA\xAC\xE5\xB8\x82", "\xE5\x8C\x97\xE4\xBA\xAC" }, // 北京市 -> 北京
        { "\xE4\xB8\x8A\xE6\xB5\xB7\xE5\xB8\x82", "\xE4\xB8\x8A\xE6\xB5\xB7" }, // 上海市 -> 上海
        { "\xE5\xA4\xA9\xE6\xB4\xA5\xE5\xB8\x82", "\xE5\xA4\xA9\xE6\xB4\xA5" }, // 天津市 -> 天津
        { "\xE9\x87\x8D\xE5\xBA\x86\xE5\xB8\x82", "\xE9\x87\x8D\xE5\xBA\x86" }, // 重庆市 -> 重庆
    };
    for (size_t i = 0; i < sizeof(municipalities) / sizeof(municipalities[0]); ++i) {
        if (!strcmp(out, municipalities[i].provider_label)) {
            strlcpy(out, municipalities[i].display_label, out_size);
            break;
        }

        // Providers may append a district, country, or another administrative
        // suffix after a municipality (for example "北京市市辖区" or
        // "北京市朝阳区"). The standby slot is a city label, so any value whose
        // leading component is one of the four municipality names normalizes
        // to its familiar short form. Do not apply this to ordinary
        // prefecture-level city names such as "东莞市".
        const size_t provider_len = strlen(municipalities[i].provider_label);
        if (strncmp(out, municipalities[i].provider_label, provider_len) != 0) {
            continue;
        }
        strlcpy(out, municipalities[i].display_label, out_size);
        break;
    }

    /* A terminal 市 is part of the official/common label of many ordinary
     * cities (for example 东莞市).  Only the explicitly listed municipalities
     * above have a compact standby presentation rule; all other provider
     * labels remain byte-for-byte intact after whitespace trimming.
     *
     * Every weather presentation, including the "天气同步中" state, uses this
     * helper.  Bound the label here rather than only in the completed-weather
     * formatter so an administrative provider label can never consume more
     * than four CJK glyph slots on the 240px standby panel. */
    const char *cursor = out;
    for (int glyphs = 0; *cursor && glyphs < 4; ++glyphs) {
        (void)utf8_next(&cursor);
    }
    out[cursor - out] = '\0';
}

static int text24_scaled_width(const char *text, int max_glyphs,
                               int scale_num, int scale_den) {
    int width = 0;
    const char *cursor = text ? text : "";
    for (int count = 0; *cursor && count < max_glyphs; ++count) {
        width += (text24_advance(utf8_next(&cursor)) * scale_num + scale_den / 2) /
                 scale_den;
    }
    return width;
}

static size_t utf8_prefix_bytes(const char *text, int glyphs) {
    const char *cursor = text ? text : "";
    const char *start = cursor;
    for (int count = 0; *cursor && count < glyphs; ++count) {
        (void)utf8_next(&cursor);
    }
    return (size_t)(cursor - start);
}

// Keep the temperature as the non-negotiable tail of the weather row.  A
// provider can return a long compound condition (for example "小雨转中雨"),
// so a fixed whole-line glyph limit can otherwise cut the temperature off even
// though the compact renderer has room for a shorter condition label.
static void format_ambient_weather_line(char *out, size_t out_size,
                                        const char *city, const char *summary,
                                        int temperature_c, bool stale) {
    if (!out || out_size == 0) return;
    const char *safe_city = city ? city : "";
    // The standby weather slot reserves room for the condition and the
    // non-negotiable temperature.  A provider city label can contain district
    // or administrative suffixes, so render at most four Unicode glyphs here
    // even after municipality normalization (for example, \"北京市朝阳区\").
    char city_label[24];
    const size_t city_bytes = utf8_prefix_bytes(safe_city, 4);
    snprintf(city_label, sizeof(city_label), "%.*s", (int)city_bytes, safe_city);
    safe_city = city_label;
    const char *safe_summary = summary ? summary : "";
    const char *stale_marker = stale ? " *" : "";
    const int summary_glyphs = (int)strlen(safe_summary); // upper bound for UTF-8 glyph count

    for (int visible = summary_glyphs; visible >= 0; --visible) {
        const size_t summary_bytes = utf8_prefix_bytes(safe_summary, visible);
        snprintf(out, out_size, "%s%s%.*s %d\xC2\xB0" "C%s",
                 safe_city, safe_city[0] ? " " : "", (int)summary_bytes,
                 safe_summary, temperature_c, stale_marker);
        if (text24_scaled_width(out, 64, AMBIENT_WEATHER_SCALE_NUM,
                                AMBIENT_WEATHER_SCALE_DEN) <= LCD_WIDTH - 4) {
            return;
        }
    }

    // A pathological location must still not hide the temperature.
    snprintf(out, out_size, "%d\xC2\xB0" "C%s", temperature_c, stale_marker);
}

// The weather row uses the native 24px CJK raster. Long provider conditions
// contract before the city or temperature is omitted, keeping the 240px Bread
// panel's right edge safe.
static void draw_text24_scaled_centered(int y, const char *text, uint16_t fg,
                                        uint16_t bg, int max_glyphs,
                                        int scale_num, int scale_den) {
    if (!text || !*text || scale_num < 1 || scale_den < 1 ||
        scale_num > scale_den || y < 0) return;
    const int height = (24 * scale_num + scale_den / 2) / scale_den;
    if (height <= 0 || y + height > LCD_HEIGHT) return;
    // Sum the rounded scaled advances rather than scaling the original total.
    // At this fractional scale the latter underestimates a four-CJK weather
    // line by a few pixels, so the final temperature glyph can be clipped by
    // the DMA bitmap.
    int width = 0;
    const char *measure = text;
    for (int count = 0; *measure && count < max_glyphs; ++count) {
        width += (text24_advance(utf8_next(&measure)) * scale_num + scale_den / 2) /
                 scale_den;
    }
    if (width <= 0 || height <= 0 || width > LCD_WIDTH) return;
    uint16_t *bitmap = heap_caps_malloc((size_t)width * height * sizeof(uint16_t), MALLOC_CAP_DMA);
    if (!bitmap) return;
    for (int i = 0; i < width * height; ++i) bitmap[i] = bg;
    const char *cursor = text;
    int pen = 0;
    for (int count = 0; *cursor && count < max_glyphs; ++count) {
        const uint32_t cp = utf8_next(&cursor);
        const uint32_t *rows = cjk24_rows(cp);
        uint8_t dynamic_bitmap[DYNAMIC_GLYPH_BYTES];
        const uint8_t *dynamic = !rows &&
            (dynamic_glyph_copy(cp, dynamic_bitmap) || full_cjk24_copy(cp, dynamic_bitmap))
                ? dynamic_bitmap : NULL;
        const int advance = text24_advance(cp);
        const int glyph_width = (advance * scale_num + scale_den / 2) / scale_den;
        for (int dy = 0; dy < height; ++dy) {
            const int source_y = dy * scale_den / scale_num;
            for (int dx = 0; dx < glyph_width; ++dx) {
                const int source_x = dx * scale_den / scale_num;
                if (source_x >= 24 || !glyph24_pixel(cp, rows, dynamic, source_y, source_x)) continue;
                const int px = pen + dx;
                if (px >= 0 && px < width) bitmap[(size_t)dy * width + px] = fg;
            }
        }
        pen += glyph_width;
    }
    const int x = (LCD_WIDTH - width) / 2;
    draw_bitmap_sync(x, y, x + width, y + height, bitmap);
    heap_caps_free(bitmap);
}

static void draw_progress_bar(int x, int y, int width, int height, unsigned value,
                              uint16_t track, uint16_t fill) {
    if (value > 100) value = 100;
    uint16_t *bitmap = heap_caps_malloc((size_t)width * height * sizeof(uint16_t), MALLOC_CAP_DMA);
    if (!bitmap) return;
    int filled = width * (int)value / 100;
    for (int py = 0; py < height; ++py) {
        for (int px = 0; px < width; ++px) bitmap[py * width + px] = px < filled ? fill : track;
    }
    draw_bitmap_sync(x, y, x + width, y + height, bitmap);
    heap_caps_free(bitmap);
}

static void fill_rect_solid(int x, int y, int width, int height, uint16_t fill) {
    if (width <= 0 || height <= 0) return;
    if (s_render_target) {
        if (x < 0) { width += x; x = 0; }
        if (y < 0) { height += y; y = 0; }
        if (x + width > LCD_WIDTH) width = LCD_WIDTH - x;
        if (y + height > LCD_HEIGHT) height = LCD_HEIGHT - y;
        for (int row = 0; row < height; ++row) {
            uint16_t *dst = s_render_target + (size_t)(y + row) * LCD_WIDTH + x;
            for (int col = 0; col < width; ++col) dst[col] = fill;
        }
        return;
    }
    const int stripe_rows = 12;
    uint16_t *bitmap = heap_caps_malloc((size_t)width * stripe_rows * sizeof(uint16_t), MALLOC_CAP_DMA);
    if (!bitmap) return;
    for (int i = 0; i < width * stripe_rows; ++i) bitmap[i] = fill;
    for (int py = 0; py < height; py += stripe_rows) {
        int rows = height - py < stripe_rows ? height - py : stripe_rows;
        draw_bitmap_sync(x, y + py, x + width, y + py + rows, bitmap);
    }
    heap_caps_free(bitmap);
}

static bool robot_rounded_box(float x, float y, float left, float top,
                              float right, float bottom, float radius) {
    float cx = x < left + radius ? left + radius :
               (x > right - radius ? right - radius : x);
    float cy = y < top + radius ? top + radius :
               (y > bottom - radius ? bottom - radius : y);
    float dx = x - cx;
    float dy = y - cy;
    return dx * dx + dy * dy <= radius * radius;
}

static uint16_t robot_rgb_mix(uint8_t ar, uint8_t ag, uint8_t ab,
                              uint8_t br, uint8_t bg, uint8_t bb, unsigned mix) {
    if (mix > 255) mix = 255;
    unsigned inv = 255 - mix;
    return color((uint8_t)((ar * inv + br * mix) / 255),
                 (uint8_t)((ag * inv + bg * mix) / 255),
                 (uint8_t)((ab * inv + bb * mix) / 255));
}

static void draw_robot_face_at(const char *state, int offset_x, int offset_y,
                               int scale_percent, uint16_t bg) {
    const bool listening = !strcmp(state, "listening");
    const bool thinking = !strcmp(state, "thinking");
    const bool speaking = !strcmp(state, "speaking");
    const bool alert = !strcmp(state, "alert");
    const bool done = !strcmp(state, "done");
    const uint8_t glow_r = alert ? 255 : thinking ? 177 : speaking || done ? 72 : 55;
    const uint8_t glow_g = alert ? 86 : thinking ? 140 : speaking || done ? 238 : 218;
    const uint8_t glow_b = alert ? 86 : thinking ? 255 : speaking || done ? 158 : 246;
    int width = (240 * scale_percent + 99) / 100;
    int height = (210 * scale_percent + 99) / 100;
    uint16_t *bitmap = NULL;
    size_t bitmap_bytes = (size_t)width * height * sizeof(uint16_t);
    // During double-buffered composition the robot bitmap is copied into a
    // PSRAM frame and never reaches SPI DMA directly. Requiring scarce
    // internal DMA memory here made the head disappear under TLS/HTTP load.
    if (s_render_target) {
        bitmap = heap_caps_malloc(bitmap_bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!bitmap) bitmap = heap_caps_malloc(bitmap_bytes, MALLOC_CAP_DMA);
    if (!bitmap) {
        ESP_LOGE(TAG, "robot bitmap allocation failed: %u bytes", (unsigned)bitmap_bytes);
        return;
    }

    for (int py = 0; py < height; ++py) {
        float y = ((float)py + 0.5f) * 100.0f / scale_percent;
        for (int px = 0; px < width; ++px) {
            float x = ((float)px + 0.5f) * 100.0f / scale_percent;
            uint16_t pixel = bg;

            /* Collar, antenna, and softly rounded ear pods. */
            if (robot_rounded_box(x, y, 91, 181, 149, 207, 11))
                pixel = robot_rgb_mix(38, 67, 91, 96, 133, 158, (unsigned)(175 - y / 2));
            if (robot_rounded_box(x, y, 114, 23, 126, 58, 6))
                pixel = robot_rgb_mix(52, 82, 105, 173, 204, 220, (unsigned)(220 - y * 2));
            float antenna_dx = x - 120.0f, antenna_dy = y - 18.0f;
            float antenna_d2 = antenna_dx * antenna_dx + antenna_dy * antenna_dy;
            if (antenna_d2 < 13.0f * 13.0f)
                pixel = antenna_d2 < 8.0f * 8.0f
                            ? color(glow_r, glow_g, glow_b)
                            : robot_rgb_mix(18, 52, 72, glow_r, glow_g, glow_b, 130);
            float ear_ly = y - 117.0f, ear_lx = x - 26.0f, ear_rx = x - 214.0f;
            float ear_ld2 = ear_lx * ear_lx + ear_ly * ear_ly;
            float ear_rd2 = ear_rx * ear_rx + ear_ly * ear_ly;
            if (ear_ld2 < 27.0f * 27.0f || ear_rd2 < 27.0f * 27.0f) {
                float d2 = ear_ld2 < ear_rd2 ? ear_ld2 : ear_rd2;
                pixel = d2 < 18.0f * 18.0f
                            ? robot_rgb_mix(25, 56, 79, glow_r, glow_g, glow_b, 76)
                            : color(69, 103, 128);
            }

            /* A pearl-metal shell with real RGB565 shading instead of flat blocks. */
            bool outer = robot_rounded_box(x, y, 27, 43, 213, 194, 43);
            bool inner = robot_rounded_box(x, y, 35, 51, 205, 186, 36);
            if (outer) pixel = color(50, 83, 109);
            if (inner) {
                int shade = (int)(205 - y * 54 / 186 - x * 20 / 240);
                if (shade < 116) shade = 116;
                pixel = color((uint8_t)(shade + 22), (uint8_t)(shade + 42),
                              (uint8_t)(shade + 55));
                if (x + y < 136) pixel = robot_rgb_mix(180, 207, 220, 240, 252, 255, 108);
            }

            /* Deep glass face, blue edge light, and a restrained reflection. */
            bool glass_edge = robot_rounded_box(x, y, 47, 67, 193, 171, 29);
            bool glass = robot_rounded_box(x, y, 53, 73, 187, 165, 23);
            if (glass_edge) pixel = robot_rgb_mix(24, 69, 93, glow_r, glow_g, glow_b, 70);
            if (glass) {
                unsigned glass_mix = (unsigned)((y - 73) * 65 / 92);
                pixel = robot_rgb_mix(13, 38, 61, 4, 17, 32, glass_mix);
                if (y < 91 && x + y < 230)
                    pixel = robot_rgb_mix(13, 38, 61, 70, 113, 138, 48);
            }

            if (glass) {
                float eye_y = thinking ? 104.0f : 116.0f;
                float eye_shift = thinking ? -3.0f : 0.0f;
                float left_dx = (x - (87.0f + eye_shift)) / 17.0f;
                float right_dx = (x - (153.0f + eye_shift)) / 17.0f;
                float eye_dy = (y - eye_y) / (alert ? 8.0f : 16.0f);
                float left_d = left_dx * left_dx + eye_dy * eye_dy;
                float right_d = right_dx * right_dx + eye_dy * eye_dy;
                float eye_d = left_d < right_d ? left_d : right_d;

                if (done) {
                    float left_curve = 109.0f + fabsf(x - 87.0f) * 0.30f;
                    float right_curve = 109.0f + fabsf(x - 153.0f) * 0.30f;
                    if ((fabsf(y - left_curve) < 3 && fabsf(x - 87) < 19) ||
                        (fabsf(y - right_curve) < 3 && fabsf(x - 153) < 19))
                        pixel = color(glow_r, glow_g, glow_b);
                } else if (eye_d < 1.55f) {
                    if (eye_d < 1.0f) pixel = color(glow_r, glow_g, glow_b);
                    else pixel = robot_rgb_mix(7, 25, 42, glow_r, glow_g, glow_b, 86);
                    if (!alert && ((x < 87 && x > 78) || (x < 153 && x > 144)) &&
                        y > eye_y - 9 && y < eye_y - 3)
                        pixel = color(236, 255, 255);
                }

                /* Cheeks and distinct mouth language for every application state. */
                float cheek_l = (x - 67) * (x - 67) + (y - 143) * (y - 143);
                float cheek_r = (x - 173) * (x - 173) + (y - 143) * (y - 143);
                if (cheek_l < 7 * 7 || cheek_r < 7 * 7)
                    pixel = robot_rgb_mix(8, 27, 44, glow_r, glow_g, glow_b, 82);
                if (listening) {
                    float md = (x - 120) * (x - 120) / 17.0f / 17.0f +
                               (y - 145) * (y - 145) / 12.0f / 12.0f;
                    if (md < 1.0f && md > 0.38f) pixel = color(glow_r, glow_g, glow_b);
                } else if (thinking) {
                    const float dot_x[3] = {107.0f, 120.0f, 133.0f};
                    for (unsigned i = 0; i < 3; ++i) {
                        float dx = x - dot_x[i];
                        float dy = y - 146.0f;
                        float radius = i == s_thinking_mouth_frame ? 4.5f : 2.5f;
                        if (dx * dx + dy * dy < radius * radius) {
                            pixel = i == s_thinking_mouth_frame
                                        ? color(242, 249, 255)
                                        : robot_rgb_mix(8, 25, 40, glow_r, glow_g, glow_b, 112);
                        }
                    }
                } else if (alert) {
                    if (robot_rounded_box(x, y, 91, 141, 149, 148, 3))
                        pixel = color(glow_r, glow_g, glow_b);
                } else {
                    float smile_y = 139.0f + (x - 120.0f) * (x - 120.0f) / 430.0f;
                    if (fabsf(y - smile_y) < (speaking ? 3.5f : 2.5f) && fabsf(x - 120) < 29)
                        pixel = color(glow_r, glow_g, glow_b);
                    if (speaking && y > 145 && y < 151 && fabsf(x - 120) < 19)
                        pixel = robot_rgb_mix(8, 25, 40, glow_r, glow_g, glow_b, 178);
                }
            }

            /* Two physical status lamps make the head read as a real device. */
            float lamp_l = (x - 57) * (x - 57) + (y - 184) * (y - 184);
            float lamp_r = (x - 183) * (x - 183) + (y - 184) * (y - 184);
            if (lamp_l < 6 * 6 || lamp_r < 6 * 6)
                pixel = color(glow_r, glow_g, glow_b);
            bitmap[(size_t)py * width + px] = pixel;
        }
    }
    draw_bitmap_sync(offset_x, offset_y, offset_x + width, offset_y + height, bitmap);
    heap_caps_free(bitmap);
}

#if CONFIG_MACLAW_BOARD_FANGTANG_4G
static float fangtang_edge(float px, float py, float ax, float ay, float bx, float by) {
    return (px - ax) * (by - ay) - (py - ay) * (bx - ax);
}

static bool fangtang_triangle(float px, float py,
                              float ax, float ay, float bx, float by,
                              float cx, float cy) {
    float ab = fangtang_edge(px, py, ax, ay, bx, by);
    float bc = fangtang_edge(px, py, bx, by, cx, cy);
    float ca = fangtang_edge(px, py, cx, cy, ax, ay);
    return (ab >= 0 && bc >= 0 && ca >= 0) ||
           (ab <= 0 && bc <= 0 && ca <= 0);
}

static float fangtang_segment_distance_sq(float px, float py,
                                           float ax, float ay,
                                           float bx, float by) {
    float dx = bx - ax;
    float dy = by - ay;
    float length_sq = dx * dx + dy * dy;
    float t = length_sq > 0.0f
                  ? ((px - ax) * dx + (py - ay) * dy) / length_sq
                  : 0.0f;
    if (t < 0.0f) t = 0.0f;
    if (t > 1.0f) t = 1.0f;
    float sx = ax + t * dx;
    float sy = ay + t * dy;
    dx = px - sx;
    dy = py - sy;
    return dx * dx + dy * dy;
}

static uint32_t fangtang_texture_hash(unsigned x, unsigned y) {
    uint32_t value = x * 0x45d9f3bu ^ y * 0x119de1f3u ^ 0x9e3779b9u;
    value ^= value >> 16;
    value *= 0x45d9f3bu;
    value ^= value >> 16;
    return value;
}

// Fangtang has its own visual identity. Bread Compact keeps the robot head;
// this single-key board uses a concrete, granulated sugar cube on every
// startup, ambient and interaction surface. RGB + INVON + IDMOFF is corrected
// by panel initialisation, so all colours here remain canonical RGB565.
static void draw_fangtang_cube_at(const char *state, int offset_x, int offset_y,
                                  int scale_percent, uint16_t bg) {
    const int native_width = 140;
    const int native_height = 130;
    const int width = (native_width * scale_percent + 99) / 100;
    const int height = (native_height * scale_percent + 99) / 100;
    const bool listening = !strcmp(state, "listening");
    const bool thinking = !strcmp(state, "thinking");
    const bool speaking = !strcmp(state, "speaking");
    const bool alert = !strcmp(state, "alert");
    const bool done = !strcmp(state, "done");
    const uint8_t accent_r = alert ? 255 : thinking ? 181 : done ? 88 : 70;
    const uint8_t accent_g = alert ? 91 : thinking ? 152 : done ? 232 : 213;
    const uint8_t accent_b = alert ? 82 : thinking ? 255 : done ? 158 : 246;
    size_t bitmap_bytes = (size_t)width * height * sizeof(uint16_t);
    uint16_t *bitmap = NULL;
    if (s_render_target) {
        bitmap = heap_caps_malloc(bitmap_bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!bitmap) bitmap = heap_caps_malloc(bitmap_bytes, MALLOC_CAP_DMA);
    if (!bitmap) {
        ESP_LOGE(TAG, "Fangtang cube allocation failed: %u bytes", (unsigned)bitmap_bytes);
        return;
    }

    // Native cube vertices: top diamond A-B-C-D and two side faces D-C-F-E / C-B-G-F.
    const float ax = 70, ay = 7, bx = 128, by = 38;
    const float cx = 70, cy = 69, dx = 12, dy = 38;
    const float ex = 12, ey = 91, fx = 70, fy = 122, gx = 128, gy = 91;
    for (int py = 0; py < height; ++py) {
        float y = ((float)py + 0.5f) * 100.0f / scale_percent;
        for (int px = 0; px < width; ++px) {
            float x = ((float)px + 0.5f) * 100.0f / scale_percent;
            uint16_t pixel = bg;

            // A restrained floor shadow anchors the mark without turning it
            // into another character or mascot.
            float shadow_x = (x - 70.0f) / 53.0f;
            float shadow_y = (y - 121.0f) / 8.0f;
            float shadow_d2 = shadow_x * shadow_x + shadow_y * shadow_y;
            if (shadow_d2 < 1.0f) {
                unsigned shadow = (unsigned)(34.0f + (1.0f - shadow_d2) * 42.0f);
                pixel = robot_rgb_mix(18, 24, 38, 72, 78, 84, shadow);
            }

            bool top = fangtang_triangle(x, y, ax, ay, bx, by, cx, cy) ||
                       fangtang_triangle(x, y, ax, ay, cx, cy, dx, dy);
            bool left = fangtang_triangle(x, y, dx, dy, cx, cy, fx, fy) ||
                        fangtang_triangle(x, y, dx, dy, fx, fy, ex, ey);
            bool right = fangtang_triangle(x, y, cx, cy, bx, by, gx, gy) ||
                         fangtang_triangle(x, y, cx, cy, gx, gy, fx, fy);
            if (top) {
                unsigned shade = (unsigned)(248 - y * 10 / 69);
                pixel = color((uint8_t)shade, (uint8_t)(shade - 2),
                              (uint8_t)(shade - 12));
            } else if (left) {
                unsigned shade = (unsigned)(230 - (y - 38) * 20 / 84);
                pixel = color((uint8_t)shade, (uint8_t)(shade - 4),
                              (uint8_t)(shade - 16));
            } else if (right) {
                unsigned shade = (unsigned)(213 - (y - 38) * 18 / 84);
                pixel = color((uint8_t)shade, (uint8_t)(shade - 4),
                              (uint8_t)(shade - 13));
            }

            // Fixed micro-granules and occasional pinholes make the faces read
            // as compressed sugar at 240x240. The texture is deterministic, so
            // state animation never causes the cube surface itself to shimmer.
            if (top || left || right) {
                uint32_t grain = fangtang_texture_hash((unsigned)(x * 3.0f),
                                                       (unsigned)(y * 3.0f));
                if ((grain & 0x3fu) == 0u) {
                    unsigned pore = 164u + ((grain >> 8) & 0x0fu);
                    pixel = color((uint8_t)pore, (uint8_t)pore,
                                  (uint8_t)(pore - 5u));
                } else if ((grain & 0x1fu) == 1u) {
                    unsigned crystal = 248u + ((grain >> 8) & 0x03u);
                    pixel = color((uint8_t)crystal, (uint8_t)crystal,
                                  (uint8_t)(crystal - 4u));
                }
            }

            // Fine neutral seams preserve the three-dimensional silhouette
            // without turning the sugar cube into a dark metal box.
            float seam = fangtang_segment_distance_sq(x, y, ax, ay, bx, by);
            float d2 = fangtang_segment_distance_sq(x, y, bx, by, gx, gy);
            if (d2 < seam) seam = d2;
            d2 = fangtang_segment_distance_sq(x, y, gx, gy, fx, fy);
            if (d2 < seam) seam = d2;
            d2 = fangtang_segment_distance_sq(x, y, fx, fy, ex, ey);
            if (d2 < seam) seam = d2;
            d2 = fangtang_segment_distance_sq(x, y, ex, ey, dx, dy);
            if (d2 < seam) seam = d2;
            d2 = fangtang_segment_distance_sq(x, y, dx, dy, ax, ay);
            if (d2 < seam) seam = d2;
            d2 = fangtang_segment_distance_sq(x, y, dx, dy, cx, cy);
            if (d2 < seam) seam = d2;
            d2 = fangtang_segment_distance_sq(x, y, cx, cy, bx, by);
            if (d2 < seam) seam = d2;
            d2 = fangtang_segment_distance_sq(x, y, cx, cy, fx, fy);
            if (d2 < seam) seam = d2;
            if ((top || left || right) && seam < 2.3f) {
                unsigned edge = seam < 0.55f ? 174u : 204u;
                pixel = color((uint8_t)edge, (uint8_t)(edge - 3u),
                              (uint8_t)(edge - 10u));
            }

            // Larger irregular pores on the top plane reinforce the compressed
            // sugar texture. Only the active thinking pore uses a state colour.
            const float crystal_x[7] = {42, 55, 68, 82, 96, 61, 84};
            const float crystal_y[7] = {37, 27, 18, 27, 38, 44, 47};
            for (int i = 0; i < 7 && top; ++i) {
                float qx = x - crystal_x[i], qy = y - crystal_y[i];
                float radius = 1.4f + (float)(i % 3) * 0.45f;
                if (qx * qx + qy * qy < radius * radius) {
                    pixel = i == (s_thinking_mouth_frame % 7) && thinking
                                ? color(accent_r, accent_g, accent_b)
                                : color(188, 185, 174);
                }
            }

            // A small non-character status glyph lives on the front faces.
            if (left || right) {
                if (alert && x > 67 && x < 73 && y > 78 && y < 101) {
                    pixel = color(accent_r, accent_g, accent_b);
                } else if (done) {
                    float check_a = fangtang_segment_distance_sq(x, y, 51, 87, 64, 99);
                    float check_b = fangtang_segment_distance_sq(x, y, 64, 99, 91, 75);
                    if (check_a < 3.0f || check_b < 3.0f)
                        pixel = color(accent_r, accent_g, accent_b);
                } else if (listening || speaking) {
                    int bar = (int)((x - 48) / 11);
                    if (bar >= 0 && bar < 5) {
                        int bar_height = speaking ? 9 + ((bar + s_thinking_mouth_frame) % 3) * 6
                                                  : 8 + (bar % 2) * 5;
                        if (fabsf(x - (52 + bar * 11)) < 2.3f &&
                            y > 91 - bar_height / 2 && y < 91 + bar_height / 2)
                            pixel = color(accent_r, accent_g, accent_b);
                    }
                } else if (thinking) {
                    const float dot_x[3] = {55, 70, 85};
                    for (int i = 0; i < 3; ++i) {
                        float qx = x - dot_x[i], qy = y - 91;
                        float radius = i == s_thinking_mouth_frame ? 4.0f : 2.4f;
                        if (qx * qx + qy * qy < radius * radius)
                            pixel = color(accent_r, accent_g, accent_b);
                    }
                }
            }
            bitmap[(size_t)py * width + px] = pixel;
        }
    }
    draw_bitmap_sync(offset_x, offset_y, offset_x + width, offset_y + height, bitmap);
    heap_caps_free(bitmap);
}

static bool draw_fangtang_sugar_at(int offset_x, int offset_y, int scale_percent,
                                   uint16_t bg) {
    const size_t source_pixels = (size_t)FANGTANG_SUGAR_WIDTH * FANGTANG_SUGAR_HEIGHT;
    const size_t rgb_bytes = (size_t)(_binary_fangtang_sugar_rgb565_end -
                                      _binary_fangtang_sugar_rgb565_start);
    const size_t alpha_bytes = (size_t)(_binary_fangtang_sugar_a8_end -
                                        _binary_fangtang_sugar_a8_start);
    if (rgb_bytes != source_pixels * sizeof(uint16_t) || alpha_bytes != source_pixels) {
        ESP_LOGE(TAG, "invalid Fangtang sugar artwork: rgb=%u alpha=%u",
                 (unsigned)rgb_bytes, (unsigned)alpha_bytes);
        return false;
    }

    const int width = (FANGTANG_SUGAR_WIDTH * scale_percent + 99) / 100;
    const int height = (FANGTANG_SUGAR_HEIGHT * scale_percent + 99) / 100;
    if (width <= 0 || height <= 0 || offset_x < 0 || offset_y < 0 ||
        offset_x + width > LCD_WIDTH || offset_y + height > LCD_HEIGHT) {
        return false;
    }

    const uint16_t *source = (const uint16_t *)_binary_fangtang_sugar_rgb565_start;
    const uint8_t *alpha = _binary_fangtang_sugar_a8_start;
    const size_t output_bytes = (size_t)width * height * sizeof(uint16_t);
    uint16_t *bitmap = s_render_target
                           ? heap_caps_malloc(output_bytes,
                                              MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT)
                           : NULL;
    if (!bitmap) bitmap = heap_caps_malloc(output_bytes, MALLOC_CAP_DMA);
    if (!bitmap) {
        ESP_LOGE(TAG, "Fangtang sugar allocation failed: %u bytes",
                 (unsigned)output_bytes);
        return false;
    }

    const uint16_t bg_native = (uint16_t)((bg << 8) | (bg >> 8));
    const uint8_t bg_r = (uint8_t)((bg_native >> 11) * 255 / 31);
    const uint8_t bg_g = (uint8_t)(((bg_native >> 5) & 0x3f) * 255 / 63);
    const uint8_t bg_b = (uint8_t)((bg_native & 0x1f) * 255 / 31);
    for (int y = 0; y < height; ++y) {
        int source_y = y * FANGTANG_SUGAR_HEIGHT / height;
        for (int x = 0; x < width; ++x) {
            int source_x = x * FANGTANG_SUGAR_WIDTH / width;
            size_t source_index = (size_t)source_y * FANGTANG_SUGAR_WIDTH + source_x;
            uint8_t a = alpha[source_index];
            if (a == 0) {
                bitmap[(size_t)y * width + x] = bg;
                continue;
            }

            /* The embedded file already stores canonical RGB565 in esp_lcd's
             * wire byte order. Decode once for alpha blending, exactly as the
             * ordinary color() path does for the background. */
            uint16_t fg_native = (uint16_t)((source[source_index] << 8) |
                                            (source[source_index] >> 8));
            uint8_t fg_r = (uint8_t)((fg_native >> 11) * 255 / 31);
            uint8_t fg_g = (uint8_t)(((fg_native >> 5) & 0x3f) * 255 / 63);
            uint8_t fg_b = (uint8_t)((fg_native & 0x1f) * 255 / 31);
            uint8_t r = (uint8_t)(((unsigned)fg_r * a +
                                   (unsigned)bg_r * (255u - a) + 127u) / 255u);
            uint8_t g = (uint8_t)(((unsigned)fg_g * a +
                                   (unsigned)bg_g * (255u - a) + 127u) / 255u);
            uint8_t b = (uint8_t)(((unsigned)fg_b * a +
                                   (unsigned)bg_b * (255u - a) + 127u) / 255u);
            bitmap[(size_t)y * width + x] = color(r, g, b);
        }
    }

    draw_bitmap_sync(offset_x, offset_y, offset_x + width, offset_y + height, bitmap);
    heap_caps_free(bitmap);
    return true;
}

static void draw_fangtang_activity_indicator(const char *state, int center_x,
                                              int center_y, uint16_t bg) {
    const uint16_t active = !strcmp(state, "thinking") ? color(196, 169, 255) :
                            !strcmp(state, "listening") ? color(96, 220, 255) :
                            !strcmp(state, "speaking") ? color(104, 240, 170) :
                            color(220, 225, 230);
    if (!strcmp(state, "thinking")) {
        for (int i = 0; i < 3; ++i) {
            const int radius = i == (int)s_thinking_mouth_frame ? 4 : 2;
            const int dot_x = center_x + (i - 1) * 15;
            for (int y = -4; y <= 4; ++y) {
                for (int x = -4; x <= 4; ++x) {
                    if (x * x + y * y <= radius * radius) {
                        fill_rect_solid(dot_x + x, center_y + y, 1, 1, active);
                    }
                }
            }
        }
        return;
    }
    if (!strcmp(state, "listening") || !strcmp(state, "speaking")) {
        for (int i = 0; i < 5; ++i) {
            int height = !strcmp(state, "speaking")
                             ? 5 + ((i + (int)s_thinking_mouth_frame) % 3) * 4
                             : 5 + (i % 2) * 4;
            fill_rect_solid(center_x - 22 + i * 10, center_y - height / 2,
                            3, height, active);
        }
        return;
    }
    fill_rect_solid(center_x - 14, center_y - 1, 28, 2, bg);
}

static void draw_fangtang_thinking_frame(void) {
    const int left = LCD_WIDTH / 2 - 22;
    const int top = 145;
    const int width = 45;
    const int height = 11;
    const uint16_t bg = state_color("thinking");
    const uint16_t active = color(196, 169, 255);

    /* Patch only the activity strip. Fangtang presents complete frames for
     * ordinary page transitions because its proven NV3023 path is row-wise,
     * but a three-dot tick must not resend all 57,600 pixels while HTTP voice
     * upload and the Hub poller are active. Keep the front framebuffer in sync
     * so the next composed transaction-stage draw cannot restore stale dots. */
    if (!s_present_staging || width > LCD_WIDTH) return;
    uint16_t *front = s_front_frame_valid ? s_framebuffers[s_front_frame] : NULL;
    for (int row = 0; row < height; ++row) {
        for (int x = 0; x < width; ++x) s_present_staging[x] = bg;
        for (int dot = 0; dot < 3; ++dot) {
            const int radius = dot == (int)s_thinking_mouth_frame ? 4 : 2;
            const int center_x = 22 + (dot - 1) * 15;
            const int dy = row - 5;
            for (int dx = -4; dx <= 4; ++dx) {
                int px = center_x + dx;
                if (px >= 0 && px < width &&
                    dx * dx + dy * dy <= radius * radius) {
                    s_present_staging[px] = active;
                }
            }
        }
        esp_err_t err = panel_draw_bitmap_sync(
            left, top + row, left + width, top + row + 1, s_present_staging);
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "Fangtang thinking row %d update failed: %s",
                     row, esp_err_to_name(err));
            s_front_frame_valid = false;
            return;
        }
        if (front) {
            memcpy(front + (size_t)(top + row) * LCD_WIDTH + left,
                   s_present_staging, (size_t)width * sizeof(uint16_t));
        }
    }
}
#endif

static inline uint16_t remote_pet_panel_rgb565(uint16_t pixel) {
    /* Hub/GUI frames and this renderer are canonical RGB565. Fangtang now uses
     * RGB MADCTL order just like the media, so no per-pet color conversion is
     * needed. The compositor still performs the one required wire byte swap. */
    return pixel;
}

static inline uint16_t composite_remote_pet_pixel(const uint8_t *source,
                                                  size_t source_index,
                                                  uint16_t background) {
    uint16_t pet = (uint16_t)source[source_index] |
                   (uint16_t)((uint16_t)source[source_index + 1] << 8);
    pet = remote_pet_panel_rgb565(pet);
    uint8_t alpha = source[source_index + 2];
    if (alpha == 255) return (uint16_t)((pet << 8) | (pet >> 8));
    if (alpha == 0) return (uint16_t)((background << 8) | (background >> 8));

    uint32_t inv = 255u - alpha;
    uint32_t pr = (pet >> 11) & 0x1fu;
    uint32_t pg = (pet >> 5) & 0x3fu;
    uint32_t pb = pet & 0x1fu;
    uint32_t br = (background >> 11) & 0x1fu;
    uint32_t bgc = (background >> 5) & 0x3fu;
    uint32_t bb = background & 0x1fu;
    uint16_t blended =
        (uint16_t)((((pr * alpha + br * inv + 127u) / 255u) << 11) |
                   (((pg * alpha + bgc * inv + 127u) / 255u) << 5) |
                   ((pb * alpha + bb * inv + 127u) / 255u));
    return (uint16_t)((blended << 8) | (blended >> 8));
}

static bool response_internal_metadata_line(const char *start, const char *end) {
    while (start < end && (*start == ' ' || *start == '\t')) ++start;
    // Accept presentation markers left behind by Markdown conversion, but only
    // at a physical line boundary. This keeps ordinary prose containing the
    // same words intact.
    bool advanced = true;
    while (advanced && start < end) {
        advanced = false;
        if (*start == '>' || *start == '`' || *start == '*') {
            ++start;
            advanced = true;
        } else if (*start == '-' && start + 1 < end && start[1] == ' ') {
            start += 2;
            advanced = true;
        }
        while (start < end && (*start == ' ' || *start == '\t')) ++start;
    }
    static const char *const prefixes[] = {
        "route task:", "route source:", "route model:",
        "route reason:", "route escalated:", "cost tier:",
        "input tokens:", "output tokens:", "total tokens:",
        "cache read tokens:", "cache write tokens:",
        "no aux/route",
    };
    for (size_t i = 0; i < sizeof(prefixes) / sizeof(prefixes[0]); ++i) {
        size_t prefix_len = strlen(prefixes[i]);
        if ((size_t)(end - start) >= prefix_len &&
            !strncasecmp(start, prefixes[i], prefix_len)) {
            return true;
        }
    }
    return false;
}

static void response_copy_without_internal_metadata(char *dst, size_t dst_size,
                                                     const char *text) {
    if (!dst || dst_size == 0) return;
    dst[0] = '\0';
    const char *cursor = text ? text : "";
    size_t used = 0;
    bool first_content = true;
    while (*cursor && used + 1 < dst_size) {
        const char *line = cursor;
        const char *line_end = strchr(line, '\n');
        if (!line_end) line_end = line + strlen(line);
        const char *content = line;
        while (content < line_end && (*content == ' ' || *content == '\t' || *content == '\r')) ++content;
        if (first_content && line_end - content >= 3 && content[0] == '[' &&
            (content[1] == 'i' || content[1] == 'I') && content[2] == ']' &&
            (content + 3 == line_end || content[3] == ' ' || content[3] == '\t' || content[3] == '\r')) {
            content += 3;
            while (content < line_end && (*content == ' ' || *content == '\t')) ++content;
        }
        const char *trimmed_end = line_end;
        while (trimmed_end > content && (trimmed_end[-1] == ' ' || trimmed_end[-1] == '\t' || trimmed_end[-1] == '\r')) --trimmed_end;
        bool empty = content == trimmed_end;
        if (!empty && !response_internal_metadata_line(content, trimmed_end)) {
            size_t bytes = (size_t)(line_end - content);
            if (bytes > dst_size - used - 1) bytes = dst_size - used - 1;
            memcpy(dst + used, content, bytes);
            used += bytes;
            first_content = false;
            if (*line_end == '\n' && used + 1 < dst_size) dst[used++] = '\n';
        } else if (empty && used > 0 && dst[used - 1] != '\n' && used + 1 < dst_size) {
            dst[used++] = '\n';
        }
        cursor = *line_end == '\n' ? line_end + 1 : line_end;
    }
    while (used > 0 && (dst[used - 1] == '\n' || dst[used - 1] == '\r' ||
                        dst[used - 1] == ' ' || dst[used - 1] == '\t')) --used;
    dst[used] = '\0';
    const char *visible = response_visible_start(dst);
    if (visible != dst) memmove(dst, visible, strlen(visible) + 1);
}

static inline uint16_t composite_remote_pet_sample(const uint8_t *source,
                                                   size_t source_index,
                                                   bool valid,
                                                   uint16_t background) {
    if (!valid) return (uint16_t)((background << 8) | (background >> 8));
    return composite_remote_pet_pixel(source, source_index, background);
}

static inline uint16_t composite_interpolated_remote_pet_samples(
    const uint8_t *first_source, const uint8_t *second_source,
    size_t first_index, size_t second_index,
    bool first_valid, bool second_valid,
    uint16_t background, uint32_t mix) {
    if (!mix || first_source == second_source) {
        return composite_remote_pet_sample(first_source, first_index, first_valid,
                                           background);
    }
    if (mix >= 256u) {
        return composite_remote_pet_sample(second_source, second_index, second_valid,
                                           background);
    }
    uint32_t inverse = 256u - mix;
    uint32_t first_alpha = first_valid ? first_source[first_index + 2] : 0u;
    uint32_t second_alpha = second_valid ? second_source[second_index + 2] : 0u;

    uint16_t first = first_valid
                         ? (uint16_t)first_source[first_index] |
                               (uint16_t)((uint16_t)first_source[first_index + 1] << 8)
                         : 0;
    uint16_t second = second_valid
                          ? (uint16_t)second_source[second_index] |
                                (uint16_t)((uint16_t)second_source[second_index + 1] << 8)
                          : 0;
    if (first_valid) first = remote_pet_panel_rgb565(first);
    if (second_valid) second = remote_pet_panel_rgb565(second);
    uint32_t alpha = (first_alpha * inverse + second_alpha * mix + 128u) >> 8;
    if (!alpha) return (uint16_t)((background << 8) | (background >> 8));
    // Interpolate premultiplied color so a transparent pixel's unused RGB
    // cannot create a dark fringe while the silhouette moves between frames.
    uint32_t premul_red = ((((first >> 11) & 0x1fu) * first_alpha * inverse) +
                           (((second >> 11) & 0x1fu) * second_alpha * mix) +
                           128u) >> 8;
    uint32_t premul_green = ((((first >> 5) & 0x3fu) * first_alpha * inverse) +
                             (((second >> 5) & 0x3fu) * second_alpha * mix) +
                             128u) >> 8;
    uint32_t premul_blue = (((first & 0x1fu) * first_alpha * inverse) +
                            ((second & 0x1fu) * second_alpha * mix) +
                            128u) >> 8;
    uint32_t background_red = (background >> 11) & 0x1fu;
    uint32_t background_green = (background >> 5) & 0x3fu;
    uint32_t background_blue = background & 0x1fu;
    uint32_t inverse_alpha = 255u - alpha;
    uint16_t blended = (uint16_t)((((premul_red + background_red * inverse_alpha + 127u) /
                                    255u) << 11) |
                                  (((premul_green + background_green * inverse_alpha + 127u) /
                                    255u) << 5) |
                                  ((premul_blue + background_blue * inverse_alpha + 127u) /
                                   255u));
    return (uint16_t)((blended << 8) | (blended >> 8));
}

static int remote_pet_q8_nearest(int32_t value) {
    return value >= 0 ? (int)((value + 128) / 256)
                      : -(int)((-value + 128) / 256);
}

static bool remote_pet_sample_index(int32_t x_q8, int32_t y_q8,
                                    size_t *source_index) {
    int x = remote_pet_q8_nearest(x_q8);
    int y = remote_pet_q8_nearest(y_q8);
    if (x < 0 || y < 0 || x >= (int)s_remote_pet_width ||
        y >= (int)s_remote_pet_height) {
        *source_index = 0;
        return false;
    }
    *source_index = ((size_t)y * s_remote_pet_width + (size_t)x) * 3u;
    return true;
}

static bool remote_pet_target_size(size_t width, size_t height,
                                   size_t *out_width, size_t *out_height) {
    if (!width || !height || !out_width || !out_height) return false;
    size_t target_width = AMBIENT_PET_MAX_WIDTH;
    size_t target_height = target_width * height / width;
    if (target_height > AMBIENT_PET_MAX_HEIGHT) {
        target_height = AMBIENT_PET_MAX_HEIGHT;
        target_width = target_height * width / height;
    }
    if (!target_width || !target_height || target_width > AMBIENT_PET_MAX_WIDTH ||
        target_height > AMBIENT_PET_MAX_HEIGHT) return false;
    *out_width = target_width;
    *out_height = target_height;
    return true;
}

bool board_port_get_pet_asset_install_budget(size_t source_width, size_t source_height,
                                             size_t frame_count, size_t *out_external_bytes) {
    if (!out_external_bytes || frame_count > REMOTE_PET_MAX_FRAMES) return false;
    if (frame_count == 0) {
        *out_external_bytes = 0;
        return true;
    }
    size_t target_width = 0, target_height = 0;
    if (!remote_pet_target_size(source_width, source_height, &target_width, &target_height) ||
        target_width > SIZE_MAX / target_height ||
        target_width * target_height > SIZE_MAX / 3u ||
        target_width * target_height * 3u > SIZE_MAX / frame_count) {
        return false;
    }
    *out_external_bytes = target_width * target_height * 3u * frame_count;
    return true;
}

static void scale_remote_pet_frame(const uint8_t *source, size_t source_width,
                                   size_t source_height, uint8_t *destination,
                                   size_t target_width, size_t target_height) {
    if (source_width == target_width && source_height == target_height) {
        memcpy(destination, source, source_width * source_height * 3u);
        return;
    }
    for (size_t y = 0; y < target_height; ++y) {
        uint32_t source_y_fp = target_height > 1
                                   ? (uint32_t)(y * (source_height - 1u) * 256u /
                                                (target_height - 1u))
                                   : 0;
        size_t y0 = source_y_fp >> 8;
        size_t y1 = y0 + 1u < source_height ? y0 + 1u : y0;
        uint32_t fy = source_y_fp & 0xffu;
        for (size_t x = 0; x < target_width; ++x) {
            uint32_t source_x_fp = target_width > 1
                                       ? (uint32_t)(x * (source_width - 1u) * 256u /
                                                    (target_width - 1u))
                                       : 0;
            size_t x0 = source_x_fp >> 8;
            size_t x1 = x0 + 1u < source_width ? x0 + 1u : x0;
            uint32_t fx = source_x_fp & 0xffu;
            uint32_t weights[4] = {
                (256u - fx) * (256u - fy), fx * (256u - fy),
                (256u - fx) * fy, fx * fy,
            };
            size_t indexes[4] = {
                (y0 * source_width + x0) * 3u,
                (y0 * source_width + x1) * 3u,
                (y1 * source_width + x0) * 3u,
                (y1 * source_width + x1) * 3u,
            };
            uint32_t alpha_sum = 0, red_sum = 0, green_sum = 0, blue_sum = 0;
            for (size_t sample = 0; sample < 4; ++sample) {
                size_t index = indexes[sample];
                uint16_t pixel = (uint16_t)source[index] |
                                 (uint16_t)((uint16_t)source[index + 1] << 8);
                uint32_t alpha = source[index + 2];
                uint32_t weight = weights[sample];
                alpha_sum += alpha * weight;
                red_sum += ((pixel >> 11) & 0x1fu) * alpha * weight;
                green_sum += ((pixel >> 5) & 0x3fu) * alpha * weight;
                blue_sum += (pixel & 0x1fu) * alpha * weight;
            }
            uint32_t alpha = (alpha_sum + 32768u) >> 16;
            uint32_t red = 0, green = 0, blue = 0;
            if (alpha) {
                red = (((red_sum + 32768u) >> 16) + alpha / 2u) / alpha;
                green = (((green_sum + 32768u) >> 16) + alpha / 2u) / alpha;
                blue = (((blue_sum + 32768u) >> 16) + alpha / 2u) / alpha;
            }
            uint16_t pixel = (uint16_t)((red << 11) | (green << 5) | blue);
            size_t destination_index = (y * target_width + x) * 3u;
            destination[destination_index] = (uint8_t)pixel;
            destination[destination_index + 1] = (uint8_t)(pixel >> 8);
            destination[destination_index + 2] = (uint8_t)alpha;
        }
        /* Eight high-resolution frames are scaled during startup immediately
         * after Wi-Fi obtains an address.  Let the idle task run regularly so
         * this CPU-heavy bilinear pass cannot starve the task watchdog. */
        if ((y & 7u) == 7u) vTaskDelay(1);
    }
}

static bool draw_remote_pet_frame(uint16_t bg) {
    if (!s_remote_pet_frame_count || !s_remote_pet_frames[0] ||
        !s_remote_pet_width || !s_remote_pet_height) return false;

    /* Advance from presented animation ticks, not wall-clock time. TLS, flash
     * GC, or a full-screen transition can delay one render substantially; a
     * wall-clock phase then skips across several poses on the next tick and is
     * perceived as a jump. The animation task advances this phase exactly once
     * per displayed tick, so load can slow motion slightly but never teleport
     * the character. Ordinary UI redraws reuse the current pose. */
    uint64_t elapsed_ms = s_remote_pet_animation_elapsed_ms;
    size_t index = 0;
    size_t next_index = 0;
    uint32_t mix = 0;
    if (s_remote_pet_frame_count > 1 && s_remote_pet_frame_ms) {
        index = (elapsed_ms / s_remote_pet_frame_ms) % s_remote_pet_frame_count;
        next_index = (index + 1u) % s_remote_pet_frame_count;
        mix = (uint32_t)((elapsed_ms % s_remote_pet_frame_ms) * 256u /
                         s_remote_pet_frame_ms);
    }
    const uint8_t *source = s_remote_pet_frames[index];
    const uint8_t *next_source = s_remote_pet_frame_count > 1
                                     ? s_remote_pet_frames[next_index]
                                     : source;
    if (!source) return false;
    if (!next_source) {
        next_source = source;
        mix = 0;
    }

    // Frames are pre-scaled once when installed. The animation hot path only
    // composites them, avoiding nearest-neighbour resampling on every tick.
    int target_w = (int)s_remote_pet_width;
    int target_h = (int)s_remote_pet_height;
    if (target_w < 1 || target_h < 1 || target_w > AMBIENT_PET_MAX_WIDTH ||
        target_h > AMBIENT_PET_MAX_HEIGHT) return false;
    int left = (LCD_WIDTH - target_w) / 2;
    int top = AMBIENT_PET_TOP + (AMBIENT_PET_MAX_HEIGHT - target_h) / 2;
    if (left < 0 || top < 0 || left + target_w > LCD_WIDTH ||
        top + target_h > LCD_HEIGHT) return false;
    uint16_t background = (uint16_t)((bg << 8) | (bg >> 8));
    /* Every exported pose already uses the same 256x256 authored canvas. Keep
     * that canvas fixed for the whole loop. Alpha-centroid compensation reacts
     * to a moving claw, ear, or eye as if the complete character translated,
     * which shifts the body between ticks and leaves a visible LCD trail. */
    // Compose directly into the back buffer. The old path allocated and freed
    // a full scaled bitmap for every animation frame, fragmenting PSRAM and
    // adding an avoidable full-frame copy every 700 ms.
    if (s_render_target) {
        for (int y = 0; y < target_h; ++y) {
            uint16_t *destination = s_render_target +
                                    (size_t)(top + y) * LCD_WIDTH + left;
            for (int x = 0; x < target_w; ++x) {
                size_t first_index = 0, second_index = 0;
                bool first_valid = remote_pet_sample_index(
                    (int32_t)x * 256,
                    (int32_t)y * 256, &first_index);
                bool second_valid = remote_pet_sample_index(
                    (int32_t)x * 256,
                    (int32_t)y * 256, &second_index);
                destination[x] = composite_interpolated_remote_pet_samples(
                    source, next_source, first_index, second_index,
                    first_valid, second_valid, background, mix);
            }
        }
        return true;
    }

    // Direct-draw fallback is only used when the double framebuffer could not
    // be allocated. Reuse the persistent DMA staging area in 16-row bands
    // instead of allocating another bitmap or issuing 150 one-row transfers.
    if (!s_present_staging) return false;
    for (int band_y = 0; band_y < target_h; band_y += LCD_STRIPE_ROWS) {
        int rows = target_h - band_y < LCD_STRIPE_ROWS
                       ? target_h - band_y : LCD_STRIPE_ROWS;
        for (int row = 0; row < rows; ++row) {
            int y = band_y + row;
            uint16_t *destination = s_present_staging + (size_t)row * target_w;
            for (int x = 0; x < target_w; ++x) {
                size_t first_index = 0, second_index = 0;
                bool first_valid = remote_pet_sample_index(
                    (int32_t)x * 256,
                    (int32_t)y * 256, &first_index);
                bool second_valid = remote_pet_sample_index(
                    (int32_t)x * 256,
                    (int32_t)y * 256, &second_index);
                destination[x] = composite_interpolated_remote_pet_samples(
                    source, next_source, first_index, second_index,
                    first_valid, second_valid, background, mix);
            }
        }
        if (panel_draw_bitmap_sync(left, top + band_y, left + target_w,
                                   top + band_y + rows,
                                   s_present_staging) != ESP_OK) return false;
    }
    return true;
}

/* Animate only the small glass-mouth region. Repainting the complete 240x320
 * framebuffer for three dots wastes SPI bandwidth and can make the otherwise
 * double-buffered thinking page appear to pulse under TLS load. */
static void draw_thinking_mouth_frame(void) {
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    /* Fangtang has no robot mouth. Patch only its compact activity strip. */
    draw_fangtang_thinking_frame();
    return;
#else
    const int source_left = 99;
    const int source_top = 137;
    const int source_width = 42;
    const int source_height = 18;
    const int scale_percent = 92;
    const int output_x = 10 + source_left * scale_percent / 100;
    const int output_y = source_top * scale_percent / 100;
    const int width = (source_width * scale_percent + 99) / 100;
    const int height = (source_height * scale_percent + 99) / 100;
    uint16_t *bitmap = s_present_staging;
    const uint8_t glow_r = 177, glow_g = 140, glow_b = 255;

    // esp_lcd queues DMA from the supplied buffer. A task-stack bitmap can be
    // reclaimed while the transfer is still completing and corrupt the task
    // stack, which caused the reset seen on the first animation frame. Reuse
    // the board's persistent internal-DMA staging allocation instead.
    if (!bitmap) return;

    for (int py = 0; py < height; ++py) {
        float y = source_top + ((float)py + 0.5f) * 100.0f / scale_percent;
        for (int px = 0; px < width; ++px) {
            float x = source_left + ((float)px + 0.5f) * 100.0f / scale_percent;
            unsigned glass_mix = (unsigned)((y - 73.0f) * 65.0f / 92.0f);
            uint16_t pixel = robot_rgb_mix(13, 38, 61, 4, 17, 32, glass_mix);
            const float dot_x[3] = {107.0f, 120.0f, 133.0f};
            for (unsigned i = 0; i < 3; ++i) {
                float dx = x - dot_x[i];
                float dy = y - 146.0f;
                float radius = i == s_thinking_mouth_frame ? 4.5f : 2.5f;
                if (dx * dx + dy * dy < radius * radius) {
                    pixel = i == s_thinking_mouth_frame
                                ? color(242, 249, 255)
                                : robot_rgb_mix(8, 25, 40, glow_r, glow_g, glow_b, 112);
                }
            }
            bitmap[(size_t)py * width + px] = pixel;
        }
    }
    draw_bitmap_sync(output_x, output_y, output_x + width, output_y + height, bitmap);

    // Keep the front framebuffer authoritative. Every later double-buffered
    // screen starts by comparing against it; without this patch the mouth
    // animation lives only on the LCD and can be unexpectedly restored by a
    // subsequent partial presentation.
    if (s_front_frame_valid && s_framebuffers[s_front_frame]) {
        uint16_t *front = s_framebuffers[s_front_frame];
        for (int row = 0; row < height; ++row) {
            memcpy(front + (size_t)(output_y + row) * LCD_WIDTH + output_x,
                   bitmap + (size_t)row * width, (size_t)width * sizeof(uint16_t));
        }
    }
#endif
}

static void thinking_mouth_task(void *arg) {
    (void)arg;
    while (true) {
        if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(THINKING_MOUTH_FRAME_MS)) != 0) {
            break;
        }
        xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
        // Re-check every terminal ownership flag while holding the same LCD
        // lock used by full-screen transitions.  A final response can arrive
        // exactly when this periodic task wakes; this guard guarantees the
        // three-dot marquee stops before the result surface is committed.
        if (s_thinking_surface_visible && !s_recording_active &&
            !s_response_active && s_foreground_surface &&
            !strcmp(s_state, "thinking")) {
            s_thinking_mouth_frame = (s_thinking_mouth_frame + 1) % 3;
            draw_thinking_mouth_frame();
        }
        xSemaphoreGiveRecursive(s_lcd_mutex);
    }
    if (s_thinking_mouth_stopped) xSemaphoreGive(s_thinking_mouth_stopped);
    vTaskDelete(NULL);
}

static void show_state_screen(const char *state) {
    s_thinking_surface_visible = false;
    uint16_t bg = state_color(state);
    bool composed = begin_screen_frame();
    fill_screen(bg);
    bool ambient = !strcmp(state, "idle") || !strcmp(state, "quiet");
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    if (ambient) {
        const char *clock_text = s_ambient_time[0] ? s_ambient_time : "--:--:--";
        // Row one is a single balanced status line. Keep the Chinese status
        // immediately beside the clock instead of spending an entire row on it.
        const char *network_text = s_gateway_ready ? "在线" : "等待";
        size_t clock_len = strlen(clock_text);
        int network_width = text24_width(network_text, 2);
        int first_row_width = (int)clock_len * 18 + 8 + network_width;
        int first_row_x = (LCD_WIDTH - first_row_width) / 2;
        if (first_row_x < 2) first_row_x = 2;
        draw_ascii_at(first_row_x, 6, clock_text, color(240, 248, 255), bg);
        draw_text24_clipped(first_row_x + (int)clock_len * 18 + 8, 4, network_text,
                            s_gateway_ready ? color(91, 224, 149) : color(245, 184, 75),
                            bg, 2);

        // The second and final information row is date + weekday. Everything
        // below it belongs to the Fangtang mark.
        char calendar[64];
        snprintf(calendar, sizeof(calendar), "%s %s", s_ambient_date, s_ambient_weekday);
        const int calendar_width = text24_width(calendar, 9);
        // Both transports are rendered as icon + explicit small text. Reserve
        // each mark's real painted width before centring the complete row.
        // WIFI uses 4 glyphs x 12 px = 48 px after its 22 px icon offset.
        // The final 2 px are spacing rather than painted pixels, so reserve
        // the exact 68 px extent and keep the centred row from drifting left.
        const int transport_width = s_network_transport_cellular ? 38 : 68;
        // Leave a clearly visible pause after the calendar before the compact
        // radio mark. On this dense 240 px row, five pixels made the first
        // signal bar read like another date glyph.
        const int calendar_gap = 9;
        int calendar_x = (LCD_WIDTH - calendar_width - calendar_gap - transport_width) / 2;
        if (calendar_x < 2) calendar_x = 2;
        draw_text24_clipped(calendar_x, 32, calendar, color(166, 194, 216), bg, 9);
        const int icon_x = calendar_x + calendar_width + calendar_gap;
        const uint16_t icon_color = s_gateway_ready ? color(91, 224, 149)
                                                     : color(166, 194, 216);
        if (s_network_transport_cellular) {
            // Compact signal bars followed by a literal 4G label stay legible
            // on this 240 px panel and do not depend on an extra font glyph.
            fill_rect_solid(icon_x, 48, 2, 4, icon_color);
            fill_rect_solid(icon_x + 4, 45, 2, 7, icon_color);
            fill_rect_solid(icon_x + 8, 42, 2, 10, icon_color);
            // The smaller label paints rows 37..43, above the sugar artwork.
            draw_fangtang_network_label(icon_x + 14, 37, true, icon_color, bg);
        } else {
            // Three nested arcs and a dot form the familiar Wi-Fi icon.
            for (int y = 40; y <= 50; ++y) {
                for (int x = 0; x < 18; ++x) {
                    float dx = (float)x - 8.5f;
                    float dy = (float)y - 52.0f;
                    float radius = sqrtf(dx * dx + dy * dy);
                    bool upper = dy < 0.0f;
                    if (upper && ((radius > 8.0f && radius < 9.5f) ||
                                  (radius > 5.0f && radius < 6.5f) ||
                                  (radius > 2.1f && radius < 3.4f))) {
                        fill_rect_solid(icon_x + x, y, 1, 1, icon_color);
                    }
                }
            }
            fill_rect_solid(icon_x + 8, 51, 3, 2, icon_color);
            // The former Wi-Fi branch drew only the radio waves. Keep an
            // explicit label so the selected uplink is unambiguous at a glance.
            draw_fangtang_network_label(icon_x + 22, 39, false,
                                         color(240, 248, 255), bg);
        }

        // The sugar cube is the product/startup mark, not the standby pet.
        // Once Hub supplies the selected pet, reuse the same transparent,
        // animated asset pipeline as Bread Compact in the Fangtang-sized area.
        // Keep the sugar only as a bounded loading/offline fallback.
        if (!draw_remote_pet_frame(bg) && !draw_fangtang_sugar_at(26, 68, 100, bg)) {
            draw_fangtang_cube_at(state, 36, 70, 120, bg);
        }
    } else {
        const char *label = !strcmp(state, "listening") ? "正在听取" :
                            !strcmp(state, "thinking") ? s_command_stage :
                            !strcmp(state, "speaking") ? "正在回复" :
                            !strcmp(state, "alert") ? "请注意" :
                            !strcmp(state, "done") ? "处理完成" : "方糖";
        if (!draw_fangtang_sugar_at(43, 4, 82, bg)) {
            draw_fangtang_cube_at(state, 57, 5, 90, bg);
        }
        draw_fangtang_activity_indicator(state, LCD_WIDTH / 2, 150, bg);
        draw_text24_centered(166, label, color(248, 252, 255), bg, 8);
        draw_text24_centered(207,
                             !strcmp(state, "thinking") ? "双击激活键可取消" : "请稍候",
                             color(145, 220, 235), bg, 8);
    }
#else
    if (ambient) {
        const char *clock_text = s_ambient_time[0] ? s_ambient_time : "--:--:--";
        const char *connection_text = s_gateway_ready ? "在线" :
                                      (s_wifi_connected ? "服务中" : "联网中");
        const uint16_t connection_color = s_gateway_ready
                                              ? color(91, 224, 149)
                                              : color(245, 184, 75);
        size_t clock_glyphs = strlen(clock_text);
        if (clock_glyphs > 12) clock_glyphs = 12;
        int clock_width = (int)clock_glyphs * 18;
        int connection_width = text24_width(connection_text, 3);
        int first_row_x = (LCD_WIDTH - clock_width - 8 - connection_width) / 2;
        if (first_row_x < 4) first_row_x = 4;
        draw_ascii_at(first_row_x, 8, clock_text, color(240, 248, 255), bg);
        draw_text24_clipped(first_row_x + clock_width + 8, 6, connection_text,
                            connection_color, bg, 3);
        char calendar[64];
        snprintf(calendar, sizeof(calendar), "%s %s", s_ambient_date, s_ambient_weekday);
        const uint16_t calendar_color = color(166, 194, 216);
        if (s_alarm_scheduled) {
            const int icon_width = 14;
            const int calendar_width = text24_width(calendar, 10);
            const int group_x = (LCD_WIDTH - icon_width - 5 - calendar_width) / 2;
            draw_alarm_indicator(group_x, 43, calendar_color);
            draw_text24_clipped(group_x + icon_width + 5, 38, calendar,
                                calendar_color, bg, 10);
        } else {
            draw_text24_centered(38, calendar, calendar_color, bg, 10);
        }
        char city_label[sizeof(s_ambient_location)];
        copy_ambient_city_label(city_label, sizeof(city_label), s_ambient_location);
        char weather[96];
        if (s_ambient_weather_valid) {
            format_ambient_weather_line(weather, sizeof(weather), city_label,
                                        s_ambient_weather, s_ambient_temperature,
                                        s_ambient_weather_stale);
        } else {
            snprintf(weather, sizeof(weather), "%s 天气同步中", city_label);
        }
        // Weather uses the native 24px standby style. The line formatter
        // preserves city and temperature before shortening a long weather
        // description.
        draw_text24_scaled_centered(AMBIENT_WEATHER_TEXT_Y, weather,
                                    color(121, 210, 224), bg, 64,
                                    AMBIENT_WEATHER_SCALE_NUM,
                                    AMBIENT_WEATHER_SCALE_DEN);

        // Everything below the three compact information rows belongs to the
        // pet. There is deliberately no ready/tagline row or bottom status
        // row: connection state lives beside the clock and weather lives
        // immediately below the calendar.
        if (!draw_remote_pet_frame(bg)) {
            const int native_width = (240 * AMBIENT_NATIVE_PET_SCALE + 99) / 100;
            const int native_height = (210 * AMBIENT_NATIVE_PET_SCALE + 99) / 100;
            draw_robot_face_at(
                state,
                (LCD_WIDTH - native_width) / 2,
                AMBIENT_PET_TOP + (AMBIENT_PET_MAX_HEIGHT - native_height) / 2,
                AMBIENT_NATIVE_PET_SCALE,
                bg);
        }
    } else {
        const char *label = !strcmp(state, "listening") ? "正在听取" :
                            !strcmp(state, "thinking") ? s_command_stage :
                            !strcmp(state, "speaking") ? "正在回复" :
                            !strcmp(state, "alert") ? "请注意" :
                            !strcmp(state, "done") ? "处理完成" : "码卡龙";
        draw_robot_face_at(state, 10, 0, 92, bg);
        draw_text24_centered(226, label, color(255, 255, 255), bg, 8);
        draw_text24_centered(274,
                             !strcmp(state, "thinking") ? "双击激活键可取消" : "请稍候",
                             color(145, 220, 235), bg, 10);
    }
    #endif
    finish_screen_frame(composed);
    s_thinking_surface_visible = !ambient && !strcmp(state, "thinking");
}

static void ensure_thinking_mouth_task(void) {
    if (!s_background_tasks_lock ||
        xSemaphoreTake(s_background_tasks_lock, pdMS_TO_TICKS(100)) != pdTRUE) return;
    if (s_thinking_mouth_task) {
        xSemaphoreGive(s_background_tasks_lock);
        return;
    }
    s_thinking_mouth_stopped = xSemaphoreCreateBinary();
    if (!s_thinking_mouth_stopped) {
        xSemaphoreGive(s_background_tasks_lock);
        ESP_LOGW(TAG, "thinking mouth animation disabled: cannot create completion semaphore");
        return;
    }
    TaskHandle_t task = NULL;
    if (xTaskCreate(thinking_mouth_task, "bread_thinking_mouth", 3072,
                    NULL, 2, &task) == pdPASS) {
        s_thinking_mouth_task = task;
    } else {
        vSemaphoreDelete(s_thinking_mouth_stopped);
        s_thinking_mouth_stopped = NULL;
        ESP_LOGW(TAG, "thinking mouth animation disabled: cannot create task");
    }
    xSemaphoreGive(s_background_tasks_lock);
}

static bool show_remote_pet_animation_frame(void) {
    if (!s_front_frame_valid || !begin_composed_frame()) return false;

    /* Preserve the already rendered clock, calendar and text rows, but rebuild
     * the complete pet rectangle from its flat background on every tick. A
     * back buffer can be more than one pose old after another full-screen draw;
     * copying it and only painting the new silhouette leaves those old opaque
     * pixels behind. Clearing before compositing makes the result independent
     * of buffer history and lets the changed-row presenter erase departed
     * claws/ears on the panel. */
    memcpy(s_render_target, s_framebuffers[s_front_frame], LCD_FRAME_BYTES);
    uint16_t bg = state_color(s_state);
    fill_rect_solid((LCD_WIDTH - AMBIENT_PET_MAX_WIDTH) / 2,
                    AMBIENT_PET_TOP,
                    AMBIENT_PET_MAX_WIDTH,
                    AMBIENT_PET_MAX_HEIGHT,
                    bg);
    if (!draw_remote_pet_frame(bg)) {
        s_render_target = NULL;
        return false;
    }
    finish_screen_frame(true);
    return true;
}

static void show_status(const char *title, const char *line) {
    s_thinking_surface_visible = false;
    uint16_t bg = state_color(s_state);
    bool composed = begin_screen_frame();
    fill_screen(bg);
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    if (!draw_fangtang_sugar_at(60, 0, 64, bg)) {
        draw_fangtang_cube_at(s_state, 70, 2, 72, bg);
    }
    draw_text24_centered(112, title && title[0] ? title : "方糖",
                         color(248, 252, 255), bg, 9);
    draw_text24_centered(154, line && line[0] ? line : "设备就绪",
                         color(121, 210, 224), bg, 9);
    draw_text24_centered(208, "请使用激活键", color(157, 184, 205), bg, 8);
#else
    // Message/ready surfaces use the same visual identity as the reusable pet
    // states. Keeping the face here avoids the old bare "MACLAW / READY" page
    // while still leaving two calm, high-contrast rows for status copy.
    draw_robot_face_at(s_state, 54, 4, 55, bg);
    draw_text24_centered(190, title && title[0] ? title : "码卡龙",
                         color(248, 252, 255), bg, 9);
    draw_text24_centered(236, line && line[0] ? line : "设备已就绪",
                         color(121, 210, 224), bg, 10);
    draw_text24_centered(280, "请使用激活键", color(157, 184, 205), bg, 8);
    #endif
    finish_screen_frame(composed);
}

static void lcd_startup_screen(void) {
    if (!s_panel) return;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    // Keep a dedicated product splash visible throughout handshake and Welcome
    // playback. The ready path replaces it with the clock/weekday standby page
    // only after Welcome has finished.
    const uint16_t bg = state_color("idle");
    bool composed = begin_screen_frame();
    fill_screen(bg);
    if (!draw_fangtang_sugar_at(26, 8, 100, bg)) {
        draw_fangtang_cube_at("startup", 25, 3, 136, bg);
    }
    draw_text24_centered(207, "MaClaw Mate", color(244, 249, 253), bg, 11);
    finish_screen_frame(composed);
    return;
#else
    const size_t expected_bytes = LCD_FRAME_BYTES;
    const size_t asset_bytes = (size_t)(_binary_bread_compact_splash_rgb565_end -
                                        _binary_bread_compact_splash_rgb565_start);
    if (asset_bytes != expected_bytes) {
        ESP_LOGE(TAG, "invalid startup artwork: %u bytes (expected %u)",
                 (unsigned)asset_bytes, (unsigned)expected_bytes);
        fill_screen(state_color("idle"));
        return;
    }

    // Present directly in DMA-sized bands. Avoid copying this immutable
    // 150 KiB full-screen asset through PSRAM/double buffering at boot.
    const uint16_t *pixels = (const uint16_t *)_binary_bread_compact_splash_rgb565_start;
    for (int y = 0; y < LCD_HEIGHT; y += LCD_STRIPE_ROWS) {
        int y2 = y + LCD_STRIPE_ROWS < LCD_HEIGHT ? y + LCD_STRIPE_ROWS : LCD_HEIGHT;
        ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(
            0, y, LCD_WIDTH, y2, pixels + (size_t)y * LCD_WIDTH));
    }
#endif
}

static void remote_pet_animation_task(void *arg) {
    (void)arg;
    uint64_t rendered_tick = UINT64_MAX;
    while (true) {
        /* Schedule from the completed presentation. If TLS or SPI makes one
         * frame late, vTaskDelayUntil would run catch-up frames back-to-back;
         * that uneven burst cadence looks like both a jump and panel ghosting. */
        uint32_t delay_ms = REMOTE_PET_RENDER_FRAME_MS;
#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
        // Bread needs this task even without an animated pack so it can enforce
        // the idle display timeout. Avoid waking 12.5 times a second when it has
        // no animation work; a later pet install automatically restores 80 ms.
        if (s_remote_pet_frame_count < 2) delay_ms = 500;
#endif
        if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(delay_ms)) != 0) break;
        xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        if (s_response_active && !s_response_image_active && s_response_next_page_us > 0 &&
            esp_timer_get_time() >= s_response_next_page_us) {
            unsigned pages = response_page_count();
            if (s_response_page + 1 < pages) {
                ++s_response_page;
                s_response_next_page_us = esp_timer_get_time() + RESPONSE_AUTO_PAGE_INTERVAL_US;
                draw_response_page();
            } else {
                s_response_next_page_us = 0;
            }
        }
        // Reuse this always-resident timer on the single-key board, but preserve
        // the Bread cadence instead of advancing on every 80 ms pet tick.
        if (s_thinking_surface_visible && !s_recording_active &&
            !s_response_active && s_foreground_surface &&
            !strcmp(s_state, "thinking") &&
            esp_timer_get_time() >= s_fangtang_thinking_next_frame_us) {
            s_thinking_mouth_frame = (s_thinking_mouth_frame + 1) % 3;
            s_fangtang_thinking_next_frame_us = esp_timer_get_time() +
                (int64_t)THINKING_MOUTH_FRAME_MS * 1000;
            draw_fangtang_thinking_frame();
        }
#endif
        bool ambient = !strcmp(s_state, "idle") || !strcmp(s_state, "quiet");
        if (s_remote_pet_frame_count > 1 && s_remote_pet_frame_ms && ambient &&
            !s_display_sleeping && !s_foreground_surface && !s_recording_active &&
            !s_alarm_visual_active) {
            uint64_t current_tick = ((uint64_t)esp_timer_get_time() / 1000u) /
                                    REMOTE_PET_RENDER_FRAME_MS;
            if (current_tick != rendered_tick) {
                uint64_t loop_ms = (uint64_t)s_remote_pet_frame_ms *
                                   s_remote_pet_frame_count;
                s_remote_pet_animation_elapsed_ms += REMOTE_PET_RENDER_FRAME_MS;
                if (loop_ms) s_remote_pet_animation_elapsed_ms %= loop_ms;
                if (!show_remote_pet_animation_frame()) show_state_screen(s_state);
                rendered_tick = current_tick;
            }
        } else {
            rendered_tick = UINT64_MAX;
        }
        xSemaphoreGiveRecursive(s_lcd_mutex);
    }
    if (s_remote_pet_animation_stopped) xSemaphoreGive(s_remote_pet_animation_stopped);
    vTaskDelete(NULL);
}

static void ensure_remote_pet_animation_task(void) {
    if (!s_background_tasks_lock ||
        xSemaphoreTake(s_background_tasks_lock, pdMS_TO_TICKS(100)) != pdTRUE) return;
    if (s_remote_pet_animation_task) {
        xSemaphoreGive(s_background_tasks_lock);
        return;
    }
    s_remote_pet_animation_stopped = xSemaphoreCreateBinary();
    if (!s_remote_pet_animation_stopped) {
        xSemaphoreGive(s_background_tasks_lock);
        ESP_LOGW(TAG, "remote pet animation disabled: cannot create completion semaphore");
        return;
    }
    TaskHandle_t task = NULL;
    if (xTaskCreate(remote_pet_animation_task, "bread_pet_animation", 3072,
                    NULL, 2, &task) == pdPASS) {
        s_remote_pet_animation_task = task;
    } else {
        vSemaphoreDelete(s_remote_pet_animation_stopped);
        s_remote_pet_animation_stopped = NULL;
        ESP_LOGW(TAG, "remote pet animation disabled: cannot create task");
    }
    xSemaphoreGive(s_background_tasks_lock);
}

static uint16_t state_color(const char *state) {
    if (!strcmp(state, "listening")) return color(22, 120, 255);
    if (!strcmp(state, "thinking")) return color(140, 75, 235);
    if (!strcmp(state, "speaking")) return color(16, 185, 115);
    if (!strcmp(state, "alert")) return color(225, 55, 55);
    if (!strcmp(state, "done")) return color(30, 160, 100);
    return color(18, 24, 38);
}

static esp_err_t audio_init(void) {
    if (s_audio_ready) return ESP_OK;
    i2s_chan_config_t cfg = I2S_CHANNEL_DEFAULT_CONFIG(I2S_NUM_0, I2S_ROLE_MASTER);
    cfg.dma_desc_num = 8;
    cfg.dma_frame_num = 256;
    ESP_RETURN_ON_ERROR(i2s_new_channel(&cfg, NULL, &s_rx), TAG, "mic channel");
    i2s_std_config_t mic = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(AUDIO_RATE),
        .slot_cfg = I2S_STD_MSB_SLOT_DEFAULT_CONFIG(I2S_DATA_BIT_WIDTH_32BIT, I2S_SLOT_MODE_MONO),
        .gpio_cfg = {.mclk = I2S_GPIO_UNUSED, .bclk = MIC_BCLK, .ws = MIC_WS,
                     .dout = I2S_GPIO_UNUSED, .din = MIC_DIN, .invert_flags = {0}},
    };
    mic.slot_cfg.slot_mask = I2S_STD_SLOT_LEFT;
    ESP_RETURN_ON_ERROR(i2s_channel_init_std_mode(s_rx, &mic), TAG, "mic mode");
    ESP_RETURN_ON_ERROR(i2s_channel_enable(s_rx), TAG, "mic enable");

    cfg.id = I2S_NUM_1;
    /* Once a descriptor has been transmitted it must become silence. Without
     * this, an underrun can replay the final PCM block until TX is stopped. */
    cfg.auto_clear_after_cb = true;
    ESP_RETURN_ON_ERROR(i2s_new_channel(&cfg, &s_tx, NULL), TAG, "speaker channel");
    i2s_std_config_t speaker = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(AUDIO_RATE),
        .slot_cfg = I2S_STD_PHILIPS_SLOT_DEFAULT_CONFIG(I2S_DATA_BIT_WIDTH_16BIT, I2S_SLOT_MODE_STEREO),
        .gpio_cfg = {.mclk = I2S_GPIO_UNUSED, .bclk = SPK_BCLK, .ws = SPK_WS,
                     .dout = SPK_DOUT, .din = I2S_GPIO_UNUSED, .invert_flags = {0}},
    };
    ESP_RETURN_ON_ERROR(i2s_channel_init_std_mode(s_tx, &speaker), TAG, "speaker mode");
    /* Keep TX stopped while it is idle. Leaving the channel enabled after a
     * short chime lets some direct-I2S amplifiers continue reproducing the
     * final DMA descriptor, which sounds like a tone that never ends. Each
     * playback owns an explicit enable/write/disable cycle below. */
    s_audio_ready = true;
    ESP_LOGI(TAG, "Bread Compact direct-I2S audio ready");
    return ESP_OK;
}

#if CONFIG_MACLAW_BOARD_FANGTANG_4G && \
    !defined(MACLAW_FANGTANG_EXTERNAL_POWER_TELEMETRY)
static unsigned fangtang_battery_percent_from_adc(int adc) {
    static const struct {
        int adc;
        unsigned percent;
    } levels[] = {
        {1970, 0}, {2062, 20}, {2154, 40},
        {2246, 60}, {2338, 80}, {2430, 100},
    };
    if (adc <= levels[0].adc) return 0;
    if (adc >= levels[5].adc) return 100;
    for (size_t i = 0; i + 1 < sizeof(levels) / sizeof(levels[0]); ++i) {
        if (adc < levels[i + 1].adc) {
            int span = levels[i + 1].adc - levels[i].adc;
            int offset = adc - levels[i].adc;
            return levels[i].percent +
                   (unsigned)(offset * (int)(levels[i + 1].percent - levels[i].percent) /
                              span);
        }
    }
    return 100;
}

static void fangtang_power_task(void *arg) {
    (void)arg;
    int samples[3] = {0};
    unsigned sample_count = 0;
    unsigned sample_next = 0;
    unsigned ticks = 0;
    while (true) {
        bool charging = gpio_get_level(FANGTANG_CHARGE_STATUS_GPIO) != 0;
        bool sample_due = sample_count < 3 || (++ticks % 60) == 0;
        if (sample_due && s_battery_adc) {
            int raw = 0;
            if (adc_oneshot_read(s_battery_adc,
                                 (adc_channel_t)CONFIG_MACLAW_FANGTANG_BATTERY_ADC_CHANNEL,
                                 &raw) == ESP_OK) {
                samples[sample_next] = raw;
                sample_next = (sample_next + 1) % 3;
                if (sample_count < 3) ++sample_count;
                int total = 0;
                for (unsigned i = 0; i < sample_count; ++i) total += samples[i];
                int average = total / (int)sample_count;
                unsigned level = fangtang_battery_percent_from_adc(average);
                taskENTER_CRITICAL(&s_power_status_lock);
                s_battery_level = level;
                s_battery_level_valid = true;
                s_battery_charging = charging;
                taskEXIT_CRITICAL(&s_power_status_lock);
                ESP_LOGI(TAG, "Fangtang power: adc=%d average=%d battery=%u%% charging=%s",
                         raw, average, level, charging ? "yes" : "no");
            }
        } else {
            taskENTER_CRITICAL(&s_power_status_lock);
            s_battery_charging = charging;
            taskEXIT_CRITICAL(&s_power_status_lock);
        }
        vTaskDelay(pdMS_TO_TICKS(1000));
    }
}

static esp_err_t fangtang_power_init(void) {
    gpio_config_t charge = {
        .pin_bit_mask = 1ULL << FANGTANG_CHARGE_STATUS_GPIO,
        .mode = GPIO_MODE_INPUT,
    };
    ESP_RETURN_ON_ERROR(gpio_config(&charge), TAG, "charge status GPIO");
    adc_oneshot_unit_init_cfg_t adc_cfg = {
        .unit_id = CONFIG_MACLAW_FANGTANG_BATTERY_ADC_UNIT == 1
                       ? ADC_UNIT_1 : ADC_UNIT_2,
        .ulp_mode = ADC_ULP_MODE_DISABLE,
    };
    ESP_RETURN_ON_ERROR(adc_oneshot_new_unit(&adc_cfg, &s_battery_adc),
                        TAG, "battery ADC unit");
    adc_oneshot_chan_cfg_t channel_cfg = {
        .atten = ADC_ATTEN_DB_12,
        .bitwidth = ADC_BITWIDTH_12,
    };
    ESP_RETURN_ON_ERROR(adc_oneshot_config_channel(
                            s_battery_adc,
                            (adc_channel_t)CONFIG_MACLAW_FANGTANG_BATTERY_ADC_CHANNEL,
                            &channel_cfg),
                        TAG, "battery ADC channel");
    return xTaskCreate(fangtang_power_task, "fangtang_power", 3072,
                       NULL, 1, NULL) == pdPASS
               ? ESP_OK : ESP_ERR_NO_MEM;
}
#endif

static esp_err_t read_mono(int16_t *mono, size_t capacity, size_t *read, uint16_t *level) {
    if (!mono || capacity == 0) return ESP_ERR_INVALID_ARG;
    int32_t raw[512];
    size_t bytes = 0;
    ESP_RETURN_ON_ERROR(i2s_channel_read(s_rx, raw, sizeof(raw), &bytes, pdMS_TO_TICKS(1000)), TAG, "mic read");
    size_t count = bytes / sizeof(raw[0]);
    if (count > capacity) count = capacity;
    int32_t peak = 0;
    for (size_t i = 0; i < count; ++i) {
        int16_t sample = (int16_t)(raw[i] >> 14);
        mono[i] = sample;
        int32_t magnitude = sample < 0 ? -(int32_t)sample : sample;
        if (magnitude > peak) peak = magnitude;
    }
    if (read) *read = count;
    if (level) *level = peak > 12000 ? 1000 : (uint16_t)(peak * 1000 / 12000);
    return ESP_OK;
}

static uint16_t command_capture_mean_level(const int16_t *samples, size_t count);
#if CONFIG_MACLAW_BOARD_FANGTANG_4G && \
    defined(MACLAW_FANGTANG_EXTERNAL_BOOT_SELECTOR)
/* Implemented by the selected Fangtang profile translation unit.  It runs
 * before this legacy scanner exists, so GPIO0 never has concurrent gesture
 * owners during the startup transport-selection window. */
void fangtang_board_run_boot_network_selector(void);
#endif

static void button_task(void *arg) {
    (void)arg;
    bool previous = gpio_get_level(BUTTON_ACTIVATE) != 0;
#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
    bool volume_up_raw = gpio_get_level(BUTTON_VOLUME_UP) != 0;
    bool volume_down_raw = gpio_get_level(BUTTON_VOLUME_DOWN) != 0;
    bool volume_up_stable = volume_up_raw;
    bool volume_down_stable = volume_down_raw;
    int64_t volume_up_changed_at = 0;
    int64_t volume_down_changed_at = 0;
#endif
    int64_t pressed_at = 0;
    int64_t short_pending_at = 0;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    // Gesture ownership is decided when the first click is released, not when
    // the 500 ms single/double decision eventually expires. A first click near
    // the end of the 1.8 s startup selector can otherwise mature after the
    // window closes and leak into normal operation as voice (SHORT), or combine
    // with a late second click and start a meeting (DOUBLE).
    bool short_pending_boot_owned = false;
#endif
    bool long_sent = false;
    while (true) {
        int64_t now = esp_timer_get_time();
        bool released = gpio_get_level(BUTTON_ACTIVATE) != 0;
        if (previous && !released) {
            pressed_at = now;
            long_sent = false;
            if (s_button_cb) {
                s_button_cb(BOARD_INPUT_PRESSED, BOARD_INPUT_SOURCE_ACTIVATE_KEY,
                            s_button_arg);
            }
        }
        /* Fire while the key is still held. Waiting for release made a valid
         * long press look unresponsive and also allowed contact bounce on the
         * release edge to turn it into an ordinary short press. */
        if (!released && pressed_at && !long_sent && now - pressed_at >= 2500000) {
            long_sent = true;
            short_pending_at = 0;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
            short_pending_boot_owned = false;
#endif
            ESP_LOGI(TAG, "activate long hold detected");
            if (s_button_cb) {
                s_button_cb(BOARD_BUTTON_LONG, BOARD_INPUT_SOURCE_ACTIVATE_KEY,
                            s_button_arg);
            }
        }
        if (!previous && released && pressed_at) {
            int64_t duration = now - pressed_at;
            if (long_sent || duration >= 2500000) {
                short_pending_at = 0;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                short_pending_boot_owned = false;
#endif
            } else if (short_pending_at && now - short_pending_at <= 500000) {
                short_pending_at = 0;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                if (s_boot_network_window_active) {
                    short_pending_boot_owned = false;
                    s_boot_network_toggle_requested = true;
                    ESP_LOGI(TAG, "GPIO0 startup double click: network toggle requested");
                } else if (short_pending_boot_owned) {
                    short_pending_boot_owned = false;
                    ESP_LOGI(TAG, "GPIO0 startup gesture completed after selector close; consumed");
                } else
#endif
                if (s_button_cb) {
                    s_button_cb(BOARD_BUTTON_DOUBLE, BOARD_INPUT_SOURCE_ACTIVATE_KEY,
                                s_button_arg);
                }
            } else {
                short_pending_at = now;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                short_pending_boot_owned = s_boot_network_window_active;
#endif
            }
            pressed_at = 0;
        }
        previous = released;
        if (short_pending_at && now - short_pending_at > 500000) {
            short_pending_at = 0;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
            bool consume_startup_click = s_boot_network_window_active ||
                                         short_pending_boot_owned;
            short_pending_boot_owned = false;
            if (consume_startup_click) {
                // Consume the whole gesture even if its delayed single-click
                // decision crosses the selector deadline.
            } else
#endif
            if (s_button_cb) {
                s_button_cb(BOARD_BUTTON_SHORT, BOARD_INPUT_SOURCE_ACTIVATE_KEY,
                            s_button_arg);
            }
        }

 #if !CONFIG_MACLAW_BOARD_FANGTANG_4G
        bool volume_up_released = gpio_get_level(BUTTON_VOLUME_UP) != 0;
        if (volume_up_released != volume_up_raw) {
            volume_up_raw = volume_up_released;
            volume_up_changed_at = now;
        }
        if (volume_up_stable != volume_up_raw && volume_up_changed_at &&
            now - volume_up_changed_at >= 30000) {
            volume_up_stable = volume_up_raw;
            ESP_LOGI(TAG, "volume up key GPIO%d level=%d",
                     BUTTON_VOLUME_UP, volume_up_stable ? 1 : 0);
            if (volume_up_stable) {
                ESP_LOGI(TAG, "volume up key released (GPIO%d)", BUTTON_VOLUME_UP);
                if (s_button_cb) {
                    s_button_cb(BOARD_INPUT_VOLUME_UP, BOARD_INPUT_SOURCE_OTHER_KEY,
                                s_button_arg);
                }
            }
        }

        bool volume_down_released = gpio_get_level(BUTTON_VOLUME_DOWN) != 0;
        if (volume_down_released != volume_down_raw) {
            volume_down_raw = volume_down_released;
            volume_down_changed_at = now;
        }
        if (volume_down_stable != volume_down_raw && volume_down_changed_at &&
            now - volume_down_changed_at >= 30000) {
            volume_down_stable = volume_down_raw;
            ESP_LOGI(TAG, "volume down key GPIO%d level=%d",
                     BUTTON_VOLUME_DOWN, volume_down_stable ? 1 : 0);
            if (volume_down_stable) {
                ESP_LOGI(TAG, "volume down key released (GPIO%d)", BUTTON_VOLUME_DOWN);
                if (s_button_cb) {
                    s_button_cb(BOARD_INPUT_VOLUME_DOWN, BOARD_INPUT_SOURCE_OTHER_KEY,
                                s_button_arg);
                }
            }
        }
#endif

        /* A direct task notification is the scanner's stop token.  Do not use
         * a cross-core volatile flag for lifecycle synchronization. */
        if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(20)) != 0) break;
    }

    s_button_cb = NULL;
    s_button_arg = NULL;
    if (s_button_task_stopped) xSemaphoreGive(s_button_task_stopped);
    vTaskDelete(NULL);
}

esp_err_t board_port_init(board_port_button_cb_t cb, void *arg) {
    s_button_cb = cb;
    s_button_arg = arg;
    // Hub reachability is session state. Never restore a previous boot's
    // ONLINE bit: the current handshake/poll must prove it again.
    s_gateway_ready = false;
    s_background_tasks_lock = xSemaphoreCreateMutex();
    if (!s_background_tasks_lock) return ESP_ERR_NO_MEM;
    s_audio_mutex = xSemaphoreCreateMutex();
    if (!s_audio_mutex) return ESP_ERR_NO_MEM;
    s_lcd_mutex = xSemaphoreCreateRecursiveMutex();
    if (!s_lcd_mutex) return ESP_ERR_NO_MEM;
    s_lcd_transfer_done = xSemaphoreCreateBinary();
    if (!s_lcd_transfer_done) return ESP_ERR_NO_MEM;
#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
    ledc_timer_config_t backlight_timer = {
        .speed_mode = LEDC_LOW_SPEED_MODE,
        .duty_resolution = BREAD_BACKLIGHT_LEDC_RESOLUTION,
        .timer_num = BREAD_BACKLIGHT_LEDC_TIMER,
        .freq_hz = 5000,
        .clk_cfg = LEDC_AUTO_CLK,
    };
    ledc_channel_config_t backlight_channel = {
        .gpio_num = LCD_BACKLIGHT,
        .speed_mode = LEDC_LOW_SPEED_MODE,
        .channel = BREAD_BACKLIGHT_LEDC_CHANNEL,
        .intr_type = LEDC_INTR_DISABLE,
        .timer_sel = BREAD_BACKLIGHT_LEDC_TIMER,
        .duty = 0,
        .hpoint = 0,
    };
    ESP_ERROR_CHECK(ledc_timer_config(&backlight_timer));
    ESP_ERROR_CHECK(ledc_channel_config(&backlight_channel));
#endif
    #if CONFIG_MACLAW_BOARD_FANGTANG_4G
    ESP_ERROR_CHECK(fangtang_display_init_hardware(
        LCD_HOST, lcd_color_transfer_done, s_lcd_transfer_done, &s_panel, &s_panel_io));
    ESP_LOGI(TAG, "NV3023 viewport ready: 240x240, GRAM Y=%d..%d",
             LCD_Y_OFFSET, LCD_Y_OFFSET + LCD_HEIGHT - 1);
    #else
    spi_bus_config_t bus = {.mosi_io_num = LCD_MOSI, .miso_io_num = GPIO_NUM_NC,
                            .sclk_io_num = LCD_CLK, .quadwp_io_num = GPIO_NUM_NC,
                            .quadhd_io_num = GPIO_NUM_NC,
                            .max_transfer_sz = LCD_WIDTH * 16 * sizeof(uint16_t)};
    #endif
    #if !CONFIG_MACLAW_BOARD_FANGTANG_4G
    ESP_ERROR_CHECK(spi_bus_initialize(LCD_HOST, &bus, SPI_DMA_CH_AUTO));
    esp_lcd_panel_io_handle_t io = NULL;
    esp_lcd_panel_io_spi_config_t io_cfg = {.cs_gpio_num = LCD_CS, .dc_gpio_num = LCD_DC,
        .spi_mode = 3, .pclk_hz = 20 * 1000 * 1000,
        .trans_queue_depth = 10, .lcd_cmd_bits = 8, .lcd_param_bits = 8,
        .on_color_trans_done = lcd_color_transfer_done, .user_ctx = s_lcd_transfer_done};
    ESP_ERROR_CHECK(esp_lcd_new_panel_io_spi(LCD_HOST, &io_cfg, &io));
    esp_lcd_panel_dev_config_t panel_cfg = {.reset_gpio_num = LCD_RST,
        .rgb_ele_order = LCD_RGB_ELEMENT_ORDER_RGB, .bits_per_pixel = 16};
    ESP_ERROR_CHECK(esp_lcd_new_panel_st7789(io, &panel_cfg, &s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_reset(s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_init(s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_invert_color(s_panel, true));
    ESP_ERROR_CHECK(esp_lcd_panel_disp_on_off(s_panel, true));
    ESP_ERROR_CHECK(ledc_set_duty(LEDC_LOW_SPEED_MODE,
                                  BREAD_BACKLIGHT_LEDC_CHANNEL,
                                  BREAD_BACKLIGHT_DUTY));
    ESP_ERROR_CHECK(ledc_update_duty(LEDC_LOW_SPEED_MODE,
                                     BREAD_BACKLIGHT_LEDC_CHANNEL));
#endif
    for (size_t i = 0; i < 2; ++i) {
        s_framebuffers[i] = heap_caps_malloc(LCD_FRAME_BYTES,
                                             MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!s_framebuffers[i]) {
            ESP_LOGW(TAG, "cannot allocate LCD framebuffer %u", (unsigned)i);
            break;
        }
        memset(s_framebuffers[i], 0, LCD_FRAME_BYTES);
    }
    s_present_staging = heap_caps_malloc(
        (size_t)LCD_WIDTH * LCD_STRIPE_ROWS * sizeof(uint16_t),
        MALLOC_CAP_DMA | MALLOC_CAP_INTERNAL);
    if (!s_framebuffers[0] || !s_framebuffers[1] || !s_present_staging
    ) {
        for (size_t i = 0; i < 2; ++i) {
            if (s_framebuffers[i]) heap_caps_free(s_framebuffers[i]);
            s_framebuffers[i] = NULL;
        }
        ESP_LOGW(TAG, "LCD double buffering disabled: insufficient memory%s",
                 s_present_staging ? "; DMA stripe fallback retained" : "");
    } else {
        ESP_LOGI(TAG, "LCD double buffering ready: %u bytes PSRAM + %u bytes DMA",
                 (unsigned)(LCD_FRAME_BYTES * 2),
                 (unsigned)(LCD_WIDTH * LCD_STRIPE_ROWS * sizeof(uint16_t)));
    }
    lcd_startup_screen();
    // Do not clear the board-specific artwork on a fixed timer. Application startup
    // owns the transition now and keeps this screen visible through handshake
    // and Welcome playback; the ready surface replaces it explicitly.
    gpio_config_t button = {.pin_bit_mask = (1ULL << BUTTON_ACTIVATE)
#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
                                             | (1ULL << BUTTON_VOLUME_UP)
                                             | (1ULL << BUTTON_VOLUME_DOWN)
#endif
                                            ,
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE};
    ESP_ERROR_CHECK(gpio_config(&button));
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    ESP_LOGI(TAG, "button GPIO idle level: activate=%d", gpio_get_level(BUTTON_ACTIVATE));
#else
    ESP_LOGI(TAG, "button GPIO idle levels: activate=%d volume_up=%d volume_down=%d",
             gpio_get_level(BUTTON_ACTIVATE), gpio_get_level(BUTTON_VOLUME_UP),
             gpio_get_level(BUTTON_VOLUME_DOWN));
#endif
    ESP_RETURN_ON_ERROR(audio_init(), TAG, "audio init");
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
#if defined(MACLAW_FANGTANG_EXTERNAL_POWER_TELEMETRY)
    ESP_RETURN_ON_ERROR(fangtang_board_power_init(), TAG, "power monitor init");
#else
    ESP_RETURN_ON_ERROR(fangtang_power_init(), TAG, "power monitor init");
#endif
#endif
#if CONFIG_MACLAW_BOARD_FANGTANG_4G && \
    defined(MACLAW_FANGTANG_EXTERNAL_BOOT_SELECTOR)
    fangtang_board_run_boot_network_selector();
#endif
    s_button_task_stopped = xSemaphoreCreateBinary();
    if (!s_button_task_stopped) return ESP_ERR_NO_MEM;
    if (xTaskCreate(button_task, "bread_button", 3072, NULL, 4, &s_button_task) != pdPASS) {
        vSemaphoreDelete(s_button_task_stopped);
        s_button_task_stopped = NULL;
        return ESP_ERR_NO_MEM;
    }
    // Besides pet animation this task owns the compact boards' 30-minute idle
    // display timeout. Fangtang also uses it for timed response pagination.
    ensure_remote_pet_animation_task();
    // The mouth animation is decorative state feedback. Give its floating-point
    // renderer enough stack headroom, but never make the essential buttons,
    // display, microphone, or speaker unavailable if this task cannot start.
    /* The compact LCD repaints thinking dots from state transitions. Avoid an
     * always-resident decorative task during Wi-Fi's memory-intensive startup. */
    /* Remote assets are optional and their animation task is started only when
     * a frame pack is actually installed. Keeping an always-idle task alive at
     * boot wastes an internal task stack while Wi-Fi allocates scan buffers. */
    return ESP_OK;
}

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G || \
    !defined(MACLAW_FANGTANG_EXTERNAL_BOOT_SELECTOR)
bool board_port_wait_for_boot_network_toggle(uint32_t window_ms) {
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    s_boot_network_toggle_requested = false;
    s_boot_network_window_active = true;
    const int64_t deadline = esp_timer_get_time() + (int64_t)window_ms * 1000;
    ESP_LOGI(TAG, "GPIO0 startup network selector active for %u ms",
             (unsigned)window_ms);
    while (esp_timer_get_time() < deadline && !s_boot_network_toggle_requested) {
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    s_boot_network_window_active = false;
    bool requested = s_boot_network_toggle_requested;
    s_boot_network_toggle_requested = false;
    ESP_LOGI(TAG, "GPIO0 startup network selector closed: %s",
             requested ? "toggle" : "unchanged");
    return requested;
#else
    (void)window_ms;
    return false;
#endif
}
#endif

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G || \
    !defined(MACLAW_FANGTANG_EXTERNAL_CONNECTIVITY_CONFIGURATION)
bool board_port_load_transport_selection(bool *out_cellular) {
    if (out_cellular) *out_cellular = false;
    return false;
}

bool board_port_apply_startup_transport_toggle(uint32_t window_ms,
                                               bool current_cellular,
                                               bool *out_cellular) {
    (void)window_ms;
    if (out_cellular) *out_cellular = current_cellular;
    return false;
}

void board_port_adapt_gateway_url(char *gateway_url, size_t capacity,
                                  bool cellular_active) {
    (void)gateway_url;
    (void)capacity;
    (void)cellular_active;
}
#endif

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G || \
    !defined(MACLAW_FANGTANG_EXTERNAL_CELLULAR_PREPARATION)
esp_err_t board_port_prepare_cellular_transport(void) {
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    if (CONFIG_MACLAW_FANGTANG_MODEM_UART_TX_GPIO < 0 ||
        CONFIG_MACLAW_FANGTANG_MODEM_UART_RX_GPIO < 0) {
        return ESP_ERR_INVALID_ARG;
    }
    if (CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO >= 0) {
        gpio_config_t guard = {
            .pin_bit_mask = 1ULL << CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO,
            .mode = GPIO_MODE_OUTPUT,
            .pull_down_en = GPIO_PULLDOWN_ENABLE,
        };
        esp_err_t err = gpio_config(&guard);
        if (err != ESP_OK) return err;
        err = gpio_set_level(CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO,
                             CONFIG_MACLAW_FANGTANG_MODEM_GUARD_LEVEL);
        if (err != ESP_OK) return err;
    }
    if (CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO >= 0) {
        gpio_config_t power = {
            .pin_bit_mask = 1ULL << CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO,
            .mode = GPIO_MODE_OUTPUT,
        };
        esp_err_t err = gpio_config(&power);
        if (err != ESP_OK) return err;
        err = gpio_set_level(CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO,
                             CONFIG_MACLAW_FANGTANG_MODEM_POWER_ACTIVE_LEVEL);
        if (err != ESP_OK) return err;
        vTaskDelay(pdMS_TO_TICKS(500));
    }
    return ESP_OK;
#else
    return ESP_ERR_NOT_SUPPORTED;
#endif
}
#endif

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G || \
    !defined(MACLAW_FANGTANG_EXTERNAL_CELLULAR_CANCELLATION)
bool board_port_cancel_cellular_foreground_request(void) {
    return false;
}
#endif

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G || \
    !defined(MACLAW_FANGTANG_EXTERNAL_CELLULAR_START) || \
    !defined(MACLAW_FANGTANG_EXTERNAL_CELLULAR_HTTP)
esp_err_t board_port_start_cellular_transport(uint32_t timeout_ms) {
    (void)timeout_ms;
    return ESP_ERR_NOT_SUPPORTED;
}

bool board_port_is_cellular_transport_ready(void) {
    return false;
}

esp_err_t board_port_cellular_http_request(
    const device_connectivity_http_request_t *request) {
    (void)request;
    return ESP_ERR_NOT_SUPPORTED;
}

esp_err_t board_port_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request) {
    (void)request;
    return ESP_ERR_NOT_SUPPORTED;
}
#endif

void board_port_show_startup_screen(void) {
    if (!s_panel || !s_lcd_mutex) return;
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    if (s_alarm_visual_active) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    s_response_active = false;
    s_recording_active = false;
    s_foreground_surface = true;
    lcd_startup_screen();
    xSemaphoreGiveRecursive(s_lcd_mutex);
}

esp_err_t board_port_adjust_output_volume(int delta_percent, unsigned *out_percent) {
    unsigned current = __atomic_load_n(&s_output_volume, __ATOMIC_RELAXED);
    int next = (int)current + delta_percent;
    if (next < 0) next = 0;
    if (next > 100) next = 100;
    esp_err_t err = board_port_set_output_volume((unsigned)next);
    if (err == ESP_OK && out_percent) *out_percent = (unsigned)next;
    return err;
}

esp_err_t board_port_set_output_volume(unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    __atomic_store_n(&s_output_volume, percent, __ATOMIC_RELAXED);
    ESP_LOGI(TAG, "direct-I2S output volume applied: %u%%", percent);
    return ESP_OK;
}

esp_err_t board_port_stop_input(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_button_task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == s_button_task) return ESP_ERR_INVALID_STATE;

    xTaskNotifyGive(s_button_task);
    if (!s_button_task_stopped ||
        xSemaphoreTake(s_button_task_stopped, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        ESP_LOGW(TAG, "timed out stopping board input scanner");
        return ESP_ERR_TIMEOUT;
    }
    vSemaphoreDelete(s_button_task_stopped);
    s_button_task_stopped = NULL;
    s_button_task = NULL;
    ESP_LOGI(TAG, "board input scanner stopped");
    return ESP_OK;
}

static esp_err_t stop_background_task(TaskHandle_t *task,
                                      SemaphoreHandle_t *stopped,
                                      TickType_t timeout) {
    if (!*task) return ESP_OK;
    xTaskNotifyGive(*task);
    if (!*stopped || xSemaphoreTake(*stopped, timeout) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    vSemaphoreDelete(*stopped);
    *stopped = NULL;
    *task = NULL;
    return ESP_OK;
}

esp_err_t board_port_stop_background_tasks(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_background_tasks_lock ||
        xSemaphoreTake(s_background_tasks_lock, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    if (xTaskGetCurrentTaskHandle() == s_remote_pet_animation_task ||
        xTaskGetCurrentTaskHandle() == s_thinking_mouth_task) {
        xSemaphoreGive(s_background_tasks_lock);
        return ESP_ERR_INVALID_STATE;
    }
    const TickType_t deadline = pdMS_TO_TICKS(timeout_ms);
    const TickType_t started = xTaskGetTickCount();
    esp_err_t err = stop_background_task(&s_thinking_mouth_task,
                                         &s_thinking_mouth_stopped, deadline);
    TickType_t elapsed = xTaskGetTickCount() - started;
    TickType_t remaining = elapsed >= deadline ? 0 : deadline - elapsed;
    if (err == ESP_OK && remaining > 0) {
        err = stop_background_task(&s_remote_pet_animation_task,
                                   &s_remote_pet_animation_stopped, remaining);
    } else if (err == ESP_OK) {
        err = ESP_ERR_TIMEOUT;
    }
 #if CONFIG_MACLAW_BOARD_FANGTANG_4G && \
    defined(MACLAW_FANGTANG_EXTERNAL_POWER_MONITOR_STOP)
    elapsed = xTaskGetTickCount() - started;
    remaining = elapsed >= deadline ? 0 : deadline - elapsed;
    if (err == ESP_OK && remaining > 0) {
        err = fangtang_board_stop_power_monitor((uint32_t)remaining * portTICK_PERIOD_MS);
    } else if (err == ESP_OK) {
        err = ESP_ERR_TIMEOUT;
    }
 #endif
    xSemaphoreGive(s_background_tasks_lock);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "timed out stopping board background task");
    } else {
        ESP_LOGI(TAG, "board background tasks stopped");
    }
    return err;
}

void board_port_set_pet_state(const char *state) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    const char *next_state = state ? state : "idle";
    bool ambient = !strcmp(next_state, "idle") || !strcmp(next_state, "quiet");
    // A completed answer remains the foreground owner until the application
    // explicitly dismisses it. Late idle/quiet state updates must not erase it.
    if (ambient && s_response_active && s_foreground_surface) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    strlcpy(s_state, next_state, sizeof(s_state));
    if (ambient) {
        enter_ambient_awake_locked();
    } else {
        s_idle_pet_sleep_expires_us = 0;
    }
    if (!strcmp(next_state, "thinking")) {
        s_thinking_mouth_frame = 0;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        s_fangtang_thinking_next_frame_us = esp_timer_get_time() +
            (int64_t)THINKING_MOUTH_FRAME_MS * 1000;
#else
        ensure_thinking_mouth_task();
#endif
    }
    if (!s_recording_active) {
        s_response_active = false;
        s_foreground_surface = !ambient;
        show_state_screen(s_state);
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_set_command_stage(const char *stage) {
    const char *next_stage = stage && stage[0] ? stage : "正在处理";
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    bool changed = strcmp(s_command_stage, next_stage) != 0;
    if (changed) strlcpy(s_command_stage, next_stage, sizeof(s_command_stage));
    // Redraw only on a real phase transition. Reasserting the long-running
    // remote stage is therefore free and never replaces the animated surface.
    if (changed && s_thinking_surface_visible && !s_recording_active &&
        !s_response_active && s_foreground_surface && !strcmp(s_state, "thinking")) {
        show_state_screen(s_state);
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_set_command_display_lock(bool locked) {
    if (s_lcd_mutex) xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_foreground_surface = locked;
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_set_command_cancel_enabled(bool enabled) {(void)enabled;}
void board_port_set_pet_profile(const char *skin, bool motion_enabled) {
    (void)motion_enabled;
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    bool changed = skin && skin[0] && strcmp(s_pet_skin, skin) != 0;
    if (changed) strlcpy(s_pet_skin, skin, sizeof(s_pet_skin));
    if (changed && !s_foreground_surface && !s_recording_active) {
        if (s_display_sleeping) {
            xSemaphoreGiveRecursive(s_lcd_mutex);
            return;
        }
        show_state_screen(s_state);
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
esp_err_t board_port_set_pet_asset(const uint8_t *const *frames, size_t frame_count,
                                   size_t width, size_t height, uint32_t frame_ms) {
    if (frame_count > REMOTE_PET_MAX_FRAMES) return ESP_ERR_INVALID_ARG;
    size_t bytes = 0;
    size_t target_width = 0, target_height = 0;
    if (frame_count) {
        if (!frames || width < 1 || height < 1 || width > 256 || height > 256) {
            return ESP_ERR_INVALID_ARG;
        }
        if (width > SIZE_MAX / height || width * height > SIZE_MAX / 3u) {
            return ESP_ERR_INVALID_SIZE;
        }
        if (!remote_pet_target_size(width, height, &target_width, &target_height)) {
            return ESP_ERR_INVALID_SIZE;
        }
        bytes = target_width * target_height * 3u;
    }
    uint8_t *copies[REMOTE_PET_MAX_FRAMES] = {0};
    for (size_t i = 0; i < frame_count; ++i) {
        if (!frames[i]) {
            for (size_t j = 0; j < i; ++j) free(copies[j]);
            return ESP_ERR_INVALID_ARG;
        }
        copies[i] = heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!copies[i]) copies[i] = malloc(bytes);
        if (!copies[i]) {
            for (size_t j = 0; j < i; ++j) free(copies[j]);
            return ESP_ERR_NO_MEM;
        }
        scale_remote_pet_frame(frames[i], width, height, copies[i],
                               target_width, target_height);
    }
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    for (size_t i = 0; i < REMOTE_PET_MAX_FRAMES; ++i) {
        free(s_remote_pet_frames[i]);
        s_remote_pet_frames[i] = copies[i];
    }
    s_remote_pet_frame_count = frame_count;
    s_remote_pet_width = frame_count ? target_width : 0;
    s_remote_pet_height = frame_count ? target_height : 0;
    s_remote_pet_frame_ms = frame_ms ? frame_ms : REMOTE_PET_DEFAULT_KEYFRAME_MS;
    s_remote_pet_animation_elapsed_ms = 0;
    ensure_remote_pet_animation_task();
    if (!s_display_sleeping && !s_foreground_surface && !s_recording_active) {
        show_state_screen(s_state);
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
    return ESP_OK;
}
void board_port_set_recording_mode(bool meeting) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_recording_mode = meeting;
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_set_recording_visual(bool active, bool paused, uint32_t elapsed) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    bool new_session = active && !s_recording_active;
    bool visual_changed = !active || new_session || paused != s_recording_paused ||
                          elapsed != s_recording_elapsed;
    s_recording_active = active;
    if (active) s_thinking_surface_visible = false;
    if (active) s_response_active = false;
    s_foreground_surface = active;
    s_recording_paused = active && paused;
    s_recording_elapsed = active ? elapsed : 0;
    if (!active) {
        memset(s_recording_levels, 0, sizeof(s_recording_levels));
        s_recording_smoothed_level = 0;
        if (!strcmp(s_state, "idle") || !strcmp(s_state, "quiet")) {
            enter_ambient_awake_locked();
        }
        show_state_screen(s_state);
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    if (new_session) {
        memset(s_recording_levels, 0, sizeof(s_recording_levels));
        s_recording_smoothed_level = 0;
    }
    if (!visual_changed) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    uint16_t bg = color(10,19,30);
    uint16_t accent = paused ? color(244,178,58) : color(241,76,85);
    uint16_t cyan = color(72,205,220);
    uint16_t muted = color(91,118,138);
    bool composed = begin_screen_frame();
    fill_screen(bg);
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    // Fangtang's panel is 240x240. Keep every recording affordance inside the
    // visible viewport instead of inheriting Bread Compact's 240x320 rows.
    fill_rect_solid(16, 8, 208, 4, accent);
    fill_rect_solid(16, 232, 208, 4, accent);
    fill_rect_solid(28, 23, 18, 18, accent);
    fill_rect_solid(33, 28, 8, 8, color(255,235,238));
    draw_text24_clipped(56, 20, paused ? "已暂停" : "正在听取",
                        color(245,250,255), bg, 7);
    draw_text24_centered(52, s_recording_mode ? "会议录音" : "语音指令",
                         paused ? color(244,178,58) : cyan, bg, 8);
#else
    fill_rect_solid(16, 16, 208, 4, accent);
    fill_rect_solid(16, 300, 208, 4, accent);
    fill_rect_solid(28, 43, 20, 20, accent);
    fill_rect_solid(34, 49, 8, 8, color(255,235,238));
    draw_text24_clipped(62, 42, paused ? "已暂停" : "正在听取",
                        color(245,250,255), bg, 7);
    draw_text24_centered(78, s_recording_mode ? "会议录音" : "语音指令",
                         paused ? color(244,178,58) : cyan, bg, 8);
#endif
    char timer[16];
    snprintf(timer, sizeof(timer), "%02lu:%02lu", (unsigned long)(elapsed / 60), (unsigned long)(elapsed % 60));
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    draw_ascii_centered(82, timer, color(255,255,255), bg);
    fill_rect_solid(20, 114, 200, 1, muted);
#else
    draw_ascii_centered(112, timer, color(255,255,255), bg);
    fill_rect_solid(20, 158, 200, 1, muted);
#endif
    for (int column = 0; column < 24; ++column) {
        uint16_t level = paused ? 0 : s_recording_levels[column];
        if (level > 1000) level = 1000;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        int half = 2 + (int)(level * 32u / 1000u);
        int center_y = 158;
#else
        int half = 2 + (int)(level * 42u / 1000u);
#endif
        int x = 22 + column * 8;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        fill_rect_solid(x, center_y - half, 5, half * 2 + 1,
                        paused ? muted : cyan);
#else
        fill_rect_solid(x, 205 - half, 5, half * 2 + 1, paused ? muted : cyan);
#endif
    }
    char level_label[20];
    snprintf(level_label, sizeof(level_label), "MIC %u%%",
             (unsigned)(s_recording_smoothed_level / 10u));
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    // Keep a full text-row gap above the 24 px instruction at y=211.
    // The former y=195 baseline overlapped visibly on the 240 px panel.
    draw_ascii_centered(184, level_label, paused ? muted : cyan, bg);
    draw_text24_centered(211, s_recording_mode ? "按激活键停止" : "说完后自动处理",
                         color(163,188,207), bg, 8);
#else
    draw_ascii_centered(226, level_label, paused ? muted : cyan, bg);
    draw_text24_centered(260, s_recording_mode ? "按激活键停止保存" : "说完后自动处理",
                         color(163,188,207), bg, 9);
#endif
    finish_screen_frame(composed);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_set_audio_level(uint16_t level, uint32_t elapsed) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    if (!s_recording_active) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    s_recording_smoothed_level = level > s_recording_smoothed_level
                                     ? (uint16_t)((s_recording_smoothed_level + level * 3u) / 4u)
                                     : (uint16_t)((s_recording_smoothed_level * 7u + level) / 8u);
    memmove(&s_recording_levels[0], &s_recording_levels[1],
            (24 - 1) * sizeof(s_recording_levels[0]));
    s_recording_levels[23] = s_recording_smoothed_level;
    uint16_t bg = color(10,19,30);
    uint16_t cyan = color(72,205,220);
    uint16_t muted = color(91,118,138);
    bool composed = begin_screen_frame();
    if (composed) {
        memcpy(s_render_target, s_framebuffers[s_front_frame], LCD_FRAME_BYTES);
    }
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    fill_rect_solid(18, 116, 204, 94, bg);
    fill_rect_solid(20, 158, 200, 1, muted);
#else
    fill_rect_solid(18, 160, 204, 90, bg);
    fill_rect_solid(20, 205, 200, 1, muted);
#endif
    for (int column = 0; column < 24; ++column) {
        uint16_t history_level = s_recording_paused ? 0 : s_recording_levels[column];
        if (history_level > 1000) history_level = 1000;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        int half = 2 + (int)(history_level * 32u / 1000u);
        int center_y = 158;
#else
        int half = 2 + (int)(history_level * 42u / 1000u);
#endif
        int x = 22 + column * 8;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        fill_rect_solid(x, center_y - half, 5, half * 2 + 1,
                        s_recording_paused ? muted : cyan);
#else
        fill_rect_solid(x, 205 - half, 5, half * 2 + 1,
                        s_recording_paused ? muted : cyan);
#endif
    }
    char level_label[20];
    snprintf(level_label, sizeof(level_label), "MIC %u%%",
             (unsigned)(s_recording_smoothed_level / 10u));
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    draw_ascii_centered(184, level_label, s_recording_paused ? muted : cyan, bg);
#else
    draw_ascii_centered(226, level_label, s_recording_paused ? muted : cyan, bg);
#endif
    finish_screen_frame(composed);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_push_recording_pcm(const int16_t *samples, size_t count) {
    (void)samples;
    (void)count;
    // The direct-I2S port already feeds its normalized level into the shared
    // 24-column waveform history; raw PCM remains available for future renderers.
}
void board_port_show_text(const char *title, const char *text) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_thinking_surface_visible = false;
    ESP_LOGI(TAG, "display: %s | %s", title?title:"", text?text:"");
    s_response_active = false;
    s_foreground_surface = true;
    if (title && !strcmp(title, "SETUP MODE")) {
        show_status("设备设置", "正在准备配置");
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    bool ready = title && text &&
                 (!strcmp(title, "MACLAW") || !strcmp(title, "码卡龙")) &&
                 (!strcmp(text, "READY") || !strcmp(text, "Ready") ||
                  !strcmp(text, "ready"));
    if (ready) {
        strlcpy(s_state, "idle", sizeof(s_state));
        s_foreground_surface = false;
        enter_ambient_awake_locked();
        show_state_screen(s_state);
    } else {
        show_status(title, text);
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_show_upload_progress(size_t done, size_t total, const char *stage) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_thinking_surface_visible = false;
    uint16_t bg = color(9,35,64);
    // Avoid 32-bit size_t overflow for long recordings: done * 100 can wrap
    // well below the advertised 512 MiB meeting quota.
    unsigned percent = 0;
    if (total) {
        size_t whole = done / total;
        size_t remainder = done % total;
        percent = whole >= 1 ? 100
                             : (unsigned)(((uint64_t)remainder * 100u) / total);
    }
    if (percent > 100) percent = 100;
    const char *visible_stage = stage && stage[0] ? stage : "正在上传";
    s_response_active = false;
    s_foreground_surface = true;
    bool composed = begin_screen_frame();
    fill_screen(bg);
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    draw_text24_centered(24, "会议录音", color(255,255,255), bg, 8);
    draw_text24_centered(62, visible_stage, color(170,215,235), bg, 9);
    draw_progress_bar(24, 108, 192, 18, percent, color(28,80,111), color(72,205,220));
    char label[16]; snprintf(label, sizeof(label), "%u%%", percent);
    draw_ascii_centered(138, label, color(255,255,255), bg);
    draw_text24_centered(190, "上传中，请勿断电", color(150,195,215), bg, 9);
#else
    draw_text24_centered(66, "会议录音", color(255,255,255), bg, 8);
    draw_text24_centered(112, visible_stage, color(170,215,235), bg, 9);
    draw_progress_bar(24, 184, 192, 18, percent, color(28,80,111), color(72,205,220));
    char label[16]; snprintf(label, sizeof(label), "%u%%", percent);
    draw_ascii_centered(226, label, color(255,255,255), bg);
    // Eight full-width glyphs fit within the 240 px panel. Keep this warning
    // as one semantic line so punctuation can never be stranded at line start.
    draw_text24_centered(272, "上传中，请勿断电", color(150,195,215), bg, 9);
#endif
    finish_screen_frame(composed);
    ESP_LOGI(TAG,"upload %u/%u %s",(unsigned)done,(unsigned)total,stage?stage:"");
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_show_response(const char *title, const char *text) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_thinking_surface_visible = false;
    s_foreground_surface = true;
    s_response_active = true;
    s_response_image_active = false;
    s_response_page = 0;
    strlcpy(s_response_title, title && title[0] ? title : "码卡龙", sizeof(s_response_title));
    response_copy_without_internal_metadata(s_response_text, sizeof(s_response_text), text);
    if (!s_response_text[0]) {
        strlcpy(s_response_text, "没有收到文字回复", sizeof(s_response_text));
    }
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    s_response_next_page_us = response_page_count() > 1
                                  ? esp_timer_get_time() + RESPONSE_AUTO_PAGE_INTERVAL_US
                                  : 0;
#endif
    draw_response_page();
    ESP_LOGI(TAG, "response pages=%u: %s", response_page_count(), s_response_text);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}

void board_port_set_alarm_visual(bool active, unsigned frame, const char *time_text,
                                 const char *label, unsigned attempt, unsigned max_attempts) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    if (!active) {
        // app_ui owns the interrupted scene and replays it immediately after
        // this guard is released. Do not force an ambient frame in between.
        s_alarm_visual_active = false;
        s_foreground_surface = false;
        enter_ambient_awake_locked();
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    // Bread has no animated alarm geometry. Avoid repainting an identical
    // 240x320 frame after every sound burst; only a changed attempt needs new
    // text. This removes sustained LCD/SPI work from the one-minute ring path.
    static unsigned rendered_attempt;
    if (s_alarm_visual_active && rendered_attempt == attempt) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    (void)frame;
    s_alarm_visual_active = true;
    rendered_attempt = attempt;
    s_foreground_surface = true;
    s_response_active = false;
    uint16_t bg = color(9, 23, 38), accent = color(235, 177, 74);
    bool composed = begin_screen_frame();
    fill_screen(bg);
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    fill_rect_solid(16, 8, 208, 4, accent);
    draw_text24_centered(27, "闹钟响铃", color(244, 249, 252), bg, 8);
    draw_ascii_centered(69, time_text && strlen(time_text) >= 16 ? time_text + 11 : "--:--", accent, bg);
    if (label && label[0]) draw_text24_centered(112, label, color(221, 234, 242), bg, 9);
    char attempt_text[28];
    snprintf(attempt_text, sizeof(attempt_text), "%u / %u", attempt, max_attempts);
    draw_ascii_centered(157, attempt_text, color(145, 177, 197), bg);
    draw_text24_centered(199, "按激活键停止", color(145, 177, 197), bg, 8);
#else
    fill_rect_solid(16, 18, 208, 4, accent);
    draw_text24_centered(48, "闹钟响铃", color(244, 249, 252), bg, 8);
    draw_ascii_centered(105, time_text && strlen(time_text) >= 16 ? time_text + 11 : "--:--", accent, bg);
    if (label && label[0]) draw_text24_centered(176, label, color(221, 234, 242), bg, 9);
    char attempt_text[28];
    snprintf(attempt_text, sizeof(attempt_text), "%u / %u", attempt, max_attempts);
    draw_ascii_centered(230, attempt_text, color(145, 177, 197), bg);
    draw_text24_centered(278, "按激活键停止", color(145, 177, 197), bg, 8);
#endif
    finish_screen_frame(composed);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}

void board_port_show_response_image(const char *title, const char *caption,
                                    const uint16_t *pixels, size_t width, size_t height) {
    if (!pixels || width < 1 || width > 64 || height < 1 || height > 64) return;
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_thinking_surface_visible = false;
    s_foreground_surface = true;
    s_response_active = true;
    s_response_image_active = true;
    s_response_page = 0;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    s_response_next_page_us = 0;
#endif
    uint16_t bg = color(8, 17, 28), header = color(14, 31, 47);
    uint16_t accent = color(76, 168, 207), ink = color(244, 248, 251);
    uint16_t muted = color(174, 198, 215);
    bool composed = begin_screen_frame();
    fill_screen(bg);
    const int image_header_h =
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        FANGTANG_HEADER_H;
#else
        60;
#endif
    fill_rect_solid(0, 0, LCD_WIDTH, image_header_h, header);
    fill_rect_solid(RESPONSE_TEXT_X,
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                    10, 4, 24,
#else
                    19, 4, 23,
#endif
                    accent);
    draw_text24_clipped(RESPONSE_TEXT_X + 14,
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                        10,
#else
                        18,
#endif
                        title && title[0] ? title : "码卡龙", ink, header, 8);
    fill_rect_solid(RESPONSE_TEXT_X, image_header_h - 1, RESPONSE_TEXT_WIDTH, 1,
                    color(31, 62, 82));

    // Scale small gateway thumbnails to a useful reading size with nearest-
    // neighbour sampling. It preserves icons, QR-like art and screenshots and
    // avoids wasting most of this compact display on empty background.
    int content_top = image_header_h + 8;
    int content_bottom = caption && caption[0]
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                             ? 172
#else
                             ? 222
#endif
                             : RESPONSE_FOOTER_Y;
    int available_h = content_bottom - content_top;
    int max_w = 176;
    int scale_x = max_w / (int)width;
    int scale_y = available_h / (int)height;
    int image_scale = scale_x < scale_y ? scale_x : scale_y;
    if (image_scale < 1) image_scale = 1;
    if (image_scale > 3) image_scale = 3;
    int shown_w = (int)width * image_scale;
    int shown_h = (int)height * image_scale;
    uint16_t *scaled = NULL;
    const uint16_t *shown_pixels = pixels;
    if (image_scale > 1) {
        scaled = heap_caps_malloc((size_t)shown_w * shown_h * sizeof(uint16_t),
                                  MALLOC_CAP_DMA);
        if (scaled) {
            for (int y = 0; y < shown_h; ++y) {
                for (int x = 0; x < shown_w; ++x) {
                    scaled[(size_t)y * shown_w + x] =
                        pixels[(size_t)(y / image_scale) * width + (x / image_scale)];
                }
            }
            shown_pixels = scaled;
        } else {
            shown_w = (int)width;
            shown_h = (int)height;
        }
    }
    int image_x = (LCD_WIDTH - shown_w) / 2;
    int image_y = content_top + (content_bottom - content_top - shown_h) / 2;
    /* Gateway response images are application/vnd.maclaw.rgb565be: each
     * uint16_t loaded by the little-endian ESP32 already contains the two wire
     * bytes in the order required by esp_lcd (for example red f8 00 loads as
     * 0x00f8). Scaling therefore copies uint16_t values verbatim. Do not apply
     * the pet RGB565LE conversion here; that would swap response-image colours. */
    ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(image_x, image_y,
        image_x + shown_w, image_y + shown_h, shown_pixels));
    heap_caps_free(scaled);
    if (caption && caption[0]) {
        draw_text24_centered(
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
            178,
#else
            238,
#endif
            caption, muted, bg, 8);
    }
    fill_rect_solid(0, RESPONSE_FOOTER_Y, LCD_WIDTH,
                    LCD_HEIGHT - RESPONSE_FOOTER_Y, color(11, 24, 38));
    draw_text24_clipped(RESPONSE_TEXT_X,
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                        214,
#else
                        287,
#endif
                        "激活键返回", muted,
                        color(11, 24, 38), 5);
    finish_screen_frame(composed);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}

bool board_port_navigate_response(int page_delta) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    if (!s_response_active || page_delta == 0) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return false;
    }
    if (s_response_image_active) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return true;
    }
    unsigned pages = response_page_count();
    if (pages > 1) {
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        // This board has no physical paging keys. Only its timed response
        // renderer advances pages after the initial boot network-selection
        // window has elapsed.
        (void)page_delta;
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return true;
#else
        int next = (int)s_response_page + page_delta;
        // The physical reading keys are most useful when they never appear to
        // stop responding at an edge: previous on page 1 wraps to the final
        // page, while next on the final page wraps to page 1.
        if (next < 0) next = (int)pages - 1;
        if (next >= (int)pages) next = 0;
        if ((unsigned)next != s_response_page) {
            s_response_page = (unsigned)next;
            draw_response_page();
        }
#endif
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
    return true;
}

bool board_port_get_response_page(unsigned *page) {
    if (!page || !s_lcd_mutex) return false;
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    bool active = s_response_active;
    if (active) *page = s_response_image_active ? 0u : s_response_page;
    xSemaphoreGiveRecursive(s_lcd_mutex);
    return active;
}

bool board_port_restore_response_page(unsigned page) {
    if (!s_lcd_mutex) return false;
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    if (!s_response_active) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return false;
    }
    if (!s_response_image_active) {
        unsigned pages = response_page_count();
        unsigned target = pages > 0 && page >= pages ? pages - 1 : page;
        if (target != s_response_page) {
            s_response_page = target;
            draw_response_page();
        }
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        s_response_next_page_us = pages > 1
                                      ? esp_timer_get_time() + RESPONSE_AUTO_PAGE_INTERVAL_US
                                      : 0;
#endif
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
    return true;
}
int board_port_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]) {
    if (!bitmap || codepoint < 0x20 || codepoint > 0xffff) return 0;
    size_t replacement = 0;
    uint32_t oldest = UINT32_MAX;
    taskENTER_CRITICAL(&s_glyph_lock);
    for (size_t i = 0; i < DYNAMIC_GLYPH_CACHE_CAPACITY; ++i) {
        if (s_dynamic_glyphs[i].used && s_dynamic_glyphs[i].codepoint == codepoint) {
            replacement = i; oldest = 0; break;
        }
        if (!s_dynamic_glyphs[i].used) { replacement = i; oldest = 0; break; }
        if (s_dynamic_glyphs[i].last_used < oldest) {
            oldest = s_dynamic_glyphs[i].last_used; replacement = i;
        }
    }
    s_dynamic_glyphs[replacement].codepoint = codepoint;
    memcpy(s_dynamic_glyphs[replacement].bitmap, bitmap, DYNAMIC_GLYPH_BYTES);
    s_dynamic_glyphs[replacement].last_used = ++s_dynamic_glyph_clock;
    s_dynamic_glyphs[replacement].used = true;
    taskEXIT_CRITICAL(&s_glyph_lock);
    return 1;
}
void board_port_show_qrcode_matrix(const uint8_t *modules, size_t module_count,
                                   const char *ssid) {
    if (!modules || module_count == 0 || module_count > 177) return;
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    wake_display_for_draw_locked();
    s_idle_pet_sleep_expires_us = 0;
    s_thinking_surface_visible = false;
    s_foreground_surface = true;
    s_response_active = false;
    int module_size = (int)module_count;
    int scale = 180 / (module_size + 8);
    if (scale < 1) { board_port_show_text("setup", ssid); xSemaphoreGiveRecursive(s_lcd_mutex); return; }
    int side = (module_size + 8) * scale;
    uint16_t *qr = heap_caps_malloc((size_t)side * side * sizeof(uint16_t), MALLOC_CAP_DMA);
    if (!qr) { board_port_show_text("setup", ssid); xSemaphoreGiveRecursive(s_lcd_mutex); return; }
    for (int y = 0; y < side; ++y) for (int x = 0; x < side; ++x) {
        int mx = x / scale - 4, my = y / scale - 4;
        bool dark = mx >= 0 && my >= 0 && mx < module_size && my < module_size &&
                    modules[(size_t)my * module_count + mx] != 0;
        qr[y * side + x] = dark ? color(0, 0, 0) : color(255, 255, 255);
    }
    fill_screen(color(255, 255, 255));
    int qr_y = (LCD_HEIGHT - side) / 2 - 20;
    draw_bitmap_sync((LCD_WIDTH - side) / 2, qr_y, (LCD_WIDTH + side) / 2, qr_y + side, qr);
    heap_caps_free(qr);
    draw_ascii_centered(LCD_HEIGHT - 42, "WIFI SETUP", color(0, 0, 0), color(255, 255, 255));
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_show_ready_prompt(const char *title, const char *text) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    if (s_alarm_visual_active) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    s_wifi_connected = true;
    s_gateway_ready = true;
    s_response_active = false;
    strlcpy(s_state, "idle", sizeof(s_state));
    s_foreground_surface = false;
    enter_ambient_awake_locked();
    show_state_screen(s_state);
    ESP_LOGI(TAG, "ready: %s | %s", title ? title : "", text ? text : "");
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_cancel_ready_prompt(void) {}
// Board-owned DISPLAY_OFF transaction shared by Bread Compact and Fangtang.
// It is deliberately display-only: active system services continue until a
// future power coordinator has proven a board-specific MCU sleep transaction.
bool board_port_enter_display_off(void) {
    if (!s_lcd_mutex || !s_panel) return false;
    if (xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return false;
    // Reject a stale timer expiry that races a new foreground render.  The
    // Power Service owns timing; this adapter remains the only authority for
    // deciding whether its actual scene can safely lose panel/backlight.
    bool ambient = !strcmp(s_state, "idle") || !strcmp(s_state, "quiet");
    if (s_display_sleeping || !ambient || s_foreground_surface ||
        s_recording_active || s_response_active || s_alarm_visual_active) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return false;
    }
    s_display_sleeping = true;
    s_idle_pet_sleep_expires_us = 0;
    ESP_ERROR_CHECK_WITHOUT_ABORT(esp_lcd_panel_disp_on_off(s_panel, false));
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    ESP_ERROR_CHECK_WITHOUT_ABORT(fangtang_display_set_backlight(false));
#else
    ESP_ERROR_CHECK_WITHOUT_ABORT(ledc_set_duty(LEDC_LOW_SPEED_MODE,
                                                BREAD_BACKLIGHT_LEDC_CHANNEL,
                                                0));
    ESP_ERROR_CHECK_WITHOUT_ABORT(ledc_update_duty(LEDC_LOW_SPEED_MODE,
                                                   BREAD_BACKLIGHT_LEDC_CHANNEL));
#endif
    xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "display HAL entered DISPLAY_OFF");
    return true;
}

bool board_port_display_is_off(void) {
    if (!s_lcd_mutex) return false;
    if (xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return false;
    bool display_off = s_display_sleeping;
    xSemaphoreGiveRecursive(s_lcd_mutex);
    return display_off;
}

bool board_port_wake_from_idle(void) {
    if (!s_lcd_mutex) return false;
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    if (!s_display_sleeping) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return false;
    }
    wake_display_for_draw_locked();
    s_idle_pet_sleep_expires_us = esp_timer_get_time() + IDLE_PET_SLEEP_TIMEOUT_US;
    strlcpy(s_state, "idle", sizeof(s_state));
    s_response_active = false;
    s_foreground_surface = false;
    show_state_screen(s_state);
    xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "ambient display awakened");
    return true;
}
void board_port_set_wifi_status(const char *ssid, bool connected) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    strlcpy(s_wifi_ssid, ssid ? ssid : "", sizeof(s_wifi_ssid));
    /* Once the authenticated gateway is ready, a transient STA disconnect
     * must not demote the UI. The networking layer reconnects independently. */
    if (!s_gateway_ready || connected) s_wifi_connected = connected;
    if (!s_display_sleeping && !s_recording_active && !s_foreground_surface &&
        (!strcmp(s_state, "idle") || !strcmp(s_state, "quiet"))) show_state_screen(s_state);
    ESP_LOGI(TAG,"wifi %s %s",ssid?ssid:"",connected?"on":"off");
    xSemaphoreGiveRecursive(s_lcd_mutex);
}

void board_port_show_qrcode(esp_qrcode_handle_t qrcode, const char *ssid) {
    if (!qrcode) return;
    const int size = esp_qrcode_get_size(qrcode);
    if (size <= 0 || size > 177) return;
    const size_t pixels = (size_t)size * size;
    uint8_t *modules = heap_caps_malloc(pixels, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!modules) return;
    for (int y = 0; y < size; ++y) {
        for (int x = 0; x < size; ++x) {
            modules[(size_t)y * size + x] = esp_qrcode_get_module(qrcode, x, y) ? 1u : 0u;
        }
    }
    board_port_show_qrcode_matrix(modules, (size_t)size, ssid);
    heap_caps_free(modules);
}
void board_port_set_service_ready(bool ready) {
    if (!s_lcd_mutex) {
        s_gateway_ready = ready;
        return;
    }
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    bool changed = s_gateway_ready != ready;
    s_gateway_ready = ready;
    if (changed && !s_display_sleeping && !s_recording_active && !s_foreground_surface &&
        !s_alarm_visual_active &&
        (!strcmp(s_state, "idle") || !strcmp(s_state, "quiet"))) {
        show_state_screen(s_state);
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_set_ambient(const char *time, const char *location, const char *date, const char *weekday,
 const char *weather, int temp, bool valid, bool stale) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    strlcpy(s_ambient_time, time ? time : "", sizeof(s_ambient_time));
    strlcpy(s_ambient_location, location ? location : "", sizeof(s_ambient_location));
    strlcpy(s_ambient_date, date ? date : "", sizeof(s_ambient_date));
    strlcpy(s_ambient_weekday, weekday ? weekday : "", sizeof(s_ambient_weekday));
    strlcpy(s_ambient_weather, weather ? weather : "", sizeof(s_ambient_weather));
    s_ambient_temperature = temp;
    s_ambient_weather_valid = valid;
    s_ambient_weather_stale = stale;
    if (!s_display_sleeping && !s_recording_active && !s_foreground_surface &&
        (!strcmp(s_state, "idle") || !strcmp(s_state, "quiet"))) show_state_screen(s_state);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}

void board_port_set_alarm_scheduled(bool scheduled) {
    if (!s_lcd_mutex) return;
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    if (s_alarm_scheduled != scheduled) {
        s_alarm_scheduled = scheduled;
        const bool ambient = !strcmp(s_state, "idle") || !strcmp(s_state, "quiet");
        if (ambient && !s_display_sleeping && !s_foreground_surface &&
            !s_recording_active && !s_alarm_visual_active) {
            show_state_screen(s_state);
        }
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
}

esp_err_t board_port_audio_stream_start(void) {
    board_port_pause_wake_word(true);
    /* Pause is asynchronous. Let the recognizer finish its current I2S read
     * and explicitly acknowledge the pause before foreground capture begins,
     * otherwise it can consume the first command samples after the mutex is
     * released between chunks. */
    for (unsigned i = 0; s_wake_task && !s_wake_pause_acknowledged && i < 40; ++i) {
        vTaskDelay(pdMS_TO_TICKS(5));
    }
    if (xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(5000)) != pdTRUE) {
        board_port_pause_wake_word(false);
        return ESP_ERR_TIMEOUT;
    }
    esp_err_t err = audio_init();
    if (err != ESP_OK) {
        xSemaphoreGive(s_audio_mutex);
        board_port_pause_wake_word(false);
        return err;
    }
    s_audio_stream_owned = true;
    return ESP_OK;
}
esp_err_t board_port_audio_stream_read(int16_t *mono, size_t capacity, size_t *read, uint16_t *level) {
    if (!mono || !read) return ESP_ERR_INVALID_ARG;
    return read_mono(mono, capacity, read, level);
}
void board_port_audio_stream_stop(void) {
    if (s_audio_stream_owned) {
        s_audio_stream_owned = false;
        xSemaphoreGive(s_audio_mutex);
    }
    board_port_pause_wake_word(false);
}

void board_port_pause_wake_word(bool paused) {
    s_wake_paused = paused;
    if (!paused) s_wake_pause_acknowledged = false;
}

static void wake_word_task(void *arg) {
    (void)arg;
    /* start_wake_word publishes the task handle immediately after creation.
     * Waiting here closes the small create/early-exit race on model failures. */
    while (s_wake_task_starting) vTaskDelay(1);

    s_wake_ready = false;
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
    for (size_t i = 0;
         command_err == ESP_OK && i < sizeof(s_wake_word_phonetics) / sizeof(s_wake_word_phonetics[0]);
         ++i) {
        command_err = esp_mn_commands_add(WAKE_WORD_COMMAND_ID,
                                          s_wake_word_phonetics[i]);
    }
    esp_mn_error_t *command_errors = command_err == ESP_OK ? esp_mn_commands_update() : NULL;
    if (command_err != ESP_OK || command_errors != NULL) {
        ESP_LOGE(TAG,
                 "offline wake disabled: word '%s' variants rejected (err=%s, rejected=%d)",
                 WAKE_WORD_LABEL, esp_err_to_name(command_err),
                 command_errors ? command_errors->num : 0);
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }
    if (multinet->set_det_threshold) {
        int threshold_err = multinet->set_det_threshold(
            model_data, WAKE_WORD_DETECTION_THRESHOLD);
        if (threshold_err != 0) {
            ESP_LOGW(TAG, "offline wake threshold %.2f was not applied: %d",
                     (double)WAKE_WORD_DETECTION_THRESHOLD, threshold_err);
        }
    }

    const int chunk_samples = multinet->get_samp_chunksize(model_data);
    const int sample_rate = multinet->get_samp_rate(model_data);
    if (chunk_samples <= 0 || sample_rate != AUDIO_RATE) {
        ESP_LOGE(TAG, "offline wake disabled: model format is %d Hz / %d samples",
                 sample_rate, chunk_samples);
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }
    int16_t *mono = heap_caps_malloc((size_t)chunk_samples * sizeof(*mono),
                                     MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    int32_t *raw = heap_caps_malloc((size_t)chunk_samples * sizeof(*raw),
                                    MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!mono || !raw) {
        ESP_LOGE(TAG, "offline wake disabled: no memory for %d-sample buffers",
                 chunk_samples);
        heap_caps_free(mono);
        heap_caps_free(raw);
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }

    s_wake_ready = true;
    ESP_LOGI(TAG,
             "offline wake listening: model=%s phrase='%s' variants=%u threshold=%.2f rate=%d chunk=%d",
             model_name, WAKE_WORD_LABEL,
             (unsigned)(sizeof(s_wake_word_phonetics) / sizeof(s_wake_word_phonetics[0])),
             (double)WAKE_WORD_DETECTION_THRESHOLD, sample_rate, chunk_samples);
    multinet->print_active_speech_commands(model_data);

    bool model_was_paused = false;
    int64_t last_detection_us = 0;
    int64_t last_audio_diagnostic_us = 0;
    while (!s_wake_stop_requested) {
        if (s_wake_paused) {
            if (!model_was_paused) {
                multinet->clean(model_data);
                model_was_paused = true;
            }
            s_wake_pause_acknowledged = true;
            vTaskDelay(pdMS_TO_TICKS(20));
            continue;
        }
        model_was_paused = false;
        s_wake_pause_acknowledged = false;
        if (!s_audio_mutex || xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(50)) != pdTRUE) {
            continue;
        }
        size_t received = 0;
        esp_err_t read_err = i2s_channel_read(
            s_rx, raw, (size_t)chunk_samples * sizeof(*raw), &received,
            pdMS_TO_TICKS(250));
        xSemaphoreGive(s_audio_mutex);
        if (read_err != ESP_OK) {
            if (read_err != ESP_ERR_TIMEOUT) {
                ESP_LOGW(TAG, "offline wake microphone read failed: %s",
                         esp_err_to_name(read_err));
            }
            continue;
        }

        size_t samples = received / sizeof(*raw);
        int32_t input_peak = 0;
        int32_t peak = 0;
        uint64_t energy = 0;
        for (int i = 0; i < chunk_samples; ++i) {
            int32_t input = i < (int)samples ? raw[i] >> 14 : 0;
            int32_t input_magnitude = input < 0 ? -input : input;
            if (input_magnitude > input_peak) input_peak = input_magnitude;
            int32_t amplified = input * WAKE_WORD_INPUT_GAIN_NUM /
                                WAKE_WORD_INPUT_GAIN_DEN;
            if (amplified > INT16_MAX) amplified = INT16_MAX;
            if (amplified < INT16_MIN) amplified = INT16_MIN;
            int16_t sample = (int16_t)amplified;
            mono[i] = sample;
            int32_t magnitude = sample < 0 ? -(int32_t)sample : sample;
            if (magnitude > peak) peak = magnitude;
            energy += (uint32_t)magnitude;
        }
        int64_t diagnostic_now_us = esp_timer_get_time();
        if (diagnostic_now_us - last_audio_diagnostic_us >= 2000000) {
            last_audio_diagnostic_us = diagnostic_now_us;
            ESP_LOGI(TAG,
                     "offline wake mic: samples=%u input_peak=%ld peak=%ld mean=%lu shift=14 gain=%.2f",
                     (unsigned)samples, (long)input_peak, (long)peak,
                     (unsigned long)(energy / (uint32_t)chunk_samples),
                     (double)WAKE_WORD_INPUT_GAIN_NUM / WAKE_WORD_INPUT_GAIN_DEN);
        }

        esp_mn_state_t state = multinet->detect(model_data, mono);
        vTaskDelay(1);
        if (state == ESP_MN_STATE_TIMEOUT) {
            multinet->clean(model_data);
            continue;
        }
        if (state != ESP_MN_STATE_DETECTED) continue;
        esp_mn_results_t *result = multinet->get_results(model_data);
        if (!result || result->num == 0 ||
            result->command_id[0] != WAKE_WORD_COMMAND_ID) {
            continue;
        }
        int64_t now_us = esp_timer_get_time();
        if (now_us - last_detection_us < WAKE_WORD_COOLDOWN_US) {
            multinet->clean(model_data);
            continue;
        }
        last_detection_us = now_us;
        ESP_LOGI(TAG, "offline wake word detected: %s phrase=%d text='%s' raw='%s' (prob=%.3f)",
                 WAKE_WORD_LABEL, result->phrase_id[0], result->string,
                 result->raw_string, (double)result->prob[0]);
        board_port_wake_word_cb_t callback = s_wake_cb;
        void *callback_arg = s_wake_arg;
        multinet->clean(model_data);
        if (callback) callback(callback_arg);
    }

    heap_caps_free(mono);
    heap_caps_free(raw);
    multinet->destroy(model_data);
    esp_srmodel_deinit(models);
    ESP_LOGI(TAG, "offline wake stopped and model memory released");

finish:
    taskENTER_CRITICAL(&s_wake_lock);
    s_wake_stop_requested = false;
    s_wake_paused = false;
    s_wake_pause_acknowledged = false;
    s_wake_ready = false;
    s_wake_task = NULL;
    taskEXIT_CRITICAL(&s_wake_lock);
    vTaskDelete(NULL);
}

esp_err_t board_port_start_wake_word(board_port_wake_word_cb_t cb, void *arg) {
    if (!cb) return ESP_ERR_INVALID_ARG;
    if (!s_audio_mutex ||
        xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(5000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    esp_err_t audio_err = audio_init();
    xSemaphoreGive(s_audio_mutex);
    if (audio_err != ESP_OK) {
        ESP_LOGE(TAG, "offline wake microphone init failed: %s",
                 esp_err_to_name(audio_err));
        return audio_err;
    }

    taskENTER_CRITICAL(&s_wake_lock);
    if (s_wake_task || s_wake_task_starting) {
        bool starting = s_wake_task_starting;
        bool ready = s_wake_ready;
        taskEXIT_CRITICAL(&s_wake_lock);
        /* A duplicate start may race asynchronous model initialization. Wait
         * for the actual ready flag instead of treating task allocation as a
         * working recognizer. This lets the application supervisor retry a
         * failed/timed-out model without reporting a false success. */
        for (unsigned i = 0; i < 400 && (s_wake_task || starting) && !ready; ++i) {
            vTaskDelay(pdMS_TO_TICKS(25));
            taskENTER_CRITICAL(&s_wake_lock);
            starting = s_wake_task_starting;
            ready = s_wake_ready;
            taskEXIT_CRITICAL(&s_wake_lock);
        }
        if (ready) return ESP_OK;
        return (s_wake_task || starting) ? ESP_ERR_TIMEOUT : ESP_FAIL;
    }
    s_wake_task_starting = true;
    s_wake_cb = cb;
    s_wake_arg = arg;
    s_wake_paused = false;
    s_wake_pause_acknowledged = false;
    s_wake_ready = false;
    s_wake_stop_requested = false;
    taskEXIT_CRITICAL(&s_wake_lock);

    TaskHandle_t task = NULL;
    BaseType_t created = xTaskCreatePinnedToCore(
        wake_word_task, "maclaw_offline_wake", 10240, NULL, 4, &task, 1);
    taskENTER_CRITICAL(&s_wake_lock);
    if (created != pdPASS) {
        s_wake_task_starting = false;
        s_wake_cb = NULL;
        s_wake_arg = NULL;
        taskEXIT_CRITICAL(&s_wake_lock);
        return ESP_ERR_NO_MEM;
    }
    s_wake_task = task;
    s_wake_task_starting = false;
    taskEXIT_CRITICAL(&s_wake_lock);
    /* Task creation only proves that a stack was allocated. MultiNet model
     * creation happens asynchronously and can still fail after a command has
     * fragmented internal RAM. Report success only after inference is ready,
     * so the application restart supervisor can retry a real init failure. */
    for (unsigned i = 0; i < 400 && s_wake_task && !s_wake_ready; ++i) {
        vTaskDelay(pdMS_TO_TICKS(25));
    }
    if (s_wake_ready) {
        ESP_LOGI(TAG, "offline wake task ready");
        return ESP_OK;
    }
    if (!s_wake_task) {
        ESP_LOGW(TAG, "offline wake task exited during model initialization");
        return ESP_FAIL;
    }
    ESP_LOGW(TAG, "offline wake model initialization timed out");
    return ESP_ERR_TIMEOUT;
}

esp_err_t board_port_stop_wake_word(void) {
    taskENTER_CRITICAL(&s_wake_lock);
    if (!s_wake_task && !s_wake_task_starting) {
        taskEXIT_CRITICAL(&s_wake_lock);
        return ESP_ERR_INVALID_STATE;
    }
    s_wake_paused = true;
    s_wake_stop_requested = true;
    taskEXIT_CRITICAL(&s_wake_lock);

    for (unsigned i = 0; i < 240 && (s_wake_task || s_wake_task_starting); ++i) {
        vTaskDelay(pdMS_TO_TICKS(25));
    }
    if (s_wake_task || s_wake_task_starting) return ESP_ERR_TIMEOUT;
    taskENTER_CRITICAL(&s_wake_lock);
    s_wake_cb = NULL;
    s_wake_arg = NULL;
    taskEXIT_CRITICAL(&s_wake_lock);
    ESP_LOGI(TAG, "offline wake task stopped cleanly");
    return ESP_OK;
}

esp_err_t board_port_capture_wav(uint8_t **out, size_t *out_len) {
    if (!out || !out_len) return ESP_ERR_INVALID_ARG;
    *out=NULL;*out_len=0;
    // Standby MultiNet owns the same I2S RX channel. Wait for its current
    // read to finish before taking the mutex, otherwise its next inference
    // chunk can steal the beginning of the command.
    board_port_pause_wake_word(true);
    for (unsigned i = 0; s_wake_task && !s_wake_pause_acknowledged && i < 40; ++i) {
        vTaskDelay(pdMS_TO_TICKS(5));
    }
    if (!s_audio_mutex || xSemaphoreTake(s_audio_mutex,pdMS_TO_TICKS(1500))!=pdTRUE) {
        board_port_pause_wake_word(false);
        return ESP_ERR_TIMEOUT;
    }
    const size_t max_samples = AUDIO_RATE * COMMAND_CAPTURE_MAX_SECONDS;
    const size_t start_timeout_samples =
        AUDIO_RATE * COMMAND_CAPTURE_START_TIMEOUT_MS / 1000;
    const size_t silence_samples = AUDIO_RATE * COMMAND_CAPTURE_SILENCE_MS / 1000;
    const size_t start_confirm_samples =
        AUDIO_RATE * COMMAND_CAPTURE_START_CONFIRM_MS / 1000;
    const size_t preroll_samples = AUDIO_RATE * COMMAND_CAPTURE_PREROLL_MS / 1000;
    size_t len=44+max_samples*2;
    uint8_t *wav=heap_caps_malloc(len,MALLOC_CAP_SPIRAM|MALLOC_CAP_8BIT);
    if(!wav){xSemaphoreGive(s_audio_mutex);board_port_pause_wake_word(false);return ESP_ERR_NO_MEM;}
    memset(wav,0,44);memcpy(wav,"RIFF",4);uint32_t v=len-8;memcpy(wav+4,&v,4);memcpy(wav+8,"WAVEfmt ",8);
    v=16;memcpy(wav+16,&v,4);uint16_t s=1;memcpy(wav+20,&s,2);memcpy(wav+22,&s,2);v=AUDIO_RATE;memcpy(wav+24,&v,4);
    v=AUDIO_RATE*2;memcpy(wav+28,&v,4);s=2;memcpy(wav+32,&s,2);s=16;memcpy(wav+34,&s,2);memcpy(wav+36,"data",4);v=max_samples*2;memcpy(wav+40,&v,4);
    int16_t *pcm = (int16_t *)(wav + 44);
    size_t done = 0;
    size_t voiced = 0;
    size_t silence = 0;
    size_t speech_start_sample = 0;
    bool speech_started = false;
    uint16_t smoothed_level = 0;
    uint16_t idle_level = 0;
    uint32_t last_ui_second = UINT32_MAX;
    s_command_capture_active = true;
    while (done < max_samples) {
        size_t got = 0;
        uint16_t level = 0;
        esp_err_t err = read_mono(pcm + done, max_samples - done, &got, &level);
        if (err != ESP_OK) {
            s_command_capture_active = false;
            free(wav);
            xSemaphoreGive(s_audio_mutex);
            board_port_pause_wake_word(false);
            return err;
        }
        if (got == 0) continue;
        done += got;
        const uint16_t mean_level = command_capture_mean_level(pcm + done - got, got);
        smoothed_level = level > smoothed_level
                             ? (uint16_t)((smoothed_level + level * 3u) / 4u)
                             : (uint16_t)((smoothed_level * 7u + level) / 8u);
        uint32_t elapsed = (uint32_t)(done / AUDIO_RATE);
        // Command capture is synchronous, unlike the meeting stream. Feed the
        // same shared recording UI from this loop so its timer and waveform
        // remain live on every supported board.
        board_port_push_recording_pcm(pcm + done - got, got);
        board_port_set_audio_level(smoothed_level, elapsed);
        if (elapsed != last_ui_second) {
            board_port_set_recording_visual(true, false, elapsed);
            last_ui_second = elapsed;
        }
        // Require a short run of voiced frames to reject clicks, then end only
        // after a longer quiet interval. Hysteresis keeps ordinary soft
        // consonants and brief between-word pauses from ending the command.
        if (!speech_started) {
            // Learn the local microphone floor while waiting for speech.  The
            // minimum keeps a spoken preamble from raising the floor.
            if (idle_level == 0 || mean_level < idle_level) idle_level = mean_level;
            voiced = level >= COMMAND_CAPTURE_START_LEVEL ? voiced + got : 0;
            if (voiced >= start_confirm_samples) {
                speech_started = true;
                silence = 0;
                speech_start_sample = done - voiced;
                ESP_LOGI(TAG, "command speech started after %u ms", (unsigned)(done * 1000 / AUDIO_RATE));
            } else if (done >= start_timeout_samples) {
                ESP_LOGI(TAG, "command capture timed out waiting for speech");
                s_command_capture_active = false;
                free(wav);
                xSemaphoreGive(s_audio_mutex);
                board_port_pause_wake_word(false);
                return ESP_ERR_NOT_FOUND;
            }
        } else {
            uint16_t silence_level = idle_level + COMMAND_CAPTURE_SILENCE_MARGIN;
            if (silence_level < COMMAND_CAPTURE_SILENCE_FLOOR) {
                silence_level = COMMAND_CAPTURE_SILENCE_FLOOR;
            }
            if (silence_level > COMMAND_CAPTURE_SILENCE_CEILING) {
                silence_level = COMMAND_CAPTURE_SILENCE_CEILING;
            }
            silence = mean_level <= silence_level ? silence + got : 0;
            if (silence >= silence_samples) {
                ESP_LOGI(TAG,
                         "command capture ended after %u ms of silence (mean=%u threshold=%u)",
                         COMMAND_CAPTURE_SILENCE_MS, mean_level, silence_level);
                break;
            }
        }
        if (s_command_capture_stop_requested) {
            ESP_LOGI(TAG, "command capture manually stopped: speech=%s elapsed=%ums",
                     speech_started ? "yes" : "no",
                     (unsigned)(done * 1000 / AUDIO_RATE));
            break;
        }
    }
    s_command_capture_active = false;
    if (!speech_started) {
        free(wav);
        xSemaphoreGive(s_audio_mutex);
        board_port_pause_wake_word(false);
        return ESP_ERR_NOT_FOUND;
    }
    const size_t trim_start = speech_start_sample > preroll_samples
                                  ? speech_start_sample - preroll_samples
                                  : 0;
    const size_t captured_samples = done - trim_start;
    if (trim_start > 0) {
        memmove(pcm, pcm + trim_start, captured_samples * sizeof(*pcm));
    }
    ESP_LOGI(TAG, "captured %u mono samples (trimmed %u ms)",
             (unsigned)captured_samples,
             (unsigned)(trim_start * 1000 / AUDIO_RATE));
    const size_t actual_len = 44 + captured_samples * sizeof(*pcm);
    v = (uint32_t)(actual_len - 8); memcpy(wav + 4, &v, sizeof(v));
    v = (uint32_t)(captured_samples * sizeof(*pcm)); memcpy(wav + 40, &v, sizeof(v));
    xSemaphoreGive(s_audio_mutex);
    board_port_pause_wake_word(false);
    *out=wav;
    *out_len=actual_len;
    return ESP_OK;
}

// Peak level is useful for promptly noticing the beginning of speech, but is
// a poor silence detector on small MEMS microphones: one click in a 32 ms I2S
// block makes the whole block appear loud. Fangtang's microphone can also have
// a sizeable DC offset, so measure mean deviation from the block's DC level.
// Keep its 0..1000 scale compatible with the peak level from read_mono().
static uint16_t command_capture_mean_level(const int16_t *samples, size_t count) {
    if (!samples || count == 0) return 0;
    int64_t sum = 0;
    for (size_t i = 0; i < count; ++i) {
        sum += samples[i];
    }
    const int32_t dc = (int32_t)(sum / (int64_t)count);
    uint64_t deviation_sum = 0;
    for (size_t i = 0; i < count; ++i) {
        int32_t deviation = (int32_t)samples[i] - dc;
        deviation_sum += (uint32_t)(deviation < 0 ? -deviation : deviation);
    }
    uint32_t mean_deviation = (uint32_t)(deviation_sum / count);
    return mean_deviation >= 12000 ? 1000
                                   : (uint16_t)(mean_deviation * 1000 / 12000);
}

void board_port_set_network_transport(bool cellular) {
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    if (!s_lcd_mutex) {
        s_network_transport_cellular = cellular;
        return;
    }
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    bool changed = s_network_transport_cellular != cellular;
    s_network_transport_cellular = cellular;
    if (changed && !s_display_sleeping && !s_recording_active && !s_foreground_surface &&
        (!strcmp(s_state, "idle") || !strcmp(s_state, "quiet"))) {
        show_state_screen(s_state);
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
#else
    (void)cellular;
#endif
}

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G || \
    !defined(MACLAW_FANGTANG_EXTERNAL_POWER_STATUS_GETTER)
bool board_port_get_power_status(unsigned *level_percent, bool *charging) {
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    taskENTER_CRITICAL(&s_power_status_lock);
    bool valid = s_battery_level_valid;
    if (level_percent) *level_percent = s_battery_level;
    if (charging) *charging = s_battery_charging;
    taskEXIT_CRITICAL(&s_power_status_lock);
    return valid;
#else
    (void)level_percent;
    (void)charging;
    return false;
#endif
}
#endif

void board_port_request_capture_stop(void) {
    // Retain a stop that lands in the application-to-reader hand-off window.
    s_command_capture_stop_requested = true;
}

void board_port_reset_capture_stop(void) {
    s_command_capture_stop_requested = false;
}

void board_port_request_audio_playback_stop(void) {
    if (s_audio_playback_owner) s_audio_playback_stop_requested = true;
}

static esp_err_t write_stereo(const int16_t *source, size_t frames, unsigned channels) {
    int16_t stereo[512];
    // Take one coherent gain snapshot per DMA block. A GUI hardware_config
    // update is then visible on the next block without partially scaling a
    // stereo frame or racing an optimizer-cached ordinary variable.
    size_t done = 0;
    while (done < frames) {
        if (s_audio_playback_stop_requested) return ESP_ERR_INVALID_STATE;
        size_t count = frames - done;
        if (count > 256) count = 256;
        const unsigned volume = __atomic_load_n(&s_output_volume, __ATOMIC_RELAXED);
        for (size_t i = 0; i < count; ++i) {
            int32_t left = source[(done + i) * channels];
            int32_t right = channels == 2 ? source[(done + i) * 2 + 1] : left;
            stereo[i * 2] = (int16_t)(left * (int32_t)volume / 100);
            stereo[i * 2 + 1] = (int16_t)(right * (int32_t)volume / 100);
        }
        size_t written = 0;
        size_t expected = count * 2 * sizeof(int16_t);
        esp_err_t err = i2s_channel_write(s_tx, stereo, expected, &written, pdMS_TO_TICKS(1000));
        if (err != ESP_OK) return err;
        if (written != expected) return ESP_ERR_TIMEOUT;
        done += count;
    }
    return ESP_OK;
}

static esp_err_t speaker_play_begin(void) {
    return i2s_channel_enable(s_tx);
}

static esp_err_t speaker_play_end(esp_err_t playback_err) {
    /* Give the final descriptor time to leave DMA, followed by a short zero
     * tail. Disabling immediately after i2s_channel_write only proves that the
     * bytes were queued, not that the speaker consumed them. */
    vTaskDelay(pdMS_TO_TICKS(20));
    int16_t silence[128] = {0};
    esp_err_t silence_err = write_stereo(silence, 128, 1);
    vTaskDelay(pdMS_TO_TICKS(10));
    esp_err_t stop_err = i2s_channel_disable(s_tx);
    if (stop_err != ESP_OK) ESP_LOGW(TAG, "speaker stop failed: %s", esp_err_to_name(stop_err));
    if (playback_err != ESP_OK) return playback_err;
    if (silence_err != ESP_OK) return silence_err;
    return stop_err;
}

esp_err_t board_port_play_wav(const uint8_t *wav, size_t len) {
    if (!wav || len < 44 || memcmp(wav, "RIFF", 4) != 0 ||
        memcmp(wav + 8, "WAVE", 4) != 0) {
        return ESP_ERR_INVALID_ARG;
    }
    const uint8_t *format_chunk = NULL;
    const uint8_t *audio_data = NULL;
    size_t format_size = 0;
    size_t audio_size = 0;
    for (size_t offset = 12; offset + 8 <= len;) {
        const uint8_t *chunk = wav + offset;
        uint32_t chunk_size = (uint32_t)chunk[4] | ((uint32_t)chunk[5] << 8) |
                              ((uint32_t)chunk[6] << 16) | ((uint32_t)chunk[7] << 24);
        offset += 8;
        if (chunk_size > len - offset) return ESP_ERR_INVALID_SIZE;
        if (memcmp(chunk, "fmt ", 4) == 0) {
            format_chunk = wav + offset;
            format_size = chunk_size;
        } else if (memcmp(chunk, "data", 4) == 0) {
            audio_data = wav + offset;
            audio_size = chunk_size;
        }
        size_t padded = (size_t)chunk_size + (chunk_size & 1u);
        if (padded > len - offset) return ESP_ERR_INVALID_SIZE;
        offset += padded;
    }
    if (!format_chunk || format_size < 16 || !audio_data || !audio_size) {
        return ESP_ERR_INVALID_ARG;
    }
    uint16_t format = (uint16_t)format_chunk[0] | ((uint16_t)format_chunk[1] << 8);
    uint16_t channels = (uint16_t)format_chunk[2] | ((uint16_t)format_chunk[3] << 8);
    uint32_t rate = (uint32_t)format_chunk[4] | ((uint32_t)format_chunk[5] << 8) |
                    ((uint32_t)format_chunk[6] << 16) | ((uint32_t)format_chunk[7] << 24);
    uint16_t bits = (uint16_t)format_chunk[14] | ((uint16_t)format_chunk[15] << 8);
    if (format != 1 || bits != 16 || (channels != 1 && channels != 2) || rate != AUDIO_RATE) {
        return ESP_ERR_NOT_SUPPORTED;
    }
    size_t frame_bytes = channels * sizeof(int16_t);
    if (audio_size % frame_bytes != 0) return ESP_ERR_INVALID_SIZE;
    esp_err_t err = board_port_audio_playback_begin();
    if (err == ESP_OK) {
        err = board_port_audio_playback_write((const int16_t *)audio_data,
                                              audio_size / frame_bytes, channels);
        err = board_port_audio_playback_end(err);
    }
    return err;
}

esp_err_t board_port_audio_playback_begin(void) {
    if (!s_audio_mutex) return ESP_ERR_INVALID_STATE;
    // Give MultiNet time to leave its current microphone read and acknowledge
    // the pause before speaker playback takes the shared I2S mutex. Acquiring
    // the mutex first leaves a race where the recognizer cannot acknowledge
    // until after playback and may resume with stale detector state.
    board_port_pause_wake_word(true);
    for (unsigned i = 0; s_wake_task && !s_wake_pause_acknowledged && i < 60; ++i) {
        vTaskDelay(pdMS_TO_TICKS(5));
    }
    if (xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(1500)) != pdTRUE) {
        board_port_pause_wake_word(false);
        return ESP_ERR_TIMEOUT;
    }
    if (s_audio_playback_owner) {
        xSemaphoreGive(s_audio_mutex);
        board_port_pause_wake_word(false);
        return ESP_ERR_INVALID_STATE;
    }
    esp_err_t err = audio_init();
    if (err == ESP_OK) err = speaker_play_begin();
    if (err == ESP_OK) {
        s_audio_playback_stop_requested = false;
        s_audio_playback_owner = xTaskGetCurrentTaskHandle();
    } else {
        xSemaphoreGive(s_audio_mutex);
        board_port_pause_wake_word(false);
    }
    return err;
}

esp_err_t board_port_audio_playback_write(const int16_t *pcm, size_t frames,
                                          unsigned channels) {
    if (s_audio_playback_owner != xTaskGetCurrentTaskHandle()) {
        return ESP_ERR_INVALID_STATE;
    }
    if (!pcm || frames == 0 || (channels != 1 && channels != 2)) {
        return ESP_ERR_INVALID_ARG;
    }
    if (s_audio_playback_stop_requested) return ESP_ERR_INVALID_STATE;
    return write_stereo(pcm, frames, channels);
}

esp_err_t board_port_audio_playback_end(esp_err_t playback_err) {
    if (s_audio_playback_owner != xTaskGetCurrentTaskHandle()) {
        return ESP_ERR_INVALID_STATE;
    }
    esp_err_t err = speaker_play_end(playback_err);
    s_audio_playback_owner = NULL;
    s_audio_playback_stop_requested = false;
    xSemaphoreGive(s_audio_mutex);
    board_port_pause_wake_word(false);
    return err;
}
esp_err_t board_port_play_ack_chime(void) {
    esp_err_t err = board_port_audio_playback_begin();
    if (err != ESP_OK) return err;
    int16_t mono[256];
    for (int block = 0; block < 10 && err == ESP_OK; ++block) {
        for (int i = 0; i < 256; ++i) {
            mono[i] = sinf(2 * 3.14159265f * 660 * (block * 256 + i) / AUDIO_RATE) * 3500;
        }
        err = board_port_audio_playback_write(mono, 256, 1);
    }
    return board_port_audio_playback_end(err);
}
esp_err_t board_port_play_alarm_burst(void) {
    int16_t mono[256];
    esp_err_t err = board_port_audio_playback_begin();
    if (err != ESP_OK) return err;
    for (int strike = 0; strike < 3 && err == ESP_OK; ++strike) {
        float hz = strike & 1 ? 2050.0f : 1700.0f;
        for (int block = 0; block < 5 && err == ESP_OK; ++block) {
            float amplitude = 7600.0f - block * 1100.0f;
            for (int i = 0; i < 256; ++i) {
                mono[i] = (int16_t)(sinf(2.0f * 3.14159265f * hz * (block * 256 + i) / AUDIO_RATE) * amplitude);
            }
            err = board_port_audio_playback_write(mono, 256, 1);
        }
    }
    return board_port_audio_playback_end(err);
}
esp_err_t board_port_play_ack_voice(void) {return board_port_play_ack_chime();}
