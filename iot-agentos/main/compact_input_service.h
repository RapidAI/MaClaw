#pragma once

/* Private compact-board Input HAL implementation boundary.  This service owns
 * the selected profile's electrical key contract, scanner task, debounce and
 * gesture classification.  The compact renderer supplies only a normalized
 * publisher during board construction; it neither owns a scanner task nor
 * sees the scanner's FreeRTOS lifetime state.  This is not a Device/Platform
 * API. */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "esp_err.h"
/* Profile-private adapters may choose their scanner worker footprint.  Pull in
 * the FreeRTOS base before an adapter may include task.h; no RTOS type appears
 * in this service's callable contract. */
#include "freertos/FreeRTOS.h"

typedef struct {
    bool activate_released;
    bool volume_up_released;
    bool volume_down_released;
} compact_input_raw_state_t;

esp_err_t compact_input_service_initialize(void);
void compact_input_service_read_raw(compact_input_raw_state_t *out_state);
bool compact_input_service_has_volume_keys(void);
/* A key-only profile without dedicated volume controls can expose two bounded
 * release-time hold thresholds for the normalized volume +/- intents. Zero
 * means that the profile has no alternate gesture. */
int64_t compact_input_service_local_volume_increase_hold_us(void);
int64_t compact_input_service_local_volume_decrease_hold_us(void);
int64_t compact_input_service_activate_debounce_us(void);
int64_t compact_input_service_volume_debounce_us(void);
int64_t compact_input_service_long_press_us(void);
int64_t compact_input_service_double_click_us(void);
const char *compact_input_service_name(void);
typedef void (*compact_input_publish_cb_t)(device_input_action_t action,
                                           device_input_source_t source,
                                           void *context);
/* Preparation allocates only the scanner's completion state.  It gives the
 * compact renderer a precise startup rollback point without leaking a task or
 * semaphore to it. */
esp_err_t compact_input_service_prepare_scanner(void);
esp_err_t compact_input_service_start_scanner(compact_input_publish_cb_t publish,
                                              void *context);
/* Stops the privately-owned scanner before Input Service releases its event
 * queues.  A timeout intentionally retains the task/semaphore ownership for
 * a later lifecycle pass. */
esp_err_t compact_input_service_stop_scanner(uint32_t timeout_ms);
/* Future-MCU-sleep fence for the retained GPIO scanner. PREPARE parks the
 * current task generation after its last physical read. Once it has closed
 * admission, including an ACK timeout, only the owning Power transaction's
 * ABORT may resume that generation; neither operation reinitializes the
 * board adapter. */
esp_err_t compact_input_service_prepare_system_sleep(uint32_t timeout_ms);
void compact_input_service_abort_system_sleep_prepare(void);
/* Releases only unpublished scanner preparation after failed startup.  It is
 * never a board deinit or a way to reclaim a running scanner. */
void compact_input_service_discard_unpublished_scanner_state(void);
/* Compact boards currently expose no command-cancel gesture distinct from
 * their normal physical-key semantics.  Keep that deliberate no-op below the
 * selected Input HAL so business policy never needs a board-port branch. */
void compact_input_service_set_command_cancel_enabled(bool enabled);
void compact_input_service_run_startup_selector(void);
bool compact_input_service_consume_startup_selector_result(uint32_t window_ms);
bool compact_input_service_response_paging_uses_volume_keys(void);
