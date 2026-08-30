#include <assert.h>
#include <stdio.h>

#include "services/startup_welcome_service.h"

int main(void) {
    startup_welcome_service_host_t host = {
        .struct_size = sizeof(startup_welcome_service_host_t),
        .log_gate_released = NULL,
        .log_gate_timed_out = NULL,
        .context = NULL,
    };
    assert(host.struct_size == sizeof(host));
    assert(sizeof(startup_welcome_service_host_t) >= sizeof(void *) * 3u);
    puts("PASS startup Welcome value contract");
    return 0;
}
