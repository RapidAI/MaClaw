#pragma once

/* Private compact-board Audio HAL implementation boundary.  The common
 * renderer owns PCM conversion, command/wake session policy and playback
 * content; this service owns selected-profile I2S handles, physical buffers,
 * the audio ownership mutex and every capture/playback session lifecycle.
 * It is not a Device or Platform API. */

#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"
#include "freertos/FreeRTOS.h"

#include "boards/compact_audio_calibration.h"

const compact_audio_calibration_t *compact_audio_service_calibration(void);
esp_err_t compact_audio_service_prepare(uint32_t timeout_ms);
esp_err_t compact_audio_service_set_output_volume(unsigned percent, uint32_t timeout_ms);
void *compact_audio_service_allocate_command_wav(size_t bytes);
void compact_audio_service_free_command_wav(void *buffer);
/* Foreground capture sessions retain Audio-HAL ownership between begin/end.
 * The renderer supplies only normalized PCM policy; it must never take an
 * I2S mutex or manipulate session ownership itself. */
esp_err_t compact_audio_service_stream_begin(uint32_t timeout_ms);
esp_err_t compact_audio_service_command_capture_begin(uint32_t timeout_ms);
typedef struct {
    int32_t input_peak;
    int32_t peak;
    uint16_t level;
    uint16_t mean_level;
} compact_audio_capture_stats_t;

/* The service translates direct-I2S 32-bit slots to normalized mono PCM and
 * returns physical signal statistics. VAD, UI and command duration policy
 * remain in the renderer. */
esp_err_t compact_audio_service_capture_read(int16_t *mono, size_t capacity,
                                             size_t *samples_read,
                                             compact_audio_capture_stats_t *out_stats,
                                             TickType_t timeout);
void compact_audio_service_stream_end(void);
void compact_audio_service_command_capture_end(void);
/* Cancellation is part of the foreground Audio-HAL session.  The application
 * may request/reset it around an asynchronous interaction, but renderer code
 * must not retain a cross-core capture-stop flag beside scene state. */
void compact_audio_service_request_command_capture_stop(void);
void compact_audio_service_reset_command_capture_stop(void);
bool compact_audio_service_command_capture_stop_requested(void);

/* Wake inference borrows the physical RX channel per bounded block, so a
 * foreground capture/playback session can preempt it without renderer-owned
 * synchronization objects. */
typedef struct {
    int16_t *mono;
    size_t frames;
} compact_audio_wake_capture_t;

esp_err_t compact_audio_service_wake_capture_begin(size_t frames,
                                                   compact_audio_wake_capture_t *capture);
esp_err_t compact_audio_service_wake_capture_read(compact_audio_wake_capture_t *capture,
                                                  uint32_t timeout_ms,
                                                  compact_audio_capture_stats_t *out_stats);
void compact_audio_service_wake_capture_end(compact_audio_wake_capture_t *capture);

esp_err_t compact_audio_service_playback_begin(uint32_t timeout_ms);
esp_err_t compact_audio_service_playback_write(const void *buffer, size_t bytes,
                                               size_t *out_written,
                                               TickType_t timeout);
esp_err_t compact_audio_service_playback_end(void);
void compact_audio_service_request_playback_stop(void);
