#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"

/*
 * Internal lifecycle registry for every long-lived task, timer or callback
 * owner that must be quiesced before its dependencies are released.  This is
 * deliberately not part of Device API: application code names a domain owner,
 * never a FreeRTOS task handle or a board implementation detail.
 */
typedef enum {
    TASK_REGISTRY_OWNER_DIAGNOSTICS = 1,
    TASK_REGISTRY_OWNER_STORAGE,
    TASK_REGISTRY_OWNER_CONNECTIVITY,
    TASK_REGISTRY_OWNER_INTERACTION,
    TASK_REGISTRY_OWNER_AUDIO,
    TASK_REGISTRY_OWNER_POWER,
    TASK_REGISTRY_OWNER_BOARD,
} task_registry_owner_t;

typedef esp_err_t (*task_registry_stop_fn_t)(void *context, uint32_t timeout_ms);

typedef struct {
    uint32_t struct_size;
    task_registry_owner_t owner;
    /* Static diagnostic label. The registry does not retain caller buffers. */
    const char *name;
    void *context;
    task_registry_stop_fn_t stop;
} task_registry_entry_t;

typedef struct {
    uint32_t registered_count;
    uint32_t stop_failures;
} task_registry_snapshot_t;

esp_err_t task_registry_init(void);
esp_err_t task_registry_register(const task_registry_entry_t *entry);
void task_registry_unregister(task_registry_owner_t owner, void *context);
/* Bounded counterpart for lifecycle owners.  Natural worker exit may retain
 * the legacy unbounded helper, but a parent shutdown must never spend beyond
 * its own deadline merely removing bookkeeping after an already-joined child. */
esp_err_t task_registry_unregister_with_timeout(task_registry_owner_t owner,
                                                void *context,
                                                uint32_t timeout_ms);

/* Stops entries in reverse registration order.  A timeout or error keeps the
 * entry registered, which prevents a later lifecycle phase from treating the
 * owner as safely drained. */
esp_err_t task_registry_stop_owner(task_registry_owner_t owner, uint32_t timeout_ms);
esp_err_t task_registry_stop_all(uint32_t timeout_ms);
bool task_registry_get_snapshot(task_registry_snapshot_t *out_snapshot);
