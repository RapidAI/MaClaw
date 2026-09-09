#pragma once

/*
 * Media transfer arbitration for server speech and optional pet artwork.
 *
 * This service owns the single large-transfer lane, its foreground-priority
 * observation, and the offline-wake memory lease count.  The composition root
 * retains the physical transfer client, Audio arbitration and startup-pet state;
 * it supplies only value actions below.  No SDK, RTOS, transport, allocator or
 * board object crosses this contract.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef struct {
    uint32_t struct_size;
    void (*stop_wake_word_for_media)(const char *source, void *context);
    void (*cancel_startup_pet_for_server_audio)(void *context);
    bool (*take_startup_pet_audio_preemption)(void *context);
    void (*rearm_preempted_startup_pet)(void *context);
    void (*schedule_wake_restart)(void *context);
    void *context;
} media_transfer_service_host_t;

device_status_t media_transfer_service_init(
    const media_transfer_service_host_t *host);

/* Reversible System Sleep participant. PREPARE closes new media admission
 * and waits for a pre-existing lane/wake-lease owner to leave. A timeout
 * leaves admission closed until the Power-owned ABORT counterpart runs. */
device_status_t media_transfer_service_prepare_system_sleep(uint32_t timeout_ms);
void media_transfer_service_abort_system_sleep_prepare(void);

/* Server audio is a message-scoped, singleton lease. `true` means this call
 * acquired it; a duplicate call leaves the existing lease intact and returns
 * false.  Both cases retain foreground priority over optional artwork. */
bool media_transfer_service_begin_server_audio_wake_lease(const char *source);
bool media_transfer_service_finish_server_audio_wake_lease(void);

/* Optional callers may nest leases. The final release schedules the normal
 * asynchronous wake restart, never starts recognizer work inline. */
/* Returns true only when this call acquired an optional lease. Callers must
 * pass the result to finish; a rejected begin must never release another
 * transaction's lease. */
bool media_transfer_service_begin_optional_wake_lease(const char *source);
bool media_transfer_service_finish_optional_wake_lease(void);

bool media_transfer_service_server_audio_wake_lease_active(void);
void media_transfer_service_set_audio_download_active(bool active);
bool media_transfer_service_audio_download_active(void);

/* The caller owns the physical request. Holding this lane proves that an
 * optional pet transfer cannot overlap a server-audio TLS body. */
device_status_t media_transfer_service_take_lane(uint32_t timeout_ms);
void media_transfer_service_release_lane(void);
