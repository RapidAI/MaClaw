#include <assert.h>
#include <stdio.h>

#include "services/media_transfer_service.h"

int main(void) {
    media_transfer_service_host_t host = {
        .struct_size = sizeof(media_transfer_service_host_t),
        .stop_wake_word_for_media = NULL,
        .cancel_startup_pet_for_server_audio = NULL,
        .take_startup_pet_audio_preemption = NULL,
        .rearm_preempted_startup_pet = NULL,
        .schedule_wake_restart = NULL,
        .context = NULL,
    };
    assert(host.struct_size == sizeof(host));
    assert(sizeof(media_transfer_service_host_t) >= sizeof(void *) * 6u);
    puts("PASS media transfer value contract");
    return 0;
}
