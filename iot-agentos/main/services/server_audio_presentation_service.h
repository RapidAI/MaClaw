#pragma once

/*
 * Server-audio format/presentation adapter.
 *
 * Gateway Dispatcher receives remote audio as MIME plus an owned byte span.
 * This service owns format classification and chooses the private MP3 or WAV
 * renderer.  The public host seam deliberately contains values only: no
 * Gateway JSON, ESP-IDF status, audio device, codec, allocator or RTOS type
 * reaches the dispatcher or composition root.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef struct {
    uint32_t struct_size;
    device_status_t (*play_mp3)(const uint8_t *data, uint32_t length, void *context);
    device_status_t (*play_wav)(const uint8_t *data, uint32_t length, void *context);
    void *context;
} server_audio_presentation_service_host_t;

device_status_t server_audio_presentation_service_init(
    const server_audio_presentation_service_host_t *host);

/* A missing MIME is permitted; an MPEG frame sync or ID3 marker then selects
 * the MP3 renderer. Other admitted bytes use the existing WAV path. */
bool server_audio_presentation_service_mime_supported(const char *mime);
bool server_audio_presentation_service_url_allowed(const char *url);
device_status_t server_audio_presentation_service_play(const char *mime,
                                                       const uint8_t *data,
                                                       uint32_t length);

/* Maps only stable Device API statuses to a permanent remote-content result.
 * Busy, timeout, I/O and memory pressure remain retryable Gateway outcomes. */
bool server_audio_presentation_service_error_is_permanent(device_status_t status);
