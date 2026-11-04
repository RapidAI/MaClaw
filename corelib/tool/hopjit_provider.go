package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
)

type HopJITSelectionInvoker func(context.Context, PlannedSelection, CanonicalRequest) SelectionExecutionResult

type HopJITPlanToolProvider struct {
	Executor  *PlanExecutor
	Scope     InvocationScope
	Plan      ToolPlan
	Grants    map[string]InvocationGrant
	Schemas   map[string]map[string]interface{}
	Satisfied map[string]bool
	Invoke    HopJITSelectionInvoker
}

var _ hopjit.ToolProvider = (*HopJITPlanToolProvider)(nil)

func (p *HopJITPlanToolProvider) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	if p == nil || p.Executor == nil || p.Invoke == nil {
		return nil, errors.New("hopjit_plan_provider_unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	grant, ok := p.Grants[name]
	if !ok {
		return nil, fmt.Errorf("hopjit_tool_not_authorized: %s", name)
	}
	schema, ok := p.Schemas[name]
	if !ok {
		return nil, fmt.Errorf("hopjit_tool_schema_missing: %s", name)
	}
	if args == nil {
		args = map[string]any{}
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("hopjit_tool_arguments_invalid: %w", err)
	}
	request, err := CanonicalizeAuthorizedInvocationArguments(string(encoded), schema, grant.ParameterAuthorization)
	if err != nil {
		return nil, err
	}
	result, _, err := p.Executor.Execute(grant, p.Scope, p.Plan, p.Satisfied, func(selection PlannedSelection) SelectionExecutionResult {
		return p.Invoke(ctx, selection, request)
	})
	if err != nil {
		return nil, err
	}
	switch {
	case result.Paused:
		return nil, errors.New("hopjit_tool_pause_requires_driver")
	case result.AwaitingReceipt:
		return nil, errors.New("hopjit_tool_awaiting_receipt")
	case result.Unknown:
		return nil, errors.New("hopjit_tool_outcome_unknown")
	case !result.Succeeded:
		if result.ReasonCode != "" {
			return nil, errors.New(result.ReasonCode)
		}
		return nil, errors.New("hopjit_tool_execution_failed")
	default:
		return result.Result, nil
	}
}
