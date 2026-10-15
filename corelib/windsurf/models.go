package windsurf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Model is one Cascade model the signed-in Devin account can call.
type Model struct {
	ID   string
	Name string
}

// ListModels asks the Devin API for the account's Cascade model catalog.
func ListModels(ctx context.Context, apiServer, apiKey string) ([]Model, error) {
	apiServer = strings.TrimRight(strings.TrimSpace(apiServer), "/")
	apiKey = strings.TrimSpace(apiKey)
	if apiServer == "" || apiKey == "" {
		return nil, fmt.Errorf("请先完成 Devin 登录")
	}
	body := encodeBytes(1, buildMetadata(apiKey, "", newID(), uint64(time.Now().UnixMilli()), newID()))
	endpoint := apiServer + "/exa.api_server_pb.ApiServerService/GetCascadeModelConfigs"
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/proto")
	req.Header.Set("Connect-Protocol-Version", "1")
	resp, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("列出 Devin 模型失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("列出 Devin 模型失败: %s", connectError(resp.StatusCode, raw))
	}
	fields, err := iterFields(raw)
	if err != nil {
		return nil, fmt.Errorf("解析 Devin 模型列表失败: %w", err)
	}
	seen := map[string]struct{}{}
	var models []Model
	for _, f := range fields {
		if f.num != 1 || f.wire != 2 {
			continue
		}
		blob, ok := f.value.([]byte)
		if !ok {
			continue
		}
		sub, err := iterFields(blob)
		if err != nil {
			continue
		}
		if disabled, ok := fieldVarint(sub, 4); ok && disabled == 1 {
			continue
		}
		id := fieldString(sub, 22)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		name := fieldString(sub, 1)
		if name == "" {
			name = id
		}
		models = append(models, Model{ID: id, Name: name})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("Devin 未返回可用模型")
	}
	return models, nil
}
