#pragma once

/*
 * Ambient Service (A9 first increment).
 *
 * Owns the standby time/weather aggregation that used to live in main.c: the
 * in-memory weather model, NVS cache load/save via weather_cache_service, the
 * display-clock (SNTP/Hub epoch + monotonic advance) and the once-per-second
 * cadence worker that republishes the ambient scene.
 *
 * Hub ambient/glyph JSON decode lives here; pet asset download/cache workers
 * and the SNTP monitor remain composition-root / Connectivity concerns.
 * Callers pass borrowed Hub JSON as an opaque pointer (parser types stay
 * in the .c file).  Dynamic glyph *ownership* (D4 per-entry records) stays in the
 * renderer; this service only forwards decoded bitmaps.
 *
 * Presentation goes through Scene Presenter (A7); Ambient Service does not
 * call app_ui or board ports, and never covers a high-priority foreground
 * (App UI already stores ambient updates behind an owned surface).
 *
 * The public contract exposes value types only: no ESP-IDF error codes,
 * FreeRTOS handles or JSON objects.
 */

#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "device_api.h"
#include "weather_cache_service.h"

/* Creates the start/stop mutex and sets the display timezone once. */
device_status_t ambient_service_init(void);

/* Restore the last persisted weather snapshot into the in-memory model.
 * Does not publish a scene; the cadence worker publishes once it starts. */
void ambient_service_load(void);

/* Replace the in-memory weather model.  Persistence is a separate call so a
 * PSRAM-stack long-poll worker can defer NVS writes exactly as before. */
void ambient_service_apply_weather(const weather_cache_snapshot_t *weather);
void ambient_service_persist_weather(void);

/* Authenticated Hub time or SNTP supplied a trusted epoch.  Updates the
 * display clock; SNTP-complete and deadline dispatch stay with the caller. */
void ambient_service_note_wall_clock(int64_t epoch_sec);

/* Standby network label and scheduled-alarm marker.  Published immediately
 * (not deferred to the clock cadence) so event-driven App UI updates keep
 * the same timing as before this increment. */
void ambient_service_apply_network(const char *ssid, bool connected);
void ambient_service_apply_alarm_scheduled(bool scheduled);

/* Standby pet identity.  Published immediately.  Transient emotions also
 * enter through this path so presenter remains the sole App UI caller, but
 * the clock cadence never republishes pet_state/profile. */
void ambient_service_apply_pet_state(const char *state);
void ambient_service_apply_pet_profile(const char *skin, bool motion_enabled);

/* Idempotent start of the once-per-second ambient cadence worker.  A caller
 * that needs a proven local feedback path (SAFE_MODE) receives its creation
 * result; ordinary clock sources may deliberately ignore it and continue
 * without a standby cadence. */
device_status_t ambient_service_ensure_clock_task(void);

/* Bounded stop used by the lifecycle registry / startup rollback. */
device_status_t ambient_service_stop_clock_task(uint32_t timeout_ms);

/* Future System Sleep participant for the once-per-second standby cadence.
 * PREPARE records and stops only a worker already running before the request;
 * it returns BUSY while a new worker is still in its private create/publish
 * handshake. ABORT recreates only that recorded worker. It never changes
 * ambient state, display ownership, or the selected hardware profile. */
device_status_t ambient_service_prepare_system_sleep(uint32_t timeout_ms);
void ambient_service_abort_system_sleep_prepare(void);

/* Hub "U+XXXX" glyph object keys.  Rejects surrogates and non-BMP. */
static inline bool ambient_service_parse_glyph_key(const char *key, uint32_t *codepoint) {
    if (!key || !codepoint || strlen(key) != 6 || key[0] != 'U' || key[1] != '+') return false;
    char *end = NULL;
    unsigned long value = strtoul(key + 2, &end, 16);
    if (!end || *end || value < 0x20 || value > 0xFFFF ||
        (value >= 0xD800 && value <= 0xDFFF)) return false;
    *codepoint = (uint32_t)value;
    return true;
}

/* Borrowed Hub JSON objects from the composition root.  Glyphs decode to
 * presenter bitmaps; weather updates the in-memory model.  NVS persist
 * follows the caller's task stack (PSRAM-stack poll defers). */
int ambient_service_apply_hub_glyphs(const void *glyphs_json);
void ambient_service_apply_hub_ambient(const void *ambient_json);
