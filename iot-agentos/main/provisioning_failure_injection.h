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
/* Late-start SAFE_MODE proof: reached only after the complete minimum local
 * service set is ready and before any physical uplink/Gateway startup. */
bool provisioning_failure_injection_safe_mode_at_local_ready(void);
/* Forces the durable force-setup consume transaction to fail before it clears
 * the one-shot flag. This remains a compile-time-only C7 HIL seam. */
bool provisioning_failure_injection_safe_mode_force_setup_take_fails(void);

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

/* Boots a deterministic, service-private Power Lease test.  It covers the
 * DISPLAY_OFF PREPARE -> COMMIT admission fence and its lifecycle drain
 * boundary without exposing a panel, wake source or runtime test command. */
bool provisioning_failure_injection_power_lease_display_off_commit_test_enabled(void);

/* Drives a compile-time-only Power Service HIL.  It holds the real shared
 * transition mutex across an ambient deadline, then verifies retry, wake,
 * foreground-lease and lifecycle cancellation against the selected panel.
 * It adds no runtime control surface and is absent from release images. */
bool provisioning_failure_injection_power_display_off_retry_hil_test_enabled(void);


/* Compile-time-only proof for Sleep Schedule's natural-end marker ownership.
 * The test is service-private: it performs no physical wake, persistence or
 * runtime policy mutation. */
bool provisioning_failure_injection_sleep_schedule_end_handoff_test_enabled(void);

/* Dedicated hardware-in-the-loop companion for the marker proof. It drives
 * no public test API: the selected image performs one ephemeral panel
 * DISPLAY_OFF -> schedule wake -> composed App UI handoff at boot. */
bool provisioning_failure_injection_sleep_schedule_end_handoff_hil_test_enabled(void);

/* Profile-private Motion HAL startup proof.  The selected Waveshare-only test
 * artifact fails the optional QMI8658 acquisition before it touches I2C, so
 * the existing adapter cleanup and common-device bootstrap can be observed
 * without a runtime test endpoint or an actual sensor disconnection. */
bool provisioning_failure_injection_waveshare_qmi8658_init_fails(void);

/* Runtime counterpart: the selected Waveshare-only test artifact injects one
 * normalised Motion sample read failure only after the startup probe succeeds.
 * It is deliberately not a Device/Platform or Hub test control. */
bool provisioning_failure_injection_waveshare_qmi8658_motion_read_fails_once(void);
