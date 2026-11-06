package hopjit

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type hopStatement struct {
	kind       string
	target     string
	expression string
	line       int
	indent     int
	children   []*hopStatement
}

func validateHopPythonBody(body string) error {
	_, err := parseHopPython(body)
	return err
}

// StripHopPythonLineComment drops a # that sits outside quotes, including
// one that follows code on the same line. A # inside a string stays. The
// compiler uses this same cut when it decides a body is a retrieval, so the
// body it keeps is the body parseHopPython runs.
func StripHopPythonLineComment(line string) string {
	var quote rune
	escaped := false
	runes := []rune(line)
	for index, r := range runes {
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			continue
		}
		if r == '#' {
			return string(runes[:index])
		}
	}
	return line
}

// HopPythonRuns reports whether the interpreter can evaluate body.
// defined are names already assigned when the step starts. Every branch is
// checked. A tool call must use named arguments, and a name that is read on
// the evaluated side must already be assigned. The probe tool returns a
// non-empty string, matching a search result that lets or keep its left side.
func HopPythonRuns(body string, defined []string) error {
	statements, err := parseHopPython(body)
	if err != nil {
		return err
	}
	scope := make(map[string]bool, len(defined))
	for _, name := range defined {
		if name != "" {
			scope[name] = true
		}
	}
	return hopPythonBlockRuns(statements, scope)
}

func hopPythonBlockRuns(statements []*hopStatement, scope map[string]bool) error {
	for _, statement := range statements {
		switch statement.kind {
		case "assign":
			if err := hopPythonExpressionRuns(statement.expression, scope); err != nil {
				return fmt.Errorf("line %d: %w", statement.line, err)
			}
			scope[statement.target] = true
		case "if", "elif":
			if err := hopPythonExpressionRuns(statement.expression, scope); err != nil {
				return fmt.Errorf("line %d: %w", statement.line, err)
			}
			if err := hopPythonBlockRuns(statement.children, cloneBoolSet(scope)); err != nil {
				return err
			}
		case "else":
			if err := hopPythonBlockRuns(statement.children, cloneBoolSet(scope)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("line %d: unsupported statement %s", statement.line, statement.kind)
		}
	}
	return nil
}

func hopPythonExpressionRuns(expression string, scope map[string]bool) error {
	variables := make(map[string]VariableValue, len(scope))
	for name := range scope {
		variables[name] = VariableValue{Value: "probe", Assigned: true}
	}
	_, err := evalExpression(context.Background(), expression, variables, ToolProviderFunc(func(context.Context, string, map[string]any) (any, error) {
		return "probe", nil
	}))
	return err
}

func cloneBoolSet(scope map[string]bool) map[string]bool {
	out := make(map[string]bool, len(scope))
	for name := range scope {
		out[name] = true
	}
	return out
}

func executeHopPython(ctx context.Context, body string, variables map[string]VariableValue, tools ToolProvider) error {
	statements, err := parseHopPython(body)
	if err != nil {
		return err
	}
	return executeHopStatements(ctx, statements, variables, tools)
}

func parseHopPython(body string) ([]*hopStatement, error) {
	var flat []*hopStatement
	for index, raw := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		code := StripHopPythonLineComment(raw)
		if strings.TrimSpace(code) == "" {
			continue
		}
		if strings.ContainsRune(code, '\t') {
			return nil, fmt.Errorf("HOP_PYTHON_TAB: line %d uses tab indentation", index+1)
		}
		indent := len(code) - len(strings.TrimLeft(code, " "))
		text := strings.TrimSpace(code)
		statement := &hopStatement{line: index + 1, indent: indent}
		switch {
		case strings.HasPrefix(text, "if ") && strings.HasSuffix(text, ":"):
			statement.kind, statement.expression = "if", strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "if "), ":"))
		case strings.HasPrefix(text, "elif ") && strings.HasSuffix(text, ":"):
			statement.kind, statement.expression = "elif", strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "elif "), ":"))
		case text == "else:":
			statement.kind = "else"
		case strings.HasPrefix(text, "for ") || strings.HasPrefix(text, "while "):
			return nil, fmt.Errorf("HOP_PYTHON_LOOP_FORBIDDEN: line %d must use a HopSpec loop step", index+1)
		case strings.HasPrefix(text, "import ") || strings.HasPrefix(text, "from "):
			return nil, fmt.Errorf("HOP_PYTHON_IMPORT_FORBIDDEN: line %d", index+1)
		default:
			target, expression, ok := strings.Cut(text, "=")
			if !ok || strings.HasSuffix(strings.TrimSpace(target), "=") || strings.HasPrefix(strings.TrimSpace(expression), "=") {
				return nil, fmt.Errorf("HOP_PYTHON_ASSIGNMENT_REQUIRED: line %d", index+1)
			}
			statement.kind = "assign"
			statement.target = strings.TrimSpace(target)
			statement.expression = strings.TrimSpace(expression)
			if statement.target == "" || statement.expression == "" {
				return nil, fmt.Errorf("HOP_PYTHON_ASSIGNMENT_INVALID: line %d", index+1)
			}
		}
		flat = append(flat, statement)
	}
	if len(flat) > 10000 {
		return nil, errors.New("HOP_PYTHON_BODY_TOO_LARGE")
	}
	index := 0
	statements, err := parseHopBlock(flat, &index, 0)
	if err != nil {
		return nil, err
	}
	if index != len(flat) {
		return nil, errors.New("HOP_PYTHON_INDENT: inconsistent indentation")
	}
	return statements, nil
}

func parseHopBlock(flat []*hopStatement, index *int, indent int) ([]*hopStatement, error) {
	var out []*hopStatement
	for *index < len(flat) {
		statement := flat[*index]
		if statement.indent < indent {
			break
		}
		if statement.indent > indent {
			return nil, fmt.Errorf("HOP_PYTHON_INDENT: unexpected indentation at line %d", statement.line)
		}
		*index++
		if statement.kind == "if" || statement.kind == "elif" || statement.kind == "else" {
			if *index < len(flat) && flat[*index].indent > indent {
				children, err := parseHopBlock(flat, index, flat[*index].indent)
				if err != nil {
					return nil, err
				}
				statement.children = children
			} else {
				return nil, fmt.Errorf("HOP_PYTHON_EMPTY_BRANCH: line %d", statement.line)
			}
		}
		out = append(out, statement)
	}
	return out, nil
}

func executeHopStatements(ctx context.Context, statements []*hopStatement, variables map[string]VariableValue, tools ToolProvider) error {
	for index := 0; index < len(statements); index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		statement := statements[index]
		switch statement.kind {
		case "assign":
			value, err := evalExpression(ctx, statement.expression, variables, tools)
			if err != nil {
				return fmt.Errorf("line %d: %w", statement.line, err)
			}
			variables[statement.target] = VariableValue{Value: value, Assigned: true}
		case "if":
			branch := statement
			for {
				condition := true
				if branch.kind != "else" {
					value, err := evalExpression(ctx, branch.expression, variables, tools)
					if err != nil {
						return err
					}
					condition = truthy(value)
				}
				if condition {
					if err := executeHopStatements(ctx, branch.children, variables, tools); err != nil {
						return err
					}
				}
				if index+1 >= len(statements) || statements[index+1].kind != "elif" && statements[index+1].kind != "else" {
					break
				}
				index++
				branch = statements[index]
				if condition {
					for index+1 < len(statements) && (statements[index+1].kind == "elif" || statements[index+1].kind == "else") {
						index++
					}
					break
				}
			}
		case "elif", "else":
			return fmt.Errorf("line %d: %s without matching if", statement.line, statement.kind)
		default:
			return fmt.Errorf("line %d: unsupported statement %s", statement.line, statement.kind)
		}
	}
	return nil
}
