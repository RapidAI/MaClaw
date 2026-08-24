#pragma once

/*
 * Foreground & Interruption Coordinator (A6 second increment, SHADOW mode).
 *
 * Implements the foreground lease / priority / resume-token decision model of
 * the business appendix (section 8): which owner should hold the foreground,
 * and which owner the foreground should restore to when it ends.  In this
 * increment the coordinator runs in shadow mode only: it observes the same
 * domain events that drive surface transitions, recomputes its decision and
 * logs divergences from the actual App UI surface.  It changes no pixel,
 * input route, audio focus or timing; every observe call is append-only.
 *
 * The public contract exposes value types only.  No ESP-IDF error codes,
 * FreeRTOS handles, JSON objects or framebuffers cross this boundary; scene
 * snapshots are small enums, never pixel state.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

/* Foreground owner identity.  One lease slot per owner. */
typedef enum {
    FOREGROUND_OWNER_NONE = 0,      /* no lease: ambient PET owns the panel */
    FOREGROUND_OWNER_STARTUP,       /* boot splash / startup sequence */
    FOREGROUND_OWNER_SETUP,         /* provisioning portal (config recovery) */
    FOREGROUND_OWNER_COMMAND_VOICE, /* voice command capture/processing */
    FOREGROUND_OWNER_COMMAND_RESULT,/* completed command result page + TTS */
    FOREGROUND_OWNER_MEETING,       /* meeting record/upload/result */
    FOREGROUND_OWNER_ALARM,         /* locally due ringing alarm */
    FOREGROUND_OWNER_UPDATE,        /* update reminder (reserved) */
    FOREGROUND_OWNER_COUNT
} foreground_owner_t;

/* Higher value preempts lower (appendix section 8.2 order). */
typedef enum {
    FOREGROUND_PRIORITY_AMBIENT = 0,  /* Ambient / PET */
    FOREGROUND_PRIORITY_PROGRESS,     /* upload / processing progress */
    FOREGROUND_PRIORITY_RESULT,       /* command / meeting result and TTS */
    FOREGROUND_PRIORITY_CAPTURE,      /* command / meeting capture */
    FOREGROUND_PRIORITY_RECOVERY,     /* config recovery / irreversible error */
    FOREGROUND_PRIORITY_ALARM_DUE,    /* locally due Alarm */
    FOREGROUND_PRIORITY_SAFETY,       /* safety-forced */
} foreground_priority_t;

/* Semantic scene snapshot: rebuildable semantics, never pixel state. */
typedef enum {
    FOREGROUND_SCENE_AMBIENT = 0,
    FOREGROUND_SCENE_STARTUP_SPLASH,
    FOREGROUND_SCENE_SETUP_PORTAL,
    FOREGROUND_SCENE_VOICE_COMMAND,   /* capture + thinking (locked surface) */
    FOREGROUND_SCENE_COMMAND_MESSAGE, /* transient error/cancelled message */
    FOREGROUND_SCENE_COMMAND_RESULT,  /* response page + result speech */
    FOREGROUND_SCENE_MEETING_RECORD,
    FOREGROUND_SCENE_MEETING_UPLOAD,
    FOREGROUND_SCENE_MEETING_RESULT,
    FOREGROUND_SCENE_ALARM_RING,
} foreground_scene_kind_t;

/* Lease record (value snapshot of one slot). */
typedef struct {
    foreground_owner_t owner;
    foreground_priority_t priority;
    foreground_scene_kind_t scene;
    uint32_t token;          /* resume token: monotonic lease generation */
    int64_t acquired_at_us;
    int64_t released_at_us;  /* 0 while held */
    bool held;
} foreground_lease_t;

device_status_t foreground_coordinator_init(void);

/* Decision API.  acquire returns the new lease token (monotonic, never 0).
 * release rejects a stale token: it fails and logs, it never removes a newer
 * owner's lease.  current() returns the highest-priority held lease owner
 * (FOREGROUND_OWNER_NONE means ambient).  expected_restore_after() computes
 * who should own the foreground once `owner` releases. */
uint32_t foreground_coordinator_acquire(foreground_owner_t owner,
                                        foreground_priority_t priority,
                                        foreground_scene_kind_t scene);
bool foreground_coordinator_release(foreground_owner_t owner, uint32_t token);
foreground_owner_t foreground_coordinator_current(void);
foreground_owner_t foreground_coordinator_expected_restore_after(foreground_owner_t owner);
bool foreground_coordinator_get_lease(foreground_owner_t owner,
                                      foreground_lease_t *out_lease);

/* Shadow observation entry points.  Append-only domain-event feeds from the
 * business services and App UI; each recomputes the decision and runs the
 * divergence comparison against the actual App UI surface. */
void foreground_coordinator_observe_acquire(foreground_owner_t owner,
                                            foreground_priority_t priority,
                                            foreground_scene_kind_t scene);
void foreground_coordinator_observe_release(foreground_owner_t owner);
void foreground_coordinator_observe_display_lock(bool locked);
void foreground_coordinator_observe_ambient_restored(void);

/* Reserved switch for the later authoritative cut-over.  Compile-time
 * default is false and nothing in this increment enables it; the flag is
 * inert by design in shadow mode. */
void foreground_coordinator_set_authoritative(bool enabled);
bool foreground_coordinator_is_authoritative(void);
