#include <stdbool.h>
#include <stdio.h>

#include "fall_detection_classifier.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static device_motion_sample_t sample_at(uint64_t timestamp_us, int32_t x,
                                        int32_t y, int32_t z, int32_t gyro) {
    return (device_motion_sample_t){
        .struct_size = sizeof(device_motion_sample_t),
        .abi_version = DEVICE_MOTION_SAMPLE_ABI_VERSION,
        .timestamp_us = timestamp_us,
        .acceleration_mg_x = x,
        .acceleration_mg_y = y,
        .acceleration_mg_z = z,
        .angular_rate_mdps_x = gyro,
        .angular_rate_mdps_y = 0,
        .angular_rate_mdps_z = 0,
    };
}

static bool observe(fall_detection_classifier_t *classifier, uint64_t timestamp_us,
                    int32_t x, int32_t y, int32_t z, int32_t gyro) {
    const device_motion_sample_t sample = sample_at(timestamp_us, x, y, z, gyro);
    return fall_detection_classifier_observe(classifier, &sample);
}

static bool establish_complete_fall(fall_detection_classifier_t *classifier,
                                    uint64_t start_us) {
    if (observe(classifier, start_us, 0, 0, 1000, 0)) return false;
    if (observe(classifier, start_us + 100000u, 0, 0, 100, 0)) return false;
    if (observe(classifier, start_us + 250000u, 0, 0, 3000, 200000)) return false;

    for (uint64_t elapsed_us = 350000u; elapsed_us <= 3350000u;
         elapsed_us += 250000u) {
        const bool detected = observe(classifier, start_us + elapsed_us,
                                      0, 0, -1000, 0);
        if (elapsed_us < 3350000u && detected) return false;
        if (elapsed_us == 3350000u) return detected;
    }
    return false;
}

int main(void) {
    fall_detection_classifier_t classifier = {0};

    fall_detection_classifier_reset(&classifier);
    CHECK(establish_complete_fall(&classifier, 100000u));
    CHECK(!observe(&classifier, 3600000u, 0, 0, -1000, 0));

    fall_detection_classifier_reset(&classifier);
    CHECK(!observe(&classifier, 100000u, 0, 0, 1000, 0));
    CHECK(!observe(&classifier, 200000u, 0, 0, 100, 0));
    CHECK(!observe(&classifier, 800001u, 0, 0, 3000, 200000));
    CHECK(!observe(&classifier, 900000u, 0, 0, -1000, 0));

    fall_detection_classifier_reset(&classifier);
    CHECK(!observe(&classifier, 100000u, 0, 0, 1000, 0));
    CHECK(!observe(&classifier, 200000u, 0, 0, 100, 0));
    CHECK(!observe(&classifier, 350000u, 0, 0, 3000, 200000));
    CHECK(!observe(&classifier, 900001u, 0, 0, -1000, 0));
    for (uint64_t elapsed_us = 1000000u; elapsed_us <= 4200000u;
         elapsed_us += 250000u) {
        CHECK(!observe(&classifier, elapsed_us, 0, 0, -1000, 0));
    }

    fall_detection_classifier_reset(&classifier);
    CHECK(!observe(&classifier, 100000u, 0, 0, 1000, 0));
    CHECK(!observe(&classifier, 200000u, 0, 0, 100, 0));
    CHECK(!observe(&classifier, 200000u, 0, 0, 3000, 200000));
    CHECK(!observe(&classifier, 300000u, 0, 0, 3000, 200000));
    CHECK(!observe(&classifier, 250000u, 0, 0, -1000, 0));

    fall_detection_classifier_reset(&classifier);
    CHECK(!observe(&classifier, 100000u, 0, 0, 1000, 0));
    CHECK(!observe(&classifier, 200000u, 0, 0, 100, 0));
    device_motion_sample_t invalid = sample_at(300000u, 0, 0, 3000, 0);
    invalid.abi_version++;
    CHECK(!fall_detection_classifier_observe(&classifier, &invalid));
    invalid = sample_at(300000u, 0, 0, 3000, 0);
    invalid.timestamp_us = 0;
    CHECK(!fall_detection_classifier_observe(&classifier, &invalid));
    CHECK(!observe(&classifier, 400000u, 0, 0, 3000, 0));

    fall_detection_classifier_reset(&classifier);
    CHECK(!observe(&classifier, 100000u, 0, 0, 1000, 0));
    CHECK(!observe(&classifier, 250000u, 0, 0, 3000, 200000));
    for (uint64_t elapsed_us = 350000u; elapsed_us <= 4000000u;
         elapsed_us += 250000u) {
        CHECK(!observe(&classifier, elapsed_us, 0, 0, -1000, 0));
    }

    puts("PASS fall classifier requires one contiguous freefall/impact/orientation/stillness sequence");
    return 0;
}
