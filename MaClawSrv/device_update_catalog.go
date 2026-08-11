package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

// The catalog is deliberately metadata-only.  The Hub may discover a release
// from GitHub, but a device never receives an asset URL and never downloads or
// writes firmware.  Only ClawMate Maker verifies and installs .clawfw files.
const (
	srvDeviceUpdateCatalogFile = "device_update_catalog.json"
	srvDeviceUpdateChannel     = "stable"
	// Schema 2 is a Hub-owned, verified GitHub Release snapshot.  The former
	// operator-authored schema 1 has no cryptographic provenance and is rejected
	// rather than becoming an accidental production trust bypass.
	srvDeviceUpdateCatalogSchemaVersion = 2
)

type srvDeviceFirmwareBinding struct {
	DeviceID        string `json:"deviceId"`
	TenantID        string `json:"tenantId"`
	UserID          string `json:"userId"`
	CredentialEpoch int64  `json:"credentialEpoch"`
	BoardID         string `json:"boardId"`
	HardwareRev     string `json:"hardwareRev"`
	LayoutID        string `json:"layoutId"`
	CompatibilityID string `json:"compatibilityId"`
	PairedAt        int64  `json:"pairedAt"`
	LastSeenAt      int64  `json:"lastSeenAt"`
}

type srvDeviceUpdateRelease struct {
	ProductID           string `json:"productId"`
	BoardID             string `json:"boardId"`
	HardwareRev         string `json:"hardwareRev"`
	LayoutID            string `json:"layoutId"`
	CompatibilityID     string `json:"compatibilityId"`
	Channel             string `json:"channel"`
	ReleaseSequence     int64  `json:"releaseSequence"`
	DisplayVersion      string `json:"displayVersion"`
	ReleaseTag          string `json:"releaseTag"`
	PublishedAt         int64  `json:"publishedAt"`
	Severity            string `json:"severity"`
	Critical            bool   `json:"critical"`
	MinimumMakerVersion string `json:"minimumMakerVersion"`
	PackageID           string `json:"packageId"`
	ManifestSHA256      string `json:"manifestSha256"`
	NotesSummary        string `json:"notesSummary"`
	NotesSHA256         string `json:"notesSha256"`
	CheckAfterSeconds   int    `json:"checkAfterSeconds"`
	Withdrawn           bool   `json:"withdrawn"`
	// Hub-only provenance. It must never be copied into a device handshake.
	ArchiveSHA256   string `json:"archiveSha256,omitempty"`
	SourceAssetID   int64  `json:"sourceAssetId,omitempty"`
	SourceAssetName string `json:"sourceAssetName,omitempty"`
	SourceAssetSize int64  `json:"sourceAssetSize,omitempty"`
}

type srvDeviceUpdateCatalogDocument struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Releases      []srvDeviceUpdateRelease `json:"releases"`
	// Schema 2 provenance; schema 1 does not have this information.
	Source        string `json:"source,omitempty"`
	Repository    string `json:"repository,omitempty"`
	ReleaseID     int64  `json:"releaseId,omitempty"`
	ReleaseTag    string `json:"releaseTag,omitempty"`
	VerifiedAt    int64  `json:"verifiedAt,omitempty"`
	MaxAgeSeconds int    `json:"maxAgeSeconds,omitempty"`
}

// srvDeviceUpdateCatalog is intentionally an operator-controlled local cache,
// not a client-writable API.  A future GitHub provider may atomically replace
// this file only after checking an allow-listed, signed .clawfw manifest.
// Until that verifier exists in Hub, the catalog is fail-closed: malformed,
// untrusted or missing data simply yields no update notification.
type srvDeviceUpdateCatalog struct {
	path string
	mu   sync.Mutex
}

func newSrvDeviceUpdateCatalog(dataRoot string) *srvDeviceUpdateCatalog {
	return &srvDeviceUpdateCatalog{path: filepath.Join(dataRoot, srvDeviceUpdateCatalogFile)}
}

func (c *srvDeviceUpdateCatalog) latestFor(identity coreim.FirmwareIdentity) (srvDeviceUpdateRelease, bool) {
	if c == nil || !validSrvFirmwareIdentity(identity) {
		return srvDeviceUpdateRelease{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	document, ok := c.documentLocked()
	if !ok || !srvCatalogDocumentFresh(document, time.Now().UTC()) {
		return srvDeviceUpdateRelease{}, false
	}
	var latest srvDeviceUpdateRelease
	for _, release := range document.Releases {
		if !validSrvDeviceUpdateRelease(release) || release.Withdrawn ||
			release.ProductID != identity.ProductID || release.BoardID != identity.BoardID ||
			release.HardwareRev != identity.HardwareRev || release.LayoutID != identity.LayoutID ||
			release.CompatibilityID != identity.CompatibilityID || release.Channel != srvDeviceUpdateChannel ||
			release.ReleaseSequence <= identity.ReleaseSequence {
			continue
		}
		if release.ReleaseSequence > latest.ReleaseSequence {
			latest = release
		}
	}
	return latest, latest.ReleaseSequence > 0
}

func (c *srvDeviceUpdateCatalog) document() (srvDeviceUpdateCatalogDocument, bool) {
	if c == nil {
		return srvDeviceUpdateCatalogDocument{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.documentLocked()
}

func (c *srvDeviceUpdateCatalog) documentLocked() (srvDeviceUpdateCatalogDocument, bool) {
	raw, err := os.ReadFile(c.path)
	if err != nil || len(raw) == 0 || len(raw) > 1024*1024 {
		return srvDeviceUpdateCatalogDocument{}, false
	}
	var document srvDeviceUpdateCatalogDocument
	if err := json.Unmarshal(raw, &document); err != nil || document.SchemaVersion != srvDeviceUpdateCatalogSchemaVersion {
		return srvDeviceUpdateCatalogDocument{}, false
	}
	return document, true
}

func (c *srvDeviceUpdateCatalog) replaceTrusted(document srvDeviceUpdateCatalogDocument) error {
	if c == nil {
		return errors.New("device update catalog is unavailable")
	}
	if err := validateSrvTrustedCatalogDocument(document, time.Now().UTC()); err != nil {
		return err
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func srvCatalogDocumentFresh(document srvDeviceUpdateCatalogDocument, now time.Time) bool {
	if document.SchemaVersion != srvDeviceUpdateCatalogSchemaVersion || document.VerifiedAt <= 0 || document.MaxAgeSeconds <= 0 || document.MaxAgeSeconds > int(srvReleaseCatalogMaxAge/time.Second) {
		return false
	}
	return now.UnixMilli() <= document.VerifiedAt+int64(document.MaxAgeSeconds)*1000
}

func validateSrvTrustedCatalogDocument(document srvDeviceUpdateCatalogDocument, now time.Time) error {
	if document.SchemaVersion != srvDeviceUpdateCatalogSchemaVersion || document.Source != "github-release" || document.Repository != srvOfficialFirmwareRepository || document.ReleaseID <= 0 || !validSrvCatalogString(document.ReleaseTag, 128) || document.VerifiedAt <= 0 || document.MaxAgeSeconds <= 0 || document.MaxAgeSeconds > int(srvReleaseCatalogMaxAge/time.Second) || !srvCatalogDocumentFresh(document, now) || len(document.Releases) != len(srvOfficialFirmwareProfiles) {
		return errors.New("trusted release catalog document is invalid or expired")
	}
	seen := make(map[string]bool, len(document.Releases))
	for _, release := range document.Releases {
		if !validSrvDeviceUpdateRelease(release) || release.ReleaseTag != document.ReleaseTag || release.SourceAssetID <= 0 || release.SourceAssetSize <= 0 || !validSrvCatalogString(release.SourceAssetName, 256) || !validSrvSHA256(release.ArchiveSHA256) || seen[release.BoardID] {
			return errors.New("trusted release catalog entry is invalid")
		}
		seen[release.BoardID] = true
	}
	for _, profile := range srvOfficialFirmwareProfiles {
		if !seen[profile.boardID] {
			return errors.New("trusted release catalog is missing an official board")
		}
	}
	return nil
}

func validSrvFirmwareIdentity(identity coreim.FirmwareIdentity) bool {
	if !validSrvDeviceID(identity.DeviceID) || !validSrvCatalogString(identity.ProductID, 96) ||
		!validSrvCatalogString(identity.BoardID, 128) || !validSrvCatalogString(identity.HardwareRev, 64) ||
		!validSrvCatalogString(identity.LayoutID, 128) || !validSrvCatalogString(identity.CompatibilityID, 256) ||
		!validSrvCatalogString(identity.AppVersion, 128) || identity.ReleaseSequence < 0 {
		return false
	}
	if identity.ELFSHA256 != "" && !validSrvSHA256(identity.ELFSHA256) {
		return false
	}
	return true
}

func validSrvDeviceUpdateRelease(release srvDeviceUpdateRelease) bool {
	if !validSrvCatalogString(release.ProductID, 96) || !validSrvCatalogString(release.BoardID, 128) ||
		!validSrvCatalogString(release.HardwareRev, 64) || !validSrvCatalogString(release.LayoutID, 128) ||
		!validSrvCatalogString(release.CompatibilityID, 256) || release.Channel != srvDeviceUpdateChannel ||
		release.ReleaseSequence <= 0 || !validSrvCatalogString(release.DisplayVersion, 128) ||
		!validSrvCatalogString(release.ReleaseTag, 128) || !validSrvCatalogString(release.PackageID, 256) ||
		!validSrvSHA256(release.ManifestSHA256) || release.PublishedAt <= 0 {
		return false
	}
	if release.Severity == "" {
		release.Severity = "normal"
	}
	if release.Severity != "normal" && release.Severity != "important" && release.Severity != "critical" {
		return false
	}
	return release.CheckAfterSeconds >= 60 && release.CheckAfterSeconds <= 30*24*60*60
}

func validSrvCatalogString(value string, maximum int) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= maximum && !strings.ContainsAny(value, "\r\n\t")
}

func validSrvDeviceID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 3 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func validSrvSHA256(value string) bool {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "sha256:")
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func (s *HTTPServer) bindHardwareDevice(ctx context.Context, principal srvThirdPartyPrincipal, identity coreim.FirmwareIdentity, paired bool) error {
	if s == nil || s.deviceUpdateBindings == nil || !validSrvFirmwareIdentity(identity) {
		return errors.New("device identity is invalid")
	}
	return s.deviceUpdateBindings.bind(ctx, principal, identity, paired)
}

func (s *HTTPServer) bindHardwareDeviceForPairing(ctx context.Context, principal srvThirdPartyPrincipal, identity coreim.FirmwareIdentity) error {
	if !validSrvFirmwareIdentity(identity) {
		return errors.New("device firmware identity is required for pairing")
	}
	return s.bindHardwareDevice(ctx, principal, identity, true)
}

type srvDeviceUpdateBindingStore struct {
	path string
	mu   sync.Mutex
	data map[string]srvDeviceFirmwareBinding
}

func newSrvDeviceUpdateBindingStore(dataRoot string) *srvDeviceUpdateBindingStore {
	s := &srvDeviceUpdateBindingStore{path: filepath.Join(dataRoot, "device_firmware_bindings.json"), data: map[string]srvDeviceFirmwareBinding{}}
	s.load()
	return s
}

func (s *srvDeviceUpdateBindingStore) load() {
	if s == nil {
		return
	}
	raw, err := os.ReadFile(s.path)
	if err != nil || len(raw) == 0 || len(raw) > 4*1024*1024 {
		return
	}
	var data map[string]srvDeviceFirmwareBinding
	if json.Unmarshal(raw, &data) == nil {
		s.data = data
	}
}

func (s *srvDeviceUpdateBindingStore) bind(_ context.Context, principal srvThirdPartyPrincipal, identity coreim.FirmwareIdentity, paired bool) error {
	if s == nil {
		return errors.New("device binding store is unavailable")
	}
	now := time.Now().UTC().UnixMilli()
	s.mu.Lock()
	defer s.mu.Unlock()
	current, found := s.data[identity.DeviceID]
	if found && (current.TenantID != principal.Principal.TenantID || current.UserID != principal.Principal.UserID) {
		return fmt.Errorf("device identity is already paired to another owner")
	}
	if found && !paired && current.BoardID != "" && (current.BoardID != identity.BoardID || current.HardwareRev != identity.HardwareRev || current.LayoutID != identity.LayoutID || current.CompatibilityID != identity.CompatibilityID) {
		return fmt.Errorf("device firmware identity conflicts with its pairing binding")
	}
	if !found && !paired {
		return fmt.Errorf("device is not paired")
	}
	epoch := current.CredentialEpoch
	if epoch <= 0 {
		epoch = 1
	}
	binding := srvDeviceFirmwareBinding{DeviceID: identity.DeviceID, TenantID: principal.Principal.TenantID, UserID: principal.Principal.UserID, CredentialEpoch: epoch, BoardID: identity.BoardID, HardwareRev: identity.HardwareRev, LayoutID: identity.LayoutID, CompatibilityID: identity.CompatibilityID, PairedAt: current.PairedAt, LastSeenAt: now}
	if binding.PairedAt == 0 {
		binding.PairedAt = now
	}
	// Keep the memory view and durable JSON file atomic from the caller's
	// perspective.  A failed rename/write must not leave this running Hub
	// authorizing a binding which will disappear after restart.
	s.data[identity.DeviceID] = binding
	if err := s.saveLocked(); err != nil {
		if found {
			s.data[identity.DeviceID] = current
		} else {
			delete(s.data, identity.DeviceID)
		}
		return err
	}
	return nil
}

// reserve records the owner/client relationship at pairing time.  Legacy voice
// pairing cannot carry the firmware identity, so the first identity-bearing
// handshake completes this record.  A reservation still cannot query a
// catalog: lookup requires the complete bound board/layout identity.
func (s *srvDeviceUpdateBindingStore) reserve(principal srvThirdPartyPrincipal, deviceID string) error {
	if s == nil || !validSrvDeviceID(deviceID) {
		return errors.New("device binding store is unavailable")
	}
	now := time.Now().UTC().UnixMilli()
	s.mu.Lock()
	defer s.mu.Unlock()
	current, found := s.data[deviceID]
	if found && (current.TenantID != principal.Principal.TenantID || current.UserID != principal.Principal.UserID) {
		return fmt.Errorf("device identity is already paired to another owner")
	}
	epoch := current.CredentialEpoch + 1
	if epoch <= 0 {
		epoch = 1
	}
	reservation := srvDeviceFirmwareBinding{DeviceID: deviceID, TenantID: principal.Principal.TenantID, UserID: principal.Principal.UserID, CredentialEpoch: epoch, PairedAt: now, LastSeenAt: now}
	s.data[deviceID] = reservation
	if err := s.saveLocked(); err != nil {
		if found {
			s.data[deviceID] = current
		} else {
			delete(s.data, deviceID)
		}
		return err
	}
	return nil
}

func (s *HTTPServer) reserveHardwareDeviceForPairing(principal srvThirdPartyPrincipal, deviceID string) error {
	if s == nil || s.deviceUpdateBindings == nil {
		return errors.New("device binding store is unavailable")
	}
	return s.deviceUpdateBindings.reserve(principal, deviceID)
}

// unbindHardwareDevice removes the firmware identity binding as part of a
// hardware unpair.  The device's bearer is currently user scoped, so this
// durable identity removal plus the agent-binding tombstone is what prevents a
// stale device from regaining access before it is paired again.
func (s *HTTPServer) unbindHardwareDevice(p agentservice.Principal, deviceID string) error {
	if s == nil || s.deviceUpdateBindings == nil {
		return nil
	}
	return s.deviceUpdateBindings.delete(p, deviceID)
}

func (s *srvDeviceUpdateBindingStore) delete(p agentservice.Principal, deviceID string) error {
	if s == nil {
		return errors.New("device binding store is unavailable")
	}
	deviceID = strings.TrimSpace(deviceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.data[deviceID]
	if !ok || binding.TenantID != p.TenantID || binding.UserID != p.UserID {
		return nil
	}
	delete(s.data, deviceID)
	if err := s.saveLocked(); err != nil {
		s.data[deviceID] = binding
		return err
	}
	return nil
}

func (s *srvDeviceUpdateBindingStore) lookup(principal srvThirdPartyPrincipal, identity coreim.FirmwareIdentity) bool {
	if s == nil || !validSrvFirmwareIdentity(identity) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.data[identity.DeviceID]
	return ok && binding.TenantID == principal.Principal.TenantID && binding.UserID == principal.Principal.UserID && binding.BoardID == identity.BoardID && binding.HardwareRev == identity.HardwareRev && binding.LayoutID == identity.LayoutID && binding.CompatibilityID == identity.CompatibilityID && binding.CredentialEpoch > 0
}

func (s *srvDeviceUpdateBindingStore) saveLocked() error {
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *HTTPServer) firmwareUpdateForHandshake(principal srvThirdPartyPrincipal, identity coreim.FirmwareIdentity) *coreim.FirmwareUpdateMetadata {
	// Fail closed when no durable pairing or no trusted/validated catalog item
	// exists.  A generic no-update answer is intentional and avoids letting an
	// unbound bearer enumerate profile-specific releases.
	if s == nil || s.deviceUpdateBindings == nil || s.deviceUpdateCatalog == nil || !s.deviceUpdateBindings.lookup(principal, identity) {
		return nil
	}
	release, ok := s.deviceUpdateCatalog.latestFor(identity)
	if !ok {
		return &coreim.FirmwareUpdateMetadata{Available: false, RequiresComputer: true}
	}
	return &coreim.FirmwareUpdateMetadata{Available: true, RequiresComputer: true, ReleaseSequence: release.ReleaseSequence, DisplayVersion: release.DisplayVersion, ReleaseTag: release.ReleaseTag, Channel: release.Channel, PublishedAt: release.PublishedAt, Severity: release.Severity, Critical: release.Critical, MinimumMakerVersion: release.MinimumMakerVersion, PackageID: release.PackageID, ManifestSHA256: release.ManifestSHA256, NotesSummary: release.NotesSummary, NotesSHA256: release.NotesSHA256, CheckAfterSeconds: release.CheckAfterSeconds, Withdrawn: false}
}
