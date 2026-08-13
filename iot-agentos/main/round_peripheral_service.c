#include "round_peripheral_service.h"
#include "round_audio_lifecycle.h"
#include "round_peripheral_lifecycle.h"

/* Exactly one translation unit selects the profile's PMIC/IMU/touch adapter.
 * The selected implementation retains all controller handles privately; the
 * shared Audio, Input, Power and Sensor paths see only normalized facts. */
#include "boards/round_peripheral_adapter.h"

esp_err_t round_peripheral_service_prepare(unsigned output_volume,
                                           uint32_t timeout_ms) {
    return round_audio_lifecycle_prepare_shared_bus(output_volume, timeout_ms);
}

esp_err_t round_peripheral_lifecycle_attach(i2c_master_bus_handle_t bus) {
    return round_peripheral_adapter_initialize(bus);
}

void round_peripheral_lifecycle_detach(void) {
    round_peripheral_adapter_release();
}

bool round_peripheral_service_touch_read(bool *pressed, uint8_t *gesture) {
    return round_peripheral_adapter_touch_read(pressed, gesture);
}

bool round_peripheral_service_touch_is_native_double_tap(uint8_t gesture) {
    return round_peripheral_adapter_touch_is_native_double_tap(gesture);
}

bool round_peripheral_service_touch_ready(void) {
    return round_peripheral_adapter_touch_ready();
}

bool round_peripheral_service_get_power_status(unsigned *level_percent, bool *charging) {
    return round_peripheral_adapter_get_power_status(level_percent, charging);
}

esp_err_t round_peripheral_service_get_motion_sample(device_motion_sample_t *out_sample) {
    return round_peripheral_adapter_get_motion_sample(out_sample);
}
