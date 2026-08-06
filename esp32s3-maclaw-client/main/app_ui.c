#include "app_ui.h"

#include <string.h>

#include "board_port.h"
#include "freertos/FreeRTOS.h"

static app_ui_model_t s_model;
static portMUX_TYPE s_model_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_upload_progress_valid;
static unsigned s_upload_progress_percent;
static char s_upload_progress_stage[32];

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

void app_ui_show_startup_screen(void) {
    taskENTER_CRITICAL(&s_model_lock);
    if (s_model.surface == APP_UI_SURFACE_ALARM) {
        taskEXIT_CRITICAL(&s_model_lock);
        return;
    }
    s_model.surface = APP_UI_SURFACE_STARTUP;
    s_model.command_display_locked = true;
    strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
    s_upload_progress_valid = false;
    s_upload_progress_stage[0] = '\0';
    taskEXIT_CRITICAL(&s_model_lock);
    board_port_set_command_display_lock(true);
    board_port_show_startup_screen();
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

void app_ui_set_command_stage(const char *stage) {
    board_port_set_command_stage(stage);
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
    // Board ports already separate model mutation from painting through their
    // foreground-display guard. Always apply the new profile so the first ready
    // frame is current; the startup artwork remains pixel-stable while locked.
    board_port_set_pet_profile(skin, motion_enabled);
}

esp_err_t app_ui_set_pet_asset(const uint8_t *const *frames, size_t frame_count,
                               size_t width, size_t height, uint32_t frame_ms) {
    // Install the asset now without painting over startup. Both board ports
    // defer presentation while the foreground-display guard is active.
    return board_port_set_pet_asset(frames, frame_count, width, height, frame_ms);
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
    bool command_locked;
    bool new_session;
    taskENTER_CRITICAL(&s_model_lock);
    new_session = active && !s_model.recording_active;
    s_model.recording_active = active;
    s_model.recording_paused = active && paused;
    s_model.elapsed_seconds = active ? elapsed_seconds : 0;
    // The HAL clears its Bread-compatible history at every recording-session
    // boundary. Mirror that boundary in the shared model as well: callers
    // normally publish set_recording_mode() first, but this keeps a direct
    // fresh visual transition from inheriting the previous session for one
    // snapshot frame.
    if (!active || new_session) {
        s_model.audio_level = 0;
        memset(s_model.audio_history, 0, sizeof(s_model.audio_history));
        ++s_model.audio_history_revision;
    }
    // Ending capture is only an intermediate step in a voice command.  Keep
    // the shared model on its foreground surface while the worker swaps the
    // waveform for "uploading/thinking" or a result; otherwise a delayed
    // app_ui_set_pet_state() can publish an ambient frame in that gap.
    command_locked = s_model.command_display_locked;
    if (active) s_model.surface = APP_UI_SURFACE_RECORDING;
    else if (!command_locked) s_model.surface = APP_UI_SURFACE_PET;
    meeting = s_model.meeting_recording;
    strlcpy(next_pet, s_model.pet_state, sizeof(next_pet));
    taskEXIT_CRITICAL(&s_model_lock);

    // Always re-assert the mode before rendering. This makes mode and visual a
    // single shared transition even when different tasks updated the UI.
    board_port_set_recording_mode(meeting);
    board_port_set_recording_visual(active, paused, elapsed_seconds);
    if (!active && !command_locked) board_port_set_pet_state(next_pet);
}

void app_ui_set_audio_level(uint16_t level, uint32_t elapsed_seconds) {
    bool active;
    taskENTER_CRITICAL(&s_model_lock);
    active = s_model.recording_active;
    if (active) {
        if (level > 1000) level = 1000;
        // Mirror the board's Bread-compatible smoothing for state snapshots;
        // the board still receives the raw normalized level and owns its
        // physical 24-column history.
        s_model.audio_level = level > s_model.audio_level
                                  ? (uint16_t)((s_model.audio_level + level * 3u) / 4u)
                                  : (uint16_t)((s_model.audio_level * 7u + level) / 8u);
        s_model.elapsed_seconds = elapsed_seconds;
        memmove(&s_model.audio_history[0], &s_model.audio_history[1],
                (sizeof(s_model.audio_history) / sizeof(s_model.audio_history[0]) - 1) *
                    sizeof(s_model.audio_history[0]));
        s_model.audio_history[sizeof(s_model.audio_history) /
                              sizeof(s_model.audio_history[0]) - 1] = s_model.audio_level;
        ++s_model.audio_history_revision;
    }
    taskEXIT_CRITICAL(&s_model_lock);
    if (active) board_port_set_audio_level(level, elapsed_seconds);
}

void app_ui_push_recording_pcm(const int16_t *samples, size_t count) {
    // PCM belongs to recording/upload. The visual history is advanced by
    // app_ui_set_audio_level(), matching Bread's level-driven waveform.
    (void)samples;
    (void)count;
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
    // Meeting recordings may approach the Hub's 512 MiB quota. Multiplying a
    // 32-bit size_t by 100 first overflows above ~41 MiB and makes the progress
    // bar jump backwards. Divide before multiplying and retain the remainder.
    unsigned percent = 0;
    if (total_bytes) {
        size_t whole = completed_bytes / total_bytes;
        size_t remainder = completed_bytes % total_bytes;
        percent = whole >= 1 ? 100
                             : (unsigned)(((uint64_t)remainder * 100u) / total_bytes);
    }
    if (percent > 100) percent = 100;
    const char *visible_stage = stage && stage[0] ? stage : "正在上传";
    taskENTER_CRITICAL(&s_model_lock);
    bool unchanged = s_model.surface == APP_UI_SURFACE_UPLOAD &&
                     s_upload_progress_valid &&
                     s_upload_progress_percent == percent &&
                     !strcmp(s_upload_progress_stage, visible_stage);
    s_model.surface = APP_UI_SURFACE_UPLOAD;
    s_upload_progress_valid = true;
    s_upload_progress_percent = percent;
    strlcpy(s_upload_progress_stage, visible_stage, sizeof(s_upload_progress_stage));
    taskEXIT_CRITICAL(&s_model_lock);
    if (unchanged) return;
    board_port_show_upload_progress(completed_bytes, total_bytes, stage);
}

void app_ui_show_response(const char *title, const char *text) {
    stop_recording_if_needed();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_RESPONSE;
    taskEXIT_CRITICAL(&s_model_lock);
    board_port_show_response(title, text);
}

void app_ui_show_response_image(const char *title, const char *caption,
                                const uint16_t *pixels, size_t width, size_t height) {
    stop_recording_if_needed();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_RESPONSE;
    taskEXIT_CRITICAL(&s_model_lock);
    board_port_show_response_image(title, caption, pixels, width, height);
}

bool app_ui_navigate_response(int page_delta) {
    bool response_visible;
    taskENTER_CRITICAL(&s_model_lock);
    response_visible = s_model.surface == APP_UI_SURFACE_RESPONSE &&
                       !s_model.recording_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (response_visible && board_port_navigate_response(page_delta)) return true;
#if CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD || CONFIG_MACLAW_BOARD_FANGTANG_4G
    // The compact LCD HAL is the source of truth for a reply that is visibly
    // on the panel. Bread uses this for its physical page keys; Fangtang also
    // needs it so its always-running six-second pager survives a late
    // model-only state update that raced the outgoing-result draw.
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

void app_ui_restore_standby(void) {
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_PET;
    strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
    s_model.recording_active = false;
    s_model.recording_paused = false;
    s_model.meeting_recording = false;
    s_model.elapsed_seconds = 0;
    s_model.audio_level = 0;
    memset(s_model.audio_history, 0, sizeof(s_model.audio_history));
    ++s_model.audio_history_revision;
    s_model.command_display_locked = false;
    taskEXIT_CRITICAL(&s_model_lock);

    // Publish the HAL transition in ownership order: first remove the command
    // guards, then paint idle. Both board ports reject stale ambient frames
    // while the guard is set, so reversing this order would leave the cancel
    // message visible even though the application model already says PET.
    board_port_set_command_cancel_enabled(false);
    board_port_set_command_display_lock(false);
    board_port_set_recording_mode(false);
    board_port_set_pet_state("idle");
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
    taskENTER_CRITICAL(&s_model_lock);
    if (s_model.surface == APP_UI_SURFACE_ALARM) {
        taskEXIT_CRITICAL(&s_model_lock);
        return;
    }
    bool was_recording = s_model.recording_active;
    s_model.recording_active = false;
    s_model.recording_paused = false;
    s_model.audio_level = 0;
    memset(s_model.audio_history, 0, sizeof(s_model.audio_history));
    ++s_model.audio_history_revision;
    s_model.surface = APP_UI_SURFACE_PET;
    s_model.command_display_locked = false;
    strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
    taskEXIT_CRITICAL(&s_model_lock);
    if (was_recording) board_port_set_recording_visual(false, false, 0);
    board_port_set_command_display_lock(false);
    board_port_show_ready_prompt(title, text);
}

void app_ui_cancel_ready_prompt(void) {
    board_port_cancel_ready_prompt();
}

bool app_ui_wake_from_idle(void) {
    return board_port_wake_from_idle();
}

void app_ui_set_wifi_status(const char *ssid, bool connected) {
    taskENTER_CRITICAL(&s_model_lock);
    s_model.wifi_connected = connected;
    taskEXIT_CRITICAL(&s_model_lock);
    // Forward every state transition.  The board port deliberately defers the
    // cosmetic repaint while a response, recording, setup QR, or alarm owns
    // the screen, but must retain the transport state for the next standby
    // composition (the same ownership contract used for service readiness).
    board_port_set_wifi_status(ssid, connected);
}

void app_ui_set_service_ready(bool ready) {
    taskENTER_CRITICAL(&s_model_lock);
    s_model.service_ready = ready;
    taskEXIT_CRITICAL(&s_model_lock);
    // Always forward the model mutation. The board port defers repainting when
    // a command/setup/alarm owns the display, but must still remember an outage
    // that occurred behind that foreground surface.
    board_port_set_service_ready(ready);
}

void app_ui_set_ambient(const char *time, const char *location, const char *date,
                        const char *weekday, const char *weather_summary,
                        int temperature_c, bool weather_valid, bool weather_stale) {
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
    taskEXIT_CRITICAL(&s_model_lock);
    // Always forward the model mutation.  The EchoEar board port has the same
    // foreground guard as Bread Compact and stores an update received behind a
    // result/upload/setup screen for the first restored standby frame.  Dropping
    // it here used to leave date/weather stale until the next server tick.
    board_port_set_ambient(time, location, date, weekday, weather_summary,
                           temperature_c, weather_valid, weather_stale);
}

void app_ui_set_alarm_scheduled(bool scheduled) {
    taskENTER_CRITICAL(&s_model_lock);
    s_model.alarm_scheduled = scheduled;
    taskEXIT_CRITICAL(&s_model_lock);
    // The board port stores this model state even while a startup or command
    // foreground owns the LCD, then includes it in the next standby frame.
    board_port_set_alarm_scheduled(scheduled);
}

void app_ui_set_alarm_visual(bool active, unsigned frame, const char *time_text,
                             const char *label, unsigned attempt, unsigned max_attempts) {
    taskENTER_CRITICAL(&s_model_lock);
    // An alarm is an interruption, not a new command result.  When it ends,
    // both board ports must return to the same ambient-pet state regardless of
    // which foreground surface had been visible before it rang.  Clearing the
    // stale upload cache also ensures the next meeting starts with a fresh
    // progress frame rather than being suppressed as "unchanged".
    s_model.surface = active ? APP_UI_SURFACE_ALARM : APP_UI_SURFACE_PET;
    s_model.command_display_locked = active;
    if (!active) {
        strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
        s_upload_progress_valid = false;
        s_upload_progress_stage[0] = '\0';
    }
    taskEXIT_CRITICAL(&s_model_lock);
    board_port_set_alarm_visual(active, frame, time_text, label, attempt, max_attempts);
}
