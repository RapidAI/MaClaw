#pragma once

/*
 * Configuration source/authority value contract.
 *
 * Configuration Service remains the sole durable owner. This module only
 * describes which source may request a product setting and whether the
 * request may become durable. It intentionally has no NVS, clock, network,
 * RTOS, driver or board dependency so every provisioning/control surface
 * shares the same security boundary.
 */

#include <stdbool.h>
#include <stdint.h>

typedef enum {
    CONFIGURATION_SOURCE_INVALID = 0,
    CONFIGURATION_SOURCE_COMPILED_DEFAULT,
    CONFIGURATION_SOURCE_MANUFACTURING_MANIFEST,
    CONFIGURATION_SOURCE_USER_LOCAL,
    CONFIGURATION_SOURCE_HUB_AUTHENTICATED,
    CONFIGURATION_SOURCE_RUNTIME_OVERRIDE,
} configuration_source_t;

typedef enum {
    CONFIGURATION_KEY_INVALID = 0,
    CONFIGURATION_KEY_OUTPUT_VOLUME,
    CONFIGURATION_KEY_DISPLAY_BRIGHTNESS,
    CONFIGURATION_KEY_SCREEN_SLEEP_SECONDS,
    CONFIGURATION_KEY_TRANSPORT_SELECTION,
    CONFIGURATION_KEY_WIFI_CATALOGUE,
    CONFIGURATION_KEY_PROVISIONING_CREDENTIALS,
    CONFIGURATION_KEY_GATEWAY_PAIRING_TOKEN,
    CONFIGURATION_KEY_FORCE_SETUP,
} configuration_key_t;

typedef enum {
    CONFIGURATION_POLICY_DENIED = 0,
    CONFIGURATION_POLICY_ALLOW_DURABLE,
    CONFIGURATION_POLICY_ALLOW_EPHEMERAL,
} configuration_policy_decision_t;

/* Request metadata is intentionally declarative. `authenticated` represents
 * already-verified transport identity; this value layer neither parses Hub
 * messages nor holds credentials. TTL is monotonic time supplied by the
 * caller: durable requests require zero TTL, while a future explicitly
 * supported runtime override must have a bounded non-zero TTL. */
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    configuration_source_t source;
    bool authenticated;
    uint64_t ttl_ms;
} configuration_policy_request_t;

#define CONFIGURATION_POLICY_REQUEST_ABI_VERSION 1u

/* Returns the complete policy decision for a request. Unknown enums, ABI/size
 * mismatches and unsupported source/key combinations fail closed. */
configuration_policy_decision_t configuration_policy_authorize(
    configuration_key_t key, const configuration_policy_request_t *request);

/* Stable names for diagnostics/tests. They expose no secret configuration
 * values and are never used as protocol identifiers. */
const char *configuration_policy_source_name(configuration_source_t source);
const char *configuration_policy_key_name(configuration_key_t key);
