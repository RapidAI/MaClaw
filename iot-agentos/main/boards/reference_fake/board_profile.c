#include "board_profile.h"

/*
 * Reference/Fake HAL profile — Phase 8 extensibility proof.
 *
 * This profile exists only to prove that new hardware can be added by
 * implementing the stable Device API + private adapter contracts, without
 * touching shared business services (command/reply/meeting/alarm etc.).
 * It reuses the compact (direct-I2S) physical shape (240x320, single primary
 * control) and therefore can share the compact service implementations.  No
 * business-layer `#if CONFIG_MACLAW_BOARD_*` or renderer-specific fork is required.
 *
 * Product decisions (BASELINE_EXISTING vs BASELINE_PROMOTED vs PHYSICAL_EXTENSION)
 * still apply: a formal release must still ship Bread/EchoEar/Fangtang/Waveshare
 * with identical business baselines.  This fake profile is never part of the
 * formal release set — it is a CI/host-test harness for HAL ABI compatibility.
 */

bool board_profile_get(device_profile_t *out_profile) {
    if (!out_profile) return false;
    *out_profile = (device_profile_t){
        .struct_size = sizeof(device_profile_t),
        .abi_version = DEVICE_PROFILE_ABI_VERSION,
        .id = "reference-fake-v1",
        .display_width = 240,
        .display_height = 320,
        .capabilities = DEVICE_CAPABILITY_REQUIRED_BASELINE,
        .primary_interaction_source = DEVICE_INPUT_SOURCE_PRIMARY_CONTROL,
        .primary_interaction_label = "激活键",
        .volume_interaction_hint = "音量键调节音量",
        .display_wake_sources =
            DEVICE_INPUT_SOURCE_FLAG(DEVICE_INPUT_SOURCE_PRIMARY_CONTROL),
    };
    return true;
}
