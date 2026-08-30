#include <assert.h>
#include <stdio.h>

#include "services/wake_restart_worker_service.h"

int main(void) {
    wake_restart_worker_service_host_t host = {
        .struct_size = sizeof(wake_restart_worker_service_host_t),
        .restart_allowed = NULL,
        .foreground_active = NULL,
        .meeting_active = NULL,
        .optional_pet_worker_active = NULL,
        .discard_asset_client = NULL,
        .start_wake_word = NULL,
        .context = NULL,
    };
    assert(host.struct_size == sizeof(host));
    assert(sizeof(wake_restart_worker_service_host_t) >= sizeof(void *) * 7u);
    puts("PASS wake restart worker value contract");
    return 0;
}
