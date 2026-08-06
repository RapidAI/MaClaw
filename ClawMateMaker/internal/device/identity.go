package device

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"go.bug.st/serial"
)

const maxIdentityFrameBytes = 4096

// ProtocolVersion is shared by every official release profile.  Keeping the
// value exported lets release CI assert that its build-time identity contract
// still matches the desktop parser before it publishes a package.
const ProtocolVersion = 2

// AppIdentity is untrusted application-state evidence. It improves automatic
// board matching on a booting device, but it can never prove physical board
// identity because the application image is itself replaceable.
type AppIdentity struct {
	Protocol              int    `json:"protocol"`
	FirmwareTargetBoardID string `json:"firmwareTargetBoardId"`
	LayoutID              string `json:"layoutId"`
	ProjectName           string `json:"projectName"`
	AppVersion            string `json:"appVersion"`
	Chip                  string `json:"chip"`
	FlashBytes            int64  `json:"flashBytes"`
	PSRAMBytes            int64  `json:"psramBytes"`
}

type identityFrame struct {
	Type                  string `json:"type"`
	Protocol              int    `json:"protocol"`
	Nonce                 string `json:"nonce"`
	FirmwareTargetBoardID string `json:"firmware_target_board_id"`
	LegacyBoardID         string `json:"board_id"`
	LayoutID              string `json:"layout_id"`
	ProjectName           string `json:"project_name"`
	AppVersion            string `json:"app_version"`
	LegacyFirmwareVersion string `json:"firmware_version"`
	Chip                  string `json:"chip"`
	FlashBytes            int64  `json:"flash_size_bytes"`
	PSRAMBytes            int64  `json:"psram_size_bytes"`
}

func ProbeApplicationIdentity(ctx context.Context, port string) (AppIdentity, error) {
	if !validSerialPort(port) {
		return AppIdentity{}, fmt.Errorf("unsafe serial port: %q", port)
	}
	p, err := serial.Open(port, &serial.Mode{BaudRate: 115200})
	if err != nil {
		return AppIdentity{}, fmt.Errorf("open application serial: %w", err)
	}
	defer p.Close()
	// Use the serial driver's timeout rather than spawning a goroutine for each
	// blocking ReadString call. A goroutine left behind after a timeout can
	// consume the next nonce-bound reply and make a later operation appear to
	// fail randomly; it also retains the port until a byte eventually arrives.
	if err := p.SetReadTimeout(300 * time.Millisecond); err != nil {
		return AppIdentity{}, fmt.Errorf("set application serial timeout: %w", err)
	}
	nonce, err := identityNonce()
	if err != nil {
		return AppIdentity{}, err
	}
	query := fmt.Sprintf("CLAWMATE_QUERY {\"type\":\"IDENTIFY\",\"nonce\":\"%s\"}\n", nonce)
	if _, err := p.Write([]byte(query)); err != nil {
		return AppIdentity{}, fmt.Errorf("send identity query: %w", err)
	}
	// A ROM probe hard-resets the target immediately before this call. Give the
	// application enough time to boot, bring up USB Serial/JTAG and run its
	// identity task before declaring automatic recognition unavailable.
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return AppIdentity{}, err
		}
		line, err := ReadBoundedLine(p, maxIdentityFrameBytes)
		if err != nil {
			if IsSerialReadTimeout(err) {
				continue
			}
			return AppIdentity{}, err
		}
		frame, err := parseIdentityFrame(line, nonce)
		if err == nil {
			return frame, nil
		}
	}
	return AppIdentity{}, errors.New("timed out waiting for matching protocol-v2 IDENTITY")
}

func parseIdentityFrame(line, nonce string) (AppIdentity, error) {
	raw, err := eventPayload(line, "IDENTITY")
	if err != nil {
		return AppIdentity{}, err
	}
	if len(raw) > maxIdentityFrameBytes {
		return AppIdentity{}, errors.New("not an identity event")
	}
	if err := rejectDuplicateKeys(raw); err != nil {
		return AppIdentity{}, fmt.Errorf("invalid identity JSON: %w", err)
	}
	var frame identityFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return AppIdentity{}, fmt.Errorf("decode identity event: %w", err)
	}
	if frame.Type != "IDENTITY" || frame.Nonce != nonce {
		return AppIdentity{}, errors.New("identity event does not match nonce-bound query")
	}
	// Protocol v1 names the target board and version differently. It remains
	// nonce-bound, so it is useful for automatically selecting a download for
	// existing field devices; it is never manufacturing identity and never
	// substitutes for the explicit pre-write confirmation or ROM checks.
	boardID := frame.FirmwareTargetBoardID
	appVersion := frame.AppVersion
	if frame.Protocol == 1 {
		boardID = frame.LegacyBoardID
		appVersion = frame.LegacyFirmwareVersion
	}
	if (frame.Protocol != 1 && frame.Protocol != ProtocolVersion) || boardID == "" {
		return AppIdentity{}, errors.New("identity event uses an unsupported protocol or lacks a board target")
	}
	return AppIdentity{Protocol: frame.Protocol, FirmwareTargetBoardID: boardID, LayoutID: frame.LayoutID, ProjectName: frame.ProjectName, AppVersion: appVersion, Chip: frame.Chip, FlashBytes: frame.FlashBytes, PSRAMBytes: frame.PSRAMBytes}, nil
}

// eventPayload tolerates an ESP-IDF log fragment preceding a single event.
// USB serial output is a byte stream, so a logger that does not end its line
// before printf() must not make a valid, nonce-bound event undiscoverable.
// The complete JSON value is still required to be the final non-space content.
func eventPayload(line, wantType string) ([]byte, error) {
	const prefix = "CLAWMATE_EVT "
	line = strings.TrimSpace(line)
	index := strings.Index(line, prefix)
	if index < 0 {
		return nil, errors.New("not a ClawMate event")
	}
	raw := []byte(strings.TrimSpace(line[index+len(prefix):]))
	if len(raw) == 0 {
		return nil, errors.New("event has no JSON payload")
	}
	// Type is checked again after strict duplicate-key validation below. Keep
	// wantType in this helper to make callers document their expected event.
	_ = wantType
	return raw, nil
}

// rejectDuplicateKeys prevents a malformed line from relying on JSON's
// implementation-specific duplicate-key behaviour. Device identity is only a
// convenience signal, but accepting an ambiguous identity is still unsafe.
func rejectDuplicateKeys(raw []byte) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	if err := walkJSON(d); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON data")
		}
		return err
	}
	return nil
}

func walkJSON(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	switch value := token.(type) {
	case json.Delim:
		if value == '{' {
			seen := map[string]bool{}
			for d.More() {
				keyToken, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("object key is not a string")
				}
				if seen[key] {
					return fmt.Errorf("duplicate key %q", key)
				}
				seen[key] = true
				if err := walkJSON(d); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		}
		if value == '[' {
			for d.More() {
				if err := walkJSON(d); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		}
	}
	return nil
}

func identityNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
