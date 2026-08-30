#pragma once

/*
 * Internal physical-audio SPI.
 *
 * Device API owns the stable, hardware-neutral PCM/WAV and wake-word
 * contract.  This port owns only translation to the selected profile's
 * codec/I2S/wake implementation.  It exposes no codec registers, I2S
 * channels, task handles, GPIOs, sample-slot selection, or board identity.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

/* A command capture may expose bounded, normalized metering while it is
 * running.  This callback is internal to the Audio/Display service boundary:
 * it carries no board, codec, RTOS or driver identity. */
typedef void (*platform_audio_capture_progress_cb_t)(void *context,
                                                     uint16_t level,
                                                     uint32_t elapsed_seconds);

/* Shared Platform Audio implementation detail.  Profile adapters translate
 * ESP-IDF errors before passing them to this function; SDK types never cross
 * this header. */
device_status_t platform_audio_status_from_esp_err(int platform_error);

device_status_t platform_audio_set_output_volume(uint8_t percent);
device_status_t platform_audio_adjust_output_volume(int delta_percent,
                                                    uint8_t *out_percent);

device_status_t platform_audio_play_wav(const uint8_t *wav, uint32_t wav_len);
/* Plays the normalized alarm pattern with a bounded peak amplitude.  The
 * Audio Service supplies the policy-derived value; profile adapters keep the
 * waveform/codec details private. */
device_status_t platform_audio_play_alarm_burst(uint8_t peak_percent);
device_status_t platform_audio_capture_wav(
    uint8_t **out_wav, uint32_t *out_len,
    platform_audio_capture_progress_cb_t on_progress, void *progress_context);
void platform_audio_release_captured_wav(uint8_t *wav);

device_status_t platform_audio_stream_start(void);
device_status_t platform_audio_stream_read(int16_t *mono, uint32_t capacity,
                                           uint32_t *samples_read, uint16_t *level);
void platform_audio_stream_stop(void);

device_status_t platform_audio_playback_begin(void);
device_status_t platform_audio_playback_write(const int16_t *pcm, uint32_t frames,
                                              uint8_t channels);
device_status_t platform_audio_playback_end(bool playback_succeeded);
void platform_audio_request_playback_stop(void);
void platform_audio_request_capture_stop(void);
void platform_audio_reset_capture_stop(void);

device_status_t platform_audio_wake_word_start(device_wake_word_cb_t on_wake,
                                               void *context);
device_status_t platform_audio_wake_word_stop(void);
device_status_t platform_audio_wake_word_stop_with_timeout(uint32_t timeout_ms);
void platform_audio_wake_word_pause(bool paused);
/* Requests a pause and waits for the selected profile's recognizer safe
 * point. It is an internal Audio/Power coordination primitive, not a public
 * wake or hardware-sleep API; the profile retains all task/I2S details. */
device_status_t platform_audio_wake_word_pause_with_ack(bool paused,
                                                        uint32_t timeout_ms);
