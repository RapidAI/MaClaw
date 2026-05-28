package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
)

const hubRuntimeStatusResponseBodyLimit = 1 << 20
const hubRuntimeStatusMaxConcurrentFetches = 16

type hubRuntimeStatusView struct {
	HubID             string   `json:"hub_id"`
	HubName           string   `json:"hub_name"`
	BaseURL           string   `json:"base_url"`
	FetchOK           bool     `json:"fetch_ok"`
	FetchError        string   `json:"fetch_error,omitempty"`
	HTTPStatus        int      `json:"http_status,omitempty"`
	Status            string   `json:"status,omitempty"`
	ModelDir          string   `json:"model_dir,omitempty"`
	PublicModelsURL   string   `json:"public_models_url,omitempty"`
	Initialized       bool     `json:"initialized"`
	Downloading       bool     `json:"downloading"`
	Ready             bool     `json:"ready"`
	ExpectedFiles     []string `json:"expected_files,omitempty"`
	MissingFiles      []string `json:"missing_files,omitempty"`
	LogTail           []string `json:"log_tail,omitempty"`
	LastDownloadError string   `json:"last_download_error,omitempty"`
}

type hubRuntimeHub struct {
	id      string
	name    string
	baseURL string
}

func ListHubRuntimeStatusesHandler(service *hubs.Service) http.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeError(w, http.StatusInternalServerError, "LIST_HUB_RUNTIME_FAILED", "hub service is unavailable")
			return
		}
		hubsList, err := service.ListHubs(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_HUB_RUNTIME_FAILED", err.Error())
			return
		}
		items := make([]hubRuntimeHub, 0, len(hubsList))
		for _, item := range hubsList {
			if item == nil {
				continue
			}
			items = append(items, hubRuntimeHub{id: item.ID, name: item.Name, baseURL: item.BaseURL})
		}
		results := fetchHubRuntimeStatuses(r.Context(), client, items, hubRuntimeStatusMaxConcurrentFetches)
		writeJSON(w, http.StatusOK, map[string]any{"items": results})
	}
}

func fetchHubRuntimeStatuses(ctx context.Context, client *http.Client, items []hubRuntimeHub, maxConcurrent int) []hubRuntimeStatusView {
	results := make([]hubRuntimeStatusView, len(items))
	if len(items) == 0 {
		return results
	}
	if maxConcurrent <= 0 {
		maxConcurrent = hubRuntimeStatusMaxConcurrentFetches
	}
	if maxConcurrent > len(items) {
		maxConcurrent = len(items)
	}

	type job struct {
		idx int
		hub hubRuntimeHub
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	for worker := 0; worker < maxConcurrent; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				results[item.idx] = fetchHubRuntimeStatus(ctx, client, item.hub)
			}
		}()
	}
	for i, item := range items {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return results
		case jobs <- job{idx: i, hub: item}:
		}
	}
	close(jobs)
	wg.Wait()
	return results
}

func fetchHubRuntimeStatus(parent context.Context, client *http.Client, hub hubRuntimeHub) hubRuntimeStatusView {
	view := hubRuntimeStatusView{HubID: hub.id, HubName: hub.name, BaseURL: hub.baseURL}
	baseURL := strings.TrimRight(strings.TrimSpace(hub.baseURL), "/")
	if baseURL == "" {
		view.FetchError = "hub base url is empty"
		return view
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/public/model_download/status", nil)
	if err != nil {
		view.FetchError = err.Error()
		return view
	}
	resp, err := client.Do(req)
	if err != nil {
		view.FetchError = err.Error()
		return view
	}
	defer resp.Body.Close()
	view.HTTPStatus = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		view.FetchError = resp.Status
		return view
	}
	var payload struct {
		Status            string   `json:"status"`
		ModelDir          string   `json:"model_dir"`
		PublicModelsURL   string   `json:"public_models_url"`
		Initialized       bool     `json:"initialized"`
		Downloading       bool     `json:"downloading"`
		Ready             bool     `json:"ready"`
		ExpectedFiles     []string `json:"expected_files"`
		MissingFiles      []string `json:"missing_files"`
		LogTail           []string `json:"log_tail"`
		LastDownloadError string   `json:"last_download_error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, hubRuntimeStatusResponseBodyLimit)).Decode(&payload); err != nil {
		view.FetchError = err.Error()
		return view
	}
	view.FetchOK = true
	view.Status = payload.Status
	view.ModelDir = payload.ModelDir
	view.PublicModelsURL = payload.PublicModelsURL
	view.Initialized = payload.Initialized
	view.Downloading = payload.Downloading
	view.Ready = payload.Ready
	view.ExpectedFiles = payload.ExpectedFiles
	view.MissingFiles = payload.MissingFiles
	view.LogTail = payload.LogTail
	view.LastDownloadError = payload.LastDownloadError
	return view
}
