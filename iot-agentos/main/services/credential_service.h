#pragma once

/* Minimal value-only credential lifecycle authority.  Secret bytes never
 * leave this service except through an explicit bounded copy-out; callers
 * must wipe their destination.  Generation is the revocation fence. */
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "device_api.h"

#define CREDENTIAL_SERVICE_ABI_VERSION 1u
#define CREDENTIAL_SERVICE_MAX_TOKEN 95u
#define CREDENTIAL_SERVICE_IDENTITY_CAPACITY 64u

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    uint32_t generation;
    bool present;
    uint32_t length;
} credential_service_snapshot_t;

/* Optional durable generation-floor bridge.  The callbacks exchange only a
 * monotonic value; NVS handles, namespaces and storage workers remain owned by
 * the composition root.  A missing persisted floor is treated as first boot,
 * while any other read/write failure keeps lifecycle changes fail-closed. */
typedef device_status_t (*credential_service_generation_read_fn)(
    uint64_t *out_floor, void *context);
typedef device_status_t (*credential_service_generation_write_fn)(
    uint64_t floor, void *context);

device_status_t credential_service_init(void);
device_status_t credential_service_set_generation_persistence(
    credential_service_generation_read_fn read_floor,
    credential_service_generation_write_fn write_floor,
    void *context);
device_status_t credential_service_begin_generation(uint32_t *out_generation);
device_status_t credential_service_store_gateway_token(uint32_t generation,
                                                        const char *token);
device_status_t credential_service_bind_identity(uint32_t generation,
                                                  const char *identity);
/* Boot restore/rotation primitive: token and device identity become visible
 * together under one generation lock.  This avoids a transient token-only
 * or identity-only state during configuration snapshot publication. */
device_status_t credential_service_restore_gateway_token(uint32_t generation,
                                                          const char *token,
                                                          const char *identity);
device_status_t credential_service_copy_gateway_token(uint32_t generation,
                                                       char *out,
                                                       size_t capacity,
                                                       size_t *out_length,
                                                       const char *identity);
device_status_t credential_service_revoke_gateway_token(uint32_t generation);
bool credential_service_snapshot(credential_service_snapshot_t *out);
