#include "board_background_lifecycle.h"
#include "legacy_connectivity_transport.h"
#include "legacy_display_scene.h"
#include "legacy_storage_admission.h"
#include "legacy_bootstrap_input.h"
#include <ctype.h>
#include <inttypes.h>
#include <math.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "esp_check.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "provisioning_failure_injection.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "round_audio_service.h"
#include "round_display_service.h"
#include "round_input_service.h"
#include "round_peripheral_service.h"
#include "round_visual_profile_service.h"

/* The shared round renderer owns scene composition.  Geometry, raster and
 * layout are received only through the private visual-profile contract; audio
 * session defaults come from its dedicated Audio HAL service. */
#define LCD_WIDTH       (round_display_service_width())
#define LCD_HEIGHT      (round_display_service_height())
#define AUDIO_RATE      (round_audio_service_sample_rate())
#define LCD_STRIPE_ROWS (round_display_service_transfer_stripe_rows())
#define TEXT_SCALE      2
#define TEXT_ADVANCE    7
#define CJK_FONT_SIZE   ROUND_VISUAL_PROFILE_CJK24_CELL
#define CJK_ADVANCE     25
#define TEXT24_ASCII_ADVANCE 12
#define TEXT24_SPACE_ADVANCE 8
#define DYNAMIC_GLYPH_BYTES ROUND_VISUAL_PROFILE_CJK24_BYTES
#define DYNAMIC_GLYPH_CACHE_CAPACITY 128
#define COMPACT_FONT_SIZE 16
#define COMPACT_CJK_ADVANCE 17
#define COMPACT_ASCII_ADVANCE 12
static const round_display_layout_t *round_display_layout(void) {
    return round_visual_profile_display_layout();
}
#define ROUND_LAYOUT                 (round_display_layout())
#define ROUND_RECORDING_LAYOUT       (round_visual_profile_recording_layout())
#define ROUND_UPLOAD_LAYOUT          (round_visual_profile_upload_layout())
#define ROUND_ALARM_LAYOUT           (round_visual_profile_alarm_layout())
#define ROUND_MESSAGE_LAYOUT         (round_visual_profile_message_layout())
#define ROUND_RESPONSE_IMAGE_LAYOUT  (round_visual_profile_response_image_layout())
#define ROUND_QRCODE_LAYOUT          (round_visual_profile_qrcode_layout())
#define RESPONSE_TITLE_Y             (ROUND_LAYOUT->response_title_y)
#define RESPONSE_RULE_Y              (ROUND_LAYOUT->response_rule_y)
#define RESPONSE_TEXT_X              (ROUND_LAYOUT->response_text_x)
#define RESPONSE_TEXT_Y              (ROUND_LAYOUT->response_text_y)
#define RESPONSE_TEXT_W              (ROUND_LAYOUT->response_text_width)
#define RESPONSE_LINE_GAP            (ROUND_LAYOUT->response_line_gap)
#define RESPONSE_LINES_PER_PAGE      (ROUND_LAYOUT->response_lines_per_page)
#define RESPONSE_FOOTER_Y            (ROUND_LAYOUT->response_footer_y)
#define AMBIENT_TOP_W                (ROUND_LAYOUT->ambient_top_width)
#define AMBIENT_TOP_H                (ROUND_LAYOUT->ambient_top_height)
#define AMBIENT_BOTTOM_W             (ROUND_LAYOUT->ambient_bottom_width)
#define AMBIENT_BOTTOM_H             (ROUND_LAYOUT->ambient_bottom_height)
#define AMBIENT_BOTTOM_X             (ROUND_LAYOUT->ambient_bottom_x)
#define AMBIENT_BOTTOM_Y             (ROUND_LAYOUT->ambient_bottom_y)
#define AMBIENT_TOP_RING_RADIUS      (ROUND_LAYOUT->ambient_top_ring_radius)
#define AMBIENT_RING_RADIUS          (ROUND_LAYOUT->ambient_ring_radius)
#define PET_HALO_CENTER_Y            (ROUND_LAYOUT->pet_halo_center_y)
#define PET_HALO_RADIUS              (ROUND_LAYOUT->pet_halo_radius)
#define STANDBY_ART_CENTER_Y          (ROUND_LAYOUT->standby_art_center_y)
#define REMOTE_PET_TARGET            (ROUND_LAYOUT->remote_pet_target)
#define REMOTE_PET_TOP               (ROUND_LAYOUT->remote_pet_top)
#define REMOTE_PET_PROFILE_MAX_FRAMES (ROUND_LAYOUT->remote_pet_max_frames)
#define AMBIENT_OVERLAY_PIXELS       ((size_t)AMBIENT_TOP_W * (size_t)AMBIENT_TOP_H)
#define AMBIENT_OVERLAY_BYTES        (AMBIENT_OVERLAY_PIXELS * sizeof(uint16_t))
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
#define PET_ANIMATION_FRAME_MS (round_display_service_pet_animation_frame_ms())
#define REMOTE_PET_RENDER_FRAME_MS 80
// Bread Compact advances its 24-column history for each 512-sample I2S read
// (32 ms at 16 kHz), and immediately presents that change. EchoEar groups two
// native 256-sample stereo reads into the same history unit, so its round-screen
// renderer must use the same 32 ms cadence rather than the idle pet's 80 ms
// animation cadence; otherwise one frame visibly skips one or two bars.
#define RECORDING_RENDER_FRAME_MS \
    (RECORDING_WAVE_SAMPLES_PER_COLUMN * 1000u / AUDIO_RATE)
#define REMOTE_PET_DEFAULT_KEYFRAME_MS 450
#define READY_PROMPT_TIMEOUT_US (60LL * 1000 * 1000)
// The default procedural cat is shown on the permanent circular standby
// surface before a remotely selected pet has loaded.  Keep its head noticeably
// inside the top date/status ring: the former 103px face plus 55px ears reached
// into the header and visually collided with the date.  The local fallback is
// intentionally more compact than a downloaded full-frame pet.
static const char *TAG = "maclaw_board";
static SemaphoreHandle_t s_background_tasks_lock;
/* A failed startup can still receive late shared-UI publications while it is
 * draining local services.  Once lifecycle closes this admission, no such
 * publication may recreate a decorative renderer task behind the rollback.
 * This is intentionally narrower than a board-port deinit: panel/audio/I2C
 * resources remain boot-lifetime and the diagnostic surface stays valid. */
static bool s_background_tasks_admission_closed;

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
static bool s_recording_paused;
static volatile uint32_t s_recording_elapsed_seconds;
// Same attack/release smoothing as Bread Compact's recorder. The same level
// history feeds both the visible waveform and MIC readout, so one spoken
// block has one consistent amplitude everywhere on the recording surface.
static uint16_t s_recording_smoothed_level;
// Keep the same 24-column, 32 ms filtered-level history that Bread Compact
// presents. EchoEar groups its native 16 ms stereo reads into the same visual
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
// Round display drawing and the shared stripe buffer cannot be used concurrently.
static SemaphoreHandle_t s_lcd_mutex;
#define s_line (round_display_service_stripe_buffer())
// Two complete RGB565 frames live in DMA-capable PSRAM. While the LCD sends
// one frame, the renderer composes the next one into the other buffer. This
// replaces hundreds of tiny scan-line transactions per pet frame with one
// contiguous DMA transfer and prevents the source pixels changing in flight.
static uint16_t *s_framebuffers[2];
static uint8_t s_next_framebuffer;
static uint8_t s_front_framebuffer;
static bool s_front_frame_valid;
// A profile may own the boot artwork, but never the ambient standby scene.
// Keep that handoff explicit: while this flag is set a stale delta/direct
// overlay cannot make the boot totem look like the selected pet.
static bool s_startup_surface_visible;
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
// Status text gets one immutable overlay buffer.  It is never reused before a
// synchronous transfer completes.  The 466x162 AMOLED header needs 151 KiB,
// which must live in PSRAM; the 360px EchoEar keeps its smaller DMA-safe copy
// in internal RAM.  Do not reuse s_line here: that buffer is also used by the
// full-frame stripe presenter.
static uint16_t *s_ambient_overlay;
static volatile uint32_t s_skipped_pet_frames;
// The meeting stream owns the audio mutex from start to stop.  Track that
// ownership explicitly (as Bread Compact does) so an error path or duplicate
// stop cannot release a mutex held by an unrelated wake/audio operation.
static volatile bool s_command_display_locked;
static bool s_recording_is_meeting;

static esp_err_t draw_bitmap_sync(int x0, int y0, int x1, int y1,
                                   const void *pixels);
static esp_err_t present_pet_frame_delta_sync(const uint16_t *frame);
static bool draw_remote_pet(void);
static unsigned response_page_count(void);
static void draw_response_page(void);
static void draw_recording_visual(void);

// Every frame present uses the same completion fence. Keeping this in one
// helper prevents the pet and recording paths from drifting apart, and makes
// a failed submission unable to consume a completion from a later transfer.
static esp_err_t present_frame_sync(const uint16_t *frame) {
    if (!round_display_service_ready() || !frame) return ESP_ERR_INVALID_ARG;
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
    if (!round_display_service_ready() || !pixels) return ESP_ERR_INVALID_ARG;
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
    return round_display_service_draw_bitmap_sync(x0, y0, x1, y1, pixels);
}

static uint16_t rgb565(uint8_t r, uint8_t g, uint8_t b) {
    return round_display_service_rgb565(r, g, b);
}

// Interpolate in the panel's native RGB565 space. The previous pet renderer
// used only a handful of flat fills, which made a 65K-colour panel look like a
// 16-colour display. Per-scanline interpolation keeps the transfer format and
// memory footprint unchanged while exposing the colour depth that is already
// available in the ST77916 pipeline.
static uint16_t rgb565_lerp(uint16_t from, uint16_t to, uint16_t amount) {
    return round_display_service_rgb565_lerp(from, to, amount);
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
    const int reference = ROUND_LAYOUT->scene_reference_width;
    return reference > 0 ? (x * LCD_WIDTH + reference / 2) / reference : x;
}

static int round_scene_y(int y) {
    const int reference = ROUND_LAYOUT->scene_reference_height;
    return reference > 0 ? (y * LCD_HEIGHT + reference / 2) / reference : y;
}

static void fill_rect(int x0, int y0, int x1, int y1, uint16_t color) {
    if (!round_display_service_ready()) return;
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
    if (!round_display_service_ready() || !text) return;
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
int legacy_display_scene_cache_glyph(uint32_t codepoint, const uint8_t bitmap[DYNAMIC_GLYPH_BYTES]) {
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
    if (!round_display_service_ready() || !text) return;
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
            const uint32_t *rows = round_visual_profile_cjk24_rows(cp);
            uint8_t dynamic_bitmap[DYNAMIC_GLYPH_BYTES];
            const uint8_t *dynamic = !rows &&
                                     (dynamic_glyph_copy(cp, dynamic_bitmap) ||
                                      round_visual_profile_copy_cjk24(cp, dynamic_bitmap))
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
            const uint32_t *rows = round_visual_profile_cjk24_rows(cp);
            uint8_t dynamic_bitmap[DYNAMIC_GLYPH_BYTES];
            const uint8_t *dynamic = !rows &&
                                     (dynamic_glyph_copy(cp, dynamic_bitmap) ||
                                      round_visual_profile_copy_cjk24(cp, dynamic_bitmap))
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

        const uint32_t *rows = round_visual_profile_cjk24_rows(cp);
        uint8_t dynamic_bitmap[DYNAMIC_GLYPH_BYTES];
        const uint8_t *dynamic = !rows &&
                                 (dynamic_glyph_copy(cp, dynamic_bitmap) ||
                                  round_visual_profile_copy_cjk24(cp, dynamic_bitmap))
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
/* The lower information ring is the only surface that needs a larger native
 * cell on the Waveshare AMOLED. Product scenes and Device API glyph payloads
 * remain common; the selected profile only chooses its physical raster. */
static void compose_text24_curve(uint16_t *target, int stride, int width, int height,
                                 int center_x, int apex_y, int arc_radius,
                                 const char *text, uint16_t fg) {
    if (!target || !text || !text[0] || arc_radius == 0) return;
    const int radius = arc_radius < 0 ? -arc_radius : arc_radius;
    const int scale_num = ROUND_LAYOUT->curve_glyph_scale_num;
    const int scale_den = ROUND_LAYOUT->curve_glyph_scale_den;
    const int source_cell = (int)round_visual_profile_curve_glyph_cell();
    const int cell = source_cell * scale_num / scale_den;
    uint32_t glyphs[32] = {0};
    int advances[32] = {0};
    int count = 0;
    int total = 0;
    const char *cursor = text;
    while (*cursor && count < (int)(sizeof(glyphs) / sizeof(glyphs[0]))) {
        uint32_t cp = utf8_next(&cursor);
        glyphs[count] = cp;
        const int base_advance = cp == ' ' ? 8 : cp == 0x00B0 ? 10
                               : cp < 0x80 ? TEXT24_ASCII_ADVANCE : CJK_ADVANCE;
        /* Keep the logical inter-glyph gap proportional to the selected
         * physical raster.  Dividing `source_cell / CJK_FONT_SIZE` first
         * silently truncates 32 / 24 to one, so the Waveshare's native 32px
         * CJK cells advance by only the former 25px metric and overlap their
         * neighbours.  Multiply before dividing: 25 * 32 / 24 gives the
         * intended 33px advance (one clear column between 32px glyphs).
         */
        advances[count] = base_advance * source_cell * scale_num /
                          (CJK_FONT_SIZE * scale_den);
        total += advances[count++];
    }
    int pen = 0;
    for (int i = 0; i < count; ++i) {
        int midpoint = pen + advances[i] / 2 - total / 2;
        const int base_width = (glyphs[i] < 0x80 || glyphs[i] == 0x00B0)
                                   ? 10 : source_cell;
        int glyph_width = base_width * scale_num / scale_den;
        int x = center_x + midpoint - glyph_width / 2;
        int offset = midpoint < 0 ? -midpoint : midpoint;
        if (offset >= radius) offset = radius - 1;
        const int sag = radius -
                        (int)sqrtf((float)(radius * radius - offset * offset));
        int y = arc_radius > 0 ? apex_y + sag : apex_y - sag;
        if (y < 1) y = 1;
        if (y > height - cell - 1) y = height - cell - 1;
        uint8_t dynamic_source24[DYNAMIC_GLYPH_BYTES];
        const uint8_t *dynamic = dynamic_glyph_copy(glyphs[i], dynamic_source24)
                                     ? dynamic_source24 : NULL;
        round_visual_curve_glyph_t curve_glyph;
        round_visual_profile_prepare_curve_glyph(glyphs[i], dynamic, &curve_glyph);
        for (int row = 0; row < source_cell; ++row) {
            for (int col = 0; col < source_cell; ++col) {
                const bool ascii_pixel = glyphs[i] < 0x80 && row < 21 && col < 15 &&
                    (glyph((char)glyphs[i])[col / 3] & (1u << (row / 3)));
                const bool set = round_visual_profile_curve_glyph_pixel(
                    &curve_glyph, row, col, ascii_pixel);
                if (!set) continue;
                const int dy0 = y + row * scale_num / scale_den;
                const int dy1 = y + (row + 1) * scale_num / scale_den;
                const int dx0 = x + col * scale_num / scale_den;
                const int dx1 = x + (col + 1) * scale_num / scale_den;
                for (int py = dy0; py < dy1; ++py) {
                    if (py < 0 || py >= height) continue;
                    for (int px = dx0; px < dx1; ++px) {
                        if (px >= 0 && px < width) target[py * stride + px] = fg;
                    }
                }
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
    // The shared idle cat is authored around one descriptor-owned source
    // centre. The profile resolves its physical aperture and intended scale;
    // business scene selection never needs to know which round panel is used.
    const int reference = ROUND_LAYOUT->standby_art_reference_width;
    const int denominator = reference * ROUND_LAYOUT->standby_art_scale_den;
    if (denominator <= 0) return center;
    const int source_center = ROUND_LAYOUT->standby_art_source_center_tracks_target
                                  ? center : ROUND_LAYOUT->standby_art_source_center;
    return center + (value - source_center) * LCD_WIDTH *
                        ROUND_LAYOUT->standby_art_scale_num / denominator;
}

static int scale_standby_cat_radius(int value) {
    const int reference = ROUND_LAYOUT->standby_art_reference_width;
    const int denominator = reference * ROUND_LAYOUT->standby_art_scale_den;
    if (denominator <= 0) return 0;
    return value * LCD_WIDTH * ROUND_LAYOUT->standby_art_scale_num / denominator;
}

static void draw_standby_cat_eye(int x, int y, uint16_t dark, uint16_t shine) {
    const int cx = scale_standby_cat_coordinate(x, LCD_WIDTH / 2);
    const int cy = scale_standby_cat_coordinate(y, STANDBY_ART_CENTER_Y);
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
    const int base_y = scale_standby_cat_coordinate(y, STANDBY_ART_CENTER_Y);
    const int rows = scale_standby_cat_radius(55);
    for (int row = 0; row < rows; ++row) {
        const int source_row = row * ROUND_LAYOUT->standby_art_scale_den /
                               ROUND_LAYOUT->standby_art_scale_num;
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
    /* Profile-native boot art is never the normal standby pet.  Fall through
     * to the common selected-pet renderer; each layout descriptor supplies
     * the appropriate safe stage for its circular aperture. */
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
    fill_circle_vertical_gradient(LCD_WIDTH / 2, STANDBY_ART_CENTER_Y + bob,
                                  scale_standby_cat_radius(PET_HALO_RADIUS),
                                  halo_top, halo_bottom);
    // Offset inner light produces a soft dimensional halo without storing or
    // decoding any bitmap asset.
    fill_circle_vertical_gradient(LCD_WIDTH / 2,
                                  STANDBY_ART_CENTER_Y - scale_standby_cat_radius(12) + bob,
                                  scale_standby_cat_radius(92),
                                  rgb565(153, 229, 255), rgb565(53, 126, 224));
    draw_standby_cat_ear(88, 64 + pet_y_offset, 1, face_shadow, ear);
    draw_standby_cat_ear(272, 64 + pet_y_offset, -1, face_shadow, ear);
    fill_circle_vertical_gradient(LCD_WIDTH / 2,
                                  scale_standby_cat_coordinate(170 + pet_y_offset + bob,
                                                               STANDBY_ART_CENTER_Y),
                                  scale_standby_cat_radius(103),
                                  face_shadow, face_deep);
    fill_circle_vertical_gradient(LCD_WIDTH / 2,
                                  scale_standby_cat_coordinate(164 + pet_y_offset + bob,
                                                               STANDBY_ART_CENTER_Y),
                                  scale_standby_cat_radius(100),
                                  face_light, face);
    // A restrained forehead sheen and lower-face shade add volume while
    // leaving the time and calendar rings visually dominant.
    fill_circle_vertical_gradient(LCD_WIDTH / 2,
                                  scale_standby_cat_coordinate(120 + pet_y_offset + bob,
                                                               STANDBY_ART_CENTER_Y),
                                  scale_standby_cat_radius(54),
                                  rgb565_lerp(face_light, shine, 110), face_light);
    uint8_t blink_stage = s_pet_motion_enabled ? pet_blink_stage(s_pet_motion_tick) : 0u;
    if (blink_stage == 2u) {
        fill_rect(scale_standby_cat_coordinate(116, 180), scale_standby_cat_coordinate(151 + pet_y_offset + bob, STANDBY_ART_CENTER_Y), scale_standby_cat_coordinate(164, 180), scale_standby_cat_coordinate(157 + pet_y_offset + bob, STANDBY_ART_CENTER_Y), ink);
        fill_rect(scale_standby_cat_coordinate(196, 180), scale_standby_cat_coordinate(151 + pet_y_offset + bob, STANDBY_ART_CENTER_Y), scale_standby_cat_coordinate(244, 180), scale_standby_cat_coordinate(157 + pet_y_offset + bob, STANDBY_ART_CENTER_Y), ink);
    } else if (blink_stage == 1u) {
        fill_rect(scale_standby_cat_coordinate(116, 180), scale_standby_cat_coordinate(147 + pet_y_offset + bob, STANDBY_ART_CENTER_Y), scale_standby_cat_coordinate(164, 180), scale_standby_cat_coordinate(157 + pet_y_offset + bob, STANDBY_ART_CENTER_Y), ink);
        fill_rect(scale_standby_cat_coordinate(196, 180), scale_standby_cat_coordinate(147 + pet_y_offset + bob, STANDBY_ART_CENTER_Y), scale_standby_cat_coordinate(244, 180), scale_standby_cat_coordinate(157 + pet_y_offset + bob, STANDBY_ART_CENTER_Y), ink);
    } else {
        draw_standby_cat_eye(140, 151 + pet_y_offset + bob, ink, shine);
        draw_standby_cat_eye(220, 151 + pet_y_offset + bob, ink, shine);
    }
    fill_circle_vertical_gradient(LCD_WIDTH / 2,
                                  scale_standby_cat_coordinate(190 + pet_y_offset + bob,
                                                               STANDBY_ART_CENTER_Y),
                                  scale_standby_cat_radius(15),
                                  rgb565(62, 79, 104), ink);
    fill_circle(scale_standby_cat_coordinate(176, 180),
                scale_standby_cat_coordinate(186 + pet_y_offset + bob, STANDBY_ART_CENTER_Y),
                scale_standby_cat_radius(4), rgb565(180, 205, 222));
    fill_rect(scale_standby_cat_coordinate(174, 180), scale_standby_cat_coordinate(204 + pet_y_offset + bob, STANDBY_ART_CENTER_Y),
              scale_standby_cat_coordinate(187, 180), scale_standby_cat_coordinate(211 + pet_y_offset + bob, STANDBY_ART_CENTER_Y), ink);
    fill_rect(scale_standby_cat_coordinate(160, 180), scale_standby_cat_coordinate(210 + pet_y_offset + bob, STANDBY_ART_CENTER_Y),
              scale_standby_cat_coordinate(200, 180), scale_standby_cat_coordinate(216 + pet_y_offset + bob, STANDBY_ART_CENTER_Y), ink);
    fill_circle_vertical_gradient(scale_standby_cat_coordinate(118, 180),
                                  scale_standby_cat_coordinate(191 + pet_y_offset + bob, STANDBY_ART_CENTER_Y),
                                  scale_standby_cat_radius(14),
                                  rgb565(255, 180, 164), blush);
    fill_circle_vertical_gradient(scale_standby_cat_coordinate(242, 180),
                                  scale_standby_cat_coordinate(191 + pet_y_offset + bob, STANDBY_ART_CENTER_Y),
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
                                                               STANDBY_ART_CENTER_Y),
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

    // The boot surface may be profile-specific (Waveshare's eagle).  It has
    // no common standby-frame baseline, so the first pet frame must be sent
    // in full even if a later direct clock/status update changed only a rim.
    if (s_startup_surface_visible) s_front_frame_valid = false;
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
            s_startup_surface_visible = false;
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
    int top = REMOTE_PET_TOP + (REMOTE_PET_TARGET - target_h) / 2;
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
    size_t target_width = REMOTE_PET_TARGET;
    size_t target_height = target_width * height / width;
    if (target_height > REMOTE_PET_TARGET) {
        target_height = REMOTE_PET_TARGET;
        target_width = target_height * width / height;
    }
    if (!target_width || !target_height) return false;
    *out_width = target_width;
    *out_height = target_height;
    return true;
}

static bool remote_pet_cropped_target_size(size_t crop_width, size_t crop_height,
                                           size_t *out_width, size_t *out_height) {
    return remote_pet_target_size(crop_width, crop_height, out_width, out_height);
}

/* A hub pet is RGBA (RGB565A8).  Some artwork reserves much of its common
 * source canvas as transparent safety margin, which makes the actual pet look
 * unexpectedly tiny on a large circular aperture.  Keep the policy in the
 * display descriptor, but derive one shared crop from all frames here so an
 * animated pose never jitters as individual extremities move. */
static bool remote_pet_visible_bounds(uint8_t *const *frames, size_t frame_count,
                                      size_t width, size_t height,
                                      size_t *out_left, size_t *out_top,
                                      size_t *out_width, size_t *out_height) {
    if (!frames || !frame_count || !width || !height || !out_left || !out_top ||
        !out_width || !out_height) return false;
    *out_left = 0;
    *out_top = 0;
    *out_width = width;
    *out_height = height;
    if (!ROUND_LAYOUT->remote_pet_trim_transparent_padding) return true;

    size_t min_x = width, min_y = height, max_x = 0, max_y = 0;
    bool found = false;
    for (size_t frame = 0; frame < frame_count; ++frame) {
        if (!frames[frame]) return false;
        for (size_t y = 0; y < height; ++y) {
            for (size_t x = 0; x < width; ++x) {
                if (frames[frame][(y * width + x) * 3u + 2u] < 8u) continue;
                if (!found || x < min_x) min_x = x;
                if (!found || y < min_y) min_y = y;
                if (!found || x > max_x) max_x = x;
                if (!found || y > max_y) max_y = y;
                found = true;
            }
        }
    }
    /* Do not turn a malformed all-transparent pack into a zero-sized source;
     * the normal visible-pixel gate will retain the native pet fallback. */
    if (!found) return true;
    *out_left = min_x;
    *out_top = min_y;
    *out_width = max_x - min_x + 1u;
    *out_height = max_y - min_y + 1u;
    return true;
}

bool legacy_display_scene_get_pet_asset_install_budget(size_t source_width, size_t source_height,
                                             size_t frame_count, size_t *out_total_external_bytes,
                                             size_t *out_max_external_allocation_bytes,
                                             size_t *out_max_frame_count) {
    if (!out_total_external_bytes || !out_max_external_allocation_bytes ||
        !out_max_frame_count || frame_count > REMOTE_PET_MAX_FRAMES) return false;
    /* Per-profile rendering capacity is a HAL fact.  The 466 px AMOLED
     * retains two full framebuffers, whereas EchoEar can safely use the
     * complete eight-keyframe loop. */
    const size_t max_frame_count = REMOTE_PET_PROFILE_MAX_FRAMES;
    const size_t effective_frame_count = frame_count < max_frame_count
                                             ? frame_count : max_frame_count;
    if (frame_count == 0) {
        *out_total_external_bytes = 0;
        *out_max_external_allocation_bytes = 0;
        *out_max_frame_count = max_frame_count;
        return true;
    }
    size_t target_width = 0, target_height = 0;
    if (!remote_pet_target_size(source_width, source_height, &target_width, &target_height) ||
        target_width > SIZE_MAX / target_height ||
        target_width * target_height > SIZE_MAX / 3u ||
        target_width * target_height * 3u > SIZE_MAX / frame_count) {
        return false;
    }
    const size_t frame_bytes = target_width * target_height * 3u;
    *out_total_external_bytes = frame_bytes * effective_frame_count;
    *out_max_external_allocation_bytes = frame_bytes;
    *out_max_frame_count = max_frame_count;
    return true;
}

bool legacy_storage_admission_allows_optional_flash_work(void) {
    return ROUND_LAYOUT->allows_optional_flash_work;
}

static void scale_remote_pet_frame(const uint8_t *source, size_t source_width,
                                   size_t source_left, size_t source_top,
                                   size_t source_crop_width, size_t source_crop_height,
                                   uint8_t *destination,
                                   size_t target_width, size_t target_height) {
    if (source_left == 0 && source_top == 0 && source_width == source_crop_width &&
        source_crop_height == target_height && source_crop_width == target_width) {
        memcpy(destination, source, source_width * source_crop_height * 3u);
        return;
    }
    for (size_t y = 0; y < target_height; ++y) {
        uint32_t source_y_fp = target_height > 1
                                   ? (uint32_t)(y * (source_crop_height - 1u) * 256u /
                                                (target_height - 1u))
                                   : 0;
        size_t y0 = source_y_fp >> 8;
        size_t y1 = y0 + 1u < source_crop_height ? y0 + 1u : y0;
        uint32_t fy = source_y_fp & 0xffu;
        for (size_t x = 0; x < target_width; ++x) {
            uint32_t source_x_fp = target_width > 1
                                       ? (uint32_t)(x * (source_crop_width - 1u) * 256u /
                                                    (target_width - 1u))
                                       : 0;
            size_t x0 = source_x_fp >> 8;
            size_t x1 = x0 + 1u < source_crop_width ? x0 + 1u : x0;
            uint32_t fx = source_x_fp & 0xffu;
            uint32_t weights[4] = {
                (256u - fx) * (256u - fy), fx * (256u - fy),
                (256u - fx) * fy, fx * fy,
            };
            size_t indexes[4] = {
                ((source_top + y0) * source_width + source_left + x0) * 3u,
                ((source_top + y0) * source_width + source_left + x1) * 3u,
                ((source_top + y1) * source_width + source_left + x0) * 3u,
                ((source_top + y1) * source_width + source_left + x1) * 3u,
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
        /* Scaling is optional background work. Let the idle task run while a
         * wide round-display keyframe is being prepared, rather than turning a
         * 312px asset install into a task-watchdog risk. */
        if ((y & 7u) == 7u) vTaskDelay(1);
    }
}

static void compose_recording_meter(const uint16_t wave_levels[RECORDING_WAVE_COLUMNS],
                                    uint16_t recording_smoothed_level,
                                    bool recording_paused, uint16_t bg,
                                    uint16_t cyan, uint16_t muted) {
    enum { RECORDING_VISUAL_BARS = RECORDING_WAVE_COLUMNS };
    const round_recording_layout_t *layout = ROUND_RECORDING_LAYOUT;
    // Keep Bread Compact's actual bar rhythm, not merely its column count:
    // 24 five-pixel bars advance on an eight-pixel pitch.  Stretching that to
    // 12/7 on EchoEar made the waveform read as a different visual language
    // and made each 32 ms update transfer substantially more pixels.  This
    // 189 px span is centred inside the round screen's safe chord.
    const int wave_left = layout->waveform_left;
    const int wave_pitch = layout->waveform_pitch;
    const int wave_bar_width = layout->waveform_bar_width;
    const int wave_center = layout->waveform_center_y;
    const int wave_half_height = layout->waveform_half_height;
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
    draw_centered_text(layout->microphone_label_y, level_label,
                       recording_paused ? muted : cyan, bg);
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
        const round_recording_layout_t *layout = ROUND_RECORDING_LAYOUT;
        fill_rect(layout->waveform_clear_left, layout->waveform_clear_top,
                  layout->waveform_clear_left + layout->waveform_clear_width,
                  layout->waveform_clear_top + layout->waveform_clear_height,
                  rgb565(10, 19, 30));
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
    const round_recording_layout_t *layout = ROUND_RECORDING_LAYOUT;
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
    fill_rect(layout->accent_left, layout->accent_top_y,
              LCD_WIDTH - layout->accent_left, layout->accent_top_y + 4, accent);
    fill_rect(layout->accent_left, LCD_HEIGHT - layout->accent_bottom_y - 4,
              LCD_WIDTH - layout->accent_left, LCD_HEIGHT - layout->accent_bottom_y,
              accent);
    // Bread's active marker is a 20 px outer square with an 8 px light core.
    // Preserve that scale on the round panel; only its position moves inward
    // for the visible chord.
    fill_rect(layout->icon_x, layout->icon_y,
              layout->icon_x + layout->icon_outer_size,
              layout->icon_y + layout->icon_outer_size, accent);
    fill_rect(layout->icon_x + layout->icon_inner_offset,
              layout->icon_y + layout->icon_inner_offset,
              layout->icon_x + layout->icon_inner_offset + layout->icon_inner_size,
              layout->icon_y + layout->icon_inner_offset + layout->icon_inner_size,
              rgb565(255, 235, 238));
    draw_text24_centered_safe(layout->status_y, recording_paused ? "已暂停" : "正在听取",
                               layout->status_max_width, fg, bg);
    draw_text24_centered_safe(layout->title_y, recording_is_meeting ? "会议录音" : "语音指令",
                               layout->title_max_width, recording_paused ? amber : cyan, bg);
    uint32_t minutes = recording_elapsed_seconds / 60;
    uint32_t seconds = recording_elapsed_seconds % 60;
    char elapsed[16];
    snprintf(elapsed, sizeof(elapsed), "%02lu:%02lu", (unsigned long)minutes, (unsigned long)seconds);
    draw_centered_text(layout->timer_y, elapsed, fg, bg);

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
        draw_text24_centered_safe(layout->instruction_y,
                                  layout->meeting_stop_instruction,
                                  layout->instruction_max_width, muted, bg);
    } else {
        draw_text24_centered_safe(layout->instruction_y,
                                  layout->command_completion_instruction,
                                  layout->instruction_max_width, muted, bg);
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
        const uint32_t frame_delay_ms = s_recording_active
                                            ? RECORDING_RENDER_FRAME_MS
                                            : (remote_pack_active
                                                   ? REMOTE_PET_RENDER_FRAME_MS
                                                   : PET_ANIMATION_FRAME_MS);
        if (!round_display_service_animation_wait_ms(frame_delay_ms)) break;
        // Read the revision after the delay. Reading it at the end of a frame
        // can acknowledge a clock update that arrived while the previous DMA
        // transfer was running even though that second was never rendered.
        uint32_t pending_ambient_revision = s_ambient_revision;
        if (s_setup_qrcode_visible) {
            // The QR code and its white quiet zone must stay pixel-stable for
            // phone cameras. Nothing else should draw while setup is active.
        } else if (s_recording_active) {
            // Recording frames are driven synchronously by
            // legacy_display_scene_set_audio_level(): one completed 512-sample capture
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
}

/* The shared renderer may need to replace an ambient frame with a foreground
 * scene after DISPLAY_OFF.  It owns only the state transition; the display
 * adapter owns the actual panel/backlight or controller-register ordering. */
static void wake_round_display_for_draw_locked(void) {
    if (!s_display_sleeping) return;
    esp_err_t err = round_display_service_wake_from_display_off();
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "DISPLAY_OFF wake transaction failed: %s", esp_err_to_name(err));
        return;
    }
    s_display_sleeping = false;
    // DISPLAY_OFF does not promise that controller GRAM survives unchanged.
    // Do not let the first post-wake pet or recorder delta compare against a
    // pre-sleep software snapshot: it could omit pixels that the physical
    // panel discarded while off.  The next presenter therefore takes its
    // established full-frame path and re-arms its own delta baseline only
    // after that transfer completes.
    s_front_frame_valid = false;
    s_recording_frame_baseline = false;
    s_recording_meter_dirty = false;
}

esp_err_t legacy_bootstrap_input_initialize(void) {
    /* Renderer construction is boot-lifetime.  Input starts later, after its
     * public queue and envelope publisher exist, so a second bootstrap must
     * never allocate another panel or mutex set. */
    if (s_background_tasks_lock || s_lcd_mutex || round_display_service_ready()) {
        return ESP_ERR_INVALID_STATE;
    }
    if (round_audio_service_sample_rate() == 0) return ESP_ERR_INVALID_STATE;
    s_background_tasks_lock = xSemaphoreCreateMutex();
    if (!s_background_tasks_lock) return ESP_ERR_NO_MEM;
    s_lcd_mutex = xSemaphoreCreateRecursiveMutex();
    if (!s_lcd_mutex) return ESP_ERR_NO_MEM;
    esp_err_t display_init_err = round_display_service_initialize(100);
    if (display_init_err != ESP_OK) {
        ESP_LOGE(TAG, "round display adapter init failed: %s",
                 esp_err_to_name(display_init_err));
        return display_init_err;
    }

    for (size_t i = 0; i < 2; ++i) {
        s_framebuffers[i] = round_display_service_allocate_framebuffer(
            LCD_FRAMEBUFFER_BYTES);
        if (!s_framebuffers[i]) {
            ESP_LOGW(TAG, "DMA PSRAM framebuffer %u unavailable; using stripe renderer",
                     (unsigned)i);
            for (size_t j = 0; j < 2; ++j) {
                if (s_framebuffers[j]) {
                    round_display_service_free_render_buffer(s_framebuffers[j]);
                }
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
    s_ambient_overlay = round_display_service_allocate_ambient_overlay(
        AMBIENT_OVERLAY_BYTES);
    if (!s_ambient_overlay) {
        ESP_LOGE(TAG, "cannot allocate %u-byte ambient overlay", (unsigned)AMBIENT_OVERLAY_BYTES);
        return ESP_ERR_NO_MEM;
    }
    ESP_LOGI(TAG, "ambient overlay ready: %u bytes in %s",
             (unsigned)AMBIENT_OVERLAY_BYTES,
              round_display_service_ambient_overlay_memory_name()
    );
    // Do not submit any full-screen transfer until networking has completed
    // its fragile association phase.  On this S3, simultaneous LCD GDMA and
    // Wi-Fi ROM initialisation can corrupt Wi-Fi's timer callback state.
    // legacy_display_scene_set_command_lock(false) starts the pet task after
    // the startup surface is released.

    // Peripheral Service owns the semantic preflight for touch/PMIC/IMU before
    // Input starts. Its private Audio lifecycle bridge creates the shared I2C
    // bus without leaking that electrical dependency into the renderer.
    esp_err_t input_i2c_err = round_peripheral_service_prepare(
        round_audio_service_default_output_volume(), 5000);
    if (input_i2c_err != ESP_OK) {
        ESP_LOGW(TAG, "touch/audio init deferred: %s", esp_err_to_name(input_i2c_err));
    }

    ESP_RETURN_ON_ERROR(round_display_service_run_animation_deadline_test(), TAG,
                        "animation stop deadline test");
    ESP_LOGI(TAG, "%s round display/peripheral adapter ready", round_audio_service_name());
    return ESP_OK;
}

esp_err_t legacy_bootstrap_input_start_scanner(
    legacy_input_scanner_publish_cb_t on_button, void *arg) {
    if (!on_button || !s_background_tasks_lock || !s_lcd_mutex ||
        !round_display_service_ready()) {
        return ESP_ERR_INVALID_STATE;
    }
    ESP_RETURN_ON_ERROR(round_input_service_start(on_button, arg), TAG,
                        "round input service start failed");
    ESP_LOGI(TAG, "%s round input scanner ready", round_audio_service_name());
    return ESP_OK;
}

bool legacy_connectivity_transport_load_selection(bool *out_cellular) {
    if (out_cellular) *out_cellular = false;
    return false;
}

bool legacy_connectivity_transport_apply_startup_toggle(uint32_t window_ms,
                                                        bool current_cellular,
                                                        bool *out_cellular) {
    (void)window_ms;
    if (out_cellular) *out_cellular = current_cellular;
    return false;
}

void legacy_connectivity_transport_adapt_gateway_url(char *gateway_url, size_t capacity,
                                                     bool cellular_active) {
    (void)gateway_url;
    (void)capacity;
    (void)cellular_active;
}

esp_err_t legacy_connectivity_transport_prepare_cellular(void) {
    return ESP_ERR_NOT_SUPPORTED;
}

bool legacy_connectivity_transport_cancel_foreground_request(void) {
    return false;
}

bool legacy_connectivity_transport_cancel_requests_for_owner(const void *owner) {
    (void)owner;
    return false;
}

esp_err_t legacy_connectivity_transport_start_cellular(uint32_t timeout_ms) {
    (void)timeout_ms;
    return ESP_ERR_NOT_SUPPORTED;
}

bool legacy_connectivity_transport_cellular_ready(void) {
    return false;
}

esp_err_t legacy_connectivity_transport_quiesce_cellular(uint32_t timeout_ms) {
    (void)timeout_ms;
    return ESP_ERR_NOT_SUPPORTED;
}

esp_err_t legacy_connectivity_transport_deinit_cellular(uint32_t timeout_ms) {
    (void)timeout_ms;
    return ESP_ERR_NOT_SUPPORTED;
}

esp_err_t legacy_connectivity_transport_reinitialize_cellular(uint32_t timeout_ms) {
    (void)timeout_ms;
    return ESP_ERR_NOT_SUPPORTED;
}

esp_err_t legacy_connectivity_transport_http_request(
    const device_connectivity_http_request_t *request) {
    (void)request;
    return ESP_ERR_NOT_SUPPORTED;
}

esp_err_t legacy_connectivity_transport_http_stream_request(
    const device_connectivity_stream_request_t *request) {
    (void)request;
    return ESP_ERR_NOT_SUPPORTED;
}

void legacy_display_scene_show_startup(void) {
    if (!round_display_service_ready() || !s_lcd_mutex) return;
    if (xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    if (s_alarm_visual_active) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    s_command_display_locked = true;
    s_startup_surface_visible = true;
    // The startup artwork is not a delta baseline for an ambient frame.
    s_front_frame_valid = false;
    s_recording_frame_baseline = false;
    s_response_active = false;
    if (round_display_service_has_startup_art()) {
        // A profile-specific product mark is allowed only on its boot surface.
        // The profile adapter owns the artwork; the shared scene owns its
        // lifetime and replaces it with the normal selected-pet surface on
        // the ready transition.
        uint16_t *frame = s_framebuffers[s_next_framebuffer];
        if (frame) s_render_target = frame;
        fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, state_color("idle"));
        round_display_service_compose_startup_art(frame, LCD_WIDTH, LCD_HEIGHT);
        draw_startup_brand_arc(state_color("idle"));
        s_render_target = NULL;
        if (frame) {
            if (present_frame_sync(frame) == ESP_OK) {
                s_next_framebuffer ^= 1u;
                ++s_presented_frames;
            }
        }
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
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

void legacy_display_scene_set_pet_state(const char *state) {
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
        /* Idle/quiet are always the common selected-pet scene. A board
         * startup asset has no place in this state, even when this state
         * update arrives through a recovery/reconnect path rather than the
         * normal ready-prompt path. This is a scene-ownership transition, not
         * a Waveshare-specific business rule. */
        if (!s_command_display_locked && s_startup_surface_visible) {
            s_startup_surface_visible = false;
            s_front_frame_valid = false;
            s_recording_frame_baseline = false;
            ESP_LOGI(TAG, "startup artwork released for ambient pet scene");
        }
        if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) == pdTRUE) {
            wake_round_display_for_draw_locked();
            xSemaphoreGiveRecursive(s_lcd_mutex);
        }
        s_idle_pet_visible = true;
    } else {
        s_idle_pet_visible = false;
    }
    if (!s_recording_active) draw_pet();
}

void legacy_display_scene_set_command_stage(const char *stage) {
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

void legacy_display_scene_set_command_lock(bool locked) {
    s_command_display_locked = locked;
    if (locked) s_ready_prompt_expires_us = 0;
    if (!locked) {
        /* The startup surface can be a profile-specific boot mark (the
         * Waveshare eagle), whereas the released surface is always the common
         * ambient pet.  Do not let a retained/delta framebuffer treat the boot
         * mark as an already-present pet scene: the first standby presentation
         * must replace every pixel. */
        /* A hardware profile may have painted a boot-only mark (the
         * Waveshare eagle) before the application became ready. The moment
         * the shared UI releases that foreground owner, make the next ambient
         * frame authoritative. In particular, do this before draw_pet(): a
         * late weather/service update must never compose its rings over boot
         * art and make that art look like the current pet. */
        s_startup_surface_visible = false;
        s_front_frame_valid = false;
        s_recording_frame_baseline = false;
    }
    if (!locked && s_background_tasks_lock &&
        xSemaphoreTake(s_background_tasks_lock, pdMS_TO_TICKS(100)) == pdTRUE) {
        // By the time the Welcome/wake sequence releases the boot surface,
        // TLS and ESP-SR have legitimately consumed the remaining contiguous
        // internal heap.  The renderer keeps all transfer buffers in static
        // DMA memory/PSRAM and does not need an internal task stack; allocating
        // this idle-only worker from PSRAM prevents the standby pet from being
        // silently dropped exactly when the device becomes ready.
        /* Recheck after acquiring the creation/stop gate.  Without this,
         * a UI thread which sampled `admission_closed=false` could block
         * behind rollback and create a new task immediately after rollback
         * had joined the old generation. */
        if (!s_background_tasks_admission_closed &&
            !round_display_service_animation_running()) {
            if (round_display_service_start_animation(pet_animation_task, NULL) != ESP_OK) {
                ESP_LOGE(TAG, "cannot start deferred pet animation task");
            }
        }
        xSemaphoreGive(s_background_tasks_lock);
    }
    // The startup artwork/ready hint is a foreground surface.  Once the
    // shared UI model releases that lock, explicitly publish the pet frame;
    // otherwise the first ambient repaint waits for a later animation tick
    // and the round display can remain blank after boot.
    if (!locked && !s_recording_active && !s_response_active &&
        !s_setup_qrcode_visible && ambient_visible_for_state()) {
        if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) == pdTRUE) {
            wake_round_display_for_draw_locked();
            xSemaphoreGiveRecursive(s_lcd_mutex);
        }
        s_idle_pet_visible = true;
        draw_pet();
    }
}

void legacy_display_scene_set_pet_profile(const char *skin, bool motion_enabled) {
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

/* A consuming caller transfers source ownership only after the renderer has
 * reserved the complete replacement.  This preserves every verified source
 * frame if a fragmented heap rejects a later target allocation, allowing the
 * common business fallback to retry fewer keyframes safely. */
static esp_err_t set_pet_asset_internal(uint8_t **frames, bool consume_sources,
                                        size_t frame_count, size_t width, size_t height,
                                        uint32_t frame_ms) {
    if (frame_count > REMOTE_PET_MAX_FRAMES) return ESP_ERR_INVALID_ARG;
    size_t bytes = 0;
    size_t target_width = 0, target_height = 0;
    size_t source_left = 0, source_top = 0;
    size_t source_crop_width = width, source_crop_height = height;
    if (frame_count) {
        if (!frames || width < 1 || height < 1 || width > 256 || height > 256) return ESP_ERR_INVALID_ARG;
        if (width > SIZE_MAX / height || width * height > SIZE_MAX / 3u) return ESP_ERR_INVALID_SIZE;
        if (!remote_pet_visible_bounds(frames, frame_count, width, height,
                                       &source_left, &source_top,
                                       &source_crop_width, &source_crop_height) ||
            !remote_pet_cropped_target_size(source_crop_width, source_crop_height,
                                            &target_width, &target_height)) {
            return ESP_ERR_INVALID_SIZE;
        }
        bytes = target_width * target_height * 3u;
    }
    uint8_t *copies[REMOTE_PET_MAX_FRAMES] = {0};
    bool has_visible_pixels = false;
    for (size_t i = 0; i < frame_count; ++i) {
        if (!frames[i]) goto no_mem;
        copies[i] = round_display_service_allocate_remote_pet_frame(bytes);
        if (!copies[i]) goto no_mem;
    }
    for (size_t i = 0; i < frame_count; ++i) {
        scale_remote_pet_frame(frames[i], width, source_left, source_top,
                               source_crop_width, source_crop_height, copies[i],
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
    if (consume_sources) {
        for (size_t i = 0; i < frame_count; ++i) {
            round_display_service_release_consumed_pet_source(frames[i]);
            frames[i] = NULL;
        }
    }
    if (s_lcd_mutex) xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    for (size_t i = 0; i < REMOTE_PET_MAX_FRAMES; ++i) {
        round_display_service_free_remote_pet_frame(s_remote_pet_frames[i]);
        s_remote_pet_frames[i] = copies[i];
    }
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
    for (size_t i = 0; i < REMOTE_PET_MAX_FRAMES; ++i) {
        round_display_service_free_remote_pet_frame(copies[i]);
    }
    return ESP_ERR_NO_MEM;
}

esp_err_t legacy_display_scene_set_pet_asset(const uint8_t *const *frames, size_t frame_count,
                                   size_t width, size_t height, uint32_t frame_ms) {
    return set_pet_asset_internal((uint8_t **)frames, false,
                                  frame_count, width, height, frame_ms);
}

esp_err_t legacy_display_scene_set_pet_asset_consuming(uint8_t **frames, size_t frame_count,
                                             size_t width, size_t height, uint32_t frame_ms) {
    return set_pet_asset_internal(frames, true, frame_count, width, height, frame_ms);
}

void legacy_display_scene_set_recording_visual(bool active, bool paused, uint32_t elapsed_seconds) {
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
    // state/timer boundary. Meter samples call legacy_display_scene_set_audio_level(),
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

void legacy_display_scene_set_audio_level(uint16_t level, uint32_t elapsed_seconds) {
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
    // legacy_display_scene_set_recording_visual() at the one-second boundary. Updating
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

void legacy_display_scene_show_text(const char *title, const char *text) {
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    // A short status message is a foreground surface too.  In particular it
    // can replace a paged result after a transport or recording transition;
    // leave that result marked active and its timer can repaint an old page
    // over this message (or later over the restored standby pet).  Bread
    // Compact clears the same ownership state before every status screen.
    s_response_active = false;
    s_response_image_active = false;
    s_message_active = true;
    wake_round_display_for_draw_locked();
    s_idle_pet_visible = false;
    const uint16_t bg = state_color(s_pet_state);
    const uint16_t header = rgb565(14, 31, 47);
    const uint16_t ink = rgb565(248, 252, 255);
    const uint16_t body = rgb565(194, 220, 236);
    const uint16_t muted = rgb565(136, 174, 197);
    const round_message_layout_t *layout = ROUND_MESSAGE_LAYOUT;
    const char *visible_title = title && title[0] ? title : "码卡龙";
    const char *visible_text = text ? text : "";

    // Bread's status surface is a complete scene, not a two-line overlay on a
    // previous pet frame.  Compose the EchoEar equivalent into a framebuffer
    // first so every foreground hand-off is atomic and the circular panel gets
    // a coherent message page instead of a leftover pet above the text.
    uint16_t *frame = s_framebuffers[s_next_framebuffer];
    if (frame) s_render_target = frame;
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    fill_circle_vertical_gradient(layout->avatar_center_x, layout->avatar_outer_center_y,
                                  layout->avatar_outer_radius, rgb565(69, 146, 203),
                                  rgb565(27, 67, 127));
    fill_circle_vertical_gradient(layout->avatar_center_x, layout->avatar_inner_center_y,
                                  layout->avatar_inner_radius, rgb565(190, 242, 246),
                                  rgb565(95, 183, 204));
    draw_ear(layout->left_ear_x, layout->left_ear_y, 1,
             rgb565(47, 123, 153), rgb565(87, 183, 196));
    draw_ear(layout->right_ear_x, layout->right_ear_y, -1,
             rgb565(47, 123, 153), rgb565(87, 183, 196));
    draw_eye(layout->left_eye_x, layout->left_eye_y, rgb565(24, 44, 70), ink);
    draw_eye(layout->right_eye_x, layout->right_eye_y, rgb565(24, 44, 70), ink);
    fill_circle(layout->nose_center_x, layout->nose_center_y, layout->nose_radius,
                rgb565(37, 58, 84));
    fill_rect(layout->mouth_left, layout->mouth_top_y, layout->mouth_right,
              layout->mouth_bottom_y, rgb565(37, 58, 84));

    fill_rect(layout->divider_left, layout->divider_y,
              LCD_WIDTH - layout->divider_left,
              layout->divider_y + layout->divider_height, header);
    int title_width = text24_width(visible_title, 10);
    if (title_width > layout->title_max_width) title_width = layout->title_max_width;
    draw_text24((LCD_WIDTH - title_width) / 2, layout->title_y, visible_title, ink, bg);
    char body_line[64];
    const char *next_line = copy_text24_line(body_line, sizeof(body_line), visible_text,
                                              layout->body_max_width);
    int body_width = text24_width(body_line, 12);
    if (body_width > layout->body_max_width) body_width = layout->body_max_width;
    draw_text24((LCD_WIDTH - body_width) / 2, layout->body_y, body_line, body, bg);
    if (next_line && *next_line) {
        char continuation[64];
        (void)copy_text24_line(continuation, sizeof(continuation), next_line,
                               layout->body_max_width);
        int continuation_width = text24_width(continuation, 12);
        if (continuation_width > layout->body_max_width) continuation_width = layout->body_max_width;
        draw_text24((LCD_WIDTH - continuation_width) / 2, layout->body_continuation_y,
                    continuation, body, bg);
    }
    draw_text24_centered_safe(layout->hint_y, "轻触继续", layout->hint_max_width,
                              muted, bg);
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

void legacy_display_scene_show_upload_progress(size_t completed_bytes, size_t total_bytes,
                                     const char *stage) {
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    // A transfer is an independent, quiet surface. It avoids the animated pet
    // DMA path while a large HTTPS upload is active and makes progress legible.
    // It must also supersede a previously held response surface: an answer can
    // remain on screen when the user starts a meeting, and leaving that stale
    // ownership bit set prevents a later completion from restoring standby.
    s_ready_prompt_expires_us = 0;
    s_idle_pet_visible = false;
    s_message_active = false;
    s_response_active = false;
    s_response_image_active = false;
    const uint16_t bg = rgb565(9, 35, 64);
    const uint16_t fg = rgb565(244, 250, 255);
    const uint16_t muted = rgb565(174, 206, 224);
    const uint16_t track = rgb565(28, 80, 111);
    const uint16_t fill = rgb565(72, 205, 220);
    const round_upload_layout_t *layout = ROUND_UPLOAD_LAYOUT;
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
    fill_rect(layout->accent_left, layout->accent_y,
              LCD_WIDTH - layout->accent_left, layout->accent_y + 2, fill);
    const char *visible_stage = stage && stage[0] ? stage : "正在上传";
    int heading_width = text24_width("会议录音", 8);
    draw_text24((LCD_WIDTH - heading_width) / 2, layout->title_y, "会议录音", fg, bg);
    char stage_line[64];
    const char *stage_next = copy_text24_line(stage_line, sizeof(stage_line), visible_stage,
                                               layout->text_max_width);
    int stage_width = text24_width(stage_line, 12);
    if (stage_width > layout->text_max_width) stage_width = layout->text_max_width;
    draw_text24((LCD_WIDTH - stage_width) / 2, layout->stage_y, stage_line, muted, bg);
    if (stage_next && *stage_next) {
        char stage_continuation[64];
        (void)copy_text24_line(stage_continuation, sizeof(stage_continuation), stage_next,
                               layout->text_max_width);
        int continuation_width = text24_width(stage_continuation, 12);
        if (continuation_width > layout->text_max_width) continuation_width = layout->text_max_width;
        draw_text24((LCD_WIDTH - continuation_width) / 2, layout->stage_continuation_y,
                    stage_continuation, muted, bg);
    }
    const int x = layout->progress_x, y = layout->progress_y;
    const int w = layout->progress_width, h = layout->progress_height;
    fill_rect(x, y, x + w, y + h, track);
    if (percent) fill_rect(x, y, x + (int)(w * percent / 100u), y + h, fill);
    char label[16];
    snprintf(label, sizeof(label), "%lu%%", (unsigned long)percent);
    draw_centered_text(layout->percent_y, label, fg, bg);
    char bytes[40];
    snprintf(bytes, sizeof(bytes), "%lu/%lu KB", (unsigned long)(completed_bytes / 1024u),
             (unsigned long)(total_bytes / 1024u));
    draw_centered_text(layout->bytes_y, bytes, muted, bg);
    draw_text24_centered_safe(layout->warning_y, "上传中，请勿断电",
                              layout->warning_max_width, muted, bg);
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
    if (!round_display_service_ready() || !modules || size == 0) return;
    const int quiet_zone = 4;
    const round_qrcode_layout_t *layout = ROUND_QRCODE_LAYOUT;
    // A QR code is necessarily square, but its corners must remain inside the
    // round aperture.  204px at y=40 is safely within the EchoEar's visible
    // circle, including the required white quiet zone; the former 228px tile
    // at y=12 placed its upper corners behind the bezel.
    const int available = layout->maximum_qr_square;
    const int module = available / (size + quiet_zone * 2);
    if (module < 2) {
        ESP_LOGW(TAG, "QR code is too large for display: %u modules", (unsigned)size);
        return;
    }
    const int qr_pixels = (size + quiet_zone * 2) * module;
    const int x0 = (LCD_WIDTH - qr_pixels) / 2;
    const int y0 = layout->qr_top_y;
    const uint16_t page_bg = state_color("quiet");
    const uint16_t white = rgb565(255, 255, 255);
    const uint16_t black = rgb565(0, 0, 0);

    s_ready_prompt_expires_us = 0;
    s_idle_pet_visible = false;
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
    wake_round_display_for_draw_locked();
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
    draw_text24((LCD_WIDTH - text24_width(title, 9)) / 2, layout->title_y, title,
                rgb565(255, 255, 255), page_bg);
    char instruction[40];
    snprintf(instruction, sizeof(instruction), "热点 %s", ssid ? ssid : "");
    char instruction_line[40];
    const char *instruction_next = copy_text24_line(instruction_line,
                                                     sizeof(instruction_line),
                                                     instruction, layout->text_max_width);
    int instruction_width = text24_width(instruction_line, 12);
    draw_text24((LCD_WIDTH - instruction_width) / 2, layout->instruction_y, instruction_line,
                rgb565(220, 235, 255), page_bg);
    if (instruction_next && *instruction_next) {
        char continuation[40];
        (void)copy_text24_line(continuation, sizeof(continuation), instruction_next,
                               layout->text_max_width);
        int continuation_width = text24_width(continuation, 12);
        draw_text24((LCD_WIDTH - continuation_width) / 2, layout->continuation_y, continuation,
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

void legacy_display_scene_show_qrcode_modules(const uint8_t *modules, size_t module_count,
                                   const char *ssid) {
    show_qrcode_matrix(modules, module_count, ssid);
}

void legacy_display_scene_set_wifi_status(const char *ssid, bool connected) {
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

void legacy_display_scene_set_ambient(const char *time, const char *location, const char *date, const char *weekday,
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

void legacy_display_scene_set_alarm_scheduled(bool scheduled) {
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

void legacy_display_scene_show_ready_prompt(const char *title, const char *text) {
    if (s_alarm_visual_active) return;
    /*
     * The board profile may have left a boot-only artwork on the panel.  The
     * ready hand-off must therefore be one serialized transaction: releasing
     * the command surface and painting the first ambient frame separately lets
     * a direct date/weather update land between them and leaves the boot
     * totem visible as if it were the standby pet.  This is especially obvious
     * on the Waveshare round screen, where the opaque eagle can cover the
     * lower weather ring.
     *
     * s_lcd_mutex is recursive, and legacy_display_scene_set_pet_state() deliberately
     * uses the normal full-frame presenter.  Holding it here makes that
     * presenter replace every boot pixel before any concurrent ambient update
     * can compose a rim overlay.  No board-specific scene choice leaks into
     * the business/UI layer: a board with remote pet frames uses them; a board
     * without one uses its native fallback pet.
     */
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) {
        ESP_LOGW(TAG, "ready standby hand-off could not acquire LCD");
        return;
    }
    // Completing provisioning is the terminal transition for the QR surface.
    // Keep its guard while a phone is scanning, but release it before the
    // ready path republishes idle; otherwise legacy_display_scene_set_pet_state() keeps
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
    /* `show_ready_prompt` is also used by recovery/provisioning paths.  Make
     * this terminal transition self-contained instead of relying on every
     * caller to have released the startup lock first.  A board-native boot
     * mark (Waveshare's eagle) is exclusively a startup surface: it must not
     * remain eligible as the standby delta baseline once the shared UI reaches
     * ready.  The following idle draw is consequently a complete common pet
     * scene, whether Hub supplied a remote pet or the local fallback is used. */
    // Use the normal release transition rather than changing the flag in
    // place.  Besides keeping frame baselines invalid, it starts the ambient
    // animator once startup has genuinely released the display.
    legacy_display_scene_set_command_lock(false);
    s_startup_surface_visible = false;
    s_front_frame_valid = false;
    s_recording_frame_baseline = false;
    wake_round_display_for_draw_locked();
    legacy_display_scene_set_pet_state("idle");
    s_ready_prompt_expires_us = 0;
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "ready standby: %s | %s", title ? title : "", text ? text : "");
}

void legacy_display_scene_cancel_ready_prompt(void) {
    s_ready_prompt_expires_us = 0;
}

esp_err_t legacy_display_scene_set_brightness(unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    if (s_lcd_mutex &&
        xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) == pdTRUE) {
        /* Display Service owns both the pending-off level and the selected
         * profile's PWM/DCS write.  A zero level remains a valid illuminated
         * runtime state distinct from DISPLAY_OFF. */
        esp_err_t err = round_display_service_set_brightness(percent);
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return err;
    } else {
        /* Initialization and teardown callers can arrive before the renderer
         * creates its mutex.  Preserve the exact same service transaction so
         * profile state cannot diverge from a later wake. */
        return round_display_service_set_brightness(percent);
    }
}

// Board-owned DISPLAY_OFF transaction. Network, alarm, and wake-word services
// deliberately stay alive; the application controls idle policy while this HAL
// owns only the physical panel/backlight state and matching wake bookkeeping.
esp_err_t legacy_display_scene_enter_display_off(void) {
    if (!s_lcd_mutex || !round_display_service_ready()) return ESP_ERR_INVALID_STATE;
    if (xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return ESP_ERR_TIMEOUT;
    // A Power Service deadline can become due just as a foreground transition
    // is being published. The display adapter is the final arbiter of its
    // local scene, so it refuses stale ambient deadlines instead of blanking
    // an alarm, response, recorder, setup QR, or status page.
    if (s_display_sleeping || !s_idle_pet_visible || s_message_active ||
        s_recording_active || s_response_active || s_response_image_active ||
        s_setup_qrcode_visible || s_alarm_visual_active ||
        s_command_display_locked) {
        ESP_LOGW(TAG,
                 "DISPLAY_OFF rejected: sleeping=%d idle=%d message=%d recording=%d response=%d image=%d setup=%d alarm=%d command=%d",
                 s_display_sleeping, s_idle_pet_visible, s_message_active,
                 s_recording_active, s_response_active, s_response_image_active,
                 s_setup_qrcode_visible, s_alarm_visual_active,
                 s_command_display_locked);
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    s_idle_pet_visible = false;
    esp_err_t err = round_display_service_enter_display_off();
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "DISPLAY_OFF entry transaction failed: %s", esp_err_to_name(err));
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return err;
    }
    s_display_sleeping = true;
    // The retained software frame is no longer proof of what the controller
    // will show after DISP ON.  Invalidate it at the physical boundary too,
    // so any later foreground scene that wakes the panel starts with a full
    // paint rather than a stale ambient delta.
    s_front_frame_valid = false;
    s_recording_frame_baseline = false;
    s_recording_meter_dirty = false;
    xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "display HAL entered DISPLAY_OFF");
    return ESP_OK;
}

bool legacy_display_scene_is_off(void) {
    if (!s_lcd_mutex) return false;
    if (xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return false;
    bool display_off = s_display_sleeping;
    xSemaphoreGiveRecursive(s_lcd_mutex);
    return display_off;
}

esp_err_t legacy_display_scene_wake_display(void) {
    if (!s_lcd_mutex || !round_display_service_ready()) return ESP_ERR_INVALID_STATE;
    if (xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return ESP_ERR_TIMEOUT;
    if (!s_display_sleeping) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    wake_round_display_for_draw_locked();
    if (s_display_sleeping) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return ESP_FAIL;
    }
    // The first physical contact after the 30-minute ambient timeout is a
    // screen wake, not an implicit voice command. Re-publish the same ready
    // pet surface and arm its normal hint timeout. The shared UI / Power
    // Service remains the sole owner of the DISPLAY_OFF timeout, matching the
    // documented board-port contract and the compact board's "first press
    // returns" flow.
    s_idle_pet_visible = true;
    s_ready_prompt_expires_us = esp_timer_get_time() + READY_PROMPT_TIMEOUT_US;
    draw_pet();
    xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "ambient display awakened");
    return ESP_OK;
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

void legacy_display_scene_set_recording_mode(bool meeting) {
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
    fill_rect(ROUND_LAYOUT->response_header_left, ROUND_LAYOUT->response_header_top_y,
              LCD_WIDTH - ROUND_LAYOUT->response_header_left,
              ROUND_LAYOUT->response_header_bottom_y, header);
    fill_rect(ROUND_LAYOUT->response_rule_left, RESPONSE_RULE_Y,
              LCD_WIDTH - ROUND_LAYOUT->response_rule_left,
              RESPONSE_RULE_Y + ROUND_LAYOUT->response_rule_height,
              rgb565(31, 62, 82));
    fill_rect(RESPONSE_TEXT_X,
              RESPONSE_FOOTER_Y + ROUND_LAYOUT->response_footer_rule_top_offset,
              LCD_WIDTH - RESPONSE_TEXT_X,
              RESPONSE_FOOTER_Y + ROUND_LAYOUT->response_footer_rule_top_offset +
                  ROUND_LAYOUT->response_footer_rule_height,
              footer);
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

void legacy_display_scene_show_response(const char *title, const char *text) {
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    s_ready_prompt_expires_us = 0;
    // Enter the result state without calling legacy_display_scene_set_pet_state(). That
    // public setter paints a complete pet frame immediately; doing so just
    // before this page produced a visible boot/idle-looking flash between the
    // thinking screen and every streamed response message.
    strlcpy(s_pet_state, "speaking", sizeof(s_pet_state));
    s_idle_pet_visible = false;
    wake_round_display_for_draw_locked();
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

void legacy_display_scene_set_alarm_visual(bool active, unsigned frame, const char *time_text,
                                 const char *label, unsigned attempt, unsigned max_attempts) {
    if (!active) {
        // The shared UI coordinator immediately replays the authoritative
        // foreground scene. Release only the alarm-local guard here; drawing
        // idle would flash and would discard a response/upload/setup page.
        s_alarm_visual_active = false;
        if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
        wake_round_display_for_draw_locked();
        if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
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
    const round_alarm_layout_t *layout = ROUND_ALARM_LAYOUT;
    int sway = (frame & 1u) ? 5 : -5;
    uint16_t *composed = s_framebuffers[s_next_framebuffer];
    if (composed) s_render_target = composed;
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    // The wide rectangular header was visibly clipped by the circular bezel.
    // Use a compact title/rule that follows the result screen's safe geometry.
    fill_rect(layout->rule_left, layout->rule_y,
              LCD_WIDTH - layout->rule_left, layout->rule_y + layout->rule_height,
              panel);
    draw_text24_centered_safe(layout->title_y, "闹钟响铃", layout->title_max_width,
                              white, bg);
    // Restrained mechanical twin-bell silhouette. Alternating horizontal
    // offset conveys ringing without flashing the whole display.
    int cx = LCD_WIDTH / 2 + sway;
    fill_circle(cx, layout->bell_center_y, layout->bell_outer_radius, steel);
    fill_circle(cx, layout->bell_center_y, layout->bell_inner_radius, panel);
    fill_circle(cx - layout->bell_side_offset_x, layout->bell_side_center_y,
                layout->bell_side_radius, amber);
    fill_circle(cx + layout->bell_side_offset_x, layout->bell_side_center_y,
                layout->bell_side_radius, amber);
    fill_rect(cx - layout->bell_side_bar_outer_x, layout->bell_side_bar_y,
              cx - layout->bell_side_bar_inner_x,
              layout->bell_side_bar_y + layout->bell_side_bar_height, amber);
    fill_rect(cx + layout->bell_side_bar_inner_x, layout->bell_side_bar_y,
              cx + layout->bell_side_bar_outer_x,
              layout->bell_side_bar_y + layout->bell_side_bar_height, amber);
    fill_rect(cx - layout->bell_top_knob_radius, layout->bell_top_stem_top,
              cx + layout->bell_top_knob_radius, layout->bell_top_stem_bottom, amber);
    fill_circle(cx, layout->bell_top_knob_y, layout->bell_top_knob_radius, amber);
    fill_rect(cx - layout->bell_leg_outer_x, layout->bell_leg_top_y,
              cx - layout->bell_leg_inner_x, layout->bell_leg_bottom_y, steel);
    fill_rect(cx + layout->bell_leg_inner_x, layout->bell_leg_top_y,
              cx + layout->bell_leg_outer_x, layout->bell_leg_bottom_y, steel);
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
    draw_text24_centered_safe(layout->clock_center_y, clock, layout->clock_max_width,
                              white, panel);
    if (label && label[0]) {
        draw_text24_centered_safe(layout->label_y, label, layout->label_max_width,
                                  white, bg);
    }
    char hint[48];
    snprintf(hint, sizeof(hint), "轻触停止  %u/%u", attempt, max_attempts);
    draw_text24_centered_safe(layout->hint_y, hint, layout->hint_max_width, muted, bg);
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

void legacy_display_scene_show_response_image(const char *title, const char *caption,
                                    const uint16_t *pixels, size_t width, size_t height) {
    if (!pixels || width < 1 || width > 64 || height < 1 || height > 64) return;
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return;
    s_ready_prompt_expires_us = 0;
    strlcpy(s_pet_state, "speaking", sizeof(s_pet_state));
    s_idle_pet_visible = false;
    wake_round_display_for_draw_locked();
    s_message_active = false;
    s_response_active = true;
    s_response_image_active = true;
    s_response_page = 0;
    uint16_t bg = rgb565(8, 17, 28), header = rgb565(14, 31, 47);
    uint16_t ink = rgb565(244, 248, 251);
    uint16_t muted = rgb565(174, 198, 215);
    const round_response_image_layout_t *layout = ROUND_RESPONSE_IMAGE_LAYOUT;
    uint16_t *frame = s_framebuffers[s_next_framebuffer];
    if (frame) s_render_target = frame;
    fill_rect(0, 0, LCD_WIDTH, LCD_HEIGHT, bg);
    // Use the same inscribed reading area as text replies.  A rectangular
    // edge-to-edge header or 64px thumbnail makes an image result look clipped
    // by the circular bezel, while this panel keeps both the title and image
    // visually centred in the round surface.
    fill_rect(layout->header_left, layout->header_top_y,
              LCD_WIDTH - layout->header_left, layout->header_bottom_y, header);
    fill_rect(layout->rule_left, layout->rule_y, LCD_WIDTH - layout->rule_left,
              layout->rule_y + layout->rule_height, rgb565(31, 62, 82));
    const char *visible_title = title && title[0] ? title : "码卡龙";
    draw_text24_centered_safe(layout->title_y, visible_title,
                              layout->title_max_width, ink, header);

    const int content_top = layout->content_top_y;
    const int content_bottom = caption && caption[0]
                                   ? layout->content_bottom_with_caption_y
                                   : layout->content_bottom_without_caption_y;
    const int available_w = LCD_WIDTH - layout->content_side_margin * 2;
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
        const char *caption_next = draw_text24_centered_safe(layout->caption_first_y,
                                                               caption,
                                                               layout->caption_max_width,
                                                               muted, bg);
        if (caption_next && *caption_next) {
            (void)draw_text24_centered_safe(layout->caption_second_y, caption_next,
                                             layout->caption_max_width, muted, bg);
        }
    }
    draw_text24(layout->hint_x, layout->hint_y, "轻触返回", muted, bg);
    s_render_target = NULL;
    if (frame && present_frame_sync(frame) == ESP_OK) {
        s_next_framebuffer ^= 1u;
        ++s_presented_frames;
    }
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}

bool legacy_display_scene_navigate_response(int page_delta) {
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

bool legacy_display_scene_get_response_page(unsigned *page) {
    if (!page) return false;
    if (s_lcd_mutex && xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) {
        return false;
    }
    bool active = s_response_active;
    if (active) *page = s_response_image_active ? 0u : s_response_page;
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
    return active;
}

bool legacy_display_scene_restore_response_page(unsigned page) {
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

esp_err_t legacy_bootstrap_input_stop_scanner(uint32_t timeout_ms) {
    return round_input_service_stop(timeout_ms);
}

esp_err_t board_background_lifecycle_stop(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    /* Match the compact renderer's lifecycle contract: an initialization
     * rollback may reach this board facade before optional decorative work
     * has ever been admitted.  There is then no display-owned task to join;
     * report an idempotent no-op so a later service owner can still close its
     * own partially initialized state.  This is deliberately not runtime
     * renderer restart/deinitialization support. */
    if (!s_background_tasks_lock) return ESP_OK;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t deadline = pdMS_TO_TICKS(timeout_ms);
    if (xSemaphoreTake(s_background_tasks_lock, deadline) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    /* Publish closure while holding the same mutex used by the deferred
     * renderer creation path.  A late UI update can therefore either create
     * its task before this stop owns the lock (and be joined below), or see
     * this closed generation and remain task-free. */
    s_background_tasks_admission_closed = true;
    const TickType_t elapsed = xTaskGetTickCount() - started;
    const TickType_t remaining = elapsed >= deadline ? 0 : deadline - elapsed;
    const esp_err_t animation_stop_err = remaining == 0
                                           ? ESP_ERR_TIMEOUT
                                           : round_display_service_stop_animation(
                                               (uint32_t)(remaining * portTICK_PERIOD_MS));
    if (animation_stop_err != ESP_OK) {
        xSemaphoreGive(s_background_tasks_lock);
        ESP_LOGW(TAG, "cannot stop board pet animation task: %s",
                 esp_err_to_name(animation_stop_err));
        return animation_stop_err;
    }
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
    if (!round_display_service_ready() || !frame) return ESP_ERR_INVALID_ARG;
    // Delta presentation trusts that panel GRAM mirrors the front buffer.
    // A silently corrupted QSPI transfer would otherwise persist forever in
    // static regions, because later frames only re-send pixels whose composed
    // value changed.  Re-present the complete frame every few seconds so
    // transient link errors heal instead of accumulating as displaced sprite
    // fragments.
    static unsigned s_delta_presents;
    if (!s_front_frame_valid || !s_framebuffers[s_front_framebuffer] ||
        (++s_delta_presents % 125u) == 0u) {
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
        round_display_service_align_dirty_columns(&left_changed, &right_changed,
                                                  LCD_WIDTH);
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

void legacy_connectivity_transport_set_network_transport(bool cellular) {
    (void)cellular;
}

void legacy_display_scene_set_service_ready(bool ready) {
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
