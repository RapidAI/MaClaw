#pragma once

/*
 * Alarm business service.
 *
 * Owns the alarm domain that used to live in alarm_manager.c: the durable
 * store schema (16 entries, 48-byte UTF-8 labels, ALM1->ALM2 migration,
 * 8-slot replay cache, active-alarm ownership), the repository decisions
 * (mutation and replay result committed as one blob, rollback on persistence
 * failure), the scheduler worker (deadline-armed due evaluation, 60 s ring x
 * up to 3 attempts, 5-minute retry interval, touch/key dismissal) and the
 * feedback presentation (alarm visual, alarm burst, scheduled indicator).
 *
 * alarm_manager.c remains as a thin facade: it keeps the Device Tool
 * Registry cJSON adapter, the tool-admission counter and the System Sleep
 * participant contract required by the HAL boundary gate, and drives this
 * service through the value-typed API below.  No ESP-IDF error codes,
 * FreeRTOS handles, JSON objects or persistence primitives cross this
 * boundary; the scheduler task identity is an opaque integer token.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "device_api.h"

#define ALARM_SERVICE_MAX_COUNT 16u
#define ALARM_SERVICE_LABEL_BYTES 48u
#define ALARM_SERVICE_RESULT_CACHE_KEY_BYTES 64u
#define ALARM_SERVICE_RESULT_CACHE_JSON_BYTES 512u

typedef struct {
    uint32_t id;
    int64_t trigger_at_ms;
    char label[ALARM_SERVICE_LABEL_BYTES + 1u];
} alarm_service_item_t;

typedef void (*alarm_service_ring_callback_t)(void *arg);

/* The facade owns the reversible System Sleep admission flag; the scheduler
 * worker observes it through this installed query (direct volatile read,
 * never blocking). */
typedef bool (*alarm_service_sleep_prepare_query_t)(void);

device_status_t alarm_service_init(void);
/* Two-phase deinit bracketing the facade's tool-admission drain.
 * close_and_join closes scheduling and joins the worker while holding the
 * lifecycle mutex; finish completes the deadline hand-off and releases it.
 * abort releases the held lifecycle mutex when the facade drain times out. */
device_status_t alarm_service_deinit_close_and_join(uint32_t timeout_ms);
device_status_t alarm_service_deinit_finish(uint32_t timeout_ms);
void alarm_service_deinit_abort(void);

/* Lifecycle serialization shared by the facade's System Sleep participant. */
device_status_t alarm_service_lifecycle_acquire(uint32_t timeout_ms);
void alarm_service_lifecycle_release(void);
bool alarm_service_is_ready(void);
bool alarm_service_is_ringing(void);
bool alarm_service_stop_requested(void);
/* Opaque scheduler task identity for the facade's cooperative notification;
 * 0 when the scheduler is not running. */
uintptr_t alarm_service_scheduler_task_token(void);
void alarm_service_set_ring_callback(alarm_service_ring_callback_t callback,
                                     void *arg);
void alarm_service_set_sleep_prepare_query(alarm_service_sleep_prepare_query_t query);
void alarm_service_dismiss(void);

/* Tool transaction scope (facade's cJSON adapter holds the store lock across
 * parse -> mutate -> replay record -> persist so mutation and replay result
 * remain one durable commit). */
device_status_t alarm_service_tool_store_lock(uint32_t timeout_ms);
void alarm_service_tool_store_unlock(void);
bool alarm_service_replay_find(const char *key, int32_t *out_status,
                               char *out_detail, size_t detail_capacity,
                               char *out_result_json, size_t result_json_capacity);
/* Opaque rollback snapshot of the durable store; NULL on allocation
 * failure.  Free with alarm_service_store_snapshot_free(). */
void *alarm_service_store_snapshot(void);
void alarm_service_store_snapshot_free(void *snapshot);
void alarm_service_store_rollback(void *snapshot);

/* Domain mutations.  Call with the tool store lock held; error strings are
 * produced exactly as the legacy adapter emitted them. */
device_status_t alarm_service_create(int64_t trigger_ms, const char *label,
                                     alarm_service_item_t *out_created,
                                     uint32_t *out_display_index,
                                     uint32_t *out_count,
                                     char *error, size_t error_size);
device_status_t alarm_service_clear_all(uint32_t *out_cleared,
                                        bool *out_dismiss_active);
device_status_t alarm_service_clear(int32_t index,
                                    alarm_service_item_t *out_cleared,
                                    uint32_t *out_display_index,
                                    uint32_t *out_count,
                                    bool *out_dismiss_active,
                                    char *error, size_t error_size);
uint32_t alarm_service_list_count(void);
bool alarm_service_list_entry(uint32_t visible_index,
                              alarm_service_item_t *out_item,
                              bool *out_active);
/* Writes the replay-cache record for a mutation; returns
 * DEVICE_STATUS_RESOURCE_EXHAUSTED when the result JSON does not fit the
 * durable record (the facade then rolls the store back). */
device_status_t alarm_service_replay_record(const char *key, int32_t status,
                                            const char *detail,
                                            const char *result_json);
/* Returns the platform persistence result code (0 on success). */
int32_t alarm_service_persist(void);
void alarm_service_rearm_deadline(void);
void alarm_service_publish_scheduled(void);
void alarm_service_request_dismiss(void);

/* System Sleep PREPARE probe: bounded store-lock read of durable active
 * alarm ownership. */
device_status_t alarm_service_active_alarm_present(uint32_t timeout_ms,
                                                   bool *out_present);
/* Reads the earliest queued (not currently active) alarm under the durable
 * store lock.  A false `out_present` means the queue is empty. */
device_status_t alarm_service_earliest_queued_alarm(uint32_t timeout_ms,
                                                    bool *out_present,
                                                    int64_t *out_epoch_ms);
