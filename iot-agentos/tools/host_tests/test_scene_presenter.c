/*
 * Host test of the shipped Scene Presenter ambient path (A7 second increment).
 *
 * Compiles the real presentation/scene_presenter.c against a capturing
 * app_ui_set_ambient stub.  Proves:
 *   - scene_kind numeric identity matches Foreground Coordinator
 *   - NULL publish is a no-op
 *   - publish_ambient forwards every field without rewriting values
 */

#include <stdio.h>
#include <string.h>
#include <stdbool.h>
#include <stdint.h>

#include "app_ui.h"
#include "presentation/scene_model.h"
#include "presentation/scene_presenter.h"
#include "services/foreground_coordinator.h"
#include "services/ambient_service.h"
#include "weather_cache_service.h"

_Static_assert((int)SCENE_KIND_AMBIENT == (int)FOREGROUND_SCENE_AMBIENT, "AMBIENT");
_Static_assert((int)SCENE_KIND_STARTUP_SPLASH == (int)FOREGROUND_SCENE_STARTUP_SPLASH, "STARTUP");
_Static_assert((int)SCENE_KIND_SETUP_PORTAL == (int)FOREGROUND_SCENE_SETUP_PORTAL, "SETUP");
_Static_assert((int)SCENE_KIND_VOICE_COMMAND == (int)FOREGROUND_SCENE_VOICE_COMMAND, "VOICE");
_Static_assert((int)SCENE_KIND_COMMAND_MESSAGE == (int)FOREGROUND_SCENE_COMMAND_MESSAGE, "MESSAGE");
_Static_assert((int)SCENE_KIND_COMMAND_RESULT == (int)FOREGROUND_SCENE_COMMAND_RESULT, "RESULT");
_Static_assert((int)SCENE_KIND_MEETING_RECORD == (int)FOREGROUND_SCENE_MEETING_RECORD, "MEETING_RECORD");
_Static_assert((int)SCENE_KIND_MEETING_UPLOAD == (int)FOREGROUND_SCENE_MEETING_UPLOAD, "MEETING_UPLOAD");
_Static_assert((int)SCENE_KIND_MEETING_RESULT == (int)FOREGROUND_SCENE_MEETING_RESULT, "MEETING_RESULT");
_Static_assert((int)SCENE_KIND_ALARM_RING == (int)FOREGROUND_SCENE_ALARM_RING, "ALARM");
_Static_assert(SCENE_AMBIENT_TIME_CAPACITY == sizeof(((app_ui_model_t *)0)->ambient_time), "time cap");
_Static_assert(SCENE_AMBIENT_LOCATION_CAPACITY == sizeof(((app_ui_model_t *)0)->ambient_location), "loc cap");
_Static_assert(SCENE_AMBIENT_DATE_CAPACITY == sizeof(((app_ui_model_t *)0)->ambient_date), "date cap");
_Static_assert(SCENE_AMBIENT_WEEKDAY_CAPACITY == sizeof(((app_ui_model_t *)0)->ambient_weekday), "weekday cap");
_Static_assert(SCENE_AMBIENT_WEATHER_CAPACITY == sizeof(((app_ui_model_t *)0)->ambient_weather), "weather cap");
_Static_assert(WEATHER_CACHE_SUMMARY_CAPACITY <= SCENE_AMBIENT_WEATHER_CAPACITY, "summary fits scene");
_Static_assert(WEATHER_CACHE_LOCATION_CAPACITY <= SCENE_AMBIENT_LOCATION_CAPACITY, "location fits scene");

typedef struct {
    int calls;
    char time[SCENE_AMBIENT_TIME_CAPACITY];
    char location[SCENE_AMBIENT_LOCATION_CAPACITY];
    char date[SCENE_AMBIENT_DATE_CAPACITY];
    char weekday[SCENE_AMBIENT_WEEKDAY_CAPACITY];
    char weather[SCENE_AMBIENT_WEATHER_CAPACITY];
    int temperature_c;
    bool weather_valid;
    bool weather_stale;
} captured_ambient_t;

static captured_ambient_t s_captured;
static int s_network_calls;
static char s_network_ssid[SCENE_NETWORK_SSID_CAPACITY];
static bool s_network_connected;
static int s_scheduled_calls;
static bool s_alarm_scheduled;
static int s_pet_state_calls;
static char s_pet_state[SCENE_PET_STATE_CAPACITY];
static bool s_pet_state_was_null;
static int s_pet_profile_calls;
static char s_pet_skin[SCENE_PET_SKIN_CAPACITY];
static bool s_pet_motion;
static bool s_pet_skin_was_null;
static int s_glyph_calls;
static uint32_t s_glyph_codepoint;
static uint8_t s_glyph_bitmap[SCENE_GLYPH_BITMAP_BYTES];
static int s_stage_calls;
static char s_command_stage[SCENE_COMMAND_STAGE_CAPACITY];
static bool s_stage_was_null;
static int s_rec_mode_calls;
static bool s_rec_meeting;
static int s_rec_visual_calls;
static bool s_rec_active;
static bool s_rec_paused;
static uint32_t s_rec_elapsed;
static int s_alarm_visual_calls;
static bool s_alarm_visual_active;
static unsigned s_alarm_frame;
static char s_alarm_time[SCENE_ALARM_TIME_CAPACITY];
static char s_alarm_label[SCENE_ALARM_LABEL_CAPACITY];
static unsigned s_alarm_attempt;
static unsigned s_alarm_max_attempts;
static bool s_alarm_time_was_null;
static bool s_alarm_label_was_null;
static int s_message_calls;
static char s_message_title[SCENE_MESSAGE_TITLE_CAPACITY];
static char s_message_body[128];
static bool s_message_title_was_null;
static bool s_message_body_was_null;
static int s_response_calls;
static char s_response_title[SCENE_MESSAGE_TITLE_CAPACITY];
static char s_response_body[128];
static int s_response_image_calls;
static size_t s_response_width;
static size_t s_response_height;
static const uint16_t *s_response_pixels;
static int s_upload_calls;
static size_t s_upload_done;
static size_t s_upload_total;
static char s_upload_stage[SCENE_UPLOAD_STAGE_CAPACITY];
static bool s_upload_stage_was_null;
static int s_ready_calls;
static char s_ready_title[SCENE_MESSAGE_TITLE_CAPACITY];
static int s_ready_cancel_calls;
static int s_qr_calls;
static size_t s_qr_count;
static char s_qr_ssid[SCENE_NETWORK_SSID_CAPACITY];
static int s_splash_calls;
static int s_lock_calls;
static bool s_command_locked;
static int s_cancel_calls;
static bool s_command_cancel_enabled;
static int s_navigate_calls;
static int s_navigate_delta;
static bool s_navigate_result;
static int s_dismiss_calls;
static bool s_dismiss_result;
static int s_restore_calls;
static int s_wake_calls;
static bool s_wake_result;
static int s_level_calls;
static uint16_t s_level;
static uint32_t s_level_elapsed;
static int s_pcm_calls;
static const int16_t *s_pcm_samples;
static size_t s_pcm_count;
static int s_pet_asset_calls;
static const uint8_t *const *s_pet_asset_frames;
static size_t s_pet_asset_count;
static size_t s_pet_asset_width;
static size_t s_pet_asset_height;
static uint32_t s_pet_asset_frame_ms;
static device_status_t s_pet_asset_status;
static int s_pet_asset_consuming_calls;
static uint8_t **s_pet_asset_consuming_frames;
static size_t s_pet_asset_consuming_count;
static device_status_t s_pet_asset_consuming_status;
static int s_service_ready_calls;
static bool s_service_ready;
static int s_idle_policy_calls;
static uint32_t s_idle_policy_ms;
static device_status_t s_idle_policy_status;
static bool s_idle_policy_ack_valid;
static app_ui_display_off_idle_policy_state_t s_idle_policy_ack;
static int s_brightness_calls;
static uint8_t s_brightness_percent;
static device_status_t s_brightness_status;
static int s_schedule_wake_calls;

_Static_assert(SCENE_NETWORK_SSID_CAPACITY == sizeof(((app_ui_model_t *)0)->wifi_ssid),
               "network ssid cap");
_Static_assert(SCENE_PET_STATE_CAPACITY == sizeof(((app_ui_model_t *)0)->pet_state),
               "pet state cap");
_Static_assert(SCENE_PET_SKIN_CAPACITY == sizeof(((app_ui_model_t *)0)->pet_skin),
               "pet skin cap");
_Static_assert(SCENE_COMMAND_STAGE_CAPACITY == sizeof(((app_ui_model_t *)0)->command_stage),
               "command stage cap");

void app_ui_set_ambient(const char *time, const char *location, const char *date,
                        const char *weekday, const char *weather_summary,
                        int temperature_c, bool weather_valid, bool weather_stale) {
    s_captured.calls += 1;
    snprintf(s_captured.time, sizeof(s_captured.time), "%s", time ? time : "");
    snprintf(s_captured.location, sizeof(s_captured.location), "%s", location ? location : "");
    snprintf(s_captured.date, sizeof(s_captured.date), "%s", date ? date : "");
    snprintf(s_captured.weekday, sizeof(s_captured.weekday), "%s", weekday ? weekday : "");
    snprintf(s_captured.weather, sizeof(s_captured.weather), "%s", weather_summary ? weather_summary : "");
    s_captured.temperature_c = temperature_c;
    s_captured.weather_valid = weather_valid;
    s_captured.weather_stale = weather_stale;
}

void app_ui_set_wifi_status(const char *ssid, bool connected) {
    s_network_calls += 1;
    snprintf(s_network_ssid, sizeof(s_network_ssid), "%s", ssid ? ssid : "");
    s_network_connected = connected;
}

void app_ui_set_alarm_scheduled(bool scheduled) {
    s_scheduled_calls += 1;
    s_alarm_scheduled = scheduled;
}

void app_ui_set_pet_state(const char *state) {
    s_pet_state_calls += 1;
    s_pet_state_was_null = (state == NULL);
    snprintf(s_pet_state, sizeof(s_pet_state), "%s", state ? state : "");
}

void app_ui_set_pet_profile(const char *skin, bool motion_enabled) {
    s_pet_profile_calls += 1;
    s_pet_skin_was_null = (skin == NULL);
    snprintf(s_pet_skin, sizeof(s_pet_skin), "%s", skin ? skin : "");
    s_pet_motion = motion_enabled;
}

int app_ui_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]) {
    s_glyph_calls += 1;
    s_glyph_codepoint = codepoint;
    if (bitmap) memcpy(s_glyph_bitmap, bitmap, sizeof(s_glyph_bitmap));
    return 1;
}

void app_ui_set_command_stage(const char *stage) {
    s_stage_calls += 1;
    s_stage_was_null = (stage == NULL);
    snprintf(s_command_stage, sizeof(s_command_stage), "%s", stage ? stage : "");
}

void app_ui_set_recording_mode(bool meeting) {
    s_rec_mode_calls += 1;
    s_rec_meeting = meeting;
}

void app_ui_set_recording_visual(bool active, bool paused, uint32_t elapsed_seconds) {
    s_rec_visual_calls += 1;
    s_rec_active = active;
    s_rec_paused = paused;
    s_rec_elapsed = elapsed_seconds;
}

void app_ui_set_alarm_visual(bool active, unsigned frame, const char *time_text,
                             const char *label, unsigned attempt, unsigned max_attempts) {
    s_alarm_visual_calls += 1;
    s_alarm_visual_active = active;
    s_alarm_frame = frame;
    s_alarm_time_was_null = (time_text == NULL);
    s_alarm_label_was_null = (label == NULL);
    snprintf(s_alarm_time, sizeof(s_alarm_time), "%s", time_text ? time_text : "");
    snprintf(s_alarm_label, sizeof(s_alarm_label), "%s", label ? label : "");
    s_alarm_attempt = attempt;
    s_alarm_max_attempts = max_attempts;
}

void app_ui_show_text(const char *title, const char *text) {
    s_message_calls += 1;
    s_message_title_was_null = (title == NULL);
    s_message_body_was_null = (text == NULL);
    snprintf(s_message_title, sizeof(s_message_title), "%s", title ? title : "");
    snprintf(s_message_body, sizeof(s_message_body), "%s", text ? text : "");
}

void app_ui_show_response(const char *title, const char *text) {
    s_response_calls += 1;
    snprintf(s_response_title, sizeof(s_response_title), "%s", title ? title : "");
    snprintf(s_response_body, sizeof(s_response_body), "%s", text ? text : "");
}

void app_ui_show_response_image(const char *title, const char *caption,
                                const uint16_t *pixels, size_t width, size_t height) {
    (void)title;
    (void)caption;
    s_response_image_calls += 1;
    s_response_pixels = pixels;
    s_response_width = width;
    s_response_height = height;
}

void app_ui_show_upload_progress(size_t completed_bytes, size_t total_bytes,
                                 const char *stage) {
    s_upload_calls += 1;
    s_upload_done = completed_bytes;
    s_upload_total = total_bytes;
    s_upload_stage_was_null = (stage == NULL);
    snprintf(s_upload_stage, sizeof(s_upload_stage), "%s", stage ? stage : "");
}

void app_ui_show_ready_prompt(const char *title, const char *text) {
    (void)text;
    s_ready_calls += 1;
    snprintf(s_ready_title, sizeof(s_ready_title), "%s", title ? title : "");
}

void app_ui_cancel_ready_prompt(void) {
    s_ready_cancel_calls += 1;
}

bool app_ui_show_qrcode_modules(const uint8_t *modules, size_t module_count,
                                const char *ssid) {
    s_qr_calls += 1;
    s_qr_count = module_count;
    snprintf(s_qr_ssid, sizeof(s_qr_ssid), "%s", ssid ? ssid : "");
    return modules != NULL;
}

void app_ui_show_startup_screen(void) {
    s_splash_calls += 1;
}

void app_ui_set_command_display_lock(bool locked) {
    s_lock_calls += 1;
    s_command_locked = locked;
}

void app_ui_set_command_cancel_enabled(bool enabled) {
    s_cancel_calls += 1;
    s_command_cancel_enabled = enabled;
}

bool app_ui_navigate_response(int page_delta) {
    s_navigate_calls += 1;
    s_navigate_delta = page_delta;
    return s_navigate_result;
}

bool app_ui_dismiss_response(void) {
    s_dismiss_calls += 1;
    return s_dismiss_result;
}

void app_ui_restore_standby(void) {
    s_restore_calls += 1;
}

bool app_ui_wake_from_idle(void) {
    s_wake_calls += 1;
    return s_wake_result;
}

void app_ui_set_audio_level(uint16_t level, uint32_t elapsed_seconds) {
    s_level_calls += 1;
    s_level = level;
    s_level_elapsed = elapsed_seconds;
}

void app_ui_push_recording_pcm(const int16_t *samples, size_t count) {
    s_pcm_calls += 1;
    s_pcm_samples = samples;
    s_pcm_count = count;
}

device_status_t app_ui_set_pet_asset(const uint8_t *const *frames, size_t frame_count,
                                     size_t width, size_t height, uint32_t frame_ms) {
    s_pet_asset_calls += 1;
    s_pet_asset_frames = frames;
    s_pet_asset_count = frame_count;
    s_pet_asset_width = width;
    s_pet_asset_height = height;
    s_pet_asset_frame_ms = frame_ms;
    return s_pet_asset_status;
}

device_status_t app_ui_set_pet_asset_consuming(uint8_t **frames, size_t frame_count,
                                               size_t width, size_t height, uint32_t frame_ms) {
    (void)width;
    (void)height;
    (void)frame_ms;
    s_pet_asset_consuming_calls += 1;
    s_pet_asset_consuming_frames = frames;
    s_pet_asset_consuming_count = frame_count;
    return s_pet_asset_consuming_status;
}

void app_ui_set_service_ready(bool ready) {
    s_service_ready_calls += 1;
    s_service_ready = ready;
}

device_status_t app_ui_apply_display_off_idle_policy(uint32_t idle_after_ms) {
    s_idle_policy_calls += 1;
    s_idle_policy_ms = idle_after_ms;
    return s_idle_policy_status;
}

bool app_ui_get_display_off_idle_policy_state(
    app_ui_display_off_idle_policy_state_t *out_state) {
    if (!out_state || !s_idle_policy_ack_valid) return false;
    *out_state = s_idle_policy_ack;
    return true;
}

device_status_t app_ui_apply_remote_brightness(uint8_t percent) {
    s_brightness_calls += 1;
    s_brightness_percent = percent;
    return s_brightness_status;
}

void app_ui_note_schedule_display_wake(void) {
    s_schedule_wake_calls += 1;
}

static int fail(const char *msg) {
    fprintf(stderr, "FAIL: %s\n", msg);
    return 1;
}

int main(void) {
    if (scene_presenter_init() != DEVICE_STATUS_OK) return fail("init");

    memset(&s_captured, 0, sizeof(s_captured));
    scene_presenter_publish_ambient(NULL);
    if (s_captured.calls != 0) return fail("NULL publish must not touch App UI");

    scene_ambient_fields_t fields;
    memset(&fields, 0, sizeof(fields));
    snprintf(fields.time, sizeof(fields.time), "12:34:56");
    snprintf(fields.location, sizeof(fields.location), "Beijing");
    snprintf(fields.date, sizeof(fields.date), "08/20");
    snprintf(fields.weekday, sizeof(fields.weekday), "Thursday");
    snprintf(fields.weather, sizeof(fields.weather), "Cloudy");
    fields.temperature_c = 27;
    fields.weather_valid = true;
    fields.weather_stale = false;

    scene_presenter_publish_ambient(&fields);
    if (s_captured.calls != 1) return fail("expected one App UI publish");
    if (strcmp(s_captured.time, fields.time) != 0) return fail("time rewritten");
    if (strcmp(s_captured.location, fields.location) != 0) return fail("location rewritten");
    if (strcmp(s_captured.date, fields.date) != 0) return fail("date rewritten");
    if (strcmp(s_captured.weekday, fields.weekday) != 0) return fail("weekday rewritten");
    if (strcmp(s_captured.weather, fields.weather) != 0) return fail("weather rewritten");
    if (s_captured.temperature_c != fields.temperature_c) return fail("temperature rewritten");
    if (s_captured.weather_valid != fields.weather_valid) return fail("valid flag rewritten");
    if (s_captured.weather_stale != fields.weather_stale) return fail("stale flag rewritten");

    fields.weather_stale = true;
    fields.weather_valid = true;
    fields.temperature_c = -3;
    scene_presenter_publish_ambient(&fields);
    if (s_captured.calls != 2) return fail("expected second publish");
    if (!s_captured.weather_stale) return fail("stale=true not forwarded");
    if (s_captured.temperature_c != -3) return fail("negative temperature not forwarded");

    memset(&fields, 0, sizeof(fields));
    fields.weather_valid = false;
    fields.weather_stale = false;
    scene_presenter_publish_ambient(&fields);
    if (s_captured.calls != 3) return fail("expected empty-field publish");
    if (s_captured.time[0] || s_captured.location[0] || s_captured.weather[0]) {
        return fail("empty strings rewritten");
    }
    if (s_captured.weather_valid || s_captured.weather_stale) {
        return fail("false flags rewritten");
    }
    if (s_captured.temperature_c != 0) return fail("zero temperature rewritten");

    scene_presenter_publish_network(NULL);
    if (s_network_calls != 0) return fail("NULL network publish must not touch App UI");
    scene_network_fields_t network = {0};
    snprintf(network.ssid, sizeof(network.ssid), "4G");
    network.connected = true;
    scene_presenter_publish_network(&network);
    if (s_network_calls != 1) return fail("expected one network publish");
    if (strcmp(s_network_ssid, "4G") != 0) return fail("network ssid rewritten");
    if (!s_network_connected) return fail("network connected not forwarded");

    scene_presenter_publish_alarm_scheduled(true);
    if (s_scheduled_calls != 1 || !s_alarm_scheduled) return fail("scheduled=true not forwarded");
    scene_presenter_publish_alarm_scheduled(false);
    if (s_scheduled_calls != 2 || s_alarm_scheduled) return fail("scheduled=false not forwarded");

    scene_presenter_publish_pet_state("speaking");
    if (s_pet_state_calls != 1) return fail("expected one pet_state publish");
    if (strcmp(s_pet_state, "speaking") != 0) return fail("pet_state rewritten");
    scene_presenter_publish_pet_state(NULL);
    if (s_pet_state_calls != 2 || !s_pet_state_was_null) {
        return fail("NULL pet_state must be forwarded for App UI idle contract");
    }

    scene_presenter_publish_pet_profile("panda", false);
    if (s_pet_profile_calls != 1) return fail("expected one pet_profile publish");
    if (strcmp(s_pet_skin, "panda") != 0) return fail("pet skin rewritten");
    if (s_pet_motion) return fail("pet motion not forwarded");
    scene_presenter_publish_pet_profile(NULL, true);
    if (s_pet_profile_calls != 2 || !s_pet_skin_was_null || !s_pet_motion) {
        return fail("NULL pet skin / motion=true not forwarded");
    }

    uint8_t glyph[SCENE_GLYPH_BITMAP_BYTES];
    memset(glyph, 0xA5, sizeof(glyph));
    if (scene_presenter_cache_glyph(0x4E2D, NULL) != 0 || s_glyph_calls != 0) {
        return fail("NULL glyph bitmap must be a no-op");
    }
    if (scene_presenter_cache_glyph(0x4E2D, glyph) != 1) return fail("glyph cache result rewritten");
    if (s_glyph_calls != 1 || s_glyph_codepoint != 0x4E2D) return fail("glyph codepoint rewritten");
    if (memcmp(s_glyph_bitmap, glyph, sizeof(glyph)) != 0) return fail("glyph bitmap rewritten");

    scene_presenter_publish_command_stage("正在上传语音");
    if (s_stage_calls != 1 || strcmp(s_command_stage, "正在上传语音") != 0) {
        return fail("command stage rewritten");
    }
    scene_presenter_publish_command_stage(NULL);
    if (s_stage_calls != 2 || !s_stage_was_null) return fail("NULL command stage not forwarded");

    scene_presenter_publish_recording_mode(true);
    if (s_rec_mode_calls != 1 || !s_rec_meeting) return fail("recording mode not forwarded");
    scene_presenter_publish_recording_visual(true, true, 12);
    if (s_rec_visual_calls != 1 || !s_rec_active || !s_rec_paused || s_rec_elapsed != 12) {
        return fail("recording visual rewritten");
    }

    scene_presenter_publish_alarm_visual(true, 3, "07:30", "起床", 2, 3);
    if (s_alarm_visual_calls != 1 || !s_alarm_visual_active || s_alarm_frame != 3) {
        return fail("alarm visual not forwarded");
    }
    if (strcmp(s_alarm_time, "07:30") != 0 || strcmp(s_alarm_label, "起床") != 0) {
        return fail("alarm visual text rewritten");
    }
    if (s_alarm_attempt != 2 || s_alarm_max_attempts != 3) return fail("alarm visual attempts rewritten");
    scene_presenter_publish_alarm_visual(false, 0, NULL, NULL, 1, 3);
    if (s_alarm_visual_calls != 2 || s_alarm_visual_active) return fail("alarm visual clear not forwarded");
    if (!s_alarm_time_was_null || !s_alarm_label_was_null) {
        return fail("NULL alarm visual text must be forwarded");
    }

    scene_presenter_publish_message("已取消", "本次操作已停止");
    if (s_message_calls != 1 || strcmp(s_message_title, "已取消") != 0 ||
        strcmp(s_message_body, "本次操作已停止") != 0) {
        return fail("message rewritten");
    }
    scene_presenter_publish_message(NULL, NULL);
    if (s_message_calls != 2 || !s_message_title_was_null || !s_message_body_was_null) {
        return fail("NULL message not forwarded");
    }

    scene_presenter_publish_response("码卡龙", "hello");
    if (s_response_calls != 1 || strcmp(s_response_title, "码卡龙") != 0 ||
        strcmp(s_response_body, "hello") != 0) {
        return fail("response rewritten");
    }
    uint16_t pixel = 0xF800;
    scene_presenter_publish_response_image("码卡龙", "cap", &pixel, 1, 1);
    if (s_response_image_calls != 1 || s_response_pixels != &pixel ||
        s_response_width != 1 || s_response_height != 1) {
        return fail("response image rewritten");
    }

    scene_presenter_publish_upload_progress(10, 40, "正在上传录音");
    if (s_upload_calls != 1 || s_upload_done != 10 || s_upload_total != 40 ||
        strcmp(s_upload_stage, "正在上传录音") != 0) {
        return fail("upload progress rewritten");
    }
    scene_presenter_publish_upload_progress(0, 1, NULL);
    if (s_upload_calls != 2 || !s_upload_stage_was_null) return fail("NULL upload stage not forwarded");

    scene_presenter_publish_ready_prompt("设备已就绪", "ok");
    if (s_ready_calls != 1 || strcmp(s_ready_title, "设备已就绪") != 0) {
        return fail("ready prompt rewritten");
    }
    scene_presenter_cancel_ready_prompt();
    if (s_ready_cancel_calls != 1) return fail("ready cancel not forwarded");

    uint8_t qr[4] = {1, 0, 0, 1};
    if (!scene_presenter_publish_setup_qr(qr, 4, "Maclaw-AP") || s_qr_calls != 1 ||
        s_qr_count != 4 || strcmp(s_qr_ssid, "Maclaw-AP") != 0) {
        return fail("setup QR rewritten");
    }

    scene_presenter_publish_startup_splash();
    if (s_splash_calls != 1) return fail("startup splash not forwarded");
    scene_presenter_publish_command_display_lock(true);
    if (s_lock_calls != 1 || !s_command_locked) return fail("display lock=true not forwarded");
    scene_presenter_publish_command_display_lock(false);
    if (s_lock_calls != 2 || s_command_locked) return fail("display lock=false not forwarded");
    scene_presenter_publish_command_cancel_enabled(true);
    if (s_cancel_calls != 1 || !s_command_cancel_enabled) {
        return fail("cancel enabled=true not forwarded");
    }
    scene_presenter_publish_command_cancel_enabled(false);
    if (s_cancel_calls != 2 || s_command_cancel_enabled) {
        return fail("cancel enabled=false not forwarded");
    }

    s_navigate_result = true;
    if (!scene_presenter_navigate_response(1) || s_navigate_calls != 1 ||
        s_navigate_delta != 1) {
        return fail("navigate +1 not forwarded");
    }
    s_navigate_result = false;
    if (scene_presenter_navigate_response(-1) || s_navigate_calls != 2 ||
        s_navigate_delta != -1) {
        return fail("navigate -1 / false not forwarded");
    }

    s_dismiss_result = true;
    if (!scene_presenter_dismiss_response() || s_dismiss_calls != 1) {
        return fail("dismiss true not forwarded");
    }
    s_dismiss_result = false;
    if (scene_presenter_dismiss_response() || s_dismiss_calls != 2) {
        return fail("dismiss false not forwarded");
    }

    scene_presenter_restore_standby();
    if (s_restore_calls != 1) return fail("restore_standby not forwarded");

    s_wake_result = true;
    if (!scene_presenter_wake_from_idle() || s_wake_calls != 1) {
        return fail("wake true not forwarded");
    }
    s_wake_result = false;
    if (scene_presenter_wake_from_idle() || s_wake_calls != 2) {
        return fail("wake false not forwarded");
    }

    scene_presenter_publish_audio_level(42, 7);
    if (s_level_calls != 1 || s_level != 42 || s_level_elapsed != 7) {
        return fail("audio level not forwarded");
    }
    scene_presenter_publish_audio_level(2000, 0);
    if (s_level_calls != 2 || s_level != 2000) {
        return fail("unclamped audio level not forwarded");
    }
    int16_t pcm[2] = {1, -1};
    scene_presenter_push_recording_pcm(pcm, 2);
    if (s_pcm_calls != 1 || s_pcm_samples != pcm || s_pcm_count != 2) {
        return fail("recording PCM rewritten");
    }
    scene_presenter_push_recording_pcm(NULL, 0);
    if (s_pcm_calls != 2 || s_pcm_samples != NULL || s_pcm_count != 0) {
        return fail("NULL recording PCM not forwarded");
    }

    s_pet_asset_status = DEVICE_STATUS_OK;
    if (scene_presenter_set_pet_asset(NULL, 0, 0, 0, 0) != DEVICE_STATUS_OK ||
        s_pet_asset_calls != 1 || s_pet_asset_frames != NULL || s_pet_asset_count != 0) {
        return fail("NULL pet asset clear not forwarded");
    }
    const uint8_t frame0[2] = {0x11, 0x22};
    const uint8_t *borrowed[1] = {frame0};
    s_pet_asset_status = DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (scene_presenter_set_pet_asset(borrowed, 1, 64, 64, 120) !=
            DEVICE_STATUS_RESOURCE_EXHAUSTED ||
        s_pet_asset_calls != 2 || s_pet_asset_frames != borrowed ||
        s_pet_asset_count != 1 || s_pet_asset_width != 64 || s_pet_asset_height != 64 ||
        s_pet_asset_frame_ms != 120) {
        return fail("borrowed pet asset rewritten");
    }
    uint8_t *consuming[2] = {(uint8_t *)frame0, NULL};
    s_pet_asset_consuming_status = DEVICE_STATUS_OK;
    if (scene_presenter_set_pet_asset_consuming(consuming, 2, 32, 32, 80) !=
            DEVICE_STATUS_OK ||
        s_pet_asset_consuming_calls != 1 || s_pet_asset_consuming_frames != consuming ||
        s_pet_asset_consuming_count != 2) {
        return fail("consuming pet asset rewritten");
    }

    scene_presenter_publish_service_ready(true);
    if (s_service_ready_calls != 1 || !s_service_ready) {
        return fail("service_ready=true not forwarded");
    }
    scene_presenter_publish_service_ready(false);
    if (s_service_ready_calls != 2 || s_service_ready) {
        return fail("service_ready=false not forwarded");
    }

    s_idle_policy_status = DEVICE_STATUS_OK;
    if (scene_presenter_apply_display_off_idle_policy(30000) != DEVICE_STATUS_OK ||
        s_idle_policy_calls != 1 || s_idle_policy_ms != 30000) {
        return fail("idle policy not forwarded");
    }
    s_idle_policy_status = DEVICE_STATUS_BUSY;
    if (scene_presenter_apply_display_off_idle_policy(0) != DEVICE_STATUS_BUSY ||
        s_idle_policy_calls != 2 || s_idle_policy_ms != 0) {
        return fail("idle policy zero/status not forwarded");
    }
    s_idle_policy_ack = (app_ui_display_off_idle_policy_state_t){
        .struct_size = sizeof(s_idle_policy_ack),
        .abi_version = APP_UI_DISPLAY_OFF_IDLE_POLICY_STATE_ABI_VERSION,
        .known = true,
        .idle_after_ms = 30000u,
        .last_status = DEVICE_STATUS_OK,
        .schedule_required = true,
        .schedule_known = true,
        .schedule_armed = true,
        .schedule_last_status = DEVICE_STATUS_OK,
    };
    s_idle_policy_ack_valid = true;
    scene_display_off_idle_policy_state_t idle_ack = {0};
    if (!scene_presenter_get_display_off_idle_policy_state(&idle_ack) ||
        idle_ack.struct_size != sizeof(idle_ack) ||
        idle_ack.abi_version != SCENE_DISPLAY_OFF_IDLE_POLICY_STATE_ABI_VERSION ||
        !idle_ack.known || idle_ack.idle_after_ms != 30000u ||
        idle_ack.last_status != DEVICE_STATUS_OK || !idle_ack.schedule_required ||
        !idle_ack.schedule_known || !idle_ack.schedule_armed ||
        idle_ack.schedule_last_status != DEVICE_STATUS_OK) {
        return fail("idle policy acknowledgement not translated");
    }
    s_idle_policy_ack.abi_version = 0u;
    if (scene_presenter_get_display_off_idle_policy_state(&idle_ack)) {
        return fail("bad UI idle acknowledgement must be rejected");
    }

    s_brightness_status = DEVICE_STATUS_OK;
    if (scene_presenter_apply_remote_brightness(40) != DEVICE_STATUS_OK ||
        s_brightness_calls != 1 || s_brightness_percent != 40) {
        return fail("remote brightness not forwarded");
    }
    s_brightness_status = DEVICE_STATUS_INVALID_ARGUMENT;
    if (scene_presenter_apply_remote_brightness(101) != DEVICE_STATUS_INVALID_ARGUMENT ||
        s_brightness_calls != 2 || s_brightness_percent != 101) {
        return fail("unclamped remote brightness / status not forwarded");
    }

    scene_presenter_note_schedule_display_wake();
    if (s_schedule_wake_calls != 1) return fail("schedule display wake not forwarded");

    uint32_t cp = 0;
    if (!ambient_service_parse_glyph_key("U+4E2D", &cp) || cp != 0x4E2D) {
        return fail("CJK glyph key not parsed");
    }
    if (ambient_service_parse_glyph_key(NULL, &cp) ||
        ambient_service_parse_glyph_key("U+4E2", &cp) ||
        ambient_service_parse_glyph_key("U+001F", &cp) ||
        ambient_service_parse_glyph_key("U+D800", &cp) ||
        ambient_service_parse_glyph_key("U+10000", &cp)) {
        return fail("invalid glyph key must be rejected");
    }

    printf("PASS scene_presenter ambient publish + scene_kind alignment\n");
    return 0;
}
