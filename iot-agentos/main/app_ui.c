#include "app_ui.h"

#include <stdlib.h>
#include <string.h>

#include "device_api.h"
#include "services/foreground_coordinator.h"
#include "sleep_schedule_service.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

#define APP_UI_DEFAULT_DISPLAY_OFF_IDLE_MS (60u * 1000u)

static app_ui_model_t s_model;
static portMUX_TYPE s_model_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_upload_progress_valid;
static unsigned s_upload_progress_percent;
static char s_upload_progress_stage[32];
static uint32_t s_display_off_idle_ms = APP_UI_DEFAULT_DISPLAY_OFF_IDLE_MS;
/* A semantic acknowledgement, deliberately independent of the asynchronous
 * physical DISPLAY_OFF transition.  This lets the Configuration coordinator
 * distinguish retained policy intent from a later panel/power observation. */
static bool s_display_off_idle_policy_known;
static device_status_t s_display_off_idle_policy_last_status = DEVICE_STATUS_UNAVAILABLE;
/* This is a Power-scheduler admission receipt, not a panel-state shadow. It
 * prevents Configuration from calling a policy applied merely because the UI
 * stored its timeout while an immediate ambient deadline failed to arm. */
static bool s_display_off_idle_schedule_required;
static bool s_display_off_idle_schedule_known;
static bool s_display_off_idle_schedule_armed;
static device_status_t s_display_off_idle_schedule_last_status = DEVICE_STATUS_UNAVAILABLE;
/* A policy timeout change cannot reuse an old deadline until Power has
 * observably cancelled it.  Retain this across coordinator retries: the
 * desired value is already stored, so comparing only the next requested value
 * would otherwise turn a failed replacement into a false "already armed"
 * acknowledgement for the former timeout. */
static bool s_display_off_idle_replacement_pending;
/* The common UI owns one idle deadline.  Rendering/pet synchronization may
 * publish the same ambient scene repeatedly; those repaints are not activity
 * and must never renew an already running deadline. */
static bool s_ambient_display_off_armed;
static bool s_ambient_display_off_scheduling;
static uint64_t s_ambient_display_off_deadline_us;

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
    uint32_t model_revision;
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

/* Alarm is an exclusive foreground owner rather than a replay kind: when it
 * releases the panel, the interrupted replay scene is rendered instead. Keep
 * its currently submitted text/frame in UI-owned storage nevertheless, so an
 * async Display Service never has to retain an Alarm Manager stack buffer. */
typedef struct {
    char time_text[16];
    char label[64];
    uint32_t frame;
    uint32_t attempt;
    uint32_t max_attempts;
} app_ui_alarm_presentation_t;

static app_ui_replay_state_t s_replay;
static app_ui_alarm_presentation_t s_alarm_presentation;
static StaticSemaphore_t s_replay_mutex_storage;
static SemaphoreHandle_t s_replay_mutex;
static uint32_t s_next_ui_revision;

/* Must run under s_model_lock.  The counter is intentionally process-local:
 * persisted UI state, panel DMA completion and renderer animation frames are
 * not part of the Application UI scene identity. */
static void model_touch_locked(void) {
    uint32_t revision = ++s_next_ui_revision;
    if (revision == 0) revision = ++s_next_ui_revision;
    s_model.revision = revision;
}

static bool model_is_ambient_pet(app_ui_surface_t surface, const char *pet_state,
                                 bool recording, bool command_locked,
                                 bool alarm_active) {
    return surface == APP_UI_SURFACE_PET && !recording && !command_locked &&
           !alarm_active && pet_state &&
           (!strcmp(pet_state, "idle") || !strcmp(pet_state, "quiet"));
}

static device_status_t cancel_ambient_display_off(void);

static void replay_lock(void) {
    if (s_replay_mutex) xSemaphoreTakeRecursive(s_replay_mutex, portMAX_DELAY);
}

static void replay_unlock(void) {
    if (s_replay_mutex) xSemaphoreGiveRecursive(s_replay_mutex);
}

static void note_idle_policy_schedule_observation(bool required,
                                                  bool known,
                                                  bool armed,
                                                  device_status_t status) {
    taskENTER_CRITICAL(&s_model_lock);
    if (s_display_off_idle_policy_known) {
        s_display_off_idle_schedule_required = required;
        s_display_off_idle_schedule_known = known;
        s_display_off_idle_schedule_armed = armed;
        s_display_off_idle_schedule_last_status = status;
    }
    taskEXIT_CRITICAL(&s_model_lock);
}

static device_status_t arm_ambient_display_off(uint32_t delay_ms) {
	if (s_display_off_idle_ms == 0) {
		return cancel_ambient_display_off();
	}
	if (delay_ms == APP_UI_DEFAULT_DISPLAY_OFF_IDLE_MS) delay_ms = s_display_off_idle_ms;
    taskENTER_CRITICAL(&s_model_lock);
    bool already_armed = s_ambient_display_off_armed;
    bool scheduling = s_ambient_display_off_scheduling;
    uint64_t deadline_us = s_ambient_display_off_deadline_us;
    uint64_t now_us = (uint64_t)esp_timer_get_time();
    if (already_armed && deadline_us != 0 && now_us >= deadline_us) {
        /* esp_timer invokes the physical transition asynchronously.  Once its
         * deadline has passed, do not let a harmless ambient repaint turn it
         * into a fresh full timeout while that callback is about to run. */
        taskEXIT_CRITICAL(&s_model_lock);
        note_idle_policy_schedule_observation(true, false, false, DEVICE_STATUS_BUSY);
        return DEVICE_STATUS_BUSY;
    }
    if (!already_armed && !scheduling) s_ambient_display_off_scheduling = true;
    taskEXIT_CRITICAL(&s_model_lock);
    if (already_armed) {
        /* The same retained policy may be reconciled again while its original
         * deadline is still active. It is an already-observed scheduler
         * admission, not a reason to create a second deadline. */
        note_idle_policy_schedule_observation(true, true, true, DEVICE_STATUS_OK);
        return DEVICE_STATUS_OK;
    }
    if (scheduling) {
        note_idle_policy_schedule_observation(true, false, false, DEVICE_STATUS_BUSY);
        return DEVICE_STATUS_BUSY;
    }
    delay_ms = sleep_schedule_service_adjust_display_off_delay(delay_ms);
    device_status_t status = device_power_schedule_display_off(delay_ms);
    bool observed_armed = false;
    if (status == DEVICE_STATUS_OK) {
        device_power_snapshot_t power = {0};
        observed_armed = device_power_get_snapshot(&power) && power.display_off_armed;
        if (!observed_armed) status = DEVICE_STATUS_BUSY;
    }
    taskENTER_CRITICAL(&s_model_lock);
    s_ambient_display_off_scheduling = false;
    if (observed_armed) {
        s_ambient_display_off_armed = true;
        s_ambient_display_off_deadline_us = (uint64_t)esp_timer_get_time() +
                                             (uint64_t)delay_ms * 1000u;
    }
    taskEXIT_CRITICAL(&s_model_lock);
    note_idle_policy_schedule_observation(true, status == DEVICE_STATUS_OK,
                                          observed_armed, status);
    if (status != DEVICE_STATUS_OK) {
        /* Power scheduling is an energy optimization; it must not make a
         * foreground transaction fail on an otherwise usable device. */
        ESP_LOGW("maclaw_ui", "cannot arm DISPLAY_OFF: status=%d", status);
    } else {
        ESP_LOGI("maclaw_ui", "DISPLAY_OFF armed after %lu ms", (unsigned long)delay_ms);
    }
    return status;
}

device_status_t app_ui_apply_display_off_idle_policy(uint32_t idle_after_ms) {
	bool timeout_changed;
	bool replacement_pending;
	taskENTER_CRITICAL(&s_model_lock);
	timeout_changed = s_display_off_idle_ms != idle_after_ms;
	s_display_off_idle_ms = idle_after_ms;
	if (timeout_changed && idle_after_ms != 0) {
		s_display_off_idle_replacement_pending = true;
	}
	replacement_pending = s_display_off_idle_replacement_pending;
	/* The policy is fully retained under the same model lock before any
	 * ambient-only scheduling decision below. A foreground scene may suppress
	 * scheduling for now, but it must not make Configuration reconciliation
	 * report that the requested policy was never accepted. */
	s_display_off_idle_policy_known = true;
	s_display_off_idle_policy_last_status = DEVICE_STATUS_OK;
	s_display_off_idle_schedule_required = false;
	s_display_off_idle_schedule_known = false;
	s_display_off_idle_schedule_armed = false;
	s_display_off_idle_schedule_last_status = DEVICE_STATUS_UNAVAILABLE;
	taskEXIT_CRITICAL(&s_model_lock);
	if (idle_after_ms == 0) {
		const device_status_t cancel_status = cancel_ambient_display_off();
		note_idle_policy_schedule_observation(false, cancel_status == DEVICE_STATUS_OK,
									 false, cancel_status);
		return cancel_status;
	} else {
		if (timeout_changed || replacement_pending) {
			const device_status_t cancel_status = cancel_ambient_display_off();
			if (cancel_status != DEVICE_STATUS_OK) return cancel_status;
			taskENTER_CRITICAL(&s_model_lock);
			s_display_off_idle_replacement_pending = false;
			taskEXIT_CRITICAL(&s_model_lock);
		}
		app_ui_model_t model = app_ui_snapshot();
		bool ambient = (model.surface == APP_UI_SURFACE_PET) &&
			(!strcmp(model.pet_state, "idle") || !strcmp(model.pet_state, "quiet"));
		if (!ambient || model.recording_active || model.command_display_locked ||
			model.alarm_visual_active) {
			note_idle_policy_schedule_observation(false, true, false, DEVICE_STATUS_OK);
			return DEVICE_STATUS_OK;
		}
		return arm_ambient_display_off(idle_after_ms);
	}
	return DEVICE_STATUS_OK;
}

bool app_ui_get_display_off_idle_policy_state(
    app_ui_display_off_idle_policy_state_t *out_state) {
    if (!out_state) return false;
    taskENTER_CRITICAL(&s_model_lock);
    *out_state = (app_ui_display_off_idle_policy_state_t){
        .struct_size = sizeof(*out_state),
        .abi_version = APP_UI_DISPLAY_OFF_IDLE_POLICY_STATE_ABI_VERSION,
        .known = s_display_off_idle_policy_known,
        .idle_after_ms = s_display_off_idle_ms,
        .last_status = s_display_off_idle_policy_last_status,
        .schedule_required = s_display_off_idle_schedule_required,
        .schedule_known = s_display_off_idle_schedule_known,
        .schedule_armed = s_display_off_idle_schedule_armed,
        .schedule_last_status = s_display_off_idle_schedule_last_status,
    };
    taskEXIT_CRITICAL(&s_model_lock);
    return true;
}

static device_status_t cancel_ambient_display_off(void) {
	const device_status_t status = device_power_cancel_display_off();
	if (status != DEVICE_STATUS_OK) {
		note_idle_policy_schedule_observation(true, false, false, status);
		return status;
	}
    taskENTER_CRITICAL(&s_model_lock);
    s_ambient_display_off_armed = false;
    s_ambient_display_off_scheduling = false;
    s_ambient_display_off_deadline_us = 0;
    s_display_off_idle_replacement_pending = false;
    taskEXIT_CRITICAL(&s_model_lock);
    note_idle_policy_schedule_observation(false, true, false, DEVICE_STATUS_OK);
	return DEVICE_STATUS_OK;
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
    /* Selecting a replay payload is itself a scene mutation: READY_PROMPT →
     * PET, for example, does not necessarily change a scalar in s_model.
     * Bind the copied payload to the exact UI revision while both replay and
     * model state are stable. */
    taskENTER_CRITICAL(&s_model_lock);
    model_touch_locked();
    s_replay.model_revision = s_model.revision;
    taskEXIT_CRITICAL(&s_model_lock);
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
            device_display_set_command_lock(true);
            device_display_show_startup();
            break;
        case APP_UI_REPLAY_RECORDING:
            device_display_set_command_lock(model.command_display_locked);
            device_display_set_recording_mode(model.meeting_recording);
            device_display_set_recording_visual(model.recording_active,
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
            device_display_set_command_lock(model.command_display_locked);
            device_display_show_text(s_replay.title, s_replay.text);
            break;
        case APP_UI_REPLAY_UPLOAD:
            device_display_set_command_lock(model.command_display_locked);
            device_display_show_upload_progress((uint32_t)s_replay.completed_bytes,
                                            (uint32_t)s_replay.total_bytes,
                                            s_replay.stage);
            break;
        case APP_UI_REPLAY_RESPONSE_TEXT:
            device_display_set_command_lock(model.command_display_locked);
            device_display_show_response(s_replay.title, s_replay.text);
            (void)device_display_restore_response_page((uint32_t)s_replay.response_page);
            break;
        case APP_UI_REPLAY_RESPONSE_IMAGE:
            device_display_set_command_lock(model.command_display_locked);
            device_display_show_response_image(s_replay.title, s_replay.text,
                                           s_replay.image_pixels,
                                           (uint32_t)s_replay.width,
                                           (uint32_t)s_replay.height);
            break;
        case APP_UI_REPLAY_SETUP_QR:
            device_display_set_command_lock(model.command_display_locked);
            device_display_show_qrcode_modules(s_replay.qr_modules,
                                          (uint32_t)s_replay.qr_module_count,
                                          s_replay.ssid);
            break;
        case APP_UI_REPLAY_READY_PROMPT:
            device_display_set_command_lock(false);
            device_display_show_ready_prompt(s_replay.title, s_replay.text);
            break;
        case APP_UI_REPLAY_PET:
        default:
            device_display_set_command_lock(model.command_display_locked);
            device_display_set_command_stage(model.command_stage);
            device_input_set_command_cancel_enabled(model.command_cancel_enabled);
            device_display_set_pet_profile(model.pet_skin, true);
            device_display_set_pet_state(model.pet_state);
            break;
    }
}

/* Caller holds replay_lock(). Keeping this state mutation and its renderer
 * transition inside the same recursive submission boundary prevents a
 * foreground publisher from interleaving a stale recording-clear frame. */
static void stop_recording_if_needed_locked(void) {
    bool was_recording;
    bool alarm_active;
    taskENTER_CRITICAL(&s_model_lock);
    was_recording = s_model.recording_active;
    s_model.recording_active = false;
    s_model.recording_paused = false;
    if (was_recording) model_touch_locked();
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (was_recording && !alarm_active) device_display_set_recording_visual(false, false, 0);
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
    s_ambient_display_off_armed = false;
    s_ambient_display_off_scheduling = false;
    s_ambient_display_off_deadline_us = 0;
    s_display_off_idle_replacement_pending = false;
    s_model.surface = APP_UI_SURFACE_PET;
    strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
    strlcpy(s_model.pet_skin, "clawmate", sizeof(s_model.pet_skin));
    strlcpy(s_model.command_stage, "正在处理", sizeof(s_model.command_stage));
    model_touch_locked();
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
    (void)cancel_ambient_display_off();
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_STARTUP;
    s_model.command_display_locked = true;
    strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
    s_upload_progress_valid = false;
    s_upload_progress_stage[0] = '\0';
    model_touch_locked();
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    foreground_coordinator_observe_acquire(FOREGROUND_OWNER_STARTUP,
                                           FOREGROUND_PRIORITY_RECOVERY,
                                           FOREGROUND_SCENE_STARTUP_SPLASH);
    replay_begin_locked(APP_UI_REPLAY_STARTUP);
    if (alarm_active) {
        replay_unlock();
        return;
    }
    device_display_set_command_lock(true);
    device_display_show_startup();
    replay_unlock();
}

void app_ui_set_pet_state(const char *state) {
    bool recording;
    bool suppress_ambient;
    bool alarm_active;
    bool entered_ambient;
    bool ambient_after;
    char display_state[sizeof(s_model.pet_state)];
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    const char *next_state = state ? state : "idle";
    const bool was_ambient = model_is_ambient_pet(
        s_model.surface, s_model.pet_state, s_model.recording_active,
        s_model.command_display_locked, s_model.alarm_visual_active);
    suppress_ambient = s_model.command_display_locked &&
                       (!strcmp(next_state, "idle") || !strcmp(next_state, "quiet"));
    if (!suppress_ambient) {
        strlcpy(s_model.pet_state, next_state, sizeof(s_model.pet_state));
    }
    recording = s_model.recording_active;
    if (!recording && !suppress_ambient) s_model.surface = APP_UI_SURFACE_PET;
    if (!suppress_ambient) model_touch_locked();
    alarm_active = s_model.alarm_visual_active;
    entered_ambient = !was_ambient && model_is_ambient_pet(
        s_model.surface, s_model.pet_state, s_model.recording_active,
        s_model.command_display_locked, s_model.alarm_visual_active);
    ambient_after = model_is_ambient_pet(
        s_model.surface, s_model.pet_state, s_model.recording_active,
        s_model.command_display_locked, s_model.alarm_visual_active);
    strlcpy(display_state, s_model.pet_state, sizeof(display_state));
    taskEXIT_CRITICAL(&s_model_lock);
    if (!recording && !suppress_ambient) replay_begin_locked(APP_UI_REPLAY_PET);
    // During recording the requested pet state is retained in the shared model
    // and becomes visible when the recorder closes. It cannot overwrite the
    // waveform midway through a capture.
    if (!recording && !suppress_ambient && !alarm_active) {
        device_display_set_pet_state(display_state);
    }
    if (!recording && !suppress_ambient && !alarm_active && entered_ambient) {
        foreground_coordinator_observe_ambient_restored();
		/* The Hub may periodically repeat an unchanged idle/quiet state while
		 * delivering ambient data or pet metadata.  That is not user activity:
		 * rearming here would keep every profile lit indefinitely.  The timeout
		 * starts only when the common UI actually returns from a foreground
		 * surface to the ambient pet. */
		arm_ambient_display_off(APP_UI_DEFAULT_DISPLAY_OFF_IDLE_MS);
    } else if (!recording && !suppress_ambient) {
        if (!ambient_after) {
            (void)cancel_ambient_display_off();
        }
    }
    replay_unlock();
}

void app_ui_set_command_stage(const char *stage) {
    bool alarm_active;
    char display_stage[sizeof(s_model.command_stage)];
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    strlcpy(s_model.command_stage, stage && stage[0] ? stage : "正在处理",
            sizeof(s_model.command_stage));
    model_touch_locked();
    alarm_active = s_model.alarm_visual_active;
    strlcpy(display_stage, s_model.command_stage, sizeof(display_stage));
    taskEXIT_CRITICAL(&s_model_lock);
    if (!alarm_active) device_display_set_command_stage(display_stage);
    replay_unlock();
}

void app_ui_set_command_display_lock(bool locked) {
    bool alarm_active;
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.command_display_locked = locked;
    model_touch_locked();
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (!alarm_active) device_display_set_command_lock(locked);
    replay_unlock();
}

void app_ui_set_command_cancel_enabled(bool enabled) {
    bool alarm_active;
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.command_cancel_enabled = enabled;
    model_touch_locked();
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (!alarm_active) device_input_set_command_cancel_enabled(enabled);
    replay_unlock();
}

void app_ui_set_pet_profile(const char *skin, bool motion_enabled) {
    // Board ports already separate model mutation from painting through their
    // foreground-display guard. Always apply the new profile so the first ready
    // frame is current; the startup artwork remains pixel-stable while locked.
    replay_lock();
    char display_skin[sizeof(s_model.pet_skin)];
    taskENTER_CRITICAL(&s_model_lock);
    if (skin && skin[0]) {
        strlcpy(s_model.pet_skin, skin, sizeof(s_model.pet_skin));
    }
    model_touch_locked();
    strlcpy(display_skin, s_model.pet_skin, sizeof(display_skin));
    taskEXIT_CRITICAL(&s_model_lock);
    device_display_set_pet_profile(display_skin, motion_enabled);
    replay_unlock();
}

device_status_t app_ui_set_pet_asset(const uint8_t *const *frames, size_t frame_count,
                                     size_t width, size_t height, uint32_t frame_ms) {
    // Install the asset now without painting over startup. Both board ports
    // defer presentation while the foreground-display guard is active.
    if (frame_count > UINT32_MAX || width > UINT32_MAX || height > UINT32_MAX) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    replay_lock();
    device_status_t status = device_display_set_pet_asset(
        frames, (uint32_t)frame_count, (uint32_t)width, (uint32_t)height, frame_ms);
    replay_unlock();
    return status;
}

device_status_t app_ui_set_pet_asset_consuming(uint8_t **frames, size_t frame_count,
                                               size_t width, size_t height, uint32_t frame_ms) {
    if (frame_count > UINT32_MAX || width > UINT32_MAX || height > UINT32_MAX) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    replay_lock();
    device_status_t status = device_display_set_pet_asset_consuming(
        frames, (uint32_t)frame_count, (uint32_t)width, (uint32_t)height, frame_ms);
    replay_unlock();
    return status;
}

void app_ui_set_recording_mode(bool meeting) {
    bool alarm_active;
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.meeting_recording = meeting;
    s_model.elapsed_seconds = 0;
    model_touch_locked();
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (!alarm_active) device_display_set_recording_mode(meeting);
    replay_unlock();
}

void app_ui_set_recording_visual(bool active, bool paused, uint32_t elapsed_seconds) {
    char next_pet[sizeof(s_model.pet_state)];
    bool meeting;
    bool command_locked;
    bool alarm_active;
    bool was_recording;
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    was_recording = s_model.recording_active;
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
    model_touch_locked();
    meeting = s_model.meeting_recording;
    alarm_active = s_model.alarm_visual_active;
    strlcpy(next_pet, s_model.pet_state, sizeof(next_pet));
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(active ? APP_UI_REPLAY_RECORDING : APP_UI_REPLAY_PET);

    if (active || command_locked) {
        (void)cancel_ambient_display_off();
    } else {
        /* A capture close is a transition back to the ambient surface.  It is
         * the owner of the idle deadline, so arm only when this call actually
         * left an active recording.  Some recorders publish their final
         * inactive visual more than once; treating each duplicate as activity
         * would indefinitely postpone DISPLAY_OFF on every board profile. */
        if (was_recording) arm_ambient_display_off(APP_UI_DEFAULT_DISPLAY_OFF_IDLE_MS);
    }

    if (alarm_active) {
        replay_unlock();
        return;
    }

    // Always re-assert the mode before rendering. This makes mode and visual a
    // single shared transition even when different tasks updated the UI.
    device_display_set_recording_mode(meeting);
    device_display_set_recording_visual(active, paused, elapsed_seconds);
    if (!active && !command_locked) device_display_set_pet_state(next_pet);
    replay_unlock();
}

void app_ui_set_audio_level(uint16_t level, uint32_t elapsed_seconds) {
    bool active;
    bool alarm_active;
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    active = s_model.recording_active;
    if (active) {
        if (level > 1000) level = 1000;
        // The app model owns state transitions only.  The physical board is
        // the sole owner of Bread Compact's smoothing and 24-column history.
    }
    if (active) model_touch_locked();
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (active && !alarm_active) device_display_set_audio_level(level, elapsed_seconds);
    replay_unlock();
}

void app_ui_push_recording_pcm(const int16_t *samples, size_t count) {
    // PCM belongs to recording/upload. The visual history is advanced by
    // app_ui_set_audio_level(), matching Bread's level-driven waveform.
    (void)samples;
    (void)count;
}

void app_ui_show_text(const char *title, const char *text) {
    (void)cancel_ambient_display_off();
    replay_lock();
    stop_recording_if_needed_locked();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_MESSAGE;
    model_touch_locked();
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(APP_UI_REPLAY_MESSAGE);
    strlcpy(s_replay.title, title ? title : "", sizeof(s_replay.title));
    strlcpy(s_replay.text, text ? text : "", sizeof(s_replay.text));
    /* Cross the UI -> Device Display seam only with the replay-owned copy.
     * Today the selected renderer consumes this synchronously, but this keeps
     * a caller's HTTP/stack buffer from becoming an accidental display
     * payload owner while the Display Task migration is staged. */
    if (!alarm_active) device_display_show_text(s_replay.title, s_replay.text);
    replay_unlock();
}

void app_ui_show_upload_progress(size_t completed_bytes, size_t total_bytes,
                                 const char *stage) {
    (void)cancel_ambient_display_off();
    replay_lock();
    stop_recording_if_needed_locked();
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
    model_touch_locked();
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(APP_UI_REPLAY_UPLOAD);
    s_replay.completed_bytes = completed_bytes;
    s_replay.total_bytes = total_bytes;
    strlcpy(s_replay.stage, visible_stage, sizeof(s_replay.stage));
    if (!unchanged && !alarm_active) {
        device_display_show_upload_progress((uint32_t)s_replay.completed_bytes,
                                            (uint32_t)s_replay.total_bytes,
                                            s_replay.stage);
    }
    replay_unlock();
}

void app_ui_show_response(const char *title, const char *text) {
    (void)cancel_ambient_display_off();
    replay_lock();
    stop_recording_if_needed_locked();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_RESPONSE;
    model_touch_locked();
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(APP_UI_REPLAY_RESPONSE_TEXT);
    strlcpy(s_replay.title, title ? title : "", sizeof(s_replay.title));
    strlcpy(s_replay.text, text ? text : "", sizeof(s_replay.text));
    if (!alarm_active) {
        device_display_show_response(s_replay.title, s_replay.text);
    }
    replay_unlock();
}

void app_ui_show_response_image(const char *title, const char *caption,
                                const uint16_t *pixels, size_t width, size_t height) {
    (void)cancel_ambient_display_off();
    if (!pixels || width < 1 || width > 64 || height < 1 || height > 64) return;
    replay_lock();
    stop_recording_if_needed_locked();
    size_t pixel_count = width * height;
    uint16_t *owned_pixels = malloc(pixel_count * sizeof(*owned_pixels));
    if (!owned_pixels) {
        replay_unlock();
        return;
    }
    memcpy(owned_pixels, pixels, pixel_count * sizeof(*owned_pixels));
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_RESPONSE;
    model_touch_locked();
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(APP_UI_REPLAY_RESPONSE_IMAGE);
    s_replay.image_pixels = owned_pixels;
    s_replay.width = width;
    s_replay.height = height;
    strlcpy(s_replay.title, title ? title : "", sizeof(s_replay.title));
    strlcpy(s_replay.text, caption ? caption : "", sizeof(s_replay.text));
    if (!alarm_active) {
        /* The copied image is the UI payload. Never make a renderer depend on
         * the Gateway decoder's transient response buffer. */
        device_display_show_response_image(s_replay.title, s_replay.text,
                                           s_replay.image_pixels,
                                           (uint32_t)s_replay.width,
                                           (uint32_t)s_replay.height);
    }
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
    // The active renderer is the source of truth for reply pagination on
    // every profile.  It may implement manual pages, auto-pages, or neither;
    // the shared UI coordinator must not know which hardware selected it.
    if (response_visible) handled = device_display_navigate_response(page_delta);
    if (handled && s_replay.kind == APP_UI_REPLAY_RESPONSE_TEXT) {
        uint32_t page = 0;
        if (device_display_get_response_page(&page)) s_replay.response_page = page;
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
        model_touch_locked();
    }
    strlcpy(pet_state, s_model.pet_state, sizeof(pet_state));
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (response_visible) replay_begin_locked(APP_UI_REPLAY_PET);
    if (response_visible && !alarm_active) {
        foreground_coordinator_observe_ambient_restored();
        // Release the HAL's foreground guard before requesting the ambient
        // repaint. EchoEar keeps response_active as a stale-frame barrier;
        // Bread Compact uses the same lock to reject late idle updates.
        device_display_set_command_lock(false);
        device_display_set_pet_state(pet_state);
		arm_ambient_display_off(APP_UI_DEFAULT_DISPLAY_OFF_IDLE_MS);
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
    model_touch_locked();
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    foreground_coordinator_observe_ambient_restored();
    replay_begin_locked(APP_UI_REPLAY_PET);

    if (alarm_active) {
        replay_unlock();
        return;
    }

    // Publish the HAL transition in ownership order: first remove the command
    // guards, then paint idle. Both board ports reject stale ambient frames
    // while the guard is set, so reversing this order would leave the cancel
    // message visible even though the application model already says PET.
    device_input_set_command_cancel_enabled(false);
    device_display_set_command_lock(false);
    device_display_set_recording_mode(false);
    device_display_set_pet_state("idle");
	arm_ambient_display_off(APP_UI_DEFAULT_DISPLAY_OFF_IDLE_MS);
    replay_unlock();
}

int app_ui_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]) {
    /* The caller owns this source buffer. Display Service performs the
     * synchronous submission copy before the call returns. */
    replay_lock();
    int cached = device_display_cache_glyph(codepoint, bitmap);
    replay_unlock();
    return cached;
}

bool app_ui_show_qrcode_modules(const uint8_t *modules, size_t module_count,
                                const char *ssid) {
    (void)cancel_ambient_display_off();
    /* ESP QR encoders use an N×N matrix. Reject malformed producer output
     * before it can mutate shared UI/replay state. */
    if (!modules || module_count == 0 || module_count > 177u * 177u) return false;
    size_t size = 1;
    while (size * size < module_count) ++size;
    if (size * size != module_count) return false;
    uint8_t *owned_modules = malloc(module_count);
    if (!owned_modules) return false;
    for (size_t index = 0; index < module_count; ++index) {
        owned_modules[index] = modules[index] ? 1u : 0u;
    }
    replay_lock();
    stop_recording_if_needed_locked();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.surface = APP_UI_SURFACE_SETUP;
    model_touch_locked();
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    replay_begin_locked(APP_UI_REPLAY_SETUP_QR);
    s_replay.qr_modules = owned_modules;
    s_replay.qr_module_count = size;
    strlcpy(s_replay.ssid, ssid ? ssid : "", sizeof(s_replay.ssid));
    if (!alarm_active) {
        device_display_show_qrcode_modules(s_replay.qr_modules,
                                           (uint32_t)s_replay.qr_module_count,
                                           s_replay.ssid);
    }
    replay_unlock();
    return true;
}

void app_ui_show_ready_prompt(const char *title, const char *text) {
    (void)cancel_ambient_display_off();
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    bool was_recording = s_model.recording_active;
    s_model.recording_active = false;
    s_model.recording_paused = false;
    s_model.surface = APP_UI_SURFACE_PET;
    s_model.command_display_locked = false;
    strlcpy(s_model.pet_state, "idle", sizeof(s_model.pet_state));
    model_touch_locked();
    bool alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    /* Ready is an ambient state, not a foreground scene.  Keeping the legacy
     * status copy as the replay surface left the normal idle pet hidden for a
     * full display-off interval, so an already restored multi-frame pack had
     * no opportunity to animate.  The business model stays PET/idle; publish
     * that same normalized state through Display HAL and leave any ready copy
     * to logs/voice feedback. */
    replay_begin_locked(APP_UI_REPLAY_PET);
    s_replay.title[0] = '\0';
    s_replay.text[0] = '\0';
    if (!alarm_active) {
        if (was_recording) device_display_set_recording_visual(false, false, 0);
        device_display_set_command_lock(false);
        device_display_set_pet_state("idle");
        /* The old renderer held the ready prompt for one minute and then
         * started its 30-minute ambient timer. Preserve that user-visible
         * timing while moving the actual deadline ownership to Power Service. */
        arm_ambient_display_off(APP_UI_DEFAULT_DISPLAY_OFF_IDLE_MS);
    }
    replay_unlock();
}

void app_ui_cancel_ready_prompt(void) {
    // A new interaction consumes the prompt before it has a foreground scene.
    // Disarm here rather than relying on a later recorder/message transition.
    (void)cancel_ambient_display_off();
    replay_lock();
    bool alarm_active;
    if (s_replay.kind == APP_UI_REPLAY_READY_PROMPT) {
        replay_begin_locked(APP_UI_REPLAY_PET);
    }
    taskENTER_CRITICAL(&s_model_lock);
    alarm_active = s_model.alarm_visual_active;
    taskEXIT_CRITICAL(&s_model_lock);
    if (!alarm_active) device_display_cancel_ready_prompt();
    replay_unlock();
}

bool app_ui_wake_from_idle(void) {
    const device_status_t wake_status = device_power_wake_display_from_user();
    if (wake_status == DEVICE_STATUS_OK) {
        /* The power service consumed the old timer before waking the panel.
         * Mirror that state in the UI owner so the next genuine return to
         * ambient can create a fresh deadline. */
        taskENTER_CRITICAL(&s_model_lock);
        s_ambient_display_off_armed = false;
        s_ambient_display_off_scheduling = false;
        s_ambient_display_off_deadline_us = 0;
        taskEXIT_CRITICAL(&s_model_lock);
    }
    return wake_status == DEVICE_STATUS_OK;
}

void app_ui_note_schedule_display_wake(void) {
    /* Schedule Power has consumed its deadline before it reports this wake.
     * Clear the UI mirror even if a foreground surface already restored the
     * panel, then create one ordinary deadline only for the ambient pet. This
     * deliberately remains distinct from app_ui_wake_from_idle(): no local
     * input activity or manual schedule override is synthesized here. */
    taskENTER_CRITICAL(&s_model_lock);
    s_ambient_display_off_armed = false;
    s_ambient_display_off_scheduling = false;
    s_ambient_display_off_deadline_us = 0;
    const bool ambient = model_is_ambient_pet(s_model.surface, s_model.pet_state,
                                              s_model.recording_active,
                                              s_model.command_display_locked,
                                              s_model.alarm_visual_active);
    taskEXIT_CRITICAL(&s_model_lock);
    if (ambient) arm_ambient_display_off(APP_UI_DEFAULT_DISPLAY_OFF_IDLE_MS);
}

device_status_t app_ui_apply_remote_brightness(uint8_t percent) {
    if (percent > 100) return DEVICE_STATUS_INVALID_ARGUMENT;

    /* A GUI brightness change is display management, not a user interaction:
     * only a visible non-zero level may restore DISPLAY_OFF, and it must not
     * enter the input/voice-command path.  Keep zero as the existing
     * backlight-off-while-running command. */
    device_power_snapshot_t power = {0};
    const bool was_display_off =
        percent != 0 && device_power_get_snapshot(&power) &&
        power.state == DEVICE_POWER_STATE_DISPLAY_OFF;
    bool woke = false;
    if (was_display_off) {
        const device_status_t wake_status = device_power_wake_display_from_remote_control();
        woke = wake_status == DEVICE_STATUS_OK;
        if (!woke) {
            /* A foreground render can legitimately win the race and restore
             * the panel itself.  Re-read the physical observation before
             * deciding whether this is a benign race or an actual failed
             * remote wake.  In the latter case do not accept/persist a GUI
             * setting that the user cannot yet see. */
            device_power_snapshot_t after_wake = {0};
            if (device_power_get_snapshot(&after_wake) &&
                after_wake.state == DEVICE_POWER_STATE_DISPLAY_OFF) {
                ESP_LOGW("maclaw_ui", "remote brightness left panel in DISPLAY_OFF");
                return wake_status == DEVICE_STATUS_OK ? DEVICE_STATUS_BUSY : wake_status;
            }
        }
    }
    if (woke) {
        /* Power Service consumed the expired deadline.  Reconcile the UI's
         * bookkeeping before creating the normal new ambient idle window. */
        taskENTER_CRITICAL(&s_model_lock);
        s_ambient_display_off_armed = false;
        s_ambient_display_off_scheduling = false;
        s_ambient_display_off_deadline_us = 0;
        taskEXIT_CRITICAL(&s_model_lock);
    }

    replay_lock();
    device_status_t status = device_display_set_brightness(percent);
    replay_unlock();
    if (status != DEVICE_STATUS_OK || !woke) return status;

    app_ui_model_t model = app_ui_snapshot();
    if (model_is_ambient_pet(model.surface, model.pet_state,
                             model.recording_active, model.command_display_locked,
                             model.alarm_visual_active)) {
        arm_ambient_display_off(APP_UI_DEFAULT_DISPLAY_OFF_IDLE_MS);
    }
    return status;
}

void app_ui_set_wifi_status(const char *ssid, bool connected) {
    char display_ssid[sizeof(s_model.wifi_ssid)];
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.wifi_connected = connected;
    strlcpy(s_model.wifi_ssid, ssid ? ssid : "", sizeof(s_model.wifi_ssid));
    model_touch_locked();
    strlcpy(display_ssid, s_model.wifi_ssid, sizeof(display_ssid));
    taskEXIT_CRITICAL(&s_model_lock);
    // Forward every state transition.  The board port deliberately defers the
    // cosmetic repaint while a response, recording, setup QR, or alarm owns
    // the screen, but must retain the transport state for the next standby
    // composition (the same ownership contract used for service readiness).
    device_display_set_wifi_status(display_ssid, connected);
    replay_unlock();
}

void app_ui_set_service_ready(bool ready) {
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.service_ready = ready;
    model_touch_locked();
    taskEXIT_CRITICAL(&s_model_lock);
    // Always forward the model mutation. The board port defers repainting when
    // a command/setup/alarm owns the display, but must still remember an outage
    // that occurred behind that foreground surface.
    device_display_set_service_ready(ready);
    replay_unlock();
}

void app_ui_set_ambient(const char *time, const char *location, const char *date,
                        const char *weekday, const char *weather_summary,
                        int temperature_c, bool weather_valid, bool weather_stale) {
    app_ui_model_t display_model;
    replay_lock();
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
    model_touch_locked();
    display_model = s_model;
    taskEXIT_CRITICAL(&s_model_lock);
    // Always forward the model mutation.  The EchoEar board port has the same
    // foreground guard as Bread Compact and stores an update received behind a
    // result/upload/setup screen for the first restored standby frame.  Dropping
    // it here used to leave date/weather stale until the next server tick.
    device_display_set_ambient(display_model.ambient_time,
                               display_model.ambient_location,
                               display_model.ambient_date,
                               display_model.ambient_weekday,
                               display_model.ambient_weather,
                               display_model.ambient_temperature_c,
                               display_model.ambient_weather_valid,
                               display_model.ambient_weather_stale);
    replay_unlock();
}

void app_ui_set_alarm_scheduled(bool scheduled) {
    replay_lock();
    taskENTER_CRITICAL(&s_model_lock);
    s_model.alarm_scheduled = scheduled;
    model_touch_locked();
    taskEXIT_CRITICAL(&s_model_lock);
    // The board port stores this model state even while a startup or command
    // foreground owns the LCD, then includes it in the next standby frame.
    device_display_set_alarm_scheduled(scheduled);
    replay_unlock();
}

void app_ui_set_alarm_visual(bool active, unsigned frame, const char *time_text,
                             const char *label, unsigned attempt, unsigned max_attempts) {
    replay_lock();
    uint32_t interrupted_response_page = 0;
    bool have_interrupted_response_page = false;
    taskENTER_CRITICAL(&s_model_lock);
    bool was_active = s_model.alarm_visual_active;
    bool text_response_visible = !was_active && active &&
                                 s_model.surface == APP_UI_SURFACE_RESPONSE &&
                                 s_replay.kind == APP_UI_REPLAY_RESPONSE_TEXT;
    s_model.alarm_visual_active = active;
    model_touch_locked();
    strlcpy(s_alarm_presentation.time_text, time_text ? time_text : "",
            sizeof(s_alarm_presentation.time_text));
    strlcpy(s_alarm_presentation.label, label ? label : "",
            sizeof(s_alarm_presentation.label));
    s_alarm_presentation.frame = frame;
    s_alarm_presentation.attempt = attempt;
    s_alarm_presentation.max_attempts = max_attempts;
    taskEXIT_CRITICAL(&s_model_lock);
    if (active) {
        (void)cancel_ambient_display_off();
        if (text_response_visible) {
            have_interrupted_response_page =
                device_display_get_response_page(&interrupted_response_page);
            if (have_interrupted_response_page) {
                s_replay.response_page = interrupted_response_page;
            }
        }
        device_display_set_alarm_visual(true, s_alarm_presentation.frame,
                                        s_alarm_presentation.time_text,
                                        s_alarm_presentation.label,
                                        s_alarm_presentation.attempt,
                                        s_alarm_presentation.max_attempts);
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
    device_display_set_alarm_visual(false, s_alarm_presentation.frame,
                                    s_alarm_presentation.time_text,
                                    s_alarm_presentation.label,
                                    s_alarm_presentation.attempt,
                                    s_alarm_presentation.max_attempts);
    replay_render_locked();
    if (s_replay.kind == APP_UI_REPLAY_PET) {
		arm_ambient_display_off(APP_UI_DEFAULT_DISPLAY_OFF_IDLE_MS);
    }
    replay_unlock();
}
