#include "presentation/safe_mode_input_policy.h"

safe_mode_input_route_t safe_mode_input_policy_route(bool alarm_initialized,
                                                      bool alarm_ringing,
                                                      bool primary_interaction_source,
                                                      bool safe_mode_active) {
    if (alarm_initialized && alarm_ringing) {
        return primary_interaction_source ? SAFE_MODE_INPUT_ROUTE_DISMISS_ALARM
                                          : SAFE_MODE_INPUT_ROUTE_IGNORE_RINGING_ALARM;
    }
    return safe_mode_active ? SAFE_MODE_INPUT_ROUTE_IGNORE_SAFE_MODE
                            : SAFE_MODE_INPUT_ROUTE_CONTINUE;
}
