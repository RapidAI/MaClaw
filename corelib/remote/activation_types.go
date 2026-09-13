package remote

import (
	"crypto/rand"
	"fmt"
)

// RemoteActivationResult 远程激活结果。
type RemoteActivationResult struct {
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
	Code         string `json:"code,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Email        string `json:"email,omitempty"`
	SN           string `json:"sn,omitempty"`
	MachineID    string `json:"machine_id,omitempty"`
	MachineToken string `json:"machine_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	VIPFlag      bool   `json:"vip_flag,omitempty"`
}

// RemoteProbeResult 远程探测结果。
type RemoteProbeResult struct {
	InvitationCodeRequired bool   `json:"invitation_code_required"`
	Status                 string `json:"status,omitempty"`
	Message                string `json:"message,omitempty"`
}

// RemoteActivationStatus 远程激活状态。
type RemoteActivationStatus struct {
	Activated bool   `json:"activated"`
	Email     string `json:"email"`
	SN        string `json:"sn"`
	MachineID string `json:"machine_id"`
	HubURL    string `json:"hub_url"`
}

// RemoteHubCenterHub 描述一个 Hub Center 中的 Hub。
type RemoteHubCenterHub struct {
	HubID          string `json:"hub_id"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	Visibility     string `json:"visibility"`
	EnrollmentMode string `json:"enrollment_mode"`
	Status         string `json:"status"`
}

// GenerateClientID produces a UUID v4 string used to stably identify a desktop instance.
func GenerateClientID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40 // version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
