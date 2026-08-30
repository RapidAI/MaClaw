#include <stdio.h>
#include <string.h>
#include "services/credential_service.h"

#define CHECK(x) do { if (!(x)) { fprintf(stderr, "failed: %s\n", #x); return 1; } } while (0)

static uint64_t s_floor;
static bool s_floor_present;
static bool s_floor_write_fail;
static device_status_t read_floor(uint64_t *out, void *context) {
    (void)context;
    if (!out) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!s_floor_present) return DEVICE_STATUS_NOT_FOUND;
    *out = s_floor;
    return DEVICE_STATUS_OK;
}
static device_status_t write_floor(uint64_t floor, void *context) {
    (void)context;
    if (s_floor_write_fail) return DEVICE_STATUS_IO_ERROR;
    s_floor = floor;
    s_floor_present = true;
    return DEVICE_STATUS_OK;
}

int main(void) {
    /* The transport may allocate a process-local generation before the
     * persistence bridge is installed.  A durable floor behind that value is
     * repaired upward rather than treated as corruption. */
    s_floor = 1u;
    s_floor_present = true;
    CHECK(credential_service_init() == DEVICE_STATUS_OK);
    uint32_t early_generation = 0;
    CHECK(credential_service_begin_generation(&early_generation) == DEVICE_STATUS_OK);
    CHECK(credential_service_set_generation_persistence(read_floor, write_floor, NULL) ==
          DEVICE_STATUS_OK);
    CHECK(s_floor == early_generation);

    /* Values outside the 32-bit generation ABI are corruption, not a
     * candidate to truncate. */
    s_floor = (uint64_t)UINT32_MAX + 1u;
    CHECK(credential_service_set_generation_persistence(read_floor, write_floor, NULL) ==
          DEVICE_STATUS_INTERNAL_ERROR);

    /* Simulate a reboot where the durable generation floor is already ahead
     * of the service's process-local value.  Rebinding the bridge must adopt
     * the higher floor and continue monotonically. */
    s_floor = 100u;
    CHECK(credential_service_set_generation_persistence(read_floor, write_floor, NULL) ==
          DEVICE_STATUS_OK);
    uint32_t generation = 0;
    CHECK(credential_service_begin_generation(&generation) == DEVICE_STATUS_OK);
    CHECK(generation > 100u);
    CHECK(credential_service_bind_identity(generation, "device-a") == DEVICE_STATUS_OK);
    CHECK(generation != 0);
    CHECK(credential_service_store_gateway_token(generation, "alpha") == DEVICE_STATUS_OK);
    char token[16] = {0}; size_t length = 0;
    CHECK(credential_service_copy_gateway_token(generation, token, sizeof(token), &length, "device-a") == DEVICE_STATUS_OK);
    CHECK(length == 5 && strcmp(token, "alpha") == 0);
    CHECK(credential_service_revoke_gateway_token(generation) == DEVICE_STATUS_OK);
    CHECK(s_floor_present && s_floor >= generation);
    CHECK(credential_service_copy_gateway_token(generation, token, sizeof(token), &length, "device-a") == DEVICE_STATUS_BUSY);
    uint32_t next = 0;
    CHECK(credential_service_begin_generation(&next) == DEVICE_STATUS_OK && next != generation);
    CHECK(credential_service_bind_identity(next, "device-a") == DEVICE_STATUS_OK);
    CHECK(credential_service_copy_gateway_token(next, token, sizeof(token), &length, "device-a") == DEVICE_STATUS_NOT_FOUND);
    CHECK(credential_service_copy_gateway_token(next, token, sizeof(token), &length, "device-b") == DEVICE_STATUS_BUSY);
    CHECK(credential_service_store_gateway_token(generation, "stale") == DEVICE_STATUS_BUSY);
    /* Restore publishes token+identity together and rejects malformed input
     * without disturbing the previously committed generation. */
    CHECK(credential_service_restore_gateway_token(next, "bravo", "device-b") == DEVICE_STATUS_OK);
    memset(token, 0, sizeof(token)); length = 0;
    CHECK(credential_service_copy_gateway_token(next, token, sizeof(token), &length, "device-b") == DEVICE_STATUS_OK);
    CHECK(length == 5 && strcmp(token, "bravo") == 0);
    CHECK(credential_service_restore_gateway_token(next, "", "device-c") == DEVICE_STATUS_INVALID_ARGUMENT);
    memset(token, 0, sizeof(token));
    CHECK(credential_service_copy_gateway_token(next, token, sizeof(token), NULL, "device-b") == DEVICE_STATUS_OK);
    CHECK(strcmp(token, "bravo") == 0);
    /* Fixed-size malformed input must be rejected without an unbounded read;
     * failed copy-out must also scrub a reused caller buffer. */
    char unterminated_token[CREDENTIAL_SERVICE_MAX_TOKEN + 1u];
    memset(unterminated_token, 'x', sizeof(unterminated_token));
    CHECK(credential_service_store_gateway_token(next, unterminated_token) ==
          DEVICE_STATUS_INVALID_ARGUMENT);
    char unterminated_identity[CREDENTIAL_SERVICE_IDENTITY_CAPACITY];
    memset(unterminated_identity, 'i', sizeof(unterminated_identity));
    CHECK(credential_service_bind_identity(next, unterminated_identity) ==
          DEVICE_STATUS_INVALID_ARGUMENT);
    memset(token, 's', sizeof(token));
    length = 9u;
    CHECK(credential_service_copy_gateway_token(generation, token, sizeof(token),
                                                &length, "device-b") == DEVICE_STATUS_BUSY);
    CHECK(length == 0u);
    for (size_t i = 0; i < sizeof(token); ++i) CHECK(token[i] == '\0');
    /* A durable-floor write failure is terminal for this in-memory service
     * generation: the new tombstone remains advanced, but no credential read
     * or replacement may proceed on unverifiable persistence. */
    s_floor_write_fail = true;
    CHECK(credential_service_begin_generation(&next) == DEVICE_STATUS_IO_ERROR);
    CHECK(credential_service_copy_gateway_token(next, token, sizeof(token),
                                                NULL, "device-b") ==
          DEVICE_STATUS_UNAVAILABLE);
    CHECK(!credential_service_snapshot(&(credential_service_snapshot_t){
        .struct_size = sizeof(credential_service_snapshot_t),
        .abi_version = CREDENTIAL_SERVICE_ABI_VERSION,
    }));
    puts("PASS credential generation and revocation fence");
    return 0;
}
