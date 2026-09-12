#include "services/secret_storage.h"

#include <stdatomic.h>
#include <string.h>

#include "esp_err.h"
#include "esp_memory_utils.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "mbedtls/gcm.h"
#include "mbedtls/md.h"
#include "mbedtls/platform_util.h"
#include "persistence_service.h"

/*
 * self-contained backend:
 *   - AES-128-GCM via mbedtls (no dynamic allocation)
 *   - per-write random nonce sourced from esp_random / rand in host build
 *   - HMAC-SHA-256 derivation for the host test helper
 *
 * secret_storage_init / _deinit are idempotent. The static state is
 * initialised lazily by _init so the constructor is a no-op until the
 * subsystem is actually used. This avoids touching the key provider or the
 * persistence service on platforms that don't need it (e.g. the bootloader).
 */

#define SECRET_STORAGE_LOG_TAG "secret_storage"

static const uint8_t kMagic[SECRET_STORAGE_MAGIC_LEN] = {
    'S', 'C', 'S', 'V',
};

static atomic_flag s_lock = ATOMIC_FLAG_INIT;
static bool s_initialized;
static secret_storage_key_provider_fn s_key_provider;
static void *s_key_provider_context;

static void lock_storage(void) {
    while (atomic_flag_test_and_set_explicit(&s_lock, memory_order_acquire)) {}
}
static void unlock_storage(void) {
    atomic_flag_clear_explicit(&s_lock, memory_order_release);
}

device_status_t secret_storage_init(void) {
    lock_storage();
    if (!s_initialized) {
        s_key_provider = NULL;
        s_key_provider_context = NULL;
        s_initialized = true;
    }
    unlock_storage();
    return DEVICE_STATUS_OK;
}

device_status_t secret_storage_deinit(void) {
    lock_storage();
    s_key_provider = NULL;
    s_key_provider_context = NULL;
    s_initialized = false;
    unlock_storage();
    return DEVICE_STATUS_OK;
}

bool secret_storage_is_initialized(void) {
    lock_storage();
    const bool value = s_initialized;
    unlock_storage();
    return value;
}

device_status_t secret_storage_set_key_provider(secret_storage_key_provider_fn provider,
                                                void *context) {
    if (!provider) return DEVICE_STATUS_INVALID_ARGUMENT;
    lock_storage();
    if (!s_initialized) { unlock_storage(); return DEVICE_STATUS_UNAVAILABLE; }
    s_key_provider = provider;
    s_key_provider_context = context;
    unlock_storage();
    return DEVICE_STATUS_OK;
}

/* ------------------------------------------------------------------ */
/* Random nonce.  esp_random on the target, rand() in host build.      */
/* ------------------------------------------------------------------ */

static void fill_random(uint8_t *out, size_t length) {
#if defined(MACLAW_HOST_BUILD)
    for (size_t i = 0; i < length; ++i) {
        out[i] = (uint8_t)(rand() & 0xFF);
    }
#else
    const uint32_t produced = esp_random() & 0xFFFFFFFFu;
    if (length >= sizeof(uint32_t)) {
        memcpy(out, &produced, sizeof(uint32_t));
    } else {
        memcpy(out, &produced, length);
    }
    if (length > sizeof(uint32_t)) {
        /* Mix the remainder with a folded second draw so the nonce is not
         * trivially constant on a freshly-booted chip. */
        uint32_t tail = esp_random();
        for (size_t i = sizeof(uint32_t); i < length; ++i) {
            tail ^= (uint32_t)out[i - sizeof(uint32_t)] << ((i & 3u) * 8u);
            out[i] = (uint8_t)(tail & 0xFF);
        }
    }
#endif
}

/* ------------------------------------------------------------------ */
/* AEAD context (mbedtls_gcm on target, in-memory copy on host).      */
/* ------------------------------------------------------------------ */

struct secret_storage_aead_context {
    mbedtls_gcm_context gcm;
    bool ready;
};

secret_storage_aead_context_t *secret_storage_aead_new(void) {
    secret_storage_aead_context_t *context =
        (secret_storage_aead_context_t *)heap_caps_malloc(
            sizeof(*context), MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!context) return NULL;
    mbedtls_gcm_init(&context->gcm);
    context->ready = false;
    return context;
}

void secret_storage_aead_free(secret_storage_aead_context_t *context) {
    if (!context) return;
    if (context->ready) mbedtls_gcm_free(&context->gcm);
    mbedtls_platform_zeroize(context, sizeof(*context));
    heap_caps_free(context);
}

static device_status_t aead_set_key(secret_storage_aead_context_t *context,
                                    const uint8_t key[32]) {
    if (!context || !key) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int init_err = mbedtls_gcm_starts(&context->gcm, MBEDTLS_CIPHER_ID_AES,
                                            key, 128);
    if (init_err != 0) return DEVICE_STATUS_INTERNAL_ERROR;
    context->ready = true;
    return DEVICE_STATUS_OK;
}

static device_status_t aead_seal(secret_storage_aead_context_t *context,
                                 const uint8_t nonce[SECRET_STORAGE_NONCE_LEN],
                                 const uint8_t *plaintext, size_t plaintext_len,
                                 uint8_t tag[SECRET_STORAGE_TAG_LEN],
                                 uint8_t *ciphertext) {
    if (!context || !context->ready) return DEVICE_STATUS_UNAVAILABLE;
    /* Header consists of the magic + version + reserved + nonce + length so a
     * flipped nonce or length is caught by the tag. */
    uint8_t header[SECRET_STORAGE_HEADER_LEN - SECRET_STORAGE_TAG_LEN];
    memcpy(header, kMagic, SECRET_STORAGE_MAGIC_LEN);
    header[4] = SECRET_STORAGE_VERSION;
    memset(&header[5], 0, 3);
    memcpy(&header[8], nonce, SECRET_STORAGE_NONCE_LEN);
    const uint16_t length_field = (uint16_t)plaintext_len;
    memcpy(&header[20], &length_field, sizeof(length_field));
    const int err = mbedtls_gcm_crypt_and_tag(
        &context->gcm, MBEDTLS_GCM_ENCRYPT, plaintext_len, nonce,
        SECRET_STORAGE_NONCE_LEN, header, sizeof(header), plaintext, ciphertext,
        SECRET_STORAGE_TAG_LEN, tag);
    if (err != 0) return DEVICE_STATUS_INTERNAL_ERROR;
    return DEVICE_STATUS_OK;
}

static device_status_t aead_open(secret_storage_aead_context_t *context,
                                 const uint8_t nonce[SECRET_STORAGE_NONCE_LEN],
                                 const uint8_t *ciphertext, size_t ciphertext_len,
                                 const uint8_t tag[SECRET_STORAGE_TAG_LEN],
                                 uint8_t *plaintext) {
    if (!context || !context->ready) return DEVICE_STATUS_UNAVAILABLE;
    uint8_t header[SECRET_STORAGE_HEADER_LEN - SECRET_STORAGE_TAG_LEN];
    memcpy(header, kMagic, SECRET_STORAGE_MAGIC_LEN);
    header[4] = SECRET_STORAGE_VERSION;
    memset(&header[5], 0, 3);
    memcpy(&header[8], nonce, SECRET_STORAGE_NONCE_LEN);
    const uint16_t length_field = (uint16_t)ciphertext_len;
    memcpy(&header[20], &length_field, sizeof(length_field));
    const int err = mbedtls_gcm_crypt_and_tag(
        &context->gcm, MBEDTLS_GCM_DECRYPT, ciphertext_len, nonce,
        SECRET_STORAGE_NONCE_LEN, header, sizeof(header), ciphertext, plaintext,
        SECRET_STORAGE_TAG_LEN, tag);
    if (err != 0) return DEVICE_STATUS_INTERNAL_ERROR;
    return DEVICE_STATUS_OK;
}

/* ------------------------------------------------------------------ */
/* Storage write / read helpers                                        */
/* ------------------------------------------------------------------ */

static device_status_t call_key_provider(uint8_t out_key[32]) {
    lock_storage();
    secret_storage_key_provider_fn provider = s_key_provider;
    void *context = s_key_provider_context;
    const bool initialized = s_initialized;
    unlock_storage();
    if (!initialized) return DEVICE_STATUS_UNAVAILABLE;
    if (!provider) return DEVICE_STATUS_UNAVAILABLE;
    const device_status_t status = provider(out_key, context);
    if (status == DEVICE_STATUS_OK) {
        /* A misbehaving provider that returns OK without filling the buffer
         * must not silently degrade to "all zero" encryption. */
        static const uint8_t kZeroKey[32] = {0};
        if (memcmp(out_key, kZeroKey, sizeof(kZeroKey)) == 0) {
            return DEVICE_STATUS_INTERNAL_ERROR;
        }
    }
    return status;
}

device_status_t secret_storage_write(const char *name_space, const char *key,
                                     const void *data, size_t size) {
    if (!name_space || !key || (!data && size > 0)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (size > SECRET_STORAGE_MAX_PAYLOAD) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    uint8_t derived_key[32];
    const device_status_t key_status = call_key_provider(derived_key);
    if (key_status != DEVICE_STATUS_OK) return key_status;
    mbedtls_platform_zeroize(derived_key, sizeof(derived_key));

    const size_t blob_size = SECRET_STORAGE_HEADER_LEN + size;
    uint8_t *blob = (uint8_t *)heap_caps_malloc(blob_size, MALLOC_CAP_INTERNAL);
    if (!blob) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    secret_storage_aead_context_t *aead = secret_storage_aead_new();
    if (!aead) {
        heap_caps_free(blob);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    memcpy(blob, kMagic, SECRET_STORAGE_MAGIC_LEN);
    blob[4] = SECRET_STORAGE_VERSION;
    memset(&blob[5], 0, 3);
    uint8_t nonce[SECRET_STORAGE_NONCE_LEN];
    fill_random(nonce, sizeof(nonce));
    memcpy(&blob[8], nonce, SECRET_STORAGE_NONCE_LEN);
    const uint16_t length_field = (uint16_t)size;
    memcpy(&blob[20], &length_field, sizeof(length_field));
    uint8_t tag[SECRET_STORAGE_TAG_LEN];
    const device_status_t seal_status = aead_seal(
        aead, nonce, (const uint8_t *)data, size, tag, &blob[SECRET_STORAGE_HEADER_LEN]);
    mbedtls_platform_zeroize(nonce, sizeof(nonce));
    if (seal_status != DEVICE_STATUS_OK) {
        mbedtls_platform_zeroize(blob, blob_size);
        heap_caps_free(blob);
        secret_storage_aead_free(aead);
        return seal_status;
    }
    memcpy(&blob[22], tag, SECRET_STORAGE_TAG_LEN);
    mbedtls_platform_zeroize(tag, sizeof(tag));
    const device_status_t write_status = persistence_service_write_blob(
        name_space, key, blob, blob_size);
    mbedtls_platform_zeroize(blob, blob_size);
    heap_caps_free(blob);
    secret_storage_aead_free(aead);
    return write_status;
}

device_status_t secret_storage_read(const char *name_space, const char *key,
                                    void *out_value, size_t *inout_size) {
    if (!name_space || !key || !out_value || !inout_size) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    const size_t capacity = *inout_size;
    const size_t max_blob = SECRET_STORAGE_HEADER_LEN + SECRET_STORAGE_MAX_PAYLOAD;
    size_t blob_size = max_blob;
    uint8_t *blob = (uint8_t *)heap_caps_malloc(max_blob, MALLOC_CAP_INTERNAL);
    if (!blob) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    secret_storage_aead_context_t *aead = secret_storage_aead_new();
    if (!aead) {
        heap_caps_free(blob);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    const device_status_t read_status = persistence_service_read_blob(
        name_space, key, blob, &blob_size);
    if (read_status != DEVICE_STATUS_OK) {
        mbedtls_platform_zeroize(blob, max_blob);
        heap_caps_free(blob);
        secret_storage_aead_free(aead);
        return read_status;
    }
    if (blob_size < SECRET_STORAGE_HEADER_LEN ||
        memcmp(blob, kMagic, SECRET_STORAGE_MAGIC_LEN) != 0 ||
        blob[4] != SECRET_STORAGE_VERSION ||
        blob[5] != 0 || blob[6] != 0 || blob[7] != 0) {
        mbedtls_platform_zeroize(blob, max_blob);
        heap_caps_free(blob);
        secret_storage_aead_free(aead);
        return DEVICE_STATUS_NOT_FOUND;
    }
    uint8_t nonce[SECRET_STORAGE_NONCE_LEN];
    memcpy(nonce, &blob[8], SECRET_STORAGE_NONCE_LEN);
    uint16_t length_field = 0;
    memcpy(&length_field, &blob[20], sizeof(length_field));
    if (length_field > SECRET_STORAGE_MAX_PAYLOAD ||
        blob_size != (size_t)SECRET_STORAGE_HEADER_LEN + (size_t)length_field) {
        mbedtls_platform_zeroize(blob, max_blob);
        mbedtls_platform_zeroize(nonce, sizeof(nonce));
        heap_caps_free(blob);
        secret_storage_aead_free(aead);
        return DEVICE_STATUS_NOT_FOUND;
    }
    uint8_t tag[SECRET_STORAGE_TAG_LEN];
    memcpy(tag, &blob[22], SECRET_STORAGE_TAG_LEN);
    if (capacity < (size_t)length_field + 1u) {
        /* Always require room for the trailing NUL so callers can treat the
         * recovered bytes as a C string without overrunning. */
        mbedtls_platform_zeroize(blob, max_blob);
        mbedtls_platform_zeroize(nonce, sizeof(nonce));
        mbedtls_platform_zeroize(tag, sizeof(tag));
        heap_caps_free(blob);
        secret_storage_aead_free(aead);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    memset(out_value, 0, capacity);
    uint8_t derived_key[32];
    const device_status_t key_status = call_key_provider(derived_key);
    if (key_status != DEVICE_STATUS_OK) {
        mbedtls_platform_zeroize(blob, max_blob);
        mbedtls_platform_zeroize(nonce, sizeof(nonce));
        mbedtls_platform_zeroize(tag, sizeof(tag));
        heap_caps_free(blob);
        secret_storage_aead_free(aead);
        return key_status;
    }
    const device_status_t open_status = aead_open(
        aead, nonce, &blob[SECRET_STORAGE_HEADER_LEN], length_field, tag,
        (uint8_t *)out_value);
    mbedtls_platform_zeroize(derived_key, sizeof(derived_key));
    mbedtls_platform_zeroize(blob, max_blob);
    mbedtls_platform_zeroize(nonce, sizeof(nonce));
    mbedtls_platform_zeroize(tag, sizeof(tag));
    heap_caps_free(blob);
    secret_storage_aead_free(aead);
    if (open_status != DEVICE_STATUS_OK) {
        memset(out_value, 0, capacity);
        return DEVICE_STATUS_NOT_FOUND;
    }
    *inout_size = length_field;
    return DEVICE_STATUS_OK;
}

device_status_t secret_storage_erase(const char *name_space, const char *key) {
    return persistence_service_erase_key(name_space, key);
}

/* ------------------------------------------------------------------ */
/* HMAC-SHA-256 derivation used by the host test helper.              */
/* ------------------------------------------------------------------ */

device_status_t secret_storage_derive_test_key(const uint8_t seed[16],
                                               uint8_t out_key[32]) {
    if (!seed || !out_key) return DEVICE_STATUS_INVALID_ARGUMENT;
    static const uint8_t kLabel[] = "maclaw/secret-storage v1";
    const mbedtls_md_info_t *info = mbedtls_md_info_from_type(MBEDTLS_MD_SHA256);
    if (!info) return DEVICE_STATUS_UNAVAILABLE;
    const int err = mbedtls_md_hmac(info, kLabel, sizeof(kLabel) - 1u, seed, 16,
                                   out_key);
    if (err != 0) return DEVICE_STATUS_INTERNAL_ERROR;
    return DEVICE_STATUS_OK;
}