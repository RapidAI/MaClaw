package guiapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var fileCompanionGroupIDMatch = regexp.MustCompile(`^cg_[0-9a-f]{32}$`)

// FileCompanionGroup is a reading group. The creator is the admin.
type FileCompanionGroup struct {
	ID      string                     `json:"id"`
	Name    string                     `json:"name"`
	Owner   string                     `json:"owner"`
	Admin   bool                       `json:"admin"`
	Icon    string                     `json:"icon,omitempty"`
	Members []FileCompanionGroupMember `json:"members"`
}

// FileCompanionGroupMember is one invited or accepted reader.
type FileCompanionGroupMember struct {
	Contact string `json:"contact"`
	Status  string `json:"status"`
}

// FileCompanionGroupInvite is waiting in the main window's notification bell.
type FileCompanionGroupInvite struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Owner     string `json:"owner"`
	Contact   string `json:"contact"`
	InvitedAt string `json:"invited_at,omitempty"`
	// NotificationID is the Hub notification that delivered this invitation.
	// It is what a client that was offline pulls on its next connect.
	NotificationID string `json:"notification_id,omitempty"`
}

func (a *App) ListFileCompanionGroups() ([]FileCompanionGroup, error) {
	empty := []FileCompanionGroup{}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return empty, nil
	}
	body, err := a.fileCompanionHubJSON(http.MethodGet, hub+"/api/mobile/documents/groups", viewer, nil)
	if err != nil {
		if status, ok := err.(*fileCompanionShareStatus); ok && status.status == http.StatusNotFound {
			return empty, nil
		}
		return empty, err
	}
	var payload struct {
		Groups []FileCompanionGroup `json:"groups"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return empty, errors.New("云端群组无法读取")
	}
	if payload.Groups == nil {
		return empty, nil
	}
	for i := range payload.Groups {
		if payload.Groups[i].Members == nil {
			payload.Groups[i].Members = []FileCompanionGroupMember{}
		}
		payload.Groups[i].Icon = fileCompanionGroupIcon(payload.Groups[i].Icon)
	}
	return payload.Groups, nil
}

func (a *App) CreateFileCompanionGroup(name string) (FileCompanionGroup, error) {
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return FileCompanionGroup{}, err
	}
	payload, _ := json.Marshal(map[string]string{"name": name})
	body, err := a.fileCompanionHubJSON(http.MethodPost, hub+"/api/mobile/documents/groups", viewer, payload)
	if err != nil {
		return FileCompanionGroup{}, err
	}
	return decodeFileCompanionGroup(body)
}

func (a *App) AddFileCompanionGroupMember(groupID, contact string) (FileCompanionGroup, error) {
	return a.postFileCompanionGroupMember(http.MethodPost, groupID, contact)
}

func (a *App) RemoveFileCompanionGroupMember(groupID, contact string) (FileCompanionGroup, error) {
	return a.postFileCompanionGroupMember(http.MethodDelete, groupID, contact)
}

func (a *App) UpdateFileCompanionGroup(groupID, name, icon string) (FileCompanionGroup, error) {
	if !fileCompanionGroupIDMatch.MatchString(groupID) {
		return FileCompanionGroup{}, errors.New("群组不存在")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return FileCompanionGroup{}, err
	}
	payload, _ := json.Marshal(map[string]string{"name": name, "icon": icon})
	body, err := a.fileCompanionHubJSON(http.MethodPatch, hub+"/api/mobile/documents/groups/"+groupID, viewer, payload)
	if err != nil {
		return FileCompanionGroup{}, err
	}
	return decodeFileCompanionGroup(body)
}

func (a *App) DeleteFileCompanionGroup(groupID string) error {
	if !fileCompanionGroupIDMatch.MatchString(groupID) {
		return errors.New("群组不存在")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return err
	}
	_, err = a.fileCompanionHubJSON(http.MethodDelete, hub+"/api/mobile/documents/groups/"+groupID, viewer, nil)
	return err
}

func (a *App) LeaveFileCompanionGroup(groupID string) error {
	if !fileCompanionGroupIDMatch.MatchString(groupID) {
		return errors.New("群组不存在")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return err
	}
	_, err = a.fileCompanionHubJSON(http.MethodPost, hub+"/api/mobile/documents/groups/"+groupID+"/leave", viewer, []byte(`{}`))
	return err
}

func (a *App) postFileCompanionGroupMember(method, groupID, contact string) (FileCompanionGroup, error) {
	if !fileCompanionGroupIDMatch.MatchString(groupID) {
		return FileCompanionGroup{}, errors.New("群组不存在")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return FileCompanionGroup{}, err
	}
	payload, _ := json.Marshal(map[string]string{"contact": contact})
	body, err := a.fileCompanionHubJSON(method, hub+"/api/mobile/documents/groups/"+groupID+"/members", viewer, payload)
	if err != nil {
		return FileCompanionGroup{}, err
	}
	return decodeFileCompanionGroup(body)
}

func (a *App) ListFileCompanionGroupInvites() ([]FileCompanionGroupInvite, error) {
	empty := []FileCompanionGroupInvite{}
	if a == nil {
		return empty, nil
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return empty, nil
	}
	invites, err := a.fetchFileCompanionGroupInvites(hub, viewer)
	if err != nil {
		return nil, err
	}
	if invites == nil {
		return empty, nil
	}
	return invites, nil
}

// fetchFileCompanionGroupInvites reads the pending invite list for a signed-in
// viewer. nil means the Hub has no invitation route (an older build).
func (a *App) fetchFileCompanionGroupInvites(hub, viewer string) ([]FileCompanionGroupInvite, error) {
	body, err := a.fileCompanionHubJSON(http.MethodGet, hub+"/api/mobile/documents/groups/invites", viewer, nil)
	if err != nil {
		// An empty 404 is an old hub whose invites route is missing. A 404
		// that carries a hub message is a real failure and must be shown.
		if fileCompanionInviteListIsOldHub(err) {
			return nil, nil
		}
		return nil, fileCompanionGroupCallError(err)
	}
	var payload struct {
		Invites []FileCompanionGroupInvite `json:"invites"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil, errors.New("云端群组无法读取")
	}
	return payload.Invites, nil
}

func (a *App) AcceptFileCompanionGroupInvite(groupID string) error {
	return a.decideFileCompanionGroupInvite(groupID, "accept")
}

func (a *App) DeclineFileCompanionGroupInvite(groupID string) error {
	return a.decideFileCompanionGroupInvite(groupID, "decline")
}

// IgnoreFileCompanionGroupInvite silences an invitation on the Hub. The member
// row stays pending for the admin, but this person stops being notified, so the
// choice survives a reinstall or a second device.
func (a *App) IgnoreFileCompanionGroupInvite(groupID string) error {
	return a.decideFileCompanionGroupInvite(groupID, "ignore")
}

func (a *App) decideFileCompanionGroupInvite(groupID, action string) error {
	if !fileCompanionGroupIDMatch.MatchString(groupID) {
		return errors.New("没有找到入群邀请")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return err
	}
	_, err = a.fileCompanionHubJSON(http.MethodPost, hub+"/api/mobile/documents/groups/"+groupID+"/"+action, viewer, []byte(`{}`))
	return fileCompanionGroupCallError(err)
}

// fileCompanionInviteListIsOldHub is true only when the invites route is
// missing. A 404 with a hub message is not an empty inbox.
func fileCompanionInviteListIsOldHub(err error) bool {
	var status *fileCompanionShareStatus
	if !errors.As(err, &status) || status == nil {
		return false
	}
	return status.status == http.StatusNotFound && strings.TrimSpace(status.message) == ""
}

// Group calls share the document-share HTTP status. An empty body must not
// be reported as a document-share failure.
func fileCompanionGroupCallError(err error) error {
	var status *fileCompanionShareStatus
	if !errors.As(err, &status) || status == nil || strings.TrimSpace(status.message) != "" {
		return err
	}
	switch status.status {
	case http.StatusUnauthorized:
		return errors.New("请先登录 MaClaw Hub")
	case http.StatusNotFound:
		return errors.New("没有找到入群邀请")
	default:
		return fmt.Errorf("云端群组无法读取（HTTP %d）", status.status)
	}
}

func (a *App) fileCompanionHubJSON(method, url, viewer string, payload []byte) ([]byte, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewer)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("暂时无法连接云端")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &fileCompanionShareStatus{status: resp.StatusCode, message: fileCompanionShareMessage(body)}
	}
	return body, nil
}

func decodeFileCompanionGroup(body []byte) (FileCompanionGroup, error) {
	var group FileCompanionGroup
	if json.Unmarshal(body, &group) != nil || !fileCompanionGroupIDMatch.MatchString(group.ID) {
		return FileCompanionGroup{}, errors.New("云端群组无法读取")
	}
	if group.Members == nil {
		group.Members = []FileCompanionGroupMember{}
	}
	group.Icon = fileCompanionGroupIcon(group.Icon)
	return group, nil
}

func fileCompanionGroupIcon(icon string) string {
	switch strings.TrimSpace(icon) {
	case "book", "people", "star", "pen", "leaf", "flask":
		return icon
	default:
		return "book"
	}
}
