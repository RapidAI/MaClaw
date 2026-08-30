#pragma once

/*
 * Cold-start runtime admission facts.
 *
 * This service owns the three cross-callback facts that determine whether the
 * ordinary startup sequence is complete, whether a late Wi-Fi recovery may
 * start Gateway, and whether SAFE_MODE has terminally closed ordinary
 * admission.  The composition root retains all physical start/stop actions,
 * boot ordering and SAFE_MODE quiescence bridges.  This contract is strictly
 * value-only: no SDK, RTOS, transport, JSON, allocator or board detail leaks
 * through it.
 */

#include <stdbool.h>

#include "device_api.h"

#define STARTUP_RUNTIME_STATE_BOOT_SESSION_ID_CAPACITY 33u

device_status_t startup_runtime_state_service_init(void);

/* Captures the current boot's opaque Gateway correlation value once. The
 * returned pointer stays valid for this boot after a successful capture and
 * is immutable thereafter; it carries no credential or board detail. */
bool startup_runtime_state_service_capture_boot_session_id(const char *session_id);
const char *startup_runtime_state_service_boot_session_id(void);
bool startup_runtime_state_service_matches_boot_session_id(const char *session_id);

/* Captures the staged-candidate evidence returned by Configuration's single
 * durable boot-snapshot transaction. This is deliberately a boot-scoped fact:
 * later Configuration queries must not turn an admission/read failure into a
 * false "confirmed" result. The value is immutable after the first capture
 * and can only be observed after capture succeeds. */
bool startup_runtime_state_service_capture_staged_provisioning(bool staged);
bool startup_runtime_state_service_staged_provisioning_pending(void);

/* A new authenticated startup sequence owns the pre-ready state. It is
 * rejected once SAFE_MODE has terminally closed ordinary admission. */
bool startup_runtime_state_service_begin_sequence(void);
void startup_runtime_state_service_complete_sequence(void);
bool startup_runtime_state_service_sequence_complete(void);

/* Opens the late Wi-Fi/IP callback bridge only while normal startup remains
 * eligible. SAFE_MODE can never be reversed through this API. */
bool startup_runtime_state_service_permit_gateway_startup(void);
bool startup_runtime_state_service_gateway_startup_recovery_allowed(void);

/* Atomically closes ordinary startup/Gateway admission for this boot. Returns
 * true only for the caller that performed the terminal transition. */
bool startup_runtime_state_service_enter_safe_mode(void);
bool startup_runtime_state_service_safe_mode_active(void);
