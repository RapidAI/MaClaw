#include "legacy_connectivity_transport.h"
#include "board_background_lifecycle.h"
#include "legacy_display_scene.h"
#include "legacy_storage_admission.h"
#include "legacy_bootstrap_input.h"
#include "compact_audio_service.h"
#include "compact_connectivity_service.h"
#include "compact_display_service.h"
#include "compact_input_service.h"
#include "compact_peripheral_service.h"
#include "font_cjk24.h"

#include <math.h>
#include <string.h>

#include "esp_check.h"
#include "compact_visual_profile_service.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "provisioning_failure_injection.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

/* The renderer consumes normalized profile facts. Selected adapters retain
 * the concrete display and microphone identities. */
static inline int compact_display_width(void) {
    return compact_display_service_width();
}
static inline int compact_display_height(void) {
    return compact_display_service_height();
}
#define LCD_WIDTH (compact_display_width())
#define LCD_HEIGHT (compact_display_height())
#define BACKLIGHT_BRIGHTNESS_DEFAULT 0u
#define THINKING_MOUTH_FRAME_MS 420
#define REMOTE_PET_RENDER_FRAME_MS 80
#define REMOTE_PET_DEFAULT_KEYFRAME_MS 450
#define LCD_FRAME_PIXELS ((size_t)LCD_WIDTH * LCD_HEIGHT)
#define LCD_FRAME_BYTES (LCD_FRAME_PIXELS * sizeof(uint16_t))

static const char *TAG = "maclaw_compact_renderer";
static legacy_input_scanner_publish_cb_t s_button_cb;
static void *s_button_arg;
static SemaphoreHandle_t s_background_tasks_lock;
/* Once a failed-startup rollback closes decorative-work admission, late
 * shared UI state must not recreate a pet/thinking worker after lifecycle has
 * already joined it.  This is not a board deinit or renderer restart claim. */
static bool s_background_tasks_admission_closed;
static SemaphoreHandle_t s_lcd_mutex;
static uint16_t *s_framebuffers[2];
static uint16_t *s_render_target;
static uint16_t *s_present_staging;
static unsigned s_front_frame;
static bool s_front_frame_valid;
static bool s_direct_draw_warning_logged;
static unsigned s_thinking_mouth_frame;
static bool s_thinking_surface_visible;
static bool s_recording_mode;
static bool s_recording_active;
static bool s_foreground_surface;
static bool s_alarm_visual_active;
static bool s_display_sleeping;
static bool s_recording_paused;
static uint32_t s_recording_elapsed;
static uint16_t s_recording_levels[24];
static uint16_t s_recording_smoothed_level;
static char s_state[16] = "idle";
static char s_command_stage[32] = "正在处理";
static char s_pet_skin[32] = "clawmate";
#define REMOTE_PET_MAX_FRAMES 8
static uint8_t *s_remote_pet_frames[REMOTE_PET_MAX_FRAMES];
static size_t s_remote_pet_frame_count, s_remote_pet_width, s_remote_pet_height;
static uint32_t s_remote_pet_frame_ms = REMOTE_PET_DEFAULT_KEYFRAME_MS;
static uint64_t s_remote_pet_animation_elapsed_ms;
/* A disabled motion profile keeps the latest downloaded pet pose, but avoids
 * starting the decorative worker and avoids all per-frame SPI transfers. */
static bool s_pet_motion_enabled = true;
/* Diagnostic state is deliberately scene-neutral: it records why the shared
 * pet worker is currently not submitting frames without exposing any panel,
 * SPI, GPIO or RTOS fact above the Display HAL. */
static uint32_t s_remote_pet_animation_blocked_mask;
static int64_t s_remote_pet_animation_blocked_logged_us;
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

/* `legacy_bootstrap_input_initialize()` publishes no scanner or decorative worker until its
 * last steps.  If construction fails before the panel transaction succeeds,
 * release only renderer-owned objects that cannot yet be observed by another
 * task.  The display adapters independently roll back their partial
 * controller/bus acquisition before returning an error.
 *
 * Do not extend this helper into a runtime board deinit: after a successful
 * panel bring-up the panel, audio path and their renderer locks deliberately
 * remain boot-lifetime diagnostic resources.  In particular, a failed later
 * input/audio/peripheral step must retain them rather than freeing a handle a
 * diagnostic Display Service may still borrow. */
static void compact_renderer_discard_unpublished_init_state(void) {
    for (size_t i = 0; i < sizeof(s_framebuffers) / sizeof(s_framebuffers[0]); ++i) {
        if (s_framebuffers[i]) {
            compact_display_service_free_buffer(s_framebuffers[i]);
            s_framebuffers[i] = NULL;
        }
    }
    if (s_present_staging) {
        compact_display_service_free_buffer(s_present_staging);
        s_present_staging = NULL;
    }
    s_render_target = NULL;
    s_front_frame = 0;
    s_front_frame_valid = false;
    s_direct_draw_warning_logged = false;

    compact_input_service_discard_unpublished_scanner_state();
    compact_display_service_discard_unpublished_state();
    if (s_lcd_mutex) {
        vSemaphoreDelete(s_lcd_mutex);
        s_lcd_mutex = NULL;
    }
    if (s_background_tasks_lock) {
        vSemaphoreDelete(s_background_tasks_lock);
        s_background_tasks_lock = NULL;
    }
    s_background_tasks_admission_closed = false;
    s_button_cb = NULL;
    s_button_arg = NULL;
}
/* After a successful panel transaction, initialization is intentionally
 * fail-closed instead of pretending the board can be reconstructed in the
 * same boot.  No task has been published on these paths, so closing only
 * admission prevents optional renderer activity while retaining the safe
 * diagnostic surface and the boot-lifetime hardware handles. */
static esp_err_t compact_renderer_fail_after_hardware_init(esp_err_t err,
                                                            const char *step) {
    s_background_tasks_admission_closed = true;
    s_button_cb = NULL;
    s_button_arg = NULL;
    /* The Input service owns whether a scanner was published and its private
     * completion primitive.  Failed preparation is releasable; a published
     * scanner remains joinable through its Input HAL stop path. */
    compact_input_service_discard_unpublished_scanner_state();
    ESP_LOGE(TAG, "compact renderer initialization stopped after hardware init at %s: %s; "
             "retaining boot-lifetime diagnostic hardware",
             step ? step : "unknown", esp_err_to_name(err));
    return err;
}
#define RESPONSE_TEXT_CAPACITY 2048
static inline const compact_response_layout_t *compact_response_layout(void) {
    return compact_visual_profile_response_layout();
}
static inline const compact_standby_layout_t *compact_standby_layout(void) {
    return compact_visual_profile_standby_layout();
}
static inline const compact_recording_layout_t *compact_recording_layout(void) {
    return compact_visual_profile_recording_layout();
}
static inline const compact_upload_layout_t *compact_upload_layout(void) {
    return compact_visual_profile_upload_layout();
}
static inline const compact_alarm_layout_t *compact_alarm_layout(void) {
    return compact_visual_profile_alarm_layout();
}
#define RESPONSE_LAYOUT (compact_response_layout())
#define STANDBY_LAYOUT (compact_standby_layout())
#define RECORDING_LAYOUT (compact_recording_layout())
#define UPLOAD_LAYOUT (compact_upload_layout())
#define ALARM_LAYOUT (compact_alarm_layout())
/* Chunk height is a physical Display-HAL fact shared by startup, full-frame,
 * dirty-region and fallback paths. It must not be smuggled through a standby
 * visual layout merely because the standby pet happens to use it too. */
#define LCD_STRIPE_ROWS (compact_display_service_transfer_stripe_rows())
#define AMBIENT_WEATHER_TEXT_Y (STANDBY_LAYOUT->weather_text_y)
#define AMBIENT_WEATHER_SCALE_NUM (STANDBY_LAYOUT->weather_scale_num)
#define AMBIENT_WEATHER_SCALE_DEN (STANDBY_LAYOUT->weather_scale_den)
#define AMBIENT_PET_TOP (STANDBY_LAYOUT->pet_top)
#define AMBIENT_PET_MAX_WIDTH (STANDBY_LAYOUT->pet_max_width)
#define AMBIENT_PET_MAX_HEIGHT (LCD_HEIGHT - AMBIENT_PET_TOP)
#define AMBIENT_NATIVE_PET_SCALE (STANDBY_LAYOUT->native_pet_scale_percent)
#define RESPONSE_LINES_PER_PAGE (RESPONSE_LAYOUT->lines_per_page)
#define RESPONSE_TEXT_X (RESPONSE_LAYOUT->text_x)
#define RESPONSE_TEXT_Y (RESPONSE_LAYOUT->text_y)
#define RESPONSE_LINE_HEIGHT (RESPONSE_LAYOUT->line_height)
#define RESPONSE_FOOTER_Y (RESPONSE_LAYOUT->footer_y)
#define RESPONSE_TEXT_WIDTH (LCD_WIDTH - RESPONSE_TEXT_X * 2)
static bool s_response_active;
// Image pixels are not retained after the synchronous present. Track the
// surface kind so page buttons cannot replace it with stale text state.
static bool s_response_image_active;
static unsigned s_response_page;
static int64_t s_response_next_page_us;
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
static uint16_t *allocate_temporary_display_bitmap(size_t bytes);
static void free_temporary_display_bitmap(void *bitmap);
static esp_err_t draw_bitmap_sync(int x0, int y0, int x1, int y1, const void *pixels);
static bool begin_screen_frame(void);
static void finish_screen_frame(bool composed);
static void fill_screen(uint16_t c);
static uint16_t color(uint8_t r, uint8_t g, uint8_t b);
static int text24_width(const char *text, int max_glyphs);
static void draw_ascii_at(int x0, int y, const char *text, uint16_t fg, uint16_t bg);

static bool compact_profile_bridge_display_ready(void *context) {
    (void)context; return compact_display_service_ready();
}
static bool compact_profile_bridge_begin_frame(void *context) {
    (void)context; return begin_screen_frame();
}
static void compact_profile_bridge_finish_frame(void *context, bool composed) {
    (void)context; finish_screen_frame(composed);
}
static void compact_profile_bridge_fill_screen(void *context, uint16_t background) {
    (void)context; fill_screen(background);
}
static uint16_t compact_profile_bridge_state_color(void *context, const char *state) {
    (void)context; return state_color(state);
}
static uint16_t compact_profile_bridge_color(void *context, uint8_t red,
                                             uint8_t green, uint8_t blue) {
    (void)context; return color(red, green, blue);
}
static int compact_profile_bridge_text24_width(void *context, const char *text,
                                               int max_glyphs) {
    (void)context; return text24_width(text, max_glyphs);
}
static void compact_profile_bridge_draw_ascii(void *context, int x, int y, const char *text,
                                              uint16_t foreground, uint16_t background) {
    (void)context; draw_ascii_at(x, y, text, foreground, background);
}
static void compact_profile_bridge_draw_text24(void *context, int x, int y, const char *text,
                                               uint16_t foreground, uint16_t background,
                                               int max_glyphs) {
    (void)context; draw_text24_clipped(x, y, text, foreground, background, max_glyphs);
}
static void compact_profile_bridge_draw_text24_centered(void *context, int y, const char *text,
                                                        uint16_t foreground, uint16_t background,
                                                        int max_glyphs) {
    (void)context; draw_text24_centered(y, text, foreground, background, max_glyphs);
}
static bool compact_profile_bridge_draw_remote_pet(void *context, uint16_t background) {
    (void)context; return draw_remote_pet_frame(background);
}
static bool compact_profile_bridge_network_is_cellular(void *context) {
    (void)context; return compact_visual_profile_network_transport_is_cellular();
}
static uint16_t *compact_profile_bridge_allocate_bitmap(void *context, size_t bytes) {
    (void)context; return allocate_temporary_display_bitmap(bytes);
}
static void compact_profile_bridge_free_bitmap(void *context, void *bitmap) {
    (void)context; free_temporary_display_bitmap(bitmap);
}
static bool compact_profile_bridge_draw_bitmap(void *context, int x, int y,
                                               int width, int height,
                                               const uint16_t *pixels) {
    (void)context;
    return draw_bitmap_sync(x, y, x + width, y + height, pixels) == ESP_OK;
}
static void compact_profile_bridge_fill_rect(void *context, int x, int y,
                                             int width, int height, uint16_t fill) {
    (void)context; fill_rect_solid(x, y, width, height, fill);
}
static void compact_profile_bind_renderer_primitives(void) {
    const compact_profile_render_bridge_t bridge = {
        .panel_width = LCD_WIDTH, .panel_height = LCD_HEIGHT,
        .display_ready = compact_profile_bridge_display_ready,
        .begin_frame = compact_profile_bridge_begin_frame,
        .finish_frame = compact_profile_bridge_finish_frame,
        .fill_screen = compact_profile_bridge_fill_screen,
        .state_color = compact_profile_bridge_state_color,
        .color = compact_profile_bridge_color,
        .text24_width = compact_profile_bridge_text24_width,
        .draw_ascii = compact_profile_bridge_draw_ascii,
        .draw_text24 = compact_profile_bridge_draw_text24,
        .draw_text24_centered = compact_profile_bridge_draw_text24_centered,
        .draw_remote_pet = compact_profile_bridge_draw_remote_pet,
        .network_is_cellular = compact_profile_bridge_network_is_cellular,
        .allocate_bitmap = compact_profile_bridge_allocate_bitmap,
        .free_bitmap = compact_profile_bridge_free_bitmap,
        .draw_bitmap = compact_profile_bridge_draw_bitmap,
        .fill_rect = compact_profile_bridge_fill_rect,
    };
    compact_visual_profile_bind_renderer(&bridge);
}
/* A display bitmap is CPU-copied into the retained composition frame when
 * one is active; otherwise the panel consumes it directly. The selected
 * display adapter owns both physical-memory policies, leaving this common
 * renderer independent of ESP-IDF capability flags. */
static uint16_t *allocate_temporary_display_bitmap(size_t bytes) {
    return s_render_target
               ? compact_display_service_allocate_temporary_composition_bitmap(bytes)
               : compact_display_service_allocate_temporary_transfer_bitmap(bytes);
}

static void free_temporary_display_bitmap(void *bitmap) {
    compact_display_service_free_temporary_bitmap(bitmap);
}

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

static esp_err_t panel_draw_bitmap_sync(int x0, int y0, int x1, int y1, const void *pixels) {
    return compact_display_service_draw_bitmap_sync(x0, y0, x1, y1, pixels);
}

/* A profile can replace the shared robot-mouth artwork with one bounded
 * panel-specific patch.  It never owns a task, scene decision, LCD lock or
 * framebuffer: those remain common so both profiles obey the same thinking
 * admission/recovery rules. */
static bool draw_profile_thinking_patch(void) {
    if (!compact_display_service_uses_profile_thinking_patch()) return false;
    if (!s_present_staging) return true;
    compact_display_animation_patch_t patch = {0};
    const uint16_t background = state_color("thinking");
    const size_t capacity = (size_t)LCD_WIDTH * LCD_STRIPE_ROWS;
    if (!compact_display_service_compose_thinking_patch(
            s_present_staging, capacity, s_thinking_mouth_frame, background, &patch)) {
        ESP_LOGW(TAG, "profile thinking patch composition rejected");
        return true;
    }
    if (patch.left < 0 || patch.top < 0 || patch.width <= 0 || patch.height <= 0 ||
        patch.left + patch.width > LCD_WIDTH || patch.top + patch.height > LCD_HEIGHT ||
        (size_t)patch.width * patch.height > capacity) {
        ESP_LOGW(TAG, "profile thinking patch geometry rejected");
        return true;
    }
    const esp_err_t err = panel_draw_bitmap_sync(
        patch.left, patch.top, patch.left + patch.width, patch.top + patch.height,
        s_present_staging);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "profile thinking patch update failed: %s", esp_err_to_name(err));
        s_front_frame_valid = false;
        return true;
    }
    if (s_front_frame_valid && s_framebuffers[s_front_frame]) {
        uint16_t *front = s_framebuffers[s_front_frame];
        for (int row = 0; row < patch.height; ++row) {
            memcpy(front + (size_t)(patch.top + row) * LCD_WIDTH + patch.left,
                   s_present_staging + (size_t)row * patch.width,
                   (size_t)patch.width * sizeof(uint16_t));
        }
    }
    return true;
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

static unsigned s_display_brightness = BACKLIGHT_BRIGHTNESS_DEFAULT;

// Applies a 0..100 brightness level to the backlight PWM.  Callers hold
// s_lcd_mutex, or run during legacy_bootstrap_input_initialize before concurrent display work.
// Preserve the physical adapter error so the shared Display facade does not
// acknowledge/persist a GUI setting that never reached the hardware.
static esp_err_t apply_backlight_brightness(unsigned percent) {
    return compact_display_service_set_brightness(percent);
}

static void wake_display_for_draw_locked(void) {
    if (!s_display_sleeping) return;
    esp_err_t err = compact_display_service_wake_from_display_off(s_display_brightness);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "DISPLAY_OFF wake transaction failed: %s", esp_err_to_name(err));
        return;
    }
    s_display_sleeping = false;
    // A controller is allowed to lose or alter GRAM while DISP is disabled.
    // Invalidate the front snapshot so the first wake draw transfers every row
    // even when its pixels happen to match the last pre-sleep ambient frame.
    s_front_frame_valid = false;
}

static void enter_ambient_awake_locked(void) {
    wake_display_for_draw_locked();
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
    bool presentation_ok = true;
    s_render_target = NULL;
    if (compact_display_service_uses_delta_presentation()) {
        uint16_t *previous = s_framebuffers[s_front_frame];
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
    } else {
        esp_err_t err = panel_draw_bitmap_sync(0, 0, LCD_WIDTH, LCD_HEIGHT, next);
        if (err != ESP_OK) {
            presentation_ok = false;
            ESP_LOGE(TAG, "LCD full-frame transfer failed: %s",
                     esp_err_to_name(err));
        }
    }
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
    if (!compact_display_service_ready()) return;
    if (s_render_target) {
        for (size_t i = 0; i < LCD_FRAME_PIXELS; ++i) s_render_target[i] = c;
        return;
    }
    uint16_t *line = s_present_staging;
    bool temporary_line = false;
    if (!line) {
        line = compact_display_service_allocate_transfer_buffer(
            (size_t)LCD_WIDTH * LCD_STRIPE_ROWS * sizeof(uint16_t));
        temporary_line = line != NULL;
    }
    if (!line) return;
    for (size_t i = 0; i < (size_t)LCD_WIDTH * LCD_STRIPE_ROWS; ++i) line[i] = c;
    for (int y = 0; y < LCD_HEIGHT; y += LCD_STRIPE_ROWS) {
        int y2 = y + LCD_STRIPE_ROWS < LCD_HEIGHT ? y + LCD_STRIPE_ROWS : LCD_HEIGHT;
        draw_bitmap_sync(0, y, LCD_WIDTH, y2, line);
    }
    if (temporary_line) compact_display_service_free_buffer(line);
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
    uint16_t *bitmap = allocate_temporary_display_bitmap(
        (size_t)width * 7 * scale * sizeof(uint16_t));
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
    free_temporary_display_bitmap(bitmap);
}

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
    fill_rect_solid(0, 0, LCD_WIDTH, RESPONSE_LAYOUT->header_height, header);
    fill_rect_solid(RESPONSE_TEXT_X,
                    RESPONSE_LAYOUT->title_accent_y,
                    RESPONSE_LAYOUT->title_accent_width,
                    RESPONSE_LAYOUT->title_accent_height,
                    accent);
    draw_text24_clipped(RESPONSE_TEXT_X + RESPONSE_LAYOUT->title_x_offset,
                        RESPONSE_LAYOUT->title_y,
                        s_response_title[0] ? s_response_title : "处理结果",
                        title, header, 8);
    fill_rect_solid(RESPONSE_TEXT_X, RESPONSE_LAYOUT->header_height - 1,
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
                        RESPONSE_LAYOUT->footer_hint_y,
                        pages > 1
                            ? (compact_input_service_response_paging_uses_volume_keys()
                                   ? "音量键翻页" : "自动翻页")
                            : "激活键返回",
                        muted, footer, 5);
    // The old centered page number occupied the same horizontal band as the
    // Chinese hint. Anchor it at the right edge in the compact ASCII renderer.
    const int indicator_width = (int)strlen(indicator) *
                                RESPONSE_LAYOUT->footer_indicator_advance;
    draw_ascii_at(LCD_WIDTH - RESPONSE_TEXT_X - indicator_width,
                  RESPONSE_LAYOUT->footer_indicator_y,
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
    uint16_t *bitmap = allocate_temporary_display_bitmap(
        (size_t)width * stripe_rows * sizeof(uint16_t));
    if (!bitmap) return;
    /* Rasterize text into the retained frame, never through the physical
     * panel while a composed frame is active.  Drawing an 8-row bitmap via
     * draw_bitmap_sync is correct but performs an extra allocation, copy and
     * bounds check for every glyph stripe.  Ambient animation redraws these
     * fixed labels only when the whole scene changes, yet its hot path still
     * benefits from keeping that composition local to the back buffer. */
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
        if (s_render_target) {
            for (int row = 0; row < stripe_rows; ++row) {
                memcpy(s_render_target + (size_t)(y + strip + row) * LCD_WIDTH + x,
                       bitmap + (size_t)row * width, (size_t)width * sizeof(uint16_t));
            }
        } else {
            draw_bitmap_sync(x, y + strip, x + width, y + strip + stripe_rows, bitmap);
        }
    }
    free_temporary_display_bitmap(bitmap);
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
    uint16_t *bitmap = allocate_temporary_display_bitmap(
        (size_t)width * height * sizeof(uint16_t));
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
    free_temporary_display_bitmap(bitmap);
}

static void draw_progress_bar(int x, int y, int width, int height, unsigned value,
                              uint16_t track, uint16_t fill) {
    if (value > 100) value = 100;
    uint16_t *bitmap = allocate_temporary_display_bitmap(
        (size_t)width * height * sizeof(uint16_t));
    if (!bitmap) return;
    int filled = width * (int)value / 100;
    for (int py = 0; py < height; ++py) {
        for (int px = 0; px < width; ++px) bitmap[py * width + px] = px < filled ? fill : track;
    }
    draw_bitmap_sync(x, y, x + width, y + height, bitmap);
    free_temporary_display_bitmap(bitmap);
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
    uint16_t *bitmap = allocate_temporary_display_bitmap(
        (size_t)width * stripe_rows * sizeof(uint16_t));
    if (!bitmap) return;
    for (int i = 0; i < width * stripe_rows; ++i) bitmap[i] = fill;
    for (int py = 0; py < height; py += stripe_rows) {
        int rows = height - py < stripe_rows ? height - py : stripe_rows;
        draw_bitmap_sync(x, y + py, x + width, y + py + rows, bitmap);
    }
    free_temporary_display_bitmap(bitmap);
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
    size_t bitmap_bytes = (size_t)width * height * sizeof(uint16_t);
    uint16_t *bitmap = allocate_temporary_display_bitmap(bitmap_bytes);
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
    free_temporary_display_bitmap(bitmap);
}

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

    /* Adjacent poses retain a large transparent canvas and many unchanged
     * opaque pixels.  Those cases formerly paid the complete premultiplied
     * blend (including several divisions) for every pixel, even though their
     * result is independent of `mix`.  The compact Display HAL deliberately
     * owns the physical transport budget, but this shared compositor must
     * also keep a slow serial profile from spending most of its frame time
     * blending background.  Preserve the exact established alpha-composite
     * result while taking the common constant-pixel fast path. */
    if (first_alpha == second_alpha) {
        if (first_alpha == 0u) {
            return (uint16_t)((background << 8) | (background >> 8));
        }
        if (first_valid && second_valid &&
            first_source[first_index] == second_source[second_index] &&
            first_source[first_index + 1] == second_source[second_index + 1]) {
            return composite_remote_pet_pixel(first_source, first_index, background);
        }
    }

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

    /* Animated pet packs normally keep their painted body fully opaque.  The
     * general premultiplied-alpha path below is required around anti-aliased
     * edges, but using it for two opaque samples performs three software
     * divisions per pixel on ESP32-S3.  On Fangtang's 40 MHz serial panel that
     * CPU time was large enough to starve a completed-frame cadence even when
     * the double PSRAM framebuffers and delta presenter transferred only the
     * changed rectangle.  Linear RGB565 channel interpolation is mathematically
     * identical when both alpha values are 255, and keeps this common renderer
     * independent of a board model or panel transport. */
    if (first_alpha == 255u && second_alpha == 255u) {
        const uint16_t blended = (uint16_t)((((((first >> 11) & 0x1fu) * inverse +
                                                ((second >> 11) & 0x1fu) * mix + 128u) >> 8)
                                              << 11) |
                                             (((((first >> 5) & 0x3fu) * inverse +
                                                ((second >> 5) & 0x3fu) * mix + 128u) >> 8)
                                              << 5) |
                                             ((((first & 0x1fu) * inverse +
                                                (second & 0x1fu) * mix + 128u) >> 8)));
        return (uint16_t)((blended << 8) | (blended >> 8));
    }
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

bool legacy_display_scene_get_pet_asset_install_budget(size_t source_width, size_t source_height,
                                             size_t frame_count, size_t *out_total_external_bytes,
                                             size_t *out_max_external_allocation_bytes,
                                             size_t *out_max_frame_count) {
    if (!out_total_external_bytes || !out_max_external_allocation_bytes ||
        !out_max_frame_count || frame_count > REMOTE_PET_MAX_FRAMES) return false;
    if (frame_count == 0) {
        *out_total_external_bytes = 0;
        *out_max_external_allocation_bytes = 0;
        *out_max_frame_count = REMOTE_PET_MAX_FRAMES;
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
    *out_total_external_bytes = frame_bytes * frame_count;
    *out_max_external_allocation_bytes = frame_bytes;
    *out_max_frame_count = REMOTE_PET_MAX_FRAMES;
    return true;
}

bool legacy_storage_admission_allows_optional_flash_work(void) {
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
            const size_t source_row = (size_t)y * s_remote_pet_width * 3u;
            for (int x = 0; x < target_w; ++x) {
                /* Installed frames are pre-scaled to this exact target size.
                 * Mapping each pixel through the old Q8 nearest-neighbour path
                 * twice did no resampling at all: it only repeated divisions
                 * and bounds checks for the two adjacent keyframes. Both
                 * keyframes therefore use the same direct source index. */
                const size_t source_index = source_row + (size_t)x * 3u;
                destination[x] = composite_interpolated_remote_pet_samples(
                    source, next_source, source_index, source_index,
                    true, true, background, mix);
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
            const size_t source_row = (size_t)y * s_remote_pet_width * 3u;
            for (int x = 0; x < target_w; ++x) {
                const size_t source_index = source_row + (size_t)x * 3u;
                destination[x] = composite_interpolated_remote_pet_samples(
                    source, next_source, source_index, source_index,
                    true, true, background, mix);
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
    /* A panel with a distinct partial-update region supplies pixels only.
     * Bread falls through to the shared robot-mouth compositor. */
    if (draw_profile_thinking_patch()) return;
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
}

static void thinking_mouth_task(void *arg) {
    (void)arg;
    while (true) {
        /* The profile owns the panel-qualified decorative cadence.  Double
         * buffering eliminates composition tearing, but it cannot improve a
         * deliberately slow worker tick.  In particular Fangtang's small
         * profile patch is safe to update more frequently than the legacy
         * common 420 ms cadence. */
        const uint32_t interval_ms = compact_display_service_thinking_worker_wait_ms(
            THINKING_MOUTH_FRAME_MS);
        /* The Display HAL owns the animation task's stop notification and
         * the millisecond-to-tick conversion.  Keeping the worker callback
         * on this semantic wait makes the thinking and pet paths obey the
         * same lifecycle contract; a new panel need only supply its Display
         * adapter rather than teach this scene about FreeRTOS timing. */
        if (!compact_display_service_animation_wait_ms(
                COMPACT_DISPLAY_ANIMATION_THINKING, interval_ms)) break;
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
}

static void show_state_screen(const char *state) {
    s_thinking_surface_visible = false;
    uint16_t bg = state_color(state);
    bool composed = begin_screen_frame();
    fill_screen(bg);
    bool ambient = !strcmp(state, "idle") || !strcmp(state, "quiet");
    const compact_profile_identity_state_t identity = {
        .state = state,
        .ambient_time = s_ambient_time,
        .ambient_date = s_ambient_date,
        .ambient_weekday = s_ambient_weekday,
        .command_stage = s_command_stage,
        .gateway_ready = s_gateway_ready,
        .animation_phase = s_thinking_mouth_frame,
    };
    if (!compact_visual_profile_render_state_identity(&identity, ambient, bg)) {
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
    }
    finish_screen_frame(composed);
    s_thinking_surface_visible = !ambient && !strcmp(state, "thinking");
}

static void ensure_thinking_mouth_task(void) {
    if (!s_background_tasks_lock ||
        xSemaphoreTake(s_background_tasks_lock, pdMS_TO_TICKS(100)) != pdTRUE) return;
    if (s_background_tasks_admission_closed) {
        xSemaphoreGive(s_background_tasks_lock);
        return;
    }
    if (compact_display_service_animation_running(
            COMPACT_DISPLAY_ANIMATION_THINKING)) {
        xSemaphoreGive(s_background_tasks_lock);
        return;
    }
    const esp_err_t err = compact_display_service_start_animation(
        COMPACT_DISPLAY_ANIMATION_THINKING, thinking_mouth_task, NULL);
    if (err != ESP_OK && err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "thinking mouth animation disabled: %s", esp_err_to_name(err));
    }
    xSemaphoreGive(s_background_tasks_lock);
}

static bool show_remote_pet_animation_frame(void) {
    if (!s_front_frame_valid || !begin_composed_frame()) return false;

    /* Preserve the clock and every other static surface from the front image,
     * but rebuild the complete pet rectangle from its flat background on each
     * tick. A back buffer can be more than one pose old after a full-screen
     * draw, so merely painting a new silhouette onto it leaves departed opaque
     * pixels behind.
     *
     * Do not copy the entire screen just to overwrite the pet rectangle. On
     * the Fangtang 40 MHz SPI profile that full PSRAM read/write is concurrent
     * with blending and LCD DMA, making a retained two-frame renderer look
     * slower than it is. Copy only the static pixels around the normalized
     * standby pet rectangle, then fill that rectangle below. This is scene
     * geometry rather than a board branch: any compact visual profile benefits
     * while retaining exactly the same front/back framebuffer invariant. */
    uint16_t *front = s_framebuffers[s_front_frame];
    const int pet_left = (LCD_WIDTH - AMBIENT_PET_MAX_WIDTH) / 2;
    const int pet_top = AMBIENT_PET_TOP;
    const int pet_right = pet_left + AMBIENT_PET_MAX_WIDTH;
    const int pet_bottom = pet_top + AMBIENT_PET_MAX_HEIGHT;
    if (!front || pet_left < 0 || pet_top < 0 || pet_right > LCD_WIDTH ||
        pet_bottom > LCD_HEIGHT) {
        s_render_target = NULL;
        return false;
    }
    if (pet_top > 0) {
        memcpy(s_render_target, front,
               (size_t)pet_top * LCD_WIDTH * sizeof(*front));
    }
    for (int y = pet_top; y < pet_bottom; ++y) {
        uint16_t *destination = s_render_target + (size_t)y * LCD_WIDTH;
        const uint16_t *source = front + (size_t)y * LCD_WIDTH;
        if (pet_left > 0) {
            memcpy(destination, source, (size_t)pet_left * sizeof(*front));
        }
        if (pet_right < LCD_WIDTH) {
            memcpy(destination + pet_right, source + pet_right,
                   (size_t)(LCD_WIDTH - pet_right) * sizeof(*front));
        }
    }
    if (pet_bottom < LCD_HEIGHT) {
        memcpy(s_render_target + (size_t)pet_bottom * LCD_WIDTH,
               front + (size_t)pet_bottom * LCD_WIDTH,
               (size_t)(LCD_HEIGHT - pet_bottom) * LCD_WIDTH * sizeof(*front));
    }
    uint16_t bg = state_color(s_state);
    fill_rect_solid(pet_left, pet_top,
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
    const compact_profile_identity_state_t identity = {
        .state = s_state,
        .ambient_time = s_ambient_time,
        .ambient_date = s_ambient_date,
        .ambient_weekday = s_ambient_weekday,
        .command_stage = s_command_stage,
        .gateway_ready = s_gateway_ready,
        .animation_phase = s_thinking_mouth_frame,
    };
    if (!compact_visual_profile_render_status_identity(&identity, title, line, bg)) {
        // Message/ready surfaces use the same visual identity as the reusable pet
        // states. Keeping the face here avoids the old bare "MACLAW / READY" page
        // while still leaving two calm, high-contrast rows for status copy.
        draw_robot_face_at(s_state, 54, 4, 55, bg);
        draw_text24_centered(190, title && title[0] ? title : "码卡龙",
                             color(248, 252, 255), bg, 9);
        draw_text24_centered(236, line && line[0] ? line : "设备已就绪",
                             color(121, 210, 224), bg, 10);
        draw_text24_centered(280, "请使用激活键", color(157, 184, 205), bg, 8);
    }
    finish_screen_frame(composed);
}

static void lcd_startup_screen(void) {
    if (!compact_display_service_ready()) return;
    if (compact_visual_profile_render_startup_art()) return;

    const compact_startup_full_frame_t art = compact_display_service_startup_full_frame();
    const size_t expected_bytes = (size_t)LCD_WIDTH * LCD_HEIGHT * sizeof(uint16_t);
    if (!art.pixels || art.width != LCD_WIDTH || art.height != LCD_HEIGHT ||
        art.bytes != expected_bytes) {
        ESP_LOGE(TAG, "invalid startup artwork: %u bytes (expected %u)",
                 (unsigned)art.bytes, (unsigned)expected_bytes);
        fill_screen(state_color("idle"));
        return;
    }

    // Present directly in DMA-sized bands. Avoid copying this immutable
    // 150 KiB full-screen asset through PSRAM/double buffering at boot.
    for (int y = 0; y < LCD_HEIGHT; y += LCD_STRIPE_ROWS) {
        int y2 = y + LCD_STRIPE_ROWS < LCD_HEIGHT ? y + LCD_STRIPE_ROWS : LCD_HEIGHT;
        ESP_ERROR_CHECK_WITHOUT_ABORT(draw_bitmap_sync(
            0, y, LCD_WIDTH, y2, art.pixels + (size_t)y * LCD_WIDTH));
    }
}

static void remote_pet_animation_task(void *arg) {
    (void)arg;
    uint64_t rendered_tick = UINT64_MAX;
    int64_t previous_animation_started_us = 0;
    uint32_t previous_presentation_us = 0;
    while (true) {
        /* The display profile may select a faster, qualified cadence for an
        * animated pet.  Treat that returned interval as the presentation
        * clock as well as the task wait: advancing the blend phase by the
        * old common constant would make a board-specific faster refresh play
        * the remote pack too quickly. */
        const uint32_t render_interval_ms = compact_display_service_pet_worker_wait_ms(
            s_remote_pet_frame_count, REMOTE_PET_RENDER_FRAME_MS);
        /* The profile interval is a completed-frame cadence, not an extra
         * delay to append after the panel has already spent that long drawing.
         *
         * This distinction matters for Fangtang's single-lane SPI panel: a
         * dense 176 px pet transition can take about 100 ms to compose and
         * present.  Sleeping its historical 50 ms again afterwards reduced
         * the real cadence to roughly 150 ms even though both PSRAM frame
         * buffers and delta presentation were working correctly.  Wait only
         * for the unused part of the interval.  A one-tick yield after an
         * over-budget frame preserves cancellation responsiveness and avoids
         * a busy loop, while allowing the next retained back buffer to start
         * as soon as the serial transport is available.
         *
         * This remains scene-neutral: the Display HAL supplies the physical
         * cadence and the renderer still owns frame ordering, DMA lifetime,
         * and elapsed-time interpolation. */
        uint32_t wait_ms = render_interval_ms;
        if (s_remote_pet_frame_count > 1 && previous_presentation_us) {
            const uint64_t interval_us = (uint64_t)render_interval_ms * 1000u;
            if ((uint64_t)previous_presentation_us < interval_us) {
                wait_ms = (uint32_t)((interval_us - previous_presentation_us + 999u) /
                                     1000u);
            } else {
                wait_ms = 1u;
            }
        }
        if (!compact_display_service_animation_wait_ms(
                COMPACT_DISPLAY_ANIMATION_PET, wait_ms)) break;
        xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
        if (RESPONSE_LAYOUT->automatic_page_interval_us > 0 &&
            s_response_active && !s_response_image_active && s_response_next_page_us > 0 &&
            esp_timer_get_time() >= s_response_next_page_us) {
            unsigned pages = response_page_count();
            if (s_response_page + 1 < pages) {
                ++s_response_page;
                s_response_next_page_us = esp_timer_get_time() +
                                          RESPONSE_LAYOUT->automatic_page_interval_us;
                draw_response_page();
            } else {
                s_response_next_page_us = 0;
            }
        }
        bool ambient = !strcmp(s_state, "idle") || !strcmp(s_state, "quiet");
        uint32_t blocked_mask = 0;
        if (!s_pet_motion_enabled) blocked_mask |= 1u << 0;
        if (s_remote_pet_frame_count < 2) blocked_mask |= 1u << 1;
        if (!s_remote_pet_frame_ms) blocked_mask |= 1u << 2;
        if (!ambient) blocked_mask |= 1u << 3;
        if (s_display_sleeping) blocked_mask |= 1u << 4;
        if (s_foreground_surface) blocked_mask |= 1u << 5;
        if (s_recording_active) blocked_mask |= 1u << 6;
        if (s_alarm_visual_active) blocked_mask |= 1u << 7;
        if (!blocked_mask) {
            s_remote_pet_animation_blocked_mask = 0;
            s_remote_pet_animation_blocked_logged_us = 0;
            uint64_t current_tick = ((uint64_t)esp_timer_get_time() / 1000u) /
                                    render_interval_ms;
            if (current_tick != rendered_tick) {
                uint64_t loop_ms = (uint64_t)s_remote_pet_frame_ms *
                                   s_remote_pet_frame_count;
                const int64_t presentation_started_us = esp_timer_get_time();
                uint32_t phase_advance_ms = render_interval_ms;
                if (compact_display_service_pet_animation_tracks_elapsed_time() &&
                    previous_animation_started_us > 0 &&
                    presentation_started_us > previous_animation_started_us) {
                    const int64_t observed_us = presentation_started_us -
                                                previous_animation_started_us;
                    /* A short SPI-busy interval should advance the authored
                     * pack at real presentation time.  Bound an unusually
                     * long scheduler stall so resuming an idle display cannot
                     * teleport across an arbitrary number of poses. */
                    const uint64_t observed_ms = (uint64_t)observed_us / 1000u;
                    phase_advance_ms = observed_ms > 250u ? 250u :
                                       (uint32_t)observed_ms;
                    if (!phase_advance_ms) phase_advance_ms = render_interval_ms;
                }
                previous_animation_started_us = presentation_started_us;
                s_remote_pet_animation_elapsed_ms += phase_advance_ms;
                if (loop_ms) s_remote_pet_animation_elapsed_ms %= loop_ms;
                const bool presented = show_remote_pet_animation_frame();
                const int64_t presentation_elapsed_us =
                    esp_timer_get_time() - presentation_started_us;
                const uint32_t bounded_presentation_us =
                    presentation_elapsed_us > UINT32_MAX ? UINT32_MAX :
                                                          (uint32_t)presentation_elapsed_us;
                compact_display_service_note_pet_animation_tick(
                    render_interval_ms, presented, bounded_presentation_us);
                previous_presentation_us = presented ? bounded_presentation_us : 0;
                if (!presented) show_state_screen(s_state);
                rendered_tick = current_tick;
            }
        } else {
            const int64_t now_us = esp_timer_get_time();
            if (blocked_mask != s_remote_pet_animation_blocked_mask ||
                now_us - s_remote_pet_animation_blocked_logged_us >= 5000000) {
                ESP_LOGI(TAG,
                         "pet animation waiting: mask=0x%02lx motion=%d frames=%u frame_ms=%lu state=%s sleeping=%d foreground=%d recording=%d alarm=%d",
                         (unsigned long)blocked_mask, s_pet_motion_enabled,
                         (unsigned)s_remote_pet_frame_count,
                         (unsigned long)s_remote_pet_frame_ms, s_state,
                         s_display_sleeping, s_foreground_surface,
                         s_recording_active, s_alarm_visual_active);
                s_remote_pet_animation_blocked_mask = blocked_mask;
                s_remote_pet_animation_blocked_logged_us = now_us;
            }
            rendered_tick = UINT64_MAX;
            previous_animation_started_us = 0;
            previous_presentation_us = 0;
        }
        xSemaphoreGiveRecursive(s_lcd_mutex);
    }
}

static void ensure_remote_pet_animation_task(void) {
    if (!s_background_tasks_lock ||
        xSemaphoreTake(s_background_tasks_lock, pdMS_TO_TICKS(100)) != pdTRUE) return;
    /* A decorative worker has no useful idle responsibility. Starting it
     * before an asset arrives can leave a zero-frame worker alive across the
     * cache restore, where its lifecycle slot then masks the real animation
     * creation request. Admit it only once a motion-enabled multi-frame pack
     * is actually resident. */
    if (s_background_tasks_admission_closed || !s_pet_motion_enabled ||
        s_remote_pet_frame_count < 2 || !s_remote_pet_frame_ms) {
        xSemaphoreGive(s_background_tasks_lock);
        return;
    }
    if (compact_display_service_animation_running(COMPACT_DISPLAY_ANIMATION_PET)) {
        ESP_LOGI(TAG, "pet animation worker already running: motion=%d frames=%u",
                 s_pet_motion_enabled, (unsigned)s_remote_pet_frame_count);
        xSemaphoreGive(s_background_tasks_lock);
        return;
    }
    const esp_err_t err = compact_display_service_start_animation(
        COMPACT_DISPLAY_ANIMATION_PET, remote_pet_animation_task, NULL);
    if (err == ESP_OK) {
        ESP_LOGI(TAG, "pet animation worker started: motion=%d frames=%u frame_ms=%lu",
                 s_pet_motion_enabled, (unsigned)s_remote_pet_frame_count,
                 (unsigned long)s_remote_pet_frame_ms);
    }
    if (err != ESP_OK && err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "remote pet animation disabled: %s", esp_err_to_name(err));
    }
    xSemaphoreGive(s_background_tasks_lock);
}

static void stop_remote_pet_animation_locked(void) {
    /* This is a best-effort decorative teardown, never an application-path
     * failure. Do not wait while holding the scene mutex: the worker needs it
     * to observe its stop notification and complete safely. */
    if (!compact_display_service_animation_running(COMPACT_DISPLAY_ANIMATION_PET)) {
        return;
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
    const esp_err_t err = compact_display_service_stop_animation(
        COMPACT_DISPLAY_ANIMATION_PET, 1000);
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    if (err != ESP_OK && err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "remote pet animation stop failed: %s", esp_err_to_name(err));
    }
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
    ESP_RETURN_ON_ERROR(compact_audio_service_prepare(5000), TAG,
                        "direct-I2S hardware init");
    /* Keep TX stopped while it is idle. Leaving the channel enabled after a
     * short chime lets some direct-I2S amplifiers continue reproducing the
     * final DMA descriptor, which sounds like a tone that never ends. Each
     * playback owns an explicit enable/write/disable cycle below. */
    return ESP_OK;
}

static void compact_renderer_publish_input(device_input_action_t action,
                                           device_input_source_t source,
                                           void *context) {
    (void)context;
    if (s_button_cb) s_button_cb(action, source, s_button_arg);
}

esp_err_t legacy_bootstrap_input_initialize(void) {
    /* A successful panel bring-up is deliberately boot-lifetime. Reject a
     * second init attempt before changing callbacks or allocating a second
     * set of renderer synchronization objects; this is a fail-closed
     * lifecycle boundary, not a restart API. */
    if (s_background_tasks_lock || s_lcd_mutex ||
        compact_display_service_ready()) {
        return ESP_ERR_INVALID_STATE;
    }
    // Hub reachability is session state. Never restore a previous boot's
    // ONLINE bit: the current handshake/poll must prove it again.
    s_gateway_ready = false;
    s_background_tasks_lock = xSemaphoreCreateMutex();
    if (!s_background_tasks_lock) {
        compact_renderer_discard_unpublished_init_state();
        return ESP_ERR_NO_MEM;
    }
    s_lcd_mutex = xSemaphoreCreateRecursiveMutex();
    if (!s_lcd_mutex) {
        compact_renderer_discard_unpublished_init_state();
        return ESP_ERR_NO_MEM;
    }
    /* Display construction is a profile-private transaction.  Do not turn an
     * adapter failure into ESP_ERROR_CHECK abort/reboot here: Input Service
     * must receive the ordinary error so the common lifecycle rollback can
     * enter its diagnostic/degraded path after the adapter has released its
     * partial panel resources. */
    s_display_brightness = compact_display_service_default_brightness();
    esp_err_t display_init_err = compact_display_service_initialize();
    if (display_init_err != ESP_OK) {
        ESP_LOGE(TAG, "compact display adapter init failed: %s",
                 esp_err_to_name(display_init_err));
        compact_renderer_discard_unpublished_init_state();
        return display_init_err;
    }
    display_init_err = apply_backlight_brightness(s_display_brightness);
    if (display_init_err != ESP_OK) {
        ESP_LOGE(TAG, "compact display brightness init failed: %s",
                 esp_err_to_name(display_init_err));
        return compact_renderer_fail_after_hardware_init(display_init_err,
                                                         "initial brightness");
    }
    compact_profile_bind_renderer_primitives();
    for (size_t i = 0; i < 2; ++i) {
        s_framebuffers[i] = compact_display_service_allocate_framebuffer(LCD_FRAME_BYTES);
        if (!s_framebuffers[i]) {
            ESP_LOGW(TAG, "cannot allocate LCD framebuffer %u", (unsigned)i);
            break;
        }
        memset(s_framebuffers[i], 0, LCD_FRAME_BYTES);
    }
    s_present_staging = compact_display_service_allocate_transfer_buffer(
        (size_t)LCD_WIDTH * LCD_STRIPE_ROWS * sizeof(uint16_t));
    if (!s_framebuffers[0] || !s_framebuffers[1] || !s_present_staging
    ) {
        for (size_t i = 0; i < 2; ++i) {
            if (s_framebuffers[i]) compact_display_service_free_buffer(s_framebuffers[i]);
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
    esp_err_t input_init_err = compact_input_service_initialize();
    if (input_init_err != ESP_OK) {
        return compact_renderer_fail_after_hardware_init(input_init_err,
                                                         "input adapter");
    }
    if (provisioning_failure_injection_compact_renderer_initialization_should_fail_after(1)) {
        return compact_renderer_fail_after_hardware_init(ESP_FAIL, "test input adapter");
    }
    compact_input_raw_state_t input_idle = {0};
    compact_input_service_read_raw(&input_idle);
    ESP_LOGI(TAG, "input adapter ready: %s activate_released=%d volume_keys=%d",
             compact_input_service_name(), input_idle.activate_released ? 1 : 0,
             compact_input_service_has_volume_keys() ? 1 : 0);
    esp_err_t audio_init_err = audio_init();
    if (audio_init_err != ESP_OK) {
        return compact_renderer_fail_after_hardware_init(audio_init_err,
                                                         "audio adapter");
    }
    if (provisioning_failure_injection_compact_renderer_initialization_should_fail_after(2)) {
        return compact_renderer_fail_after_hardware_init(ESP_FAIL, "test audio adapter");
    }
    esp_err_t peripheral_init_err = compact_peripheral_service_initialize();
    if (peripheral_init_err != ESP_OK) {
        return compact_renderer_fail_after_hardware_init(peripheral_init_err,
                                                         "profile peripheral adapter");
    }
    if (provisioning_failure_injection_compact_renderer_initialization_should_fail_after(3)) {
        return compact_renderer_fail_after_hardware_init(ESP_FAIL,
                                                         "test profile peripheral adapter");
    }
    /* Input profiles may own a bounded boot-time control window before the
     * common gesture scanner starts. Bread's adapter is a no-op; Fangtang's
     * adapter reserves GPIO0 for its existing network selector.  This must
     * happen before Connectivity chooses its uplink, but it does not publish
     * a command-input callback or create a scanner task. */
    compact_input_service_run_startup_selector();
    esp_err_t animation_deadline_test_err =
        compact_display_service_run_animation_deadline_test();
    if (animation_deadline_test_err != ESP_OK) {
        return compact_renderer_fail_after_hardware_init(animation_deadline_test_err,
                                                         "animation stop deadline test");
    }
    // Power Service owns the idle-display deadline. The pet worker is started
    // only when a motion-enabled multi-frame asset is installed, avoiding a
    // zero-frame lifecycle slot during cache restore.
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

esp_err_t legacy_bootstrap_input_start_scanner(
    legacy_input_scanner_publish_cb_t cb, void *arg) {
    /* Bootstrap constructs the panel/audio/peripheral transaction exactly
     * once.  Scanner startup is deliberately a later Input Service concern;
     * never let it recreate or reconfigure boot-lifetime renderer state. */
    if (!cb || !s_background_tasks_lock || !s_lcd_mutex ||
        !compact_display_service_ready()) {
        return ESP_ERR_INVALID_STATE;
    }
    if (s_button_cb) {
        return ESP_ERR_INVALID_STATE;
    }
    s_button_cb = cb;
    s_button_arg = arg;
    esp_err_t scanner_prepare_err = compact_input_service_prepare_scanner();
    if (scanner_prepare_err != ESP_OK) {
        return compact_renderer_fail_after_hardware_init(scanner_prepare_err,
                                                         "input scanner preparation");
    }
    if (provisioning_failure_injection_compact_renderer_initialization_should_fail_after(4)) {
        return compact_renderer_fail_after_hardware_init(ESP_FAIL,
                                                         "test input completion semaphore");
    }
    if (provisioning_failure_injection_compact_renderer_initialization_should_fail_after(5)) {
        return compact_renderer_fail_after_hardware_init(ESP_FAIL,
                                                         "test before input scanner task");
    }
    esp_err_t scanner_start_err = compact_input_service_start_scanner(
        compact_renderer_publish_input, NULL);
    if (scanner_start_err != ESP_OK) {
        return compact_renderer_fail_after_hardware_init(scanner_start_err,
                                                         "input scanner task");
    }
    /* The task has now been published and may access its completion semaphore.
     * Stage 6 therefore verifies that the normal cooperative stop/join route
     * owns shutdown; the failure helper must not reclaim task-owned state. */
    if (provisioning_failure_injection_compact_renderer_initialization_should_fail_after(6)) {
        return compact_renderer_fail_after_hardware_init(ESP_FAIL,
                                                          "test after input scanner task");
    }
    return ESP_OK;
}

bool legacy_connectivity_transport_load_selection(bool *out_cellular) {
    return compact_connectivity_service_load_transport_selection(out_cellular);
}

bool legacy_connectivity_transport_apply_startup_toggle(uint32_t window_ms,
                                                         bool current_cellular,
                                                         bool *out_cellular) {
    /* Input owns raw GPIO sampling and the boot-window gesture. Pass only its
     * normalized result to Connectivity so the selected modem adapter never
     * gains a private dependency on another HAL implementation. */
    const bool toggle_requested =
        compact_input_service_consume_startup_selector_result(window_ms);
    return compact_connectivity_service_apply_startup_transport_toggle(
        toggle_requested, current_cellular, out_cellular);
}

void legacy_connectivity_transport_adapt_gateway_url(char *gateway_url, size_t capacity,
                                                     bool cellular_active) {
    compact_connectivity_service_adapt_gateway_url(gateway_url, capacity,
                                                   cellular_active);
}

esp_err_t legacy_connectivity_transport_prepare_cellular(void) {
    return compact_connectivity_service_prepare_cellular_transport();
}

bool legacy_connectivity_transport_cancel_foreground_request(void) {
    return compact_connectivity_service_cancel_cellular_foreground_request();
}

bool legacy_connectivity_transport_cancel_requests_for_owner(const void *owner) {
    return compact_connectivity_service_cancel_cellular_requests_for_owner(owner);
}

esp_err_t legacy_connectivity_transport_start_cellular(uint32_t timeout_ms) {
    return compact_connectivity_service_start_cellular_transport(timeout_ms);
}

bool legacy_connectivity_transport_cellular_ready(void) {
    return compact_connectivity_service_is_cellular_transport_ready();
}

esp_err_t legacy_connectivity_transport_quiesce_cellular(uint32_t timeout_ms) {
    return compact_connectivity_service_quiesce_cellular_transport(timeout_ms);
}

esp_err_t legacy_connectivity_transport_http_request(
    const device_connectivity_http_request_t *request) {
    return compact_connectivity_service_cellular_http_request(request);
}

esp_err_t legacy_connectivity_transport_http_stream_request(
    const device_connectivity_stream_request_t *request) {
    return compact_connectivity_service_cellular_http_stream_request(request);
}

void legacy_display_scene_show_startup(void) {
    if (!compact_display_service_ready() || !s_lcd_mutex) return;
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
esp_err_t legacy_display_scene_set_brightness(unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    if (s_lcd_mutex &&
        xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) == pdTRUE) {
        const unsigned previous = s_display_brightness;
        s_display_brightness = percent;
        // A sleeping panel keeps the level pending; the wake path re-applies
        // it together with DISP ON.  Level 0 is a valid
        // backlight-off-while-running state, distinct from DISPLAY_OFF.
        esp_err_t err = ESP_OK;
        if (!s_display_sleeping) err = apply_backlight_brightness(percent);
        if (err != ESP_OK) {
            s_display_brightness = previous;
            xSemaphoreGiveRecursive(s_lcd_mutex);
            return err;
        }
        xSemaphoreGiveRecursive(s_lcd_mutex);
    } else {
        s_display_brightness = percent;
    }
    return ESP_OK;
}

esp_err_t legacy_bootstrap_input_stop_scanner(uint32_t timeout_ms) {
    const esp_err_t status = compact_input_service_stop_scanner(timeout_ms);
    if (status == ESP_OK) {
        /* The scanner has joined, so no task can still call this renderer's
         * forwarding callback. Drop the old Input Service generation before a
         * later start installs a fresh normalized publisher. */
        s_button_cb = NULL;
        s_button_arg = NULL;
    }
    return status;
}

esp_err_t board_background_lifecycle_stop(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    /* Input/board initialization can fail before this renderer has published
     * any decorative worker or even its creation mutex. A rollback then owns
     * no background task to join; treating that inactive state as a timeout
     * prevents later independent lifecycle owners (notably Display Service)
     * from closing. This is an idempotent no-op, not board restart support. */
    if (!s_background_tasks_lock) return ESP_OK;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t deadline = pdMS_TO_TICKS(timeout_ms);
    if (xSemaphoreTake(s_background_tasks_lock, deadline) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    /* This lock is also the only task-creation gate.  Closing it before the
     * joins makes rollback linear: a late pet/state publication can neither
     * start nor resurrect a decorative renderer worker after this point. */
    s_background_tasks_admission_closed = true;
    TickType_t elapsed = xTaskGetTickCount() - started;
    TickType_t remaining = elapsed >= deadline ? 0 : deadline - elapsed;
    esp_err_t err = remaining == 0 ? ESP_ERR_TIMEOUT :
        compact_display_service_stop_animation(
            COMPACT_DISPLAY_ANIMATION_THINKING,
            (uint32_t)remaining * portTICK_PERIOD_MS);
    elapsed = xTaskGetTickCount() - started;
    remaining = elapsed >= deadline ? 0 : deadline - elapsed;
    if (err == ESP_OK && remaining > 0) {
        err = compact_display_service_stop_animation(
            COMPACT_DISPLAY_ANIMATION_PET,
            (uint32_t)remaining * portTICK_PERIOD_MS);
    } else if (err == ESP_OK) {
        err = ESP_ERR_TIMEOUT;
    }
    elapsed = xTaskGetTickCount() - started;
    remaining = elapsed >= deadline ? 0 : deadline - elapsed;
    if (err == ESP_OK && remaining > 0) {
        err = compact_peripheral_service_stop_background_tasks(
            (uint32_t)remaining * portTICK_PERIOD_MS);
    } else if (err == ESP_OK) {
        err = ESP_ERR_TIMEOUT;
    }
    xSemaphoreGive(s_background_tasks_lock);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "timed out stopping board background task");
    } else {
        ESP_LOGI(TAG, "board background tasks stopped");
    }
    return err;
}

void legacy_display_scene_set_pet_state(const char *state) {
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
    }
    if (!strcmp(next_state, "thinking")) {
        s_thinking_mouth_frame = 0;
        ensure_thinking_mouth_task();
    }
    if (!s_recording_active) {
        s_response_active = false;
        s_foreground_surface = !ambient;
        show_state_screen(s_state);
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void legacy_display_scene_set_command_stage(const char *stage) {
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
void legacy_display_scene_set_command_lock(bool locked) {
    if (s_lcd_mutex) xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_foreground_surface = locked;
    if (s_lcd_mutex) xSemaphoreGiveRecursive(s_lcd_mutex);
}
void legacy_display_scene_set_pet_profile(const char *skin, bool motion_enabled) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    bool changed = skin && skin[0] && strcmp(s_pet_skin, skin) != 0;
    bool motion_changed = s_pet_motion_enabled != motion_enabled;
    if (changed) strlcpy(s_pet_skin, skin, sizeof(s_pet_skin));
    s_pet_motion_enabled = motion_enabled;
    if (motion_changed && !motion_enabled) stop_remote_pet_animation_locked();
    if (motion_changed && motion_enabled && s_remote_pet_frame_count > 1) {
        ensure_remote_pet_animation_task();
    }
    if ((changed || motion_changed) && !s_foreground_surface && !s_recording_active) {
        if (s_display_sleeping) {
            xSemaphoreGiveRecursive(s_lcd_mutex);
            return;
        }
        show_state_screen(s_state);
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
esp_err_t legacy_display_scene_set_pet_asset(const uint8_t *const *frames, size_t frame_count,
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
            for (size_t j = 0; j < i; ++j) {
                compact_display_service_free_remote_pet_frame(copies[j]);
            }
            return ESP_ERR_INVALID_ARG;
        }
        copies[i] = compact_display_service_allocate_remote_pet_frame(bytes);
        if (!copies[i]) {
            for (size_t j = 0; j < i; ++j) {
                compact_display_service_free_remote_pet_frame(copies[j]);
            }
            return ESP_ERR_NO_MEM;
        }
        scale_remote_pet_frame(frames[i], width, height, copies[i],
                               target_width, target_height);
    }
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    for (size_t i = 0; i < REMOTE_PET_MAX_FRAMES; ++i) {
        compact_display_service_free_remote_pet_frame(s_remote_pet_frames[i]);
        s_remote_pet_frames[i] = copies[i];
    }
    s_remote_pet_frame_count = frame_count;
    s_remote_pet_width = frame_count ? target_width : 0;
    s_remote_pet_height = frame_count ? target_height : 0;
    s_remote_pet_frame_ms = frame_ms ? frame_ms : REMOTE_PET_DEFAULT_KEYFRAME_MS;
    s_remote_pet_animation_elapsed_ms = 0;
    if (s_pet_motion_enabled) ensure_remote_pet_animation_task();
    if (!s_display_sleeping && !s_foreground_surface && !s_recording_active) {
        show_state_screen(s_state);
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
    return ESP_OK;
}

// These boards copy every frame up front; the consuming contract simply
// releases the caller's sources after the copy completes.
esp_err_t legacy_display_scene_set_pet_asset_consuming(uint8_t **frames, size_t frame_count,
                                             size_t width, size_t height, uint32_t frame_ms) {
    esp_err_t err = legacy_display_scene_set_pet_asset((const uint8_t *const *)frames,
                                             frame_count, width, height, frame_ms);
    if (err == ESP_OK && frames) {
        for (size_t i = 0; i < frame_count && i < REMOTE_PET_MAX_FRAMES; ++i) {
            compact_display_service_release_consumed_pet_source(frames[i]);
            frames[i] = NULL;
        }
    }
    return err;
}
void legacy_display_scene_set_recording_mode(bool meeting) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_recording_mode = meeting;
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void legacy_display_scene_set_recording_visual(bool active, bool paused, uint32_t elapsed) {
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
    const compact_recording_layout_t *layout = RECORDING_LAYOUT;
    bool composed = begin_screen_frame();
    fill_screen(bg);
    fill_rect_solid(16, layout->accent_top_y, 208, 4, accent);
    fill_rect_solid(16, layout->accent_bottom_y, 208, 4, accent);
    fill_rect_solid(layout->icon_x, layout->icon_y,
                    layout->icon_outer_size, layout->icon_outer_size, accent);
    fill_rect_solid(layout->icon_x + layout->icon_inner_offset,
                    layout->icon_y + layout->icon_inner_offset,
                    layout->icon_inner_size, layout->icon_inner_size, color(255,235,238));
    draw_text24_clipped(layout->status_text_x, layout->status_text_y,
                        paused ? "已暂停" : "正在听取",
                        color(245,250,255), bg, layout->status_scale);
    draw_text24_centered(layout->title_y,
                         s_recording_mode ? "会议录音" : "语音指令",
                         paused ? color(244,178,58) : cyan, bg,
                         layout->title_scale);
    char timer[16];
    snprintf(timer, sizeof(timer), "%02lu:%02lu", (unsigned long)(elapsed / 60), (unsigned long)(elapsed % 60));
    draw_ascii_centered(layout->timer_y, timer, color(255,255,255), bg);
    fill_rect_solid(20, layout->waveform_rule_y, 200, 1, muted);
    for (int column = 0; column < 24; ++column) {
        uint16_t level = paused ? 0 : s_recording_levels[column];
        if (level > 1000) level = 1000;
        int half = 2 + (int)(level * (unsigned)layout->waveform_half_height / 1000u);
        int x = 22 + column * 8;
        fill_rect_solid(x, layout->waveform_center_y - half, 5, half * 2 + 1,
                        paused ? muted : cyan);
    }
    char level_label[20];
    snprintf(level_label, sizeof(level_label), "MIC %u%%",
             (unsigned)(s_recording_smoothed_level / 10u));
    draw_ascii_centered(layout->microphone_label_y, level_label, paused ? muted : cyan, bg);
    draw_text24_centered(layout->instruction_y,
                         s_recording_mode ? layout->meeting_stop_instruction : "说完后自动处理",
                         color(163,188,207), bg, layout->instruction_scale);
    finish_screen_frame(composed);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void legacy_display_scene_set_audio_level(uint16_t level, uint32_t elapsed) {
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
    const compact_recording_layout_t *layout = RECORDING_LAYOUT;
    bool composed = begin_screen_frame();
    if (composed) {
        memcpy(s_render_target, s_framebuffers[s_front_frame], LCD_FRAME_BYTES);
    }
    fill_rect_solid(18, layout->waveform_clear_top, 204,
                    layout->waveform_clear_height, bg);
    fill_rect_solid(20, layout->waveform_center_y, 200, 1, muted);
    for (int column = 0; column < 24; ++column) {
        uint16_t history_level = s_recording_paused ? 0 : s_recording_levels[column];
        if (history_level > 1000) history_level = 1000;
        int half = 2 + (int)(history_level * (unsigned)layout->waveform_half_height / 1000u);
        int x = 22 + column * 8;
        fill_rect_solid(x, layout->waveform_center_y - half, 5, half * 2 + 1,
                        s_recording_paused ? muted : cyan);
    }
    char level_label[20];
    snprintf(level_label, sizeof(level_label), "MIC %u%%",
             (unsigned)(s_recording_smoothed_level / 10u));
    draw_ascii_centered(layout->microphone_label_y, level_label,
                        s_recording_paused ? muted : cyan, bg);
    finish_screen_frame(composed);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void legacy_display_scene_show_text(const char *title, const char *text) {
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
void legacy_display_scene_show_upload_progress(size_t done, size_t total, const char *stage) {
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
    const compact_upload_layout_t *layout = UPLOAD_LAYOUT;
    draw_text24_centered(layout->title_y, "会议录音", color(255,255,255), bg,
                         layout->title_scale);
    draw_text24_centered(layout->stage_y, visible_stage, color(170,215,235), bg,
                         layout->stage_scale);
    draw_progress_bar(24, layout->progress_y, 192, 18, percent,
                      color(28,80,111), color(72,205,220));
    char label[16]; snprintf(label, sizeof(label), "%u%%", percent);
    draw_ascii_centered(layout->percent_y, label, color(255,255,255), bg);
    // Eight full-width glyphs fit within the 240 px panel. Keep this warning
    // as one semantic line so punctuation can never be stranded at line start.
    draw_text24_centered(layout->warning_y, "上传中，请勿断电",
                         color(150,195,215), bg, layout->warning_scale);
    finish_screen_frame(composed);
    ESP_LOGI(TAG,"upload %u/%u %s",(unsigned)done,(unsigned)total,stage?stage:"");
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void legacy_display_scene_show_response(const char *title, const char *text) {
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
s_response_next_page_us = RESPONSE_LAYOUT->automatic_page_interval_us > 0 &&
                               response_page_count() > 1
                                  ? esp_timer_get_time() +
                                        RESPONSE_LAYOUT->automatic_page_interval_us
                                  : 0;
    draw_response_page();
    ESP_LOGI(TAG, "response pages=%u: %s", response_page_count(), s_response_text);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void legacy_display_scene_set_alarm_visual(bool active, unsigned frame, const char *time_text,
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
    const compact_alarm_layout_t *layout = ALARM_LAYOUT;
    fill_rect_solid(16, layout->accent_y, 208, 4, accent);
    draw_text24_centered(layout->title_y, "闹钟响铃", color(244, 249, 252), bg,
                         layout->title_scale);
    draw_ascii_centered(layout->time_y,
                        time_text && strlen(time_text) >= 16 ? time_text + 11 : "--:--",
                        accent, bg);
    if (label && label[0]) {
        draw_text24_centered(layout->label_y, label, color(221, 234, 242), bg,
                             layout->label_scale);
    }
    char attempt_text[28];
    snprintf(attempt_text, sizeof(attempt_text), "%u / %u", attempt, max_attempts);
    draw_ascii_centered(layout->attempt_y, attempt_text, color(145, 177, 197), bg);
    draw_text24_centered(layout->instruction_y, "按激活键停止",
                         color(145, 177, 197), bg, layout->instruction_scale);
    finish_screen_frame(composed);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void legacy_display_scene_show_response_image(const char *title, const char *caption,
                                    const uint16_t *pixels, size_t width, size_t height) {
    if (!pixels || width < 1 || width > 64 || height < 1 || height > 64) return;
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_thinking_surface_visible = false;
    s_foreground_surface = true;
    s_response_active = true;
    s_response_image_active = true;
    s_response_page = 0;
s_response_next_page_us = 0;
    uint16_t bg = color(8, 17, 28), header = color(14, 31, 47);
    uint16_t accent = color(76, 168, 207), ink = color(244, 248, 251);
    uint16_t muted = color(174, 198, 215);
    bool composed = begin_screen_frame();
    fill_screen(bg);
    const int image_header_h = RESPONSE_LAYOUT->header_height;
    fill_rect_solid(0, 0, LCD_WIDTH, image_header_h, header);
    fill_rect_solid(RESPONSE_TEXT_X,
                    RESPONSE_LAYOUT->image_accent_y,
                    RESPONSE_LAYOUT->image_accent_width,
                    RESPONSE_LAYOUT->image_accent_height,
                    accent);
    draw_text24_clipped(RESPONSE_TEXT_X + RESPONSE_LAYOUT->image_title_x_offset,
                        RESPONSE_LAYOUT->image_title_y,
                        title && title[0] ? title : "码卡龙", ink, header, 8);
    fill_rect_solid(RESPONSE_TEXT_X, image_header_h - 1, RESPONSE_TEXT_WIDTH, 1,
                    color(31, 62, 82));

    // Scale small gateway thumbnails to a useful reading size with nearest-
    // neighbour sampling. It preserves icons, QR-like art and screenshots and
    // avoids wasting most of this compact display on empty background.
    int content_top = image_header_h + 8;
    int content_bottom = caption && caption[0] ? RESPONSE_LAYOUT->image_caption_bottom
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
        scaled = allocate_temporary_display_bitmap(
            (size_t)shown_w * shown_h * sizeof(uint16_t));
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
    const esp_err_t image_draw_err = draw_bitmap_sync(
        image_x, image_y, image_x + shown_w, image_y + shown_h, shown_pixels);
    free_temporary_display_bitmap(scaled);
    if (image_draw_err != ESP_OK) {
        ESP_LOGW(TAG, "response image transfer failed: %s",
                 esp_err_to_name(image_draw_err));
    }
    if (caption && caption[0]) {
        draw_text24_centered(RESPONSE_LAYOUT->image_caption_y, caption, muted, bg, 8);
    }
    fill_rect_solid(0, RESPONSE_FOOTER_Y, LCD_WIDTH,
                    LCD_HEIGHT - RESPONSE_FOOTER_Y, color(11, 24, 38));
    draw_text24_clipped(RESPONSE_TEXT_X,
                        RESPONSE_LAYOUT->footer_hint_y,
                        "激活键返回", muted,
                        color(11, 24, 38), 5);
    finish_screen_frame(composed);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}

bool legacy_display_scene_navigate_response(int page_delta) {
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
        /* A board without normalized page keys advances through the profile
         * layout's timed-paging path.  This is an input capability, not a
         * board identity: future compact hardware can expose either paging
         * affordance without copying the response state machine. */
        if (!compact_input_service_response_paging_uses_volume_keys()) {
            (void)page_delta;
            xSemaphoreGiveRecursive(s_lcd_mutex);
            return true;
        }
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
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
    return true;
}

bool legacy_display_scene_get_response_page(unsigned *page) {
    if (!page || !s_lcd_mutex) return false;
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    bool active = s_response_active;
    if (active) *page = s_response_image_active ? 0u : s_response_page;
    xSemaphoreGiveRecursive(s_lcd_mutex);
    return active;
}

bool legacy_display_scene_restore_response_page(unsigned page) {
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
s_response_next_page_us = RESPONSE_LAYOUT->automatic_page_interval_us > 0 &&
                                   pages > 1
                                      ? esp_timer_get_time() +
                                            RESPONSE_LAYOUT->automatic_page_interval_us
                                      : 0;
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
    return true;
}
int legacy_display_scene_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]) {
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
void legacy_display_scene_show_qrcode_modules(const uint8_t *modules, size_t module_count,
                                   const char *ssid) {
    if (!modules || module_count == 0 || module_count > 177) return;
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    wake_display_for_draw_locked();
    s_thinking_surface_visible = false;
    s_foreground_surface = true;
    s_response_active = false;
    int module_size = (int)module_count;
    int scale = 180 / (module_size + 8);
    if (scale < 1) { legacy_display_scene_show_text("setup", ssid); xSemaphoreGiveRecursive(s_lcd_mutex); return; }
    int side = (module_size + 8) * scale;
    uint16_t *qr = allocate_temporary_display_bitmap(
        (size_t)side * side * sizeof(uint16_t));
    if (!qr) { legacy_display_scene_show_text("setup", ssid); xSemaphoreGiveRecursive(s_lcd_mutex); return; }
    for (int y = 0; y < side; ++y) for (int x = 0; x < side; ++x) {
        int mx = x / scale - 4, my = y / scale - 4;
        bool dark = mx >= 0 && my >= 0 && mx < module_size && my < module_size &&
                    modules[(size_t)my * module_count + mx] != 0;
        qr[y * side + x] = dark ? color(0, 0, 0) : color(255, 255, 255);
    }
    fill_screen(color(255, 255, 255));
    int qr_y = (LCD_HEIGHT - side) / 2 - 20;
    draw_bitmap_sync((LCD_WIDTH - side) / 2, qr_y, (LCD_WIDTH + side) / 2, qr_y + side, qr);
    free_temporary_display_bitmap(qr);
    draw_ascii_centered(LCD_HEIGHT - 42, "WIFI SETUP", color(0, 0, 0), color(255, 255, 255));
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void legacy_display_scene_show_ready_prompt(const char *title, const char *text) {
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
void legacy_display_scene_cancel_ready_prompt(void) {}
// Board-owned DISPLAY_OFF transaction shared by Bread Compact and Fangtang.
// It is deliberately display-only: active system services continue until a
// future power coordinator has proven a board-specific MCU sleep transaction.
esp_err_t legacy_display_scene_enter_display_off(void) {
    if (!s_lcd_mutex || !compact_display_service_ready()) return ESP_ERR_INVALID_STATE;
    if (xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) return ESP_ERR_TIMEOUT;
    // Reject a stale timer expiry that races a new foreground render.  The
    // Power Service owns timing; this adapter remains the only authority for
    // deciding whether its actual scene can safely lose panel/backlight.
    bool ambient = !strcmp(s_state, "idle") || !strcmp(s_state, "quiet");
    if (s_display_sleeping || !ambient || s_foreground_surface ||
        s_recording_active || s_response_active || s_alarm_visual_active) {
        ESP_LOGW(TAG,
                 "DISPLAY_OFF rejected: sleeping=%d ambient=%d foreground=%d recording=%d response=%d alarm=%d state=%s",
                 s_display_sleeping, ambient, s_foreground_surface,
                 s_recording_active, s_response_active, s_alarm_visual_active,
                 s_state);
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    esp_err_t err = compact_display_service_enter_display_off();
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "DISPLAY_OFF entry transaction failed: %s", esp_err_to_name(err));
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return err;
    }
    s_display_sleeping = true;
    // DISP OFF makes the panel's GRAM retention an implementation detail.
    // Force the shared delta presenter to refresh the entire composed screen
    // before it treats a framebuffer as the physical front image again.
    s_front_frame_valid = false;
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
    if (!s_lcd_mutex) return ESP_ERR_INVALID_STATE;
    if (xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    if (!s_display_sleeping) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    wake_display_for_draw_locked();
    if (s_display_sleeping) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return ESP_FAIL;
    }
    strlcpy(s_state, "idle", sizeof(s_state));
    s_response_active = false;
    s_foreground_surface = false;
    show_state_screen(s_state);
    xSemaphoreGiveRecursive(s_lcd_mutex);
    ESP_LOGI(TAG, "ambient display awakened");
    return ESP_OK;
}
void legacy_display_scene_set_wifi_status(const char *ssid, bool connected) {
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

void legacy_display_scene_set_service_ready(bool ready) {
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
void legacy_display_scene_set_ambient(const char *time, const char *location, const char *date, const char *weekday,
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

void legacy_display_scene_set_alarm_scheduled(bool scheduled) {
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

void legacy_connectivity_transport_set_network_transport(bool cellular) {
    if (!s_lcd_mutex) return;
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    const bool changed = compact_visual_profile_publish_network_transport(cellular);
    if (changed && !s_display_sleeping && !s_recording_active &&
        !s_foreground_surface &&
        (!strcmp(s_state, "idle") || !strcmp(s_state, "quiet"))) {
        show_state_screen(s_state);
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
