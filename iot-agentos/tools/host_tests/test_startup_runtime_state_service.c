#include <assert.h>
#include <stdio.h>
#include <string.h>

#include "services/startup_runtime_state_service.h"

int main(void) {
    assert(!startup_runtime_state_service_begin_sequence());
    assert(startup_runtime_state_service_init() == DEVICE_STATUS_OK);
    assert(!startup_runtime_state_service_sequence_complete());
    assert(!startup_runtime_state_service_gateway_startup_recovery_allowed());
    assert(!startup_runtime_state_service_safe_mode_active());
    assert(strcmp(startup_runtime_state_service_boot_session_id(), "") == 0);
    assert(!startup_runtime_state_service_matches_boot_session_id("boot-a"));
    assert(startup_runtime_state_service_capture_boot_session_id("boot-a"));
    assert(strcmp(startup_runtime_state_service_boot_session_id(), "boot-a") == 0);
    assert(startup_runtime_state_service_matches_boot_session_id("boot-a"));
    assert(!startup_runtime_state_service_matches_boot_session_id("boot-b"));
    assert(!startup_runtime_state_service_capture_boot_session_id("boot-b"));
    assert(strcmp(startup_runtime_state_service_boot_session_id(), "boot-a") == 0);
    assert(!startup_runtime_state_service_staged_provisioning_pending());
    assert(startup_runtime_state_service_capture_staged_provisioning(true));
    assert(startup_runtime_state_service_staged_provisioning_pending());
    assert(!startup_runtime_state_service_capture_staged_provisioning(false));
    assert(startup_runtime_state_service_staged_provisioning_pending());

    assert(startup_runtime_state_service_begin_sequence());
    assert(startup_runtime_state_service_permit_gateway_startup());
    assert(startup_runtime_state_service_gateway_startup_recovery_allowed());
    startup_runtime_state_service_complete_sequence();
    assert(startup_runtime_state_service_sequence_complete());
    assert(!startup_runtime_state_service_gateway_startup_recovery_allowed());

    assert(startup_runtime_state_service_enter_safe_mode());
    assert(startup_runtime_state_service_safe_mode_active());
    assert(!startup_runtime_state_service_sequence_complete());
    assert(!startup_runtime_state_service_gateway_startup_recovery_allowed());
    assert(!startup_runtime_state_service_begin_sequence());
    assert(!startup_runtime_state_service_permit_gateway_startup());
    assert(!startup_runtime_state_service_enter_safe_mode());
    startup_runtime_state_service_complete_sequence();
    assert(!startup_runtime_state_service_sequence_complete());

    puts("PASS startup runtime admission state");
    return 0;
}
