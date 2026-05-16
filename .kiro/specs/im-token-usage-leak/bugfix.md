# Bugfix Requirements Document

## Introduction

Mobile IM channels (飞书/微信/QQ/Telegram) expose internal LLM token usage statistics ("Input tokens", "Output tokens", "Total tokens") directly to end users in chat messages. This telemetry data is intended for internal monitoring and the desktop AI assistant panel (which filters it out in frontend rendering), but the IM response pipeline passes all fields through without filtering, causing the token counts to appear as visible "Label: Value" lines in user-facing messages. This leaks internal system telemetry and degrades the user experience on IM channels.

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN an LLM response includes token usage data AND the response is delivered to an IM channel (飞书/微信/QQ/Telegram) THEN the system displays "Input tokens: N" as a visible field in the user-facing message

1.2 WHEN an LLM response includes token usage data AND the response is delivered to an IM channel THEN the system displays "Output tokens: N" as a visible field in the user-facing message

1.3 WHEN an LLM response includes token usage data AND the response is delivered to an IM channel THEN the system displays "Total tokens: N" as a visible field in the user-facing message

1.4 WHEN `AgentResponse.ToGenericResponse()` converts an agent response containing token usage fields THEN the system passes all fields through to `GenericResponse.Fields` without any filtering

1.5 WHEN `GenericResponse.ToFallbackText()` renders fields for IM delivery THEN the system renders token usage fields as "Label: Value" lines visible to the end user

### Expected Behavior (Correct)

2.1 WHEN an LLM response includes token usage data AND the response is delivered to an IM channel THEN the system SHALL NOT display "Input tokens" in the user-facing message

2.2 WHEN an LLM response includes token usage data AND the response is delivered to an IM channel THEN the system SHALL NOT display "Output tokens" in the user-facing message

2.3 WHEN an LLM response includes token usage data AND the response is delivered to an IM channel THEN the system SHALL NOT display "Total tokens" in the user-facing message

2.4 WHEN `AgentResponse.ToGenericResponse()` converts an agent response containing token usage fields THEN the system SHALL filter out fields whose labels match token usage patterns (labels containing "tokens") before populating `GenericResponse.Fields`

2.5 WHEN token usage fields are filtered from `GenericResponse.Fields` THEN the system SHALL preserve the token counts in `IMAgentResponse.InputTokens`, `IMAgentResponse.OutputTokens`, and `IMAgentResponse.TotalTokens` numeric fields for internal telemetry purposes

### Unchanged Behavior (Regression Prevention)

3.1 WHEN an LLM response includes non-token-usage fields (e.g., skill results, status information) AND the response is delivered to an IM channel THEN the system SHALL CONTINUE TO display those fields normally in the user-facing message

3.2 WHEN an LLM response includes token usage data AND the response is viewed in the desktop AI assistant panel THEN the system SHALL CONTINUE TO have token usage data available for frontend rendering/filtering as before

3.3 WHEN `tokenUsageResponseFields()` generates token usage `IMResponseField` objects THEN the system SHALL CONTINUE TO populate `resp.InputTokens`, `resp.OutputTokens`, and `resp.TotalTokens` numeric fields for telemetry and logging

3.4 WHEN `GenericResponse.ToOutgoingMessage()` converts a response with non-token fields THEN the system SHALL CONTINUE TO include those fields in the `OutgoingMessage.Fields` list

3.5 WHEN `GenericResponse.ToFallbackText()` renders a response with non-token fields THEN the system SHALL CONTINUE TO render those fields as "Label: Value" lines

---

## Bug Condition (Formal)

### Bug Condition Function

```pascal
FUNCTION isBugCondition(X)
  INPUT: X of type AgentResponseDelivery
  OUTPUT: boolean

  // Returns true when token usage fields exist in the response
  // and the response is delivered to an IM channel
  RETURN X.Response.Fields CONTAINS field WHERE field.Label MATCHES ".*[Tt]okens.*"
END FUNCTION
```

### Property: Fix Checking

```pascal
// Property: Fix Checking — Token usage fields filtered from IM-visible output
FOR ALL X WHERE isBugCondition(X) DO
  genericResp ← X.Response.ToGenericResponse'()
  FOR EACH field IN genericResp.Fields DO
    ASSERT NOT (field.Label MATCHES ".*[Tt]okens.*")
  END FOR
  // Token counts still available in numeric fields for telemetry
  ASSERT X.Response.InputTokens = originalInputTokens
  ASSERT X.Response.OutputTokens = originalOutputTokens
  ASSERT X.Response.TotalTokens = originalTotalTokens
END FOR
```

### Property: Preservation Checking

```pascal
// Property: Preservation Checking — Non-token fields pass through unchanged
FOR ALL X WHERE NOT isBugCondition(X) DO
  ASSERT ToGenericResponse(X.Response) = ToGenericResponse'(X.Response)
END FOR
```
