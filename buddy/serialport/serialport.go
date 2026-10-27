// Package serialport enumerates and opens the USB serial ports a MaClaw Buddy
// device can appear on.
package serialport

import (
	"fmt"
	"io"
	"strings"

	gobugserial "go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

// Port is the subset of a serial port the buddy service needs.
type Port interface {
	io.ReadWriteCloser
}

// Info describes one candidate port.
type Info struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	VID          string `json:"vid,omitempty"`
	PID          string `json:"pid,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	IsBuddy      bool   `json:"is_buddy"`
}

// List returns serial ports, flagging those whose USB ids match a known
// ESP32-S3 or USB-UART bridge.
func List() ([]Info, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(ports))
	for _, p := range ports {
		info := Info{
			Name:         p.Name,
			Description:  p.Product,
			VID:          strings.ToUpper(p.VID),
			PID:          strings.ToUpper(p.PID),
			SerialNumber: p.SerialNumber,
		}
		info.IsBuddy = isBuddyID(info.VID, info.PID)
		out = append(out, info)
	}
	return out, nil
}

// isBuddyID accepts Espressif native USB (USB-Serial/JTAG or CDC), CP210x and
// CH34x bridges.
func isBuddyID(vid, pid string) bool {
	vid = strings.ToUpper(strings.TrimSpace(vid))
	pid = strings.ToUpper(strings.TrimSpace(pid))
	if vid == "" {
		return false
	}
	if vid == "303A" { // Espressif
		return true
	}
	if vid == "10C4" && pid == "EA60" { // Silicon Labs CP210x
		return true
	}
	if vid == "1A86" { // WCH CH34x
		return true
	}
	return false
}

// Open opens a port at 115200 8N1 with DTR and RTS explicitly deasserted.
// Toggling either line resets some ESP32 boards, so the host must never rely
// on the driver defaults.
func Open(name string) (Port, error) {
	mode := &gobugserial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   gobugserial.NoParity,
		StopBits: gobugserial.OneStopBit,
		InitialStatusBits: &gobugserial.ModemOutputBits{
			DTR: false,
			RTS: false,
		},
	}
	port, err := gobugserial.Open(name, mode)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	return port, nil
}

// Dialer opens the first matching buddy port. It is defined here so the
// guiapp integration and the development CLI share one selection rule.
func Dialer(preferred string) (Port, func() string, error) {
	if strings.TrimSpace(preferred) != "" {
		port, err := Open(preferred)
		if err != nil {
			return nil, nil, err
		}
		return port, func() string { return preferred }, nil
	}
	infos, err := List()
	if err != nil {
		return nil, nil, err
	}
	for _, info := range infos {
		if info.IsBuddy {
			port, openErr := Open(info.Name)
			if openErr != nil {
				continue
			}
			name := info.Name
			return port, func() string { return name }, nil
		}
	}
	return nil, nil, fmt.Errorf("no buddy device found")
}
