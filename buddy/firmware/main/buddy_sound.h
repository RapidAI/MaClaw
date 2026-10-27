#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"

enum {
    BUDDY_SOUND_RUNNING = 0,
    BUDDY_SOUND_COMPLETE = 1,
    BUDDY_SOUND_STANDBY = 2,
    BUDDY_SOUND_EVENT_COUNT = 3,
};

typedef struct {
    bool master;
    bool enabled[BUDDY_SOUND_EVENT_COUNT];
    int effect[BUDDY_SOUND_EVENT_COUNT];
} buddy_sound_config_t;

/** Initializes I2S + ES8311 + PA and the playback task. Missing audio
 * hardware is not fatal: the settings still persist and the device runs
 * silently. */
esp_err_t buddy_sound_init(void);

/** True when the codec/I2S path initialized successfully. */
bool buddy_sound_available(void);

/** Stores and persists the host-selected sound configuration. */
void buddy_sound_set_config(const buddy_sound_config_t *cfg);
void buddy_sound_get_config(buddy_sound_config_t *out);

/** Plays the configured effect for an event if the master and event switches
 * are on. Never blocks. */
void buddy_sound_event(int event);
