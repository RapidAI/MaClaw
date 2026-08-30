#include <stdio.h>
#include <string.h>

#include "configuration_factory_reset_policy.h"

#define CHECK(expr) do { if (!(expr)) { \
    fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__, #expr); return 1; \
} } while (0)

static configuration_factory_reset_request_t request(void) {
    return (configuration_factory_reset_request_t){
        .struct_size = sizeof(configuration_factory_reset_request_t),
        .abi_version = CONFIGURATION_FACTORY_RESET_POLICY_ABI_VERSION,
        .source = CONFIGURATION_SOURCE_USER_LOCAL,
        .authenticated = true,
        .explicit_confirmation = true,
        .generation = 9u,
        .classes = CONFIGURATION_FACTORY_RESET_CLASS_ALL,
    };
}

int main(void) {
    configuration_factory_reset_request_t req = request();
    CHECK(configuration_factory_reset_authorize(&req));
    CHECK((CONFIGURATION_FACTORY_RESET_CLASS_ALL &
           CONFIGURATION_FACTORY_RESET_CLASS_MEETING_RECORDING) != 0u);
    CHECK((CONFIGURATION_FACTORY_RESET_CLASS_ALL &
           CONFIGURATION_FACTORY_RESET_CLASS_PET_CACHE) != 0u);
    req.authenticated = false;
    CHECK(!configuration_factory_reset_authorize(&req));
    req = request();
    req.classes = CONFIGURATION_FACTORY_RESET_CLASS_CONFIGURATION;
    CHECK(!configuration_factory_reset_authorize(&req));
    req = request();
    req.explicit_confirmation = false;
    CHECK(!configuration_factory_reset_authorize(&req));

    configuration_factory_reset_journal_t journal = {0};
    uint8_t encoded[sizeof(journal)] = {0};
    CHECK(configuration_factory_reset_journal_begin(
        &journal, CONFIGURATION_FACTORY_RESET_CLASS_ALL, 9u));
    CHECK(configuration_factory_reset_journal_validate(&journal));
    CHECK(configuration_factory_reset_journal_commit(&journal));
    CHECK(journal.stage == CONFIGURATION_FACTORY_RESET_STAGE_COMMITTED);
    CHECK(!configuration_factory_reset_journal_commit(&journal));
    CHECK(configuration_factory_reset_journal_encode(&journal, encoded));
    configuration_factory_reset_journal_t decoded = {0};
    CHECK(configuration_factory_reset_journal_decode(encoded, sizeof(encoded), &decoded));
    encoded[0] ^= 1u;
    CHECK(!configuration_factory_reset_journal_decode(encoded, sizeof(encoded), &decoded));
    puts("PASS factory reset authorization and journal contract fail closed");
    return 0;
}
