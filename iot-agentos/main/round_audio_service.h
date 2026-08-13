#pragma once

/*
 * Private circular Audio HAL boundary.
 *
 * The round renderer deals only in normalized PCM, playback phases and
 * timeout values. Codec handles, I2C/I2S objects and DMA-memory capabilities
 * stay in the one Audio source owner below this header.  Controllers sharing
 * its I2C bus are exposed through the separate Peripheral service. This is
 * deliberately not a Device or Platform API.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"

const char *round_audio_service_name(void);
unsigned round_audio_service_default_output_volume(void);
uint32_t round_audio_service_sample_rate(void);

/* Audio resource ownership belongs to this private service.  The round
 * renderer asks only for a retryable ready state; it never owns codec/I2C/I2S
 * rollback state. */
esp_err_t round_audio_service_prepare_for_wake(unsigned output_volume,
                                                uint32_t timeout_ms);
esp_err_t round_audio_service_apply_output_volume(unsigned current_volume,
                                                  unsigned requested_volume,
                                                  uint32_t timeout_ms);

/* Foreground audio-session ownership is private to the Audio HAL.  The
 * renderer pauses/resumes the wake policy around these calls, but never owns
 * the I2S mutex or a playback task handle. */
esp_err_t round_audio_service_stream_begin(unsigned output_volume,
                                           uint32_t timeout_ms,
                                           const char *diagnostic_label);
esp_err_t round_audio_service_stream_read(int16_t *mono, size_t sample_capacity,
                                          size_t *samples_read, uint16_t *level);
void round_audio_service_stream_end(void);
esp_err_t round_audio_service_command_capture_begin(unsigned output_volume,
                                                     uint32_t timeout_ms);
/* Physical capture statistics are produced beside PCM conditioning.  Command
 * VAD and recording UI consume these normalized values, but keep their policy
 * and thresholds in the common renderer. */
typedef struct {
    int32_t peak;
    uint16_t level;
    uint16_t mean_level;
} round_audio_capture_stats_t;

esp_err_t round_audio_service_command_capture_read(int16_t *mono,
                                                    size_t sample_capacity,
                                                    size_t *samples_read,
                                                    round_audio_capture_stats_t *out_stats);
void round_audio_service_command_capture_end(void);
/* As for compact boards, command cancellation belongs to the Audio-HAL
 * session, not a renderer-owned volatile shared across CPU cores. */
void round_audio_service_request_command_capture_stop(void);
void round_audio_service_reset_command_capture_stop(void);
bool round_audio_service_command_capture_stop_requested(void);
esp_err_t round_audio_service_playback_begin(unsigned output_volume,
                                             uint32_t timeout_ms);
esp_err_t round_audio_service_playback_write(const int16_t *pcm, size_t frames,
                                             unsigned channels);
esp_err_t round_audio_service_playback_end(esp_err_t playback_err);
void round_audio_service_request_playback_stop(void);

/* Capture conditioning is a private Audio-HAL concern.  It turns the
 * selected codec's normalized mono stream into bounded, DC-stable PCM while
 * retaining no command/VAD/UI policy.  The renderer owns when a session starts
 * and how its returned level is interpreted. */
/* Offline wake consumes a quieter PCM conditioning profile than uploaded
 * recordings.  The recognizer/session policy remains in the renderer; this
 * service owns only codec-scale sample cleanup and its scalar diagnostics. */
typedef struct {
    int32_t input_peak;
    uint32_t rms;
    uint16_t invalid_samples;
    uint32_t gain_q8;
} round_audio_wake_pcm_stats_t;

/* Wake inference owns no physical PCM buffers.  The recognizer receives only
 * its normalized mono block; wire-slot packing, bounded I2S ownership and
 * DMA-capable allocation remain in Audio HAL. */
typedef struct {
    int16_t *mono;
    size_t frames;
} round_audio_wake_capture_t;

esp_err_t round_audio_service_wake_capture_begin(size_t frames,
                                                 round_audio_wake_capture_t *capture);
esp_err_t round_audio_service_wake_capture_read(round_audio_wake_capture_t *capture,
                                                uint32_t timeout_ms,
                                                round_audio_wake_pcm_stats_t *out_stats);
void round_audio_service_wake_capture_end(round_audio_wake_capture_t *capture);

void *round_audio_service_allocate_command_wav(size_t bytes);
void round_audio_service_free_command_wav(void *buffer);
