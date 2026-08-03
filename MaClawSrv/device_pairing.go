package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// A spoken pairing code is a short-lived, single-use bootstrap secret. It is
// never used as a gateway bearer token.
const srvDevicePairingTTL = 10 * time.Minute

type srvDevicePairingRecord struct {
	Principal agentservice.Principal
	Code      string
	ExpiresAt time.Time
}

type srvDevicePairingStore struct {
	mu    sync.Mutex
	codes map[string]srvDevicePairingRecord
}

func newSrvDevicePairingStore() *srvDevicePairingStore {
	return &srvDevicePairingStore{codes: map[string]srvDevicePairingRecord{}}
}

func (s *srvDevicePairingStore) create(p agentservice.Principal, now time.Time) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, fmt.Errorf("device pairing is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	for attempts := 0; attempts < 20; attempts++ {
		var raw [4]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", time.Time{}, err
		}
		value := (uint64(raw[0])<<24 | uint64(raw[1])<<16 | uint64(raw[2])<<8 | uint64(raw[3])) % 1000000
		code := fmt.Sprintf("%06d", value)
		if _, exists := s.codes[code]; exists {
			continue
		}
		expiresAt := now.Add(srvDevicePairingTTL)
		s.codes[code] = srvDevicePairingRecord{Principal: p, Code: code, ExpiresAt: expiresAt}
		return code, expiresAt, nil
	}
	return "", time.Time{}, fmt.Errorf("could not allocate pairing code")
}

func (s *srvDevicePairingStore) consume(code string, now time.Time) (srvDevicePairingRecord, bool) {
	if s == nil {
		return srvDevicePairingRecord{}, false
	}
	code = strings.TrimSpace(code)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	rec, ok := s.codes[code]
	if !ok || !rec.ExpiresAt.After(now) {
		return srvDevicePairingRecord{}, false
	}
	delete(s.codes, code)
	return rec, true
}

func (s *srvDevicePairingStore) pruneLocked(now time.Time) {
	for code, rec := range s.codes {
		if !rec.ExpiresAt.After(now) {
			delete(s.codes, code)
		}
	}
}

func (s *HTTPServer) handleCreateDevicePairing(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if s.devicePairings == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "device pairing is unavailable"})
		return
	}
	pairCode, expiresAt, err := s.devicePairings.create(p, time.Now().UTC())
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"pairCode": pairCode, "expires_at": expiresAt, "expires_in_seconds": int(srvDevicePairingTTL.Seconds())})
}

func (s *HTTPServer) handleDeviceGatewayPair(w http.ResponseWriter, r *http.Request) {
	limitKey := "device-pair:" + requestClientIP(r)
	now := time.Now().UTC()
	if s.devicePairLimit != nil {
		if allowed, retryAfter := s.devicePairLimit.AllowWithRetry(limitKey, now); !allowed {
			writeRateLimitError(w, retryAfter)
			return
		}
	}
	var in struct {
		ClientID string `json:"clientId"`
		PairCode string `json:"pairCode"`
		// Code preserves interoperability with already-flashed clients that
		// predate the explicit pairCode field. New clients send pairCode.
		Code string `json:"code"`
	}
	if !decodeThirdPartyGatewayJSON(w, r, &in) {
		return
	}
	clientID := coreDevicePairingID(in.ClientID)
	pairCode := strings.TrimSpace(in.PairCode)
	if pairCode == "" {
		pairCode = strings.TrimSpace(in.Code)
	}
	if clientID == "" || !isSixDigitCode(pairCode) {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", "clientId and a 6-digit pairCode are required")
		return
	}
	out, err := s.pairHardwareDevice(r.Context(), clientID, pairCode)
	if err != nil {
		if s.devicePairLimit != nil {
			if retryAfter := s.devicePairLimit.RegisterFailure(limitKey, now); retryAfter > 0 {
				writeRateLimitError(w, retryAfter)
				return
			}
		}
		writeThirdPartyGatewayError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	if s.devicePairLimit != nil {
		s.devicePairLimit.ResetFailures(limitKey)
	}
	writeJSON(w, http.StatusCreated, out)
}

// handleDeviceGatewayVoicePair accepts an unpaired device's WAV recording of a
// one-time pairing code. It has no bearer token by design; deploy it through
// HTTPS and rate-limit it at the reverse proxy.
func (s *HTTPServer) handleDeviceGatewayVoicePair(w http.ResponseWriter, r *http.Request) {
	limitKey := "device-pair-voice:" + requestClientIP(r)
	now := time.Now().UTC()
	if s.devicePairLimit != nil {
		if allowed, retryAfter := s.devicePairLimit.AllowWithRetry(limitKey, now); !allowed {
			writeRateLimitError(w, retryAfter)
			return
		}
	}
	clientID := coreDevicePairingID(r.Header.Get("X-MaClaw-Client-ID"))
	if clientID == "" {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", "X-MaClaw-Client-ID is required")
		return
	}
	wav, err := readASRWAVPayload(w, r)
	if err != nil {
		writeASRPayloadError(w, err)
		return
	}
	if s.aiModels == nil {
		writeThirdPartyGatewayError(w, http.StatusServiceUnavailable, "unavailable", "speech recognition is unavailable")
		return
	}
	transcript, err := s.aiModels.transcribeWAV(r.Context(), corelib.AppConfig{}, wav)
	if err != nil {
		s.writeAIModelRuntimeError(w, srvAIModelASR, corelib.AppConfig{}, err)
		return
	}
	code, ok := devicePairCodeFromTranscript(transcript)
	if !ok {
		if s.devicePairLimit != nil {
			if retryAfter := s.devicePairLimit.RegisterFailure(limitKey, now); retryAfter > 0 {
				writeRateLimitError(w, retryAfter)
				return
			}
		}
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_pair_code", "please speak exactly six digits")
		return
	}
	out, err := s.pairHardwareDevice(r.Context(), clientID, code)
	if err != nil {
		if s.devicePairLimit != nil {
			if retryAfter := s.devicePairLimit.RegisterFailure(limitKey, now); retryAfter > 0 {
				writeRateLimitError(w, retryAfter)
				return
			}
		}
		writeThirdPartyGatewayError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	if s.devicePairLimit != nil {
		s.devicePairLimit.ResetFailures(limitKey)
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *HTTPServer) pairHardwareDevice(ctx context.Context, clientID, code string) (map[string]any, error) {
	rec, ok := s.devicePairings.consume(code, time.Now().UTC())
	if !ok || subtle.ConstantTimeCompare([]byte(rec.Code), []byte(code)) != 1 {
		return nil, fmt.Errorf("invalid or expired pairing code")
	}
	cfg, err := s.svc.GetRawUserConfig(ctx, rec.Principal)
	if err != nil || cfg == nil {
		return nil, fmt.Errorf("owner configuration is unavailable")
	}
	before := cfg.AppConfig
	if strings.TrimSpace(cfg.AppConfig.ThirdPartyGatewayToken) == "" {
		cfg.AppConfig.ThirdPartyGatewayToken, err = randomThirdPartyGatewayToken()
		if err != nil {
			return nil, fmt.Errorf("could not create device credential")
		}
	}
	cfg.AppConfig.ThirdPartyGatewayEnabled = true
	if _, err := s.svc.UpdateUserConfig(ctx, rec.Principal, cfg.AppConfig); err != nil {
		return nil, fmt.Errorf("could not activate device gateway")
	}
	s.syncThirdPartyIMConfigTransition(rec.Principal, before, cfg.AppConfig)
	return map[string]any{"ok": true, "clientId": clientID, "gatewayToken": cfg.AppConfig.ThirdPartyGatewayToken, "protocolVersion": srvThirdPartyProtocolVersion}, nil
}

func isSixDigitCode(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 6 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func devicePairCodeFromTranscript(transcript string) (string, bool) {
	var digits strings.Builder
	for _, r := range strings.TrimSpace(transcript) {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
			continue
		}
		if digit, ok := spokenChineseDigit(r); ok {
			digits.WriteByte(digit)
		}
	}
	code := digits.String()
	return code, isSixDigitCode(code)
}

// spokenChineseDigit deliberately accepts only individual digit words, not
// numeric units such as 十/百. Pairing codes must be spoken one digit at a
// time, which avoids accepting an ambiguous ASR transcript as a credential.
func spokenChineseDigit(r rune) (byte, bool) {
	switch r {
	case '零', '〇':
		return '0', true
	case '一', '幺':
		return '1', true
	case '二', '两':
		return '2', true
	case '三':
		return '3', true
	case '四':
		return '4', true
	case '五':
		return '5', true
	case '六':
		return '6', true
	case '七':
		return '7', true
	case '八':
		return '8', true
	case '九':
		return '9', true
	default:
		return 0, false
	}
}

func coreDevicePairingID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 3 || len(value) > 64 {
		return ""
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return ""
		}
	}
	return value
}
