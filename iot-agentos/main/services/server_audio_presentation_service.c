#include "services/server_audio_presentation_service.h"

#include <string.h>

static server_audio_presentation_service_host_t s_host;
static bool s_initialized;

static bool host_valid(const server_audio_presentation_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) && host->play_mp3 &&
           host->play_wav;
}

static bool payload_is_mp3(const char *mime, const uint8_t *data, uint32_t length) {
    if (mime && (!strcmp(mime, "audio/mpeg") || !strcmp(mime, "audio/mp3"))) {
        return true;
    }
    if (!data || length < 2u) return false;
    if (length >= 3u && memcmp(data, "ID3", 3u) == 0) return true;
    /* The decoder validates MPEG layer/version fields. This only selects the
     * renderer for servers which omit a MIME type. */
    return data[0] == 0xffu && (data[1] & 0xe0u) == 0xe0u;
}

device_status_t server_audio_presentation_service_init(
    const server_audio_presentation_service_host_t *host) {
    if (!host_valid(host)) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (s_initialized) {
        return s_host.play_mp3 == host->play_mp3 && s_host.play_wav == host->play_wav &&
                       s_host.context == host->context
                   ? DEVICE_STATUS_OK
                   : DEVICE_STATUS_BUSY;
    }
    s_host = *host;
    s_initialized = true;
    return DEVICE_STATUS_OK;
}

bool server_audio_presentation_service_mime_supported(const char *mime) {
    return !mime || !strcmp(mime, "audio/wav") || !strcmp(mime, "audio/x-wav") ||
           !strcmp(mime, "audio/mpeg") || !strcmp(mime, "audio/mp3");
}

bool server_audio_presentation_service_url_allowed(const char *url) {
    static const char prefix[] = "/api/im-gateway/v1/media/";
    return url && url[0] == '/' &&
           strncmp(url, prefix, sizeof(prefix) - 1u) == 0;
}

device_status_t server_audio_presentation_service_play(const char *mime,
                                                       const uint8_t *data,
                                                       uint32_t length) {
    if (!s_initialized) return DEVICE_STATUS_UNAVAILABLE;
    if (!data || length == 0u || !server_audio_presentation_service_mime_supported(mime)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    return payload_is_mp3(mime, data, length)
               ? s_host.play_mp3(data, length, s_host.context)
               : s_host.play_wav(data, length, s_host.context);
}

bool server_audio_presentation_service_error_is_permanent(device_status_t status) {
    switch (status) {
        case DEVICE_STATUS_INVALID_ARGUMENT:
        case DEVICE_STATUS_UNAVAILABLE:
        case DEVICE_STATUS_NOT_FOUND:
            return true;
        case DEVICE_STATUS_BUSY:
        case DEVICE_STATUS_TIMEOUT:
        case DEVICE_STATUS_RESOURCE_EXHAUSTED:
        case DEVICE_STATUS_IO_ERROR:
        case DEVICE_STATUS_INTERNAL_ERROR:
        case DEVICE_STATUS_OK:
        default:
            return false;
    }
}
