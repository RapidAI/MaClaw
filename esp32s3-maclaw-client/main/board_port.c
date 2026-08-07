#include "board_port.h"

#include <ctype.h>
#include <inttypes.h>
#include <math.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "driver/gpio.h"
#include "driver/i2c_master.h"
#include "driver/i2s_std.h"
#include "driver/i2s_tdm.h"
#include "driver/spi_master.h"
#include "esp_heap_caps.h"
#include "esp_check.h"
#include "esp_mn_models.h"
#include "esp_mn_speech_commands.h"
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"
#include "esp_lcd_io_i2c.h"
#include "esp_lcd_st77916.h"
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
#include "esp_lcd_co5300.h"
#include "esp_lcd_touch.h"
#include "esp_lcd_touch_cst9217.h"
#endif
#include "esp_log.h"
#include "esp_timer.h"
#include "mbedtls/base64.h"
#include "model_path.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#if !CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
#include "echoear_st77916_init.h"
#endif
#include "font_cjk24.h"

// EchoEar-2ST board definition. GPIO values are the physical GPIO numbers;
// the original Zephyr board file uses gpio1 offsets for pins 40/41/48.
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
// Waveshare ESP32-S3 Touch AMOLED 1.75C.  Keep its pins and panel protocol
// local to the adapter: domain code sees only Device API scenes/input/audio.
#define LCD_HOST        SPI2_HOST
#define LCD_WIDTH       466
#define LCD_HEIGHT      466
#define LCD_SCLK        GPIO_NUM_38
#define LCD_DATA0       GPIO_NUM_4
#define LCD_DATA1       GPIO_NUM_5
#define LCD_DATA2       GPIO_NUM_6
#define LCD_DATA3       GPIO_NUM_7
#define LCD_CS          GPIO_NUM_12
#define LCD_RESET       GPIO_NUM_1
#define LCD_BACKLIGHT   GPIO_NUM_NC
#define LCD_QSPI_PCLK_HZ (40 * 1000 * 1000)
#define FUNCTION_BUTTON GPIO_NUM_0
#define TOUCH_IRQ       GPIO_NUM_11
#define CST8XX_ADDRESS  0x15
#define AUDIO_I2C_SCL   GPIO_NUM_14
#define AUDIO_I2C_SDA   GPIO_NUM_15
#define AUDIO_MCLK      GPIO_NUM_16
#define AUDIO_BCLK      GPIO_NUM_9
#define AUDIO_WS        GPIO_NUM_45
#define AUDIO_DOUT      GPIO_NUM_8
#define AUDIO_DIN       GPIO_NUM_10
#define AUDIO_PA_ENABLE GPIO_NUM_46
#define ES7210_ADDRESS  0x40
#define ES8311_ADDRESS  0x18
#define ES8311_DAC_MUTE_REG 0x31
#define ES8311_DAC_VOLUME_REG 0x32
#define OUTPUT_VOLUME_DEFAULT 70
/* MaClaw Device API transports normalized 16 kHz signed PCM.  Keep the first
 * integration at that contract even though the vendor demo uses 24 kHz; a
 * sample-rate converter belongs in a future audio adapter, not in business
 * capture/playback paths. */
#define AUDIO_RATE      16000
#define WAVESHARE_AMOLED 1
#else
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
#define LCD_QSPI_PCLK_HZ (20 * 1000 * 1000)
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
#define ES8311_DAC_MUTE_REG 0x31
#define ES8311_DAC_VOLUME_REG 0x32
#define OUTPUT_VOLUME_DEFAULT 70
#define AUDIO_RATE      16000
#define WAVESHARE_AMOLED 0
#endif
/* ES8311's 16 kHz coefficient set is derived for a 256fs master clock.
 * Keep the hardware clock relation stated here because the microphone and
 * DAC share the same I2S controller on EchoEar-2ST:
 *   MCLK = 16,000 * 256 = 4.096 MHz
 *   DAC BCLK = 16,000 * 2 channels * 32-bit slots = 1.024 MHz
 *
 * PCM samples remain 16-bit.  The ES8311 coefficient's BCLK divider, however,
 * is for 32-bit stereo slots (64 BCLKs per LRCK), not packed 16-bit slots.
 */
#define ECHOEAR_AUDIO_MCLK_MULTIPLE I2S_MCLK_MULTIPLE_256
// End a command after a natural pause instead of a fixed recording window.
// Thirty seconds leaves room for multi-step requests while keeping the maximum
// 16 kHz PCM WAV allocation below 1 MiB on the current in-memory upload path.
#define COMMAND_CAPTURE_MAX_SECONDS 30
#define COMMAND_CAPTURE_START_TIMEOUT_MS 6000
#define COMMAND_CAPTURE_SILENCE_MS 1200
#define COMMAND_CAPTURE_START_CONFIRM_MS 80
#define COMMAND_CAPTURE_START_LEVEL 55
#define COMMAND_CAPTURE_SILENCE_FLOOR 20
#define COMMAND_CAPTURE_SILENCE_MARGIN 15
#define COMMAND_CAPTURE_SILENCE_CEILING 90
#define COMMAND_CAPTURE_PREROLL_MS 300
#define WAKE_WORD_COMMAND_ID 1
// Keep one recognizer running in standby. Running Chinese and English models
// together doubles inference work and makes normal-speed wake-up unreliable.
#define WAKE_WORD_CN_LABEL "码卡龙"
// MultiNet7 Chinese commands use space-separated pinyin syllables without
// tone digits.  Supplying tone digits makes its command validator reject the
// phrase, which silently disabled standby wake-up altogether.
// Spaces are token boundaries, not required pauses. Connected delivery often
// voices the middle consonant or reduces its vowel, so all common paths map to
// the same command ID and trigger exactly the same interaction.
static const char *const s_wake_word_cn_phonetics[] = {
    "ma ka long",
    "ma ga long",
    "ma ke long",
};
// The default command threshold favours deliberate, slow commands.  A modest
// reduction preserves a practical false-wake margin while accepting normal
// conversational delivery of the short product name.
#define WAKE_WORD_DETECTION_THRESHOLD 0.24f
#define WAKE_WORD_COOLDOWN_US (2LL * 1000 * 1000)
#define WAKE_WORD_DIAGNOSTIC_INTERVAL_US (2LL * 1000 * 1000)
#define WAKE_WORD_TARGET_RMS 3400
#define WAKE_WORD_MIN_SOFTWARE_GAIN_Q8 256
#define WAKE_WORD_MAX_SOFTWARE_GAIN_Q8 (24 * 256)
#define WAKE_WORD_GAIN_ATTACK_SHIFT 1
#define WAKE_WORD_GAIN_RELEASE_SHIFT 4
#define WAKE_WORD_GAIN_UPDATE_FLOOR 96
#define ECHOEAR_MIC_SLOT_COUNT 4
#define ECHOEAR_MIC_SELECTED_SLOT 0
// Keep every foreground path on the same physical slot as the verified wake
// recognizer. Slots 0 and 2 carry microphones on EchoEar-2ST; slots 1 and 3
// are empty in the ES7210 four-slot stream. Slot 0 is the tested clean source.
#define RECORDING_INVALID_SAMPLE_ABS 32500
#define RECORDING_DC_BLOCKER_Q15 32604  // 0.995
#define RECORDING_TARGET_RMS 3400
#define RECORDING_MIN_SOFTWARE_GAIN_Q8 256
#define RECORDING_MAX_SOFTWARE_GAIN_Q8 (24 * 256)
#define RECORDING_GAIN_ATTACK_SHIFT 1
#define RECORDING_GAIN_RELEASE_SHIFT 4
#define RECORDING_GAIN_UPDATE_FLOOR 96
#define RECORDING_OUTPUT_LIMIT 30000

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
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
/* 466px round viewport. Keep all shared scenes inside the real circular
 * aperture; this is a layout descriptor, not a business/UI branch. */
#define RESPONSE_TITLE_Y 48
#define RESPONSE_RULE_Y  100
#define RESPONSE_TEXT_X 76
#define RESPONSE_TEXT_Y 126
#define RESPONSE_TEXT_W (LCD_WIDTH - RESPONSE_TEXT_X * 2)
#define RESPONSE_LINE_GAP 34
#define RESPONSE_LINES_PER_PAGE 6
#define RESPONSE_FOOTER_Y 364
#define AMBIENT_TOP_W   LCD_WIDTH
#define AMBIENT_TOP_H   162
#define AMBIENT_BOTTOM_W 398
#define AMBIENT_BOTTOM_H 108
#define AMBIENT_BOTTOM_X ((LCD_WIDTH - AMBIENT_BOTTOM_W) / 2)
#define AMBIENT_BOTTOM_Y 346
#define AMBIENT_TOP_RING_RADIUS 312
#define AMBIENT_RING_RADIUS 196
#define PET_HALO_CENTER_Y 226
// Native pet art remains expressed in the 360px design grid.  The renderer
// maps this radius into the 466px safe zone and applies its deliberate 7/8
// standby scale, yielding a 120px halo instead of accidentally over-scaling
// a 466px value a second time.
#define PET_HALO_RADIUS   106
#else
#define RESPONSE_TITLE_Y 36
#define RESPONSE_RULE_Y  78
// EchoEar has a 360x360 framebuffer behind a circular panel.  Rectangular
// screen margins are not enough near the rim: text placed at x=32 or in the
// bottom 66 pixels is physically masked by the enclosure.  Keep the whole
// response reading column inside a conservative inscribed safe area instead.
#define RESPONSE_TEXT_X 60
#define RESPONSE_TEXT_Y 102
#define RESPONSE_TEXT_W (LCD_WIDTH - RESPONSE_TEXT_X * 2)
#define RESPONSE_LINE_GAP 30
#define RESPONSE_LINES_PER_PAGE 5
#define RESPONSE_FOOTER_Y 278
#define AMBIENT_TOP_W   360
#define AMBIENT_TOP_H   128
#define AMBIENT_BOTTOM_W 316
#define AMBIENT_BOTTOM_H 90
#define AMBIENT_BOTTOM_X 22
#define AMBIENT_BOTTOM_Y 268
#define AMBIENT_TOP_RING_RADIUS 240
#define AMBIENT_RING_RADIUS 150
#define PET_HALO_CENTER_Y 175
#define PET_HALO_RADIUS   106
#endif
#define AMBIENT_OVERLAY_PIXELS ((size_t)AMBIENT_TOP_W * (size_t)AMBIENT_TOP_H)
#define AMBIENT_OVERLAY_BYTES (AMBIENT_OVERLAY_PIXELS * sizeof(uint16_t))
#define RESPONSE_TEXT_CAPACITY 768
// Use a slightly tighter common circle for the two information bands.  With
// the overlay origins below, its centre lands at y=169 on the 360px panel for
// both arcs, so the date/time and weather no longer read as two unrelated,
// almost-flat curves.  The taller transparent overlays leave room for the
// visibly rounder ends without clipping glyphs.
// The upper status string can contain date, weekday, time, service state and
// alarm marker together. A 150px radius dropped its first and last glyphs
// below the 80px transparent overlay, so leading zeroes (for example "08")
// and the final CJK glyph in "在线" were clipped. Keep the lower information
// ring unchanged, but give the upper status ring a gentler, fully-contained
// arc.
#define AMBIENT_TRANSPARENT_KEY 0x0001u
#define LCD_FRAMEBUFFER_PIXELS ((size_t)LCD_WIDTH * LCD_HEIGHT)
#define LCD_FRAMEBUFFER_BYTES  (LCD_FRAMEBUFFER_PIXELS * sizeof(uint16_t))
// The EchoEar's long flex cable is not reliable at the component default of
// 40 MHz QSPI: a marginal edge manifests as repeated coloured columns. Native
// procedural motion remains calm, while the pre-composed remote pet uses the
// Bread Compact 80 ms cadence and a changed-region presenter below.
#define PET_ANIMATION_FRAME_MS 150
#define REMOTE_PET_RENDER_FRAME_MS 80
// Bread Compact advances its 24-column history for each 512-sample I2S read
// (32 ms at 16 kHz), and immediately presents that change. EchoEar groups four
// native 128-sample TDM reads into the same history unit, so its round-screen
// renderer must use the same 32 ms cadence rather than the idle pet's 80 ms
// animation cadence; otherwise one frame visibly skips one or two bars.
#define RECORDING_RENDER_FRAME_MS \
    (RECORDING_WAVE_SAMPLES_PER_COLUMN * 1000u / AUDIO_RATE)
#define REMOTE_PET_DEFAULT_KEYFRAME_MS 450
#define READY_PROMPT_TIMEOUT_US (60LL * 1000 * 1000)
#define IDLE_PET_SLEEP_TIMEOUT_US (30LL * 60 * 1000 * 1000)
#define INPUT_DOUBLE_WINDOW_US  (500LL * 1000)
#define INPUT_LONG_HOLD_US      (2500LL * 1000)
// The default procedural cat is shown on the permanent circular standby
// surface before a remotely selected pet has loaded.  Keep its head noticeably
// inside the top date/status ring: the former 103px face plus 55px ears reached
// into the header and visually collided with the date.  The local fallback is
// intentionally more compact than a downloaded full-frame pet.
#define STANDBY_CAT_SCALE_NUM 7
#define STANDBY_CAT_SCALE_DEN 8

#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
static const uint8_t s_co5300_qspi_mode[] = {0x20};
static const uint8_t s_co5300_qspi_enable[] = {0x10};
static const uint8_t s_co5300_qspi_clock[] = {0xA0};
static const uint8_t s_co5300_page0[] = {0x00};
static const uint8_t s_co5300_c4[] = {0x80};
static const uint8_t s_co5300_rgb565[] = {0x55};
static const uint8_t s_co5300_te[] = {0x00};
static const uint8_t s_co5300_control[] = {0x20};
static const uint8_t s_co5300_brightness[] = {0xFF};
static const uint8_t s_co5300_cabc[] = {0xFF};
static const uint8_t s_co5300_column[] = {0x00, 0x06, 0x01, 0xD7};
static const uint8_t s_co5300_row[] = {0x00, 0x00, 0x01, 0xD1};
static const co5300_lcd_init_cmd_t s_waveshare_co5300_init_cmds[] = {
    {0xFE, s_co5300_qspi_mode, sizeof(s_co5300_qspi_mode), 0},
    {0x19, s_co5300_qspi_enable, sizeof(s_co5300_qspi_enable), 0},
    {0x1C, s_co5300_qspi_clock, sizeof(s_co5300_qspi_clock), 0},
    {0xFE, s_co5300_page0, sizeof(s_co5300_page0), 0},
    {0xC4, s_co5300_c4, sizeof(s_co5300_c4), 0},
    {0x3A, s_co5300_rgb565, sizeof(s_co5300_rgb565), 0},
    {0x35, s_co5300_te, sizeof(s_co5300_te), 0},
    {0x53, s_co5300_control, sizeof(s_co5300_control), 0},
    {0x51, s_co5300_brightness, sizeof(s_co5300_brightness), 0},
    {0x63, s_co5300_cabc, sizeof(s_co5300_cabc), 0},
    {0x2A, s_co5300_column, sizeof(s_co5300_column), 0},
    {0x2B, s_co5300_row, sizeof(s_co5300_row), 600},
    {0x11, NULL, 0, 600}, {0x29, NULL, 0, 0},
};
#endif

static const char *TAG = "maclaw_board";
static esp_lcd_panel_handle_t s_panel;
static esp_lcd_panel_io_handle_t s_panel_io;
static board_port_button_cb_t s_on_button;
static void *s_on_press_arg;
/* Input Service owns the queues consumed by this scanner's callbacks.  The
 * scanner therefore has an explicit stop/join handshake for degraded-startup
 * rollback.  It intentionally does not make the whole board adapter
 * restartable: LCD, audio and I2C resources are still boot-lifetime. */
static TaskHandle_t s_button_task;
static SemaphoreHandle_t s_button_task_stopped;
static SemaphoreHandle_t s_background_tasks_lock;
static TaskHandle_t s_pet_animation_task;
static SemaphoreHandle_t s_pet_animation_stopped;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
static volatile bool s_boot_network_window_active;
static volatile bool s_boot_network_toggle_requested;
#endif

static char s_pet_state[16] = "quiet";
static char s_pet_skin[32] = "clawmate";
static bool s_pet_motion_enabled = true;
static uint8_t s_pet_frame;
static uint32_t s_pet_motion_tick;
#define REMOTE_PET_MAX_FRAMES 8
static uint8_t *s_remote_pet_frames[REMOTE_PET_MAX_FRAMES];
static size_t s_remote_pet_frame_count;
static size_t s_remote_pet_width;
static size_t s_remote_pet_height;
static uint32_t s_remote_pet_frame_ms = REMOTE_PET_DEFAULT_KEYFRAME_MS;
// Advance this only after an actual rendered tick. Using wall-clock time lets a
// Wi-Fi/TLS or LCD delay skip across several authored poses on the next frame,
// which reads as a jump rather than smooth animation.
static uint64_t s_remote_pet_animation_elapsed_ms;
// A malformed or fully transparent desktop asset used to be treated as a
// successful remote pet.  It cleared the native pet and left the standby
// clock alone on screen, which looks exactly like the pet vanished.  Keep an
// explicit visibility gate so the native renderer remains the safe fallback.
static bool s_remote_pet_has_visible_pixels;
static bool s_recording_active;
static bool s_service_ready;
// Keep transport and authenticated-service state separate, just as the Bread
// Compact port does.  A service can still be reconnecting after Wi-Fi joined,
// and a status event can arrive while a foreground surface owns the LCD.
// Persisting both values here lets the next idle frame describe the real
// connection state instead of whichever small corner indicator happened to
// render last.
static bool s_wifi_connected;
static bool s_alarm_scheduled;
static char s_command_stage[32] = "正在处理";
static volatile bool s_command_capture_active;
static volatile bool s_command_capture_stop_requested;
static bool s_recording_paused;
static volatile uint32_t s_recording_elapsed_seconds;
// Same attack/release smoothing as Bread Compact's recorder. The same level
// history feeds both the visible waveform and MIC readout, so one spoken
// block has one consistent amplitude everywhere on the recording surface.
static uint16_t s_recording_smoothed_level;
// Keep the same 24-column, 32 ms filtered-level history that Bread Compact
// presents. EchoEar groups its native 8 ms TDM reads into the same visual
// unit, so bar progression and the visible time span match the compact board.
#define RECORDING_WAVE_COLUMNS 24
#define RECORDING_WAVE_SAMPLES_PER_COLUMN 512
static uint16_t s_recording_wave_levels[RECORDING_WAVE_COLUMNS];
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
// Image replies are rendered from transient decoded pixels and intentionally
// have no text-page backing store. Keep them distinct from paginated replies
// so a page gesture cannot redraw stale s_response_text over the image.
static bool s_response_image_active;
static bool s_alarm_visual_active;
// A short system notice is an exclusive foreground surface too.  The shared
// app model calls it MESSAGE; track it here so the idle animation cannot
// repaint a pet over network/setup/meeting notices on its next tick.
static bool s_message_active;
// The wake recognizer invokes its callback on its own task while its 10 KiB
// internal stack and MultiNet allocations are still live.  Do not try to
// create the equally sized foreground worker in that instant: there may be
// enough aggregate memory but no contiguous internal block.  Stop and release
// the recognizer first, then invoke the application callback from a small
// deferred dispatcher.
static volatile bool s_wake_callback_pending;
static TaskHandle_t s_wake_callback_task;
static unsigned s_response_page;
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
extern const uint8_t _binary_cjk24_cjk_bin_start[];
extern const uint8_t _binary_cjk24_cjk_bin_end[];
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
static uint8_t s_front_framebuffer;
static bool s_front_frame_valid;
// A valid front frame is not necessarily a recording frame: it may still be a
// pet/result/upload scene.  Keep that distinction so the first recorder frame
// always owns every pixel before later recorder frames use delta updates.
static volatile bool s_recording_frame_baseline;
// Full recording scenes are required when header/mode/timer/pause state
// changes. Audio-only updates instead patch the meter dirty region over the
// last composed scene, matching Bread Compact's per-block renderer.
static volatile bool s_recording_meter_dirty;
static uint16_t *s_render_target;
static volatile uint32_t s_presented_frames;
static bool s_pet_animation_started;
// Status text gets one immutable overlay buffer.  It is never reused before a
// synchronous transfer completes.  The 466x162 AMOLED header needs 151 KiB,
// which must live in PSRAM; the 360px EchoEar keeps its smaller DMA-safe copy
// in internal RAM.  Do not reuse s_line here: that buffer is also used by the
// full-frame stripe presenter.
static uint16_t *s_ambient_overlay;
static uint16_t s_draw_transactions;
static volatile uint32_t s_skipped_pet_frames;
static i2c_master_bus_handle_t s_audio_i2c_bus;
static i2c_master_dev_handle_t s_es7210;
static i2c_master_dev_handle_t s_es8311;
static i2c_master_dev_handle_t s_touch;
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
static i2c_master_dev_handle_t s_axp2101;
static i2c_master_dev_handle_t s_qmi8658;
static esp_lcd_panel_io_handle_t s_cst9217_io;
static esp_lcd_touch_handle_t s_cst9217_touch;
#endif
static i2s_chan_handle_t s_audio_rx;
static i2s_chan_handle_t s_audio_tx;
static bool s_audio_ready;
// The meeting stream owns the audio mutex from start to stop.  Track that
// ownership explicitly (as Bread Compact does) so an error path or duplicate
// stop cannot release a mutex held by an unrelated wake/audio operation.
static bool s_audio_stream_owned;
static TaskHandle_t s_audio_playback_owner;
static volatile bool s_audio_playback_stop_requested;
static unsigned s_output_volume = OUTPUT_VOLUME_DEFAULT;
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
static TaskHandle_t s_wake_word_task;
static volatile bool s_wake_word_task_starting;
static volatile bool s_wake_word_ready;
static board_port_wake_word_cb_t s_on_wake_word;
static void *s_on_wake_word_arg;
static volatile bool s_wake_word_paused;
static volatile bool s_wake_word_pause_acknowledged;
static volatile bool s_wake_word_stop_requested;
static portMUX_TYPE s_wake_word_lock = portMUX_INITIALIZER_UNLOCKED;

typedef struct {
    int32_t previous_input;
    int32_t previous_filtered;
    uint32_t gain_q8;
    uint32_t diagnostic_samples;
    uint32_t diagnostic_bad_samples;
    int32_t diagnostic_input_peak;
    int32_t diagnostic_output_peak;
    uint32_t diagnostic_rms;
    const char *diagnostic_label;
} recording_pcm_filter_t;
static recording_pcm_filter_t s_recording_pcm_filter;

static int32_t sample_magnitude(int32_t sample) {
    return sample < 0 ? -sample : sample;
}

static void recording_pcm_reset(const char *label) {
    memset(&s_recording_pcm_filter, 0, sizeof(s_recording_pcm_filter));
    // The verified codec stream is quiet, so begin ready for nearby speech.
    // The attack path below immediately backs this down if a block is loud.
    s_recording_pcm_filter.gain_q8 = RECORDING_MAX_SOFTWARE_GAIN_Q8;
    s_recording_pcm_filter.diagnostic_label = label;
}

// Convert the verified ES7210 slot to clean mono PCM. Full-scale bus artefacts
// are replaced before a fixed-point DC blocker. A bounded RMS AGC then restores
// the very quiet but valid EchoEar signal to the same useful range as the other
// boards. Gain falls quickly to protect loud speech, recovers slowly, and does
// not update below the measured speech floor so silence is never pumped up.
// Meeting and command recordings share this path so uploaded PCM and VAD see
// identical samples.
static int32_t recording_pcm_process(const int16_t *tdm, size_t frames,
                                     int16_t *mono) {
    recording_pcm_filter_t *filter = &s_recording_pcm_filter;
    uint64_t energy = 0;
    int32_t chunk_peak = 0;
    for (size_t i = 0; i < frames; ++i) {
        int32_t input = tdm[i * ECHOEAR_MIC_SLOT_COUNT + ECHOEAR_MIC_SELECTED_SLOT];
        int32_t input_magnitude = sample_magnitude(input);
        if (input_magnitude > filter->diagnostic_input_peak) {
            filter->diagnostic_input_peak = input_magnitude;
        }

        if (input_magnitude >= RECORDING_INVALID_SAMPLE_ABS) {
            // Holding the previous input lets the high-pass output decay
            // smoothly instead of turning one damaged word into a loud click.
            input = filter->previous_input;
            ++filter->diagnostic_bad_samples;
        }

        int32_t filtered = input - filter->previous_input +
                           (int32_t)(((int64_t)filter->previous_filtered *
                                      RECORDING_DC_BLOCKER_Q15) >> 15);
        filter->previous_input = input;
        filter->previous_filtered = filtered;

        // Keep the DC-blocked block in the caller's output area while its RMS
        // is measured; it is replaced with final scaled PCM in the second pass.
        if (filtered > INT16_MAX) filtered = INT16_MAX;
        if (filtered < INT16_MIN) filtered = INT16_MIN;
        mono[i] = (int16_t)filtered;
        energy += (uint64_t)((int64_t)filtered * filtered);
    }

    uint32_t rms = frames ? (uint32_t)sqrtf((float)(energy / frames)) : 0;
    if (rms >= RECORDING_GAIN_UPDATE_FLOOR) {
        uint32_t target_q8 = (RECORDING_TARGET_RMS * 256u) / rms;
        if (target_q8 < RECORDING_MIN_SOFTWARE_GAIN_Q8) {
            target_q8 = RECORDING_MIN_SOFTWARE_GAIN_Q8;
        }
        if (target_q8 > RECORDING_MAX_SOFTWARE_GAIN_Q8) {
            target_q8 = RECORDING_MAX_SOFTWARE_GAIN_Q8;
        }
        unsigned shift = target_q8 < filter->gain_q8
                             ? RECORDING_GAIN_ATTACK_SHIFT
                             : RECORDING_GAIN_RELEASE_SHIFT;
        filter->gain_q8 = (uint32_t)((int32_t)filter->gain_q8 +
                                     ((int32_t)target_q8 - (int32_t)filter->gain_q8) /
                                         (1 << shift));
    }
    filter->diagnostic_rms = rms;

    for (size_t i = 0; i < frames; ++i) {
        int32_t output = (int32_t)(((int64_t)mono[i] * filter->gain_q8) >> 8);
        if (output > RECORDING_OUTPUT_LIMIT) output = RECORDING_OUTPUT_LIMIT;
        if (output < -RECORDING_OUTPUT_LIMIT) output = -RECORDING_OUTPUT_LIMIT;
        mono[i] = (int16_t)output;

        int32_t output_magnitude = sample_magnitude(output);
        if (output_magnitude > chunk_peak) chunk_peak = output_magnitude;
        if (output_magnitude > filter->diagnostic_output_peak) {
            filter->diagnostic_output_peak = output_magnitude;
        }
    }

    filter->diagnostic_samples += (uint32_t)frames;
    if (filter->diagnostic_samples >= AUDIO_RATE) {
        ESP_LOGI(TAG, "%s mic: S%u peak=%ld rms=%lu bad=%lu; clean peak=%ld gain=%.2f",
                 filter->diagnostic_label ? filter->diagnostic_label : "recording",
                 ECHOEAR_MIC_SELECTED_SLOT,
                 (long)filter->diagnostic_input_peak,
                 (unsigned long)filter->diagnostic_rms,
                 (unsigned long)filter->diagnostic_bad_samples,
                 (long)filter->diagnostic_output_peak,
                 (double)filter->gain_q8 / 256.0);
        filter->diagnostic_samples = 0;
        filter->diagnostic_bad_samples = 0;
        filter->diagnostic_input_peak = 0;
        filter->diagnostic_output_peak = 0;
    }
    return chunk_peak;
}

// Peak is right for an immediate speech-start response, but one short click
// can keep a peak-only VAD alive for an entire I2S block.  Use the mean
// absolute deviation around the block's DC level for the completion decision,
// matching Bread Compact's natural-pause contract while retaining EchoEar's
// selected-slot cleanup and bounded AGC above.
static uint16_t command_capture_mean_level(const int16_t *samples, size_t count) {
    if (!samples || count == 0) return 0;
    int64_t sum = 0;
    for (size_t i = 0; i < count; ++i) sum += samples[i];
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
static esp_err_t present_pet_frame_delta_sync(const uint16_t *frame);
static bool draw_remote_pet(void);
static unsigned response_page_count(void);
static void draw_response_page(void);
static void draw_recording_visual(void);

static void emit_button_input(board_input_action_t action,
                              board_input_source_t source) {
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    // GPIO0 double-click has one meaning while the board is booting: select
    // 4G for this boot. Do not let those contacts leak into the normal voice
    // / meeting gesture handler.
    if (s_boot_network_window_active) {
        if (action == BOARD_INPUT_SECONDARY &&
            source == BOARD_INPUT_SOURCE_ACTIVATE_KEY) {
            s_boot_network_toggle_requested = true;
        }
        return;
    }
#endif
    if (s_on_button) s_on_button(action, source, s_on_press_arg);
}

// Every frame present uses the same completion fence. Keeping this in one
// helper prevents the pet and recording paths from drifting apart, and makes
// a failed submission unable to consume a completion from a later transfer.
static esp_err_t present_frame_sync(const uint16_t *frame) {
    if (!s_panel || !frame) return ESP_ERR_INVALID_ARG;
    // A non-pet full-screen surface (recording, result, setup, alarm) replaces
    // the pixels that a later standby delta would otherwise compare against.
    // draw_pet_frame() revalidates this cache only after it presents a complete
    // ambient pet frame successfully.
    s_front_frame_valid = false;
    // A complete foreground scene also supersedes any former recorder delta
    // baseline. draw_recording_visual() restores this only after its own full
    // first frame succeeds, preventing an out-of-band status/setup screen from
    // being used as the source for a later waveform delta.
    s_recording_frame_baseline = false;
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
    // Any direct LCD write (status text, QR, clock ring, etc.) changes pixels
    // outside the pet double buffer.  The next standby frame must therefore be
    // a full present; otherwise its delta compares against an old pet frame and
    // leaves foreground glyphs in unchanged areas as visible afterimages.
    // A successful pet delta revalidates the cache at its caller, while a failed
    // or interrupted transfer correctly leaves it invalid for the next frame.
    s_front_frame_valid = false;
    // Keep the recorder's baseline under the same rule. This helper is also
    // used by direct status/clock writes and by every stripe of a delta frame;
    // draw_recording_visual() re-arms it only after its entire recorder frame
    // has completed successfully.
    s_recording_frame_baseline = false;
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

static esp_err_t es8311_read(uint8_t reg, uint8_t *value) {
    if (!value) return ESP_ERR_INVALID_ARG;
    return i2c_master_transmit_receive(s_es8311, &reg, sizeof(reg), value,
                                       sizeof(*value), 1000);
}

static uint8_t es8311_volume_register(unsigned percent) {
    if (percent == 0) return 0;
    // ES8311's DAC volume register spans 0x00..0xFF. Match the codec's
    // documented linear percentage mapping while keeping 100% at 0xFF.
    return (uint8_t)((percent * 256u / 100u) - 1u);
}

static esp_err_t es8311_set_output_volume(unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    return es8311_write(ES8311_DAC_VOLUME_REG, es8311_volume_register(percent));
}

/* PA shutdown alone does not stop the ES8311 DAC/reference path. Mute the
 * codec whenever no playback owns the PA, then unmute after the first queued
 * PCM block. This prevents DAC floor/reference ramp noise being mixed into
 * every voice reply on the EchoEar analogue path. */
static esp_err_t es8311_set_dac_muted(bool muted) {
    /* REG31 also carries DAC control bits.  Only alter the documented mute
     * pair, as the reference driver does; writing a literal 0x00 after a
     * runtime configuration can accidentally undo unrelated DAC controls. */
    uint8_t reg31 = 0;
    ESP_RETURN_ON_ERROR(es8311_read(ES8311_DAC_MUTE_REG, &reg31), TAG,
                        "read ES8311 DAC mute register");
    if (muted) {
        reg31 |= 0x60;
    } else {
        reg31 &= (uint8_t)~0x60;
    }
    return es8311_write(ES8311_DAC_MUTE_REG, reg31);
}

static esp_err_t es8311_init(void) {
    // EchoEar-2ST uses the ES8311 at 0x18 as I2S slave. This sequence is
    // Espressif's BSP initialization for 16-bit, 16 kHz playback with the
    // ESP32-S3 supplying its 4.096 MHz MCLK.
    static const uint8_t init[][2] = {
        {0x00, 0x1F}, {0x00, 0x00}, {0x00, 0x80},
        // ES8311 REG06 stores BCLK divider minus one. With this board's
        // 4.096 MHz MCLK and 16 kHz I2S stream, the physical divider is four
        // and the codec must receive 0x03. Programming 0x04 makes the DAC
        // consume the wrong bit-clock ratio: data is audible but speech is
        // severely distorted, like a weak two-way radio.
        /* For 4.096 MHz/16 kHz the official ES8311 coefficients are
         * ADC OSR=0x10 and DAC OSR=0x20.  Programming 0x10 into REG04
         * clocks the DAC at the ADC oversampling ratio, which preserves
         * rough tones but corrupts broadband speech with radio-like noise. */
        /* REG01: use external MCLK (bit7=0) with normal, non-inverted
         * MCLK polarity (bit6=0).  `0x7f` inadvertently sets bit6 and makes
         * the ES8311 sample MCLK on the opposite edge.  That can leave short
         * tones recognisable while making broadband voice sound like strong
         * white noise.  Espressif's ES8311 reference driver derives `0x3f`
         * for slave mode with MCLK enabled and no inversion. */
        {0x01, 0x3F}, {0x02, 0x00}, {0x03, 0x10}, {0x04, 0x20},
        {0x05, 0x00}, {0x06, 0x03}, {0x07, 0x00}, {0x08, 0xFF},
        /* REG09/REG0A are I2S-format interfaces.  The old literal 0x0C
         * advertised a 16-bit word while the ESP now deliberately emits
         * 16 valid bits in 32-bit slots (64 BCLK/LRCK).  Per ES8311's
         * reference driver, bits [4:2]=100 select a 32-bit I2S word; leaving
         * it at 0x0C makes the DAC advance to the next channel after 16 clocks
         * and interleave padding/data as broadband white noise. */
        {0x09, 0x10}, {0x0A, 0x10}, {0x0D, 0x01}, {0x0E, 0x02},
        {0x12, 0x00}, {0x13, 0x10}, {0x14, 0x1A}, {0x15, 0x40},
        {0x1C, 0x6A}, {0x37, 0x08}, {0x44, 0x58}, {0x31, 0x60},
    };
    ESP_RETURN_ON_ERROR(es8311_write(init[0][0], init[0][1]), TAG, "ES8311 reset");
    vTaskDelay(pdMS_TO_TICKS(20));
    for (size_t i = 1; i < sizeof(init) / sizeof(init[0]); ++i) {
        ESP_RETURN_ON_ERROR(es8311_write(init[i][0], init[i][1]), TAG,
                            "ES8311 reg %02x", init[i][0]);
    }
    return es8311_set_output_volume(s_output_volume);
}

#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
/* The PMIC is a board detail.  Higher layers only receive normalized battery
 * state through board_port_get_power_status(). */
static esp_err_t axp2101_write(uint8_t reg, uint8_t value) {
    uint8_t bytes[2] = {reg, value};
    return i2c_master_transmit(s_axp2101, bytes, sizeof(bytes), 1000);
}

static esp_err_t axp2101_read(uint8_t reg, uint8_t *value) {
    return i2c_master_transmit_receive(s_axp2101, &reg, sizeof(reg), value, 1, 1000);
}

static bool axp2101_read_power_status(unsigned *level_percent, bool *charging) {
    uint8_t capacity = 0;
    uint8_t state = 0;
    if (axp2101_read(0xA4, &capacity) != ESP_OK || axp2101_read(0x00, &state) != ESP_OK) {
        return false;
    }
    if (level_percent) *level_percent = capacity > 100 ? 100 : capacity;
    /* AXP2101 register 0x01 bits [6:5] encode current direction.  Bit 2
     * alone means charge-done, not active charging, which previously made
     * normal discharging state appear as a charger state to the common HAL. */
    if (charging) *charging = ((state >> 5) & 0x03u) == 1u;
    return true;
}

static esp_err_t axp2101_init(void) {
    const i2c_device_config_t cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7, .device_address = 0x34,
        .scl_speed_hz = 100000,
    };
    ESP_RETURN_ON_ERROR(i2c_master_bus_add_device(s_audio_i2c_bus, &cfg, &s_axp2101),
                        TAG, "add AXP2101");
    static const uint8_t init[][2] = {
        {0x80, 0x01}, {0x90, 0x01}, {0x91, 0x00}, {0x82, 0x12},
        {0x92, 0x1c}, {0x64, 0x02}, {0x61, 0x02}, {0x62, 0x0a},
        {0x63, 0x01}, {0x22, 0x06}, {0x27, 0x10},
    };
    for (size_t i = 0; i < sizeof(init) / sizeof(init[0]); ++i) {
        ESP_RETURN_ON_ERROR(axp2101_write(init[i][0], init[i][1]), TAG,
                            "AXP2101 reg %02x", init[i][0]);
    }
    return ESP_OK;
}

static esp_err_t cst9217_init(void) {
    esp_lcd_touch_config_t touch_cfg = {
        .x_max = LCD_WIDTH - 1, .y_max = LCD_HEIGHT - 1,
        .rst_gpio_num = GPIO_NUM_2, .int_gpio_num = TOUCH_IRQ,
        .levels = {.reset = 0, .interrupt = 0},
        .flags = {.swap_xy = 0, .mirror_x = 1, .mirror_y = 1},
    };
    esp_lcd_panel_io_i2c_config_t io_cfg = ESP_LCD_TOUCH_IO_I2C_CST9217_CONFIG();
    io_cfg.scl_speed_hz = 400000;
    ESP_RETURN_ON_ERROR(esp_lcd_new_panel_io_i2c(s_audio_i2c_bus, &io_cfg, &s_cst9217_io),
                        TAG, "create CST9217 I2C IO");
    return esp_lcd_touch_new_i2c_cst9217(s_cst9217_io, &touch_cfg, &s_cst9217_touch);
}

/* QMI8658 is a board-local device.  The common Motion HAL receives only a
 * normalized sample; identity, range configuration and raw register values
 * deliberately never escape this adapter.  The Waveshare schematic exposes
 * the IMU on the same I2C bus at 0x6B. */
#define QMI8658_ADDRESS             0x6B
#define QMI8658_REG_WHO_AM_I        0x00
#define QMI8658_REG_CTRL1           0x02
#define QMI8658_REG_CTRL2           0x03
#define QMI8658_REG_CTRL3           0x04
#define QMI8658_REG_CTRL7           0x08
#define QMI8658_REG_DATA            0x35
#define QMI8658_WHO_AM_I_VALUE      0x05

static esp_err_t qmi8658_write(uint8_t reg, uint8_t value) {
    const uint8_t bytes[2] = {reg, value};
    return i2c_master_transmit(s_qmi8658, bytes, sizeof(bytes), 1000);
}

static esp_err_t qmi8658_read(uint8_t reg, uint8_t *data, size_t length) {
    return i2c_master_transmit_receive(s_qmi8658, &reg, sizeof(reg), data, length, 1000);
}

static int16_t qmi8658_decode_i16(const uint8_t *data) {
    return (int16_t)((uint16_t)data[0] | ((uint16_t)data[1] << 8));
}

static esp_err_t qmi8658_init(void) {
    const i2c_device_config_t cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7, .device_address = QMI8658_ADDRESS,
        .scl_speed_hz = 400000,
    };
    ESP_RETURN_ON_ERROR(i2c_master_bus_add_device(s_audio_i2c_bus, &cfg, &s_qmi8658),
                        TAG, "add QMI8658");
    uint8_t who_am_i = 0;
    ESP_RETURN_ON_ERROR(qmi8658_read(QMI8658_REG_WHO_AM_I, &who_am_i, 1), TAG,
                        "read QMI8658 identity");
    if (who_am_i != QMI8658_WHO_AM_I_VALUE) {
        ESP_LOGE(TAG, "unexpected QMI8658 identity 0x%02x", who_am_i);
        return ESP_ERR_NOT_FOUND;
    }
    /* 0x40 selects auto-address-increment and little-endian output.  The
     * verified reference driver encodes CTRL2 range in [5:4] and ODR in
     * [3:0], and CTRL3 analogously.  Use +/-8g/125Hz and +/-1024dps/112Hz:
     * enough headroom and time resolution for later fall-state calibration. */
    ESP_RETURN_ON_ERROR(qmi8658_write(QMI8658_REG_CTRL1, 0x40), TAG, "QMI8658 CTRL1");
    ESP_RETURN_ON_ERROR(qmi8658_write(QMI8658_REG_CTRL2, 0x26), TAG, "QMI8658 CTRL2");
    ESP_RETURN_ON_ERROR(qmi8658_write(QMI8658_REG_CTRL3, 0x66), TAG, "QMI8658 CTRL3");
    ESP_RETURN_ON_ERROR(qmi8658_write(QMI8658_REG_CTRL7, 0x03), TAG, "QMI8658 CTRL7");
    ESP_LOGI(TAG, "QMI8658 ready: accel 8g/125Hz, gyro 1024dps/112Hz");
    return ESP_OK;
}
#endif

static void audio_init_rollback(void) {
    // audio_init() is intentionally retryable: board_port_init() probes audio
    // early for touch support, and wake startup may retry it later. Leaving a
    // partly-created I2C bus or I2S channel behind makes every later attempt
    // fail with "bus already acquired", permanently disabling EchoEar wake.
    if (s_audio_tx) {
        (void)i2s_channel_disable(s_audio_tx);
        (void)i2s_del_channel(s_audio_tx);
        s_audio_tx = NULL;
    }
    if (s_audio_rx) {
        (void)i2s_channel_disable(s_audio_rx);
        (void)i2s_del_channel(s_audio_rx);
        s_audio_rx = NULL;
    }
    if (s_touch) {
        (void)i2c_master_bus_rm_device(s_touch);
        s_touch = NULL;
    }
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    if (s_cst9217_touch) {
        (void)esp_lcd_touch_del(s_cst9217_touch);
        s_cst9217_touch = NULL;
    }
    if (s_cst9217_io) {
        (void)esp_lcd_panel_io_del(s_cst9217_io);
        s_cst9217_io = NULL;
    }
    if (s_axp2101) {
        (void)i2c_master_bus_rm_device(s_axp2101);
        s_axp2101 = NULL;
    }
    if (s_qmi8658) {
        (void)i2c_master_bus_rm_device(s_qmi8658);
        s_qmi8658 = NULL;
    }
#endif
    if (s_es8311) {
        (void)i2c_master_bus_rm_device(s_es8311);
        s_es8311 = NULL;
    }
    if (s_es7210) {
        (void)i2c_master_bus_rm_device(s_es7210);
        s_es7210 = NULL;
    }
    if (s_audio_i2c_bus) {
        (void)i2c_del_master_bus(s_audio_i2c_bus);
        s_audio_i2c_bus = NULL;
    }
    s_audio_ready = false;
}

static esp_err_t audio_init(void) {
    if (s_audio_ready) return ESP_OK;
    // Recover from an earlier partial attempt before claiming the peripherals
    // again. A successful initialization never enters this path because it
    // returns above with s_audio_ready set.
    audio_init_rollback();
    esp_err_t err = ESP_OK;
    i2c_master_bus_config_t bus_cfg = {
        .i2c_port = I2C_NUM_0, .sda_io_num = AUDIO_I2C_SDA,
        .scl_io_num = AUDIO_I2C_SCL, .clk_source = I2C_CLK_SRC_DEFAULT,
        .glitch_ignore_cnt = 7, .flags.enable_internal_pullup = true,
    };
    err = i2c_new_master_bus(&bus_cfg, &s_audio_i2c_bus);
    if (err != ESP_OK) goto fail;
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    err = axp2101_init();
    if (err != ESP_OK) goto fail;
    err = cst9217_init();
    if (err != ESP_OK) goto fail;
    err = qmi8658_init();
    if (err != ESP_OK) goto fail;
#endif
    i2c_device_config_t dev_cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7, .device_address = ES7210_ADDRESS,
        .scl_speed_hz = 100000,
    };
    err = i2c_master_bus_add_device(s_audio_i2c_bus, &dev_cfg, &s_es7210);
    if (err != ESP_OK) goto fail;
    i2c_device_config_t speaker_cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7, .device_address = ES8311_ADDRESS,
        .scl_speed_hz = 100000,
    };
    err = i2c_master_bus_add_device(s_audio_i2c_bus, &speaker_cfg, &s_es8311);
    if (err != ESP_OK) goto fail;
#if !CONFIG_MACLAW_BOARD_FANGTANG_4G && !CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    i2c_device_config_t touch_cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7, .device_address = CST8XX_ADDRESS,
        .scl_speed_hz = 100000,
    };
    esp_err_t touch_err = i2c_master_bus_add_device(s_audio_i2c_bus, &touch_cfg, &s_touch);
    if (touch_err != ESP_OK) {
        ESP_LOGW(TAG, "CST8xx touch add failed: %s", esp_err_to_name(touch_err));
        s_touch = NULL;
    }
#endif

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
        if (err != ESP_OK) goto fail;
    }

    i2s_chan_config_t chan_cfg = I2S_CHANNEL_DEFAULT_CONFIG(I2S_NUM_0, I2S_ROLE_MASTER);
    chan_cfg.dma_desc_num = 8;
    chan_cfg.dma_frame_num = 256;
    // ES7210 input and ES8311 output share I2S0's MCLK/BCLK/WS wires. Allocate
    // them as one full-duplex channel pair so the driver owns and configures
    // the clock domain only once. Creating RX and TX in two independent calls
    // makes the second call collide with the first controller/GPIO ownership;
    // on real EchoEar hardware that leaves the microphone stream looking like
    // near-full-scale random data and MultiNet cannot recognise any phrase.
    err = i2s_new_channel(&chan_cfg, &s_audio_tx, &s_audio_rx);
    if (err != ESP_OK) goto fail;
    // Keep the previously verified full-duplex initialization order: RX claims
    // the shared clock domain first, then TX attaches the ES8311 output path.
    // Explicit TX/RX configuration needs a board-level clock probe before it
    // can replace this known-audible baseline.
    i2s_tdm_config_t rx_cfg = {
        .clk_cfg = I2S_TDM_CLK_DEFAULT_CONFIG(AUDIO_RATE),
        .slot_cfg = I2S_TDM_PHILIPS_SLOT_DEFAULT_CONFIG(
            I2S_DATA_BIT_WIDTH_16BIT, I2S_SLOT_MODE_STEREO,
            I2S_TDM_SLOT0 | I2S_TDM_SLOT1 | I2S_TDM_SLOT2 | I2S_TDM_SLOT3),
        .gpio_cfg = {
            .mclk = AUDIO_MCLK, .bclk = AUDIO_BCLK, .ws = AUDIO_WS,
            .dout = I2S_GPIO_UNUSED, .din = AUDIO_DIN,
            .invert_flags = {.mclk_inv = false, .bclk_inv = false, .ws_inv = false},
        },
    };
    err = i2s_channel_init_tdm_mode(s_audio_rx, &rx_cfg);
    if (err != ESP_OK) goto fail;
    i2s_std_config_t tx_cfg = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(AUDIO_RATE),
        .slot_cfg = I2S_STD_PHILIPS_SLOT_DEFAULT_CONFIG(
            I2S_DATA_BIT_WIDTH_16BIT, I2S_SLOT_MODE_STEREO),
        .gpio_cfg = {
            .mclk = AUDIO_MCLK, .bclk = AUDIO_BCLK, .ws = AUDIO_WS,
            .dout = AUDIO_DOUT, .din = I2S_GPIO_UNUSED,
            .invert_flags = {.mclk_inv = false, .bclk_inv = false, .ws_inv = false},
        },
    };
    // Preserve the known-audible shared-clock initialization order above, but
    // make the ES8311 data alignment explicit. This board's codec expects the
    // 16-bit samples left-aligned in the Philips slots; relying on the IDF
    // default leaves speech data offset although simple tones remain audible.
    tx_cfg.slot_cfg.left_align = true;
    // The ES8311 16 kHz / 4.096 MHz coefficient table requires 64 BCLKs per
    // frame.  Sending 16-bit stereo slots here produces only 32 BCLKs and
    // makes the DAC decode speech as intense broadband noise.  Keep 16-bit
    // PCM data, but transport it left-aligned in 32-bit stereo slots.
    tx_cfg.slot_cfg.slot_bit_width = I2S_SLOT_BIT_WIDTH_32BIT;
    tx_cfg.clk_cfg.mclk_multiple = ECHOEAR_AUDIO_MCLK_MULTIPLE;
    err = i2s_channel_init_std_mode(s_audio_tx, &tx_cfg);
    if (err != ESP_OK) goto fail;
    // EchoEar's ES7210/ES8311 share this full-duplex I2S clock domain.  Unlike
    // Bread Compact's separate speaker path, stopping TX also makes the live
    // microphone samples go silent on this codec pair.  Keep TX enabled and
    // use AUDIO_PA_ENABLE plus an explicit zero tail to make playback quiet.
    err = i2s_channel_enable(s_audio_tx);
    if (err != ESP_OK) goto fail;
    err = i2s_channel_enable(s_audio_rx);
    if (err != ESP_OK) goto fail;
    gpio_config_t pa_cfg = {
        .pin_bit_mask = 1ULL << AUDIO_PA_ENABLE, .mode = GPIO_MODE_OUTPUT,
        .pull_up_en = GPIO_PULLUP_DISABLE, .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    err = gpio_config(&pa_cfg);
    if (err != ESP_OK) goto fail;
    err = gpio_set_level(AUDIO_PA_ENABLE, 0);
    if (err != ESP_OK) goto fail;
    err = es8311_init();
    if (err != ESP_OK) goto fail;
    s_audio_ready = true;
    i2s_chan_info_t tx_info = {0};
    if (i2s_channel_get_info(s_audio_tx, &tx_info) == ESP_OK) {
        ESP_LOGI(TAG, "EchoEar audio ready: ES7210 mic + ES8311 speaker at %dHz "
                 "(MCLK=%" PRIu32 "Hz, DAC BCLK=%" PRIu32
                 "Hz, 16-bit PCM in 32-bit stereo slots, MCLK normal)",
                 AUDIO_RATE, tx_info.mclk_hz, tx_info.bclk_hz);
    } else {
        ESP_LOGW(TAG, "EchoEar audio ready but I2S clock diagnostics unavailable");
    }
    return ESP_OK;
fail:
    ESP_LOGE(TAG, "EchoEar audio initialization failed: %s; rolling back for retry",
             esp_err_to_name(err));
    audio_init_rollback();
    return err;
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
    // Keep the ambient face on Bread Compact's deep blue-black base.  The
    // former brighter blue read washed-out on EchoEar's high-brightness round
    // panel and reduced contrast against the cool pet halo and white clock.
    if (state && !strcmp(state, "idle")) return rgb565(18, 24, 38);
    // "quiet" is an ambient standby state too. Match Bread Compact's same
    // deep base rather than letting the boot/reconnect standby frame jump to
    // a lighter blue than the normal idle frame.
    return rgb565(18, 24, 38);
}

/* Shared round scenes were authored around the 360px EchoEar.  The 1.75C
 * adapter retains their semantics but scales their local art coordinates into
 * its larger logical viewport; this keeps display geometry below the HAL. */
static int round_scene_x(int x) {
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    return (x * LCD_WIDTH + 180) / 360;
#else
    return x;
#endif
}

static int round_scene_y(int y) {
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    return (y * LCD_HEIGHT + 180) / 360;
#else
    return y;
#endif
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

// Result thumbnails arrive as compact RGB565 tiles (at most 64 x 64), which
// are far too small when drawn 1:1 on EchoEar's 360px round panel.  Scale them
// while composing into the same full-frame surface as the result chrome so no
// intermediate direct transfer can show through or leave an old page behind.
static void draw_or_compose_scaled_bitmap(int x0, int y0, int x1, int y1,
                                          const uint16_t *pixels,
                                          size_t source_width,
                                          size_t source_height) {
    if (!pixels || !source_width || !source_height || x0 < 0 || y0 < 0 ||
        x1 > LCD_WIDTH || y1 > LCD_HEIGHT || x0 >= x1 || y0 >= y1) return;
    const int target_width = x1 - x0;
    const int target_height = y1 - y0;
    if (s_render_target) {
        for (int y = 0; y < target_height; ++y) {
            size_t source_y = (size_t)y * source_height / (size_t)target_height;
            uint16_t *dst = s_render_target + (size_t)(y0 + y) * LCD_WIDTH + x0;
            for (int x = 0; x < target_width; ++x) {
                size_t source_x = (size_t)x * source_width / (size_t)target_width;
                dst[x] = pixels[source_y * source_width + source_x];
            }
        }
        return;
    }
    for (int strip_y = 0; strip_y < target_height; strip_y += LCD_STRIPE_ROWS) {
        const int rows = (target_height - strip_y) < LCD_STRIPE_ROWS
                             ? (target_height - strip_y)
                             : LCD_STRIPE_ROWS;
        for (int row = 0; row < rows; ++row) {
            size_t source_y = (size_t)(strip_y + row) * source_height /
                              (size_t)target_height;
            for (int x = 0; x < target_width; ++x) {
                size_t source_x = (size_t)x * source_width / (size_t)target_width;
                s_line[(size_t)row * target_width + x] =
                    pixels[source_y * source_width + source_x];
            }
        }
        ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(
            x0, y0 + strip_y, x1, y0 + strip_y + rows, s_line));
    }
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

static bool full_cjk24_copy(uint32_t codepoint,
                            uint8_t bitmap[DYNAMIC_GLYPH_BYTES]) {
    if (codepoint < 0x4E00 || codepoint >= 0xA000) return false;
    size_t offset = (size_t)(codepoint - 0x4E00) * DYNAMIC_GLYPH_BYTES;
    size_t available = (size_t)(_binary_cjk24_cjk_bin_end -
                                _binary_cjk24_cjk_bin_start);
    if (offset + DYNAMIC_GLYPH_BYTES > available) return false;
    memcpy(bitmap, _binary_cjk24_cjk_bin_start + offset, DYNAMIC_GLYPH_BYTES);
    return true;
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

static int text24_width(const char *text, int max_glyphs) {
    if (!text || max_glyphs <= 0) return 0;
    int width = 0;
    int glyphs = 0;
    const char *cursor = text;
    while (*cursor && glyphs++ < max_glyphs) {
        width += text24_advance(utf8_next(&cursor));
    }
    return width > 0 ? width - 1 : 0;
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
            const uint8_t *dynamic = !rows &&
                                     (dynamic_glyph_copy(cp, dynamic_bitmap) ||
                                      full_cjk24_copy(cp, dynamic_bitmap))
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
            const uint8_t *dynamic = !rows &&
                                     (dynamic_glyph_copy(cp, dynamic_bitmap) ||
                                      full_cjk24_copy(cp, dynamic_bitmap))
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
        uint8_t dynamic_bitmap[DYNAMIC_GLYPH_BYTES];
        const uint8_t *dynamic = !rows &&
                                 (dynamic_glyph_copy(cp, dynamic_bitmap) ||
                                  full_cjk24_copy(cp, dynamic_bitmap))
                                     ? dynamic_bitmap : NULL;
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
                for (int sy = source_y0; !set && sy < source_y1; ++sy) {
                    for (int sx = source_x0; sx < source_x1; ++sx) {
                        if (glyph24_pixel(cp, rows, dynamic, sy, sx)) {
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

// Place legible upright glyphs on a true circular arc. Rotating the 16px CJK
// bitmap made strokes fragment on this LCD, so the glyphs stay upright while
// their baselines follow the rim. A positive radius creates a top arc (centre
// high, sides low); a negative radius creates the matching lower arc.
//
// This intentionally does not use the former x^2/divisor approximation.  That
// parabola looks almost flat in the middle and then bends too abruptly near the
// ends, which made the date/time and weather rings read as uneven on EchoEar's
// round display.
static void compose_text16_curve(uint16_t *target, int stride, int width, int height,
                                 int center_x, int apex_y, int arc_radius,
                                 const char *text, uint16_t fg) {
    if (!target || !text || !text[0] || arc_radius == 0) return;
    const int radius = arc_radius < 0 ? -arc_radius : arc_radius;
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
        int offset = midpoint < 0 ? -midpoint : midpoint;
        // Text never spans outside the selected circle, but clamp defensively
        // so an unusually long provider string cannot make sqrtf negative.
        if (offset >= radius) offset = radius - 1;
        const int sag = radius - (int)sqrtf((float)(radius * radius - offset * offset));
        int y = arc_radius > 0 ? apex_y + sag : apex_y - sag;
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
                                 int center_x, int apex_y, int arc_radius,
                                 const char *text,
                                 uint16_t fg) {
    if (!target || !text || !text[0] || arc_radius == 0) return;
    const int radius = arc_radius < 0 ? -arc_radius : arc_radius;
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
        // circular arc; the negative radius reverses it for the lower rim.
        int offset = midpoint < 0 ? -midpoint : midpoint;
        if (offset >= radius) offset = radius - 1;
        const int sag = radius - (int)sqrtf((float)(radius * radius - offset * offset));
        int y = arc_radius > 0 ? apex_y + sag : apex_y - sag;
        if (y < 1) y = 1;
        if (y > height - CJK_FONT_SIZE - 1) y = height - CJK_FONT_SIZE - 1;
        const uint32_t *rows = cjk24_rows(glyphs[i]);
        uint8_t dynamic_bitmap[DYNAMIC_GLYPH_BYTES];
        const uint8_t *dynamic = !rows &&
                                 (dynamic_glyph_copy(glyphs[i], dynamic_bitmap) ||
                                  full_cjk24_copy(glyphs[i], dynamic_bitmap))
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

// Copy one display line using the same mixed CJK/ASCII advances as the
// renderer.  Fixed glyph counts are not enough here: a short English status
// needs more letters than a Chinese one, while either must remain inside the
// circular message page's safe column.
static const char *copy_text24_line(char *out, size_t out_size, const char *source,
                                    int max_width) {
    if (!out || !out_size) return source;
    out[0] = '\0';
    if (!source || max_width <= 0) return source;
    const char *cursor = source;
    size_t used = 0;
    int width = 0;
    while (*cursor) {
        const char *start = cursor;
        uint32_t cp = utf8_next(&cursor);
        if (cp == '\r' || cp == '\n') {
            while (*cursor == '\r' || *cursor == '\n') ++cursor;
            break;
        }
        const int advance = text24_advance(cp);
        const size_t bytes = (size_t)(cursor - start);
        if (width && width + advance > max_width) {
            cursor = start;
            break;
        }
        if (used + bytes >= out_size) {
            cursor = start;
            break;
        }
        memcpy(out + used, start, bytes);
        used += bytes;
        out[used] = '\0';
        width += advance;
    }
    // Guarantee progress for an unexpected wide glyph or malformed provider
    // string; otherwise callers trying to make a second line could stall.
    if (!out[0] && *source) {
        const char *cursor = source;
        const char *start = cursor;
        (void)utf8_next(&cursor);
        size_t bytes = (size_t)(cursor - start);
        if (bytes < out_size) {
            memcpy(out, start, bytes);
            out[bytes] = '\0';
        }
        return cursor;
    }
    return cursor;
}

// `draw_text24()` is intentionally a low-level primitive: it clips only at
// the rectangular framebuffer edge.  Response titles and image captions need
// the circular display's narrower safe column instead, so copy a measured
// display line before centring rather than measuring a long source string and
// hoping its first glyphs happen to remain centred.
static const char *draw_text24_centered_safe(int y, const char *text, int max_width,
                                             uint16_t fg, uint16_t bg) {
    char line[96];
    const char *next = copy_text24_line(line, sizeof(line), text, max_width);
    int line_width = text24_width(line, 21);
    if (line_width > max_width) line_width = max_width;
    draw_text24((LCD_WIDTH - line_width) / 2, y, line, fg, bg);
    return next;
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
    bool service_ready;
    bool wifi_connected;
    bool alarm_scheduled;
    taskENTER_CRITICAL(&s_state_lock);
    memcpy(ambient_time, s_ambient_time, sizeof(ambient_time));
    memcpy(ambient_location, s_ambient_location, sizeof(ambient_location));
    memcpy(ambient_date, s_ambient_date, sizeof(ambient_date));
    memcpy(ambient_weekday, s_ambient_weekday, sizeof(ambient_weekday));
    memcpy(ambient_weather, s_ambient_weather, sizeof(ambient_weather));
    ambient_temperature_c = s_ambient_temperature_c;
    ambient_weather_valid = s_ambient_weather_valid;
    service_ready = s_service_ready;
    wifi_connected = s_wifi_connected;
    alarm_scheduled = s_alarm_scheduled;
    taskEXIT_CRITICAL(&s_state_lock);

    // During full-frame composition these are transparent text overlays. The
    // old opaque background fill created visible rectangular top/bottom masks
    // even though the physical 360 x 360 LCD has no such dead zones. Direct
    // standalone updates retain an opaque background because no backing frame
    // is available in that path.
    const bool keyed_overlay = s_render_target != NULL;
    const uint16_t overlay_bg = keyed_overlay ? AMBIENT_TRANSPARENT_KEY : bg;
    if (!s_ambient_overlay) return;
    for (size_t i = 0; i < AMBIENT_OVERLAY_PIXELS; ++i) {
        s_ambient_overlay[i] = overlay_bg;
    }
    bool clock_valid = ambient_time[0] && strcmp(ambient_time, "--:--:--");
    if (clock_valid) {
        char date_ring[48];
        char status_ring[48];
        // Upper ring is time context only. It ends above y=62 so the pet's
        // ears and head always remain completely untouched.
        // Keep the compact service and alarm state on the same upper ring as
        // the clock.  This mirrors Bread's ready/wait and scheduled-alarm
        // feedback without consuming the circular screen's pet-safe center.
        // Keep the same three connection meanings as Bread Compact: the
        // gateway is online, Wi-Fi is up but the gateway is still starting,
        // or the board is joining the network.  This belongs to the composed
        // ambient frame, so a subsequent pet animation cannot erase it.
        const char *connection = service_ready ? "在线" :
                                 (wifi_connected ? "服务中" : "联网中");
        snprintf(date_ring, sizeof(date_ring), "%s %s%s", ambient_date,
                 ambient_weekday, alarm_scheduled ? " AL" : "");
        snprintf(status_ring, sizeof(status_ring), "%s %s", ambient_time, connection);
        // Both upper strings use the weather ring's native 24px glyphs. They
        // are split into two arcs so neither end is forced into the circular
        // bezel when a full date and service label are present.
        compose_text24_curve(s_ambient_overlay, AMBIENT_TOP_W, AMBIENT_TOP_W, AMBIENT_TOP_H,
                             AMBIENT_TOP_W / 2, 16, AMBIENT_TOP_RING_RADIUS,
                             status_ring, primary);
        compose_text24_curve(s_ambient_overlay, AMBIENT_TOP_W, AMBIENT_TOP_W, AMBIENT_TOP_H,
                             AMBIENT_TOP_W / 2, 56, AMBIENT_TOP_RING_RADIUS,
                             date_ring, primary);
    }
    draw_or_compose_bitmap(0, 0, AMBIENT_TOP_W, AMBIENT_TOP_H,
                           s_ambient_overlay, keyed_overlay);

    // City precedes weather on the matching lower arc. Its physical region
    // starts below the native pet's 96..272 drawing area, so neither text nor
    // background clearing can cut into the pet circle.
    for (size_t i = 0; i < AMBIENT_OVERLAY_PIXELS; ++i) {
        s_ambient_overlay[i] = overlay_bg;
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
        compose_text24_curve(s_ambient_overlay, AMBIENT_BOTTOM_W, AMBIENT_BOTTOM_W, AMBIENT_BOTTOM_H,
                             // Mirror the upper rim exactly, reversing the
                             // sign for the lower half of the external circle.
                             AMBIENT_BOTTOM_W / 2, 52, -AMBIENT_RING_RADIUS,
                             lower_ring, primary);
    }
    draw_or_compose_bitmap(
        // This taller, transparent area starts above the old lower strip so
        // the two ends of the tighter lower arc remain visible.  It only
        // paints foreground glyphs; the pet pixels underneath are preserved.
        AMBIENT_BOTTOM_X, AMBIENT_BOTTOM_Y,
        AMBIENT_BOTTOM_X + AMBIENT_BOTTOM_W, AMBIENT_BOTTOM_Y + AMBIENT_BOTTOM_H,
                           s_ambient_overlay, keyed_overlay);
}

// The boot surface is intentionally separate from standby: while the selected
// pet may change, the product mark should remain recognisable from the first
// powered pixel.  Keep the lettering upright for legibility, but place it on
// the lower half of the same true circle used by EchoEar's ambient rings.
static void draw_startup_brand_arc(uint16_t bg) {
    if (!s_ambient_overlay) return;
    const bool keyed_overlay = s_render_target != NULL;
    const uint16_t overlay_bg = keyed_overlay ? AMBIENT_TRANSPARENT_KEY : bg;
    for (size_t i = 0; i < AMBIENT_OVERLAY_PIXELS; ++i) {
        s_ambient_overlay[i] = overlay_bg;
    }
    compose_text24_curve(s_ambient_overlay, AMBIENT_BOTTOM_W, AMBIENT_BOTTOM_W,
                         AMBIENT_BOTTOM_H, AMBIENT_BOTTOM_W / 2, 48,
                         -AMBIENT_RING_RADIUS, "MaClaw Mate",
                         rgb565(244, 249, 253));
    draw_or_compose_bitmap(AMBIENT_BOTTOM_X, AMBIENT_BOTTOM_Y,
                           AMBIENT_BOTTOM_X + AMBIENT_BOTTOM_W,
                           AMBIENT_BOTTOM_Y + AMBIENT_BOTTOM_H, s_ambient_overlay,
                           keyed_overlay);
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

static int scale_standby_cat_coordinate(int value, int center) {
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    // Native pet coordinates are authored in a 360px square around (180,180).
    // Preserve the intentional 7/8 smaller idle cat while mapping it into the
    // target board's pet safe zone.  `center` is the board-specific anchor:
    // LCD centre for x, and the visual halo centre for y.
    return center + (value - 180) * LCD_WIDTH * STANDBY_CAT_SCALE_NUM /
                        (360 * STANDBY_CAT_SCALE_DEN);
#else
    // Preserve EchoEar's established 360px composition byte-for-byte.
    return center + (value - center) * STANDBY_CAT_SCALE_NUM / STANDBY_CAT_SCALE_DEN;
#endif
}

static int scale_standby_cat_radius(int value) {
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    return value * LCD_WIDTH * STANDBY_CAT_SCALE_NUM /
                   (360 * STANDBY_CAT_SCALE_DEN);
#else
    return value * STANDBY_CAT_SCALE_NUM / STANDBY_CAT_SCALE_DEN;
#endif
}

static void draw_standby_cat_eye(int x, int y, uint16_t dark, uint16_t shine) {
    const int cx = scale_standby_cat_coordinate(x, LCD_WIDTH / 2);
    const int cy = scale_standby_cat_coordinate(y, PET_HALO_CENTER_Y);
    fill_circle_vertical_gradient(cx, cy, scale_standby_cat_radius(25),
                                  rgb565(31, 67, 101), dark);
    fill_circle_vertical_gradient(cx, cy + scale_standby_cat_radius(2),
                                  scale_standby_cat_radius(16),
                                  rgb565(67, 207, 225), rgb565(21, 91, 145));
    fill_circle(cx, cy + scale_standby_cat_radius(5), scale_standby_cat_radius(9),
                rgb565(10, 24, 42));
    fill_circle(cx + scale_standby_cat_radius(8), cy - scale_standby_cat_radius(8),
                scale_standby_cat_radius(7), shine);
    fill_circle(cx - scale_standby_cat_radius(5), cy + scale_standby_cat_radius(8),
                scale_standby_cat_radius(3), rgb565(135, 235, 245));
    fill_rect(cx - scale_standby_cat_radius(18), cy + scale_standby_cat_radius(20),
              cx + scale_standby_cat_radius(19), cy + scale_standby_cat_radius(24), dark);
}

static void draw_standby_cat_ear(int x, int y, int dir,
                                 uint16_t outer, uint16_t inner) {
    const int base_x = scale_standby_cat_coordinate(x, LCD_WIDTH / 2);
    const int base_y = scale_standby_cat_coordinate(y, PET_HALO_CENTER_Y);
    const int rows = scale_standby_cat_radius(55);
    for (int row = 0; row < rows; ++row) {
        const int source_row = row * STANDBY_CAT_SCALE_DEN / STANDBY_CAT_SCALE_NUM;
        const int w = scale_standby_cat_radius(20 + source_row / 2);
        const int left = dir > 0 ? base_x : base_x - w;
        fill_rect(left, base_y + row, left + w, base_y + row + 1, outer);
        if (source_row > 13) {
            const int inner_w = scale_standby_cat_radius(20 + source_row / 2 - 15);
            const int inner_left = dir > 0 ? base_x + scale_standby_cat_radius(7)
                                            : base_x - inner_w - scale_standby_cat_radius(7);
            fill_rect(inner_left, base_y + row, inner_left + inner_w, base_y + row + 1, inner);
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
    const int rows = round_scene_y(55);
    for (int row = 0; row < rows; ++row) {
        int source_row = row * 360 / LCD_HEIGHT;
        int w = round_scene_x(20 + source_row / 2);
        int left = dir > 0 ? x : x - w;
        fill_rect(left, y + row, left + w, y + row + 1, outer);
        if (source_row > 13) {
            int inner_w = round_scene_x(20 + source_row / 2 - 15);
            int inner_left = dir > 0 ? x + round_scene_x(7)
                                     : x - inner_w - round_scene_x(7);
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
    if (s_remote_pet_frame_count > 1 && s_remote_pet_frame_ms) {
        // The remote pack phase is driven by completed animation ticks rather
        // than wall time.  A stalled TLS request or an LCD transfer therefore
        // slows the pet briefly instead of skipping it several authored poses
        // forward on the next repaint.
        uint64_t elapsed_ms = s_remote_pet_animation_elapsed_ms;
        uint32_t remote_frame = (uint32_t)((elapsed_ms / s_remote_pet_frame_ms) %
                                           s_remote_pet_frame_count);
        uint32_t remote_mix = (uint32_t)(((elapsed_ms % s_remote_pet_frame_ms) * 16u) /
                                         s_remote_pet_frame_ms);
        // Include the interpolation phase so duplicate-frame suppression keeps
        // rendering a smooth transition instead of freezing between keyframes.
        return 0x80000000u | remote_frame | (remote_mix << 8);
    }
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
    fill_circle_vertical_gradient(LCD_WIDTH / 2, PET_HALO_CENTER_Y + bob,
                                  round_scene_x(PET_HALO_RADIUS),
                                  halo_top, halo_bottom);
    fill_circle_vertical_gradient(LCD_WIDTH / 2,
                                  PET_HALO_CENTER_Y - round_scene_y(11) + bob,
                                  round_scene_x(92),
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
    fill_circle_vertical_gradient(round_scene_x(244 + tail_root_shift), round_scene_y(224) + bob,
                                  round_scene_x(43),
                                  rgb565(126, 96, 99), rgb565(66, 46, 51));
    fill_circle_vertical_gradient(round_scene_x(238 + tail_mid_shift), round_scene_y(213) + bob,
                                  round_scene_x(30),
                                  rgb565(212, 169, 252), rgb565(143, 82, 222));
    fill_circle_vertical_gradient(round_scene_x(246 + tail_tip_shift), round_scene_y(192) + bob,
                                  round_scene_x(19),
                                  rgb565(139, 105, 108), rgb565(70, 48, 54));

    // Fluffy seated body and paws.
    fill_circle_vertical_gradient(LCD_WIDTH / 2, round_scene_y(217) + bob, round_scene_x(73), rgb565(236, 211, 186),
                                  rgb565(194, 158, 131));
    fill_circle_vertical_gradient(LCD_WIDTH / 2, round_scene_y(210) + bob, round_scene_x(70), rgb565(255, 247, 229),
                                  fur_shadow);
    fill_circle_vertical_gradient(round_scene_x(130), round_scene_y(250) + bob, round_scene_x(31), rgb565(255, 244, 225),
                                  fur_shadow);
    fill_circle_vertical_gradient(round_scene_x(230), round_scene_y(250) + bob, round_scene_x(31), rgb565(255, 244, 225),
                                  fur_shadow);
    fill_circle(round_scene_x(130), round_scene_y(260) + bob, round_scene_x(17), seal);
    fill_circle(round_scene_x(230), round_scene_y(260) + bob, round_scene_x(17), seal);
    fill_circle(round_scene_x(126), round_scene_y(257) + bob, round_scene_x(5), pink);
    fill_circle(round_scene_x(134), round_scene_y(257) + bob, round_scene_x(5), pink);
    fill_circle(round_scene_x(226), round_scene_y(257) + bob, round_scene_x(5), pink);
    fill_circle(round_scene_x(234), round_scene_y(257) + bob, round_scene_x(5), pink);

    // Head and seal-point ears.  The paired nested triangles read clearly at
    // 360 px while leaving the calendar unobscured.
    fill_circle_vertical_gradient(LCD_WIDTH / 2, round_scene_y(146) + bob, round_scene_x(75), rgb565(241, 220, 199),
                                  rgb565(197, 164, 139));
    draw_ear(round_scene_x(111), round_scene_y(80) + bob, 1, seal, pink);
    draw_ear(round_scene_x(249), round_scene_y(80) + bob, -1, seal, pink);
    fill_circle_vertical_gradient(LCD_WIDTH / 2, round_scene_y(142) + bob, round_scene_x(72), rgb565(255, 249, 234),
                                  rgb565(226, 197, 169));
    fill_circle_vertical_gradient(round_scene_x(134), round_scene_y(143) + bob, round_scene_x(31), rgb565(129, 99, 102),
                                  rgb565(66, 46, 51));
    fill_circle_vertical_gradient(round_scene_x(226), round_scene_y(143) + bob, round_scene_x(31), rgb565(129, 99, 102),
                                  rgb565(66, 46, 51));
    fill_circle_vertical_gradient(LCD_WIDTH / 2, round_scene_y(171) + bob, round_scene_x(26), rgb565(132, 101, 103),
                                  rgb565(68, 46, 52));
    fill_circle(LCD_WIDTH / 2, round_scene_y(165) + bob, round_scene_x(19), fur_shadow);

    // Blink briefly only every few seconds. The old 8-frame loop closed the
    // eyes roughly twice per second, which read as flicker rather than life.
    uint8_t blink_stage = s_pet_motion_enabled ? pet_blink_stage(s_pet_motion_tick) : 0u;
    if (blink_stage == 2u) {
        fill_rect(round_scene_x(119), round_scene_y(138) + bob,
                  round_scene_x(149), round_scene_y(144) + bob, blue_dark);
        fill_rect(round_scene_x(211), round_scene_y(138) + bob,
                  round_scene_x(241), round_scene_y(144) + bob, blue_dark);
    } else if (blink_stage == 1u) {
        fill_circle(round_scene_x(134), round_scene_y(145) + bob, round_scene_x(11), blue_dark);
        fill_circle(round_scene_x(226), round_scene_y(145) + bob, round_scene_x(11), blue_dark);
        fill_rect(round_scene_x(121), round_scene_y(132) + bob, round_scene_x(148), round_scene_y(142) + bob, rgb565(129, 99, 102));
        fill_rect(round_scene_x(213), round_scene_y(132) + bob, round_scene_x(240), round_scene_y(142) + bob, rgb565(129, 99, 102));
    } else {
        fill_circle(round_scene_x(134), round_scene_y(142) + bob, round_scene_x(17), blue_dark);
        fill_circle(round_scene_x(226), round_scene_y(142) + bob, round_scene_x(17), blue_dark);
        fill_circle(round_scene_x(134), round_scene_y(139) + bob, round_scene_x(12), blue);
        fill_circle(round_scene_x(226), round_scene_y(139) + bob, round_scene_x(12), blue);
        fill_circle(round_scene_x(140), round_scene_y(134) + bob, round_scene_x(5), shine);
        fill_circle(round_scene_x(232), round_scene_y(134) + bob, round_scene_x(5), shine);
    }
    fill_circle(LCD_WIDTH / 2, round_scene_y(167) + bob, round_scene_x(7), pink);
    fill_circle(round_scene_x(178), round_scene_y(165) + bob, round_scene_x(2), shine);
    fill_rect(round_scene_x(177), round_scene_y(174) + bob, round_scene_x(183), round_scene_y(184) + bob, seal);
    fill_rect(round_scene_x(163), round_scene_y(183) + bob, LCD_WIDTH / 2, round_scene_y(188) + bob, seal);
    fill_rect(LCD_WIDTH / 2, round_scene_y(183) + bob, round_scene_x(198), round_scene_y(188) + bob, seal);
    (void)bg;
}

static void draw_pet_frame_contents(bool redraw_background) {
    if (s_display_sleeping) return;
    uint16_t bg = state_color(s_pet_state);
    // The selected GUI pet pack still needs the shared clock/service/alarm
    // context painted around it.  `draw_remote_pet()` clears the frame before
    // compositing its transparent pixels and then adds those curved rings.
    if (ambient_visible_for_state() && s_remote_pet_frame_count && draw_remote_pet()) return;
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

    if (!strcmp(s_pet_state, "thinking")) {
        // Bread Compact shows the exact command phase beneath its thinking
        // face. Keep the same business feedback on EchoEar, using one short
        // CJK line placed in the lower circular safe zone. It is rendered as
        // part of the animation frame, so phase changes never race a pet draw.
        const char *stage = s_command_stage[0] ? s_command_stage : "正在处理";
        int stage_width = text24_width(stage, 8);
        int stage_x = (LCD_WIDTH - stage_width) / 2;
        if (stage_x < 42) stage_x = 42;
        draw_text24(stage_x, 32, stage, rgb565(236, 247, 255), bg);
    }
    // Keep the pet body anchored. Idle life now comes only from blinking and
    // the independently eased tail; moving the complete silhouette vertically
    // made the character look as if it were bouncing in place.
    int bob = 0;
    if (ragdoll) {
        draw_ragdoll_pet(bob, bg);
        if (ambient_visible_for_state()) draw_clock_calendar(bg);
        return;
    }
    // Keep the pet silhouette beneath the curved time band. The local cat is
    // deliberately 7/8 scale on the standby surface: before a selected pet
    // pack appears, its ears and head must leave a clear visual gap below the
    // date/status ring.
    const int pet_y_offset = 18;
    // The ambient header owns y=8..62 and the lower ring owns y=288..359.
    // Center/radius are shared with the ragdoll renderer so the whole circle
    // remains rounded across every selected pet and every bobbing frame.
    fill_circle_vertical_gradient(LCD_WIDTH / 2, PET_HALO_CENTER_Y + bob,
                                  scale_standby_cat_radius(PET_HALO_RADIUS),
                                  halo_top, halo_bottom);
    // Offset inner light produces a soft dimensional halo without storing or
    // decoding any bitmap asset.
    fill_circle_vertical_gradient(LCD_WIDTH / 2,
                                  PET_HALO_CENTER_Y - scale_standby_cat_radius(12) + bob,
                                  scale_standby_cat_radius(92),
                                  rgb565(153, 229, 255), rgb565(53, 126, 224));
    draw_standby_cat_ear(88, 64 + pet_y_offset, 1, face_shadow, ear);
    draw_standby_cat_ear(272, 64 + pet_y_offset, -1, face_shadow, ear);
    fill_circle_vertical_gradient(LCD_WIDTH / 2,
                                  scale_standby_cat_coordinate(170 + pet_y_offset + bob,
                                                               PET_HALO_CENTER_Y),
                                  scale_standby_cat_radius(103),
                                  face_shadow, face_deep);
    fill_circle_vertical_gradient(LCD_WIDTH / 2,
                                  scale_standby_cat_coordinate(164 + pet_y_offset + bob,
                                                               PET_HALO_CENTER_Y),
                                  scale_standby_cat_radius(100),
                                  face_light, face);
    // A restrained forehead sheen and lower-face shade add volume while
    // leaving the time and calendar rings visually dominant.
    fill_circle_vertical_gradient(LCD_WIDTH / 2,
                                  scale_standby_cat_coordinate(120 + pet_y_offset + bob,
                                                               PET_HALO_CENTER_Y),
                                  scale_standby_cat_radius(54),
                                  rgb565_lerp(face_light, shine, 110), face_light);
    uint8_t blink_stage = s_pet_motion_enabled ? pet_blink_stage(s_pet_motion_tick) : 0u;
    if (blink_stage == 2u) {
        fill_rect(scale_standby_cat_coordinate(116, 180), scale_standby_cat_coordinate(151 + pet_y_offset + bob, PET_HALO_CENTER_Y), scale_standby_cat_coordinate(164, 180), scale_standby_cat_coordinate(157 + pet_y_offset + bob, PET_HALO_CENTER_Y), ink);
        fill_rect(scale_standby_cat_coordinate(196, 180), scale_standby_cat_coordinate(151 + pet_y_offset + bob, PET_HALO_CENTER_Y), scale_standby_cat_coordinate(244, 180), scale_standby_cat_coordinate(157 + pet_y_offset + bob, PET_HALO_CENTER_Y), ink);
    } else if (blink_stage == 1u) {
        fill_rect(scale_standby_cat_coordinate(116, 180), scale_standby_cat_coordinate(147 + pet_y_offset + bob, PET_HALO_CENTER_Y), scale_standby_cat_coordinate(164, 180), scale_standby_cat_coordinate(157 + pet_y_offset + bob, PET_HALO_CENTER_Y), ink);
        fill_rect(scale_standby_cat_coordinate(196, 180), scale_standby_cat_coordinate(147 + pet_y_offset + bob, PET_HALO_CENTER_Y), scale_standby_cat_coordinate(244, 180), scale_standby_cat_coordinate(157 + pet_y_offset + bob, PET_HALO_CENTER_Y), ink);
    } else {
        draw_standby_cat_eye(140, 151 + pet_y_offset + bob, ink, shine);
        draw_standby_cat_eye(220, 151 + pet_y_offset + bob, ink, shine);
    }
    fill_circle_vertical_gradient(LCD_WIDTH / 2,
                                  scale_standby_cat_coordinate(190 + pet_y_offset + bob,
                                                               PET_HALO_CENTER_Y),
                                  scale_standby_cat_radius(15),
                                  rgb565(62, 79, 104), ink);
    fill_circle(scale_standby_cat_coordinate(176, 180),
                scale_standby_cat_coordinate(186 + pet_y_offset + bob, PET_HALO_CENTER_Y),
                scale_standby_cat_radius(4), rgb565(180, 205, 222));
    fill_rect(scale_standby_cat_coordinate(174, 180), scale_standby_cat_coordinate(204 + pet_y_offset + bob, PET_HALO_CENTER_Y),
              scale_standby_cat_coordinate(187, 180), scale_standby_cat_coordinate(211 + pet_y_offset + bob, PET_HALO_CENTER_Y), ink);
    fill_rect(scale_standby_cat_coordinate(160, 180), scale_standby_cat_coordinate(210 + pet_y_offset + bob, PET_HALO_CENTER_Y),
              scale_standby_cat_coordinate(200, 180), scale_standby_cat_coordinate(216 + pet_y_offset + bob, PET_HALO_CENTER_Y), ink);
    fill_circle_vertical_gradient(scale_standby_cat_coordinate(118, 180),
                                  scale_standby_cat_coordinate(191 + pet_y_offset + bob, PET_HALO_CENTER_Y),
                                  scale_standby_cat_radius(14),
                                  rgb565(255, 180, 164), blush);
    fill_circle_vertical_gradient(scale_standby_cat_coordinate(242, 180),
                                  scale_standby_cat_coordinate(191 + pet_y_offset + bob, PET_HALO_CENTER_Y),
                                  scale_standby_cat_radius(14),
                                  rgb565(255, 180, 164), blush);
    if (!strcmp(s_pet_state, "thinking")) {
        // A restrained orbit above the head is visible without competing with
        // the time band. The changing brightness gives the state an obvious
        // direction even on the small round LCD.
        const int dot_x[3] = {round_scene_x(158), round_scene_x(180), round_scene_x(202)};
        const int active = (int)((s_pet_motion_tick / 4u) % 3u);
        for (int i = 0; i < 3; ++i) {
            uint16_t dot = i == active ? rgb565(244, 250, 255) : rgb565(142, 190, 255);
            fill_circle(dot_x[i], round_scene_y(82),
                        round_scene_x(i == active ? 5 : 3), dot);
        }
    }
    fill_circle_vertical_gradient(LCD_WIDTH / 2,
                                  scale_standby_cat_coordinate(236 + pet_y_offset + bob,
                                                               PET_HALO_CENTER_Y),
                                  scale_standby_cat_radius(39),
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
    if (s_display_sleeping || s_recording_active || s_response_active || s_message_active ||
        s_alarm_visual_active ||
        s_setup_qrcode_visible ||
        (s_command_display_locked && ambient_visible_for_state())) return;
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    if (s_display_sleeping || s_recording_active || s_response_active || s_message_active ||
        s_alarm_visual_active ||
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
        esp_err_t draw_err = present_pet_frame_delta_sync(frame);
        if (draw_err == ESP_OK) {
            s_front_framebuffer = s_next_framebuffer;
            s_front_frame_valid = true;
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

static bool draw_remote_pet(void) {
    if (!s_remote_pet_has_visible_pixels || !s_remote_pet_frame_count ||
        !s_remote_pet_width || !s_remote_pet_height) return false;
    // Keep ordinary redraws (clock/service updates) on the same pose that was
    // actually presented by the animation worker.  This is deliberately not a
    // wall-clock value: a delayed frame must never make the pet teleport.
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
    uint16_t bg = state_color(s_pet_state);
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    int target_w = (int)s_remote_pet_width;
    int target_h = (int)s_remote_pet_height;
    int left = (LCD_WIDTH - target_w) / 2;
    int top = 72 + (220 - target_h) / 2;
    for (int y = 0; y < target_h; ++y) {
        for (int x = 0; x < target_w; ++x) {
            size_t source_index = ((size_t)y * s_remote_pet_width + (size_t)x) * 3u;
            uint16_t first_pet = (uint16_t)source[source_index] |
                                 (uint16_t)((uint16_t)source[source_index + 1] << 8);
            uint16_t second_pet = (uint16_t)next_source[source_index] |
                                  (uint16_t)((uint16_t)next_source[source_index + 1] << 8);
            uint32_t inverse_mix = 256u - mix;
            uint32_t first_alpha = source[source_index + 2];
            uint32_t second_alpha = next_source[source_index + 2];
            uint8_t alpha = (uint8_t)((first_alpha * inverse_mix +
                                       second_alpha * mix + 128u) >> 8);
            uint16_t background_wire = s_render_target
                ? s_render_target[(size_t)(top + y) * LCD_WIDTH + left + x]
                : bg;
            uint16_t background = (uint16_t)((background_wire << 8) | (background_wire >> 8));
            if (alpha == 0) {
                s_line[x] = background_wire;
            } else {
                // Interpolate premultiplied color, then composite once. Straight
                // RGB interpolation lets the arbitrary RGB stored in a fully
                // transparent pixel darken a moving antialiased edge.
                uint32_t premul_r = ((((first_pet >> 11) & 0x1fu) * first_alpha * inverse_mix) +
                                     (((second_pet >> 11) & 0x1fu) * second_alpha * mix) + 128u) >> 8;
                uint32_t premul_g = ((((first_pet >> 5) & 0x3fu) * first_alpha * inverse_mix) +
                                     (((second_pet >> 5) & 0x3fu) * second_alpha * mix) + 128u) >> 8;
                uint32_t premul_b = (((first_pet & 0x1fu) * first_alpha * inverse_mix) +
                                     ((second_pet & 0x1fu) * second_alpha * mix) + 128u) >> 8;
                uint32_t inv = 255u - alpha;
                uint32_t br = (background >> 11) & 0x1fu, bgc = (background >> 5) & 0x3fu, bb = background & 0x1fu;
                uint16_t blended = (uint16_t)((((premul_r + br * inv + 127u) / 255u) << 11) |
                                              (((premul_g + bgc * inv + 127u) / 255u) << 5) |
                                              ((premul_b + bb * inv + 127u) / 255u));
                s_line[x] = (uint16_t)((blended << 8) | (blended >> 8));
            }
        }
        if (s_render_target) {
            memcpy(s_render_target + (size_t)(top + y) * LCD_WIDTH + left,
                   s_line, (size_t)target_w * sizeof(uint16_t));
        } else if (draw_bitmap_sync(left, top + y, left + target_w, top + y + 1, s_line) != ESP_OK) return false;
    }
    if (ambient_visible_for_state()) draw_clock_calendar(bg);
    return true;
}

static bool remote_pet_target_size(size_t width, size_t height,
                                   size_t *out_width, size_t *out_height) {
    if (!width || !height || !out_width || !out_height) return false;
    size_t target_width = 220u;
    size_t target_height = target_width * height / width;
    if (target_height > 220u) {
        target_height = 220u;
        target_width = target_height * width / height;
    }
    if (!target_width || !target_height) return false;
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
                (y0 * source_width + x0) * 3u, (y0 * source_width + x1) * 3u,
                (y1 * source_width + x0) * 3u, (y1 * source_width + x1) * 3u,
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
    }
}

static void compose_recording_meter(const uint16_t wave_levels[RECORDING_WAVE_COLUMNS],
                                    uint16_t recording_smoothed_level,
                                    bool recording_paused, uint16_t bg,
                                    uint16_t cyan, uint16_t muted) {
    enum { RECORDING_VISUAL_BARS = RECORDING_WAVE_COLUMNS };
    // Keep Bread Compact's actual bar rhythm, not merely its column count:
    // 24 five-pixel bars advance on an eight-pixel pitch.  Stretching that to
    // 12/7 on EchoEar made the waveform read as a different visual language
    // and made each 32 ms update transfer substantially more pixels.  This
    // 189 px span is centred inside the round screen's safe chord.
    const int wave_left = 84;
    const int wave_pitch = 8;
    const int wave_bar_width = 5;
    const int wave_center = 205;
    const int wave_half_height = 42;
    fill_rect(wave_left - 4, wave_center,
              wave_left + (RECORDING_VISUAL_BARS - 1) * wave_pitch +
                  wave_bar_width + 4,
              wave_center + 1, muted);
    for (int bar = 0; bar < RECORDING_VISUAL_BARS; ++bar) {
        uint16_t level = recording_paused ? 0 : wave_levels[bar];
        // Keep Bread Compact's five-pixel quiet bar (2 px above/below the
        // centre rule). EchoEar only adapts placement for the round aperture.
        int half = 2 + (int)(level * wave_half_height / 1000u);
        if (half > wave_half_height) half = wave_half_height;
        int x = wave_left + bar * wave_pitch;
        fill_rect(x, wave_center - half, x + wave_bar_width,
                  wave_center + half + 1, recording_paused ? muted : cyan);
    }
    char level_label[20];
    snprintf(level_label, sizeof(level_label), "MIC %u%%",
             (unsigned)(recording_smoothed_level / 10u));
    draw_centered_text(260, level_label, recording_paused ? muted : cyan, bg);
}

static void draw_recording_meter_visual(void) {
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    bool recording_active;
    bool recording_paused;
    bool recording_frame_baseline;
    uint16_t recording_smoothed_level;
    uint16_t wave_levels[RECORDING_WAVE_COLUMNS] = {0};
    taskENTER_CRITICAL(&s_state_lock);
    recording_active = s_recording_active;
    recording_paused = s_recording_paused;
    recording_smoothed_level = s_recording_smoothed_level;
    memcpy(wave_levels, s_recording_wave_levels, sizeof(wave_levels));
    recording_frame_baseline = s_recording_frame_baseline;
    taskEXIT_CRITICAL(&s_state_lock);

    if (!recording_active || recording_paused || !recording_frame_baseline ||
        !s_front_frame_valid || !s_framebuffers[s_front_framebuffer]) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        if (recording_active) draw_recording_visual();
        return;
    }

    uint16_t *frame = s_framebuffers[s_next_framebuffer];
    if (frame) {
        // Bread Compact starts from its previous composed frame and touches
        // only the live meter band. Keep that exact ownership model here: it
        // avoids re-rasterising static header/timer/prompt pixels every 32 ms.
        memcpy(frame, s_framebuffers[s_front_framebuffer], LCD_FRAMEBUFFER_BYTES);
        s_render_target = frame;
        fill_rect(72, 157, 288, 280, rgb565(10, 19, 30));
        compose_recording_meter(wave_levels, recording_smoothed_level, false,
                                rgb565(10, 19, 30), rgb565(72, 205, 220),
                                rgb565(91, 118, 138));
        s_render_target = NULL;
        esp_err_t draw_err = present_pet_frame_delta_sync(frame);
        if (draw_err == ESP_OK) {
            s_front_framebuffer = s_next_framebuffer;
            s_front_frame_valid = true;
            s_recording_frame_baseline = true;
            s_next_framebuffer ^= 1u;
            ++s_presented_frames;
            s_recording_meter_dirty = false;
        } else {
            s_recording_meter_dirty = true;
            ESP_LOGE(TAG, "recording meter present failed: %s", esp_err_to_name(draw_err));
        }
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

static void draw_recording_visual(void) {
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    // The animation task may have queued this draw just before capture ended.
    // Never allow that old waveform frame to replace the thinking/result page.
    bool recording_active;
    bool recording_paused;
    bool recording_is_meeting;
    uint32_t recording_elapsed_seconds;
    uint16_t recording_smoothed_level;
    uint16_t wave_levels[RECORDING_WAVE_COLUMNS] = {0};
    bool recording_frame_baseline;
    // The capture task updates the meter while this renderer prepares a frame.
    // Take one coherent state snapshot, as Bread's LCD-serialized renderer
    // does, so a pause/mode/timer transition cannot mix two recorder states in
    // one presented image.
    taskENTER_CRITICAL(&s_state_lock);
    recording_active = s_recording_active;
    recording_paused = s_recording_paused;
    recording_is_meeting = s_recording_is_meeting;
    recording_elapsed_seconds = s_recording_elapsed_seconds;
    recording_smoothed_level = s_recording_smoothed_level;
    memcpy(wave_levels, s_recording_wave_levels, sizeof(wave_levels));
    recording_frame_baseline = s_recording_frame_baseline;
    taskEXIT_CRITICAL(&s_state_lock);
    if (!recording_active) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    // Compose the complete recording surface off-screen. The first recorder
    // frame must be a full foreground hand-off; later frames can compare
    // against this same recorder scene so they only send the timer/meter
    // regions which genuinely changed.
    uint16_t *frame = s_framebuffers[s_next_framebuffer];
    if (frame) s_render_target = frame;
    uint16_t bg = rgb565(10, 19, 30);
    uint16_t red = rgb565(241, 76, 85);
    uint16_t amber = rgb565(244, 178, 58);
    uint16_t cyan = rgb565(72, 205, 220);
    // Keep Bread Compact's muted recording chrome exactly; the round layout
    // changes geometry, not the recorder's visual hierarchy.
    uint16_t muted = rgb565(91, 118, 138);
    uint16_t fg = rgb565(244, 250, 255);
    uint16_t accent = recording_paused ? amber : red;
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    // Keep Bread Compact's visual grammar (thin recording rules, square red
    // indicator, then state / mode / timer) while placing it inside EchoEar's
    // circular safe area. The prior thick bands and breathing red disc made
    // the same recording state read as a different product surface.
    // A 360 px framebuffer does not mean its corner pixels are visible: at
    // these high/low rows the circular aperture only exposes a ~216 px chord.
    // Keep Bread's paired thin recorder rules wholly inside that chord.  The
    // matched coordinates leave a small bezel margin at every corner; the
    // prior bottom-right pixel sat just outside the physical circle.
    fill_rect(78, 34, LCD_WIDTH - 78, 38, accent);
    fill_rect(78, LCD_HEIGHT - 38, LCD_WIDTH - 78, LCD_HEIGHT - 34, accent);
    // Bread's active marker is a 20 px outer square with an 8 px light core.
    // Preserve that scale on the round panel; only its position moves inward
    // for the visible chord.
    fill_rect(58, 52, 78, 72, accent);
    fill_rect(64, 58, 72, 66, rgb565(255, 235, 238));
    draw_text24_centered_safe(48, recording_paused ? "已暂停" : "正在听取",
                              198, fg, bg);
    draw_text24_centered_safe(84, recording_is_meeting ? "会议录音" : "语音指令",
                              198, recording_paused ? amber : cyan, bg);
    uint32_t minutes = recording_elapsed_seconds / 60;
    uint32_t seconds = recording_elapsed_seconds % 60;
    char elapsed[16];
    snprintf(elapsed, sizeof(elapsed), "%02lu:%02lu", (unsigned long)minutes, (unsigned long)seconds);
    draw_centered_text(122, elapsed, fg, bg);

    // Match Bread Compact's clear, symmetric meter: the same 24 filtered-level
    // history columns advance from left to right around one quiet centre line.
    compose_recording_meter(wave_levels, recording_smoothed_level,
                            recording_paused, bg, cyan, muted);
    // Bread Compact keeps one calm action line below the meter.  The previous
    // EchoEar layout added a second state row just four pixels above it, so the
    // two 24 px glyph rows visibly collided. State is already explicit in the
    // header and mode row; retain the action only, inside the round panel's
    // lower safe zone.
    if (recording_is_meeting) {
        draw_text24_centered_safe(298, "点屏停止保存", 180, muted, bg);
    } else {
        draw_text24_centered_safe(298, "说完自动处理", 180, muted, bg);
    }
    s_render_target = NULL;
    if (frame) {
        // First frame: a complete scene prevents result/upload/standby pixels
        // leaking into the recorder. Once it is on screen, retain the recorder
        // frame as the delta baseline. This preserves Bread's live waveform
        // cadence without re-sending 259 KB every 32 ms on the S3 QSPI panel.
        esp_err_t draw_err = recording_frame_baseline
                                 ? present_pet_frame_delta_sync(frame)
                                 : present_frame_sync(frame);
        if (draw_err == ESP_OK) {
            s_front_framebuffer = s_next_framebuffer;
            s_front_frame_valid = true;
            s_recording_frame_baseline = true;
            s_next_framebuffer ^= 1u;
            ++s_presented_frames;
            s_recording_meter_dirty = false;
        } else {
            s_recording_meter_dirty = true;
            ESP_LOGE(TAG, "recording frame present failed: %s", esp_err_to_name(draw_err));
        }
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

static void pet_animation_task(void *arg) {
    (void)arg;
    uint32_t rendered_ambient_revision = s_ambient_revision;
    uint32_t rendered_motion_signature = pet_motion_signature();
    int64_t next_diagnostic_us = esp_timer_get_time() + 5000000;
    while (true) {
        // Schedule from the completed presentation.  vTaskDelayUntil() tries
        // to catch up after a slow SPI/TLS interval, causing several remote
        // poses to be drawn back-to-back.  That looks like a jump and can also
        // leave a trail on this round panel.  The authored remote animation
        // gets Bread Compact's 80 ms cadence; native idle motion keeps its
        // quieter 150 ms rhythm.
        // The 80 ms cadence belongs only to the interpolated standby pet.
        // Recording advances at the Bread-compatible 32 ms PCM history rate;
        // it is kept independent from remote-pet timing so a pet pack cannot
        // make the recorder skip live waveform columns.
        bool remote_pack_active = s_remote_pet_has_visible_pixels &&
                                  s_remote_pet_frame_count > 1 &&
                                  s_remote_pet_frame_ms &&
                                  s_idle_pet_visible &&
                                  ambient_visible_for_state() &&
                                  !s_recording_active && !s_response_active &&
                                  !s_alarm_visual_active && !s_setup_qrcode_visible;
        const TickType_t frame_delay = s_recording_active
                                           ? pdMS_TO_TICKS(RECORDING_RENDER_FRAME_MS)
                                           : pdMS_TO_TICKS(remote_pack_active
                                                               ? REMOTE_PET_RENDER_FRAME_MS
                                                               : PET_ANIMATION_FRAME_MS);
        if (ulTaskNotifyTake(pdTRUE, frame_delay) != 0) break;
        // Read the revision after the delay. Reading it at the end of a frame
        // can acknowledge a clock update that arrived while the previous DMA
        // transfer was running even though that second was never rendered.
        uint32_t pending_ambient_revision = s_ambient_revision;
        if (s_setup_qrcode_visible) {
            // The QR code and its white quiet zone must stay pixel-stable for
            // phone cameras. Nothing else should draw while setup is active.
        } else if (s_recording_active) {
            // Recording frames are driven synchronously by
            // board_port_set_audio_level(): one completed 512-sample capture
            // block produces one waveform frame, exactly as on Bread Compact.
            // The idle animation scheduler must not add a second, phase-shifted
            // draw between two audio blocks. If a 32 ms meter frame was
            // blocked by a foreground LCD transfer, render the latest state
            // once here rather than losing that audio history update forever.
            if (s_recording_meter_dirty) draw_recording_meter_visual();
        } else if (s_message_active && s_ready_prompt_expires_us == 0) {
            // A regular status notice is deliberately static.  Ready prompts
            // carry the one explicit expiry which returns to the ambient pet.
        } else if (!s_command_display_locked && s_ready_prompt_expires_us > 0 &&
                   esp_timer_get_time() >= s_ready_prompt_expires_us) {
            s_ready_prompt_expires_us = 0;
            s_message_active = false;
            // The pet profile has already been received from MaClaw GUI. A
            // fresh idle draw removes the temporary instruction text as well.
            strlcpy(s_pet_state, "idle", sizeof(s_pet_state));
            s_idle_pet_visible = true;
            s_idle_pet_sleep_expires_us = esp_timer_get_time() + IDLE_PET_SLEEP_TIMEOUT_US;
            draw_pet();
        } else if (!s_display_sleeping && s_pet_motion_enabled &&
                   (s_idle_pet_visible || !strcmp(s_pet_state, "thinking"))) {
            s_pet_frame = (uint8_t)((s_pet_frame + 1u) % 8u);
            ++s_pet_motion_tick;
            bool remote_animation = s_remote_pet_has_visible_pixels &&
                                    s_remote_pet_frame_count > 1 &&
                                    s_remote_pet_frame_ms &&
                                    ambient_visible_for_state();
            if (remote_animation) {
                // Advance exactly once for the frame we are about to present.
                // We deliberately do not make up lost ticks after a blocked
                // transfer: consistent motion matters more than wall-clock
                // speed on a small LCD.
                uint64_t loop_ms = (uint64_t)s_remote_pet_frame_ms *
                                   s_remote_pet_frame_count;
                s_remote_pet_animation_elapsed_ms += REMOTE_PET_RENDER_FRAME_MS;
                if (loop_ms) s_remote_pet_animation_elapsed_ms %= loop_ms;
            }
            uint32_t motion_signature = pet_motion_signature();
            if (motion_signature != rendered_motion_signature ||
                pending_ambient_revision != rendered_ambient_revision) {
                // Compose only when visible geometry or ambient text changed.
                // Remote packs use 80 ms interpolated poses; native geometry
                // only repaints when it visibly changes.
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
    if (s_pet_animation_stopped) xSemaphoreGive(s_pet_animation_stopped);
    vTaskDelete(NULL);
}

static bool touch_read(bool *pressed, uint8_t *gesture) {
    if (pressed) *pressed = false;
    if (gesture) *gesture = 0;
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    if (!s_cst9217_touch) return false;
    if (esp_lcd_touch_read_data(s_cst9217_touch) != ESP_OK) return false;
    esp_lcd_touch_point_data_t point = {0};
    uint8_t points = 0;
    bool down = esp_lcd_touch_get_data(s_cst9217_touch, &point, &points, 1) == ESP_OK;
    if (pressed) *pressed = down && points != 0;
    return true;
#else
    if (!s_touch) return false;

    // Read the gesture ID and finger count in one transaction. CST816 reports
    // double-click as 0x0B; using that hardware result avoids guessing whether
    // two close contacts are a controller echo or a real user double tap.
    uint8_t reg = 0x01;
    uint8_t status[2] = {0};
    if (i2c_master_transmit_receive(s_touch, &reg, 1, status, sizeof(status), 50) != ESP_OK) {
        return false;
    }
    if (gesture) *gesture = status[0];
    if (pressed) *pressed = (status[1] & 0x0Fu) != 0;
    return true;
#endif
}

static void button_task(void *arg) {
    (void)arg;
    // EchoEar-2ST exposes BOOT on ESP-IDF GPIO0. The separately labelled
    // PWR/FUNCTION key in Zephyr's gpio1 bank is the board power-control key
    // and does not provide a dependable application GPIO while running.
    // A panel tap and BOOT still share gesture classification, but the source
    // remains attached to every callback so enclosure-specific features never
    // mistake the BOOT GPIO for a touch.
    bool button_pressed = gpio_get_level(FUNCTION_BUTTON) == 0;
    bool panel_pressed = false;
#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
    touch_read(&panel_pressed, NULL);
#endif
    bool pressed = button_pressed || panel_pressed;
    board_input_source_t gesture_source =
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
        BOARD_INPUT_SOURCE_ACTIVATE_KEY;
#else
        panel_pressed ? BOARD_INPUT_SOURCE_TOUCH : BOARD_INPUT_SOURCE_OTHER_KEY;
#endif
    board_input_source_t pending_source = BOARD_INPUT_SOURCE_UNKNOWN;
    int64_t pressed_at_us = pressed ? esp_timer_get_time() : 0;
    int64_t released_at_us = 0;
    bool long_sent = false;
    bool short_pending = false;
    bool native_double_sent = false;
    uint32_t command_gesture_revision = s_command_gesture_revision;
    ESP_LOGI(TAG, "interaction monitor ready: boot_gpio=%d idle_level=%d touch=%s irq=%d",
             FUNCTION_BUTTON, gpio_get_level(FUNCTION_BUTTON),
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
             s_cst9217_touch ? "yes" : "no",
#else
             s_touch ? "yes" : "no",
#endif
             TOUCH_IRQ);
    while (true) {
        bool now_button_pressed = gpio_get_level(FUNCTION_BUTTON) == 0;
        bool now_panel_pressed = false;
        uint8_t now_touch_gesture = 0;
#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
        touch_read(&now_panel_pressed, &now_touch_gesture);
#endif
        bool now_pressed = now_button_pressed || now_panel_pressed;
        int64_t now_us = esp_timer_get_time();
        if (command_gesture_revision != s_command_gesture_revision) {
            // Entering the thinking phase starts a completely fresh gesture
            // window. In particular, discard the tap that originally started
            // command recording; otherwise it can be mistaken for the first
            // half of a later cancel double tap.
            command_gesture_revision = s_command_gesture_revision;
            pressed = now_pressed;
            gesture_source =
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                BOARD_INPUT_SOURCE_ACTIVATE_KEY;
#else
                now_panel_pressed ? BOARD_INPUT_SOURCE_TOUCH : BOARD_INPUT_SOURCE_OTHER_KEY;
#endif
            pending_source = BOARD_INPUT_SOURCE_UNKNOWN;
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
            if (s_on_button) {
                // Some CST816 revisions report the native gesture in the same
                // scan as the first observable contact. Emit the physical edge
                // here too so latency-sensitive touch surfaces never wait for
                // a later debounced state transition.
                s_on_button(BOARD_INPUT_PRESSED, BOARD_INPUT_SOURCE_TOUCH,
                            s_on_press_arg);
                s_on_button(BOARD_BUTTON_DOUBLE, BOARD_INPUT_SOURCE_TOUCH,
                            s_on_press_arg);
            }
        }
        if (now_pressed != pressed) {
            vTaskDelay(pdMS_TO_TICKS(25));
            now_button_pressed = gpio_get_level(FUNCTION_BUTTON) == 0;
            now_touch_gesture = 0;
#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
            touch_read(&now_panel_pressed, &now_touch_gesture);
#endif
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
                    gesture_source =
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
                        BOARD_INPUT_SOURCE_ACTIVATE_KEY;
#else
                        now_panel_pressed ? BOARD_INPUT_SOURCE_TOUCH : BOARD_INPUT_SOURCE_OTHER_KEY;
#endif
                    if (now_touch_gesture != 0x0B) native_double_sent = false;
                    ESP_LOGI(TAG, "button/touch down");
                    if (now_touch_gesture != 0x0B) {
                        emit_button_input(BOARD_INPUT_PRESSED, gesture_source);
                    }
                } else {
                    uint32_t held_ms = pressed_at_us > 0
                                           ? (uint32_t)((now_us - pressed_at_us) / 1000)
                                           : 0;
                    ESP_LOGI(TAG, "button/touch up: held=%lu ms", (unsigned long)held_ms);
                    uint32_t minimum_tap_ms =
                        (gesture_source == BOARD_INPUT_SOURCE_TOUCH &&
                         s_command_cancel_enabled) ? 15 : 30;
                    if (!long_sent && held_ms >= minimum_tap_ms) {
                        int64_t since_previous_us = now_us - released_at_us;
                        // Keep completion timing identical to Bread Compact:
                        // a deliberate double tap is a 500 ms gesture and a
                        // single tap becomes available immediately after that
                        // window. CST816's native 0x0B still takes the faster
                        // path above; the 100 ms lower bound below continues
                        // to reject its duplicate raw contact as a false tap.
                        int64_t double_window_us = INPUT_DOUBLE_WINDOW_US;
                        if (!native_double_sent && short_pending &&
                            pending_source == gesture_source &&
                            ((gesture_source != BOARD_INPUT_SOURCE_TOUCH &&
                              since_previous_us <= double_window_us) ||
                             (gesture_source == BOARD_INPUT_SOURCE_TOUCH &&
                              since_previous_us >= 100000 &&
                              since_previous_us <= double_window_us))) {
                            short_pending = false;
                            native_double_sent = true;
                            if (gesture_source == BOARD_INPUT_SOURCE_TOUCH) {
                                s_touch_gesture_consumed = true;
                                s_touch_gesture_released_at_us = 0;
                            }
                            ESP_LOGI(TAG, "button gesture: double (%s timing gap=%lld ms)",
                                     gesture_source == BOARD_INPUT_SOURCE_TOUCH ? "touch" : "button",
                                     (long long)(since_previous_us / 1000));
                            emit_button_input(BOARD_BUTTON_DOUBLE, gesture_source);
                        } else if (gesture_source == BOARD_INPUT_SOURCE_TOUCH &&
                                   s_command_cancel_enabled &&
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
                            pending_source = gesture_source;
                            released_at_us = now_us;
                        }
                    }
                }
            }
        }
        if (pressed && !long_sent && pressed_at_us > 0 &&
            now_us - pressed_at_us >= INPUT_LONG_HOLD_US) {
            long_sent = true;
            short_pending = false;
            ESP_LOGI(TAG, "button gesture: long");
            emit_button_input(BOARD_BUTTON_LONG, gesture_source);
        }
        int64_t pending_window_us = INPUT_DOUBLE_WINDOW_US;
        if (!pressed && short_pending && !s_touch_gesture_consumed &&
            now_us - released_at_us > pending_window_us) {
            short_pending = false;
            ESP_LOGI(TAG, "button gesture: short");
            emit_button_input(BOARD_BUTTON_SHORT, pending_source);
            pending_source = BOARD_INPUT_SOURCE_UNKNOWN;
        }
        /* The lifecycle owner wakes the scanner through its task notification;
         * do not use a volatile flag as a cross-task stop protocol. */
        if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(15)) != 0) break;
    }

    s_on_button = NULL;
    s_on_press_arg = NULL;
    if (s_button_task_stopped) xSemaphoreGive(s_button_task_stopped);
    vTaskDelete(NULL);
}
esp_err_t board_port_init(board_port_button_cb_t on_button, void *arg) {
    s_on_button = on_button;
    s_on_press_arg = arg;
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    s_boot_network_window_active = true;
    s_boot_network_toggle_requested = false;
#endif
    s_background_tasks_lock = xSemaphoreCreateMutex();
    if (!s_background_tasks_lock) return ESP_ERR_NO_MEM;
    s_lcd_mutex = xSemaphoreCreateRecursiveMutex();
    if (!s_lcd_mutex) return ESP_ERR_NO_MEM;
    s_audio_mutex = xSemaphoreCreateMutex();
    if (!s_audio_mutex) return ESP_ERR_NO_MEM;
    s_lcd_transfer_done = xSemaphoreCreateBinary();
    if (!s_lcd_transfer_done) return ESP_ERR_NO_MEM;

    if (LCD_BACKLIGHT != GPIO_NUM_NC) {
        ESP_ERROR_CHECK(gpio_set_direction(LCD_BACKLIGHT, GPIO_MODE_OUTPUT));
        ESP_ERROR_CHECK(gpio_set_level(LCD_BACKLIGHT, 0));
    }
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    const spi_bus_config_t bus_config = {
        .sclk_io_num = LCD_SCLK, .data0_io_num = LCD_DATA0,
        .data1_io_num = LCD_DATA1, .data2_io_num = LCD_DATA2,
        .data3_io_num = LCD_DATA3, .max_transfer_sz = LCD_FRAMEBUFFER_BYTES,
        .flags = SPICOMMON_BUSFLAG_QUAD,
    };
#else
    const spi_bus_config_t bus_config = ST77916_PANEL_BUS_QSPI_CONFIG(
        LCD_SCLK, LCD_DATA0, LCD_DATA1, LCD_DATA2, LCD_DATA3, LCD_FRAMEBUFFER_BYTES);
#endif
    ESP_ERROR_CHECK(spi_bus_initialize(LCD_HOST, &bus_config, SPI_DMA_CH_AUTO));
    esp_lcd_panel_io_handle_t io = NULL;
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    esp_lcd_panel_io_spi_config_t io_config = {
        .cs_gpio_num = LCD_CS, .dc_gpio_num = GPIO_NUM_NC, .spi_mode = 0,
        .pclk_hz = LCD_QSPI_PCLK_HZ, .trans_queue_depth = 10,
        .lcd_cmd_bits = 32, .lcd_param_bits = 8, .on_color_trans_done = lcd_color_transfer_done,
        .user_ctx = s_lcd_transfer_done, .flags = {.quad_mode = true},
    };
#else
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
    // The component macro defaults to 40 MHz.  The physical EchoEar display
    // path needs a conservative clock to prevent vertical stripe corruption.
    io_config.pclk_hz = LCD_QSPI_PCLK_HZ;
    io_config.flags.psram_dma_direct = false;
#endif
    ESP_ERROR_CHECK(esp_lcd_new_panel_io_spi(LCD_HOST, &io_config, &io));
    s_panel_io = io;
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    co5300_vendor_config_t vendor_config = {
        .init_cmds = s_waveshare_co5300_init_cmds,
        .init_cmds_size = sizeof(s_waveshare_co5300_init_cmds) /
                          sizeof(s_waveshare_co5300_init_cmds[0]),
        .flags = {.use_qspi_interface = 1},
    };
#else
    st77916_vendor_config_t vendor_config = {
        .init_cmds = s_echoear_init_cmds,
        .init_cmds_size = sizeof(s_echoear_init_cmds) / sizeof(s_echoear_init_cmds[0]),
        .flags = {.use_qspi_interface = 1},
    };
#endif
    const esp_lcd_panel_dev_config_t panel_config = {
        .reset_gpio_num = LCD_RESET,
        .rgb_ele_order = LCD_RGB_ELEMENT_ORDER_RGB,
        .bits_per_pixel = 16,
        .vendor_config = &vendor_config,
    };
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    ESP_ERROR_CHECK(esp_lcd_new_panel_co5300(io, &panel_config, &s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_set_gap(s_panel, 6, 0));
#else
    ESP_ERROR_CHECK(esp_lcd_new_panel_st77916(io, &panel_config, &s_panel));
#endif
    ESP_ERROR_CHECK(esp_lcd_panel_reset(s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_init(s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_disp_on_off(s_panel, true));
    if (LCD_BACKLIGHT != GPIO_NUM_NC) ESP_ERROR_CHECK(gpio_set_level(LCD_BACKLIGHT, 1));

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
    // The two curved text regions are composed serially.  The larger AMOLED
    // header cannot be a static DMA object without starving internal memory,
    // while panel IO is already configured to bounce PSRAM sources safely.
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    const uint32_t overlay_caps = MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT;
#else
    const uint32_t overlay_caps = MALLOC_CAP_INTERNAL | MALLOC_CAP_DMA | MALLOC_CAP_8BIT;
#endif
    s_ambient_overlay = heap_caps_malloc(AMBIENT_OVERLAY_BYTES, overlay_caps);
    if (!s_ambient_overlay) {
        ESP_LOGE(TAG, "cannot allocate %u-byte ambient overlay", (unsigned)AMBIENT_OVERLAY_BYTES);
        return ESP_ERR_NO_MEM;
    }
    ESP_LOGI(TAG, "ambient overlay ready: %u bytes in %s",
             (unsigned)AMBIENT_OVERLAY_BYTES,
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
             "PSRAM"
#else
             "internal DMA memory"
#endif
    );
    // Do not submit any full-screen transfer until networking has completed
    // its fragile association phase.  On this S3, simultaneous LCD GDMA and
    // Wi-Fi ROM initialisation can corrupt Wi-Fi's timer callback state.
    // board_port_set_command_display_lock(false) starts the pet task after
    // the startup surface is released.

    // Initialize the shared I2C bus early so both touch interaction and later
    // microphone capture can use it without reconfiguring GPIO11/GPIO12.
    esp_err_t input_i2c_err = audio_init();
    if (input_i2c_err != ESP_OK) {
        ESP_LOGW(TAG, "touch/audio init deferred: %s", esp_err_to_name(input_i2c_err));
    }
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    else {
        device_motion_sample_t motion = {
            .struct_size = sizeof(motion),
            .abi_version = DEVICE_MOTION_SAMPLE_ABI_VERSION,
        };
        if (board_port_motion_get_sample(&motion) == ESP_OK) {
            ESP_LOGI(TAG, "Motion HAL sample: a=(%ld,%ld,%ld)mg g=(%ld,%ld,%ld)mdps",
                     (long)motion.acceleration_mg_x, (long)motion.acceleration_mg_y,
                     (long)motion.acceleration_mg_z, (long)motion.angular_rate_mdps_x,
                     (long)motion.angular_rate_mdps_y, (long)motion.angular_rate_mdps_z);
        } else {
            ESP_LOGW(TAG, "Motion HAL sample unavailable after QMI8658 initialization");
        }
    }
#endif

    gpio_config_t button_cfg = {
        .pin_bit_mask = 1ULL << FUNCTION_BUTTON,
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    ESP_ERROR_CHECK(gpio_config(&button_cfg));
    s_button_task_stopped = xSemaphoreCreateBinary();
    if (!s_button_task_stopped) return ESP_ERR_NO_MEM;
    BaseType_t button_task_created = xTaskCreate(button_task, "echoear_button", 3072, NULL, 4,
                                                  &s_button_task);
    if (button_task_created != pdPASS) {
        ESP_LOGE(TAG, "cannot start button task");
        vSemaphoreDelete(s_button_task_stopped);
        s_button_task_stopped = NULL;
        return ESP_ERR_NO_MEM;
    }
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    ESP_LOGI(TAG, "Waveshare 1.75C CO5300 round AMOLED, CST9217 touch and boot key ready");
#else
    ESP_LOGI(TAG, "EchoEar-2ST ST77916 QSPI display and function button ready");
#endif
    return ESP_OK;
}

bool board_port_wait_for_boot_network_toggle(uint32_t window_ms) {
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
    int64_t deadline_us = esp_timer_get_time() + (int64_t)window_ms * 1000;
    while (!s_boot_network_toggle_requested && esp_timer_get_time() < deadline_us) {
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    bool toggle = s_boot_network_toggle_requested;
    s_boot_network_window_active = false;
    s_boot_network_toggle_requested = false;
    ESP_LOGI(TAG, "Fangtang boot network choice: %s",
             toggle ? "4G selected" : "Wi-Fi selected");
    return toggle;
#else
    (void)window_ms;
    return false;
#endif
}

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

esp_err_t board_port_prepare_cellular_transport(void) {
    return ESP_ERR_NOT_SUPPORTED;
}

bool board_port_cancel_cellular_foreground_request(void) {
    return false;
}

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

void board_port_show_startup_screen(void) {
    if (!s_panel || !s_lcd_mutex) return;
    if (xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    if (s_alarm_visual_active) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    s_command_display_locked = true;
    s_response_active = false;
    // draw_pet() intentionally refuses to paint while the display lock is set.
    // Paint this explicit boot frame directly under the LCD mutex instead.
    uint16_t *frame = s_framebuffers[s_next_framebuffer];
    if (frame) s_render_target = frame;
    draw_pet_frame_contents(true);
    // The pet gives the startup surface a warm, familiar face.  The curved
    // wordmark below it makes the otherwise head-only boot composition read
    // as MaClaw Mate before any ambient date/weather is available.
    draw_startup_brand_arc(state_color("idle"));
    s_render_target = NULL;
    if (frame) {
        if (present_frame_sync(frame) == ESP_OK) {
            s_next_framebuffer ^= 1u;
            ++s_presented_frames;
        }
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
}

esp_err_t board_port_adjust_output_volume(int delta_percent, unsigned *out_percent) {
    int next = (int)s_output_volume + delta_percent;
    if (next < 0) next = 0;
    if (next > 100) next = 100;
    esp_err_t err = board_port_set_output_volume((unsigned)next);
    if (out_percent) *out_percent = s_output_volume;
    return err;
}

esp_err_t board_port_set_output_volume(unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    if (!s_audio_mutex || xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(1500)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    esp_err_t err = audio_init();
    if (err == ESP_OK) err = es8311_set_output_volume(percent);
    if (err == ESP_OK) {
        s_output_volume = percent;
        ESP_LOGI(TAG, "speaker output volume: %u%% (ES8311 reg32=0x%02x)",
                 percent, es8311_volume_register(percent));
    }
    xSemaphoreGive(s_audio_mutex);
    return err;
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
    if (ambient_state && s_command_display_locked) return;
    // A real pet-state transition replaces any short status notice.  This is
    // also the normal terminal path for a notice without a ready-prompt timer.
    s_message_active = false;
    if (strcmp(next_state, "speaking")) {
        s_response_active = false;
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

void board_port_set_command_stage(const char *stage) {
    const char *next_stage = stage && stage[0] ? stage : "正在处理";
    bool changed;
    taskENTER_CRITICAL(&s_state_lock);
    changed = strcmp(s_command_stage, next_stage) != 0;
    if (changed) strlcpy(s_command_stage, next_stage, sizeof(s_command_stage));
    taskEXIT_CRITICAL(&s_state_lock);
    // Redraw only at a real phase transition. This matches Bread's behaviour
    // while preserving EchoEar's animated thinking surface between updates.
    if (changed && !s_recording_active && !s_response_active &&
        !s_display_sleeping && !s_setup_qrcode_visible &&
        !strcmp(s_pet_state, "thinking")) {
        draw_pet();
    }
    ESP_LOGI(TAG, "command stage: %s", next_stage);
}

void board_port_set_command_display_lock(bool locked) {
    s_command_display_locked = locked;
    if (locked) s_ready_prompt_expires_us = 0;
    if (!locked && !s_pet_animation_started && s_background_tasks_lock &&
        xSemaphoreTake(s_background_tasks_lock, pdMS_TO_TICKS(100)) == pdTRUE) {
        // By the time the Welcome/wake sequence releases the boot surface,
        // TLS and ESP-SR have legitimately consumed the remaining contiguous
        // internal heap.  The renderer keeps all transfer buffers in static
        // DMA memory/PSRAM and does not need an internal task stack; allocating
        // this idle-only worker from PSRAM prevents the standby pet from being
        // silently dropped exactly when the device becomes ready.
        s_pet_animation_stopped = xSemaphoreCreateBinary();
        BaseType_t created = s_pet_animation_stopped
            ? xTaskCreatePinnedToCoreWithCaps(
                pet_animation_task, "maclaw_pet_animation", 6144, NULL, 2,
                &s_pet_animation_task, 1, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT)
            : pdFAIL;
        if (created == pdPASS) {
            s_pet_animation_started = true;
        } else {
            if (s_pet_animation_stopped) vSemaphoreDelete(s_pet_animation_stopped);
            s_pet_animation_stopped = NULL;
            s_pet_animation_task = NULL;
            ESP_LOGE(TAG, "cannot start deferred pet animation task");
        }
        xSemaphoreGive(s_background_tasks_lock);
    }
    // The startup artwork/ready hint is a foreground surface.  Once the
    // shared UI model releases that lock, explicitly publish the pet frame;
    // otherwise the first ambient repaint waits for a later animation tick
    // and the round display can remain blank after boot.
    if (!locked && !s_recording_active && !s_response_active &&
        !s_setup_qrcode_visible && ambient_visible_for_state()) {
        s_display_sleeping = false;
        s_idle_pet_visible = true;
        s_idle_pet_sleep_expires_us = esp_timer_get_time() + IDLE_PET_SLEEP_TIMEOUT_US;
        draw_pet();
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
	if (!s_recording_active && !s_command_display_locked) draw_pet();
}

esp_err_t board_port_set_pet_asset(const uint8_t *const *frames, size_t frame_count,
                                   size_t width, size_t height, uint32_t frame_ms) {
    if (frame_count > REMOTE_PET_MAX_FRAMES) return ESP_ERR_INVALID_ARG;
    size_t bytes = 0;
    size_t target_width = 0, target_height = 0;
    if (frame_count) {
        if (!frames || width < 1 || height < 1 || width > 256 || height > 256) return ESP_ERR_INVALID_ARG;
        if (width > SIZE_MAX / height || width * height > SIZE_MAX / 3u) return ESP_ERR_INVALID_SIZE;
        if (!remote_pet_target_size(width, height, &target_width, &target_height)) {
            return ESP_ERR_INVALID_SIZE;
        }
        bytes = target_width * target_height * 3u;
    }
    uint8_t *copies[REMOTE_PET_MAX_FRAMES] = {0};
    bool has_visible_pixels = false;
    for (size_t i = 0; i < frame_count; ++i) {
        if (!frames[i]) goto no_mem;
        copies[i] = heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!copies[i]) copies[i] = malloc(bytes);
        if (!copies[i]) goto no_mem;
        scale_remote_pet_frame(frames[i], width, height, copies[i],
                               target_width, target_height);
        // RGB565A8 stores alpha in byte 2.  A tiny threshold ignores fully
        // transparent padding while preserving antialiased pet edges.
        for (size_t pixel = 0; pixel < target_width * target_height; ++pixel) {
            if (copies[i][pixel * 3u + 2u] >= 8u) {
                has_visible_pixels = true;
                break;
            }
        }
    }
    if (s_lcd_mutex) xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    for (size_t i = 0; i < REMOTE_PET_MAX_FRAMES; ++i) { free(s_remote_pet_frames[i]); s_remote_pet_frames[i] = copies[i]; }
    s_remote_pet_frame_count = frame_count;
    s_remote_pet_width = frame_count ? target_width : 0;
    s_remote_pet_height = frame_count ? target_height : 0;
    s_remote_pet_frame_ms = frame_ms ? frame_ms : REMOTE_PET_DEFAULT_KEYFRAME_MS;
    s_remote_pet_animation_elapsed_ms = 0;
    s_remote_pet_has_visible_pixels = frame_count && has_visible_pixels;
    if (frame_count && !has_visible_pixels) {
        ESP_LOGW(TAG, "remote pet asset has no visible pixels; using native standby pet");
    }
    bool show = !s_command_display_locked && !s_recording_active && ambient_visible_for_state();
    // Compose through the normal double-buffered path for both install and
    // clear. Direct scan-line drawing here exposed a partially updated pet,
    // and clearing otherwise left the removed asset visible until a later
    // animation/clock tick happened to repaint the idle screen.
    if (show) draw_pet();
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    return ESP_OK;
no_mem:
    for (size_t i = 0; i < REMOTE_PET_MAX_FRAMES; ++i) free(copies[i]);
    return ESP_ERR_NO_MEM;
}

void board_port_set_recording_visual(bool active, bool paused, uint32_t elapsed_seconds) {
    bool new_session;
    bool visual_changed;
    taskENTER_CRITICAL(&s_state_lock);
    new_session = active && !s_recording_active;
    visual_changed = !active || new_session ||
                     paused != s_recording_paused ||
                     (active && elapsed_seconds != s_recording_elapsed_seconds);
    if (active) s_ready_prompt_expires_us = 0;
    if (active) {
        s_idle_pet_visible = false;
        s_idle_pet_sleep_expires_us = 0;
    }
    if (active) {
        // Bread Compact makes a fresh recorder the foreground owner. Clear
        // both reply variants here as well: an activation can begin directly
        // from a still-visible result, and leaving its ownership bit set made
        // EchoEar's next recording exit behave as if that old answer remained
        // on screen.
        s_message_active = false;
        s_response_active = false;
        s_response_image_active = false;
    }
    s_recording_active = active;
    s_recording_paused = active && paused;
    s_recording_elapsed_seconds = active ? elapsed_seconds : 0;
    // Reset only at a real state boundary. Both command and meeting capture
    // publish an initial elapsed=0 update after PCM has already begun to flow;
    // treating that as a reset discarded the first visible slice of speech.
    if (!active || new_session) {
        memset(s_recording_wave_levels, 0, sizeof(s_recording_wave_levels));
        s_recording_frame_baseline = false;
        s_recording_meter_dirty = false;
    }
    // Bread Compact starts every recording with a quiet meter. Reset the
    // matching shared history too, otherwise a command begun immediately after
    // a loud meeting opens with the previous session's percentage for one frame.
    if (!active || new_session) {
        s_recording_smoothed_level = 0;
    }
    taskEXIT_CRITICAL(&s_state_lock);
    // Bread Compact only redraws the complete static recording scene at a
    // state/timer boundary. Meter samples call board_port_set_audio_level(),
    // which owns the live waveform frames. Avoid re-composing a no-op scene on
    // every caller reassertion so a paused/steady recorder cannot compete with
    // the next audio-driven update.
    if (active && visual_changed) {
        draw_recording_visual();
    } else {
        if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) == pdTRUE) {
        // Wait for a recording frame already in flight before the caller
        // presents thinking/upload/result. Without this fence, a renderer that
        // took its state snapshot just before the transition can overwrite the
        // newer foreground screen after capture has already ended.
            xSemaphoreGiveRecursive(s_lcd_mutex);
        }
        if (!s_command_display_locked && !s_response_active) {
        // Finishing a command recording is a foreground transition, not a
        // return to the ambient pet.  The caller has already selected the
        // thinking/result/error surface; repainting a pet here queues one old
        // full frame between the waveform and that surface, which resembles a
        // brief boot/standby screen.  Only restore the pet when no foreground
        // command owns the display.
            draw_pet();
        }
    }
}

void board_port_set_audio_level(uint16_t level, uint32_t elapsed_seconds) {
    if (level > 1000) level = 1000;
    bool should_draw = false;
    taskENTER_CRITICAL(&s_state_lock);
    if (!s_recording_active) {
        taskEXIT_CRITICAL(&s_state_lock);
        return;
    }
    s_recording_smoothed_level = level > s_recording_smoothed_level
                                     ? (uint16_t)((s_recording_smoothed_level + level * 3u) / 4u)
                                     : (uint16_t)((s_recording_smoothed_level * 7u + level) / 8u);
    // Bread Compact shifts exactly this filtered meter value into its 24 bar
    // history for every 512-frame input unit. Keep EchoEar's round renderer
    // on that same source so the waveform and its MIC percentage agree.
    memmove(&s_recording_wave_levels[0], &s_recording_wave_levels[1],
            (RECORDING_WAVE_COLUMNS - 1) * sizeof(s_recording_wave_levels[0]));
    s_recording_wave_levels[RECORDING_WAVE_COLUMNS - 1] = s_recording_smoothed_level;
    // Keep the live waveform cadence independent from the visible timer.
    // Bread Compact advances its recorder clock only from
    // board_port_set_recording_visual() at the one-second boundary. Updating
    // it here made that later call appear unchanged, so EchoEar could skip the
    // full frame containing the next timer value. The meter does not draw the
    // timer region, therefore it must leave this state alone.
    should_draw = true;
    s_recording_meter_dirty = true;
    taskEXIT_CRITICAL(&s_state_lock);
    // Bread Compact owns the waveform cadence at the capture boundary: each
    // 512-sample level update immediately redraws the meter. EchoEar used to
    // rebuild the full recording scene here, unlike Bread Compact which
    // updates only its meter band from the previous composed frame. Keep the
    // same one audio block -> one UI frame contract without spending the whole
    // 32 ms budget on static labels and round-screen chrome.
    if (should_draw) draw_recording_meter_visual();
}

void board_port_push_recording_pcm(const int16_t *samples, size_t count) {
    // EchoEar's board renderer derives its 24-column history from the same
    // normalized level path as Bread Compact. PCM is retained by the caller
    // for upload; it must not introduce a second, differently-smoothed wave.
    (void)samples;
    (void)count;
}

void board_port_show_text(const char *title, const char *text) {
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    // A short status message is a foreground surface too.  In particular it
    // can replace a paged result after a transport or recording transition;
    // leave that result marked active and its timer can repaint an old page
    // over this message (or later over the restored standby pet).  Bread
    // Compact clears the same ownership state before every status screen.
    s_response_active = false;
    s_response_image_active = false;
    s_message_active = true;
    s_display_sleeping = false;
    s_idle_pet_visible = false;
    s_idle_pet_sleep_expires_us = 0;
    ESP_ERROR_CHECK_WITHOUT_ABORT(esp_lcd_panel_disp_on_off(s_panel, true));
    if (LCD_BACKLIGHT != GPIO_NUM_NC) {
        ESP_ERROR_CHECK_WITHOUT_ABORT(gpio_set_level(LCD_BACKLIGHT, 1));
    }
    const uint16_t bg = state_color(s_pet_state);
    const uint16_t header = rgb565(14, 31, 47);
    const uint16_t ink = rgb565(248, 252, 255);
    const uint16_t body = rgb565(194, 220, 236);
    const uint16_t muted = rgb565(136, 174, 197);
    const char *visible_title = title && title[0] ? title : "码卡龙";
    const char *visible_text = text ? text : "";

    // Bread's status surface is a complete scene, not a two-line overlay on a
    // previous pet frame.  Compose the EchoEar equivalent into a framebuffer
    // first so every foreground hand-off is atomic and the circular panel gets
    // a coherent message page instead of a leftover pet above the text.
    uint16_t *frame = s_framebuffers[s_next_framebuffer];
    if (frame) s_render_target = frame;
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    fill_circle_vertical_gradient(180, 146, 82, rgb565(69, 146, 203),
                                  rgb565(27, 67, 127));
    fill_circle_vertical_gradient(180, 141, 70, rgb565(190, 242, 246),
                                  rgb565(95, 183, 204));
    draw_ear(119, 80, 1, rgb565(47, 123, 153), rgb565(87, 183, 196));
    draw_ear(241, 80, -1, rgb565(47, 123, 153), rgb565(87, 183, 196));
    draw_eye(153, 144, rgb565(24, 44, 70), ink);
    draw_eye(207, 144, rgb565(24, 44, 70), ink);
    fill_circle(180, 170, 11, rgb565(37, 58, 84));
    fill_rect(166, 184, 194, 190, rgb565(37, 58, 84));

    fill_rect(50, 220, LCD_WIDTH - 50, 222, header);
    int title_width = text24_width(visible_title, 10);
    if (title_width > RESPONSE_TEXT_W) title_width = RESPONSE_TEXT_W;
    draw_text24((LCD_WIDTH - title_width) / 2, 240, visible_title, ink, bg);
    char body_line[64];
    const char *next_line = copy_text24_line(body_line, sizeof(body_line), visible_text,
                                              RESPONSE_TEXT_W);
    int body_width = text24_width(body_line, 12);
    if (body_width > RESPONSE_TEXT_W) body_width = RESPONSE_TEXT_W;
    draw_text24((LCD_WIDTH - body_width) / 2, 278, body_line, body, bg);
    if (next_line && *next_line) {
        char continuation[64];
        (void)copy_text24_line(continuation, sizeof(continuation), next_line,
                               RESPONSE_TEXT_W);
        int continuation_width = text24_width(continuation, 12);
        if (continuation_width > RESPONSE_TEXT_W) continuation_width = RESPONSE_TEXT_W;
        draw_text24((LCD_WIDTH - continuation_width) / 2, 306, continuation, body, bg);
    }
    draw_text24((LCD_WIDTH - text24_width("轻触继续", 8)) / 2,
                RESPONSE_FOOTER_Y + 58, "轻触继续", muted, bg);
    s_render_target = NULL;
    if (frame) {
        esp_err_t draw_err = present_frame_sync(frame);
        if (draw_err == ESP_OK) {
            s_next_framebuffer ^= 1u;
            ++s_presented_frames;
        } else {
            ESP_LOGE(TAG, "message frame present failed: %s", esp_err_to_name(draw_err));
        }
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "%s: %s", title ? title : "MaClaw", text ? text : "");
}

void board_port_show_upload_progress(size_t completed_bytes, size_t total_bytes,
                                     const char *stage) {
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    // A transfer is an independent, quiet surface. It avoids the animated pet
    // DMA path while a large HTTPS upload is active and makes progress legible.
    // It must also supersede a previously held response surface: an answer can
    // remain on screen when the user starts a meeting, and leaving that stale
    // ownership bit set prevents a later completion from restoring standby.
    s_ready_prompt_expires_us = 0;
    s_idle_pet_visible = false;
    s_idle_pet_sleep_expires_us = 0;
    s_message_active = false;
    s_response_active = false;
    s_response_image_active = false;
    const uint16_t bg = rgb565(9, 35, 64);
    const uint16_t fg = rgb565(244, 250, 255);
    const uint16_t muted = rgb565(174, 206, 224);
    const uint16_t track = rgb565(28, 80, 111);
    const uint16_t fill = rgb565(72, 205, 220);
    // Preserve the shared UI's overflow-safe calculation. A recording can be
    // hundreds of MiB, so multiplying a 32-bit size_t before dividing wraps
    // long before the 512 MiB meeting limit and makes progress jump backwards.
    uint32_t percent = 0;
    if (total_bytes) {
        size_t whole = completed_bytes / total_bytes;
        size_t remainder = completed_bytes % total_bytes;
        percent = whole >= 1 ? 100u
                             : (uint32_t)(((uint64_t)remainder * 100u) / total_bytes);
    }
    if (percent > 100) percent = 100;
    // Unlike a pet delta, upload progress is a complete foreground page.  Build
    // it in the same double buffer as the result/message pages so that a stage
    // change cannot expose an old pet or a half-updated progress bar.
    uint16_t *frame = s_framebuffers[s_next_framebuffer];
    if (frame) s_render_target = frame;
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    fill_rect(52, 34, LCD_WIDTH - 52, 36, fill);
    const char *visible_stage = stage && stage[0] ? stage : "正在上传";
    int heading_width = text24_width("会议录音", 8);
    draw_text24((LCD_WIDTH - heading_width) / 2, 62, "会议录音", fg, bg);
    char stage_line[64];
    const char *stage_next = copy_text24_line(stage_line, sizeof(stage_line), visible_stage,
                                               RESPONSE_TEXT_W);
    int stage_width = text24_width(stage_line, 12);
    if (stage_width > RESPONSE_TEXT_W) stage_width = RESPONSE_TEXT_W;
    draw_text24((LCD_WIDTH - stage_width) / 2, 106, stage_line, muted, bg);
    if (stage_next && *stage_next) {
        char stage_continuation[64];
        (void)copy_text24_line(stage_continuation, sizeof(stage_continuation), stage_next,
                               RESPONSE_TEXT_W);
        int continuation_width = text24_width(stage_continuation, 12);
        if (continuation_width > RESPONSE_TEXT_W) continuation_width = RESPONSE_TEXT_W;
        draw_text24((LCD_WIDTH - continuation_width) / 2, 134,
                    stage_continuation, muted, bg);
    }
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
    draw_text24((LCD_WIDTH - text24_width("上传中，请勿断电", 10)) / 2,
                304, "上传中，请勿断电", muted, bg);
    s_render_target = NULL;
    if (frame) {
        esp_err_t draw_err = present_frame_sync(frame);
        if (draw_err == ESP_OK) {
            s_next_framebuffer ^= 1u;
            ++s_presented_frames;
        } else {
            ESP_LOGE(TAG, "upload frame present failed: %s", esp_err_to_name(draw_err));
        }
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

static void show_qrcode_matrix(const uint8_t *modules, size_t size, const char *ssid) {
    if (!s_panel || !modules || size == 0) return;
    const int quiet_zone = 4;
    // A QR code is necessarily square, but its corners must remain inside the
    // round aperture.  204px at y=40 is safely within the EchoEar's visible
    // circle, including the required white quiet zone; the former 228px tile
    // at y=12 placed its upper corners behind the bezel.
    const int available = 204;
    const int module = available / (size + quiet_zone * 2);
    if (module < 2) {
        ESP_LOGW(TAG, "QR code is too large for display: %u modules", (unsigned)size);
        return;
    }
    const int qr_pixels = (size + quiet_zone * 2) * module;
    const int x0 = (LCD_WIDTH - qr_pixels) / 2;
    const int y0 = 40;
    const uint16_t page_bg = state_color("quiet");
    const uint16_t white = rgb565(255, 255, 255);
    const uint16_t black = rgb565(0, 0, 0);

    s_ready_prompt_expires_us = 0;
    s_display_sleeping = false;
    s_idle_pet_visible = false;
    s_idle_pet_sleep_expires_us = 0;
    s_message_active = false;
    // Setup replaces every prior foreground page.  Stop a deferred response
    // turn before publishing the QR guard; otherwise an already-due response
    // page can obtain the LCD mutex first and briefly cover the code.
    s_response_active = false;
    s_response_image_active = false;
    // Set this before drawing so the animation task cannot paint a pet frame
    // between QR stripes on the shared LCD.
    s_setup_qrcode_visible = true;
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    // Match the other foreground screens: construct the complete QR page in
    // the back buffer before it touches the panel.  QR rendering used to draw
    // one stripe at a time directly to the LCD, which exposed old pet/result
    // pixels during the transition and could leave a torn code if another
    // foreground update arrived while provisioning started.
    uint16_t *frame = s_framebuffers[s_next_framebuffer];
    if (frame) s_render_target = frame;
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, page_bg);
    // A white quiet zone is required for reliable recognition by WeChat and
    // phone cameras, especially when the device is held at an angle. Compose
    for (int py = 0; py < qr_pixels; ++py) {
        const int module_y = py / module - quiet_zone;
        uint16_t *row = frame ? frame + (size_t)(y0 + py) * LCD_WIDTH + x0 : NULL;
        for (int px = 0; px < qr_pixels; ++px) {
            const int module_x = px / module - quiet_zone;
            const bool black_module = module_x >= 0 && module_x < size &&
                                       module_y >= 0 && module_y < size &&
                                       modules[(size_t)module_y * size + module_x] != 0;
            if (row) row[px] = black_module ? black : white;
        }
    }
    const char *title = "微信扫码加入热点";
    draw_text24((LCD_WIDTH - text24_width(title, 9)) / 2, 270, title,
                rgb565(255, 255, 255), page_bg);
    char instruction[40];
    snprintf(instruction, sizeof(instruction), "热点 %s", ssid ? ssid : "");
    char instruction_line[40];
    const char *instruction_next = copy_text24_line(instruction_line,
                                                     sizeof(instruction_line),
                                                     instruction, RESPONSE_TEXT_W);
    int instruction_width = text24_width(instruction_line, 12);
    draw_text24((LCD_WIDTH - instruction_width) / 2, 304, instruction_line,
                rgb565(220, 235, 255), page_bg);
    if (instruction_next && *instruction_next) {
        char continuation[40];
        (void)copy_text24_line(continuation, sizeof(continuation), instruction_next,
                               RESPONSE_TEXT_W);
        int continuation_width = text24_width(continuation, 12);
        draw_text24((LCD_WIDTH - continuation_width) / 2, 330, continuation,
                    rgb565(220, 235, 255), page_bg);
    }
    s_render_target = NULL;
    if (frame) {
        esp_err_t draw_err = present_frame_sync(frame);
        if (draw_err == ESP_OK) {
            s_next_framebuffer ^= 1u;
            ++s_presented_frames;
        } else {
            ESP_LOGE(TAG, "QR frame present failed: %s", esp_err_to_name(draw_err));
        }
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "showing setup Wi-Fi QR for %s", ssid ? ssid : "");
}

void board_port_show_qrcode_matrix(const uint8_t *modules, size_t module_count,
                                   const char *ssid) {
    show_qrcode_matrix(modules, module_count, ssid);
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
    show_qrcode_matrix(modules, (size_t)size, ssid);
    heap_caps_free(modules);
}

void board_port_set_wifi_status(const char *ssid, bool connected) {
    bool changed;
    taskENTER_CRITICAL(&s_state_lock);
    // Once the gateway has authenticated, retain a transient transport loss
    // until either transport recovers or the gateway itself reports offline.
    // This mirrors Bread Compact and avoids briefly showing "联网中" during a
    // healthy reconnect cycle.
    bool next_connected = (!s_service_ready || connected) ? connected : s_wifi_connected;
    changed = s_wifi_connected != next_connected;
    s_wifi_connected = next_connected;
    if (changed) ++s_ambient_revision;
    taskEXIT_CRITICAL(&s_state_lock);
    // Do not write a small Wi-Fi widget directly to the panel here. The
    // standby renderer uses a double-buffered changed-region presenter; a
    // direct patch is unknown to its front-frame snapshot and can survive as
    // a stale "WIFI" fragment when the following pet frame omits that region.
    // Fold the transport state into the same curved ambient ring as Bread
    // Compact. It keeps one owner for every standby pixel and lets the next
    // composed frame clear or redraw the exact changed area without flicker.
    if (changed && !s_display_sleeping && !s_recording_active &&
        !s_setup_qrcode_visible && !s_command_display_locked &&
        ambient_visible_for_state()) {
        draw_pet();
    }
    ESP_LOGI(TAG, "Wi-Fi state: %s (%s)", next_connected ? "connected" : "connecting",
             ssid ? ssid : "");
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

void board_port_set_alarm_scheduled(bool scheduled) {
    bool changed;
    taskENTER_CRITICAL(&s_state_lock);
    changed = s_alarm_scheduled != scheduled;
    s_alarm_scheduled = scheduled;
    if (changed) ++s_ambient_revision;
    taskEXIT_CRITICAL(&s_state_lock);
    if (changed && !s_display_sleeping && !s_recording_active &&
        !s_setup_qrcode_visible && !s_command_display_locked &&
        ambient_visible_for_state()) {
        draw_pet();
    }
}

void board_port_show_ready_prompt(const char *title, const char *text) {
    if (s_alarm_visual_active) return;
    // Completing provisioning is the terminal transition for the QR surface.
    // Keep its guard while a phone is scanning, but release it before the
    // ready path republishes idle; otherwise board_port_set_pet_state() keeps
    // deferring the standby frame forever and EchoEar remains visually stuck
    // on the old QR page. Bread Compact clears its foreground owner as part of
    // this same ready-to-idle transition.
    s_setup_qrcode_visible = false;
    // A successful startup belongs on the actual standby surface.  The former
    // implementation drew the pet and then immediately replaced it with the
    // generic full-screen "device ready" message for a full minute.  That made
    // a healthy EchoEar look as though its configured pet had disappeared (and
    // made it feel slow when the pet finally returned).  Bread's ready flow
    // hands the screen back to its idle state; do the same here.
    s_message_active = false;
    s_display_sleeping = false;
    ESP_ERROR_CHECK_WITHOUT_ABORT(esp_lcd_panel_disp_on_off(s_panel, true));
    if (LCD_BACKLIGHT != GPIO_NUM_NC) {
        ESP_ERROR_CHECK_WITHOUT_ABORT(gpio_set_level(LCD_BACKLIGHT, 1));
    }
    board_port_set_pet_state("idle");
    s_ready_prompt_expires_us = 0;
    ESP_LOGI(TAG, "ready standby: %s | %s", title ? title : "", text ? text : "");
}

void board_port_cancel_ready_prompt(void) {
    s_ready_prompt_expires_us = 0;
}

// Board-owned DISPLAY_OFF transaction. Network, alarm, and wake-word services
// deliberately stay alive; the application controls idle policy while this HAL
// owns only the physical panel/backlight state and matching wake bookkeeping.
bool board_port_enter_display_off(void) {
    if (!s_lcd_mutex || !s_panel) return false;
    if (xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return false;
    // A Power Service deadline can become due just as a foreground transition
    // is being published. The display adapter is the final arbiter of its
    // local scene, so it refuses stale ambient deadlines instead of blanking
    // an alarm, response, recorder, setup QR, or status page.
    if (s_display_sleeping || !s_idle_pet_visible || s_message_active ||
        s_recording_active || s_response_active || s_response_image_active ||
        s_setup_qrcode_visible || s_alarm_visual_active ||
        s_command_display_locked) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return false;
    }
    s_idle_pet_visible = false;
    s_idle_pet_sleep_expires_us = 0;
    s_display_sleeping = true;
    ESP_ERROR_CHECK_WITHOUT_ABORT(esp_lcd_panel_disp_on_off(s_panel, false));
    if (LCD_BACKLIGHT != GPIO_NUM_NC) {
        ESP_ERROR_CHECK_WITHOUT_ABORT(gpio_set_level(LCD_BACKLIGHT, 0));
    }
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
    if (!s_lcd_mutex || !s_panel) return false;
    if (xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return false;
    if (!s_display_sleeping) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return false;
    }
    s_display_sleeping = false;
    ESP_ERROR_CHECK_WITHOUT_ABORT(esp_lcd_panel_disp_on_off(s_panel, true));
    if (LCD_BACKLIGHT != GPIO_NUM_NC) {
        ESP_ERROR_CHECK_WITHOUT_ABORT(gpio_set_level(LCD_BACKLIGHT, 1));
    }
    // The first physical contact after the 30-minute ambient timeout is a
    // screen wake, not an implicit voice command. Re-publish the same ready
    // pet surface and arm its normal hint timeout, matching the documented
    // board-port contract and the compact board's "first press returns" flow.
    s_idle_pet_visible = true;
    s_idle_pet_sleep_expires_us = esp_timer_get_time() + IDLE_PET_SLEEP_TIMEOUT_US;
    s_ready_prompt_expires_us = esp_timer_get_time() + READY_PROMPT_TIMEOUT_US;
    draw_pet();
    xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "ambient display awakened");
    return true;
}

esp_err_t board_port_audio_stream_start(void) {
    board_port_pause_wake_word(true);
    // MultiNet may already be inside a 250 ms I2S read when pause is asserted,
    // and foreground network/UI work can delay its mutex release. Allow a
    // bounded settling window so a valid double tap does not intermittently
    // fail before capture has even started.
    for (unsigned i = 0;
         s_wake_word_task && !s_wake_word_pause_acknowledged && i < 40; ++i) {
        vTaskDelay(pdMS_TO_TICKS(5));
    }
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
        if (err == ESP_OK) recording_pcm_reset("meeting");
        if (err != ESP_OK) {
            xSemaphoreGive(s_audio_mutex);
            board_port_pause_wake_word(false);
        }
    }
    if (err == ESP_OK) s_audio_stream_owned = true;
    return err;
}

esp_err_t board_port_audio_stream_read(int16_t *mono, size_t sample_capacity,
                                       size_t *samples_read, uint16_t *level) {
    if (samples_read) *samples_read = 0;
    if (level) *level = 0;
    if (!mono || !samples_read || sample_capacity == 0) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(audio_init(), TAG, "microphone init failed");
    // Bread Compact reads one 512-sample (32 ms) mono block for both meeting
    // persistence and the shared recorder meter. EchoEar's TDM DMA delivery
    // is normally 128 mono frames, so build the same unit here rather than
    // advancing the meeting UI four times faster on the physical board.
    int16_t tdm[512];
    size_t total_frames = 0;
    int32_t peak = 0;
    while (total_frames < sample_capacity) {
        size_t received = 0;
        esp_err_t err = i2s_channel_read(s_audio_rx, tdm, sizeof(tdm), &received,
                                         pdMS_TO_TICKS(1000));
        if (err != ESP_OK) return err;
        size_t frames = received / (sizeof(int16_t) * ECHOEAR_MIC_SLOT_COUNT);
        if (frames == 0) continue;
        if (frames > sample_capacity - total_frames) {
            frames = sample_capacity - total_frames;
        }
        int32_t chunk_peak = recording_pcm_process(tdm, frames, mono + total_frames);
        if (chunk_peak > peak) peak = chunk_peak;
        total_frames += frames;
    }
    uint32_t scaled = peak <= 180 ? 0 : (uint32_t)(peak - 180) * 1000u / (12000u - 180u);
    if (scaled > 1000) scaled = 1000;
    if (level) *level = (uint16_t)scaled;
    // The app UI forwards this exact PCM block to the common recording surface
    // after it has accepted the read. Keep this transport helper data-only so
    // a meeting block contributes one waveform bucket rather than two.
    *samples_read = total_frames;
    return ESP_OK;
}

void board_port_audio_stream_stop(void) {
    // Keep I2S enabled for the normal six-second command path. The next stream
    // read drains any DMA frames accumulated while a meeting was paused.
    if (s_audio_stream_owned && s_audio_mutex) {
        s_audio_stream_owned = false;
        xSemaphoreGive(s_audio_mutex);
    }
    board_port_pause_wake_word(false);
}

void board_port_pause_wake_word(bool paused) {
    s_wake_word_paused = paused;
    if (!paused) s_wake_word_pause_acknowledged = false;
}

static bool response_break(uint32_t cp) {
    return cp == '\n' || cp == '\r';
}

// Hub replies can include zero-width formatting marks and desktop-only emoji.
// They have no usable shape in this compact LCD font and otherwise consume a
// whole CJK cell as a question mark before the actual answer.
static bool response_invisible_format(uint32_t cp) {
    return cp == 0xFEFF || cp == 0x200B || cp == 0x200C || cp == 0x200D ||
           cp == 0x200E || cp == 0x200F || cp == 0xFE0E || cp == 0xFE0F ||
           (cp >= 0x202A && cp <= 0x202E) ||
           (cp >= 0x2060 && cp <= 0x2069);
}

static bool response_closing_punctuation(uint32_t cp) {
    switch (cp) {
        case 0x3001: case 0x3002: case 0xFF0C: case 0xFF0E:
        case 0xFF01: case 0xFF1A: case 0xFF1B: case 0xFF1F:
        case 0xFF09: case 0x3009: case 0x300B: case 0x300D:
        case 0x300F: case 0x3011: case 0x3015: case 0x3017:
        case 0x3019: case 0x301B: case 0x2026:
            return true;
        default:
            return false;
    }
}

static bool response_opening_punctuation(uint32_t cp) {
    switch (cp) {
        case 0xFF08: case 0x3008: case 0x300A: case 0x300C:
        case 0x300E: case 0x3010: case 0x3014: case 0x3016:
        case 0x3018: case 0x301A:
            return true;
        default:
            return false;
    }
}

void board_port_set_recording_mode(bool meeting) {
    taskENTER_CRITICAL(&s_state_lock);
    s_recording_is_meeting = meeting;
    taskEXIT_CRITICAL(&s_state_lock);
}

// Match Bread Compact's server-response hygiene.  Older desktop paths can
// prepend routing/token diagnostics to a normal answer; those are useful in a
// log but should never occupy one of the five readable lines on EchoEar.
static bool response_internal_metadata_line(const char *start, const char *end) {
    while (start < end && (*start == ' ' || *start == '\t')) ++start;
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
        "cache read tokens:", "cache write tokens:", "no aux/route",
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
    if (!dst || !dst_size) return;
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
        if (content < trimmed_end && !response_internal_metadata_line(content, trimmed_end)) {
            size_t bytes = (size_t)(line_end - content);
            if (bytes > dst_size - used - 1) bytes = dst_size - used - 1;
            memcpy(dst + used, content, bytes);
            used += bytes;
            first_content = false;
            if (*line_end == '\n' && used + 1 < dst_size) dst[used++] = '\n';
        }
        cursor = *line_end == '\n' ? line_end + 1 : line_end;
    }
    while (used > 0 && (dst[used - 1] == '\n' || dst[used - 1] == '\r' ||
                        dst[used - 1] == ' ' || dst[used - 1] == '\t')) --used;
    dst[used] = '\0';
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
        size_t bytes = (size_t)(cursor - before);
        if (width && width + advance > RESPONSE_TEXT_W) {
            if (response_closing_punctuation(cp) && used + bytes < line_size) {
                // Keep terminal punctuation with the preceding clause. A one
                // glyph optical overhang reads better than a new line that
                // begins with an orphan comma or closing bracket.
                memcpy(line + used, before, bytes);
                used += bytes;
                line[used] = '\0';
            } else {
                cursor = before;
                // Never leave an opening bracket stranded at the line end.
                if (response_opening_punctuation(last_cp) && last_before && last_used > 0) {
                    cursor = last_before;
                    used = last_used;
                    width = last_width;
                    line[used] = '\0';
                }
            }
            break;
        }
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
    if (!s_response_active || s_response_image_active || s_recording_active ||
        s_setup_qrcode_visible || s_alarm_visual_active) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    uint16_t bg = rgb565(8, 17, 28);
    uint16_t header = rgb565(14, 31, 47);
    uint16_t footer = rgb565(11, 24, 38);
    uint16_t title_color = rgb565(244, 248, 251);
    uint16_t body_color = rgb565(214, 227, 237);
    uint16_t muted = rgb565(145, 172, 191);
    uint16_t accent = rgb565(76, 168, 207);
    uint16_t *frame = s_framebuffers[s_next_framebuffer];
    if (frame) s_render_target = frame;
    // A reply is a dedicated reading surface. Compose and present it as one
    // frame so arrival and manual page changes never expose partial drawing.
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    // The physical display is round.  Keep every meaningful element inside
    // the inscribed reading area: the top/bottom corners of a rectangular
    // header or footer are not visible through the EchoEar enclosure.
    fill_rect(48, 22, LCD_WIDTH - 48, RESPONSE_RULE_Y + 2, header);
    fill_rect(RESPONSE_TEXT_X, RESPONSE_RULE_Y, LCD_WIDTH - RESPONSE_TEXT_X,
              RESPONSE_RULE_Y + 2, rgb565(31, 62, 82));
    fill_rect(RESPONSE_TEXT_X, RESPONSE_FOOTER_Y - 8, LCD_WIDTH - RESPONSE_TEXT_X,
              RESPONSE_FOOTER_Y - 7, footer);
    draw_text24_centered_safe(RESPONSE_TITLE_Y,
                              s_response_title[0] ? s_response_title : "处理结果",
                              RESPONSE_TEXT_W - 24, title_color, header);
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
    // EchoEar has physical side keys, so it follows Bread Compact's deliberate
    // reader flow: the answer stays on the current page until the user moves
    // it.  Auto-turning belongs only to one-key boards such as Fangtang.
    draw_text24(RESPONSE_TEXT_X, RESPONSE_FOOTER_Y,
                pages > 1 ? "音量键翻页" : "轻点返回", muted, bg);
    char indicator[16];
    snprintf(indicator, sizeof(indicator), "%u/%u", s_response_page + 1, pages);
    draw_text24(LCD_WIDTH - RESPONSE_TEXT_X - text24_width(indicator, 8),
                RESPONSE_FOOTER_Y, indicator, accent, bg);
    s_render_target = NULL;
    if (frame) {
        esp_err_t draw_err = present_frame_sync(frame);
        if (draw_err == ESP_OK) {
            s_next_framebuffer ^= 1u;
            ++s_presented_frames;
        } else {
            ESP_LOGE(TAG, "response frame present failed: %s", esp_err_to_name(draw_err));
        }
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

void board_port_show_response(const char *title, const char *text) {
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    s_ready_prompt_expires_us = 0;
    // Enter the result state without calling board_port_set_pet_state(). That
    // public setter paints a complete pet frame immediately; doing so just
    // before this page produced a visible boot/idle-looking flash between the
    // thinking screen and every streamed response message.
    strlcpy(s_pet_state, "speaking", sizeof(s_pet_state));
    s_idle_pet_visible = false;
    s_idle_pet_sleep_expires_us = 0;
    s_display_sleeping = false;
    // EchoEar has manual response keys.  Keep the response timer cleared so a
    // long answer cannot turn beneath the reader; Bread Compact has the same
    // manual-paging contract.
    s_response_active = false;
    s_response_image_active = false;
    s_message_active = false;
    s_response_page = 0;
    strlcpy(s_response_title, title && title[0] ? title : "码卡龙", sizeof(s_response_title));
    response_copy_without_internal_metadata(s_response_text, sizeof(s_response_text), text);
    if (!s_response_text[0]) {
        strlcpy(s_response_text, "没有收到文字回复", sizeof(s_response_text));
    }
    s_response_active = true;
    draw_response_page();
    ESP_LOGI(TAG, "response: %s", s_response_text);
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

void board_port_set_alarm_visual(bool active, unsigned frame, const char *time_text,
                                 const char *label, unsigned attempt, unsigned max_attempts) {
    if (!active) {
        // The shared UI coordinator immediately replays the authoritative
        // foreground scene. Release only the alarm-local guard here; drawing
        // idle would flash and would discard a response/upload/setup page.
        s_alarm_visual_active = false;
        s_display_sleeping = false;
        ESP_ERROR_CHECK_WITHOUT_ABORT(esp_lcd_panel_disp_on_off(s_panel, true));
        if (LCD_BACKLIGHT != GPIO_NUM_NC) {
            ESP_ERROR_CHECK_WITHOUT_ABORT(gpio_set_level(LCD_BACKLIGHT, 1));
        }
        return;
    }
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    s_command_display_locked = true;
    s_response_active = false;
    s_response_image_active = false;
    s_message_active = false;
    s_alarm_visual_active = true;
    const uint16_t bg = rgb565(9, 23, 38);
    const uint16_t panel = rgb565(17, 43, 64);
    const uint16_t steel = rgb565(78, 159, 194);
    const uint16_t amber = rgb565(235, 177, 74);
    const uint16_t white = rgb565(241, 248, 252);
    const uint16_t muted = rgb565(145, 177, 197);
    int sway = (frame & 1u) ? 5 : -5;
    uint16_t *composed = s_framebuffers[s_next_framebuffer];
    if (composed) s_render_target = composed;
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    // The wide rectangular header was visibly clipped by the circular bezel.
    // Use a compact title/rule that follows the result screen's safe geometry.
    fill_rect(68, 78, LCD_WIDTH - 68, 81, panel);
    draw_text24_centered_safe(40, "闹钟响铃", 208, white, bg);
    // Restrained mechanical twin-bell silhouette. Alternating horizontal
    // offset conveys ringing without flashing the whole display.
    int cx = LCD_WIDTH / 2 + sway;
    fill_circle(cx, 183, 70, steel);
    fill_circle(cx, 183, 57, panel);
    fill_circle(cx - 65, 112, 29, amber);
    fill_circle(cx + 65, 112, 29, amber);
    fill_rect(cx - 78, 134, cx - 50, 142, amber);
    fill_rect(cx + 50, 134, cx + 78, 142, amber);
    fill_rect(cx - 5, 96, cx + 5, 116, amber);
    fill_circle(cx, 92, 8, amber);
    fill_rect(cx - 34, 244, cx - 20, 265, steel);
    fill_rect(cx + 20, 244, cx + 34, 265, steel);
    // Alarm payloads currently look like "YYYY-MM-DD HH:MM". Locate the
    // clock rather than assuming a byte offset so a locale/ISO change stays
    // display-safe.
    const char *clock = "--:--";
    char clock_text[6] = {0};
    if (time_text) {
        for (const char *cursor = time_text; cursor[0] && cursor[1] && cursor[2] &&
             cursor[3] && cursor[4]; ++cursor) {
            if (isdigit((unsigned char)cursor[0]) && isdigit((unsigned char)cursor[1]) &&
                cursor[2] == ':' && isdigit((unsigned char)cursor[3]) &&
                isdigit((unsigned char)cursor[4])) {
                memcpy(clock_text, cursor, 5);
                clock = clock_text;
            }
        }
    }
    draw_text24_centered_safe(164, clock, 180, white, panel);
    if (label && label[0]) draw_text24_centered_safe(276, label, 244, white, bg);
    char hint[48];
    snprintf(hint, sizeof(hint), "轻触停止  %u/%u", attempt, max_attempts);
    draw_text24_centered_safe(318, hint, 224, muted, bg);
    s_render_target = NULL;
    if (composed) {
        // The alarm silhouette deliberately moves by only a few pixels. Keep
        // that motion fluid without re-sending the static background, label
        // and ring chrome on every 120 ms audio burst.
        esp_err_t draw_err = present_pet_frame_delta_sync(composed);
        if (draw_err == ESP_OK) {
            s_front_framebuffer = s_next_framebuffer;
            s_front_frame_valid = true;
            s_next_framebuffer ^= 1u;
            ++s_presented_frames;
        } else {
            ESP_LOGE(TAG, "alarm frame present failed: %s", esp_err_to_name(draw_err));
        }
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

void board_port_show_response_image(const char *title, const char *caption,
                                    const uint16_t *pixels, size_t width, size_t height) {
    if (!pixels || width < 1 || width > 64 || height < 1 || height > 64) return;
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    s_ready_prompt_expires_us = 0;
    strlcpy(s_pet_state, "speaking", sizeof(s_pet_state));
    s_idle_pet_visible = false;
    s_display_sleeping = false;
    s_message_active = false;
    s_response_active = true;
    s_response_image_active = true;
    s_response_page = 0;
    uint16_t bg = rgb565(8, 17, 28), header = rgb565(14, 31, 47);
    uint16_t ink = rgb565(244, 248, 251);
    uint16_t muted = rgb565(174, 198, 215);
    uint16_t *frame = s_framebuffers[s_next_framebuffer];
    if (frame) s_render_target = frame;
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    // Use the same inscribed reading area as text replies.  A rectangular
    // edge-to-edge header or 64px thumbnail makes an image result look clipped
    // by the circular bezel, while this panel keeps both the title and image
    // visually centred in the round surface.
    fill_rect(48, 22, LCD_WIDTH - 48, RESPONSE_RULE_Y + 2, header);
    fill_rect(RESPONSE_TEXT_X, RESPONSE_RULE_Y, LCD_WIDTH - RESPONSE_TEXT_X,
              RESPONSE_RULE_Y + 2, rgb565(31, 62, 82));
    const char *visible_title = title && title[0] ? title : "码卡龙";
    draw_text24_centered_safe(RESPONSE_TITLE_Y, visible_title,
                              RESPONSE_TEXT_W - 24, ink, header);

    const int content_top = RESPONSE_RULE_Y + 14;
    const int content_bottom = caption && caption[0] ? 234 : RESPONSE_FOOTER_Y - 18;
    const int available_w = LCD_WIDTH - RESPONSE_TEXT_X * 2;
    const int available_h = content_bottom - content_top;
    int scale_x = available_w / (int)width;
    int scale_y = available_h / (int)height;
    int scale = scale_x < scale_y ? scale_x : scale_y;
    if (scale < 1) scale = 1;
    const int shown_w = (int)width * scale;
    const int shown_h = (int)height * scale;
    const int image_x = (LCD_WIDTH - shown_w) / 2;
    const int image_y = content_top + (available_h - shown_h) / 2;
    draw_or_compose_scaled_bitmap(image_x, image_y, image_x + shown_w,
                                  image_y + shown_h, pixels, width, height);
    if (caption && caption[0]) {
        const char *caption_next = draw_text24_centered_safe(246, caption,
                                                               RESPONSE_TEXT_W,
                                                               muted, bg);
        if (caption_next && *caption_next) {
            (void)draw_text24_centered_safe(272, caption_next,
                                             RESPONSE_TEXT_W, muted, bg);
        }
    }
    draw_text24(RESPONSE_TEXT_X, RESPONSE_FOOTER_Y, "轻触返回", muted, bg);
    s_render_target = NULL;
    if (frame && present_frame_sync(frame) == ESP_OK) {
        s_next_framebuffer ^= 1u;
        ++s_presented_frames;
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

bool board_port_navigate_response(int page_delta) {
    if (page_delta == 0) return false;
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return false;
    if (!s_response_active) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        return false;
    }
    if (s_response_image_active) {
        // The gesture belongs to the visible response, but an image reply has
        // no alternate page and its decoded pixel buffer is no longer retained.
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        return true;
    }
    unsigned pages = response_page_count();
    if (pages < 2) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        return true;
    }
    int next = (int)s_response_page + page_delta;
    // Keep the result reader cyclic, as on Bread Compact.  EchoEar maps the
    // side volume keys to previous/next while a reply is visible; pinning at an
    // edge made a working key appear dead and contradicted the shared input
    // flow's documented 1 -> 2 -> ... -> 1 sequence.
    if (next < 0) next = (int)pages - 1;
    if (next >= (int)pages) next = 0;
    if ((unsigned)next != s_response_page) {
        s_response_page = (unsigned)next;
        draw_response_page();
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    return true;
}

bool board_port_get_response_page(unsigned *page) {
    if (!page) return false;
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) {
        return false;
    }
    bool active = s_response_active;
    if (active) *page = s_response_image_active ? 0u : s_response_page;
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    return active;
}

bool board_port_restore_response_page(unsigned page) {
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) {
        return false;
    }
    if (!s_response_active) {
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
        return false;
    }
    if (!s_response_image_active) {
        unsigned pages = response_page_count();
        unsigned target = pages > 0 && page >= pages ? pages - 1 : page;
        if (target != s_response_page) {
            s_response_page = target;
            draw_response_page();
        }
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    return true;
}

static void wake_callback_dispatch_task(void *arg) {
    (void)arg;
    // The recognizer has observed the phrase and will tear itself down. Wait
    // for its own cleanup rather than deleting it from another task; MultiNet
    // owns several model allocations that must be released on its normal exit.
    for (unsigned i = 0; i < 240 && s_wake_word_task; ++i) {
        vTaskDelay(pdMS_TO_TICKS(25));
    }
    board_port_wake_word_cb_t callback = NULL;
    void *callback_arg = NULL;
    bool recognizer_released = false;
    taskENTER_CRITICAL(&s_wake_word_lock);
    if (!s_wake_word_task && s_wake_callback_pending) {
        callback = s_on_wake_word;
        callback_arg = s_on_wake_word_arg;
        recognizer_released = true;
    }
    s_wake_callback_pending = false;
    s_wake_callback_task = NULL;
    taskEXIT_CRITICAL(&s_wake_word_lock);
    if (callback) {
        ESP_LOGI(TAG, "offline wake recognizer released; dispatching foreground callback");
        callback(callback_arg);
    } else if (!recognizer_released) {
        // Do not silently lose a confirmed wake phrase if a future model
        // cleanup regression holds the task handle beyond this bounded wait.
        // The next normal supervisor pass will rebuild the recognizer.
        ESP_LOGW(TAG, "offline wake callback skipped: recognizer cleanup timed out");
    }
    vTaskDeleteWithCaps(NULL);
}

static void wake_word_task(void *arg) {
    (void)arg;
    /* start_wake_word publishes the created handle before this task may own
     * and later clear it. This closes the early model-failure race. */
    while (s_wake_word_task_starting) vTaskDelay(1);
    s_wake_word_ready = false;
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
         command_err == ESP_OK && i < sizeof(s_wake_word_cn_phonetics) / sizeof(s_wake_word_cn_phonetics[0]);
         ++i) {
        command_err = esp_mn_commands_add(WAKE_WORD_COMMAND_ID,
                                          s_wake_word_cn_phonetics[i]);
    }
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
    int16_t *tdm = heap_caps_malloc((size_t)chunk_samples * ECHOEAR_MIC_SLOT_COUNT *
                                        sizeof(int16_t),
                                    MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!mono || !tdm) {
        ESP_LOGE(TAG, "offline wake disabled: no memory for %d-sample audio buffers", chunk_samples);
        heap_caps_free(mono);
        heap_caps_free(tdm);
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }

    s_wake_word_ready = true;
    ESP_LOGI(TAG,
             "offline wake listening: model=%s phrase='%s' variants=%u threshold=%.2f rate=%d chunk=%d",
             model_name, WAKE_WORD_CN_LABEL,
             (unsigned)(sizeof(s_wake_word_cn_phonetics) / sizeof(s_wake_word_cn_phonetics[0])),
             (double)WAKE_WORD_DETECTION_THRESHOLD, sample_rate, chunk_samples);
    multinet->print_active_speech_commands(model_data);
    bool model_was_paused = false;
    int64_t last_detection_us = 0;
    int64_t last_audio_diagnostic_us = 0;
    int16_t last_valid_wake_sample = 0;
    uint32_t wake_gain_q8 = WAKE_WORD_MAX_SOFTWARE_GAIN_Q8;
    while (true) {
        if (s_wake_word_stop_requested) break;
        if (s_wake_word_paused) {
            if (!model_was_paused) {
                multinet->clean(model_data);
                model_was_paused = true;
            }
            s_wake_word_pause_acknowledged = true;
            vTaskDelay(pdMS_TO_TICKS(20));
            continue;
        }
        model_was_paused = false;
        s_wake_word_pause_acknowledged = false;
        if (!s_audio_mutex || xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(50)) != pdTRUE) {
            continue;
        }
        size_t received = 0;
        esp_err_t read_err = i2s_channel_read(
            s_audio_rx, tdm,
            (size_t)chunk_samples * ECHOEAR_MIC_SLOT_COUNT * sizeof(int16_t),
            &received, pdMS_TO_TICKS(250));
        xSemaphoreGive(s_audio_mutex);
        if (read_err != ESP_OK) {
            if (read_err != ESP_ERR_TIMEOUT) {
                ESP_LOGW(TAG, "offline wake microphone read failed: %s", esp_err_to_name(read_err));
            }
            continue;
        }

        size_t frames = received /
                        (sizeof(int16_t) * ECHOEAR_MIC_SLOT_COUNT);
        if (frames < (size_t)chunk_samples) {
            memset(tdm + frames * ECHOEAR_MIC_SLOT_COUNT, 0,
                   ((size_t)chunk_samples - frames) * ECHOEAR_MIC_SLOT_COUNT *
                       sizeof(int16_t));
        }
        // Feed MultiNet the codec PCM directly, following Fangtang's working
        // recognizer path. The recording high-pass/make-up chain is useful for
        // uploaded speech, but on EchoEar it amplified idle noise close to full
        // scale and destroyed MultiNet's expected feature range. EchoEar's
        // verified speech capsule is the first ES7210 TDM slot; remove its DC bias and
        // restore the feature scale that MultiNet receives on Fangtang. The
        // correctly clocked EchoEar codec is very quiet (idle MAD in the tens),
        // so use a bounded block gain derived from the idle floor instead of a
        // fixed analogue gain that can clip loud nearby speech.
        int64_t slot_sum[ECHOEAR_MIC_SLOT_COUNT] = {0};
        uint32_t slot_valid[ECHOEAR_MIC_SLOT_COUNT] = {0};
        uint16_t slot_bad[ECHOEAR_MIC_SLOT_COUNT] = {0};
        int32_t slot_peak[ECHOEAR_MIC_SLOT_COUNT] = {0};
        for (int i = 0; i < chunk_samples; ++i) {
            for (unsigned slot = 0; slot < ECHOEAR_MIC_SLOT_COUNT; ++slot) {
                int32_t sample = tdm[i * ECHOEAR_MIC_SLOT_COUNT + slot];
                int32_t magnitude = sample_magnitude(sample);
                if (magnitude > slot_peak[slot]) slot_peak[slot] = magnitude;
                if (magnitude < RECORDING_INVALID_SAMPLE_ABS) {
                    slot_sum[slot] += sample;
                    ++slot_valid[slot];
                } else {
                    ++slot_bad[slot];
                }
            }
        }
        int32_t slot_dc[ECHOEAR_MIC_SLOT_COUNT] = {0};
        uint64_t slot_energy[ECHOEAR_MIC_SLOT_COUNT] = {0};
        for (unsigned slot = 0; slot < ECHOEAR_MIC_SLOT_COUNT; ++slot) {
            if (slot_valid[slot]) {
                slot_dc[slot] = (int32_t)(slot_sum[slot] / slot_valid[slot]);
            }
        }
        for (int i = 0; i < chunk_samples; ++i) {
            for (unsigned slot = 0; slot < ECHOEAR_MIC_SLOT_COUNT; ++slot) {
                int32_t sample = tdm[i * ECHOEAR_MIC_SLOT_COUNT + slot];
                if (sample_magnitude(sample) < RECORDING_INVALID_SAMPLE_ABS) {
                    int32_t centered = sample - slot_dc[slot];
                    slot_energy[slot] += (uint64_t)((int64_t)centered * centered);
                }
            }
        }
        const unsigned selected = ECHOEAR_MIC_SELECTED_SLOT;
        uint32_t mean_square = (uint32_t)(slot_energy[selected] / (uint32_t)chunk_samples);
        uint32_t rms = (uint32_t)sqrtf((float)mean_square);
        if (rms >= WAKE_WORD_GAIN_UPDATE_FLOOR) {
            uint32_t target_q8 = (WAKE_WORD_TARGET_RMS * 256u) / rms;
            if (target_q8 < WAKE_WORD_MIN_SOFTWARE_GAIN_Q8) {
                target_q8 = WAKE_WORD_MIN_SOFTWARE_GAIN_Q8;
            }
            if (target_q8 > WAKE_WORD_MAX_SOFTWARE_GAIN_Q8) {
                target_q8 = WAKE_WORD_MAX_SOFTWARE_GAIN_Q8;
            }
            unsigned shift = target_q8 < wake_gain_q8
                                 ? WAKE_WORD_GAIN_ATTACK_SHIFT
                                 : WAKE_WORD_GAIN_RELEASE_SHIFT;
            wake_gain_q8 = (uint32_t)((int32_t)wake_gain_q8 +
                                      ((int32_t)target_q8 - (int32_t)wake_gain_q8) /
                                          (1 << shift));
        }
        for (int i = 0; i < chunk_samples; ++i) {
            int32_t input = tdm[i * ECHOEAR_MIC_SLOT_COUNT + selected];
            int32_t sample;
            if (sample_magnitude(input) < RECORDING_INVALID_SAMPLE_ABS) {
                sample = (int32_t)(((int64_t)(input - slot_dc[selected]) *
                                    wake_gain_q8) >> 8);
            } else {
                sample = last_valid_wake_sample;
            }
            if (sample > INT16_MAX) sample = INT16_MAX;
            if (sample < INT16_MIN) sample = INT16_MIN;
            last_valid_wake_sample = (int16_t)sample;
            mono[i] = (int16_t)sample;
        }
        int64_t diagnostic_now_us = esp_timer_get_time();
        if (diagnostic_now_us - last_audio_diagnostic_us >=
            WAKE_WORD_DIAGNOSTIC_INTERVAL_US) {
            last_audio_diagnostic_us = diagnostic_now_us;
            ESP_LOGI(TAG,
                     "offline wake mic: S0 peak=%ld rms=%lu bad=%u; "
                     "S1=%ld S2=%ld S3=%ld; selected=S%u gain=%.2f",
                     (long)slot_peak[0], (unsigned long)rms, slot_bad[0],
                     (long)slot_peak[1], (long)slot_peak[2], (long)slot_peak[3],
                     selected, (double)wake_gain_q8 / 256.0);
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
            ESP_LOGI(TAG, "offline wake word detected: %s phrase=%d text='%s' raw='%s' (prob=%.3f)",
                     WAKE_WORD_CN_LABEL, result->phrase_id[0], result->string,
                     result->raw_string, (double)result->prob[0]);
            multinet->clean(model_data);
            // Do not create the foreground task while MultiNet still owns its
            // large internal allocations.  Queue a callback after this task
            // has released the recognizer, matching the physical-key path
            // which stops wake recognition before it creates the worker.
            taskENTER_CRITICAL(&s_wake_word_lock);
            bool dispatch_pending = s_wake_callback_pending || s_wake_callback_task;
            if (!dispatch_pending) {
                s_wake_callback_pending = true;
                s_wake_word_stop_requested = true;
            }
            taskEXIT_CRITICAL(&s_wake_word_lock);
            if (!dispatch_pending) {
                TaskHandle_t dispatcher = NULL;
                BaseType_t created = xTaskCreateWithCaps(
                    wake_callback_dispatch_task, "maclaw_wake_dispatch", 3072,
                    NULL, 5, &dispatcher, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
                taskENTER_CRITICAL(&s_wake_word_lock);
                if (created == pdPASS) {
                    s_wake_callback_task = dispatcher;
                } else {
                    // Keep the device responsive if PSRAM is temporarily
                    // fragmented; the next detected phrase can retry safely.
                    s_wake_callback_pending = false;
                    ESP_LOGE(TAG, "cannot queue offline wake callback");
                }
                taskEXIT_CRITICAL(&s_wake_word_lock);
            }
        } else {
            multinet->clean(model_data);
        }
    }

    heap_caps_free(mono);
    heap_caps_free(tdm);
    multinet->destroy(model_data);
    esp_srmodel_deinit(models);
    ESP_LOGI(TAG, "offline wake stopped and model memory released");

finish:
    taskENTER_CRITICAL(&s_wake_word_lock);
    s_wake_word_stop_requested = false;
    s_wake_word_paused = false;
    s_wake_word_pause_acknowledged = false;
    s_wake_word_ready = false;
    s_wake_word_task = NULL;
    taskEXIT_CRITICAL(&s_wake_word_lock);
    vTaskDelete(NULL);
}

esp_err_t board_port_start_wake_word(board_port_wake_word_cb_t on_wake, void *arg) {
    if (!on_wake) return ESP_ERR_INVALID_ARG;
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
    taskENTER_CRITICAL(&s_wake_word_lock);
    if (s_wake_word_task || s_wake_word_task_starting) {
        bool starting = s_wake_word_task_starting;
        bool ready = s_wake_word_ready;
        taskEXIT_CRITICAL(&s_wake_word_lock);
        /* Do not equate an allocated task with a ready recognizer. A restart
         * request can arrive while MultiNet is still loading; wait for its
         * explicit readiness signal and propagate timeout/failure accurately. */
        for (unsigned i = 0; i < 400 && (s_wake_word_task || starting) && !ready; ++i) {
            vTaskDelay(pdMS_TO_TICKS(25));
            taskENTER_CRITICAL(&s_wake_word_lock);
            starting = s_wake_word_task_starting;
            ready = s_wake_word_ready;
            taskEXIT_CRITICAL(&s_wake_word_lock);
        }
        if (ready) return ESP_OK;
        return (s_wake_word_task || starting) ? ESP_ERR_TIMEOUT : ESP_FAIL;
    }
    s_wake_word_task_starting = true;
    s_on_wake_word = on_wake;
    s_on_wake_word_arg = arg;
    s_wake_word_paused = false;
    s_wake_word_pause_acknowledged = false;
    s_wake_word_ready = false;
    s_wake_word_stop_requested = false;
    taskEXIT_CRITICAL(&s_wake_word_lock);
    TaskHandle_t task = NULL;
    BaseType_t created = xTaskCreatePinnedToCore(wake_word_task, "maclaw_offline_wake",
                                                 10240, NULL, 4, &task, 1);
    taskENTER_CRITICAL(&s_wake_word_lock);
    if (created != pdPASS) {
        s_wake_word_task_starting = false;
        s_on_wake_word = NULL;
        s_on_wake_word_arg = NULL;
        taskEXIT_CRITICAL(&s_wake_word_lock);
        return ESP_ERR_NO_MEM;
    }
    s_wake_word_task = task;
    s_wake_word_task_starting = false;
    taskEXIT_CRITICAL(&s_wake_word_lock);
    for (unsigned i = 0;
         i < 400 && s_wake_word_task && !s_wake_word_ready; ++i) {
        vTaskDelay(pdMS_TO_TICKS(25));
    }
    if (s_wake_word_ready) {
        ESP_LOGI(TAG, "offline wake task ready");
        return ESP_OK;
    }
    if (!s_wake_word_task) {
        ESP_LOGW(TAG, "offline wake task exited during model initialization");
        return ESP_FAIL;
    }
    ESP_LOGW(TAG, "offline wake model initialization timed out");
    return ESP_ERR_TIMEOUT;
}

esp_err_t board_port_stop_wake_word(void) {
    taskENTER_CRITICAL(&s_wake_word_lock);
    if (!s_wake_word_task && !s_wake_word_task_starting) {
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
    for (unsigned i = 0;
         i < 240 && (s_wake_word_task || s_wake_word_task_starting); ++i) {
        vTaskDelay(pdMS_TO_TICKS(25));
    }
    if (s_wake_word_task || s_wake_word_task_starting) return ESP_ERR_TIMEOUT;
    taskENTER_CRITICAL(&s_wake_word_lock);
    s_on_wake_word = NULL;
    s_on_wake_word_arg = NULL;
    taskEXIT_CRITICAL(&s_wake_word_lock);
    ESP_LOGI(TAG, "offline wake task stopped cleanly");
    return ESP_OK;
}

esp_err_t board_port_capture_wav(uint8_t **out_wav, size_t *out_len) {
    if (out_wav) *out_wav = NULL;
    if (out_len) *out_len = 0;
    if (!out_wav || !out_len) return ESP_ERR_INVALID_ARG;
    board_port_pause_wake_word(true);
    // MultiNet reads from this I2S RX channel in the background. It must
    // acknowledge the pause before foreground capture takes the mutex, or it
    // can consume the first command frame in the hand-off window.
    for (unsigned i = 0;
         s_wake_word_task && !s_wake_word_pause_acknowledged && i < 40; ++i) {
        vTaskDelay(pdMS_TO_TICKS(5));
    }
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
    recording_pcm_reset("command");

    const size_t max_samples = AUDIO_RATE * COMMAND_CAPTURE_MAX_SECONDS;
    const size_t start_timeout_samples =
        AUDIO_RATE * COMMAND_CAPTURE_START_TIMEOUT_MS / 1000;
    const size_t silence_samples = AUDIO_RATE * COMMAND_CAPTURE_SILENCE_MS / 1000;
    const size_t start_confirm_samples =
        AUDIO_RATE * COMMAND_CAPTURE_START_CONFIRM_MS / 1000;
    const size_t preroll_samples = AUDIO_RATE * COMMAND_CAPTURE_PREROLL_MS / 1000;
    const size_t wav_capacity = 44 + max_samples * sizeof(int16_t);
    // A 30-second mono command is almost 1 MiB. Keep that payload in PSRAM so
    // command capture cannot consume the small internal/DMA heap needed by
    // Wi-Fi and mbedTLS immediately afterwards. The recorder writes it only as
    // ordinary byte-addressable PCM; it is never an I2S DMA descriptor.
    uint8_t *wav = heap_caps_malloc(wav_capacity,
                                    MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!wav) {
        xSemaphoreGive(s_audio_mutex);
        board_port_pause_wake_word(false);
        return ESP_ERR_NO_MEM;
    }
    memset(wav, 0, 44);
    memcpy(wav, "RIFF", 4);
    uint32_t riff_size = (uint32_t)wav_capacity - 8;
    memcpy(wav + 4, &riff_size, 4);
    memcpy(wav + 8, "WAVEfmt ", 8);
    uint32_t fmt_size = 16; uint16_t pcm = 1, channels = 1, bits = 16;
    uint32_t rate = AUDIO_RATE, byte_rate = AUDIO_RATE * 2; uint16_t align = 2;
    memcpy(wav + 16, &fmt_size, 4); memcpy(wav + 20, &pcm, 2);
    memcpy(wav + 22, &channels, 2); memcpy(wav + 24, &rate, 4);
    memcpy(wav + 28, &byte_rate, 4); memcpy(wav + 32, &align, 2);
    memcpy(wav + 34, &bits, 2); memcpy(wav + 36, "data", 4);
    uint32_t data_size = (uint32_t)(max_samples * 2);
    memcpy(wav + 40, &data_size, 4);

    int16_t tdm[512];
    int16_t *mono = (int16_t *)(wav + 44);
    size_t written_samples = 0;
    size_t voiced_samples = 0;
    size_t silence_samples_seen = 0;
    size_t speech_start_sample = 0;
    bool speech_started = false;
    int32_t peak = 0;
    // Bread Compact's command reader receives a 512-frame (32 ms) block and
    // applies a capture-side attack/release filter before handing the value to
    // the shared recorder, which applies its own filter. EchoEar receives four
    // 128-frame TDM blocks in that same interval, so aggregate the peak first:
    // filtering each TDM block would make the meter four times more responsive
    // than the reference board rather than matching its visible cadence.
    uint16_t capture_smoothed_level = 0;
    uint16_t meter_pending_peak = 0;
    size_t meter_pending_samples = 0;
    uint16_t idle_level = 0;
    uint32_t last_ui_second = UINT32_MAX;
    s_command_capture_active = true;
    while (written_samples < max_samples) {
        size_t received = 0;
        esp_err_t err = i2s_channel_read(s_audio_rx, tdm, sizeof(tdm), &received,
                                         pdMS_TO_TICKS(1000));
        if (err != ESP_OK) {
            s_command_capture_active = false;
            free(wav);
            xSemaphoreGive(s_audio_mutex);
            board_port_pause_wake_word(false);
            return err;
        }
        size_t frames = received /
                        (sizeof(int16_t) * ECHOEAR_MIC_SLOT_COUNT);
        if (frames == 0) {
            // A successful I2S read may still return no complete TDM frame.
            // Do not feed the VAD state or pointer arithmetic with an empty
            // chunk; simply keep waiting within the existing time bounds.
            continue;
        }
        if (frames > max_samples - written_samples) frames = max_samples - written_samples;
        int32_t chunk_peak = recording_pcm_process(tdm, frames,
                                                   &mono[written_samples]);
        written_samples += frames;
        if (chunk_peak > peak) peak = chunk_peak;
        // recording_pcm_process() brings the quiet EchoEar capsule into the
        // same 10k-12k peak range used by the common UI/VAD thresholds.
        uint16_t raw_level = chunk_peak <= 180 ? 0
                             : (uint16_t)(((chunk_peak - 180) * 1000) / (12000 - 180));
        if (raw_level > 1000) raw_level = 1000;
        uint32_t elapsed = (uint32_t)(written_samples / AUDIO_RATE);
        if (raw_level > meter_pending_peak) meter_pending_peak = raw_level;
        meter_pending_samples += frames;
        if (meter_pending_samples >= RECORDING_WAVE_SAMPLES_PER_COLUMN) {
            capture_smoothed_level = meter_pending_peak > capture_smoothed_level
                                         ? (uint16_t)((capture_smoothed_level +
                                                       meter_pending_peak * 3u) / 4u)
                                         : (uint16_t)((capture_smoothed_level * 7u +
                                                       meter_pending_peak) / 8u);
            // This intentionally matches Bread Compact's short-command path:
            // one filter in capture and a second one in the common recording
            // UI. VAD below continues to use raw_level, preserving capture
            // start/stop sensitivity.
            board_port_set_audio_level(capture_smoothed_level, elapsed);
            meter_pending_peak = 0;
            meter_pending_samples = 0;
        }
        // Command capture is synchronous, unlike the meeting stream.  Keep the
        // shared recording surface alive just as Bread Compact does so timer,
        // MIC readout and PCM waveform advance together instead of relying on
        // the unrelated 150 ms standby animation task.
        if (elapsed != last_ui_second) {
            board_port_set_recording_visual(true, false, elapsed);
            last_ui_second = elapsed;
        }
        // Hysteresis ignores short loud clicks before speech, then allows
        // natural intra-word dips without treating them as the command end.
        const uint16_t mean_level = command_capture_mean_level(
            &mono[written_samples - frames], frames);
        if (!speech_started) {
            // Learn the room's actual post-filter noise floor before speech.
            // The minimum avoids a spoken preamble raising the completion
            // threshold and keeps the next quiet pause recognisable.
            if (idle_level == 0 || mean_level < idle_level) idle_level = mean_level;
            voiced_samples = raw_level >= COMMAND_CAPTURE_START_LEVEL
                                 ? voiced_samples + frames
                                 : 0;
            if (voiced_samples >= start_confirm_samples) {
                speech_started = true;
                silence_samples_seen = 0;
                speech_start_sample = written_samples - voiced_samples;
                ESP_LOGI(TAG, "command speech started after %u ms",
                         (unsigned)(written_samples * 1000 / AUDIO_RATE));
            } else if (written_samples >= start_timeout_samples) {
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
            silence_samples_seen = mean_level <= silence_level
                                       ? silence_samples_seen + frames
                                       : 0;
            if (silence_samples_seen >= silence_samples) {
                ESP_LOGI(TAG,
                         "command capture ended after %u ms of silence (mean=%u threshold=%u)",
                         COMMAND_CAPTURE_SILENCE_MS, mean_level, silence_level);
                break;
            }
        }
        if (s_command_capture_stop_requested) {
            ESP_LOGI(TAG, "command capture manually stopped: speech=%s elapsed=%ums",
                     speech_started ? "yes" : "no",
                     (unsigned)(written_samples * 1000 / AUDIO_RATE));
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
    const size_t captured_samples = written_samples - trim_start;
    if (trim_start > 0) {
        memmove(mono, mono + trim_start, captured_samples * sizeof(*mono));
    }
    ESP_LOGI(TAG, "captured %u mono samples (trimmed %u ms), peak=%ld",
             (unsigned)captured_samples,
             (unsigned)(trim_start * 1000 / AUDIO_RATE), (long)peak);
    const size_t wav_len = 44 + captured_samples * sizeof(*mono);
    riff_size = (uint32_t)wav_len - 8;
    memcpy(wav + 4, &riff_size, sizeof(riff_size));
    data_size = (uint32_t)(captured_samples * sizeof(*mono));
    memcpy(wav + 40, &data_size, sizeof(data_size));
    xSemaphoreGive(s_audio_mutex);
    board_port_pause_wake_word(false);
    *out_wav = wav;
    *out_len = wav_len;
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

esp_err_t board_port_stop_background_tasks(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_background_tasks_lock ||
        xSemaphoreTake(s_background_tasks_lock, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    if (!s_pet_animation_task) {
        xSemaphoreGive(s_background_tasks_lock);
        return ESP_OK;
    }
    if (xTaskGetCurrentTaskHandle() == s_pet_animation_task) {
        xSemaphoreGive(s_background_tasks_lock);
        return ESP_ERR_INVALID_STATE;
    }
    xTaskNotifyGive(s_pet_animation_task);
    if (!s_pet_animation_stopped ||
        xSemaphoreTake(s_pet_animation_stopped, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        xSemaphoreGive(s_background_tasks_lock);
        ESP_LOGW(TAG, "timed out stopping board pet animation task");
        return ESP_ERR_TIMEOUT;
    }
    vSemaphoreDelete(s_pet_animation_stopped);
    s_pet_animation_stopped = NULL;
    s_pet_animation_task = NULL;
    s_pet_animation_started = false;
    xSemaphoreGive(s_background_tasks_lock);
    ESP_LOGI(TAG, "board pet animation task stopped");
    return ESP_OK;
}

// Bread Compact presents only rows/columns that changed from the previous
// composed frame. EchoEar keeps the same ownership rule but retains its
// conservative 20 MHz QSPI and bounce-buffered DMA path. This cuts a normal
// remote-pet tick from a full 360x360 transfer down to the pet's changing
// rectangle, which makes the authored 80 ms in-between frames look fluid
// without reintroducing the old panel stripe/flash failure.
static esp_err_t present_pet_frame_delta_sync(const uint16_t *frame) {
    if (!s_panel || !frame) return ESP_ERR_INVALID_ARG;
    if (!s_front_frame_valid || !s_framebuffers[s_front_framebuffer]) {
        return present_frame_sync(frame);
    }
    const uint16_t *previous = s_framebuffers[s_front_framebuffer];
    for (int y = 0; y < LCD_HEIGHT; y += LCD_STRIPE_ROWS) {
        const int rows = (LCD_HEIGHT - y) < LCD_STRIPE_ROWS
                             ? (LCD_HEIGHT - y) : LCD_STRIPE_ROWS;
        int first_changed = -1;
        int last_changed = -1;
        int left_changed = LCD_WIDTH;
        int right_changed = -1;
        for (int row = 0; row < rows; ++row) {
            const uint16_t *next_row = frame + (size_t)(y + row) * LCD_WIDTH;
            const uint16_t *old_row = previous + (size_t)(y + row) * LCD_WIDTH;
            if (memcmp(next_row, old_row, LCD_WIDTH * sizeof(uint16_t)) == 0) continue;
            if (first_changed < 0) first_changed = row;
            last_changed = row;
            int left = 0;
            while (left < LCD_WIDTH && next_row[left] == old_row[left]) ++left;
            int right = LCD_WIDTH - 1;
            while (right > left && next_row[right] == old_row[right]) --right;
            if (left < left_changed) left_changed = left;
            if (right > right_changed) right_changed = right;
        }
        if (first_changed < 0) continue;
        const int changed_rows = last_changed - first_changed + 1;
        const int changed_width = right_changed - left_changed + 1;
        for (int row = 0; row < changed_rows; ++row) {
            memcpy(s_line + (size_t)row * changed_width,
                   frame + (size_t)(y + first_changed + row) * LCD_WIDTH + left_changed,
                   (size_t)changed_width * sizeof(uint16_t));
        }
        esp_err_t err = draw_bitmap_sync(left_changed, y + first_changed,
                                         right_changed + 1,
                                         y + first_changed + changed_rows,
                                         s_line);
        if (err != ESP_OK) return err;
    }
    return ESP_OK;
}

void board_port_set_network_transport(bool cellular) {
    (void)cellular;
}

void board_port_set_service_ready(bool ready) {
    bool changed;
    taskENTER_CRITICAL(&s_state_lock);
    changed = s_service_ready != ready;
    s_service_ready = ready;
    if (changed) ++s_ambient_revision;
    taskEXIT_CRITICAL(&s_state_lock);
    if (changed && !s_display_sleeping && !s_recording_active &&
        !s_setup_qrcode_visible && !s_command_display_locked &&
        ambient_visible_for_state()) {
        draw_pet();
    }
    ESP_LOGI(TAG, "gateway service: %s", ready ? "ready" : "not ready");
}

bool board_port_get_power_status(unsigned *level_percent, bool *charging) {
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    if (!s_axp2101) return false;
    return axp2101_read_power_status(level_percent, charging);
#else
    (void)level_percent;
    (void)charging;
    return false;
#endif
}

esp_err_t board_port_motion_get_sample(device_motion_sample_t *out_sample) {
    if (!out_sample) return ESP_ERR_INVALID_ARG;
#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
    if (!s_qmi8658) return ESP_ERR_NOT_FOUND;
    uint8_t data[12] = {0};
    ESP_RETURN_ON_ERROR(qmi8658_read(QMI8658_REG_DATA, data, sizeof(data)), TAG,
                        "read QMI8658 motion sample");
    /* The adapter configures QMI8658 at +/-8 g and +/-1024 dps.  Convert
     * sensor LSBs into the Device API's integer engineering units here, once,
     * rather than forcing every business consumer to understand its range. */
    const int32_t acceleration_mg_per_lsb_num = 8000;
    const int32_t angular_rate_mdps_per_lsb_num = 1024000;
    out_sample->timestamp_us = (uint64_t)esp_timer_get_time();
    out_sample->acceleration_mg_x =
        (int32_t)qmi8658_decode_i16(&data[0]) * acceleration_mg_per_lsb_num / 32768;
    out_sample->acceleration_mg_y =
        (int32_t)qmi8658_decode_i16(&data[2]) * acceleration_mg_per_lsb_num / 32768;
    out_sample->acceleration_mg_z =
        (int32_t)qmi8658_decode_i16(&data[4]) * acceleration_mg_per_lsb_num / 32768;
    out_sample->angular_rate_mdps_x =
        (int32_t)qmi8658_decode_i16(&data[6]) * angular_rate_mdps_per_lsb_num / 32768;
    out_sample->angular_rate_mdps_y =
        (int32_t)qmi8658_decode_i16(&data[8]) * angular_rate_mdps_per_lsb_num / 32768;
    out_sample->angular_rate_mdps_z =
        (int32_t)qmi8658_decode_i16(&data[10]) * angular_rate_mdps_per_lsb_num / 32768;
    return ESP_OK;
#else
    return ESP_ERR_NOT_SUPPORTED;
#endif
}

void board_port_request_capture_stop(void) {
    // Retain a stop arriving after the application publishes RECORDING but
    // before the synchronous reader marks itself active.
    s_command_capture_stop_requested = true;
}

void board_port_reset_capture_stop(void) {
    s_command_capture_stop_requested = false;
}

void board_port_request_audio_playback_stop(void) {
    if (s_audio_playback_owner) s_audio_playback_stop_requested = true;
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
        size_t padded = (size_t)chunk_len + (chunk_len & 1u);
        if (padded > wav_len - offset) return ESP_ERR_INVALID_SIZE;
        offset += padded;
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
    size_t frame_bytes = channels * sizeof(int16_t);
    if (data_len % frame_bytes != 0) return ESP_ERR_INVALID_SIZE;
    esp_err_t err = board_port_audio_playback_begin();
    if (err == ESP_OK) {
        err = board_port_audio_playback_write((const int16_t *)data,
                                              data_len / frame_bytes,
                                              channels);
        err = board_port_audio_playback_end(err);
    }
    return err;
}

esp_err_t board_port_audio_playback_begin(void) {
    if (!s_audio_mutex) return ESP_ERR_INVALID_STATE;
    // Pause recognition before competing for the shared I2S lock. MultiNet may
    // already be inside a bounded microphone read; waiting for its explicit
    // acknowledgement lets it release the bus and clean its detector state
    // before speaker DMA starts. This prevents playback from racing a resumed
    // recognizer and improves wake reliability after a response finishes.
    board_port_pause_wake_word(true);
    for (unsigned i = 0;
         s_wake_word_task && !s_wake_word_pause_acknowledged && i < 60; ++i) {
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
    if (err == ESP_OK) err = es8311_set_dac_muted(true);
    if (err == ESP_OK) err = gpio_set_level(AUDIO_PA_ENABLE, 1);
    // Let the external amplifier and DAC reference settle while muted.
    if (err == ESP_OK) vTaskDelay(pdMS_TO_TICKS(10));
    if (err == ESP_OK) {
        s_audio_playback_stop_requested = false;
        s_audio_playback_owner = xTaskGetCurrentTaskHandle();
    } else {
        (void)gpio_set_level(AUDIO_PA_ENABLE, 0);
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
    /* This codec needs 32-bit I2S slots (64 BCLK/LRCK), while the configured
     * DMA data width and the Device API PCM contract remain signed 16-bit.
     * ESP-IDF uses data_bit_width, not slot_bit_width, for its DMA frame
     * payload, and emits the 16-bit samples in the configured left-aligned
     * slots.  Keep this buffer packed int16_t; expanding it would change the
     * DMA frame contract and corrupt the stream. */
    int16_t stereo[512];
    size_t offset = 0;
    while (offset < frames) {
        if (s_audio_playback_stop_requested) return ESP_ERR_INVALID_STATE;
        size_t count = frames - offset;
        if (count > 256) count = 256;
        for (size_t i = 0; i < count; ++i) {
            int16_t left = pcm[(offset + i) * channels];
            int16_t right = channels == 2 ? pcm[(offset + i) * 2 + 1] : left;
            stereo[i * 2] = left;
            stereo[i * 2 + 1] = right;
        }
        size_t written = 0;
        size_t expected = count * 2 * sizeof(int16_t);
        esp_err_t err = i2s_channel_write(s_audio_tx, stereo, expected, &written,
                                          pdMS_TO_TICKS(1000));
        if (err != ESP_OK) return err;
        if (written != expected) return ESP_ERR_TIMEOUT;
        if (offset == 0) {
            // The first block was accepted by DMA while the PA/reference was
            // settling. Reveal audio only after it has a valid PCM source.
            err = es8311_set_dac_muted(false);
            if (err != ESP_OK) return err;
        }
        offset += count;
    }
    return ESP_OK;
}

esp_err_t board_port_audio_playback_end(esp_err_t playback_err) {
    if (s_audio_playback_owner != xTaskGetCurrentTaskHandle()) {
        return ESP_ERR_INVALID_STATE;
    }
    // i2s_channel_write only queues DMA.  Allow its final descriptor to
    // leave the peripheral, then send an explicit short zero tail before
    // shutting the EchoEar PA down.  Bread Compact can also stop its TX
    // channel, but EchoEar's shared full-duplex clock requires that channel
    // to remain enabled for the ES7210 microphone.
    vTaskDelay(pdMS_TO_TICKS(20));
    int16_t silence[256] = {0};
    esp_err_t silence_err = board_port_audio_playback_write(silence, 256, 1);
    vTaskDelay(pdMS_TO_TICKS(10));
    esp_err_t mute_err = es8311_set_dac_muted(true);
    vTaskDelay(pdMS_TO_TICKS(5));
    esp_err_t pa_err = gpio_set_level(AUDIO_PA_ENABLE, 0);
    s_audio_playback_owner = NULL;
    s_audio_playback_stop_requested = false;
    xSemaphoreGive(s_audio_mutex);
    board_port_pause_wake_word(false);
    if (playback_err != ESP_OK) return playback_err;
    if (silence_err != ESP_OK) return silence_err;
    if (mute_err != ESP_OK) return mute_err;
    return pa_err;
}

esp_err_t board_port_play_ack_chime(void) {
    esp_err_t err = board_port_audio_playback_begin();
    if (err != ESP_OK) return err;
    int16_t mono[256];
    // A short two-note acknowledgement: distinct enough to hear, soft enough
    // not to be confused with an alarm. The waveform is generated locally so
    // the board can confirm receipt before network TTS is available.
    for (int note = 0; err == ESP_OK && note < 2; ++note) {
        const int half_period = note == 0 ? 20 : 15; // 400 Hz, then ~533 Hz.
        for (int frame = 0; frame < AUDIO_RATE / 7; frame += 256) {
            int frames = (AUDIO_RATE / 7 - frame) > 256 ? 256 : (AUDIO_RATE / 7 - frame);
            for (int i = 0; i < frames; ++i) {
                int phase = (frame + i) % (half_period * 2);
                mono[i] = phase < half_period ? 2600 : -2600;
            }
            err = board_port_audio_playback_write(mono, (size_t)frames, 1);
        }
    }
    return board_port_audio_playback_end(err);
}

esp_err_t board_port_play_alarm_burst(void) {
    esp_err_t err = board_port_audio_playback_begin();
    if (err != ESP_OK) return err;
    int16_t mono[256];
    // Alternating ~1.7/2.1 kHz square waves with a decaying envelope mimic the
    // bright impact and chatter of a traditional twin-bell mechanism.
    for (int strike = 0; strike < 3 && err == ESP_OK; ++strike) {
        int half_period = strike & 1 ? 4 : 5;
        for (int frame = 0; frame < AUDIO_RATE / 12; frame += 256) {
            int frames = AUDIO_RATE / 12 - frame;
            if (frames > 256) frames = 256;
            for (int i = 0; i < frames; ++i) {
                int position = frame + i;
                int envelope = 8200 - position * 5;
                if (envelope < 1400) envelope = 1400;
                int16_t sample = ((position / half_period) & 1) ? envelope : -envelope;
                mono[i] = sample;
            }
            err = board_port_audio_playback_write(mono, (size_t)frames, 1);
        }
    }
    return board_port_audio_playback_end(err);
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
