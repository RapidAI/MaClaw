package skill

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	PipelineRunStackArg      = "__pipeline_stack"
	PipelineInternalCallArg  = "__pipeline_internal_call"
	MaxPipelineRunStackDepth = 16
)

type pipelineInternalCallMarker struct{}

var pipelineInternalCallSentinel = &pipelineInternalCallMarker{}

func PipelineRunStackFromArgs(runArgs map[string]interface{}) []string {
	raw, ok := runArgs[PipelineRunStackArg]
	if !ok || raw == nil {
		return nil
	}
	return normalizePipelineRunStack(raw)
}

func TrustedPipelineRunStackFromArgs(runArgs map[string]interface{}) []string {
	if !IsInternalPipelineRunArgs(runArgs) {
		return nil
	}
	return PipelineRunStackFromArgs(runArgs)
}

func WithPipelineRunStack(runArgs map[string]interface{}, skillName string) map[string]interface{} {
	dst := map[string]interface{}{}
	for k, v := range runArgs {
		dst[k] = v
	}
	stack := TrustedPipelineRunStackFromArgs(runArgs)
	if name := strings.TrimSpace(skillName); name != "" {
		stack = append(stack, name)
	}
	if len(stack) > 0 {
		dst[PipelineRunStackArg] = stack
		dst[PipelineInternalCallArg] = pipelineInternalCallSentinel
	}
	return dst
}

func IsInternalPipelineRunArgs(runArgs map[string]interface{}) bool {
	if len(PipelineRunStackFromArgs(runArgs)) == 0 {
		return false
	}
	raw, ok := runArgs[PipelineInternalCallArg]
	if !ok {
		return false
	}
	return raw == pipelineInternalCallSentinel
}

func IsPipelineBaseRunArgAllowed(key string) bool {
	if key == PipelineRunStackArg || key == PipelineInternalCallArg {
		return true
	}
	key = canonicalRunVarKey(key)
	if isPipelineContextCarrierKey(key) {
		return true
	}
	switch key {
	case "env", "extra_env", "environment":
		return true
	default:
		return false
	}
}

func isPipelineContextCarrierKey(key string) bool {
	key = canonicalRunVarKey(key)
	switch key {
	case "mode", "content", "message", "prompt", "task", "description":
		return true
	}
	for _, candidate := range RunVarFallbackKeys {
		if canonicalRunVarKey(candidate) == key {
			return true
		}
	}
	return false
}

func BuildPipelineSubSkillRunArgs(baseRunArgs map[string]interface{}, params map[string]string) map[string]interface{} {
	dst := map[string]interface{}{}
	for key, value := range baseRunArgs {
		if IsPipelineBaseRunArgAllowed(key) {
			dst[key] = value
		}
	}
	for _, key := range pipelineContextCarrierKeys() {
		if _, exists := lookupRunArg(dst, key); exists {
			continue
		}
		if value, ok := lookupRunControlArg(baseRunArgs, key); ok {
			dst[key] = value
		}
	}
	if _, exists := lookupRunArg(dst, "input"); !exists {
		if value := plainStringArgsInput(baseRunArgs); value != "" {
			dst["input"] = value
		}
	}
	if extraEnv := ExtractRunExtraEnvFromArgs(baseRunArgs); len(extraEnv) > 0 {
		dst["extra_env"] = extraEnv
	}
	for key, value := range params {
		if IsPipelineControlParamKey(key) || IsManageSkillRunnerControlKey(key) {
			continue
		}
		if isPipelineEnvParamKey(key) {
			merged := ExtractRunExtraEnvFromArgs(dst)
			if merged == nil {
				merged = map[string]string{}
			}
			for envKey, envValue := range ExtractRunExtraEnv(value) {
				merged[envKey] = envValue
			}
			if len(merged) > 0 {
				dst["extra_env"] = merged
			}
			continue
		}
		dst[key] = value
	}
	return dst
}

func pipelineContextCarrierKeys() []string {
	seen := map[string]bool{}
	var keys []string
	add := func(key string) {
		key = canonicalRunVarKey(key)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		keys = append(keys, key)
	}
	for _, key := range RunVarFallbackKeys {
		add(key)
	}
	for _, key := range []string{"mode", "content", "message", "prompt", "task", "description"} {
		add(key)
	}
	return keys
}

func plainStringArgsInput(runArgs map[string]interface{}) string {
	raw, ok := lookupRunArg(runArgs, "args")
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		return ""
	}
	return value
}

func isPipelineEnvParamKey(key string) bool {
	switch canonicalRunVarKey(key) {
	case "env", "extra_env", "environment":
		return true
	default:
		return false
	}
}

func IsPipelineControlParamKey(key string) bool {
	if key == PipelineRunStackArg || key == PipelineInternalCallArg {
		return true
	}
	switch canonicalRunVarKey(key) {
	case "pipeline_stack", "pipeline_internal_call":
		return true
	default:
		return false
	}
}

func PipelineRunStackContains(stack []string, skillName string) bool {
	target := canonicalPipelineSkillName(skillName)
	if target == "" {
		return false
	}
	for _, item := range stack {
		if canonicalPipelineSkillName(item) == target {
			return true
		}
	}
	return false
}

func PipelineRunStackDepthExceeded(stack []string) bool {
	return len(stack) >= MaxPipelineRunStackDepth
}

func CheckTrustedPipelineRunStack(runArgs map[string]interface{}, skillName string) error {
	stack := TrustedPipelineRunStackFromArgs(runArgs)
	if PipelineRunStackContains(stack, skillName) {
		return fmt.Errorf("%s", FormatPipelineRecursionMessage(skillName, stack))
	}
	if PipelineRunStackDepthExceeded(stack) {
		return fmt.Errorf("%s", FormatPipelineStackDepthMessage(skillName, stack))
	}
	return nil
}

func FormatPipelineRecursionMessage(skillName string, stack []string) string {
	chain := append(append([]string(nil), stack...), strings.TrimSpace(skillName))
	return fmt.Sprintf("pipeline recursion detected: %s", strings.Join(chain, " -> "))
}

func FormatPipelineStackDepthMessage(skillName string, stack []string) string {
	chain := append(append([]string(nil), stack...), strings.TrimSpace(skillName))
	return fmt.Sprintf("pipeline nesting depth exceeded (%d): %s", MaxPipelineRunStackDepth, strings.Join(chain, " -> "))
}

func normalizePipelineRunStack(raw interface{}) []string {
	add := func(out []string, value interface{}) []string {
		s := strings.TrimSpace(fmt.Sprint(value))
		if s != "" {
			out = append(out, s)
		}
		return out
	}
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = add(out, item)
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = add(out, item)
		}
		return out
	case string:
		value := strings.TrimSpace(v)
		if value == "" {
			return nil
		}
		var parsed []string
		if strings.HasPrefix(value, "[") && json.Unmarshal([]byte(value), &parsed) == nil {
			return normalizePipelineRunStack(parsed)
		}
		value = strings.ReplaceAll(value, "->", ",")
		parts := strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '>' || r == '\n' || r == '\r' || r == '\t'
		})
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			out = add(out, part)
		}
		return out
	default:
		return add(nil, v)
	}
}

func canonicalPipelineSkillName(skillName string) string {
	return strings.ToLower(strings.TrimSpace(skillName))
}
