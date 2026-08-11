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
