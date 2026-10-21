package guiapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DesktopBotMaterialUpload is Hub's answer to one material chunk. Path
// (~/素材/<bot>/<name>) is set once the whole file is on the bot's desktop.
type DesktopBotMaterialUpload struct {
	UploadID string `json:"upload_id"`
	Name     string `json:"name"`
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
	Done     bool   `json:"done"`
	Path     string `json:"path,omitempty"`
}

// desktopBotMaterialChunkMax matches corelib/desktop.UploadChunkMax.
const desktopBotMaterialChunkMax = 4 << 20

// UploadDesktopBotMaterialChunk sends one base64 chunk of a material file to
// POST /api/v1/bots/{id}/desktop/files with the person's login. The frontend
// slices the file and reports progress between chunks.
func (a *App) UploadDesktopBotMaterialChunk(botID, uploadID, name string, offset, total int64, dataBase64 string) (*DesktopBotMaterialUpload, error) {
	botID = strings.TrimSpace(botID)
	if botID == "" || strings.TrimSpace(uploadID) == "" || strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("bot id, upload id and name are required")
	}
	data, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		return nil, fmt.Errorf("chunk is not base64")
	}
	if len(data) > desktopBotMaterialChunkMax {
		return nil, fmt.Errorf("chunk is too large")
	}
	body := map[string]any{"upload_id": uploadID, "name": name, "offset": offset, "total": total, "data": data}
	var out DesktopBotMaterialUpload
	// The last chunk waits for the whole file to reach the desktop.
	// A 409 still names the bytes Hub already has. Return that struct so the
	// page can resume; a thrown error would drop the body.
	status, err := a.desktopBotExchange(context.Background(), http.MethodPost, "/api/v1/bots/"+url.PathEscape(botID)+"/desktop/files", body, &out, 10*time.Minute, nil)
	if status == http.StatusConflict && out.UploadID != "" {
		return &out, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}
