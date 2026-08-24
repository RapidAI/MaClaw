package agent

import "reflect"

// tool_definitions.go provides the ToolDef helper for building OpenAI-compatible
// tool definitions. Used by CoreToolRegistry.BuildDefinitions() and by GUI's
// tool_registry_builtin.go.
//
// Tool definitions are NOT maintained here as a standalone list. They are
// registered together with their handlers in tool_register_core.go via
// RegisterCoreTools(). This eliminates the "two independent lists" problem.

// ToolDef builds a single OpenAI-compatible tool definition. The schema and
// required list come from a long-lived registry entry, so copy both before
// publishing them into a model request definition. A request-side transformer
// must never be able to mutate registry inventory and thereby change a later
// request's tool surface.
func ToolDef(name, desc string, props map[string]interface{}, required []string) map[string]interface{} {
	params := map[string]interface{}{
		"type": "object",
	}
	if props != nil {
		params["properties"] = cloneToolDefinitionProperties(props)
	} else {
		params["properties"] = map[string]interface{}{}
	}
	if len(required) > 0 {
		params["required"] = append([]string(nil), required...)
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": desc,
			"parameters":  params,
		},
	}
}

func cloneToolDefinitionProperties(props map[string]interface{}) map[string]interface{} {
	if props == nil {
		return nil
	}
	cloned := make(map[string]interface{}, len(props))
	for key, value := range props {
		cloned[key] = cloneToolDefinitionValue(value)
	}
	return cloned
}

func cloneToolDefinitionValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneToolDefinitionProperties(typed)
	case map[string]string:
		cloned := make(map[string]string, len(typed))
		for key, value := range typed {
			cloned[key] = value
		}
		return cloned
	case []interface{}:
		cloned := make([]interface{}, len(typed))
		for index, value := range typed {
			cloned[index] = cloneToolDefinitionValue(value)
		}
		return cloned
	case []string:
		return append([]string(nil), typed...)
	case []map[string]interface{}:
		cloned := make([]map[string]interface{}, len(typed))
		for index, value := range typed {
			cloned[index] = cloneToolDefinitionProperties(value)
		}
		return cloned
	default:
		// Schema producers occasionally use named map/slice types or compose
		// shapes such as map[string][]string. They are still JSON-shaped mutable
		// data and must not bypass the snapshot boundary merely because they are
		// not one of the common concrete types above. Preserve non-string-keyed
		// maps, pointers, structs and other opaque values so the later wire-freeze
		// retains its existing fail-closed behavior for malformed schemas.
		return cloneToolDefinitionJSONValue(reflect.ValueOf(value)).Interface()
	}
}

func cloneToolDefinitionJSONValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return value
		}
		cloned := reflect.New(value.Type()).Elem()
		cloned.Set(cloneToolDefinitionJSONValue(value.Elem()))
		return cloned
	case reflect.Map:
		if value.IsNil() || value.Type().Key().Kind() != reflect.String {
			return value
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			cloned.SetMapIndex(iterator.Key(), cloneToolDefinitionJSONValue(iterator.Value()))
		}
		return cloned
	case reflect.Slice:
		if value.IsNil() {
			return value
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			cloned.Index(index).Set(cloneToolDefinitionJSONValue(value.Index(index)))
		}
		return cloned
	case reflect.Array:
		cloned := reflect.New(value.Type()).Elem()
		for index := 0; index < value.Len(); index++ {
			cloned.Index(index).Set(cloneToolDefinitionJSONValue(value.Index(index)))
		}
		return cloned
	default:
		return value
	}
}

// CloneToolDefinitionMap returns an independent copy of a JSON-shaped model
// tool definition or schema. Renderers use it whenever their source schema is
// retained in a long-lived registry, plan, or host configuration.
func CloneToolDefinitionMap(definition map[string]interface{}) map[string]interface{} {
	return cloneToolDefinitionProperties(definition)
}
