#pragma once

/* Private neutral facade from the shared round gesture classifier to the
 * selected Input source owner. */

#include <stdbool.h>

#include "boards/round_input_profile.h"
#include "device_api.h"
#include "esp_err.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

const round_input_profile_t *round_input_profile_service_profile(void);
esp_err_t round_input_profile_service_initialize_activate_key(void);
bool round_input_profile_service_activate_key_pressed(void);
device_input_source_t round_input_profile_service_resolve_source(bool key_pressed,
                                                                  bool touch_pressed);
bool round_input_profile_service_consume_boot_gesture(device_input_action_t action,
                                                       device_input_source_t source);
BaseType_t round_input_profile_service_start_scan_task(TaskFunction_t entry,
                                                       TaskHandle_t *out_task);
