#include <stdio.h>

#include "configuration_policy.h"

#define CHECK(expr) do { \
    if (!(expr)) { \
        fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__, #expr); \
        return 1; \
    } \
} while (0)

static configuration_policy_request_t request(configuration_source_t source,
                                              bool authenticated, uint64_t ttl_ms) {
    return (configuration_policy_request_t){
        .struct_size = sizeof(configuration_policy_request_t),
        .abi_version = CONFIGURATION_POLICY_REQUEST_ABI_VERSION,
        .source = source,
        .authenticated = authenticated,
        .ttl_ms = ttl_ms,
    };
}

int main(void) {
    configuration_policy_request_t local =
        request(CONFIGURATION_SOURCE_USER_LOCAL, true, 0u);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_OUTPUT_VOLUME, &local) ==
          CONFIGURATION_POLICY_ALLOW_DURABLE);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_PROVISIONING_CREDENTIALS, &local) ==
          CONFIGURATION_POLICY_ALLOW_DURABLE);

    configuration_policy_request_t hub =
        request(CONFIGURATION_SOURCE_HUB_AUTHENTICATED, true, 0u);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_OUTPUT_VOLUME, &hub) ==
          CONFIGURATION_POLICY_ALLOW_DURABLE);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_TRANSPORT_SELECTION, &hub) ==
          CONFIGURATION_POLICY_ALLOW_DURABLE);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_DISPLAY_BRIGHTNESS, &hub) ==
          CONFIGURATION_POLICY_ALLOW_DURABLE);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_SCREEN_SLEEP_SECONDS, &hub) ==
          CONFIGURATION_POLICY_ALLOW_DURABLE);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_PROVISIONING_CREDENTIALS, &hub) ==
          CONFIGURATION_POLICY_DENIED);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_GATEWAY_PAIRING_TOKEN, &hub) ==
          CONFIGURATION_POLICY_DENIED);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_FORCE_SETUP, &hub) ==
          CONFIGURATION_POLICY_DENIED);

    hub.authenticated = false;
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_OUTPUT_VOLUME, &hub) ==
          CONFIGURATION_POLICY_DENIED);
    hub.authenticated = true;
    hub.ttl_ms = 1u;
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_OUTPUT_VOLUME, &hub) ==
          CONFIGURATION_POLICY_DENIED);

    configuration_policy_request_t runtime =
        request(CONFIGURATION_SOURCE_RUNTIME_OVERRIDE, true, 60000u);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_OUTPUT_VOLUME, &runtime) ==
          CONFIGURATION_POLICY_ALLOW_EPHEMERAL);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_TRANSPORT_SELECTION, &runtime) ==
          CONFIGURATION_POLICY_ALLOW_EPHEMERAL);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_DISPLAY_BRIGHTNESS, &runtime) ==
          CONFIGURATION_POLICY_ALLOW_EPHEMERAL);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_SCREEN_SLEEP_SECONDS, &runtime) ==
          CONFIGURATION_POLICY_ALLOW_EPHEMERAL);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_WIFI_CATALOGUE, &runtime) ==
          CONFIGURATION_POLICY_DENIED);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_PROVISIONING_CREDENTIALS, &runtime) ==
          CONFIGURATION_POLICY_DENIED);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_GATEWAY_PAIRING_TOKEN, &runtime) ==
          CONFIGURATION_POLICY_DENIED);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_FORCE_SETUP, &runtime) ==
          CONFIGURATION_POLICY_DENIED);
    runtime.ttl_ms = 16u * 60u * 1000u;
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_OUTPUT_VOLUME, &runtime) ==
          CONFIGURATION_POLICY_DENIED);

    local.struct_size = 0u;
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_OUTPUT_VOLUME, &local) ==
          CONFIGURATION_POLICY_DENIED);
    CHECK(configuration_policy_authorize(CONFIGURATION_KEY_INVALID, &hub) ==
          CONFIGURATION_POLICY_DENIED);

    puts("PASS configuration source authority and TTL policy fail closed");
    return 0;
}
