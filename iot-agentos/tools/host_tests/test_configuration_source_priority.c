#include <stdio.h>

#include "configuration_source_priority.h"

#define CHECK(expr) do { \
    if (!(expr)) { \
        fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__, #expr); \
        return 1; \
    } \
} while (0)

static configuration_source_candidate_t candidate(
    configuration_source_t source, bool present, bool authenticated, uint64_t ttl_ms) {
    return (configuration_source_candidate_t){
        .struct_size = sizeof(configuration_source_candidate_t),
        .abi_version = CONFIGURATION_SOURCE_CANDIDATE_ABI_VERSION,
        .source = source,
        .present = present,
        .provenance = {
            .struct_size = sizeof(configuration_policy_request_t),
            .abi_version = CONFIGURATION_POLICY_REQUEST_ABI_VERSION,
            .source = source,
            .authenticated = authenticated,
            .ttl_ms = ttl_ms,
        },
    };
}

int main(void) {
    CHECK(configuration_source_priority(CONFIGURATION_SOURCE_INVALID) == 0u);
    CHECK(configuration_source_priority(CONFIGURATION_SOURCE_COMPILED_DEFAULT) <
          configuration_source_priority(CONFIGURATION_SOURCE_MANUFACTURING_MANIFEST));
    CHECK(configuration_source_priority(CONFIGURATION_SOURCE_MANUFACTURING_MANIFEST) <
          configuration_source_priority(CONFIGURATION_SOURCE_USER_LOCAL));
    CHECK(configuration_source_priority(CONFIGURATION_SOURCE_USER_LOCAL) <
          configuration_source_priority(CONFIGURATION_SOURCE_HUB_AUTHENTICATED));
    CHECK(configuration_source_priority(CONFIGURATION_SOURCE_HUB_AUTHENTICATED) <
          configuration_source_priority(CONFIGURATION_SOURCE_RUNTIME_OVERRIDE));

    configuration_source_candidate_t inputs[] = {
        candidate(CONFIGURATION_SOURCE_COMPILED_DEFAULT, true, false, 0u),
        candidate(CONFIGURATION_SOURCE_MANUFACTURING_MANIFEST, true, true, 0u),
        candidate(CONFIGURATION_SOURCE_USER_LOCAL, true, false, 0u),
        candidate(CONFIGURATION_SOURCE_HUB_AUTHENTICATED, true, true, 0u),
        candidate(CONFIGURATION_SOURCE_RUNTIME_OVERRIDE, true, true, 1000u),
    };
    configuration_source_selection_t selected = {0};
    CHECK(configuration_source_priority_resolve(CONFIGURATION_KEY_OUTPUT_VOLUME,
                                                inputs, 5u, &selected) ==
          CONFIGURATION_SOURCE_RESOLVE_OK);
    CHECK(selected.struct_size == sizeof(selected));
    CHECK(selected.abi_version == CONFIGURATION_SOURCE_SELECTION_ABI_VERSION);
    CHECK(selected.source == CONFIGURATION_SOURCE_RUNTIME_OVERRIDE &&
          selected.priority == configuration_source_priority(selected.source) &&
          selected.candidate_index == 4u);

    inputs[4].present = false;
    CHECK(configuration_source_priority_resolve(CONFIGURATION_KEY_OUTPUT_VOLUME,
                                                inputs, 5u, &selected) ==
          CONFIGURATION_SOURCE_RESOLVE_OK);
    CHECK(selected.source == CONFIGURATION_SOURCE_HUB_AUTHENTICATED &&
          selected.candidate_index == 3u);

    inputs[3].provenance.authenticated = false;
    CHECK(configuration_source_priority_resolve(CONFIGURATION_KEY_OUTPUT_VOLUME,
                                                inputs, 5u, &selected) ==
          CONFIGURATION_SOURCE_RESOLVE_INVALID_CANDIDATE);
    inputs[3].provenance.authenticated = true;
    inputs[0].present = false;
    inputs[1].present = false;
    inputs[2].present = false;
    inputs[3].present = false;
    CHECK(configuration_source_priority_resolve(CONFIGURATION_KEY_OUTPUT_VOLUME,
                                                inputs, 5u, &selected) ==
          CONFIGURATION_SOURCE_RESOLVE_NO_CANDIDATE);

    configuration_source_candidate_t duplicate[] = {
        candidate(CONFIGURATION_SOURCE_USER_LOCAL, true, false, 0u),
        candidate(CONFIGURATION_SOURCE_USER_LOCAL, true, false, 0u),
    };
    CHECK(configuration_source_priority_resolve(CONFIGURATION_KEY_OUTPUT_VOLUME,
                                                duplicate, 2u, &selected) ==
          CONFIGURATION_SOURCE_RESOLVE_INVALID_CANDIDATE);

    configuration_source_candidate_t blocked =
        candidate(CONFIGURATION_SOURCE_RUNTIME_OVERRIDE, true, true, 1u);
    CHECK(configuration_source_priority_resolve(CONFIGURATION_KEY_GATEWAY_PAIRING_TOKEN,
                                                &blocked, 1u, &selected) ==
          CONFIGURATION_SOURCE_RESOLVE_INVALID_CANDIDATE);
    CHECK(configuration_source_priority_resolve(CONFIGURATION_KEY_INVALID,
                                                inputs, 5u, &selected) ==
          CONFIGURATION_SOURCE_RESOLVE_INVALID_ARGUMENT);

    puts("PASS configuration source priority selects one authorized per-key fact deterministically");
    return 0;
}
