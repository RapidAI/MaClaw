package hopjit

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

type expressionToken struct {
	kind  string
	text  string
	value any
}

func evalCondition(expression string, variables map[string]VariableValue) (bool, error) {
	value, err := evalExpression(context.Background(), expression, variables, nil)
	if err != nil {
		return false, err
	}
	return truthy(value), nil
}

func evalExpression(ctx context.Context, expression string, variables map[string]VariableValue, tools ToolProvider) (any, error) {
	tokens, err := lexExpression(expression)
	if err != nil {
		return nil, err
	}
	parser := &expressionParser{ctx: ctx, tokens: tokens, variables: variables, tools: tools}
	value, err := parser.parseOr()
	if err != nil {
		return nil, err
	}
	if parser.peek().kind != "eof" {
		return nil, fmt.Errorf("unexpected token %q", parser.peek().text)
	}
	return value, nil
}

func lexExpression(input string) ([]expressionToken, error) {
	var tokens []expressionToken
	runes := []rune(input)
	for index := 0; index < len(runes); {
		r := runes[index]
		if unicode.IsSpace(r) {
			index++
			continue
		}
		if strings.ContainsRune("()[]{},:.", r) {
			tokens = append(tokens, expressionToken{kind: string(r), text: string(r)})
			index++
			continue
		}
		if r == '\'' || r == '"' {
			value, next, err := lexExpressionString(runes, index)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, expressionToken{kind: "literal", text: value, value: value})
			index = next
			continue
		}
		if unicode.IsDigit(r) {
			start := index
			for index < len(runes) && unicode.IsDigit(runes[index]) {
				index++
			}
			if index < len(runes) && runes[index] == '.' {
				index++
				for index < len(runes) && unicode.IsDigit(runes[index]) {
					index++
				}
			}
			text := string(runes[start:index])
			number, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, expressionToken{kind: "literal", text: text, value: number})
			continue
		}
		if unicode.IsLetter(r) || r == '_' {
			start := index
			index++
			for index < len(runes) && (unicode.IsLetter(runes[index]) || unicode.IsDigit(runes[index]) || runes[index] == '_') {
				index++
			}
			tokens = append(tokens, expressionToken{kind: "identifier", text: string(runes[start:index])})
			continue
		}
		matched := ""
		for _, operator := range []string{"==", "!=", "<=", ">=", "<", ">", "+", "-", "*", "/", "%"} {
			if strings.HasPrefix(string(runes[index:]), operator) {
				matched = operator
				break
			}
		}
		if matched == "" {
			return nil, fmt.Errorf("unexpected character %q", r)
		}
		tokens = append(tokens, expressionToken{kind: "operator", text: matched})
		index += len([]rune(matched))
	}
	tokens = append(tokens, expressionToken{kind: "eof"})
	return tokens, nil
}

func lexExpressionString(runes []rune, start int) (string, int, error) {
	quote := runes[start]
	var out strings.Builder
	for index := start + 1; index < len(runes); index++ {
		r := runes[index]
		if r == quote {
			return out.String(), index + 1, nil
		}
		if r != '\\' {
			out.WriteRune(r)
			continue
		}
		index++
		if index >= len(runes) {
			break
		}
		escaped := runes[index]
		switch escaped {
		case 'n':
			out.WriteRune('\n')
		case 'r':
			out.WriteRune('\r')
		case 't':
			out.WriteRune('\t')
		default:
			out.WriteRune(escaped)
		}
	}
	return "", 0, errors.New("unterminated string literal")
}

type expressionParser struct {
	ctx       context.Context
	tokens    []expressionToken
	index     int
	variables map[string]VariableValue
	tools     ToolProvider
	skip      int
}

func (p *expressionParser) peek() expressionToken {
	return p.tokens[p.index]
}

func (p *expressionParser) take() expressionToken {
	token := p.tokens[p.index]
	p.index++
	return token
}

func (p *expressionParser) parseOr() (any, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == "identifier" && p.peek().text == "or" {
		p.take()
		if truthy(left) {
			p.skip++
			_, err = p.parseAnd()
			p.skip--
		} else {
			var right any
			right, err = p.parseAnd()
			left = truthy(right)
		}
		if err != nil {
			return nil, err
		}
	}
	return left, nil
}

func (p *expressionParser) parseAnd() (any, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == "identifier" && p.peek().text == "and" {
		p.take()
		if !truthy(left) {
			p.skip++
			_, err = p.parseNot()
			p.skip--
		} else {
			var right any
			right, err = p.parseNot()
			left = truthy(right)
		}
		if err != nil {
			return nil, err
		}
	}
	return left, nil
}

func (p *expressionParser) parseNot() (any, error) {
	if p.peek().kind == "identifier" && p.peek().text == "not" {
		p.take()
		value, err := p.parseNot()
		if err != nil || p.skip > 0 {
			return nil, err
		}
		return !truthy(value), nil
	}
	return p.parseComparison()
}

func (p *expressionParser) parseComparison() (any, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	operator := ""
	if p.peek().kind == "operator" && strings.Contains(" == != <= >= < > ", " "+p.peek().text+" ") {
		operator = p.take().text
	} else if p.peek().kind == "identifier" && p.peek().text == "is" {
		p.take()
		operator = "is"
		if p.peek().kind == "identifier" && p.peek().text == "not" {
			p.take()
			operator = "is not"
		}
	}
	if operator == "" {
		return left, nil
	}
	right, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	if p.skip > 0 {
		return nil, nil
	}
	switch operator {
	case "==", "is":
		return valuesEqual(left, right), nil
	case "!=", "is not":
		return !valuesEqual(left, right), nil
	case "<", "<=", ">", ">=":
		return compareValues(left, right, operator)
	default:
		return nil, fmt.Errorf("unsupported comparison %s", operator)
	}
}

func (p *expressionParser) parseAdditive() (any, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == "operator" && (p.peek().text == "+" || p.peek().text == "-") {
		operator := p.take().text
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		if p.skip > 0 {
			left = nil
			continue
		}
		left, err = arithmeticValue(left, right, operator)
		if err != nil {
			return nil, err
		}
	}
	return left, nil
}

func (p *expressionParser) parseMultiplicative() (any, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == "operator" && (p.peek().text == "*" || p.peek().text == "/" || p.peek().text == "%") {
		operator := p.take().text
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		if p.skip > 0 {
			left = nil
			continue
		}
		left, err = arithmeticValue(left, right, operator)
		if err != nil {
			return nil, err
		}
	}
	return left, nil
}

func (p *expressionParser) parseUnary() (any, error) {
	if p.peek().kind == "operator" && p.peek().text == "-" {
		p.take()
		value, err := p.parseUnary()
		if err != nil || p.skip > 0 {
			return nil, err
		}
		number, ok := numericValue(value)
		if !ok {
			return nil, errors.New("unary minus requires a number")
		}
		return -number, nil
	}
	return p.parsePrimary()
}

func (p *expressionParser) parsePrimary() (any, error) {
	value, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().kind {
		case ".":
			p.take()
			field := p.take()
			if field.kind != "identifier" {
				return nil, errors.New("field name is required after dot")
			}
			if p.skip > 0 {
				value = nil
				continue
			}
			value, err = fieldValue(value, field.text)
			if err != nil {
				return nil, err
			}
		case "[":
			p.take()
			index, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			if p.peek().kind != "]" {
				return nil, errors.New("missing closing bracket")
			}
			p.take()
			if p.skip > 0 {
				value = nil
				continue
			}
			value, err = indexValue(value, index)
			if err != nil {
				return nil, err
			}
		default:
			return value, nil
		}
	}
}

func (p *expressionParser) parseAtom() (any, error) {
	token := p.take()
	switch token.kind {
	case "literal":
		return token.value, nil
	case "identifier":
		switch token.text {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "None", "none", "null":
			return nil, nil
		default:
			if p.peek().kind == "(" {
				return p.parseCall(token.text)
			}
			value := p.variables[token.text]
			if !value.Assigned {
				return nil, nil
			}
			return value.Value, nil
		}
	case "(":
		value, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != ")" {
			return nil, errors.New("missing closing parenthesis")
		}
		p.take()
		return value, nil
	case "[":
		var values []any
		if p.peek().kind == "]" {
			p.take()
			return values, nil
		}
		for {
			value, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			values = append(values, value)
			if p.peek().kind == "]" {
				p.take()
				return values, nil
			}
			if p.peek().kind != "," {
				return nil, errors.New("expected comma in list literal")
			}
			p.take()
		}
	case "{":
		values := map[string]any{}
		if p.peek().kind == "}" {
			p.take()
			return values, nil
		}
		for {
			key := p.take()
			if key.kind != "literal" && key.kind != "identifier" {
				return nil, errors.New("object key must be a string or identifier")
			}
			if p.peek().kind != ":" {
				return nil, errors.New("expected colon after object key")
			}
			p.take()
			value, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			values[key.text] = value
			if p.peek().kind == "}" {
				p.take()
				return values, nil
			}
			if p.peek().kind != "," {
				return nil, errors.New("expected comma in object literal")
			}
			p.take()
		}
	default:
		return nil, fmt.Errorf("expected value, got %q", token.text)
	}
}

func (p *expressionParser) parseCall(name string) (any, error) {
	p.take()
	var positional []any
	named := map[string]any{}
	if p.peek().kind == ")" {
		p.take()
		return p.callValue(name, positional, named)
	}
	for {
		if p.peek().kind == "identifier" && p.tokens[p.index+1].kind == ":" {
			key := p.take().text
			p.take()
			value, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			named[key] = value
		} else {
			value, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			positional = append(positional, value)
		}
		if p.peek().kind == ")" {
			p.take()
			break
		}
		if p.peek().kind != "," {
			return nil, errors.New("expected comma in call arguments")
		}
		p.take()
	}
	return p.callValue(name, positional, named)
}

func valuesEqual(left, right any) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if a, ok := numericValue(left); ok {
		if b, ok := numericValue(right); ok {
			return a == b
		}
	}
	return reflect.DeepEqual(left, right)
}

func compareValues(left, right any, operator string) (bool, error) {
	if a, ok := numericValue(left); ok {
		b, ok := numericValue(right)
		if !ok {
			return false, errors.New("ordered comparison requires two numbers")
		}
		return compareOrdered(a, b, operator), nil
	}
	a, aOK := left.(string)
	b, bOK := right.(string)
	if !aOK || !bOK {
		return false, errors.New("ordered comparison requires numbers or strings")
	}
	return compareOrdered(a, b, operator), nil
}

func compareOrdered[T float64 | string](left, right T, operator string) bool {
	switch operator {
	case "<":
		return left < right
	case "<=":
		return left <= right
	case ">":
		return left > right
	default:
		return left >= right
	}
}

func numericValue(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int8:
		return float64(number), true
	case int16:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint8:
		return float64(number), true
	case uint16:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	case float32:
		return float64(number), true
	case float64:
		return number, true
	default:
		return 0, false
	}
}

func truthy(value any) bool {
	if value == nil {
		return false
	}
	if boolean, ok := value.(bool); ok {
		return boolean
	}
	if number, ok := numericValue(value); ok {
		return number != 0
	}
	valueOf := reflect.ValueOf(value)
	switch valueOf.Kind() {
	case reflect.String, reflect.Array, reflect.Slice, reflect.Map:
		return valueOf.Len() > 0
	default:
		return true
	}
}
