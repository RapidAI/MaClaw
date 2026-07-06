package main

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

type mobileServerProfilePayload struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	AuthMode string `json:"auth_mode"`
	Tag      string `json:"tag,omitempty"`
	Note     string `json:"note,omitempty"`
}

func (c *RemoteHubClient) publishMobileServerProfilesOnce() {
	if c == nil || c.app == nil {
		return
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return
	}
	profiles := mobileServerProfilesFromSSHHosts(cfg.SSHHosts)
	if profiles == nil {
		profiles = []mobileServerProfilePayload{}
	}
	if err := c.publishMobileServerProfiles(profiles); err != nil {
		log.Printf("[hub-client] mobile server profile publish failed: %v", err)
	}
}

func (c *RemoteHubClient) publishMobileServerProfiles(profiles []mobileServerProfilePayload) error {
	payload := map[string]any{"profiles": profiles}
	return c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPut, "/api/mobile/server-profiles", payload, nil)
}

func mobileServerProfilesFromSSHHosts(hosts []corelib.SSHHostEntry) []mobileServerProfilePayload {
	out := make([]mobileServerProfilePayload, 0, len(hosts))
	seen := map[string]struct{}{}
	for _, host := range hosts {
		if strings.TrimSpace(host.Host) == "" || strings.TrimSpace(host.User) == "" {
			continue
		}
		cfg := mobileBackendSSHHostConfig(host)
		id := strings.TrimSpace(host.Label)
		if id == "" {
			id = cfg.SSHHostID()
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		name := strings.TrimSpace(host.Label)
		if name == "" {
			name = cfg.SSHHostID()
		}
		out = append(out, mobileServerProfilePayload{
			ID:       id,
			Name:     name,
			Host:     strings.TrimSpace(host.Host),
			Port:     cfg.Port,
			Username: strings.TrimSpace(host.User),
			AuthMode: mobileServerProfileAuthMode(host),
			Tag:      "desktop",
			Note:     "Published from MaClaw desktop SSHHosts.",
		})
	}
	return out
}

func mobileServerProfileAuthMode(host corelib.SSHHostEntry) string {
	switch strings.ToLower(strings.TrimSpace(host.AuthMethod)) {
	case "key", "private_key", "private-key":
		return "private_key"
	case "agent":
		return "agent"
	case "password":
		return "password"
	default:
		cfg := remote.SSHHostConfig{
			Host:       strings.TrimSpace(host.Host),
			Port:       host.Port,
			User:       strings.TrimSpace(host.User),
			AuthMethod: strings.TrimSpace(host.AuthMethod),
			KeyPath:    strings.TrimSpace(host.KeyPath),
		}
		cfg.Defaults()
		if cfg.AuthMethod == "key" {
			return "private_key"
		}
		return cfg.AuthMethod
	}
}
