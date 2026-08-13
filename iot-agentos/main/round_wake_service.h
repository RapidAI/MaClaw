#pragma once

/*
 * Private circular wake-recognizer lifecycle service.
 *
 * This seam owns ESP-SR/MultiNet model orchestration, FreeRTOS task
 * publication, cancellation, pause acknowledgement and deferred-callback
 * draining for the round boards.  The shared renderer supplies only the
 * semantic business action to take after a recognised phrase; it never owns a
 * recognizer task, model handle, ESP-SR command vocabulary, or audio capture
 * buffer.
 */

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"
typedef void (*round_wake_service_callback_t)(void *arg);

esp_err_t round_wake_service_start(round_wake_service_callback_t callback,
                                   void *callback_arg,
                                   uint32_t ready_timeout_ms);
esp_err_t round_wake_service_stop(uint32_t timeout_ms);

void round_wake_service_set_paused(bool paused);
bool round_wake_service_wait_for_pause_ack(uint32_t timeout_ms);
