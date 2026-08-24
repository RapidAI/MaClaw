package httpthreat

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// Same AES-256-GCM at-rest form as the existing audit/artifact store:
// enc:v1:<base64(nonce || ciphertext)>. Legacy plaintext rows stay readable.

var (
	errRestKey     = errors.New("httpthreat: corpus key required")
	errRestCorrupt = errors.New("httpthreat: corpus ciphertext corrupt")
)

type diskSample struct {
	Sample
	EmbedEnc string `json:"embed_enc,omitempty"`
}

func (e *Engine) loadRestKey() {
	if e == nil || strings.TrimSpace(e.dir) == "" {
		return
	}
	path := filepath.Join(e.dir, "corpus.key")
	if raw, err := os.ReadFile(path); err == nil && len(raw) == 32 {
		e.restKeyBytes = raw
		return
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return
	}
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		return
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return
	}
	e.restKeyBytes = key
}

func (e *Engine) restKey() []byte {
	if e == nil {
		return nil
	}
	return e.restKeyBytes
}

func sealBytes(key []byte, aad, plain []byte) (string, error) {
	if len(key) != 32 || len(plain) == 0 {
		return "", nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, plain, aad)
	return RestPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func openBytes(key []byte, aad []byte, stored string) ([]byte, error) {
	if !strings.HasPrefix(stored, RestPrefix) {
		return []byte(stored), nil
	}
	if len(key) != 32 {
		return nil, errRestKey
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, RestPrefix))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < aead.NonceSize() {
		return nil, errRestCorrupt
	}
	return aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], aad)
}

func floatsToBytes(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	out := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(x))
	}
	return out
}

func bytesToFloats(b []byte) []float32 {
	if len(b) < 4 || len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

func (e *Engine) sealSamples(samples []Sample) []diskSample {
	key := e.restKey()
	out := make([]diskSample, 0, len(samples))
	for _, s := range samples {
		row := diskSample{Sample: s}
		aad := []byte(s.TenantID + "\n" + s.ID)
		if key != nil && s.Preview != "" && !strings.HasPrefix(s.Preview, RestPrefix) {
			if enc, err := sealBytes(key, aad, []byte(s.Preview)); err == nil && enc != "" {
				row.Preview = enc
			}
		}
		if key != nil && len(s.Embedding) > 0 {
			if enc, err := sealBytes(key, append(aad, []byte("\nemb")...), floatsToBytes(s.Embedding)); err == nil && enc != "" {
				row.Embedding = nil
				row.EmbedEnc = enc
			}
		}
		out = append(out, row)
	}
	return out
}

func (e *Engine) openSamples(raw []byte) []Sample {
	var rows []diskSample
	if err := json.Unmarshal(raw, &rows); err != nil {
		var legacy []Sample
		if json.Unmarshal(raw, &legacy) == nil {
			return legacy
		}
		return nil
	}
	key := e.restKey()
	out := make([]Sample, 0, len(rows))
	for _, row := range rows {
		s := row.Sample
		aad := []byte(s.TenantID + "\n" + s.ID)
		if strings.HasPrefix(s.Preview, RestPrefix) {
			if plain, err := openBytes(key, aad, s.Preview); err == nil {
				s.Preview = string(plain)
			} else {
				s.Preview = ""
			}
		}
		if row.EmbedEnc != "" {
			if plain, err := openBytes(key, append(aad, []byte("\nemb")...), row.EmbedEnc); err == nil {
				s.Embedding = bytesToFloats(plain)
			}
		}
		out = append(out, s)
	}
	return out
}
