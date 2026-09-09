#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cJSON.h"
#include "esp_err.h"
#include "esp_heap_caps.h"

#include "device_api.h"
#include "device_tool_registry.h"
#include "persistence_service.h"
#include "services/gateway_tool_result_outbox_policy.h"
#include "services/gateway_tool_result_service.h"
#include "services/gateway_transport.h"

#define CHECK(x) do { if (!(x)) { fprintf(stderr, "failed: %s\n", #x); return 1; } } while (0)
#define REQUIRE(x) do { if (!(x)) { fprintf(stderr, "failed: %s\n", #x); exit(1); } } while (0)

static int g_seq;

/* ---- injectable allocator ---- */
static int g_malloc_calls;
static int g_malloc_fail_at = -1;
void *heap_caps_malloc(size_t size, uint32_t caps) {
    (void)caps;
    ++g_malloc_calls;
    if (g_malloc_fail_at > 0 && g_malloc_calls == g_malloc_fail_at) return NULL;
    return malloc(size);
}
void heap_caps_free(void *ptr) { free(ptr); }

/* ---- device tool registry stub ---- */
static const char *g_known_tool;
static bool g_requires_idem;
static bool g_ready = true;
static esp_err_t g_execute_err = ESP_OK;
static const char *g_execute_detail;
static bool g_execute_has_result = true;
static device_tool_definition_t g_definition;
static char g_seen_idempotency_key[64];
static bool g_execute_called;

bool device_tool_registry_find(const char *name, const device_tool_definition_t **out_definition) {
    if (g_known_tool && name && strcmp(name, g_known_tool) == 0) {
        g_definition.name = g_known_tool;
        *out_definition = &g_definition;
        return true;
    }
    *out_definition = NULL;
    return false;
}
bool device_tool_registry_requires_idempotency(const device_tool_definition_t *definition) {
    (void)definition;
    return g_requires_idem;
}
bool device_tool_registry_is_ready(const device_tool_definition_t *definition) {
    (void)definition;
    return g_ready;
}
esp_err_t device_tool_registry_execute(const device_tool_definition_t *definition,
                                       cJSON *arguments, const char *idempotency_key,
                                       cJSON **out_result, char *error, size_t error_size) {
    (void)definition;
    (void)arguments;
    g_execute_called = true;
    snprintf(g_seen_idempotency_key, sizeof(g_seen_idempotency_key), "%s",
             idempotency_key ? idempotency_key : "");
    if (g_execute_detail) snprintf(error, error_size, "%s", g_execute_detail);
    if (g_execute_err == ESP_OK && g_execute_has_result) {
        *out_result = cJSON_CreateObject();
    }
    return g_execute_err;
}

/* ---- gateway transport stub ---- */
static int32_t g_post_err = 0;
static int g_post_count;
static int g_post_seq;
static char g_last_post_path[128];
static char *g_last_post_payload;
int32_t gateway_transport_post_json(const char *path, const char *payload,
                                    uint32_t accepted_status_mask) {
    REQUIRE(accepted_status_mask == (GATEWAY_TRANSPORT_ACCEPT_200 |
                                     GATEWAY_TRANSPORT_ACCEPT_202 |
                                     GATEWAY_TRANSPORT_ACCEPT_204));
    snprintf(g_last_post_path, sizeof(g_last_post_path), "%s", path);
    free(g_last_post_payload);
    g_last_post_payload = malloc(strlen(payload) + 1u);
    strcpy(g_last_post_payload, payload);
    ++g_post_count;
    g_post_seq = ++g_seq;
    return g_post_err;
}
const char *gateway_transport_device_id(void) { return "dev-1"; }

/* ---- persistence stub (single blob slot) ---- */
static uint8_t g_blob[GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY];
static size_t g_blob_size;
static bool g_blob_present;
static int g_write_count;
static int g_write_seq;
static int g_erase_count;
static device_status_t g_write_err = DEVICE_STATUS_OK;
device_status_t persistence_service_read_blob(const char *name_space, const char *key,
                                              void *out_value, size_t *inout_size) {
    REQUIRE(strcmp(name_space, "gateway") == 0);
    REQUIRE(strcmp(key, "tool_result_outbox") == 0);
    if (!g_blob_present) return DEVICE_STATUS_NOT_FOUND;
    if (*inout_size < g_blob_size) return DEVICE_STATUS_INVALID_ARGUMENT;
    memcpy(out_value, g_blob, g_blob_size);
    *inout_size = g_blob_size;
    return DEVICE_STATUS_OK;
}
device_status_t persistence_service_write_blob(const char *name_space, const char *key,
                                               const void *value, size_t size) {
    REQUIRE(strcmp(name_space, "gateway") == 0);
    REQUIRE(strcmp(key, "tool_result_outbox") == 0);
    ++g_write_count;
    g_write_seq = ++g_seq;
    if (g_write_err != DEVICE_STATUS_OK) return g_write_err;
    REQUIRE(size <= sizeof(g_blob));
    memcpy(g_blob, value, size);
    g_blob_size = size;
    g_blob_present = true;
    return DEVICE_STATUS_OK;
}
device_status_t persistence_service_erase_key(const char *name_space, const char *key) {
    REQUIRE(strcmp(name_space, "gateway") == 0);
    REQUIRE(strcmp(key, "tool_result_outbox") == 0);
    ++g_erase_count;
    g_blob_present = false;
    g_blob_size = 0;
    return DEVICE_STATUS_OK;
}

/* ---- factory reset stub ---- */
static int g_reboot_calls;
static bool g_reboot_last;
void factory_reset_service_reboot_if_pending(bool result_durable) {
    ++g_reboot_calls;
    g_reboot_last = result_durable;
}

int device_status_to_platform_error(device_status_t status) {
    return status == DEVICE_STATUS_OK ? 0 : 1000 + (int)status;
}

/* ---- helpers ---- */
static void reset_stubs(void) {
    g_malloc_calls = 0;
    g_malloc_fail_at = -1;
    g_known_tool = NULL;
    g_requires_idem = false;
    g_ready = true;
    g_execute_err = ESP_OK;
    g_execute_detail = NULL;
    g_execute_has_result = true;
    g_seen_idempotency_key[0] = '\0';
    g_execute_called = false;
    g_post_err = 0;
    g_post_count = 0;
    g_post_seq = 0;
    g_last_post_path[0] = '\0';
    free(g_last_post_payload);
    g_last_post_payload = NULL;
    g_blob_present = false;
    g_blob_size = 0;
    g_write_count = 0;
    g_write_seq = 0;
    g_erase_count = 0;
    g_write_err = DEVICE_STATUS_OK;
    g_seq = 0;
    g_reboot_calls = 0;
    g_reboot_last = false;
    gateway_tool_result_service_init();
}

static cJSON *make_tool_call(const char *id, const char *name, const char *idem,
                             bool with_arguments_object) {
    cJSON *item = cJSON_CreateObject();
    cJSON *call = cJSON_AddObjectToObject(item, "toolCall");
    cJSON_AddStringToObject(call, "id", id);
    cJSON_AddStringToObject(call, "name", name);
    if (idem) cJSON_AddStringToObject(call, "idempotencyKey", idem);
    if (with_arguments_object) {
        cJSON_AddObjectToObject(call, "arguments");
    } else {
        cJSON_AddNumberToObject(call, "arguments", 42);
    }
    return item;
}

/* Parses the last posted payload and returns the requested field. */
static const char *posted_field(const char *path1, const char *path2) {
    static char value[160];
    value[0] = '\0';
    cJSON *body = cJSON_Parse(g_last_post_payload);
    REQUIRE(body != NULL);
    cJSON *node = cJSON_GetObjectItemCaseSensitive(body, path1);
    if (path2) node = cJSON_GetObjectItemCaseSensitive(node, path2);
    if (cJSON_IsString(node)) snprintf(value, sizeof(value), "%s", node->valuestring);
    cJSON_Delete(body);
    return value;
}

static bool posted_bool_field(const char *path1, const char *path2) {
    cJSON *body = cJSON_Parse(g_last_post_payload);
    REQUIRE(body != NULL);
    cJSON *node = cJSON_GetObjectItemCaseSensitive(body, path1);
    if (path2) node = cJSON_GetObjectItemCaseSensitive(node, path2);
    const bool value = cJSON_IsTrue(node);
    cJSON_Delete(body);
    return value;
}

/* Seeds the durable blob with a current-format queue holding `records`. */
static void seed_queue(const char **records, int count) {
    const size_t cap = sizeof(g_blob);
    char *queue = malloc(cap);
    char *next = malloc(cap);
    size_t q = 0;
    for (int i = 0; i < count; ++i) {
        size_t out = 0;
        const size_t len = strlen(records[i]) + 1u;
        REQUIRE(gateway_tool_result_outbox_append(i == 0 ? NULL : queue, i == 0 ? 0u : q,
                                                  records[i], len, next, cap,
                                                  &out) == DEVICE_STATUS_OK);
        memcpy(queue, next, out);
        q = out;
    }
    memcpy(g_blob, queue, q);
    g_blob_size = q;
    g_blob_present = true;
    free(queue);
    free(next);
}

int main(void) {
    /* 1. successful execution posts a succeeded envelope with all fields. */
    reset_stubs();
    g_known_tool = "echo";
    cJSON *item = make_tool_call("c1", "echo", "k1", true);
    cJSON_AddStringToObject(item, "conversationId", "conv1");
    CHECK(gateway_tool_result_service_handle_tool_call(item) == 0);
    cJSON_Delete(item);
    CHECK(g_execute_called);
    CHECK(strcmp(g_seen_idempotency_key, "k1") == 0);
    CHECK(strcmp(g_last_post_path, "/api/im-gateway/v1/tool-result") == 0);
    CHECK(strcmp(posted_field("clientId", NULL), "dev-1") == 0);
    CHECK(strcmp(posted_field("resultId", NULL), "c1") == 0);
    CHECK(strcmp(posted_field("toolCallId", NULL), "c1") == 0);
    CHECK(strcmp(posted_field("toolName", NULL), "echo") == 0);
    CHECK(strcmp(posted_field("conversationId", NULL), "conv1") == 0);
    CHECK(strcmp(posted_field("idempotencyKey", NULL), "k1") == 0);
    CHECK(strcmp(posted_field("status", NULL), "succeeded") == 0);
    CHECK(g_write_count == 0 && g_erase_count == 0);
    CHECK(g_reboot_calls == 1 && !g_reboot_last);

    /* 2. unknown tool -> unknown_tool, not retryable. */
    reset_stubs();
    item = make_tool_call("c2", "missing", NULL, true);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == 0);
    cJSON_Delete(item);
    CHECK(!g_execute_called);
    CHECK(strcmp(posted_field("status", NULL), "failed") == 0);
    CHECK(strcmp(posted_field("error", "code"), "unknown_tool") == 0);
    CHECK(!posted_bool_field("error", "retryable"));

    /* 3. tool not ready -> device_error, retryable, fixed detail. */
    reset_stubs();
    g_known_tool = "echo";
    g_ready = false;
    item = make_tool_call("c3", "echo", NULL, true);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == 0);
    cJSON_Delete(item);
    CHECK(!g_execute_called);
    CHECK(strcmp(posted_field("error", "code"), "device_error") == 0);
    CHECK(posted_bool_field("error", "retryable"));
    CHECK(strcmp(posted_field("error", "message"), "client tool is temporarily unavailable") == 0);

    /* 4. missing idempotencyKey when required -> invalid_arguments. */
    reset_stubs();
    g_known_tool = "echo";
    g_requires_idem = true;
    item = make_tool_call("c4", "echo", NULL, true);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == 0);
    cJSON_Delete(item);
    CHECK(!g_execute_called);
    CHECK(strcmp(posted_field("error", "code"), "invalid_arguments") == 0);
    CHECK(!posted_bool_field("error", "retryable"));

    /* 5. non-object arguments -> invalid_arguments. */
    reset_stubs();
    g_known_tool = "echo";
    item = make_tool_call("c5", "echo", NULL, false);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == 0);
    cJSON_Delete(item);
    CHECK(!g_execute_called);
    CHECK(strcmp(posted_field("error", "code"), "invalid_arguments") == 0);

    /* 6. timeout -> device_busy retryable. */
    reset_stubs();
    g_known_tool = "echo";
    g_execute_err = ESP_ERR_TIMEOUT;
    item = make_tool_call("c6", "echo", NULL, true);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == 0);
    cJSON_Delete(item);
    CHECK(strcmp(posted_field("error", "code"), "device_busy") == 0);
    CHECK(posted_bool_field("error", "retryable"));

    /* 7. plain NO_MEM -> device_busy retryable. */
    reset_stubs();
    g_known_tool = "echo";
    g_execute_err = ESP_ERR_NO_MEM;
    g_execute_detail = "out of slots";
    item = make_tool_call("c7", "echo", NULL, true);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == 0);
    cJSON_Delete(item);
    CHECK(strcmp(posted_field("error", "code"), "device_busy") == 0);
    CHECK(posted_bool_field("error", "retryable"));

    /* 8. persistent capacity NO_MEM -> capacity_exhausted, not retryable. */
    reset_stubs();
    g_known_tool = "echo";
    g_execute_err = ESP_ERR_NO_MEM;
    g_execute_detail = "alarm capacity reached";
    item = make_tool_call("c8", "echo", NULL, true);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == 0);
    cJSON_Delete(item);
    CHECK(strcmp(posted_field("error", "code"), "capacity_exhausted") == 0);
    CHECK(!posted_bool_field("error", "retryable"));

    /* 9. INVALID_ARG from execute -> invalid_arguments, detail as message. */
    reset_stubs();
    g_known_tool = "echo";
    g_execute_err = ESP_ERR_INVALID_ARG;
    g_execute_detail = "bad hour";
    item = make_tool_call("c9", "echo", NULL, true);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == 0);
    cJSON_Delete(item);
    CHECK(strcmp(posted_field("error", "code"), "invalid_arguments") == 0);
    CHECK(strcmp(posted_field("error", "message"), "bad hour") == 0);
    CHECK(!posted_bool_field("error", "retryable"));

    /* 10. other errors -> device_error retryable. */
    reset_stubs();
    g_known_tool = "echo";
    g_execute_err = ESP_ERR_NOT_FOUND;
    item = make_tool_call("c10", "echo", NULL, true);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == 0);
    cJSON_Delete(item);
    CHECK(strcmp(posted_field("error", "code"), "device_error") == 0);
    CHECK(posted_bool_field("error", "retryable"));

    /* 11. POST failure persists the result into the durable outbox. */
    reset_stubs();
    g_known_tool = "echo";
    g_post_err = ESP_ERR_TIMEOUT;
    item = make_tool_call("c11", "echo", NULL, true);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == ESP_ERR_TIMEOUT);
    cJSON_Delete(item);
    CHECK(g_write_count == 1 && g_blob_present);
    {
        char record[512];
        size_t record_size = 0;
        CHECK(gateway_tool_result_outbox_peek((const char *)g_blob, g_blob_size,
                                              record, sizeof(record),
                                              &record_size) == DEVICE_STATUS_OK);
        cJSON *queued = cJSON_Parse(record);
        CHECK(queued != NULL);
        cJSON *rid = cJSON_GetObjectItemCaseSensitive(queued, "resultId");
        CHECK(cJSON_IsString(rid) && strcmp(rid->valuestring, "c11") == 0);
        cJSON_Delete(queued);
    }
    CHECK(g_reboot_calls == 1 && !g_reboot_last);

    /* 12. POST failure + persist failure keeps result non-durable. */
    reset_stubs();
    g_known_tool = "echo";
    g_post_err = ESP_ERR_TIMEOUT;
    g_write_err = DEVICE_STATUS_IO_ERROR;
    item = make_tool_call("c12", "echo", NULL, true);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == ESP_ERR_TIMEOUT);
    cJSON_Delete(item);
    CHECK(g_write_count == 1 && !g_blob_present);
    CHECK(g_reboot_calls == 1 && !g_reboot_last);

    /* 13. factory_reset result durable via outbox -> reboot handoff true. */
    reset_stubs();
    g_known_tool = "factory_reset";
    g_post_err = ESP_ERR_TIMEOUT;
    item = make_tool_call("c13", "factory_reset", NULL, true);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == ESP_ERR_TIMEOUT);
    cJSON_Delete(item);
    CHECK(g_reboot_calls == 1 && g_reboot_last);

    /* 14. factory_reset result posted directly -> reboot handoff true. */
    reset_stubs();
    g_known_tool = "factory_reset";
    item = make_tool_call("c14", "factory_reset", NULL, true);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == 0);
    cJSON_Delete(item);
    CHECK(g_reboot_calls == 1 && g_reboot_last);

    /* 15. factory_reset result undeliverable -> reboot handoff false. */
    reset_stubs();
    g_known_tool = "factory_reset";
    g_post_err = ESP_ERR_TIMEOUT;
    g_write_err = DEVICE_STATUS_IO_ERROR;
    item = make_tool_call("c15", "factory_reset", NULL, true);
    CHECK(gateway_tool_result_service_handle_tool_call(item) == ESP_ERR_TIMEOUT);
    cJSON_Delete(item);
    CHECK(g_reboot_calls == 1 && !g_reboot_last);

    /* 16. flush on an empty outbox is a no-op. */
    reset_stubs();
    CHECK(gateway_tool_result_service_flush_outbox() == 0);
    CHECK(g_post_count == 0 && g_erase_count == 0 && g_write_count == 0);

    /* 17. flush a single record: pop empties the queue -> erase. The replayed
     * id is recorded before POST and consumed by the dedup probe. */
    reset_stubs();
    {
        const char *r1 = "{\"clientId\":\"d\",\"resultId\":\"r1\",\"toolCallId\":\"r1\",\"toolName\":\"echo\",\"conversationId\":\"default\",\"status\":\"succeeded\",\"result\":{}}";
        const char *records[] = {r1};
        seed_queue(records, 1);
    }
    CHECK(gateway_tool_result_service_flush_outbox() == 0);
    CHECK(g_post_count == 1);
    CHECK(g_erase_count == 1 && !g_blob_present);
    CHECK(g_write_count == 0);
    CHECK(g_reboot_calls == 0);
    {
        cJSON *dup = cJSON_CreateObject();
        cJSON_AddStringToObject(dup, "toolCallId", "r1");
        CHECK(gateway_tool_result_service_outbox_already_delivered(dup));
        /* Consumed: a second probe must miss. */
        CHECK(!gateway_tool_result_service_outbox_already_delivered(dup));
        cJSON_Delete(dup);
    }

    /* 18. dedup also matches the plain id field and survives a mismatch. */
    reset_stubs();
    {
        const char *r1 = "{\"clientId\":\"d\",\"resultId\":\"r9\",\"toolCallId\":\"r9\",\"toolName\":\"echo\",\"conversationId\":\"default\",\"status\":\"succeeded\",\"result\":{}}";
        const char *records[] = {r1};
        seed_queue(records, 1);
    }
    CHECK(gateway_tool_result_service_flush_outbox() == 0);
    {
        cJSON *other = cJSON_CreateObject();
        cJSON_AddStringToObject(other, "id", "other");
        CHECK(!gateway_tool_result_service_outbox_already_delivered(other));
        cJSON_Delete(other);
        cJSON *dup = cJSON_CreateObject();
        cJSON_AddStringToObject(dup, "id", "r9");
        CHECK(gateway_tool_result_service_outbox_already_delivered(dup));
        cJSON_Delete(dup);
        CHECK(!gateway_tool_result_service_outbox_already_delivered(NULL));
    }

    /* 19. flush with two records rewrites the remainder durably. */
    reset_stubs();
    {
        const char *r1 = "{\"clientId\":\"d\",\"resultId\":\"a\",\"toolCallId\":\"a\",\"toolName\":\"echo\",\"conversationId\":\"default\",\"status\":\"succeeded\",\"result\":{}}";
        const char *r2 = "{\"clientId\":\"d\",\"resultId\":\"b\",\"toolCallId\":\"b\",\"toolName\":\"echo\",\"conversationId\":\"default\",\"status\":\"succeeded\",\"result\":{}}";
        const char *records[] = {r1, r2};
        seed_queue(records, 2);
    }
    CHECK(gateway_tool_result_service_flush_outbox() == 0);
    CHECK(g_post_count == 1);
    CHECK(g_write_count == 1 && g_blob_present);
    CHECK(g_erase_count == 0);
    {
        char record[512];
        size_t record_size = 0;
        CHECK(gateway_tool_result_outbox_peek((const char *)g_blob, g_blob_size,
                                              record, sizeof(record),
                                              &record_size) == DEVICE_STATUS_OK);
        cJSON *queued = cJSON_Parse(record);
        cJSON *rid = cJSON_GetObjectItemCaseSensitive(queued, "resultId");
        CHECK(cJSON_IsString(rid) && strcmp(rid->valuestring, "b") == 0);
        cJSON_Delete(queued);
    }

    /* 20. failed replay POST clears the delivered id and never pops/erases. */
    reset_stubs();
    {
        const char *r1 = "{\"clientId\":\"d\",\"resultId\":\"r5\",\"toolCallId\":\"r5\",\"toolName\":\"echo\",\"conversationId\":\"default\",\"status\":\"succeeded\",\"result\":{}}";
        const char *records[] = {r1};
        seed_queue(records, 1);
        const size_t seeded_size = g_blob_size;
        g_post_err = ESP_ERR_TIMEOUT;
        CHECK(gateway_tool_result_service_flush_outbox() == ESP_ERR_TIMEOUT);
        CHECK(g_post_count == 1);
        CHECK(g_erase_count == 0 && g_write_count == 0);
        CHECK(g_blob_present && g_blob_size == seeded_size);
        cJSON *dup = cJSON_CreateObject();
        cJSON_AddStringToObject(dup, "toolCallId", "r5");
        CHECK(!gateway_tool_result_service_outbox_already_delivered(dup));
        cJSON_Delete(dup);
    }

    /* 21. legacy queue is upgraded and persisted before any replay POST. */
    reset_stubs();
    {
        const char *r1 = "{\"clientId\":\"d\",\"resultId\":\"legacy1\",\"toolCallId\":\"legacy1\",\"toolName\":\"echo\",\"conversationId\":\"default\",\"status\":\"succeeded\",\"result\":{}}";
        const uint32_t len = (uint32_t)(strlen(r1) + 1u);
        memcpy(g_blob, &len, sizeof(len));
        memcpy(g_blob + sizeof(len), r1, len);
        g_blob_size = sizeof(len) + len;
        g_blob_present = true;
    }
    CHECK(gateway_tool_result_service_flush_outbox() == 0);
    CHECK(g_post_count == 1);
    CHECK(g_write_count == 1);
    CHECK(g_write_seq < g_post_seq);
    CHECK(g_erase_count == 1 && !g_blob_present);

    /* 22. pop allocation failure is fail-closed: no erase, queue intact. */
    reset_stubs();
    {
        const char *r1 = "{\"clientId\":\"d\",\"resultId\":\"r7\",\"toolCallId\":\"r7\",\"toolName\":\"echo\",\"conversationId\":\"default\",\"status\":\"succeeded\",\"result\":{}}";
        const char *records[] = {r1};
        seed_queue(records, 1);
        const size_t seeded_size = g_blob_size;
        /* Allocations: #1 payload, #2 record, #3 remaining (pop output). */
        g_malloc_fail_at = 3;
        CHECK(gateway_tool_result_service_flush_outbox() != 0);
        CHECK(g_post_count == 1);
        CHECK(g_erase_count == 0 && g_write_count == 0);
        CHECK(g_blob_present && g_blob_size == seeded_size);
    }

    /* 23. factory_reset record delivered by flush triggers the pending reboot. */
    reset_stubs();
    {
        const char *r1 = "{\"clientId\":\"d\",\"resultId\":\"fr1\",\"toolCallId\":\"fr1\",\"toolName\":\"factory_reset\",\"conversationId\":\"default\",\"status\":\"succeeded\",\"result\":{}}";
        const char *records[] = {r1};
        seed_queue(records, 1);
    }
    CHECK(gateway_tool_result_service_flush_outbox() == 0);
    CHECK(g_reboot_calls == 1 && g_reboot_last);

    /* 24. malformed envelope is rejected without touching the registry. */
    reset_stubs();
    g_known_tool = "echo";
    item = cJSON_CreateObject();
    cJSON_AddStringToObject(item, "toolCall", "not-an-object");
    CHECK(gateway_tool_result_service_handle_tool_call(item) == ESP_ERR_INVALID_ARG);
    cJSON_Delete(item);
    CHECK(!g_execute_called && g_post_count == 0);

    free(g_last_post_payload);
    printf("gateway tool result service host test passed\n");
    return 0;
}
