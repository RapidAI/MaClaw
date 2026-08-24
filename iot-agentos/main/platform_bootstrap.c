#include "platform_bootstrap.h"

#include "platform_bootstrap_profile.h"

device_status_t platform_bootstrap_initialize(void) {
    return platform_bootstrap_profile_initialize();
}
