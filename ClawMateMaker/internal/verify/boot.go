// Package verify validates post-flash application boot evidence.
package verify

import (
	"bufio"
	"bytes"
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

const MaxFrameBytes = 4096

type Expectation struct {
	BoardID           string
	LayoutID          string
	ReleaseSequence   int64
	ProjectName       string
	AppVersion        string
	AppELFSHA256      string
	Chip              string
	FlashBytes        int64
	PSRAMBytes        int64
	RequiredSelfTests []string
}

type Status struct {
	Type                  string            `json:"type"`
	Protocol              int               `json:"protocol"`
	Nonce                 string            `json:"nonce"`
	Ready                 bool              `json:"ready"`
	FirmwareTargetBoardID string            `json:"firmware_target_board_id"`
	LayoutID              string            `json:"layout_id"`
	ReleaseSequence       int64             `json:"release_sequence"`
	ProjectName           string            `json:"project_name"`
	AppVersion            string            `json:"app_version"`
	AppELFSHA256          string            `json:"app_elf_sha256"`
	Chip                  string            `json:"chip"`
	FlashBytes            int64             `json:"flash_size_bytes"`
	PSRAMBytes            int64             `json:"psram_size_bytes"`
	SelfTest              map[string]string `json:"self_test"`
}

type Result struct {
	NonceID  string `json:"nonceId"`
	Status   Status `json:"status"`
	Attempts int    `json:"attempts"`
}

func NewNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func NonceID(nonce string) string {
	if len(nonce) < 12 {
		return "redacted"
	}
	return nonce[:6] + "…" + nonce[len(nonce)-6:]
}

// ParseFrame only accepts the formal one-line protocol-v2 BOOT_STATUS shape.
// No legacy BOOT_OK event, broadcast without nonce, or unknown protocol can pass.
func ParseFrame(line string, expectedNonce string, expected Expectation) (Status, error) {
	line = strings.TrimSpace(line)
	if len(line) > MaxFrameBytes {
		return Status{}, errors.New("protocol frame exceeds 4 KiB")
	}
	const prefix = "CLAWMATE_EVT "
	if !strings.HasPrefix(line, prefix) {
		return Status{}, errors.New("not a ClawMate event")
	}
	raw := []byte(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
	if err := rejectDuplicateKeys(raw); err != nil {
		return Status{}, err
	}
	var s Status
	if err := json.Unmarshal(raw, &s); err != nil {
		return Status{}, fmt.Errorf("decode boot status: %w", err)
	}
	if s.Type != "BOOT_STATUS" || s.Protocol != 2 || s.Nonce != expectedNonce || !s.Ready {
		return Status{}, errors.New("event is not a ready matching BOOT_STATUS v2")
	}
	if s.FirmwareTargetBoardID != expected.BoardID || s.LayoutID != expected.LayoutID || s.ReleaseSequence != expected.ReleaseSequence || s.ProjectName != expected.ProjectName || s.AppVersion != expected.AppVersion || s.AppELFSHA256 != expected.AppELFSHA256 || !strings.EqualFold(s.Chip, expected.Chip) || s.FlashBytes != expected.FlashBytes {
		return Status{}, errors.New("boot status identity does not match the frozen firmware plan")
	}
	if expected.PSRAMBytes > 0 && s.PSRAMBytes < expected.PSRAMBytes {
		return Status{}, errors.New("boot status PSRAM capacity is below package requirement")
	}
	for _, name := range expected.RequiredSelfTests {
		if s.SelfTest[name] != "ok" {
			return Status{}, fmt.Errorf("required self-test %s is not ok", name)
		}
	}
	return s, nil
}

// Wait opens the application serial endpoint after the ROM session is closed,
// issues fresh nonce queries, records every incoming line through appendRaw,
// and succeeds only with a fully matched v2 response.
func Wait(ctx context.Context, port string, baud int, timeout time.Duration, expected Expectation, appendRaw func(string) error) (Result, error) {
	if baud <= 0 || timeout <= 0 {
		return Result{}, errors.New("invalid boot verification transport settings")
	}
	p, err := serial.Open(port, &serial.Mode{BaudRate: baud})
	if err != nil {
		return Result{}, fmt.Errorf("open application serial: %w", err)
	}
	defer p.Close()
	deadline := time.Now().Add(timeout)
	reader := bufio.NewReaderSize(p, MaxFrameBytes+1)
	attempt := 0
	for time.Now().Before(deadline) {
		nonce, err := NewNonce()
		if err != nil {
			return Result{}, err
		}
		attempt++
		query := fmt.Sprintf("CLAWMATE_QUERY {\"type\":\"BOOT_STATUS\",\"nonce\":\"%s\"}\n", nonce)
		if _, err := p.Write([]byte(query)); err != nil {
			return Result{}, fmt.Errorf("send boot-status query: %w", err)
		}
		queryUntil := time.Now().Add(3 * time.Second)
		for time.Now().Before(queryUntil) && time.Now().Before(deadline) {
			line, readErr := readLineWithContext(ctx, reader, 500*time.Millisecond)
			if readErr != nil {
				if errors.Is(readErr, context.DeadlineExceeded) || errors.Is(readErr, io.EOF) {
					continue
				}
				return Result{}, readErr
			}
			if appendRaw != nil {
				_ = appendRaw(line + "\n")
			}
			status, parseErr := ParseFrame(line, nonce, expected)
			if parseErr == nil {
				return Result{NonceID: NonceID(nonce), Status: status, Attempts: attempt}, nil
			}
		}
	}
	return Result{}, errors.New("timed out waiting for matching BOOT_STATUS v2")
}

func readLineWithContext(parent context.Context, r *bufio.Reader, wait time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(parent, wait)
	defer cancel()
	type answer struct {
		line string
		err  error
	}
	ch := make(chan answer, 1)
	go func() { line, err := r.ReadString('\n'); ch <- answer{line, err} }()
	select {
	case answer := <-ch:
		return answer.line, answer.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func rejectDuplicateKeys(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := walkJSON(d); err != nil {
		return fmt.Errorf("invalid protocol JSON: %w", err)
	}
	if d.More() {
		return errors.New("trailing JSON data")
	}
	return nil
}
func walkJSON(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	switch v := token.(type) {
	case json.Delim:
		if v == '{' {
			seen := map[string]bool{}
			for d.More() {
				keyToken, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("object key is not string")
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
		if v == '[' {
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
