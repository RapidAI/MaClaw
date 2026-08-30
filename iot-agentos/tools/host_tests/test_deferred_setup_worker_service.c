#include <assert.h>
#include <stdio.h>

#include "services/deferred_setup_worker_service.h"

int main(void) {
    deferred_setup_worker_service_host_t host = {
        .struct_size = sizeof(deferred_setup_worker_service_host_t),
        .meeting_active = NULL,
        .start_setup_portal = NULL,
        .context = NULL,
    };
    assert(host.struct_size == sizeof(host));
    assert(sizeof(deferred_setup_worker_service_host_t) >= sizeof(void *) * 3u);
    puts("PASS deferred setup worker value contract");
    return 0;
}
