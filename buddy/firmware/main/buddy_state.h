#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

#define BUDDY_MAX_TASKS 5
#define BUDDY_TITLE_BYTES 64
#define BUDDY_LABEL_BYTES 32
#define BUDDY_PHASE_BYTES 48
#define BUDDY_ELAPSED_BYTES 16

typedef struct {
    char id[24];
    char title[BUDDY_TITLE_BYTES + 1];
    char phase[BUDDY_PHASE_BYTES + 1];
    char elapsed[BUDDY_ELAPSED_BYTES + 1];
    float progress;  // -1 means "unknown"
} buddy_task_t;

typedef struct {
    int total;
    int in_progress;
    int pending;
    int paused;
    int done;
    int failed;
} buddy_counts_t;

typedef struct {
    char kind[12];
    char id[24];
    char title[BUDDY_TITLE_BYTES + 1];
} buddy_attention_t;

typedef struct {
    bool has_state;
    uint64_t state_seq;
    buddy_counts_t counts;
    int current_count;
    buddy_task_t current[BUDDY_MAX_TASKS];
    buddy_attention_t attention;
    bool stale;

    bool has_weather;
    bool weather_stale;
    char weather_summary[BUDDY_LABEL_BYTES + 1];
    int weather_temp;
    char weather_location[BUDDY_LABEL_BYTES + 1];

    bool has_clock;
    char date[24];
    char weekday[16];

    bool celebrate_active;
    char celebrate_title[BUDDY_TITLE_BYTES + 1];
    int64_t celebrate_start_ms;
    int celebrate_seconds;

    bool online;
    int64_t last_host_ms;
    bool prompt_active;
    char prompt[96];
    /* -1 derives the pet mood from the page; otherwise a BUDDY_MOOD_* value. */
    int pet_explicit;

    int missing_glyphs;
} buddy_view_t;

void buddy_state_init(void);
void buddy_state_lock(void);
void buddy_state_unlock(void);
void buddy_state_get(buddy_view_t *out);

/** Records that a host frame arrived; also clears prompt state on hello. */
void buddy_state_note_host(void);
void buddy_state_apply_state(const buddy_counts_t *counts, const buddy_task_t *current,
                             int current_count, const buddy_attention_t *attention, bool stale,
                             uint64_t seq);
/** Applies a weather report. Returns true when the stored summary, location,
 * or temperature changed. A stale-bit flip alone returns false. */
bool buddy_state_apply_weather(const char *summary, int temperature_c, const char *location,
                               bool stale);
void buddy_state_apply_clock(const char *date, const char *weekday);
/** Starts the task-completed celebration page for `seconds` (clamped 2..10). */
void buddy_state_apply_celebrate(const char *title, int seconds);
void buddy_state_set_prompt(const char *text);
void buddy_state_clear_prompt(void);
/** Stores a host pet hint. Empty or unknown clears it back to automatic. */
void buddy_state_set_pet(const char *state);
void buddy_state_add_missing(int count);

/** Recomputes online/stale; call from the UI tick. */
void buddy_state_tick(void);

/** Persists/restores the last weather snapshot in NVS (namespace "buddy"). */
bool buddy_state_weather_load(void);
void buddy_state_weather_save(void);

/** Seconds since boot, for the device heartbeat. */
uint32_t buddy_state_uptime_s(void);
