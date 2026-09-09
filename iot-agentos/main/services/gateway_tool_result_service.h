#pragma once

/*
 * Gateway tool-result orchestration (A8/A12 increment).
 *
 * Owns the Hub toolCall dispatch and tool-result delivery orchestration that
 * used to live in main.c: toolCall envelope parsing and Device Tool Registry
 * execution, the result/error envelope mapping (code + retryable), the direct
 * transport POST, the durable outbox fallback on POST failure, the one-shot
 * delivered-id dedup record that lets the Dispatcher skip an already
 * replayed toolCall, the outbox replay flush (legacy upgrade, peek/post/pop,
 * durable rewrite or erase) and the factory-reset delivery gate.
 *
 * The public contract exposes value types only: no ESP-IDF error codes,
 * FreeRTOS handles, HTTP client handles or JSON types.  Inbound message
 * envelopes cross as opaque nodes owned by the Dispatcher; the factory-reset
 * authorization stays a boolean value.  Physical dependencies (Device Tool
 * Registry, Gateway Transport, Persistence, the PSRAM allocator and Factory
 * Reset) are device-layer services this module calls directly, matching the
 * meeting/alarm service convention.
 */

#include <stdbool.h>
#include <stdint.h>

/* Clears the service-owned delivered-id dedup record.  Called once at
 * startup beside the other service initializations. */
void gateway_tool_result_service_init(void);

/* Executes one Hub toolCall envelope and delivers its result, persisting to
 * the durable outbox when the direct POST fails.  `message_item` is an
 * opaque JSON node owned by the caller.  Returns platform error codes
 * (0 on success, including a result that is durably queued but not yet
 * posted is still the POST failure code). */
int32_t gateway_tool_result_service_handle_tool_call(const void *message_item);

/* One-shot dedup against the id recorded by the most recent outbox replay
 * POST.  A hit consumes (clears) the record and returns true so the
 * Dispatcher skips re-executing an already delivered toolCall. */
bool gateway_tool_result_service_outbox_already_delivered(const void *message_item);

/* Replays the durable outbox head: upgrades a legacy queue (persisted before
 * any POST), records the delivered id before replay, pops on success and
 * durably rewrites the remainder or erases an empty queue.  A failed POST
 * clears the delivered-id record; a failed pop is fail-closed and never
 * erases.  Returns platform error codes (0 on success). */
int32_t gateway_tool_result_service_flush_outbox(void);
