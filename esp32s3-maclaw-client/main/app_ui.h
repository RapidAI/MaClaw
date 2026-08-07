#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "qrcode.h"

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
    app_ui_surface_t surface;
    char pet_state[16];
    bool recording_active;
    bool meeting_recording;
    bool recording_paused;
    uint32_t elapsed_seconds;
    bool wifi_connected;
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

void app_ui_init(void);
app_ui_model_t app_ui_snapshot(void);

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
esp_err_t app_ui_set_pet_asset(const uint8_t *const *frames, size_t frame_count,
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
int app_ui_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]);
void app_ui_show_qrcode(esp_qrcode_handle_t qrcode, const char *ssid);
void app_ui_show_ready_prompt(const char *title, const char *text);
void app_ui_cancel_ready_prompt(void);
bool app_ui_wake_from_idle(void);
void app_ui_set_wifi_status(const char *ssid, bool connected);
void app_ui_set_service_ready(bool ready);
void app_ui_set_ambient(const char *time, const char *location, const char *date,
                        const char *weekday, const char *weather_summary,
                        int temperature_c, bool weather_valid, bool weather_stale);
void app_ui_set_alarm_scheduled(bool scheduled);
void app_ui_set_alarm_visual(bool active, unsigned frame, const char *time_text,
                             const char *label, unsigned attempt, unsigned max_attempts);
