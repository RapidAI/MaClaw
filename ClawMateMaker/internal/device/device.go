// Package device discovers serial candidates without opening them.
package device

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"go.bug.st/serial/enumerator"
)

type Candidate struct {
	Port        string `json:"port"`
	Name        string `json:"name"`
	VendorID    string `json:"vendorId,omitempty"`
	ProductID   string `json:"productId,omitempty"`
	Serial      string `json:"serial,omitempty"`
	IsUSB       bool   `json:"isUsb"`
	LikelyEsp   bool   `json:"likelyEsp"`
	Description string `json:"description,omitempty"`
}

func ListCandidates() ([]Candidate, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, fmt.Errorf("enumerate serial ports: %w", err)
	}
	result := make([]Candidate, 0, len(ports))
	for _, p := range ports {
		c := Candidate{Port: p.Name, Name: p.Name, VendorID: strings.ToUpper(p.VID), ProductID: strings.ToUpper(p.PID), Serial: p.SerialNumber, IsUSB: p.IsUSB, Description: p.Product}
		c.LikelyEsp = isLikelyESP(c.VendorID, c.ProductID, p.Product)
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Port < result[j].Port })
	return result, nil
}

func isLikelyESP(vid, pid string, parts ...string) bool {
	vid = strings.ToUpper(vid)
	pid = strings.ToUpper(pid)
	if vid == "303A" {
		return true
	}
	for _, v := range parts {
		if strings.Contains(strings.ToLower(v), "espressif") || strings.Contains(strings.ToLower(v), "esp32") {
			return true
		}
	}
	// USB-UART bridge IDs are candidates, not identity proof.
	return (vid == "10C4" && pid == "EA60") || (vid == "1A86" && (pid == "7523" || pid == "55D4")) || (vid == "0403" && pid == "6001")
}

func Platform() string { return runtime.GOOS }
