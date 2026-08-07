#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "cJSON.h"
#include "esp_err.h"

// Metadata-only device update service.  It deliberately has no firmware URL,
// downloader, flash writer or restart API: a fixed 16 MiB device tells the
// user to use ClawMate Maker on a computer instead.

#define UPDATE_SERVICE_VERSION_CAPACITY 64
#define UPDATE_SERVICE_TAG_CAPACITY 80
#define UPDATE_SERVICE_DIGEST_CAPACITY 72
#define UPDATE_SERVICE_DETAIL_CAPACITY 144

typedef struct {
    int64_t running_release_sequence;
} update_service_config_t;

typedef struct {
    bool available;
    bool critical;
    int64_t release_sequence;
    char display_version[UPDATE_SERVICE_VERSION_CAPACITY];
    char release_tag[UPDATE_SERVICE_TAG_CAPACITY];
    char manifest_sha256[UPDATE_SERVICE_DIGEST_CAPACITY];
    int64_t reminder_interval_seconds;
    int64_t remind_after_epoch;
    bool pending_presentation;
    char title[32];
    char detail[UPDATE_SERVICE_DETAIL_CAPACITY];
} update_service_status_t;

esp_err_t update_service_init(const update_service_config_t *config);

// Consumes only validated Hub metadata. It returns true only when a user
// visible reminder should be presented. `now_epoch` is 0 when wall-clock time
// is not trustworthy; the service then remains conservative and never treats a
// deferred/dismiss deadline as elapsed.
bool update_service_apply_metadata(cJSON *metadata, int64_t now_epoch,
                                   bool defer_presentation);

bool update_service_take_pending_presentation(char *title, size_t title_size,
                                              char *detail, size_t detail_size);
esp_err_t update_service_execute_tool(const char *name, cJSON *arguments,
                                      cJSON **out_result, char *error,
                                      size_t error_size);
void update_service_get_status(update_service_status_t *out);
