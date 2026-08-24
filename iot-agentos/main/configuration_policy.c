#include "configuration_policy.h"

#include <stddef.h>

#define CONFIGURATION_POLICY_MAX_RUNTIME_OVERRIDE_TTL_MS (15u * 60u * 1000u)

static bool request_has_current_abi(const configuration_policy_request_t *request) {
    return request && request->struct_size == sizeof(*request) &&
           request->abi_version == CONFIGURATION_POLICY_REQUEST_ABI_VERSION;
}

static bool source_is_known(configuration_source_t source) {
    return source >= CONFIGURATION_SOURCE_COMPILED_DEFAULT &&
           source <= CONFIGURATION_SOURCE_RUNTIME_OVERRIDE;
}

static bool key_is_known(configuration_key_t key) {
    return key >= CONFIGURATION_KEY_OUTPUT_VOLUME &&
           key <= CONFIGURATION_KEY_FORCE_SETUP;
}

configuration_policy_decision_t configuration_policy_authorize(
    configuration_key_t key, const configuration_policy_request_t *request) {
    if (!key_is_known(key) || !request_has_current_abi(request) ||
        !source_is_known(request->source)) {
        return CONFIGURATION_POLICY_DENIED;
    }

    switch (request->source) {
        case CONFIGURATION_SOURCE_COMPILED_DEFAULT:
        case CONFIGURATION_SOURCE_MANUFACTURING_MANIFEST:
            /* These are build/production inputs, never runtime mutation
             * authorities. They cannot be impersonated by Hub or UI calls. */
            return CONFIGURATION_POLICY_DENIED;

        case CONFIGURATION_SOURCE_USER_LOCAL:
            /* Explicit local intent is durable product configuration. It must
             * not carry a TTL because expiring a persisted user choice would
             * create an ambiguous recovery policy. */
            return request->ttl_ms == 0u ? CONFIGURATION_POLICY_ALLOW_DURABLE
                                         : CONFIGURATION_POLICY_DENIED;

        case CONFIGURATION_SOURCE_HUB_AUTHENTICATED:
            /* Remote configuration is restricted to normal product policy;
             * it cannot rewrite Wi-Fi/EAP credentials, issue pairing evidence,
             * or trigger an unbounded/unauthenticated override. */
            if (!request->authenticated || request->ttl_ms != 0u ||
                key == CONFIGURATION_KEY_PROVISIONING_CREDENTIALS ||
                key == CONFIGURATION_KEY_GATEWAY_PAIRING_TOKEN ||
                key == CONFIGURATION_KEY_FORCE_SETUP) {
                return CONFIGURATION_POLICY_DENIED;
            }
            return CONFIGURATION_POLICY_ALLOW_DURABLE;

        case CONFIGURATION_SOURCE_RUNTIME_OVERRIDE:
            /* Runtime override support is deliberately not enabled for any
             * durable key yet. Only reversible user-policy knobs may ever
             * enter its future ephemeral store: credentials, pairing evidence
             * and factory/setup control are never transient remote state.
             * Preserve the bounded TTL validation so a future implementation
             * cannot accidentally accept infinity. */
            if (key != CONFIGURATION_KEY_OUTPUT_VOLUME &&
                key != CONFIGURATION_KEY_DISPLAY_BRIGHTNESS &&
                key != CONFIGURATION_KEY_SCREEN_SLEEP_SECONDS &&
                key != CONFIGURATION_KEY_TRANSPORT_SELECTION) {
                return CONFIGURATION_POLICY_DENIED;
            }
            return request->authenticated && request->ttl_ms > 0u &&
                   request->ttl_ms <= CONFIGURATION_POLICY_MAX_RUNTIME_OVERRIDE_TTL_MS
                       ? CONFIGURATION_POLICY_ALLOW_EPHEMERAL
                       : CONFIGURATION_POLICY_DENIED;

        default:
            return CONFIGURATION_POLICY_DENIED;
    }
}

const char *configuration_policy_source_name(configuration_source_t source) {
    switch (source) {
        case CONFIGURATION_SOURCE_COMPILED_DEFAULT: return "compiled_default";
        case CONFIGURATION_SOURCE_MANUFACTURING_MANIFEST: return "manufacturing_manifest";
        case CONFIGURATION_SOURCE_USER_LOCAL: return "user_local";
        case CONFIGURATION_SOURCE_HUB_AUTHENTICATED: return "hub_authenticated";
        case CONFIGURATION_SOURCE_RUNTIME_OVERRIDE: return "runtime_override";
        default: return "invalid";
    }
}

const char *configuration_policy_key_name(configuration_key_t key) {
    switch (key) {
        case CONFIGURATION_KEY_OUTPUT_VOLUME: return "output_volume";
        case CONFIGURATION_KEY_DISPLAY_BRIGHTNESS: return "display_brightness";
        case CONFIGURATION_KEY_SCREEN_SLEEP_SECONDS: return "screen_sleep_seconds";
        case CONFIGURATION_KEY_TRANSPORT_SELECTION: return "transport_selection";
        case CONFIGURATION_KEY_WIFI_CATALOGUE: return "wifi_catalogue";
        case CONFIGURATION_KEY_PROVISIONING_CREDENTIALS: return "provisioning_credentials";
        case CONFIGURATION_KEY_GATEWAY_PAIRING_TOKEN: return "gateway_pairing_token";
        case CONFIGURATION_KEY_FORCE_SETUP: return "force_setup";
        default: return "invalid";
    }
}
