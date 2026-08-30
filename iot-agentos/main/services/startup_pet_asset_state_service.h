#pragma once

/*
 * Startup pet asset latest-wins state.
 *
 * Owns only the descriptor and admission facts shared by handshake, deferred
 * download, server-audio preemption and sleep rollback. HTTP, JSON parsing,
 * renderer installation, timers and worker lifetime remain at their existing
 * owners. The public contract contains descriptor values only.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "services/pet_asset_service.h"

#define STARTUP_PET_ASSET_STATE_SKIN_CAPACITY 32u

typedef struct {
    bool pending;
    bool present;
    bool preempted_by_audio;
    uint32_t generation;
    pet_asset_descriptor_t descriptor;
    char skin[STARTUP_PET_ASSET_STATE_SKIN_CAPACITY];
} startup_pet_asset_state_snapshot_t;

device_status_t startup_pet_asset_state_service_init(void);

/* Replaces the retained handshake descriptor. Every replacement opens a new
 * generation and starts a fresh optional transaction, even when `present` is
 * false so an invalid/asset-less descriptor can clear stale artwork later. */
device_status_t startup_pet_asset_state_service_record(
    const pet_asset_descriptor_t *descriptor, bool present, const char *skin);

bool startup_pet_asset_state_service_snapshot(
    startup_pet_asset_state_snapshot_t *out_snapshot);
bool startup_pet_asset_state_service_pending(void);
bool startup_pet_asset_state_service_pending_generation(uint32_t generation);
void startup_pet_asset_state_service_set_pending(bool pending);
/* Completes only the generation captured by the caller. A newer handshake or
 * re-arm therefore cannot be cleared by an older worker finishing late. */
bool startup_pet_asset_state_service_finish_generation(uint32_t generation);

/* Atomically consumes one bounded capacity-retry budget entry for the
 * admitted descriptor generation. A new handshake resets this accounting;
 * an older worker can neither consume budget for nor report a newer profile.
 * Returns false when the generation is no longer pending or its budget has
 * already been exhausted. */
bool startup_pet_asset_state_service_take_capacity_retry(uint32_t generation,
                                                          uint32_t retry_limit,
                                                          uint32_t *out_attempt);
/* Returns a reservation when the physical timer could not be armed. This is
 * generation-fenced so a stale caller cannot alter a replacement profile. */
void startup_pet_asset_state_service_return_capacity_retry(uint32_t generation);

/* Reversible System Sleep fence for the retained descriptor state. PREPARE
 * snapshots the state before the worker/timer participants are parked;
 * ABORT restores that snapshot as one generation-fenced transition. The
 * caller receives whether the restored snapshot carried an audio-preemption
 * marker, then decides when the physical audio lease may re-arm work. */
device_status_t startup_pet_asset_state_service_prepare_system_sleep(void);
bool startup_pet_asset_state_service_abort_system_sleep_prepare(
    bool *out_restored_audio_preemption);
bool startup_pet_asset_state_service_system_sleep_preparing(void);

/* Atomically cancels an admitted optional transaction for foreground audio.
 * Returns true only when an active transaction was preempted. */
bool startup_pet_asset_state_service_preempt_for_audio(bool worker_stopping);
/* Returns and clears the deferred audio-rearm marker. */
bool startup_pet_asset_state_service_take_audio_preemption(void);

/* Sleep PREPARE stores these values at the composition root; ABORT restores
 * them through one atomic state transition. */
void startup_pet_asset_state_service_restore(bool pending,
                                             bool preempted_by_audio);

/* Tests whether a runtime pet-profile message is exactly the retained startup
 * profile. It prevents a duplicate Hub mirror from racing its own handshake
 * transaction, while a different profile still supersedes it. */
bool startup_pet_asset_state_service_matches_profile(const char *revision,
                                                      const char *skin);
