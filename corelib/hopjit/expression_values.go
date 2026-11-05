package hopjit

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

func (p *expressionParser) callValue(name string, positional []any, named map[string]any) (any, error) {
	if p.skip > 0 {
		return nil, nil
	}
	if len(named) > 0 && isPureFunction(name) {
		return nil, fmt.Errorf("pure function %s requires positional arguments", name)
	}
	if isPureFunction(name) {
		return callPureFunction(name, positional)
	}
	if p.tools == nil {
		return nil, fmt.Errorf("UNKNOWN_FUNCTION: %s", name)
	}
	if len(positional) > 0 {
		return nil, fmt.Errorf("tool %s requires named arguments", name)
	}
	if err := p.ctx.Err(); err != nil {
		return nil, err
	}
	return p.tools.Execute(p.ctx, name, named)
}

func isPureFunction(name string) bool {
	switch name {
	case "len", "sum", "int", "float", "str", "bool", "lower", "upper", "strip", "parse_json", "startswith", "endswith", "get":
		return true
	default:
		return false
	}
}

func callPureFunction(name string, args []any) (any, error) {
	require := func(count int) error {
		if len(args) != count {
			return fmt.Errorf("%s expects %d arguments", name, count)
		}
		return nil
	}
	switch name {
	case "len":
		if err := require(1); err != nil {
			return nil, err
		}
		if text, ok := args[0].(string); ok {
			return len([]rune(text)), nil
		}
		value := reflect.ValueOf(args[0])
		if value.IsValid() && (value.Kind() == reflect.Array || value.Kind() == reflect.Slice || value.Kind() == reflect.Map) {
			return value.Len(), nil
		}
		return nil, errors.New("len expects text, list, or object")
	case "sum":
		if err := require(1); err != nil {
			return nil, err
		}
		items, ok := valueSlice(args[0])
		if !ok {
			return nil, errors.New("sum expects a list")
		}
		total := 0.0
		for _, item := range items {
			number, ok := numericValue(item)
			if !ok {
				return nil, errors.New("sum list must contain numbers")
			}
			total += number
		}
		return total, nil
	case "int":
		if err := require(1); err != nil {
			return nil, err
		}
		if number, ok := numericValue(args[0]); ok {
			return int64(number), nil
		}
		return strconv.ParseInt(strings.TrimSpace(fmt.Sprint(args[0])), 10, 64)
	case "float":
		if err := require(1); err != nil {
			return nil, err
		}
		if number, ok := numericValue(args[0]); ok {
			return number, nil
		}
		return strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(args[0])), 64)
	case "str":
		if err := require(1); err != nil {
			return nil, err
		}
		return fmt.Sprint(args[0]), nil
	case "bool":
		if err := require(1); err != nil {
			return nil, err
		}
		return truthy(args[0]), nil
	case "lower", "upper", "strip":
		if err := require(1); err != nil {
			return nil, err
		}
		text, ok := args[0].(string)
		if !ok {
			return nil, errors.New(name + " expects text")
		}
		if name == "lower" {
			return strings.ToLower(text), nil
		}
		if name == "upper" {
			return strings.ToUpper(text), nil
		}
		return strings.TrimSpace(text), nil
	case "parse_json":
		if err := require(1); err != nil {
			return nil, err
		}
		text, ok := args[0].(string)
		if !ok {
			return args[0], nil
		}
		var value any
		if err := json.Unmarshal([]byte(text), &value); err != nil {
			return nil, err
		}
		return value, nil
	case "startswith", "endswith":
		if err := require(2); err != nil {
			return nil, err
		}
		text, ok1 := args[0].(string)
		prefix, ok2 := args[1].(string)
		if !ok1 || !ok2 {
			return nil, errors.New(name + " expects text arguments")
		}
		if name == "startswith" {
			return strings.HasPrefix(text, prefix), nil
		}
		return strings.HasSuffix(text, prefix), nil
	case "get":
		if err := require(2); err != nil {
			return nil, err
		}
		return fieldValue(args[0], fmt.Sprint(args[1]))
	default:
		return nil, errors.New("unknown pure function")
	}
}

func arithmeticValue(left, right any, operator string) (any, error) {
	if operator == "+" {
		if a, ok := left.(string); ok {
			if b, ok := right.(string); ok {
				return a + b, nil
			}
		}
	}
	a, aOK := numericValue(left)
	b, bOK := numericValue(right)
	if !aOK || !bOK {
		return nil, fmt.Errorf("operator %s requires numbers", operator)
	}
	switch operator {
	case "+":
		return a + b, nil
	case "-":
		return a - b, nil
	case "*":
		return a * b, nil
	case "/":
		if b == 0 {
			return nil, errors.New("division by zero")
		}
		return a / b, nil
	case "%":
		leftInteger, rightInteger := int64(a), int64(b)
		if float64(leftInteger) != a || float64(rightInteger) != b {
			return nil, errors.New("modulo requires integers")
		}
		if rightInteger == 0 {
			return nil, errors.New("modulo by zero")
		}
		return float64(leftInteger % rightInteger), nil
	default:
		return nil, errors.New("unknown arithmetic operator")
	}
}

func fieldValue(value any, field string) (any, error) {
	if object, ok := value.(map[string]any); ok {
		return object[field], nil
	}
	if value == nil {
		return nil, nil
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Struct {
		fieldValue := reflected.FieldByName(field)
		if fieldValue.IsValid() && fieldValue.CanInterface() {
			return fieldValue.Interface(), nil
		}
	}
	return nil, fmt.Errorf("field %s is unavailable", field)
}

func indexValue(value, index any) (any, error) {
	if object, ok := value.(map[string]any); ok {
		return object[fmt.Sprint(index)], nil
	}
	items, ok := valueSlice(value)
	if !ok {
		return nil, errors.New("index requires list or object")
	}
	number, ok := numericValue(index)
	if !ok {
		return nil, errors.New("list index must be numeric")
	}
	position := int(number)
	if position < 0 {
		position += len(items)
	}
	if position < 0 || position >= len(items) {
		return nil, errors.New("list index out of range")
	}
	return items[position], nil
}
