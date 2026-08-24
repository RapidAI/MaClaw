#include "motion_service.h"

#include <limits.h>

#include "platform_sensor.h"

/* A profile adapter owns unit conversion and its controller error mapping,
 * but a successful Device API result must still be a complete by-value ABI
 * record.  Keep that validation above every profile so a new IMU cannot hand
 * an uninitialised/partial sample to shared fall policy merely by returning
 * success from its private driver wrapper.  No universal acceleration/gyro
 * range is imposed here: the public contract deliberately permits different
 * full-scale selections once translated into engineering units. */
/* Device Motion uses fixed-width integer engineering units.  The shared Fall
 * classifier squares acceleration components and combines scaled directions,
 * so accept only values which cannot overflow those normalised calculations.
 * This is a public value envelope, not a QMI/controller full-scale setting:
 * adapters remain responsible for their own raw range and conversion. */
#define DEVICE_MOTION_ACCELERATION_COMPONENT_ABS_MAX_MG 32000
#define DEVICE_MOTION_ANGULAR_RATE_COMPONENT_ABS_MAX_MDPS 1000000000

static bool motion_component_is_within_abs_limit(int32_t value, int32_t limit) {
    return value >= -limit && value <= limit;
}

static bool motion_sample_is_valid(const device_motion_sample_t *sample) {
    return sample && sample->struct_size == sizeof(*sample) &&
           sample->abi_version == DEVICE_MOTION_SAMPLE_ABI_VERSION &&
           sample->timestamp_us != 0 &&
           sample->timestamp_us <= (uint64_t)INT64_MAX &&
           motion_component_is_within_abs_limit(
               sample->acceleration_mg_x,
               DEVICE_MOTION_ACCELERATION_COMPONENT_ABS_MAX_MG) &&
           motion_component_is_within_abs_limit(
               sample->acceleration_mg_y,
               DEVICE_MOTION_ACCELERATION_COMPONENT_ABS_MAX_MG) &&
           motion_component_is_within_abs_limit(
               sample->acceleration_mg_z,
               DEVICE_MOTION_ACCELERATION_COMPONENT_ABS_MAX_MG) &&
           motion_component_is_within_abs_limit(
               sample->angular_rate_mdps_x,
               DEVICE_MOTION_ANGULAR_RATE_COMPONENT_ABS_MAX_MDPS) &&
           motion_component_is_within_abs_limit(
               sample->angular_rate_mdps_y,
               DEVICE_MOTION_ANGULAR_RATE_COMPONENT_ABS_MAX_MDPS) &&
           motion_component_is_within_abs_limit(
               sample->angular_rate_mdps_z,
               DEVICE_MOTION_ANGULAR_RATE_COMPONENT_ABS_MAX_MDPS);
}

/* A controller may be polled, IRQ-driven, or replaced by a future profile,
 * but shared fall policy always consumes a chronological sample stream. Keep
 * the last accepted timestamp at this boundary rather than making every
 * consumer defend its interval arithmetic against stale/duplicate samples.
 *
 * The timestamp belongs to the current boot's local monotonic clock. It may
 * restart at zero after a reboot, so this process-local guard deliberately
 * has no durable state and does not impose a maximum inter-sample gap. */
static uint64_t s_last_accepted_timestamp_us;

static bool motion_sample_timestamp_is_new(uint64_t timestamp_us) {
    uint64_t last = __atomic_load_n(&s_last_accepted_timestamp_us,
                                    __ATOMIC_ACQUIRE);
    for (;;) {
        if (timestamp_us <= last) return false;
        if (__atomic_compare_exchange_n(&s_last_accepted_timestamp_us, &last,
                                        timestamp_us, false, __ATOMIC_ACQ_REL,
                                        __ATOMIC_ACQUIRE)) {
            return true;
        }
        /* A concurrent caller won the race. `last` now contains its accepted
         * timestamp, so retry only if this sample remains strictly newer. */
    }
}

device_status_t motion_service_get_sample(device_motion_sample_t *out_sample) {
    if (!out_sample) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!device_profile_has_capability(DEVICE_CAPABILITY_MOTION_SENSOR)) {
        return DEVICE_STATUS_UNAVAILABLE;
    }
    device_motion_sample_t sample = {
        .struct_size = sizeof(sample),
        .abi_version = DEVICE_MOTION_SAMPLE_ABI_VERSION,
    };
    const device_status_t status = platform_sensor_get_motion_sample(&sample);
    if (status != DEVICE_STATUS_OK) return status;
    if (!motion_sample_is_valid(&sample)) return DEVICE_STATUS_IO_ERROR;
    if (!motion_sample_timestamp_is_new(sample.timestamp_us)) {
        return DEVICE_STATUS_IO_ERROR;
    }
    *out_sample = sample;
    return DEVICE_STATUS_OK;
}
