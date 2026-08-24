#pragma once

/*
 * Meeting recording Storage adapter.
 *
 * Owns the retained WAV object's name, VFS handle and WAV integrity rules.
 * Meeting Service retains consent/state/upload policy and crypto; the
 * composition root retains HTTP transport and worker lifetime.  The opaque
 * handle intentionally keeps the native VFS handle and storage path out of
 * both layers.
 */

#include <stddef.h>
#include <stdint.h>

#include "device_api.h"

typedef struct meeting_recording_storage_handle meeting_recording_storage_handle_t;

/* Creates a durable zero-length WAV placeholder.  The caller must persist its
 * recovery marker only after this function succeeds. */
device_status_t meeting_recording_storage_create(
    meeting_recording_storage_handle_t **out_handle);

/* Appends PCM in capture-sized blocks.  The adapter checkpoints PCM at a
 * bounded cadence while retaining the original zero-length header; after an
 * interrupted recording, open_for_upload() may repair only that known
 * placeholder header. */
device_status_t meeting_recording_storage_append_pcm(
    meeting_recording_storage_handle_t *handle, const int16_t *samples,
    uint32_t sample_count);

/* Finalizes the header and flushes the retained recording. */
device_status_t meeting_recording_storage_finalize(
    meeting_recording_storage_handle_t *handle, uint64_t sample_count);

/* Opens a retained recording, repairs a valid PCM payload's stale placeholder
 * header if necessary, and returns the exact byte length for upload.  A known
 * zero-length placeholder returns NOT_FOUND; an existing malformed/truncated
 * object returns IO_ERROR so recovery can retain its diagnostic metadata. */
device_status_t meeting_recording_storage_open_for_upload(
    meeting_recording_storage_handle_t **out_handle, uint32_t *out_size);

/* Reads an exact bounded byte range.  It is suitable as an opaque transport
 * callback; callers never receive a VFS handle. */
device_status_t meeting_recording_storage_read_range(
    void *context, uint32_t offset, void *buffer, uint32_t requested,
    uint32_t *out_read);

void meeting_recording_storage_close(meeting_recording_storage_handle_t *handle);

/* True only for a retained recording containing at least one PCM sample and
 * passing the same integrity policy used for upload.  It may repair the known
 * durable zero-length placeholder header after an interrupted recording. */
bool meeting_recording_storage_has_pending_audio(void);

/* Removes only the retained meeting WAV, never recovery metadata or other
 * Storage objects.  Missing recordings are considered already clear. */
device_status_t meeting_recording_storage_clear(void);
