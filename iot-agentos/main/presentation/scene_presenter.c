#include "presentation/scene_presenter.h"

#include "app_ui.h"

_Static_assert(SCENE_AMBIENT_TIME_CAPACITY == sizeof(((app_ui_model_t *)0)->ambient_time),
               "ambient time capacity must match App UI");
_Static_assert(SCENE_AMBIENT_LOCATION_CAPACITY == sizeof(((app_ui_model_t *)0)->ambient_location),
               "ambient location capacity must match App UI");
_Static_assert(SCENE_AMBIENT_DATE_CAPACITY == sizeof(((app_ui_model_t *)0)->ambient_date),
               "ambient date capacity must match App UI");
_Static_assert(SCENE_AMBIENT_WEEKDAY_CAPACITY == sizeof(((app_ui_model_t *)0)->ambient_weekday),
               "ambient weekday capacity must match App UI");
_Static_assert(SCENE_AMBIENT_WEATHER_CAPACITY == sizeof(((app_ui_model_t *)0)->ambient_weather),
               "ambient weather capacity must match App UI");
_Static_assert(SCENE_NETWORK_SSID_CAPACITY == sizeof(((app_ui_model_t *)0)->wifi_ssid),
               "network ssid capacity must match App UI");
_Static_assert(SCENE_PET_STATE_CAPACITY == sizeof(((app_ui_model_t *)0)->pet_state),
               "pet state capacity must match App UI");
_Static_assert(SCENE_PET_SKIN_CAPACITY == sizeof(((app_ui_model_t *)0)->pet_skin),
               "pet skin capacity must match App UI");
_Static_assert(SCENE_COMMAND_STAGE_CAPACITY == sizeof(((app_ui_model_t *)0)->command_stage),
               "command stage capacity must match App UI");

device_status_t scene_presenter_init(void) {
    return DEVICE_STATUS_OK;
}

void scene_presenter_publish_ambient(const scene_ambient_fields_t *fields) {
    if (!fields) return;
    app_ui_set_ambient(fields->time,
                       fields->location,
                       fields->date,
                       fields->weekday,
                       fields->weather,
                       fields->temperature_c,
                       fields->weather_valid,
                       fields->weather_stale);
}

void scene_presenter_publish_network(const scene_network_fields_t *fields) {
    if (!fields) return;
    app_ui_set_wifi_status(fields->ssid, fields->connected);
}

void scene_presenter_publish_alarm_scheduled(bool scheduled) {
    app_ui_set_alarm_scheduled(scheduled);
}

void scene_presenter_publish_pet_state(const char *state) {
    app_ui_set_pet_state(state);
}

void scene_presenter_publish_pet_profile(const char *skin, bool motion_enabled) {
    app_ui_set_pet_profile(skin, motion_enabled);
}

device_status_t scene_presenter_set_pet_asset(const uint8_t *const *frames,
                                              size_t frame_count, size_t width,
                                              size_t height, uint32_t frame_ms) {
    return app_ui_set_pet_asset(frames, frame_count, width, height, frame_ms);
}

device_status_t scene_presenter_set_pet_asset_consuming(uint8_t **frames,
                                                        size_t frame_count, size_t width,
                                                        size_t height, uint32_t frame_ms) {
    return app_ui_set_pet_asset_consuming(frames, frame_count, width, height, frame_ms);
}

int scene_presenter_cache_glyph(uint32_t codepoint,
                                const uint8_t bitmap[SCENE_GLYPH_BITMAP_BYTES]) {
    if (!bitmap) return 0;
    return app_ui_cache_glyph(codepoint, bitmap);
}

void scene_presenter_publish_command_stage(const char *stage) {
    app_ui_set_command_stage(stage);
}

void scene_presenter_publish_recording_mode(bool meeting) {
    app_ui_set_recording_mode(meeting);
}

void scene_presenter_publish_recording_visual(bool active, bool paused,
                                              uint32_t elapsed_seconds) {
    app_ui_set_recording_visual(active, paused, elapsed_seconds);
}

void scene_presenter_publish_audio_level(uint16_t level, uint32_t elapsed_seconds) {
    app_ui_set_audio_level(level, elapsed_seconds);
}

void scene_presenter_push_recording_pcm(const int16_t *samples, size_t count) {
    app_ui_push_recording_pcm(samples, count);
}

void scene_presenter_publish_alarm_visual(bool active, unsigned frame,
                                          const char *time_text, const char *label,
                                          unsigned attempt, unsigned max_attempts) {
    app_ui_set_alarm_visual(active, frame, time_text, label, attempt, max_attempts);
}

void scene_presenter_publish_message(const char *title, const char *text) {
    app_ui_show_text(title, text);
}

void scene_presenter_publish_response(const char *title, const char *text) {
    app_ui_show_response(title, text);
}

void scene_presenter_publish_response_image(const char *title, const char *caption,
                                            const uint16_t *pixels, size_t width,
                                            size_t height) {
    app_ui_show_response_image(title, caption, pixels, width, height);
}

void scene_presenter_publish_upload_progress(size_t completed_bytes, size_t total_bytes,
                                             const char *stage) {
    app_ui_show_upload_progress(completed_bytes, total_bytes, stage);
}

void scene_presenter_publish_ready_prompt(const char *title, const char *text) {
    app_ui_show_ready_prompt(title, text);
}

void scene_presenter_cancel_ready_prompt(void) {
    app_ui_cancel_ready_prompt();
}

bool scene_presenter_publish_setup_qr(const uint8_t *modules, size_t module_count,
                                      const char *ssid) {
    return app_ui_show_qrcode_modules(modules, module_count, ssid);
}

void scene_presenter_publish_startup_splash(void) {
    app_ui_show_startup_screen();
}

void scene_presenter_publish_command_display_lock(bool locked) {
    app_ui_set_command_display_lock(locked);
}

void scene_presenter_publish_command_cancel_enabled(bool enabled) {
    app_ui_set_command_cancel_enabled(enabled);
}

bool scene_presenter_navigate_response(int page_delta) {
    return app_ui_navigate_response(page_delta);
}

bool scene_presenter_dismiss_response(void) {
    return app_ui_dismiss_response();
}

void scene_presenter_restore_standby(void) {
    app_ui_restore_standby();
}

bool scene_presenter_wake_from_idle(void) {
    return app_ui_wake_from_idle();
}

void scene_presenter_publish_service_ready(bool ready) {
    app_ui_set_service_ready(ready);
}

device_status_t scene_presenter_apply_display_off_idle_policy(uint32_t idle_after_ms) {
    return app_ui_apply_display_off_idle_policy(idle_after_ms);
}

bool scene_presenter_get_display_off_idle_policy_state(
    scene_display_off_idle_policy_state_t *out_state) {
    if (!out_state) return false;
    app_ui_display_off_idle_policy_state_t ui_state = {0};
    if (!app_ui_get_display_off_idle_policy_state(&ui_state) ||
        ui_state.struct_size != sizeof(ui_state) ||
        ui_state.abi_version != APP_UI_DISPLAY_OFF_IDLE_POLICY_STATE_ABI_VERSION) {
        return false;
    }
    *out_state = (scene_display_off_idle_policy_state_t){
        .struct_size = sizeof(*out_state),
        .abi_version = SCENE_DISPLAY_OFF_IDLE_POLICY_STATE_ABI_VERSION,
        .known = ui_state.known,
        .idle_after_ms = ui_state.idle_after_ms,
        .last_status = ui_state.last_status,
        .schedule_required = ui_state.schedule_required,
        .schedule_known = ui_state.schedule_known,
        .schedule_armed = ui_state.schedule_armed,
        .schedule_last_status = ui_state.schedule_last_status,
    };
    return true;
}

device_status_t scene_presenter_apply_remote_brightness(uint8_t percent) {
    return app_ui_apply_remote_brightness(percent);
}

void scene_presenter_note_schedule_display_wake(void) {
    app_ui_note_schedule_display_wake();
}
