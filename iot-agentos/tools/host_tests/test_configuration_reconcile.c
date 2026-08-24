#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>

#include "configuration_reconcile.h"

#define CHECK(expr) do { \
    if (!(expr)) { \
        fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__, #expr); \
        return 1; \
    } \
} while (0)

typedef struct {
    int events[32];
    size_t event_count;
    bool validate_ok;
    bool publish_ok;
    bool prepare_ok[3];
    bool apply_ok[3];
    bool rollback_ok[3];
} trace_t;

typedef struct {
    trace_t *trace;
    unsigned index;
} observer_context_t;

static void note(trace_t *trace, int event) {
    trace->events[trace->event_count++] = event;
}

static bool validate(const void *candidate, size_t size, void *context) {
    trace_t *trace = context;
    note(trace, 10);
    return candidate && size == sizeof(uint32_t) && trace->validate_ok;
}

static bool publish(uint64_t revision, const void *candidate, size_t size, void *context) {
    trace_t *trace = context;
    note(trace, 20);
    return revision == 2u && candidate && size == sizeof(uint32_t) && trace->publish_ok;
}

static bool prepare(uint64_t previous, uint64_t candidate, const void *value,
                    size_t size, void *context) {
    observer_context_t *observer = context;
    note(observer->trace, 30 + (int)observer->index);
    return previous == 1u && candidate == 2u && value && size == sizeof(uint32_t) &&
           observer->trace->prepare_ok[observer->index];
}

static bool apply(uint64_t candidate, const void *value, size_t size, void *context) {
    observer_context_t *observer = context;
    note(observer->trace, 40 + (int)observer->index);
    return candidate == 2u && value && size == sizeof(uint32_t) &&
           observer->trace->apply_ok[observer->index];
}

static bool rollback(uint64_t previous, void *context) {
    observer_context_t *observer = context;
    note(observer->trace, 50 + (int)observer->index);
    return previous == 1u && observer->trace->rollback_ok[observer->index];
}

static configuration_reconcile_result_t run(trace_t *trace) {
    uint32_t candidate = 42u;
    observer_context_t contexts[3] = {
        {.trace = trace, .index = 0u},
        {.trace = trace, .index = 1u},
        {.trace = trace, .index = 2u},
    };
    configuration_reconcile_observer_t observers[3] = {
        {.prepare = prepare, .apply = apply, .rollback = rollback, .context = &contexts[0]},
        {.prepare = prepare, .apply = apply, .rollback = rollback, .context = &contexts[1]},
        {.prepare = prepare, .apply = apply, .rollback = rollback, .context = &contexts[2]},
    };
    const configuration_reconcile_transaction_t transaction = {
        .validate = validate, .publish = publish, .context = trace,
    };
    return configuration_reconcile_execute(1u, 2u, &candidate, sizeof(candidate),
                                           &transaction, observers, 3u);
}

static bool events_are(const trace_t *trace, const int *expected, size_t count) {
    if (trace->event_count != count) return false;
    for (size_t i = 0; i < count; ++i) {
        if (trace->events[i] != expected[i]) return false;
    }
    return true;
}

int main(void) {
    trace_t trace = {.validate_ok = true, .publish_ok = true,
                     .prepare_ok = {true, true, true},
                     .apply_ok = {true, true, true},
                     .rollback_ok = {true, true, true}};
    CHECK(run(&trace) == CONFIGURATION_RECONCILE_OK);
    const int success[] = {10, 30, 31, 32, 20, 40, 41, 42};
    CHECK(events_are(&trace, success, sizeof(success) / sizeof(success[0])));

    trace = (trace_t){.validate_ok = false, .publish_ok = true,
                      .prepare_ok = {true, true, true}, .apply_ok = {true, true, true},
                      .rollback_ok = {true, true, true}};
    CHECK(run(&trace) == CONFIGURATION_RECONCILE_VALIDATE_FAILED);
    const int validation_failure[] = {10};
    CHECK(events_are(&trace, validation_failure, 1u));

    trace = (trace_t){.validate_ok = true, .publish_ok = true,
                      .prepare_ok = {true, false, true}, .apply_ok = {true, true, true},
                      .rollback_ok = {true, true, true}};
    CHECK(run(&trace) == CONFIGURATION_RECONCILE_PREPARE_FAILED);
    const int prepare_failure[] = {10, 30, 31, 50};
    CHECK(events_are(&trace, prepare_failure, 4u));

    trace = (trace_t){.validate_ok = true, .publish_ok = false,
                      .prepare_ok = {true, true, true}, .apply_ok = {true, true, true},
                      .rollback_ok = {true, true, true}};
    CHECK(run(&trace) == CONFIGURATION_RECONCILE_PUBLISH_FAILED);
    const int publish_failure[] = {10, 30, 31, 32, 20, 52, 51, 50};
    CHECK(events_are(&trace, publish_failure, 8u));

    trace = (trace_t){.validate_ok = true, .publish_ok = true,
                      .prepare_ok = {true, true, true}, .apply_ok = {true, false, true},
                      .rollback_ok = {true, true, true}};
    CHECK(run(&trace) == CONFIGURATION_RECONCILE_APPLY_FAILED);
    const int apply_failure[] = {10, 30, 31, 32, 20, 40, 41, 51, 50};
    CHECK(events_are(&trace, apply_failure, sizeof(apply_failure) / sizeof(apply_failure[0])));

    trace = (trace_t){.validate_ok = true, .publish_ok = true,
                      .prepare_ok = {true, true, true}, .apply_ok = {true, true, false},
                      .rollback_ok = {true, false, true}};
    CHECK(run(&trace) == CONFIGURATION_RECONCILE_UNKNOWN_OUTCOME);
    const int unknown_outcome[] = {10, 30, 31, 32, 20, 40, 41, 42, 52, 51, 50};
    CHECK(events_are(&trace, unknown_outcome, sizeof(unknown_outcome) / sizeof(unknown_outcome[0])));

    trace = (trace_t){.validate_ok = true, .publish_ok = true,
                      .prepare_ok = {true, false, true}, .apply_ok = {true, true, true},
                      .rollback_ok = {false, true, true}};
    CHECK(run(&trace) == CONFIGURATION_RECONCILE_UNKNOWN_OUTCOME);
    const int prepare_unknown[] = {10, 30, 31, 50};
    CHECK(events_are(&trace, prepare_unknown, sizeof(prepare_unknown) / sizeof(prepare_unknown[0])));

    uint32_t candidate = 7u;
    const configuration_reconcile_transaction_t transaction = {
        .validate = validate, .publish = publish, .context = &trace,
    };
    CHECK(configuration_reconcile_execute(2u, 2u, &candidate, sizeof(candidate),
                                          &transaction, NULL, 0u) ==
          CONFIGURATION_RECONCILE_INVALID_ARGUMENT);

    puts("PASS configuration reconcile preserves validate/prepare/publish/apply/rollback order");
    return 0;
}
