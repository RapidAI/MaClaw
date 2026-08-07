#pragma once

/*
 * Bounded operation ownership for long-running domain work.
 *
 * This first service intentionally has one active foreground slot.  It gives
 * the existing voice-command flow an explicit, versioned owner without making
 * board adapters, FreeRTOS tasks or raw handles part of the domain contract.
 * Future independent domains can either serialize through this slot or add a
 * named domain slot without changing the context/envelope semantics.
 */

#include "device_api.h"

#define DEVICE_OPERATION_CONTEXT_ABI_VERSION 1u

typedef enum {
    DEVICE_OPERATION_KIND_NONE = 0,
    DEVICE_OPERATION_KIND_VOICE_INTERACTION,
    DEVICE_OPERATION_KIND_MEETING_RECORDING,
    DEVICE_OPERATION_KIND_MEETING_RESUME,
} device_operation_kind_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    /* Local, boot-lifetime correlation identifier.  It is diagnostic only;
     * protocol IDs remain owned by Gateway/Identity once those services land. */
    uint64_t operation_id;
    uint32_t generation;
    device_operation_kind_t kind;
    /* Zero means no fixed deadline.  Long agent work currently ends by a
     * terminal Hub reply or explicit cancel, not by an invented timeout. */
    uint64_t deadline_us;
    bool cancel_requested;
    bool terminal_committed;
} device_operation_context_t;

void operation_context_service_init(void);
device_status_t operation_context_begin(device_operation_kind_t kind,
                                        uint64_t deadline_us,
                                        device_operation_context_t *out_context);
bool operation_context_matches(uint32_t generation);
/* True while the generation is still the active slot, including the short
 * cleanup period after another worker committed its terminal presentation. */
bool operation_context_is_current(uint32_t generation);
bool operation_context_request_cancel(uint32_t generation);
bool operation_context_cancel_requested(uint32_t generation);
/* Exactly one terminal transition may win for an active generation. */
bool operation_context_commit_terminal(uint32_t generation);
bool operation_context_get_active(device_operation_context_t *out_context);
