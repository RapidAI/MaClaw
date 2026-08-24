#pragma once

/* Runtime override facade. Kept separate from configuration_service.h so the
 * value model can depend on the durable snapshot type without forming a
 * circular public header dependency. Composition-root policy ingress owns
 * this facade; board adapters and business services do not. */

#include "configuration_effective_policy.h"

device_status_t configuration_service_apply_runtime_override(
    const configuration_runtime_override_t *override);
device_status_t configuration_service_remove_runtime_override(
    configuration_runtime_override_value_kind_t kind);
device_status_t configuration_service_clear_runtime_overrides(void);
/* Returns the earliest retained override expiry in the current boot's
 * monotonic-ms epoch; zero means none. This is scheduler evidence only, never
 * an independently resolved policy value. */
device_status_t configuration_service_next_runtime_override_expiry_ms(
    uint64_t *out_expires_at_monotonic_ms);
/* Resolves all currently live volatile policy over one copied durable
 * revision. Expired values are discarded by this single owner before the
 * effective revision is returned. */
device_status_t configuration_service_load_effective_revisioned_snapshot(
    configuration_effective_revisioned_snapshot_t *out_snapshot);
