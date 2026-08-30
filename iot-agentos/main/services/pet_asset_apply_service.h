#pragma once

/*
 * Pet asset display-application coordinator.
 *
 * Owns the serialized transition from verified source frames to the semantic
 * Display service, including the currently installed revision.  Transport,
 * cryptographic verification, cache I/O and capability authorization remain
 * outside this service.  The public seam intentionally contains values and
 * source buffers only; it exposes neither a renderer nor an RTOS object.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "services/pet_asset_service.h"

device_status_t pet_asset_apply_service_init(void);

/* Releases every non-NULL source frame.  The allocator choice is private to
 * the coordinator so a caller may free a partially consumed transaction by
 * the same rule used for its optional cache mirror. */
void pet_asset_apply_service_free_frames(
    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES], uint32_t frame_count);

/* True when at least `descriptor->frame_count` frames of this revision have
 * already been accepted by Display. */
bool pet_asset_apply_service_revision_installed(
    const pet_asset_descriptor_t *descriptor);

/* Installs a borrowed first frame without changing the recorded full-pack
 * revision. It keeps the startup standby surface useful while the remaining
 * verified frames are still downloading. */
device_status_t pet_asset_apply_service_install_preview(
    const pet_asset_descriptor_t *descriptor,
    uint8_t *const frames[PET_ASSET_SERVICE_MAX_FRAMES]);

/* The caller may supply a synchronous, value-only late-admission probe.  It
 * is called only while the coordinator holds its display-application mutex,
 * immediately before it clones or consumes a complete pack.  This prevents a
 * startup download which waited behind a newer runtime update from publishing
 * its stale descriptor.  The probe is not retained and must not call back
 * into this service. */
typedef bool (*pet_asset_apply_service_admitted_fn)(void *context);

/* Clears the rendered pet and forgets the applied revision only after the
 * Display service accepted the clear. Like full-pack installation, an
 * optional late-admission probe is evaluated only after taking the renderer
 * mutex. This prevents a stale withdrawn startup descriptor, which waited
 * behind a newer install, from clearing that newer artwork. */
device_status_t pet_asset_apply_service_clear(
    pet_asset_apply_service_admitted_fn admitted, void *admission_context);

/* Atomically prepares an optional complete cache mirror and consumes the
 * supplied source frames into Display. */
device_status_t pet_asset_apply_service_install_full(
    const pet_asset_descriptor_t *descriptor,
    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES],
    bool prepare_cache_mirror,
    uint8_t *cache_frames[PET_ASSET_SERVICE_MAX_FRAMES],
    pet_asset_apply_service_admitted_fn admitted,
    void *admission_context,
    int *out_installed_frame_count,
    int *out_installed_frame_ms);
