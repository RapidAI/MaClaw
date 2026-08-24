#pragma once

/* Private circular-board Input HAL implementation boundary.
 *
 * This is not a Device/Platform API.  It owns the normalized gesture scanner
 * and delegates electrical reads to the selected profile adapter. The board
 * port retains only lifecycle forwarding; this contract uses normalized Device
 * Input values rather than importing the board facade.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "esp_err.h"

typedef void (*round_input_service_publish_cb_t)(device_input_action_t action,
                                                 device_input_source_t source,
                                                 void *context);
esp_err_t round_input_service_start(round_input_service_publish_cb_t on_button,
                                   void *arg);
esp_err_t round_input_service_stop(uint32_t timeout_ms);
/* Future-MCU-sleep fence: retain the scanner and its controller ownership,
 * but park it at a verified no-read boundary. Once PREPARE closes admission,
 * including an ACK timeout, only the owning Power transaction's ABORT resumes
 * the same task generation; neither operation deinitializes touch/key
 * hardware. */
esp_err_t round_input_service_prepare_system_sleep(uint32_t timeout_ms);
void round_input_service_abort_system_sleep_prepare(void);
void round_input_service_set_command_cancel_enabled(bool enabled);
