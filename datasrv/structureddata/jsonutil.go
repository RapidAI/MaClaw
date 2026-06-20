package structureddata

import "encoding/json"

func cloneJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	data, err := json.Marshal(in)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func cloneJSONValue[T any](in T) T {
	data, err := json.Marshal(in)
	if err != nil {
		var zero T
		return zero
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		var zero T
		return zero
	}
	return out
}

func mergeJSONPatchMap(base, patch map[string]any) map[string]any {
	out := cloneJSONMap(base)
	if out == nil {
		out = map[string]any{}
	}
	mergeJSONPatchInto(out, patch)
	return out
}

func mergeJSONPatchInto(base map[string]any, patch map[string]any) {
	for key, value := range patch {
		if nestedPatch, ok := value.(map[string]any); ok {
			if nestedBase, ok := base[key].(map[string]any); ok {
				mergeJSONPatchInto(nestedBase, nestedPatch)
				base[key] = nestedBase
			} else {
				base[key] = mergeJSONPatchMap(nil, nestedPatch)
			}
			continue
		}
		base[key] = value
	}
}
