#include "provisioning_failure_injection.h"

/* Kconfig values are emitted into this generated header, not passed as
 * compiler -D options. Keep this dependency local to the test seam so a
 * selected test-only failure point cannot accidentally compile as disabled. */
#include "sdkconfig.h"

#ifndef CONFIG_MACLAW_TEST_BUILD
#define CONFIG_MACLAW_TEST_BUILD 0
#endif

#ifndef CONFIG_MACLAW_PROVISIONING_FAILURE_LIFECYCLE_PRIMITIVES
#define CONFIG_MACLAW_PROVISIONING_FAILURE_LIFECYCLE_PRIMITIVES 0
#endif

#ifndef CONFIG_MACLAW_PROVISIONING_FAILURE_TASK_CREATE
#define CONFIG_MACLAW_PROVISIONING_FAILURE_TASK_CREATE 0
#endif

#ifndef CONFIG_MACLAW_PROVISIONING_FAILURE_TASK_REGISTRY_REGISTER
#define CONFIG_MACLAW_PROVISIONING_FAILURE_TASK_REGISTRY_REGISTER 0
#endif

#ifndef CONFIG_MACLAW_PROVISIONING_TEST_FORCE_PORTAL
#define CONFIG_MACLAW_PROVISIONING_TEST_FORCE_PORTAL 0
#endif
#ifndef CONFIG_MACLAW_SAFE_MODE_TEST_LOCAL_READY_FAILURE
#define CONFIG_MACLAW_SAFE_MODE_TEST_LOCAL_READY_FAILURE 0
#endif
#ifndef CONFIG_MACLAW_SAFE_MODE_TEST_FORCE_SETUP_TAKE_FAILURE
#define CONFIG_MACLAW_SAFE_MODE_TEST_FORCE_SETUP_TAKE_FAILURE 0
#endif
#ifndef CONFIG_MACLAW_DISPLAY_FAILURE_INITIALIZATION
#define CONFIG_MACLAW_DISPLAY_FAILURE_INITIALIZATION 0
#endif
#ifndef CONFIG_MACLAW_DISPLAY_FAILURE_STAGE
#define CONFIG_MACLAW_DISPLAY_FAILURE_STAGE 0
#endif
#ifndef CONFIG_MACLAW_COMPACT_RENDERER_FAILURE_INITIALIZATION
#define CONFIG_MACLAW_COMPACT_RENDERER_FAILURE_INITIALIZATION 0
#endif
#ifndef CONFIG_MACLAW_COMPACT_RENDERER_FAILURE_STAGE
#define CONFIG_MACLAW_COMPACT_RENDERER_FAILURE_STAGE 0
#endif
#ifndef CONFIG_MACLAW_DISPLAY_SERVICE_FAIL_AFTER_INIT
#define CONFIG_MACLAW_DISPLAY_SERVICE_FAIL_AFTER_INIT 0
#endif
#ifndef CONFIG_MACLAW_DISPLAY_SERVICE_STOP_DELAY_MS
#define CONFIG_MACLAW_DISPLAY_SERVICE_STOP_DELAY_MS 0
#endif
#ifndef CONFIG_MACLAW_DISPLAY_SERVICE_REQUEST_DELAY_ONCE_MS
#define CONFIG_MACLAW_DISPLAY_SERVICE_REQUEST_DELAY_ONCE_MS 0
#endif
#ifndef CONFIG_MACLAW_DISPLAY_SERVICE_SECONDARY_STOP_DELAY_MS
#define CONFIG_MACLAW_DISPLAY_SERVICE_SECONDARY_STOP_DELAY_MS 0
#endif
#ifndef CONFIG_MACLAW_DISPLAY_SERVICE_SECONDARY_STOP_TIMEOUT_MS
#define CONFIG_MACLAW_DISPLAY_SERVICE_SECONDARY_STOP_TIMEOUT_MS 0
#endif
#ifndef CONFIG_MACLAW_DISPLAY_TRANSFER_FENCE_TIMEOUT_ONCE
#define CONFIG_MACLAW_DISPLAY_TRANSFER_FENCE_TIMEOUT_ONCE 0
#endif
#ifndef CONFIG_MACLAW_TASK_REGISTRY_LIFECYCLE_TEST
#define CONFIG_MACLAW_TASK_REGISTRY_LIFECYCLE_TEST 0
#endif
#ifndef CONFIG_MACLAW_POWER_LEASE_DISPLAY_OFF_COMMIT_TEST
#define CONFIG_MACLAW_POWER_LEASE_DISPLAY_OFF_COMMIT_TEST 0
#endif
#ifndef CONFIG_MACLAW_POWER_DISPLAY_OFF_RETRY_HIL_TEST
#define CONFIG_MACLAW_POWER_DISPLAY_OFF_RETRY_HIL_TEST 0
#endif
#ifndef CONFIG_MACLAW_SLEEP_SCHEDULE_END_HANDOFF_TEST
#define CONFIG_MACLAW_SLEEP_SCHEDULE_END_HANDOFF_TEST 0
#endif
#ifndef CONFIG_MACLAW_SLEEP_SCHEDULE_END_HANDOFF_HIL_TEST
#define CONFIG_MACLAW_SLEEP_SCHEDULE_END_HANDOFF_HIL_TEST 0
#endif
#ifndef CONFIG_MACLAW_WAVESHARE_QMI8658_INIT_FAILURE
#define CONFIG_MACLAW_WAVESHARE_QMI8658_INIT_FAILURE 0
#endif
#ifndef CONFIG_MACLAW_WAVESHARE_QMI8658_MOTION_READ_FAILURE
#define CONFIG_MACLAW_WAVESHARE_QMI8658_MOTION_READ_FAILURE 0
#endif
#ifndef CONFIG_MACLAW_COMPACT_DISPLAY_ANIMATION_DEADLINE_TEST
#define CONFIG_MACLAW_COMPACT_DISPLAY_ANIMATION_DEADLINE_TEST 0
#endif
#ifndef CONFIG_MACLAW_ROUND_DISPLAY_ANIMATION_DEADLINE_TEST
#define CONFIG_MACLAW_ROUND_DISPLAY_ANIMATION_DEADLINE_TEST 0
#endif

#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_DISPLAY_TRANSFER_FENCE_TIMEOUT_ONCE
static bool s_display_transfer_fence_timeout_consumed;
#endif
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_WAVESHARE_QMI8658_MOTION_READ_FAILURE
static bool s_waveshare_qmi8658_motion_read_probe_seen;
static bool s_waveshare_qmi8658_motion_read_failure_consumed;
#endif
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_DISPLAY_SERVICE_REQUEST_DELAY_ONCE_MS > 0
static bool s_display_service_request_delay_consumed;
#endif

bool provisioning_failure_injection_lifecycle_primitives_unavailable(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_PROVISIONING_FAILURE_LIFECYCLE_PRIMITIVES
    return true;
#else
    return false;
#endif
}

bool provisioning_failure_injection_task_create_fails(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_PROVISIONING_FAILURE_TASK_CREATE
    return true;
#else
    return false;
#endif
}

bool provisioning_failure_injection_task_registry_register_fails(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_PROVISIONING_FAILURE_TASK_REGISTRY_REGISTER
    return true;
#else
    return false;
#endif
}

bool provisioning_failure_injection_force_portal_at_boot(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_PROVISIONING_TEST_FORCE_PORTAL
    return true;
#else
    return false;
#endif
}

bool provisioning_failure_injection_display_initialization_should_fail_after(
    unsigned completed_stage) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_DISPLAY_FAILURE_INITIALIZATION
    return CONFIG_MACLAW_DISPLAY_FAILURE_STAGE == completed_stage;
#else
    (void)completed_stage;
    return false;
#endif
}

bool provisioning_failure_injection_compact_renderer_initialization_should_fail_after(
    unsigned completed_stage) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_COMPACT_RENDERER_FAILURE_INITIALIZATION
    return CONFIG_MACLAW_COMPACT_RENDERER_FAILURE_STAGE == completed_stage;
#else
    (void)completed_stage;
    return false;
#endif
}

bool provisioning_failure_injection_display_service_fail_after_init(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_DISPLAY_SERVICE_FAIL_AFTER_INIT
    return true;
#else
    return false;
#endif
}

uint32_t provisioning_failure_injection_display_service_stop_delay_ms(void) {
#if CONFIG_MACLAW_TEST_BUILD
    return CONFIG_MACLAW_DISPLAY_SERVICE_STOP_DELAY_MS;
#else
    return 0;
#endif
}

uint32_t provisioning_failure_injection_display_service_request_delay_once_ms(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_DISPLAY_SERVICE_REQUEST_DELAY_ONCE_MS > 0
    if (!s_display_service_request_delay_consumed) {
        s_display_service_request_delay_consumed = true;
        return CONFIG_MACLAW_DISPLAY_SERVICE_REQUEST_DELAY_ONCE_MS;
    }
#endif
    return 0;
}

bool provisioning_failure_injection_display_service_request_delay_enabled(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_DISPLAY_SERVICE_REQUEST_DELAY_ONCE_MS > 0
    return true;
#else
    return false;
#endif
}

uint32_t provisioning_failure_injection_display_service_secondary_stop_delay_ms(void) {
#if CONFIG_MACLAW_TEST_BUILD
    return CONFIG_MACLAW_DISPLAY_SERVICE_SECONDARY_STOP_DELAY_MS;
#else
    return 0;
#endif
}

uint32_t provisioning_failure_injection_display_service_secondary_stop_timeout_ms(void) {
#if CONFIG_MACLAW_TEST_BUILD
    return CONFIG_MACLAW_DISPLAY_SERVICE_SECONDARY_STOP_TIMEOUT_MS;
#else
    return 0;
#endif
}

bool provisioning_failure_injection_compact_display_animation_deadline_test_enabled(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_COMPACT_DISPLAY_ANIMATION_DEADLINE_TEST
    return true;
#else
    return false;
#endif
}

uint32_t provisioning_failure_injection_compact_display_animation_pre_completion_delay_ms(void) {
    return provisioning_failure_injection_compact_display_animation_deadline_test_enabled()
               ? 70u
               : 0u;
}

uint32_t provisioning_failure_injection_compact_display_animation_post_completion_delay_ms(void) {
    return provisioning_failure_injection_compact_display_animation_deadline_test_enabled()
               ? 70u
               : 0u;
}

bool provisioning_failure_injection_round_display_animation_deadline_test_enabled(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_ROUND_DISPLAY_ANIMATION_DEADLINE_TEST
    return true;
#else
    return false;
#endif
}

uint32_t provisioning_failure_injection_round_display_animation_pre_completion_delay_ms(void) {
    return provisioning_failure_injection_round_display_animation_deadline_test_enabled()
               ? 70u
               : 0u;
}

uint32_t provisioning_failure_injection_round_display_animation_post_completion_delay_ms(void) {
    return provisioning_failure_injection_round_display_animation_deadline_test_enabled()
               ? 70u
               : 0u;
}

bool provisioning_failure_injection_display_transfer_fence_timeout_once(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_DISPLAY_TRANSFER_FENCE_TIMEOUT_ONCE
    if (!s_display_transfer_fence_timeout_consumed) {
        s_display_transfer_fence_timeout_consumed = true;
        return true;
    }
#endif
    return false;
}

bool provisioning_failure_injection_task_registry_lifecycle_test_enabled(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_TASK_REGISTRY_LIFECYCLE_TEST
    return true;
#else
    return false;
#endif
}

bool provisioning_failure_injection_power_lease_display_off_commit_test_enabled(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_POWER_LEASE_DISPLAY_OFF_COMMIT_TEST
    return true;
#else
    return false;
#endif
}

bool provisioning_failure_injection_power_display_off_retry_hil_test_enabled(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_POWER_DISPLAY_OFF_RETRY_HIL_TEST
    return true;
#else
    return false;
#endif
}

bool provisioning_failure_injection_sleep_schedule_end_handoff_test_enabled(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_SLEEP_SCHEDULE_END_HANDOFF_TEST
    return true;
#else
    return false;
#endif
}

bool provisioning_failure_injection_sleep_schedule_end_handoff_hil_test_enabled(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_SLEEP_SCHEDULE_END_HANDOFF_HIL_TEST
    return true;
#else
    return false;
#endif
}

bool provisioning_failure_injection_safe_mode_at_local_ready(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_SAFE_MODE_TEST_LOCAL_READY_FAILURE
    return true;
#else
    return false;
#endif
}

bool provisioning_failure_injection_safe_mode_force_setup_take_fails(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_SAFE_MODE_TEST_FORCE_SETUP_TAKE_FAILURE
    return true;
#else
    return false;
#endif
}

bool provisioning_failure_injection_waveshare_qmi8658_init_fails(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_WAVESHARE_QMI8658_INIT_FAILURE
    return true;
#else
    return false;
#endif
}

bool provisioning_failure_injection_waveshare_qmi8658_motion_read_fails_once(void) {
#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_WAVESHARE_QMI8658_MOTION_READ_FAILURE
    /* The first call is Fall Detection's shared boot availability probe. Let
     * it exercise the real adapter, then fail exactly one retained-worker
     * sample so the test cannot accidentally turn into the init-failure case. */
    if (!s_waveshare_qmi8658_motion_read_probe_seen) {
        s_waveshare_qmi8658_motion_read_probe_seen = true;
        return false;
    }
    if (!s_waveshare_qmi8658_motion_read_failure_consumed) {
        s_waveshare_qmi8658_motion_read_failure_consumed = true;
        return true;
    }
#endif
    return false;
}
