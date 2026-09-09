package database

import (
	"fmt"
	"strings"
)

// bindNamed converts :name placeholders to positional ? placeholders while
// skipping quoted strings, comments and PostgreSQL :: casts. Drivers that use
// numbered placeholders can apply their own final rewrite to the returned
// ordered arguments.
func bindNamed(sqlText string, params map[string]interface{}) (string, []interface{}, error) {
	return bindNamedDialect(sqlText, params, func(int) string { return "?" })
}

// bindNamedDialect is the shared scanner used by every adapter.  The
// placeholder callback is invoked only for a parsed :name token, so dialect
// operators such as PostgreSQL's bare `?` are never rewritten accidentally.
func bindNamedDialect(sqlText string, params map[string]interface{}, placeholder func(int) string) (string, []interface{}, error) {
	var b strings.Builder
	args := make([]interface{}, 0)
	used := make(map[string]struct{})
	for i := 0; i < len(sqlText); {
		if sqlText[i] == '\'' || sqlText[i] == '"' || sqlText[i] == '`' || sqlText[i] == '[' {
			q := sqlText[i]
			end := q
			if q == '[' {
				end = ']'
			}
			b.WriteByte(q)
			i++
			for i < len(sqlText) {
				b.WriteByte(sqlText[i])
				if sqlText[i] == end {
					if i+1 < len(sqlText) && sqlText[i+1] == end {
						b.WriteByte(sqlText[i+1])
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if sqlText[i] == '-' && i+1 < len(sqlText) && sqlText[i+1] == '-' {
			j := i
			for j < len(sqlText) && sqlText[j] != '\n' {
				b.WriteByte(sqlText[j])
				j++
			}
			i = j
			continue
		}
		if sqlText[i] == '/' && i+1 < len(sqlText) && sqlText[i+1] == '*' {
			j := i
			for j+1 < len(sqlText) && !(sqlText[j] == '*' && sqlText[j+1] == '/') {
				b.WriteByte(sqlText[j])
				j++
			}
			if j+1 < len(sqlText) {
				b.WriteString("*/")
				j += 2
			}
			i = j
			continue
		}
		if sqlText[i] == ':' && i+1 < len(sqlText) && sqlText[i+1] == ':' {
			b.WriteString("::")
			i += 2
			continue
		}
		if sqlText[i] == ':' && i+1 < len(sqlText) && isIdentStart(sqlText[i+1]) {
			j := i + 1
			for j < len(sqlText) && isIdentPart(sqlText[j]) {
				j++
			}
			name := sqlText[i+1 : j]
			v, ok := params[name]
			if !ok {
				return "", nil, fmt.Errorf("missing parameter: %s", name)
			}
			used[name] = struct{}{}
			b.WriteString(placeholder(len(args) + 1))
			args = append(args, v)
			i = j
			continue
		}
		b.WriteByte(sqlText[i])
		i++
	}
	for name := range params {
		if _, ok := used[name]; !ok {
			return "", nil, fmt.Errorf("unused parameter: %s", name)
		}
	}
	return b.String(), args, nil
}

func bindSQL(dialect, sqlText string, named map[string]interface{}, positional []interface{}, mode string) (string, []interface{}, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" || mode == "named" {
		if len(positional) > 0 {
			return "", nil, fmt.Errorf("syntax: positional params require parameter_mode=positional")
		}
		return bindForDialect(dialect, sqlText, named)
	}
	if mode != "positional" {
		return "", nil, fmt.Errorf("syntax: unknown parameter_mode %q", mode)
	}
	return bindPositional(dialect, sqlText, named, positional)
}

func bindPositional(dialect, sqlText string, named map[string]interface{}, positional []interface{}) (string, []interface{}, error) {
	if len(named) > 0 {
		return "", nil, fmt.Errorf("syntax: positional mode does not accept named params")
	}
	switch strings.ToLower(strings.TrimSpace(dialect)) {
	case "mysql", "sqlite", "excel", "":
	default:
		return "", nil, fmt.Errorf("syntax: positional parameters are not supported for %s", dialect)
	}
	var b strings.Builder
	args := make([]interface{}, 0, len(positional))
	index := 0
	for i := 0; i < len(sqlText); {
		if sqlText[i] == '\'' || sqlText[i] == '"' || sqlText[i] == '`' {
			q := sqlText[i]
			b.WriteByte(q)
			i++
			for i < len(sqlText) {
				b.WriteByte(sqlText[i])
				if sqlText[i] == q {
					if i+1 < len(sqlText) && sqlText[i+1] == q {
						b.WriteByte(sqlText[i+1])
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if sqlText[i] == '-' && i+1 < len(sqlText) && sqlText[i+1] == '-' {
			j := i
			for j < len(sqlText) && sqlText[j] != '\n' {
				b.WriteByte(sqlText[j])
				j++
			}
			i = j
			continue
		}
		if sqlText[i] == '/' && i+1 < len(sqlText) && sqlText[i+1] == '*' {
			j := i
			for j+1 < len(sqlText) && !(sqlText[j] == '*' && sqlText[j+1] == '/') {
				b.WriteByte(sqlText[j])
				j++
			}
			if j+1 < len(sqlText) {
				b.WriteString("*/")
				j += 2
			}
			i = j
			continue
		}
		if sqlText[i] == '?' {
			if index >= len(positional) {
				return "", nil, fmt.Errorf("missing parameter: positional %d", index+1)
			}
			b.WriteByte('?')
			args = append(args, positional[index])
			index++
			i++
			continue
		}
		b.WriteByte(sqlText[i])
		i++
	}
	if index != len(positional) {
		return "", nil, fmt.Errorf("unused parameter: positional extra %d", len(positional)-index)
	}
	return b.String(), args, nil
}

func isIdentStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isIdentPart(c byte) bool  { return isIdentStart(c) || c >= '0' && c <= '9' }
