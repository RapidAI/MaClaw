#pragma once

/*
 * Presentation-layer semantic scene model (A7 second increment).
 *
 * Business services publish rebuildable semantic fields; board renderers map
 * those fields onto round/rect/small-panel geometry.  This header carries no
 * pixel state, framebuffer pointers, GPIO, touch coordinates or board
 * identity.  Field sizes match the shared App UI model so a presenter can
 * forward them without truncation or layout invention.
 *
 * Authoritative foreground switching remains with Foreground Coordinator
 * (still shadow-mode).  This model is the typed payload that a later
 * presenter/coordinator cut-over will consume.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

/* Semantic scene kinds.  Numeric order matches foreground_scene_kind_t so a
 * later coordinator authoritative switch can share the same vocabulary. */
typedef enum {
    SCENE_KIND_AMBIENT = 0,
    SCENE_KIND_STARTUP_SPLASH,
    SCENE_KIND_SETUP_PORTAL,
    SCENE_KIND_VOICE_COMMAND,
    SCENE_KIND_COMMAND_MESSAGE,
    SCENE_KIND_COMMAND_RESULT,
    SCENE_KIND_MEETING_RECORD,
    SCENE_KIND_MEETING_UPLOAD,
    SCENE_KIND_MEETING_RESULT,
    SCENE_KIND_ALARM_RING,
} scene_kind_t;

#define SCENE_AMBIENT_TIME_CAPACITY 16u
#define SCENE_AMBIENT_LOCATION_CAPACITY 24u
#define SCENE_AMBIENT_DATE_CAPACITY 24u
#define SCENE_AMBIENT_WEEKDAY_CAPACITY 24u
#define SCENE_AMBIENT_WEATHER_CAPACITY 32u
#define SCENE_NETWORK_SSID_CAPACITY 64u
#define SCENE_PET_STATE_CAPACITY 16u
#define SCENE_PET_SKIN_CAPACITY 32u
#define SCENE_GLYPH_BITMAP_BYTES 72u
#define SCENE_COMMAND_STAGE_CAPACITY 32u
#define SCENE_ALARM_TIME_CAPACITY 16u
#define SCENE_ALARM_LABEL_CAPACITY 64u
#define SCENE_MESSAGE_TITLE_CAPACITY 64u
#define SCENE_MESSAGE_BODY_CAPACITY 2048u
#define SCENE_UPLOAD_STAGE_CAPACITY 32u

/* Standby/ambient fields.  Always-forward semantics: a high-priority
 * foreground may own the pixels, but the model must still accept the latest
 * time/weather so the first restored standby frame is not stale. */
typedef struct {
    char time[SCENE_AMBIENT_TIME_CAPACITY];
    char location[SCENE_AMBIENT_LOCATION_CAPACITY];
    char date[SCENE_AMBIENT_DATE_CAPACITY];
    char weekday[SCENE_AMBIENT_WEEKDAY_CAPACITY];
    char weather[SCENE_AMBIENT_WEATHER_CAPACITY];
    int temperature_c;
    bool weather_valid;
    bool weather_stale;
} scene_ambient_fields_t;

/* Standby network label.  Always-forward: a command/setup/alarm surface may
 * own the pixels, but the model still accepts the latest transport state for
 * the first restored standby frame. */
typedef struct {
    char ssid[SCENE_NETWORK_SSID_CAPACITY];
    bool connected;
} scene_network_fields_t;

/* Standby pet identity.  Transient emotions (speaking/thinking/alert) are
 * published event-driven through the presenter and must not be republished
 * by the clock cadence, or they would overwrite Command/Reply/Meeting. */
typedef struct {
    char state[SCENE_PET_STATE_CAPACITY];
} scene_pet_state_fields_t;

typedef struct {
    char skin[SCENE_PET_SKIN_CAPACITY];
    bool motion_enabled;
} scene_pet_profile_fields_t;

typedef struct {
    bool meeting;
} scene_recording_mode_fields_t;

typedef struct {
    bool active;
    bool paused;
    uint32_t elapsed_seconds;
} scene_recording_visual_fields_t;

typedef struct {
    bool active;
    unsigned frame;
    char time_text[SCENE_ALARM_TIME_CAPACITY];
    char label[SCENE_ALARM_LABEL_CAPACITY];
    unsigned attempt;
    unsigned max_attempts;
} scene_alarm_visual_fields_t;

/* Message/reply/upload titles match App UI replay copies.  Presenter still
 * forwards borrowed pointers; App UI owns the durable replay buffers. */
typedef struct {
    char title[SCENE_MESSAGE_TITLE_CAPACITY];
    char body[SCENE_MESSAGE_BODY_CAPACITY];
} scene_message_fields_t;

typedef struct {
    size_t completed_bytes;
    size_t total_bytes;
    char stage[SCENE_UPLOAD_STAGE_CAPACITY];
} scene_upload_progress_fields_t;
