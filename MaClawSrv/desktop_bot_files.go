package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// Large bot files go through Hub's bot file store: the bytes are streamed
// there (by the cloud desktop itself when the file is on the desktop), and
// the chat message carries only a reference. The person's MaClaw app
// downloads it over HTTP with resume into the bot's folder. Small files stay
// inline as before. The same upload serves local_computer download.

// botFileInlineMax is the largest file still sent inline (base64) in a chat
// message: MACLAW_DELIVER_INLINE_MAX_KB, default 1 MB.
func botFileInlineMax() int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MACLAW_DELIVER_INLINE_MAX_KB"))); err == nil && n > 0 {
		return n << 10
	}
	return 1 << 20
}

type botFileCreated struct {
	OK          bool   `json:"ok"`
	ID          string `json:"id"`
	UploadToken string `json:"upload_token"`
	Complete    bool   `json:"complete"`
	MaxBytes    int64  `json:"max_bytes"`
	Received    int64  `json:"received"`
	SHA256      string `json:"sha256"`
	Code        string `json:"code"`
	Error       string `json:"error"`
}

// botFileHub is one Hub call for the store; tests replace it.
var botFileHub = func(ctx context.Context, method, apiPath string, headers map[string]string, body io.Reader, size int64) ([]byte, int, error) {
	client := desktopHubClientFromEnv()
	if client == nil {
		return nil, 0, fmt.Errorf("this server is not connected to Hub")
	}
	req, err := http.NewRequestWithContext(ctx, method, client.baseURL+apiPath, body)
	if err != nil {
		return nil, 0, err
	}
	if size >= 0 && body != nil {
		req.ContentLength = size
	}
	req.Header.Set("Authorization", "Bearer "+client.token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Minute}).Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("Hub is unreachable: %v", err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return payload, resp.StatusCode, nil
}

func botFileDecode(payload []byte, status int) (botFileCreated, error) {
	var out botFileCreated
	_ = json.Unmarshal(payload, &out)
	if status == http.StatusNotFound && out.Code == "" || status == http.StatusMethodNotAllowed {
		return out, fmt.Errorf("this server's Hub has no bot file store yet (it needs the newer Hub build)")
	}
	if status < 200 || status >= 300 || (!out.OK && out.Error != "") {
		msg := strings.TrimSpace(out.Error)
		if msg == "" {
			msg = desktopRejectedMessage(payload)
		}
		return out, fmt.Errorf("Hub's file store refused it: %s", msg)
	}
	return out, nil
}

// botFileCreate reserves a file for this run's owner.
func botFileCreate(ctx context.Context, scope agentruntime.Scope, name, mime string, size int64) (botFileCreated, error) {
	raw, _ := json.Marshal(map[string]any{
		"tenant_id": strings.TrimSpace(scope.TenantID), "user_id": strings.TrimSpace(scope.UserID),
		"instance_id": strings.TrimSpace(scope.InstanceID), "name": name, "mime": mime, "size": size,
	})
	payload, status, err := botFileHub(ctx, http.MethodPost, "/api/v1/desktop-services/files", map[string]string{"Content-Type": "application/json"}, bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return botFileCreated{}, err
	}
	return botFileDecode(payload, status)
}

// botFileUploadBytes stores bytes this server holds (a document it built).
func botFileUploadBytes(ctx context.Context, scope agentruntime.Scope, name, mime string, raw []byte) (string, error) {
	created, err := botFileCreate(ctx, scope, name, mime, int64(len(raw)))
	if err != nil {
		return "", err
	}
	if created.Complete {
		return created.ID, nil
	}
	var offset int64
	for attempt := 0; offset < int64(len(raw)); attempt++ {
		if attempt > 6 {
			return "", fmt.Errorf("the upload to Hub kept failing at %d of %d bytes", offset, len(raw))
		}
		payload, status, err := botFileHub(ctx, http.MethodPut, "/api/v1/desktop-services/files/"+created.ID+"/data?offset="+strconv.FormatInt(offset, 10),
			map[string]string{"X-Upload-Token": created.UploadToken, "Content-Type": "application/octet-stream"}, bytes.NewReader(raw[offset:]), int64(len(raw))-offset)
		if err != nil {
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}
		var out botFileCreated
		_ = json.Unmarshal(payload, &out)
		if status == http.StatusConflict || status == http.StatusOK {
			offset = out.Received
			if out.Complete {
				return created.ID, nil
			}
			continue
		}
		if _, err := botFileDecode(payload, status); err != nil {
			return "", err
		}
	}
	return created.ID, nil
}

var botFileIDRe = regexp.MustCompile(`^bf_[0-9a-f]{23}$`)

// botFileStoreFromDesktop streams a file on the bot's cloud desktop into
// the store with the desktop helper and waits for it (up to wait).
func botFileStoreFromDesktop(ctx context.Context, scope agentruntime.Scope, filePath, name string, wait time.Duration) (string, int64, error) {
	out, err := localRelayBash(ctx, scope, "stat -c %s -- "+shellQuote(filePath))
	size, perr := strconv.ParseInt(strings.TrimSpace(lastLine(out)), 10, 64)
	if err != nil || perr != nil {
		return "", 0, fmt.Errorf("%s is not a readable file in the cloud desktop", filePath)
	}
	created, err := botFileCreate(ctx, scope, name, desktopDeliverMIME(name), size)
	if err != nil {
		return "", size, err
	}
	if !botFileIDRe.MatchString(created.ID) {
		return "", size, fmt.Errorf("Hub returned an invalid file id")
	}
	if created.Complete {
		return created.ID, size, nil
	}
	st, err := localRelayHelperRun(ctx, scope, created.ID, created.UploadToken, "store --id "+created.ID+" --file "+shellQuote(filePath), 100)
	if err != nil {
		return "", size, err
	}
	deadline := time.Now().Add(wait)
	for st.State == "running" || st.State == "starting" {
		if time.Now().After(deadline) {
			return created.ID, size, fmt.Errorf("the upload to the server is still running (%s of %s); check later with local_computer action=transfer_status id=%s", desktopHumanSize(int(st.Done)), desktopHumanSize(int(size)), created.ID)
		}
		select {
		case <-ctx.Done():
			return "", size, ctx.Err()
		case <-time.After(5 * time.Second):
		}
		st, err = localRelayReadState(ctx, scope, created.ID)
		if err != nil {
			return "", size, err
		}
	}
	if st.State != "done" {
		msg := strings.TrimSpace(st.Error)
		if msg == "" {
			msg = "the upload stopped"
		}
		return "", size, fmt.Errorf("the file could not be uploaded to the server: %s", msg)
	}
	return created.ID, size, nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// desktopSayFileRef sends one stored file to the person as its own chat
// message; the message carries the reference, not the bytes.
func desktopSayFileRef(ctx context.Context, scope agentruntime.Scope, name, ref string, size int64) (string, error) {
	owner := desktopOwnerUserID(scope.UserID)
	if inst := strings.TrimSpace(scope.InstanceID); inst == "" || owner == "" {
		return "", fmt.Errorf("desktop document could not be attached: this run has no bot instance to send it from")
	}
	body := map[string]any{
		"tenant_id":   strings.TrimSpace(scope.TenantID),
		"user_id":     owner,
		"instance_id": scope.InstanceID,
		"room":        "",
		"text":        fmt.Sprintf("文件：%s（%s）", name, desktopHumanSize(int(size))),
		"key":         desktopMessageKey(scope.InstanceID+"|"+scope.RunID, "deliver-ref", name, ref),
		"files":       []map[string]string{{"name": name, "ref": ref}},
	}
	if err := desktopDeliverLargeSay(ctx, body); err != nil && !strings.Contains(err.Error(), "ALREADY_SAID") && !strings.Contains(err.Error(), "409") {
		return "", fmt.Errorf("desktop document (%s) was uploaded but its message could not be sent: %v. Tell the person its path on the computer in your final reply", desktopHumanSize(int(size)), err)
	}
	return fmt.Sprintf("Delivered %s (%s) as its own chat message: the person's MaClaw app downloads it from the server and saves it in this bot's folder on their computer. Say in your final reply that the file was sent as a separate message. Do not shrink, re-render or deliver it again.", name, desktopHumanSize(int(size))), nil
}

// deliverDesktopViaStore uploads a desktop file to the store and sends the
// reference.
func deliverDesktopViaStore(ctx context.Context, scope agentruntime.Scope, resolved, name string) (string, error) {
	ref, size, err := botFileStoreFromDesktop(ctx, scope, resolved, name, 20*time.Minute)
	if err != nil {
		return "", fmt.Errorf("desktop document could not be sent: %v. Do not shrink it: tell the person its path on the computer in your final reply", err)
	}
	return desktopSayFileRef(ctx, scope, name, ref, size)
}
