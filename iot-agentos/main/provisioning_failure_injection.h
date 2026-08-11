#pragma once

#include <stdbool.h>

/* Compile-time-only test seam for the post-save provisioning coordinator.
 * There is no runtime setter, HTTP route, Hub message, or console command.
 * Production configs compile all checks to constant false. */
bool provisioning_failure_injection_lifecycle_primitives_unavailable(void);
bool provisioning_failure_injection_task_create_fails(void);
bool provisioning_failure_injection_task_registry_register_fails(void);
bool provisioning_failure_injection_force_portal_at_boot(void);

/* Compile-time display bring-up faults used only by profile adapters.
 * Stage numbering remains private to adapter resource acquisition order. */
bool provisioning_failure_injection_display_initialization_should_fail_after(
    unsigned completed_stage);

/* Compile-time fault points for the shared compact renderer after its display
 * adapter has successfully acquired the panel. These model the renderer's own
 * acquisition boundary rather than exposing a Bread/Fangtang detail through a
 * public HAL contract. */
bool provisioning_failure_injection_compact_renderer_initialization_should_fail_after(
    unsigned completed_stage);
