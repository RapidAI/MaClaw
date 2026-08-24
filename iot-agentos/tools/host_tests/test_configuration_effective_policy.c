#include <stdio.h>
#include <string.h>

#include "configuration_effective_policy.h"
#include "configuration_runtime_override_store.h"

#define CHECK(expr) do { \
    if (!(expr)) { \
        fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__, #expr); \
        return 1; \
    } \
} while (0)

static configuration_snapshot_t durable_snapshot(void) {
    configuration_snapshot_t snapshot = {0};
    snapshot.output_volume = 35u;
    snapshot.output_volume_saved = true;
    snapshot.display_brightness = 40u;
    snapshot.display_brightness_saved = true;
    snapshot.screen_sleep_seconds = 300u;
    snapshot.screen_sleep_seconds_saved = true;
    snapshot.cellular_transport_selected = false;
    snapshot.cellular_transport_selection_saved = true;
    return snapshot;
}

static configuration_runtime_override_t override(
    configuration_runtime_override_value_kind_t kind, uint32_t value, uint64_t expires_at) {
    return (configuration_runtime_override_t){
        .struct_size = sizeof(configuration_runtime_override_t),
        .abi_version = CONFIGURATION_RUNTIME_OVERRIDE_ABI_VERSION,
        .kind = kind,
        .value_u32 = value,
        .expires_at_monotonic_ms = expires_at,
        .provenance = {
            .struct_size = sizeof(configuration_policy_request_t),
            .abi_version = CONFIGURATION_POLICY_REQUEST_ABI_VERSION,
            .source = CONFIGURATION_SOURCE_RUNTIME_OVERRIDE,
            .authenticated = true,
            .ttl_ms = 0u,
        },
    };
}

int main(void) {
    const configuration_snapshot_t durable = durable_snapshot();
    configuration_snapshot_t effective = {0};
    bool active = true;
    CHECK(configuration_effective_policy_resolve(&durable, NULL, 100u, &effective, &active));
    CHECK(!active);
    CHECK(!memcmp(&durable, &effective, sizeof(durable)));

    configuration_runtime_override_t volume = override(
        CONFIGURATION_RUNTIME_OVERRIDE_VALUE_OUTPUT_VOLUME, 72u, 1100u);
    CHECK(configuration_effective_policy_resolve(&durable, &volume, 100u, &effective, &active));
    CHECK(active && effective.output_volume == 72u && effective.output_volume_saved);
    CHECK(durable.output_volume == 35u);

    configuration_runtime_override_t brightness = override(
        CONFIGURATION_RUNTIME_OVERRIDE_VALUE_DISPLAY_BRIGHTNESS, 0u, 1100u);
    CHECK(configuration_effective_policy_resolve(&durable, &brightness, 100u, &effective, &active));
    CHECK(active && effective.display_brightness == 0u && effective.display_brightness_saved);

    configuration_runtime_override_t sleep = override(
        CONFIGURATION_RUNTIME_OVERRIDE_VALUE_SCREEN_SLEEP_SECONDS, 600u, 1100u);
    CHECK(configuration_effective_policy_resolve(&durable, &sleep, 100u, &effective, &active));
    CHECK(active && effective.screen_sleep_seconds == 600u && effective.screen_sleep_seconds_saved);

    configuration_runtime_override_t transport = override(
        CONFIGURATION_RUNTIME_OVERRIDE_VALUE_TRANSPORT_SELECTION, 1u, 1100u);
    CHECK(configuration_effective_policy_resolve(&durable, &transport, 100u, &effective, &active));
    CHECK(active && effective.cellular_transport_selected && effective.cellular_transport_selection_saved);

    CHECK(!configuration_effective_policy_resolve(&durable, &volume, 1100u, &effective, &active));
    volume.expires_at_monotonic_ms = 16u * 60u * 1000u + 100u;
    CHECK(!configuration_effective_policy_resolve(&durable, &volume, 100u, &effective, &active));
    volume.expires_at_monotonic_ms = 1100u;
    volume.value_u32 = 101u;
    CHECK(!configuration_effective_policy_resolve(&durable, &volume, 100u, &effective, &active));
    volume.value_u32 = 72u;
    volume.abi_version = 0u;
    CHECK(!configuration_effective_policy_resolve(&durable, &volume, 100u, &effective, &active));

    configuration_runtime_override_store_t store = {0};
    configuration_runtime_override_store_init(&store);
    CHECK(store.effective_revision == 1u);
    volume = override(CONFIGURATION_RUNTIME_OVERRIDE_VALUE_OUTPUT_VOLUME, 72u, 1100u);
    brightness = override(CONFIGURATION_RUNTIME_OVERRIDE_VALUE_DISPLAY_BRIGHTNESS, 10u, 1100u);
    CHECK(configuration_runtime_override_store_put(&store, &volume, 100u) ==
          CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK);
    const uint64_t after_volume = store.effective_revision;
    CHECK(configuration_runtime_override_store_put(&store, &brightness, 100u) ==
          CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK);
    CHECK(store.effective_revision > after_volume);
    uint64_t effective_revision = 0u;
    uint32_t active_mask = 0u;
    CHECK(configuration_runtime_override_store_resolve(&store, &durable, 100u, &effective,
                                                       &effective_revision, &active_mask) ==
          CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK);
    CHECK(effective.output_volume == 72u && effective.display_brightness == 10u);
    CHECK(active_mask == 0x3u && effective_revision == store.effective_revision);
    uint64_t earliest_expiry = 0u;
    CHECK(configuration_runtime_override_store_next_expiry(&store, &earliest_expiry) ==
          CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK);
    CHECK(earliest_expiry == 1100u);
    const configuration_snapshot_t durable_before_overrides = durable_snapshot();
    CHECK(!memcmp(&durable, &durable_before_overrides, sizeof(durable)));

    /* Expiry is removed from the single owner once, then all consumers can
     * reconcile one new effective revision rather than expiring independently. */
    CHECK(configuration_runtime_override_store_resolve(&store, &durable, 1100u, &effective,
                                                       &effective_revision, &active_mask) ==
          CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK);
    CHECK(active_mask == 0u && effective.output_volume == durable.output_volume &&
          effective.display_brightness == durable.display_brightness);
    CHECK(effective_revision == store.effective_revision &&
          effective_revision > after_volume);
    CHECK(configuration_runtime_override_store_next_expiry(&store, &earliest_expiry) ==
          CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK);
    CHECK(earliest_expiry == 0u);

    volume = override(CONFIGURATION_RUNTIME_OVERRIDE_VALUE_OUTPUT_VOLUME, 60u, 1200u);
    volume.provenance.authenticated = false;
    CHECK(configuration_runtime_override_store_put(&store, &volume, 1100u) ==
          CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT);
    volume.provenance.authenticated = true;
    volume.provenance.ttl_ms = 1u;
    CHECK(configuration_runtime_override_store_put(&store, &volume, 1100u) ==
          CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT);
    CHECK(configuration_runtime_override_store_clear_all(&store) ==
          CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK);

    puts("PASS effective configuration overlays only authenticated, bounded runtime policy overrides");
    return 0;
}
