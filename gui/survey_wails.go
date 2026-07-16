package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ListSurveys returns Hub survey list JSON.
// filterJSON may be {"status":"draft|published|closed|archived|all"} or empty.
func (a *App) ListSurveys(filterJSON string) (string, error) {
	status := ""
	if strings.TrimSpace(filterJSON) != "" {
		var f struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal([]byte(filterJSON), &f)
		status = strings.TrimSpace(f.Status)
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	raw, err := c.List(context.Background(), status)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func requireSurveyID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("survey id required")
	}
	return id, nil
}

// GetSurvey returns survey detail (salt redacted by Hub).
func (a *App) GetSurvey(id string) (string, error) {
	id, err := requireSurveyID(id)
	if err != nil {
		return "", err
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	raw, err := c.Get(context.Background(), id)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// CreateSurvey creates a draft survey from JSON CreateInput.
func (a *App) CreateSurvey(inputJSON string) (string, error) {
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	raw, err := c.Create(context.Background(), json.RawMessage(inputJSON))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// PublishSurvey publishes a survey.
func (a *App) PublishSurvey(id string, optsJSON string) (string, error) {
	id, err := requireSurveyID(id)
	if err != nil {
		return "", err
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	raw, err := c.Publish(context.Background(), id, json.RawMessage(optsJSON))
	if err != nil {
		return "", err
	}
	var opts struct {
		Announce bool `json:"announce"`
	}
	_ = json.Unmarshal([]byte(optsJSON), &opts)
	if opts.Announce {
		annRaw, annErr := a.AnnounceSurveyToBoundGroups(id)
		// Always return published survey; surface announce problems without failing publish.
		var published map[string]any
		if json.Unmarshal(raw, &published) == nil && published != nil {
			if annErr != nil {
				published["announce_failures"] = []string{annErr.Error()}
			} else {
				var ann struct {
					Failures []string `json:"failures"`
				}
				if json.Unmarshal([]byte(annRaw), &ann) == nil && len(ann.Failures) > 0 {
					published["announce_failures"] = ann.Failures
				}
			}
			if patched, mErr := json.Marshal(published); mErr == nil {
				raw = patched
			}
		}
	}
	return string(raw), nil
}

// CloseSurvey closes a published survey.
func (a *App) CloseSurvey(id string) (string, error) {
	id, err := requireSurveyID(id)
	if err != nil {
		return "", err
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	raw, err := c.Close(context.Background(), id)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ReopenSurvey reopens a closed survey (keeps code + responses).
func (a *App) ReopenSurvey(id string) (string, error) {
	id, err := requireSurveyID(id)
	if err != nil {
		return "", err
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	raw, err := c.Reopen(context.Background(), id)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ArchiveSurvey archives a draft or closed survey.
func (a *App) ArchiveSurvey(id string) (string, error) {
	id, err := requireSurveyID(id)
	if err != nil {
		return "", err
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	raw, err := c.Archive(context.Background(), id)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// DuplicateSurvey copies a survey into a new draft (new code + salt).
func (a *App) DuplicateSurvey(id string) (string, error) {
	id, err := requireSurveyID(id)
	if err != nil {
		return "", err
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	raw, err := c.Duplicate(context.Background(), id)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// DeleteSurvey deletes a draft or archived survey (cascade).
func (a *App) DeleteSurvey(id string) error {
	id, err := requireSurveyID(id)
	if err != nil {
		return err
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return err
	}
	return c.Delete(context.Background(), id)
}

// UpdateSurvey patches a draft survey.
func (a *App) UpdateSurvey(id string, inputJSON string) (string, error) {
	id, err := requireSurveyID(id)
	if err != nil {
		return "", err
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	raw, err := c.Update(context.Background(), id, json.RawMessage(inputJSON))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// BindSurveyGroups binds Lansenger groups to a survey.
func (a *App) BindSurveyGroups(id string, bodyJSON string) (string, error) {
	id, err := requireSurveyID(id)
	if err != nil {
		return "", err
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	raw, err := c.Bind(context.Background(), id, json.RawMessage(bodyJSON))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// UnbindSurveyGroup removes one group binding.
func (a *App) UnbindSurveyGroup(id, platform, groupID string) error {
	id, err := requireSurveyID(id)
	if err != nil {
		return err
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return err
	}
	if strings.TrimSpace(platform) == "" {
		platform = "lansenger"
	}
	return c.Unbind(context.Background(), id, platform, strings.TrimSpace(groupID))
}

// ListSurveyResponses returns submitted responses JSON.
func (a *App) ListSurveyResponses(id string) (string, error) {
	id, err := requireSurveyID(id)
	if err != nil {
		return "", err
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	raw, err := c.Responses(context.Background(), id)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// GetSurveyStats returns aggregate stats JSON.
func (a *App) GetSurveyStats(id string) (string, error) {
	id, err := requireSurveyID(id)
	if err != nil {
		return "", err
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	raw, err := c.Stats(context.Background(), id)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ExportSurveyXLSX downloads responses from Hub and writes a local multi-sheet xlsx.
func (a *App) ExportSurveyXLSX(id string) (string, error) {
	return a.exportSurveyXLSX(id, nil)
}

// ExportSurveyXLSXFiltered exports using a client-supplied responses JSON body
// of shape {"responses":[...]} (e.g. option-filtered subset). Empty body falls back to full Hub list.
func (a *App) ExportSurveyXLSXFiltered(id string, responsesJSON string) (string, error) {
	var override json.RawMessage
	if strings.TrimSpace(responsesJSON) != "" {
		override = json.RawMessage(responsesJSON)
	}
	return a.exportSurveyXLSX(id, override)
}

func (a *App) exportSurveyXLSX(id string, responsesOverride json.RawMessage) (string, error) {
	id, err := requireSurveyID(id)
	if err != nil {
		return "", err
	}
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	detailRaw, err := c.Get(context.Background(), id)
	if err != nil {
		return "", err
	}
	var respRaw json.RawMessage
	if len(responsesOverride) > 0 {
		respRaw = responsesOverride
	} else {
		respRaw, err = c.Responses(context.Background(), id)
		if err != nil {
			return "", err
		}
	}
	data, err := buildSurveyExportXLSX(detailRaw, respRaw)
	if err != nil {
		return "", err
	}
	var meta struct {
		ShortCode string `json:"short_code"`
	}
	_ = json.Unmarshal(detailRaw, &meta)
	defaultName := defaultSurveyExportName(meta.ShortCode, time.Now())
	path := ""
	if a.ctx != nil {
		path, err = runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			DefaultFilename: defaultName,
			Title:           "Export survey",
			Filters: []runtime.FileFilter{
				{DisplayName: "Excel", Pattern: "*.xlsx"},
			},
		})
		if err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(path) == "" {
		dir := filepath.Join(a.GetDataDir(), "exports")
		_ = os.MkdirAll(dir, 0o755)
		path = filepath.Join(dir, defaultName)
	}
	if err := writeSurveyExcelFile(path, data); err != nil {
		return "", err
	}
	// Best-effort reveal in file manager (matches knowledge export UX).
	_ = a.ShowItemInFolder(path)
	return path, nil
}

// AnnounceSurveyToBoundGroups sends announce text to bound Lansenger groups.
func (a *App) AnnounceSurveyToBoundGroups(id string) (string, error) {
	c, err := a.newSurveyHubClient()
	if err != nil {
		return "", err
	}
	raw, err := c.Get(context.Background(), strings.TrimSpace(id))
	if err != nil {
		return "", err
	}
	var sv struct {
		Title     string `json:"title"`
		ShortCode string `json:"short_code"`
		Settings  struct {
			Deadline    string `json:"deadline"`
			TargetCount int    `json:"target_count"`
		} `json:"settings"`
		Bindings []struct {
			Platform string `json:"platform"`
			GroupID  string `json:"group_id"`
		} `json:"bindings"`
	}
	if err := json.Unmarshal(raw, &sv); err != nil {
		return "", err
	}
	text := fmt.Sprintf("【问卷】%s\n短码：%s", sv.Title, sv.ShortCode)
	if sv.Settings.Deadline != "" {
		if t, err := time.Parse(time.RFC3339, sv.Settings.Deadline); err == nil {
			text += "\n截止：" + t.Local().Format("2006-01-02 15:04")
		} else {
			text += "\n截止：" + sv.Settings.Deadline
		}
	}
	if sv.Settings.TargetCount > 0 {
		text += fmt.Sprintf("\n目标回收：%d 份", sv.Settings.TargetCount)
	}
	text += fmt.Sprintf("\n请 @机器人 发送 /survey %s 开始填写", sv.ShortCode)
	var failures []string
	for _, b := range sv.Bindings {
		if b.Platform != "lansenger" {
			continue
		}
		if err := a.sendLansengerGroupText(b.GroupID, text); err != nil {
			failures = append(failures, b.GroupID+": "+err.Error())
		}
	}
	out, _ := json.Marshal(map[string]any{"failures": failures})
	return string(out), nil
}

func (a *App) sendLansengerGroupText(groupID, text string) error {
	if a.lansengerGateway == nil {
		return fmt.Errorf("lansenger gateway not running")
	}
	a.lansengerGateway.mu.Lock()
	gw := a.lansengerGateway.gateway
	a.lansengerGateway.mu.Unlock()
	if gw == nil {
		return fmt.Errorf("lansenger gateway not connected")
	}
	return gw.SendText(context.Background(), lansenger.OutgoingText{
		ToUserID: groupID,
		Text:     text,
		IsGroup:  true,
	})
}
