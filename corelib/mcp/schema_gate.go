package mcp

import "fmt"

// RejectSchema fails closed when a discovered input schema cannot be
// checked. An empty schema, a non-object schema, or a required name that
// is not a property is not a pass. Annotations are ignored by the parser
// and are not consulted here.
func RejectSchema(schema map[string]interface{}) error {
	if len(schema) == 0 {
		return fmt.Errorf("mcp tool schema is missing")
	}
	if typ, ok := schema["type"].(string); ok && typ != "" && typ != "object" {
		return fmt.Errorf("mcp tool schema type %q is not object", typ)
	}
	props, hasProps := schema["properties"]
	var properties map[string]interface{}
	if hasProps {
		typed, ok := props.(map[string]interface{})
		if !ok {
			return fmt.Errorf("mcp tool schema properties are invalid")
		}
		properties = typed
	}
	req, hasReq := schema["required"]
	if !hasReq {
		return nil
	}
	names, err := requiredNames(req)
	if err != nil {
		return err
	}
	for _, name := range names {
		if properties == nil {
			return fmt.Errorf("mcp tool schema required %s has no properties", name)
		}
		if _, ok := properties[name]; !ok {
			return fmt.Errorf("mcp tool schema required %s is not a property", name)
		}
	}
	return nil
}

// RejectUnknownArgs rejects argument names the schema does not declare.
// An empty argument object is allowed. Extra names are not.
func RejectUnknownArgs(schema map[string]interface{}, args map[string]interface{}) error {
	if len(args) == 0 {
		return nil
	}
	properties, _ := schema["properties"].(map[string]interface{})
	if len(properties) == 0 {
		return fmt.Errorf("mcp arguments are not in the schema")
	}
	for name := range args {
		if _, ok := properties[name]; !ok {
			return fmt.Errorf("mcp argument %s is not in the schema", name)
		}
	}
	return nil
}

func requiredNames(req any) ([]string, error) {
	switch list := req.(type) {
	case []interface{}:
		out := make([]string, 0, len(list))
		for _, item := range list {
			name, ok := item.(string)
			if !ok || name == "" {
				return nil, fmt.Errorf("mcp tool schema required name is invalid")
			}
			out = append(out, name)
		}
		return out, nil
	case []string:
		for _, name := range list {
			if name == "" {
				return nil, fmt.Errorf("mcp tool schema required name is invalid")
			}
		}
		return list, nil
	default:
		return nil, fmt.Errorf("mcp tool schema required is invalid")
	}
}
