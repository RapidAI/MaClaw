/* Host test for the meeting-recording Storage adapter.  It verifies the
 * retained WAV transaction independently from ESP-IDF, FreeRTOS and HTTP. */

#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>

#include "meeting_recording_storage.h"

static int fail(const char *message) {
    fprintf(stderr, "FAIL: %s\n", message);
    return 1;
}

static uint32_t le32(const uint8_t *data) {
    return (uint32_t)data[0] | ((uint32_t)data[1] << 8) |
           ((uint32_t)data[2] << 16) | ((uint32_t)data[3] << 24);
}

static int test_checkpointed_placeholder_recovery(void) {
    (void)meeting_recording_storage_clear();
    meeting_recording_storage_handle_t *recording = NULL;
    if (meeting_recording_storage_create(&recording) != DEVICE_STATUS_OK || !recording) {
        return fail("create checkpoint fixture");
    }
    /* 32 KiB reaches the production durability cadence exactly.  Deliberately
     * close without finalize to model a reset after a completed checkpoint:
     * the retained zero-length placeholder must be repairable on recovery. */
    int16_t samples[16384] = {0};
    samples[0] = 17;
    samples[16383] = -17;
    if (meeting_recording_storage_append_pcm(recording, samples, 16384) != DEVICE_STATUS_OK) {
        meeting_recording_storage_close(recording);
        return fail("checkpoint append");
    }
    meeting_recording_storage_close(recording);

    struct stat info;
    if (stat("build-host-tests/meeting-recording/meeting.wav", &info) != 0 ||
        (uint64_t)info.st_size != 44u + sizeof(samples)) {
        return fail("checkpoint payload visible");
    }
    uint32_t size = 0;
    if (meeting_recording_storage_open_for_upload(&recording, &size) != DEVICE_STATUS_OK ||
        size != 44 + sizeof(samples)) {
        meeting_recording_storage_close(recording);
        return fail("checkpoint placeholder repair");
    }
    uint8_t header[44] = {0};
    uint32_t read = 0;
    if (meeting_recording_storage_read_range(recording, 0, header, sizeof(header), &read) !=
            DEVICE_STATUS_OK ||
        read != sizeof(header) || le32(header + 40) != sizeof(samples)) {
        meeting_recording_storage_close(recording);
        return fail("checkpoint repaired header");
    }
    meeting_recording_storage_close(recording);
    return 0;
}

int main(void) {
    (void)meeting_recording_storage_clear();
    meeting_recording_storage_handle_t *recording = NULL;
    if (meeting_recording_storage_create(&recording) != DEVICE_STATUS_OK ||
        !recording || meeting_recording_storage_has_pending_audio()) {
        return fail("placeholder create");
    }
    const int16_t samples[] = {42, -42, 32767, -32768};
    if (meeting_recording_storage_append_pcm(recording, samples, 4) != DEVICE_STATUS_OK ||
        meeting_recording_storage_finalize(recording, 4) != DEVICE_STATUS_OK) {
        return fail("append/finalize");
    }
    meeting_recording_storage_close(recording);
    if (!meeting_recording_storage_has_pending_audio()) return fail("pending audio");

    uint32_t size = 0;
    if (meeting_recording_storage_open_for_upload(&recording, &size) != DEVICE_STATUS_OK ||
        size != 52) return fail("open finalized WAV");
    uint8_t wav[52] = {0};
    uint32_t read = 0;
    if (meeting_recording_storage_read_range(recording, 0, wav, sizeof(wav), &read) !=
            DEVICE_STATUS_OK ||
        read != sizeof(wav) || memcmp(wav, "RIFF", 4) || memcmp(wav + 8, "WAVE", 4) ||
        le32(wav + 40) != 8 || memcmp(wav + 44, samples, sizeof(samples))) {
        return fail("WAV bytes");
    }
    if (meeting_recording_storage_read_range(recording, 51, wav, 2, &read) == DEVICE_STATUS_OK) {
        return fail("range boundary");
    }
    meeting_recording_storage_close(recording);

    /* A reset after PCM write can leave the known zero-length placeholder
     * header. Opening must repair exactly that retained payload. */
    if (meeting_recording_storage_create(&recording) != DEVICE_STATUS_OK ||
        meeting_recording_storage_append_pcm(recording, samples, 4) != DEVICE_STATUS_OK) {
        return fail("create stale header fixture");
    }
    meeting_recording_storage_close(recording);
    if (meeting_recording_storage_open_for_upload(&recording, &size) != DEVICE_STATUS_OK ||
        meeting_recording_storage_read_range(recording, 0, wav, 44, &read) != DEVICE_STATUS_OK ||
        le32(wav + 40) != 8) {
        return fail("repair stale header");
    }
    meeting_recording_storage_close(recording);

    if (test_checkpointed_placeholder_recovery() != 0) return 1;

    FILE *file = fopen("build-host-tests/meeting-recording/meeting.wav", "wb");
    uint8_t corrupt[44] = {0};
    memcpy(corrupt, "RIFF", 4);
    if (!file || fwrite(corrupt, 1, sizeof(corrupt), file) != sizeof(corrupt) ||
        fwrite(samples, sizeof(samples), 1, file) != 1 || fclose(file) != 0) {
        return fail("write corrupt-header fixture");
    }
    if (meeting_recording_storage_open_for_upload(&recording, &size) != DEVICE_STATUS_IO_ERROR) {
        meeting_recording_storage_close(recording);
        return fail("corrupt recording header was not reported as retained corruption");
    }
    if (meeting_recording_storage_has_pending_audio()) {
        return fail("corrupt recording treated as resumable");
    }

    file = fopen("build-host-tests/meeting-recording/meeting.wav", "wb");
    if (!file || fwrite(corrupt, 1, sizeof(corrupt), file) != sizeof(corrupt) ||
        fputc(1, file) == EOF || fclose(file) != 0) {
        return fail("write truncated fixture");
    }
    if (meeting_recording_storage_open_for_upload(&recording, &size) != DEVICE_STATUS_IO_ERROR) {
        meeting_recording_storage_close(recording);
        return fail("truncated recording was not reported as retained corruption");
    }
    if (meeting_recording_storage_has_pending_audio()) {
        return fail("truncated recording treated as resumable");
    }
    if (meeting_recording_storage_clear() != DEVICE_STATUS_OK ||
        meeting_recording_storage_has_pending_audio() ||
        meeting_recording_storage_open_for_upload(&recording, &size) != DEVICE_STATUS_NOT_FOUND) {
        return fail("clear/missing recording");
    }
    printf("PASS meeting recording storage adapter\n");
    return 0;
}
