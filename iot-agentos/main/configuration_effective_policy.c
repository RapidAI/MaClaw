#include "configuration_effective_policy.h"

#include <limits.h>

#include "configuration_source_priority.h"

static bool valid_screen_sleep_seconds(uint32_t seconds) {
    switch (seconds) {
        case 0u: case 60u: case 180u: case 300u: case 600u: case 1800u:
        case 3600u: case 7200u: case 10800u: case 14400u: case 18000u:
            return true;
        default:
            return false;
    }
}

static configuration_key_t override_key(
    configuration_runtime_override_value_kind_t kind) {
    switch (kind) {
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_OUTPUT_VOLUME:
            return CONFIGURATION_KEY_OUTPUT_VOLUME;
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_DISPLAY_BRIGHTNESS:
            return CONFIGURATION_KEY_DISPLAY_BRIGHTNESS;
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_SCREEN_SLEEP_SECONDS:
            return CONFIGURATION_KEY_SCREEN_SLEEP_SECONDS;
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_TRANSPORT_SELECTION:
            return CONFIGURATION_KEY_TRANSPORT_SELECTION;
        default:
            return CONFIGURATION_KEY_INVALID;
    }
}

static bool override_value_is_valid(const configuration_runtime_override_t *override) {
    if (!override) return false;
    switch (override->kind) {
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_OUTPUT_VOLUME:
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_DISPLAY_BRIGHTNESS:
            return override->value_u32 <= 100u;
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_SCREEN_SLEEP_SECONDS:
            return valid_screen_sleep_seconds(override->value_u32);
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_TRANSPORT_SELECTION:
            return override->value_u32 <= 1u;
        default:
            return false;
    }
}

bool configuration_runtime_override_validate(
    const configuration_runtime_override_t *override,
    uint64_t now_monotonic_ms) {
    if (!override || override->struct_size != sizeof(*override) ||
        override->abi_version != CONFIGURATION_RUNTIME_OVERRIDE_ABI_VERSION ||
        override->expires_at_monotonic_ms == 0u ||
        override->expires_at_monotonic_ms <= now_monotonic_ms ||
        !override_value_is_valid(override) ||
        override->provenance.struct_size != sizeof(override->provenance) ||
        override->provenance.abi_version != CONFIGURATION_POLICY_REQUEST_ABI_VERSION ||
        override->provenance.source != CONFIGURATION_SOURCE_RUNTIME_OVERRIDE ||
        /* The expiry timestamp is the sole TTL fact. A record with a second,
         * caller-supplied TTL could otherwise be audited differently at put
         * and resolve time. */
        override->provenance.ttl_ms != 0u) {
        return false;
    }
    configuration_policy_request_t authority = override->provenance;
    authority.ttl_ms = override->expires_at_monotonic_ms - now_monotonic_ms;
    const configuration_source_candidate_t candidate = {
        .struct_size = sizeof(candidate),
        .abi_version = CONFIGURATION_SOURCE_CANDIDATE_ABI_VERSION,
        .source = CONFIGURATION_SOURCE_RUNTIME_OVERRIDE,
        .present = true,
        .provenance = authority,
    };
    configuration_source_selection_t selected = {0};
    return configuration_source_priority_resolve(override_key(override->kind),
                                                 &candidate, 1u, &selected) ==
               CONFIGURATION_SOURCE_RESOLVE_OK &&
           selected.source == CONFIGURATION_SOURCE_RUNTIME_OVERRIDE;
}

bool configuration_effective_policy_resolve(
    const configuration_snapshot_t *durable,
    const configuration_runtime_override_t *override,
    uint64_t now_monotonic_ms,
    configuration_snapshot_t *out_effective,
    bool *out_override_active) {
    if (!durable || !out_effective || !out_override_active ||
        durable->output_volume > 100u || durable->display_brightness > 100u ||
        (durable->screen_sleep_seconds_saved &&
         !valid_screen_sleep_seconds(durable->screen_sleep_seconds))) {
        return false;
    }
    *out_effective = *durable;
    *out_override_active = false;
    if (!override) return true;
    if (!configuration_runtime_override_validate(override, now_monotonic_ms)) return false;
    switch (override->kind) {
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_OUTPUT_VOLUME:
            out_effective->output_volume = (uint8_t)override->value_u32;
            out_effective->output_volume_saved = true;
            break;
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_DISPLAY_BRIGHTNESS:
            out_effective->display_brightness = (uint8_t)override->value_u32;
            out_effective->display_brightness_saved = true;
            break;
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_SCREEN_SLEEP_SECONDS:
            out_effective->screen_sleep_seconds = override->value_u32;
            out_effective->screen_sleep_seconds_saved = true;
            break;
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_TRANSPORT_SELECTION:
            out_effective->cellular_transport_selected = override->value_u32 != 0u;
            out_effective->cellular_transport_selection_saved = true;
            break;
        default:
            return false;
    }
    *out_override_active = true;
    return true;
}
