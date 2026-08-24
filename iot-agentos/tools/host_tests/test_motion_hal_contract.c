#include <stdio.h>
#include <string.h>
#include <limits.h>

#include "motion_service.h"
#include "platform_sensor_profile.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static bool s_has_motion_capability;
static device_status_t s_profile_status;
static device_motion_sample_t s_profile_sample;
static unsigned s_profile_calls;

bool device_profile_has_capability(device_capability_flags_t capability) {
    return capability == DEVICE_CAPABILITY_MOTION_SENSOR && s_has_motion_capability;
}

device_status_t platform_sensor_profile_get_motion_sample(
    device_motion_sample_t *out_sample) {
    CHECK(out_sample != NULL);
    ++s_profile_calls;
    *out_sample = s_profile_sample;
    return s_profile_status;
}

static void reset_profile(void) {
    s_has_motion_capability = true;
    s_profile_status = DEVICE_STATUS_OK;
    s_profile_sample = (device_motion_sample_t){
        .struct_size = sizeof(s_profile_sample),
        .abi_version = DEVICE_MOTION_SAMPLE_ABI_VERSION,
        .timestamp_us = 42,
        .acceleration_mg_x = 1,
        .acceleration_mg_y = 2,
        .acceleration_mg_z = 1000,
        .angular_rate_mdps_x = 3,
        .angular_rate_mdps_y = 4,
        .angular_rate_mdps_z = 5,
    };
    s_profile_calls = 0;
}

int main(void) {
    device_motion_sample_t out = {0};

    CHECK(motion_service_get_sample(NULL) == DEVICE_STATUS_INVALID_ARGUMENT);

    reset_profile();
    s_has_motion_capability = false;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_UNAVAILABLE);
    CHECK(s_profile_calls == 0);

    reset_profile();
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_OK);
    CHECK(s_profile_calls == 1);
    CHECK(memcmp(&out, &s_profile_sample, sizeof(out)) == 0);

    reset_profile();
    s_profile_sample.timestamp_us = 43;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_OK);
    CHECK(out.timestamp_us == 43);

    reset_profile();
    s_profile_sample.timestamp_us = 43;
    out.timestamp_us = 0xA5A5u;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_IO_ERROR);
    CHECK(out.timestamp_us == 0xA5A5u);

    reset_profile();
    s_profile_sample.timestamp_us = 42;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_IO_ERROR);

    reset_profile();
    s_profile_sample.timestamp_us = 44;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_OK);

    reset_profile();
    out.timestamp_us = 0xA5A5u;
    s_profile_sample.struct_size--;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_IO_ERROR);
    CHECK(out.timestamp_us == 0xA5A5u);

    reset_profile();
    s_profile_sample.abi_version++;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_IO_ERROR);

    reset_profile();
    s_profile_sample.timestamp_us = 0;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_IO_ERROR);

    reset_profile();
    s_profile_sample.timestamp_us = (uint64_t)INT64_MAX + 1u;
    out.timestamp_us = 0xA5A5u;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_IO_ERROR);
    CHECK(out.timestamp_us == 0xA5A5u);

    reset_profile();
    s_profile_sample.timestamp_us = 46;
    s_profile_sample.acceleration_mg_x = 32001;
    out.timestamp_us = 0xA5A5u;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_IO_ERROR);
    CHECK(out.timestamp_us == 0xA5A5u);

    reset_profile();
    s_profile_sample.timestamp_us = 46;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_OK);

    reset_profile();
    s_profile_sample.timestamp_us = 47;
    s_profile_sample.angular_rate_mdps_z = INT32_MAX;
    out.timestamp_us = 0xA5A5u;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_IO_ERROR);
    CHECK(out.timestamp_us == 0xA5A5u);

    reset_profile();
    s_profile_sample.timestamp_us = 47;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_OK);

    reset_profile();
    s_profile_status = DEVICE_STATUS_TIMEOUT;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_TIMEOUT);

    reset_profile();
    s_profile_sample.timestamp_us = 48;
    CHECK(motion_service_get_sample(&out) == DEVICE_STATUS_OK);

    puts("PASS Motion HAL accepts only complete normalized samples");
    return 0;
}
