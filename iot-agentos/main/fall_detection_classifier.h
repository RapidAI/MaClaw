#pragma once

/*
 * Platform-neutral suspected-fall evidence classifier.
 *
 * This value core consumes only normalized Device Motion samples.  It owns no
 * task, timer, persistence, UI, gateway, sensor handle or escalation policy;
 * the Fall Detection service owns those lifecycle concerns.  A timestamp gap
 * is a hard evidence boundary: samples separated by a stalled shared bus,
 * profile restart or scheduler starvation must never be combined into one
 * suspected event.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

#define FALL_DETECTION_CLASSIFIER_MAX_INTER_SAMPLE_GAP_US 500000u

typedef enum {
    FALL_DETECTION_CLASSIFIER_MONITORING = 0,
    FALL_DETECTION_CLASSIFIER_FREEFALL,
    FALL_DETECTION_CLASSIFIER_POST_IMPACT,
} fall_detection_classifier_state_t;

typedef struct {
    bool have_baseline;
    int32_t baseline_x;
    int32_t baseline_y;
    int32_t baseline_z;
    uint64_t last_timestamp_us;
    uint64_t freefall_start_us;
    uint64_t impact_us;
    uint64_t still_start_us;
    bool orientation_changed;
    fall_detection_classifier_state_t state;
} fall_detection_classifier_t;

void fall_detection_classifier_reset(fall_detection_classifier_t *classifier);

/* Returns true only for a complete freefall → impact → orientation-change →
 * stillness sequence. Invalid, stale or discontinuous input clears partial
 * evidence and returns false; callers may continue monitoring with a fresh
 * chronological stream. */
bool fall_detection_classifier_observe(fall_detection_classifier_t *classifier,
                                       const device_motion_sample_t *sample);
