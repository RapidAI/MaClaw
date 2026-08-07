#include "operation_context.h"

#include <string.h>

#include "freertos/FreeRTOS.h"

typedef struct {
    device_operation_context_t active;
    uint64_t next_operation_id;
    uint32_t next_generation;
    bool initialized;
} operation_context_state_t;

static operation_context_state_t s_state;
static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;

static uint32_t next_nonzero_u32(uint32_t *value) {
    ++*value;
    if (*value == 0) ++*value;
    return *value;
}

static uint64_t next_nonzero_u64(uint64_t *value) {
    ++*value;
    if (*value == 0) ++*value;
    return *value;
}

void operation_context_service_init(void) {
    taskENTER_CRITICAL(&s_lock);
    memset(&s_state, 0, sizeof(s_state));
    s_state.initialized = true;
    taskEXIT_CRITICAL(&s_lock);
}

device_status_t operation_context_begin(device_operation_kind_t kind,
                                        uint64_t deadline_us,
                                        device_operation_context_t *out_context) {
    if (!out_context || kind == DEVICE_OPERATION_KIND_NONE) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    taskENTER_CRITICAL(&s_lock);
    if (!s_state.initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    if (s_state.active.generation != 0 && !s_state.active.terminal_committed) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_state.active = (device_operation_context_t){
        .struct_size = sizeof(device_operation_context_t),
        .abi_version = DEVICE_OPERATION_CONTEXT_ABI_VERSION,
        .operation_id = next_nonzero_u64(&s_state.next_operation_id),
        .generation = next_nonzero_u32(&s_state.next_generation),
        .kind = kind,
        .deadline_us = deadline_us,
    };
    *out_context = s_state.active;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

bool operation_context_matches(uint32_t generation) {
    bool matches;
    taskENTER_CRITICAL(&s_lock);
    matches = s_state.initialized && generation != 0 &&
              s_state.active.generation == generation &&
              !s_state.active.terminal_committed;
    taskEXIT_CRITICAL(&s_lock);
    return matches;
}

bool operation_context_is_current(uint32_t generation) {
    bool current;
    taskENTER_CRITICAL(&s_lock);
    current = s_state.initialized && generation != 0 &&
              s_state.active.generation == generation;
    taskEXIT_CRITICAL(&s_lock);
    return current;
}

bool operation_context_request_cancel(uint32_t generation) {
    bool accepted;
    taskENTER_CRITICAL(&s_lock);
    accepted = s_state.initialized && generation != 0 &&
               s_state.active.generation == generation &&
               !s_state.active.terminal_committed;
    if (accepted) s_state.active.cancel_requested = true;
    taskEXIT_CRITICAL(&s_lock);
    return accepted;
}

bool operation_context_cancel_requested(uint32_t generation) {
    bool requested;
    taskENTER_CRITICAL(&s_lock);
    requested = s_state.initialized && generation != 0 &&
                s_state.active.generation == generation &&
                !s_state.active.terminal_committed &&
                s_state.active.cancel_requested;
    taskEXIT_CRITICAL(&s_lock);
    return requested;
}

bool operation_context_commit_terminal(uint32_t generation) {
    bool committed;
    taskENTER_CRITICAL(&s_lock);
    committed = s_state.initialized && generation != 0 &&
                s_state.active.generation == generation &&
                !s_state.active.terminal_committed;
    if (committed) s_state.active.terminal_committed = true;
    taskEXIT_CRITICAL(&s_lock);
    return committed;
}

bool operation_context_get_active(device_operation_context_t *out_context) {
    if (!out_context) return false;
    taskENTER_CRITICAL(&s_lock);
    if (!s_state.initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return false;
    }
    *out_context = s_state.active;
    taskEXIT_CRITICAL(&s_lock);
    return true;
}
