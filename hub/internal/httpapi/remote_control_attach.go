package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
)

// PostRemoteControlAttachmentHandler
// POST /api/remote-control/grants/{id}/attach?task_path=…&upload_id=…&name=…
//
//	&offset=…&total=…
//
// A phone attaching a file to one task of a controlled machine. The phone
// cannot write to that machine's disk, and the op channel carries JSON up to
// 1 MB, so the bytes take this chunked route: the body is the raw chunk and
// the answer is botmgmt.AttachmentStaged.
//
// The order is what keeps this safe:
//  1. the caller must be the grant's controller (same headers as the op
//     channel),
//  2. the grant must be active with `operate` and the controlled machine
//     online — remotecontrol.Service.Exec enforces both while resolving the
//     task with op `attach_begin`,
//  3. the controlled machine answers the task directory as its bot's
//     container sees it, plus the bot id. Nothing outside that directory is
//     written, and a task the Hub cannot reach a container for is refused.
//
// The file lands as <task dir>/attachments/<upload id>-<name>; the task agent
// reads it there, and `get_task` reports it as a delivered file.
func PostRemoteControlAttachmentHandler(svc *remotecontrol.Service, uploads *botmgmt.Service, identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil || uploads == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "remote control is unavailable")
			return
		}
		grantID := strings.TrimSpace(r.PathValue("id"))
		principal, machineID, ok := viewerForAttachment(w, r, identity)
		if !ok {
			return
		}
		if _, err := svc.ControllerGrant(r.Context(), principal.TenantID, principal.UserID, machineID, grantID); err != nil {
			writeRemoteControlError(w, err)
			return
		}
		query := r.URL.Query()
		taskPath := strings.TrimSpace(query.Get("task_path"))
		uploadID := strings.TrimSpace(query.Get("upload_id"))
		offset := int64(-1)
		total := int64(-1)
		if _, err := fmt.Sscanf(strings.TrimSpace(query.Get("offset")), "%d", &offset); err != nil {
			offset = -1
		}
		if _, err := fmt.Sscanf(strings.TrimSpace(query.Get("total")), "%d", &total); err != nil {
			total = -1
		}
		raw := query.Get("name")
		if taskPath == "" || uploadID == "" || raw == "" || offset < 0 || total <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID", "附件请求无效")
			return
		}
		chunk, readErr := io.ReadAll(io.LimitReader(r.Body, (4<<20)+1))
		if readErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_UPLOAD", "附件内容读取失败")
			return
		}
		if len(chunk) > 4<<20 {
			writeError(w, http.StatusRequestEntityTooLarge, "INVALID_UPLOAD", "附件分片过大")
			return
		}

		// Resolve the task on the controlled machine. Exec checks the grant
		// status, the operate permission and that the machine is online.
		probe, _ := json.Marshal(map[string]string{"project_path": taskPath})
		reply, err := svc.Exec(r.Context(), principal.TenantID, principal.UserID, machineID, grantID, remotecontrol.OpAttachBegin, probe)
		if err != nil {
			writeRemoteControlError(w, err)
			return
		}
		var target struct {
			TaskDir string `json:"task_dir"`
			BotID   string `json:"bot_id"`
			NodeID  string `json:"node_id"`
		}
		if err := json.Unmarshal(reply, &target); err != nil || strings.TrimSpace(target.TaskDir) == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "没有找到这个任务")
			return
		}
		staged, err := uploads.StageAttachmentChunk(r.Context(), principal.TenantID, botmgmt.AttachmentChunk{
			Ref: botmgmt.AttachmentRef{
				GrantID:  grantID,
				UploadID: uploadID,
				Name:     filepath.Base(raw),
				Path:     target.TaskDir,
				BotID:    target.BotID,
				NodeID:   target.NodeID,
			},
			Offset: offset,
			Total:  total,
			Data:   chunk,
		})
		if err != nil {
			writeAttachmentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, staged)
	}
}

func viewerForAttachment(w http.ResponseWriter, r *http.Request, identity *auth.IdentityService) (*auth.ViewerPrincipal, string, bool) {
	principal, err := authenticateViewerRequest(r, identity)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
		return nil, "", false
	}
	machineID := strings.TrimSpace(r.Header.Get("X-Machine-Id"))
	if machineID == "" {
		machineID = strings.TrimSpace(r.URL.Query().Get("machine_id"))
	}
	return principal, machineID, true
}

func writeAttachmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, botmgmt.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "INVALID_UPLOAD", strings.TrimSpace(err.Error()))
	case errors.Is(err, botmgmt.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", strings.TrimSpace(err.Error()))
	case errors.Is(err, botmgmt.ErrNodeOffline):
		writeError(w, http.StatusBadGateway, "NODE_OFFLINE", botmgmt.ErrNodeOffline.Error())
	default:
		writeError(w, http.StatusBadGateway, "UPLOAD_FAILED", strings.TrimSpace(err.Error()))
	}
}
