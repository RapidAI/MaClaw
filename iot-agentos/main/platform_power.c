#include "platform_power.h"

#include "board_port.h"
#include "display_service.h"

bool platform_power_enter_display_off(void) {
    return display_service_enter_display_off();
}

bool platform_power_wake_display(void) {
    return display_service_wake_display();
}

bool platform_power_display_is_off(void) {
    return display_service_display_is_off();
}

bool platform_power_get_telemetry(device_power_telemetry_t *out_telemetry) {
    if (!out_telemetry) return false;
    unsigned level = 0;
    bool charging = false;
    const bool available = board_port_get_power_status(&level, &charging);
    *out_telemetry = (device_power_telemetry_t){
        .available = available,
        .level_percent = (uint8_t)(level > 100 ? 100 : level),
        .charging = charging,
    };
    return available;
}
