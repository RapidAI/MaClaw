#include "fall_detection_classifier.h"

#include <limits.h>

/* Conservative engineering defaults, not medical thresholds. */
#define FALL_FREEFALL_MAGNITUDE_MG 350
#define FALL_FREEFALL_MIN_US 100000u
#define FALL_IMPACT_MAGNITUDE_MG 2500
#define FALL_IMPACT_WINDOW_US 1500000u
#define FALL_STILL_MAGNITUDE_MIN_MG 800
#define FALL_STILL_MAGNITUDE_MAX_MG 1200
#define FALL_STILL_MAX_GYRO_MDPS 100000
#define FALL_STILL_MIN_US 3000000u
#define FALL_ORIENTATION_COS_PERMILLE 707

static int64_t square_i32(int32_t value) {
    const int64_t wide = value;
    return wide * wide;
}

static int64_t acceleration_magnitude_squared(const device_motion_sample_t *sample) {
    return square_i32(sample->acceleration_mg_x) +
           square_i32(sample->acceleration_mg_y) +
           square_i32(sample->acceleration_mg_z);
}

static bool magnitude_is_between(const device_motion_sample_t *sample,
                                 int32_t minimum_mg, int32_t maximum_mg) {
    const int64_t value = acceleration_magnitude_squared(sample);
    return value >= square_i32(minimum_mg) && value <= square_i32(maximum_mg);
}

static int32_t max_abs_gyro_mdps(const device_motion_sample_t *sample) {
    int64_t x = sample->angular_rate_mdps_x;
    int64_t y = sample->angular_rate_mdps_y;
    int64_t z = sample->angular_rate_mdps_z;
    if (x < 0) x = -x;
    if (y < 0) y = -y;
    if (z < 0) z = -z;
    int64_t maximum = x > y ? x : y;
    if (z > maximum) maximum = z;
    return maximum > INT32_MAX ? INT32_MAX : (int32_t)maximum;
}

static bool is_still(const device_motion_sample_t *sample) {
    return magnitude_is_between(sample, FALL_STILL_MAGNITUDE_MIN_MG,
                                FALL_STILL_MAGNITUDE_MAX_MG) &&
           max_abs_gyro_mdps(sample) <= FALL_STILL_MAX_GYRO_MDPS;
}

static void update_baseline_if_stable(fall_detection_classifier_t *classifier,
                                      const device_motion_sample_t *sample) {
    if (!is_still(sample)) return;
    classifier->baseline_x = sample->acceleration_mg_x;
    classifier->baseline_y = sample->acceleration_mg_y;
    classifier->baseline_z = sample->acceleration_mg_z;
    classifier->have_baseline = true;
}

static bool orientation_changed_from_baseline(
    const fall_detection_classifier_t *classifier,
    const device_motion_sample_t *sample) {
    if (!classifier->have_baseline) return false;
    const int32_t baseline_x = classifier->baseline_x / 32;
    const int32_t baseline_y = classifier->baseline_y / 32;
    const int32_t baseline_z = classifier->baseline_z / 32;
    const int32_t current_x = sample->acceleration_mg_x / 32;
    const int32_t current_y = sample->acceleration_mg_y / 32;
    const int32_t current_z = sample->acceleration_mg_z / 32;
    const int64_t dot = (int64_t)baseline_x * current_x +
                        (int64_t)baseline_y * current_y +
                        (int64_t)baseline_z * current_z;
    if (dot <= 0) return true;
    const int64_t baseline_sq = square_i32(baseline_x) + square_i32(baseline_y) +
                                square_i32(baseline_z);
    const int64_t current_sq = square_i32(current_x) + square_i32(current_y) +
                               square_i32(current_z);
    if (baseline_sq == 0 || current_sq == 0) return false;
    const int64_t left = dot * 1000LL;
    const int64_t right_sq = (int64_t)FALL_ORIENTATION_COS_PERMILLE *
                             FALL_ORIENTATION_COS_PERMILLE * baseline_sq * current_sq;
    return left * left <= right_sq;
}

static bool sample_is_complete(const device_motion_sample_t *sample) {
    return sample && sample->struct_size == sizeof(*sample) &&
           sample->abi_version == DEVICE_MOTION_SAMPLE_ABI_VERSION &&
           sample->timestamp_us != 0u;
}

void fall_detection_classifier_reset(fall_detection_classifier_t *classifier) {
    if (!classifier) return;
    classifier->state = FALL_DETECTION_CLASSIFIER_MONITORING;
    classifier->freefall_start_us = 0u;
    classifier->impact_us = 0u;
    classifier->still_start_us = 0u;
    classifier->orientation_changed = false;
}

bool fall_detection_classifier_observe(fall_detection_classifier_t *classifier,
                                       const device_motion_sample_t *sample) {
    if (!classifier || !sample_is_complete(sample)) {
        if (classifier) fall_detection_classifier_reset(classifier);
        return false;
    }
    if (classifier->last_timestamp_us != 0u &&
        (sample->timestamp_us <= classifier->last_timestamp_us ||
         sample->timestamp_us - classifier->last_timestamp_us >
             FALL_DETECTION_CLASSIFIER_MAX_INTER_SAMPLE_GAP_US)) {
        /* Keep only a new baseline after discontinuity; all partial fall
         * evidence is tied to the previous contiguous motion stream. */
        fall_detection_classifier_reset(classifier);
        classifier->have_baseline = false;
    }
    classifier->last_timestamp_us = sample->timestamp_us;

    const uint64_t now_us = sample->timestamp_us;
    const int64_t magnitude_sq = acceleration_magnitude_squared(sample);
    const bool freefall = magnitude_sq <= square_i32(FALL_FREEFALL_MAGNITUDE_MG);
    const bool impact = magnitude_sq >= square_i32(FALL_IMPACT_MAGNITUDE_MG);

    switch (classifier->state) {
        case FALL_DETECTION_CLASSIFIER_MONITORING:
            update_baseline_if_stable(classifier, sample);
            if (freefall) {
                classifier->state = FALL_DETECTION_CLASSIFIER_FREEFALL;
                classifier->freefall_start_us = now_us;
            }
            break;
        case FALL_DETECTION_CLASSIFIER_FREEFALL:
            if (impact && now_us - classifier->freefall_start_us >= FALL_FREEFALL_MIN_US) {
                classifier->state = FALL_DETECTION_CLASSIFIER_POST_IMPACT;
                classifier->impact_us = now_us;
                classifier->still_start_us = 0u;
                classifier->orientation_changed = false;
            } else if (!freefall || now_us - classifier->freefall_start_us >
                                      FALL_IMPACT_WINDOW_US) {
                fall_detection_classifier_reset(classifier);
                update_baseline_if_stable(classifier, sample);
            }
            break;
        case FALL_DETECTION_CLASSIFIER_POST_IMPACT:
            if (now_us - classifier->impact_us >
                FALL_IMPACT_WINDOW_US + FALL_STILL_MIN_US) {
                fall_detection_classifier_reset(classifier);
                update_baseline_if_stable(classifier, sample);
                break;
            }
            if (is_still(sample)) {
                if (orientation_changed_from_baseline(classifier, sample)) {
                    classifier->orientation_changed = true;
                }
                if (classifier->still_start_us == 0u) {
                    classifier->still_start_us = now_us;
                }
                if (classifier->orientation_changed &&
                    now_us - classifier->still_start_us >= FALL_STILL_MIN_US) {
                    fall_detection_classifier_reset(classifier);
                    return true;
                }
            } else {
                classifier->still_start_us = 0u;
            }
            break;
    }
    return false;
}
