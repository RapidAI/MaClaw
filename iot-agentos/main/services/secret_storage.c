#include "services/secret_storage.h"

#include <stdatomic.h>
#include <string.h>

#if defined(MACLAW_HOST_BUILD)
#include <stdlib.h>

#include "esp_heap_caps.h"
#else
#include "esp_err.h"
#include "esp_heap_caps.h"
#include "esp_memory_utils.h"
#include "esp_random.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "mbedtls/platform_util.h"
#include "psa/crypto.h"
#endif

#include "persistence_service.h"

/*
 * self-contained backend:
 *   - AES-128-GCM via the PSA Crypto API on the target (no dynamic
 *     allocation: the key is imported into a volatile PSA key slot and the
 *     multipart operation state lives on the caller's stack)
 *   - host builds use an in-memory copy of the payload authenticated with a
 *     self-contained HMAC-SHA-256, so the host unit test needs no crypto
 *     library while still exercising real tag/nonce tamper detection
 *   - per-write random nonce sourced from esp_random / rand in host build
 *   - HMAC-SHA-256 derivation for the host test helper
 *
 * secret_storage_init / _deinit are idempotent. The static state is
 * initialised lazily by _init so the constructor is a no-op until the
 * subsystem is actually used. This avoids touching the key provider or the
 * persistence service on platforms that don't need it (e.g. the bootloader).
 */

#define SECRET_STORAGE_LOG_TAG "secret_storage"

#if !defined(MACLAW_HOST_BUILD)
/* AES-128-GCM with the fixed 128-bit tag the blob layout reserves. */
#define SECRET_STORAGE_AEAD_ALG \
    PSA_ALG_AEAD_WITH_SHORTENED_TAG(PSA_ALG_GCM, SECRET_STORAGE_TAG_LEN)
#endif

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

/*
 * Scrub helper. On the target this is mbedtls_platform_zeroize() from the
 * public TF-PSA-Crypto compatibility header; host builds cannot assume any
 * mbedTLS headers, so use memset() pinned in place with a compiler barrier.
 * Key material held inside PSA key slots is wiped by psa_destroy_key().
 */
#if defined(MACLAW_HOST_BUILD)
static void secret_storage_zeroize(void *buf, size_t len) {
    if (!buf || len == 0) return;
    memset(buf, 0, len);
    __asm__ volatile("" : : "r"(buf), "r"(len) : "memory");
}
#else
static void secret_storage_zeroize(void *buf, size_t len) {
    mbedtls_platform_zeroize(buf, len);
}
#endif

#if defined(MACLAW_HOST_BUILD)
/* ------------------------------------------------------------------ */
/* Self-contained SHA-256 / HMAC-SHA-256 for the host stand-in.       */
/*                                                                    */
/* The host backend keeps the payload as an in-memory copy, but the   */
/* authentication tag and the test-key derivation are real            */
/* HMAC-SHA-256 so the host unit test exercises genuine AAD and tag   */
/* binding without depending on a host crypto library.                */
/* ------------------------------------------------------------------ */

typedef struct {
    uint32_t h[8];
    uint64_t total_len;
    uint8_t block[64];
    size_t block_len;
} host_sha256_context_t;

static uint32_t host_sha256_rotr32(uint32_t value, uint32_t bits) {
    return (value >> bits) | (value << (32u - bits));
}

static void host_sha256_compress(host_sha256_context_t *ctx,
                                 const uint8_t block[64]) {
    static const uint32_t kRound[64] = {
        0x428a2f98u, 0x71374491u, 0xb5c0fbcfu, 0xe9b5dba5u,
        0x3956c25bu, 0x59f111f1u, 0x923f82a4u, 0xab1c5ed5u,
        0xd807aa98u, 0x12835b01u, 0x243185beu, 0x550c7dc3u,
        0x72be5d74u, 0x80deb1feu, 0x9bdc06a7u, 0xc19bf174u,
        0xe49b69c1u, 0xefbe4786u, 0x0fc19dc6u, 0x240ca1ccu,
        0x2de92c6fu, 0x4a7484aau, 0x5cb0a9dcu, 0x76f988dau,
        0x983e5152u, 0xa831c66du, 0xb00327c8u, 0xbf597fc7u,
        0xc6e00bf3u, 0xd5a79147u, 0x06ca6351u, 0x14292967u,
        0x27b70a85u, 0x2e1b2138u, 0x4d2c6dfcu, 0x53380d13u,
        0x650a7354u, 0x766a0abbu, 0x81c2c92eu, 0x92722c85u,
        0xa2bfe8a1u, 0xa81a664bu, 0xc24b8b70u, 0xc76c51a3u,
        0xd192e819u, 0xd6990624u, 0xf40e3585u, 0x106aa070u,
        0x19a4c116u, 0x1e376c08u, 0x2748774cu, 0x34b0bcb5u,
        0x391c0cb3u, 0x4ed8aa4au, 0x5b9cca4fu, 0x682e6ff3u,
        0x748f82eeu, 0x78a5636fu, 0x84c87814u, 0x8cc70208u,
        0x90befffau, 0xa4506cebu, 0xbef9a3f7u, 0xc67178f2u,
    };
    uint32_t w[64];
    for (size_t i = 0; i < 16u; ++i) {
        w[i] = ((uint32_t)block[i * 4u] << 24) |
               ((uint32_t)block[i * 4u + 1u] << 16) |
               ((uint32_t)block[i * 4u + 2u] << 8) |
               (uint32_t)block[i * 4u + 3u];
    }
    for (size_t i = 16u; i < 64u; ++i) {
        const uint32_t s0 = host_sha256_rotr32(w[i - 15u], 7) ^
                            host_sha256_rotr32(w[i - 15u], 18) ^
                            (w[i - 15u] >> 3);
        const uint32_t s1 = host_sha256_rotr32(w[i - 2u], 17) ^
                            host_sha256_rotr32(w[i - 2u], 19) ^
                            (w[i - 2u] >> 10);
        w[i] = w[i - 16u] + s0 + w[i - 7u] + s1;
    }
    uint32_t a = ctx->h[0];
    uint32_t b = ctx->h[1];
    uint32_t c = ctx->h[2];
    uint32_t d = ctx->h[3];
    uint32_t e = ctx->h[4];
    uint32_t f = ctx->h[5];
    uint32_t g = ctx->h[6];
    uint32_t h = ctx->h[7];
    for (size_t i = 0; i < 64u; ++i) {
        const uint32_t s1 = host_sha256_rotr32(e, 6) ^ host_sha256_rotr32(e, 11) ^
                            host_sha256_rotr32(e, 25);
        const uint32_t ch = (e & f) ^ (~e & g);
        const uint32_t t1 = h + s1 + ch + kRound[i] + w[i];
        const uint32_t s0 = host_sha256_rotr32(a, 2) ^ host_sha256_rotr32(a, 13) ^
                            host_sha256_rotr32(a, 22);
        const uint32_t maj = (a & b) ^ (a & c) ^ (b & c);
        const uint32_t t2 = s0 + maj;
        h = g;
        g = f;
        f = e;
        e = d + t1;
        d = c;
        c = b;
        b = a;
        a = t1 + t2;
    }
    ctx->h[0] += a;
    ctx->h[1] += b;
    ctx->h[2] += c;
    ctx->h[3] += d;
    ctx->h[4] += e;
    ctx->h[5] += f;
    ctx->h[6] += g;
    ctx->h[7] += h;
}

static void host_sha256_init(host_sha256_context_t *ctx) {
    static const uint32_t kInitial[8] = {
        0x6a09e667u, 0xbb67ae85u, 0x3c6ef372u, 0xa54ff53au,
        0x510e527fu, 0x9b05688cu, 0x1f83d9abu, 0x5be0cd19u,
    };
    memcpy(ctx->h, kInitial, sizeof(kInitial));
    ctx->total_len = 0;
    ctx->block_len = 0;
}

static void host_sha256_update(host_sha256_context_t *ctx, const uint8_t *data,
                               size_t len) {
    ctx->total_len += len;
    while (len > 0) {
        const size_t take =
            (64u - ctx->block_len) < len ? (64u - ctx->block_len) : len;
        memcpy(&ctx->block[ctx->block_len], data, take);
        ctx->block_len += take;
        data += take;
        len -= take;
        if (ctx->block_len == 64u) {
            host_sha256_compress(ctx, ctx->block);
            ctx->block_len = 0;
        }
    }
}

static void host_sha256_final(host_sha256_context_t *ctx, uint8_t out[32]) {
    const uint64_t bit_len = ctx->total_len * 8u;
    uint8_t pad = 0x80u;
    host_sha256_update(ctx, &pad, 1);
    uint8_t zero = 0;
    while (ctx->block_len != 56u) {
        host_sha256_update(ctx, &zero, 1);
    }
    uint8_t length_be[8];
    for (size_t i = 0; i < 8u; ++i) {
        length_be[i] = (uint8_t)(bit_len >> (56u - i * 8u));
    }
    host_sha256_update(ctx, length_be, sizeof(length_be));
    for (size_t i = 0; i < 8u; ++i) {
        out[i * 4u] = (uint8_t)(ctx->h[i] >> 24);
        out[i * 4u + 1u] = (uint8_t)(ctx->h[i] >> 16);
        out[i * 4u + 2u] = (uint8_t)(ctx->h[i] >> 8);
        out[i * 4u + 3u] = (uint8_t)ctx->h[i];
    }
    secret_storage_zeroize(ctx, sizeof(*ctx));
}

/* out = HMAC-SHA-256(key, part1 || part2). Either part may be NULL when its
 * length is zero. */
static void host_hmac_sha256(const uint8_t *key, size_t key_len,
                             const uint8_t *part1, size_t part1_len,
                             const uint8_t *part2, size_t part2_len,
                             uint8_t out[32]) {
    uint8_t key_block[64];
    uint8_t stage[64 + 32];
    uint8_t inner[32];
    host_sha256_context_t ctx;

    memset(key_block, 0, sizeof(key_block));
    if (key_len > sizeof(key_block)) {
        host_sha256_init(&ctx);
        host_sha256_update(&ctx, key, key_len);
        host_sha256_final(&ctx, key_block);
    } else if (key_len > 0) {
        memcpy(key_block, key, key_len);
    }

    for (size_t i = 0; i < 64u; ++i) stage[i] = key_block[i] ^ 0x36u;
    host_sha256_init(&ctx);
    host_sha256_update(&ctx, stage, 64);
    if (part1_len > 0) host_sha256_update(&ctx, part1, part1_len);
    if (part2_len > 0) host_sha256_update(&ctx, part2, part2_len);
    host_sha256_final(&ctx, inner);

    for (size_t i = 0; i < 64u; ++i) stage[i] = key_block[i] ^ 0x5cu;
    host_sha256_init(&ctx);
    host_sha256_update(&ctx, stage, 64);
    host_sha256_update(&ctx, inner, sizeof(inner));
    host_sha256_final(&ctx, out);

    secret_storage_zeroize(key_block, sizeof(key_block));
    secret_storage_zeroize(stage, sizeof(stage));
    secret_storage_zeroize(inner, sizeof(inner));
}
#endif /* MACLAW_HOST_BUILD */

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
/* AEAD context (PSA AES-128-GCM on target, in-memory copy on host).  */
/* ------------------------------------------------------------------ */

struct secret_storage_aead_context {
#if defined(MACLAW_HOST_BUILD)
    uint8_t key[16];
#else
    psa_key_id_t key;
#endif
    bool ready;
};

secret_storage_aead_context_t *secret_storage_aead_new(void) {
    secret_storage_aead_context_t *context =
        (secret_storage_aead_context_t *)heap_caps_malloc(
            sizeof(*context), MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!context) return NULL;
#if !defined(MACLAW_HOST_BUILD)
    context->key = 0;
#endif
    context->ready = false;
    return context;
}

void secret_storage_aead_free(secret_storage_aead_context_t *context) {
    if (!context) return;
#if !defined(MACLAW_HOST_BUILD)
    /* psa_destroy_key() wipes the slot's key material per the PSA spec. */
    if (context->ready) psa_destroy_key(context->key);
#endif
    secret_storage_zeroize(context, sizeof(*context));
    heap_caps_free(context);
}

static device_status_t aead_set_key(secret_storage_aead_context_t *context,
                                    const uint8_t key[32]) {
    if (!context || !key) return DEVICE_STATUS_INVALID_ARGUMENT;
#if defined(MACLAW_HOST_BUILD)
    memcpy(context->key, key, sizeof(context->key));
    context->ready = true;
    return DEVICE_STATUS_OK;
#else
    if (psa_crypto_init() != PSA_SUCCESS) return DEVICE_STATUS_UNAVAILABLE;
    /* The header contract splits the 32-byte provider output into two
     * independent 16-byte halves; AES-128 uses the leading half. */
    psa_key_attributes_t attributes = PSA_KEY_ATTRIBUTES_INIT;
    psa_set_key_usage_flags(&attributes,
                            PSA_KEY_USAGE_ENCRYPT | PSA_KEY_USAGE_DECRYPT);
    psa_set_key_algorithm(&attributes, SECRET_STORAGE_AEAD_ALG);
    psa_set_key_type(&attributes, PSA_KEY_TYPE_AES);
    psa_set_key_bits(&attributes, 128);
    const psa_status_t status =
        psa_import_key(&attributes, key, sizeof(context->key), &context->key);
    psa_reset_key_attributes(&attributes);
    if (status != PSA_SUCCESS) return DEVICE_STATUS_INTERNAL_ERROR;
    context->ready = true;
    return DEVICE_STATUS_OK;
#endif
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
#if defined(MACLAW_HOST_BUILD)
    /* In-memory copy backend: confidentiality is a stand-in, but the tag is
     * real HMAC-SHA-256 over the same authenticated data the target binds. */
    memcpy(ciphertext, plaintext, plaintext_len);
    uint8_t full_tag[32];
    host_hmac_sha256(context->key, sizeof(context->key), header, sizeof(header),
                     ciphertext, plaintext_len, full_tag);
    memcpy(tag, full_tag, SECRET_STORAGE_TAG_LEN);
    secret_storage_zeroize(full_tag, sizeof(full_tag));
    secret_storage_zeroize(header, sizeof(header));
    return DEVICE_STATUS_OK;
#else
    /* Multipart AEAD keeps the ciphertext and the tag in separate buffers,
     * matching the blob layout, with no scratch allocation. */
    static const uint8_t kEmpty = 0;
    psa_aead_operation_t operation = PSA_AEAD_OPERATION_INIT;
    psa_status_t status =
        psa_aead_encrypt_setup(&operation, context->key, SECRET_STORAGE_AEAD_ALG);
    if (status == PSA_SUCCESS) {
        status = psa_aead_set_nonce(&operation, nonce, SECRET_STORAGE_NONCE_LEN);
    }
    if (status == PSA_SUCCESS) {
        status = psa_aead_update_ad(&operation, header, sizeof(header));
    }
    size_t produced = 0;
    if (status == PSA_SUCCESS) {
        status = psa_aead_update(&operation,
                                 plaintext_len > 0 ? plaintext : &kEmpty,
                                 plaintext_len, ciphertext, plaintext_len,
                                 &produced);
    }
    size_t finished = 0;
    size_t tag_len = 0;
    if (status == PSA_SUCCESS) {
        status = psa_aead_finish(&operation, ciphertext + produced,
                                 SECRET_STORAGE_TAG_LEN, &finished, tag,
                                 SECRET_STORAGE_TAG_LEN, &tag_len);
    }
    if (status != PSA_SUCCESS) {
        psa_aead_abort(&operation);
        secret_storage_zeroize(header, sizeof(header));
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    secret_storage_zeroize(header, sizeof(header));
    return DEVICE_STATUS_OK;
#endif
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
#if defined(MACLAW_HOST_BUILD)
    uint8_t full_tag[32];
    host_hmac_sha256(context->key, sizeof(context->key), header, sizeof(header),
                     ciphertext, ciphertext_len, full_tag);
    uint8_t tag_diff = 0;
    for (size_t i = 0; i < SECRET_STORAGE_TAG_LEN; ++i) {
        tag_diff |= full_tag[i] ^ tag[i];
    }
    secret_storage_zeroize(full_tag, sizeof(full_tag));
    secret_storage_zeroize(header, sizeof(header));
    if (tag_diff != 0) return DEVICE_STATUS_INTERNAL_ERROR;
    memcpy(plaintext, ciphertext, ciphertext_len);
    return DEVICE_STATUS_OK;
#else
    static const uint8_t kEmpty = 0;
    psa_aead_operation_t operation = PSA_AEAD_OPERATION_INIT;
    psa_status_t status =
        psa_aead_decrypt_setup(&operation, context->key, SECRET_STORAGE_AEAD_ALG);
    if (status == PSA_SUCCESS) {
        status = psa_aead_set_nonce(&operation, nonce, SECRET_STORAGE_NONCE_LEN);
    }
    if (status == PSA_SUCCESS) {
        status = psa_aead_update_ad(&operation, header, sizeof(header));
    }
    size_t produced = 0;
    if (status == PSA_SUCCESS) {
        status = psa_aead_update(&operation,
                                 ciphertext_len > 0 ? ciphertext : &kEmpty,
                                 ciphertext_len, plaintext, ciphertext_len,
                                 &produced);
    }
    size_t verified = 0;
    if (status == PSA_SUCCESS) {
        /* The final verify both flushes trailing plaintext and checks the
         * tag; any mismatch surfaces as PSA_ERROR_INVALID_SIGNATURE. */
        status = psa_aead_verify(&operation, plaintext + produced,
                                 ciphertext_len - produced, &verified, tag,
                                 SECRET_STORAGE_TAG_LEN);
    }
    if (status != PSA_SUCCESS) {
        psa_aead_abort(&operation);
        secret_storage_zeroize(header, sizeof(header));
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    secret_storage_zeroize(header, sizeof(header));
    return DEVICE_STATUS_OK;
#endif
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
    secret_storage_zeroize(derived_key, sizeof(derived_key));

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
    secret_storage_zeroize(nonce, sizeof(nonce));
    if (seal_status != DEVICE_STATUS_OK) {
        secret_storage_zeroize(blob, blob_size);
        heap_caps_free(blob);
        secret_storage_aead_free(aead);
        return seal_status;
    }
    memcpy(&blob[22], tag, SECRET_STORAGE_TAG_LEN);
    secret_storage_zeroize(tag, sizeof(tag));
    const device_status_t write_status = persistence_service_write_blob(
        name_space, key, blob, blob_size);
    secret_storage_zeroize(blob, blob_size);
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
        secret_storage_zeroize(blob, max_blob);
        heap_caps_free(blob);
        secret_storage_aead_free(aead);
        return read_status;
    }
    if (blob_size < SECRET_STORAGE_HEADER_LEN ||
        memcmp(blob, kMagic, SECRET_STORAGE_MAGIC_LEN) != 0 ||
        blob[4] != SECRET_STORAGE_VERSION ||
        blob[5] != 0 || blob[6] != 0 || blob[7] != 0) {
        secret_storage_zeroize(blob, max_blob);
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
        secret_storage_zeroize(blob, max_blob);
        secret_storage_zeroize(nonce, sizeof(nonce));
        heap_caps_free(blob);
        secret_storage_aead_free(aead);
        return DEVICE_STATUS_NOT_FOUND;
    }
    uint8_t tag[SECRET_STORAGE_TAG_LEN];
    memcpy(tag, &blob[22], SECRET_STORAGE_TAG_LEN);
    if (capacity < (size_t)length_field + 1u) {
        /* Always require room for the trailing NUL so callers can treat the
         * recovered bytes as a C string without overrunning. */
        secret_storage_zeroize(blob, max_blob);
        secret_storage_zeroize(nonce, sizeof(nonce));
        secret_storage_zeroize(tag, sizeof(tag));
        heap_caps_free(blob);
        secret_storage_aead_free(aead);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    memset(out_value, 0, capacity);
    uint8_t derived_key[32];
    const device_status_t key_status = call_key_provider(derived_key);
    if (key_status != DEVICE_STATUS_OK) {
        secret_storage_zeroize(blob, max_blob);
        secret_storage_zeroize(nonce, sizeof(nonce));
        secret_storage_zeroize(tag, sizeof(tag));
        heap_caps_free(blob);
        secret_storage_aead_free(aead);
        return key_status;
    }
    const device_status_t open_status = aead_open(
        aead, nonce, &blob[SECRET_STORAGE_HEADER_LEN], length_field, tag,
        (uint8_t *)out_value);
    secret_storage_zeroize(derived_key, sizeof(derived_key));
    secret_storage_zeroize(blob, max_blob);
    secret_storage_zeroize(nonce, sizeof(nonce));
    secret_storage_zeroize(tag, sizeof(tag));
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
#if defined(MACLAW_HOST_BUILD)
    host_hmac_sha256(seed, 16, kLabel, sizeof(kLabel) - 1u, NULL, 0, out_key);
    return DEVICE_STATUS_OK;
#else
    if (psa_crypto_init() != PSA_SUCCESS) return DEVICE_STATUS_UNAVAILABLE;
    psa_key_attributes_t attributes = PSA_KEY_ATTRIBUTES_INIT;
    psa_set_key_type(&attributes, PSA_KEY_TYPE_HMAC);
    psa_set_key_bits(&attributes, 128);
    psa_set_key_usage_flags(&attributes, PSA_KEY_USAGE_SIGN_MESSAGE);
    psa_set_key_algorithm(&attributes, PSA_ALG_HMAC(PSA_ALG_SHA_256));
    psa_key_id_t hmac_key = 0;
    psa_status_t status = psa_import_key(&attributes, seed, 16, &hmac_key);
    psa_reset_key_attributes(&attributes);
    if (status != PSA_SUCCESS) return DEVICE_STATUS_INTERNAL_ERROR;
    size_t mac_len = 0;
    status = psa_mac_compute(hmac_key, PSA_ALG_HMAC(PSA_ALG_SHA_256), kLabel,
                             sizeof(kLabel) - 1u, out_key, 32, &mac_len);
    /* psa_destroy_key() also scrubs the imported seed copy in the slot. */
    const psa_status_t destroy_status = psa_destroy_key(hmac_key);
    if (status != PSA_SUCCESS || destroy_status != PSA_SUCCESS ||
        mac_len != 32) {
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    return DEVICE_STATUS_OK;
#endif
}
