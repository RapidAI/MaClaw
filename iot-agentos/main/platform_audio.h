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

device_status_t platform_audio_set_output_volume(uint8_t percent);
device_status_t platform_audio_adjust_output_volume(int delta_percent,
                                                    uint8_t *out_percent);

device_status_t platform_audio_play_wav(const uint8_t *wav, uint32_t wav_len);
device_status_t platform_audio_play_alarm_burst(void);
device_status_t platform_audio_capture_wav(uint8_t **out_wav, uint32_t *out_len);
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
