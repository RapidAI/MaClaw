#include "services/foreground_coordinator.h"

#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "app_ui.h"

/* Shadow-mode decision log tag: new diagnostics only, no legacy behavior. */
static const char *TAG = "foreground_coord";

static portMUX_TYPE s_foreground_lock = portMUX_INITIALIZER_UNLOCKED;

static foreground_lease_t s_leases[FOREGROUND_OWNER_COUNT];
static uint32_t s_next_token = 1;
static uint32_t s_event_seq;
static bool s_authoritative;

/* Divergence dedup state: the last reported (actual, expected, owner, scene)
 * tuple.  A divergence is logged only when the tuple changes; returning to a
 * matching state after a reported divergence emits one DEBUG convergence
 * line and re-arms the dedup. */
static bool s_divergence_open;
static int s_last_actual = -1;
static int s_last_expected = -1;
static foreground_owner_t s_last_owner = FOREGROUND_OWNER_NONE;
static foreground_scene_kind_t s_last_scene = FOREGROUND_SCENE_AMBIENT;

/* Semantic scene -> App UI surface mapping.  This is the coordinator's model
 * of the legacy presentation rules; divergences from the actual surface are
 * exactly what the shadow comparison reports. */
static int expected_surface_for_scene(foreground_scene_kind_t scene) {
    switch (scene) {
        case FOREGROUND_SCENE_STARTUP_SPLASH: return APP_UI_SURFACE_STARTUP;
        case FOREGROUND_SCENE_SETUP_PORTAL: return APP_UI_SURFACE_SETUP;
        case FOREGROUND_SCENE_VOICE_COMMAND: return APP_UI_SURFACE_RECORDING;
        case FOREGROUND_SCENE_COMMAND_MESSAGE: return APP_UI_SURFACE_MESSAGE;
        case FOREGROUND_SCENE_COMMAND_RESULT: return APP_UI_SURFACE_RESPONSE;
        case FOREGROUND_SCENE_MEETING_RECORD: return APP_UI_SURFACE_RECORDING;
        case FOREGROUND_SCENE_MEETING_UPLOAD: return APP_UI_SURFACE_UPLOAD;
        case FOREGROUND_SCENE_MEETING_RESULT: return APP_UI_SURFACE_MESSAGE;
        case FOREGROUND_SCENE_ALARM_RING: return APP_UI_SURFACE_ALARM;
        case FOREGROUND_SCENE_AMBIENT:
        default: return APP_UI_SURFACE_PET;
    }
}

/* Highest-priority held lease; ties resolve to the newest token.  Caller
 * holds s_foreground_lock. */
static int top_lease_index_locked(void) {
    int top = -1;
    for (int i = 0; i < (int)FOREGROUND_OWNER_COUNT; ++i) {
        if (!s_leases[i].held) continue;
        if (top < 0 || s_leases[i].priority > s_leases[top].priority ||
            (s_leases[i].priority == s_leases[top].priority &&
             s_leases[i].token > s_leases[top].token)) {
            top = i;
        }
    }
    return top;
}

static uint32_t held_mask_locked(void) {
    uint32_t mask = 0;
    for (int i = 0; i < (int)FOREGROUND_OWNER_COUNT; ++i) {
        if (s_leases[i].held) mask |= 1u << i;
    }
    return mask;
}

/* Shadow comparison: recompute the expected surface and compare it with the
 * actual App UI surface.  Never touches presentation state. */
static void shadow_compare(void) {
    foreground_owner_t owner;
    foreground_scene_kind_t scene;
    uint32_t mask;
    uint32_t seq;
    taskENTER_CRITICAL(&s_foreground_lock);
    int top = top_lease_index_locked();
    owner = top >= 0 ? s_leases[top].owner : FOREGROUND_OWNER_NONE;
    scene = top >= 0 ? s_leases[top].scene : FOREGROUND_SCENE_AMBIENT;
    mask = held_mask_locked();
    seq = s_event_seq;
    taskEXIT_CRITICAL(&s_foreground_lock);

    int expected = expected_surface_for_scene(scene);
    int actual = (int)app_ui_snapshot().surface;
    if (actual == expected) {
        taskENTER_CRITICAL(&s_foreground_lock);
        bool was_open = s_divergence_open;
        s_divergence_open = false;
        taskEXIT_CRITICAL(&s_foreground_lock);
        if (was_open) {
            ESP_LOGD(TAG, "foreground shadow converged: surface=%d owner=%d seq=%lu",
                     actual, (int)owner, (unsigned long)seq);
        }
        return;
    }
    taskENTER_CRITICAL(&s_foreground_lock);
    bool changed = !s_divergence_open || s_last_actual != actual ||
                   s_last_expected != expected || s_last_owner != owner ||
                   s_last_scene != scene;
    if (changed) {
        s_divergence_open = true;
        s_last_actual = actual;
        s_last_expected = expected;
        s_last_owner = owner;
        s_last_scene = scene;
    }
    taskEXIT_CRITICAL(&s_foreground_lock);
    if (changed) {
        ESP_LOGI(TAG,
                 "foreground shadow divergence: actual=%d expected=%d owner=%d scene=%d held=0x%lx seq=%lu",
                 actual, expected, (int)owner, (int)scene, (unsigned long)mask,
                 (unsigned long)seq);
    }
}

uint32_t foreground_coordinator_acquire(foreground_owner_t owner,
                                        foreground_priority_t priority,
                                        foreground_scene_kind_t scene) {
    if (owner <= FOREGROUND_OWNER_NONE || owner >= FOREGROUND_OWNER_COUNT) return 0;
    uint32_t token;
    taskENTER_CRITICAL(&s_foreground_lock);
    token = s_next_token++;
    if (s_next_token == 0) s_next_token = 1;
    s_leases[owner] = (foreground_lease_t){
        .owner = owner,
        .priority = priority,
        .scene = scene,
        .token = token,
        .acquired_at_us = esp_timer_get_time(),
        .released_at_us = 0,
        .held = true,
    };
    ++s_event_seq;
    taskEXIT_CRITICAL(&s_foreground_lock);
    return token;
}

bool foreground_coordinator_release(foreground_owner_t owner, uint32_t token) {
    if (owner <= FOREGROUND_OWNER_NONE || owner >= FOREGROUND_OWNER_COUNT) return false;
    bool released = false;
    taskENTER_CRITICAL(&s_foreground_lock);
    if (s_leases[owner].held && s_leases[owner].token == token) {
        s_leases[owner].held = false;
        s_leases[owner].released_at_us = esp_timer_get_time();
        ++s_event_seq;
        released = true;
    }
    uint32_t held_token = s_leases[owner].held ? s_leases[owner].token : 0;
    taskEXIT_CRITICAL(&s_foreground_lock);
    if (!released) {
        /* A stale token must never displace a newer owner (appendix section 8).
         * This rejection is decision-layer behavior and is logged even in
         * shadow mode. */
        ESP_LOGW(TAG, "foreground release rejected: owner=%d token=%lu held_token=%lu",
                 (int)owner, (unsigned long)token, (unsigned long)held_token);
    }
    return released;
}

foreground_owner_t foreground_coordinator_current(void) {
    foreground_owner_t owner = FOREGROUND_OWNER_NONE;
    taskENTER_CRITICAL(&s_foreground_lock);
    int top = top_lease_index_locked();
    if (top >= 0) owner = s_leases[top].owner;
    taskEXIT_CRITICAL(&s_foreground_lock);
    return owner;
}

foreground_owner_t foreground_coordinator_expected_restore_after(foreground_owner_t owner) {
    foreground_owner_t restore = FOREGROUND_OWNER_NONE;
    int top_priority = -1;
    uint32_t top_token = 0;
    taskENTER_CRITICAL(&s_foreground_lock);
    for (int i = 0; i < (int)FOREGROUND_OWNER_COUNT; ++i) {
        if (i == (int)owner || !s_leases[i].held) continue;
        if ((int)s_leases[i].priority > top_priority ||
            ((int)s_leases[i].priority == top_priority && s_leases[i].token > top_token)) {
            top_priority = (int)s_leases[i].priority;
            top_token = s_leases[i].token;
            restore = s_leases[i].owner;
        }
    }
    taskEXIT_CRITICAL(&s_foreground_lock);
    return restore;
}

bool foreground_coordinator_get_lease(foreground_owner_t owner,
                                      foreground_lease_t *out_lease) {
    if (owner <= FOREGROUND_OWNER_NONE || owner >= FOREGROUND_OWNER_COUNT || !out_lease) {
        return false;
    }
    taskENTER_CRITICAL(&s_foreground_lock);
    *out_lease = s_leases[owner];
    taskEXIT_CRITICAL(&s_foreground_lock);
    return true;
}

void foreground_coordinator_observe_acquire(foreground_owner_t owner,
                                            foreground_priority_t priority,
                                            foreground_scene_kind_t scene) {
    (void)foreground_coordinator_acquire(owner, priority, scene);
    shadow_compare();
}

void foreground_coordinator_observe_release(foreground_owner_t owner) {
    if (owner <= FOREGROUND_OWNER_NONE || owner >= FOREGROUND_OWNER_COUNT) return;
    bool changed = false;
    taskENTER_CRITICAL(&s_foreground_lock);
    if (s_leases[owner].held) {
        s_leases[owner].held = false;
        s_leases[owner].released_at_us = esp_timer_get_time();
        ++s_event_seq;
        changed = true;
    }
    taskEXIT_CRITICAL(&s_foreground_lock);
    if (changed) shadow_compare();
}

void foreground_coordinator_observe_display_lock(bool locked) {
    if (locked) {
        /* Meeting Service borrows the command display lock for its upload
         * surface; that borrow is not a voice-command lease.  Only a lock
         * without a held meeting lease starts a voice-command foreground. */
        if (foreground_coordinator_current() != FOREGROUND_OWNER_MEETING) {
            foreground_coordinator_observe_acquire(FOREGROUND_OWNER_COMMAND_VOICE,
                                                   FOREGROUND_PRIORITY_CAPTURE,
                                                   FOREGROUND_SCENE_VOICE_COMMAND);
        }
    } else {
        foreground_coordinator_observe_release(FOREGROUND_OWNER_COMMAND_VOICE);
    }
}

void foreground_coordinator_observe_ambient_restored(void) {
    bool changed = false;
    taskENTER_CRITICAL(&s_foreground_lock);
    for (int i = 0; i < (int)FOREGROUND_OWNER_COUNT; ++i) {
        /* A ringing alarm overlays the ambient surface and owns its own
         * precise release point in Alarm Service; an ambient restore
         * elsewhere never retires it. */
        if (i == (int)FOREGROUND_OWNER_ALARM) continue;
        if (s_leases[i].held) {
            s_leases[i].held = false;
            s_leases[i].released_at_us = esp_timer_get_time();
            changed = true;
        }
    }
    if (changed) ++s_event_seq;
    taskEXIT_CRITICAL(&s_foreground_lock);
    if (changed) shadow_compare();
}

void foreground_coordinator_set_authoritative(bool enabled) {
    taskENTER_CRITICAL(&s_foreground_lock);
    s_authoritative = enabled;
    taskEXIT_CRITICAL(&s_foreground_lock);
}

bool foreground_coordinator_is_authoritative(void) {
    bool authoritative;
    taskENTER_CRITICAL(&s_foreground_lock);
    authoritative = s_authoritative;
    taskEXIT_CRITICAL(&s_foreground_lock);
    return authoritative;
}

device_status_t foreground_coordinator_init(void) {
    taskENTER_CRITICAL(&s_foreground_lock);
    for (int i = 0; i < (int)FOREGROUND_OWNER_COUNT; ++i) {
        s_leases[i] = (foreground_lease_t){.owner = (foreground_owner_t)i};
    }
    s_next_token = 1;
    s_event_seq = 0;
    s_authoritative = false;
    s_divergence_open = false;
    s_last_actual = -1;
    s_last_expected = -1;
    s_last_owner = FOREGROUND_OWNER_NONE;
    s_last_scene = FOREGROUND_SCENE_AMBIENT;
    taskEXIT_CRITICAL(&s_foreground_lock);
    return DEVICE_STATUS_OK;
}
