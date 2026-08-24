#include "meeting_recording_storage.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifdef _WIN32
#include <io.h>
#define meeting_recording_sync(file) _commit(_fileno(file))
#else
#define meeting_recording_sync(file) fsync(fileno(file))
#endif

#if defined(MEETING_RECORDING_STORAGE_HOST_TEST)
#define MEETING_RECORDING_PATH "build-host-tests/meeting-recording/meeting.wav"
#elif !defined(MEETING_RECORDING_PATH)
#define MEETING_RECORDING_PATH "/storage/meeting.wav"
#endif

#define MEETING_WAV_HEADER_BYTES 44u
#define MEETING_WAV_SAMPLE_RATE 16000u
/* Keep a known-valid placeholder header on disk from create(), then make
 * appended PCM durable at a bounded cadence.  Syncing every 512-sample I2S
 * block would put Flash/cache-off work on the audio hot path; never syncing
 * until finalize() leaves an unbounded amount of a retained meeting exposed
 * to reset/power loss.  At 16 kHz mono S16 this is one second of audio. */
#define MEETING_RECORDING_CHECKPOINT_BYTES (32u * 1024u)

struct meeting_recording_storage_handle {
    FILE *file;
    uint32_t size;
    uint32_t uncheckpointed_bytes;
};

static device_status_t io_status(void) {
    return DEVICE_STATUS_IO_ERROR;
}

static void put_le16(uint8_t *out, uint16_t value) {
    out[0] = (uint8_t)value;
    out[1] = (uint8_t)(value >> 8);
}

static void put_le32(uint8_t *out, uint32_t value) {
    out[0] = (uint8_t)value;
    out[1] = (uint8_t)(value >> 8);
    out[2] = (uint8_t)(value >> 16);
    out[3] = (uint8_t)(value >> 24);
}

static void build_wav_header(uint8_t header[MEETING_WAV_HEADER_BYTES],
                             uint32_t pcm_bytes) {
    memset(header, 0, MEETING_WAV_HEADER_BYTES);
    memcpy(header, "RIFF", 4);
    put_le32(header + 4, 36u + pcm_bytes);
    memcpy(header + 8, "WAVEfmt ", 8);
    put_le32(header + 16, 16);
    put_le16(header + 20, 1);
    put_le16(header + 22, 1);
    put_le32(header + 24, MEETING_WAV_SAMPLE_RATE);
    put_le32(header + 28, MEETING_WAV_SAMPLE_RATE * 2u);
    put_le16(header + 32, 2);
    put_le16(header + 34, 16);
    memcpy(header + 36, "data", 4);
    put_le32(header + 40, pcm_bytes);
}

static bool flush_durable(FILE *file) {
    return file && fflush(file) == 0 && meeting_recording_sync(file) == 0;
}

static device_status_t write_header(FILE *file, uint32_t pcm_bytes) {
    if (!file) return DEVICE_STATUS_INVALID_ARGUMENT;
    uint8_t header[MEETING_WAV_HEADER_BYTES];
    build_wav_header(header, pcm_bytes);
    if (fseek(file, 0, SEEK_SET) != 0 ||
        fwrite(header, 1, sizeof(header), file) != sizeof(header) ||
        !flush_durable(file)) {
        return io_status();
    }
    return DEVICE_STATUS_OK;
}

static device_status_t validate_or_repair_header(FILE *file, uint32_t size) {
    if (!file) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (size == MEETING_WAV_HEADER_BYTES) {
        /* The durable zero-length placeholder is a known no-audio state.  It
         * is not resumable and lets Meeting Service discard its pending marker
         * after a recording that never received a PCM sample. */
        return DEVICE_STATUS_NOT_FOUND;
    }
    if (size < MEETING_WAV_HEADER_BYTES ||
        ((size - MEETING_WAV_HEADER_BYTES) % sizeof(int16_t)) != 0) {
        /* This is materially different from an absent object.  Recovery must
         * retain its metadata as evidence rather than treating a truncated
         * retained object as an already-cleaned meeting. */
        return DEVICE_STATUS_IO_ERROR;
    }
    const uint32_t pcm_bytes = size - MEETING_WAV_HEADER_BYTES;
    uint8_t expected[MEETING_WAV_HEADER_BYTES];
    uint8_t actual[MEETING_WAV_HEADER_BYTES];
    uint8_t placeholder[MEETING_WAV_HEADER_BYTES];
    build_wav_header(expected, pcm_bytes);
    if (fseek(file, 0, SEEK_SET) != 0 ||
        fread(actual, 1, sizeof(actual), file) != sizeof(actual)) {
        return io_status();
    }
    if (memcmp(actual, expected, sizeof(expected)) == 0) return DEVICE_STATUS_OK;
    build_wav_header(placeholder, 0);
    if (memcmp(actual, placeholder, sizeof(placeholder)) != 0) {
        /* Do not turn arbitrary damaged/foreign content into an apparently
         * valid meeting merely because its byte count happens to align. */
        return DEVICE_STATUS_IO_ERROR;
    }
    /* A power loss after appending PCM can leave the known durable zero-length
     * placeholder in front of an otherwise exact PCM payload.  This is the
     * only header repair that is safe to publish for resume. */
    return write_header(file, pcm_bytes);
}

device_status_t meeting_recording_storage_create(
    meeting_recording_storage_handle_t **out_handle) {
    if (!out_handle) return DEVICE_STATUS_INVALID_ARGUMENT;
    *out_handle = NULL;
    FILE *file = fopen(MEETING_RECORDING_PATH, "wb+");
    if (!file) return io_status();
    device_status_t status = write_header(file, 0);
    if (status != DEVICE_STATUS_OK) {
        (void)fclose(file);
        (void)unlink(MEETING_RECORDING_PATH);
        return status;
    }
    meeting_recording_storage_handle_t *handle = calloc(1, sizeof(*handle));
    if (!handle) {
        (void)fclose(file);
        (void)unlink(MEETING_RECORDING_PATH);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    handle->file = file;
    handle->size = MEETING_WAV_HEADER_BYTES;
    *out_handle = handle;
    return DEVICE_STATUS_OK;
}

device_status_t meeting_recording_storage_append_pcm(
    meeting_recording_storage_handle_t *handle, const int16_t *samples,
    uint32_t sample_count) {
    if (!handle || !handle->file || (!samples && sample_count != 0)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (sample_count > (UINT32_MAX - handle->size) / sizeof(*samples)) {
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    if (sample_count != 0 &&
        fwrite(samples, sizeof(*samples), sample_count, handle->file) != sample_count) {
        return io_status();
    }
    const uint32_t appended_bytes = sample_count * sizeof(*samples);
    handle->size += appended_bytes;
    /* Overflow is impossible because the size admission above leaves enough
     * room for this exact append, but saturating here keeps a future larger
     * checkpoint interval from silently wrapping into a false "not due". */
    if (handle->uncheckpointed_bytes > UINT32_MAX - appended_bytes) {
        handle->uncheckpointed_bytes = MEETING_RECORDING_CHECKPOINT_BYTES;
    } else {
        handle->uncheckpointed_bytes += appended_bytes;
    }
    if (handle->uncheckpointed_bytes >= MEETING_RECORDING_CHECKPOINT_BYTES) {
        if (!flush_durable(handle->file)) return io_status();
        handle->uncheckpointed_bytes = 0;
    }
    return DEVICE_STATUS_OK;
}

device_status_t meeting_recording_storage_finalize(
    meeting_recording_storage_handle_t *handle, uint64_t sample_count) {
    if (!handle || !handle->file || sample_count == 0 ||
        sample_count > UINT32_MAX / sizeof(int16_t)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    const uint32_t expected_size = MEETING_WAV_HEADER_BYTES +
                                   (uint32_t)sample_count * sizeof(int16_t);
    if (handle->size != expected_size) return DEVICE_STATUS_IO_ERROR;
    device_status_t status = write_header(handle->file,
                                          expected_size - MEETING_WAV_HEADER_BYTES);
    if (status == DEVICE_STATUS_OK) handle->uncheckpointed_bytes = 0;
    return status;
}

device_status_t meeting_recording_storage_open_for_upload(
    meeting_recording_storage_handle_t **out_handle, uint32_t *out_size) {
    if (!out_handle || !out_size) return DEVICE_STATUS_INVALID_ARGUMENT;
    *out_handle = NULL;
    *out_size = 0;
    struct stat info;
    if (stat(MEETING_RECORDING_PATH, &info) != 0) {
        return errno == ENOENT ? DEVICE_STATUS_NOT_FOUND : io_status();
    }
    if (info.st_size < 0 || (uint64_t)info.st_size > UINT32_MAX) {
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    FILE *file = fopen(MEETING_RECORDING_PATH, "rb+");
    if (!file) return io_status();
    const uint32_t size = (uint32_t)info.st_size;
    device_status_t status = validate_or_repair_header(file, size);
    if (status != DEVICE_STATUS_OK) {
        (void)fclose(file);
        return status;
    }
    meeting_recording_storage_handle_t *handle = calloc(1, sizeof(*handle));
    if (!handle) {
        (void)fclose(file);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    handle->file = file;
    handle->size = size;
    *out_handle = handle;
    *out_size = size;
    return DEVICE_STATUS_OK;
}

device_status_t meeting_recording_storage_read_range(
    void *context, uint32_t offset, void *buffer, uint32_t requested,
    uint32_t *out_read) {
    meeting_recording_storage_handle_t *handle = context;
    if (!handle || !handle->file || !buffer || !out_read || offset > handle->size ||
        requested > handle->size - offset) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    *out_read = 0;
    if (fseek(handle->file, (long)offset, SEEK_SET) != 0) return io_status();
    const size_t count = fread(buffer, 1, requested, handle->file);
    *out_read = (uint32_t)count;
    return count == requested ? DEVICE_STATUS_OK : io_status();
}

void meeting_recording_storage_close(meeting_recording_storage_handle_t *handle) {
    if (!handle) return;
    if (handle->file) (void)fclose(handle->file);
    free(handle);
}

bool meeting_recording_storage_has_pending_audio(void) {
    struct stat info;
    if (stat(MEETING_RECORDING_PATH, &info) != 0 ||
        info.st_size <= (off_t)MEETING_WAV_HEADER_BYTES) {
        return false;
    }

    /* The recovery marker is durable independently from the WAV object.  Do
     * not let its presence turn arbitrary bytes at the retained-object path
     * into a resumable meeting at boot.  Reuse the same strict validation (and
     * the one allowed stale-placeholder repair) that the uploader uses, so the
     * startup decision and the later upload decision cannot disagree. */
    meeting_recording_storage_handle_t *recording = NULL;
    uint32_t size = 0;
    const device_status_t status =
        meeting_recording_storage_open_for_upload(&recording, &size);
    if (recording) meeting_recording_storage_close(recording);
    return status == DEVICE_STATUS_OK && size > MEETING_WAV_HEADER_BYTES;
}

device_status_t meeting_recording_storage_clear(void) {
    if (unlink(MEETING_RECORDING_PATH) == 0 || errno == ENOENT) {
        return DEVICE_STATUS_OK;
    }
    return io_status();
}
