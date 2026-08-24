#include "configuration_source_priority.h"

#include <string.h>

static bool candidate_has_current_abi(const configuration_source_candidate_t *candidate) {
    return candidate && candidate->struct_size == sizeof(*candidate) &&
           candidate->abi_version == CONFIGURATION_SOURCE_CANDIDATE_ABI_VERSION &&
           candidate->provenance.struct_size == sizeof(candidate->provenance) &&
           candidate->provenance.abi_version == CONFIGURATION_POLICY_REQUEST_ABI_VERSION &&
           candidate->provenance.source == candidate->source;
}

static bool key_is_known(configuration_key_t key) {
    return key >= CONFIGURATION_KEY_OUTPUT_VOLUME &&
           key <= CONFIGURATION_KEY_FORCE_SETUP;
}

static bool source_allows_compiled_default(configuration_key_t key) {
    return key == CONFIGURATION_KEY_OUTPUT_VOLUME ||
           key == CONFIGURATION_KEY_DISPLAY_BRIGHTNESS ||
           key == CONFIGURATION_KEY_SCREEN_SLEEP_SECONDS ||
           key == CONFIGURATION_KEY_TRANSPORT_SELECTION ||
           key == CONFIGURATION_KEY_WIFI_CATALOGUE;
}

static bool source_allows_manufacturing_manifest(configuration_key_t key) {
    /* A verified manufacturing manifest may seed calibrated product defaults
     * and factory connectivity. Pairing evidence and recovery controls are
     * deliberately never manufacturing policy. */
    return key != CONFIGURATION_KEY_GATEWAY_PAIRING_TOKEN &&
           key != CONFIGURATION_KEY_FORCE_SETUP;
}

uint32_t configuration_source_priority(configuration_source_t source) {
    switch (source) {
        case CONFIGURATION_SOURCE_COMPILED_DEFAULT: return 10u;
        case CONFIGURATION_SOURCE_MANUFACTURING_MANIFEST: return 20u;
        case CONFIGURATION_SOURCE_USER_LOCAL: return 30u;
        case CONFIGURATION_SOURCE_HUB_AUTHENTICATED: return 40u;
        case CONFIGURATION_SOURCE_RUNTIME_OVERRIDE: return 50u;
        default: return 0u;
    }
}

static bool candidate_is_authorized(configuration_key_t key,
                                    const configuration_source_candidate_t *candidate) {
    if (!candidate_has_current_abi(candidate) || !key_is_known(key) ||
        configuration_source_priority(candidate->source) == 0u) {
        return false;
    }
    switch (candidate->source) {
        case CONFIGURATION_SOURCE_COMPILED_DEFAULT:
            return !candidate->provenance.authenticated &&
                   candidate->provenance.ttl_ms == 0u &&
                   source_allows_compiled_default(key);
        case CONFIGURATION_SOURCE_MANUFACTURING_MANIFEST:
            return candidate->provenance.authenticated &&
                   candidate->provenance.ttl_ms == 0u &&
                   source_allows_manufacturing_manifest(key);
        case CONFIGURATION_SOURCE_USER_LOCAL:
        case CONFIGURATION_SOURCE_HUB_AUTHENTICATED:
            return configuration_policy_authorize(key, &candidate->provenance) ==
                   CONFIGURATION_POLICY_ALLOW_DURABLE;
        case CONFIGURATION_SOURCE_RUNTIME_OVERRIDE:
            return configuration_policy_authorize(key, &candidate->provenance) ==
                   CONFIGURATION_POLICY_ALLOW_EPHEMERAL;
        default:
            return false;
    }
}

configuration_source_resolve_result_t configuration_source_priority_resolve(
    configuration_key_t key,
    const configuration_source_candidate_t *candidates,
    size_t candidate_count,
    configuration_source_selection_t *out_selection) {
    if (!key_is_known(key) || !candidates || candidate_count == 0u || !out_selection) {
        return CONFIGURATION_SOURCE_RESOLVE_INVALID_ARGUMENT;
    }

    bool seen_present_source[CONFIGURATION_SOURCE_RUNTIME_OVERRIDE + 1u] = {false};
    bool found = false;
    uint32_t winning_priority = 0u;
    size_t winning_index = 0u;
    for (size_t i = 0u; i < candidate_count; ++i) {
        const configuration_source_candidate_t *candidate = &candidates[i];
        if (!candidate_is_authorized(key, candidate)) {
            return CONFIGURATION_SOURCE_RESOLVE_INVALID_CANDIDATE;
        }
        if (!candidate->present) continue;
        const unsigned source_index = (unsigned)candidate->source;
        if (seen_present_source[source_index]) {
            return CONFIGURATION_SOURCE_RESOLVE_INVALID_CANDIDATE;
        }
        seen_present_source[source_index] = true;
        const uint32_t priority = configuration_source_priority(candidate->source);
        if (!found || priority > winning_priority) {
            found = true;
            winning_priority = priority;
            winning_index = i;
        }
    }
    if (!found) return CONFIGURATION_SOURCE_RESOLVE_NO_CANDIDATE;
    *out_selection = (configuration_source_selection_t){
        .struct_size = sizeof(*out_selection),
        .abi_version = CONFIGURATION_SOURCE_SELECTION_ABI_VERSION,
        .source = candidates[winning_index].source,
        .priority = winning_priority,
        .candidate_index = winning_index,
    };
    return CONFIGURATION_SOURCE_RESOLVE_OK;
}
