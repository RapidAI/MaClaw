package guiapp

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type sqliteValueField struct {
	Path   string
	Kind   string
	Text   string
	Number float64
	Bool   bool
}

const maxSQLiteValueNesting = 128

func flattenSQLiteValueFields(value interface{}) ([]sqliteValueField, error) {
	fields := make([]sqliteValueField, 0)
	if err := appendSQLiteValueFields(&fields, "", value, 0); err != nil {
		return nil, err
	}
	return fields, nil
}

func appendSQLiteValueFields(fields *[]sqliteValueField, path string, value interface{}, depth int) error {
	if depth > maxSQLiteValueNesting {
		return fmt.Errorf("SQLite value nesting exceeds %d", maxSQLiteValueNesting)
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		if len(typed) == 0 {
			*fields = append(*fields, sqliteValueField{Path: path, Kind: "m"})
			return nil
		}
		for key, child := range typed {
			if err := appendSQLiteValueFields(fields, sqliteMapFieldPath(path, key), child, depth+1); err != nil {
				return err
			}
		}
		return nil
	case []interface{}:
		if len(typed) == 0 {
			*fields = append(*fields, sqliteValueField{Path: path, Kind: "a"})
			return nil
		}
		for index, child := range typed {
			if err := appendSQLiteValueFields(fields, sqliteArrayFieldPath(path, index), child, depth+1); err != nil {
				return err
			}
		}
		return nil
	case string:
		*fields = append(*fields, sqliteValueField{Path: path, Kind: "s", Text: typed})
		return nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return fmt.Errorf("SQLite value at %q is not a finite number", path)
		}
		*fields = append(*fields, sqliteValueField{Path: path, Kind: "n", Number: typed})
		return nil
	case bool:
		*fields = append(*fields, sqliteValueField{Path: path, Kind: "b", Bool: typed})
		return nil
	case nil:
		*fields = append(*fields, sqliteValueField{Path: path, Kind: "0"})
		return nil
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		var normalized interface{}
		if err := json.Unmarshal(raw, &normalized); err != nil {
			return err
		}
		return appendSQLiteValueFields(fields, path, normalized, depth)
	}
}

func sqliteMapFieldPath(parent, key string) string {
	return parent + "/m" + strconv.Itoa(len(key)) + ":" + key
}

func sqliteArrayFieldPath(parent string, index int) string {
	return parent + "/a" + strconv.Itoa(index)
}

type sqliteValuePathSegment struct {
	key     string
	index   int
	isArray bool
}

func parseSQLiteValueFieldPath(path string) ([]sqliteValuePathSegment, error) {
	if path == "" {
		return nil, fmt.Errorf("empty SQLite value field path")
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	segments := make([]sqliteValuePathSegment, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("invalid SQLite value field path %q", path)
		}
		switch part[0] {
		case 'm':
			separator := strings.IndexByte(part, ':')
			if separator < 2 {
				return nil, fmt.Errorf("invalid SQLite map field path %q", path)
			}
			length, err := strconv.Atoi(part[1:separator])
			if err != nil {
				return nil, fmt.Errorf("invalid SQLite map field path %q: %w", path, err)
			}
			key := part[separator+1:]
			if len(key) != length {
				return nil, fmt.Errorf("invalid SQLite map key length in %q", path)
			}
			segments = append(segments, sqliteValuePathSegment{key: key})
		case 'a':
			index, err := strconv.Atoi(part[1:])
			if err != nil || index < 0 {
				return nil, fmt.Errorf("invalid SQLite array field path %q", path)
			}
			segments = append(segments, sqliteValuePathSegment{index: index, isArray: true})
		default:
			return nil, fmt.Errorf("invalid SQLite value field path %q", path)
		}
	}
	return segments, nil
}

func sqliteValueFromField(field sqliteValueField) interface{} {
	switch field.Kind {
	case "s":
		return field.Text
	case "n":
		return field.Number
	case "b":
		return field.Bool
	case "m":
		return map[string]interface{}{}
	case "a":
		return []interface{}{}
	default:
		return nil
	}
}

func unflattenSQLiteValueFields(fields []sqliteValueField) (map[string]interface{}, error) {
	root := map[string]interface{}{}
	for _, field := range fields {
		segments, err := parseSQLiteValueFieldPath(field.Path)
		if err != nil {
			return nil, err
		}
		updated, err := setSQLiteValueField(root, segments, sqliteValueFromField(field))
		if err != nil {
			return nil, err
		}
		root = updated.(map[string]interface{})
	}
	return root, nil
}

func setSQLiteValueField(current interface{}, segments []sqliteValuePathSegment, value interface{}) (interface{}, error) {
	if len(segments) == 0 {
		return value, nil
	}
	segment := segments[0]
	if segment.isArray {
		array, _ := current.([]interface{})
		for len(array) <= segment.index {
			array = append(array, nil)
		}
		child, err := setSQLiteValueField(array[segment.index], segments[1:], value)
		if err != nil {
			return nil, err
		}
		array[segment.index] = child
		return array, nil
	}
	object, _ := current.(map[string]interface{})
	if object == nil {
		object = map[string]interface{}{}
	}
	child, err := setSQLiteValueField(object[segment.key], segments[1:], value)
	if err != nil {
		return nil, err
	}
	object[segment.key] = child
	return object, nil
}
