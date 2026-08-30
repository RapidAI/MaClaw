#include <stdio.h>
#include <string.h>
#include "services/gateway_tool_result_outbox_policy.h"
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"failed: %s\n",#x); return 1; } } while (0)
int main(void) {
    char valid[] = "{\"clientId\":\"c1\",\"resultId\":\"r1\",\"toolCallId\":\"r1\",\"toolName\":\"alarm_status\",\"conversationId\":\"default\",\"status\":\"succeeded\",\"result\":{}}";
    CHECK(gateway_tool_result_outbox_validate_record(valid, sizeof(valid), GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY) == DEVICE_STATUS_OK);
    CHECK(gateway_tool_result_outbox_validate_record(valid, sizeof(valid)-1u, GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(gateway_tool_result_outbox_validate_record(valid, sizeof(valid), 4u) == DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(gateway_tool_result_outbox_validate_record("", 1u, GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    char non_json[] = "result\0";
    CHECK(gateway_tool_result_outbox_validate_record(non_json, sizeof(non_json),
                                                     GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    char embedded[] = {'{', '}', '\0', 'x', '\0'};
    CHECK(gateway_tool_result_outbox_validate_record(embedded, sizeof(embedded),
                                                     sizeof(embedded)) == DEVICE_STATUS_INVALID_ARGUMENT);
    char queue[GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY]; char next[GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY];
    size_t q = 0, n = 0; char a[] = "{\"clientId\":\"c1\",\"resultId\":\"a\",\"toolCallId\":\"a\",\"toolName\":\"alarm_status\",\"conversationId\":\"default\",\"status\":\"succeeded\",\"result\":{}}"; char b[] = "{\"clientId\":\"c1\",\"resultId\":\"b\",\"toolCallId\":\"b\",\"toolName\":\"alarm_status\",\"conversationId\":\"default\",\"status\":\"failed\",\"error\":{}}";
    CHECK(gateway_tool_result_outbox_append(NULL, 0, a, sizeof(a), queue, sizeof(queue), &q) == DEVICE_STATUS_OK);
    CHECK(gateway_tool_result_outbox_append(NULL, 1, a, sizeof(a), queue, sizeof(queue), &q) == DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(gateway_tool_result_outbox_append(queue, q, b, sizeof(b), next, sizeof(next), &n) == DEVICE_STATUS_OK);
    char out[256]; size_t outn = 0;
    CHECK(gateway_tool_result_outbox_peek(next, n, out, sizeof(out), &outn) == DEVICE_STATUS_OK);
    CHECK(strcmp(out, a) == 0);
    size_t popped = 0; CHECK(gateway_tool_result_outbox_pop(next, n, queue, sizeof(queue), &popped) == DEVICE_STATUS_OK);
    CHECK(popped > 0); CHECK(gateway_tool_result_outbox_peek(queue, popped, out, sizeof(out), &outn) == DEVICE_STATUS_OK);
    CHECK(strcmp(out, b) == 0);
    /* Appending in place must preserve the existing queue prefix. */
    size_t in_place = 0;
    CHECK(gateway_tool_result_outbox_append(queue, popped, a, sizeof(a), queue,
                                            sizeof(queue), &in_place) == DEVICE_STATUS_OK);
    CHECK(in_place == popped + GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES + sizeof(a));
    CHECK(gateway_tool_result_outbox_peek(queue, in_place, out, sizeof(out), &outn) == DEVICE_STATUS_OK);
    CHECK(strcmp(out, b) == 0);
    /* Pop into the same queue buffer must also be overlap-safe. */
    CHECK(gateway_tool_result_outbox_pop(queue, in_place, queue, sizeof(queue), &popped) == DEVICE_STATUS_OK);
    CHECK(popped == GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES +
          GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES + sizeof(a));
    CHECK(gateway_tool_result_outbox_peek(queue, popped, out, sizeof(out), &outn) == DEVICE_STATUS_OK);
    CHECK(strcmp(out, a) == 0);
    /* A partially overlapping queue/output pair must be rejected: copying the
     * prefix could otherwise destroy records before sequence discovery. */
    char overlap[GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY + 8u];
    memcpy(overlap, queue, popped);
    CHECK(gateway_tool_result_outbox_append(overlap, popped, a, sizeof(a),
                                            overlap + 1, sizeof(overlap) - 1u,
                                            &in_place) == DEVICE_STATUS_INVALID_ARGUMENT);
    unsigned char malformed[8] = {0xff, 0xff, 0xff, 0x7f, '{', '}', '\0', '\0'};
    CHECK(gateway_tool_result_outbox_peek((const char *)malformed, sizeof(malformed),
                                          out, sizeof(out), &outn) == DEVICE_STATUS_INVALID_ARGUMENT);
    /* Old unversioned length-prefix blobs must never be interpreted as the
     * current queue format during recovery. */
    unsigned char old_queue[8] = {4u, 0u, 0u, 0u, '{', '}', '\0', '\0'};
    CHECK(gateway_tool_result_outbox_peek((const char *)old_queue, sizeof(old_queue),
                                          out, sizeof(out), &outn) == DEVICE_STATUS_INVALID_ARGUMENT);
    char upgraded[GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY]; size_t upgraded_size = 0;
    unsigned char legacy_record[sizeof(uint32_t) + sizeof(a)];
    memcpy(legacy_record, &(uint32_t){(uint32_t)sizeof(a)}, sizeof(uint32_t));
    memcpy(legacy_record + sizeof(uint32_t), a, sizeof(a));
    CHECK(gateway_tool_result_outbox_upgrade_legacy(
              (const char *)legacy_record, sizeof(legacy_record), upgraded,
              sizeof(upgraded), &upgraded_size) == DEVICE_STATUS_OK);
    CHECK(gateway_tool_result_outbox_peek(upgraded, upgraded_size, out,
                                          sizeof(out), &outn) == DEVICE_STATUS_OK);
    CHECK(strcmp(out, a) == 0);
    CHECK(gateway_tool_result_outbox_upgrade_legacy(
              (const char *)old_queue, sizeof(old_queue), upgraded,
              sizeof(upgraded), &upgraded_size) == DEVICE_STATUS_INVALID_ARGUMENT);
    unsigned char malformed_current[8] = {0};
    memcpy(malformed_current, "BOTO", 4);
    CHECK(gateway_tool_result_outbox_upgrade_legacy(
              (const char *)malformed_current, sizeof(malformed_current), upgraded,
              sizeof(upgraded), &upgraded_size) == DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(gateway_tool_result_outbox_upgrade_legacy(
              (const char *)legacy_record, sizeof(legacy_record), (char *)legacy_record,
              sizeof(legacy_record), &upgraded_size) == DEVICE_STATUS_INVALID_ARGUMENT);
    /* A partially overlapping conversion would destroy the only legacy copy
     * before all records had been validated; reject it just like exact alias. */
    CHECK(gateway_tool_result_outbox_upgrade_legacy(
              (const char *)legacy_record, sizeof(legacy_record),
              (char *)legacy_record + 1, sizeof(legacy_record) - 1u,
              &upgraded_size) == DEVICE_STATUS_INVALID_ARGUMENT);
    unsigned char bad_sequence[GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES +
                               GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES + 4u] = {0};
    memcpy(bad_sequence, "BOTO", 4);
    bad_sequence[4] = GATEWAY_TOOL_RESULT_OUTBOX_FORMAT_VERSION;
    bad_sequence[8] = 4u;
    bad_sequence[12] = '{'; bad_sequence[13] = '}'; bad_sequence[14] = '\0';
    CHECK(gateway_tool_result_outbox_peek((const char *)bad_sequence, sizeof(bad_sequence),
                                          out, sizeof(out), &outn) == DEVICE_STATUS_INVALID_ARGUMENT);
    char tiny[8];
    CHECK(gateway_tool_result_outbox_append(NULL, 0, a, sizeof(a), tiny,
                                            sizeof(tiny), &q) == DEVICE_STATUS_RESOURCE_EXHAUSTED);
    char missing[] = "{\"resultId\":\"r1\",\"status\":\"failed\"}";
    CHECK(gateway_tool_result_outbox_validate_record(missing, sizeof(missing),
                                                     GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    char bad_status[] = "{\"clientId\":\"c1\",\"resultId\":\"r1\",\"toolCallId\":\"r1\",\"toolName\":\"alarm_status\",\"conversationId\":\"default\",\"status\":\"pending\"}";
    CHECK(gateway_tool_result_outbox_validate_record(bad_status, sizeof(bad_status),
                                                     GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY) == DEVICE_STATUS_INVALID_ARGUMENT);
    char nested[] = "{\"clientId\":\"c1\",\"resultId\":\"r1\",\"toolCallId\":\"r1\",\"toolName\":\"alarm_status\",\"conversationId\":\"default\",\"status\":\"succeeded\",\"result\":{\"ok\":true,\"count\":2}}";
    CHECK(gateway_tool_result_outbox_validate_record(nested, sizeof(nested),
                                                     GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY) == DEVICE_STATUS_OK);
    char nested_error[] = "{\"clientId\":\"c1\",\"resultId\":\"r1\",\"toolCallId\":\"r1\",\"toolName\":\"alarm_status\",\"conversationId\":\"default\",\"status\":\"failed\",\"error\":{\"code\":\"device_error\",\"retryable\":false}}";
    CHECK(gateway_tool_result_outbox_validate_record(nested_error, sizeof(nested_error),
                                                     GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY) == DEVICE_STATUS_OK);
    return 0;
}
