#pragma once

#include <stddef.h>

#include "device_api.h"

#define GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY (64u * 1024u)
#define GATEWAY_TOOL_RESULT_OUTBOX_FORMAT_MAGIC 0x47544f42u /* GTOB */
#define GATEWAY_TOOL_RESULT_OUTBOX_FORMAT_VERSION 1u
#define GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES 8u
#define GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES 8u

device_status_t gateway_tool_result_outbox_validate_record(const char *payload,
                                                           size_t stored_size,
                                                           size_t capacity);

/* One-time, explicit upgrade for blobs written by the pre-versioned queue.
 * The caller must persist the returned blob before replaying it.  Unknown or
 * malformed legacy data is rejected; no best-effort reinterpretation occurs. */
device_status_t gateway_tool_result_outbox_upgrade_legacy(const char *legacy_queue,
                                                          size_t legacy_size,
                                                          char *out_queue,
                                                          size_t out_capacity,
                                                          size_t *out_size);

device_status_t gateway_tool_result_outbox_validate_queue(const char *queue,
                                                          size_t queue_size,
                                                          size_t capacity);

/* The queue blob is an 8-byte magic/version header followed by records of
 * little-endian uint32 length + uint32 sequence + NUL-terminated JSON. These
 * helpers operate on caller-owned buffers and never retain pointers. */
device_status_t gateway_tool_result_outbox_append(const char *queue,
                                                 size_t queue_size,
                                                 const char *record,
                                                 size_t record_size,
                                                 char *out_queue,
                                                 size_t out_capacity,
                                                 size_t *out_size);
device_status_t gateway_tool_result_outbox_peek(const char *queue,
                                                size_t queue_size,
                                                char *out_record,
                                                size_t out_capacity,
                                                size_t *out_record_size);
device_status_t gateway_tool_result_outbox_pop(const char *queue,
                                               size_t queue_size,
                                               char *out_queue,
                                               size_t out_capacity,
                                               size_t *out_size);
