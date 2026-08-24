#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "device_api.h"

// Hardware-independent UI state.  Application code changes this model; the
// selected board port only translates the model into pixels for its panel.
typedef enum {
    APP_UI_SURFACE_PET = 0,
    APP_UI_SURFACE_STARTUP,
    APP_UI_SURFACE_RECORDING,
    APP_UI_SURFACE_MESSAGE,
    APP_UI_SURFACE_UPLOAD,
    APP_UI_SURFACE_RESPONSE,
    APP_UI_SURFACE_SETUP,
    APP_UI_SURFACE_ALARM,
} app_ui_surface_t;

typedef struct {
    /* Monotonic UI-scene identity. It changes whenever the shared model
     * accepts a presentation mutation and is diagnostic/correlation metadata,
     * not a panel-present or DMA-completion counter. */
    uint32_t revision;
    app_ui_surface_t surface;
    char pet_state[16];
    char pet_skin[32];
    bool recording_active;
    bool meeting_recording;
    bool recording_paused;
    uint32_t elapsed_seconds;
    bool wifi_connected;
    char wifi_ssid[64];
    bool service_ready;
    bool command_display_locked;
    bool command_cancel_enabled;
    bool alarm_visual_active;
    char command_stage[32];
    char ambient_time[16];
    char ambient_location[24];
    char ambient_date[24];
    char ambient_weekday[24];
    char ambient_weather[32];
    int ambient_temperature_c;
    bool ambient_weather_valid;
    bool ambient_weather_stale;
    bool alarm_scheduled;
} app_ui_model_t;

/* UI-level acknowledgement for the DISPLAY_OFF idle policy.  This proves
 * only that the common UI accepted and retained the policy; scheduling a
 * future panel transition remains owned below the Power/Display HAL. */
#define APP_UI_DISPLAY_OFF_IDLE_POLICY_STATE_ABI_VERSION 2u
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    /* Policy acceptance and physical deadline admission are intentionally
     * separate facts. A foreground scene can retain a valid policy while it
     * correctly defers arming the panel-off deadline. */
    bool known;
    uint32_t idle_after_ms;
    device_status_t last_status;
    bool schedule_required;
    bool schedule_known;
    bool schedule_armed;
    device_status_t schedule_last_status;
} app_ui_display_off_idle_policy_state_t;

void app_ui_init(void);
app_ui_model_t app_ui_snapshot(void);
// Atomically accepts the idle policy used before the ambient display enters
// DISPLAY_OFF. Zero disables automatic display-off; this never requests MCU
// sleep. OK confirms the common UI accepted the policy, not that a panel has
// already transitioned to DISPLAY_OFF (that is asynchronous and profile-owned).
device_status_t app_ui_apply_display_off_idle_policy(uint32_t idle_after_ms);
bool app_ui_get_display_off_idle_policy_state(
    app_ui_display_off_idle_policy_state_t *out_state);

// Holds the board's boot artwork as an exclusive foreground surface. Ambient,
// pet-profile, and Wi-Fi updates may update their models while this is active,
// but cannot repaint the display until the ready surface is published.
void app_ui_show_startup_screen(void);
void app_ui_set_pet_state(const char *state);
void app_ui_set_command_stage(const char *stage);
void app_ui_set_command_display_lock(bool locked);
void app_ui_set_command_cancel_enabled(bool enabled);
/* These generic presentation updates are intentionally owned by the shared UI
 * model. The selected renderer decides how a profile/asset is displayed. */
void app_ui_set_pet_profile(const char *skin, bool motion_enabled);
device_status_t app_ui_set_pet_asset(const uint8_t *const *frames, size_t frame_count,
                                     size_t width, size_t height, uint32_t frame_ms);
device_status_t app_ui_set_pet_asset_consuming(uint8_t **frames, size_t frame_count,
                                               size_t width, size_t height, uint32_t frame_ms);
void app_ui_set_recording_mode(bool meeting);
void app_ui_set_recording_visual(bool active, bool paused, uint32_t elapsed_seconds);
void app_ui_set_audio_level(uint16_t level, uint32_t elapsed_seconds);
void app_ui_push_recording_pcm(const int16_t *samples, size_t count);
void app_ui_show_text(const char *title, const char *text);
void app_ui_show_upload_progress(size_t completed_bytes, size_t total_bytes,
                                 const char *stage);
void app_ui_show_response(const char *title, const char *text);
void app_ui_show_response_image(const char *title, const char *caption,
                                const uint16_t *pixels, size_t width, size_t height);
bool app_ui_navigate_response(int page_delta);
// Closes a completed response and restores the ambient pet surface. Returns
// false when the current surface is not a response, so the same primary input
// can retain its normal start-recording behavior elsewhere.
bool app_ui_dismiss_response(void);
// Unconditionally returns any foreground command surface (message, upload, or
// response) to the animated ambient pet screen. This is used by cancellation,
// where the terminal surface is a MESSAGE rather than a normal RESPONSE.
void app_ui_restore_standby(void);
/* Borrowed 72-byte 24x24 bitmap. App UI serializes the immediate display
 * submission; the caller may release the source storage after this returns. */
int app_ui_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]);
/* The UI owns a replay-safe copy of the module matrix. QR encoder handles
 * stay with the producer (for example provisioning), keeping this common
 * boundary independent of any QR encoder library. The same foreground-payload
 * rule applies to text, images and ready prompts: callers retain no display
 * buffer ownership after these calls return. */
bool app_ui_show_qrcode_modules(const uint8_t *modules, size_t module_count,
                                const char *ssid);
void app_ui_show_ready_prompt(const char *title, const char *text);
void app_ui_cancel_ready_prompt(void);
bool app_ui_wake_from_idle(void);
/* Reconciles App UI's ambient-idle bookkeeping after a successful scheduled
 * DISPLAY_OFF wake. This is not an input event and never writes a schedule
 * manual-wake override. It re-arms only an already-ambient pet surface. */
void app_ui_note_schedule_display_wake(void);
/* Applies a MaClaw GUI brightness update.  A non-zero level restores an
 * already DISPLAY_OFF ambient panel only; it never synthesizes input or
 * starts voice capture.  Zero remains a backlight-only update. */
device_status_t app_ui_apply_remote_brightness(uint8_t percent);
void app_ui_set_wifi_status(const char *ssid, bool connected);
void app_ui_set_service_ready(bool ready);
void app_ui_set_ambient(const char *time, const char *location, const char *date,
                        const char *weekday, const char *weather_summary,
                        int temperature_c, bool weather_valid, bool weather_stale);
void app_ui_set_alarm_scheduled(bool scheduled);
void app_ui_set_alarm_visual(bool active, unsigned frame, const char *time_text,
                             const char *label, unsigned attempt, unsigned max_attempts);
