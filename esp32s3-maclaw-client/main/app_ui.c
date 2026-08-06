#include "app_ui.h"

#include <stdlib.h>
#include <string.h>

#include "board_port.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

static app_ui_model_t s_model;
static portMUX_TYPE s_model_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_upload_progress_valid;
static unsigned s_upload_progress_percent;
static char s_upload_progress_stage[32];

typedef enum {
    APP_UI_REPLAY_PET = 0,
    APP_UI_REPLAY_STARTUP,
    APP_UI_REPLAY_RECORDING,
    APP_UI_REPLAY_MESSAGE,
    APP_UI_REPLAY_UPLOAD,
    APP_UI_REPLAY_RESPONSE_TEXT,
    APP_UI_REPLAY_RESPONSE_IMAGE,
    APP_UI_REPLAY_SETUP_QR,
    APP_UI_REPLAY_READY_PROMPT,
} app_ui_replay_kind_t;

typedef struct {
    app_ui_replay_kind_t kind;
    char title[64];
    char text[2048];
    char stage[32];
    char ssid[64];
    size_t completed_bytes;
    size_t total_bytes;
    size_t width;
    size_t height;
    size_t qr_module_count;
    unsigned response_page;
    uint16_t *image_pixels;
    uint8_t *qr_modules;
} app_ui_replay_state_t;

static app_ui_replay_state_t s_replay;
static StaticSemaphore_t s_replay_mutex_storage;
static SemaphoreHandle_t s_replay_mutex;

static void replay_lock(void) {
    if (s_replay_mutex) xSemaphoreTakeRecursive(s_replay_mutex, portMAX_DELAY);
}

static void replay_unlock(void) {
    if (s_replay_mutex) xSemaphoreGiveRecursive(s_replay_mutex);
}

static void replay_release_dynamic_locked(void) {
    free(s_replay.image_pixels);
    free(s_replay.qr_modules);
    s_replay.image_pixels = NULL;
    s_replay.qr_modules = NULL;
    s_replay.width = 0;
    s_replay.height = 0;
    s_replay.qr_module_count = 0;
}

static void replay_begin_locked(app_ui_replay_kind_t kind) {
    replay_release_dynamic_locked();
    s_replay.kind = kind;
    s_replay.title[0] = '\0';
    s_replay.text[0] = '\0';
    s_replay.stage[0] = '\0';
    s_replay.ssid[0] = '\0';
    s_replay.completed_bytes = 0;
    s_replay.total_bytes = 0;
    s_replay.response_page = 0;
}

static void replay_render_locked(void) {
    app_ui_model_t model = app_ui_snapshot();
    switch (s_replay.kind) {
        case APP_UI_REPLAY_STARTUP:
            board_port_set_command_display_lock(true);
            board_port_show_startup_screen();
            break;
        case APP_UI_REPLAY_RECORDING:
            board_port_set_command_display_lock(model.command_display_locked);
            board_port_set_recording_mode(model.meeting_recording);
            board_port_set_recording_visual(model.recording_active,
                                            model.recording_paused,
                                            model.elapsed_seconds);
            // A replay restores the already composed recording scene after an
            // alarm or other foreground owner releases the LCD.  It is not a
            // new 512-sample audio block.  Feeding a cached level through
            // the board here advanced the 24-column history once more and
            // applied another smoothing pass, which made the waveform jump by
            // one bar immediately after the foreground transition.  The next
            // real capture block owns that update, exactly as on Bread Compact.
            break;
        case APP_UI_REPLAY_MESSAGE:
            board_port_set_command_display_lock(model.command_display_locked);
            board_port_show_text(s_replay.title, s_replay.text);
            break;
        case APP_UI_REPLAY_UPLOAD:
            board_port_set_command_display_lock(model.command_display_locked);
            board_port_show_upload_progress(s_replay.completed_bytes,
                                            s_replay.total_bytes,
                                            s_replay.stage);
            break;
        case APP_UI_REPLAY_RESPONSE_TEXT:
            board_port_set_command_display_lock(model.command_display_locked);
            board_port_show_response(s_replay.title, s_replay.text);
            (void)board_port_restore_response_page(s_replay.response_page);
            break;
        case APP_UI_REPLAY_RESPONSE_IMAGE:
            board_port_set_command_display_lock(model.command_display_locked);
            board_port_show_response_image(s_replay.title, s_replay.text,
                                           s_replay.image_pixels,
                                           s_replay.width, s_replay.height);
            break;
        case APP_UI_REPLAY_SETUP_QR:
            board_port_set_command_display_lock(model.command_display_locked);
            board_port_show_qrcode_matrix(s_replay.qr_modules,
                                          s_replay.qr_module_count,
                                          s_replay.ssid);
            break;
        case APP_UI_REPLAY_READY_PROMPT:
            board_port_set_command_display_lock(false);
            board_port_show_ready_prompt(s_replay.title, s_replay.text);
            break;
        case APP_UI_REPLAY_PET:
        default:
            board_port_set_command_display_lock(model.command_display_locked);
            board_port_set_command_stage(model.command_stage);
            board_port_set_command_cancel_enabled(model.command_cancel_enabled);
            board_port_set_pet_state(model.pet_state);
            break;
    }
}

static void stop_recording_if_needed(void) {
    bool was_recording;
    bool alarm_active;
    taskENTER_CRITICAL(&s_model_lock);
    was_recording = s_model.recording_active;
    s_model.recording_active = false;
    s_model.recording_paused = false;
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (was_recording && !alarm_active) board_port_set_recording_visual(false, false, 0);
}

void app_ui_init(void) {
    if (!s_replay_mutex) {
        s_replay_mutex = xSemaphoreCreateRecursiveMutexStatic(&s_replay_mutex_storage);
    }
    replay_lock();
    replay_release_dynamic_locked();
    memset(&s_replay, 0, sizeof(s_replay));
    s_replay.kind = APP_UI_REPLAY_PET;
    taskENTER_CRITICAL(&s_model_lock);
    memset(&s_model, 0, sizeof(s_model));
    s_model.surface = APP_UI_SURFACE_PET;
    strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
    strlcpy(s_model.command_stage, "正在处理", sizeof(s_model.command_stage));
    taskEXIT_CRITICAL(&s_model_lock);
    replay_unlock();
}

app_ui_model_t app_ui_snapshot(void) {
    app_ui_model_t copy;
    taskENTER_CRITICAL(&s_model_lock);
    copy = s_model;
    if (copy.alarm_visual_active) copy.surface = APP_UI_SURFACE_ALARM;
    taskEXIT_CRITICAL(&s_model_lock);
    return copy;
}

void app_ui_show_startup_screen(void) {
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_STARTUP;
    s_model.command_display_locked = true;
    strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
    s_upload_progress_valid = false;
    s_upload_progress_stage[0] = '\0';
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(APP_UI_REPLAY_STARTUP);
    if (alarm_active) {
        replay_unlock();
        return;
    }
    board_port_set_command_display_lock(true);
    board_port_show_startup_screen();
    replay_unlock();
}

void app_ui_set_pet_state(const char *state) {
    bool recording;
    bool suppress_ambient;
    bool alarm_active;
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    const char *next_state = state ? state : "idle";
    suppress_ambient = s_model.command_display_locked &&
                       (!strcmp(next_state, "idle") || !strcmp(next_state, "quiet"));
    if (!suppress_ambient) {
        strlcpy(s_model.pet_state, next_state, sizeof(s_model.pet_state));
    }
    recording = s_model.recording_active;
    if (!recording && !suppress_ambient) s_model.surface = APP_UI_SURFACE_PET;
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (!recording && !suppress_ambient) replay_begin_locked(APP_UI_REPLAY_PET);
    // During recording the requested pet state is retained in the shared model
    // and becomes visible when the recorder closes. It cannot overwrite the
    // waveform midway through a capture.
    if (!recording && !suppress_ambient && !alarm_active) board_port_set_pet_state(state);
    replay_unlock();
}

void app_ui_set_command_stage(const char *stage) {
    bool alarm_active;
    taskENTER_CRITICAL(&s_model_lock);
    strlcpy(s_model.command_stage, stage && stage[0] ? stage : "正在处理",
            sizeof(s_model.command_stage));
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (!alarm_active) board_port_set_command_stage(stage);
}

void app_ui_set_command_display_lock(bool locked) {
    bool alarm_active;
    taskENTER_CRITICAL(&s_model_lock);
    s_model.command_display_locked = locked;
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (!alarm_active) board_port_set_command_display_lock(locked);
}

void app_ui_set_command_cancel_enabled(bool enabled) {
    bool alarm_active;
    taskENTER_CRITICAL(&s_model_lock);
    s_model.command_cancel_enabled = enabled;
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (!alarm_active) board_port_set_command_cancel_enabled(enabled);
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
    bool alarm_active;
    taskENTER_CRITICAL(&s_model_lock);
    s_model.meeting_recording = meeting;
    s_model.elapsed_seconds = 0;
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (!alarm_active) board_port_set_recording_mode(meeting);
}

void app_ui_set_recording_visual(bool active, bool paused, uint32_t elapsed_seconds) {
    char next_pet[sizeof(s_model.pet_state)];
    bool meeting;
    bool command_locked;
    bool alarm_active;
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.recording_active = active;
    s_model.recording_paused = active && paused;
    s_model.elapsed_seconds = active ? elapsed_seconds : 0;
    // Ending capture is only an intermediate step in a voice command.  Keep
    // the shared model on its foreground surface while the worker swaps the
    // waveform for "uploading/thinking" or a result; otherwise a delayed
    // app_ui_set_pet_state() can publish an ambient frame in that gap.
    command_locked = s_model.command_display_locked;
    if (active) s_model.surface = APP_UI_SURFACE_RECORDING;
    else if (!command_locked) s_model.surface = APP_UI_SURFACE_PET;
    meeting = s_model.meeting_recording;
    alarm_active = s_model.alarm_visual_active;
    strlcpy(next_pet, s_model.pet_state, sizeof(next_pet));
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(active ? APP_UI_REPLAY_RECORDING : APP_UI_REPLAY_PET);

    if (alarm_active) {
        replay_unlock();
        return;
    }

    // Always re-assert the mode before rendering. This makes mode and visual a
    // single shared transition even when different tasks updated the UI.
    board_port_set_recording_mode(meeting);
    board_port_set_recording_visual(active, paused, elapsed_seconds);
    if (!active && !command_locked) board_port_set_pet_state(next_pet);
    replay_unlock();
}

void app_ui_set_audio_level(uint16_t level, uint32_t elapsed_seconds) {
    bool active;
    bool alarm_active;
    taskENTER_CRITICAL(&s_model_lock);
    active = s_model.recording_active;
    if (active) {
        if (level > 1000) level = 1000;
        // The app model owns state transitions only.  The physical board is
        // the sole owner of Bread Compact's smoothing and 24-column history.
    }
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (active && !alarm_active) board_port_set_audio_level(level, elapsed_seconds);
}

void app_ui_push_recording_pcm(const int16_t *samples, size_t count) {
    // PCM belongs to recording/upload. The visual history is advanced by
    // app_ui_set_audio_level(), matching Bread's level-driven waveform.
    (void)samples;
    (void)count;
}

void app_ui_show_text(const char *title, const char *text) {
    stop_recording_if_needed();
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_MESSAGE;
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(APP_UI_REPLAY_MESSAGE);
    strlcpy(s_replay.title, title ? title : "", sizeof(s_replay.title));
    strlcpy(s_replay.text, text ? text : "", sizeof(s_replay.text));
    if (!alarm_active) board_port_show_text(title, text);
    replay_unlock();
}

void app_ui_show_upload_progress(size_t completed_bytes, size_t total_bytes,
                                 const char *stage) {
    stop_recording_if_needed();
    replay_lock();
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
    bool alarm_active = s_model.alarm_visual_active;
    s_upload_progress_valid = true;
    s_upload_progress_percent = percent;
    strlcpy(s_upload_progress_stage, visible_stage, sizeof(s_upload_progress_stage));
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(APP_UI_REPLAY_UPLOAD);
    s_replay.completed_bytes = completed_bytes;
    s_replay.total_bytes = total_bytes;
    strlcpy(s_replay.stage, visible_stage, sizeof(s_replay.stage));
    if (!unchanged && !alarm_active) {
        board_port_show_upload_progress(completed_bytes, total_bytes, stage);
    }
    replay_unlock();
}

void app_ui_show_response(const char *title, const char *text) {
    stop_recording_if_needed();
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_RESPONSE;
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(APP_UI_REPLAY_RESPONSE_TEXT);
    strlcpy(s_replay.title, title ? title : "", sizeof(s_replay.title));
    strlcpy(s_replay.text, text ? text : "", sizeof(s_replay.text));
    if (!alarm_active) board_port_show_response(title, text);
    replay_unlock();
}

void app_ui_show_response_image(const char *title, const char *caption,
                                const uint16_t *pixels, size_t width, size_t height) {
    stop_recording_if_needed();
    if (!pixels || width < 1 || width > 64 || height < 1 || height > 64) return;
    replay_lock();
    size_t pixel_count = width * height;
    uint16_t *owned_pixels = malloc(pixel_count * sizeof(*owned_pixels));
    if (!owned_pixels) {
        replay_unlock();
        return;
    }
    memcpy(owned_pixels, pixels, pixel_count * sizeof(*owned_pixels));
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_RESPONSE;
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(APP_UI_REPLAY_RESPONSE_IMAGE);
    s_replay.image_pixels = owned_pixels;
    s_replay.width = width;
    s_replay.height = height;
    strlcpy(s_replay.title, title ? title : "", sizeof(s_replay.title));
    strlcpy(s_replay.text, caption ? caption : "", sizeof(s_replay.text));
    if (!alarm_active) board_port_show_response_image(title, caption, pixels, width, height);
    replay_unlock();
}

bool app_ui_navigate_response(int page_delta) {
    bool response_visible;
    bool alarm_active;
    bool handled = false;
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    response_visible = s_model.surface == APP_UI_SURFACE_RESPONSE &&
                       !s_model.recording_active;
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (response_visible && alarm_active) {
        // Alarm owns input while it rings; keep the interrupted page stable.
        replay_unlock();
        return true;
    }
    if (response_visible) handled = board_port_navigate_response(page_delta);
#if CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD || CONFIG_MACLAW_BOARD_FANGTANG_4G
    // The compact LCD HAL is the source of truth for a reply that is visibly
    // on the panel. Bread uses this for its physical page keys; Fangtang also
    // needs it so its always-running six-second pager survives a late
    // model-only state update that raced the outgoing-result draw.
    if (!handled) handled = board_port_navigate_response(page_delta);
#endif
    if (handled && s_replay.kind == APP_UI_REPLAY_RESPONSE_TEXT) {
        unsigned page = 0;
        if (board_port_get_response_page(&page)) s_replay.response_page = page;
    }
    replay_unlock();
    return handled;
}

bool app_ui_dismiss_response(void) {
    char pet_state[sizeof(s_model.pet_state)];
    bool response_visible;
    bool alarm_active;
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    response_visible = s_model.surface == APP_UI_SURFACE_RESPONSE &&
                       !s_model.recording_active;
    if (response_visible) {
        s_model.surface = APP_UI_SURFACE_PET;
        strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
        s_model.command_display_locked = false;
    }
    strlcpy(pet_state, s_model.pet_state, sizeof(pet_state));
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (response_visible) replay_begin_locked(APP_UI_REPLAY_PET);
    if (response_visible && !alarm_active) {
        // Release the HAL's foreground guard before requesting the ambient
        // repaint. EchoEar keeps response_active as a stale-frame barrier;
        // Bread Compact uses the same lock to reject late idle updates.
        board_port_set_command_display_lock(false);
        board_port_set_pet_state(pet_state);
    }
    replay_unlock();
    return response_visible;
}

void app_ui_restore_standby(void) {
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_PET;
    strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
    s_model.recording_active = false;
    s_model.recording_paused = false;
    s_model.meeting_recording = false;
    s_model.elapsed_seconds = 0;
    s_model.command_display_locked = false;
    s_model.command_cancel_enabled = false;
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(APP_UI_REPLAY_PET);

    if (alarm_active) {
        replay_unlock();
        return;
    }

    // Publish the HAL transition in ownership order: first remove the command
    // guards, then paint idle. Both board ports reject stale ambient frames
    // while the guard is set, so reversing this order would leave the cancel
    // message visible even though the application model already says PET.
    board_port_set_command_cancel_enabled(false);
    board_port_set_command_display_lock(false);
    board_port_set_recording_mode(false);
    board_port_set_pet_state("idle");
    replay_unlock();
}

int app_ui_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]) {
    return board_port_cache_glyph(codepoint, bitmap);
}

void app_ui_show_qrcode(esp_qrcode_handle_t qrcode, const char *ssid) {
    if (!qrcode) return;
    int size = esp_qrcode_get_size(qrcode);
    if (size <= 0 || size > 177) return;
    size_t module_bytes = (size_t)size * size;
    uint8_t *modules = malloc(module_bytes);
    if (!modules) return;
    for (int y = 0; y < size; ++y) {
        for (int x = 0; x < size; ++x) {
            modules[(size_t)y * size + x] = esp_qrcode_get_module(qrcode, x, y) ? 1u : 0u;
        }
    }
    stop_recording_if_needed();
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_SETUP;
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(APP_UI_REPLAY_SETUP_QR);
    s_replay.qr_modules = modules;
    s_replay.qr_module_count = (size_t)size;
    strlcpy(s_replay.ssid, ssid ? ssid : "", sizeof(s_replay.ssid));
    if (!alarm_active) board_port_show_qrcode_matrix(modules, (size_t)size, ssid);
    replay_unlock();
}

void app_ui_show_ready_prompt(const char *title, const char *text) {
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    bool was_recording = s_model.recording_active;
    s_model.recording_active = false;
    s_model.recording_paused = false;
    s_model.surface = APP_UI_SURFACE_PET;
    s_model.command_display_locked = false;
    strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(APP_UI_REPLAY_READY_PROMPT);
    strlcpy(s_replay.title, title ? title : "", sizeof(s_replay.title));
    strlcpy(s_replay.text, text ? text : "", sizeof(s_replay.text));
    if (!alarm_active) {
        if (was_recording) board_port_set_recording_visual(false, false, 0);
        board_port_set_command_display_lock(false);
        board_port_show_ready_prompt(title, text);
    }
    replay_unlock();
}

void app_ui_cancel_ready_prompt(void) {
    replay_lock();
    bool alarm_active;
    if (s_replay.kind == APP_UI_REPLAY_READY_PROMPT) {
        replay_begin_locked(APP_UI_REPLAY_PET);
    }
    taskENTER_CRITICAL(&s_model_lock);
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (!alarm_active) board_port_cancel_ready_prompt();
    replay_unlock();
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
    replay_lock();
    unsigned interrupted_response_page = 0;
    bool have_interrupted_response_page = false;
    taskENTER_CRITICAL(&s_model_lock);
    bool was_active = s_model.alarm_visual_active;
    bool text_response_visible = !was_active && active &&
                                 s_model.surface == APP_UI_SURFACE_RESPONSE &&
                                 s_replay.kind == APP_UI_REPLAY_RESPONSE_TEXT;
    s_model.alarm_visual_active = active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (active) {
        if (text_response_visible) {
            have_interrupted_response_page =
                board_port_get_response_page(&interrupted_response_page);
            if (have_interrupted_response_page) {
                s_replay.response_page = interrupted_response_page;
            }
        }
        board_port_set_alarm_visual(true, frame, time_text, label, attempt, max_attempts);
        replay_unlock();
        return;
    }
    if (!was_active) {
        replay_unlock();
        return;
    }
    // Board-local alarm ownership is released without drawing an interim idle
    // page. The latest scene published while the alarm was ringing is then
    // replayed atomically, including copied image or QR payloads.
    board_port_set_alarm_visual(false, frame, time_text, label, attempt, max_attempts);
    replay_render_locked();
    replay_unlock();
}
