#pragma once

/*
 * Provisioning configuration transaction value model.
 *
 * This intentionally contains neither NVS, Wi-Fi, Hub HTTP, FreeRTOS nor
 * board state. Configuration Service makes this record durable; Connectivity
 * and Gateway provide the external evidence that selects confirm or rollback.
 */

#include <stdbool.h>
#include <stdint.h>

#include "configuration_service.h"

typedef struct {
    configuration_snapshot_t confirmed_snapshot;
    configuration_snapshot_t staged_snapshot;
    bool staged;
    /* A reset must not give an unconfirmed candidate an unlimited fresh retry
     * window.  This counter is durable with the candidate, while the actual
     * Wi-Fi/Hub deadlines remain in their Connectivity/Gateway owners. */
    uint32_t staged_boot_attempts;
} configuration_provisioning_transaction_t;

/* A portal save gets a bounded number of complete candidate boots.  This is
 * deliberately a product/business rule, not a radio, board, or RTC policy. */
#define CONFIGURATION_TRANSACTION_MAX_STAGED_BOOT_ATTEMPTS 3u

/* Pair-code syntax is a Configuration value rule shared by every trusted
 * provisioning surface. It has no portal, radio, or board dependency. */
bool configuration_transaction_valid_pairing_code(const char *code);

/* Validate a portal/BLE/USB provisioning request and derive a fresh candidate
 * from the confirmed baseline. Only user-owned Wi-Fi/EAP, Hub URL and pairing
 * fields may change; volume, selected transport and the existing personal
 * network catalogue stay in the baseline. The transaction is unchanged on
 * rejection. This is a pure value operation: Configuration Service makes the
 * resulting transaction durable. */
bool configuration_transaction_stage_provisioning_request(
    configuration_provisioning_transaction_t *transaction,
    const configuration_provisioning_request_t *request);

/* Applies an ordinary confirmed-policy mutation without accidentally losing it
 * when a pending candidate is later promoted.  `confirmed_policy` is the
 * complete next confirmed snapshot produced by Configuration Service.  While
 * staged, the candidate retains its provisioned Wi-Fi/EAP, Hub URL, one-time
 * pair code and empty token, but inherits shared device policy (volume,
 * selected uplink and saved personal-network catalogue).  The operation is
 * all-or-nothing and leaves the transaction unchanged on malformed input.
 *
 * This is deliberately a value transition: it has no NVS, Wi-Fi, HTTP, task
 * or board dependency.  Pair-code replacement is not an ordinary policy
 * mutation while a candidate is pending; Configuration Service rejects that
 * ambiguous flow rather than silently replacing staged confirmation evidence.
 */
bool configuration_transaction_apply_confirmed_policy(
    configuration_provisioning_transaction_t *transaction,
    const configuration_snapshot_t *confirmed_policy);

/* Candidate is the configuration to activate for this boot; the bool tells
 * callers whether it still requires Wi-Fi + Hub confirmation. */
const configuration_snapshot_t *configuration_transaction_boot_snapshot(
    const configuration_provisioning_transaction_t *transaction, bool *out_staged);

/* Records one candidate boot before radio/Gateway work starts.  Returns false
 * if no candidate exists or its durable boot budget has expired; expiry rolls
 * back to the confirmed snapshot in the same value transition. */
bool configuration_transaction_begin_staged_boot(
    configuration_provisioning_transaction_t *transaction);

/* Hub token is pairing-completion evidence. This all-or-nothing value
 * transition supports the two legitimate pairing flows without weak boolean
 * switches: a staged candidate is promoted, while an already-confirmed
 * network (reuse-network portal or voice pairing) retains its configuration.
 * In both cases the token replaces the active token and the consumed pairing
 * code is cleared. Invalid token/snapshot input leaves the transaction
 * unchanged. */
bool configuration_transaction_commit_gateway_pairing_token(
    configuration_provisioning_transaction_t *transaction, const char *token);

/* A failed candidate removes only the candidate. This is intentionally
 * idempotent so reset/recovery callers may repeat it safely. */
void configuration_transaction_rollback(configuration_provisioning_transaction_t *transaction);
