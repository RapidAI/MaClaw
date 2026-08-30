#include <assert.h>
#include <stdio.h>

#include "services/server_audio_presentation_service.h"

int main(void) {
    assert(server_audio_presentation_service_mime_supported(NULL));
    assert(server_audio_presentation_service_mime_supported("audio/wav"));
    assert(server_audio_presentation_service_mime_supported("audio/mpeg"));
    assert(!server_audio_presentation_service_mime_supported("audio/ogg"));
    assert(server_audio_presentation_service_url_allowed(
        "/api/im-gateway/v1/media/abc"));
    assert(!server_audio_presentation_service_url_allowed("https://evil.invalid/a"));
    assert(server_audio_presentation_service_error_is_permanent(
        DEVICE_STATUS_INVALID_ARGUMENT));
    assert(!server_audio_presentation_service_error_is_permanent(DEVICE_STATUS_BUSY));
    assert(!server_audio_presentation_service_error_is_permanent(DEVICE_STATUS_TIMEOUT));
    assert(!server_audio_presentation_service_error_is_permanent(
        DEVICE_STATUS_RESOURCE_EXHAUSTED));
    puts("PASS server audio presentation value policy");
    return 0;
}
