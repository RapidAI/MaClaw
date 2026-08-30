#include <stdio.h>
#include <stdint.h>
#include <string.h>

#include "services/gateway_ack_outbox_policy.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

int main(void) {
    char valid[] = "{\"clientId\":\"c1\",\"messageIds\":[\"m1\"],\"status\":\"delivered\"}";
    CHECK(gateway_ack_outbox_validate_record(valid, sizeof(valid),
                                              GATEWAY_ACK_OUTBOX_CAPACITY) == DEVICE_STATUS_OK);
    char non_json[] = "ack\0";
    CHECK(gateway_ack_outbox_validate_record(non_json, sizeof(non_json),
                                              GATEWAY_ACK_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(gateway_ack_outbox_validate_record(valid, sizeof(valid) - 1u,
                                              GATEWAY_ACK_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(gateway_ack_outbox_validate_record(valid, sizeof(valid) + 1u,
                                              GATEWAY_ACK_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(gateway_ack_outbox_validate_record(valid, sizeof(valid), 4u) == DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(gateway_ack_outbox_validate_record("", 1u,
                                              GATEWAY_ACK_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    char embedded[] = "{}\0junk";
    CHECK(gateway_ack_outbox_validate_record(embedded, sizeof(embedded),
                                              GATEWAY_ACK_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    /* A hostile persistence size must never make validation scan beyond the
     * declared blob; the global capacity is an additional hard ceiling. */
    CHECK(gateway_ack_outbox_validate_record(valid, GATEWAY_ACK_OUTBOX_CAPACITY + 1u,
                                              SIZE_MAX) == DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(gateway_ack_outbox_validate_record(NULL, 0u,
                                              GATEWAY_ACK_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    char missing_client[] = "{\"messageIds\":[\"m1\"],\"status\":\"delivered\"}";
    CHECK(gateway_ack_outbox_validate_record(missing_client, sizeof(missing_client),
                                              GATEWAY_ACK_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    char bad_status[] = "{\"clientId\":\"c1\",\"messageIds\":[\"m1\"],\"status\":\"pending\"}";
    CHECK(gateway_ack_outbox_validate_record(bad_status, sizeof(bad_status),
                                              GATEWAY_ACK_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    char empty_ids[] = "{\"clientId\":\"c1\",\"messageIds\":[],\"status\":\"failed\"}";
    CHECK(gateway_ack_outbox_validate_record(empty_ids, sizeof(empty_ids),
                                              GATEWAY_ACK_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    char unknown[] = "{\"clientId\":\"c1\",\"messageIds\":[\"m1\"],\"status\":\"failed\",\"extra\":1}";
    CHECK(gateway_ack_outbox_validate_record(unknown, sizeof(unknown),
                                              GATEWAY_ACK_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    return 0;
}
