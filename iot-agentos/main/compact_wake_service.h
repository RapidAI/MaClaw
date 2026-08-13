#pragma once

/*
 * Private compact-board wake-recognizer lifecycle service.
 *
 * This service owns ESP-SR/MultiNet model orchestration, FreeRTOS task
 * publication, ready/stop/pause state and callback lifetime for Bread Compact
 * and Fangtang.  The common compact renderer supplies only the semantic
 * business action after a confirmed wake phrase; it never owns a recognizer
 * worker generation, model/runtime handle, capture buffer or synchronization
 * state.
 */

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"
typedef void (*compact_wake_service_callback_t)(void *arg);

esp_err_t compact_wake_service_start(compact_wake_service_callback_t callback,
                                     void *callback_arg,
                                     uint32_t ready_timeout_ms);
esp_err_t compact_wake_service_stop(uint32_t timeout_ms);

void compact_wake_service_set_paused(bool paused);
bool compact_wake_service_wait_for_pause_ack(uint32_t timeout_ms);
