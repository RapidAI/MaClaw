#include "configuration_transaction.h"

#include <string.h>

static bool bounded_string(const char *value, size_t capacity) {
    return value && memchr(value, '\0', capacity) != NULL;
}

/* This module is shared by every future provisioning surface.  Do not rely on
 * Configuration Service's durable-store validation to make its value-only
 * candidate derivation safe: a malformed confirmed catalogue must fail closed
 * before it reaches strcmp() or memmove(). */
static bool valid_confirmed_snapshot(const configuration_snapshot_t *snapshot) {
    if (!snapshot || snapshot->output_volume > 100u ||
        snapshot->display_brightness > 100u ||
        snapshot->wifi_network_count > CONFIGURATION_WIFI_NETWORK_CAPACITY ||
        !bounded_string(snapshot->wifi_ssid, sizeof(snapshot->wifi_ssid)) ||
        !bounded_string(snapshot->wifi_password, sizeof(snapshot->wifi_password)) ||
        !bounded_string(snapshot->wifi_security, sizeof(snapshot->wifi_security)) ||
        !bounded_string(snapshot->wifi_eap_method, sizeof(snapshot->wifi_eap_method)) ||
        !bounded_string(snapshot->wifi_identity, sizeof(snapshot->wifi_identity)) ||
        !bounded_string(snapshot->wifi_username, sizeof(snapshot->wifi_username)) ||
        !bounded_string(snapshot->wifi_ttls_phase2, sizeof(snapshot->wifi_ttls_phase2)) ||
        !bounded_string(snapshot->wifi_ca_mode, sizeof(snapshot->wifi_ca_mode)) ||
        !bounded_string(snapshot->wifi_server_domain, sizeof(snapshot->wifi_server_domain)) ||
        !bounded_string(snapshot->gateway_url, sizeof(snapshot->gateway_url)) ||
        !bounded_string(snapshot->pair_code, sizeof(snapshot->pair_code)) ||
        !bounded_string(snapshot->gateway_token, sizeof(snapshot->gateway_token))) {
        return false;
    }
    for (uint8_t i = 0; i < snapshot->wifi_network_count; ++i) {
        const configuration_wifi_network_t *network = &snapshot->wifi_networks[i];
        if (!network->ssid[0] ||
            !bounded_string(network->ssid, sizeof(network->ssid)) ||
            !bounded_string(network->password, sizeof(network->password))) {
            return false;
        }
        for (uint8_t j = 0; j < i; ++j) {
            if (!strcmp(network->ssid, snapshot->wifi_networks[j].ssid)) return false;
        }
    }
    return true;
}

static bool valid_provisioning_request(const configuration_provisioning_request_t *request) {
    if (!request ||
        !bounded_string(request->ssid, sizeof(request->ssid)) ||
        !bounded_string(request->password, sizeof(request->password)) ||
        !bounded_string(request->gateway, sizeof(request->gateway)) ||
        !bounded_string(request->code, sizeof(request->code)) ||
        !bounded_string(request->security, sizeof(request->security)) ||
        !bounded_string(request->eap_method, sizeof(request->eap_method)) ||
        !bounded_string(request->identity, sizeof(request->identity)) ||
        !bounded_string(request->username, sizeof(request->username)) ||
        !bounded_string(request->ttls_phase2, sizeof(request->ttls_phase2)) ||
        !bounded_string(request->ca_mode, sizeof(request->ca_mode)) ||
        !bounded_string(request->server_domain, sizeof(request->server_domain)) ||
        !request->ssid[0] || strlen(request->ssid) > 32u ||
        strlen(request->password) >= sizeof(request->password) ||
        !request->gateway[0] || strlen(request->gateway) >= sizeof(request->gateway) ||
        !configuration_transaction_valid_pairing_code(request->code) ||
        (strcmp(request->security, "personal") && strcmp(request->security, "enterprise"))) {
        return false;
    }
    const char *gateway_host = NULL;
    if (!strncmp(request->gateway, "https://", 8u)) gateway_host = request->gateway + 8u;
    else if (!strncmp(request->gateway, "http://", 7u)) gateway_host = request->gateway + 7u;
    if (!gateway_host || !gateway_host[0] || gateway_host[0] == '/' ||
        strchr(gateway_host, ' ')) {
        return false;
    }
    if (!strcmp(request->security, "enterprise")) {
        return (!strcmp(request->eap_method, "peap") ||
                !strcmp(request->eap_method, "ttls")) &&
               request->username[0] &&
               strlen(request->username) < sizeof(request->username) &&
               strlen(request->identity) < sizeof(request->identity) &&
               (!strcmp(request->ttls_phase2, "mschapv2") ||
                !strcmp(request->ttls_phase2, "pap")) &&
               (!strcmp(request->ca_mode, "system") ||
                !strcmp(request->ca_mode, "none")) &&
               strlen(request->server_domain) < sizeof(request->server_domain);
    }
    return true;
}

bool configuration_transaction_valid_pairing_code(const char *code) {
    if (!code || !bounded_string(code, CONFIGURATION_PAIR_CODE_CAPACITY) ||
        strlen(code) != CONFIGURATION_PAIR_CODE_CAPACITY - 1u) {
        return false;
    }
    for (const char *digit = code; *digit; ++digit) {
        if (*digit < '0' || *digit > '9') return false;
    }
    return true;
}

bool configuration_transaction_stage_provisioning_request(
    configuration_provisioning_transaction_t *transaction,
    const configuration_provisioning_request_t *request) {
    if (!transaction || !valid_confirmed_snapshot(&transaction->confirmed_snapshot) ||
        !valid_provisioning_request(request)) {
        return false;
    }
    configuration_snapshot_t candidate = transaction->confirmed_snapshot;
    const bool enterprise = !strcmp(request->security, "enterprise");
    strcpy(candidate.wifi_ssid, request->ssid);
    strcpy(candidate.wifi_password, request->password);
    strcpy(candidate.wifi_security, request->security);
    strcpy(candidate.wifi_eap_method, enterprise ? request->eap_method : "peap");
    strcpy(candidate.wifi_identity, enterprise ? request->identity : "");
    strcpy(candidate.wifi_username, enterprise ? request->username : "");
    strcpy(candidate.wifi_ttls_phase2,
           enterprise ? request->ttls_phase2 : "mschapv2");
    strcpy(candidate.wifi_ca_mode, enterprise ? request->ca_mode : "system");
    strcpy(candidate.wifi_server_domain, enterprise ? request->server_domain : "");
    strcpy(candidate.gateway_url, request->gateway);
    strcpy(candidate.pair_code, request->code);
    candidate.gateway_token[0] = '\0';
    if (!enterprise) {
        uint8_t slot = candidate.wifi_network_count;
        for (uint8_t i = 0; i < candidate.wifi_network_count; ++i) {
            if (!strcmp(candidate.wifi_networks[i].ssid, candidate.wifi_ssid)) {
                slot = i;
                break;
            }
        }
        if (slot == candidate.wifi_network_count) {
            if (slot >= CONFIGURATION_WIFI_NETWORK_CAPACITY) {
                memmove(&candidate.wifi_networks[0], &candidate.wifi_networks[1],
                        (CONFIGURATION_WIFI_NETWORK_CAPACITY - 1u) *
                            sizeof(candidate.wifi_networks[0]));
                slot = CONFIGURATION_WIFI_NETWORK_CAPACITY - 1u;
            } else {
                candidate.wifi_network_count = slot + 1u;
            }
        }
        strcpy(candidate.wifi_networks[slot].ssid, candidate.wifi_ssid);
        strcpy(candidate.wifi_networks[slot].password, candidate.wifi_password);
    }
    transaction->staged_snapshot = candidate;
    transaction->staged = true;
    transaction->staged_boot_attempts = 0;
    return true;
}

static void upsert_personal_network(configuration_snapshot_t *snapshot,
                                    const char *ssid, const char *password) {
    if (!snapshot || !ssid || !ssid[0]) return;
    uint8_t slot = snapshot->wifi_network_count;
    for (uint8_t i = 0; i < snapshot->wifi_network_count; ++i) {
        if (!strcmp(snapshot->wifi_networks[i].ssid, ssid)) {
            slot = i;
            break;
        }
    }
    if (slot == snapshot->wifi_network_count) {
        if (slot >= CONFIGURATION_WIFI_NETWORK_CAPACITY) {
            memmove(&snapshot->wifi_networks[0], &snapshot->wifi_networks[1],
                    (CONFIGURATION_WIFI_NETWORK_CAPACITY - 1u) *
                        sizeof(snapshot->wifi_networks[0]));
            slot = CONFIGURATION_WIFI_NETWORK_CAPACITY - 1u;
        } else {
            snapshot->wifi_network_count = slot + 1u;
        }
    }
    strcpy(snapshot->wifi_networks[slot].ssid, ssid);
    strcpy(snapshot->wifi_networks[slot].password, password ? password : "");
}

bool configuration_transaction_apply_confirmed_policy(
    configuration_provisioning_transaction_t *transaction,
    const configuration_snapshot_t *confirmed_policy) {
    if (!transaction || !confirmed_policy ||
        !valid_confirmed_snapshot(&transaction->confirmed_snapshot) ||
        !valid_confirmed_snapshot(confirmed_policy) ||
        (transaction->staged &&
         !valid_confirmed_snapshot(&transaction->staged_snapshot))) {
        return false;
    }

    /* Work from a copy so a validation or catalogue reconciliation failure can
     * never leave a half-updated durable transaction behind. */
    configuration_provisioning_transaction_t updated = *transaction;
    updated.confirmed_snapshot = *confirmed_policy;
    if (updated.staged) {
        configuration_snapshot_t *candidate = &updated.staged_snapshot;
        candidate->output_volume = confirmed_policy->output_volume;
        candidate->output_volume_saved = confirmed_policy->output_volume_saved;
        candidate->display_brightness = confirmed_policy->display_brightness;
        candidate->display_brightness_saved = confirmed_policy->display_brightness_saved;
        candidate->screen_sleep_seconds = confirmed_policy->screen_sleep_seconds;
        candidate->screen_sleep_seconds_saved =
            confirmed_policy->screen_sleep_seconds_saved;
        candidate->cellular_transport_selected =
            confirmed_policy->cellular_transport_selected;
        candidate->cellular_transport_selection_saved =
            confirmed_policy->cellular_transport_selection_saved;

        /* Saved personal networks are device policy, whereas the candidate's
         * primary network is still unconfirmed provisioning evidence.  Rebase
         * the catalogue on the new confirmed policy, then retain that primary
         * candidate entry using the same bounded FIFO rule as STAGE. */
        memcpy(candidate->wifi_networks, confirmed_policy->wifi_networks,
               sizeof(candidate->wifi_networks));
        candidate->wifi_network_count = confirmed_policy->wifi_network_count;
        if (candidate->wifi_ssid[0] &&
            strcmp(candidate->wifi_security, "enterprise")) {
            upsert_personal_network(candidate, candidate->wifi_ssid,
                                    candidate->wifi_password);
        }
        if (!valid_confirmed_snapshot(candidate)) return false;
    }
    *transaction = updated;
    return true;
}

const configuration_snapshot_t *configuration_transaction_boot_snapshot(
    const configuration_provisioning_transaction_t *transaction, bool *out_staged) {
    if (out_staged) *out_staged = false;
    if (!transaction) return NULL;
    if (transaction->staged) {
        if (out_staged) *out_staged = true;
        return &transaction->staged_snapshot;
    }
    return &transaction->confirmed_snapshot;
}

bool configuration_transaction_begin_staged_boot(
    configuration_provisioning_transaction_t *transaction) {
    if (!transaction || !transaction->staged) return false;
    if (transaction->staged_boot_attempts >=
        CONFIGURATION_TRANSACTION_MAX_STAGED_BOOT_ATTEMPTS) {
        configuration_transaction_rollback(transaction);
        return false;
    }
    ++transaction->staged_boot_attempts;
    return true;
}

bool configuration_transaction_commit_gateway_pairing_token(
    configuration_provisioning_transaction_t *transaction, const char *token) {
    if (!transaction || !token || !token[0] ||
        !bounded_string(token, CONFIGURATION_GATEWAY_TOKEN_CAPACITY) ||
        strlen(token) >= CONFIGURATION_GATEWAY_TOKEN_CAPACITY ||
        !valid_confirmed_snapshot(&transaction->confirmed_snapshot) ||
        (transaction->staged &&
         !valid_confirmed_snapshot(&transaction->staged_snapshot))) {
        return false;
    }

    configuration_snapshot_t confirmed = transaction->staged
                                            ? transaction->staged_snapshot
                                            : transaction->confirmed_snapshot;
    strcpy(confirmed.gateway_token, token);
    confirmed.pair_code[0] = '\0';
    transaction->confirmed_snapshot = confirmed;
    transaction->staged = false;
    transaction->staged_boot_attempts = 0;
    memset(&transaction->staged_snapshot, 0, sizeof(transaction->staged_snapshot));
    return true;
}

void configuration_transaction_rollback(configuration_provisioning_transaction_t *transaction) {
    if (!transaction) return;
    transaction->staged = false;
    transaction->staged_boot_attempts = 0;
    memset(&transaction->staged_snapshot, 0, sizeof(transaction->staged_snapshot));
}
