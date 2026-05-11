package main

import (
	"fmt"
	"strconv"
	"strings"
)

func parseScreenshotDisplayIndex(raw interface{}) (int, error) {
	switch v := raw.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case string:
		trimmed := strings.TrimSpace(v)
		displayIndex, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, fmt.Errorf("display 参数无效: %s", v)
		}
		return displayIndex, nil
	default:
		return 0, fmt.Errorf("display 参数类型无效: %T", raw)
	}
}
