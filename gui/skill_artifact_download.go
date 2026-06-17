package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const skillArtifactDownloadMaxBytes int64 = 512 * 1024 * 1024

var skillArtifactDownloadSafeNameRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (a *App) DownloadSkillRunArtifact(runID, artifactRef string) (*SkillArtifactRegistryEntry, error) {
	return a.DownloadSkillRunArtifactForOwner("", runID, artifactRef)
}

func (a *App) DownloadSkillRunArtifactForOwner(ownerID, runID, artifactRef string) (*SkillArtifactRegistryEntry, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	entry, err := a.GetSkillRunArtifactForOwner(ownerID, runID, artifactRef)
	if err != nil {
		return nil, err
	}
	if entry.Available {
		return entry, nil
	}
	remoteURL := strings.TrimSpace(entry.RemoteURL)
	if remoteURL == "" {
		return nil, fmt.Errorf("artifact remote url is required")
	}
	allowPrivateHosts := strings.TrimSpace(a.testHomeDir) != ""
	parsed, err := parseAllowedSkillArtifactRemoteURL(remoteURL, allowPrivateHosts)
	if err != nil {
		return nil, err
	}
	cacheDir := filepath.Join(a.GetDataDir(), "skill_artifacts", "cache", safeSkillArtifactDownloadName(entry.RunID), safeSkillArtifactDownloadName(entry.ArtifactID))
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("artifact cache mkdir: %w", err)
	}
	fileName := safeSkillArtifactDownloadFileName(entry, parsed)
	destPath := filepath.Join(cacheDir, fileName)
	tmpPath := destPath + ".tmp"
	_ = os.Remove(tmpPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
	if err != nil {
		return nil, fmt.Errorf("artifact download request: %w", err)
	}
	client := &http.Client{
		Timeout:   10 * time.Minute,
		Transport: skillArtifactDownloadTransport(allowPrivateHosts),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("artifact download too many redirects")
			}
			if req == nil || req.URL == nil {
				return fmt.Errorf("artifact redirect url is required")
			}
			if _, err := parseAllowedSkillArtifactRemoteURL(req.URL.String(), allowPrivateHosts); err != nil {
				return err
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("artifact download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("artifact download failed: %s", resp.Status)
	}
	if resp.ContentLength > skillArtifactDownloadMaxBytes {
		return nil, fmt.Errorf("artifact download too large")
	}

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("artifact cache create: %w", err)
	}
	hasher := sha256.New()
	limited := io.LimitReader(resp.Body, skillArtifactDownloadMaxBytes+1)
	written, copyErr := io.Copy(io.MultiWriter(out, hasher), limited)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("artifact download copy: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("artifact cache close: %w", closeErr)
	}
	if written > skillArtifactDownloadMaxBytes {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("artifact download too large")
	}
	actualChecksum := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if err := verifySkillArtifactChecksum(entry.Checksum, actualChecksum); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	_ = os.Remove(destPath)
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("artifact cache rename: %w", err)
	}
	return a.UpdateSkillRunArtifactCacheForOwner(ownerID, entry.RunID, entry.URI, destPath, actualChecksum)
}

func skillArtifactDownloadTransport(allowPrivateHosts bool) *http.Transport {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	if allowPrivateHosts {
		return base
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if isBlockedSkillArtifactRemoteIP(ip.IP) {
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		}
		return nil, fmt.Errorf("artifact remote host resolves to private address")
	}
	return base
}

func parseAllowedSkillArtifactRemoteURL(rawURL string, allowPrivateHosts bool) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("artifact remote url invalid: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("artifact remote url scheme is not allowed")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return nil, fmt.Errorf("artifact remote url host is required")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("artifact remote url userinfo is not allowed")
	}
	if !allowPrivateHosts {
		host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
		if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
			return nil, fmt.Errorf("artifact remote url host is private")
		}
		if ip := net.ParseIP(host); ip != nil && isBlockedSkillArtifactRemoteIP(ip) {
			return nil, fmt.Errorf("artifact remote url host is private")
		}
	}
	return parsed, nil
}

func isBlockedSkillArtifactRemoteIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func safeSkillArtifactDownloadFileName(entry *SkillArtifactRegistryEntry, parsed *url.URL) string {
	name := strings.TrimSpace(entry.Name)
	if name == "" && parsed != nil {
		name = path.Base(parsed.Path)
	}
	if name == "" || name == "." || name == "/" {
		name = strings.TrimSpace(entry.ArtifactID)
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if base == "" {
		base = strings.TrimSpace(entry.ArtifactID)
	}
	base = safeSkillArtifactDownloadName(base)
	if base == "" {
		base = "artifact"
	}
	ext = skillArtifactDownloadSafeNameRe.ReplaceAllString(ext, "")
	if ext == "" && strings.TrimSpace(entry.ArtifactID) != "" {
		ext = filepath.Ext(entry.ArtifactID)
	}
	return base + ext
}

func safeSkillArtifactDownloadName(value string) string {
	value = strings.TrimSpace(value)
	value = skillArtifactDownloadSafeNameRe.ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-")
	if len(value) > 80 {
		value = value[:80]
	}
	return value
}

func verifySkillArtifactChecksum(expected, actual string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	actual = strings.ToLower(strings.TrimSpace(actual))
	if expected == "" {
		return nil
	}
	if strings.HasPrefix(expected, "sha256:") {
		expected = strings.TrimPrefix(expected, "sha256:")
	}
	if strings.HasPrefix(actual, "sha256:") {
		actual = strings.TrimPrefix(actual, "sha256:")
	}
	if len(expected) != 64 {
		return fmt.Errorf("artifact checksum format is unsupported")
	}
	if expected != actual {
		return fmt.Errorf("artifact checksum mismatch")
	}
	return nil
}
