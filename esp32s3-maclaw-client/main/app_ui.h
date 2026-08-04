#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "qrcode.h"

// Hardware-independent UI state.  Application code changes this model; the
// selected board port only translates the model into pixels for its panel.
typedef enum {
    APP_UI_SURFACE_PET = 0,
    APP_UI_SURFACE_RECORDING,
    APP_UI_SURFACE_MESSAGE,
    APP_UI_SURFACE_UPLOAD,
    APP_UI_SURFACE_RESPONSE,
    APP_UI_SURFACE_SETUP,
} app_ui_surface_t;

typedef struct {
    app_ui_surface_t surface;
    char pet_state[16];
    bool recording_active;
    bool meeting_recording;
    bool recording_paused;
    uint32_t elapsed_seconds;
    uint16_t audio_level;
    uint16_t audio_history[24];
    uint32_t audio_history_revision;
    bool wifi_connected;
    bool command_display_locked;
    char ambient_time[16];
    char ambient_location[24];
    char ambient_date[24];
    char ambient_weekday[24];
    char ambient_weather[32];
    int ambient_temperature_c;
    bool ambient_weather_valid;
    bool ambient_weather_stale;
} app_ui_model_t;

void app_ui_init(void);
app_ui_model_t app_ui_snapshot(void);

void app_ui_set_pet_state(const char *state);
void app_ui_set_command_display_lock(bool locked);
void app_ui_set_command_cancel_enabled(bool enabled);
void app_ui_set_pet_profile(const char *skin, bool motion_enabled);
void app_ui_set_recording_mode(bool meeting);
void app_ui_set_recording_visual(bool active, bool paused, uint32_t elapsed_seconds);
void app_ui_set_audio_level(uint16_t level, uint32_t elapsed_seconds);
void app_ui_push_recording_pcm(const int16_t *samples, size_t count);
void app_ui_show_text(const char *title, const char *text);
void app_ui_show_upload_progress(size_t completed_bytes, size_t total_bytes,
                                 const char *stage);
void app_ui_show_response(const char *title, const char *text);
bool app_ui_navigate_response(int page_delta);
// Closes a completed response and restores the ambient pet surface. Returns
// false when the current surface is not a response, so the same primary input
// can retain its normal start-recording behavior elsewhere.
bool app_ui_dismiss_response(void);
int app_ui_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]);
void app_ui_show_qrcode(esp_qrcode_handle_t qrcode, const char *ssid);
void app_ui_show_ready_prompt(const char *title, const char *text);
void app_ui_cancel_ready_prompt(void);
bool app_ui_wake_from_idle(void);
void app_ui_set_wifi_status(const char *ssid, bool connected);
void app_ui_set_ambient(const char *time, const char *location, const char *date,
                        const char *weekday, const char *weather_summary,
                        int temperature_c, bool weather_valid, bool weather_stale);
