#include <stdio.h>
#include <string.h>

#include "configuration_transaction.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static configuration_snapshot_t snapshot(const char *ssid, const char *token) {
    configuration_snapshot_t value = {0};
    snprintf(value.wifi_ssid, sizeof(value.wifi_ssid), "%s", ssid);
    snprintf(value.gateway_token, sizeof(value.gateway_token), "%s", token);
    snprintf(value.wifi_security, sizeof(value.wifi_security), "personal");
    snprintf(value.wifi_eap_method, sizeof(value.wifi_eap_method), "peap");
    snprintf(value.wifi_ttls_phase2, sizeof(value.wifi_ttls_phase2), "mschapv2");
    snprintf(value.wifi_ca_mode, sizeof(value.wifi_ca_mode), "system");
    return value;
}

static configuration_provisioning_request_t provisioning_request(
    const char *ssid, const char *security) {
    configuration_provisioning_request_t request = {0};
    snprintf(request.ssid, sizeof(request.ssid), "%s", ssid);
    snprintf(request.password, sizeof(request.password), "candidate-password");
    snprintf(request.gateway, sizeof(request.gateway), "https://hub.example");
    snprintf(request.code, sizeof(request.code), "123456");
    snprintf(request.security, sizeof(request.security), "%s", security);
    snprintf(request.eap_method, sizeof(request.eap_method), "peap");
    snprintf(request.ttls_phase2, sizeof(request.ttls_phase2), "mschapv2");
    snprintf(request.ca_mode, sizeof(request.ca_mode), "system");
    return request;
}

int main(void) {
    CHECK(configuration_transaction_valid_pairing_code("123456"));
    CHECK(!configuration_transaction_valid_pairing_code("12345"));
    CHECK(!configuration_transaction_valid_pairing_code("12345x"));

    configuration_provisioning_transaction_t transaction = {
        .confirmed_snapshot = snapshot("confirmed-wifi", "old-token"),
    };
    bool staged = true;
    const configuration_snapshot_t *boot =
        configuration_transaction_boot_snapshot(&transaction, &staged);
    CHECK(boot != NULL && !staged);
    CHECK(!strcmp(boot->wifi_ssid, "confirmed-wifi"));
    CHECK(!strcmp(boot->gateway_token, "old-token"));

    /* The transaction intentionally has no public arbitrary-snapshot stage or
     * generic confirm operation. Every candidate must originate from the
     * validated request/baseline transition and Gateway confirmation must
     * carry a bounded Hub token. */
    configuration_provisioning_request_t candidate_request =
        provisioning_request("candidate-wifi", "personal");
    CHECK(configuration_transaction_stage_provisioning_request(
        &transaction, &candidate_request));
    CHECK(transaction.staged);
    CHECK(transaction.staged_boot_attempts == 0);
    CHECK(!strcmp(transaction.confirmed_snapshot.wifi_ssid, "confirmed-wifi"));
    CHECK(!strcmp(transaction.confirmed_snapshot.gateway_token, "old-token"));
    boot = configuration_transaction_boot_snapshot(&transaction, &staged);
    CHECK(staged && boot == &transaction.staged_snapshot);
    CHECK(!strcmp(boot->wifi_ssid, "candidate-wifi"));

    /* A power loss must not grant an unconfirmed candidate an endless new
     * retry window. The configuration owner persists each accepted boot. */
    CHECK(configuration_transaction_begin_staged_boot(&transaction));
    CHECK(transaction.staged_boot_attempts == 1);
    CHECK(configuration_transaction_begin_staged_boot(&transaction));
    CHECK(transaction.staged_boot_attempts == 2);
    CHECK(configuration_transaction_begin_staged_boot(&transaction));
    CHECK(transaction.staged_boot_attempts == 3);
    CHECK(!configuration_transaction_begin_staged_boot(&transaction));
    CHECK(!transaction.staged);
    CHECK(transaction.staged_boot_attempts == 0);
    CHECK(!strcmp(transaction.confirmed_snapshot.wifi_ssid, "confirmed-wifi"));

    CHECK(configuration_transaction_stage_provisioning_request(
        &transaction, &candidate_request));

    /* Failed Wi-Fi or an authoritative Hub pair-code rejection must restore
     * exactly the old owner/network, not mutate it through candidate fields. */
    configuration_transaction_rollback(&transaction);
    CHECK(!transaction.staged);
    boot = configuration_transaction_boot_snapshot(&transaction, &staged);
    CHECK(!staged && boot == &transaction.confirmed_snapshot);
    CHECK(!strcmp(boot->wifi_ssid, "confirmed-wifi"));
    CHECK(!strcmp(boot->gateway_token, "old-token"));
    CHECK(transaction.staged_snapshot.wifi_ssid[0] == '\0');
    CHECK(transaction.staged_boot_attempts == 0);
    configuration_transaction_rollback(&transaction);
    CHECK(!transaction.staged);

    CHECK(configuration_transaction_stage_provisioning_request(
        &transaction, &candidate_request));
    CHECK(!configuration_transaction_commit_gateway_pairing_token(&transaction, ""));
    CHECK(transaction.staged);
    CHECK(configuration_transaction_commit_gateway_pairing_token(&transaction, "new-token"));
    CHECK(!transaction.staged);
    CHECK(!strcmp(transaction.confirmed_snapshot.wifi_ssid, "candidate-wifi"));
    CHECK(!strcmp(transaction.confirmed_snapshot.gateway_token, "new-token"));
    CHECK(transaction.confirmed_snapshot.pair_code[0] == '\0');
    CHECK(transaction.staged_snapshot.wifi_ssid[0] == '\0');
    CHECK(transaction.staged_boot_attempts == 0);

    /* Request validation and candidate derivation are a pure transaction
     * operation: successful staging preserves confirmed policy fields that
     * are not represented in portal/BLE/USB input. */
    transaction.confirmed_snapshot.output_volume = 37;
    transaction.confirmed_snapshot.output_volume_saved = true;
    transaction.confirmed_snapshot.display_brightness = 42;
    transaction.confirmed_snapshot.display_brightness_saved = true;
    transaction.confirmed_snapshot.screen_sleep_seconds = 300;
    transaction.confirmed_snapshot.screen_sleep_seconds_saved = true;
    transaction.confirmed_snapshot.cellular_transport_selected = true;
    transaction.confirmed_snapshot.cellular_transport_selection_saved = true;
    snprintf(transaction.confirmed_snapshot.wifi_networks[0].ssid,
             sizeof(transaction.confirmed_snapshot.wifi_networks[0].ssid), "older-wifi");
    snprintf(transaction.confirmed_snapshot.wifi_networks[0].password,
             sizeof(transaction.confirmed_snapshot.wifi_networks[0].password), "older-password");
    transaction.confirmed_snapshot.wifi_network_count = 1;
    configuration_provisioning_request_t request =
        provisioning_request("candidate-wifi", "personal");
    CHECK(sizeof(request.ssid) == CONFIGURATION_WIFI_VALUE_CAPACITY);
    CHECK(sizeof(request.gateway) == CONFIGURATION_GATEWAY_URL_CAPACITY);
    CHECK(sizeof(request.code) == CONFIGURATION_PAIR_CODE_CAPACITY);
    CHECK(!strcmp(request.security, "personal"));
    CHECK(configuration_transaction_stage_provisioning_request(&transaction, &request));
    CHECK(transaction.staged);
    CHECK(!strcmp(transaction.staged_snapshot.wifi_ssid, "candidate-wifi"));
    CHECK(!strcmp(transaction.staged_snapshot.gateway_url, "https://hub.example"));
    CHECK(transaction.staged_snapshot.gateway_token[0] == '\0');
    CHECK(transaction.staged_snapshot.output_volume == 37);
    CHECK(transaction.staged_snapshot.output_volume_saved);
    CHECK(transaction.staged_snapshot.display_brightness == 42);
    CHECK(transaction.staged_snapshot.display_brightness_saved);
    CHECK(transaction.staged_snapshot.screen_sleep_seconds == 300);
    CHECK(transaction.staged_snapshot.screen_sleep_seconds_saved);
    CHECK(transaction.staged_snapshot.cellular_transport_selected);
    CHECK(transaction.staged_snapshot.cellular_transport_selection_saved);
    CHECK(transaction.staged_snapshot.wifi_network_count == 2);
    CHECK(!strcmp(transaction.staged_snapshot.wifi_networks[0].ssid, "older-wifi"));
    CHECK(!strcmp(transaction.staged_snapshot.wifi_networks[1].ssid, "candidate-wifi"));

    /* Gateway origins are HTTPS-only and reject ambiguous authority input. */
    configuration_snapshot_t before_gateway_reject = transaction.staged_snapshot;
    snprintf(request.gateway, sizeof(request.gateway), "http://hub.example");
    CHECK(!configuration_transaction_stage_provisioning_request(&transaction, &request));
    snprintf(request.gateway, sizeof(request.gateway), "https://hub.example?x=1");
    CHECK(!configuration_transaction_stage_provisioning_request(&transaction, &request));
    CHECK(!memcmp(&before_gateway_reject, &transaction.staged_snapshot,
                  sizeof(before_gateway_reject)));
    snprintf(request.gateway, sizeof(request.gateway), "https://hub.example");

    /* A normal policy write may arrive while the candidate is waiting for Hub
     * confirmation (for example remote volume or uplink selection).  It must
     * survive the later candidate promotion without replacing candidate-owned
     * Wi-Fi/Hub/pair-code evidence. */
    configuration_snapshot_t revised_policy = transaction.confirmed_snapshot;
    revised_policy.output_volume = 62;
    revised_policy.output_volume_saved = true;
    revised_policy.display_brightness = 55;
    revised_policy.display_brightness_saved = true;
    revised_policy.screen_sleep_seconds = 600;
    revised_policy.screen_sleep_seconds_saved = true;
    revised_policy.cellular_transport_selected = false;
    revised_policy.cellular_transport_selection_saved = true;
    snprintf(revised_policy.wifi_networks[0].ssid,
             sizeof(revised_policy.wifi_networks[0].ssid), "policy-wifi");
    snprintf(revised_policy.wifi_networks[0].password,
             sizeof(revised_policy.wifi_networks[0].password), "policy-password");
    revised_policy.wifi_network_count = 1;
    CHECK(configuration_transaction_apply_confirmed_policy(&transaction, &revised_policy));
    CHECK(transaction.staged);
    CHECK(!strcmp(transaction.staged_snapshot.wifi_ssid, "candidate-wifi"));
    CHECK(!strcmp(transaction.staged_snapshot.gateway_url, "https://hub.example"));
    CHECK(!strcmp(transaction.staged_snapshot.pair_code, "123456"));
    CHECK(transaction.staged_snapshot.gateway_token[0] == '\0');
    CHECK(transaction.staged_snapshot.output_volume == 62);
    CHECK(transaction.staged_snapshot.display_brightness == 55);
    CHECK(transaction.staged_snapshot.display_brightness_saved);
    CHECK(transaction.staged_snapshot.screen_sleep_seconds == 600);
    CHECK(transaction.staged_snapshot.screen_sleep_seconds_saved);
    CHECK(!transaction.staged_snapshot.cellular_transport_selected);
    CHECK(transaction.staged_snapshot.wifi_network_count == 2);
    CHECK(!strcmp(transaction.staged_snapshot.wifi_networks[0].ssid, "policy-wifi"));
    CHECK(!strcmp(transaction.staged_snapshot.wifi_networks[1].ssid, "candidate-wifi"));
    CHECK(configuration_transaction_commit_gateway_pairing_token(&transaction, "policy-token"));
    CHECK(!transaction.staged);
    CHECK(transaction.confirmed_snapshot.output_volume == 62);
    CHECK(transaction.confirmed_snapshot.display_brightness == 55);
    CHECK(transaction.confirmed_snapshot.display_brightness_saved);
    CHECK(transaction.confirmed_snapshot.screen_sleep_seconds == 600);
    CHECK(transaction.confirmed_snapshot.screen_sleep_seconds_saved);
    CHECK(!transaction.confirmed_snapshot.cellular_transport_selected);
    CHECK(transaction.confirmed_snapshot.wifi_network_count == 2);
    CHECK(!strcmp(transaction.confirmed_snapshot.wifi_networks[0].ssid, "policy-wifi"));
    CHECK(!strcmp(transaction.confirmed_snapshot.wifi_networks[1].ssid, "candidate-wifi"));

    /* The policy transition fails closed and byte-for-byte unchanged when a
     * future caller hands it malformed data. */
    configuration_provisioning_transaction_t before_bad_policy = transaction;
    configuration_snapshot_t malformed_policy = transaction.confirmed_snapshot;
    memset(malformed_policy.gateway_url, 'x', sizeof(malformed_policy.gateway_url));
    CHECK(!configuration_transaction_apply_confirmed_policy(&transaction, &malformed_policy));
    CHECK(!memcmp(&before_bad_policy, &transaction, sizeof(transaction)));

    /* Reuse-network and voice pairing legitimately bind a token without a
     * new candidate: confirmed network policy remains untouched. */
    configuration_transaction_rollback(&transaction);
    configuration_snapshot_t before_reuse_network = transaction.confirmed_snapshot;
    snprintf(transaction.confirmed_snapshot.pair_code,
             sizeof(transaction.confirmed_snapshot.pair_code), "654321");
    CHECK(configuration_transaction_commit_gateway_pairing_token(&transaction, "another-token"));
    CHECK(!strcmp(transaction.confirmed_snapshot.wifi_ssid,
                  before_reuse_network.wifi_ssid));
    CHECK(!strcmp(transaction.confirmed_snapshot.gateway_token, "another-token"));
    CHECK(transaction.confirmed_snapshot.pair_code[0] == '\0');

    /* A future BLE/USB caller can invoke the value model without first
     * loading NVS.  Its confirmed baseline must therefore be validated here,
     * and a corrupt catalogue must neither stage a candidate nor dereference
     * unterminated data. */
    configuration_provisioning_transaction_t corrupt_baseline = transaction;
    corrupt_baseline.confirmed_snapshot.wifi_network_count = 1;
    memset(corrupt_baseline.confirmed_snapshot.wifi_networks[0].ssid, 'x',
           sizeof(corrupt_baseline.confirmed_snapshot.wifi_networks[0].ssid));
    configuration_provisioning_transaction_t before_corrupt = corrupt_baseline;
    CHECK(!configuration_transaction_stage_provisioning_request(
        &corrupt_baseline, &request));
    CHECK(!memcmp(&before_corrupt, &corrupt_baseline, sizeof(before_corrupt)));

    /* Invalid input must leave the prior candidate untouched. */
    configuration_snapshot_t before_reject = transaction.staged_snapshot;
    request.code[0] = 'x';
    CHECK(!configuration_transaction_stage_provisioning_request(&transaction, &request));
    CHECK(!memcmp(&before_reject, &transaction.staged_snapshot,
                  sizeof(before_reject)));

    request = provisioning_request("enterprise-wifi", "enterprise");
    snprintf(request.eap_method, sizeof(request.eap_method), "ttls");
    snprintf(request.identity, sizeof(request.identity), "identity");
    snprintf(request.username, sizeof(request.username), "user");
    snprintf(request.ttls_phase2, sizeof(request.ttls_phase2), "pap");
    snprintf(request.ca_mode, sizeof(request.ca_mode), "none");
    snprintf(request.server_domain, sizeof(request.server_domain), "radius.example");
    configuration_snapshot_t before_enterprise_reject = transaction.staged_snapshot;
    CHECK(!configuration_transaction_stage_provisioning_request(&transaction, &request));
    CHECK(!memcmp(&before_enterprise_reject, &transaction.staged_snapshot,
                  sizeof(before_enterprise_reject)));
    snprintf(request.ca_mode, sizeof(request.ca_mode), "system");
    CHECK(configuration_transaction_stage_provisioning_request(&transaction, &request));
    CHECK(!strcmp(transaction.staged_snapshot.wifi_security, "enterprise"));
    CHECK(transaction.staged_snapshot.wifi_network_count == 2);
    CHECK(!strcmp(transaction.staged_snapshot.wifi_networks[0].ssid, "policy-wifi"));
    CHECK(!strcmp(transaction.staged_snapshot.wifi_networks[1].ssid, "candidate-wifi"));
    puts("PASS configuration provisioning transaction");
    return 0;
}
