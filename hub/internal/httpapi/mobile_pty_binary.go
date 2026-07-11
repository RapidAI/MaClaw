package httpapi

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MCP1 binary realtime frames for hub_exec PTY (Phase E full-duplex path).
//
// Layout:
//
//	[0..3]  magic "MCP1"
//	[4]     type: 1=pty_in  2=pty_out  3=pty_ack
//	[5]     flags: bit0=raw  bit1=ok  bit2=error
//	[6..7]  session_id length (uint16 BE)
//	[8..)   session_id (UTF-8)
//	[rest)  payload bytes
//
// Max frame size 20KiB (header + session + payload).

const (
	mobilePtyBinaryMagic0 = 'M'
	mobilePtyBinaryMagic1 = 'C'
	mobilePtyBinaryMagic2 = 'P'
	mobilePtyBinaryMagic3 = '1'

	mobilePtyBinaryTypeIn  byte = 1
	mobilePtyBinaryTypeOut byte = 2
	mobilePtyBinaryTypeAck byte = 3

	mobilePtyBinaryFlagRaw   byte = 1 << 0
	mobilePtyBinaryFlagOK    byte = 1 << 1
	mobilePtyBinaryFlagError byte = 1 << 2

	mobilePtyBinaryMaxFrame = 20 << 10
	mobilePtyBinaryMaxSID   = 256
)

type mobilePtyBinaryFrame struct {
	Type      byte
	Flags     byte
	SessionID string
	Payload   []byte
}

func (f mobilePtyBinaryFrame) Raw() bool   { return f.Flags&mobilePtyBinaryFlagRaw != 0 }
func (f mobilePtyBinaryFrame) OK() bool    { return f.Flags&mobilePtyBinaryFlagOK != 0 }
func (f mobilePtyBinaryFrame) Error() bool { return f.Flags&mobilePtyBinaryFlagError != 0 }

func mobilePtyBinaryIsMagic(data []byte) bool {
	return len(data) >= 4 &&
		data[0] == mobilePtyBinaryMagic0 &&
		data[1] == mobilePtyBinaryMagic1 &&
		data[2] == mobilePtyBinaryMagic2 &&
		data[3] == mobilePtyBinaryMagic3
}

func mobilePtyBinaryEncode(frame mobilePtyBinaryFrame) ([]byte, error) {
	sid := strings.TrimSpace(frame.SessionID)
	if len(sid) > mobilePtyBinaryMaxSID {
		return nil, fmt.Errorf("session_id too long")
	}
	if frame.Type == 0 {
		return nil, fmt.Errorf("type required")
	}
	payload := frame.Payload
	if payload == nil {
		payload = []byte{}
	}
	total := 8 + len(sid) + len(payload)
	if total > mobilePtyBinaryMaxFrame {
		return nil, fmt.Errorf("frame too large (%d > %d)", total, mobilePtyBinaryMaxFrame)
	}
	out := make([]byte, total)
	out[0] = mobilePtyBinaryMagic0
	out[1] = mobilePtyBinaryMagic1
	out[2] = mobilePtyBinaryMagic2
	out[3] = mobilePtyBinaryMagic3
	out[4] = frame.Type
	out[5] = frame.Flags
	binary.BigEndian.PutUint16(out[6:8], uint16(len(sid)))
	copy(out[8:], sid)
	copy(out[8+len(sid):], payload)
	return out, nil
}

func mobilePtyBinaryDecode(data []byte) (mobilePtyBinaryFrame, error) {
	var zero mobilePtyBinaryFrame
	if !mobilePtyBinaryIsMagic(data) {
		return zero, fmt.Errorf("not MCP1 frame")
	}
	if len(data) < 8 {
		return zero, fmt.Errorf("frame too short")
	}
	if len(data) > mobilePtyBinaryMaxFrame {
		return zero, fmt.Errorf("frame too large")
	}
	typ := data[4]
	flags := data[5]
	sidLen := int(binary.BigEndian.Uint16(data[6:8]))
	if sidLen < 0 || 8+sidLen > len(data) {
		return zero, fmt.Errorf("invalid session_id length")
	}
	sidBytes := data[8 : 8+sidLen]
	if sidLen > 0 && !utf8.Valid(sidBytes) {
		return zero, fmt.Errorf("session_id must be UTF-8")
	}
	payload := data[8+sidLen:]
	// Copy payload so callers can keep after buffer reuse.
	var payCopy []byte
	if len(payload) > 0 {
		payCopy = make([]byte, len(payload))
		copy(payCopy, payload)
	}
	return mobilePtyBinaryFrame{
		Type:      typ,
		Flags:     flags,
		SessionID: string(sidBytes),
		Payload:   payCopy,
	}, nil
}

func mobilePtyBinaryEncodeInput(sessionID string, payload []byte, raw bool) ([]byte, error) {
	flags := byte(0)
	if raw {
		flags |= mobilePtyBinaryFlagRaw
	}
	return mobilePtyBinaryEncode(mobilePtyBinaryFrame{
		Type:      mobilePtyBinaryTypeIn,
		Flags:     flags,
		SessionID: sessionID,
		Payload:   payload,
	})
}

func mobilePtyBinaryEncodeOutput(sessionID string, chunk []byte) ([]byte, error) {
	return mobilePtyBinaryEncode(mobilePtyBinaryFrame{
		Type:      mobilePtyBinaryTypeOut,
		Flags:     0,
		SessionID: sessionID,
		Payload:   chunk,
	})
}

func mobilePtyBinaryEncodeAck(sessionID string, ok bool, errMsg string) ([]byte, error) {
	flags := mobilePtyBinaryFlagOK
	var payload []byte
	if !ok {
		flags = mobilePtyBinaryFlagError
		payload = []byte(errMsg)
	}
	return mobilePtyBinaryEncode(mobilePtyBinaryFrame{
		Type:      mobilePtyBinaryTypeAck,
		Flags:     flags,
		SessionID: sessionID,
		Payload:   payload,
	})
}
