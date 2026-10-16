package qoder

import "fmt"

// uuidFormat renders 16 raw bytes as the canonical dashed form.
func uuidFormat(raw []byte) string {
	if len(raw) != 16 {
		return "maclaw-device"
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:])
}
