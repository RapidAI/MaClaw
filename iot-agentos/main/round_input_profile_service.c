#include "round_input_profile_service.h"

/* Exactly one translation unit includes the selected input-only adapter. */
#include "boards/round_input_profile_adapter.h"

const round_input_profile_t *round_input_profile_service_profile(void) {
    return round_selected_input_profile();
}

esp_err_t round_input_profile_service_initialize_activate_key(void) {
    return round_selected_input_initialize_activate_key();
}

bool round_input_profile_service_activate_key_pressed(void) {
    return round_selected_input_activate_key_pressed();
}

device_input_source_t round_input_profile_service_resolve_source(bool key_pressed,
                                                                  bool touch_pressed) {
    return round_selected_input_resolve_source(key_pressed, touch_pressed);
}

bool round_input_profile_service_consume_boot_gesture(device_input_action_t action,
                                                       device_input_source_t source) {
    return round_selected_input_consume_boot_gesture(action, source);
}

BaseType_t round_input_profile_service_start_scan_task(TaskFunction_t entry,
                                                       TaskHandle_t *out_task) {
    return round_selected_input_start_scan_task(entry, out_task);
}
