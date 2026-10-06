// Package desktop is the shared desktop contract used by the Docker service,
// Hub, and MaClawSrv. The Docker service process stays in its own directory.
package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	DefaultImage   = "maclaw-gui:1"
	DefaultMemory  = "2500m"
	DefaultCPUs    = "1.5"
	DefaultShmSize = "512m"
	// ProxyPort is the container port published for the per-user CDP proxy.
	// Display :20 maps to this port inside the image.
	ProxyPort = "19020"
	// VNCPort is the container port for the noVNC page used when a person
	// has to log in or solve a captcha.
	VNCPort = "6080"
	// MaxAppArgs is one batched xdotool invocation. Sixteen steps of the
	// longest app action stay under this limit.
	MaxAppArgs = 128
)

// ErrInvalid means the desktop identity or resource request cannot be used.
var ErrInvalid = errors.New("invalid desktop request")

// Resources are the container limits applied when a desktop is created.
type Resources struct {
	Image   string
	Memory  string
	CPUs    string
	ShmSize string
}

// EnsureStatus is the one-line JSON the in-container supervisor prints.
type EnsureStatus struct {
	Display string
	CDPPort int
	Token   string
}

// Mount is one private volume for a user's desktop data.
type Mount struct {
	Volume string
	Target string
}

// PrivateMounts are the volumes that hold one user's desktop and nobody else's.
// Browser logins live on /desktops. App logins live in the home directory.
// Software installed under /opt or /usr/local stays on those volumes.
func PrivateMounts(tenantID, userID string) ([]Mount, error) {
	key, err := UserKey(tenantID, userID)
	if err != nil {
		return nil, err
	}
	return []Mount{
		{Volume: "maclaw-desktops-" + key, Target: "/desktops"},
		{Volume: "maclaw-home-" + key, Target: "/home/desktop"},
		{Volume: "maclaw-opt-" + key, Target: "/opt"},
		{Volume: "maclaw-local-" + key, Target: "/usr/local"},
	}, nil
}

// StateImage is the private image that keeps packages installed into this
// user's container. It is never the shared desktop image.
func StateImage(tenantID, userID string) (string, error) {
	key, err := UserKey(tenantID, userID)
	if err != nil {
		return "", err
	}
	return "maclaw-desktop-user-" + key + ":state", nil
}

// UserKey is the desktop identity for one account. Instance id is not part
// of the key: every agent instance of this user shares the desktop.
func UserKey(tenantID, userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", fmt.Errorf("%w: user is required", ErrInvalid)
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(tenantID) + "\x00" + userID))
	return hex.EncodeToString(sum[:8]), nil
}

func ValidUserKey(key string) bool {
	return userKeyPattern.MatchString(key)
}

func ValidUserID(raw string) bool {
	return userPattern.MatchString(strings.TrimSpace(raw))
}

func ValidDisplay(display string) bool {
	return displayPattern.MatchString(strings.TrimSpace(display))
}

// NormalizeResources fills defaults and checks image, memory, cpu, and shm.
func NormalizeResources(image, memory, cpus, shm string) (Resources, error) {
	if strings.TrimSpace(image) == "" {
		image = DefaultImage
	}
	if strings.TrimSpace(memory) == "" {
		memory = DefaultMemory
	}
	if strings.TrimSpace(cpus) == "" {
		cpus = DefaultCPUs
	}
	if strings.TrimSpace(shm) == "" {
		shm = DefaultShmSize
	}
	var err error
	if image, err = NormalizeImage(image); err != nil {
		return Resources{}, err
	}
	if memory, err = NormalizeMemory(memory); err != nil {
		return Resources{}, err
	}
	if cpus, err = NormalizeCPUs(cpus); err != nil {
		return Resources{}, err
	}
	if shm, err = NormalizeMemory(shm); err != nil {
		return Resources{}, fmt.Errorf("%w: shm_size is invalid", ErrInvalid)
	}
	return Resources{Image: image, Memory: memory, CPUs: cpus, ShmSize: shm}, nil
}

func NormalizeImage(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !imagePattern.MatchString(raw) {
		return "", fmt.Errorf("%w: image is invalid", ErrInvalid)
	}
	return raw, nil
}

func NormalizeMemory(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if !memoryPattern.MatchString(raw) {
		return "", fmt.Errorf("%w: memory is invalid", ErrInvalid)
	}
	return raw, nil
}

func NormalizeCPUs(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 || value > 64 {
		return "", fmt.Errorf("%w: cpus is invalid", ErrInvalid)
	}
	return strconv.FormatFloat(value, 'f', -1, 64), nil
}

// ParseEnsure reads the supervisor's JSON status line.
func ParseEnsure(out string) (EnsureStatus, error) {
	var status EnsureStatus
	found := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var payload struct {
			Display string `json:"display"`
			CDPPort int    `json:"cdp_port"`
			Token   string `json:"token"`
		}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			return EnsureStatus{}, fmt.Errorf("%w: desktop supervisor returned invalid status", ErrInvalid)
		}
		status = EnsureStatus{Display: payload.Display, CDPPort: payload.CDPPort, Token: payload.Token}
		found = true
	}
	if !found || !ValidDisplay(status.Display) || status.CDPPort <= 0 || status.CDPPort > 65535 {
		return EnsureStatus{}, fmt.Errorf("%w: desktop supervisor returned no endpoint", ErrInvalid)
	}
	// An older image without token gates reports no token; callers fall back
	// to the legacy tokenless URL in that case.
	if status.Token != "" && !ValidGateToken(status.Token) {
		return EnsureStatus{}, fmt.Errorf("%w: desktop supervisor returned an invalid token", ErrInvalid)
	}
	return status, nil
}

// ValidGateToken matches the hex tokens the in-container supervisor issues for
// its CDP and noVNC gates.
func ValidGateToken(token string) bool {
	return gateTokenPattern.MatchString(token)
}

var (
	imagePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,200}$`)
	memoryPattern  = regexp.MustCompile(`(?i)^[1-9][0-9]{0,6}([kmg]i?b?)?$`)
	userPattern    = regexp.MustCompile(`^[A-Za-z0-9_.:@+-]{1,200}$`)
	userKeyPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)
	gateTokenPattern = regexp.MustCompile(`^[0-9a-f]{16,64}$`)
	displayPattern = regexp.MustCompile(`^:[0-9]{1,3}$`)
)
