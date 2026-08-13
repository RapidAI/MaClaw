#include "compact_peripheral_service.h"

/* This keeps ADC/GPIO/task/critical-section mechanics outside renderer and
 * Device/Platform-facing code. */
#include "boards/compact_peripheral_adapter_selector.h"

esp_err_t compact_peripheral_service_initialize(void) {
    return compact_peripheral_adapter_init();
}
esp_err_t compact_peripheral_service_stop_background_tasks(uint32_t timeout_ms) {
    return compact_peripheral_adapter_stop_background_tasks(timeout_ms);
}
bool compact_peripheral_service_get_power_status(unsigned *level_percent, bool *charging) {
    return compact_peripheral_adapter_get_power_status(level_percent, charging);
}
esp_err_t compact_peripheral_service_get_motion_sample(device_motion_sample_t *out_sample) {
    return compact_peripheral_adapter_get_motion_sample(out_sample);
}
