#include "app_ui.h"

#include <string.h>

#include "board_port.h"
#include "freertos/FreeRTOS.h"

static app_ui_model_t s_model;
static portMUX_TYPE s_model_lock = portMUX_INITIALIZER_UNLOCKED;

static void stop_recording_if_needed(void) {
    bool was_recording;
    taskENTER_CRITICAL(&s_model_lock);
    was_recording = s_model.recording_active;
    s_model.recording_active = false;
    s_model.recording_paused = false;
    s_model.audio_level = 0;
    memset(s_model.audio_history, 0, sizeof(s_model.audio_history));
    ++s_model.audio_history_revision;
    taskEXIT_CRITICAL(&s_model_lock);
    if (was_recording) board_port_set_recording_visual(false, false, 0);
}

void app_ui_init(void) {
    taskENTER_CRITICAL(&s_model_lock);
    memset(&s_model, 0, sizeof(s_model));
    s_model.surface = APP_UI_SURFACE_PET;
    strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
    taskEXIT_CRITICAL(&s_model_lock);
}

app_ui_model_t app_ui_snapshot(void) {
    app_ui_model_t copy;
    taskENTER_CRITICAL(&s_model_lock);
    copy = s_model;
    taskEXIT_CRITICAL(&s_model_lock);
    return copy;
}

void app_ui_set_pet_state(const char *state) {
    bool recording;
    bool suppress_ambient;
    taskENTER_CRITICAL(&s_model_lock);
    const char *next_state = state ? state : "idle";
    suppress_ambient = s_model.command_display_locked &&
                       (!strcmp(next_state, "idle") || !strcmp(next_state, "quiet"));
    if (!suppress_ambient) {
        strlcpy(s_model.pet_state, next_state, sizeof(s_model.pet_state));
    }
    recording = s_model.recording_active;
    if (!recording && !suppress_ambient) s_model.surface = APP_UI_SURFACE_PET;
    taskEXIT_CRITICAL(&s_model_lock);
    // During recording the requested pet state is retained in the shared model
    // and becomes visible when the recorder closes. It cannot overwrite the
    // waveform midway through a capture.
    if (!recording && !suppress_ambient) board_port_set_pet_state(state);
}

void app_ui_set_command_display_lock(bool locked) {
    taskENTER_CRITICAL(&s_model_lock);
    s_model.command_display_locked = locked;
    taskEXIT_CRITICAL(&s_model_lock);
    board_port_set_command_display_lock(locked);
}

void app_ui_set_command_cancel_enabled(bool enabled) {
    board_port_set_command_cancel_enabled(enabled);
}

void app_ui_set_pet_profile(const char *skin, bool motion_enabled) {
    board_port_set_pet_profile(skin, motion_enabled);
}

void app_ui_set_recording_mode(bool meeting) {
    taskENTER_CRITICAL(&s_model_lock);
    s_model.meeting_recording = meeting;
    s_model.elapsed_seconds = 0;
    s_model.audio_level = 0;
    memset(s_model.audio_history, 0, sizeof(s_model.audio_history));
    ++s_model.audio_history_revision;
    taskEXIT_CRITICAL(&s_model_lock);
    board_port_set_recording_mode(meeting);
}

void app_ui_set_recording_visual(bool active, bool paused, uint32_t elapsed_seconds) {
    char next_pet[sizeof(s_model.pet_state)];
    bool meeting;
    taskENTER_CRITICAL(&s_model_lock);
    s_model.recording_active = active;
    s_model.recording_paused = active && paused;
    s_model.elapsed_seconds = active ? elapsed_seconds : 0;
    if (!active) s_model.audio_level = 0;
    if (!active) {
        memset(s_model.audio_history, 0, sizeof(s_model.audio_history));
        ++s_model.audio_history_revision;
    }
    s_model.surface = active ? APP_UI_SURFACE_RECORDING : APP_UI_SURFACE_PET;
    meeting = s_model.meeting_recording;
    strlcpy(next_pet, s_model.pet_state, sizeof(next_pet));
    taskEXIT_CRITICAL(&s_model_lock);

    // Always re-assert the mode before rendering. This makes mode and visual a
    // single shared transition even when different tasks updated the UI.
    board_port_set_recording_mode(meeting);
    board_port_set_recording_visual(active, paused, elapsed_seconds);
    if (!active) board_port_set_pet_state(next_pet);
}

void app_ui_set_audio_level(uint16_t level, uint32_t elapsed_seconds) {
    bool active;
    taskENTER_CRITICAL(&s_model_lock);
    active = s_model.recording_active;
    if (active) {
        s_model.audio_level = level;
        s_model.elapsed_seconds = elapsed_seconds;
    }
    taskEXIT_CRITICAL(&s_model_lock);
    if (active) board_port_set_audio_level(level, elapsed_seconds);
}

void app_ui_push_recording_pcm(const int16_t *samples, size_t count) {
    if (!samples || count == 0) return;
    uint64_t magnitude_sum = 0;
    uint32_t usable = 0;
    for (size_t i = 0; i < count; ++i) {
        int32_t sample = samples[i];
        uint32_t magnitude = sample < 0 ? (uint32_t)-sample : (uint32_t)sample;
        if (magnitude >= 32500u) continue;
        magnitude_sum += magnitude;
        ++usable;
    }
    uint32_t mean = usable ? (uint32_t)(magnitude_sum / usable) : 0;
    uint16_t level = mean <= 180u ? 0u :
                     (mean >= 9000u ? 1000u : (uint16_t)((mean - 180u) * 1000u / 8820u));
    taskENTER_CRITICAL(&s_model_lock);
    if (s_model.recording_active) {
        memmove(&s_model.audio_history[0], &s_model.audio_history[1],
                (sizeof(s_model.audio_history) / sizeof(s_model.audio_history[0]) - 1) *
                    sizeof(s_model.audio_history[0]));
        s_model.audio_history[sizeof(s_model.audio_history) /
                              sizeof(s_model.audio_history[0]) - 1] = level;
        ++s_model.audio_history_revision;
    }
    taskEXIT_CRITICAL(&s_model_lock);
    board_port_push_recording_pcm(samples, count);
}

void app_ui_show_text(const char *title, const char *text) {
    stop_recording_if_needed();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_MESSAGE;
    taskEXIT_CRITICAL(&s_model_lock);
    board_port_show_text(title, text);
}

void app_ui_show_upload_progress(size_t completed_bytes, size_t total_bytes,
                                 const char *stage) {
    stop_recording_if_needed();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_UPLOAD;
    taskEXIT_CRITICAL(&s_model_lock);
    board_port_show_upload_progress(completed_bytes, total_bytes, stage);
}

void app_ui_show_response(const char *title, const char *text) {
    stop_recording_if_needed();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_RESPONSE;
    taskEXIT_CRITICAL(&s_model_lock);
    board_port_show_response(title, text);
}

bool app_ui_navigate_response(int page_delta) {
    bool response_visible;
    taskENTER_CRITICAL(&s_model_lock);
    response_visible = s_model.surface == APP_UI_SURFACE_RESPONSE &&
                       !s_model.recording_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (response_visible && board_port_navigate_response(page_delta)) return true;
#if CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD
    // On the physical-key board the HAL is the source of truth for a reply
    // that is visibly on the LCD. This fallback also covers a late model-only
    // state update that raced the outgoing-result draw.
    return board_port_navigate_response(page_delta);
#else
    return false;
#endif
}

bool app_ui_dismiss_response(void) {
    char pet_state[sizeof(s_model.pet_state)];
    bool response_visible;
    taskENTER_CRITICAL(&s_model_lock);
    response_visible = s_model.surface == APP_UI_SURFACE_RESPONSE &&
                       !s_model.recording_active;
    if (response_visible) {
        s_model.surface = APP_UI_SURFACE_PET;
        strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
        s_model.command_display_locked = false;
    }
    strlcpy(pet_state, s_model.pet_state, sizeof(pet_state));
    taskEXIT_CRITICAL(&s_model_lock);
    if (response_visible) {
        // Release the HAL's foreground guard before requesting the ambient
        // repaint. EchoEar keeps response_active as a stale-frame barrier;
        // Bread Compact uses the same lock to reject late idle updates.
        board_port_set_command_display_lock(false);
        board_port_set_pet_state(pet_state);
    }
    return response_visible;
}

int app_ui_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]) {
    return board_port_cache_glyph(codepoint, bitmap);
}

void app_ui_show_qrcode(esp_qrcode_handle_t qrcode, const char *ssid) {
    stop_recording_if_needed();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_SETUP;
    taskEXIT_CRITICAL(&s_model_lock);
    board_port_show_qrcode(qrcode, ssid);
}

void app_ui_show_ready_prompt(const char *title, const char *text) {
    stop_recording_if_needed();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_PET;
    strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
    taskEXIT_CRITICAL(&s_model_lock);
    board_port_show_ready_prompt(title, text);
}

void app_ui_cancel_ready_prompt(void) {
    board_port_cancel_ready_prompt();
}

bool app_ui_wake_from_idle(void) {
    return board_port_wake_from_idle();
}

void app_ui_set_wifi_status(const char *ssid, bool connected) {
    bool render;
    taskENTER_CRITICAL(&s_model_lock);
    s_model.wifi_connected = connected;
    render = s_model.surface == APP_UI_SURFACE_PET && !s_model.recording_active &&
             !s_model.command_display_locked;
    taskEXIT_CRITICAL(&s_model_lock);
    if (render) board_port_set_wifi_status(ssid, connected);
}

void app_ui_set_ambient(const char *time, const char *location, const char *date,
                        const char *weekday, const char *weather_summary,
                        int temperature_c, bool weather_valid, bool weather_stale) {
    bool render;
    taskENTER_CRITICAL(&s_model_lock);
    strlcpy(s_model.ambient_time, time ? time : "", sizeof(s_model.ambient_time));
    strlcpy(s_model.ambient_location, location ? location : "", sizeof(s_model.ambient_location));
    strlcpy(s_model.ambient_date, date ? date : "", sizeof(s_model.ambient_date));
    strlcpy(s_model.ambient_weekday, weekday ? weekday : "", sizeof(s_model.ambient_weekday));
    strlcpy(s_model.ambient_weather, weather_summary ? weather_summary : "",
            sizeof(s_model.ambient_weather));
    s_model.ambient_temperature_c = temperature_c;
    s_model.ambient_weather_valid = weather_valid;
    s_model.ambient_weather_stale = weather_stale;
    render = s_model.surface == APP_UI_SURFACE_PET && !s_model.recording_active &&
             !s_model.command_display_locked;
    taskEXIT_CRITICAL(&s_model_lock);
    // Ambient data belongs to the shared model, but only the background pet
    // surface may paint it. Foreground recording/upload/reply/setup surfaces
    // keep exclusive ownership until their state transition completes.
    if (render) {
        board_port_set_ambient(time, location, date, weekday, weather_summary,
                               temperature_c, weather_valid, weather_stale);
    }
}
