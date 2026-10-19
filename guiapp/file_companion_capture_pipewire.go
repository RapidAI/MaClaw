package guiapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// pipeWireOutputNode returns the PipeWire node id whose application.process.id
// equals pid. Input and source nodes are skipped so a microphone is not recorded.
// A node for any other process is ignored. This is not a PulseAudio sink monitor.
func pipeWireOutputNode(raw []byte, pid int) string {
	var nodes []map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &nodes); err != nil {
		return ""
	}
	fallback := ""
	for _, node := range nodes {
		if fmt.Sprint(node["type"]) != "PipeWire:Interface:Node" {
			continue
		}
		props := pipeWireProps(node)
		if !pipeWirePID(props, pid) {
			continue
		}
		class := fmt.Sprint(props["media.class"])
		if strings.Contains(class, "Input") || strings.Contains(class, "Source") {
			continue
		}
		id := pipeWireID(node["id"])
		if id == "" {
			continue
		}
		if strings.Contains(class, "Output") {
			return id
		}
		if fallback == "" {
			fallback = id
		}
	}
	return fallback
}

func pipeWireProps(node map[string]any) map[string]any {
	if info, ok := node["info"].(map[string]any); ok {
		if props, ok := info["props"].(map[string]any); ok {
			return props
		}
	}
	if props, ok := node["props"].(map[string]any); ok {
		return props
	}
	return map[string]any{}
}

func pipeWirePID(props map[string]any, pid int) bool {
	value, ok := props["application.process.id"]
	if !ok || value == nil {
		return false
	}
	switch n := value.(type) {
	case float64:
		return int(n) == pid
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(n))
		return err == nil && parsed == pid
	default:
		return fmt.Sprint(value) == strconv.Itoa(pid)
	}
}

func pipeWireID(value any) string {
	switch n := value.(type) {
	case float64:
		if n <= 0 {
			return ""
		}
		return strconv.Itoa(int(n))
	case string:
		return strings.TrimSpace(n)
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			return ""
		}
		return text
	}
}
