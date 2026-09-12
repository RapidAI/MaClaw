/*
 * At-rest encryption wrapper for small NVS-resident secrets.
 *
 * Three plaintext keys (wifi_pass / pair_code / gateway_token) used to live as
 * raw NVS strings or as fields inside the larger Configuration V7 blob. Either
 * path leaks credentials to anyone who can read the raw flash image: OTA
 * packages, UART dumps, vendor factory provisioning logs, cloned factory NVS
 * partitions, etc.
 *
 * secret_storage stores these fields as authenticated ciphertext blobs in a
 * dedicated NVS namespace. The blob layout is fixed so the format is
 * self-describing and a future reader can reject records whose header does
 * not match.
 *
 *    offset  field
 *    ------  --------------------------------------------------
 *    0..3    magic   = 'S','C','S','V'
 *    4       version = 0x01
 *    5..7    reserved, must be zero
 *    8..19   96-bit AES-GCM nonce, generated fresh per write
 *    20..21  uint16 little-endian ciphertext length (max 1024)
 *    22..37  128-bit AES-GCM authentication tag
 *    38..    ciphertext bytes
 *
 * The encryption key is supplied by a key provider registered at init time.
 * The reference firmware derives it from the chip's eFuse BLOCK_KEY0 via
 * `esp_hmac_calculate`; host tests inject a deterministic provider so the
 * self-test is portable and reproducible.
 *
 * secret_storage is layered on top of persistence_service_* so it never
 * touches an open NVS handle or a raw mutex, and the same PSRAM-stack
 * forwarding rule that already protects other NVS writes continues to apply.
 *
 * Writes that exceed the maximum payload (SECRET_STORAGE_MAX_PAYLOAD) are
 * rejected with DEVICE_STATUS_INVALID_ARGUMENT before any NVS I/O is issued.
 * Reads return DEVICE_STATUS_NOT_FOUND when the magic/version are wrong so
 * an attacker who flips a byte cannot silently downgrade a record to
 * plaintext.
 */
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "device_api.h"

#define SECRET_STORAGE_MAGIC "SCSV"
#define SECRET_STORAGE_MAGIC_LEN 4u
#define SECRET_STORAGE_VERSION 0x01u
#define SECRET_STORAGE_NONCE_LEN 12u
#define SECRET_STORAGE_TAG_LEN 16u
/* Header = magic(4) + version(1) + reserved(3) + nonce(12) + len(2) + tag(16) */
#define SECRET_STORAGE_HEADER_LEN 38u
/* 1024 bytes of plaintext is well above the largest real secret:
 *  - wifi_pass: 64 bytes per CONFIGURATION_WIFI_VALUE_CAPACITY
 *  - pair_code: 6 bytes per CONFIGURATION_PAIR_CODE_CAPACITY
 *  - gateway_token: 95 bytes per CONFIGURATION_GATEWAY_TOKEN_CAPACITY
 * The cap is generous on purpose so future symmetric keys (e.g. per-tenant
 * encryption keys, device private keys) can reuse this wrapper. */
#define SECRET_STORAGE_MAX_PAYLOAD 1024u

typedef device_status_t (*secret_storage_key_provider_fn)(uint8_t out_key[32],
                                                         void *context);

/* Initialize the encryption backend.  The key provider is required before any
 * read or write can succeed; secret_storage_init rejects calls that would
 * leave the backend in a half-configured state. */
device_status_t secret_storage_init(void);
device_status_t secret_storage_deinit(void);
bool secret_storage_is_initialized(void);

/* Install the encryption key source.  The provider must yield a 32-byte
 * buffer; secret_storage uses the leading 16 bytes (AES-128) for the
 * confidentiality key and the trailing 16 bytes as the AES-GCM initial
 * counter salt, so callers should treat the buffer as two independent
 * 16-byte halves.  The provider is invoked on every read and write so it may
 * rotate the underlying material (for example an HMAC-derived key with a
 * per-write salt).  Returning a non-OK status from the provider fails the
 * in-flight call without ever touching NVS. */
device_status_t secret_storage_set_key_provider(secret_storage_key_provider_fn provider,
                                                void *context);

/* Encrypt `data` (size bytes) and write the resulting authenticated blob
 * under (namespace, key).  Replacing an existing record rewrites the blob
 * atomically through persistence_service_write_blob; a write that fails
 * after the encryption step leaves the previous record on disk intact. */
device_status_t secret_storage_write(const char *name_space, const char *key,
                                     const void *data, size_t size);

/* Read and authenticate a record previously written by secret_storage_write.
 * On success `inout_size` is updated to the recovered plaintext length. */
device_status_t secret_storage_read(const char *name_space, const char *key,
                                    void *out_value, size_t *inout_size);

/* Erase a record.  A missing record returns DEVICE_STATUS_OK so callers can
 * treat erase as idempotent. */
device_status_t secret_storage_erase(const char *name_space, const char *key);

/* Internal AES-GCM primitives exposed for host unit tests.  Production code
 * should not call these directly; they are not stable across versions. */
struct secret_storage_aead_context;
typedef struct secret_storage_aead_context secret_storage_aead_context_t;

/* Allocate a single-shot AEAD context backed by the implementation that
 * shipped on the target.  Returns NULL when the configured backend cannot
 * be initialised (e.g. the host build selected OpenSSL but it is absent). */
secret_storage_aead_context_t *secret_storage_aead_new(void);
void secret_storage_aead_free(secret_storage_aead_context_t *context);

/* Convenience: derive a 32-byte key from a 16-byte input.  The derivation is
 * intentionally simple (HMAC-SHA-256 with a fixed label) so the host build
 * can verify key derivation produces a stable key for the same input. */
device_status_t secret_storage_derive_test_key(const uint8_t seed[16],
                                               uint8_t out_key[32]);