#include "board_port.h"

#include <ctype.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "driver/gpio.h"
#include "driver/i2c_master.h"
#include "driver/i2s_std.h"
#include "driver/ledc.h"
#include "driver/spi_master.h"
#include "esp_attr.h"
#include "esp_heap_caps.h"
#include "esp_check.h"
#include "esp_mn_models.h"
#include "esp_mn_speech_commands.h"
#if CONFIG_MACLAW_AFE_ENABLE
#include "esp_afe_sr_models.h"
#endif
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"
#include "esp_lcd_panel_commands.h"
#include "esp_lcd_st77916.h"
#include "esp_log.h"
#include "esp_timer.h"
#if CONFIG_MACLAW_POWER_SAVE
#include "esp_pm.h"
#endif
#include "model_path.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "echoear_st77916_init.h"
#include "audio_common.h"
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
#define AUDIO_DMA_DESC_NUM  8
#define AUDIO_DMA_FRAME_NUM 256
#define AUDIO_SECONDS   6
#define WAKE_WORD_COMMAND_ID 1
// Keep one recognizer running in standby. Running Chinese and English models
// together doubles inference work and makes normal-speed wake-up unreliable.
#define WAKE_WORD_CN_LABEL "码卡龙"
// MultiNet7 Chinese commands use space-separated pinyin syllables without
// tone digits.  Supplying tone digits makes its command validator reject the
// phrase, which silently disabled standby wake-up altogether.
#define WAKE_WORD_CN_PHONETIC "ma ka long"
// The default command threshold favours deliberate, slow commands.  A modest
// reduction preserves a practical false-wake margin while accepting normal
// conversational delivery of the short product name.
#define WAKE_WORD_DETECTION_THRESHOLD 0.30f
#define WAKE_WORD_COOLDOWN_US (2LL * 1000 * 1000)
#define WAKE_WORD_INVALID_SAMPLE_ABS 32500
#if CONFIG_MACLAW_AFE_ENABLE
// AEC playback-reference ring depth, in mono samples. 4096 samples cover
// 256 ms at 16 kHz, comfortably above the TX DMA queue depth plus the
// acoustic path delay the AEC filter has to model.
#define AFE_AEC_REF_SAMPLES 4096
// The AFE feed task is pinned to core 0 (the wake-word/MultiNet task owns
// core 1) and outranks the recognizer so I2S RX never starves.
#define AFE_FEED_TASK_STACK 4096
#define AFE_FEED_TASK_PRIORITY 5
#endif

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
// Idle pet motion (blink, thinking dots) still reads naturally at ~6 fps.
// Halving the cadence while only the idle pet is on screen halves the
// full-frame composition and QSPI DMA work, which matters on battery. The
// higher rate is kept for recording/response surfaces that need the faster
// visual feedback. With the esp-sr AFE enabled, core 0 is already busy with
// the feed task, so every skipped composition also frees CPU headroom.
#define PET_ANIMATION_MOTION_FRAME_MS 160
#define READY_PROMPT_TIMEOUT_US (60LL * 1000 * 1000)
// Default idle screen-off timeout. The settings menu overrides it at runtime
// (0 seconds keeps the panel always on); see s_screen_timeout_us.
#define SCREEN_TIMEOUT_DEFAULT_SECONDS 300u
#define PET_HALO_CENTER_Y 175
#define PET_HALO_RADIUS   106
#define PET_ASSET_MAX_WIDTH 128
#define PET_ASSET_MAX_HEIGHT 128
#define PET_ASSET_MAX_FRAMES 2
#define PET_ASSET_REVISION_CAPACITY 24

// Backlight dimming: the ST77916 backlight LED sits on GPIO41 and is driven
// by one LEDC channel so the idle-sleep transition can ramp brightness
// instead of switching it as a plain GPIO.
#define BACKLIGHT_LEDC_MODE     LEDC_LOW_SPEED_MODE
#define BACKLIGHT_LEDC_TIMER    LEDC_TIMER_0
#define BACKLIGHT_LEDC_CHANNEL  LEDC_CHANNEL_0
#define BACKLIGHT_LEDC_FREQ_HZ  5000
#define BACKLIGHT_MAX_DUTY      ((1u << 10) - 1u)
// esp_lcd_st77916 frames register writes for the QSPI interface with the
// 0x02 (1-1-1 page program) opcode in the top byte and the 8-bit command
// shifted left by 8; see tx_param() in esp_lcd_st77916_spi.c. SLPIN/SLPOUT
// use no parameters.
#define ST77916_QSPI_CMD(cmd)   ((0x02 << 24) | ((int)(cmd) << 8))
// Power down the I2S TX direction after this much playback inactivity. The
// RX direction stays enabled at all times for the AFE/wake-word path.
#define SPEAKER_IDLE_TIMEOUT_US (30LL * 1000 * 1000)
// After this much gesture inactivity the button task parks on the GPIO
// interrupt notification instead of polling the shared I2C touch bus.
#define TOUCH_IDLE_TIMEOUT_US   (2LL * 1000 * 1000)

static const char *TAG = "maclaw_board";
static esp_lcd_panel_handle_t s_panel;
static esp_lcd_panel_io_handle_t s_panel_io;
static board_port_button_cb_t s_on_button;
static void *s_on_press_arg;
static board_port_settings_cb_t s_on_settings;
static void *s_on_settings_arg;

typedef enum {
    SETTINGS_SCREEN_CLOSED = 0,
    SETTINGS_SCREEN_MENU,
    SETTINGS_SCREEN_CONFIRM_PAIRING,
    SETTINGS_SCREEN_CONFIRM_ALL,
    SETTINGS_SCREEN_TIMEOUT_PICKER,
} settings_screen_t;

static volatile settings_screen_t s_settings_screen;
static volatile bool s_settings_dragging;
static volatile uint8_t s_settings_pending_volume = 70;
// Highlighted entry of s_screen_timeout_presets[] while the picker page is
// open; initialised to the 300 s default preset.
static volatile uint8_t s_settings_pending_timeout_idx = 1;
// Codec volume follows every drag sample, but a 360x360 LCD present is much
// more expensive. Throttle visual previews and always submit the final value
// on release so dragging stays responsive without flooding the display queue.
static int64_t s_settings_last_preview_post_us;
static TaskHandle_t s_settings_task;
// Settings requests travel on a short queue instead of a task
// notification so a burst (volume drag previews) can never overwrite a
// pending destructive clear confirmation.
static QueueHandle_t s_settings_queue;

#define SETTINGS_TASK_REDRAW         1u
#define SETTINGS_TASK_VOLUME_PREVIEW 2u
#define SETTINGS_TASK_VOLUME_COMMIT  3u
#define SETTINGS_TASK_CLEAR_PAIRING  4u
#define SETTINGS_TASK_CLEAR_ALL      5u
#define SETTINGS_TASK_TIMEOUT_CHANGED 6u
#define SETTINGS_TASK_KIND_MASK      0xFFu
#define SETTINGS_TASK_VALUE_SHIFT    8u

// Screen-off timeout presets offered by the settings picker, in tap order.
// 0 seconds means the panel never sleeps. The labels are drawn on the menu
// row and as the picker's large centered choice.
typedef struct {
    uint32_t seconds;
    const char *label;
} screen_timeout_preset_t;
static const screen_timeout_preset_t s_screen_timeout_presets[] = {
    {60, "1 分钟"},    {300, "5 分钟"},   {600, "10 分钟"},
    {900, "15 分钟"},  {1200, "20 分钟"}, {1800, "30 分钟"},
    {3600, "1 小时"},  {7200, "2 小时"},  {10800, "3 小时"},
    {14400, "4 小时"}, {18000, "5 小时"}, {0, "不熄屏"},
};
#define SCREEN_TIMEOUT_PRESET_COUNT \
    ((uint8_t)(sizeof(s_screen_timeout_presets) / sizeof(s_screen_timeout_presets[0])))

static char s_pet_state[16] = "quiet";
static char s_pet_skin[32] = "clawmate";
static bool s_pet_motion_enabled = true;
static uint8_t *s_pet_asset_frames;
static uint16_t s_pet_asset_width;
static uint16_t s_pet_asset_height;
static uint8_t s_pet_asset_frame_count;
static uint32_t s_pet_asset_frame_interval_ms = 700;
static char s_pet_asset_revision[PET_ASSET_REVISION_CAPACITY];
// Set when a profile update arrives while a foreground command owns the LCD;
// board_port_set_command_display_lock(false) repaints once the lock lifts.
static volatile bool s_pet_profile_dirty;
static uint8_t s_pet_frame;
static uint32_t s_pet_motion_tick;
static bool s_recording_active;
static bool s_recording_paused;
// Bumped on every recording-surface state change; the animation task skips
// redrawing the static paused surface while this stays constant.
static volatile uint32_t s_recording_visual_revision;
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
static uint32_t s_recording_wave_pending_abs_sum;
static uint16_t s_recording_wave_pending_usable;
static uint16_t s_recording_wave_pending_clipped;
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
// Runtime idle screen-off budget; 0 disables idle sleep entirely. Written
// through board_port_set_screen_timeout(), persisted in NVS by the app.
static int64_t s_screen_timeout_us = SCREEN_TIMEOUT_DEFAULT_SECONDS * 1000000LL;
static bool s_idle_pet_visible;
static bool s_display_sleeping;
// Set by board_port_wake_from_idle() instead of running the ~250 ms wake
// sequence in the touch scan task; pet_animation_task performs it.
static volatile bool s_display_wake_pending;
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
// Same change-counter discipline as s_ambient_revision: bumped on every pet
// profile (skin/motion) change so the animation task repaints even when the
// update landed while recording or a display lock skipped the direct draw.
static volatile uint32_t s_pet_profile_revision;
// Last profile revision that draw_pet_frame() actually presented.
// Updated only after a real draw so the animation task's reconcile
// branch never acknowledges a frame an early-out skipped.
static volatile uint32_t s_rendered_pet_profile_revision;
static portMUX_TYPE s_state_lock = portMUX_INITIALIZER_UNLOCKED;

// s_pet_state and s_pet_skin are written by network/UI tasks while the
// animation task reads them on every frame. Follow the same discipline as
// the ambient strings (see draw_clock_calendar): snapshot under s_state_lock,
// compare and use outside the critical section.
static void pet_state_copy(char *out, size_t cap) {
    taskENTER_CRITICAL(&s_state_lock);
    strlcpy(out, s_pet_state, cap);
    taskEXIT_CRITICAL(&s_state_lock);
}

static void pet_state_store(const char *state) {
    taskENTER_CRITICAL(&s_state_lock);
    strlcpy(s_pet_state, state, sizeof(s_pet_state));
    taskEXIT_CRITICAL(&s_state_lock);
}

static bool pet_state_is(const char *first, const char *second) {
    char current[sizeof(s_pet_state)];
    pet_state_copy(current, sizeof(current));
    return !strcmp(current, first) || (second && !strcmp(current, second));
}

static void pet_skin_copy(char *out, size_t cap) {
    taskENTER_CRITICAL(&s_state_lock);
    strlcpy(out, s_pet_skin, cap);
    taskEXIT_CRITICAL(&s_state_lock);
}
static uint32_t pet_animation_frame_ms(void);
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
// Status text and the two ambient dirty regions share one DMA buffer sized
// for the largest region. Every consumer fills it and hands it straight to
// draw_or_compose_bitmap(), which either copies synchronously into the PSRAM
// render target or waits for the LCD transfer to finish, so sequential reuse
// cannot modify pixels that are still in flight (the failure mode that once
// required dedicated buffers, before the draw fence existed). Sharing frees
// ~78 KB of internal RAM for the esp-sr AFE.
#define SHARED_TEXT_PIXELS (AMBIENT_BOTTOM_W * AMBIENT_BOTTOM_H)  // largest of the three
static DMA_ATTR uint16_t s_shared_text[SHARED_TEXT_PIXELS];
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
static volatile bool s_command_cancel_enabled;
static volatile uint32_t s_command_gesture_revision;
// Once a double tap is emitted, discard every residual native/raw touch event
// until the panel has been continuously released. Otherwise the same physical
// second tap can later mature into a SHORT event and start a fresh command.
static volatile bool s_touch_gesture_consumed;
static int64_t s_touch_gesture_released_at_us;
static bool s_recording_is_meeting;
static SemaphoreHandle_t s_audio_mutex;
// A short voice command normally stops after AUDIO_SECONDS, but a second
// panel tap should submit immediately. These flags are touched only by the
// capture worker and the input task, so volatile visibility is sufficient;
// the capture worker remains the sole owner of the WAV buffer and I2S RX.
static volatile bool s_short_capture_accepts_stop;
static volatile bool s_short_capture_stop_requested;
// Backlight state: s_backlight_percent is the user-selected brightness,
// s_backlight_level the duty currently applied by the fade ramp.
static uint8_t s_backlight_percent = 100;
static uint8_t s_backlight_level;
// Speaker idle power management. The TX channel starts enabled from
// audio_init() and is cut after SPEAKER_IDLE_TIMEOUT_US without playback.
static bool s_speaker_tx_enabled;
static int64_t s_speaker_last_used_us;
static TaskHandle_t s_button_task;
#if CONFIG_MACLAW_POWER_SAVE
static esp_pm_lock_handle_t s_lcd_apb_lock;
#endif
// Recording health counters, reset at every capture/stream start and exposed
// through board_port_get_record_stats() so a meeting with dropped audio is
// visible instead of silently shipping a WAV with holes.
static uint32_t s_record_overrun_count;
static uint32_t s_record_short_read_count;
// DC-blocker (first-order high-pass) state. The ES7210 on this board has a
// measurable DC offset; removing it before upload gives the server ASR the
// full usable dynamic range.
static int32_t s_hpf_x_prev;
static int32_t s_hpf_y_prev;
// Meeting-path AGC: tracks the recent peak with decay so quiet far-field
// speech is normalised without pumping on every chunk.
static int32_t s_stream_agc_peak;
// Speaker volume in percent, persisted by the app layer. The default matches
// the long-standing ES8311 init value 0xB2 (~70%).
static uint8_t s_volume_percent = 70;
// volatile: board_port_stop_wake_word() polls this handle while the wake word
// task clears it from its own context just before vTaskDelete().
static volatile TaskHandle_t s_wake_word_task;
static board_port_wake_word_cb_t s_on_wake_word;
static void *s_on_wake_word_arg;
static volatile bool s_wake_word_paused;
static volatile bool s_wake_word_stop_requested;
static portMUX_TYPE s_wake_word_lock = portMUX_INITIALIZER_UNLOCKED;
// Set while the speaker is playing. Unlike s_wake_word_paused (which hands
// the microphone exclusively to the capture/meeting paths), playback keeps
// the AFE feed/fetch loop running so the AEC stays converged and barge-in
// stays possible; the wake task only suppresses the detection callback. The
// raw fallback path treats this flag like a pause.
static volatile bool s_playback_active;
#if CONFIG_MACLAW_AFE_ENABLE
static const esp_afe_sr_iface_t *s_afe_iface;
static esp_afe_sr_data_t *s_afe_data;
static afe_config_t *s_afe_config;
static volatile TaskHandle_t s_afe_feed_task;
static volatile bool s_afe_feed_stop;
// Single-producer (playback) single-consumer (AFE feed) ring of the mono PCM
// queued to the speaker, consumed as the AEC reference channel.
static int16_t *s_aec_ref_buf;
static uint32_t s_aec_ref_wr;
static uint32_t s_aec_ref_rd;
static portMUX_TYPE s_aec_ref_lock = portMUX_INITIALIZER_UNLOCKED;
static void aec_ref_write(const int16_t *samples, size_t count);
#endif

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
static void draw_settings_icon(void);

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
#if CONFIG_MACLAW_POWER_SAVE
    // Keep APB at full speed for the QSPI DMA burst under DFS.
    if (s_lcd_apb_lock) esp_pm_lock_acquire(s_lcd_apb_lock);
#endif
    esp_err_t err = esp_lcd_panel_draw_bitmap(s_panel, x0, y0, x1, y1, pixels);
    if (err == ESP_OK) err = wait_for_lcd_color_transfer();
#if CONFIG_MACLAW_POWER_SAVE
    if (s_lcd_apb_lock) esp_pm_lock_release(s_lcd_apb_lock);
#endif
    return err;
}

static void backlight_apply(uint8_t level) {
    s_backlight_level = level;
    uint32_t duty = (uint32_t)level * BACKLIGHT_MAX_DUTY / 100u;
    ESP_ERROR_CHECK_WITHOUT_ABORT(ledc_set_duty(BACKLIGHT_LEDC_MODE, BACKLIGHT_LEDC_CHANNEL, duty));
    ESP_ERROR_CHECK_WITHOUT_ABORT(ledc_update_duty(BACKLIGHT_LEDC_MODE, BACKLIGHT_LEDC_CHANNEL));
}

void board_port_set_backlight(uint8_t percent) {
    if (percent > 100) percent = 100;
    s_backlight_percent = percent;
    backlight_apply(percent);
}

void board_port_prepare_deep_sleep(void) {
    // Hold the backlight off and the PA disabled through deep sleep. Without
    // gpio_hold the pads return to their reset state once the LEDC/GPIO
    // peripherals power down, which can re-light the backlight and drain the
    // cell the battery monitor is trying to protect.
    board_port_set_backlight(0);
    (void)gpio_set_level(AUDIO_PA_ENABLE, 0);
    gpio_hold_en(LCD_BACKLIGHT);
    gpio_hold_en(AUDIO_PA_ENABLE);
    gpio_deep_sleep_hold_en();
}

// A short linear ramp for sleep/wake transitions: a handful of 20% steps is
// enough to read as a soft fade; this is deliberately not an animation engine.
static void backlight_fade_to(uint8_t target) {
    int level = s_backlight_level;
    while (level != (int)target) {
        level += level < (int)target ? 20 : -20;
        if (level < 0) level = 0;
        if (level > 100) level = 100;
        backlight_apply((uint8_t)level);
        if (level != (int)target) vTaskDelay(pdMS_TO_TICKS(25));
    }
}

// Idle-sleep sequencing for the ST77916. On top of DISPON/DISPOFF the panel
// is sent SLPIN/SLPOUT through the same QSPI vendor-command channel the
// esp_lcd_st77916 driver uses for its init sequence (tx_param()), which cuts
// the panel's internal oscillator while the screen is off. SLPOUT needs the
// datasheet 120 ms before the display can accept DISPON again.
static void display_enter_sleep(void) {
    // The whole DISPOFF+SLPIN sequence must be atomic against frame draws;
    // s_lcd_mutex is recursive, so callers already holding it stay safe.
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    backlight_fade_to(0);
    ESP_ERROR_CHECK_WITHOUT_ABORT(esp_lcd_panel_disp_on_off(s_panel, false));
    if (s_panel_io) {
        (void)esp_lcd_panel_io_tx_param(s_panel_io, ST77916_QSPI_CMD(LCD_CMD_SLPIN), NULL, 0);
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

static void display_exit_sleep(bool panel_in_sleep) {
    // Same serialization as display_enter_sleep(): SLPOUT/DISPON/backlight
    // must not interleave with an in-flight frame transfer.
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    if (panel_in_sleep && s_panel_io) {
        (void)esp_lcd_panel_io_tx_param(s_panel_io, ST77916_QSPI_CMD(LCD_CMD_SLPOUT), NULL, 0);
        vTaskDelay(pdMS_TO_TICKS(120));
    }
    ESP_ERROR_CHECK_WITHOUT_ABORT(esp_lcd_panel_disp_on_off(s_panel, true));
    backlight_fade_to(s_backlight_percent);
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

// Speaker idle power management. Only the I2S TX direction is cut: on
// ESP32-S3 i2s_channel_disable() stops the TX state machine/DMA but leaves
// the shared BCK/WS/MCLK generators running (tx_clk_active stays set), so the
// always-on RX direction feeding the AFE/wake word is unaffected. The PA is
// already switched off by playback_end(). ES8311 DAC register power states
// are not touched: the exact low-power sequence for this board's codec
// wiring is not documented, so no unverified register values are written.
// Under CONFIG_PM_ENABLE the I2S driver already holds its own APB frequency
// lock while the TX channel is enabled, so no extra lock is needed here.
// Both helpers must be called with s_audio_mutex held.
static void speaker_wakeup(void) {
    if (s_audio_tx && !s_speaker_tx_enabled) {
        esp_err_t err = i2s_channel_enable(s_audio_tx);
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "I2S TX re-enable failed: %s", esp_err_to_name(err));
        } else {
            s_speaker_tx_enabled = true;
        }
    }
    s_speaker_last_used_us = esp_timer_get_time();
}

static void speaker_idle_powerdown(void) {
    if (!s_audio_tx || !s_speaker_tx_enabled) return;
    if (esp_timer_get_time() - s_speaker_last_used_us < SPEAKER_IDLE_TIMEOUT_US) return;
    esp_err_t err = i2s_channel_disable(s_audio_tx);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "I2S TX idle power-down failed: %s", esp_err_to_name(err));
        return;
    }
    s_speaker_tx_enabled = false;
    ESP_LOGI(TAG, "speaker idle: I2S TX powered down");
}

static esp_err_t es7210_write(uint8_t reg, uint8_t value) {
    uint8_t bytes[2] = {reg, value};
    return i2c_master_transmit(s_es7210, bytes, sizeof(bytes), 1000);
}

static esp_err_t es8311_write(uint8_t reg, uint8_t value) {
    uint8_t bytes[2] = {reg, value};
    return i2c_master_transmit(s_es8311, bytes, sizeof(bytes), 1000);
}

// ES8311 reg 0x32 (DAC volume): 0xFF is full scale (0 dB), lower values
// attenuate in fine steps, 0x00 mutes.
static esp_err_t es8311_apply_volume(void) {
    uint8_t reg = s_volume_percent == 0 ? 0
                : (uint8_t)((s_volume_percent * 255u + 50u) / 100u);
    return es8311_write(0x32, reg);
}

esp_err_t board_port_set_volume(uint8_t percent) {
    if (percent > 100) percent = 100;
    s_volume_percent = percent;
    // Before the codec is initialised the value is only stored; es8311_init()
    // applies it on first use.
    if (!s_audio_ready) return ESP_OK;
    return es8311_apply_volume();
}

uint8_t board_port_get_volume(void) {
    return s_volume_percent;
}

void board_port_set_screen_timeout(uint32_t seconds) {
    // Only preset values are meaningful (the NVS copy could have been edited
    // off-list); snap anything unknown to the 300 s default so the menu label
    // and the actual behaviour always agree.
    if (seconds != 0) {
        bool known = false;
        for (uint8_t i = 0; i < SCREEN_TIMEOUT_PRESET_COUNT; ++i) {
            if (s_screen_timeout_presets[i].seconds == seconds) { known = true; break; }
        }
        if (!known) seconds = 300;
    }
    s_screen_timeout_us = (int64_t)seconds * 1000000LL;
    if (seconds == 0) {
        // The zero expiry is inert: the sleep check is guarded on
        // s_screen_timeout_us > 0, so the panel stays awake indefinitely.
        s_idle_pet_sleep_expires_us = 0;
    } else if (s_idle_pet_visible && !s_display_sleeping) {
        // Apply the new budget to the currently running idle countdown.
        s_idle_pet_sleep_expires_us = esp_timer_get_time() + s_screen_timeout_us;
    }
}

uint32_t board_port_get_screen_timeout(void) {
    return (uint32_t)(s_screen_timeout_us / 1000000LL);
}

// Picker index for a timeout in seconds; unknown values fall back to the
// 300 s default preset so the menu row always shows a sane label.
static uint8_t screen_timeout_preset_index(uint32_t seconds) {
    for (uint8_t i = 0; i < SCREEN_TIMEOUT_PRESET_COUNT; ++i) {
        if (s_screen_timeout_presets[i].seconds == seconds) return i;
    }
    return 1;
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
    // The table carries the historical default (0xB2); re-apply the persisted
    // volume so a user setting survives the codec reset above.
    return es8311_apply_volume();
}

// Undo a partially constructed audio/input chain so a later audio_init()
// retries from a clean slate. Without this, a mid-sequence failure (e.g. the
// ES8311 add) left the I2C master bus allocated and every retry failed at
// i2c_new_master_bus() with ESP_ERR_INVALID_STATE forever.
static void audio_init_rollback(void) {
    if (s_audio_rx) {
        (void)i2s_channel_disable(s_audio_rx);
        (void)i2s_del_channel(s_audio_rx);
        s_audio_rx = NULL;
    }
    if (s_audio_tx) {
        (void)i2s_channel_disable(s_audio_tx);
        (void)i2s_del_channel(s_audio_tx);
        s_audio_tx = NULL;
    }
    if (s_touch) {
        i2c_master_dev_handle_t device = s_touch;
        s_touch = NULL;
        (void)i2c_master_bus_rm_device(device);
    }
    if (s_es8311) { (void)i2c_master_bus_rm_device(s_es8311); s_es8311 = NULL; }
    if (s_es7210) { (void)i2c_master_bus_rm_device(s_es7210); s_es7210 = NULL; }
    if (s_audio_i2c_bus) { (void)i2c_del_master_bus(s_audio_i2c_bus); s_audio_i2c_bus = NULL; }
}

static esp_err_t audio_init(void) {
    if (s_audio_ready) return ESP_OK;
    esp_err_t err;
    i2c_master_bus_config_t bus_cfg = {
        .i2c_port = I2C_NUM_0, .sda_io_num = AUDIO_I2C_SDA,
        .scl_io_num = AUDIO_I2C_SCL, .clk_source = I2C_CLK_SRC_DEFAULT,
        .glitch_ignore_cnt = 7, .flags.enable_internal_pullup = true,
    };
    err = i2c_new_master_bus(&bus_cfg, &s_audio_i2c_bus);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "audio I2C init: %s", esp_err_to_name(err));
        goto fail;
    }
    i2c_device_config_t dev_cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7, .device_address = ES7210_ADDRESS,
        .scl_speed_hz = 100000,
    };
    err = i2c_master_bus_add_device(s_audio_i2c_bus, &dev_cfg, &s_es7210);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "ES7210 add: %s", esp_err_to_name(err));
        goto fail;
    }
    i2c_device_config_t speaker_cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7, .device_address = ES8311_ADDRESS,
        .scl_speed_hz = 100000,
    };
    err = i2c_master_bus_add_device(s_audio_i2c_bus, &speaker_cfg, &s_es8311);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "ES8311 add: %s", esp_err_to_name(err));
        goto fail;
    }
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
        // 0x1E clips the EchoEar microphone continuously.  Keep the verified
        // 0x1A analogue gain so speech remains within the recognizer's usable
        // PCM range.
        {0x43,0x1A},{0x44,0x1A},{0x45,0x1A},{0x46,0x1A},
        {0x47,0x08},{0x48,0x08},{0x49,0x00},{0x4A,0x00},
        {0x07,0x20},{0x02,0xC1},{0x04,0x01},{0x05,0x00},
        {0x06,0x04},{0x4B,0x0F},{0x4C,0x0F},{0x00,0x71},{0x00,0x41},
    };
    for (size_t i = 0; i < sizeof(init) / sizeof(init[0]); ++i) {
        err = es7210_write(init[i][0], init[i][1]);
        if (err != ESP_OK) {
            ESP_LOGE(TAG, "ES7210 reg %02x: %s", init[i][0], esp_err_to_name(err));
            goto fail;
        }
    }

    i2s_chan_config_t chan_cfg = I2S_CHANNEL_DEFAULT_CONFIG(I2S_NUM_0, I2S_ROLE_MASTER);
    chan_cfg.dma_desc_num = AUDIO_DMA_DESC_NUM;
    chan_cfg.dma_frame_num = AUDIO_DMA_FRAME_NUM;
    err = i2s_new_channel(&chan_cfg, NULL, &s_audio_rx);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "I2S RX create: %s", esp_err_to_name(err));
        goto fail;
    }
    i2s_std_config_t std_cfg = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(AUDIO_RATE),
        .slot_cfg = I2S_STD_PHILIPS_SLOT_DEFAULT_CONFIG(I2S_DATA_BIT_WIDTH_16BIT, I2S_SLOT_MODE_STEREO),
        .gpio_cfg = {
            .mclk = AUDIO_MCLK, .bclk = AUDIO_BCLK, .ws = AUDIO_WS,
            .dout = I2S_GPIO_UNUSED, .din = AUDIO_DIN,
            .invert_flags = {.mclk_inv = false, .bclk_inv = false, .ws_inv = false},
        },
    };
    err = i2s_channel_init_std_mode(s_audio_rx, &std_cfg);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "I2S RX mode: %s", esp_err_to_name(err));
        goto fail;
    }
    err = i2s_channel_enable(s_audio_rx);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "I2S RX enable: %s", esp_err_to_name(err));
        goto fail;
    }
    err = i2s_new_channel(&chan_cfg, &s_audio_tx, NULL);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "I2S TX create: %s", esp_err_to_name(err));
        goto fail;
    }
    std_cfg.gpio_cfg.dout = AUDIO_DOUT;
    err = i2s_channel_init_std_mode(s_audio_tx, &std_cfg);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "I2S TX mode: %s", esp_err_to_name(err));
        goto fail;
    }
    err = i2s_channel_enable(s_audio_tx);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "I2S TX enable: %s", esp_err_to_name(err));
        goto fail;
    }
    s_speaker_tx_enabled = true;
    s_speaker_last_used_us = esp_timer_get_time();
    gpio_config_t pa_cfg = {
        .pin_bit_mask = 1ULL << AUDIO_PA_ENABLE, .mode = GPIO_MODE_OUTPUT,
        .pull_up_en = GPIO_PULLUP_DISABLE, .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    err = gpio_config(&pa_cfg);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "speaker PA GPIO: %s", esp_err_to_name(err));
        goto fail;
    }
    err = gpio_set_level(AUDIO_PA_ENABLE, 0);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "speaker PA off: %s", esp_err_to_name(err));
        goto fail;
    }
    err = es8311_init();
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "ES8311 init: %s", esp_err_to_name(err));
        goto fail;
    }
    s_audio_ready = true;
    ESP_LOGI(TAG, "EchoEar audio ready: ES7210 mic + ES8311 speaker at 16kHz");
    return ESP_OK;
fail:
    audio_init_rollback();
    return err;
}

// The TX DMA queue holds AUDIO_DMA_DESC_NUM * AUDIO_DMA_FRAME_NUM frames of
// audio (128 ms at 16 kHz). i2s_channel_write() returns as soon as bytes are
// queued, not when they have actually been played, so cutting PA power right
// after the last write truncates the tail of every reply, and powering the
// PA up/down in the middle of a stream clicks. Pad both ends with a short
// silence ramp and wait for the queue to drain before powering the PA down.
#define AUDIO_TX_DMA_MS ((AUDIO_DMA_DESC_NUM * AUDIO_DMA_FRAME_NUM * 1000) / AUDIO_RATE)
#define AUDIO_ANTI_POP_PAD_MS 15

static esp_err_t playback_write_silence_ms(unsigned ms) {
    int16_t zeros[512] = {0};  // 256 stereo frames
    unsigned frames_left = (AUDIO_RATE * ms) / 1000;
    while (frames_left) {
        size_t frames = frames_left > 256 ? 256 : frames_left;
        size_t written = 0;
        esp_err_t err = i2s_channel_write(s_audio_tx, zeros, frames * 2 * sizeof(int16_t),
                                          &written, pdMS_TO_TICKS(1000));
        if (err != ESP_OK) return err;
#if CONFIG_MACLAW_AFE_ENABLE
        // Keep the AEC reference aligned with the anti-pop pads too.
        aec_ref_write(zeros, frames);
#endif
        frames_left -= frames;
    }
    return ESP_OK;
}

static esp_err_t playback_begin(void) {
    esp_err_t err = audio_init();
    if (err == ESP_OK) speaker_wakeup();
    if (err == ESP_OK) err = gpio_set_level(AUDIO_PA_ENABLE, 1);
    if (err == ESP_OK) err = playback_write_silence_ms(AUDIO_ANTI_POP_PAD_MS);
    return err;
}

static void playback_end(void) {
    // Queue a trailing silence pad, then wait out whatever the DMA queue still
    // holds (at most one full buffer) plus margin before cutting PA power.
    (void)playback_write_silence_ms(AUDIO_ANTI_POP_PAD_MS);
    vTaskDelay(pdMS_TO_TICKS(AUDIO_TX_DMA_MS + 50));
    (void)gpio_set_level(AUDIO_PA_ENABLE, 0);
    // The 30 s idle countdown starts from the end of playback, not its start.
    s_speaker_last_used_us = esp_timer_get_time();
}

static void record_stats_reset(void) {
    s_record_overrun_count = 0;
    s_record_short_read_count = 0;
    s_hpf_x_prev = 0;
    s_hpf_y_prev = 0;
    s_stream_agc_peak = 4096;
}

void board_port_get_record_stats(uint32_t *overruns, uint32_t *short_reads) {
    if (overruns) *overruns = s_record_overrun_count;
    if (short_reads) *short_reads = s_record_short_read_count;
}

static int16_t record_hpf(int32_t x) {
    // y[i] = x[i] - x[i-1] + 0.995 * y[i-1]; 0.995 in Q15 is 32604.
    int32_t y = x - s_hpf_x_prev + (int32_t)(((int64_t)s_hpf_y_prev * 32604) >> 15);
    s_hpf_x_prev = x;
    s_hpf_y_prev = y;
    if (y > INT16_MAX) y = INT16_MAX;
    else if (y < INT16_MIN) y = INT16_MIN;
    return (int16_t)y;
}

// Discard DMA frames that accumulated while the recognizer was paused or a
// meeting was suspended; otherwise the first ~128 ms of a new recording is
// stale audio captured before the recording actually started.
static void audio_rx_flush_stale(void) {
    int16_t scratch[512];
    // Read until the DMA queue is empty; a full queue needs more than three
    // chunks. The 16-iteration cap only guards against a pathological driver.
    for (unsigned i = 0; i < 16; ++i) {
        size_t received = 0;
        if (i2s_channel_read(s_audio_rx, scratch, sizeof(scratch), &received, 0) != ESP_OK ||
            received == 0) {
            break;
        }
    }
}

// Normalisation target: -3 dBFS. Gain is capped at 12 dB (4x) so background
// noise is not amplified when nobody spoke.
#define RECORD_AGC_TARGET_PEAK 23196
#define RECORD_AGC_MAX_GAIN_Q15 (4 << 15)
#define RECORD_AGC_QUIET_PEAK 8192

static int32_t record_agc_gain_q15(int32_t recent_peak) {
    if (recent_peak <= 0 || recent_peak >= RECORD_AGC_QUIET_PEAK) return 1 << 15;
    int32_t gain_q15 = (int32_t)(((int64_t)RECORD_AGC_TARGET_PEAK << 15) / recent_peak);
    return gain_q15 > RECORD_AGC_MAX_GAIN_Q15 ? RECORD_AGC_MAX_GAIN_Q15 : gain_q15;
}

static void record_apply_gain(int16_t *samples, size_t count, int32_t gain_q15) {
    if (gain_q15 == (1 << 15)) return;
    for (size_t i = 0; i < count; ++i) {
        int32_t v = (int32_t)(((int64_t)samples[i] * gain_q15) >> 15);
        if (v > INT16_MAX) v = INT16_MAX;
        else if (v < INT16_MIN) v = INT16_MIN;
        samples[i] = (int16_t)v;
    }
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
    // (v * 0x8081) >> 23 is v / 255 for every 16-bit v, without a divide.
    uint16_t r = (uint16_t)(((uint32_t)(fr * inv + tr * amount + 127) * 0x8081u) >> 23);
    uint16_t g = (uint16_t)(((uint32_t)(fg * inv + tg * amount + 127) * 0x8081u) >> 23);
    uint16_t b = (uint16_t)(((uint32_t)(fb * inv + tb * amount + 127) * 0x8081u) >> 23);
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
        // Rows of the 360-wide framebuffer stay 4-byte aligned, so the body
        // can be filled two pixels at a time with one odd pixel at each end.
        const uint32_t pair = (uint32_t)color | ((uint32_t)color << 16);
        const int width = x1 - x0;
        for (int y = y0; y < y1; ++y) {
            uint16_t *row = s_render_target + (size_t)y * LCD_WIDTH + x0;
            int n = width;
            if ((uintptr_t)row & 3) {
                *row++ = color;
                --n;
            }
            uint32_t *row32 = (uint32_t *)row;
            for (int i = 0; i < n / 2; ++i) row32[i] = pair;
            if (n & 1) row[n - 1] = color;
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
    static const uint8_t percent[5] = {0x63, 0x13, 0x08, 0x64, 0x63};
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
    if (c == '%') return percent;
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
    for (size_t i = 0; i < (size_t)AMBIENT_TOP_W * AMBIENT_TOP_H; ++i) {
        s_shared_text[i] = overlay_bg;
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
        compose_text16_curve(s_shared_text, AMBIENT_TOP_W, AMBIENT_TOP_W, AMBIENT_TOP_H,
                             AMBIENT_TOP_W / 2, 7, 520, ring, primary);
    }
    draw_or_compose_bitmap(12, 13, 12 + AMBIENT_TOP_W, 13 + AMBIENT_TOP_H,
                           s_shared_text, keyed_overlay);

    // City precedes weather on the matching lower arc. Its physical region
    // starts below the native pet's 96..272 drawing area, so neither text nor
    // background clearing can cut into the pet circle.
    for (size_t i = 0; i < (size_t)AMBIENT_BOTTOM_W * AMBIENT_BOTTOM_H; ++i) {
        s_shared_text[i] = overlay_bg;
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
        compose_text24_curve(s_shared_text, AMBIENT_BOTTOM_W, AMBIENT_BOTTOM_W, AMBIENT_BOTTOM_H,
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
        s_shared_text, keyed_overlay);
}

// The quiet screen is also a passive pet surface.  Treat it like idle so a
// clock that has already been drawn never appears frozen while the device is
// waiting for its first interaction after boot or reconnect.
static bool ambient_visible_for_state(void) {
    return pet_state_is("idle", "quiet");
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
	if (s_pet_asset_frames && s_pet_asset_frame_count > 1 &&
	    s_pet_asset_frame_interval_ms > 0) {
		uint64_t elapsed_ms = (uint64_t)s_pet_motion_tick * pet_animation_frame_ms();
		signature |= 0x80000000u |
			(uint32_t)((elapsed_ms / s_pet_asset_frame_interval_ms) % s_pet_asset_frame_count) << 24;
	}
    // Processing is an active state, not a static acknowledgement. Advance a
    // small three-dot orbit at 320 ms steps so the user can tell that the
    // recorded request is still being handled while network work is pending.
    if (pet_state_is("thinking", NULL)) {
        signature |= 0x10000u | ((s_pet_motion_tick / 4u) & 3u) << 17;
    }
    char pet_skin[sizeof(s_pet_skin)];
    pet_skin_copy(pet_skin, sizeof(pet_skin));
    if (strstr(pet_skin, "ragdoll") != NULL) {
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
    char pet_state[sizeof(s_pet_state)];
    char pet_skin[sizeof(s_pet_skin)];
    pet_state_copy(pet_state, sizeof(pet_state));
    pet_skin_copy(pet_skin, sizeof(pet_skin));
    uint16_t bg = state_color(pet_state);
    uint16_t halo_top = rgb565(113, 211, 255);
    uint16_t halo_bottom = rgb565(34, 91, 204);
    bool ragdoll = strstr(pet_skin, "ragdoll") != NULL;
    bool mini = strstr(pet_skin, "mini") != NULL;
    bool claw = strstr(pet_skin, "claw") != NULL;
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
	// Prefer the exact frame rendered by MaClaw GUI when one has been fetched.
	// It is centered in the pet-owned band and scaled with nearest-neighbour
	// sampling; the clock/calendar and settings affordance remain native so the
	// hardware interaction surface is still readable around arbitrary packs.
	if (s_pet_asset_frames && s_pet_asset_frame_count > 0 &&
	    s_pet_asset_width > 0 && s_pet_asset_height > 0) {
		uint8_t frame_index = 0;
		if (s_pet_motion_enabled && s_pet_asset_frame_count > 1 &&
		    s_pet_asset_frame_interval_ms > 0) {
			uint64_t elapsed_ms = (uint64_t)s_pet_motion_tick * pet_animation_frame_ms();
			frame_index = (uint8_t)((elapsed_ms / s_pet_asset_frame_interval_ms) % s_pet_asset_frame_count);
		}
		const uint8_t *src = s_pet_asset_frames +
			(size_t)frame_index * s_pet_asset_width * s_pet_asset_height * 2;
		const int size = 216;
		const int left = (LCD_WIDTH - size) / 2;
		const int top = 67;
		for (int y = 0; y < size; ++y) {
			uint16_t *dst = s_render_target ? s_render_target + (size_t)(top + y) * LCD_WIDTH + left : s_line;
			int sy = y * s_pet_asset_height / size;
			for (int x = 0; x < size; ++x) {
				int sx = x * s_pet_asset_width / size;
				size_t index = ((size_t)sy * s_pet_asset_width + sx) * 2;
				uint16_t logical = (uint16_t)src[index] | ((uint16_t)src[index + 1] << 8);
				dst[x] = __builtin_bswap16(logical);
			}
			if (!s_render_target) {
				ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(left, top + y, left + size, top + y + 1, s_line));
			}
		}
		if (ambient_visible_for_state()) {
			draw_clock_calendar(bg);
			draw_settings_icon();
		}
		return;
	}
    // Keep the pet body anchored. Idle life now comes only from blinking and
    // the independently eased tail; moving the complete silhouette vertically
    // made the character look as if it were bouncing in place.
    int bob = 0;
    if (ragdoll) {
        draw_ragdoll_pet(bob, bg);
        if (ambient_visible_for_state()) {
            draw_clock_calendar(bg);
            // The gear marks the settings entry corner; show it only on
            // the idle/quiet surface where the touch entry is armed.
            draw_settings_icon();
        }
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
    if (!strcmp(pet_state, "thinking")) {
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
    if (ambient_visible_for_state()) {
        draw_clock_calendar(bg);
        // The gear marks the settings entry corner; show it only on the
        // idle/quiet surface where the touch entry is armed.
        draw_settings_icon();
    }
}

static bool draw_pet_frame(bool redraw_background) {
    // A renderer can be selected by the animation task and then wait behind a
    // result/recording transfer for the LCD mutex. Re-check ownership after
    // taking the mutex so that this stale pet frame cannot paint over the
    // newer command surface once the transfer ahead of it has completed.
    if (s_display_sleeping || s_recording_active || s_response_active ||
        s_setup_qrcode_visible || s_settings_screen != SETTINGS_SCREEN_CLOSED ||
        (s_command_display_locked && ambient_visible_for_state())) return false;
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return false;
    if (s_display_sleeping || s_recording_active || s_response_active ||
        s_setup_qrcode_visible || s_settings_screen != SETTINGS_SCREEN_CLOSED ||
        (s_command_display_locked && ambient_visible_for_state())) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        return false;
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
        // Throttle: a persistent panel fault would otherwise log at the full
        // animation rate (up to 12.5 Hz).
        static unsigned s_present_fail_count;
        if (draw_err == ESP_OK) {
            s_present_fail_count = 0;
            s_next_framebuffer ^= 1u;
            ++s_presented_frames;
        } else {
            if ((++s_present_fail_count & 0x3Fu) == 1u) {
                ESP_LOGE(TAG, "frame present failed (%u in a row): %s",
                         s_present_fail_count, esp_err_to_name(draw_err));
            }
            if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
            // The framebuffer was not consumed (a partially transferred stripe
            // series is fully redrawn on retry): leave the profile revision
            // pending so the reconcile branch retries next frame.
            return false;
        }
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    // The frame reached the panel (or the stripe path drew directly):
    // acknowledge the profile revision here so every successful draw
    // path (direct and reconciled) agrees on what has been rendered,
    // and a skipped early-out never counts.
    s_rendered_pet_profile_revision = s_pet_profile_revision;
    return true;
}

static void draw_pet(void) {
    (void)draw_pet_frame(true);
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

    // Snapshot the measured PCM envelope. These are the same microphone
    // samples that are saved/uploaded; no animation phase or random value is
    // involved, so silence remains near the centre and speech transients move.
    int16_t wave_min[RECORDING_WAVE_COLUMNS];
    int16_t wave_max[RECORDING_WAVE_COLUMNS];
    taskENTER_CRITICAL(&s_state_lock);
    memcpy(wave_min, s_recording_wave_min, sizeof(wave_min));
    memcpy(wave_max, s_recording_wave_max, sizeof(wave_max));
    taskEXIT_CRITICAL(&s_state_lock);
    const int wave_left = 26;
    const int wave_width = 308;
    const int wave_center = 220;
    const int wave_half_height = 42;
    fill_rect(wave_left, wave_center, wave_left + wave_width, wave_center + 1, muted);
    if (!s_recording_paused) {
        for (int column = 0; column < RECORDING_WAVE_COLUMNS; ++column) {
            int32_t lo = wave_min[column] < 0 ? -(int32_t)wave_min[column] : wave_min[column];
            int32_t hi = wave_max[column] < 0 ? -(int32_t)wave_max[column] : wave_max[column];
            int32_t amplitude = (lo + hi) / 2;
            if (amplitude > 9000) amplitude = 9000;
            int half = amplitude > 180 ? 1 + (int)(amplitude * wave_half_height / 9000) : 1;
            int x = wave_left + column * wave_width / RECORDING_WAVE_COLUMNS;
            fill_rect(x, wave_center - half, x + 2, wave_center + half + 1, cyan);
        }
    }
    if (s_recording_is_meeting) {
        draw_text24(98, 266, s_recording_paused ? "会议记录已暂停" : "会议记录进行中",
                    s_recording_paused ? amber : red, bg);
        draw_text24(80, 302, "点屏停止保存", muted, bg);
    } else {
        draw_text24(98, 266, "正在记录命令", cyan, bg);
        draw_text24(68, 302, "说完点屏，或等待提交", muted, bg);
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

static uint32_t pet_animation_frame_ms(void) {
    // While the display sleeps no frame is composed at all (every draw branch
    // below is gated on s_display_sleeping); the loop only keeps the timeout
    // bookkeeping alive, so it can tick at the slow cadence too.
    if (s_display_sleeping) return PET_ANIMATION_MOTION_FRAME_MS;
    if (s_pet_motion_enabled && (s_idle_pet_visible || pet_state_is("thinking", NULL))) {
        return PET_ANIMATION_MOTION_FRAME_MS;
    }
    return PET_ANIMATION_FRAME_MS;
}

static void pet_animation_task(void *arg) {
    (void)arg;
    uint32_t rendered_ambient_revision = s_ambient_revision;
    uint32_t rendered_motion_signature = pet_motion_signature();
    TickType_t next_frame = xTaskGetTickCount();
    int64_t next_diagnostic_us = esp_timer_get_time() + 5000000;
    int64_t next_speaker_check_us = esp_timer_get_time() + 5000000;
    while (true) {
        // Keep a stable cadence: render/transfer time must not accumulate on
        // top of the requested frame interval.
        vTaskDelayUntil(&next_frame, pdMS_TO_TICKS(pet_animation_frame_ms()));
        // Read the revision after the delay. Reading it at the end of a frame
        // can acknowledge a clock update that arrived while the previous DMA
        // transfer was running even though that second was never rendered.
        uint32_t pending_ambient_revision = s_ambient_revision;
        if (s_display_wake_pending) {
            // Deferred panel wake from board_port_wake_from_idle(); runs here
            // on the display task instead of blocking touch scanning.
            s_display_wake_pending = false;
            display_exit_sleep(true);
        }
        if (s_setup_qrcode_visible || s_settings_screen != SETTINGS_SCREEN_CLOSED) {
            // The QR code and its white quiet zone must stay pixel-stable for
            // phone cameras. Nothing else should draw while setup is active.
        } else if (s_recording_active) {
            // A paused recording surface is static: the elapsed time is
            // frozen and the level reads zero, so redrawing it at the motion
            // cadence is pure DMA waste. Repaint only on state changes.
            static uint32_t rendered_recording_revision;
            if (!s_recording_paused ||
                s_recording_visual_revision != rendered_recording_revision) {
                s_pet_frame = (uint8_t)((s_pet_frame + 1u) % 8u);
                draw_recording_visual();
                rendered_recording_revision = s_recording_visual_revision;
            }
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
            pet_state_store("idle");
            s_idle_pet_visible = true;
            s_idle_pet_sleep_expires_us = esp_timer_get_time() + s_screen_timeout_us;
            draw_pet();
        } else if (s_idle_pet_visible && s_screen_timeout_us > 0 &&
                   esp_timer_get_time() >= s_idle_pet_sleep_expires_us) {
            s_idle_pet_visible = false;
            s_display_sleeping = true;
            display_enter_sleep();
        } else if (!s_display_sleeping && !s_command_display_locked &&
                   !s_response_active &&
                   s_rendered_pet_profile_revision != s_pet_profile_revision) {
            // A profile update may land while recording or a foreground lock
            // owns the LCD and skip its direct draw. Reconcile the revision
            // here so the old skin/motion never survives the return to the
            // ambient pet surface. The lock/response guards keep the revision
            // pending until draw_pet_frame() can actually present a frame,
            // and only a real draw may acknowledge the revisions.
            if (draw_pet_frame(true)) {
                rendered_motion_signature = pet_motion_signature();
                rendered_ambient_revision = pending_ambient_revision;
            }
        } else if (!s_display_sleeping && s_pet_motion_enabled &&
                   (s_idle_pet_visible || pet_state_is("thinking", NULL))) {
            s_pet_frame = (uint8_t)((s_pet_frame + 1u) % 8u);
            ++s_pet_motion_tick;
            uint32_t motion_signature = pet_motion_signature();
            if (motion_signature != rendered_motion_signature ||
                pending_ambient_revision != rendered_ambient_revision) {
                // Compose only when visible geometry or ambient text changed.
                // The 80 ms motion clock still advances continuously, preserving
                // natural timing while skipping duplicate full-frame work.
                if (draw_pet_frame(true)) {
                    rendered_motion_signature = motion_signature;
                    rendered_ambient_revision = pending_ambient_revision;
                }
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
                char pet_state[sizeof(s_pet_state)];
                pet_state_copy(pet_state, sizeof(pet_state));
                draw_clock_calendar(state_color(pet_state));
                xSemaphoreGiveRecursive(s_lcd_mutex);
            }
            rendered_ambient_revision = s_ambient_revision;
        }
        int64_t diagnostic_now_us = esp_timer_get_time();
        if (diagnostic_now_us >= next_speaker_check_us) {
            // Speaker idle power-down check. This task keeps its cadence even
            // while the display sleeps, so it can own the 30 s timeout. A
            // playback in progress holds s_audio_mutex for seconds; if the
            // mutex is busy the speaker is not idle anyway, so just skip.
            next_speaker_check_us = diagnostic_now_us + 1000000;
            if (s_audio_mutex && xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(20)) == pdTRUE) {
                speaker_idle_powerdown();
                xSemaphoreGive(s_audio_mutex);
            }
        }
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

// Falling edge on FUNCTION_BUTTON or the CST816 INT line wakes the
// interaction monitor from its low-power notification wait.
static void IRAM_ATTR interaction_gpio_isr(void *arg) {
    (void)arg;
    BaseType_t task_woken = pdFALSE;
    if (s_button_task) vTaskNotifyGiveFromISR(s_button_task, &task_woken);
    if (task_woken == pdTRUE) portYIELD_FROM_ISR();
}

static bool touch_read(bool *pressed, uint8_t *gesture, uint16_t *x, uint16_t *y) {
    if (pressed) *pressed = false;
    if (gesture) *gesture = 0;
    if (!s_touch) return false;

    // Read the gesture ID and finger count in one transaction. CST816 reports
    // double-click as 0x0B; using that hardware result avoids guessing whether
    // two close contacts are a controller echo or a real user double tap.
    uint8_t reg = 0x01;
    uint8_t status[6] = {0};
    if (i2c_master_transmit_receive(s_touch, &reg, 1, status, sizeof(status), 50) != ESP_OK) {
        return false;
    }
    if (gesture) *gesture = status[0];
    if (pressed) *pressed = (status[1] & 0x0Fu) != 0;
    if (x) *x = (uint16_t)(((status[2] & 0x0Fu) << 8) | status[3]);
    if (y) *y = (uint16_t)(((status[4] & 0x0Fu) << 8) | status[5]);
    return true;
}

static void draw_settings_icon(void) {
    const uint16_t surface = rgb565(18, 48, 74);
    const uint16_t ink = rgb565(221, 238, 248);
    // Keep the control inside the round safe area and below the Wi-Fi status
    // region (x=242..350, y=14..56), otherwise network updates erase it.
    const int cx = 305;
    const int cy = 82;
    fill_circle(cx, cy, 23, surface);
    for (int i = 0; i < 8; ++i) {
        const int dx[8] = {0, 10, 14, 10, 0, -10, -14, -10};
        const int dy[8] = {-14, -10, 0, 10, 14, 10, 0, -10};
        fill_circle(cx + dx[i], cy + dy[i], 4, ink);
    }
    fill_circle(cx, cy, 12, ink);
    fill_circle(cx, cy, 5, surface);
}

static int text24_width(const char *text) {
    int width = 0;
    const char *cursor = text ? text : "";
    while (*cursor) width += text24_advance(utf8_next(&cursor));
    return width > 0 ? width - 1 : 0;
}

static void draw_centered_text24(int y, const char *text, uint16_t fg, uint16_t bg) {
    draw_text24((LCD_WIDTH - text24_width(text)) / 2, y, text, fg, bg);
}

// Draws a left/right pointing triangle for the picker arrows. The CJK font
// carries no such glyphs, so the arrows are plain geometry.
static void fill_arrow(int x, int y_center, int height, bool points_right, uint16_t color) {
    int half = height / 2;
    for (int dy = -half; dy <= half; ++dy) {
        int len = half - (dy < 0 ? -dy : dy) + 1;
        if (points_right) {
            fill_rect(x, y_center + dy, x + len, y_center + dy, color);
        } else {
            fill_rect(x - len, y_center + dy, x, y_center + dy, color);
        }
    }
}

static void draw_settings_surface(void) {
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    // A foreground command owns the LCD: never paint the menu over it.
    if (s_settings_screen == SETTINGS_SCREEN_CLOSED || s_command_display_locked) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    uint16_t *frame = s_framebuffers[s_next_framebuffer];
    if (!frame) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    s_render_target = frame;
    const uint16_t bg = rgb565(8, 28, 47);
    const uint16_t fg = rgb565(241, 248, 252);
    const uint16_t muted = rgb565(159, 189, 207);
    const uint16_t surface = rgb565(20, 57, 84);
    const uint16_t accent = rgb565(70, 185, 220);
    const uint16_t danger = rgb565(214, 69, 74);
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    settings_screen_t screen = s_settings_screen;
    if (screen == SETTINGS_SCREEN_MENU) {
        draw_centered_text24(22, "设备设置", fg, bg);
        draw_text24(42, 60, "音量", muted, bg);
        char volume[12];
        snprintf(volume, sizeof(volume), "%u%%", (unsigned)s_settings_pending_volume);
        draw_text24(276, 60, volume, fg, bg);
        fill_rect(55, 96, 305, 104, surface);
        int knob_x = 55 + (int)s_settings_pending_volume * 250 / 100;
        fill_rect(55, 96, knob_x, 104, accent);
        fill_circle(knob_x, 100, 13, fg);
        // Screen-timeout row: a tap anywhere on it opens the picker page.
        fill_rect(48, 126, 312, 172, surface);
        draw_text24(60, 137, "熄屏时间", fg, surface);
        const char *timeout_label =
            s_screen_timeout_presets[screen_timeout_preset_index(
                board_port_get_screen_timeout())].label;
        draw_text24(306 - text24_width(timeout_label), 137, timeout_label, accent, surface);
        fill_rect(48, 186, 312, 232, surface);
        draw_centered_text24(197, "清除配对信息", fg, surface);
        fill_rect(48, 246, 312, 292, danger);
        draw_centered_text24(257, "清除所有配置", fg, danger);
        draw_centered_text24(316, "点此返回", muted, bg);
    } else if (screen == SETTINGS_SCREEN_TIMEOUT_PICKER) {
        draw_centered_text24(28, "熄屏时间", fg, bg);
        // Large centered choice; tapping the left/right half of the panel
        // steps through the presets, the bottom button applies and returns.
        const char *choice = s_screen_timeout_presets[s_settings_pending_timeout_idx].label;
        draw_centered_text24(150, choice, fg, bg);
        fill_arrow(64, 162, 36, false, muted);
        fill_arrow(296, 162, 36, true, muted);
        fill_rect(96, 300, 264, 346, accent);
        draw_centered_text24(311, "确定", fg, accent);
    } else {
        bool clear_all = screen == SETTINGS_SCREEN_CONFIRM_ALL;
        draw_centered_text24(48, "请确认", fg, bg);
        draw_centered_text24(102, clear_all ? "清除所有配置？" : "清除配对信息？", fg, bg);
        draw_centered_text24(145, clear_all ? "将重新进入配网" : "保留无线网络", muted, bg);
        fill_rect(46, 230, 170, 280, surface);
        fill_rect(190, 230, 314, 280, clear_all ? danger : accent);
        draw_text24(84, 242, "取消", fg, surface);
        draw_text24(228, 242, "确认", fg, clear_all ? danger : accent);
    }
    s_render_target = NULL;
    if (present_frame_sync(frame) == ESP_OK) {
        s_next_framebuffer ^= 1u;
        ++s_presented_frames;
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

static uint8_t settings_volume_from_x(uint16_t x) {
    if (x <= 55) return 0;
    if (x >= 305) return 100;
    return (uint8_t)(((uint32_t)(x - 55) * 100u + 125u) / 250u);
}

static void settings_preview_volume(uint8_t value) {
    if (value > 100) value = 100;
    if (value == s_settings_pending_volume) return;
    s_settings_pending_volume = value;
    // Codec I2C is short and independent of LCD DMA. Apply it directly so the
    // speaker follows the finger even when a full-frame settings repaint is
    // still in progress; persistence remains a release-only callback.
    (void)board_port_set_volume(value);
}

static void settings_post(uint32_t kind, uint8_t value) {
    if (!s_settings_queue) return;
    uint32_t message = kind | ((uint32_t)value << SETTINGS_TASK_VALUE_SHIFT);
    if (xQueueSend(s_settings_queue, &message, 0) == pdTRUE) return;
    // A full queue must never swallow either a destructive confirmation or
    // a final volume/timeout commit. Preview/redraw messages are disposable,
    // while losing a release-edge commit would make the shown value revert
    // after reboot. Preserve queued clears, discard one older non-clear message,
    // then retry the important request.
    // Known and accepted trade-off: with several CLEAR requests queued at
    // once the rescue can restore them in a different order. That state is
    // practically unreachable (each clear starts the setup portal).
    if (kind == SETTINGS_TASK_VOLUME_COMMIT ||
        kind == SETTINGS_TASK_TIMEOUT_CHANGED ||
        kind == SETTINGS_TASK_CLEAR_PAIRING || kind == SETTINGS_TASK_CLEAR_ALL) {
        uint32_t held[4];
        size_t held_count = 0;
        uint32_t pending = 0;
        bool made_room = false;
        while (xQueueReceive(s_settings_queue, &pending, 0) == pdTRUE) {
            uint32_t pending_kind = pending & SETTINGS_TASK_KIND_MASK;
            if (pending_kind == SETTINGS_TASK_CLEAR_PAIRING ||
                pending_kind == SETTINGS_TASK_CLEAR_ALL) {
                if (held_count < sizeof(held) / sizeof(held[0])) held[held_count++] = pending;
                continue;
            }
            // Keep scanning after the first disposable item so preserved
            // clears can be restored first and the new important request can
            // be appended last. Requeuing clears before making enough room
            // could otherwise refill all four slots and still drop commit.
            if (!made_room) {
                made_room = true;
            } else if (held_count < sizeof(held) / sizeof(held[0])) {
                held[held_count++] = pending;
            }
        }
        for (size_t i = 0; i < held_count; ++i) {
            (void)xQueueSend(s_settings_queue, &held[i], 0);
        }
        if (made_room && xQueueSend(s_settings_queue, &message, 0) == pdTRUE) return;
    }
    ESP_LOGW(TAG, "settings message dropped: kind=%lu", (unsigned long)kind);
}

static void settings_worker(void *arg) {
    (void)arg;
    while (true) {
        uint32_t message = 0;
        if (xQueueReceive(s_settings_queue, &message, portMAX_DELAY) != pdTRUE) continue;
        uint32_t kind = message & SETTINGS_TASK_KIND_MASK;
        uint8_t value = (uint8_t)(message >> SETTINGS_TASK_VALUE_SHIFT);
        if (kind == SETTINGS_TASK_VOLUME_PREVIEW || kind == SETTINGS_TASK_VOLUME_COMMIT) {
            if (board_port_get_volume() != value) (void)board_port_set_volume(value);
            s_settings_pending_volume = value;
            draw_settings_surface();
            if (kind == SETTINGS_TASK_VOLUME_COMMIT && s_on_settings) {
                s_on_settings(BOARD_SETTINGS_VOLUME_CHANGED, value, s_on_settings_arg);
            }
        } else if (kind == SETTINGS_TASK_TIMEOUT_CHANGED) {
            // The value is the preset index chosen on the picker page.
            if (value < SCREEN_TIMEOUT_PRESET_COUNT) {
                board_port_set_screen_timeout(s_screen_timeout_presets[value].seconds);
                s_settings_pending_timeout_idx = value;
            }
            s_settings_screen = SETTINGS_SCREEN_MENU;
            draw_settings_surface();
            if (s_on_settings) {
                s_on_settings(BOARD_SETTINGS_TIMEOUT_CHANGED, value, s_on_settings_arg);
            }
        } else if (kind == SETTINGS_TASK_REDRAW) {
            draw_settings_surface();
        } else if (kind == SETTINGS_TASK_CLEAR_PAIRING || kind == SETTINGS_TASK_CLEAR_ALL) {
            s_settings_screen = SETTINGS_SCREEN_CLOSED;
            s_settings_dragging = false;
            // The app callback starts the provisioning portal and can block
            // while Wi-Fi/httpd are initialized. Give the user immediate
            // feedback before leaving this display task context.
            board_port_show_text(kind == SETTINGS_TASK_CLEAR_ALL ? "配置已清除" : "配对已清除",
                                 "正在启动设置热点");
            if (s_on_settings) {
                s_on_settings(kind == SETTINGS_TASK_CLEAR_ALL ? BOARD_SETTINGS_CLEAR_ALL
                                                              : BOARD_SETTINGS_CLEAR_PAIRING,
                              0, s_on_settings_arg);
            }
        }
    }
}

static bool settings_handle_touch(bool pressed, bool coords_valid, uint16_t x, uint16_t y) {
    static bool was_pressed;
    static uint16_t last_x;
    static uint16_t last_y;
    // CST816 reports zero fingers on release and no longer guarantees a
    // valid coordinate, and a failed I2C sample carries none at all. Cache
    // positions only from successful contact samples; release-edge tap
    // actions reuse the last cached position.
    if (pressed && coords_valid) {
        last_x = x;
        last_y = y;
    } else if (!pressed && was_pressed) {
        x = last_x;
        y = last_y;
    }
    if (s_settings_screen == SETTINGS_SCREEN_CLOSED) {
        if (pressed && !was_pressed && !s_display_sleeping &&
            x >= 275 && x <= 335 && y >= 52 && y <= 112 &&
            !s_recording_active &&
            !s_setup_qrcode_visible && !s_response_active && !s_command_display_locked &&
            pet_state_is("idle", "quiet")) {
            s_settings_screen = SETTINGS_SCREEN_MENU;
            s_settings_pending_volume = board_port_get_volume();
            s_settings_last_preview_post_us = 0;
            ESP_LOGI(TAG, "settings opened: touch=(%u,%u) volume=%u%%",
                     (unsigned)x, (unsigned)y, (unsigned)s_settings_pending_volume);
            // Opening the menu is user activity: the idle-sleep budget
            // must not fire the moment a long settings session ends.
            s_idle_pet_sleep_expires_us = esp_timer_get_time() + s_screen_timeout_us;
            settings_post(SETTINGS_TASK_REDRAW, 0);
            was_pressed = true;
            return true;
        }
        was_pressed = pressed;
        return false;
    }
    if (s_settings_screen == SETTINGS_SCREEN_MENU) {
        if (pressed && y >= 82 && y <= 124) {
            s_settings_dragging = true;
            uint8_t value = settings_volume_from_x(x);
            if (value != s_settings_pending_volume) {
                settings_preview_volume(value);
                int64_t now_us = esp_timer_get_time();
                if (s_settings_last_preview_post_us == 0 ||
                    now_us - s_settings_last_preview_post_us >= 80000) {
                    s_settings_last_preview_post_us = now_us;
                    settings_post(SETTINGS_TASK_VOLUME_PREVIEW, value);
                }
            }
        } else if (!pressed && s_settings_dragging) {
            s_settings_dragging = false;
            s_settings_last_preview_post_us = 0;
            ESP_LOGI(TAG, "settings volume committed: touch=(%u,%u) value=%u%%",
                     (unsigned)x, (unsigned)y, (unsigned)s_settings_pending_volume);
            settings_post(SETTINGS_TASK_VOLUME_COMMIT, s_settings_pending_volume);
        } else if (!pressed && was_pressed) {
            if (y >= 126 && y <= 176) {
                s_settings_pending_timeout_idx =
                    screen_timeout_preset_index(board_port_get_screen_timeout());
                s_settings_screen = SETTINGS_SCREEN_TIMEOUT_PICKER;
                ESP_LOGI(TAG, "screen timeout picker opened: touch=(%u,%u) preset=%u",
                         (unsigned)x, (unsigned)y, (unsigned)s_settings_pending_timeout_idx);
                settings_post(SETTINGS_TASK_REDRAW, 0);
            } else if (y >= 182 && y <= 236) {
                s_settings_screen = SETTINGS_SCREEN_CONFIRM_PAIRING;
                ESP_LOGI(TAG, "settings pairing-clear confirmation opened: touch=(%u,%u)",
                         (unsigned)x, (unsigned)y);
                settings_post(SETTINGS_TASK_REDRAW, 0);
            } else if (y >= 242 && y <= 296) {
                s_settings_screen = SETTINGS_SCREEN_CONFIRM_ALL;
                ESP_LOGI(TAG, "settings full-clear confirmation opened: touch=(%u,%u)",
                         (unsigned)x, (unsigned)y);
                settings_post(SETTINGS_TASK_REDRAW, 0);
            } else if (y >= 302) {
                s_settings_screen = SETTINGS_SCREEN_CLOSED;
                ESP_LOGI(TAG, "settings closed: touch=(%u,%u)",
                         (unsigned)x, (unsigned)y);
                // Returning to the pet surface counts as activity too; the
                // budget armed before the menu opened has long expired.
                s_idle_pet_sleep_expires_us = esp_timer_get_time() + s_screen_timeout_us;
                if (s_on_settings) s_on_settings(BOARD_SETTINGS_CLOSED, 0, s_on_settings_arg);
                draw_pet();
            }
        }
    } else if (s_settings_screen == SETTINGS_SCREEN_TIMEOUT_PICKER) {
        if (!pressed && was_pressed) {
            if (y >= 292) {
                // Apply the highlighted preset and return to the menu.
                ESP_LOGI(TAG, "screen timeout committed: preset=%u (%lu s)",
                         (unsigned)s_settings_pending_timeout_idx,
                         (unsigned long)s_screen_timeout_presets[s_settings_pending_timeout_idx].seconds);
                settings_post(SETTINGS_TASK_TIMEOUT_CHANGED, s_settings_pending_timeout_idx);
            } else if (y >= 110 && y <= 214) {
                // Left half steps back, right half steps forward, wrapping at
                // both ends of the preset list.
                if (x < LCD_WIDTH / 2) {
                    s_settings_pending_timeout_idx = (uint8_t)(
                        (s_settings_pending_timeout_idx + SCREEN_TIMEOUT_PRESET_COUNT - 1u) %
                        SCREEN_TIMEOUT_PRESET_COUNT);
                } else {
                    s_settings_pending_timeout_idx = (uint8_t)(
                        (s_settings_pending_timeout_idx + 1u) % SCREEN_TIMEOUT_PRESET_COUNT);
                }
                settings_post(SETTINGS_TASK_REDRAW, 0);
            }
        }
    } else if (!pressed && was_pressed) {
        // Match the drawn buttons exactly (see draw_settings_surface):
        // cancel at x 46..170, confirm at x 190..314, both at y 230..280.
        if (y >= 230 && y <= 280 && x >= 46 && x <= 170) {
            s_settings_screen = SETTINGS_SCREEN_MENU;
            ESP_LOGI(TAG, "settings confirmation cancelled: touch=(%u,%u)",
                     (unsigned)x, (unsigned)y);
            settings_post(SETTINGS_TASK_REDRAW, 0);
        } else if (y >= 230 && y <= 280 && x >= 190 && x <= 314) {
            ESP_LOGI(TAG, "settings confirmation accepted: touch=(%u,%u) action=%s",
                     (unsigned)x, (unsigned)y,
                     s_settings_screen == SETTINGS_SCREEN_CONFIRM_ALL ? "clear-all" : "clear-pairing");
            settings_post(s_settings_screen == SETTINGS_SCREEN_CONFIRM_ALL
                              ? SETTINGS_TASK_CLEAR_ALL : SETTINGS_TASK_CLEAR_PAIRING, 0);
        }
    }
    was_pressed = pressed;
    return true;
}
// A tap (short or double) on a slept panel only wakes the screen; it must not
// start a voice command or a meeting recording on a dark display. Re-arms the
// idle budget when a timeout is configured. Arm before publishing visibility:
// the animation task on the other core checks visible && expired, and a zero
// expiry would re-sleep the panel in the gap between the two writes. Returns
// true when the gesture was consumed as a wake.
static bool consume_gesture_as_wake_if_sleeping(int64_t now_us) {
    if (!s_display_sleeping) return false;
    board_port_wake_from_idle();
    if (s_screen_timeout_us > 0) {
        s_idle_pet_sleep_expires_us = now_us + s_screen_timeout_us;
    }
    s_idle_pet_visible = true;
    return true;
}

static void button_task(void *arg) {
    (void)arg;
    // EchoEar-2ST exposes BOOT on ESP-IDF GPIO0. The separately labelled
    // PWR/FUNCTION key in Zephyr's gpio1 bank is the board power-control key
    // and does not provide a dependable application GPIO while running.
    // Treat a panel tap as the normal interaction gesture as well, matching
    // the vendor user guide; both inputs feed the same short/double/long logic.
    bool button_pressed = gpio_get_level(FUNCTION_BUTTON) == 0;
    bool panel_pressed = false;
    touch_read(&panel_pressed, NULL, NULL, NULL);
    bool pressed = button_pressed || panel_pressed;
    bool gesture_from_touch = panel_pressed;
    int64_t pressed_at_us = pressed ? esp_timer_get_time() : 0;
    int64_t released_at_us = 0;
    bool long_sent = false;
    bool short_pending = false;
    bool native_double_sent = false;
    uint32_t command_gesture_revision = s_command_gesture_revision;
    // Last time any contact or gesture state was in flight; after
    // TOUCH_IDLE_TIMEOUT_US of real quiet the loop parks on the GPIO
    // interrupt notification between 500 ms keep-alive passes.
    int64_t last_activity_us = esp_timer_get_time();
    ESP_LOGI(TAG, "interaction monitor ready: boot_gpio=%d idle_level=%d touch=%s irq=%d",
             FUNCTION_BUTTON, gpio_get_level(FUNCTION_BUTTON), s_touch ? "yes" : "no", TOUCH_IRQ);
    // Tri-state touch sampling: an I2C read failure must keep the last known
    // panel level (touch_read() reports pressed=false on failure, which used
    // to look exactly like "finger released"), and the debounced level only
    // flips after two consecutive agreeing good samples. Three samples used
    // to swallow real 30-45 ms taps; two still reject a single bus glitch.
    bool filtered_panel_pressed = panel_pressed;
    // Previous tick's debounced panel level; lets the settings handler
    // see release edges while the menu is closed (see the call below).
    bool prev_panel_pressed = panel_pressed;
    unsigned touch_change_streak = 0;
    while (true) {
        bool now_button_pressed = gpio_get_level(FUNCTION_BUTTON) == 0;
        uint8_t now_touch_gesture = 0;
        uint16_t touch_x = 0, touch_y = 0;
        bool sample_panel_pressed = filtered_panel_pressed;
        bool touch_sample_ok = touch_read(&sample_panel_pressed, &now_touch_gesture, &touch_x, &touch_y);
        if (touch_sample_ok) {
            if (sample_panel_pressed == filtered_panel_pressed) {
                touch_change_streak = 0;
            } else if (++touch_change_streak >= 2) {
                filtered_panel_pressed = sample_panel_pressed;
                touch_change_streak = 0;
            }
        }
        bool now_panel_pressed = filtered_panel_pressed;
        // The release edge (now == false, prev == true) must reach the
        // handler even with the menu closed; otherwise a plain tap
        // latches the handler's was_pressed and deadlocks the gear entry.
        if (now_panel_pressed || prev_panel_pressed ||
            s_settings_screen != SETTINGS_SCREEN_CLOSED || s_settings_dragging) {
            // During the first release sample the two-sample debounce still
            // reports the filtered panel as pressed, but CST816 already says
            // zero fingers and its coordinate bytes are undefined (commonly
            // 0,0).  Do not let that sample overwrite the last real contact:
            // settings_handle_touch() needs it for release-edge buttons and
            // confirmation actions.
            bool touch_coords_valid = touch_sample_ok && sample_panel_pressed;
            if (settings_handle_touch(now_panel_pressed, touch_coords_valid, touch_x, touch_y)) {
                prev_panel_pressed = now_panel_pressed;
                pressed = now_button_pressed;
                short_pending = false;
                native_double_sent = false;
                vTaskDelay(pdMS_TO_TICKS(15));
                continue;
            }
        }
        // Track the level for the next tick so the release edge reaches
        // the settings handler even while the menu is closed. This feeds
        // only settings_handle_touch(); the gesture machine below still
        // works on the current debounced level.
        prev_panel_pressed = now_panel_pressed;
        bool now_pressed = now_button_pressed || now_panel_pressed;
        int64_t now_us = esp_timer_get_time();
        // Any live contact or in-flight gesture state keeps the fast 15 ms
        // sampling cadence; only real quiet arms the low-power wait below.
        if (now_pressed || pressed || short_pending || s_touch_gesture_consumed) {
            last_activity_us = now_us;
        }
        if (command_gesture_revision != s_command_gesture_revision) {
            // Entering the thinking phase starts a completely fresh gesture
            // window. In particular, discard the tap that originally started
            // command recording; otherwise it can be mistaken for the first
            // half of a later cancel double tap.
            command_gesture_revision = s_command_gesture_revision;
            pressed = now_pressed;
            gesture_from_touch = now_panel_pressed;
            pressed_at_us = now_pressed ? now_us : 0;
            released_at_us = 0;
            long_sent = false;
            short_pending = false;
            native_double_sent = false;
            s_touch_gesture_consumed = false;
            s_touch_gesture_released_at_us = 0;
            ESP_LOGI(TAG, "fresh command-cancel gesture window armed");
            vTaskDelay(pdMS_TO_TICKS(15));
            continue;
        }
        if (s_touch_gesture_consumed) {
            short_pending = false;
            native_double_sent = true;
            if (now_panel_pressed) {
                s_touch_gesture_released_at_us = 0;
            } else if (s_touch_gesture_released_at_us == 0) {
                s_touch_gesture_released_at_us = now_us;
            } else if (now_us - s_touch_gesture_released_at_us >= 250000) {
                s_touch_gesture_consumed = false;
                s_touch_gesture_released_at_us = 0;
                native_double_sent = false;
                ESP_LOGD(TAG, "touch gesture drain complete");
            }
        }
        // CST816's native 0x0B is the most reliable double-tap indication.
        // Accept it in standby as well as during command cancellation. A stale
        // value is gated by a fresh pressed edge plus native_double_sent, so it
        // cannot be replayed after the contact has drained.
        if (now_panel_pressed && now_touch_gesture == 0x0B && !native_double_sent) {
            short_pending = false;
            native_double_sent = true;
            s_touch_gesture_consumed = true;
            s_touch_gesture_released_at_us = 0;
            ESP_LOGI(TAG, "button gesture: double (CST816)");
            if (!consume_gesture_as_wake_if_sleeping(now_us) && s_on_button) {
                s_on_button(BOARD_BUTTON_DOUBLE, s_on_press_arg);
            }
        }
        if (now_pressed != pressed) {
            vTaskDelay(pdMS_TO_TICKS(25));
            now_button_pressed = gpio_get_level(FUNCTION_BUTTON) == 0;
            // This confirmation read is failure-aware as well: only a
            // successful sample may rewrite the panel level here, and an
            // accepted edge realigns the tri-state filter above.
            if (touch_read(&sample_panel_pressed, &now_touch_gesture, &touch_x, &touch_y)) {
                now_panel_pressed = sample_panel_pressed;
                filtered_panel_pressed = sample_panel_pressed;
                touch_change_streak = 0;
            }
            now_pressed = now_button_pressed || now_panel_pressed;
            if (now_pressed != pressed) {
                // Use the time after the contact has passed debounce. The old
                // pre-delay timestamp shortened every touch by about 25 ms,
                // causing quick but valid taps to be discarded as <30 ms.
                now_us = esp_timer_get_time();
                pressed = now_pressed;
                if (pressed) {
                    pressed_at_us = now_us;
                    long_sent = false;
                    gesture_from_touch = now_panel_pressed;
                    if (now_touch_gesture != 0x0B) native_double_sent = false;
                    ESP_LOGI(TAG, "button/touch down");
                } else {
                    uint32_t held_ms = pressed_at_us > 0
                                           ? (uint32_t)((now_us - pressed_at_us) / 1000)
                                           : 0;
                    ESP_LOGI(TAG, "button/touch up: held=%lu ms", (unsigned long)held_ms);
                    uint32_t minimum_tap_ms =
                        (gesture_from_touch && s_command_cancel_enabled) ? 15 : 30;
                    if (!long_sent && held_ms >= minimum_tap_ms) {
                        int64_t since_previous_us = now_us - released_at_us;
                        int64_t double_window_us =
                            (gesture_from_touch && s_command_cancel_enabled) ? 1300000 : 650000;
                        if (!native_double_sent && short_pending &&
                            ((!gesture_from_touch && since_previous_us <= double_window_us) ||
                             (gesture_from_touch && since_previous_us >= 100000 &&
                              since_previous_us <= double_window_us))) {
                            short_pending = false;
                            native_double_sent = true;
                            if (gesture_from_touch) {
                                s_touch_gesture_consumed = true;
                                s_touch_gesture_released_at_us = 0;
                            }
                            ESP_LOGI(TAG, "button gesture: double (%s timing gap=%lld ms)",
                                     gesture_from_touch ? "touch" : "button",
                                     (long long)(since_previous_us / 1000));
                            if (!consume_gesture_as_wake_if_sleeping(now_us) && s_on_button) {
                                s_on_button(BOARD_BUTTON_DOUBLE, s_on_press_arg);
                            }
                        } else if (gesture_from_touch && s_command_cancel_enabled &&
                                   short_pending && since_previous_us < 100000) {
                            // CST816 can report a second contact about 100 ms
                            // after one physical tap. Keep the original release
                            // timestamp so that echo is neither a cancel nor a
                            // new first tap in the double-tap window.
                            ESP_LOGD(TAG, "ignored CST816 duplicate contact: gap=%lld ms",
                                     (long long)(since_previous_us / 1000));
                        } else if (!native_double_sent && !s_touch_gesture_consumed) {
                            // A CST816 touch double is emitted above from its
                            // native 0x0B gesture. Multiple raw contacts here
                            // still belong to one tap, so keep only one pending
                            // short event instead of promoting an echo to double.
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
        int64_t pending_window_us = s_command_cancel_enabled ? 1300000 : 650000;
        if (!pressed && short_pending && !s_touch_gesture_consumed &&
            now_us - released_at_us > pending_window_us) {
            short_pending = false;
            if (consume_gesture_as_wake_if_sleeping(now_us)) {
                ESP_LOGI(TAG, "button gesture: wake only (display slept)");
            } else {
                ESP_LOGI(TAG, "button gesture: short");
                if (s_on_button) s_on_button(BOARD_BUTTON_SHORT, s_on_press_arg);
            }
        }
        // Low-power idle: with no gesture state in flight for
        // TOUCH_IDLE_TIMEOUT_US, park on the GPIO interrupt notification
        // instead of polling the shared I2C touch bus every 15 ms. The
        // bounded 500 ms timeout keeps the gesture state machine advancing
        // and recovers from any interrupt edge that was ever missed.
        if (now_us - last_activity_us >= TOUCH_IDLE_TIMEOUT_US) {
            ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(500));
        } else {
            vTaskDelay(pdMS_TO_TICKS(15));
        }
    }
}
void board_port_set_settings_callback(board_port_settings_cb_t callback, void *arg) {
    s_on_settings = callback;
    s_on_settings_arg = arg;
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

    ledc_timer_config_t backlight_timer = {
        .speed_mode = BACKLIGHT_LEDC_MODE,
        .duty_resolution = LEDC_TIMER_10_BIT,
        .timer_num = BACKLIGHT_LEDC_TIMER,
        .freq_hz = BACKLIGHT_LEDC_FREQ_HZ,
        .clk_cfg = LEDC_AUTO_CLK,
    };
    ESP_ERROR_CHECK(ledc_timer_config(&backlight_timer));
    ledc_channel_config_t backlight_channel = {
        .gpio_num = LCD_BACKLIGHT,
        .speed_mode = BACKLIGHT_LEDC_MODE,
        .channel = BACKLIGHT_LEDC_CHANNEL,
        .intr_type = LEDC_INTR_DISABLE,
        .timer_sel = BACKLIGHT_LEDC_TIMER,
        .duty = 0,
        .hpoint = 0,
    };
    ESP_ERROR_CHECK(ledc_channel_config(&backlight_channel));
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
    board_port_set_backlight(s_backlight_percent);

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

    // Both interaction inputs get a falling-edge interrupt so button_task can
    // park on a notification while idle instead of polling the shared I2C
    // touch bus every 15 ms. FUNCTION_BUTTON is active low; the CST816 pulses
    // its INT line low on touch events with its factory default trigger mode
    // (no unverified IRQ-mode registers are written to the touch controller).
    gpio_config_t button_cfg = {
        .pin_bit_mask = (1ULL << FUNCTION_BUTTON) | (1ULL << TOUCH_IRQ),
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_NEGEDGE,
    };
    ESP_ERROR_CHECK(gpio_config(&button_cfg));
    s_settings_queue = xQueueCreate(4, sizeof(uint32_t));
    if (!s_settings_queue) return ESP_ERR_NO_MEM;
    BaseType_t settings_task_created = xTaskCreate(settings_worker, "maclaw_settings", 4096, NULL, 3,
                                                  &s_settings_task);
    if (settings_task_created != pdPASS) return ESP_ERR_NO_MEM;
    BaseType_t button_task_created = xTaskCreate(button_task, "echoear_button", 4096, NULL, 4,
                                                  &s_button_task);
    if (button_task_created != pdPASS) {
        ESP_LOGE(TAG, "cannot start button task");
        return ESP_ERR_NO_MEM;
    }
    esp_err_t isr_err = gpio_install_isr_service(ESP_INTR_FLAG_IRAM);
    if (isr_err != ESP_OK && isr_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "GPIO ISR service unavailable: %s", esp_err_to_name(isr_err));
    } else {
        ESP_ERROR_CHECK_WITHOUT_ABORT(gpio_isr_handler_add(FUNCTION_BUTTON, interaction_gpio_isr, NULL));
        ESP_ERROR_CHECK_WITHOUT_ABORT(gpio_isr_handler_add(TOUCH_IRQ, interaction_gpio_isr, NULL));
    }
#if CONFIG_MACLAW_POWER_SAVE
    // DFS between 80 and 240 MHz plus tickless idle. This only takes effect
    // when CONFIG_PM_ENABLE=y and CONFIG_FREERTOS_USE_TICKLESS_IDLE=y are set
    // in sdkconfig (see the notes in sdkconfig.defaults). Light sleep stays
    // off: the AFE feed task must run continuously for wake-word AEC.
    esp_pm_config_t pm_config = {
        .max_freq_mhz = 240,
        .min_freq_mhz = 80,
        .light_sleep_enable = false,
    };
    esp_err_t pm_err = esp_pm_configure(&pm_config);
    if (pm_err == ESP_OK) {
        (void)esp_pm_lock_create(ESP_PM_APB_FREQ_MAX, 0, "lcd_dma", &s_lcd_apb_lock);
    } else {
        ESP_LOGW(TAG, "power management unavailable: %s (CONFIG_PM_ENABLE not set?)",
                 esp_err_to_name(pm_err));
    }
#endif
    ESP_LOGI(TAG, "EchoEar-2ST ST77916 QSPI display and function button ready");
    return ESP_OK;
}

void board_port_set_pet_state(const char *state) {
    const char *next_state = state ? state : "idle";
    // The provisioning screen is intentionally pixel-stable for phone cameras.
    // A delayed state message must not clear this guard and let a pet frame
    // overwrite the QR code halfway through scanning.
    if (s_setup_qrcode_visible || s_settings_screen != SETTINGS_SCREEN_CLOSED) {
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
    pet_state_store(next_state);
    // Idle/quiet are the permanent ambient pet face. Previously every state
    // update cleared s_idle_pet_visible, so the animation task stopped owning
    // refreshes and both pet motion and clock seconds could freeze indefinitely.
    if (!strcmp(next_state, "idle") || !strcmp(next_state, "quiet")) {
        // Clearing the flag alone leaves a slept panel dark: run the full
        // SLPOUT/DISPON/backlight sequence when the display actually slept.
        if (s_display_sleeping) {
            s_display_sleeping = false;
            display_exit_sleep(true);
        }
        s_idle_pet_visible = true;
        s_idle_pet_sleep_expires_us = esp_timer_get_time() + s_screen_timeout_us;
    } else {
        s_idle_pet_visible = false;
        s_idle_pet_sleep_expires_us = 0;
    }
    if (!s_recording_active) draw_pet();
}

void board_port_set_command_display_lock(bool locked) {
    s_command_display_locked = locked;
    if (locked) {
        s_ready_prompt_expires_us = 0;
    } else if (s_pet_profile_dirty) {
        // A pet profile update arrived while the foreground command owned the
        // LCD and only recorded its values. Repaint now that the lock lifts.
        s_pet_profile_dirty = false;
        if (!s_recording_active) draw_pet();
    }
}

void board_port_set_command_cancel_enabled(bool enabled) {
    if (enabled && !s_command_cancel_enabled) {
        ++s_command_gesture_revision;
    }
    s_command_cancel_enabled = enabled;
}

void board_port_set_pet_profile(const char *skin, bool motion_enabled) {
	const char *next_skin = (skin && skin[0]) ? skin : s_pet_skin;
	char normalized_skin[sizeof(s_pet_skin)];
	strlcpy(normalized_skin, next_skin, sizeof(normalized_skin));
	bool skin_changed = false;
	bool motion_changed = false;
	taskENTER_CRITICAL(&s_state_lock);
	skin_changed = strcmp(s_pet_skin, normalized_skin) != 0;
	motion_changed = s_pet_motion_enabled != motion_enabled;
	if (skin_changed) {
		strlcpy(s_pet_skin, normalized_skin, sizeof(s_pet_skin));
		s_pet_frame = 0;
		s_pet_motion_tick = 0;
	}
	if (motion_changed) s_pet_motion_enabled = motion_enabled;
	if (skin_changed || motion_changed) {
		// Record every profile change even while a foreground command owns
		// the LCD: the revision lets the animation task reconcile later and
		// the dirty flag forces a repaint when the display lock lifts.
		++s_pet_profile_revision;
		if (s_command_display_locked) s_pet_profile_dirty = true;
	}
	taskEXIT_CRITICAL(&s_state_lock);
	if (!skin_changed && !motion_changed) {
		ESP_LOGD(TAG, "pet profile unchanged: skin=%s", normalized_skin);
		return;
	}
	// The GUI motion flag is authoritative. When disabled the pet renders a
	// static frame (no blink, no tail sway) and the animation task keeps only
	// the ambient clock path alive instead of playing motion frames.
	ESP_LOGI(TAG, "pet profile: skin=%s motion=%s%s",
			 normalized_skin, motion_enabled ? "enabled" : "disabled",
			 s_command_display_locked ? " (deferred while display locked)" : "");
	// During recording the direct draw is skipped; the animation task still
	// repaints from the revision difference once the recording surface ends.
	if (s_command_display_locked || s_recording_active) return;
	draw_pet();
}

esp_err_t board_port_set_pet_asset(const uint8_t *frames, size_t frame_count,
								   uint16_t width, uint16_t height,
								   uint32_t frame_interval_ms,
								   const char *revision) {
	if (!frames || frame_count == 0) {
		if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) {
			return ESP_ERR_TIMEOUT;
		}
		free(s_pet_asset_frames);
		s_pet_asset_frames = NULL;
		s_pet_asset_width = s_pet_asset_height = 0;
		s_pet_asset_frame_count = 0;
		s_pet_asset_revision[0] = '\0';
		++s_pet_profile_revision;
		if (!s_command_display_locked && !s_recording_active) draw_pet();
		if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
		return ESP_OK;
	}
	if (width < 32 || width > PET_ASSET_MAX_WIDTH || height < 32 ||
	    height > PET_ASSET_MAX_HEIGHT || frame_count > PET_ASSET_MAX_FRAMES) {
		return ESP_ERR_INVALID_ARG;
	}
	size_t frame_bytes = (size_t)width * height * 2;
	size_t total_bytes = frame_bytes * frame_count;
	uint8_t *copy = heap_caps_malloc(total_bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
	if (!copy) return ESP_ERR_NO_MEM;
	memcpy(copy, frames, total_bytes);
	if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) {
		free(copy);
		return ESP_ERR_TIMEOUT;
	}
	if (revision && s_pet_asset_frames && !strcmp(s_pet_asset_revision, revision)) {
		free(copy);
		if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
		return ESP_OK;
	}
	uint8_t *old = s_pet_asset_frames;
	s_pet_asset_frames = copy;
	s_pet_asset_width = width;
	s_pet_asset_height = height;
	s_pet_asset_frame_count = (uint8_t)frame_count;
	s_pet_asset_frame_interval_ms = frame_interval_ms >= 100 ? frame_interval_ms : 700;
	strlcpy(s_pet_asset_revision, revision ? revision : "", sizeof(s_pet_asset_revision));
	free(old);
	++s_pet_profile_revision;
	ESP_LOGI(TAG, "GUI pet asset installed: %ux%u frames=%u revision=%s",
			 width, height, (unsigned)frame_count, s_pet_asset_revision);
	if (!s_command_display_locked && !s_recording_active) draw_pet();
	if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
	return ESP_OK;
}

bool board_port_has_pet_asset_revision(const char *revision) {
	if (!revision || !revision[0]) return false;
	bool matches = false;
	if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, pdMS_TO_TICKS(1000)) != pdTRUE) {
		return false;
	}
	matches = s_pet_asset_frames && s_pet_asset_frame_count > 0 &&
		!strcmp(s_pet_asset_revision, revision);
	if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
	return matches;
}

void board_port_set_recording_visual(bool active, bool paused, uint32_t elapsed_seconds) {
    if (active) s_ready_prompt_expires_us = 0;
    if (active) {
        // A wake word can start recording while the settings menu is open.
        // The recording surface owns the LCD from here on; close the menu
        // so no stale settings frame or drag state survives the switch.
        s_settings_screen = SETTINGS_SCREEN_CLOSED;
        s_settings_dragging = false;
        s_idle_pet_visible = false;
        s_idle_pet_sleep_expires_us = 0;
    }
    s_recording_active = active;
    s_recording_paused = paused;
    s_recording_elapsed_seconds = elapsed_seconds;
    ++s_recording_visual_revision;
    if (!active || elapsed_seconds == 0) {
        taskENTER_CRITICAL(&s_state_lock);
        memset(s_recording_wave_min, 0, sizeof(s_recording_wave_min));
        memset(s_recording_wave_max, 0, sizeof(s_recording_wave_max));
        s_recording_wave_pending_min = INT16_MAX;
        s_recording_wave_pending_max = INT16_MIN;
        s_recording_wave_pending_samples = 0;
        s_recording_wave_pending_abs_sum = 0;
        s_recording_wave_pending_usable = 0;
        s_recording_wave_pending_clipped = 0;
        s_recording_wave_dc = 0;
        taskEXIT_CRITICAL(&s_state_lock);
    }
    if (!active) s_recording_audio_level = 0;
    if (active) {
        draw_recording_visual();
    } else if (!s_command_display_locked && !s_response_active) {
        // Finishing a command recording is a foreground transition, not a
        // return to the ambient pet.  The caller has already selected the
        // thinking/result/error surface; repainting a pet here queues one old
        // full frame between the waveform and that surface, which resembles a
        // brief boot/standby screen.  Only restore the pet when no foreground
        // command owns the display.
        draw_pet();
    }
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
    for (size_t i = 0; i < count; ++i) {
        int16_t sample = samples[i];
        int32_t raw_magnitude = sample < 0 ? -(int32_t)sample : sample;
        // Values this close to full scale are transport/clock artefacts on
        // EchoEar's ES7210 path far more often than voice. Do not let them
        // dominate the visual signal. The original PCM is still retained.
        if (raw_magnitude >= 32500) {
            ++s_recording_wave_pending_clipped;
        } else {
            // A 1/64 low-pass baseline follows the analogue DC level but is
            // far too slow to follow speech. This makes silence a thin line
            // and spoken sound the visible changing envelope.
            s_recording_wave_dc += ((int32_t)sample - s_recording_wave_dc) / 64;
            int32_t deviation = (int32_t)sample - s_recording_wave_dc;
            uint32_t magnitude = deviation < 0 ? (uint32_t)-deviation : (uint32_t)deviation;
            s_recording_wave_pending_abs_sum += (uint32_t)magnitude;
            ++s_recording_wave_pending_usable;
        }
        ++s_recording_wave_pending_samples;
        if (s_recording_wave_pending_samples >= RECORDING_WAVE_SAMPLES_PER_COLUMN) {
            if (completed < sizeof(completed_min) / sizeof(completed_min[0])) {
                // Discard a predominantly clipped bucket. Otherwise use a
                // gated mean absolute amplitude, which is naturally smoother
                // than min/max yet still follows consonants and speech rhythm.
                uint32_t mean = s_recording_wave_pending_usable
                                    ? s_recording_wave_pending_abs_sum /
                                          s_recording_wave_pending_usable
                                    : 0;
                if (s_recording_wave_pending_clipped >
                        RECORDING_WAVE_SAMPLES_PER_COLUMN / 4 ||
                    mean <= 180) mean = 0;
                if (mean > 9000) mean = 9000;
                completed_min[completed] = -(int16_t)mean;
                completed_max[completed] = (int16_t)mean;
                ++completed;
            }
            s_recording_wave_pending_min = INT16_MAX;
            s_recording_wave_pending_max = INT16_MIN;
            s_recording_wave_pending_samples = 0;
            s_recording_wave_pending_abs_sum = 0;
            s_recording_wave_pending_usable = 0;
            s_recording_wave_pending_clipped = 0;
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
    if (s_settings_screen != SETTINGS_SCREEN_CLOSED) return;
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    char pet_state[sizeof(s_pet_state)];
    pet_state_copy(pet_state, sizeof(pet_state));
    uint16_t bg = state_color(pet_state);
    // The 24-dot CJK font is sized for the physical 360-pixel round display.
    // Keep the two lines inside the lower safe area masked by the enclosure.
    fill_rect(18, 246, 342, 326, bg);
    // Compose both 24-dot lines into the shared text buffer and submit one
    // contiguous transfer. The draw fence keeps the queued DMA source stable,
    // and the LCD mutex serializes subsequent updates.
    for (size_t i = 0; i < (size_t)STATUS_TEXT_W * STATUS_TEXT_H; ++i) {
        s_shared_text[i] = bg;
    }
    compose_text24(s_shared_text, STATUS_TEXT_W, STATUS_TEXT_W, 0,
                   title ? title : "码卡龙", rgb565(255, 255, 255));
    compose_text24(s_shared_text, STATUS_TEXT_W, STATUS_TEXT_W, STATUS_TEXT_GAP,
                   text ? text : "", rgb565(220, 235, 255));
    ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(
        STATUS_TEXT_X, STATUS_TEXT_Y,
        STATUS_TEXT_X + STATUS_TEXT_W, STATUS_TEXT_Y + STATUS_TEXT_H,
        s_shared_text));
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "%s: %s", title ? title : "MaClaw", text ? text : "");
}

void board_port_show_upload_progress(size_t completed_bytes, size_t total_bytes,
                                     const char *stage) {
    if (s_settings_screen != SETTINGS_SCREEN_CLOSED) return;
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    // A transfer is an independent, quiet surface. It avoids the animated pet
    // DMA path while a large HTTPS upload is active and makes progress legible.
    const uint16_t bg = rgb565(9, 35, 64);
    const uint16_t fg = rgb565(244, 250, 255);
    const uint16_t muted = rgb565(174, 206, 224);
    const uint16_t track = rgb565(28, 80, 111);
    const uint16_t fill = rgb565(72, 205, 220);
    uint32_t percent = total_bytes ? (uint32_t)(completed_bytes * 100u / total_bytes) : 0;
    if (percent > 100) percent = 100;
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    draw_text24(88, 78, "会议录音", fg, bg);
    draw_text24(72, 118, stage && stage[0] ? stage : "正在上传", muted, bg);
    const int x = 42, y = 178, w = 276, h = 18;
    fill_rect(x, y, x + w, y + h, track);
    if (percent) fill_rect(x, y, x + (int)(w * percent / 100u), y + h, fill);
    char label[16];
    snprintf(label, sizeof(label), "%lu%%", (unsigned long)percent);
    draw_centered_text(222, label, fg, bg);
    char bytes[40];
    snprintf(bytes, sizeof(bytes), "%lu/%lu KB", (unsigned long)(completed_bytes / 1024u),
             (unsigned long)(total_bytes / 1024u));
    draw_centered_text(258, bytes, muted, bg);
    draw_text24(72, 302, "上传中，请勿断电", muted, bg);
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
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
    // A QR drawn on a slept panel is unscannable; wake it fully first.
    if (s_display_sleeping) {
        s_display_sleeping = false;
        display_exit_sleep(true);
    }
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
    for (size_t i = 0; i < (size_t)STATUS_TEXT_W * STATUS_TEXT_H; ++i) {
        s_shared_text[i] = page_bg;
    }
    compose_text24(s_shared_text, STATUS_TEXT_W, STATUS_TEXT_W, 0,
                   "微信扫码加入热点", rgb565(255, 255, 255));
    char instruction[40];
    snprintf(instruction, sizeof(instruction), "热点 %s", ssid ? ssid : "");
    compose_text24(s_shared_text, STATUS_TEXT_W, STATUS_TEXT_W, STATUS_TEXT_GAP,
                   instruction, rgb565(220, 235, 255));
    ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(
        STATUS_TEXT_X, STATUS_TEXT_Y,
        STATUS_TEXT_X + STATUS_TEXT_W, STATUS_TEXT_Y + STATUS_TEXT_H,
        s_shared_text));
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "showing setup Wi-Fi QR for %s", ssid ? ssid : "");
}

void board_port_show_setup_portal(const char *ssid, const char *url,
                                  bool pairing_only) {
    if (!s_panel) return;
    const uint16_t bg = state_color("quiet");
    const uint16_t fg = rgb565(255, 255, 255);
    const uint16_t muted = rgb565(188, 216, 234);
    const uint16_t accent = pairing_only ? rgb565(255, 185, 78)
                                         : rgb565(72, 205, 220);

    s_ready_prompt_expires_us = 0;
    if (s_display_sleeping) {
        s_display_sleeping = false;
        display_exit_sleep(true);
    }
    s_idle_pet_visible = false;
    s_idle_pet_sleep_expires_us = 0;
    // Use the same ownership flag as the QR page: the animation, Wi-Fi and
    // ambient tasks must not paint over this recovery information.
    s_setup_qrcode_visible = true;
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    uint16_t *frame = s_framebuffers[s_next_framebuffer];
    if (!frame) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        // Stripe fallback: without a PSRAM framebuffer the full-page compose
        // is impossible, but the recovery instructions must still reach the
        // panel — returning here with s_setup_qrcode_visible set would strand
        // the user on a blank screen. show_text uses the shared stripe buffer
        // and works in this memory situation.
        board_port_show_text(pairing_only ? "设备配对设置" : "设备网络设置",
                             ssid && ssid[0] ? ssid : "MACLAW-SETUP");
        return;
    }
    s_render_target = frame;
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    fill_circle(180, 83, 38, accent);
    // Compact link/hotspot emblem, drawn geometrically so it does not depend
    // on a special font glyph.
    fill_circle(165, 83, 15, bg);
    fill_circle(195, 83, 15, bg);
    fill_rect(165, 73, 195, 93, accent);
    draw_centered_text24(137, pairing_only ? "设备配对设置" : "设备网络设置",
                         fg, bg);
    draw_centered_text24(188, "连接以下热点", muted, bg);
    draw_centered_text24(225, ssid && ssid[0] ? ssid : "MACLAW-SETUP",
                         accent, bg);
    draw_centered_text24(269, "浏览器打开", muted, bg);
    draw_centered_text24(306, url && url[0] ? url : "192.168.4.1", fg, bg);
    s_render_target = NULL;
    if (present_frame_sync(frame) == ESP_OK) {
        s_next_framebuffer ^= 1u;
        ++s_presented_frames;
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "showing %s portal: hotspot=%s url=%s",
             pairing_only ? "pairing" : "setup",
             ssid ? ssid : "", url ? url : "");
}

void board_port_set_wifi_status(const char *ssid, bool connected) {
    if (!s_panel || s_recording_active || s_setup_qrcode_visible ||
        s_settings_screen != SETTINGS_SCREEN_CLOSED || s_command_display_locked) return;
    // Wi-Fi is transport state, not command UI. Never overlay it on the
    // thinking/result states, where it resembles a transition to startup.
    if (!pet_state_is("idle", "quiet")) return;
    // This indicator is cosmetic. Never let it hold Wi-Fi startup behind a
    // full animated frame; the next status event can paint it instead.
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, pdMS_TO_TICKS(40)) != pdTRUE) {
        ESP_LOGI(TAG, "Wi-Fi indicator deferred: %s", connected ? "connected" : "connecting");
        return;
    }
    char pet_state[sizeof(s_pet_state)];
    pet_state_copy(pet_state, sizeof(pet_state));
    uint16_t bg = state_color(pet_state);
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
    if (idle_pet_visible || s_setup_qrcode_visible ||
        s_settings_screen != SETTINGS_SCREEN_CLOSED || s_command_display_locked) return;
    if ((top_changed || bottom_changed) && !display_sleeping && !recording_active &&
        ambient_visible_for_state()) {
        if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
        char pet_state[sizeof(s_pet_state)];
        pet_state_copy(pet_state, sizeof(pet_state));
        draw_clock_calendar(state_color(pet_state));
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    }
}

void board_port_show_ready_prompt(const char *title, const char *text) {
    // The ready state uses the current GUI-configured pet as its background;
    // only the brief action hint is transient.
    bool panel_in_sleep = s_display_sleeping;
    s_display_sleeping = false;
    display_exit_sleep(panel_in_sleep);
    board_port_set_pet_state("idle");
    board_port_show_text(title, text);
    s_ready_prompt_expires_us = esp_timer_get_time() + READY_PROMPT_TIMEOUT_US;
}

void board_port_cancel_ready_prompt(void) {
    s_ready_prompt_expires_us = 0;
}

bool board_port_wake_from_idle(void) {
    if (!s_display_sleeping) return false;
    // The full wake sequence (SLPOUT + 120 ms + backlight fade, ~250 ms) is
    // too slow for the touch scan task this runs on; flag it and let
    // pet_animation_task execute display_exit_sleep(). Later draws serialize
    // behind that sequence through s_lcd_mutex.
    s_display_sleeping = false;
    s_display_wake_pending = true;
    s_idle_pet_visible = false;
    s_idle_pet_sleep_expires_us = 0;
    return true;
}

esp_err_t board_port_audio_stream_start(void) {
    board_port_pause_wake_word(true);
    // MultiNet may already be inside a 250 ms I2S read when pause is asserted,
    // and foreground network/UI work can delay its mutex release. Allow a
    // bounded settling window so a valid double tap does not intermittently
    // fail before capture has even started.
    if (!s_audio_mutex || xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(5000)) != pdTRUE) {
        ESP_LOGE(TAG, "meeting microphone mutex timeout");
        board_port_pause_wake_word(false);
        return ESP_ERR_TIMEOUT;
    }
    esp_err_t err = audio_init();
    if (err != ESP_OK) {
        xSemaphoreGive(s_audio_mutex);
        board_port_pause_wake_word(false);
    } else {
        // Restore the non-clipping analogue gain used by standby recognition.
        // Software suppression is deliberately bypassed here.
        for (uint8_t reg = 0x43; reg <= 0x46; ++reg) {
            err = es7210_write(reg, 0x1A);
            if (err != ESP_OK) break;
        }
        if (err != ESP_OK) {
            xSemaphoreGive(s_audio_mutex);
            board_port_pause_wake_word(false);
        } else {
            record_stats_reset();
            audio_rx_flush_stale();
        }
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
    if (err != ESP_OK) {
        // A flash erase can block the writer for hundreds of ms; the overrun
        // is counted so the meeting summary can report dropped audio instead
        // of shipping a WAV with silent holes.
        ++s_record_overrun_count;
        return err;
    }
    if (received < sizeof(stereo)) ++s_record_short_read_count;
    size_t frames = received / (sizeof(int16_t) * 2);
    if (frames > sample_capacity) frames = sample_capacity;
    int32_t chunk_peak = 0;
    for (size_t i = 0; i < frames; ++i) {
        // The supplied recording proves that the left slot is currently
        // corrupted: most of its words sit near +/-32768 and the old
        // per-sample "louder channel" selector therefore chose it almost all
        // the time, burying speech under full-scale digital noise. Keep the
        // clean MIC2/right slot for a stable mono meeting recording.
        int16_t sample = record_hpf(stereo[i * 2 + 1]);
        mono[i] = sample;
        int32_t magnitude = sample < 0 ? -(int32_t)sample : (int32_t)sample;
        if (magnitude > chunk_peak) chunk_peak = magnitude;
    }
    // Sliding-window normalisation: follow the recent peak with decay so a
    // quiet speaker is lifted toward -3 dBFS without per-chunk pumping.
    if (chunk_peak > s_stream_agc_peak) {
        s_stream_agc_peak = chunk_peak;
    } else {
        s_stream_agc_peak = (s_stream_agc_peak * 31 + chunk_peak) / 32;
    }
    record_apply_gain(mono, frames, record_agc_gain_q15(s_stream_agc_peak));
    uint32_t scaled = chunk_peak <= 180 ? 0 : (uint32_t)(chunk_peak - 180) * 1000u / (12000u - 180u);
    if (scaled > 1000) scaled = 1000;
    if (level) *level = (uint16_t)scaled;
    recording_wave_push_pcm(mono, frames);
    *samples_read = frames;
    return ESP_OK;
}

void board_port_audio_stream_stop(void) {
    // Keep I2S enabled for the normal six-second command path. DMA frames
    // accumulated while a meeting was paused are discarded by the flush in
    // board_port_audio_stream_start()/board_port_capture_wav().
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
    ++s_recording_visual_revision;
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
    if (!s_response_active || s_recording_active || s_setup_qrcode_visible ||
        s_settings_screen != SETTINGS_SCREEN_CLOSED) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    char pet_state[sizeof(s_pet_state)];
    pet_state_copy(pet_state, sizeof(pet_state));
    uint16_t bg = state_color(pet_state);
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
    if (s_settings_screen != SETTINGS_SCREEN_CLOSED) return;
    s_ready_prompt_expires_us = 0;
    // Enter the result state without calling board_port_set_pet_state(). That
    // public setter paints a complete pet frame immediately; doing so just
    // before this page produced a visible boot/idle-looking flash between the
    // thinking screen and every streamed response message.
    pet_state_store("speaking");
    s_idle_pet_visible = false;
    s_idle_pet_sleep_expires_us = 0;
    // The answer must also reach a panel that went through idle sleep.
    if (s_display_sleeping) {
        s_display_sleeping = false;
        display_exit_sleep(true);
    }
    s_response_active = true;
    s_response_page = 0;
    strlcpy(s_response_title, title && title[0] ? title : "码卡龙", sizeof(s_response_title));
    strlcpy(s_response_text, text && text[0] ? text : "没有收到文字回复", sizeof(s_response_text));
    s_response_next_page_us = esp_timer_get_time() + RESPONSE_PAGE_INTERVAL_US;
    draw_response_page();
    ESP_LOGI(TAG, "response: %s", s_response_text);
}

#if CONFIG_MACLAW_AFE_ENABLE
// ---------------- esp-sr AFE (AEC + NS + AGC) for the wake-word path --------
//
// Data flow: afe_feed_task (core 0) reads the ES7210 stereo stream, picks the
// healthy right capsule as the microphone, pairs it with the playback
// reference from s_aec_ref_buf (zeros when the speaker is silent) and feeds
// the interleaved [mic, ref] frames to the AFE. wake_word_task (core 1) calls
// fetch() for the processed mono PCM and chunks it into MultiNet. If any
// initialisation step fails, the AFE is torn down and the raw I2S path below
// keeps the wake word working.

// Overrun (producer more than a ring ahead) drops the oldest samples so the
// reference stays time-aligned with what the speaker plays now; underrun
// zero-fills, which matches actual silence.
static void aec_ref_write(const int16_t *samples, size_t count) {
    if (!s_aec_ref_buf || !samples || !count) return;
    taskENTER_CRITICAL(&s_aec_ref_lock);
    for (size_t i = 0; i < count; ++i) {
        s_aec_ref_buf[s_aec_ref_wr % AFE_AEC_REF_SAMPLES] = samples[i];
        ++s_aec_ref_wr;
    }
    if (s_aec_ref_wr - s_aec_ref_rd > AFE_AEC_REF_SAMPLES) {
        s_aec_ref_rd = s_aec_ref_wr - AFE_AEC_REF_SAMPLES;
    }
    taskEXIT_CRITICAL(&s_aec_ref_lock);
}

static void aec_ref_read(int16_t *out, size_t count) {
    size_t available;
    taskENTER_CRITICAL(&s_aec_ref_lock);
    uint32_t depth = s_aec_ref_wr - s_aec_ref_rd;
    if (depth > count) {
        // Consumer fell behind: skip the stale tail so the reference stays
        // aligned with the samples currently leaving the speaker.
        s_aec_ref_rd += depth - (uint32_t)count;
        depth = (uint32_t)count;
    }
    available = depth;
    for (size_t i = 0; i < available; ++i) {
        out[i] = s_aec_ref_buf[(s_aec_ref_rd + i) % AFE_AEC_REF_SAMPLES];
    }
    s_aec_ref_rd += (uint32_t)available;
    taskEXIT_CRITICAL(&s_aec_ref_lock);
    for (size_t i = available; i < count; ++i) out[i] = 0;
}

static void aec_ref_reset(void) {
    taskENTER_CRITICAL(&s_aec_ref_lock);
    s_aec_ref_rd = s_aec_ref_wr;
    taskEXIT_CRITICAL(&s_aec_ref_lock);
}

static void afe_feed_task(void *arg) {
    (void)arg;
    const int feed_chunk = s_afe_iface->get_feed_chunksize(s_afe_data);
    const int feed_channels = s_afe_iface->get_feed_channel_num(s_afe_data);
    int16_t *stereo = heap_caps_malloc((size_t)feed_chunk * 2 * sizeof(int16_t),
                                       MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    int16_t *frame = heap_caps_malloc((size_t)feed_chunk * feed_channels * sizeof(int16_t),
                                      MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    int16_t *ref = heap_caps_malloc((size_t)feed_chunk * sizeof(int16_t),
                                    MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!stereo || !frame || !ref) {
        ESP_LOGE(TAG, "AFE feed task aborted: no memory for %d-sample buffers", feed_chunk);
        goto finish;
    }
    bool mic_was_paused = false;
    while (!s_afe_feed_stop) {
        if (s_wake_word_paused) {
            // The capture/meeting paths own the microphone while paused; the
            // AFE input ring just drains and is reset on resume.
            mic_was_paused = true;
            vTaskDelay(pdMS_TO_TICKS(20));
            continue;
        }
        if (mic_was_paused) {
            mic_was_paused = false;
            audio_rx_flush_stale();
            aec_ref_reset();
        }
        // Never take s_audio_mutex here: playback holds it for whole seconds
        // and the AEC must keep hearing the microphone while the speaker
        // plays. RX and TX do not contend; the capture paths exclude this
        // task through s_wake_word_paused instead.
        size_t received = 0;
        esp_err_t read_err = i2s_channel_read(s_audio_rx, stereo,
                                              (size_t)feed_chunk * 2 * sizeof(int16_t),
                                              &received, pdMS_TO_TICKS(100));
        if (read_err != ESP_OK && read_err != ESP_ERR_TIMEOUT) {
            ESP_LOGW(TAG, "AFE feed microphone read failed: %s", esp_err_to_name(read_err));
            vTaskDelay(pdMS_TO_TICKS(10));
            continue;
        }
        if (s_wake_word_paused) {
            // A capture may have claimed the microphone while this read was
            // blocked; discard the chunk instead of feeding it to the AFE so
            // the recording keeps its opening samples uncontested.
            continue;
        }
        size_t frames = received / (sizeof(int16_t) * 2);
        if (frames < (size_t)feed_chunk) {
            memset(stereo + frames * 2, 0,
                   ((size_t)feed_chunk - frames) * 2 * sizeof(int16_t));
        }
        aec_ref_read(ref, (size_t)feed_chunk);
        for (int i = 0; i < feed_chunk; ++i) {
            // The left capsule is electrically damaged (full-scale garbage),
            // so the healthy right slot is the AFE microphone, with the same
            // per-sample fallback arbitration as the raw wake-word path.
            int16_t right = stereo[i * 2 + 1];
            int16_t left = stereo[i * 2];
            int32_t right_abs = right < 0 ? -(int32_t)right : right;
            int32_t left_abs = left < 0 ? -(int32_t)left : left;
            int16_t mic = right_abs < WAKE_WORD_INVALID_SAMPLE_ABS ? right
                          : left_abs < WAKE_WORD_INVALID_SAMPLE_ABS ? left : 0;
            frame[i * feed_channels] = mic;
            frame[i * feed_channels + 1] = ref[i];
        }
        s_afe_iface->feed(s_afe_data, frame);
    }
finish:
    heap_caps_free(stereo);
    heap_caps_free(frame);
    heap_caps_free(ref);
    s_afe_feed_task = NULL;
    vTaskDelete(NULL);
}

// Tears down whatever afe_wake_start() managed to create. Null-safe, and only
// ever called from the wake-word task context.
static void afe_wake_stop(void) {
    if (s_afe_feed_task) {
        s_afe_feed_stop = true;
        for (unsigned i = 0; i < 100 && s_afe_feed_task; ++i) {
            vTaskDelay(pdMS_TO_TICKS(10));
        }
        if (s_afe_feed_task) ESP_LOGW(TAG, "AFE feed task did not exit in time");
    }
    if (s_afe_iface && s_afe_data) s_afe_iface->destroy(s_afe_data);
    s_afe_data = NULL;
    s_afe_iface = NULL;
    if (s_afe_config) afe_config_free(s_afe_config);
    s_afe_config = NULL;
    heap_caps_free(s_aec_ref_buf);
    s_aec_ref_buf = NULL;
    s_aec_ref_wr = 0;
    s_aec_ref_rd = 0;
}

static bool afe_wake_start(srmodel_list_t *models) {
    // 1 microphone + 1 playback reference ("MR", reference last).
    s_afe_config = afe_config_init("MR", models, AFE_TYPE_SR, AFE_MODE_LOW_COST);
    if (!s_afe_config) {
        ESP_LOGE(TAG, "AFE disabled: afe_config_init failed");
        return false;
    }
    // AEC/NS/AGC only. VAD stays off (no vadn model in the srmodels
    // partition) and WakeNet stays off: MultiNet keeps running in the wake
    // task on the fetched mono PCM.
    s_afe_config->aec_init = true;
    s_afe_config->ns_init = true;
    s_afe_config->afe_ns_mode = AFE_NS_MODE_WEBRTC;
    s_afe_config->vad_init = false;
    s_afe_config->wakenet_init = false;
    s_afe_config->agc_init = true;
    s_afe_config->agc_mode = AFE_AGC_MODE_WEBRTC;
    s_afe_config->afe_linear_gain = 1.0f;
    s_afe_config->debug_init = false;
    s_afe_config->afe_perferred_core = 0;
    s_afe_config->afe_perferred_priority = AFE_FEED_TASK_PRIORITY;
    s_afe_config->memory_alloc_mode = AFE_MEMORY_ALLOC_INTERNAL_PSRAM_BALANCE;
    afe_config_print(s_afe_config);

    s_afe_iface = esp_afe_handle_from_config(s_afe_config);
    if (!s_afe_iface) {
        ESP_LOGE(TAG, "AFE disabled: no AFE_SR interface for this configuration");
        goto fail;
    }
    s_afe_data = s_afe_iface->create_from_config(s_afe_config);
    if (!s_afe_data) {
        ESP_LOGE(TAG, "AFE disabled: instance creation failed (out of memory)");
        goto fail;
    }
    const int feed_chunk = s_afe_iface->get_feed_chunksize(s_afe_data);
    const int feed_channels = s_afe_iface->get_feed_channel_num(s_afe_data);
    const int fetch_chunk = s_afe_iface->get_fetch_chunksize(s_afe_data);
    if (feed_chunk <= 0 || fetch_chunk <= 0 || feed_channels != 2 ||
        s_afe_iface->get_samp_rate(s_afe_data) != AUDIO_RATE) {
        ESP_LOGE(TAG, "AFE disabled: unexpected format %d Hz, feed=%d samples x %d ch, fetch=%d",
                 s_afe_iface->get_samp_rate(s_afe_data), feed_chunk, feed_channels, fetch_chunk);
        goto fail;
    }
    s_aec_ref_buf = heap_caps_malloc(AFE_AEC_REF_SAMPLES * sizeof(int16_t),
                                     MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!s_aec_ref_buf) {
        ESP_LOGE(TAG, "AFE disabled: no PSRAM for the AEC reference ring");
        goto fail;
    }
    s_aec_ref_wr = 0;
    s_aec_ref_rd = 0;
    s_afe_feed_stop = false;
    TaskHandle_t task = NULL;
    if (xTaskCreatePinnedToCore(afe_feed_task, "maclaw_afe_feed", AFE_FEED_TASK_STACK,
                                NULL, AFE_FEED_TASK_PRIORITY, &task, 0) != pdPASS) {
        ESP_LOGE(TAG, "AFE disabled: cannot create the feed task");
        goto fail;
    }
    s_afe_feed_task = task;
    if (s_afe_iface->print_pipeline) s_afe_iface->print_pipeline(s_afe_data);
    ESP_LOGI(TAG, "AFE running: AEC+NS+AGC, feed=%d samples x %d ch, fetch=%d samples",
             feed_chunk, feed_channels, fetch_chunk);
    return true;
fail:
    afe_wake_stop();
    return false;
}
#endif // CONFIG_MACLAW_AFE_ENABLE

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
#if CONFIG_MACLAW_AFE_ENABLE
    const bool afe_started = afe_wake_start(models);
    // fetch() returns one AFE frame of mono PCM at a time while MultiNet
    // consumes chunk_samples per detect(), so stage frames until a full
    // MultiNet chunk exists.
    int16_t *afe_stage = NULL;
    int afe_staged = 0;
    if (afe_started) {
        afe_stage = heap_caps_malloc((size_t)chunk_samples * sizeof(int16_t),
                                     MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
        if (!afe_stage) {
            ESP_LOGE(TAG, "AFE staging buffer allocation failed; using raw microphone path");
            afe_wake_stop();
        }
    }
    const bool use_afe = afe_started && afe_stage != NULL;
#endif
    bool model_was_paused = false;
    int64_t last_detection_us = 0;
    int64_t last_audio_diagnostic_us = 0;
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
        if (model_was_paused) {
            model_was_paused = false;
#if CONFIG_MACLAW_AFE_ENABLE
            // Drop whatever the capture/meeting paths left in the AFE pipes.
            if (use_afe) {
                afe_staged = 0;
                s_afe_iface->reset_buffer(s_afe_data);
            }
#endif
        }
#if CONFIG_MACLAW_AFE_ENABLE
        if (use_afe) {
            afe_fetch_result_t *fetch =
                s_afe_iface->fetch_with_delay(s_afe_data, pdMS_TO_TICKS(200));
            if (s_wake_word_stop_requested) break;
            if (!fetch || fetch->ret_value != ESP_OK || !fetch->data || fetch->data_size <= 0) {
                continue;
            }
            const int16_t *pcm = fetch->data;
            int pending = fetch->data_size / (int)sizeof(int16_t);
            while (pending > 0) {
                int n = chunk_samples - afe_staged;
                if (n > pending) n = pending;
                memcpy(afe_stage + afe_staged, pcm, (size_t)n * sizeof(int16_t));
                afe_staged += n;
                pcm += n;
                pending -= n;
                if (afe_staged < chunk_samples) continue;
                afe_staged = 0;
                esp_mn_state_t state = multinet->detect(model_data, afe_stage);
                // Same watchdog yield as the raw path below.
                vTaskDelay(1);
                if (state == ESP_MN_STATE_TIMEOUT) { multinet->clean(model_data); continue; }
                if (state != ESP_MN_STATE_DETECTED) continue;
                esp_mn_results_t *result = multinet->get_results(model_data);
                if (!result || result->num == 0 || result->command_id[0] != WAKE_WORD_COMMAND_ID) continue;
                if (s_playback_active) {
                    // Barge-in groundwork: the AEC keeps the wake word
                    // audible through the speaker output, but a detection
                    // during playback is logged, never reported.
                    ESP_LOGI(TAG, "wake word detected during playback (suppressed, prob=%.3f)",
                             (double)result->prob[0]);
                    multinet->clean(model_data);
                    continue;
                }
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
            continue;
        }
#endif
        // Raw fallback path. Playback cannot be barge-in monitored without
        // the AEC reference, so the flag simply mutes listening here.
        if (s_playback_active) {
            vTaskDelay(pdMS_TO_TICKS(20));
            continue;
        }
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
        int32_t left_peak = 0;
        int32_t right_peak = 0;
        uint32_t left_energy = 0;
        uint32_t right_energy = 0;
        uint16_t left_invalid = 0;
        uint16_t right_invalid = 0;
        for (int i = 0; i < chunk_samples; ++i) {
            int32_t left = stereo[i * 2];
            int32_t right = stereo[i * 2 + 1];
            int32_t left_abs = left < 0 ? -left : left;
            int32_t right_abs = right < 0 ? -right : right;
            if (left_abs > left_peak) left_peak = left_abs;
            if (right_abs > right_peak) right_peak = right_abs;
            // A full-scale word is a bus artefact, not usable speech.  Do not
            // let one damaged slot suppress the physical microphone that is
            // actually closest to the speaker.  For two valid microphones,
            // averaging provides a stable, phase-neutral mono input.
            bool left_ok = left_abs < WAKE_WORD_INVALID_SAMPLE_ABS;
            bool right_ok = right_abs < WAKE_WORD_INVALID_SAMPLE_ABS;
            if (left_ok) left_energy += (uint32_t)left_abs;
            else ++left_invalid;
            if (right_ok) right_energy += (uint32_t)right_abs;
            else ++right_invalid;
            if (left_ok && right_ok) mono[i] = (int16_t)((left + right) / 2);
            else if (left_ok) mono[i] = (int16_t)left;
            else if (right_ok) mono[i] = (int16_t)right;
            else mono[i] = 0;
        }

        int64_t diagnostic_now_us = esp_timer_get_time();
        if (diagnostic_now_us - last_audio_diagnostic_us >= 1000000) {
            last_audio_diagnostic_us = diagnostic_now_us;
            ESP_LOGI(TAG,
                     "offline wake mic: L peak=%ld mean=%lu bad=%u; R peak=%ld mean=%lu bad=%u; mix=valid",
                     (long)left_peak, (unsigned long)(left_energy / chunk_samples), left_invalid,
                     (long)right_peak, (unsigned long)(right_energy / chunk_samples), right_invalid);
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
#if CONFIG_MACLAW_AFE_ENABLE
    heap_caps_free(afe_stage);
    afe_wake_stop();
#endif
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
    record_stats_reset();
    audio_rx_flush_stale();
	s_short_capture_stop_requested = false;
	s_short_capture_accepts_stop = true;

    const size_t mono_samples = AUDIO_RATE * AUDIO_SECONDS;
    const size_t wav_len = 44 + mono_samples * sizeof(int16_t);
    // Prefer SPIRAM for the 192 KB WAV buffer and keep internal RAM for
    // TLS/Wi-Fi, same policy as the HTTP bodies; fall back if SPIRAM is full.
    uint8_t *wav = heap_caps_malloc(wav_len, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!wav) wav = heap_caps_malloc(wav_len, MALLOC_CAP_8BIT);
    if (!wav) {
		s_short_capture_accepts_stop = false;
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
    unsigned consecutive_errors = 0;
    while (written_samples < mono_samples && !s_short_capture_stop_requested) {
        size_t received = 0;
        esp_err_t err = i2s_channel_read(s_audio_rx, stereo, sizeof(stereo), &received, pdMS_TO_TICKS(1000));
        if (err != ESP_OK) {
            // One DMA/bus timeout must not discard the seconds already
            // captured: count the overrun and keep recording. Only a run of
            // consecutive failures means the microphone is really gone.
            ++s_record_overrun_count;
            if (++consecutive_errors >= 5) {
                ESP_LOGE(TAG, "capture aborted after %u consecutive read failures",
                         consecutive_errors);
                free(wav);
				s_short_capture_accepts_stop = false;
                xSemaphoreGive(s_audio_mutex);
                board_port_pause_wake_word(false);
                return err;
            }
            continue;
        }
        consecutive_errors = 0;
        if (received < sizeof(stereo)) ++s_record_short_read_count;
        size_t frames = received / (sizeof(int16_t) * 2);
        if (frames > mono_samples - written_samples) frames = mono_samples - written_samples;
        int32_t chunk_peak = 0;
        // The left capsule on this board is electrically damaged and reads
        // near-full-scale digital garbage (see board_port_audio_stream_read),
        // so the old "pick the louder channel" selector almost always picked
        // the noise. Default to the healthy right channel and fall back to
        // the left one only for individually saturated samples, the same
        // per-sample arbitration the wake-word path uses.
        unsigned right_saturated = 0;
        for (size_t i = 0; i < frames; ++i) {
            int16_t right = stereo[i * 2 + 1];
            int32_t right_abs = right < 0 ? -(int32_t)right : (int32_t)right;
            int16_t raw = right;
            if (right_abs >= WAKE_WORD_INVALID_SAMPLE_ABS) {
                ++right_saturated;
                // The left slot may be saturated garbage as well; fall back to
                // silence for that sample, like the AFE feed arbitration.
                int16_t left = stereo[i * 2];
                int32_t left_abs = left < 0 ? -(int32_t)left : (int32_t)left;
                raw = left_abs < WAKE_WORD_INVALID_SAMPLE_ABS ? left : 0;
            }
            int16_t sample = record_hpf(raw);
            mono[written_samples++] = sample;
            int32_t magnitude = sample < 0 ? -(int32_t)sample : (int32_t)sample;
            if (magnitude > peak) peak = magnitude;
            if (magnitude > chunk_peak) chunk_peak = magnitude;
        }
        if (right_saturated) {
            ESP_LOGW(TAG, "right mic saturated (%u/%u samples); left slot used per sample",
                     right_saturated, (unsigned)frames);
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
	s_short_capture_accepts_stop = false;
	if (s_short_capture_stop_requested) {
		ESP_LOGI(TAG, "short voice capture stopped by user at %u ms",
				 (unsigned)(written_samples * 1000u / AUDIO_RATE));
	}
	if (written_samples == 0) {
		free(wav);
		xSemaphoreGive(s_audio_mutex);
		board_port_pause_wake_word(false);
		return ESP_ERR_INVALID_SIZE;
	}
	// The allocation remains at the six-second ceiling, but the media contract
	// and HTTP upload use only the samples actually captured. Patch both WAV
	// lengths so a tap-to-stop command is a complete valid file rather than a
	// six-second file with an uninitialised tail.
	uint32_t actual_data_size = (uint32_t)(written_samples * sizeof(int16_t));
	uint32_t actual_wav_len = 44u + actual_data_size;
	uint32_t actual_riff_size = actual_wav_len - 8u;
	memcpy(wav + 4, &actual_riff_size, sizeof(actual_riff_size));
	memcpy(wav + 40, &actual_data_size, sizeof(actual_data_size));
    // Normalise quiet captures toward -3 dBFS so far-field speech reaches the
    // server ASR at a usable level. Capped at 12 dB to avoid amplifying noise.
    int32_t gain_q15 = record_agc_gain_q15(peak);
    if (gain_q15 != (1 << 15)) {
        record_apply_gain(mono, mono_samples, gain_q15);
        ESP_LOGI(TAG, "capture normalised: raw peak=%ld, gain x%ld.%03ld",
                 (long)peak, (long)(gain_q15 >> 15),
                 (long)(((gain_q15 & 0x7FFF) * 1000) >> 15));
    }
    ESP_LOGI(TAG, "captured %u mono samples, peak=%ld", (unsigned)written_samples, (long)peak);
    *out_wav = wav;
    *out_len = actual_wav_len;
    xSemaphoreGive(s_audio_mutex);
    board_port_pause_wake_word(false);
    return ESP_OK;
}

bool board_port_request_capture_stop(void) {
	if (!s_short_capture_accepts_stop) return false;
	s_short_capture_stop_requested = true;
	return true;
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
#if CONFIG_MACLAW_AFE_ENABLE
    // Keep the wake-word AFE fed during playback so the AEC stays converged
    // and barge-in remains possible; the wake task suppresses detections via
    // s_playback_active. The raw fallback path treats the flag like a pause.
    s_playback_active = true;
#else
    board_port_pause_wake_word(true);
#endif
    esp_err_t err = playback_begin();
    int16_t stereo[512];
    unsigned underruns = 0;
    for (size_t offset = 0; err == ESP_OK && offset < data_len;) {
        size_t frames = (data_len - offset) / (channels * sizeof(int16_t));
        if (frames > 256) frames = 256;
        if (frames == 0) break;
#if CONFIG_MACLAW_AFE_ENABLE
        int16_t mono_ref[256];
#endif
        const int16_t *source = (const int16_t *)(data + offset);
        for (size_t i = 0; i < frames; ++i) {
            int16_t sample = source[i * channels];
            stereo[i * 2] = sample;
            stereo[i * 2 + 1] = channels == 2 ? source[i * 2 + 1] : sample;
#if CONFIG_MACLAW_AFE_ENABLE
            mono_ref[i] = sample;  // The AEC reference is the mono expansion.
#endif
        }
        // A single DMA contention timeout must not abort the reply: retry the
        // chunk a few times, continuing from the bytes already accepted, then
        // drop whatever is left and keep playing the rest.
        size_t want = frames * 2 * sizeof(int16_t);
        size_t queued = 0;
        esp_err_t write_err = ESP_OK;
        for (unsigned attempt = 0; attempt < 3 && queued < want; ++attempt) {
            size_t written = 0;
            write_err = i2s_channel_write(s_audio_tx, (const uint8_t *)stereo + queued,
                                          want - queued, &written, pdMS_TO_TICKS(1000));
            queued += written;
            if (write_err != ESP_OK) break;
            if (written % (2 * sizeof(int16_t)) != 0) {
                // IDF does not promise frame-aligned partial writes; resuming
                // mid-frame would swap the channels, so drop this chunk.
                break;
            }
        }
        if (write_err != ESP_OK || queued != want) {
            ++underruns;
            ESP_LOGW(TAG, "playback chunk dropped after retries: %s (%u/%u bytes)",
                     esp_err_to_name(write_err), (unsigned)queued, (unsigned)want);
        }
#if CONFIG_MACLAW_AFE_ENABLE
        if (queued > 0) {
            // Mirror only what actually reached the TX DMA into the AEC
            // reference ring; the reader's zero-fill matches the silence the
            // speaker really produced for the dropped remainder.
            aec_ref_write(mono_ref, queued / (2 * sizeof(int16_t)));
        }
#endif
        offset += frames * channels * sizeof(int16_t);
    }
    if (underruns) ESP_LOGW(TAG, "playback underruns: %u chunk(s) dropped", underruns);
    if (err == ESP_OK) {
        playback_end();
    } else {
        // playback_begin() failed before the TX path came up: writing the
        // trailing silence pad would fail again and the drain delay only
        // stalls the caller, so just make sure the PA is off.
        (void)gpio_set_level(AUDIO_PA_ENABLE, 0);
    }
#if CONFIG_MACLAW_AFE_ENABLE
    s_playback_active = false;
#else
    board_port_pause_wake_word(false);
#endif
    xSemaphoreGive(s_audio_mutex);
    return err;
}

esp_err_t board_port_play_ack_chime(void) {
    if (!s_audio_mutex || xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(1500)) != pdTRUE) return ESP_ERR_TIMEOUT;
#if CONFIG_MACLAW_AFE_ENABLE
    s_playback_active = true;
#else
    board_port_pause_wake_word(true);
#endif
    esp_err_t err = playback_begin();
    int16_t stereo[512];
    // A short two-note acknowledgement: distinct enough to hear, soft enough
    // not to be confused with an alarm. The waveform is generated locally so
    // the board can confirm receipt before network TTS is available.
    for (int note = 0; err == ESP_OK && note < 2; ++note) {
        const int half_period = note == 0 ? 20 : 15; // 400 Hz, then ~533 Hz.
        for (int frame = 0; frame < AUDIO_RATE / 7; frame += 256) {
            int frames = (AUDIO_RATE / 7 - frame) > 256 ? 256 : (AUDIO_RATE / 7 - frame);
#if CONFIG_MACLAW_AFE_ENABLE
            int16_t mono_ref[256];
#endif
            for (int i = 0; i < frames; ++i) {
                int phase = (frame + i) % (half_period * 2);
                int16_t sample = phase < half_period ? 2600 : -2600;
                stereo[i * 2] = sample;
                stereo[i * 2 + 1] = sample;
#if CONFIG_MACLAW_AFE_ENABLE
                mono_ref[i] = sample;
#endif
            }
            size_t written = 0;
            err = i2s_channel_write(s_audio_tx, stereo, frames * 2 * sizeof(int16_t),
                                    &written, pdMS_TO_TICKS(1000));
            if (written != (size_t)frames * 2 * sizeof(int16_t) && err == ESP_OK) err = ESP_ERR_TIMEOUT;
#if CONFIG_MACLAW_AFE_ENABLE
            if (written > 0) {
                // Mirror only the samples that actually reached the TX DMA.
                aec_ref_write(mono_ref, written / (2 * sizeof(int16_t)));
            }
#endif
        }
    }
    if (err == ESP_OK) {
        playback_end();
    } else {
        // See board_port_play_wav(): no pad/drain on a failed begin/write.
        (void)gpio_set_level(AUDIO_PA_ENABLE, 0);
    }
#if CONFIG_MACLAW_AFE_ENABLE
    s_playback_active = false;
#else
    board_port_pause_wake_word(false);
#endif
    xSemaphoreGive(s_audio_mutex);
    return err;
}
