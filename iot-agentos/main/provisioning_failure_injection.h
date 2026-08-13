#pragma once

#include <stdbool.h>
#include <stdint.h>

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

/* Test-only composition-root seam. It is reached only after the shared
 * Display Service has published its task, so startup rollback can validate
 * that the service closes admission and joins that task without asking a
 * board renderer to expose panel or DMA details. */
bool provisioning_failure_injection_display_service_fail_after_init(void);

/* Test-only delay applied by the Display Task after it has consumed its
 * terminal STOP record. It makes the caller's bounded join expire while the
 * task still owns the boot-lifetime request/completion storage, then lets the
 * same task exit late. It is never a runtime delay control. */
uint32_t provisioning_failure_injection_display_service_stop_delay_ms(void);
/* Test-only delay injected while the Display Task is already executing one
 * ordinary scene request. It covers STOP queued behind a busy renderer, not
 * the separate terminal-STOP late-exit seam. */
uint32_t provisioning_failure_injection_display_service_request_delay_once_ms(void);
bool provisioning_failure_injection_display_service_request_delay_enabled(void);
uint32_t provisioning_failure_injection_display_service_secondary_stop_delay_ms(void);
uint32_t provisioning_failure_injection_display_service_secondary_stop_timeout_ms(void);

/* Drives a service-private compact-animation lifecycle test.  The selected
 * test artifact delays completion publication while holding the animation
 * lifecycle mutex, then delays cleanup after its one-shot completion.  This
 * proves callers consume the original transaction's remaining deadline. */
bool provisioning_failure_injection_compact_display_animation_deadline_test_enabled(void);
uint32_t provisioning_failure_injection_compact_display_animation_pre_completion_delay_ms(void);
uint32_t provisioning_failure_injection_compact_display_animation_post_completion_delay_ms(void);

/* Round Display Service counterpart of the compact animation deadline proof.
 * It is compile-time test-only and never exposes a board, panel or runtime
 * control surface through Device/Platform APIs. */
bool provisioning_failure_injection_round_display_animation_deadline_test_enabled(void);
uint32_t provisioning_failure_injection_round_display_animation_pre_completion_delay_ms(void);
uint32_t provisioning_failure_injection_round_display_animation_post_completion_delay_ms(void);

/* Test-only: abandon exactly one real color-transfer fence wait. The selected
 * profile keeps the old source controller-owned until its actual callback. */
bool provisioning_failure_injection_display_transfer_fence_timeout_once(void);

/* Boots a dedicated lifecycle-test artifact through the Task Registry's
 * internal contention/deadline test. Production configurations compile this
 * to false; it is neither a business feature nor a runtime control surface. */
bool provisioning_failure_injection_task_registry_lifecycle_test_enabled(void);
