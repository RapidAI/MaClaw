package cua

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// remarshal decodes a driver answer into a typed struct so host code never
// reads the loose map by hand.
func remarshal[T any](in map[string]any) (T, error) {
	var v T
	if in == nil {
		return v, nil
	}
	buf, err := json.Marshal(in)
	if err != nil {
		return v, fmt.Errorf("cua: result not encodable: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(buf))
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("cua: unexpected result shape: %w", err)
	}
	return v, nil
}

// unmarshalStrict decodes b into v and rejects any field not in allowed.
// A misspelled selector is a caller bug worth a message, not an empty
// selector that silently matches the wrong row.
func unmarshalStrict(b []byte, v any, allowed map[string]bool) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(v); err != nil {
		return err
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	var unknown []string
	for key := range probe {
		if !allowed[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown selector field %q (use role, label_contains, role_prefix, index)", strings.Join(unknown, ", "))
	}
	return nil
}
