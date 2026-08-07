#pragma once

#include <stdbool.h>
#include <stddef.h>

#include "cJSON.h"
#include "esp_err.h"

/* A small domain-tool registry for the device Gateway boundary.  It owns no
 * board drivers and contains no product policy: each domain supplies its own
 * descriptor and handler. */

typedef esp_err_t (*device_tool_execute_fn)(const char *name, cJSON *arguments,
                                            const char *idempotency_key,
                                            cJSON **out_result, char *error,
                                            size_t error_size);

typedef bool (*device_tool_ready_fn)(void);

typedef struct {
    const char *name;
    bool mutation;
    const char *descriptor_json;
    device_tool_execute_fn execute;
    device_tool_ready_fn ready;
} device_tool_definition_t;

bool device_tool_registry_find(const char *name, const device_tool_definition_t **out_definition);
bool device_tool_registry_requires_idempotency(const device_tool_definition_t *definition);
bool device_tool_registry_is_ready(const device_tool_definition_t *definition);
esp_err_t device_tool_registry_execute(const device_tool_definition_t *definition,
                                       cJSON *arguments, const char *idempotency_key,
                                       cJSON **out_result, char *error, size_t error_size);
bool device_tool_registry_append_descriptors(cJSON *tools);
