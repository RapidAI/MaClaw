#include "services/gateway_event_ack_service.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cJSON.h"
#include "esp_err.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "presentation/scene_presenter.h"
#include "services/gateway_transport.h"

/* Keep the log tag identical to the original main.c owner so existing
 * event / approval trace filters and hardware baseline comparisons stay
 * valid. */
static const char *TAG = "maclaw_client";

#define GATEWAY_EVENT_ACK_PATH "/api/im-gateway/v1/event-ack"
#define GATEWAY_EVENT_ACK_BODY_CAPACITY 1024u

/* One card, not a queue.  The Hub asks for one decision at a time, and a
 * second prompt while the first is unanswered would make the gesture
 * ambiguous: the user cannot tell which card their press answered.
 *
 * Touched by two tasks -- the poll worker (arms, flushes) and the input
 * binding's task (records the answer) -- so every access is made under this
 * lock.  A torn read here is not cosmetic: it could pair `approved` with an
 * empty actionId, which the Hub rejects as a malformed ack and retires, losing
 * the user's answer.  The lock never spans the transport POST or a repaint;
 * each entry point copies the slot out, releases, and works on the copy. */
static event_decision_t s_pending;
static portMUX_TYPE s_slot_lock = portMUX_INITIALIZER_UNLOCKED;

static int64_t event_ack_now_ms(void) {
    return esp_timer_get_time() / 1000;
}

void gateway_event_ack_service_init(void) {
    taskENTER_CRITICAL(&s_slot_lock);
    event_decision_reset(&s_pending);
    taskEXIT_CRITICAL(&s_slot_lock);
}

bool gateway_event_ack_service_offers_decision(const event_ingest_fields_t *fields) {
    return fields && fields->actions && fields->action_count > 0;
}

bool gateway_event_ack_service_slot_free(void) {
    bool free_slot;
    taskENTER_CRITICAL(&s_slot_lock);
    free_slot = !event_decision_busy(&s_pending);
    taskEXIT_CRITICAL(&s_slot_lock);
    return free_slot;
}

static void append_text(char *buffer, size_t capacity, size_t *used, const char *text) {
    if (!buffer || !used || !text || *used + 1u >= capacity) return;
    const size_t room = capacity - *used - 1u;
    const size_t length = strlen(text);
    const size_t take = length < room ? length : room;
    memcpy(buffer + *used, text, take);
    *used += take;
    buffer[*used] = '\0';
}

/* The producer's own label for an action id.  Looked up by id rather than by
 * index because the decision slot only keeps the ids it can act on; matching
 * on the id keeps the two in step even if that filtering ever changes. */
static const char *action_label(const event_ingest_fields_t *fields, const char *id) {
    if (!fields || !id || id[0] == '\0') return NULL;
    for (int index = 0; index < fields->action_count; ++index) {
        const event_ingest_action_t *action = &fields->actions[index];
        if (action->id && strcmp(action->id, id) == 0) return action->label;
    }
    return NULL;
}

/* The card has to say what a press will do.  The action labels are the only
 * place the concrete object of a high-risk approval appears, and D5 requires
 * that object be shown rather than implied -- a blind confirm is not a
 * confirmation.  The gesture legend is device copy, so it stays here and not
 * in the host-tested header. */
static void publish_decision_card(const event_ingest_fields_t *fields,
                                  const event_decision_t *pending) {
    char body[GATEWAY_EVENT_ACK_BODY_CAPACITY];
    body[0] = '\0';
    size_t used = 0;

    const char *summary = event_ingest_display_body(fields);
    if (summary && summary[0] != '\0') {
        append_text(body, sizeof(body), &used, summary);
        append_text(body, sizeof(body), &used, "\n\n");
    }

    const int affirmative = event_decision_affirmative_index(pending);
    const int decline = event_decision_decline_index(pending);
    if (affirmative >= 0) {
        const char *label = action_label(fields, pending->actions[affirmative].id);
        append_text(body, sizeof(body), &used, "按主键同意：");
        append_text(body, sizeof(body), &used, label ? label : pending->actions[affirmative].id);
        append_text(body, sizeof(body), &used, "\n");
    }
    if (decline >= 0) {
        const char *label = action_label(fields, pending->actions[decline].id);
        append_text(body, sizeof(body), &used, "按副键拒绝：");
        append_text(body, sizeof(body), &used, label ? label : pending->actions[decline].id);
        append_text(body, sizeof(body), &used, "\n");
    }
    if (affirmative < 0 && decline < 0) {
        /* Cannot happen for an event the ingest policy validated (every
         * declared action has a usable id), but the card must not silently
         * become an unanswerable one if that invariant ever moves. */
        append_text(body, sizeof(body), &used, "此卡片未提供可执行动作");
    }

    scene_presenter_publish_response(event_ingest_display_title(fields), body);
}

/* The confirmation page.  A timeout is worded as "not done", never as
 * "refused": the user answered nothing, and telling them they declined would
 * put a decision in their mouth that they never made (plan D5). */
static void publish_outcome_card(const event_decision_t *pending) {
    const char *title = NULL;
    const char *body = NULL;
    switch (event_decision_outcome(pending)) {
        case EVENT_DECISION_OUTCOME_APPROVED:
            title = "已同意";
            body = "已按你的选择执行";
            break;
        case EVENT_DECISION_OUTCOME_DECLINED:
            title = "已拒绝";
            body = "已取消，不会执行";
            break;
        case EVENT_DECISION_OUTCOME_TIMED_OUT:
            title = "已超时";
            body = "未收到答复，操作未执行";
            break;
        default:
            return;
    }
    scene_presenter_publish_response(title, body);
}

bool gateway_event_ack_service_begin(const event_ingest_fields_t *fields) {
    const int64_t now = event_ack_now_ms();
    event_decision_t snapshot;
    bool armed;
    taskENTER_CRITICAL(&s_slot_lock);
    armed = event_decision_begin(&s_pending, fields, now);
    snapshot = s_pending;
    taskEXIT_CRITICAL(&s_slot_lock);
    if (!armed) return false;

    publish_decision_card(fields, &snapshot);
    ESP_LOGI(TAG, "approval card armed: event=%s actions=%d ttl_ms=%lld",
             snapshot.event_id, snapshot.action_count,
             (long long)snapshot.expires_at_ms);
    return true;
}

bool gateway_event_ack_service_handle_input(event_decision_input_t input) {
    const int64_t now = event_ack_now_ms();
    event_decision_t snapshot;
    bool taken;
    taskENTER_CRITICAL(&s_slot_lock);
    taken = event_decision_apply_input(&s_pending, input, now);
    snapshot = s_pending;
    taskEXIT_CRITICAL(&s_slot_lock);
    if (!taken) return false;

    ESP_LOGI(TAG, "approval answered: event=%s approved=%s action=%s",
             snapshot.event_id, snapshot.decided_approved ? "yes" : "no",
             snapshot.decided_action_id);
    /* Repaint from the input task, like the meeting messages this same
     * callback already publishes: the user must see that the press registered,
     * or they will press again and that second press falls through to voice. */
    publish_outcome_card(&snapshot);
    return true;
}

/* A 4xx is the Hub refusing this ack on its merits -- an unknown event, a
 * mismatched client, an action the card never offered.  Retrying cannot change
 * any of those, so the ack is retired with a warning instead of spinning
 * forever on an answer that can never land.  A 5xx and a transport error are
 * transient and keep the ack owed. */
static bool event_ack_status_is_permanent(int status) {
    return status >= 400 && status < 500;
}

static char *build_event_ack_payload(const event_decision_t *pending,
                                     event_decision_ack_kind_t kind) {
    const char *status = event_decision_ack_status(pending, kind);
    if (!status) return NULL;

    cJSON *body = cJSON_CreateObject();
    if (!body) return NULL;
    bool fields_ok =
        cJSON_AddStringToObject(body, "clientId", gateway_transport_device_id()) != NULL &&
        cJSON_AddStringToObject(body, "eventId", pending->event_id) != NULL &&
        cJSON_AddStringToObject(body, "status", status) != NULL;
    /* The wire contract forbids a receipt carrying a decision and an expiry
     * carrying an action, so both fields are added only when the kind owes
     * them.  event_decision_ack_* already answer NULL for the other kinds. */
    const char *action_id = event_decision_ack_action_id(pending, kind);
    if (fields_ok && action_id) {
        fields_ok = cJSON_AddStringToObject(body, "actionId", action_id) != NULL;
    }
    const char *decided_by = event_decision_ack_decided_by(kind);
    if (fields_ok && decided_by) {
        fields_ok = cJSON_AddStringToObject(body, "decidedBy", decided_by) != NULL;
    }
    char *payload = fields_ok ? cJSON_PrintUnformatted(body) : NULL;
    cJSON_Delete(body);
    return payload;
}

/* Records the delivery against the slot it was computed from.
 *
 * The event id is re-checked because the POST released the lock: a receipt can
 * legitimately be superseded by a decision while it is in flight, and marking
 * the *new* card's ack as sent from an old card's answer would retire an ack
 * that was never delivered.  (A decision and a timeout cannot interleave --
 * both make the card inactive, so apply_input refuses afterwards.) */
static void mark_ack_delivered_if_same(const event_decision_t *expected,
                                       event_decision_ack_kind_t kind, int64_t now) {
    taskENTER_CRITICAL(&s_slot_lock);
    if (strcmp(s_pending.event_id, expected->event_id) == 0) {
        event_decision_mark_ack_delivered(&s_pending, kind);
        if (event_decision_settled(&s_pending, now)) event_decision_reset(&s_pending);
    }
    taskEXIT_CRITICAL(&s_slot_lock);
}

int32_t gateway_event_ack_service_flush(void) {
    const int64_t now = event_ack_now_ms();
    event_decision_t snapshot;
    bool window_closed;
    event_decision_ack_kind_t kind;
    taskENTER_CRITICAL(&s_slot_lock);
    /* Close the window before asking which ack is owed.  Otherwise a card
     * whose deadline passed between two polls would still report "the card
     * arrived" instead of the timeout the audit needs. */
    window_closed = event_decision_note_window(&s_pending, now);
    kind = event_decision_next_ack(&s_pending, now);
    if (kind == EVENT_DECISION_ACK_NONE && event_decision_settled(&s_pending, now)) {
        event_decision_reset(&s_pending);
    }
    snapshot = s_pending;
    taskEXIT_CRITICAL(&s_slot_lock);

    if (window_closed) {
        ESP_LOGW(TAG, "approval window closed unanswered: event=%s", snapshot.event_id);
        publish_outcome_card(&snapshot);
    }
    if (kind == EVENT_DECISION_ACK_NONE) return ESP_OK;

    const char *device_id = gateway_transport_device_id();
    if (!device_id || device_id[0] == '\0') {
        /* Nothing to correlate the ack with.  Keep it owed: pairing may still
         * complete, and the poll worker does not run unpaired anyway. */
        return ESP_ERR_INVALID_STATE;
    }

    char *payload = build_event_ack_payload(&snapshot, kind);
    if (!payload) return ESP_ERR_NO_MEM;

    gateway_transport_response_t response = {0};
    int32_t err = gateway_transport_request("POST", GATEWAY_EVENT_ACK_PATH,
                                            "application/json", payload,
                                            (int32_t)strlen(payload), &response);
    const int http_status = response.status;
    gateway_transport_response_release(&response);
    free(payload);

    const char *status = event_decision_ack_status(&snapshot, kind);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "approval ack not delivered (transport): event=%s status=%s",
                 snapshot.event_id, status);
        return err;
    }
    if (http_status != 200) {
        if (!event_ack_status_is_permanent(http_status)) {
            ESP_LOGW(TAG, "approval ack refused transiently: event=%s http=%d",
                     snapshot.event_id, http_status);
            return ESP_FAIL;
        }
        ESP_LOGE(TAG, "approval ack refused permanently: event=%s http=%d status=%s",
                 snapshot.event_id, http_status, status);
    } else {
        ESP_LOGI(TAG, "approval ack delivered: event=%s status=%s action=%s",
                 snapshot.event_id, status,
                 event_decision_ack_action_id(&snapshot, kind)
                     ? event_decision_ack_action_id(&snapshot, kind)
                     : "-");
    }

    mark_ack_delivered_if_same(&snapshot, kind, now);
    return ESP_OK;
}
