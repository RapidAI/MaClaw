#pragma once

/* Value-only validation for the single-slot Gateway ACK durable outbox. */

#include <stddef.h>

#include "device_api.h"

#define GATEWAY_ACK_OUTBOX_CAPACITY 2048u

/* The persisted value is a NUL-terminated JSON envelope.  Validation is
 * deliberately independent of NVS, allocators, JSON parsers and RTOS state
 * so recovery can fail closed before any transport side effect. */
device_status_t gateway_ack_outbox_validate_record(const char *payload,
                                                   size_t stored_size,
                                                   size_t capacity);

