#include "pet_asset_cache_storage.h"

#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifdef _WIN32
#include <io.h>
#define pet_cache_sync_file(file) _commit(_fileno(file))
#else
#define pet_cache_sync_file(file) fsync(fileno(file))
#endif

#if defined(PET_CACHE_STORAGE_HOST_TEST)
#define PET_CACHE_STORAGE_BASE_PATH "build-host-tests/pet-cache"
#elif !defined(PET_CACHE_STORAGE_BASE_PATH)
#define PET_CACHE_STORAGE_BASE_PATH "/storage"
#endif
#define PET_CACHE_META_PATH PET_CACHE_STORAGE_BASE_PATH "/pet_asset.meta"
#define PET_CACHE_META_TMP_PATH PET_CACHE_STORAGE_BASE_PATH "/pet_asset.meta.tmp"
#define PET_CACHE_FRAME_PATH_FORMAT PET_CACHE_STORAGE_BASE_PATH "/pet_asset_%u.rgb565a8"
#define PET_CACHE_FRAME_TMP_PATH_FORMAT PET_CACHE_STORAGE_BASE_PATH "/pet_asset_%u.tmp"
#define PET_CACHE_LEGACY_FRAME_PATH_FORMAT PET_CACHE_STORAGE_BASE_PATH "/pet_asset_%u.rgb565le"
#define PET_CACHE_PATH_CAPACITY 64u
#define PET_CACHE_WRITE_CHUNK_BYTES 4096u

static bool cancelled(const pet_asset_cache_storage_options_t *options) {
    return options && options->cancelled && options->cancelled(options->context);
}

static void maybe_yield(const pet_asset_cache_storage_options_t *options) {
    if (options && options->yield) options->yield(options->context);
}

static void frame_path(char out[PET_CACHE_PATH_CAPACITY], const char *format, uint32_t index) {
    (void)snprintf(out, PET_CACHE_PATH_CAPACITY, format, (unsigned)index);
}

static void remove_path(const char *path) {
    if (path) (void)unlink(path);
}

static bool write_complete_file(FILE *file, const uint8_t *data, size_t size,
                                const pet_asset_cache_storage_options_t *options) {
    if (!file || !data) return false;
    size_t written = 0;
    while (written < size) {
        if (cancelled(options)) return false;
        size_t chunk = size - written;
        if (chunk > PET_CACHE_WRITE_CHUNK_BYTES) chunk = PET_CACHE_WRITE_CHUNK_BYTES;
        if (fwrite(data + written, 1, chunk, file) != chunk) return false;
        written += chunk;
        maybe_yield(options);
    }
    return fflush(file) == 0 && pet_cache_sync_file(file) == 0;
}

static bool replace_committed_file(const char *temporary, const char *final) {
    if (!temporary || !final) return false;
    if (unlink(final) != 0 && errno != ENOENT) {
        remove_path(temporary);
        return false;
    }
    if (rename(temporary, final) != 0) {
        remove_path(temporary);
        return false;
    }
    return true;
}

void pet_asset_cache_storage_clear(void) {
    remove_path(PET_CACHE_META_PATH);
    remove_path(PET_CACHE_META_TMP_PATH);
    char path[PET_CACHE_PATH_CAPACITY];
    for (uint32_t index = 0; index < PET_ASSET_SERVICE_MAX_FRAMES; ++index) {
        frame_path(path, PET_CACHE_FRAME_PATH_FORMAT, index);
        remove_path(path);
        frame_path(path, PET_CACHE_FRAME_TMP_PATH_FORMAT, index);
        remove_path(path);
        frame_path(path, PET_CACHE_LEGACY_FRAME_PATH_FORMAT, index);
        remove_path(path);
    }
}

bool pet_asset_cache_storage_write(const pet_asset_descriptor_t *descriptor,
                                   uint8_t *const frames[PET_ASSET_SERVICE_MAX_FRAMES],
                                   const pet_asset_cache_storage_options_t *options) {
    if (!descriptor || !frames || descriptor->frame_count < 1 ||
        descriptor->frame_count > (int)PET_ASSET_SERVICE_MAX_FRAMES) return false;
    size_t frame_bytes = 0;
    if (!pet_asset_service_frame_bytes(descriptor->width, descriptor->height, &frame_bytes)) {
        return false;
    }
    char metadata[PET_ASSET_SERVICE_CACHE_METADATA_CAPACITY];
    size_t metadata_length = 0;
    if (!pet_asset_service_format_cache_metadata(descriptor, metadata, sizeof(metadata),
                                                 &metadata_length)) return false;

    /* Remove the sole commit record first.  Direct final-frame writes avoid
     * doubling a 192 KiB object on fragmented SPIFFS; an interrupted sequence
     * remains invisible because restore requires the later metadata commit. */
    remove_path(PET_CACHE_META_PATH);
    remove_path(PET_CACHE_META_TMP_PATH);
    char path[PET_CACHE_PATH_CAPACITY];
    char temporary[PET_CACHE_PATH_CAPACITY];
    for (int index = 0; index < descriptor->frame_count; ++index) {
        if (!frames[index] || cancelled(options)) return false;
        frame_path(path, PET_CACHE_FRAME_PATH_FORMAT, (uint32_t)index);
        frame_path(temporary, PET_CACHE_FRAME_TMP_PATH_FORMAT, (uint32_t)index);
        remove_path(temporary);
        remove_path(path);
        maybe_yield(options);
        FILE *file = fopen(path, "wb");
        bool ok = write_complete_file(file, frames[index], frame_bytes, options);
        if (file && fclose(file) != 0) ok = false;
        if (!ok) {
            remove_path(path);
            return false;
        }
    }
    for (int index = descriptor->frame_count;
         index < (int)PET_ASSET_SERVICE_MAX_FRAMES; ++index) {
        if (cancelled(options)) return false;
        frame_path(path, PET_CACHE_FRAME_PATH_FORMAT, (uint32_t)index);
        remove_path(path);
        frame_path(path, PET_CACHE_FRAME_TMP_PATH_FORMAT, (uint32_t)index);
        remove_path(path);
    }

    FILE *meta = cancelled(options) ? NULL : fopen(PET_CACHE_META_TMP_PATH, "wb");
    bool metadata_ok = write_complete_file(meta, (const uint8_t *)metadata, metadata_length, options);
    if (meta && fclose(meta) != 0) metadata_ok = false;
    if (!metadata_ok || !replace_committed_file(PET_CACHE_META_TMP_PATH, PET_CACHE_META_PATH)) {
        remove_path(PET_CACHE_META_TMP_PATH);
        return false;
    }
    return true;
}

bool pet_asset_cache_storage_read_descriptor(pet_asset_descriptor_t *out) {
    if (!out) return false;
    FILE *file = fopen(PET_CACHE_META_PATH, "rb");
    if (!file) return false;
    char metadata[PET_ASSET_SERVICE_CACHE_METADATA_CAPACITY];
    const size_t length = fread(metadata, 1, sizeof(metadata), file);
    const bool complete = length < sizeof(metadata) && fgetc(file) == EOF;
    const bool close_ok = fclose(file) == 0;
    return close_ok && complete &&
           pet_asset_service_parse_cache_metadata(metadata, length, out);
}

bool pet_asset_cache_storage_read_frame(const pet_asset_descriptor_t *descriptor,
                                        uint32_t frame_index, uint8_t *out,
                                        size_t out_capacity) {
    if (!descriptor || !out || frame_index >= (uint32_t)descriptor->frame_count) return false;
    size_t frame_bytes = 0;
    if (!pet_asset_service_frame_bytes(descriptor->width, descriptor->height, &frame_bytes) ||
        out_capacity < frame_bytes) {
        return false;
    }
    char path[PET_CACHE_PATH_CAPACITY];
    frame_path(path, PET_CACHE_FRAME_PATH_FORMAT, frame_index);
    struct stat info;
    if (stat(path, &info) != 0 || info.st_size != (off_t)frame_bytes) return false;
    FILE *file = fopen(path, "rb");
    const bool read_ok = file && fread(out, 1, frame_bytes, file) == frame_bytes &&
                         fgetc(file) == EOF;
    const bool close_ok = !file || fclose(file) == 0;
    return read_ok && close_ok;
}

bool pet_asset_cache_storage_drop_if_stale(const char *new_revision) {
    pet_asset_descriptor_t cached = {0};
    const bool current = new_revision && new_revision[0] &&
                         pet_asset_cache_storage_read_descriptor(&cached) &&
                         strcmp(cached.revision, new_revision) == 0;
    if (current) return false;
    pet_asset_cache_storage_clear();
    return true;
}
